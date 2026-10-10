# Chương 14 — Ngôn ngữ truy vấn
*(Query Language)*

## 14.1 Đánh giá biểu thức

Cả `SELECT` lẫn `UPDATE` đều chứa **biểu thức trên các cột** cần được đánh giá.

```go
type QLEvalContex struct {
    env Record // giá trị của hàng đầu vào
    out Value  // đầu ra
    err error
}
```

Đánh giá một cây thì **hiển nhiên đúng như đã bàn ở chương trước**.

```go
func qlEval(ctx *QLEvalContex, node QLNode) {
    switch node.Type {
    // tham chiếu tới một cột
    case QL_SYM:
        if v := ctx.env.Get(string(node.Str)); v != nil {
            ctx.out = *v
        } else {
            qlErr(ctx, "unknown column: %s", node.Str)
        }
    // một giá trị hằng
    case QL_I64, QL_STR:
        ctx.out = node.Value
    // toán tử
    case QL_NEG:
        qlEval(ctx, node.Kids[0])
        if ctx.out.Type == TYPE_INT64 {
            ctx.out.I64 = -ctx.out.I64
        } else {
            qlErr(ctx, "QL_NEG type error")
        }
    // ...
    }
}
```

`INSERT` chứa **biểu thức trên hằng số**, được đánh giá với một **`env` rỗng**.

## 14.2 Range query

### Thiết lập một range query

Cả `SELECT`, `UPDATE` và `DELETE` đều làm được range query; **khác biệt nằm ở
việc làm gì với kết quả**. `QLScan` là **phần chung** biểu diễn một range query.

```go
type QLScan struct {
    Table  string // tên bảng
    Key1   QLNode // index by
    Key2   QLNode
    Filter QLNode // lọc
    Offset int64  // limit
    Limit  int64
}
```

Nó có **3 pha**: `INDEX BY`, `LIMIT`, và `FILTER`. `Scanner` cài đặt phần `INDEX BY`.

```go
func qlScanInit(req *QLScan, sc *Scanner) (err error) {
    // chuyển `QLNode` thành `Record` và `CMP_??`
    if sc.Key1, sc.Cmp1, err = qlEvalScanKey(req.Key1); err != nil {
        return err
    }
    if sc.Key2, sc.Cmp2, err = qlEvalScanKey(req.Key2); err != nil {
        return err
    }

    switch { // xử lý đặc biệt khi `Key1` và `Key2` không cùng có mặt
    case req.Key1.Type == 0 && req.Key2.Type == 0: // không có `INDEX BY`
        sc.Cmp1, sc.Cmp2 = CMP_GE, CMP_LE // quét toàn bảng
    case req.Key1.Type == QL_CMP_EQ && req.Key2.Type == 0:
        // bằng theo một tiền tố: INDEX BY key = val
        sc.Key2 = sc.Key1
        sc.Cmp1, sc.Cmp2 = CMP_GE, CMP_LE
    case req.Key1.Type != 0 && req.Key2.Type == 0:
        // khoảng mở một đầu: INDEX BY key > val
        if sc.Cmp1 > 0 {
            sc.Cmp2 = CMP_LE // so sánh với một tuple độ dài 0
        } else {
            sc.Cmp2 = CMP_GE
        }
    }
    return nil
}
```

### Nhìn lại phép mã hoá vô cực

`INDEX BY` nhận **1 trong 3 dạng** đã định nghĩa ở chương trước:

1. **`a > start AND a < end`** — một khoảng `(start, end)`.
2. **`a > s`** — một khoảng mở một đầu `(s, +∞)`.
3. **`a = p`** — một **tiền tố** của index.

Giả sử index là `(a, b)`. Truy vấn dùng **tiền tố của index** đã được xử lý sẵn
bởi phép mã hoá key ở chương 10. Vậy nên:

- **`a = p`** tương đương `a >= p AND a <= p`, mã hoá thành
  `(a, b) ≥ (p, −∞)` và `(a, b) ≤ (p, +∞)`.
- **`a > s`** tương đương `a > s AND () <= ()`, mã hoá thành
  `(a, b) > (s, −∞)` và `(a, ) < (+∞, )`.

Vì có dùng tới **tuple rỗng `()`**, nên `Key1` và `Key2` giờ **có thể có tập cột
khác nhau** — ta phải **sửa lại việc chọn index** để cho phép điều đó.

```go
func dbScan(tx *DBTX, tdef *TableDef, req *Scanner) error {
    // ...
    covered := func(key []string, index []string) bool {
        return len(index) >= len(key) && slices.Equal(index[:len(key)], key)
    }
    req.index = slices.IndexFunc(tdef.Indexes, func(index []string) bool {
        return covered(req.Key1.Cols, index) && covered(req.Key2.Cols, index)
    })
    // ...
}
```

## 14.3 Iterator cho kết quả

