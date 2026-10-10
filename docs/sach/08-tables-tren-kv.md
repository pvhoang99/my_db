# Chương 08 — Table trên nền KV
*(Tables on KV)*

## 8.1 Mã hoá hàng thành cặp KV

### Truy vấn có index: point và range

Trong relational DB, dữ liệu được mô hình hoá thành **bảng 2 chiều** gồm hàng và
cột. Người dùng nói ra ý định bằng SQL và DB **kỳ diệu** trả về kết quả. Điều ít
kỳ diệu hơn là: tuy DB **thực thi được truy vấn bất kỳ**, nhưng **không phải truy
vấn nào cũng thực dụng** (hiệu quả & mở rộng được) trong workload OLTP. Và OLTP
**luôn đòi hỏi người dùng kiểm soát cách truy vấn được thực thi** thông qua
thiết kế schema và index cho đúng.

Việc một truy vấn có index được thực thi ra sao **rút gọn về 2 thao tác**:

1. **Point query**: tìm một hàng theo một key cho trước.
2. **Range query**: tìm các hàng theo một khoảng; duyệt kết quả theo thứ tự đã sắp xếp.

Đó là lý do **B+tree và LSM-tree được cân nhắc**, còn **hashtable thì không**.

### Primary key đóng vai "key"

Hãy xét point query trước. Để tìm một hàng, phải có cách **định danh duy nhất**
hàng đó — đó chính là **primary key**, một tập con của các cột.

```sql
create table t1 (
    k1 string,
    k2 int,
    v1 string,
    v2 string,
    primary key (k1, k2)
);
```

Theo trực giác, **các cột primary key đi vào "key"**, còn **phần còn lại đi vào "value"**.

|  | key | value |
|---|---|---|
| `t1` | `k1, k2` | `v1, v2` |

Một số DB cho phép bảng **không có primary key**; cái chúng làm là **tự thêm một
primary key ẩn, sinh tự động**.

### Secondary index như những bảng riêng

Ngoài primary key, một bảng có thể được đánh index theo **nhiều cách khác nhau**.
Việc này được giải bằng **một lớp gián tiếp nữa**: các **secondary index**.

```sql
create table t1 (
    k1 string,
    k2 int,
    v1 string,
    v2 string,
    primary key (k1, k2),
    index idx1 (v1),
    index idx2 (v2, v1)
);
```

Về mặt logic, **mỗi index giống như một bảng riêng**:

```sql
create table idx1 (
    -- key được đánh index (v1)
    v1 string,
    -- primary key (k1, k2)
    k1 string,
    k2 int
);
create table idx2 (
    -- key được đánh index (v2, v1)
    v2 string,
    v1 string,
    -- primary key (k1, k2)
    k1 string,
    k2 int
);
```

Tức là **thêm một key phụ để tìm ra định danh duy nhất của hàng** (primary key).

|  | key | value |
|---|---|---|
| `t1` | `k1, k2` | `v1, v2` |
| `idx1` | `v1` | `k1, k2` |
| `idx2` | `v2, v1` | `k1, k2` |

**Primary key cũng là một index**, nhưng có thêm **ràng buộc unique**.

### Cách thay thế: row ID sinh tự động

Một số DB dùng **ID sinh tự động** làm primary key **"thật"**, đối lập với primary
key do người dùng chọn. Trong trường hợp này, **không còn phân biệt primary và
secondary**; primary key của người dùng **cũng chỉ là một lớp gián tiếp**.

|  | key | value |
|---|---|---|
| `t1` | `ID` | `k1, k2, v1, v2` |
| primary key | `k1, k2` | `ID` |
| `idx1` | `v1` | `ID` |
| `idx2` | `v2, v1` | `ID` |

Lợi thế là **ID sinh tự động có thể là một số nguyên nhỏ, độ rộng cố định**,
trong khi primary key của người dùng có thể dài tuỳ ý. Điều này nghĩa là:

- Với key dạng ID, **internal node chứa được nhiều key hơn** → **cây thấp hơn**.
- **Secondary index nhỏ hơn** vì chúng không phải nhân bản primary key của người dùng.

## 8.2 Schema của database

### Tiền tố bảng (table prefix)

Một DB có thể chứa **nhiều bảng và index**. Ta sẽ **thêm một tiền tố sinh tự động
vào đầu các key** để tất cả **dùng chung một B+tree duy nhất**. Cách này ít việc
hơn là phải duy trì nhiều cây.

|  | key | value |
|---|---|---|
| `table1` | `prefix1 + columns…` | `columns…` |
| `table2` | `prefix2 + columns…` | `columns…` |
| `index1` | `prefix3 + columns…` | `columns…` |

Tiền tố là **số nguyên 32-bit tự tăng**. Bạn cũng có thể dùng **tên bảng** thay
thế, với nhược điểm là nó **có thể dài tuỳ ý**.

### Kiểu dữ liệu

Một lợi thế của relational DB so với KV là **chúng hỗ trợ nhiều kiểu dữ liệu hơn**.
Để phản ánh điều này, ta sẽ hỗ trợ **2 kiểu**: chuỗi và số nguyên.

