# Chương 10 — Secondary Index
*(Secondary Indexes)*

## 10.1 Secondary index như những key phụ

### Schema của bảng

Như đã nhắc ở chương 08, **secondary index chỉ là những cặp KV phụ chứa primary
key**. Mỗi index được phân biệt bằng **một tiền tố key** trong B+tree.

```go
type TableDef struct {
    // do người dùng định nghĩa
    Name    string
    Types   []uint32   // kiểu của các cột
    Cols    []string   // tên các cột
    Indexes [][]string // index đầu tiên chính là primary key
    // tiền tố key B-tree gán tự động cho từng bảng và từng index
    Prefixes []uint32
}
```

**Index đầu tiên được dùng làm primary key**, vì primary key cũng là một index.

### Cấu trúc KV

Với secondary index, ta **có thể** đặt primary key vào phần **value** của B+tree,
rồi dùng nó để tìm hàng đầy đủ. Tuy nhiên, khác với primary key,
**secondary index không có ràng buộc unique**, nên **có thể có key B+tree trùng nhau**.

Thay vì sửa B+tree để hỗ trợ key trùng, ta có thể **thêm primary key vào chính
key của B+tree** để làm nó duy nhất, và **để value rỗng**.

```sql
create table t1 (
    k1 string,
    k2 int,
    v1 string,
    v2 string,
    primary key (k1, k2),
    index idx1 (v1),
    index idx2 (v2, k2)
);
```

|  | key | value |
|---|---|---|
| `t1` | `prefix1, k1, k2` | `v1, v2` |
| `idx1` | `prefix2, v1, k1, k2` | *(rỗng)* |
| `idx2` | `prefix3, v2, k2, k1` | *(rỗng)* |

## 10.2 Sử dụng secondary index

### Chọn index bằng cách khớp cột

Để làm range query, ta phải **chọn index nào khớp với key của truy vấn**. Index
được chọn lưu trong kiểu `Scanner` để `Scanner.Deref()` dùng được.

```go
type Scanner struct {
    // khoảng, từ Key1 tới Key2
    Cmp1 int // CMP_??
    Cmp2 int
    Key1 Record
    Key2 Record
    // nội bộ
    db     *DB
    tdef   *TableDef
    index  int    // dùng index nào?
    iter   *BIter // iterator B-tree bên dưới
    keyEnd []byte // Key2 đã mã hoá
}
```

Một index có thể gồm **nhiều cột**. Ví dụ, index `(a, b)` phục vụ được truy vấn
`(a, b) > (1, 2)`. Nó **cũng phục vụ được** truy vấn `a > 1`, vì điều đó tương
đương với `(a, b) > (1, +∞)`. **Việc chọn index chỉ đơn giản là khớp các cột.**

```go
func dbScan(db *DB, tdef *TableDef, req *Scanner) error {
    // ...
    isCovered := func(index []string) bool {
        key := req.Key1.Cols
        return len(index) >= len(key) && slices.Equal(index[:len(key)], key)
    }
    req.index = slices.IndexFunc(tdef.Indexes, isCovered)
    // ...
}
```

### Mã hoá cột bị thiếu thành vô cực

Trong ví dụ trên, truy vấn `a > 1` với index `(a, b)` **chỉ dùng 1 trong 2 cột**,
nên ta phải **mã hoá phần còn lại thành vô cực**.

| Truy vấn đầu vào | Dùng index thành |
|---|---|
| `a > 1` | `(a, b) > (1, +∞)` |
| `a ≤ 1` | `(a, b) < (1, +∞)` |
| `a ≥ 1` | `(a, b) > (1, −∞)` |
| `a < 1` | `(a, b) < (1, −∞)` |

Việc này làm được bằng cách **sửa lại phép mã hoá bảo toàn thứ tự**. Trước hết,
ta chọn `"\xff"` làm **`+∞`** và `""` làm **`−∞`**. Vì **không cột nào được mã
hoá thành chuỗi rỗng**, nên với trường hợp `−∞` ta chỉ việc **bỏ qua các cột bị
thiếu**. Còn với trường hợp `+∞`, ta **thêm một tag vào đầu mỗi cột đã mã hoá**
để chúng **không bao giờ bắt đầu bằng `"\xff"`**.

```go
// mã hoá bảo toàn thứ tự
func encodeValues(out []byte, vals []Value) []byte {
    for _, v := range vals {
        out = append(out, byte(v.Type)) // *thêm vào*: không bắt đầu bằng 0xff
        switch v.Type {
        case TYPE_INT64:
            var buf [8]byte
            u := uint64(v.I64) + (1 << 63)        // lật bit dấu
            binary.BigEndian.PutUint64(buf[:], u) // big endian
            out = append(out, buf[:]...)
        case TYPE_BYTES:
            out = append(out, escapeString(v.Str)...)
            out = append(out, 0) // kết thúc bằng null
        default:
            panic("what?")
        }
    }
    return out
}
```

Ta **thêm mã kiểu của cột vào đầu làm tag**. Việc này cũng **làm debug dễ hơn**,
vì giờ ta giải mã được đồ đạc chỉ bằng cách nhìn hexdump.

