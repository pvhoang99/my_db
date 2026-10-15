package buffer

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/hoangpv/minidb/internal/common"
	"github.com/hoangpv/minidb/internal/storage"
)

// newFile tạo một file có n page; page i chứa đúng một item là byte i.
func newFile(t *testing.T, n int) *storage.File {
	t.Helper()
	f, err := storage.OpenFile(filepath.Join(t.TempDir(), "rel"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	for i := 0; i < n; i++ {
		p := storage.NewPage()
		storage.PageInit(p, 0)
		p.AddItem([]byte{byte(i)})
		if _, err := f.ExtendPage(p); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

// firstByte đọc item của page qua pool rồi bỏ ghim.
func firstByte(t *testing.T, bp *Pool, f *storage.File, blk int) byte {
	t.Helper()
	b, err := bp.ReadBuffer(f, common.BlockNumber(blk))
	if err != nil {
		t.Fatal(err)
	}
	defer bp.Release(b)
	item, _ := bp.Page(b).Item(1)
	return item[0]
}

// Lần đầu đọc page thì READ, lần sau thì HIT — đúng hai con số của EXPLAIN.
func TestHitVaRead(t *testing.T) {
	f := newFile(t, 3)
	bp := New(8)

	for i := 0; i < 3; i++ {
		if got := firstByte(t, bp, f, i); got != byte(i) {
			t.Fatalf("page %d chứa %d", i, got)
		}
	}
	if s := bp.Stats(); s.Read != 3 || s.Hit != 0 {
		t.Fatalf("lần 1: %+v, muốn read=3 hit=0", s)
	}
	firstByte(t, bp, f, 1)
	firstByte(t, bp, f, 1)
	if s := bp.Stats(); s.Read != 3 || s.Hit != 2 {
		t.Fatalf("lần 2: %+v, muốn read=3 hit=2", s)
	}
}

// Page dùng thường xuyên sống sót qua clock-sweep nhờ usage count.
// Pool 4 ô; page 0 được đọc lại sau mỗi page khác -> không bao giờ bị đuổi.
func TestPageNongSongSot(t *testing.T) {
	f := newFile(t, 20)
	bp := New(4)

	firstByte(t, bp, f, 0)
	for blk := 1; blk < 20; blk++ {
		firstByte(t, bp, f, blk) // page lạnh, chỉ dùng một lần
		firstByte(t, bp, f, 0)   // page nóng
	}
	// Page 0 chỉ phải đọc từ file đúng một lần; 19 lần sau đều HIT.
	// Read = 1 (page 0) + 19 (các page lạnh).
	if s := bp.Stats(); s.Read != 20 || s.Hit != 19 {
		t.Fatalf("%+v, muốn read=20 hit=19", s)
	}
}

// Page bẩn bị đuổi thì phải được ghi xuống file trước, không mất dữ liệu.
func TestDuoiPageBanThiGhiXuong(t *testing.T) {
	f := newFile(t, 6)
	bp := New(2)

	// Sửa page 0 trong pool: thêm item 99, đánh dấu bẩn.
	b, _ := bp.ReadBuffer(f, 0)
	bp.Page(b).AddItem([]byte{99})
	bp.MarkDirty(b)
	bp.Release(b)

	// Đọc 5 page khác qua pool 2 ô -> page 0 chắc chắn bị đuổi.
	for blk := 1; blk < 6; blk++ {
		firstByte(t, bp, f, blk)
	}
	if bp.Stats().Written != 1 {
		t.Fatalf("written = %d, muốn 1 (page 0 bẩn bị đuổi)", bp.Stats().Written)
	}

	// Đọc thẳng file, bỏ qua pool: page 0 phải có item 99.
	p := storage.NewPage()
	f.ReadPage(0, p)
	item, _ := p.Item(2)
	if len(item) != 1 || item[0] != 99 {
		t.Fatalf("page 0 trên đĩa không có thay đổi: %v", item)
	}
}

// Page đang bị ghim thì không được đuổi; ghim hết thì báo lỗi.
func TestGhimHetThiBaoLoi(t *testing.T) {
	f := newFile(t, 3)
	bp := New(2)

	b0, _ := bp.ReadBuffer(f, 0)
	b1, _ := bp.ReadBuffer(f, 1)
	if _, err := bp.ReadBuffer(f, 2); !errors.Is(err, ErrNoFreeBuffer) {
		t.Fatalf("pool 2 ô đều ghim mà đọc page 2 không lỗi: %v", err)
	}
	// Bỏ ghim một ô thì đọc được.
	bp.Release(b0)
	b2, err := bp.ReadBuffer(f, 2)
	if err != nil {
		t.Fatal(err)
	}
	bp.Release(b1)
	bp.Release(b2)
}
