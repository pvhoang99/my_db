// Package common chứa các kiểu dùng chung cho mọi tầng của minidb.
//
// Tên kiểu đặt giống Postgres để đọc source thật thấy quen:
//
//	BlockNumber  <-> src/include/storage/block.h
//	OffsetNumber <-> src/include/storage/off.h
//	ItemPointer  <-> src/include/storage/itemptr.h  (chính là "ctid" / TID)
package common

import "fmt"

// BlockNumber là số thứ tự của một page trong file: page 0, page 1, ...
// Page thứ n nằm ở byte n*BLCKSZ của file.
type BlockNumber uint32

// InvalidBlockNumber đánh dấu "không trỏ tới page nào".
const InvalidBlockNumber BlockNumber = 0xFFFFFFFF

// OffsetNumber là số thứ tự của một ô (line pointer) trong page.
// Đếm từ 1, không phải 0 — giống Postgres. Số 0 nghĩa là "không hợp lệ".
type OffsetNumber uint16

// InvalidOffsetNumber là ô số 0, không bao giờ dùng.
const InvalidOffsetNumber OffsetNumber = 0

// FirstOffsetNumber là ô đầu tiên trong page.
const FirstOffsetNumber OffsetNumber = 1

// ItemPointer là địa chỉ vật lý của một dòng: (page, ô).
// Trong Postgres nó chính là cột ẩn "ctid", và là thứ page lá của index trỏ tới.
type ItemPointer struct {
	Block  BlockNumber
	Offset OffsetNumber
}

// String in theo đúng kiểu Postgres in ctid: (0,5).
func (p ItemPointer) String() string {
	return fmt.Sprintf("(%d,%d)", p.Block, p.Offset)
}
