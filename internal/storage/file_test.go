package storage

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/hoangpv/minidb/internal/common"
)

// Ghi vài page, đóng file, mở lại: dữ liệu vẫn còn và nằm đúng page.
func TestFileGhiDocMoLai(t *testing.T) {
	path := filepath.Join(t.TempDir(), "heap")

	fl, err := OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		p := NewPage()
		PageInit(p, 0)
		if _, err := p.AddItem([]byte{byte('a' + i)}); err != nil {
			t.Fatal(err)
		}
		blk, err := fl.ExtendPage(p)
		if err != nil {
			t.Fatal(err)
		}
		if blk != common.BlockNumber(i) {
			t.Fatalf("page mới là %d, muốn %d", blk, i)
		}
	}
	if err := fl.Sync(); err != nil {
		t.Fatal(err)
	}
	fl.Close()

	fl, err = OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer fl.Close()
	n, _ := fl.NumBlocks()
	if n != 3 {
		t.Fatalf("file có %d page, muốn 3", n)
	}
	p := NewPage()
	if err := fl.ReadPage(1, p); err != nil {
		t.Fatal(err)
	}
	got, _ := p.Item(1)
	if !bytes.Equal(got, []byte("b")) {
		t.Fatalf("page 1 ô 1 = %q, muốn \"b\"", got)
	}
}

func TestWritePageChuaTonTai(t *testing.T) {
	fl, err := OpenFile(filepath.Join(t.TempDir(), "heap"))
	if err != nil {
		t.Fatal(err)
	}
	defer fl.Close()
	if err := fl.WritePage(0, NewPage()); err == nil {
		t.Fatal("ghi vào page chưa tồn tại mà không báo lỗi")
	}
}
