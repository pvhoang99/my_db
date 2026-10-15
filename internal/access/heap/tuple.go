// Package heap là bảng của minidb: các dòng (tuple) nằm trong page heap.
//
// File này lo MỘT dòng: biến các giá trị cột thành byte (FormTuple) và ngược lại
// (DeformTuple), với bố cục byte GIỐNG HỆT Postgres.
//
// Tương ứng trong Postgres:
//
//	src/include/access/htup_details.h   HeapTupleHeaderData, các cờ t_infomask
//	src/backend/access/common/heaptuple.c   heap_form_tuple, heap_deform_tuple
//
// Một dòng:
//
//	0                        23        t_hoff
//	+------------------------+---------+---+---------------------------------+
//	| header 23 byte         | null    |pad| dữ liệu cột, mỗi cột căn lề     |
//	| xmin xmax cid ctid ... | bitmap  |   | theo kiểu của nó                |
//	+------------------------+---------+---+---------------------------------+
//
// Null bitmap chỉ có khi dòng có ít nhất một cột NULL. t_hoff (đầu dữ liệu)
// luôn là bội số của 8.
package heap

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/hoangpv/minidb/internal/common"
	"github.com/hoangpv/minidb/internal/types"
)

// SizeofHeapTupleHeader: header 23 byte, chưa tính null bitmap.
const SizeofHeapTupleHeader = 23

// Vị trí các trường trong header (byte). Tổng 23 byte:
//
//	t_xmin       4   transaction tạo ra dòng này             (M4)
//	t_xmax       4   transaction xoá/sửa dòng này, 0 = chưa  (M4)
//	t_cid        4   số thứ tự câu lệnh trong transaction    (M4)
//	t_ctid       6   địa chỉ của chính nó, hoặc của bản mới hơn sau UPDATE
//	t_infomask2  2   số cột (11 bit thấp) + vài cờ
//	t_infomask   2   các cờ: có NULL không, có cột dài thay đổi không, ...
//	t_hoff       1   dữ liệu cột bắt đầu ở byte nào
const (
	offXmin       = 0
	offXmax       = 4
	offCid        = 8
	offCtid       = 12
	offInfomask2  = 18
	offInfomask   = 20
	offHoff       = 22
	offNullBitmap = 23
)

// Các cờ trong t_infomask (htup_details.h). Chỉ liệt kê những cờ dùng tới.
const (
	HEAP_HASNULL        = 0x0001 // có ít nhất một cột NULL -> có null bitmap
	HEAP_HASVARWIDTH    = 0x0002 // có cột dài thay đổi (text, ...)
	HEAP_XMIN_COMMITTED = 0x0100 // gợi ý: transaction tạo dòng đã commit (M4)
	HEAP_XMIN_INVALID   = 0x0200 // gợi ý: transaction tạo dòng đã abort (M4)
	HEAP_XMAX_INVALID   = 0x0800 // xmax không có nghĩa: dòng chưa bị xoá
)

// HEAP_NATTS_MASK: 11 bit thấp của t_infomask2 là số cột.
const HEAP_NATTS_MASK = 0x07FF

// Column mô tả một cột. Tương ứng một dòng trong pg_attribute.
type Column struct {
	Name string     // tên cột, ví dụ "email"; chỉ để in lỗi, không lưu trong tuple
	Type types.Type // kiểu cột: quyết định cột chiếm bao nhiêu byte, căn lề ra sao
}

// TupleDesc mô tả các cột của một bảng, theo đúng thứ tự.
// Tuple không lưu tên hay kiểu cột: chỉ có byte dữ liệu xếp liền nhau. Muốn
// đọc ra phải biết trước TupleDesc. Tương ứng: TupleDesc trong tupdesc.h.
type TupleDesc []Column

// Header là phần đầu 23 byte của một dòng, đã giải mã ra các con số.
// Ví dụ dòng id=2 thật: Xmin=762, Xmax=0, Ctid=(0,2), Infomask=0x0902, Hoff=24.
type Header struct {
	Xmin      uint32             // transaction đã tạo ra tuple này
	Xmax      uint32             // transaction đã xoá/sửa tuple này; 0 = chưa ai
	Cid       uint32             // câu lệnh thứ mấy trong transaction đã tạo/xoá nó
	Ctid      common.ItemPointer // địa chỉ của chính nó; sau UPDATE trỏ sang bản mới
	Infomask2 uint16             // 11 bit thấp = số cột, còn lại là cờ (HOT, ...)
	Infomask  uint16             // các cờ HEAP_*: có NULL, có cột dài thay đổi, hint bit
	Hoff      uint8              // dữ liệu cột bắt đầu ở byte thứ mấy (bội số của 8)
}

