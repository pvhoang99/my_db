# Nhật ký học — 10/10/2026

Buổi thứ hai. Chốt chương 1, đi qua **chương 2, phần lớn 3, 4, nửa đầu 6, chương 10**,
rồi mổ xẻ **index của PostgreSQL thật** bằng `pageinspect`.

---

## 1. Học được gì — theo ghi chú

| # | Ghi chú | Ý chính |
|---|---|---|
| 06 | [Ghi file ngoài đời](../../ghi-chu/06-ghi-file-ngoai-doi.md) | 3 hàm SaveData; vì sao `O_TRUNC`; `sed -i` = SaveData2 không fsync |
| 07 | [B+tree vs LSM](../../ghi-chu/07-btree-vs-lsm.md) | Chương 2; binary search 23 vs B+tree 4 lần đọc đĩa; LSM qua ví dụ danh bạ; abstract KV |
| 08 | [Split và copy-on-write](../../ghi-chu/08-btree-split-va-copy-on-write.md) | Chương 3: mảng lồng nhau, split lan lên root, crash giữa split, COW |
| 09 | [Node format và meta page](../../ghi-chu/09-node-format-va-meta-page.md) | Chương 4 + 6: node tận byte, update 2 pha, meta page atomic |
| 10 | [Secondary index](../../ghi-chu/10-secondary-index.md) | Chương 8 + 10: bảng = KV, index = KV phụ, `(a,b)`, atomicity không composable |
| 11 | [Postgres index thật](../../ghi-chu/11-postgres-index-thuc-te.md) | Heap + ctid, cây 3 tầng, index-only, skip scan PG18, bitmap, UPDATE đổi ctid |

### Sợi chỉ xuyên suốt cả buổi

Mọi kỹ thuật hôm nay là **cùng một ý tưởng** lặp lại ở quy mô khác nhau:

```
SaveData2:  ghi file tam   -> fsync -> rename            (1 file)
LSM:        merge ra file moi        -> thay file cu      (ca tang)
COW B+tree: chep duong di len root   -> doi con tro root  (ca cay)
Chuong 6:   ghi page moi   -> fsync -> ghi meta page     (1 sector)
Postgres:   ghi tuple moi (ctid moi) -> VACUUM don ban cu (1 dong)
```

> **Không ghi đè cái cũ. Dựng xong cái mới. Chuyển sang nó trong một bước. fsync đúng thứ tự.**

---

## 2. Thí nghiệm đã chạy

| Thí nghiệm | Kết quả |
|---|---|
| `go test -race ./internal/storage/safewrite/` | Pass hết. SaveData1: **1573/5300** lần đọc thấy file rỗng; SaveData2/3: **0** lần rách |
| Ghi đè có/không truncate | `[ngann ban mot rat dai]` vs `[ngan]` |
| inode qua `echo >`, `sed -i`, Python | Chỉ `sed -i` đổi inode |
| `strace sed -i` | `O_EXCL` file tạm + `rename`, **không fsync** |
| Demo CRC32 (`scratchpad/crcdemo`) | 4 tình huống crash; tái hiện bẫy `crc32("") = 0` với vùng toàn số 0 |
| Dump node B+tree (`scratchpad/nodedemo`) | Chèn 1 key → gần như mọi byte đổi chỗ |
| Postgres 18 + `pageinspect` | Xem [ghi chú 11](../../ghi-chu/11-postgres-index-thuc-te.md) |

---

## 3. Kiểm tra kiến thức

✅ **Đúng:**
- Đổi city: bảng chính sửa value, index **xoá cũ + thêm mới**
- Split: chèn 8 vào `[6,7]`, chèn 10 tách `[9,10]` `[11,12]`
- Tìm 11, chèn 7 trên cây 2 tầng
- **Tự suy ra COW từ chương 1**: "ghi vào folder mới rồi rename folder"
- COW phải chép cha vì cha giữ **tham chiếu** — so sánh với fsync thư mục giữ `tên → inode`

