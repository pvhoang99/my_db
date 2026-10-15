package heap

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hoangpv/minidb/internal/common"
	"github.com/hoangpv/minidb/internal/storage"
	"github.com/hoangpv/minidb/internal/types"
)

// Các page thật lấy từ PostgreSQL 18 nằm chung ở testdata của storage.
func readPGPage(t *testing.T, name string) storage.Page {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "storage", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return storage.Page(raw)
}

var usersDesc = TupleDesc{
	{"id", types.Int4}, {"name", types.Text}, {"email", types.Text}, {"city", types.Text},
}

// CREATE TABLE hoc_m12 (a int4, b int8, c text, d bool, e text);
var hocM12Desc = TupleDesc{
	{"a", types.Int4}, {"b", types.Int8}, {"c", types.Text}, {"d", types.Bool}, {"e", types.Text},
}

// setHintXminCommitted bật cờ HEAP_XMIN_COMMITTED trong t_infomask.
func setHintXminCommitted(tup []byte) {
	m := binary.LittleEndian.Uint16(tup[offInfomask:])
	binary.LittleEndian.PutUint16(tup[offInfomask:], m|HEAP_XMIN_COMMITTED)
}

func show(vals []types.Datum) string {
	s := make([]string, len(vals))
	for i, v := range vals {
		s[i] = v.String()
	}
	return strings.Join(s, " | ")
}

// Giải mã dòng id=2 trên page 0 thật của bảng users.
// pageinspect: t_xmin=762, t_infomask=2306 (0x0902), t_hoff=24, ctid=(0,2).
func TestDeformDongPostgresThat(t *testing.T) {
	p := readPGPage(t, "pg18_users_page0.bin")
	tup, err := p.Item(2)
	if err != nil {
		t.Fatal(err)
	}
	h, vals, err := DeformTuple(usersDesc, tup)
	if err != nil {
		t.Fatal(err)
	}
	if h.Xmin != 762 || h.Xmax != 0 || h.Infomask != 0x0902 || h.Hoff != 24 || h.NAtts() != 4 {
		t.Fatalf("header = %+v", h)
	}
	if h.Ctid != (common.ItemPointer{Block: 0, Offset: 2}) {
		t.Fatalf("ctid = %v", h.Ctid)
	}
	if got := show(vals); got != "2 | user2 | u2@x.com | DN" {
		t.Fatalf("đọc ra %q", got)
	}
}

// Tạo lại đúng dòng đó bằng minidb: phải ra ĐÚNG 46 byte như Postgres.
func TestFormGiongPostgresTungByte(t *testing.T) {
	p := readPGPage(t, "pg18_users_page0.bin")
	want, _ := p.Item(2)

	got, err := FormTuple(usersDesc, []types.Datum{
		types.NewInt4(2), types.NewText("user2"), types.NewText("u2@x.com"), types.NewText("DN"),
	}, 762)
	if err != nil {
		t.Fatal(err)
	}
	SetCtid(got, common.ItemPointer{Block: 0, Offset: 2})
	// HEAP_XMIN_COMMITTED là "hint bit": Postgres bật nó về sau, khi có người đọc
	// dòng và kiểm thấy transaction 762 đã commit (M4). Lúc INSERT thì chưa có.
	setHintXminCommitted(got)

	if !bytes.Equal(got, want) {
		t.Fatalf("khác Postgres:\nminidb   % x\npostgres % x", got, want)
	}
}

// Bảng có đủ trường hợp khó: int8 căn 8, bool, NULL, chuỗi dài 200 byte.
func TestDeformCanLeVaNull(t *testing.T) {
	p := readPGPage(t, "pg18_hoc_m12_page0.bin")

	cases := []struct {
		off      common.OffsetNumber
		want     string
		infomask uint16
	}{
		// dòng 1 có NULL -> HEAP_HASNULL, null bitmap 1 byte (pageinspect: t_bits=11110000)
		{1, "1 | 10000000000 | xin chao | t | NULL", 0x0903},
		{2, "2 | -5 | " + strings.Repeat("x", 200) + " | f | b", 0x0902},
	}
	for _, c := range cases {
		tup, _ := p.Item(c.off)
		h, vals, err := DeformTuple(hocM12Desc, tup)
		if err != nil {
			t.Fatalf("dòng %d: %v", c.off, err)
		}
		if got := show(vals); got != c.want {
			t.Errorf("dòng %d: đọc ra %q", c.off, got)
		}
		if h.Infomask != c.infomask {
			t.Errorf("dòng %d: infomask %#04x, muốn %#04x", c.off, h.Infomask, c.infomask)
		}
	}
}

// Tạo lại cả hai dòng của hoc_m12, so từng byte với Postgres.
func TestFormCanLeVaNullGiongPostgres(t *testing.T) {
	p := readPGPage(t, "pg18_hoc_m12_page0.bin")
	rows := []struct {
		off  common.OffsetNumber
		xmin uint32
		vals []types.Datum
	}{
		{1, 847, []types.Datum{types.NewInt4(1), types.NewInt8(10000000000), types.NewText("xin chao"), types.NewBool(true), types.NewNull(types.Text)}},
		{2, 848, []types.Datum{types.NewInt4(2), types.NewInt8(-5), types.NewText(strings.Repeat("x", 200)), types.NewBool(false), types.NewText("b")}},
	}
	for _, r := range rows {
		want, _ := p.Item(r.off)
		got, err := FormTuple(hocM12Desc, r.vals, r.xmin)
		if err != nil {
			t.Fatal(err)
		}
		SetCtid(got, common.ItemPointer{Block: 0, Offset: r.off})
		setHintXminCommitted(got) // hint bit, xem test ở trên
		if !bytes.Equal(got, want) {
			t.Errorf("dòng %d khác Postgres:\nminidb   % x\npostgres % x", r.off, got, want)
		}
	}
}

// Ghi rồi đọc lại phải ra đúng giá trị, kể cả trường hợp biên.
func TestFormDeformKhuHoi(t *testing.T) {
	desc := TupleDesc{{"a", types.Bool}, {"b", types.Text}, {"c", types.Int8}, {"d", types.Text}, {"e", types.Int4},
		{"f", types.Text}, {"g", types.Text}, {"h", types.Text}, {"i", types.Int4}}
	vals := []types.Datum{
		types.NewBool(true), types.NewText(""), types.NewInt8(-1), types.NewText(strings.Repeat("y", 126)),
		types.NewNull(types.Int4), types.NewText(strings.Repeat("z", 127)), types.NewText("ok"),
		types.NewNull(types.Text), types.NewInt4(-2147483648),
	}
	tup, err := FormTuple(desc, vals, 1)
	if err != nil {
		t.Fatal(err)
	}
	_, got, err := DeformTuple(desc, tup)
	if err != nil {
		t.Fatal(err)
	}
	if show(got) != show(vals) {
		t.Fatalf("khứ hồi sai:\n ghi %s\n đọc %s", show(vals), show(got))
	}
}
