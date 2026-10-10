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
