// Package storage cài đặt tầng thấp nhất: slotted page 8KB và heap file trên disk.
//
// Layout một page, mô phỏng Postgres (src/include/storage/bufpage.h):
//
//	┌────────────────────────────────────────────────────────────┐
//	│ PageHeader (16B)                                           │
//	├────────────────────────────────────────────────────────────┤
//	│ ItemId[0] ItemId[1] ... ItemId[n-1]   ──► mọc xuống          │
//	├─────────────────────────┬──────────────────────────────────┤
//	│      free space         │  ◄── tuple mọc lên                │
//	├─────────────────────────┴──────────────────────────────────┤
//	│ ... tuple[n-1] ... tuple[1] ... tuple[0]                    │
//	└────────────────────────────────────────────────────────────┘
//	                          ▲                 ▲
//	                        lower             upper
package storage

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	// PageSize là 8KB, đúng bằng BLCKSZ mặc định của Postgres.
	PageSize = 8192

	pageHeaderSize = 16
	itemIDSize     = 4
)

var (
	ErrPageFull    = errors.New("storage: không đủ free space trong page")
	ErrSlotInvalid = errors.New("storage: slot không tồn tại")
	ErrSlotDead    = errors.New("storage: slot đã bị xoá")
	ErrTupleTooBig = errors.New("storage: tuple lớn hơn sức chứa một page")
)

// Page là một block 8KB nằm trong bộ nhớ. Toàn bộ state nằm trong mảng byte
// nên page có thể ghi thẳng xuống disk mà không cần serialize thêm.
type Page [PageSize]byte

// Các field của PageHeader, truy cập trực tiếp trên byte array.
//
//	[0:8]   LSN     — vị trí WAL record cuối cùng sửa page này (dùng ở M3)
//	[8:10]  lower   — offset kết thúc mảng ItemId
//	[10:12] upper   — offset bắt đầu vùng tuple
//	[12:14] special — offset vùng special (B+tree dùng ở M2)
//	[14:16] flags
const (
	offLSN     = 0
	offLower   = 8
	offUpper   = 10
	offSpecial = 12
	offFlags   = 14
)

// NewPage tạo một page rỗng đã khởi tạo header.
func NewPage() *Page {
	p := new(Page)
	p.Init()
	return p
}

// Init reset page về trạng thái rỗng (PageInit trong Postgres).
func (p *Page) Init() {
	clear(p[:])
	p.setUint16(offLower, pageHeaderSize)
	p.setUint16(offUpper, PageSize)
	p.setUint16(offSpecial, PageSize)
}

func (p *Page) uint16At(off int) uint16 { return binary.LittleEndian.Uint16(p[off:]) }
func (p *Page) setUint16(off int, v uint16) {
	binary.LittleEndian.PutUint16(p[off:], v)
}

// LSN trả về log sequence number của thay đổi cuối cùng trên page.
func (p *Page) LSN() uint64 { return binary.LittleEndian.Uint64(p[offLSN:]) }

// SetLSN ghi lại LSN; WAL dùng nó để quyết định có cần redo page hay không.
func (p *Page) SetLSN(lsn uint64) { binary.LittleEndian.PutUint64(p[offLSN:], lsn) }

func (p *Page) lower() uint16 { return p.uint16At(offLower) }
func (p *Page) upper() uint16 { return p.uint16At(offUpper) }

// NumSlots trả về số slot đã cấp phát, kể cả slot đã xoá.
// Slot đã xoá vẫn chiếm chỗ vì TID (page, slot) phải ổn định.
func (p *Page) NumSlots() int {
	return int(p.lower()-pageHeaderSize) / itemIDSize
}

// FreeSpace là số byte còn trống, đã trừ đi ItemId cần thêm cho tuple mới.
func (p *Page) FreeSpace() int {
	free := int(p.upper()) - int(p.lower())
	if free < 0 {
		return 0
	}
	return free
}

// itemID đọc cặp (offset, length) của slot thứ i.
func (p *Page) itemID(slot int) (off, length uint16) {
	base := pageHeaderSize + slot*itemIDSize
	return p.uint16At(base), p.uint16At(base + 2)
}

func (p *Page) setItemID(slot int, off, length uint16) {
	base := pageHeaderSize + slot*itemIDSize
	p.setUint16(base, off)
	p.setUint16(base+2, length)
}

// Insert ghi tuple vào page và trả về số hiệu slot.
// Slot number chính là phần "offset" trong TID của Postgres.
func (p *Page) Insert(tuple []byte) (int, error) {
	if len(tuple) > PageSize-pageHeaderSize-itemIDSize {
		return 0, ErrTupleTooBig
	}
	if len(tuple)+itemIDSize > p.FreeSpace() {
		return 0, ErrPageFull
	}

	newUpper := p.upper() - uint16(len(tuple))
	copy(p[newUpper:], tuple)

	slot := p.NumSlots()
	p.setUint16(offUpper, newUpper)
	p.setUint16(offLower, p.lower()+itemIDSize)
	p.setItemID(slot, newUpper, uint16(len(tuple)))
	return slot, nil
}

// Get trả về nội dung tuple tại slot. Slice trỏ thẳng vào page nên chỉ hợp lệ
// chừng nào page chưa bị sửa; copy ra ngoài nếu cần giữ lâu.
func (p *Page) Get(slot int) ([]byte, error) {
	if slot < 0 || slot >= p.NumSlots() {
		return nil, fmt.Errorf("%w: slot %d", ErrSlotInvalid, slot)
	}
	off, length := p.itemID(slot)
	if off == 0 {
		return nil, fmt.Errorf("%w: slot %d", ErrSlotDead, slot)
	}
	return p[off : off+length], nil
}

// Delete đánh dấu slot là dead. Không gom lại free space ngay —
// giống Postgres, việc đó để dành cho VACUUM (M4).
func (p *Page) Delete(slot int) error {
	if slot < 0 || slot >= p.NumSlots() {
		return fmt.Errorf("%w: slot %d", ErrSlotInvalid, slot)
	}
	off, _ := p.itemID(slot)
	if off == 0 {
		return fmt.Errorf("%w: slot %d", ErrSlotDead, slot)
	}
	p.setItemID(slot, 0, 0)
	return nil
}
