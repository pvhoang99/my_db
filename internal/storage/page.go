// Package storage là tầng thấp nhất của minidb: page 8KB và file chứa các page.
//
// File này cài đặt "slotted page" với bố cục byte GIỐNG HỆT Postgres, nên đọc
// được cả page thật lấy ra từ một Postgres đang chạy (xem cmd/pagedump).
//
// Tương ứng trong Postgres:
//
//	src/include/storage/bufpage.h   PageHeaderData, các macro PageGet...
//	src/include/storage/itemid.h    ItemIdData (line pointer 4 byte)
//	src/backend/storage/page/bufpage.c  PageInit, PageAddItemExtended
//
// Bố cục một page:
//
//	0        24                    pd_lower        pd_upper          pd_special   8192
//	+--------+----+----+----+-------+---------------+--------+--------+-----------+
//	| header | lp1| lp2| lp3|  ...  |   (trống)     | tuple3 | tuple2 | tuple1 | special |
//	+--------+----+----+----+-------+---------------+--------+--------+-----------+
//	          mảng line pointer ->                   <- dữ liệu mọc ngược từ cuối
//
// Line pointer mọc từ đầu xuống, dữ liệu mọc từ cuối lên; hai bên gặp nhau thì
// page đầy. Nhờ vậy thêm một dòng KHÔNG phải dời dữ liệu cũ (khác với sách Go,
// nơi chèn một key làm dời gần hết node).
//
// Thuật ngữ: "line pointer" = "ô" = ItemId. Ô thứ n (đếm từ 1) cho biết tuple
// thứ n nằm ở byte nào trong page. ctid (0,5) nghĩa là page 0, ô số 5.
package storage

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/hoangpv/minidb/internal/common"
)

// Mọi kích thước và vị trí trong file này đều tính bằng BYTE.
const (
	// BLCKSZ là kích thước một page: 8192 byte = 8KB.
	// Postgres mặc định 8KB, InnoDB 16KB, sách Go 4KB.
	BLCKSZ = 8192

	// SizeOfPageHeaderData là kích thước header ở đầu mọi page: 24 byte
	// (8 trường cộng lại: 8+2+2+2+2+2+2+4, xem bảng vị trí bên dưới).
	SizeOfPageHeaderData = 24

	// ItemIdSize là kích thước một line pointer (một ô): 4 byte.
	ItemIdSize = 4

	// PG_PAGE_LAYOUT_VERSION là phiên bản bố cục page; Postgres dùng 4 từ bản 8.3.
	PG_PAGE_LAYOUT_VERSION = 4

	// maxAlign: mọi tuple bắt đầu ở vị trí chia hết cho 8 (MAXALIGN trên máy 64-bit).
	// CPU đọc số 8 byte nhanh nhất khi nó nằm ở địa chỉ chia hết cho 8.
	maxAlign = 8
)

// Vị trí (byte) các trường trong PageHeaderData (bufpage.h). Tổng cộng 24 byte:
//
//	byte  trường              dài  ý nghĩa
//	 0    pd_lsn              8    LSN của bản ghi WAL cuối cùng sửa page này (M3)
//	 8    pd_checksum         2    checksum của page (bật mặc định từ PG 18)
//	10    pd_flags            2    cờ, ví dụ "page có line pointer trống"
//	12    pd_lower            2    byte đầu tiên còn trống (= cuối mảng line pointer)
//	14    pd_upper            2    byte ngay sau chỗ trống (= đầu vùng dữ liệu)
//	16    pd_special          2    đầu vùng "special" (heap: không có; B-tree: 16 byte)
//	18    pd_pagesize_version 2    kích thước page | phiên bản bố cục
//	20    pd_prune_xid        4    transaction cũ nhất có thể dọn được (M4)
//
// Chỗ trống của page = từ pd_lower tới pd_upper.
const (
	offLSN             = 0
	offChecksum        = 8
	offFlags           = 10
	offLower           = 12
	offUpper           = 14
	offSpecial         = 16
	offPageSizeVersion = 18
	offPruneXid        = 20
)