// NAtts là số cột ghi trong dòng (11 bit thấp của t_infomask2).
func (h Header) NAtts() int { return int(h.Infomask2 & HEAP_NATTS_MASK) }

var ErrBadTuple = errors.New("heap: tuple hỏng")

// ReadHeader đọc 23 byte đầu của một dòng.
func ReadHeader(tup []byte) (Header, error) {
	if len(tup) < SizeofHeapTupleHeader {
		return Header{}, fmt.Errorf("%w: chỉ dài %d byte", ErrBadTuple, len(tup))
	}
	// Mọi số trong header đều little-endian: byte thấp đứng trước.
	le := binary.LittleEndian
	return Header{
		Xmin: le.Uint32(tup[offXmin:]),
		Xmax: le.Uint32(tup[offXmax:]),
		Cid:  le.Uint32(tup[offCid:]),
		// ctid lưu số page thành 2 nửa 16 bit (nửa cao trước), rồi số ô.
		Ctid: common.ItemPointer{
			Block:  common.BlockNumber(uint32(le.Uint16(tup[offCtid:]))<<16 | uint32(le.Uint16(tup[offCtid+2:]))),
			Offset: common.OffsetNumber(le.Uint16(tup[offCtid+4:])),
		},
		Infomask2: le.Uint16(tup[offInfomask2:]),
		Infomask:  le.Uint16(tup[offInfomask:]),
		Hoff:      tup[offHoff],
	}, nil
}

// FormTuple biến một dòng giá trị thành byte. Tương ứng: heap_form_tuple().
//
// xmin là transaction tạo dòng. ctid để trống; heap.Insert sẽ điền khi biết
// dòng nằm ở page nào, ô nào. Cờ HEAP_XMAX_INVALID được bật sẵn như heap_insert().
func FormTuple(desc TupleDesc, values []types.Datum, xmin uint32) ([]byte, error) {
	if len(values) != len(desc) {
		return nil, fmt.Errorf("heap: %d giá trị cho %d cột", len(values), len(desc))
	}

	// 1. Duyệt các giá trị để quyết định header trông thế nào:
	//    - có cột NULL không? Có thì cần null bitmap, mỗi cột 1 bit.
	//    - có cột dài thay đổi (text) không? Có thì bật HEAP_HASVARWIDTH.
	//    Tuple mới chưa bị ai xoá, nên xmax không có nghĩa: bật HEAP_XMAX_INVALID.
	var infomask uint16 = HEAP_XMAX_INVALID
	hasNull := false
	for i, v := range values {
		if v.Type != desc[i].Type {
			return nil, fmt.Errorf("heap: cột %q kiểu %s, nhận giá trị kiểu %s", desc[i].Name, desc[i].Type, v.Type)
		}
		if v.IsNull {
			hasNull = true
		}
		if desc[i].Type.Len() == -1 {
			infomask |= HEAP_HASVARWIDTH
		}
	}
	// Dữ liệu bắt đầu sau header 23 byte + null bitmap (nếu có), làm tròn lên 8.
	// 5 cột có NULL: 23 + 1 byte bitmap = 24 -> 24. Không NULL: 23 -> 24.
	hoff := SizeofHeapTupleHeader
	if hasNull {
		infomask |= HEAP_HASNULL
		hoff += (len(desc) + 7) / 8 // số byte bitmap = số cột chia 8, làm tròn lên
	}
	hoff = alignUp(hoff, 8) // t_hoff luôn là bội số của 8

	// 2. Chép dữ liệu từng cột, căn lề theo kiểu.
	// buf bắt đầu bằng hoff byte số 0 (chỗ cho header + bitmap + đệm), dữ liệu
	// cột được nối vào sau. Vị trí căn lề tính từ đầu tuple; tuple luôn bắt đầu
	// ở vị trí chia hết cho 8 trong page nên kết quả giống hệt Postgres.
	buf := make([]byte, hoff, hoff+64)
	for _, v := range values {
		if v.IsNull {
			continue // cột NULL không chiếm byte nào, chỉ có bit trong bitmap
		}
		buf = appendDatum(buf, v)
	}

	// 3. Điền header vào hoff byte đầu (ctid để 0, heap.Insert sẽ điền).
	le := binary.LittleEndian
	le.PutUint32(buf[offXmin:], xmin)
	le.PutUint16(buf[offInfomask2:], uint16(len(desc))&HEAP_NATTS_MASK)
	le.PutUint16(buf[offInfomask:], infomask)
	buf[offHoff] = byte(hoff)
	if hasNull {
		// Bit = 1 nghĩa là KHÔNG null. Cột i nằm ở byte i/8, bit i%8.
		for i, v := range values {
			if !v.IsNull {
				buf[offNullBitmap+i/8] |= 1 << (i % 8)
			}
		}
	}
	return buf, nil
}

