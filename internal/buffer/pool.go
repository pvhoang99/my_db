// Package buffer là buffer pool: một vùng RAM chung giữ các page hay dùng, để
// không phải đọc file mỗi lần cần một page.
//
// Tương ứng trong Postgres:
//
//	src/backend/storage/buffer/bufmgr.c    ReadBuffer, ReleaseBuffer, MarkBufferDirty
//	src/backend/storage/buffer/freelist.c  StrategyGetBuffer (clock-sweep)
//	src/include/storage/buf_internals.h    BufferDesc, BufferTag
//	tham số shared_buffers                 = số ô của pool
//
// Hình dung:
//
//	heap, btree --ReadBuffer(file, page)--> POOL: [ô0][ô1][ô2]...[ôN-1]   (mỗi ô 8KB)
//	                                         có sẵn  -> HIT   (không chạm đĩa)
//	                                         chưa có -> READ  (đọc file, cất vào một ô)
//
// Hai con số hit/read chính là "Buffers: shared hit=… read=…" trong EXPLAIN.
//
// Cách dùng (giống Postgres: đọc -> dùng -> trả):
//
//	buf, _ := pool.ReadBuffer(file, blk) // ghim page vào RAM
//	p := pool.Page(buf)                  // đọc/sửa page
//	pool.MarkDirty(buf)                  // nếu đã sửa
//	pool.Release(buf)                    // bỏ ghim; page vẫn nằm trong pool
//
// Đơn giản hoá so với Postgres: chỉ một goroutine dùng pool (không có khoá
// từng ô), không có ring buffer cho seq scan lớn, chưa có luật WAL (M3).
package buffer

import (
	"errors"
	"fmt"

	"github.com/hoangpv/minidb/internal/common"
	"github.com/hoangpv/minidb/internal/storage"
)

// MaxUsageCount: usage count tối đa của một ô. Postgres: BM_MAX_USAGE_COUNT = 5.
const MaxUsageCount = 5

// Buffer là số thứ tự của một ô trong pool (0..N-1).
type Buffer int

// Tag cho biết một ô đang giữ page nào: file nào, page số mấy.
// Tương ứng BufferTag (Postgres dùng số định danh file, minidb dùng con trỏ *File).
type Tag struct {
	File  *storage.File
	Block common.BlockNumber
}

// desc là sổ ghi chép của một ô. Tương ứng BufferDesc.
type desc struct {
	tag        Tag  // ô này đang giữ page nào
	valid      bool // đã giữ một page chưa (false = ô trống, chưa dùng lần nào)
	dirty      bool // bản trong RAM đã sửa, mới hơn bản trên đĩa
	pinCount   int  // số người đang dùng; > 0 thì KHÔNG được đuổi
	usageCount int  // được dùng nhiều không (0..5); clock-sweep giảm dần
}

// Stats đếm số lần hit, read, write — giống "Buffers:" trong EXPLAIN.
type Stats struct {
	Hit     int // page có sẵn trong pool
	Read    int // page phải đọc từ file
	Written int // page bẩn phải ghi xuống file (khi bị đuổi hoặc Flush)
}

// Pool là buffer pool.
//
//	descs: [d0][d1][d2]...      sổ ghi chép của từng ô
//	pages: [8KB][8KB][8KB]...   nội dung page của từng ô (một mảng byte lớn)
//	table: (file, page) -> ô    tra nhanh "page này có sẵn chưa"
//	hand:  kim đồng hồ của clock-sweep
type Pool struct {
	descs []desc         // sổ ghi chép, mỗi ô một mục
	pages []byte         // nội dung các page, nối liền: ô i là pages[i*8192 : (i+1)*8192]
	table map[Tag]Buffer // page nào đang nằm ở ô nào (BufTable trong Postgres)
	hand  int            // kim clock-sweep: ô sẽ xét tiếp theo
	stats Stats          // đếm hit/read/written
}

var ErrNoFreeBuffer = errors.New("buffer: mọi ô đều đang bị ghim, không đuổi được ô nào")

// New tạo pool có n ô (n * 8KB RAM). Postgres: shared_buffers = 128MB = 16384 ô.
func New(n int) *Pool {
	return &Pool{
		descs: make([]desc, n),
		pages: make([]byte, n*storage.BLCKSZ),
		table: make(map[Tag]Buffer, n),
	}
}

// ReadBuffer trả về ô đang giữ page blk của file, đã ghim (pin).
// Có sẵn thì HIT; chưa có thì chọn một ô (clock-sweep), đọc page từ file vào đó.
// Tương ứng: ReadBuffer() trong bufmgr.c.
func (bp *Pool) ReadBuffer(f *storage.File, blk common.BlockNumber) (Buffer, error) {
	tag := Tag{File: f, Block: blk}

	// 1. Có sẵn trong pool: ghim, tăng usage, xong. Không chạm đĩa.
	if b, ok := bp.table[tag]; ok {
		bp.pin(b)
		bp.stats.Hit++
		return b, nil
	}

	// 2. Chưa có: tìm một ô để dùng (có thể phải đuổi page khác ra).
	b, err := bp.getVictim()
	if err != nil {
		return 0, err
	}

	// 3. Đọc page từ file vào ô đó.
	if err := f.ReadPage(blk, bp.Page(b)); err != nil {
		return 0, err
	}
	bp.stats.Read++
	bp.assign(b, tag)
	return b, nil
}