⚠️ **Đúng một nửa:**
- Binary search thua B+tree "vì nhiều random I/O" — đúng, nhưng chỗ khác biệt thật
  là **số page phải đọc**, không phải số phép so sánh
- `(city,name)` với `city='HN' AND name>'B'` — có seek, **quên điểm dừng**
- Split khi chèn 10 — đúng, chưa nói **split dừng ở tầng cha**

❌ **Sai, đáng nhớ:**

> **Chèn 0 vào cây** → tưởng phải tạo mảng mới. Thực ra mảng đầu phụ trách **mọi
> thứ < mốc đầu**; key đầu của internal node là thừa. Postgres bỏ thật (root có mục `data` rỗng).

> **Crash giữa split** → tưởng "vẫn tìm thấy vì phải atomic". Atomic **không tự có**,
> phải **tự xây**. Ghi đè nhiều page = SaveData1 quy mô lớn.

❓ **Chưa có nền:** LSM (cần ví dụ danh bạ mới hiểu).

### Đính chính của Claude

Nói `(a,b)` không dùng được cho `WHERE b` — **sai với Postgres 18** (có skip scan).
Đúng với sách và Postgres ≤ 17.

---

## 4. Còn nợ — học lại sau

- [ ] **Chương 3:** double-write, nguyên lý chung *"luôn dựng lại được cũ HOẶC mới"*
- [ ] **Chương 5:** delete/merge trong code, test B+tree
- [ ] **Chương 6:** mục 6.6 xử lý lỗi, `mmap`
- [ ] **Postgres:** UPDATE với index — dead tuple, VACUUM, **HOT update**
- [ ] **Postgres:** planner ước lượng từ `pg_stats`; loại index Hash/GIN/BRIN
- [ ] fsyncgate — đọc kỹ (nợ từ 09/10)
- [ ] Vì sao fanout leaf (249) > internal (222) (nợ từ 09/10)
- [ ] **Chốt kiến trúc: bám sách (có tầng KV) hay bám Postgres (heap + index)** —
      `ARCHITECTURE.md` hiện đang theo Postgres

## 5. Lần sau

**Code:** bước 4 kế hoạch chương 1 — crash test cho `safewrite`; rồi `LogKV` có checksum
(nhớ: CRC phải tính trên **cả `len` lẫn payload** để chặn bẫy vùng toàn số 0).

**Đọc:** chương 11 (transaction) — chương 10 kết thúc bằng "cần có transaction".

---
---

# Phần 2 — buổi tối: kế hoạch đi sâu index + Tầng 0

Quyết định **đi sâu vào tầng index**, học song song sách Go với PostgreSQL.
Học lại từ gốc **Tầng 0 — mô hình**, từng ý nhỏ một.

### 2.1 Lên kế hoạch

- Kế hoạch: [01-ke-hoach-hoc-index-song-song.md](../../01-ke-hoach-hoc-index-song-song.md) — 8 tầng, đi sâu dần
- Chốt cách học: **sách Go làm xương sống**, PG Internals làm bản đồ,
  `nbtree/README` làm chiều sâu, container `learn-pg` để kiểm chứng.
  **Không** đọc sách PG tuần tự — chương B-tree chỉ ~20 trang, viết cho DBA,
  không giải thuật toán.
- Lưu `nbtree/README` của PG 18 vào [tham-khao/](../../tham-khao/nbtree-README-pg18.txt)
- Đính chính ghi chú cuối `docs/sach/05`: sentinel key rỗng ↔ phần tử **−∞**
  ở đầu page giữa, **không phải** high key

⚠️ Kế hoạch đọc thấy **ngợp**. Đổi cách: **mỗi lần một ý nhỏ**, hiểu rồi mới sang ý tiếp.

---

### 2.2 Tầng 0 — 4 ý nhỏ

Quy ước từ nay: chỉ dùng một từ **page** (bỏ chữ "node").
**page gốc** = trên cùng · **page giữa** = ở giữa · **page lá** = dưới cùng.

#### Ý 1: tính "page chứa được bao nhiêu" thì đổi về byte rồi mới chia

