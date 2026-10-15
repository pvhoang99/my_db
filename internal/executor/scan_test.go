package executor

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/hoangpv/minidb/internal/access/btree"
	"github.com/hoangpv/minidb/internal/access/heap"
	"github.com/hoangpv/minidb/internal/buffer"
	"github.com/hoangpv/minidb/internal/types"
)

// Dữ liệu giống hệt bảng hoc_m13 trên learn-pg (100.000 dòng như users), với
// hai index: id (correlation 1,000) và name (correlation 0,814).
//
// Số liệu Postgres 18, ép từng kiểu quét (enable_* = off), "Buffers: hit + read":
//
//	id BETWEEN 1 AND 20000            Seq 736 · Index 203 · Bitmap 203
//	name BETWEEN 'user5' AND 'user6'  Seq 736 · Index 2231 · Bitmap 133
//
// minidb cho ra 736 · 204 · 204 và 736 · 2263 · 165. Hai chỗ lệch đều nằm ở
// PHẦN INDEX, phần heap thì khớp:
//   - +1 page: minidb đọc metapage mỗi lần quét; Postgres giữ sẵn nó trong
//     relcache của phiên.
//   - +32 page ở index name: Postgres dựng index bằng CREATE INDEX (sắp xếp
//     trước, lá đầy 90%); minidb chèn từng dòng theo thứ tự id, tức là lộn xộn
//     theo name, nên lá chỉ đầy ~70% (đúng như M2.3) -> khoảng tìm trải trên
//     nhiều page lá hơn. Index và Bitmap cùng lệch đúng 32 page.
var (
	once  sync.Once
	db    *testDB
	dbErr error
)

type testDB struct {
	pool          *buffer.Pool
	h             *heap.Heap
	idxID, idxNam *btree.Index
}

var cities = []string{"HN", "HCM", "DN", "HP", "CT"}

var usersDesc = heap.TupleDesc{
	{Name: "id", Type: types.Int4}, {Name: "name", Type: types.Text},
	{Name: "email", Type: types.Text}, {Name: "city", Type: types.Text},
}

func setup(t *testing.T) *testDB {
	t.Helper()
	once.Do(func() {
		dir, err := makeTempDir()
		if err != nil {
			dbErr = err
			return
		}
		pool := buffer.New(2048) // đủ chứa cả bảng lẫn index, như shared_buffers 128MB
		h, err := heap.Open(filepath.Join(dir, "users"), usersDesc, pool)
		if err != nil {
			dbErr = err
			return
		}
		idxID, err := btree.Create(filepath.Join(dir, "users_id"), types.Int4, pool)
		if err != nil {
			dbErr = err
			return
		}
		idxNam, err := btree.Create(filepath.Join(dir, "users_name"), types.Text, pool)
		if err != nil {
			dbErr = err
			return
		}
		for i := 1; i <= 100000; i++ {
			row := []types.Datum{
				types.NewInt4(int32(i)), types.NewText(fmt.Sprintf("user%d", i)),
				types.NewText(fmt.Sprintf("u%d@x.com", i)), types.NewText(cities[i%5]),
			}
			tid, err := h.Insert(row, 1000)
			if err == nil {
				err = idxID.Insert(row[0], tid)
			}
			if err == nil {
				err = idxNam.Insert(row[1], tid)
			}
			if err != nil {
				dbErr = err
				return
			}
		}
		db = &testDB{pool: pool, h: h, idxID: idxID, idxNam: idxNam}
	})
	if dbErr != nil {
		t.Fatal(dbErr)
	}
	return db
}

// run chạy hết một kiểu quét, trả về số dòng và "Buffers" (hit + read).
func run(t *testing.T, pool *buffer.Pool, mk func() (Scan, error), check func([]types.Datum) bool) (rows, buffers int) {
	t.Helper()
	before := pool.Stats()
	s, err := mk()
	if err != nil {
		t.Fatal(err)
	}
	for s.Next() {
		if !check(s.Values()) {
			t.Fatalf("dòng không thoả điều kiện: %v", s.Values())
		}
		rows++
	}
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
	s.Close()
	after := pool.Stats()
	return rows, (after.Hit + after.Read) - (before.Hit + before.Read)
}

