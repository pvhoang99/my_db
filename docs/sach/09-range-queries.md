# Chương 09 — Range Query
*(Range Queries)*

## 9.1 Iterator của B+tree

### Giao diện iterator

Các thao tác cơ bản của range query là **seek** và **iterate**. Một vị trí trong
B+tree được biểu diễn bằng **iterator có trạng thái** `BIter`.

```go
// tìm vị trí gần nhất nhỏ hơn hoặc bằng key đầu vào
func (tree *BTree) SeekLE(key []byte) *BIter

// lấy cặp KV hiện tại
func (iter *BIter) Deref() ([]byte, []byte)

// điều kiện tiên quyết của Deref()
func (iter *BIter) Valid() bool

// di chuyển lùi và tiến
func (iter *BIter) Prev()
func (iter *BIter) Next()
```

Ví dụ, truy vấn `a <= key` trông như thế này:

```go
for iter := tree.SeekLE(key); iter.Valid(); iter.Prev() {
    k, v := iter.Deref()
    // ...
}
```

### Điều hướng trong cây

Cần biết **vị trí của key hiện tại** để tìm được key anh em của nó bên trong một
node. Và nếu key anh em **nằm ở node anh em**, ta phải **quay ngược lên node cha**.
Vì **ta không dùng con trỏ tới cha**, nên ta cần **toàn bộ đường đi từ root
xuống leaf**.

```go
type BIter struct {
    tree *BTree
    path []BNode  // từ root xuống leaf
    pos  []uint16 // index vào từng node
}
```

Di chuyển iterator **giống như phép nhớ khi tăng một con số từng chữ số một**.

```go
func (iter *BIter) Next() {
    iterNext(iter, len(iter.path)-1)
}

func iterNext(iter *BIter, level int) {
    if iter.pos[level]+1 < iter.path[level].nkeys() {
        iter.pos[level]++      // di chuyển trong chính node này
    } else if level > 0 {
        iterNext(iter, level-1) // chuyển sang node anh em
    } else {
        iter.pos[len(iter.pos)-1]++ // đã vượt qua key cuối cùng
        return
    }

    if level+1 < len(iter.pos) { // cập nhật node con
        node := iter.path[level]
        kid := BNode(iter.tree.get(node.getPtr(iter.pos[level])))
        iter.path[level+1] = kid
        iter.pos[level+1] = 0
    }
}
```

### Seek tới một key

Seek tới một key **giống như một point query**, chỉ khác là **có ghi lại đường đi**.

```go
// tìm vị trí gần nhất nhỏ hơn hoặc bằng key đầu vào
func (tree *BTree) SeekLE(key []byte) *BIter {
    iter := &BIter{tree: tree}
    for ptr := tree.root; ptr != 0; {
        node := tree.get(ptr)
        idx := nodeLookupLE(node, key)
        iter.path = append(iter.path, node)
        iter.pos = append(iter.pos, idx)
        ptr = node.getPtr(idx)
    }
    return iter
}
```

`nodeLookupLE` là cho **nhỏ hơn hoặc bằng**; bạn sẽ cần cả **các toán tử khác** nữa.

```go
const (
    CMP_GE = +3 // >=
    CMP_GT = +2 // >
    CMP_LT = -2 // <
    CMP_LE = -3 // <=
)

func (tree *BTree) Seek(key []byte, cmp int) *BIter
```

## 9.2 Mã hoá bảo toàn thứ tự

### Sắp xếp dữ liệu tuỳ ý dưới dạng chuỗi byte

B+tree của ta làm việc với **key là chuỗi byte tuỳ ý**. Nhưng một cột có thể
thuộc kiểu khác, ví dụ **số**, và key có thể gồm **nhiều cột**. Để hỗ trợ range
query, **key đã serialize phải so sánh được theo đúng kiểu dữ liệu của nó**.

Cách hiển nhiên là **thay `bytes.Compare` bằng một callback** giải mã rồi so sánh
key theo schema của bảng.

Cách khác là **chọn một định dạng serialize đặc biệt, sao cho chuỗi byte kết quả
phản ánh đúng thứ tự sắp xếp**. Đây là **lối tắt ta sẽ chọn**.

### Số

Hãy bắt đầu với bài toán đơn giản: **mã hoá số nguyên không dấu** thế nào để so
sánh được bằng `bytes.Compare`? `bytes.Compare` làm việc **từng byte một** cho
tới khi gặp khác biệt. Nên **byte đầu tiên là quan trọng nhất** trong phép so sánh.
Nếu ta **đặt các bit có trọng số cao lên trước**, chúng sẽ so sánh được theo byte.
**Đó chính là số big-endian.**

```
0x0000000000000001 -> 00 00 00 00 00 00 00 01
0x0000000000000002 -> 00 00 00 00 00 00 00 02
...
0x00000000000000ff -> 00 00 00 00 00 00 00 ff
0x0000000000000100 -> 00 00 00 00 00 00 01 00
```

