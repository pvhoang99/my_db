# Chương 04 — Node của B+Tree và phép Insert
*(B+Tree Node and Insertion)*

## 4.1 Thiết kế node của B+tree

### Ta sẽ làm gì

Bước lớn đầu tiên chỉ là **cấu trúc dữ liệu B+tree**; những mối quan tâm khác của
DB sẽ bàn ở các chương sau. Ta làm **từ dưới lên**:

1. Thiết kế **định dạng node** chứa đủ mọi thông tin cần thiết.
2. Thao tác trên node theo kiểu **copy-on-write** (insert và delete key).
3. **Split** và **merge** node.
4. **Insert** và **delete** trên cây.

### Định dạng node

Mọi node của B+tree đều **cùng kích thước**, để sau này dùng được free list.
Dù lúc này ta chưa đụng tới dữ liệu trên disk, vẫn cần một định dạng node cụ thể,
vì nó quyết định **kích thước node tính bằng byte** và **khi nào phải split node**.

Một node gồm:

1. Một **header có kích thước cố định**, chứa:
   - **Loại node** (leaf hay internal).
   - **Số lượng key**.
2. Một **danh sách con trỏ** tới các node con (dành cho internal node).
3. Một **danh sách các cặp KV**.
4. Một **danh sách offset** tới các KV, dùng để **binary search**.

```
| type | nkeys |  pointers   |   offsets   | key-values | unused |
|  2B  |  2B   | nkeys * 8B  | nkeys * 2B  |    ...     |        |
```

Đây là định dạng của **mỗi cặp KV** — độ dài đi trước, dữ liệu đi sau:

```
| klen | vlen | key | val |
|  2B  |  2B  | ... | ... |
```

### Những đơn giản hoá và giới hạn

Mục tiêu của ta là học cái cơ bản, **không phải tạo ra một DB thực thụ**, nên có
vài đơn giản hoá.

**Dùng chung một định dạng cho cả leaf node và internal node.** Cái này lãng phí
một chút chỗ: leaf node không cần con trỏ, còn internal node không cần value.

Một internal node có `n` nhánh thì chứa `n` key, **mỗi key là bản sao của key nhỏ
nhất trong subtree tương ứng**. Tuy nhiên, như bạn sẽ thấy trong các tài liệu
B-tree khác, **`n` nhánh chỉ cần `n − 1` key**. Key dư ra giúp việc hình dung dễ hơn.

Ta đặt **kích thước node là 4K**, tức kích thước page điển hình của OS. Tuy nhiên,
key và value có thể lớn tuỳ ý, thậm chí vượt quá một node. Lẽ ra phải có cách lưu
KV lớn **bên ngoài** node, hoặc cho phép node có kích thước thay đổi. Vấn đề này
giải được, nhưng **không phải vấn đề cốt lõi**, nên ta bỏ qua bằng cách **giới hạn
kích thước KV** sao cho chúng luôn vừa trong một node.

```go
const HEADER = 4

const BTREE_PAGE_SIZE    = 4096
const BTREE_MAX_KEY_SIZE = 1000
const BTREE_MAX_VAL_SIZE = 3000

func init() {
    node1max := HEADER + 8 + 2 + 4 + BTREE_MAX_KEY_SIZE + BTREE_MAX_VAL_SIZE
    assert(node1max <= BTREE_PAGE_SIZE) // KV lớn nhất
}
```

Giới hạn kích thước key cũng đảm bảo rằng **một internal node luôn chứa được ít
nhất 2 key**.

### Kiểu dữ liệu trong bộ nhớ

Trong code của ta, **một node chỉ là một mảng byte** được diễn giải theo định dạng
trên. Chuyển dữ liệu từ bộ nhớ xuống disk sẽ **đơn giản hơn vì không cần bước
serialize**.

```go
type BNode []byte // có thể dump thẳng xuống disk
```

### Tách cấu trúc dữ liệu khỏi IO

Cả cấu trúc trong bộ nhớ lẫn trên disk đều cần **cấp phát / thu hồi chỗ**. Ta có
thể trừu tượng hoá việc này bằng **callback** — đây chính là **ranh giới giữa cấu
trúc dữ liệu và phần còn lại của DB**.

```go
type BTree struct {
    // con trỏ (một số hiệu page khác 0)
    root uint64
    // callback để quản lý page trên disk
    get func(uint64) []byte // giải tham chiếu một con trỏ
    new func([]byte) uint64 // cấp phát một page mới
    del func(uint64)        // thu hồi một page
}
```