func TestBaKieuQuetTheoID(t *testing.T) {
	d := setup(t)
	lo, hi := types.NewInt4(1), types.NewInt4(20000)
	inRange := func(v []types.Datum) bool { return v[0].Int >= 1 && v[0].Int <= 20000 }

	seqRows, seqBuf := run(t, d.pool, func() (Scan, error) { return NewSeqScan(d.h, inRange) }, inRange)
	idxRows, idxBuf := run(t, d.pool, func() (Scan, error) { return NewIndexScan(d.h, d.idxID, &lo, &hi) }, inRange)
	bmRows, bmBuf := run(t, d.pool, func() (Scan, error) { return NewBitmapHeapScan(d.h, d.idxID, &lo, &hi) }, inRange)

	t.Logf("id 1..20000:  Seq %d · Index %d · Bitmap %d   (Postgres: 736 · 203 · 203)", seqBuf, idxBuf, bmBuf)
	if seqRows != 20000 || idxRows != 20000 || bmRows != 20000 {
		t.Fatalf("số dòng: seq=%d index=%d bitmap=%d", seqRows, idxRows, bmRows)
	}
	if seqBuf != 736 {
		t.Fatalf("Seq Scan đọc %d page, Postgres 736", seqBuf)
	}
	// Index và Bitmap gần bằng nhau vì thứ tự index trùng thứ tự heap.
	near(t, "Index", idxBuf, 203, 5)
	near(t, "Bitmap", bmBuf, 203, 5)
}

func TestBaKieuQuetTheoName(t *testing.T) {
	d := setup(t)
	lo, hi := types.NewText("user5"), types.NewText("user6")
	inRange := func(v []types.Datum) bool { return v[1].Str >= "user5" && v[1].Str <= "user6" }

	_, seqBuf := run(t, d.pool, func() (Scan, error) { return NewSeqScan(d.h, inRange) }, inRange)
	idxRows, idxBuf := run(t, d.pool, func() (Scan, error) { return NewIndexScan(d.h, d.idxNam, &lo, &hi) }, inRange)
	bmRows, bmBuf := run(t, d.pool, func() (Scan, error) { return NewBitmapHeapScan(d.h, d.idxNam, &lo, &hi) }, inRange)

	t.Logf("name user5..user6:  Seq %d · Index %d · Bitmap %d   (Postgres: 736 · 2231 · 133)", seqBuf, idxBuf, bmBuf)
	if idxRows != 11112 || bmRows != 11112 {
		t.Fatalf("số dòng: index=%d bitmap=%d, Postgres 11112", idxRows, bmRows)
	}
	// Index Scan nhảy qua lại giữa các page heap: đắt hơn Bitmap hơn 10 lần.
	if idxBuf < 10*bmBuf {
		t.Fatalf("Index %d không đắt hơn hẳn Bitmap %d", idxBuf, bmBuf)
	}
	// Phần heap khớp Postgres; phần index nhiều hơn ~33 page (xem chú thích đầu file).
	near(t, "Index", idxBuf, 2231+33, 5)
	near(t, "Bitmap", bmBuf, 133+33, 5)
}

// Index Scan giữ thứ tự của index; Bitmap thì trả theo thứ tự page heap.
func TestThuTuKetQua(t *testing.T) {
	d := setup(t)
	lo, hi := types.NewText("user5000"), types.NewText("user5001")

	order := func(s Scan, err error) []int64 {
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		var ids []int64
		for s.Next() {
			ids = append(ids, s.Values()[0].Int)
		}
		return ids
	}
	idx := order(NewIndexScan(d.h, d.idxNam, &lo, &hi))
	bm := order(NewBitmapHeapScan(d.h, d.idxNam, &lo, &hi))
	t.Logf("Index Scan (thứ tự chữ):  %v", idx)
	t.Logf("Bitmap     (thứ tự page): %v", bm)
	// Theo chữ: user5000, user50000, user50001, ..., user50009, user5001
	if idx[0] != 5000 || idx[1] != 50000 || idx[len(idx)-1] != 5001 {
		t.Fatalf("Index Scan không theo thứ tự chữ: %v", idx)
	}
	if bm[0] != 5000 || bm[1] != 5001 || bm[2] != 50000 {
		t.Fatalf("Bitmap không theo thứ tự page: %v", bm)
	}
}

func near(t *testing.T, name string, got, want, tol int) {
	t.Helper()
	if got < want-tol || got > want+tol {
		t.Fatalf("%s đọc %d page, Postgres %d (cho lệch ±%d)", name, got, want, tol)
	}
}

func makeTempDir() (string, error) { return os.MkdirTemp("", "minidb-executor-") }