// Page là đúng 8192 byte. Không có struct, không có con trỏ Go: mảng byte này
// ghi thẳng xuống đĩa được, và đọc thẳng từ đĩa lên được.
//
// Mọi hàm của Page đọc/ghi thẳng vào các byte này. Ví dụ p.Lower() chỉ là
// "đọc 2 byte ở vị trí 12".
type Page []byte

// NewPage cấp phát một page rỗng chưa khởi tạo (toàn số 0).
// Muốn dùng để chèn dòng thì phải gọi PageInit trước.
func NewPage() Page { return make(Page, BLCKSZ) }

// Lỗi trả về khi thao tác trên page.
var (
	ErrPageFull    = errors.New("storage: page không đủ chỗ")
	ErrBadOffset   = errors.New("storage: số ô không hợp lệ")
	ErrCorruptPage = errors.New("storage: page hỏng")
)

// PageInit khởi tạo một page trống, chừa specialSize byte ở cuối.
// Heap dùng specialSize = 0; B-tree của Postgres dùng 16 (BTPageOpaqueData:
// prev, next, level, flags). Tương ứng: PageInit() trong bufpage.c.
//
// Kết quả với heap (specialSize = 0):
//
//	0       24                                                  8192
//	| header |                    trống                            |
//	         ^ lower = 24                     upper = special = 8192 ^
func PageInit(p Page, specialSize int) {
	// Làm tròn lên bội số của 8: dữ liệu luôn bắt đầu ở vị trí chia hết cho 8.
	specialSize = alignUp(specialSize)

	// Xoá sạch 8192 byte về 0, như lấy một tờ giấy trắng mới.
	clear(p)

	// Chưa có ô nào, nên mảng ô kết thúc ngay sau header (byte 24).
	p.setU16(offLower, SizeOfPageHeaderData)

	// Chưa có dữ liệu, nên vùng dữ liệu "bắt đầu" ngay trước special space.
	p.setU16(offUpper, uint16(BLCKSZ-specialSize))

	// Special space nằm ở cuối page. Heap không có special nên nó = 8192.
	p.setU16(offSpecial, uint16(BLCKSZ-specialSize))

	// Gộp hai số vào 2 byte: 8192 = 0x2000 (8 bit thấp luôn là 0), OR với 4
	// thành 0x2004. Tách ra bằng PageSize() và LayoutVersion().
	p.setU16(offPageSizeVersion, BLCKSZ|PG_PAGE_LAYOUT_VERSION)
}

// --- Đọc các trường header: mỗi hàm chỉ đọc vài byte ở vị trí cố định ---

// LSN: vị trí trong WAL của lần sửa cuối cùng lên page này (dùng ở M3).
func (p Page) LSN() uint64 {
	// pd_lsn lưu thành 2 nửa 32-bit (xlogid, xrecoff), nửa cao trước.
	hi := binary.LittleEndian.Uint32(p[offLSN:])
	lo := binary.LittleEndian.Uint32(p[offLSN+4:])
	return uint64(hi)<<32 | uint64(lo)
}
func (p Page) Checksum() uint16        { return p.u16(offChecksum) }        // byte 8-9
func (p Page) Flags() uint16           { return p.u16(offFlags) }           // byte 10-11
func (p Page) Lower() uint16           { return p.u16(offLower) }           // byte 12-13
func (p Page) Upper() uint16           { return p.u16(offUpper) }           // byte 14-15
func (p Page) Special() uint16         { return p.u16(offSpecial) }         // byte 16-17
func (p Page) PageSizeVersion() uint16 { return p.u16(offPageSizeVersion) } // byte 18-19
func (p Page) PruneXid() uint32        { return binary.LittleEndian.Uint32(p[offPruneXid:]) }

