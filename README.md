# minidb

Mini database engine viết bằng Go, build từ con số 0 để làm chủ PostgreSQL từ a→z.

- **[docs/TONG-HOP.md](docs/TONG-HOP.md)** — tổng hợp toàn bộ kiến thức đã học, xếp theo ý tưởng
- [ARCHITECTURE.md](ARCHITECTURE.md) — kiến trúc thư mục, luật phụ thuộc, map sang source Postgres
- [ROADMAP.md](ROADMAP.md) — lộ trình 7 milestone

## Trạng thái

| Milestone | Nội dung | |
|---|---|---|
| M1 | Page, tuple, heap, `pagedump` | ✅ |
| M2 | Buffer pool, B-tree, 3 kiểu quét | ✅ |
| M3 | WAL, crash recovery | ⬜ |
| M4 | MVCC, VACUUM | ⬜ |
| M5 | Parser, catalog | ⬜ |
| M6 | Planner, executor, EXPLAIN | ⬜ |
| M7 | Wire protocol | ⬜ |

Sách: xong chương 1–4, 6 (nửa đầu), 10.

## Chạy test

```bash
go test ./...
```

## Nguyên tắc làm việc trong repo này

1. Làm lần lượt từng milestone, không nhảy cóc — tầng trên luôn dựa vào tầng dưới.
2. Đặt tên khái niệm **giống Postgres thật** (page, tuple, ItemId, TID, LSN, xmin/xmax),
   để kiến thức chuyển thẳng được sang khi đọc source Postgres.
3. Mỗi milestone xong thì viết một file ghi chú trong `docs/` — tóm tắt đã học được gì,
   chỗ nào bất ngờ, Postgres làm khác mình ra sao.
4. Ưu tiên code đọc hiểu được hơn code nhanh. Tối ưu là chuyện sau.
