# Kế hoạch đọc sách

Sách: **Build Your Own Database From Scratch in Go**, James Smith, 2nd ed (2024-06-11), 103 trang.
File PDF nằm ở `book/` (gitignore vì bản quyền).

## Mục lục sách và tiến độ dịch

Bản dịch tiếng Việt nằm trong `docs/sach/` — **gitignore**, chỉ đọc local, không phát tán.

| Ch | Tên | Trang | LoC luỹ kế | Bản dịch |
|----|-----|-------|-----------|----------|
| 00 | Introduction | 1 | — | ✅ `00-gioi-thieu.md` |
| 01 | From Files To Databases | 4 | — | ✅ `01-tu-file-den-database.md` |
| 02 | Indexing Data Structures | 9 | — | ✅ `02-cau-truc-du-lieu-index.md` |
| 03 | B-Tree & Crash Recovery | 14 | — | ✅ `03-btree-va-crash-recovery.md` |
| 04 | B+Tree Node and Insertion | 20 | 366 | ✅ `04-btree-node-va-insertion.md` |
| 05 | B+Tree Deletion and Testing | 30 | — | ✅ `05-btree-deletion-va-testing.md` |
| 06 | Append-Only KV Store | 35 | 601 | ✅ `06-append-only-kv-store.md` |
| 07 | Free List: Recycle & Reuse | 47 | 731 | ✅ `07-free-list.md` |
| 08 | Tables on KV | 57 | 1107 | ✅ `08-tables-tren-kv.md` |
| 09 | Range Queries | 66 | 1294 | ✅ `09-range-queries.md` |
| 10 | Secondary Indexes | 71 | 1438 | ✅ `10-secondary-indexes.md` |
| 11 | Atomic Transactions | 76 | 1461 | ✅ `11-atomic-transactions.md` |
| 12 | Concurrency Control | 80 | 1702 | ✅ `12-concurrency-control.md` |
| 13 | SQL Parser | 88 | — | ✅ `13-sql-parser.md` |
| 14 | Query Language | 96 | 2795 | ✅ `14-query-language.md` |

Mỗi bản dịch có thêm mục **📌 Đối chiếu với PostgreSQL** ở cuối — phần này
**không có trong sách**, là ghi chú riêng để bám mục tiêu học Postgres.

## ⚠️ Sách KHÔNG dạy kiến trúc PostgreSQL

Điều quan trọng nhất cần biết trước khi code. Sách dựng một DB kiểu
**SQLite / BoltDB**, không phải kiểu Postgres:

| | Sách (BoltDB-style) | PostgreSQL |
|---|---|---|
| Nơi chứa dữ liệu | **trong B+tree** (clustered index) | **heap file** riêng; index chỉ trỏ TID |
| Cách update page | **copy-on-write** — không bao giờ sửa page cũ tại chỗ | sửa page tại chỗ |
| Chống mất dữ liệu | COW + ghi root pointer atomic + fsync | **WAL** + redo khi recovery |
| Thu hồi chỗ trống | **free list** của page | **VACUUM** dọn tuple chết |
| Kích thước page | 4KB | 8KB |
| MVCC | snapshot = giữ root pointer cũ của cây | mỗi tuple mang **xmin/xmax** |
| Concurrency | 1 writer, nhiều reader | nhiều writer + lock manager |
| Giao tiếp client | thư viện nhúng, không có server | server + **wire protocol v3** |

Sách dạy **nguyên lý chung** của mọi DB rất tốt (durability, B+tree, MVCC, parser),
nhưng **cách hiện thực** thì khác Postgres ở gần như mọi tầng.

## Kế hoạch

1. ~~Dịch hết 14 chương ra tiếng Việt vào `docs/sach/`.~~ ✅ **Xong** (4.598 dòng).
2. Sau khi đọc xong, chốt kiến trúc để code (bám sách hay bám Postgres).
3. Code theo `ROADMAP.md`.
