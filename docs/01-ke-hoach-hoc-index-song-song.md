# Kế hoạch đi sâu tầng Index — sách Go ⇄ PostgreSQL nbtree

Mục tiêu: hiểu B-tree của PostgreSQL (`nbtree`) tới mức **đọc được từng byte trên
page** và **tự code lại được một bản rút gọn** trong `internal/access/btree`.

## 4 nguồn, mỗi nguồn một vai

| Nguồn | Vai | Độ sâu |
|---|---|---|
| Sách Go ch.2–5, 9–10 (`docs/sach/`) | **Khung + code mẫu**. Đi trước mọi thứ | Nguyên lý, code chạy được |
| PG Internals (`../postgres-internals-vi/` + PDF) | **Bản đồ** Postgres làm gì, nhìn từ ngoài vào | Ch.25 bản gốc chỉ ~20 trang (tr.421–441) |
| **`docs/tham-khao/nbtree-README-pg18.txt`** | **Tài liệu sâu nhất**: thuật toán thật, viết bởi chính người code `nbtree` | 1083 dòng, mỗi mục ~50–100 dòng |
| Container `learn-pg` (PG 18.6) | **Kiểm chứng**: `pageinspect`, `amcheck`, `pg_waldump` | Byte thật trên đĩa |

> ⚠️ PG Internals viết cho **PG 14**, container là **PG 18**. Khác biệt đáng kể với
> index: PG 18 có **skip scan** (sách bảo "chưa có").

### Có nên bám sát sách PostgreSQL không? — **Không bám tuyến tính.**

- Sách xếp index ở **Phần V (cuối)**, sau MVCC, WAL, lock, planner. Đọc theo thứ tự
  thì phải qua ~400 trang mới tới index.
- Ch.25 viết cho **người dùng/DBA** (soi trang, operator class, thuộc tính, chọn
  thứ tự cột). Nó **không** giải thuật toán: Lehman–Yao, chọn điểm split, xoá page
  2 pha, WAL của split… — mấy thứ đó chỉ có trong `nbtree/README`.
- Nhưng **phải mượn** đúng các mảnh MVCC/vacuum mà index phụ thuộc (03 §3.4,
  05 §1.6 + §5, 06 §3.2). Bảng dưới chỉ rõ đọc mảnh nào, lúc nào.

→ **Sách Go làm xương sống, PG Internals làm bản đồ, README làm chiều sâu.**

## Những chỗ sách Go KHÁC nbtree (rút ra khi đọc lại)

Đây là "trục" của cả kế hoạch — mỗi tầng dưới đây đào sâu một dòng:

| | Sách Go | PG nbtree | Tầng |
|---|---|---|---|
| Lá chứa gì | key + **value** (clustered) | key + **TID** trỏ heap | 0 |
| Internal node | `n` nhánh, `n` key (key = min của cây con, "low key") | `n` nhánh, `n−1` key + phần tử **−∞** + **high key** (biên trên) | 1–2 |
| Vùng dữ liệu trong page | xếp xuôi sau mảng offset | **mọc ngược** từ cuối page; cuối page có **special space** 16B (prev, next, level, flags) | 1 |
| Tìm trong node | tuyến tính (`TODO: binary search`) | binary search `_bt_binsrch` | 2 |
| Key trùng | không cho (KV); index phụ thì nhét PK vào key | cho trùng; **TID là cột khoá ẩn** (PG 12+) → mọi entry vẫn duy nhất | 4 |
| Update node | copy-on-write cả đường lên root | sửa tại chỗ + WAL | 3, 6 |
| Đi sang key kế | quay lên cha qua `path[]` | **right-link** đi ngang | 2, 7 |
| Node vơi | **merge** khi < 1/4 page | **không merge**; chỉ xoá page **rỗng hẳn**, 2 pha | 5 |
| Đồng thời | 1 writer | nhiều writer, Lehman–Yao, khoá từng page | 6 |