### Iterator chồng lên iterator

Hai pha tiếp theo là `LIMIT` và `FILTER`. **Kết quả được tiêu thụ từ các iterator.**

```go
type RecordIter interface {
    Valid() bool
    Next()
    Deref(*Record) error
}
```

> **Tại sao dùng iterator thay vì một mảng kết quả?** Bởi vì một DB có thể phải
> làm việc với **dữ liệu lớn hơn bộ nhớ**. Iterator **không đòi hỏi kết quả phải
> nằm sẵn trong bộ nhớ cùng một lúc**; nó thậm chí có thể **stream kết quả ngay
> khi chúng được tạo ra**.

**Bảng 1: chuỗi iterator cho một câu `SELECT`.**

| Iterator | Đầu ra | Vai trò |
|---|---|---|
| `BIter` | KV | Duyệt qua B+tree. |
| `KVIter` | KV | Gộp snapshot với các update cục bộ. |
| `Scanner` | Row | Giải mã record và đi theo secondary index. |
| `qlScanIter` | Row | Offset, limit, và lọc hàng. |
| `qlSelectIter` | Row | Đánh giá biểu thức trong `SELECT`. |

### Biến đổi dữ liệu bằng iterator

**Một iterator nhận một iterator khác làm đầu vào** để biến đổi một luồng phần tử.
Đây là một **pattern lập trình rất hữu ích**.

```go
type qlSelectIter struct {
    iter  RecordIter // đầu vào
    names []string
    exprs []QLNode
}

func (iter *qlSelectIter) Valid() bool {
    return iter.iter.Valid()
}
func (iter *qlSelectIter) Next() {
    iter.iter.Next()
}
func (iter *qlSelectIter) Deref(rec *Record) error {
    if err := iter.iter.Deref(rec); err != nil {
        return err
    }
    vals, err := qlEvelMulti(*rec, iter.exprs)
    if err != nil {
        return err
    }
    *rec = Record{iter.names, vals}
    return nil
}
```

`qlScanIter` thì phức tạp hơn một chút, vì **cần một ít sổ sách cho việc lọc**.

```go
type qlScanIter struct {
    // đầu vào
    req *QLScan
    sc  Scanner
    // trạng thái
    idx int64
    end bool
    // phần tử đầu ra đã cache
    rec Record
    err error
}
```

## 14.4 Kết luận và các bước tiếp theo

Ta đã có **nhiều giao diện** tới một DB bền vững, có transaction:

1. **Một KV** nhúng được vào ứng dụng.
2. **Một relational DB** nhúng được vào ứng dụng.
3. **Một ngôn ngữ truy vấn kiểu SQL** cho relational DB đó.

Mà **không cần thêm chức năng mới**, ta có thể tạo một **giao thức mạng** để cho
phép DB chạy ở **tiến trình hoặc máy khác**. Lập trình mạng trong Go thì ở mức
cao và dễ, nhưng bạn luôn có thể học thêm với tinh thần "from scratch" —
tìm trong cuốn [**Build Your Own Redis**](https://build-your-own.org/redis/).

Và vì ta đã có **parser và interpreter cơ bản**, ta có thể tiến tới **trình biên
dịch**. Bạn có thể tạo một **ngôn ngữ lập trình** và **biên dịch ra mã máy** thay
vì chỉ thông dịch. Xem cuốn
[**From Source Code To Machine Code**](https://build-your-own.org/compiler/).

---

## 📌 Đối chiếu với PostgreSQL *(ghi chú thêm, không có trong sách)*

- **Chuỗi iterator** của sách **chính là Volcano / Iterator model** mà Postgres
  dùng. Mỗi node trong executor của Postgres có `ExecProcNode()` — tương đương
  `Next()` + `Deref()` của sách. Khi bạn chạy `EXPLAIN`, cái cây bạn thấy chính
  là **chuỗi iterator này**.
- Ánh xạ trực tiếp: `Scanner` ↔ **Index Scan**, `qlScanIter` ↔ **Filter + Limit**,
  `qlSelectIter` ↔ **Result / Projection**.
- Lý do dùng iterator ("dữ liệu lớn hơn bộ nhớ, stream được") **đúng y nguyên với
  Postgres** — đó là lý do `LIMIT 10` trên bảng tỉ hàng vẫn trả về tức thì nếu
  có index phù hợp.
- **Thứ sách thiếu so với Postgres**: không có **JOIN** (Postgres có nested loop,
  hash join, merge join), không có **aggregation / GROUP BY**, không có **sort**
  tràn ra disk, và **không có optimizer** — vì `INDEX BY` đã để người dùng tự chọn.
- Bước tiếp theo mà sách gợi ý (**giao thức mạng**) chính là **M7** trong
  `ROADMAP.md` của ta: PostgreSQL **wire protocol v3**, để `psql` thật kết nối được.
