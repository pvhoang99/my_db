# Index trong PostgreSQL thật — mổ xẻ bằng pageinspect

> Nối tiếp [10-secondary-index.md](10-secondary-index.md).
> Thí nghiệm trên **PostgreSQL 18.6**, bảng `users` 100.000 dòng, 10/10/2026.

## Dựng lại thí nghiệm

```bash
docker run -d --name learn-pg -e POSTGRES_PASSWORD=learn -p 55499:5432 postgis/postgis:18-3.6-alpine
docker exec -it learn-pg psql -U postgres
```

```sql
create extension pageinspect;
create table users (id int primary key, name text, email text, city text);
insert into users
select i, 'user'||i, 'u'||i||'@x.com', (array['HN','HCM','DN','HP','CT'])[1 + i%5]
from generate_series(1,100000) i;
create index idx_email on users(email);
create index idx_city_name on users(city, name);
vacuum analyze users;
```

Xoá: `docker rm -f learn-pg`.

## 1. Postgres không có tầng KV

```
SACH                                   POSTGRES
SQL                                    SQL
 |                                      |
Table -> ma hoa thanh (key, value)     Heap: tuple trong page 8KB (KHONG sap xep)
 |                                      + Index: key -> ctid
KV interface                            |
 |                                     Buffer pool + WAL
B+tree (mot cay)
```

**Heap page 0** — mỗi dòng là tuple có header MVCC, không có khái niệm key:

```
 lp | lp_off | lp_len | t_xmin | t_xmax | t_ctid
  1 |   8144 |     47 |    762 |      0 | (0,1)
  2 |   8096 |     46 |    762 |      0 | (0,2)
```

`lp_off` giảm dần → dữ liệu **mọc ngược từ cuối page** (slotted page).
Địa chỉ một dòng = **ctid** `(page, slot)`.

**Leaf của `idx_email`** — chỉ có key + ctid:

```
 heap_tid | data
 (73,62)  | u10000@x.com
 (73,63)  | u10001@x.com
```

→ **Mọi index Postgres đều là secondary**, kể cả `users_pkey` (`id -> ctid`).

## 2. Hình dạng cây thật

```
bt_metap('idx_email'):  root = 209, level = 2   -> 3 tang cho 100.000 email
```

Root page chỉ 2 mục, mục đầu **không có key** (= −∞):

```
 itemoffset |  ctid   | data
          1 | (3,0)   | (rong)
          2 | (208,1) | u576870@x.com
```

= "3 nhánh chỉ cần 2 key" của sách. Ở leaf, mục 1 là **high key** (giới hạn trên của
page), dùng với **right-link** (Lehman-Yao) để reader không lạc khi đang split.

## 3. Index tiết kiệm bao nhiêu

| `WHERE email='u777@x.com'` | Cách | Page đọc |
|---|---|---|
| Có index | Index Scan | **4** (3 tầng + 1 heap) |
| Không index | Seq Scan | **736** (`Rows Removed by Filter: 99999`) |

Giá: `idx_email` 3,1 MB + `idx_city_name` 3,1 MB > bảng 5,9 MB.

## 4. Index-only scan

`SELECT email ... WHERE email=...` → `Index Only Scan, Heap Fetches: 0, Buffers: 4`.
Index không biết dòng còn sống không (`xmin/xmax` ở heap) → dựa vào **visibility map**.
Bảng vừa bị sửa nhiều → `Heap Fetches` tăng.

## 5. Index nhiều cột và skip scan (PG 18)

```
D. city='HN' AND name='user5'  -> Index Scan, Index Searches: 1,  Buffers: 4
E. name='user5'                -> Index Scan, Index Searches: 11, Buffers: 39
```

E vẫn dùng index nhờ **skip scan**: tự liệt kê từng city rồi seek `(city, user5)`.
Chỉ hiệu quả khi cột trái **ít giá trị khác nhau**. Nguyên lý cũ vẫn đúng: 39 vs 4 page.

## 6. Bitmap scan khi kết quả nhiều

`city='HN'` (20% bảng) → `Bitmap Heap Scan, Heap Blocks: exact=736`.

- Index scan thường: đi theo thứ tự index → nhảy lung tung trong heap, có thể đọc
  một page nhiều lần
- Bitmap: gom hết ctid → **sắp theo page** → đọc heap một lượt, mỗi page một lần

Sách: "khớp được thì dùng". Postgres: **ước lượng chi phí** rồi chọn.

## 7. UPDATE làm dòng đổi địa chỉ

```
UPDATE users SET city='SG' WHERE id=1;
truoc: ctid (0,1)     sau: ctid (0,147)      slot 1: lp_flags = 3 (LP_DEAD)
```

Postgres **không sửa tại chỗ** — ghi **phiên bản mới** (COW ở mức tuple). Địa chỉ đổi →
**mọi index phải thêm mục mới**; mục cũ và slot chết ở lại tới khi **VACUUM**.
(HOT update tránh được việc này nếu không cột nào có index bị đổi — chưa học.)

## 8. So sánh

| | Sách / InnoDB | Postgres |
|---|---|---|
| Dữ liệu nằm ở | Leaf B+tree theo PK | **Heap** riêng |
| Index trỏ tới | Primary key | **ctid** (vị trí vật lý) |
| Tìm theo secondary | 2 lần tra cây | Index → nhảy thẳng heap |
| UPDATE | Sửa tại key | Phiên bản mới + sửa index + VACUUM |
| Chọn index | Khớp cột | Cost-based |
| Abstract engine | Dễ | Rất khó |

Giống sách nhất: **InnoDB**, SQLite, CockroachDB/TiDB (có tầng KV).

## Câu hỏi tự kiểm tra

1. Vì sao `users_pkey` cũng là secondary index?
2. Vì sao UPDATE trong Postgres phải đụng tới index còn sách thì không?
3. Khi nào Postgres chọn bitmap scan thay vì index scan?