// PageSize và LayoutVersion tách hai phần của pd_pagesize_version:
// kích thước = xoá 8 bit thấp (0x2004 -> 0x2000 = 8192),
// phiên bản  = lấy 8 bit thấp  (0x2004 -> 0x04 = 4).
func (p Page) PageSize() int      { return int(p.PageSizeVersion() &^ 0xFF) }
func (p Page) LayoutVersion() int { return int(p.PageSizeVersion() & 0xFF) }

// IsNew: page toàn số 0, chưa từng được khởi tạo. Postgres coi page như vậy là hợp lệ
// (ví dụ file vừa được nới ra nhưng chưa kịp ghi gì vào page đó).
// Page đã PageInit thì upper luôn > 0, nên upper == 0 nghĩa là chưa khởi tạo.
func (p Page) IsNew() bool { return p.Upper() == 0 }

// Verify kiểm tra header có hợp lý không, để không tin vào một page hỏng.
// Bản rút gọn của PageIsVerified() trong bufpage.c (chưa kiểm checksum — M3).
func (p Page) Verify() error {
	// Page phải dài đúng 8192 byte.
	if len(p) != BLCKSZ {
		return fmt.Errorf("%w: dài %d byte, phải là %d", ErrCorruptPage, len(p), BLCKSZ)
	}
	// Page toàn số 0 là hợp lệ (chưa dùng tới).
	if p.IsNew() {
		return nil
	}
	lower, upper, special := int(p.Lower()), int(p.Upper()), int(p.Special())
	// Kích thước và phiên bản phải đúng 8192 | 4.
	if p.PageSize() != BLCKSZ || p.LayoutVersion() != PG_PAGE_LAYOUT_VERSION {
		return fmt.Errorf("%w: pagesize_version = %#x", ErrCorruptPage, p.PageSizeVersion())
	}
	// Thứ tự bắt buộc: 24 <= lower <= upper <= special <= 8192.
	if lower < SizeOfPageHeaderData || lower > upper || upper > special || special > BLCKSZ {
		return fmt.Errorf("%w: lower=%d upper=%d special=%d", ErrCorruptPage, lower, upper, special)
	}
	return nil
}

// --- Line pointer (ItemIdData, itemid.h) ---

// ItemId là một line pointer (một ô) 4 byte. Trong C nó là bitfield:
//
//	unsigned lp_off:15, lp_flags:2, lp_len:15;
//
// Trên máy little-endian, các bit xếp từ thấp lên cao:
//
//	bit  0..14  lp_off    tuple bắt đầu ở byte nào trong page (tối đa 32767)
//	bit 15..16  lp_flags  trạng thái của ô (xem LP_*)
//	bit 17..31  lp_len    tuple dài bao nhiêu byte
//
// Ví dụ ô 2 thật trên page 0 bảng users: byte d0 9f 5c 00 -> 0x005c9fd0
// -> off = 8144, flags = 1 (NORMAL), len = 46.
type ItemId uint32

// Trạng thái của một ô (lp_flags).
const (
	LP_UNUSED   = 0 // ô trống, dùng lại được
	LP_NORMAL   = 1 // đang chứa một tuple
	LP_REDIRECT = 2 // HOT: chuyển hướng sang ô khác (M4)
	LP_DEAD     = 3 // tuple đã chết; vùng dữ liệu đã dọn, chờ VACUUM xoá mục index
)

// makeItemId gộp 3 số thành 4 byte: off ở bit 0-14, flags ở bit 15-16, len ở bit 17-31.
func makeItemId(off, flags, length int) ItemId {
	return ItemId(uint32(off) | uint32(flags)<<15 | uint32(length)<<17)
}

func (id ItemId) Off() int   { return int(id & 0x7FFF) }  // lấy 15 bit thấp
func (id ItemId) Flags() int { return int(id>>15) & 0x3 } // dịch 15 bit, lấy 2 bit
func (id ItemId) Len() int   { return int(id >> 17) }     // 15 bit cao