```go
const (
    TYPE_BYTES = 1 // chuỗi (byte tuỳ ý)
    TYPE_INT64 = 2 // số nguyên có dấu 64-bit
)

// một ô trong bảng
type Value struct {
    Type uint32 // tagged union
    I64  int64
    Str  []byte
}
```

Ô `Value` là một **tagged union** của một kiểu cụ thể.

### Record

`Record` đại diện cho một **danh sách tên cột và giá trị**.

```go
// một hàng trong bảng
type Record struct {
    Cols []string
    Vals []Value
}

func (rec *Record) AddStr(col string, val []byte) *Record {
    rec.Cols = append(rec.Cols, col)
    rec.Vals = append(rec.Vals, Value{Type: TYPE_BYTES, Str: val})
    return rec
}
func (rec *Record) AddInt64(col string, val int64) *Record
func (rec *Record) Get(col string) *Value
```

### Schema

Chương này ta **chỉ xét primary key**, để dành index cho sau.

```go
type TableDef struct {
    // do người dùng định nghĩa
    Name  string
    Types []uint32 // kiểu của các cột
    Cols  []string // tên các cột
    PKeys int      // `PKeys` cột đầu tiên là primary key
    // tiền tố key B-tree được gán tự động cho từng bảng
    Prefix uint32
}
```

### Bảng nội bộ

**Lưu schema của bảng ở đâu?** Vì ta đang code một DB, ta **biết cách lưu đồ đạc**;
ta sẽ lưu chúng trong một **bảng nội bộ được định nghĩa sẵn**.

```go
var TDEF_TABLE = &TableDef{
    Prefix: 2,
    Name:   "@table",
    Types:  []uint32{TYPE_BYTES, TYPE_BYTES},
    Cols:   []string{"name", "def"},
    PKeys:  1,
}
```

Cột `def` là `TableDef` đã được **serialize thành JSON**. Tương đương với:

```sql
create table `@table` (
    `name` string, -- tên bảng
    `def`  string, -- schema
    primary key (`name`)
);
```

Ta cũng cần giữ thêm vài thông tin, chẳng hạn **bộ đếm tự tăng** để sinh tiền tố
bảng. Hãy định nghĩa thêm một bảng nội bộ nữa cho việc này.

```go
var TDEF_META = &TableDef{
    Prefix: 1,
    Name:   "@meta",
    Types:  []uint32{TYPE_BYTES, TYPE_BYTES},
    Cols:   []string{"key", "val"},
    PKeys:  1,
}
```

## 8.3 Get, update, insert, delete, create

### Giao diện point query và update

Giao diện đọc và ghi **một hàng**:

```go
func (db *DB) Get(table string, rec *Record)    (bool, error)
func (db *DB) Insert(table string, rec Record)  (bool, error)
func (db *DB) Update(table string, rec Record)  (bool, error)
func (db *DB) Upsert(table string, rec Record)  (bool, error)
func (db *DB) Delete(table string, rec Record)  (bool, error)
```

`DB` là một lớp bọc quanh `KV`:

```go
type DB struct {
    Path string
    kv   KV
}
```

### Truy vấn theo primary key

Tham số `rec` **vừa là primary key đầu vào, vừa là hàng kết quả đầu ra**.

```go
// lấy một hàng theo primary key
func dbGet(db *DB, tdef *TableDef, rec *Record) (bool, error) {
    // 1. sắp xếp lại các cột đầu vào theo đúng schema
    values, err := checkRecord(tdef, *rec, tdef.PKeys)
    if err != nil {
        return false, err
    }
    // 2. mã hoá primary key
    key := encodeKey(nil, tdef.Prefix, values[:tdef.PKeys])
    // 3. truy vấn KV store
    val, ok := db.kv.Get(key)
    if !ok {
        return false, nil
    }
    // 4. giải mã value thành các cột
    for i := tdef.PKeys; i < len(tdef.Cols); i++ {
        values[i].Type = tdef.Types[i]
    }
    decodeValues(val, values[tdef.PKeys:])
    rec.Cols = tdef.Cols
    rec.Vals = values
    return true, nil
}
```

Code xử lý cột chỉ là việc tẻ nhạt, ta bỏ qua.

```go
// sắp xếp lại một record và kiểm tra cột bị thiếu.
// n == tdef.PKeys:      record đúng bằng một primary key
// n == len(tdef.Cols):  record chứa đủ mọi cột
func checkRecord(tdef *TableDef, rec Record, n int) ([]Value, error)
```

Bước tiếp theo là **mã hoá và giải mã**, có thể dùng bất kỳ cơ chế serialize nào.

```go
// mã hoá các cột thành "key" của KV
func encodeKey(out []byte, prefix uint32, vals []Value) []byte
// giải mã các cột từ "value" của KV
func decodeValues(in []byte, out []Value)
```

### Đọc schema

Giao diện hướng người dùng tham chiếu bảng **theo tên**, nên ta phải **lấy schema
của nó trước**.