```
1 KB = 1024 byte  ->  4 KB = 4096 byte,  8 KB = 8192 byte
page 4 KB, mỗi món 40 byte  ->  4096 / 40 = 102 món
```

#### Ý 2: mỗi tầng cây = đọc 1 page

Đi từ page gốc xuống page lá, mỗi tầng đọc đúng 1 page.
Cây 4 tầng → đọc **4 page**.

#### Ý 3: Postgres phải đọc thêm 1 page ở file bảng

Sách Go chỉ có **1 file**, cắt thành các page 4KB đánh số 0, 1, 2…; mỗi
page cất một phần của cây. Postgres có **2 file**:

```
file index:  | page 0 | page 1 (gốc) | page 2 (lá) | ...
file bảng:   | page 0 | page 1 | page 2 | page 3 | ...   <- heap, chứa dòng thật
```

| | page lá chứa | Đọc 1 dòng, cây 4 tầng |
|---|---|---|
| Sách Go | **cả dòng** | 4 page |
| Postgres | chỉ **địa chỉ** của dòng (TID) | 4 + 1 = **5 page** |

#### Ý 4: thêm 1 tầng thì sức chứa NHÂN lên, số page phải đọc chỉ CỘNG 1

Mỗi page chứa 100 thứ. **Key chỉ nằm ở page lá**, page gốc/giữa chỉ chứa địa chỉ.

```
Tang 1:  1 page goc
Tang 2:  100 page giua
Tang 3:  100 x 100 = 10.000 page la  x 100 key  =  1.000.000 key
```

| Số tầng | Số key | Page phải đọc |
|---|---|---|
| 1 | 100 | 1 |
| 2 | 10.000 | 2 |
| 3 | 1.000.000 | 3 |
| 4 | 100.000.000 | 4 |

---

### 2.3 Thí nghiệm trên Postgres thật

Bảng `hoc.t` 100.000 dòng `int`, index B-tree trên `id` (container `learn-pg`, PG 18.6).

| Đoán trước | Thật |
|---|---|
| 0,5 entry / page lá | **367** |
| 3 tầng | **2 tầng**, page gốc có **274** con |
| đọc 1 page | **3 page** (`Buffers: hit=2 read=1`) |

Tính tay ra **đúng 367**:

```
8192 - 24 (header) - 16 (special) = 8152 byte dung duoc
x 90% (PG chua 10% trong)         = 7337
/ 20 byte moi entry (16 + 4)      = 366,8  ->  367
```

3 page = page gốc → page lá → page ở file bảng.
276 page index = 274 lá + 1 gốc + 1 metapage (page 0).

---

### 2.4 Những chỗ đã vướng — và gốc của nó

❌ **"0,5 entry"** — chia **KB cho byte** (8 ÷ 16). Phải đổi cùng đơn vị trước.

❌ **"đọc 1 page"** — quên mỗi tầng cây là một lần đọc, và quên bước sang file bảng.

❌ **"10.101 key"** — cộng `1 + 100 + 10.000`. Đó là **tổng số page**, không phải
số key. Hai lỗi chồng nhau:
- key **chỉ nằm ở page lá** → chỉ đếm tầng lá
- "mỗi page chứa 100" → phải **nhân**, không cộng

❓ Hỏi lại được hai chỗ dễ lẫn: *lá và page khác gì?* (page = cái hộp, lá = vai
trò của hộp trong cây), *sách Go chỉ có 1 file thì page ở đâu?* (page = một khúc của file).

---

### 2.5 Lần sau học gì

**Tầng 1 — một page index, từng byte.** Đi từng ý nhỏ:

1. Hex là gì, vì sao `6f 01` lại là số 367 (little-endian)
2. Bên trong một page: header ở đầu, danh sách địa chỉ, dữ liệu ở cuối
3. Tự đọc header của một page lá thật trong `learn-pg`

#### Còn nợ thêm
- [ ] (để sau) Index 110.000 dòng có mấy tầng? — cần biết page giữa chỉ lấp 70%