// MaxOffsetNumber là số ô trong page (kể cả ô chết, ô trống).
// Mảng ô nằm từ byte 24 tới pd_lower, mỗi ô 4 byte, nên số ô = (lower - 24) / 4.
// Ví dụ page 0 bảng users: (612 - 24) / 4 = 147 ô.
// Tương ứng: PageGetMaxOffsetNumber().
func (p Page) MaxOffsetNumber() common.OffsetNumber {
	if p.Lower() <= SizeOfPageHeaderData {
		return 0
	}
	return common.OffsetNumber((int(p.Lower()) - SizeOfPageHeaderData) / ItemIdSize)
}

// ItemId trả về ô số off (đếm từ 1, giống Postgres).
func (p Page) ItemId(off common.OffsetNumber) (ItemId, error) {
	if off < common.FirstOffsetNumber || off > p.MaxOffsetNumber() {
		return 0, fmt.Errorf("%w: %d (page có %d ô)", ErrBadOffset, off, p.MaxOffsetNumber())
	}
	// Ô 1 ở byte 24, ô 2 ở byte 28, ô n ở byte 24 + (n-1)*4.
	pos := SizeOfPageHeaderData + (int(off)-1)*ItemIdSize
	return ItemId(binary.LittleEndian.Uint32(p[pos:])), nil
}

// Item trả về các byte của tuple ở ô off. Slice trỏ thẳng vào page, không copy:
// sửa slice là sửa page.
// Ô không ở trạng thái LP_NORMAL (chết, trống, chuyển hướng) thì không có dữ
// liệu, trả về nil.
func (p Page) Item(off common.OffsetNumber) ([]byte, error) {
	// Bước 1: đọc ô để biết tuple ở đâu, dài bao nhiêu.
	id, err := p.ItemId(off)
	if err != nil {
		return nil, err
	}
	if id.Flags() != LP_NORMAL {
		return nil, nil
	}
	// Bước 2: kiểm tuple nằm gọn trong vùng dữ liệu [upper, special), phòng page hỏng.
	if id.Off()+id.Len() > int(p.Special()) || id.Off() < int(p.Upper()) {
		return nil, fmt.Errorf("%w: ô %d trỏ ra ngoài vùng dữ liệu (off=%d len=%d)", ErrCorruptPage, off, id.Off(), id.Len())
	}
	// Bước 3: cắt đúng đoạn byte của tuple.
	return p[id.Off() : id.Off()+id.Len()], nil
}

// FreeSpace là số byte còn chèn được một tuple mới, ĐÃ trừ 4 byte cho ô của nó.
// Tương ứng: PageGetFreeSpace().
// Ví dụ page 0 bảng users: 664 - 612 - 4 = 48 byte.
func (p Page) FreeSpace() int {
	space := int(p.Upper()) - int(p.Lower()) - ItemIdSize
	return max(space, 0)
}

// AddItem chép item vào page và trả về số ô của nó.
//
// Hai bước, giống PageAddItemExtended():
//
//  1. dời pd_upper XUỐNG (về phía đầu page) đúng bằng kích thước item, căn 8 byte,
//     rồi chép dữ liệu vào đó
//
//  2. thêm một ô vào cuối mảng, dời pd_lower LÊN 4 byte
//
//     trước:  | header | o1 o2 |        trống         | t2 | t1 |
//     ^lower                 ^upper
//     sau:    | header | o1 o2 o3 |    trống    | t3 | t2 | t1 |
//     ^lower        ^upper
//
// Bản này luôn thêm ô mới ở cuối. Postgres còn tìm ô LP_UNUSED để dùng lại
// (khi cờ PD_HAS_FREE_LINES bật) — việc đó cần VACUUM, để dành cho M4.
func (p Page) AddItem(item []byte) (common.OffsetNumber, error) {
	if len(item) == 0 {
		return 0, errors.New("storage: item rỗng")
	}
	// Dữ liệu chiếm chỗ đã làm tròn lên bội số của 8 (46 byte -> chiếm 48).
	size := alignUp(len(item))
	lower, upper := int(p.Lower()), int(p.Upper())

	// Đủ chỗ cho cả dữ liệu lẫn ô mới (4 byte) không?
	if lower+ItemIdSize > upper-size {
		return 0, fmt.Errorf("%w: cần %d byte + %d byte line pointer, còn %d",
			ErrPageFull, size, ItemIdSize, upper-lower)
	}

	// Bước 1: dữ liệu đặt ngay TRƯỚC dữ liệu cũ (mọc ngược từ cuối page).
	newUpper := upper - size
	copy(p[newUpper:], item)

	// Bước 2: ô mới ghi vào ngay chỗ pd_lower đang chỉ (cuối mảng ô).
	off := common.OffsetNumber(p.MaxOffsetNumber() + 1)
	binary.LittleEndian.PutUint32(p[lower:], uint32(makeItemId(newUpper, LP_NORMAL, len(item))))

	// Cập nhật ranh giới chỗ trống: lower tiến lên 4, upper lùi xuống size.
	p.setU16(offLower, uint16(lower+ItemIdSize))
	p.setU16(offUpper, uint16(newUpper))
	return off, nil
}

