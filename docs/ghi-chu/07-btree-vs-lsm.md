# B+tree vs LSM-tree — hai cách sửa "mảng sắp xếp" (chương 2)

> Nối tiếp [04-bst-vs-btree.md](04-bst-vs-btree.md). Liên quan sách **chương 2**.

## 1. Câu hỏi của chương 2

Chương 1 kết thúc: log ghi an toàn nhưng **tìm thì phải đọc hết**.

> **Tìm nhanh bằng cách nào, mà vẫn sửa được?**

| Loại truy vấn | Cần |
|---|---|
| Scan | Không cần index, luôn `O(N)` |
| Point query (`id = 5`) | Tìm 1 key |
| Range query (`id BETWEEN 5 AND 9`) | **Seek** tới key đầu, rồi **iterate** |

Point query = range query không iterate → chỉ cần **một cấu trúc có sắp xếp**.
Hashtable bị loại vì **không có thứ tự**.

## 2. Mảng sắp xếp: tìm nhanh, sửa chậm → hai lối thoát

```
                  mang sap xep (sua O(N))
                  /                     \
   Che thanh nhieu mang nho        Dem thay doi vao mang nho,
   long nhau                       day thi MERGE vao mang lon
        |                                    |
     B+tree                               LSM-tree
```

## 3. Vì sao binary search trên đĩa vẫn thua B+tree

Cả hai đều so sánh ~30 lần với 1 tỷ key. Khác nhau ở **số page phải đọc**:

```
Binary search tren mang 1 ty key (~100 key/page):
  buoc 1: page #5.000.000   buoc 2: page #2.500.000   ...  moi buoc mot page xa
  -> ~23 lan doc dia (chi ~7 buoc cuoi roi vao cung mot page)

B+tree (~200 key/node):
  doc 1 page -> binary search 200 key TRONG RAM -> xuong tang duoi
  -> ~4 lan doc dia
```

> **Trên đĩa, chi phí tính bằng số page đọc, không phải số phép so sánh.**
> `O(log N)` giấu mất cơ số: `log₂` và `log₂₀₀` chênh ~8 lần.

## 4. LSM-tree — hiểu qua ví dụ danh bạ

**Danh bạ in sẵn** (không tẩy được) + **xấp giấy note** dán bìa.

```
An doi so:     Giay note: An: 0999        Danh ba in: An: 0901, Binh: 0902
Tra An:        xem giay note TRUOC -> 0999 (moi nhat)
Xoa Binh:      KHONG xe trang duoc -> ghi note "Binh: DA XOA"  (tombstone)
Cuoi nam:      in lai danh ba moi theo giay note; Binh bien mat han  (merge)
```

| Ví dụ | LSM-tree |
|---|---|
| Giấy note | Tầng 1 (MemTable + log), nhỏ, ghi được |
| Danh bạ in | Tầng 2, 3… (SSTable) trên đĩa, **không bao giờ sửa** |
| Tra giấy note trước | **Tầng mới hơn được ưu tiên** |
| "Bình: ĐÃ XOÁ" | **Tombstone** |
| In lại danh bạ | **Merge / compaction** — thu hồi luôn chỗ trống |

### Hai cái bẫy

- **Xoá mà không ghi tombstone** → key **sống lại**, vì tầng dưới vẫn còn bản cũ.
- **Merge mà quên áp dụng tombstone** → key sống lại **vĩnh viễn** (giấy note đã bỏ).
  Tombstone phải giữ tới khi chắc không tầng nào bên dưới còn bản cũ.

### Đánh đổi

- **Write amplification**: một key bị ghi lại nhiều lần khi đi qua các tầng
- **Đọc phải hỏi nhiều tầng** → dùng **Bloom filter** để bỏ qua tầng chắc chắn không có

## 5. LSM chính là chương 1 làm ở quy mô lớn

| Chương 1 | LSM-tree |
|---|---|
| Không ghi đè; ghi file mới rồi **rename** | Merge ra **file mới** rồi thay file cũ |
| Log append-only + checksum | Tầng 1 = **log** trên đĩa + **MemTable** trong RAM |
| "Log phải kết hợp với index" | Đây chính là chỗ kết hợp |
| "Log không thu hồi được chỗ" | Compaction thu hồi |

→ LSM **giải sẵn** vấn đề của chương 1. **B+tree thì chưa** — vẫn phải sửa node tại
chỗ. Chương 3 giải bằng **copy-on-write** ([08](08-btree-split-va-copy-on-write.md)).

## 6. Abstract được B+tree / LSM không?

**Được, ở mức giao diện KV:**

```go
type KV interface {
    Get(key []byte) ([]byte, bool, error)
    Set(key, val []byte) error
    Del(key []byte) error
    Scan(start []byte) Iterator
}
```

Ngoài đời: MySQL (InnoDB = B+tree, MyRocks = LSM), MongoDB WiredTiger, CockroachDB/TiDB.

**Ba chỗ rò rỉ:**
1. **Transaction** — cần thêm `Batch` (nhiều key atomic) và `Snapshot`
2. **Hiệu năng khác nhau** — planner không thể mù hoàn toàn về engine
3. **Crash recovery** — mỗi engine một cơ chế

⚠️ **Postgres không abstract ở mức KV** — heap, index, buffer pool, WAL gắn chặt vào
page 8KB. Đây là điểm phân nhánh "bám sách hay bám Postgres" của project.
Xem [11-postgres-index-thuc-te.md](11-postgres-index-thuc-te.md).

## Câu hỏi tự kiểm tra

1. Key `a` có ở cả tầng 1 và tầng 3 — đọc tầng nào?
2. Xoá key trong LSM mà chỉ xoá khỏi tầng 1 thì sao?
3. Vì sao LSM "an toàn sẵn" với crash còn B+tree thì không?
