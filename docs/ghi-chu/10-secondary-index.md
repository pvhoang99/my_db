# Secondary index — bảng và index trên nền KV (chương 8 + 10)

> Liên quan sách **mục 8.1** và **chương 10**. Bản Postgres thật:
> [11-postgres-index-thuc-te.md](11-postgres-index-thuc-te.md).

## 1. Bảng = key-value

```sql
CREATE TABLE users (id int, name text, email text, city text, PRIMARY KEY (id));
```

```
key       value
-----     -----------------------
id=1  ->  An,   an@x.com,   HN
id=2  ->  Binh, binh@x.com, HCM
id=3  ->  Chi,  chi@x.com,  HN
```

Primary key → **key**, các cột còn lại → **value**. Tìm theo id: nhanh.

## 2. Secondary index = thêm cặp KV, sắp theo cột khác

`WHERE email = ...` → cây sắp theo id → phải scan. Giải: **mục chỉ mục cuối sách**.

```
key              value
-------------    -----
an@x.com    ->   id=1
chi@x.com   ->   id=3
```

Tìm = **hai lần tra nhanh**: index ra id → bảng chính ra dữ liệu.

Chỉ có **một** B+tree → mỗi bảng/index gắn **tiền tố** để ở vùng riêng:

```
[T] id=1        -> An, an@x.com, HN      <- bang chinh
[E] an@x.com    -> id=1                  <- index email
```

Giá: tốn chỗ + mỗi lần ghi phải ghi thêm (3 index → 4 key mỗi INSERT).

## 3. Index không unique: nhét primary key vào key

Index city: `HN -> id=1`, `HN -> id=3` → **trùng key**, ghi sau đè ghi trước.

```
key              value
-------------    -------
HCM, id=2    ->  (rong)
HN,  id=1    ->  (rong)
HN,  id=3    ->  (rong)
```

Key luôn duy nhất, vẫn sắp theo city trước. `WHERE city='HN'` = **range query**:
seek tới HN, đọc tới khi hết HN.

## 4. Index nhiều cột `(city, name)` — như danh bạ (họ, tên)

| Truy vấn | Dùng index? |
|---|---|
| `city='HN' AND name='Chi'` | ✅ seek thẳng |
| `city='HN'` | ✅ cả nhóm HN liền nhau |
| `name='Chi'` | ❌ (sách) — tên Chi rải rác khắp các city |
| `city='HN' AND name > 'B'` | ✅ seek `(HN, 'B')`, **dừng khi hết HN** |
| `city > 'HN' AND name='Chi'` | ⚠️ chỉ dùng phần city, lọc name sau |

Quy tắc: dùng được khi truy vấn dùng các cột **từ trái sang**, và cột trái là **so sánh bằng**
thì cột kế tiếp mới tạo được khoảng liên tục.

⚠️ **Postgres 18 có skip scan** — `name='Chi'` vẫn dùng được index nếu city ít giá trị.
Xem [11](11-postgres-index-thuc-te.md).

**Mẹo vô cực:** `city='HN'` → khoảng `(HN, −∞)` tới `(HN, +∞)`.

Điểm hay quên: **điểm dừng**. Không dừng khi hết HN thì đọc lan sang city khác.

## 5. Atomicity không cộng dồn được

An chuyển HN → HCM = **3 thao tác**:

```
(1) sua  [T] id=1 -> ..., HCM
(2) xoa  [C] HN,  id=1          <- city nam trong KEY -> khong sua duoc, phai xoa + them
(3) them [C] HCM, id=1
```

Crash giữa (1) và (2) → bảng nói HCM, index nói HN → **index corruption**.

> **Từng thao tác atomic + từng thao tác atomic ≠ cả nhóm atomic.**

Giống chuyển tiền: trừ A atomic, cộng B atomic, crash ở giữa → tiền biến mất.
→ Cần **transaction** (chương 11). Với COW: làm cả 3 trên cây trong RAM, **đổi meta
page một lần ở cuối**.

## Câu hỏi tự kiểm tra

1. Đổi city của một người phải sửa những key nào?
2. Vì sao index không unique phải nhét primary key vào key?
3. `(city, name)` với `WHERE city='HN' AND name > 'B'`: seek từ đâu, dừng ở đâu?
