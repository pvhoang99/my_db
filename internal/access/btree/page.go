// Package btree là index B-tree của minidb, theo bố cục byte của nbtree trong
// Postgres. Ở bước M2.2 nó ĐỌC và TÌM KIẾM được trên file index thật của Postgres.
//
// Tương ứng trong Postgres:
//
//	src/include/access/nbtree.h        BTMetaPageData, BTPageOpaqueData, cờ BTP_*
//	src/include/access/itup.h          IndexTupleData (header 8 byte của mỗi mục)
//	src/backend/access/nbtree/nbtsearch.c   _bt_search, _bt_binsrch, _bt_moveright
//	docs/tham-khao/nbtree-README-pg18.txt   giải thích thuật toán
//
// Một page B-tree dùng chung khung slotted page với heap (storage.Page), chỉ khác:
//
//	+--------+-----------+--------+----------------------+------------------+
//	| header | ô1 ô2 ... | trống  | ... mục3 mục2 mục1    | SPECIAL 16 byte  |
//	+--------+-----------+--------+----------------------+------------------+
//	                                                       prev next level flags
//
//	- mỗi mục là một INDEX TUPLE: 8 byte header (ctid 6 + t_info 2) rồi tới key
//	- cuối page có 16 byte "special" (BTPageOpaqueData)
//	- page 0 là METAPAGE: page gốc ở đâu, cây cao mấy tầng
//
// Hai loại mục đặc biệt:
//
//	HIGH KEY  ô 1 của mọi page TRỪ page ngoài cùng bên phải: mọi key trong page
//	          đều <= high key.
//	−∞        mục dữ liệu đầu tiên của page giữa/gốc: không có key, nghĩa là
//	          "mọi thứ nhỏ hơn mốc kế tiếp".
package btree

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/hoangpv/minidb/internal/common"
	"github.com/hoangpv/minidb/internal/storage"
)

// --- Metapage (page 0) — BTMetaPageData ---

// BTREE_MAGIC đánh dấu "đây là index B-tree". Có ở byte 24 của page 0.
const BTREE_MAGIC = 0x053162

// Meta là nội dung metapage, nằm ngay sau page header (byte 24):
//
//	byte 24  btm_magic     4   luôn 0x053162
//	byte 28  btm_version   4   phiên bản (PG 13+: 4)
//	byte 32  btm_root      4   page gốc là page số mấy
//	byte 36  btm_level     4   tầng của page gốc (0 = gốc cũng là lá)
//	byte 40  btm_fastroot  4   page gốc "nhanh" (thường = root)
//	byte 44  btm_fastlevel 4
type Meta struct {
	Magic    uint32             // phải bằng BTREE_MAGIC
	Version  uint32             // phiên bản định dạng
	Root     common.BlockNumber // page gốc
	Level    uint32             // tầng của page gốc; số tầng của cây = Level + 1
	FastRoot common.BlockNumber // page thấp nhất mà chỉ có một nhánh (thường = Root)
}

// readMeta đọc metapage. Tương ứng _bt_getmeta().
func readMeta(p storage.Page) (Meta, error) {
	le := binary.LittleEndian
	c := p[storage.SizeOfPageHeaderData:] // nội dung bắt đầu sau page header
	m := Meta{
		Magic:    le.Uint32(c[0:]),
		Version:  le.Uint32(c[4:]),
		Root:     common.BlockNumber(le.Uint32(c[8:])),
		Level:    le.Uint32(c[12:]),
		FastRoot: common.BlockNumber(le.Uint32(c[16:])),
	}
	if m.Magic != BTREE_MAGIC {
		return m, fmt.Errorf("btree: page 0 không phải metapage (magic = %#x)", m.Magic)
	}
	return m, nil
}

// --- Special space — BTPageOpaqueData (16 byte cuối page) ---

