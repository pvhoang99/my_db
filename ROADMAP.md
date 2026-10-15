# minidb — roadmap chi tiết: hiểu PostgreSQL bằng cách dựng lại nó

Mục tiêu: **hiểu PostgreSQL** bằng cách dựng lại các tầng cốt lõi của nó trong Go.
Mỗi bước có hai nửa đi liền nhau: **lý thuyết** (hiểu cái gì, đọc ở đâu) và
**code** (minidb làm thế nào), rồi **đối chiếu** với một Postgres thật.

## Cách làm việc

- **Claude viết code**, mỗi bước nhỏ, tên giống Postgres, comment chỉ tới file source
  Postgres tương ứng. **Bạn đọc hiểu**, chạy, đối chiếu.
- **Mỗi bước một ý nhỏ.** Hiểu xong mới sang bước tiếp.
- **Đối chiếu bằng Postgres thật** trong container `learn-pg` (PG 18.6, có `pageinspect`).
  Chỗ nào làm được thì minidb **đọc thẳng byte của Postgres** — đọc đúng là hiểu đúng.
- Song song: **đọc được `EXPLAIN`**. Các bước có nhãn 🔍 là nơi kỹ năng đó lớn lên.

### Nguồn, mỗi nguồn một vai

| Viết tắt | Nguồn | Dùng để |
|---|---|---|
| **PGI** | PostgreSQL 14 Internals (bản dịch ở `../postgres-internals-vi/`) | Postgres làm **cụ thể** ra sao |
| **CMU** | CMU 15-445 Database Systems (bài giảng trên YouTube) | **Nguyên lý** chung, giải thích từ gốc |
| **Sách** | Build Your Own Database in Go (`docs/sach/`) | Code mẫu chạy được (B+tree, parser, iterator) |
| **README** | `docs/tham-khao/nbtree-README-pg18.txt` | B-tree của Postgres ở mức sâu nhất |
| **src** | Source Postgres (github.com/postgres/postgres) | Đọc khi muốn biết chính xác |

Kế hoạch đào sâu riêng phần index (8 tầng, nhiều lab):
[docs/01-ke-hoach-hoc-index-song-song.md](docs/01-ke-hoach-hoc-index-song-song.md) — dùng cho **M2b**.

## Kiến trúc đích

```
psql
  │  PostgreSQL wire protocol v3
┌─▼─────────────────────────────────────────────┐
│ pgwire     startup, simple query               │  M7
├────────────────────────────────────────────────┤
│ parser → planner → executor  (+ EXPLAIN)       │  M5, M6
├────────────────────────────────────────────────┤
│ MVCC: xmin/xmax, snapshot, HOT, VACUUM         │  M4
├────────────────────────────────────────────────┤
│ WAL: record, LSN, redo, checkpoint             │  M3
├────────────────────────────────────────────────┤
│ heap (bảng)          │ B-tree (index)          │  M1, M2
├────────────────────────────────────────────────┤
│ buffer pool: pin, dirty, clock-sweep           │  M2
├────────────────────────────────────────────────┤
│ page 8KB + file                                │  M1
└────────────────────────────────────────────────┘
```

## Tổng quan

| # | Tầng | Câu hỏi chính | Trạng thái |
|---|---|---|---|
| M1 | Page, tuple, heap | Một dòng nằm ở đâu trên đĩa, trông thế nào? | ✅ |
| M2 | Buffer pool, B-tree, index scan | Vì sao không đọc thẳng file? Index tìm dòng ra sao? | ✅ |
| M3 | WAL, crash recovery | Postgres sửa page tại chỗ mà vẫn an toàn khi mất điện thế nào? | ⬜ |
| M4 | MVCC, VACUUM | Vì sao UPDATE tạo dòng mới? Ai thấy bản nào? | ⬜ |
| M5 | Parser, catalog | SQL biến thành cấu trúc dữ liệu thế nào? | ⬜ |
| M6 | Planner, executor, EXPLAIN | Postgres chọn cách chạy thế nào? | ⬜ |
| M7 | Wire protocol | `psql` nói chuyện với server thế nào? | ⬜ |

Ký hiệu: ✅ xong · 🟡 đang làm · ⬜ chưa · 🔍 luyện đọc EXPLAIN · ⭐ bước quan trọng

---

## M1 · Page, tuple, heap

> Một bảng Postgres là một file chia thành các page 8KB. Mỗi page là một slotted
> page: header, mảng line pointer mọc xuống, dữ liệu mọc lên.

### M1.1 Slotted page ✅ ⭐