// SetCtid ghi địa chỉ vào t_ctid của một dòng.
// 6 byte: số page tách làm 2 nửa 16 bit (nửa cao trước), rồi số ô.
// (0,2) -> 00 00 | 00 00 | 02 00.
func SetCtid(tup []byte, tid common.ItemPointer) {
	le := binary.LittleEndian
	le.PutUint16(tup[offCtid:], uint16(tid.Block>>16))
	le.PutUint16(tup[offCtid+2:], uint16(tid.Block))
	le.PutUint16(tup[offCtid+4:], uint16(tid.Offset))
}

// appendDatum nối một giá trị vào cuối buf, có căn lề.
//
//	bool  1 byte, không căn lề
//	int4  4 byte, chèn byte 0 cho tới vị trí chia hết cho 4
//	int8  8 byte, chèn byte 0 cho tới vị trí chia hết cho 8
//	text  xem appendVarlena
func appendDatum(buf []byte, v types.Datum) []byte {
	switch v.Type {
	case types.Bool:
		b := byte(0)
		if v.Bool {
			b = 1
		}
		return append(buf, b)
	case types.Int4:
		buf = pad(buf, 4)
		return binary.LittleEndian.AppendUint32(buf, uint32(int32(v.Int)))
	case types.Int8:
		buf = pad(buf, 8)
		return binary.LittleEndian.AppendUint64(buf, uint64(v.Int))
	case types.Text:
		return appendVarlena(buf, []byte(v.Str))
	}
	panic("heap: kiểu không hỗ trợ: " + v.Type.String())
}

// appendVarlena ghi một giá trị dài thay đổi (varlena, varatt.h).
//
// Có hai kiểu header độ dài:
//
//	ngắn (1 byte): tổng <= 127 byte. Byte đầu = (tổng << 1) | 1. KHÔNG căn lề.
//	dài  (4 byte): còn lại.          4 byte đầu = tổng << 2. Căn lề 4.
//
// "tổng" tính cả header. Bit thấp nhất của byte đầu cho biết là kiểu nào:
// 1 = ngắn, 0 = dài. Byte đệm căn lề luôn là 0, nên khi đọc gặp byte khác 0
// là biết ngay đây là header ngắn, không cần nhảy qua đệm.
//
// Ví dụ "user2" (5 byte): tổng = 1 + 5 = 6, byte đầu = 6<<1|1 = 13 = 0x0d.
func appendVarlena(buf []byte, data []byte) []byte {
	if 1+len(data) <= 127 {
		// Header ngắn: 1 byte độ dài, rồi dữ liệu. Không cần đệm.
		buf = append(buf, byte((1+len(data))<<1|1))
		return append(buf, data...)
	}
	// Header dài: đệm tới bội số của 4, rồi 4 byte độ dài, rồi dữ liệu.
	buf = pad(buf, 4)
	buf = binary.LittleEndian.AppendUint32(buf, uint32(4+len(data))<<2)
	return append(buf, data...)
}

// DeformTuple đọc byte của một dòng ra các giá trị cột. Tương ứng: heap_deform_tuple().
//
// Đi ngược lại FormTuple: đọc header, rồi đọc lần lượt từng cột từ t_hoff,
// mỗi cột nhảy qua byte đệm căn lề trước khi đọc.
func DeformTuple(desc TupleDesc, tup []byte) (Header, []types.Datum, error) {
	// 1. Đọc header 23 byte.
	h, err := ReadHeader(tup)
	if err != nil {
		return h, nil, err
	}
	if int(h.Hoff) > len(tup) || h.Hoff%8 != 0 {
		return h, nil, fmt.Errorf("%w: t_hoff = %d", ErrBadTuple, h.Hoff)
	}

	// 2. Hàm hỏi "cột i có NULL không": chỉ có bitmap khi HEAP_HASNULL bật.
	// Bit của cột i nằm ở byte 23 + i/8, bit i%8. Bit 0 = NULL, bit 1 = có giá trị.
	hasNull := h.Infomask&HEAP_HASNULL != 0
	isNull := func(i int) bool {
		return hasNull && tup[offNullBitmap+i/8]&(1<<(i%8)) == 0
	}

	// 3. Đọc từng cột. pos là vị trí đang đọc, bắt đầu ở t_hoff.
	values := make([]types.Datum, len(desc))
	pos := int(h.Hoff)
	for i, col := range desc {
		// Dòng cũ có thể có ít cột hơn bảng hiện tại (ALTER TABLE ADD COLUMN
		// không viết lại dòng cũ): cột thiếu coi là NULL.
		if i >= h.NAtts() || isNull(i) {
			values[i] = types.NewNull(col.Type)
			continue
		}
		// Cột NULL không chiếm byte nào nên pos không tiến; cột có giá trị thì
		// readDatum trả về vị trí ngay sau nó.
		v, next, err := readDatum(tup, pos, col.Type)
		if err != nil {
			return h, nil, fmt.Errorf("cột %q: %w", col.Name, err)
		}
		values[i] = v
		pos = next
	}
	return h, values, nil
}