// Cờ trong btpo_flags.
const (
	BTP_LEAF = 1 << 0 // page lá
	BTP_ROOT = 1 << 1 // page gốc
	BTP_META = 1 << 3 // metapage
)

// P_NONE: "không có page bên trái/phải".
const P_NONE common.BlockNumber = 0

// Opaque là 16 byte special ở cuối mỗi page B-tree:
//
//	btpo_prev    4   page bên trái cùng tầng (P_NONE = ngoài cùng bên trái)
//	btpo_next    4   page bên phải cùng tầng (P_NONE = ngoài cùng bên phải) — "right-link"
//	btpo_level   4   tầng: 0 = lá, tăng dần lên tới gốc
//	btpo_flags   2   cờ BTP_*
//	btpo_cycleid 2   dùng khi VACUUM (M4)
type Opaque struct {
	Prev  common.BlockNumber // page bên trái
	Next  common.BlockNumber // page bên phải (right-link)
	Level uint32             // tầng
	Flags uint16             // BTP_LEAF, BTP_ROOT, ...
}

func readOpaque(p storage.Page) Opaque {
	le := binary.LittleEndian
	s := p[p.Special():] // special bắt đầu ở pd_special (8176 với page B-tree)
	return Opaque{
		Prev:  common.BlockNumber(le.Uint32(s[0:])),
		Next:  common.BlockNumber(le.Uint32(s[4:])),
		Level: le.Uint32(s[8:]),
		Flags: le.Uint16(s[12:]),
	}
}

func (o Opaque) IsLeaf() bool      { return o.Flags&BTP_LEAF != 0 }
func (o Opaque) IsRightmost() bool { return o.Next == P_NONE }

// Số ô của high key và mục dữ liệu đầu tiên.
const (
	P_HIKEY    common.OffsetNumber = 1 // high key luôn ở ô 1 (nếu có)
	P_FIRSTKEY common.OffsetNumber = 2 // mục dữ liệu đầu tiên khi CÓ high key
)

// FirstDataKey: page ngoài cùng bên phải không có high key, nên dữ liệu bắt đầu
// từ ô 1; các page khác ô 1 là high key, dữ liệu bắt đầu từ ô 2.
// Tương ứng macro P_FIRSTDATAKEY().
func (o Opaque) FirstDataKey() common.OffsetNumber {
	if o.IsRightmost() {
		return P_HIKEY
	}
	return P_FIRSTKEY
}

// --- Index tuple — IndexTupleData (itup.h) ---

// Các bit trong t_info (2 byte sau ctid).
const (
	INDEX_SIZE_MASK     = 0x1FFF // 13 bit thấp: độ dài cả mục (byte)
	INDEX_ALT_TID_MASK  = 0x2000 // nbtree: ctid được dùng theo nghĩa khác (pivot, posting list)
	INDEX_NULL_MASK     = 0x8000 // có key NULL -> có null bitmap
	sizeofIndexTupleHdr = 8      // ctid 6 + t_info 2
	BT_IS_POSTING       = 0x2000 // trong số ô của ctid: đây là posting list (dedup)

	// BT_PIVOT_HEAP_TID_ATTR: trong số ô của ctid (pivot): mục này còn giữ heap TID
	// ở 6 byte cuối. Không có cờ này = heap TID đã bị cắt (coi như −∞).
	BT_PIVOT_HEAP_TID_ATTR = 0x1000
	bt_OFFSET_MASK         = 0x0FFF // 12 bit thấp của số ô (pivot): số cột key còn giữ
)

