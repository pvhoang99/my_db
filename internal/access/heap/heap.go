package heap

// Heap là một bảng: một file gồm các page heap, mỗi page chứa nhiều tuple.
//
// Ba thao tác, tương ứng src/backend/access/heap/heapam.c:
//
//	Insert(giá trị)  -> ctid      heap_insert()
//	Fetch(ctid)      -> giá trị   heap_fetch()
//	Scan()           -> mọi dòng  heapgettup() — chính là "Seq Scan" trong EXPLAIN
//
// Chèn vào page nào? Giống Postgres khi không có chỗ trống cũ để dùng lại:
//
//	1. thử page CUỐI của bảng
//	2. không đủ chỗ thì nới file thêm MỘT page mới và chèn vào đó
//
// Postgres còn có Free Space Map (file _fsm) để tìm page cũ còn chỗ sau khi
// VACUUM dọn dẹp. Bảng chỉ có INSERT thì không cần tới, nên để dành cho M4.3.
//
// Mọi page đều đi qua buffer pool (M2.1), không đọc/ghi file trực tiếp:
//
//	heap --ReadBuffer--> buffer pool --ReadPage/WritePage--> file
//
// Mỗi lần dùng page đều theo đúng nhịp của Postgres: ghim -> dùng -> (đánh dấu
// bẩn nếu sửa) -> bỏ ghim.

import (
	"errors"
	"fmt"

	"github.com/hoangpv/minidb/internal/buffer"
	"github.com/hoangpv/minidb/internal/common"
	"github.com/hoangpv/minidb/internal/storage"
	"github.com/hoangpv/minidb/internal/types"
)

// MaxHeapTupleSize: tuple lớn nhất vừa một page trống (8192 - 24 - 4 = 8164).
// Postgres đẩy giá trị lớn ra bảng phụ (TOAST) từ khoảng 2KB; minidb chưa có
// TOAST nên chỉ từ chối tuple quá lớn.
const MaxHeapTupleSize = storage.BLCKSZ - storage.SizeOfPageHeaderData - storage.ItemIdSize

var (
	ErrTupleTooLarge = errors.New("heap: tuple quá lớn, chưa hỗ trợ TOAST")
	ErrNotFound      = errors.New("heap: không có tuple ở ctid này")
)

// Heap trong RAM chỉ còn là vài con số; nội dung page nằm trong buffer pool:
//
//	Heap                         buffer pool (dùng chung mọi bảng)      ĐĨA
//	+------------------+         +-----+-----+-----+-----+             +--------------+
//	| file  ---------- +-------> |users|users|idx  | ... | <---------> | file users   |
//	| desc             |         | p0  | p735|  p3 |     |  đọc/ghi    | p0 ... p735  |
//	| pool  ---------- +-------> +-----+-----+-----+-----+             +--------------+
//	| targetBlock=735  |
//	+------------------+
type Heap struct {
	file *storage.File // 1. file của bảng trên đĩa
	desc TupleDesc     // 2. bảng có những cột nào, kiểu gì (để Form/Deform tuple)
	pool *buffer.Pool  // 3. buffer pool dùng chung; mọi page đều lấy qua đây

	// 4. Page lần trước đã chèn vào, để lần chèn sau thử page đó trước.
	//    Chỉ là MỘT CON SỐ, không giữ nội dung page. Tương ứng rd_targblock của
	//    Postgres. InvalidBlockNumber = chưa biết (vừa mở bảng).
	targetBlock common.BlockNumber
}

// Open mở (hoặc tạo) bảng ở path với các cột desc. Page của bảng sẽ được giữ
// trong pool, có thể dùng chung với các bảng khác.
func Open(path string, desc TupleDesc, pool *buffer.Pool) (*Heap, error) {
	f, err := storage.OpenFile(path)
	if err != nil {
		return nil, err
	}
	return &Heap{file: f, desc: desc, pool: pool, targetBlock: common.InvalidBlockNumber}, nil
}

// Insert chèn một dòng và trả về địa chỉ (ctid) của nó.
// xid là transaction đang chèn; nó được ghi vào t_xmin (M4 mới dùng tới).
func (h *Heap) Insert(values []types.Datum, xid uint32) (common.ItemPointer, error) {
	// Bước 0: biến các giá trị thành byte (header 23 byte + dữ liệu cột).
	tup, err := FormTuple(h.desc, values, xid)
	if err != nil {
		return common.ItemPointer{}, err
	}
	if len(tup) > MaxHeapTupleSize {
		return common.ItemPointer{}, fmt.Errorf("%w: %d byte", ErrTupleTooLarge, len(tup))
	}

	// Bước 1–2: lấy một page đủ chỗ, đã ghim (page cuối, hoặc page mới).
	buf, blk, err := h.bufferForTuple(alignUp(len(tup), 8))
	if err != nil {
		return common.ItemPointer{}, err
	}
	defer h.pool.Release(buf) // bước cuối: bỏ ghim

	// Bước 3: tính trước ctid. AddItem luôn thêm ô mới ở cuối, nên số ô = số ô
	// hiện có + 1. t_ctid của tuple mới trỏ vào chính nó.
	p := h.pool.Page(buf)
	tid := common.ItemPointer{Block: blk, Offset: p.MaxOffsetNumber() + 1}
	SetCtid(tup, tid)

	// Bước 4: chép tuple vào page (trong pool) và đánh dấu page đã bẩn.
	off, err := p.AddItem(tup)
	if err != nil {
		return common.ItemPointer{}, err
	}
	if off != tid.Offset {
		return common.ItemPointer{}, fmt.Errorf("heap: ô dự kiến %d, nhận %d", tid.Offset, off)
	}
	h.pool.MarkDirty(buf)
	return tid, nil
}

