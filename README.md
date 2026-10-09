# minidb

Mini database engine viết bằng Go, build từ con số 0 để hiểu PostgreSQL từ a→z.

Lộ trình chi tiết: [ROADMAP.md](ROADMAP.md)

## Chạy test

```bash
go test ./...
```

## Trạng thái

- **M1 — storage layer** ✅ slotted page 8KB (`internal/storage/page.go`) + heap file (`internal/storage/heapfile.go`)
- M2 → M7: xem ROADMAP

## Đọc code theo thứ tự nào

1. `internal/storage/page.go` — một page 8KB trông như thế nào, tuple nằm ở đâu, tại sao xoá tuple lại không giải phóng chỗ ngay.
2. `internal/storage/heapfile.go` — page được map xuống file trên disk ra sao.
3. `internal/storage/*_test.go` — test chính là đặc tả hành vi; đọc test trước khi đọc impl cũng được.

## Đối chiếu với Postgres thật

| Khái niệm ở đây | Trong Postgres |
|---|---|
| `PageSize = 8192` | `BLCKSZ` |
| `Page` | `Page` / `PageHeaderData` (`bufpage.h`) |
| `ItemId (offset, length)` | `ItemIdData` |
| `(PageID, slot)` | `ItemPointerData` — tức TID, cái bạn thấy ở `SELECT ctid FROM t` |
| `HeapFile` | relation file `base/<db>/<relfilenode>` |
| `LSN` trong page header | `pd_lsn` |