// ExtendBuffer nới file thêm một page trống ở cuối và trả về ô giữ page đó, đã ghim.
// Page mới toàn số 0; người gọi tự PageInit rồi MarkDirty.
// Tương ứng: ExtendBufferedRel() trong bufmgr.c.
func (bp *Pool) ExtendBuffer(f *storage.File) (Buffer, common.BlockNumber, error) {
	b, err := bp.getVictim()
	if err != nil {
		return 0, 0, err
	}
	p := bp.Page(b)
	clear(p)
	// Ghi page trống xuống cuối file ngay, để file "giữ chỗ" cho page này:
	// lần nới sau sẽ ra số page kế tiếp.
	blk, err := f.ExtendPage(p)
	if err != nil {
		return 0, 0, err
	}
	bp.assign(b, Tag{File: f, Block: blk})
	return b, blk, nil
}

// Page trả về 8192 byte của ô b. Sửa slice này là sửa page trong pool.
// Chỉ dùng khi đang ghim ô b.
func (bp *Pool) Page(b Buffer) storage.Page {
	off := int(b) * storage.BLCKSZ
	return storage.Page(bp.pages[off : off+storage.BLCKSZ])
}

// MarkDirty đánh dấu ô b đã sửa: trước khi bị đuổi phải ghi xuống file.
// Tương ứng: MarkBufferDirty().
func (bp *Pool) MarkDirty(b Buffer) { bp.descs[b].dirty = true }

// Release bỏ ghim ô b. Page vẫn nằm trong pool cho lần sau (có thể HIT).
// Tương ứng: ReleaseBuffer().
func (bp *Pool) Release(b Buffer) {
	if bp.descs[b].pinCount <= 0 {
		panic(fmt.Sprintf("buffer: Release ô %d chưa được ghim", b))
	}
	bp.descs[b].pinCount--
}

// FlushAll ghi mọi page bẩn xuống file (chưa fsync). Gần giống việc checkpoint
// làm (M3). Tương ứng: FlushRelationBuffers() / BufferSync().
func (bp *Pool) FlushAll() error {
	for i := range bp.descs {
		if err := bp.flush(Buffer(i)); err != nil {
			return err
		}
	}
	return nil
}

// DropFile bỏ mọi page của file f khỏi pool (sau khi đã Flush). Dùng khi đóng file:
// con trỏ *File cũ sắp vô hiệu, không được để tag nào còn trỏ vào nó.
func (bp *Pool) DropFile(f *storage.File) error {
	for i := range bp.descs {
		d := &bp.descs[i]
		if !d.valid || d.tag.File != f {
			continue
		}
		if d.pinCount > 0 {
			return fmt.Errorf("buffer: page %d của file vẫn đang bị ghim", d.tag.Block)
		}
		if err := bp.flush(Buffer(i)); err != nil {
			return err
		}
		delete(bp.table, d.tag)
		*d = desc{}
	}
	return nil
}

// Stats trả về số đếm hit/read/written từ lúc tạo pool.
func (bp *Pool) Stats() Stats { return bp.stats }

// Size là số ô của pool.
func (bp *Pool) Size() int { return len(bp.descs) }

// --- bên trong ---

// pin ghim ô b và tăng usage count (tối đa 5). Mỗi lần page được dùng là thêm
// một "cơ hội sống" trước clock-sweep.
func (bp *Pool) pin(b Buffer) {
	d := &bp.descs[b]
	d.pinCount++
	if d.usageCount < MaxUsageCount {
		d.usageCount++
	}
}

// assign gắn ô b cho page tag và ghim nó.
func (bp *Pool) assign(b Buffer, tag Tag) {
	d := &bp.descs[b]
	d.tag, d.valid, d.dirty = tag, true, false
	d.pinCount, d.usageCount = 0, 0
	bp.table[tag] = b
	bp.pin(b)
}

// getVictim chọn một ô để dùng bằng clock-sweep. Tương ứng StrategyGetBuffer().
//
// Kim quay vòng qua các ô:
//
//	ô trống (chưa dùng)  -> lấy luôn
//	ô đang bị ghim       -> bỏ qua
//	usage > 0            -> giảm usage 1, đi tiếp (cho thêm một cơ hội)
//	usage = 0            -> chọn; page cũ bẩn thì ghi xuống file trước
func (bp *Pool) getVictim() (Buffer, error) {
	n := len(bp.descs)
	// Mỗi vòng ít nhất một ô giảm usage, nên sau tối đa 6 vòng (usage 5 -> 0)
	// chắc chắn tìm được, trừ khi mọi ô đều bị ghim.
	for tries := 0; tries < n*(MaxUsageCount+1); tries++ {
		b := Buffer(bp.hand)
		bp.hand = (bp.hand + 1) % n
		d := &bp.descs[b]

		if !d.valid {
			return b, nil
		}
		if d.pinCount > 0 {
			continue
		}
		if d.usageCount > 0 {
			d.usageCount--
			continue
		}
		// Đuổi page cũ: bẩn thì ghi xuống trước, rồi xoá khỏi bảng tra.
		if err := bp.flush(b); err != nil {
			return 0, err
		}
		delete(bp.table, d.tag)
		d.valid = false
		return b, nil
	}
	return 0, ErrNoFreeBuffer
}

// flush ghi ô b xuống file nếu nó bẩn.
func (bp *Pool) flush(b Buffer) error {
	d := &bp.descs[b]
	if !d.valid || !d.dirty {
		return nil
	}
	if err := d.tag.File.WritePage(d.tag.Block, bp.Page(b)); err != nil {
		return err
	}
	d.dirty = false
	bp.stats.Written++
	return nil
}