// AddItemAt chèn item vào page ở ĐÚNG ô off (đếm từ 1); các ô từ off trở đi dịch
// sang phải một ô. Dùng cho B-tree, nơi mảng ô phải luôn sắp theo key.
// Tương ứng PageAddItemExtended() khi truyền offsetNumber cụ thể.
//
//	trước:  | header | o1 o2 o3 |        trống       | dữ liệu |
//	chèn vào ô 2:
//	sau:    | header | o1 MỚI o2' o3' |   trống   | MỚI | dữ liệu |
//	                        ^ o2, o3 cũ dịch sang phải 4 byte (thành ô 3, ô 4)
//
// Chỉ dời các ô (4 byte mỗi ô), KHÔNG dời dữ liệu: dữ liệu mới vẫn đặt ngay
// trước dữ liệu cũ như AddItem.
func (p Page) AddItemAt(item []byte, off common.OffsetNumber) error {
	maxOff := p.MaxOffsetNumber()
	if off < common.FirstOffsetNumber || off > maxOff+1 {
		return fmt.Errorf("%w: chèn vào ô %d (page có %d ô)", ErrBadOffset, off, maxOff)
	}
	size := alignUp(len(item))
	lower, upper := int(p.Lower()), int(p.Upper())
	if len(item) == 0 || lower+ItemIdSize > upper-size {
		return fmt.Errorf("%w: cần %d byte + %d byte line pointer, còn %d",
			ErrPageFull, size, ItemIdSize, upper-lower)
	}

	// 1. Dữ liệu đặt ngay trước dữ liệu cũ.
	newUpper := upper - size
	copy(p[newUpper:], item)

	// 2. Dời các ô từ off tới cuối sang phải một ô (4 byte), chừa chỗ cho ô mới.
	pos := SizeOfPageHeaderData + (int(off)-1)*ItemIdSize
	copy(p[pos+ItemIdSize:lower+ItemIdSize], p[pos:lower])
	binary.LittleEndian.PutUint32(p[pos:], uint32(makeItemId(newUpper, LP_NORMAL, len(item))))

	// 3. Cập nhật ranh giới chỗ trống.
	p.setU16(offLower, uint16(lower+ItemIdSize))
	p.setU16(offUpper, uint16(newUpper))
	return nil
}

// --- tiện ích ---

// u16 đọc số 2 byte ở vị trí pos. Little-endian: byte thấp đứng trước,
// nên "64 02" đọc thành 0x0264 = 612.
func (p Page) u16(pos int) uint16 { return binary.LittleEndian.Uint16(p[pos:]) }

// setU16 ghi số 2 byte vào vị trí pos (little-endian).
func (p Page) setU16(pos int, v uint16) { binary.LittleEndian.PutUint16(p[pos:], v) }

// alignUp làm tròn n lên bội số của 8: 46 -> 48, 48 -> 48, 49 -> 56.
func alignUp(n int) int { return (n + maxAlign - 1) &^ (maxAlign - 1) }