Tiếp theo xét **số nguyên có dấu**, được biểu diễn bằng **bù hai (two's complement)**.
Trong biểu diễn bù hai, **nửa trên của miền giá trị không dấu đơn giản là bị dịch
sang thành giá trị âm**. Để đảm bảo thứ tự đúng, ta **hoán đổi nửa dương với nửa
âm** — mà việc đó chỉ là **lật bit có trọng số cao nhất (bit dấu)**.

```go
var buf [8]byte
u := uint64(v.I64) + (1 << 63)        // lật bit dấu
binary.BigEndian.PutUint64(buf[:], u) // big endian
```

Vài ví dụ:

| int64 | Chuỗi byte đã mã hoá |
|---|---|
| `MinInt64` | `00 00 00 00 00 00 00 00` |
| `-2` | `7f ff ff ff ff ff ff fe` |
| `-1` | `7f ff ff ff ff ff ff ff` |
| `0` | `80 00 00 00 00 00 00 00` |
| `1` | `80 00 00 00 00 00 00 01` |
| `MaxInt64` | `ff ff ff ff ff ff ff ff` |

Ý tưởng tổng quát là:

- **Sắp xếp bit sao cho bit trọng số cao hơn đi trước** (big-endian).
- **Ánh xạ lại bit thành số nguyên không dấu theo đúng thứ tự.**

> **Bài tập cho người đọc:** áp dụng điều này cho **số thực** (dấu + phần định trị
> + số mũ).

### Chuỗi

Key có thể gồm **nhiều cột**. Nhưng `bytes.Compare` chỉ làm việc được với **một
cột chuỗi duy nhất**, vì nó cần biết **độ dài**. Ta **không thể chỉ nối các cột
chuỗi lại với nhau**, vì làm thế sinh ra **nhập nhằng**. Ví dụ: `("a", "bc")` và
`("ab", "c")` sẽ ra cùng một kết quả.

Có **2 cách** mã hoá chuỗi kèm độ dài:

1. **Đặt độ dài lên trước** — cách này đòi hỏi phải giải mã.
2. **Đặt một ký tự phân cách ở cuối**, chẳng hạn byte null.

Với cách 2, ví dụ trên được mã hoá thành `"a\x00bc\x00"` và `"ab\x00c\x00"`.

Vấn đề của ký tự phân cách là **dữ liệu đầu vào không được chứa chính nó**. Việc
này giải bằng cách **escape ký tự phân cách**. Ta sẽ dùng byte `0x01` làm **byte
escape**, và **bản thân byte escape cũng phải được escape**. Nên cần **2 phép biến đổi**:

```
00 -> 01 01
01 -> 01 02
```

Lưu ý rằng **các chuỗi escape này vẫn bảo toàn thứ tự sắp xếp**.

### Tuple

So sánh nhiều cột (**tuple**) được làm **từng cột một cho tới khi gặp khác biệt**.
Việc này **giống hệt so sánh chuỗi**, chỉ khác là mỗi phần tử là một **giá trị có
kiểu** thay vì một byte. Ta chỉ cần **nối chuỗi byte đã mã hoá của từng cột lại**,
miễn là **không có nhập nhằng**.

## 9.3 Range query

`Scanner` là một **lớp bọc quanh iterator của B+tree**. Nó **giải mã các cặp KV
thành hàng**.

```go
// còn nằm trong khoảng hay không?
func (sc *Scanner) Valid() bool

// di chuyển iterator B-tree bên dưới
func (sc *Scanner) Next()

// lấy hàng hiện tại
func (sc *Scanner) Deref(rec *Record)

func (db *DB) Scan(table string, req *Scanner) error
```

Đầu vào là **một khoảng của primary key**.

```go
type Scanner struct {
    // khoảng, từ Key1 tới Key2
    Cmp1 int // CMP_??
    Cmp2 int
    Key1 Record
    Key2 Record
    // ...
}
```

Với **khoảng mở một đầu**, chỉ cần đặt `Key2` thành **giá trị lớn nhất/nhỏ nhất**.

## 9.4 Những gì ta đã học

- **Iterator của B+tree.**
- **Mã hoá bảo toàn thứ tự.**

Bước tiếp theo là thêm **secondary index** — mà chúng cũng chỉ là **những bảng phụ**.

---

## 📌 Đối chiếu với PostgreSQL *(ghi chú thêm, không có trong sách)*

- Sách **không dùng con trỏ tới cha** mà **lưu cả đường đi root→leaf** trong
  iterator. Postgres cũng vậy — `BTScanOpaque` giữ stack đường đi, vì con trỏ cha
  rất khó duy trì đúng khi split xảy ra đồng thời.
- Khác biệt lớn: leaf page của Postgres có **right-link** (con trỏ sang page kế
  tiếp cùng tầng, kỹ thuật **B-link tree**). Nhờ vậy scan tuần tự **chỉ cần đi
  ngang**, không phải quay ngược lên cha như `iterNext` của sách. Đây cũng là thứ
  cho phép split chạy song song với scan.
- **Mã hoá bảo toàn thứ tự** là chỗ Postgres làm **hoàn toàn khác**: Postgres
  **không** mã hoá key thành chuỗi byte so sánh được. Thay vào đó nó dùng
  **operator class** (`pg_opclass`) — mỗi kiểu dữ liệu đăng ký một **hàm so sánh**,
  và B-tree gọi hàm đó. Chính là "cách hiển nhiên" mà sách đã bỏ qua.
- Hệ quả thực tế của thiết kế Postgres: so sánh **chuỗi phụ thuộc vào collation**
  (`LC_COLLATE`) — đổi collation là phải `REINDEX`. Cách mã hoá byte của sách
  tránh được vấn đề này nhưng **mất khả năng sắp xếp theo ngôn ngữ**.