// EncodeDatum nối giá trị d vào cuối buf theo đúng luật xếp cột của heap tuple
// (căn lề, varlena). Dùng chung cho index: key trong index tuple xếp y hệt.
func EncodeDatum(buf []byte, d types.Datum) []byte { return appendDatum(buf, d) }

// DecodeDatum đọc một giá trị kiểu t bắt đầu ở vị trí pos của buf, có nhảy qua
// byte đệm căn lề. Trả về giá trị và vị trí ngay sau nó.
//
// Dùng chung cho index (access/btree): key trong index tuple được xếp theo đúng
// luật như cột trong heap tuple. Postgres cũng vậy: index_form_tuple() gọi
// heap_fill_tuple().
func DecodeDatum(buf []byte, pos int, t types.Type) (types.Datum, int, error) {
	return readDatum(buf, pos, t)
}

// readDatum đọc một giá trị bắt đầu ở pos, trả về giá trị và vị trí sau nó.
func readDatum(tup []byte, pos int, t types.Type) (types.Datum, int, error) {
	le := binary.LittleEndian
	// need kiểm còn đủ n byte để đọc, phòng tuple hỏng làm đọc lố ra ngoài.
	need := func(n int) error {
		if pos+n > len(tup) {
			return fmt.Errorf("%w: cần %d byte ở vị trí %d, dòng dài %d", ErrBadTuple, n, pos, len(tup))
		}
		return nil
	}
	switch t {
	case types.Bool:
		if err := need(1); err != nil {
			return types.Datum{}, 0, err
		}
		return types.NewBool(tup[pos] != 0), pos + 1, nil
	case types.Int4:
		pos = alignUp(pos, 4)
		if err := need(4); err != nil {
			return types.Datum{}, 0, err
		}
		return types.NewInt4(int32(le.Uint32(tup[pos:]))), pos + 4, nil
	case types.Int8:
		pos = alignUp(pos, 8)
		if err := need(8); err != nil {
			return types.Datum{}, 0, err
		}
		return types.NewInt8(int64(le.Uint64(tup[pos:]))), pos + 8, nil
	case types.Text:
		if err := need(1); err != nil {
			return types.Datum{}, 0, err
		}
		if tup[pos]&1 == 0 {
			// Byte 0 là đệm căn lề, hoặc đầu của header 4 byte: nhảy tới bội số của 4.
			// (Byte đầu của header ngắn luôn là số lẻ, nên không bao giờ rơi vào đây.)
			pos = alignUp(pos, 4)
		}
		if err := need(1); err != nil {
			return types.Datum{}, 0, err
		}
		var hdr, total int
		if tup[pos]&1 == 1 { // header ngắn 1 byte
			hdr, total = 1, int(tup[pos]>>1)
		} else { // header dài 4 byte
			if err := need(4); err != nil {
				return types.Datum{}, 0, err
			}
			hdr, total = 4, int(le.Uint32(tup[pos:])>>2)
		}
		if total < hdr {
			return types.Datum{}, 0, fmt.Errorf("%w: varlena dài %d", ErrBadTuple, total)
		}
		if err := need(total); err != nil {
			return types.Datum{}, 0, err
		}
		return types.NewText(string(tup[pos+hdr : pos+total])), pos + total, nil
	}
	return types.Datum{}, 0, fmt.Errorf("heap: kiểu không hỗ trợ: %s", t)
}

// pad chèn byte 0 vào cuối buf cho tới khi độ dài chia hết cho align.
func pad(buf []byte, align int) []byte {
	for len(buf)%align != 0 {
		buf = append(buf, 0)
	}
	return buf
}

// alignUp làm tròn n lên bội số của align: alignUp(28, 8) = 32.
func alignUp(n, align int) int { return (n + align - 1) &^ (align - 1) }
