# Chương 05 — Phép Delete của B+Tree và Testing
*(B+Tree Deletion and Testing)*

## 5.1 Giao diện mức cao

Ta thêm giao diện để dùng B+tree như một KV store.

```go
// chèn key mới hoặc update key đã có
func (tree *BTree) Insert(key []byte, val []byte)

// xoá một key, trả về key đó có tồn tại hay không
func (tree *BTree) Delete(key []byte) bool
```

> Hầu hết chi tiết đã được giới thiệu cùng với phép insert, nên phép delete không
> có nhiều điều mới để học. **Bỏ qua chương này nếu bạn đã nắm nguyên lý.**

### Duy trì root node

Có thêm chút việc phải làm để duy trì root node khi insert:

- **Tạo root node nếu cây đang rỗng.**
- **Thêm một root mới nếu root bị split.**

```go
func (tree *BTree) Insert(key []byte, val []byte) {
    if tree.root == 0 {
        // tạo node đầu tiên
        root := BNode(make([]byte, BTREE_PAGE_SIZE))
        root.setHeader(BNODE_LEAF, 2)
        // một key giả, làm cho cây phủ toàn bộ không gian key.
        // nhờ vậy mọi lần tra cứu đều tìm được node chứa nó.
        nodeAppendKV(root, 0, 0, nil, nil)
        nodeAppendKV(root, 1, 0, key, val)
        tree.root = tree.new(root)
        return
    }

    node := treeInsert(tree, tree.get(tree.root), key, val)
    nsplit, split := nodeSplit3(node)
    tree.del(tree.root)
    if nsplit > 1 {
        // root đã bị split, thêm một tầng mới.
        root := BNode(make([]byte, BTREE_PAGE_SIZE))
        root.setHeader(BNODE_NODE, nsplit)
        for i, knode := range split[:nsplit] {
            ptr, key := tree.new(knode), knode.getKey(0)
            nodeAppendKV(root, uint16(i), ptr, key, nil)
        }
        tree.root = tree.new(root)
    } else {
        tree.root = tree.new(split[0])
    }
}
```

### Giá trị sentinel

Có một mẹo khi tạo root đầu tiên: ta đã **chèn vào một key rỗng**. Cái này gọi là
**giá trị sentinel** (lính canh), dùng để **loại bỏ một trường hợp biên**.

Nếu xem lại hàm tra cứu `nodeLookupLE`, bạn sẽ thấy **nó không hoạt động nếu key
nằm ngoài khoảng của node**. Điều này được khắc phục bằng cách chèn một key rỗng
vào cây — đó là **key nhỏ nhất có thể** theo thứ tự sắp xếp, nhờ vậy `nodeLookupLE`
**luôn luôn tìm được một vị trí**.

## 5.2 Merge node

### Các hàm update node

Ta cần thêm vài hàm cho phép delete trên cây.

```go
// xoá một key khỏi leaf node
func leafDelete(new BNode, old BNode, idx uint16)

// merge 2 node thành 1
func nodeMerge(new BNode, left BNode, right BNode)

// thay 2 liên kết kề nhau bằng 1 liên kết
func nodeReplace2Kid(
    new BNode, old BNode, idx uint16, ptr uint64, key []byte,
)
```

### Điều kiện merge

Delete có thể dẫn tới **node rỗng**, và node rỗng có thể được merge với một
sibling nếu nó có sibling. Hàm `shouldMerge` trả về **nên merge với sibling nào**
(trái hay phải).

```go
// node con vừa update có nên được merge với một sibling không?
func shouldMerge(
    tree *BTree, node BNode,
    idx uint16, updated BNode,
) (int, BNode) {
    if updated.nbytes() > BTREE_PAGE_SIZE/4 {
        return 0, BNode{}
    }

    if idx > 0 {
        sibling := BNode(tree.get(node.getPtr(idx - 1)))
        merged := sibling.nbytes() + updated.nbytes() - HEADER
        if merged <= BTREE_PAGE_SIZE {
            return -1, sibling // trái
        }
    }
    if idx+1 < node.nkeys() {
        sibling := BNode(tree.get(node.getPtr(idx + 1)))
        merged := sibling.nbytes() + updated.nbytes() - HEADER
        if merged <= BTREE_PAGE_SIZE {
            return +1, sibling // phải
        }
    }
    return 0, BNode{}
}
```

Key đã xoá nghĩa là **chỗ trống không dùng tới bên trong node**. Trong trường hợp
xấu nhất, một cây gần như rỗng vẫn có thể giữ lại rất nhiều node. Ta cải thiện
điều này bằng cách **kích hoạt merge sớm hơn** — dùng **1/4 page làm ngưỡng**
thay vì đợi node rỗng hẳn. Đây là một **giới hạn mềm cho kích thước node tối thiểu**.

## 5.3 Phép delete trên B+tree

Tương tự insert, chỉ cần **thay split bằng merge**.