Với B+tree trên disk, **file database là một mảng page (node)**, tham chiếu bằng
**số hiệu page (con trỏ)**. Ta sẽ cài đặt các callback này như sau:

- `get` đọc một page từ disk.
- `new` cấp phát và ghi một page mới (**copy-on-write**).
- `del` thu hồi một page.

Ta có thể dùng **callback giả (mock)** để test cấu trúc dữ liệu hoàn toàn trong
bộ nhớ, không cần phần còn lại của DB.

## 4.2 Giải mã định dạng node

Vì node chỉ là một mảng byte, ta định nghĩa vài hàm phụ trợ để truy cập nó.

```
| type | nkeys |  pointers   |   offsets   | key-values | unused |
|  2B  |  2B   | nkeys * 8B  | nkeys * 2B  |    ...     |        |

| klen | vlen | key | val |
|  2B  |  2B  | ... | ... |
```

### Header

```go
const (
    BNODE_NODE = 1 // internal node, không có value
    BNODE_LEAF = 2 // leaf node, có value
)

func (node BNode) btype() uint16 {
    return binary.LittleEndian.Uint16(node[0:2])
}
func (node BNode) nkeys() uint16 {
    return binary.LittleEndian.Uint16(node[2:4])
}
func (node BNode) setHeader(btype uint16, nkeys uint16) {
    binary.LittleEndian.PutUint16(node[0:2], btype)
    binary.LittleEndian.PutUint16(node[2:4], nkeys)
}
```

### Con trỏ tới node con

```go
func (node BNode) getPtr(idx uint16) uint64 {
    assert(idx < node.nkeys())
    pos := HEADER + 8*idx
    return binary.LittleEndian.Uint64(node[pos:])
}
func (node BNode) setPtr(idx uint16, val uint64)
```

### Danh sách offset và các cặp KV

Định dạng này **xếp mọi thứ sát nhau liên tiếp**. Muốn tìm cặp KV thứ `n` thì phải
đọc lần lượt từng cặp một. Để dễ hơn, ta **thêm một danh sách offset** cho phép
định vị cặp KV thứ `n` trong `O(1)`. Nhờ đó cũng **binary search được bên trong
một node**.

**Mỗi offset là vị trí kết thúc của cặp KV**, tính tương đối so với đầu của cặp KV
đầu tiên. Offset bắt đầu của cặp KV đầu tiên hiển nhiên là 0, nên ta lưu offset
**kết thúc** thay vào đó — đó cũng chính là offset bắt đầu của cặp KV kế tiếp.

```go
func offsetPos(node BNode, idx uint16) uint16 {
    assert(1 <= idx && idx <= node.nkeys())
    return HEADER + 8*node.nkeys() + 2*(idx-1)
}
func (node BNode) getOffset(idx uint16) uint16 {
    if idx == 0 {
        return 0
    }
    return binary.LittleEndian.Uint16(node[offsetPos(node, idx):])
}
func (node BNode) setOffset(idx uint16, offset uint16)
```

`kvPos` trả về vị trí của cặp KV thứ `n` **so với toàn bộ node**:

```go
func (node BNode) kvPos(idx uint16) uint16 {
    assert(idx <= node.nkeys())
    return HEADER + 8*node.nkeys() + 2*node.nkeys() + node.getOffset(idx)
}
func (node BNode) getKey(idx uint16) []byte {
    assert(idx < node.nkeys())
    pos := node.kvPos(idx)
    klen := binary.LittleEndian.Uint16(node[pos:])
    return node[pos+4:][:klen]
}
func (node BNode) getVal(idx uint16) []byte
```

Nó cũng tiện lợi trả về luôn **kích thước node** (phần đã dùng) bằng một phép tra
cứu lệch một đơn vị:

```go
// kích thước node tính bằng byte
func (node BNode) nbytes() uint16 {
    return node.kvPos(node.nkeys())
}
```

### Tra cứu KV bên trong một node

Thao tác **"seek"** được dùng cho **cả range query lẫn point query** — nên về bản
chất chúng là một.

```go
// trả về node con đầu tiên có khoảng giao với key. (kid[i] <= key)
// TODO: binary search
func nodeLookupLE(node BNode, key []byte) uint16 {
    nkeys := node.nkeys()
    found := uint16(0)
    // key đầu tiên là bản sao từ node cha,
    // nên nó luôn nhỏ hơn hoặc bằng key đang tìm.
    for i := uint16(1); i < nkeys; i++ {
        cmp := bytes.Compare(node.getKey(i), key)
        if cmp <= 0 {
            found = i
        }
        if cmp >= 0 {
            break
        }
    }
    return found
}
```

