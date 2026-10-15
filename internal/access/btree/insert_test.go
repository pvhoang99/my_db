package btree

import (
	"path/filepath"
	"testing"

	"github.com/hoangpv/minidb/internal/buffer"
	"github.com/hoangpv/minidb/internal/common"
	"github.com/hoangpv/minidb/internal/types"
)

// Số liệu chuẩn từ PostgreSQL 18: tạo index trống, rồi chèn 10.000 key từng dòng một.
//
//	create table hoc_tang (id int4); create index hoc_tang_id on hoc_tang (id);
//	insert into hoc_tang select i from generate_series(1, 10000) i;
//	-> level 1, 30 page, 28 page lá, mỗi lá đầy 366 mục + high key
//
//	create table hoc_tron (id int4); create index hoc_tron_id on hoc_tron (id);
//	insert into hoc_tron select (i*7919)%10000+1 from generate_series(1,10000) i order by i;
//	-> level 1, 39 page, 37 page lá, trung bình 271 mục mỗi lá
func buildIndex(t *testing.T, keys []int) *Index {
	t.Helper()
	ix, err := Create(filepath.Join(t.TempDir(), "idx"), types.Int4, buffer.New(256))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ix.Close() })
	for i, k := range keys {
		// heap TID giả: dòng thứ i nằm ở page i/100, ô i%100+1
		tid := common.ItemPointer{Block: common.BlockNumber(i / 100), Offset: common.OffsetNumber(i%100 + 1)}
		if err := ix.Insert(types.NewInt4(int32(k)), tid); err != nil {
			t.Fatalf("chèn key %d: %v", k, err)
		}
	}
	return ix
}

func checkAllFound(t *testing.T, ix *Index, keys []int) {
	t.Helper()
	for i, k := range keys {
		r, err := ix.Search(types.NewInt4(int32(k)))
		if err != nil {
			t.Fatal(err)
		}
		want := common.ItemPointer{Block: common.BlockNumber(i / 100), Offset: common.OffsetNumber(i%100 + 1)}
		if len(r.TIDs) != 1 || r.TIDs[0] != want {
			t.Fatalf("key %d: tìm ra %v, muốn %v", k, r.TIDs, want)
		}
	}
}

// Chèn tăng dần: mỗi lần split ở page ngoài cùng bên phải, để trái đầy 90%.
// Phải ra ĐÚNG hình dạng của Postgres: 28 lá, mỗi lá 366 mục + high key.
func TestChenTangDanGiongPostgres(t *testing.T) {
	keys := make([]int, 10000)
	for i := range keys {
		keys[i] = i + 1
	}
	ix := buildIndex(t, keys)
	sh, err := ix.Check()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("minidb: level=%d page=%d lá=%d, mục mỗi lá: %v", sh.Level, sh.Pages, sh.Leaves, sh.LeafItems)
	if sh.Level != 1 || sh.Pages != 30 || sh.Leaves != 28 {
		t.Fatalf("level=%d page=%d lá=%d; Postgres: level=1 page=30 lá=28", sh.Level, sh.Pages, sh.Leaves)
	}
	for i, n := range sh.LeafItems[:len(sh.LeafItems)-1] {
		if n != 366 {
			t.Fatalf("lá thứ %d có %d mục; Postgres: 366", i, n)
		}
	}
	checkAllFound(t, ix, keys)
}

// Chèn xáo trộn: page giữa tách 50/50, các lá chỉ đầy khoảng 70%.
// Phải ra đúng như Postgres: 39 page, 37 lá. (Postgres báo trung bình 271 mục
// mỗi lá vì bt_multi_page_stats đếm cả high key; minidb không đếm: 270.)
func TestChenXaoTronGiongPostgres(t *testing.T) {
	keys := make([]int, 10000)
	for i := range keys {
		keys[i] = ((i+1)*7919)%10000 + 1
	}
	ix := buildIndex(t, keys)
	sh, err := ix.Check()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("minidb: level=%d page=%d lá=%d, trung bình %d mục/lá", sh.Level, sh.Pages, sh.Leaves, sh.TotalItems/sh.Leaves)
	if sh.Level != 1 || sh.TotalItems != 10000 {
		t.Fatalf("level=%d tổng mục=%d", sh.Level, sh.TotalItems)
	}
	if sh.Pages != 39 || sh.Leaves != 37 {
		t.Fatalf("page=%d lá=%d; Postgres: page=39 lá=37", sh.Pages, sh.Leaves)
	}
	checkAllFound(t, ix, keys)
}

// Nhiều mục cùng key: phải sắp theo heap TID, trải qua nhiều page lá, và Search
// lấy được đủ, đúng thứ tự.
func TestKeyTrung(t *testing.T) {
	ix, err := Create(filepath.Join(t.TempDir(), "idx"), types.Int4, buffer.New(64))
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	// 1500 mục key=5 xen giữa các key 1..3000 khác, chèn TID theo thứ tự ngược.
	for i := 1; i <= 3000; i++ {
		ix.Insert(types.NewInt4(int32(i*2)), common.ItemPointer{Block: 9, Offset: common.OffsetNumber(i)})
	}
	for i := 1500; i >= 1; i-- {
		tid := common.ItemPointer{Block: common.BlockNumber(i / 200), Offset: common.OffsetNumber(i%200 + 1)}
		if err := ix.Insert(types.NewInt4(5), tid); err != nil {
			t.Fatal(err)
		}
	}
	sh, err := ix.Check()
	if err != nil {
		t.Fatal(err)
	}
	r, err := ix.Search(types.NewInt4(5))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.TIDs) != 1500 {
		t.Fatalf("tìm ra %d mục key=5, muốn 1500 (cây có %d lá)", len(r.TIDs), sh.Leaves)
	}
	for i := 1; i < len(r.TIDs); i++ {
		if compareTID(r.TIDs[i-1], r.TIDs[i]) >= 0 {
			t.Fatalf("mục key=5 không sắp theo heap TID ở vị trí %d", i)
		}
	}
	t.Logf("1500 mục key=5 trải trên các page lá; đường đi tìm: %v", r.Path)
}

// Đủ nhiều key để cây lên 3 tầng: page gốc cũng phải split.
func TestCayLen3Tang(t *testing.T) {
	keys := make([]int, 150000)
	for i := range keys {
		keys[i] = ((i+1)*7919)%150000 + 1
	}
	ix := buildIndex(t, keys)
	sh, err := ix.Check()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("minidb: level=%d page=%d lá=%d", sh.Level, sh.Pages, sh.Leaves)
	if sh.Level != 2 {
		t.Fatalf("level=%d, muốn 2 (3 tầng)", sh.Level)
	}
	checkAllFound(t, ix, keys[:2000])
}