// bufferForTuple trả về một page còn ít nhất size byte trống, đã ghim.
// Tương ứng RelationGetBufferForTuple() trong hio.c.
func (h *Heap) bufferForTuple(size int) (buffer.Buffer, common.BlockNumber, error) {
	// Vừa mở bảng thì chưa biết page cuối: hỏi file có bao nhiêu page.
	if h.targetBlock == common.InvalidBlockNumber {
		n, err := h.file.NumBlocks()
		if err != nil {
			return 0, 0, err
		}
		if n > 0 {
			h.targetBlock = n - 1
		}
	}

	// Bước 1: thử page cuối.
	if h.targetBlock != common.InvalidBlockNumber {
		buf, err := h.pool.ReadBuffer(h.file, h.targetBlock)
		if err != nil {
			return 0, 0, err
		}
		if h.pool.Page(buf).FreeSpace() >= size {
			return buf, h.targetBlock, nil
		}
		h.pool.Release(buf) // không đủ chỗ: trả lại
	}

	// Bước 2: nới file thêm một page trống, dọn thành page heap.
	buf, blk, err := h.pool.ExtendBuffer(h.file)
	if err != nil {
		return 0, 0, err
	}
	storage.PageInit(h.pool.Page(buf), 0)
	h.pool.MarkDirty(buf)
	h.targetBlock = blk
	return buf, blk, nil
}

// Fetch đọc dòng ở ctid. Tương ứng heap_fetch() (chưa kiểm MVCC — M4).
//
// Ba bước: lấy page tid.Block -> lấy ô tid.Offset -> giải mã tuple.
// Đây là nửa sau của "Index Scan": index cho ctid, Fetch lấy dòng ra.
func (h *Heap) Fetch(tid common.ItemPointer) (Header, []types.Datum, error) {
	n, err := h.file.NumBlocks()
	if err != nil {
		return Header{}, nil, err
	}
	if tid.Block >= n {
		return Header{}, nil, fmt.Errorf("%w: %v (bảng có %d page)", ErrNotFound, tid, n)
	}
	buf, err := h.pool.ReadBuffer(h.file, tid.Block)
	if err != nil {
		return Header{}, nil, err
	}
	defer h.pool.Release(buf)

	p := h.pool.Page(buf)
	if tid.Offset < common.FirstOffsetNumber || tid.Offset > p.MaxOffsetNumber() {
		return Header{}, nil, fmt.Errorf("%w: %v", ErrNotFound, tid)
	}
	tup, err := p.Item(tid.Offset)
	if err != nil {
		return Header{}, nil, err
	}
	if tup == nil { // ô chết, ô trống hoặc chuyển hướng
		return Header{}, nil, fmt.Errorf("%w: %v", ErrNotFound, tid)
	}
	// DeformTuple chép giá trị ra (string mới), nên bỏ ghim page sau đó vẫn an toàn.
	return DeformTuple(h.desc, tup)
}

// Fetcher lấy dòng theo ctid, GIỮ page heap đang mở giữa các lần lấy: ctid kế
// tiếp cùng page thì không phải ghim lại (không tính thêm một lần đọc buffer).
// Đây là cách Index Scan của Postgres lấy dòng (IndexFetchHeapData, xs_cbuf):
// nếu index trả ra các ctid theo thứ tự page thì mỗi page heap chỉ mở một lần.
//
//	f := h.NewFetcher()
//	defer f.Close()
//	vals, _ := f.Fetch(tid)
type Fetcher struct {
	h      *Heap
	buf    buffer.Buffer      // page heap đang mở (đang ghim)
	blk    common.BlockNumber // số page đó
	pinned bool

	PagesOpened int // số lần phải mở (ghim) một page heap — giống "Buffers" của phần heap
}

// NewFetcher tạo một Fetcher cho bảng.
func (h *Heap) NewFetcher() *Fetcher { return &Fetcher{h: h} }

