# Nhật ký học — 11/10 → 12/10/2026

Hai buổi liền nhau. Học **đọc EXPLAIN**, chốt **cách làm minidb**, viết **roadmap
chi tiết**, rồi code xong **M1.1 (page)** và **M1.2 (tuple)** — cả hai đọc được
byte thật của PostgreSQL.

---

## 1. Đổi cách học

> **Không tự điền code nữa. Mục tiêu: hiểu PostgreSQL qua một minidb.**

Mỗi bước roadmap đi một vòng:

```
1 Lý thuyết (một ý nhỏ, có hình) -> 2 Claude viết code -> 3 Chạy, đối chiếu Postgres thật -> 4 Hỏi lại
```

- Roadmap chi tiết: [ROADMAP.md](../../../ROADMAP.md) — M1 → M7, mỗi bước có
  *Hiểu / Đọc / Code / Đối chiếu / Xong khi*
- Cấu trúc thư mục tới từng file, mỗi file ↔ một file Postgres:
  [ARCHITECTURE.md](../../../ARCHITECTURE.md)
- Trang ôn tập bằng sơ đồ (ch.1–10 + Postgres index): https://claude.ai/artifact/4HC9t1nxgtxj9X3xfJkxFS

---

## 2. Đọc EXPLAIN (11/10)

### Giải phẫu một dòng

```
Index Scan using idx_email on users  (cost=0.42..8.44 rows=1 width=28) (actual ... rows=1.00 loops=1)
  Index Cond: (email = 'u777@x.com'::text)
  Buffers: shared hit=4
```

| Nhìn vào | Để biết |
|---|---|
| Chữ đầu dòng | Quét **bằng cách nào** |
| `rows` đoán vs `rows` thật | Planner đoán đúng không — kỹ năng quan trọng nhất |
| `cost` | Đơn vị = **một lần đọc page** (`seq_page_cost=1`, `random_page_cost=4`) |
| `Buffers` | Đã đụng bao nhiêu page. `4 = 3 tầng index + 1 page heap` |
| `Index Cond` vs `Filter` | Dùng cây để nhảy tới, hay lấy ra rồi mới lọc. `Rows Removed by Filter` lớn = lãng phí |

Đọc từ **trong ra ngoài**: dòng có `->` thụt vào chạy trước, đưa kết quả lên dòng cha.

### Bốn kiểu quét — đo thật

`WHERE id <= n`:

```
khop     ty le   Postgres chon     cost
     1   0,001%  Index Scan           8
20.000   20%     Index Scan         726
60.000   60%     Seq Scan         1.986   <- seq scan gia CO DINH, index dat dan
```

Cùng 20% nhưng khác kế hoạch:

```
id <= 20000   -> Index Scan    (correlation = 1,00: dong nam lien nhau tren dia)
city = 'HN'   -> Bitmap Scan   (correlation = 0,20: HN rai khap 736 page)
   Bitmap 1.454  <  Seq 1.986  <  Index 3.542
```

### Index scan vs bitmap scan

Cả hai **đều đi xuống cây B-tree**. Khác ở lúc **sang heap**:

- **Index scan:** gặp ctid nào nhảy sang heap ngay → nhảy lung tung, có thể đọc lại
  cùng page. Kết quả **đúng thứ tự index** (hợp `ORDER BY … LIMIT`).
- **Bitmap scan:** gom hết ctid, đánh dấu page nào có dòng, rồi đọc heap **theo thứ tự
  page, mỗi page một lần**. Gộp được nhiều index (`BitmapAnd`, `BitmapOr`).

### Heap là gì

**File chứa dữ liệu thật của bảng** (`users` = `base/5/19863`, 736 page). "Heap" = **một
đống**: dòng mới đặt vào chỗ còn trống, **không sắp xếp**. Không liên quan cấu trúc dữ liệu heap.

> **Index sắp xếp được mục lục, không sắp xếp được nội dung bảng.**
> `idx_city_name` đã sắp theo city, nhưng các mục HN trỏ về page 0, 7, 73, 735, rồi quay lại 73.

---

## 3. Những quyết định đã chốt

| Câu hỏi | Chốt |
|---|---|
| Có code theo **DDD** không? | **Không dùng phần chiến thuật** (Entity, Aggregate, Repository) — chúng giấu đúng thứ đang học. **Giữ phần chiến lược**: tên giống Postgres = ngôn ngữ chung; mỗi package = bounded context; luật import chỉ đi xuống |
| Chia folder thế nào? | Theo **tầng kỹ thuật** như Postgres (`storage`, `buffer`, `access`, `recovery`…), không theo nghiệp vụ. DDD: phụ thuộc trỏ **vào domain**; database engine: phụ thuộc trỏ **xuống storage** |
| Nguồn học | PGI (Postgres làm cụ thể ra sao) · CMU 15-445 (nguyên lý) · sách Go (code mẫu) · nbtree README (B-tree sâu) |

