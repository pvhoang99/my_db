package btree

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hoangpv/minidb/internal/buffer"
	"github.com/hoangpv/minidb/internal/common"
	"github.com/hoangpv/minidb/internal/types"
)

// Hai file index THẬT chép ra từ PostgreSQL 18 (learn-pg):
//
//	create table hoc_m22 (id int4, ma text);
//	insert into hoc_m22 select i, 'k'||lpad(i::text,4,'0')||repeat('x',490)
//	  from generate_series(1,1000) i;
//	create index hoc_m22_id on hoc_m22 (id);                 -- 2 tầng, 5 page
//
//	create table hoc_m22b (ma text);
//	insert into hoc_m22b select 'k'||lpad(i::text,4,'0')||repeat('x',490)
//	  from generate_series(1,400) i;
//	create index hoc_m22b_ma on hoc_m22b (ma collate "C");   -- 3 tầng, 34 page
//
// Cách lấy file: select pg_relation_filepath('hoc_m22_id'); rồi docker cp.
//
// Cả hai bảng đều 15 dòng mỗi page heap, nên dòng thứ i nằm ở
// ctid ((i-1)/15, (i-1)%15 + 1). Postgres xác nhận: id 500 -> (33,5), 777 -> (51,12).
func ctidOf(i int) common.ItemPointer {
	return common.ItemPointer{Block: common.BlockNumber((i - 1) / 15), Offset: common.OffsetNumber((i-1)%15 + 1)}
}

func maOf(i int) string { return fmt.Sprintf("k%04d", i) + strings.Repeat("x", 490) }

func openPG(t *testing.T, name string, kt types.Type) *Index {
	t.Helper()
	ix, err := Open(filepath.Join("testdata", name), kt, buffer.New(64))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ix.Close() })
	return ix
}

// Metapage khớp bt_metap(): hoc_m22_id root=3 level=1; hoc_m22b_ma root=17 level=2.
func TestMetaGiongPostgres(t *testing.T) {
	for _, c := range []struct {
		file  string
		kt    types.Type
		root  common.BlockNumber
		level uint32
	}{
		{"pg18_hoc_m22_id.bin", types.Int4, 3, 1},
		{"pg18_hoc_m22b_ma.bin", types.Text, 17, 2},
	} {
		m, err := openPG(t, c.file, c.kt).Meta()
		if err != nil {
			t.Fatal(err)
		}
		if m.Root != c.root || m.Level != c.level {
			t.Errorf("%s: root=%d level=%d, Postgres: root=%d level=%d", c.file, m.Root, m.Level, c.root, c.level)
		}
	}
}

// Page gốc của hoc_m22_id, đúng như bt_page_items('hoc_m22_id', 3):
//
//	1 | (1,0) | (rỗng)   -> −∞, đi xuống page 1
//	2 | (2,1) | 367      -> đi xuống page 2
//	3 | (4,1) | 733      -> đi xuống page 4
func TestPageGocCoMucAmVoCuc(t *testing.T) {
	ix := openPG(t, "pg18_hoc_m22_id.bin", types.Int4)
	buf, err := ix.pool.ReadBuffer(ix.file, 3)
	if err != nil {
		t.Fatal(err)
	}
	defer ix.pool.Release(buf)
	p := ix.pool.Page(buf)
	op := readOpaque(p)
	if op.Flags&BTP_ROOT == 0 || op.IsLeaf() || op.Level != 1 || !op.IsRightmost() {
		t.Fatalf("special: %+v", op)
	}
	want := []struct {
		down common.BlockNumber
		key  string
	}{{1, ""}, {2, "367"}, {4, "733"}}
	for i, w := range want {
		it, err := readIndexTuple(p, common.OffsetNumber(i+1))
		if err != nil {
			t.Fatal(err)
		}
		key := ""
		if len(it.Key) > 0 {
			v, _ := ix.decodeForTest(it.Key)
			key = v
		}
		if it.Downlink() != w.down || key != w.key {
			t.Errorf("mục %d: downlink=%d key=%q, Postgres: %d %q", i+1, it.Downlink(), key, w.down, w.key)
		}
	}
}

// Tìm MỌI key trong index int4 thật: mỗi key ra đúng một ctid, đúng như Postgres.
// Đường đi luôn là metapage -> page gốc 3 -> một page lá.
func TestTimMoiKeyInt(t *testing.T) {
	ix := openPG(t, "pg18_hoc_m22_id.bin", types.Int4)
	for i := 1; i <= 1000; i++ {
		r, err := ix.Search(types.NewInt4(int32(i)))
		if err != nil {
			t.Fatalf("id=%d: %v", i, err)
		}
		if len(r.TIDs) != 1 || r.TIDs[0] != ctidOf(i) {
			t.Fatalf("id=%d: ctid %v, muốn %v", i, r.TIDs, ctidOf(i))
		}
		if len(r.Path) != 3 || r.Path[1] != 3 {
			t.Fatalf("id=%d: đường đi %v, muốn [0 3 lá]", i, r.Path)
		}
	}
	// id 777: lá chứa 733..1000 là page 4 (theo page gốc ở trên).
	r, _ := ix.Search(types.NewInt4(777))
	if r.Path[2] != 4 {
		t.Fatalf("id=777 đi xuống page %d, muốn 4", r.Path[2])
	}
}

// Index text 3 tầng: đường đi metapage -> gốc 17 -> page giữa -> page lá.
func TestTimMoiKeyText3Tang(t *testing.T) {
	ix := openPG(t, "pg18_hoc_m22b_ma.bin", types.Text)
	for i := 1; i <= 400; i++ {
		r, err := ix.Search(types.NewText(maOf(i)))
		if err != nil {
			t.Fatalf("k%04d: %v", i, err)
		}
		if len(r.TIDs) != 1 || r.TIDs[0] != ctidOf(i) {
			t.Fatalf("k%04d: ctid %v, muốn %v", i, r.TIDs, ctidOf(i))
		}
		if len(r.Path) != 4 || r.Path[1] != 17 {
			t.Fatalf("k%04d: đường đi %v, muốn [0 17 giữa lá]", i, r.Path)
		}
	}
}

// Key không có trong index thì không ra ctid nào.
func TestKhongTimThay(t *testing.T) {
	ix := openPG(t, "pg18_hoc_m22_id.bin", types.Int4)
	for _, k := range []int32{0, -5, 1001, 99999} {
		r, err := ix.Search(types.NewInt4(k))
		if err != nil {
			t.Fatal(err)
		}
		if len(r.TIDs) != 0 {
			t.Fatalf("id=%d: tìm ra %v", k, r.TIDs)
		}
	}
	tx := openPG(t, "pg18_hoc_m22b_ma.bin", types.Text)
	r, _ := tx.Search(types.NewText("k0200"))
	if len(r.TIDs) != 0 {
		t.Fatalf("k0200 (ngắn) không có trong index mà tìm ra %v", r.TIDs)
	}
}

func (ix *Index) decodeForTest(raw []byte) (string, error) {
	v, _, err := decodeKey(raw, ix.keyType)
	return v.String(), err
}
