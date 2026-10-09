package storage

import (
	"errors"
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
//
// TODO:
//   - os.OpenFile với O_RDWR|O_CREATE
//   - Stat() để biết file dài bao nhiêu -> suy ra numPages
//   - size không chia hết cho PageSize nghĩa là file hỏng -> trả lỗi (nhớ Close)
func OpenHeapFile(path string) (*HeapFile, error) {
	panic("TODO: OpenHeapFile")
}

// NumPages là số page hiện có trong file.
func (h *HeapFile) NumPages() PageID { return h.numPages }

// ReadPage đọc page từ disk vào bộ nhớ.
//
// TODO: id >= numPages -> bọc ErrPageNotExist. Dùng ReadAt tại offset
// id*PageSize để không phụ thuộc con trỏ file (sau này nhiều goroutine cùng đọc).
func (h *HeapFile) ReadPage(id PageID, p *Page) error {
	panic("TODO: ReadPage")
}

// WritePage ghi page xuống disk. Chưa fsync — gọi Sync nếu cần bền vững.
//
// TODO: tương tự ReadPage nhưng dùng WriteAt.
func (h *HeapFile) WritePage(id PageID, p *Page) error {
	panic("TODO: WritePage")
}

// AllocatePage nối thêm một page rỗng vào cuối file (smgrextend).
//
// TODO: ghi một page đã Init() vào offset numPages*PageSize, tăng numPages,
// trả về id và page vừa tạo.
func (h *HeapFile) AllocatePage() (PageID, *Page, error) {
	panic("TODO: AllocatePage")
}

// Sync ép dữ liệu xuống disk thật (fsync).
func (h *HeapFile) Sync() error { return h.f.Sync() }

func (h *HeapFile) Close() error { return h.f.Close() }
