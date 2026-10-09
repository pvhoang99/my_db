package storage

import (
	"bytes"
	"errors"
	"path/filepath"
	"testing"
)

func openTemp(t *testing.T) (*HeapFile, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "table.db")
	h, err := OpenHeapFile(path)
	if err != nil {
		t.Fatalf("OpenHeapFile: %v", err)
	}
	t.Cleanup(func() { h.Close() })
	return h, path
}

func TestHeapFileAllocateAndRead(t *testing.T) {
	h, _ := openTemp(t)
	if h.NumPages() != 0 {
		t.Fatalf("file mới có %d page, muốn 0", h.NumPages())
	}

	id, p, err := h.AllocatePage()
	if err != nil {
		t.Fatalf("AllocatePage: %v", err)
	}
	if id != 0 || h.NumPages() != 1 {
		t.Fatalf("id = %d, numPages = %d", id, h.NumPages())
	}

	if _, err := p.Insert([]byte("tuple-0")); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := h.WritePage(id, p); err != nil {
		t.Fatalf("WritePage: %v", err)
	}

	var got Page
	if err := h.ReadPage(id, &got); err != nil {
		t.Fatalf("ReadPage: %v", err)
	}
	data, err := got.Get(0)
	if err != nil || !bytes.Equal(data, []byte("tuple-0")) {
		t.Fatalf("đọc lại được %q, %v", data, err)
	}
}

func TestHeapFilePersistsAcrossReopen(t *testing.T) {
	h, path := openTemp(t)
	for i := 0; i < 3; i++ {
		id, p, err := h.AllocatePage()
		if err != nil {
			t.Fatalf("AllocatePage: %v", err)
		}
		if _, err := p.Insert([]byte{byte('A' + i)}); err != nil {
			t.Fatalf("Insert: %v", err)
		}
		if err := h.WritePage(id, p); err != nil {
			t.Fatalf("WritePage: %v", err)
		}
	}
	if err := h.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	h.Close()

	reopened, err := OpenHeapFile(path)
	if err != nil {
		t.Fatalf("mở lại: %v", err)
	}
	defer reopened.Close()

	if reopened.NumPages() != 3 {
		t.Fatalf("sau khi mở lại có %d page, muốn 3", reopened.NumPages())
	}
	for i := PageID(0); i < 3; i++ {
		var p Page
		if err := reopened.ReadPage(i, &p); err != nil {
			t.Fatalf("ReadPage(%d): %v", i, err)
		}
		data, err := p.Get(0)
		if err != nil {
			t.Fatalf("Get page %d: %v", i, err)
		}
		if want := byte('A' + i); data[0] != want {
			t.Fatalf("page %d chứa %q, muốn %q", i, data[0], want)
		}
	}
}

func TestHeapFileOutOfRange(t *testing.T) {
	h, _ := openTemp(t)
	var p Page
	if err := h.ReadPage(0, &p); !errors.Is(err, ErrPageNotExist) {
		t.Fatalf("ReadPage ngoài range trả %v, muốn ErrPageNotExist", err)
	}
	if err := h.WritePage(5, &p); !errors.Is(err, ErrPageNotExist) {
		t.Fatalf("WritePage ngoài range trả %v, muốn ErrPageNotExist", err)
	}
}
