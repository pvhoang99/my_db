package heap

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/hoangpv/minidb/internal/buffer"
	"github.com/hoangpv/minidb/internal/common"
	"github.com/hoangpv/minidb/internal/types"
)

var cities = []string{"HN", "HCM", "DN", "HP", "CT"}

// Đúng dữ liệu của bảng users trên learn-pg:
//
//	insert into hoc_m13 select i, 'user'||i, 'u'||i||'@x.com',
//	  (array['HN','HCM','DN','HP','CT'])[1 + i%5] from generate_series(1,100000) i;
func usersRow(i int) []types.Datum {
	return []types.Datum{
		types.NewInt4(int32(i)),
		types.NewText(fmt.Sprintf("user%d", i)),
		types.NewText(fmt.Sprintf("u%d@x.com", i)),
		types.NewText(cities[i%5]),
	}
}

func buildUsers(t *testing.T, path string, n int) (*Heap, map[int]common.ItemPointer) {
	t.Helper()
	h, err := Open(path, usersDesc, buffer.New(1024))
	if err != nil {
		t.Fatal(err)
	}
	tids := make(map[int]common.ItemPointer, n)
	for i := 1; i <= n; i++ {
		tid, err := h.Insert(usersRow(i), 1000)
		if err != nil {
			t.Fatalf("chèn dòng %d: %v", i, err)
		}
		tids[i] = tid
	}
	return h, tids
}

// Chèn 100.000 dòng giống hệt Postgres: phải ra CÙNG số page và CÙNG ctid.
// Số liệu chuẩn lấy từ learn-pg (PG 18.6):
//
//	select pg_relation_size('hoc_m13')/8192;                       -- 736
//	select id, ctid from hoc_m13 where id in (1,2,146,147,...);
//	select (ctid::text::point)[0]::int, count(*) from hoc_m13 group by 1;
func TestChenGiongPostgres(t *testing.T) {
	h, tids := buildUsers(t, filepath.Join(t.TempDir(), "users"), 100000)
	defer h.Close()

	n, _ := h.NumBlocks()
	if n != 736 {
		t.Fatalf("có %d page, Postgres có 736", n)
	}

	want := map[int]string{
		1: "(0,1)", 2: "(0,2)", 146: "(0,146)", // page 0 chứa 146 dòng
		147: "(1,1)", 148: "(1,2)", // dòng 147 sang page 1
		1000: "(7,38)", 10000: "(73,62)", 50000: "(367,78)",
		99999: "(735,29)", 100000: "(735,30)", // page cuối 30 dòng
	}
	for id, w := range want {
		if got := tids[id].String(); got != w {
			t.Errorf("id=%d: ctid %s, Postgres %s", id, got, w)
		}
	}
}

// Fetch theo ctid ra đúng dòng — đây là việc Index Scan làm sau khi có ctid từ index.
func TestFetch(t *testing.T) {
	h, tids := buildUsers(t, filepath.Join(t.TempDir(), "users"), 1000)
	defer h.Close()

	hdr, vals, err := h.Fetch(tids[777])
	if err != nil {
		t.Fatal(err)
	}
	if got := show(vals); got != "777 | user777 | u777@x.com | DN" {
		t.Fatalf("đọc ra %q", got)
	}
	if hdr.Ctid != tids[777] || hdr.Xmin != 1000 {
		t.Fatalf("header: ctid=%v xmin=%d", hdr.Ctid, hdr.Xmin)
	}

	_, _, err = h.Fetch(common.ItemPointer{Block: 0, Offset: 999})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("ctid không tồn tại: err = %v", err)
	}
}

// Seq scan đọc đủ mọi dòng, đúng thứ tự vật lý, và đọc đúng 736 page.
// So với EXPLAIN (ANALYZE, BUFFERS) SELECT * FROM users: Seq Scan, Buffers: 736.
func TestScanVaMoLai(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users")
	h, _ := buildUsers(t, path, 100000)
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}

	// Mở lại từ file: mọi thứ phải còn nguyên.
	h, err := Open(path, usersDesc, buffer.New(1024))
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	s, err := h.Scan()
	if err != nil {
		t.Fatal(err)
	}
	count, hn := 0, 0
	for s.Next() {
		count++
		v := s.Values()
		if int(v[0].Int) != count {
			t.Fatalf("dòng thứ %d có id=%d: seq scan phải đi đúng thứ tự vật lý", count, v[0].Int)
		}
		if v[3].Str == "HN" {
			hn++
		}
	}
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
	if count != 100000 || hn != 20000 || s.PagesRead != 736 {
		t.Fatalf("quét được %d dòng, %d dòng HN, đọc %d page", count, hn, s.PagesRead)
	}

	// Chèn tiếp sau khi mở lại: phải nối vào page cuối (735), không tạo page mới.
	tid, err := h.Insert(usersRow(100001), 1001)
	if err != nil {
		t.Fatal(err)
	}
	if tid.String() != "(735,31)" {
		t.Fatalf("dòng mới ở %v, muốn (735,31)", tid)
	}
}

// Giống hệt thí nghiệm trên Postgres (vừa khởi động lại, shared_buffers trống):
//
//	EXPLAIN (ANALYZE, BUFFERS) SELECT count(*) FROM hoc_m13;
//	lần 1: Seq Scan ... Buffers: shared read=736
//	lần 2: Seq Scan ... Buffers: shared hit=736
func TestScanBuffersGiongPostgres(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users")
	h, _ := buildUsers(t, path, 100000)
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}

	// Mở lại với một pool MỚI (trống) đủ lớn: 1024 ô > 736 page.
	pool := buffer.New(1024)
	h, err := Open(path, usersDesc, pool)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	scan := func() buffer.Stats {
		before := pool.Stats()
		s, _ := h.Scan()
		for s.Next() {
		}
		if err := s.Err(); err != nil {
			t.Fatal(err)
		}
		after := pool.Stats()
		return buffer.Stats{Hit: after.Hit - before.Hit, Read: after.Read - before.Read}
	}
	if got := scan(); got.Read != 736 || got.Hit != 0 {
		t.Fatalf("lần 1: hit=%d read=%d, Postgres: read=736", got.Hit, got.Read)
	}
	if got := scan(); got.Hit != 736 || got.Read != 0 {
		t.Fatalf("lần 2: hit=%d read=%d, Postgres: hit=736", got.Hit, got.Read)
	}
}

// Pool NHỎ hơn bảng: seq scan lần 2 vẫn phải đọc lại toàn bộ từ file.
// Đọc tuần tự 736 page qua 100 ô thì page đầu đã bị đuổi trước khi quét lại.
// Đây là lý do Postgres dùng "ring buffer" cho seq scan bảng lớn: chỉ cho nó
// dùng một vòng nhỏ 256KB, để không đuổi sạch những page nóng của người khác.
func TestPoolNhoHonBang(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users")
	h, _ := buildUsers(t, path, 100000)
	h.Close()

	pool := buffer.New(100)
	h, err := Open(path, usersDesc, pool)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	for lan := 1; lan <= 2; lan++ {
		before := pool.Stats().Read
		s, _ := h.Scan()
		for s.Next() {
		}
		if read := pool.Stats().Read - before; read != 736 {
			t.Fatalf("lần %d: read=%d, muốn 736", lan, read)
		}
	}
}