---

## 4. M1.1 — Page ✅

**Code:** `internal/storage/page.go`, `file.go` · `internal/common/types.go`

```
| header 24B | o1 o2 o3 -> |   trong   | <- dong3 dong2 dong1 |
             ^ lower               upper ^
```

- **Header 24 byte** = 8+2+2+2+2+2+2+4. Mọi kích thước tính bằng **byte**.
- **Ô 4 byte** = `lp_off` (15 bit) + `lp_flags` (2 bit) + `lp_len` (15 bit).
- Dữ liệu **mọc ngược** → thêm dòng không phải dời dòng cũ.
- `PageInit`: page trống có `lower=24`, `upper=special=8192` (B-tree: 8176, chừa 16 byte special).
- `pd_pagesize_version = 8192 | 4 = 0x2004` — gộp hai số vào 2 byte.
- **File** = dãy page, page n ở byte `n × 8192` (giống `md.c`).

**Đối chiếu:** đọc page 0 thật của `users`: `lower=612`, `upper=664`, 147 ô, ô 1 `LP_DEAD`.

**Phát hiện:** sau UPDATE + dọn tuple chết, Postgres **dồn page lại** — ô 2 dịch từ 8096 → 8144.

### Hỏi thêm

- **Vì sao PG 8KB, MySQL 4KB?** — Sai: InnoDB mặc định **16KB**. PG 8KB vì lịch sử;
  InnoDB 16KB vì nhét **cả dòng** vào page lá B-tree. Cả hai > 4KB ổ đĩa ghi atomic
  → cả hai cần **double-write** (PG: full-page write; InnoDB: doublewrite buffer).
- **`File` đã atomic chưa?** — **Chưa, và cố ý.** `WritePage` ghi đè tại chỗ, có thể torn
  page. Postgres cũng vậy: an toàn đến từ **WAL + full-page write** (M3).
  Ghi nhớ: `NumBlocks` hiện báo lỗi nếu file dài lẻ → sửa ở M3.2.

---

## 5. M1.2 — Tuple ✅

**Code:** `internal/types/datum.go` · `internal/access/heap/tuple.go`

```
| header 23B | null bitmap | dem | id | name | email | city |
  xmin xmax cid ctid infomask t_hoff        cot xep lien, co can le
```

Dòng `id=2` (46 byte):

```
fa02 0000 | 0000 0000 | 0000 0000 | 0000 0000 0200 | 0400 | 0209 | 18 | 00
  xmin=762    xmax=0      cid=0       ctid=(0,2)       4 cot  0x0902 hoff=24 dem
0200 0000 | 0d 7573657232 | 13 7532407...6d | 07 444e
   id=2      "user2"         "u2@x.com"       "DN"
                       24 + 4 + 6 + 9 + 3 = 46
```

| Luật | Ví dụ |
|---|---|
| Số cố định byte, **căn lề** | `int8` ở bội số của 8 → có 4 byte đệm trước nó |
| Chuỗi có **độ dài đi trước** | ngắn (≤126): 1 byte `(len<<1)\|1`, không căn lề; dài: 4 byte, căn 4 |
| **NULL không chiếm byte** | chỉ 1 bit trong null bitmap (bit 0 = NULL) |
| `t_hoff` | luôn bội số của 8 |

**Đối chiếu:** đọc đúng giá trị **và** tự tạo lại dòng **giống từng byte** với Postgres, cho
cả `users` lẫn bảng khó `hoc_m12` (int8, bool, NULL, chuỗi 200 byte).

**Hint bit:** chỉ khác 1 bit `HEAP_XMIN_COMMITTED`. Postgres bật nó khi **có người đọc**
dòng lần đầu → **chỉ đọc cũng làm page thay đổi**. Học kỹ ở M4.

### Dòng và tuple

> **Tuple = một phiên bản của một dòng, lưu trên đĩa.**

UPDATE không sửa tại chỗ mà tạo tuple mới → một dòng có thể có nhiều tuple
(`id=1`: ô 1 chết, ô 147 sống). Đó là nền của MVCC. "Tuple" cũng dùng cho mục trong
index (index tuple).

---

## 6. Hiểu nhầm đã sửa