- **Hiểu:** header 24 byte (`pd_lower`, `pd_upper`, `pd_special`…); line pointer 4 byte
  (`lp_off`, `lp_flags`, `lp_len`); vì sao dữ liệu mọc ngược từ cuối page; ctid = (page, ô).
- **Đọc:** PGI 03 *Pages and Tuples* · CMU *Database Storage* · src `bufpage.h`, `itemid.h`, `bufpage.c`
- **Code:** `internal/storage/page.go` (PageInit, AddItem, Item, FreeSpace, Verify) ·
  `internal/storage/file.go` (đọc/ghi/nới page, giống `md.c`) · `internal/common/types.go` (ItemPointer)
- **Đối chiếu:** `TestDocPagePostgresThat` đọc page 0 thật của bảng `users` và khớp
  từng con số với `page_header()` / `heap_page_items()`.
- **Đã thấy:** sau UPDATE + dọn tuple chết, Postgres **dồn page lại**: ô 2 dịch từ 8096 → 8144.

### M1.2 Định dạng tuple ✅

- **Hiểu:** `HeapTupleHeader` 23 byte (`t_xmin`, `t_xmax`, `t_ctid`, `t_infomask`, `t_hoff`);
  null bitmap; căn lề từng cột (`int4` căn 4, `int8` căn 8); `text` có header độ dài 1 hoặc 4 byte.
- **Đọc:** PGI 03 · src `htup_details.h`, `heaptuple.c`
- **Code:** `internal/types` (int4, int8, text, bool) · `internal/access/heap/tuple.go`
  (mã hoá/giải mã một dòng)
- **Đối chiếu:** giải mã `t_data` từ `heap_page_items()` của `users` ra đúng `id, name, email, city`.
- **Xong khi:** minidb in được mọi dòng của page Postgres thật, đúng giá trị.

### M1.3 Heap file ✅

- **Hiểu:** INSERT chọn page nào (page cuối, hoặc page còn chỗ theo Free Space Map);
  đọc một dòng theo ctid; seq scan = đọc page 0 → page cuối.
- **Đọc:** PGI 03 · PGI 18 *Table Access Methods* · src `heapam.c` (`heap_insert`, `heap_fetch`, `heapgettup`)
- **Code:** `internal/access/heap/heap.go`: `Insert(row) → ctid`, `Fetch(ctid)`, `Scan()` iterator
- **Đối chiếu:** chèn cùng 100.000 dòng như `users` → minidb ra **cùng số page (736)**
  và **cùng ctid** cho mỗi dòng như Postgres.

### M1.4 Công cụ `pagedump` ✅

- **Code:** `cmd/pagedump`: in header + line pointer + tuple của một page, cùng định dạng
  với pageinspect. Đọc được cả file minidb lẫn file Postgres (`docker cp` ra).
- **Dùng để:** từ đây về sau, mọi thay đổi trên đĩa đều nhìn tận mắt được.

---

## M2 · Buffer pool, B-tree, index scan

### M2.1 Buffer pool ✅ ⭐

- **Hiểu:** vì sao Postgres không dùng `mmap` (không kiểm soát được lúc ghi, không phối hợp
  được với WAL); `shared_buffers`; pin/unpin; page bẩn (dirty); chọn page để đuổi bằng
  **clock-sweep** (usage count); ring buffer cho seq scan lớn.
- **Đọc:** PGI 09 *Buffer Cache* · CMU *Buffer Pools* · src `bufmgr.c`, `freelist.c`
- **Code:** `internal/buffer`: `ReadBuffer(file, blk)`, `ReleaseBuffer`, `MarkDirty`, `FlushAll`, clock-sweep
- **Đối chiếu:** extension `pg_buffercache`; 🔍 `EXPLAIN (ANALYZE, BUFFERS)`: `shared hit` = có
  sẵn trong pool, `read` = phải đọc file. minidb in đúng hai con số đó cho seq scan của mình.

### M2.2 B-tree: page và tìm kiếm ✅ ⭐

- **Hiểu:** page B-tree có special 16 byte (prev, next, level, flags); **high key**; phần tử
  **−∞** ở đầu page giữa; metapage (page 0); đi từ page gốc xuống page lá.
- **Đọc:** PGI 25 *B-tree* · README *Notes About Data Representation*, *Lehman & Yao* (phần search) ·
  Sách ch.4 · kế hoạch index **Tầng 1–2**
- **Code:** `internal/access/btree`: định dạng page, `Search(key) → []ctid`
- **Đối chiếu:** minidb đọc file `idx_email` thật và tìm ra cùng ctid với Postgres.