// IndexTuple là một mục của page B-tree, đã tách ra:
//
//	byte 0..5  ctid    page lá: địa chỉ dòng trong heap
//	                   page giữa/gốc: số page CON để đi xuống ("downlink")
//	byte 6..7  t_info  độ dài + cờ
//	byte 8..   key     xếp theo đúng luật như cột trong heap tuple
//
// Mục ở page giữa/gốc và high key gọi là PIVOT. Pivot có cờ INDEX_ALT_TID_MASK;
// phần "số ô" của ctid khi đó không phải số ô mà là số cột key còn giữ (0 = −∞),
// cộng cờ BT_PIVOT_HEAP_TID_ATTR nếu pivot còn giữ heap TID ở 6 byte cuối.
type IndexTuple struct {
	TID  common.ItemPointer // xem chú thích ở trên: ý nghĩa tuỳ page lá hay page giữa
	Info uint16             // t_info
	Key  []byte             // phần key (sau 8 byte header); rỗng = −∞ hoặc key bị cắt

	// HeapTID: chỉ có ở pivot còn giữ heap TID (khi hai bên điểm tách có key bằng
	// nhau). nil = heap TID đã bị cắt, coi như −∞.
	HeapTID *common.ItemPointer
}

// isPivot: mục này là pivot (page giữa/gốc, hoặc high key) chứ không phải mục lá.
func (t IndexTuple) isPivot() bool {
	return t.Info&INDEX_ALT_TID_MASK != 0 && t.TID.Offset&BT_IS_POSTING == 0
}

// Downlink là page con mà mục này trỏ tới (chỉ có nghĩa ở page giữa/gốc).
func (t IndexTuple) Downlink() common.BlockNumber { return t.TID.Block }

// readIndexTuple đọc mục ở ô off.
func readIndexTuple(p storage.Page, off common.OffsetNumber) (IndexTuple, error) {
	raw, err := p.Item(off)
	if err != nil {
		return IndexTuple{}, err
	}
	if raw == nil {
		return IndexTuple{}, fmt.Errorf("btree: ô %d không chứa mục", off)
	}
	if len(raw) < sizeofIndexTupleHdr {
		return IndexTuple{}, fmt.Errorf("btree: mục ô %d chỉ dài %d byte", off, len(raw))
	}
	le := binary.LittleEndian
	t := IndexTuple{
		TID: common.ItemPointer{
			Block:  common.BlockNumber(uint32(le.Uint16(raw[0:]))<<16 | uint32(le.Uint16(raw[2:]))),
			Offset: common.OffsetNumber(le.Uint16(raw[4:])),
		},
		Info: le.Uint16(raw[6:]),
	}
	if t.Info&INDEX_NULL_MASK != 0 {
		return t, errNotSupported("key NULL")
	}
	size := int(t.Info & INDEX_SIZE_MASK)
	if size > len(raw) || size < sizeofIndexTupleHdr {
		return t, fmt.Errorf("btree: t_info nói mục dài %d, ô nói %d", size, len(raw))
	}
	t.Key = raw[sizeofIndexTupleHdr:size]
	// Pivot còn giữ heap TID: 6 byte cuối là heap TID, phần key kết thúc trước đó.
	if t.isPivot() && t.TID.Offset&BT_PIVOT_HEAP_TID_ATTR != 0 {
		ht := readTID(raw[size-6:])
		t.HeapTID = &ht
		t.Key = raw[sizeofIndexTupleHdr : size-8]
	}
	return t, nil
}

func readTID(b []byte) common.ItemPointer {
	le := binary.LittleEndian
	return common.ItemPointer{
		Block:  common.BlockNumber(uint32(le.Uint16(b[0:]))<<16 | uint32(le.Uint16(b[2:]))),
		Offset: common.OffsetNumber(le.Uint16(b[4:])),
	}
}

func writeTID(b []byte, tid common.ItemPointer) {
	le := binary.LittleEndian
	le.PutUint16(b[0:], uint16(tid.Block>>16))
	le.PutUint16(b[2:], uint16(tid.Block))
	le.PutUint16(b[4:], uint16(tid.Offset))
}

// --- Tạo index tuple (dùng khi chèn, M2.3) ---