> ✏️ Đính chính ghi chú cuối `docs/sach/05`: *sentinel key rỗng* của sách tương ứng
> với phần tử **−∞** ở đầu internal page của PG (README "Notes About Data
> Representation"), **không phải** high key. High key là **biên trên**, sách không có.

---

## Tầng −1 · Nền tảng cần có trước (bổ sung 10/10/2026)

Lộ ra ở buổi đầu: đoán "0,5 entry/page" (chia KB cho byte) và "đọc 1 page"
(quên mỗi tầng cây = 1 page). Trước khi vào sâu cần chắc 3 thứ:

| Kỹ năng | Cần cho | Bài tập |
|---|---|---|
| **Đơn vị**: bit, byte, KB; "page chứa được bao nhiêu" | mọi tầng | tính tay số entry/page, số tầng cây |
| **Đếm IO**: mỗi tầng = 1 page đọc, + heap | Tầng 0, 2, 7 | đoán `Buffers:` trong `EXPLAIN` |
| **Hex + little-endian** | Tầng 1 | đọc tay `6f 01 00 00` = 367 |

Và đọc sách Go **chương 2 + 3.1–3.3** trước khi chạm vào Postgres.

## Tầng 0 · Mô hình (1 buổi, nhanh)

| Sách Go | PG Internals | README |
|---|---|---|
| 2.1–2.4, 3.1–3.3 | 25 §0–1 · 03 §3.4 · 19 §1.3–1.5 | — |

**Lab** (đã chạy thử): 100.000 dòng `int` → cây **2 tầng**, root ~274 con, 276 page.
```sql
SELECT root, level FROM bt_metap('t_id');
SELECT live_items, btpo_level FROM bt_page_stats('t_id', (SELECT root FROM bt_metap('t_id')));
```
**Câu hỏi:** tìm 1 dòng qua index PG tốn mấy lần đọc page? Sách tốn mấy lần?

---

## Tầng 1 · Một page index, từng byte (1–2 buổi) ⭐

| Sách Go | PG Internals | README |
|---|---|---|
| 4.1–4.2 định dạng node | 03 §3.1 cấu trúc trang · 25 §3.1–3.3 metapage, high key, pageinspect | *Notes About Data Representation* |

Bố cục một page lá (đo thật trên `learn-pg`):

```
0      24                  lower=1492        upper=2304          special=8176  8192
| header | ItemId[] 4B mỗi cái →  | ... vùng trống ... | ← IndexTuple[] | opaque 16B |
```

**Lab — tự giải mã bằng tay:**
```sql
SELECT encode(substr(get_raw_page('t_id',1), 1, 24),'hex');    -- page header
SELECT encode(substr(get_raw_page('t_id',1), 8177, 16),'hex'); -- special space
```
Kết quả thật: header `…d405 0009 f01f 0420…` → little-endian: `lower=0x05d4=1492`,
`upper=0x0900=2304`, `special=0x1ff0=8176`, `0x2004` = size 8192 | version 4.
Special `00000000 02000000 00000000 0100 0000` → `prev=0, next=2, level=0, flags=1 (BTP_LEAF)`.

Mỗi IndexTuple = header 8B (`t_tid` 6B + `t_info` 2B) + key đã căn lề 8B → `itemlen=16` với `int4`.

**Code (= M1):** `internal/storage/page` — slotted page kiểu PG: `PageHeader`,
`ItemId`, `pd_lower`, `pd_upper`, `pd_special`. **Bài kiểm tra:** copy file index
thật ra (`SELECT pg_relation_filepath('t_id')` → `docker cp`) và cho code Go của
bạn **đọc được page của Postgres**. Đọc đúng = hiểu đúng.

---

## Tầng 2 · Tìm kiếm (1–2 buổi)

| Sách Go | PG Internals | README |
|---|---|---|
| 4.2 `nodeLookupLE` · 5.1 sentinel · 9.1 seek | 25 §2.1–2.4 | *The basic Lehman & Yao Algorithm* (đọc phần search) |

**Lab:** đi tay từ root → lá → heap, đúng như `Index Scan` làm:
```sql
SELECT itemoffset, ctid, data FROM bt_page_items('t_id', <root>);  -- phần tử 1 data rỗng = −∞
SELECT itemoffset, ctid, data FROM bt_page_items('t_id', <con>);   -- phần tử 1 = high key
SELECT * FROM t WHERE ctid = '(x,y)';
```
**Code:** search trong Go với binary search + quy ước −∞ + high key. So với `nodeLookupLE` của sách.
**Câu hỏi:** vì sao high key nằm ở **item 1** chứ không ở cuối? (README trả lời.)

---

## Tầng 3 · Chèn và tách trang (2 buổi) ⭐

| Sách Go | PG Internals | README + source |
|---|---|---|
| 4.3–4.5 · 5.1 root split | 25 §2.5, §3.1 · 25 §3.5 suffix truncation | *Fastpath For Index Insertion* · *Notes about suffix truncation* · `nbtsplitloc.c` (chọn điểm split) |

**Lab — tăng dần vs ngẫu nhiên** (đã chạy thử: free trung bình mỗi lá **815 B** vs **1829 B**):
```sql
CREATE TABLE a (id int); CREATE INDEX a_id ON a(id);
CREATE TABLE b (id int); CREATE INDEX b_id ON b(id);
INSERT INTO a SELECT g FROM generate_series(1, 200000) g;
INSERT INTO b SELECT g FROM generate_series(1, 200000) g ORDER BY random();
SELECT pg_size_pretty(pg_relation_size('a_id')), pg_size_pretty(pg_relation_size('b_id'));
SELECT avg(free_size) FROM bt_multi_page_stats('a_id', 1, -1) WHERE type = 'l';
SELECT avg(free_size) FROM bt_multi_page_stats('b_id', 1, -1) WHERE type = 'l';
```
Thêm: chèn tới khi `bt_metap.level` tăng → bắt được khoảnh khắc **root split**.

**Code:** split theo byte như `nodeSplit3`, rồi thêm heuristic "split lệch phải khi
chèn tăng dần". **Chạy lại đúng lab trên với cây của bạn** và so con số với PG.

---

## Tầng 4 · Key trùng: TID-as-key, deduplication (1 buổi)

| Sách Go | PG Internals | README |
|---|---|---|
| 10.1 (nhét PK vào key) · 11 (prefix compression) | 25 §2.1 (TID là khoá ẩn) · 25 §3.4 dedup | *Notes about deduplication* · *Deduplication in unique indexes* · *Posting list splits* |

**Lab** (đã chạy thử: cột 10 giá trị, 200k dòng → dedup **1368 kB** vs tắt **4416 kB**):
```sql
CREATE TABLE d (k int); INSERT INTO d SELECT g % 10 FROM generate_series(1,200000) g;
CREATE INDEX d_on  ON d(k);
CREATE INDEX d_off ON d(k) WITH (deduplicate_items = off);
SELECT pg_size_pretty(pg_relation_size('d_on')), pg_size_pretty(pg_relation_size('d_off'));
SELECT itemoffset, htid, array_length(tids,1), data FROM bt_page_items('d_on', 1) LIMIT 3;
```
**Câu hỏi:** vì sao **index UNIQUE** cũng cần dedup? Vì sao `numeric` không dedup được (`allequalimage`)?

---

## Tầng 5 · Xoá, VACUUM, bloat (2 buổi) ⭐

| Sách Go | PG Internals | README |
|---|---|---|
| 5.2–5.3 merge | 05 §1.6 `kill_prior_tuple` · 05 §5 simple / bottom-up deletion · 06 §3.2 index vacuuming · 25 §2.6 · 08 REINDEX | *Simple deletion* · *Bottom-Up deletion* · *Deleting index tuples during VACUUM* · *Deleting entire pages during VACUUM* · *Placing deleted pages in the FSM* |

**Lab** (đã chạy thử):
```sql
-- 1) index scan tự đánh dấu entry chết: xoá 300 dòng, quét qua index -> 300 item dead
DELETE FROM t WHERE id <= 300;
SET enable_seqscan = off; SET enable_bitmapscan = off;
SELECT count(*) FROM t WHERE id <= 300;
SELECT count(*) FILTER (WHERE dead) FROM bt_page_items('t_id', 1);
-- 2) index không tự co: xoá 90% -> VACUUM vẫn 2208 kB -> REINDEX còn 240 kB
```
**Câu hỏi then chốt:** sách merge thoải mái, PG thì không — vì sao? (README phần
*Deleting entire pages*: xoá page phải làm **2 pha, half-dead**, vì có reader đồng
thời đang cầm right-link.)

---

## Tầng 6 · Đồng thời và an toàn khi crash (2–3 buổi, khó nhất)

| Sách Go | PG Internals | README |
|---|---|---|
| 3.4 COW vs double-write · 6 meta page · 12 concurrency | 15 khoá bộ nhớ (buffer lock) · 10 WAL, full page writes · 14 §14.5 predicate lock trên lá B-tree | *Differences to the Lehman & Yao algorithm* · *VACUUM's linear scan, concurrent page splits* · *WAL Considerations* (incomplete split) |

**Lab:** `amcheck` kiểm cây có hợp lệ không; `pg_waldump` xem WAL record của một lần split.
```sql
CREATE EXTENSION IF NOT EXISTS amcheck;
SELECT bt_index_parent_check('t_id', true);
```
```bash
docker exec learn-pg pg_waldump -p /var/lib/postgresql/data/pg_wal <file> | grep -i btree
```
Học tầng này sau khi đã đọc PG ch.9–10 (buffer, WAL) theo kế hoạch chính.

---

## Tầng 7 · Dùng index (sách ch.9–10)

| Sách Go | PG Internals |
|---|---|
| 9.1–9.3 iterator, range · 9.2 mã hoá giữ thứ tự | 20 §1–4 index / index-only / bitmap scan · 19 §2 + 25 §4 operator class · 25 §4.4–4.5 nhiều cột, ASC/DESC |
| 10.1–10.3 secondary index, đồng bộ | 05 §2–3 HOT update |

```sql
CREATE TABLE u (id int PRIMARY KEY, email text, age int);
INSERT INTO u SELECT g, 'u'||g||'@x.com', g % 80 FROM generate_series(1, 200000) g;
CREATE INDEX u_age ON u(age); VACUUM ANALYZE u;
EXPLAIN (ANALYZE, BUFFERS) SELECT * FROM u WHERE id BETWEEN 100 AND 200;  -- Index Scan
EXPLAIN (ANALYZE, BUFFERS) SELECT id FROM u WHERE id BETWEEN 100 AND 200; -- Index Only Scan
EXPLAIN (ANALYZE, BUFFERS) SELECT * FROM u WHERE age = 30;                -- Bitmap Scan
```

---

## Không thuộc đợt này
GiST / SP-GiST / GIN / BRIN (PG 26–29), hash index (24), công thức chi phí (20 §2.2) — sách Go không có gì tương ứng.

## Tiến độ

| Tầng | Chủ đề | Code minidb | Xong |
|---|---|---|---|
| 0 | Mô hình | — | ⬜ |
| 1 | Page từng byte | slotted page + đọc file index thật của PG | ⬜ |
| 2 | Tìm kiếm | search, −∞, high key | ⬜ |
| 3 | Chèn, split | split + heuristic lệch phải | ⬜ |
| 4 | Key trùng, dedup | TID làm tiebreaker | ⬜ |
| 5 | Xoá, vacuum | đánh dấu dead, xoá page rỗng | ⬜ |
| 6 | Đồng thời, WAL | right-link, high key check | ⬜ |
| 7 | Dùng index | iterator đi ngang bằng right-link | ⬜ |
