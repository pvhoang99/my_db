# Kế hoạch đọc sách

Sách: **Build Your Own Database From Scratch in Go**, James Smith, 2nd ed (2024-06-11), 103 trang.
File nằm ở `book/` (đã gitignore vì bản quyền).

## Mục lục và trang

| Ch | Tên | Trang | LoC tích luỹ |
|----|-----|-------|--------------|
| 00 | Introduction | 1 | — |
| 01 | From Files To Databases | 4 | — |
| 02 | Indexing Data Structures | 9 | — |
| 03 | B-Tree & Crash Recovery | 14 | — |
| 04 | B+Tree Node and Insertion | 20 | 366 |
| 05 | B+Tree Deletion and Testing | 30 | — |
| 06 | Append-Only KV Store | 35 | 601 |
| 07 | Free List: Recycle & Reuse | 47 | 731 |
| 08 | Tables on KV | 57 | 1107 |
| 09 | Range Queries | 66 | 1294 |
| 10 | Secondary Indexes | 71 | 1438 |
| 11 | Atomic Transactions | 76 | 1461 |
| 12 | Concurrency Control | 80 | 1702 |
| 13 | SQL Parser | 88 | — |
| 14 | Query Language | 96 | 2795 |

## ⚠️ Sách KHÔNG phải kiến trúc PostgreSQL

Đây là điều quan trọng nhất cần biết trước khi code. Sách dựng một DB kiểu
**SQLite / BoltDB**, không phải kiểu Postgres:

| | Sách (BoltDB-style) | PostgreSQL |
|---|---|---|
| Nơi chứa dữ liệu | **Trong B+tree** (clustered index) | **Heap file** riêng; index chỉ trỏ TID |
| Cách update page | **Copy-on-write** — không bao giờ sửa page cũ tại chỗ | Sửa page tại chỗ |
| Chống mất dữ liệu | COW + ghi root pointer atomic + fsync | **WAL** (write-ahead log) + redo khi recovery |
| Thu hồi chỗ trống | **Free list** của page | **VACUUM** dọn tuple chết |
| MVCC | Snapshot = giữ root pointer cũ của cây | Mỗi tuple mang **xmin/xmax**, snapshot = tập txid đang chạy |
| Concurrency | 1 writer, nhiều reader | Nhiều writer, MVCC + lock manager |
| Giao tiếp client | Thư viện nhúng, không có server | Server + **wire protocol v3** |

Nói cách khác: sách dạy **nguyên lý chung** của mọi DB (durability, B+tree, MVCC,
parser) rất tốt, nhưng **cách hiện thực** thì khác Postgres ở gần như mọi tầng.

## Ghi chú đã đọc

- [01 — From Files To Databases](01-tu-file-den-database.md)
- [02 — Indexing Data Structures](02-cau-truc-index.md)