Hàm này tên là `nodeLookupLE` vì nó dùng toán tử **Less-than-or-Equal** (nhỏ hơn
hoặc bằng). Với point query thì lẽ ra nên dùng toán tử bằng — đó là bước ta có
thể thêm sau.

## 4.3 Update node của B+tree

### Insert vào leaf node

Xét việc insert một key vào leaf node. Bước 1 là dùng `nodeLookupLE` để tìm **vị
trí chèn**. Sau đó **copy mọi thứ sang một node mới, kèm thêm key mới**.
**Đó chính là copy-on-write.**

```go
// thêm một key mới vào leaf node
func leafInsert(
    new BNode, old BNode, idx uint16,
    key []byte, val []byte,
) {
    new.setHeader(BNODE_LEAF, old.nkeys()+1) // dựng header
    nodeAppendRange(new, old, 0, 0, idx)
    nodeAppendKV(new, idx, 0, key, val)
    nodeAppendRange(new, old, idx+1, idx, old.nkeys()-idx)
}
```

### Các hàm copy node

`nodeAppendRange` copy một dãy KV, còn `nodeAppendKV` copy một cặp KV. **Phải làm
theo đúng thứ tự**, vì các hàm này dựa vào offset của phần tử trước đó.

```go
// copy một cặp KV vào vị trí
func nodeAppendKV(new BNode, idx uint16, ptr uint64, key []byte, val []byte) {
    // con trỏ
    new.setPtr(idx, ptr)
    // KV
    pos := new.kvPos(idx)
    binary.LittleEndian.PutUint16(new[pos+0:], uint16(len(key)))
    binary.LittleEndian.PutUint16(new[pos+2:], uint16(len(val)))
    copy(new[pos+4:], key)
    copy(new[pos+4+uint16(len(key)):], val)
    // offset của key kế tiếp
    new.setOffset(idx+1, new.getOffset(idx)+4+uint16((len(key)+len(val))))
}

// copy nhiều cặp KV từ node cũ vào vị trí
func nodeAppendRange(
    new BNode, old BNode,
    dstNew uint16, srcOld uint16, n uint16,
)
```

### Update internal node

Với internal node, **liên kết tới node con luôn được update theo cơ chế
copy-on-write**, và liên kết đó **có thể biến thành nhiều liên kết** nếu node con
bị split.

```go
// thay một liên kết bằng một hoặc nhiều liên kết
func nodeReplaceKidN(
    tree *BTree, new BNode, old BNode, idx uint16,
    kids ...BNode,
) {
    inc := uint16(len(kids))
    new.setHeader(BNODE_NODE, old.nkeys()+inc-1)
    nodeAppendRange(new, old, 0, 0, idx)
    for i, node := range kids {
        nodeAppendKV(new, idx+uint16(i), tree.new(node), node.getKey(0), nil)
        //                  ^vị trí        ^con trỏ        ^key           ^val
    }
    nodeAppendRange(new, old, idx+inc, idx+1, old.nkeys()-(idx+1))
}
```

Lưu ý là callback **`tree.new` được dùng để cấp phát các node con**.

## 4.4 Split node của B+tree

Do những giới hạn kích thước ta đã áp đặt, **một node chứa được ít nhất 1 cặp KV**.
Trong trường hợp xấu nhất, một node quá khổ sẽ bị **split thành 3 node**, với một
KV lớn nằm ở giữa. Vậy nên ta **có thể phải split 2 lần**.

```go
// split một node quá khổ thành 2, sao cho node thứ 2 luôn vừa một page
func nodeSplit2(left BNode, right BNode, old BNode) {
    // code bị lược bỏ trong sách...
}

// split một node nếu nó quá lớn. kết quả là 1~3 node.
func nodeSplit3(old BNode) (uint16, [3]BNode) {
    if old.nbytes() <= BTREE_PAGE_SIZE {
        old = old[:BTREE_PAGE_SIZE]
        return 1, [3]BNode{old} // không split
    }
    left := BNode(make([]byte, 2*BTREE_PAGE_SIZE)) // có thể bị split tiếp
    right := BNode(make([]byte, BTREE_PAGE_SIZE))
    nodeSplit2(left, right, old)
    if left.nbytes() <= BTREE_PAGE_SIZE {
        left = left[:BTREE_PAGE_SIZE]
        return 2, [3]BNode{left, right} // 2 node
    }
    leftleft := BNode(make([]byte, BTREE_PAGE_SIZE))
    middle := BNode(make([]byte, BTREE_PAGE_SIZE))
    nodeSplit2(leftleft, middle, left)
    assert(leftleft.nbytes() <= BTREE_PAGE_SIZE)
    return 3, [3]BNode{leftleft, middle, right} // 3 node
}
```

