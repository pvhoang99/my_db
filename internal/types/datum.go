// Package types là hệ kiểu dữ liệu SQL của minidb: mỗi kiểu dài bao nhiêu byte,
// căn lề thế nào, và một giá trị (Datum) trông ra sao.
//
// Tương ứng trong Postgres:
//
//	pg_type (catalog)   typlen, typalign, typbyval của từng kiểu
//	src/include/postgres.h, varatt.h   Datum, varlena (kiểu dài thay đổi)
//	src/backend/utils/adt/             cài đặt từng kiểu
package types

import "fmt"

// Type là một kiểu SQL. Giá trị bằng OID của kiểu đó trong pg_type của Postgres,
// để in ra hay đối chiếu thấy quen (SELECT oid, typname FROM pg_type).
type Type uint32

const (
	Bool Type = 16
	Int8 Type = 20
	Int4 Type = 23
	Text Type = 25
)

func (t Type) String() string {
	switch t {
	case Bool:
		return "bool"
	case Int8:
		return "int8"
	case Int4:
		return "int4"
	case Text:
		return "text"
	}
	return fmt.Sprintf("type(%d)", uint32(t))
}

// Len là số byte của kiểu dài cố định, hoặc -1 nếu dài thay đổi (varlena).
// Tương ứng: pg_type.typlen.
func (t Type) Len() int {
	switch t {
	case Bool:
		return 1
	case Int4:
		return 4
	case Int8:
		return 8
	case Text:
		return -1
	}
	panic("types: kiểu không hỗ trợ: " + t.String())
}

// Align là số byte mà giá trị phải căn theo: vị trí bắt đầu chia hết cho số này.
// Tương ứng: pg_type.typalign ('c'=1, 's'=2, 'i'=4, 'd'=8).
//
// text căn 4 vì độ dài 4 byte của nó phải đọc được như một số int32. Nhưng
// chuỗi ngắn dùng độ dài 1 byte thì KHÔNG cần căn (xem heap/tuple.go).
func (t Type) Align() int {
	switch t {
	case Bool:
		return 1
	case Int4, Text:
		return 4
	case Int8:
		return 8
	}
	panic("types: kiểu không hỗ trợ: " + t.String())
}

// Datum là một giá trị của một cột. Chỉ một trong các trường có nghĩa, tuỳ Type.
// (Postgres dùng một số 8 byte cho mọi thứ; ở đây tách ra cho dễ đọc.)
type Datum struct {
	Type   Type   // kiểu của giá trị: quyết định trường nào bên dưới có nghĩa
	IsNull bool   // true = NULL; khi đó các trường bên dưới bỏ qua
	Int    int64  // giá trị khi Type là int4 hoặc int8
	Str    string // giá trị khi Type là text
	Bool   bool   // giá trị khi Type là bool
}

func NewInt4(v int32) Datum  { return Datum{Type: Int4, Int: int64(v)} }
func NewInt8(v int64) Datum  { return Datum{Type: Int8, Int: v} }
func NewText(v string) Datum { return Datum{Type: Text, Str: v} }
func NewBool(v bool) Datum   { return Datum{Type: Bool, Bool: v} }
func NewNull(t Type) Datum   { return Datum{Type: t, IsNull: true} }

// String in giá trị giống psql.
func (d Datum) String() string {
	if d.IsNull {
		return "NULL"
	}
	switch d.Type {
	case Int4, Int8:
		return fmt.Sprint(d.Int)
	case Text:
		return d.Str
	case Bool:
		if d.Bool {
			return "t"
		}
		return "f"
	}
	return "?"
}