### M2.3 B-tree: chèn và split ✅

- **Hiểu:** split page lá, đẩy key lên page cha, split page gốc → cây cao thêm; **right-link**
  để reader không lạc; chọn điểm split (chèn tăng dần thì split lệch phải).
- **Đọc:** README *Fastpath*, *suffix truncation* · Sách ch.4–5 · kế hoạch index **Tầng 3–4**
- **Code:** `Insert(key, ctid)` sửa tại chỗ (không copy-on-write như sách); TID làm khoá ẩn để key trùng vẫn duy nhất
- **Đối chiếu:** tạo cùng index với Postgres → cùng số tầng, số page lá xấp xỉ.

### M2.4 Index scan, bitmap ✅ 🔍

> Index-only scan dời sang **M4.3**: nó cần visibility map để biết khỏi mở heap.

- **Hiểu:** index scan nhảy sang heap với từng ctid; bitmap gom ctid rồi đọc heap theo thứ tự
  page; correlation quyết định cái nào rẻ.
- **Đọc:** PGI 20 *Index Scans* · ghi chú `docs/ghi-chu/11`
- **Code:** iterator `IndexScan`, `BitmapHeapScan` (bitmap chính xác, chưa lossy)
- **Đối chiếu:** đếm page heap đọc được với `id` (correlation 1) và `city` (0,2): minidb và Postgres
  phải ra **cùng kết luận** là bitmap rẻ hơn cho city.

---

## M3 · WAL và crash recovery

> Sách dùng copy-on-write nên không cần WAL. Postgres sửa page tại chỗ, nên WAL là thứ
> giữ mọi thứ an toàn. Đây là phần "double-write" còn nợ từ chương 3.

### M3.1 WAL record và LSN ⬜ ⭐

- **Hiểu:** WAL là log append-only có checksum (đúng ý chương 1); LSN = vị trí trong WAL;
  `pd_lsn` trên mỗi page; **quy tắc WAL: log xuống đĩa trước page**.
- **Đọc:** PGI 10 *Write-Ahead Log* · CMU *Logging* · src `xloginsert.c`, `xlog.c`
- **Code:** `internal/recovery`: ghi record (insert tuple), fsync khi commit, gắn `pd_lsn`;
  buffer pool chỉ được ghi page khi WAL tới LSN đó đã fsync
- **Đối chiếu:** `pg_current_wal_lsn()`, `pg_waldump` một lần INSERT.

### M3.2 Redo và checkpoint ⬜

- **Hiểu:** khởi động lại → đọc WAL từ checkpoint, **redo mù** từng record; record phải
  idempotent (so `pd_lsn` để bỏ qua record đã áp dụng); checkpoint để không phải redo từ đầu.
- **Đọc:** PGI 10–11 · CMU *Recovery* (ARIES, phần redo)
- **Code:** `Recover()`, `Checkpoint()`
- **Đối chiếu:** **crash test** (nợ từ bước 4 chương 1): `kill -9` tiến trình minidb ở điểm
  ngẫu nhiên hàng nghìn lần, khởi động lại, kiểm dữ liệu đã commit không mất.

### M3.3 Full-page write ⬜

- **Hiểu:** torn page; lần đầu sửa page sau checkpoint thì ghi **nguyên page** vào WAL — chính
  là double-write của chương 3.
- **Đọc:** PGI 10 · Sách 3.4 (double-write) · ghi chú 05 (torn write)
- **Code:** thêm full-page image vào WAL; giả lập torn page trong test.

---

## M4 · MVCC và VACUUM

### M4.1 xmin, xmax và snapshot ⬜ ⭐

- **Hiểu:** mỗi tuple mang `xmin` (ai tạo) và `xmax` (ai xoá); snapshot = danh sách transaction
  đang chạy; quy tắc "tuple này có thấy được không".
- **Đọc:** PGI 02 *Isolation*, 04 *Snapshots* · CMU *MVCC* · src `heapam_visibility.c`, `procarray.c`
- **Code:** `internal/concurrency`: cấp xid, snapshot, `IsVisible(tuple, snapshot)`; UPDATE = ghi
  phiên bản mới + đặt `xmax` cho bản cũ
- **Đối chiếu:** hai phiên `psql` song song; nhìn `t_xmin/t_xmax` bằng pageinspect.

### M4.2 HOT và dọn page ⬜

- **Hiểu:** HOT update (không đổi cột có index, cùng page) → `LP_REDIRECT`, index không bị đụng;
  page pruning dồn page lại (chính là 8096 → 8144 đã thấy ở M1.1).
