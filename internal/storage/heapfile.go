package storage

import (
	"errors"
	"fmt"
	"io"
	"os"
)

// PageID là số thứ tự page trong file (block number của Postgres).
type PageID uint32

// InvalidPageID tương ứng InvalidBlockNumber.
const InvalidPageID PageID = 0xFFFFFFFF

var ErrPageNotExist = errors.New("storage: page không tồn tại trong file")

// HeapFile là một file trên disk chứa dãy page 8KB liên tiếp —
// tương đương một relation file của Postgres (base/<db>/<relfilenode>).
//
// Tầng này cố tình "ngu": không cache, không concurrency control.
// Buffer pool ở M2 sẽ lo hai việc đó.
type HeapFile struct {
	f        *os.File
	numPages PageID
}

// OpenHeapFile mở (hoặc tạo) file chứa page.
func OpenHeapFile(path string) (*HeapFile, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if info.Size()%PageSize != 0 {
		f.Close()
		return nil, fmt.Errorf("storage: file %s dài %d byte, không chia hết cho page size", path, info.Size())
	}
	return &HeapFile{f: f, numPages: PageID(info.Size() / PageSize)}, nil
}

// NumPages là số page hiện có trong file.
func (h *HeapFile) NumPages() PageID { return h.numPages }

// ReadPage đọc page từ disk vào bộ nhớ.
func (h *HeapFile) ReadPage(id PageID, p *Page) error {
	if id >= h.numPages {
		return fmt.Errorf("%w: page %d (file có %d page)", ErrPageNotExist, id, h.numPages)
	}
	if _, err := h.f.ReadAt(p[:], int64(id)*PageSize); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

// WritePage ghi page xuống disk. Chưa fsync — gọi Sync nếu cần bền vững.
func (h *HeapFile) WritePage(id PageID, p *Page) error {
	if id >= h.numPages {
		return fmt.Errorf("%w: page %d (file có %d page)", ErrPageNotExist, id, h.numPages)
	}
	_, err := h.f.WriteAt(p[:], int64(id)*PageSize)
	return err
}

// AllocatePage nối thêm một page rỗng vào cuối file (smgrextend).
func (h *HeapFile) AllocatePage() (PageID, *Page, error) {
	p := NewPage()
	id := h.numPages
	if _, err := h.f.WriteAt(p[:], int64(id)*PageSize); err != nil {
		return InvalidPageID, nil, err
	}
	h.numPages++
	return id, p, nil
}

// Sync ép dữ liệu xuống disk thật (fsync).
func (h *HeapFile) Sync() error { return h.f.Sync() }

func (h *HeapFile) Close() error { return h.f.Close() }