```go
// lấy một hàng theo primary key
func (db *DB) Get(table string, rec *Record) (bool, error) {
    tdef := getTableDef(db, table)
    if tdef == nil {
        return false, fmt.Errorf("table not found: %s", table)
    }
    return dbGet(db, tdef, rec)
}
```

Việc đó chỉ là **một truy vấn vào bảng nội bộ `@table`** chứa các `TableDef`
mã hoá JSON.

```go
func getTableDef(db *DB, name string) *TableDef {
    rec := (&Record{}).AddStr("name", []byte(name))
    ok, err := dbGet(db, TDEF_TABLE, rec)
    assert(err == nil)
    if !ok {
        return nil
    }
    tdef := &TableDef{}
    err = json.Unmarshal(rec.Get("def").Str, tdef)
    assert(err == nil)
    return tdef
}
```

Ta **có thể cache schema trong bộ nhớ** để giảm số truy vấn, vì không ứng dụng
tỉnh táo nào lại cần tới số lượng bảng khổng lồ.

### Insert hoặc update một hàng

Có **3 câu lệnh update trong SQL**, khác nhau ở cách chúng đối xử với hàng đã tồn tại:

- **`INSERT`** chỉ thêm hàng mới (định danh bằng primary key).
- **`UPDATE`** chỉ sửa hàng đã có.
- **`UPSERT`** thêm hàng mới **hoặc** sửa hàng đã có.

> *(Ghi chú: `UPSERT` là đặc thù của PostgreSQL. Trong MySQL nó là
> `ON DUPLICATE KEY UPDATE`. Trong SQLite là `INSERT OR REPLACE`.)*

Việc này được cài bằng cách **mở rộng `BTree.Insert` với một cờ mode**.

```go
// các mode update
const (
    MODE_UPSERT      = 0 // thêm mới hoặc thay thế
    MODE_UPDATE_ONLY = 1 // chỉ update key đã có
    MODE_INSERT_ONLY = 2 // chỉ thêm key mới
)

type UpdateReq struct {
    tree *BTree
    // đầu ra
    Added bool // đã thêm một key mới
    // đầu vào
    Key  []byte
    Val  []byte
    Mode int
}

func (tree *BTree) Update(req *UpdateReq)
```

Hàm update ở đây **chỉ xử lý một hàng hoàn chỉnh**. **Update từng phần**
(đọc–sửa–ghi) được cài ở **tầng cao hơn** (ngôn ngữ truy vấn).

```go
func dbUpdate(db *DB, tdef *TableDef, rec Record, mode int) (bool, error) {
    values, err := checkRecord(tdef, rec, len(tdef.Cols))
    if err != nil {
        return false, err
    }
    key := encodeKey(nil, tdef.Prefix, values[:tdef.PKeys])
    val := encodeValues(nil, values[tdef.PKeys:])
    return db.kv.Update(key, val, mode)
}
```

### Tạo bảng

Quy trình tạo bảng khá là nhàm chán:

1. Đọc `@table` để kiểm tra **tên trùng**.
2. Đọc **bộ đếm tiền tố bảng** từ `@meta`.
3. **Tăng và update** bộ đếm tiền tố bảng trong `@meta`.
4. **Chèn schema** vào `@table`.

```go
func (db *DB) TableNew(tdef *TableDef) error
```

> ⚠️ Quy trình này **update 2 key**, nên ở đây ta **đang mất tính atomic**.
> Chuyện này sẽ được sửa sau khi ta thêm transaction.

## 8.4 Kết luận về table trên nền KV

Table trên nền KV **về cơ bản không khác gì**; nó chỉ là **thêm vài bước
serialize dữ liệu và giữ schema**. Tuy nhiên công việc chưa xong. Các bước tiếp theo:

- **Range query.**
- **Secondary index.**

---

## 📌 Đối chiếu với PostgreSQL *(ghi chú thêm, không có trong sách)*

- **Bảng nội bộ `@table`/`@meta`** của sách chính là ý tưởng **system catalog**
  của Postgres: `pg_class`, `pg_attribute`, `pg_index`… **Catalog của Postgres
  cũng là bảng thật**, truy vấn được bằng SQL — đúng tinh thần "DB thì biết cách
  lưu đồ đạc".
- Sách nhét **mọi bảng vào một B+tree duy nhất** phân biệt bằng tiền tố; Postgres
  cho **mỗi bảng và mỗi index một file riêng** (`relfilenode`).
- Sách dùng **clustered index** (primary key chứa luôn dữ liệu, secondary index
  trỏ về primary key) — giống **MySQL/InnoDB**. **Postgres thì không**: mọi index,
  kể cả primary key, đều trỏ vào **heap qua TID**. Hệ quả: ở Postgres secondary
  index **không rẻ hơn** primary index, còn ở sách/InnoDB thì secondary index
  phải **tra cứu 2 lần**.
- `UPSERT` mà sách nhắc chính là `INSERT ... ON CONFLICT ... DO UPDATE` của Postgres.
- Chỗ sách thừa nhận **"tạo bảng update 2 key nên mất atomicity"**: Postgres có
  **DDL transaction** — `CREATE TABLE` nằm trong transaction và **rollback được**,
  điều mà MySQL không làm được.