| Nghĩ | Thật ra |
|---|---|
| id nằm trên RAM, city nằm trên đĩa | Cả hai cùng một file. Khác ở **thứ tự**: bảng tình cờ xếp theo id, không theo city |
| Index đã sort thì đọc nhanh | Index sort **mục lục**, không sort **bảng** |
| Bitmap scan không dùng B-tree | Có dùng — chỉ khác lúc sang heap |
| MySQL page 4KB | InnoDB **16KB**; 4KB là page của OS |
| (Claude) `(a,b)` không dùng được cho `WHERE b` | PG 18 có **skip scan** |

---

## 7. Lần sau

~~**M1.3 — Heap file**~~ → xong, xem mục 8.

Rồi **M1.4 `pagedump`**, sang **M2.1 buffer pool** (🔍 `Buffers: hit/read`).

### Còn nợ
- [ ] Câu hỏi: chèn 1 dòng 46 byte vào page trống → `lower`, `upper` bằng bao nhiêu?
- [ ] Câu hỏi: nếu bảng chèn theo thứ tự city, `WHERE city='HN'` đọc khoảng mấy page?
- [ ] Đoán 3 kiểu quét (cuối ý 2 EXPLAIN)
- [ ] Nợ cũ: double-write (→ M3.3), HOT/VACUUM (→ M4), fsyncgate

---

## 8. Cuối buổi 12/10: M1.3 + hỏi đáp về page

### M1.3 — Heap file ✅

**Code:** `internal/access/heap/heap.go` — `Insert`, `Fetch(ctid)`, `Scan` (= Seq Scan).

INSERT chọn page: **page cuối còn đủ chỗ thì chèn, không thì nới file 1 page**. Heap giữ page cuối
trong RAM ("buffer pool 1 page", M2.1 sẽ thay).

**Đối chiếu:** chèn đúng 100.000 dòng như `users` → **736 page, mọi ctid giống Postgres**
(`147 → (1,1)`, `100000 → (735,30)`). Page 0 chứa 146 dòng, các page sau 136 — vì id nhỏ thì dòng nhỏ.
Seq scan đọc đúng 736 page = `Buffers: 736` trong EXPLAIN.

Đã thêm **comment chi tiết** cho mọi trường struct và từng bước trong hàm, toàn bộ code minidb.

### Hỏi đáp về page

Ghi chú đầy đủ: [12-ben-trong-page.md](../../ghi-chu/12-ben-trong-page.md) ·
hình động 6 bước: https://claude.ai/artifact/WA7JYzAUeUSECo9xiwJZ6s

- Page chia **5 phần**: header · mảng ô · trống · dữ liệu · special
- **Page header giữ ranh giới, ô giữ con trỏ, tuple header giữ thông tin phiên bản**
- **OffsetNumber** = ô số mấy (≠ `lp_off` = byte thứ mấy); đếm từ 1
- Ô luôn 4 byte; **mảng ô không cố định** (`lower − 24`), gần như không co
- Một page **nhiều dòng**; một dòng có thể **nhiều tuple** (phiên bản)
- Dòng > 8KB: tuple thì không (max ~8160), dòng thấy được thì có — nhờ **TOAST**
- DELETE không xoá byte nào; VACUUM dồn page, ô thành UNUSED rồi được **dùng lại**
  → SELECT không ORDER BY có thể khác thứ tự chèn

### M1.4 — pagedump ✅ · M1 hoàn thành

`go run ./cmd/pagedump -cols int4,text FILE` đọc **thẳng file Postgres** (`docker cp` ra) và khớp
từng số với pageinspect, còn in luôn giá trị. Khác file minidb ở 3 chỗ: `lsn` (chưa WAL, M3),
`checksum` (M3.3), hint bit (M4). Checksum chỉ có **trên đĩa**: `get_raw_page` (bản trong RAM) cho 0,
file trên đĩa cho 51463 → page trong RAM ≠ page trên đĩa (→ M2.1).

### Kiểm tra kiến thức M1 — **6/8**

✅ ctid (3,7) = page 3 ô 7 · DELETE chỉ ghi xmax · INSERT vào page cuối · VACUUM dồn page mà
ctid không đổi · text 50KB → dòng vài chục byte (TOAST) · torn page, WAL cứu

❌ **lower = 100 → "100 ô"**. Đúng: `(100 − 24) ÷ 4 = 19`. lower/upper/special là **vị trí byte**.
Cùng gốc với lỗi "0,5 entry" hôm 10/10: **đổi đơn vị trước khi tính**.

