# Kiến trúc thư mục

Layout theo convention Go chuẩn ([golang-standards/project-layout](https://github.com/golang-standards/project-layout)),
còn cách chia package bên trong `internal/` thì bám theo cách Postgres chia `src/backend/`.
Mục đích: khi bạn mở source Postgres thật lên, mỗi thư mục ở đây có một chỗ tương ứng để đối chiếu.

```
my_db/
├── cmd/minidb/              # entrypoint — binary server, chỉ wiring, không logic
├── internal/                # toàn bộ engine; `internal` nên Go cấm package ngoài import
│   ├── common/              #   kiểu dùng chung: PageID, TID, LSN, TxnID, Oid
│   ├── types/               #   hệ kiểu dữ liệu SQL: Datum, int/text/bool, so sánh, encode
│   ├── storage/             #   M1 — slotted page 8KB + disk manager (file I/O)
│   ├── buffer/              #   M2 — buffer pool, pin/unpin, dirty, clock-sweep
│   ├── access/              #   M2 — access method: cách đọc/ghi dữ liệu có cấu trúc
│   │   ├── heap/            #       bảng heap: tuple format, seq scan, insert/update/delete
│   │   └── btree/           #       B+tree index: search, insert, split, iterator
│   ├── recovery/            #   M3 — WAL: record, writer, redo, checkpoint
│   ├── concurrency/         #   M4 — transaction manager, snapshot, MVCC visibility, lock
│   ├── catalog/             #   M5 — system catalog: có bảng nào, cột gì, index nào
│   ├── parser/              #   M5 — lexer + parser → AST
│   ├── planner/             #   M6 — AST → plan tree, chọn seq scan hay index scan
│   ├── executor/            #   M6 — volcano model: mỗi operator có Open/Next/Close
│   └── pgwire/              #   M7 — PostgreSQL wire protocol v3
├── test/integration/        # test xuyên tầng (ghi SQL → đọc lại sau restart, v.v.)
├── docs/                    # ghi chú học tập, mỗi milestone một file
├── scripts/                 # script tiện ích (chạy psql, sinh dữ liệu, bench)
├── go.mod
├── README.md
├── ROADMAP.md
└── ARCHITECTURE.md
```

## Đối chiếu với source Postgres

| Thư mục ở đây | Trong Postgres (`src/backend/` hoặc `src/include/`) |
|---|---|
| `internal/common` | `storage/block.h`, `storage/itemptr.h`, `access/xlogdefs.h` |
| `internal/types` | `utils/adt/`, `catalog/pg_type` |
| `internal/storage` | `storage/page/bufpage.c`, `storage/smgr/md.c` |
| `internal/buffer` | `storage/buffer/bufmgr.c`, `freelist.c` |
| `internal/access/heap` | `access/heap/heapam.c`, `access/common/heaptuple.c` |
| `internal/access/btree` | `access/nbtree/` |
| `internal/recovery` | `access/transam/xlog.c`, `xloginsert.c` |
| `internal/concurrency` | `access/transam/`, `storage/ipc/procarray.c`, `storage/lmgr/` |
| `internal/catalog` | `catalog/`, `utils/cache/relcache.c` |
| `internal/parser` | `parser/scan.l`, `parser/gram.y` |
| `internal/planner` | `optimizer/` |
| `internal/executor` | `executor/` |
| `internal/pgwire` | `libpq/pqcomm.c`, `tcop/postgres.c` |

## Luật phụ thuộc

Import chỉ được đi **xuống**, không bao giờ ngược lên — đây là thứ giữ cho project
không rối khi lớn dần:

```
pgwire → executor → planner → parser
                 ↘ catalog ↘
                   access/{heap,btree} → buffer → storage
                            ↘ concurrency → recovery ↗
                                      common, types  (ai cũng import được)
```

Quy tắc thực tế:
- `common` và `types` là lá, **không import** bất kỳ package nội bộ nào khác.
- `storage` không biết gì về transaction, SQL hay index — nó chỉ biết byte và page.
- Tầng dưới không bao giờ import tầng trên. Nếu thấy mình cần làm thế, nghĩa là
  trách nhiệm đang đặt sai chỗ.

## Vì sao dùng `internal/` chứ không phải `pkg/`

Go chặn mọi module bên ngoài import package nằm dưới `internal/`. Đây là một engine,
không phải thư viện, nên mọi thứ vào `internal/` để sau này đổi API thoải mái mà
không sợ phá ai. Khi nào thực sự muốn expose một client library thì mới tạo `pkg/`.