- **Đọc:** PGI 05 *Page Pruning and HOT* · src `pruneheap.c`
- **Code:** HOT chain, pruning trong page.

### M4.3 VACUUM ⬜

- **Hiểu:** dọn tuple chết, xoá mục index trỏ tới chúng, visibility map (cho index-only scan),
  free space map.
- **Đọc:** PGI 06 *Vacuum*, 07 *Freezing* · src `vacuumlazy.c`
- **Code:** `Vacuum(table)`; 🔍 sau VACUUM, `Heap Fetches` của index-only scan về 0.

---

## M5 · Parser và catalog

### M5.1 Lexer + parser ⬜

- **Hiểu:** SQL → token → cây cú pháp (AST); chỉ một tập con: `CREATE TABLE`, `CREATE INDEX`,
  `INSERT`, `SELECT … WHERE … ORDER BY … LIMIT`, `UPDATE`, `DELETE`, `EXPLAIN`.
- **Đọc:** Sách ch.13 · PGI 16 *Query Execution Stages* (phần parse) · src `scan.l`, `gram.y`
- **Code:** `internal/parser` (parser viết tay, không dùng yacc)

### M5.2 Catalog ⬜

- **Hiểu:** Postgres lưu "có bảng nào, cột gì, index nào" **trong chính các bảng** (`pg_class`,
  `pg_attribute`, `pg_index`).
- **Đọc:** PGI 01 *Introduction* (phần catalog) · src `catalog/`
- **Code:** `internal/catalog`, lưu bằng heap của M1.

---

## M6 · Planner, executor và EXPLAIN 🔍

### M6.1 Executor kiểu volcano ⬜ ⭐

- **Hiểu:** mỗi node có `Open/Next/Close`; node cha kéo dòng từ node con; vì sao EXPLAIN đọc
  từ trong ra ngoài.
- **Đọc:** PGI 16 · CMU *Query Execution* · Sách ch.14 (iterator) · src `executor/execProcnode.c`
- **Code:** `internal/executor`: SeqScan, IndexScan, BitmapHeapScan, Filter, Project, Sort, Limit
- **Đối chiếu:** 🔍 minidb in **`EXPLAIN ANALYZE` theo đúng định dạng Postgres** (node, rows,
  loops, Buffers). Đặt cạnh output Postgres cho cùng câu truy vấn.

### M6.2 Thống kê và cost ⬜ 🔍

- **Hiểu:** `pg_stats` (n_distinct, most common values, histogram, correlation); ước lượng
  số dòng; cost = `seq_page_cost`, `random_page_cost`, CPU cost; vì sao ước lượng sai làm chọn sai plan.
- **Đọc:** PGI 17 *Statistics* · PGI 20 (cost của index scan) · src `costsize.c`, `selfuncs.c`
- **Code:** `ANALYZE` thu thống kê; planner chọn giữa seq / index / bitmap theo cost
- **Đối chiếu:** lặp lại thí nghiệm `id <= n` (0,001% → 99%): minidb đổi sang seq scan ở **cùng
  khoảng** với Postgres.

### M6.3 Join ⬜ 🔍

- **Hiểu:** Nested Loop, Hash Join, Merge Join; khi nào dùng cái nào.
- **Đọc:** PGI 21 *Nested Loop*, 22 *Hashing*, 23 *Sorting and Merging* · CMU *Join Algorithms*
- **Code:** ba thuật toán join + chọn theo cost.

---

## M7 · Wire protocol

### M7.1 Simple query protocol ⬜

- **Hiểu:** startup message, authentication (trust), `Query` → `RowDescription` / `DataRow` /
  `CommandComplete` / `ReadyForQuery`.
- **Đọc:** tài liệu Postgres *Frontend/Backend Protocol* · src `tcop/postgres.c`, `libpq/pqcomm.c`
- **Code:** `internal/pgwire`, `cmd/minidb`
- **Xong khi:** `psql -h localhost -p 5433` vào được minidb và chạy `SELECT`, `EXPLAIN`.

---

## Nguyên tắc

- Mỗi bước chạy được độc lập, có test, có một cách đối chiếu với Postgres thật.
- Đặt tên giống Postgres (page, tuple, ItemId, LSN, xmin/xmax, TID) để đọc source thật thấy quen.
- Không tối ưu sớm; ưu tiên code đọc hiểu được.
- Import chỉ đi xuống (xem [ARCHITECTURE.md](ARCHITECTURE.md)).
