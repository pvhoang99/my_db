# minidb — mini database engine in Go

Mục tiêu: hiểu PostgreSQL từ a→z bằng cách tự build lại các tầng cốt lõi của nó.
Mỗi milestone là một tầng độc lập, có test riêng, và map 1-1 với một phần thật trong Postgres.

## Kiến trúc đích

```
psql / pgx driver
      │  PostgreSQL wire protocol (v3)
┌─────▼──────────────────────────────────────────┐
│ pgwire    — startup, auth, Query/Parse/Bind     │  M7
├────────────────────────────────────────────────┤
│ sql       — lexer → parser → planner → executor │  M5, M6
├────────────────────────────────────────────────┤
│ txn       — snapshot, MVCC (xmin/xmax), vacuum  │  M4
├────────────────────────────────────────────────┤
│ wal       — write-ahead log, redo, checkpoint   │  M3
├────────────────────────────────────────────────┤
│ access    — heap table, B+tree index            │  M2
├────────────────────────────────────────────────┤
│ buffer    — buffer pool, clock-sweep, pin/dirty │  M2
├────────────────────────────────────────────────┤
│ storage   — 8KB slotted page, heap file (fd)    │  M1
└────────────────────────────────────────────────┘
```

## Milestones

| # | Nội dung | Tương ứng trong Postgres | Trạng thái |
|---|----------|--------------------------|------------|
| M1 | Slotted page 8KB + heap file trên disk | `bufpage.c`, `smgr/md.c` | ⬜ |
| M2 | Buffer pool (clock-sweep) + B+tree index | `bufmgr.c`, `nbtree/` | ⬜ |
| M3 | WAL: record, flush, crash recovery (redo) | `xlog.c` | ⬜ |
| M4 | Transactions + MVCC: xmin/xmax, snapshot, isolation | `heapam.c`, `procarray.c` | ⬜ |
| M5 | SQL frontend: lexer, parser → AST | `gram.y`, `scan.l` | ⬜ |
| M6 | Planner + volcano-model executor (seq scan, index scan, join) | `optimizer/`, `executor/` | ⬜ |
| M7 | PostgreSQL wire protocol v3 — connect bằng `psql` thật | `backend/libpq/` | ⬜ |

## Nguyên tắc

- Mỗi milestone chạy được độc lập, có unit test + một demo nhỏ.
- Đặt tên khái niệm giống Postgres (page, tuple, ItemId, LSN, xmin/xmax, TID) để kiến thức chuyển thẳng sang đọc source Postgres thật.
- Không tối ưu sớm; ưu tiên code đọc hiểu được.