Lưu ý: các node trả về **được cấp phát từ bộ nhớ**; chúng chỉ là dữ liệu tạm thời
cho tới khi `nodeReplaceKidN` thực sự cấp phát chúng.

## 4.5 Insert vào B+tree

Ta đã cài đặt 3 thao tác trên node:

- `leafInsert` — update một leaf node.
- `nodeReplaceKidN` — update một internal node.
- `nodeSplit3` — split một node quá khổ.

Giờ ghép chúng lại thành một phép insert hoàn chỉnh trên B+tree, bắt đầu bằng
việc tra cứu key từ root node cho tới khi chạm leaf.

```go
// insert một KV vào node, kết quả có thể bị split.
// caller chịu trách nhiệm thu hồi node đầu vào,
// và split + cấp phát các node kết quả.
func treeInsert(tree *BTree, node BNode, key []byte, val []byte) BNode {
    // node kết quả.
    // nó được phép lớn hơn 1 page và sẽ bị split nếu vậy
    new := BNode{data: make([]byte, 2*BTREE_PAGE_SIZE)}

    // chèn key vào đâu?
    idx := nodeLookupLE(node, key)
    // hành động tuỳ theo loại node
    switch node.btype() {
    case BNODE_LEAF:
        // leaf, node.getKey(idx) <= key
        if bytes.Equal(key, node.getKey(idx)) {
            // tìm thấy key, update nó.
            leafUpdate(new, node, idx, key, val)
        } else {
            // chèn vào sau vị trí đó.
            leafInsert(new, node, idx+1, key, val)
        }
    case BNODE_NODE:
        // internal node, chèn vào một node con.
        nodeInsert(tree, new, node, idx, key, val)
    default:
        panic("bad node!")
    }
    return new
}
```

`leafUpdate` tương tự `leafInsert`; nó **update một key đã tồn tại** thay vì chèn
thêm một key trùng.

```go
// một phần của treeInsert(): chèn KV vào một internal node
func nodeInsert(
    tree *BTree, new BNode, node BNode, idx uint16,
    key []byte, val []byte,
) {
    kptr := node.getPtr(idx)
    // chèn đệ quy vào node con
    knode := treeInsert(tree, tree.get(kptr), key, val)
    // split kết quả
    nsplit, split := nodeSplit3(knode)
    // thu hồi node con cũ
    tree.del(kptr)
    // update các liên kết tới node con
    nodeReplaceKidN(tree, new, node, idx, split[:nsplit]...)
}
```

Internal node được xử lý **đệ quy**: mỗi lần gọi trả về một node đã update, và
**caller sẽ split nó nếu quá khổ**, đồng thời lo việc cấp phát/thu hồi.

## 4.6 Tiếp theo là gì?

Gần xong rồi. Ta chỉ cần thêm những thứ sau ở chương kế tiếp:

1. **Merge node** và **delete trên cây**.
2. **Giao diện mức cao.**
3. **Callback node giả** để test.

---

## 📌 Đối chiếu với PostgreSQL *(ghi chú thêm, không có trong sách)*

- Định dạng node của sách **gần như trùng** với **slotted page** của Postgres:
  header + mảng offset + vùng dữ liệu. Khác biệt: Postgres cho vùng dữ liệu
  **mọc ngược** từ cuối page lên, còn sách xếp **liên tiếp xuôi** từ trên xuống.
- `nodeLookupLE` của sách là **tìm tuyến tính** (có ghi `TODO: binary search`);
  Postgres dùng **binary search** trong `_bt_binsrch()`.
- Postgres xử lý **KV quá lớn** bằng **TOAST** (lưu ra bảng phụ, nén/chia mảnh) —
  sách bỏ qua bằng cách giới hạn kích thước KV.
- Postgres **không** dùng copy-on-write khi split: nó sửa page tại chỗ, ghi WAL
  record trước, và dùng **right-link** (kỹ thuật **Lehman-Yao / B-link tree**)
  để reader đi song song không bị lạc trong lúc split.
