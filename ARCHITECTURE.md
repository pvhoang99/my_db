# Kiến trúc thư mục

Layout theo convention Go chuẩn ([golang-standards/project-layout](https://github.com/golang-standards/project-layout)),
còn cách chia package bên trong `internal/` thì bám theo cách Postgres chia `src/backend/`.
Mục đích: khi bạn mở source Postgres thật lên, mỗi thư mục và **mỗi file** ở đây có một
chỗ tương ứng để đối chiếu.

Database engine chia theo **tầng kỹ thuật** (từ đĩa lên SQL), không chia theo nghiệp vụ
kiểu DDD: ở đây lưu trữ không phải chi tiết bị giấu sau Repository, mà chính là thứ đang được xây.

## Cây thư mục

Ký hiệu: ✅ đã có code · `Mx.y` = bước trong [ROADMAP.md](ROADMAP.md) · `↔` = file tương ứng trong Postgres.
File chỉ được tạo khi tới bước đó; các thư mục chưa tới lượt chỉ có `.gitkeep`.

```
my_db/
├── cmd/
│   ├── minidb/main.go             M7   server: nối các tầng, mở cổng cho psql (chỉ wiring, không logic)
│   └── pagedump/main.go  ✅   M1.4 in một page ra như pageinspect (đọc được cả file Postgres)
│
├── internal/
│   ├── common/                         KIỂU DÙNG CHUNG — ai cũng import được
│   │   ├── types.go          ✅   M1.1 BlockNumber, OffsetNumber, ItemPointer (ctid)  ↔ block.h, off.h, itemptr.h
│   │   └── xid.go                 M3   LSN, TransactionId                             ↔ xlogdefs.h, transam.h
│   │
│   ├── types/                          KIỂU DỮ LIỆU SQL — ai cũng import được
│   │   ├── datum.go      ✅   M1.2 một giá trị: int4, int8, text, bool          ↔ utils/adt/
│   │   └── (so sánh key: tạm nằm trong access/btree/search.go, M2.2)
│   │
│   ├── storage/                        TẦNG 1 · byte và page
│   │   ├── page.go           ✅   M1.1 slotted page 8KB                             ↔ bufpage.c, itemid.h
│   │   ├── file.go           ✅   M1.1 file = dãy page liền nhau                    ↔ smgr/md.c
│   │   ├── checksum.go            M3.3 checksum của page                            ↔ checksum_impl.h
│   │   ├── testdata/         ✅        page Postgres thật để đối chiếu
│   │   └── safewrite/        ✅        bài tập chương 1 của sách (giữ để tham khảo)
│   │
│   ├── buffer/                         TẦNG 2 · bộ đệm
│   │   └── pool.go       ✅   M2.1 ReadBuffer, Release, MarkDirty, clock-sweep      ↔ bufmgr.c, freelist.c
│   │
│   ├── access/                         TẦNG 3 · đọc/ghi dữ liệu có cấu trúc
│   │   ├── heap/                       bảng
│   │   │   ├── tuple.go      ✅   M1.2 header 23 byte, null bitmap, giải mã cột     ↔ heaptuple.c, htup_details.h
│   │   │   ├── heap.go       ✅   M1.3 Insert, Fetch(ctid), Scan                    ↔ heapam.c
│   │   │   ├── visibility.go      M4.1 tuple này có thấy được không                 ↔ heapam_visibility.c
│   │   │   └── prune.go           M4.2 HOT, dồn page                                ↔ pruneheap.c
│   │   └── btree/                      index
│   │       ├── page.go       ✅   M2.2 high key, phần tử −∞, special 16 byte        ↔ nbtree.h
│   │       ├── search.go     ✅   M2.2 đi từ page gốc xuống page lá                 ↔ nbtsearch.c
│   │       ├── insert.go     ✅   M2.3 chèn, split, page gốc mới                   ↔ nbtinsert.c, nbtsplitloc.c
│   │       ├── check.go      ✅   M2.3 kiểm luật của cây (như amcheck)              ↔ contrib/amcheck
│   │       └── scan.go       ✅   M2.4 quét theo khoảng, đi ngang qua right-link    ↔ nbtsearch.c (_bt_first, _bt_next)
│   │
│   ├── recovery/                       TẦNG 4 · WAL
│   │   ├── record.go              M3.1 định dạng WAL record                         ↔ xloginsert.c, xlogrecord.h
│   │   ├── writer.go              M3.1 ghi, fsync, LSN                              ↔ xlog.c
│   │   ├── redo.go                M3.2 đọc lại sau crash                            ↔ xlogrecovery.c
│   │   └── checkpoint.go          M3.2                                              ↔ checkpointer.c
│   │
│   ├── concurrency/                    TẦNG 5 · transaction
│   │   ├── xact.go                M4.1 begin, commit, abort                         ↔ xact.c
│   │   ├── snapshot.go            M4.1 ai đang chạy                                 ↔ procarray.c, snapmgr.c
│   │   └── vacuum.go              M4.3                                              ↔ vacuumlazy.c
│   │
│   ├── catalog/                   M5.2 bảng nào, cột gì, index nào                  ↔ catalog/, relcache.c
│   │
│   ├── parser/                         TẦNG 6 · SQL
│   │   ├── lexer.go               M5.1 SQL → token                                  ↔ scan.l
│   │   ├── parser.go              M5.1 token → cây cú pháp                          ↔ gram.y
│   │   └── ast.go                 M5.1
│   │
│   ├── planner/
│   │   ├── stats.go               M6.2 ANALYZE, pg_stats                            ↔ analyze.c, selfuncs.c
│   │   ├── cost.go                M6.2 seq_page_cost, random_page_cost              ↔ costsize.c
│   │   └── planner.go             M6.2 chọn seq / index / bitmap / join             ↔ optimizer/
│   │
│   ├── executor/
│   │   ├── node.go                M6.1 interface Open/Next/Close                    ↔ execProcnode.c
│   │   ├── scan.go       ✅   M2.4 Seq Scan, Index Scan, Bitmap Heap Scan        ↔ nodeSeqscan.c, nodeIndexscan.c, nodeBitmapHeapscan.c
│   │   ├── filter.go, sort.go, limit.go   M6.1
│   │   ├── join_nestloop.go       M6.3                                              ↔ nodeNestloop.c
│   │   ├── join_hash.go           M6.3                                              ↔ nodeHashjoin.c
│   │   └── explain.go             M6.1 in EXPLAIN đúng định dạng Postgres           ↔ commands/explain.c
│   │
│   └── pgwire/                    M7   giao thức mạng v3                            ↔ tcop/postgres.c, libpq/pqcomm.c
│
├── test/integration/                   test xuyên tầng: ghi SQL → kill -9 → mở lại → kiểm dữ liệu
├── docs/                               ghi chú học, nhật ký, sách, tài liệu tham khảo
├── scripts/                            script tiện ích (chạy psql, sinh dữ liệu, bench)
├── go.mod
├── README.md
├── ROADMAP.md
└── ARCHITECTURE.md
```

Mỗi file Go tương ứng với một file Postgres. Muốn biết Postgres làm thật ra sao thì mở
file bên phải `↔` (đường dẫn tính từ `src/backend/` hoặc `src/include/`).

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

## Mượn gì từ DDD

Không dùng phần chiến thuật (Entity, Aggregate, Repository, Domain Event): chúng giấu
đúng những thứ đang muốn học (page là 8192 byte, việc ghi xuống đĩa) và làm khó đối
chiếu với source Postgres vốn viết theo kiểu thủ tục. Chỉ mượn phần chiến lược:

| DDD | Trong minidb |
|---|---|
| Ngôn ngữ chung | Tên đúng như Postgres: `pd_lower`, `ItemId`, `LP_DEAD`, ctid, LSN, xmin |
| Bounded context | Mỗi package một ngữ cảnh: `storage` biết byte, `buffer` biết pin/dirty, `heap` biết tuple |
| Context map | Luật phụ thuộc ở trên |
| Value object | `ItemPointer`, `BlockNumber`, `OffsetNumber`: nhỏ, bất biến, so sánh theo giá trị |

## Vì sao dùng `internal/` chứ không phải `pkg/`

Go chặn mọi module bên ngoài import package nằm dưới `internal/`. Đây là một engine,
không phải thư viện, nên mọi thứ vào `internal/` để sau này đổi API thoải mái mà
không sợ phá ai. Khi nào thực sự muốn expose một client library thì mới tạo `pkg/`.