❌ **int8 sau int4 → "byte 28"**. Đúng: **32**. int8 phải ở bội số của 8 → 4 byte đệm.
Thứ tự cột ảnh hưởng kích thước dòng: `(int4,int8,int4)` có đệm, `(int8,int4,int4)` không.

---

## 9. M2 — buffer pool, B-tree, ba kiểu quét ✅

| Bước | Code | Đối chiếu Postgres |
|---|---|---|
| **M2.1** Buffer pool | `internal/buffer/pool.go`: ReadBuffer → Page → MarkDirty → Release; pin, dirty, usage 0–5, clock-sweep | Seq scan lần 1 `read=736`, lần 2 `hit=736` — **giống hệt** (khởi động lại PG để pool trống) |
| **M2.2** B-tree tìm kiếm | `btree/page.go`, `search.go`: metapage, special 16B, index tuple, high key, −∞, Lehman–Yao | Đọc **file index thật**: tìm cả 1.000 id (2 tầng) và 400 chuỗi (3 tầng), mọi ctid khớp |
| **M2.3** Chèn + split | `insert.go`, `check.go` (như amcheck); `Page.AddItemAt` | 10.000 key tăng dần: **30 page / 28 lá / 366 mục mỗi lá**; xáo trộn: **39 page / 37 lá** — khớp |
| **M2.4** Ba kiểu quét | `btree/scan.go` (quét khoảng), `heap.Fetcher`, `executor/scan.go` | id 1..20000: 736 · 204 · 204 (PG 736 · 203 · 203); name user5..user6: 736 · 2263 · 165 (PG 736 · 2231 · 133) |

### Những ý đáng nhớ

- **Buffer pool chỉ chứa page 8KB** (của bảng, index, FSM/VM, catalog, TOAST) + sổ ghi chép mỗi ô.
  Cache theo **page**, không theo bảng: bảng lớn chỉ phần nóng nằm lại. Không cache kết quả truy vấn.
- Pool nhỏ hơn bảng → seq scan lần 2 vẫn đọc lại hết → lý do Postgres có **ring buffer 256KB**.
- **Cây B-tree là một file trên đĩa**; "con trỏ" giữa các page là **số page**. Page gốc không nhất
  thiết ở đầu file — metapage cho biết nó ở đâu.
- Ở page giữa, ctid của mục là **page con**; chỉ ở page lá ctid mới trỏ vào heap.
- Ở page lá: **tìm nhị phân** mục đầu tiên ≥ key, rồi **đi tuần tự** sang phải. B-tree sắp **ô** theo
  key (dời ô 4 byte), dữ liệu vẫn nằm lộn xộn.
- **Chọn điểm tách**: page ngoài cùng bên phải → 90% (lá) / 70% (page giữa); còn lại 50/50.
  Lần đầu minidb ra 32 lá (chỉ dùng 90% khi key ở cuối) → sửa thành "mọi split page ngoài cùng phải"
  → **37 lá, khớp**. Thứ tự chèn quyết định index to hay nhỏ (+30% page khi chèn lộn xộn).
- **Pivot khi key bằng nhau**: đã cắt heap TID → đi phải; còn giữ heap TID → đi trái.
- **Postgres vs InnoDB**: PG index phụ trỏ **ctid** (1 cây + 1 page heap) nhưng UPDATE đổi ctid
  → sửa mọi index; InnoDB index phụ lưu **khóa chính** (2 cây) vì dòng dời chỗ khi split.
- Khóa chính trong PG chỉ là một index bình thường; mọi cột nằm ở heap; id lưu 2 lần.
- "Heap" trong Postgres = **file bảng trên đĩa** (một đống page), không phải RAM.
- Dòng mới **không phải lúc nào cũng ở page cuối**: sau DELETE + VACUUM, FSM cho dòng mới vào
  page cũ; UPDATE có thể chuyển dòng sang page khác.
- **Import dữ liệu quá khứ**: sort theo cột hay hỏi theo khoảng (thường là thời gian), import vào
  bảng **chưa có index**, `CREATE INDEX` sau, rồi `ANALYZE`. Đo thật: range 5.001 dòng — đã sort
  **65 page**, lộn xộn **1.762 page**; tìm 1 dòng thì như nhau.

### Dời lại

- **Index Only Scan** → M4.3 (cần visibility map).
- minidb so sánh text theo byte → index text phải `COLLATE "C"`.
- minidb đọc metapage mỗi lần (+1 page so với PG, vốn giữ trong relcache).

### Lần sau

**M3 — WAL và crash recovery**: mất điện thì sao? Nối lại chương 1 (fsync, torn page, log +
checksum) và phần double-write còn nợ từ chương 3.
