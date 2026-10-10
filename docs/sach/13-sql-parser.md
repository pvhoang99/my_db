# Chương 13 — SQL Parser

> SQL dễ phân tích đối với máy tính mà vẫn trông giống tiếng Anh.

## 13.1 Cú pháp, parser và interpreter

### Biểu diễn ngôn ngữ máy tính dưới dạng cây

Một ngôn ngữ truy vấn là **một chuỗi được phân tích thành cấu trúc cây** để xử lý tiếp.

**Ví dụ 1:** `SELECT ... FROM foo WHERE a > b AND a < c`

```
            select
          /   |    \
    columns table  condition
      ...    foo      and
                     /   \
                    >     <
                   / \   / \
                  a   b a   c
```

**Ví dụ 2:** biểu thức `a + b * c`

```
    +
   / \
  a   *
     / \
    b   c
```

SQL chỉ là **một cú pháp cụ thể**; có những lựa chọn dễ hơn, chẳng hạn
[PRQL](https://prql-lang.org/) dựa trên pipeline, hoặc thậm chí chỉ cần
[S-expression](https://en.wikipedia.org/wiki/S-expression).

S-expression chỉ là **các dấu ngoặc lồng nhau** — cú pháp đơn giản nhất cho cấu
trúc cây tuỳ ý. **Bạn có thể bỏ qua chương này nếu chọn S-expression**, nhưng
SQL cũng không khó hơn mấy, vì **mọi thứ đều xử lý được chỉ bằng đệ quy từ trên
xuống**. Bài học của chương này cũng áp dụng cho **hầu hết các ngôn ngữ máy tính**.

### Đánh giá bằng cách thăm các node của cây

Cả `SELECT` lẫn `UPDATE` đều có thể chứa **biểu thức số học trên các cột**, và
chúng được parse thành cây như ví dụ trên. **Mỗi node của cây là một toán tử**,
và **các subtree của nó là các toán hạng**. Để đánh giá một node, **trước hết
đánh giá các subtree của nó**.

```python
# mã giả
def eval(node):
    if is_binary_operator(node):
        left, right = eval(node.left), eval(node.right)
        return node.operator(left, right)
    elif is_value(node):
        return node.value
    ...
```

> Nhìn lại, **đây chính là lý do cây lại quan trọng**: bởi vì **cây biểu diễn
> thứ tự đánh giá**. Một ngôn ngữ lập trình còn có luồng điều khiển, biến, v.v.,
> nhưng một khi bạn đã biểu diễn nó bằng cây thì **phần còn lại là hiển nhiên**.

## 13.2 Đặc tả ngôn ngữ truy vấn

### Câu lệnh

Không hẳn là SQL, chỉ là **trông giống SQL**.

```sql
create table table_name (
    a type1,
    b type2,
    ...
    index (c, b, a),
    index (d, e, f),
    primary key (a, b),
);

select expr... from table_name conditions limit x, y;
insert into table_name (cols...) values (a, b, c)...;
delete from table_name conditions limit x, y;
update table_name set a = expr, b = expr, ... conditions limit x, y;
```

### Điều kiện

Một DB SQL sẽ **tự chọn index dựa trên mệnh đề `WHERE`** nếu có thể, và/hoặc
**lấy hàng về rồi lọc** nếu điều kiện không được index phủ hết. Việc này là
**tự động, người dùng không điều khiển trực tiếp được**.

Ở đây ta **làm khác SQL**: thay vì `WHERE`, ta dùng **hai mệnh đề riêng biệt**
cho **điều kiện đánh index** và **điều kiện lọc**.

**1. Mệnh đề `INDEX BY`** chọn index và điều khiển thứ tự sắp xếp.

```sql
-- một trong 3 dạng
select expr... from table_name index by a = 1;
select expr... from table_name index by a > 1;
select expr... from table_name index by a > 1 and a < 5;

-- truy vấn cuối theo thứ tự giảm dần
select expr... from table_name index by a < 5 and a > 1;
```

**2. Mệnh đề `FILTER`** sau đó lọc các hàng.

```sql
-- điều kiện lọc có thể tuỳ ý
select expr... from table_name index by condition1 filter condition2;
select expr... from table_name filter condition2;
```

**Cả hai đều tuỳ chọn.** Và **primary key được chọn nếu thiếu `INDEX BY`**.

> **Tại sao làm khác SQL?** Workload OLTP thường kỳ vọng **hiệu năng dự đoán được**;
> một thay đổi đột ngột trong query plan là **mối nguy trong môi trường production**.
> Đó là lý do ta làm cho việc **chọn index trở nên tường minh**, để DB khỏi phải đoán.

### Biểu thức

Một biểu thức là một trong những thứ sau:

- **tên cột**,
- **giá trị hằng** như số hoặc chuỗi,
- **toán tử hai ngôi hoặc một ngôi**,
- **tuple**.

```
a OR b
a AND b
NOT a
a = b, a < b, ...   -- so sánh
a + b, a - b
a * b, a / b
-a
```

Chúng được biểu diễn thành **node của cây**:

```go
type QLNode struct {
    Type uint32   // tagged union
    I64  int64
    Str  []byte
    Kids []QLNode // các toán hạng
}
```

Các toán tử khác nhau có **độ ưu tiên (precedence) khác nhau**, như liệt kê ở trên.
Sự phức tạp này **được né tránh trong những ngữ pháp đơn giản hơn** như
S-expression. Nhưng **độ ưu tiên toán tử xử lý được bằng đệ quy đơn giản**, như
bạn sắp thấy.

## 13.3 Recursive descent (đệ quy đi xuống)

### Cấu trúc node của cây

Mỗi câu lệnh được **chia thành những phần nhỏ hơn**, bao gồm cả node biểu thức
`QLNode` — nên chúng là **cây của các thành phần**.

```go
// câu lệnh: select, update, delete
type QLSelect struct {
    QLScan
    Names  []string // expr AS name
    Output []QLNode
}

type QLUpdate struct {
    QLScan
    Names  []string
    Values []QLNode
}

type QLDelete struct {
    QLScan
}

// cấu trúc chung cho các câu lệnh: `INDEX BY`, `FILTER`, `LIMIT`
type QLScan struct {
    Table  string // tên bảng
    Key1   QLNode // index by
    Key2   QLNode
    Filter QLNode // biểu thức lọc
    Offset int64  // limit
    Limit  int64
}
```

### Chia đầu vào thành những phần nhỏ hơn

**Mọi việc parse đều là từ trên xuống.** Phần trên cùng là một **câu lệnh**; trước
hết ta **xác định loại của nó**, rồi **giao việc cho hàm cụ thể**.

```go
func pStmt(p *Parser) (r interface{}) {
    switch {
    case pKeyword(p, "create", "table"):
        r = pCreateTable(p)
    case pKeyword(p, "select"):
        r = pSelect(p)
    // ...
    }
    return r
}
```

`pKeyword` **khớp và tiêu thụ các từ khoá** từ đầu vào để xác định phần tiếp theo.
Hãy nhìn `pSelect` — **3 phần của nó được tiêu thụ bởi 3 hàm**.

```go
func pSelect(p *Parser) *QLSelect {
    stmt := QLSelect{}
    pSelectExprList(p, &stmt) // SELECT xxx
    pExpect(p, "from", "expect `FROM` table")
    stmt.Table = pMustSym(p)  // FROM table
    pScan(p, &stmt.QLScan)    // INDEX BY xxx FILTER yyy LIMIT zzz
    return &stmt
}
```

`pSelectExprList` là **danh sách biểu thức phân cách bằng dấu phẩy**. Mỗi phần tử
được giao cho `pSelectExpr`. **Dấu phẩy quyết định danh sách kết thúc ở đâu.**

```go
func pSelectExprList(p *Parser, node *QLSelect) {
    pSelectExpr(p, node)
    for pKeyword(p, ",") {
        pSelectExpr(p, node)
    }
}
```

`pScan` là phần cuối của `SELECT`. Nó lại **chia tiếp thành 3 phần nhỏ hơn**.

```go
func pScan(p *Parser, node *QLScan) {
    if pKeyword(p, "index", "by") {
        pIndexBy(p, node)
    }
    if pKeyword(p, "filter") {
        pExprOr(p, &node.Filter)
    }
    node.Offset, node.Limit = 0, math.MaxInt64
    if pKeyword(p, "limit") {
        pLimit(p, node)
    }
}
```

Chưa cần nhìn vào từng hàm, **ta đã nắm được ý tưởng của việc parse**:

1. **Chia đầu vào thành những phần ngày càng nhỏ hơn**, cho tới khi nó kết thúc
   ở một **toán tử**, một **tên**, hoặc một **giá trị hằng**.
2. **Xác định phần tiếp theo bằng cách nhìn vào từ khoá kế tiếp.**

**Bảng 1: `SELECT` được phân rã thành những phần ngày càng nhỏ.**

| `SELECT a, b` | `FROM` | `foo` | `INDEX BY x` | `FILTER y` | `LIMIT z` |
|---|---|---|---|---|---|
| `pSelectExprList` | `pExpect` | `pMustSym` | `pScan` | | |
| `pSelectExpr` | | | `pIndexBy` | `pExprOr` | `pLimit` |
| `pExprOr` | | | … | … | `pNum` |

### Biến toán tử trung tố thành cây nhị phân

`pExprOr` parse một biểu thức tuỳ ý. Biến `1+2*3-4` thành cây **đúng theo độ ưu
tiên toán tử** là chuyện không hiển nhiên, vì nó chỉ là **một danh sách số và
toán tử đan xen**. Nên hãy bắt đầu từ bài toán đơn giản hơn: **chỉ có toán tử `+`**.

```
term
term + term
term + term + term + ...
```

Biểu thức `left + right` được biểu diễn thành:

```
    +
   / \
left  right
```

Subtree bên trái **cũng có thể là một biểu thức**, nên `LL + LR + R` trở thành:

```
      +
     / \
    +   R
   / \
  LL  LR
```

Ta thêm được bao nhiêu hạng tử tuỳ ý mà **nó vẫn là cây nhị phân**. Mã giả:

```python
def parse_terms():
    node = parse_column()
    while consume('+'):
        right = parse_column()
        node = QLNode(type='+', kids=[node, right])
    return node
```

Điều này được mô tả bằng **một luật đơn giản**:

```
expr := expr + term
expr := term
```

Luật con bên trái `expr` **có thể bung ra thành chính nó**, nhưng luật con bên
phải `term` là **phần đáy không bung ra được nữa**.

### Độ ưu tiên toán tử bằng đệ quy

Bài toán tiếp theo: **thêm toán tử `*`**. Nó **ưu tiên cao hơn**, nên `term` giờ
được bung ra bằng một luật tương tự, và **phần đáy bây giờ là `factor`**.

```
expr := expr + term
expr := term
term := term * factor
term := factor
```

Mã giả:

```python
def parse_terms():
    node = parse_factors()
    while consume('+'):
        right = parse_factors()
        node = QLNode(type='+', kids=[node, right])
    return node

def parse_factors():
    node = parse_column()
    while consume('*'):
        right = parse_column()
        node = QLNode(type='*', kids=[node, right])
    return node
```

**Bảng 2: hình dung việc bung 2 luật** cho `a + b × c - d`

| | `a` | `+` | `b` | `×` | `c` | `-` | `d` |
|---|---|---|---|---|---|---|---|
| 1 | `term` | | | | | | `term` |
| 2 | `factor` | | `factor` | | `factor` | | `factor` |

Toán tử `OR` có **độ ưu tiên thấp nhất**, nên `pExprOr` là **hàm trên cùng** khi
parse một biểu thức. Nó gọi `pExprAnd` để xử lý mức ưu tiên tiếp theo, cứ thế đi
xuống tới mức ưu tiên cao nhất là `pExprUnop`, rồi gọi `pExprAtom` để parse phần
đáy (một **tên** hoặc một **giá trị hằng**).

```
a OR b          -- pExprOr
a AND b         -- pExprAnd
NOT a           -- pExprNot
a = b, a < b    -- pExprCmp
a + b, a - b    -- pExprAdd
a * b, a / b    -- pExprMul
-a              -- pExprUnop
```

Cái này gọi là **recursive descent**. Nhìn lại thì **nó chỉ là chia để trị**,
trong đó bước **"chia" chỉ là kiểm tra từ khoá kế tiếp**.

---

## 📌 Đối chiếu với PostgreSQL *(ghi chú thêm, không có trong sách)*

- Postgres **không viết parser bằng tay** như sách. Nó dùng **`flex`** cho lexer
  (`scan.l`) và **`bison`** cho parser (`gram.y`) — tức **LALR bottom-up**, không
  phải recursive descent. Nhưng **AST sinh ra thì cùng bản chất**.
- Chỗ sách **làm khác SQL quan trọng nhất**: sách bắt người dùng **chọn index
  tường minh** bằng `INDEX BY`. **Postgres thì tự động** — có **planner dựa trên
  chi phí** đọc thống kê từ `pg_statistic` rồi tự chọn. Lo lắng của sách về
  "hiệu năng không dự đoán được" là **có thật**: đó là lý do Postgres có
  `EXPLAIN`, `ANALYZE`, và các tham số như `enable_seqscan = off` để ép plan.
- `FILTER` của sách tương ứng với **`Filter:`** mà bạn thấy trong output của
  `EXPLAIN` Postgres — điều kiện **không** dùng được index nên phải lọc sau khi
  lấy hàng về. Còn `INDEX BY` tương ứng **`Index Cond:`**.
- Postgres tách **parse → analyze → rewrite → plan → execute** thành 5 giai đoạn.
  Sách gộp parse + execute làm một (interpreter trực tiếp trên AST), bỏ qua hẳn
  khâu **tối ưu hoá**.