Đây là một bước phụ nhỏ để hỗ trợ **range query trên các cột tiền tố**.

```go
// dành cho primary key và index
func encodeKey(out []byte, prefix uint32, vals []Value) []byte {
    // tiền tố bảng 4 byte
    var buf [4]byte
    binary.BigEndian.PutUint32(buf[:], prefix)
    out = append(out, buf[:]...)
    // key đã mã hoá bảo toàn thứ tự
    out = encodeValues(out, vals)
    return out
}

// dành cho khoảng đầu vào, có thể chỉ là tiền tố của key index.
func encodeKeyPartial(
    out []byte, prefix uint32, vals []Value, cmp int,
) []byte {
    out = encodeKey(out, prefix, vals)
    if cmp == CMP_GT || cmp == CMP_LE { // mã hoá cột thiếu thành vô cực
        out = append(out, 0xff) // +vô cực, không bao giờ với tới được
    } // ngược lại: -vô cực chính là chuỗi rỗng
    return out
}
```

## 10.3 Duy trì secondary index

### Đồng bộ với dữ liệu chính

Khi có secondary index, **một lần update có thể liên quan tới nhiều key B+tree**.
Khi một hàng thay đổi, ta phải **xoá các key index cũ và chèn các key mới**.
Để làm được, giao diện B+tree được **mở rộng để trả về giá trị cũ**.

```go
type UpdateReq struct {
    tree *BTree
    // đầu ra
    Added   bool   // đã thêm một key mới
    Updated bool   // đã thêm key mới HOẶC key cũ bị thay đổi
    Old     []byte // giá trị trước khi update
    // đầu vào
    Key  []byte
    Val  []byte
    Mode int
}
```

Dùng thông tin mới này:

```go
func dbUpdate(db *DB, tdef *TableDef, rec Record, mode int) (bool, error) {
    // ...
    // chèn hàng
    req := UpdateReq{Key: key, Val: val, Mode: mode}
    if _, err = db.kv.Update(&req); err != nil {
        return false, err
    }
    // duy trì các secondary index
    if req.Updated && !req.Added {
        // dùng `req.Old` để xoá các key index cũ ...
    }
    if req.Updated {
        // thêm các key index mới ...
    }
    return req.Updated, nil
}
```

### Atomicity khi update nhiều key

> ⚠️ **Atomicity không có tính kết hợp (composable)!**

Ta **mất tính atomic ngay khi có nhiều key tham gia**, **kể cả khi từng thao tác
KV riêng lẻ đều atomic**. Nếu DB crash hoặc có lỗi xảy ra giữa lúc update một
secondary index, nó **phải quay về trạng thái trước đó**.

Đạt được điều này chỉ với `get`, `set`, `del` là **rất khó** — đó chính là lý do
**giao diện KV đơn giản rất hạn chế**. Bước tiếp theo của ta là một
**giao diện KV có transaction**, cho phép thao tác atomic trên nhiều key, hoặc
thậm chí cho phép nhiều reader đồng thời.

## 10.4 Tóm tắt về table và index trên nền KV

- Hàng và cột biểu diễn dưới dạng KV.
- **Range query.**
  - Iterator của B+tree.
  - Mã hoá bảo toàn thứ tự.
- **Secondary index.**
  - Chọn index.
  - **Cần có giao diện transaction.**

---

## 📌 Đối chiếu với PostgreSQL *(ghi chú thêm, không có trong sách)*

- Mẹo **"nhét primary key vào key của index để làm nó unique"** chính là cách
  **InnoDB** làm. **Postgres không cần** mẹo này: index của Postgres **cho phép
  key trùng** một cách tự nhiên, vì leaf chứa `(key, TID)` và TID vốn đã duy nhất.
- **Chọn index bằng cách khớp cột tiền tố** là đúng nguyên lý của Postgres:
  index `(a, b)` dùng được cho `WHERE a = 1` nhưng **không** dùng được cho
  `WHERE b = 1`. Khác biệt là Postgres có **planner dựa trên chi phí** —
  nó ước lượng số hàng từ `pg_statistic` rồi **cân nhắc** giữa index scan,
  bitmap scan và seq scan, chứ không chỉ "khớp được thì dùng" như sách.
- Mẹo **mã hoá cột thiếu thành `+∞`/`−∞`** tương ứng với cách Postgres dựng
  **scan key** trong `_bt_first()`.
- Câu **"atomicity không composable"** là bài học cốt lõi, và đúng với Postgres:
  Postgres giữ index đồng bộ với heap **bên trong cùng một transaction**, ghi WAL
  cho cả heap lẫn index. Nếu lệch nhau thì sinh ra **index corruption** —
  thứ mà `amcheck` dùng để phát hiện.
- Sách phải **xoá key index cũ rồi chèn key mới** ở mỗi lần update. Postgres có
  tối ưu **HOT update**: nếu không cột nào được đánh index bị đổi và còn chỗ
  trong cùng page, nó **không đụng tới index** chút nào.
