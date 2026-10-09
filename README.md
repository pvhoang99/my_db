# minidb

Mini database engine viết bằng Go, build từ con số 0 để hiểu PostgreSQL từ a→z.

Lộ trình chi tiết: [ROADMAP.md](ROADMAP.md)

## Cách học với repo này

Mỗi milestone có sẵn **skeleton + test**, phần thân hàm để `panic("TODO: ...")`.
Nhiệm vụ của bạn là điền code cho đến khi test xanh. Test chính là đặc tả —
đọc test trước khi viết impl.

```bash
go test ./...                              # chạy tất cả
go test ./internal/storage -run TestPage -v # chạy riêng một nhóm
```

## Trạng thái

- **M1 — storage layer** 🚧 skeleton sẵn sàng, chờ bạn điền
  - [`internal/storage/page.go`](internal/storage/page.go) — slotted page 8KB
  - [`internal/storage/heapfile.go`](internal/storage/heapfile.go) — dãy page trên disk
- M2 → M7: xem ROADMAP

## Thứ tự làm M1

1. `page.go`: `Init` → `NumSlots` / `FreeSpace` → `itemID` / `setItemID` → `Insert` → `Get` → `Delete` → `LSN` / `SetLSN`
2. `heapfile.go`: `OpenHeapFile` → `ReadPage` / `WritePage` → `AllocatePage`

Câu hỏi tự kiểm tra sau khi xong:
- Vì sao `Delete` không dồn tuple lại để lấy lại chỗ trống?
- Vì sao slot đã xoá vẫn phải chiếm chỗ trong mảng ItemId?
- Một page 8KB chứa được tối đa bao nhiêu tuple 100 byte? Phần hao đi đâu?

## Đối chiếu với Postgres thật

| Khái niệm ở đây | Trong Postgres |
|---|---|
| `PageSize = 8192` | `BLCKSZ` |
| `Page` | `Page` / `PageHeaderData` (`bufpage.h`) |
| `ItemId (offset, length)` | `ItemIdData` |
| `(PageID, slot)` | `ItemPointerData` — tức TID, cái bạn thấy ở `SELECT ctid FROM t` |
| `HeapFile` | relation file `base/<db>/<relfilenode>` |
| `LSN` trong page header | `pd_lsn` |
