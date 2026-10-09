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
//
// Vì sao hai vùng mọc ngược chiều nhau? Số slot và kích thước tuple đều không
// biết trước; cho chúng ăn chung một vùng free space ở giữa thì page tự co giãn
// theo dữ liệu thực tế mà không phải chọn trước tỉ lệ nào cả.
package storage

import (
	"encoding/binary"
	"errors"
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

// Hai helper dưới đây viết sẵn — chỉ là plumbing đọc/ghi số trên byte array.
func (p *Page) uint16At(off int) uint16 { return binary.LittleEndian.Uint16(p[off:]) }
func (p *Page) setUint16(off int, v uint16) {
	binary.LittleEndian.PutUint16(p[off:], v)
}

// NewPage tạo một page rỗng đã khởi tạo header.
func NewPage() *Page {
	p := new(Page)
	p.Init()
	return p
}

// Init reset page về trạng thái rỗng (PageInit trong Postgres).
//
// TODO: xoá sạch byte array, rồi đặt lower/upper/special về đúng vị trí ban đầu.
// Gợi ý: page rỗng thì vùng ItemId chưa có phần tử nào, còn vùng tuple chưa bị
// lấn vào từ phía cuối page.
func (p *Page) Init() {
	panic("TODO: Init")
}

// LSN trả về log sequence number của thay đổi cuối cùng trên page.
func (p *Page) LSN() uint64 {
	panic("TODO: LSN")
}

// SetLSN ghi lại LSN; WAL dùng nó để quyết định có cần redo page hay không.
func (p *Page) SetLSN(lsn uint64) {
	panic("TODO: SetLSN")
}

func (p *Page) lower() uint16 { return p.uint16At(offLower) }
func (p *Page) upper() uint16 { return p.uint16At(offUpper) }

// NumSlots trả về số slot đã cấp phát, kể cả slot đã xoá.
// Slot đã xoá vẫn chiếm chỗ vì TID (page, slot) phải ổn định.
//
// TODO: suy ra từ lower — vùng ItemId bắt đầu ngay sau header, mỗi phần tử
// dài itemIDSize byte.
func (p *Page) NumSlots() int {
	panic("TODO: NumSlots")
}

// FreeSpace là số byte còn trống, tức khoảng hở giữa hai vùng đang mọc vào nhau.
//
// TODO: nhớ rằng mỗi tuple mới tốn thêm một ItemId nữa, nên caller phải so sánh
// len(tuple)+itemIDSize với giá trị này.
func (p *Page) FreeSpace() int {
	panic("TODO: FreeSpace")
}

// itemID đọc cặp (offset, length) của slot thứ i.
//
// TODO: tính địa chỉ base của slot rồi đọc hai uint16 liên tiếp.
func (p *Page) itemID(slot int) (off, length uint16) {
	panic("TODO: itemID")
}

func (p *Page) setItemID(slot int, off, length uint16) {
	panic("TODO: setItemID")
}

// Insert ghi tuple vào page và trả về số hiệu slot.
// Slot number chính là phần "offset" trong TID của Postgres.
//
// TODO các bước:
//  1. tuple dài hơn sức chứa tối đa của một page  -> ErrTupleTooBig
//  2. không đủ chỗ cho cả tuple lẫn ItemId mới    -> ErrPageFull
//  3. hạ upper xuống len(tuple) byte, copy tuple vào vị trí mới
//  4. nâng lower lên itemIDSize byte, ghi ItemId trỏ tới tuple vừa copy
//  5. trả về số hiệu slot vừa cấp
func (p *Page) Insert(tuple []byte) (int, error) {
	panic("TODO: Insert")
}

// Get trả về nội dung tuple tại slot. Slice nên trỏ thẳng vào page (không copy)
// để tránh cấp phát; caller tự copy nếu cần giữ lâu.
//
// TODO: slot ngoài [0, NumSlots) -> ErrSlotInvalid; slot đã xoá -> ErrSlotDead.
// Bọc lỗi bằng %w để errors.Is trong test nhận ra.
func (p *Page) Get(slot int) ([]byte, error) {
	panic("TODO: Get")
}

// Delete đánh dấu slot là dead. Không gom lại free space ngay —
// giống Postgres, việc đó để dành cho VACUUM (M4).
//
// TODO: quy ước ở đây là ItemId có offset == 0 nghĩa là dead (offset 0 không bao
// giờ hợp lệ vì vùng đó thuộc header).
func (p *Page) Delete(slot int) error {
	panic("TODO: Delete")
}