// Fetch đọc dòng ở ctid. Chỉ mở page mới khi ctid nằm ở page khác page đang mở.
func (f *Fetcher) Fetch(tid common.ItemPointer) ([]types.Datum, error) {
	if !f.pinned || f.blk != tid.Block {
		f.Close()
		buf, err := f.h.pool.ReadBuffer(f.h.file, tid.Block)
		if err != nil {
			return nil, err
		}
		f.buf, f.blk, f.pinned = buf, tid.Block, true
		f.PagesOpened++
	}
	tup, err := f.h.pool.Page(f.buf).Item(tid.Offset)
	if err != nil {
		return nil, err
	}
	if tup == nil {
		return nil, fmt.Errorf("%w: %v", ErrNotFound, tid)
	}
	_, vals, err := DeformTuple(f.h.desc, tup)
	return vals, err
}

// Close bỏ ghim page đang mở.
func (f *Fetcher) Close() {
	if f.pinned {
		f.h.pool.Release(f.buf)
		f.pinned = false
	}
}

// NumBlocks là số page của bảng. Page mới luôn được ghi giữ chỗ vào file ngay
// khi nới (ExtendBuffer), nên kích thước file là đủ.
func (h *Heap) NumBlocks() (common.BlockNumber, error) { return h.file.NumBlocks() }

// Flush ghi mọi page bẩn trong pool xuống file và fsync.
func (h *Heap) Flush() error {
	if err := h.pool.FlushAll(); err != nil {
		return err
	}
	return h.file.Sync()
}

// Close ghi nốt, bỏ các page của bảng khỏi pool, rồi đóng file.
func (h *Heap) Close() error {
	if err := h.Flush(); err != nil {
		h.file.Close()
		return err
	}
	if err := h.pool.DropFile(h.file); err != nil {
		h.file.Close()
		return err
	}
	return h.file.Close()
}

// --- Seq scan ---

// Scanner đi qua mọi tuple còn sống, page 0 tới page cuối, ô 1 tới ô cuối.
// Đây chính là việc "Seq Scan" làm. Tương ứng heapgettup().
//
// Cách dùng:
//
//	s, _ := h.Scan()
//	defer s.Close()
//	for s.Next() {
//		fmt.Println(s.TID(), s.Values())
//	}
//	if err := s.Err(); err != nil { ... }
type Scanner struct {
	h      *Heap               // bảng đang quét
	nblk   common.BlockNumber  // số page lúc bắt đầu quét; dòng chèn sau đó không thấy
	blk    common.BlockNumber  // page đang đọc
	off    common.OffsetNumber // ô vừa đọc xong trong page đó (0 = chưa đọc ô nào)
	buf    buffer.Buffer       // ô của pool đang giữ page blk (đang ghim)
	pinned bool                // đang ghim buf không

	tid    common.ItemPointer // ctid của dòng hiện tại (sau khi Next trả về true)
	values []types.Datum      // giá trị các cột của dòng hiện tại
	err    error              // lỗi làm dừng lượt quét, nếu có

	PagesRead int // số page đã đụng tới (hit + read) — giống "Buffers" trong EXPLAIN
}

// Scan bắt đầu một lượt quét toàn bảng.
func (h *Heap) Scan() (*Scanner, error) {
	n, err := h.NumBlocks()
	if err != nil {
		return nil, err
	}
	return &Scanner{h: h, nblk: n}, nil
}

// Next tiến tới tuple kế tiếp. Trả về false khi hết bảng hoặc có lỗi (xem Err).
//
// Hai vòng lặp lồng nhau: ngoài đi từng page, trong đi từng ô của page đó.
// Mỗi page được ghim trong lúc đọc các ô của nó, bỏ ghim khi sang page sau.
func (s *Scanner) Next() bool {
	for s.blk < s.nblk {
		// Sang page mới: ghim page đó trong pool rồi bắt đầu từ ô 1.
		if !s.pinned {
			buf, err := s.h.pool.ReadBuffer(s.h.file, s.blk)
			if err != nil {
				s.err = err
				return false
			}
			s.buf, s.pinned, s.off = buf, true, 0
			s.PagesRead++
		}
		p := s.h.pool.Page(s.buf)

		// Đi tiếp các ô còn lại của page này.
		for s.off < p.MaxOffsetNumber() {
			s.off++
			tup, err := p.Item(s.off)
			if err != nil {
				s.err = err
				return false
			}
			if tup == nil {
				continue // bỏ qua ô chết / trống
			}
			_, vals, err := DeformTuple(s.h.desc, tup)
			if err != nil {
				s.err = err
				return false
			}
			s.tid = common.ItemPointer{Block: s.blk, Offset: s.off}
			s.values = vals
			return true
		}

		// Hết ô trong page này: bỏ ghim, sang page kế tiếp.
		s.Close()
		s.blk++
	}
	return false // hết bảng
}

// Close bỏ ghim page đang đọc. Gọi khi dừng quét giữa chừng (Next tự gọi khi hết).
func (s *Scanner) Close() {
	if s.pinned {
		s.h.pool.Release(s.buf)
		s.pinned = false
	}
}

func (s *Scanner) TID() common.ItemPointer { return s.tid }
func (s *Scanner) Values() []types.Datum   { return s.values }
func (s *Scanner) Err() error              { return s.err }