// makeLeafTuple tạo mục lá: 8 byte header (ctid = địa chỉ dòng trong heap) + key.
// Độ dài làm tròn lên bội số của 8. Ví dụ key int4: 8 + 4 -> 16 byte.
func makeLeafTuple(key []byte, htid common.ItemPointer) []byte {
	size := alignUp8(sizeofIndexTupleHdr + len(key))
	t := make([]byte, size)
	writeTID(t, htid)
	binary.LittleEndian.PutUint16(t[6:], uint16(size))
	copy(t[sizeofIndexTupleHdr:], key)
	return t
}

// makePivot tạo pivot: downlink nằm ở phần "số page" của ctid, phần "số ô" ghi
// số cột key (1, hoặc 0 nếu là −∞). htid != nil thì giữ thêm heap TID ở 6 byte cuối.
//
//	−∞ trỏ page 1:      ctid (1,0), dài 8 byte, không có key   <- như Postgres: (1,0) itemlen 8
//	pivot 367 -> page 2: ctid (2,1), 8 byte header + key        <- (2,1) itemlen 16
func makePivot(key []byte, downlink common.BlockNumber, htid *common.ItemPointer) []byte {
	natts := uint16(1)
	if len(key) == 0 {
		natts = 0
	}
	size := alignUp8(sizeofIndexTupleHdr + len(key))
	if htid != nil {
		size += 8 // MAXALIGN(6 byte heap TID)
		natts |= BT_PIVOT_HEAP_TID_ATTR
	}
	t := make([]byte, size)
	writeTID(t, common.ItemPointer{Block: downlink, Offset: common.OffsetNumber(natts)})
	binary.LittleEndian.PutUint16(t[6:], uint16(size)|INDEX_ALT_TID_MASK)
	copy(t[sizeofIndexTupleHdr:], key)
	if htid != nil {
		writeTID(t[size-6:], *htid)
	}
	return t
}

// setDownlink sửa page con mà pivot raw trỏ tới (nằm ở 4 byte đầu).
func setDownlink(raw []byte, blk common.BlockNumber) {
	le := binary.LittleEndian
	le.PutUint16(raw[0:], uint16(blk>>16))
	le.PutUint16(raw[2:], uint16(blk))
}

func alignUp8(n int) int { return (n + 7) &^ 7 }

// --- Ghi metapage và special ---

// initPage dọn p thành page B-tree trống (chừa 16 byte special) với opaque o.
func initPage(p storage.Page, o Opaque) {
	storage.PageInit(p, 16)
	writeOpaque(p, o)
}

func writeOpaque(p storage.Page, o Opaque) {
	le := binary.LittleEndian
	s := p[p.Special():]
	le.PutUint32(s[0:], uint32(o.Prev))
	le.PutUint32(s[4:], uint32(o.Next))
	le.PutUint32(s[8:], o.Level)
	le.PutUint16(s[12:], o.Flags)
}

// writeMeta ghi metapage, giống _bt_initmetapage(). pd_lower trỏ ngay sau phần
// dữ liệu meta (24 + 48 = 72), như Postgres.
func writeMeta(p storage.Page, m Meta) {
	if p.IsNew() {
		initPage(p, Opaque{Flags: BTP_META})
	}
	le := binary.LittleEndian
	c := p[storage.SizeOfPageHeaderData:]
	le.PutUint32(c[0:], BTREE_MAGIC)
	le.PutUint32(c[4:], 4) // btm_version
	le.PutUint32(c[8:], uint32(m.Root))
	le.PutUint32(c[12:], m.Level)
	le.PutUint32(c[16:], uint32(m.FastRoot))
	le.PutUint32(c[20:], m.Level) // btm_fastlevel
	le.PutUint16(p[12:], storage.SizeOfPageHeaderData+48)
}

var ErrNotSupported = errors.New("btree: chưa hỗ trợ")

func errNotSupported(what string) error { return fmt.Errorf("%w: %s", ErrNotSupported, what) }