```go
// xoá một key khỏi cây
func treeDelete(tree *BTree, node BNode, key []byte) BNode

// xoá một key khỏi internal node; một phần của treeDelete()
func nodeDelete(tree *BTree, node BNode, idx uint16, key []byte) BNode {
    // đệ quy xuống node con
    kptr := node.getPtr(idx)
    updated := treeDelete(tree, tree.get(kptr), key)
    if len(updated) == 0 {
        return BNode{} // không tìm thấy
    }
    tree.del(kptr)

    new := BNode(make([]byte, BTREE_PAGE_SIZE))
    // kiểm tra xem có phải merge không
    mergeDir, sibling := shouldMerge(tree, node, idx, updated)
    switch {
    case mergeDir < 0: // trái
        merged := BNode(make([]byte, BTREE_PAGE_SIZE))
        nodeMerge(merged, sibling, updated)
        tree.del(node.getPtr(idx - 1))
        nodeReplace2Kid(new, node, idx-1, tree.new(merged), merged.getKey(0))
    case mergeDir > 0: // phải
        merged := BNode(make([]byte, BTREE_PAGE_SIZE))
        nodeMerge(merged, updated, sibling)
        tree.del(node.getPtr(idx + 1))
        nodeReplace2Kid(new, node, idx, tree.new(merged), merged.getKey(0))
    case mergeDir == 0 && updated.nkeys() == 0:
        assert(node.nkeys() == 1 && idx == 0) // 1 con rỗng nhưng không có sibling
        new.setHeader(BNODE_NODE, 0)
        // node cha cũng trở thành rỗng
    case mergeDir == 0 && updated.nkeys() > 0: // không merge
        nodeReplaceKidN(tree, new, node, idx, updated)
    }
    return new
}
```

Ngay cả khi một node trở thành rỗng, **nó vẫn có thể không được merge nếu không
có sibling nào**. Trong trường hợp đó, node rỗng **được đẩy lan lên node cha** và
sẽ được merge sau.

## 5.4 Test B+tree

Cấu trúc dữ liệu chỉ tương tác với phần còn lại của DB qua **3 callback quản lý
page**. Để test B+tree, ta có thể **mô phỏng page ngay trong bộ nhớ**.

```go
type C struct {
    tree  BTree
    ref   map[string]string // dữ liệu đối chiếu
    pages map[uint64]BNode  // page trong bộ nhớ
}

func newC() *C {
    pages := map[uint64]BNode{}
    return &C{
        tree: BTree{
            get: func(ptr uint64) []byte {
                node, ok := pages[ptr]
                assert(ok)
                return node
            },
            new: func(node []byte) uint64 {
                assert(BNode(node).nbytes() <= BTREE_PAGE_SIZE)
                ptr := uint64(uintptr(unsafe.Pointer(&node[0])))
                assert(pages[ptr] == nil)
                pages[ptr] = node
                return ptr
            },
            del: func(ptr uint64) {
                assert(pages[ptr] != nil)
                delete(pages, ptr)
            },
        },
        ref:   map[string]string{},
        pages: pages,
    }
}
```

`C.pages` là map các page đã cấp phát. Nó dùng để **kiểm tra tính hợp lệ của con
trỏ** và để đọc page. Ở đây **con trỏ thực chất là địa chỉ bộ nhớ**, và code
B+tree **không hề quan tâm** điều đó.

Để test B+tree, trước hết ta cần update nó trong nhiều kịch bản khác nhau, rồi
kiểm chứng kết quả. Việc kiểm chứng là chung cho mọi trường hợp, có **2 thứ phải
kiểm**:

1. **Cấu trúc hợp lệ.**
   - Key đã được sắp xếp.
   - Kích thước node nằm trong giới hạn.
2. **Dữ liệu khớp với bản đối chiếu.** Ta dùng một `map` để ghi lại mọi lần update.

```go
func (c *C) add(key string, val string) {
    c.tree.Insert([]byte(key), []byte(val))
    c.ref[key] = val // dữ liệu đối chiếu
}
```

Các test case **để lại làm bài tập**. Việc tiếp theo là đưa B+tree xuống disk.

---

## 📌 Đối chiếu với PostgreSQL *(ghi chú thêm, không có trong sách)*

- **Ngưỡng merge 1/4 page** của sách tương ứng khái niệm **fillfactor** và việc
  dọn page của Postgres — nhưng cơ chế rất khác.
- **Postgres gần như không merge node B-tree.** Page index rỗng được đánh dấu
  **deleted** và đưa vào **FSM (Free Space Map)** để tái sử dụng, chứ cây không
  co lại theo kiểu merge. Đó là lý do index Postgres bị **bloat** và đôi khi cần
  `REINDEX`.
- Lý do Postgres né merge: merge đòi hỏi khoá nhiều page cùng lúc, rất khó làm
  đúng khi có nhiều writer đồng thời — trong khi sách chỉ có **1 writer**.
- **Sentinel key rỗng** của sách tương ứng với **"high key"** và page **leftmost**
  trong `nbtree` của Postgres — cùng mục đích: loại bỏ trường hợp biên khi tra cứu.
- Kỹ thuật test bằng **mock 3 callback** rất đáng học: nó cho phép test toàn bộ
  logic cây mà **không cần đụng tới disk**. Áp dụng được y nguyên cho dự án của bạn.
