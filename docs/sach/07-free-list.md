# Chương 07 — Free List: Tái chế & Tái sử dụng
*(Free List: Recycle & Reuse)*

Bước cuối cùng của KV store là **tái sử dụng những page đã bị xoá** — đây cũng là
vấn đề của cả cấu trúc dữ liệu trong bộ nhớ.

## 7.1 Các kỹ thuật quản lý bộ nhớ

### Ta sẽ làm gì

Quản lý bộ nhớ (không gian) có thể **thủ công** hoặc **tự động**.
**Garbage collector** là tự động — nó phát hiện các object không còn dùng mà
không cần lập trình viên trợ giúp. Vấn đề tiếp theo là **xử lý (tái sử dụng)
những object không dùng đó như thế nào**.

Ta **không cần GC**, bởi vì trong một cấu trúc dạng cây, **việc phát hiện node
không dùng là chuyện hiển nhiên** — ta đã làm rồi qua callback `BTree.del`.
Việc ta sẽ làm là **cài đặt lại những callback đó**.

### Danh sách các object không dùng

Một trong những lý do không gian đĩa được quản lý theo **page cùng kích thước**
là vì sau khi bị xoá, **chúng trở nên có thể thay thế lẫn nhau**; DB có thể dùng
lại bất kỳ page nào khi cần. Cái này **đơn giản hơn nhiều** so với các thủ tục
quản lý bộ nhớ tổng quát như `malloc`, vốn phải xử lý kích thước tuỳ ý.

Ta cần lưu một **danh sách page không dùng**, gọi là **free list** hoặc
**object pool**. Với dữ liệu trong bộ nhớ, nó có thể chỉ là một mảng con trỏ,
hoặc một danh sách liên kết nhúng trong chính các object.

### Danh sách liên kết nhúng (embedded linked list)

Cơ chế đơn giản nhất là dùng **danh sách liên kết nhúng (intrusive)**. Con trỏ
của danh sách **nằm ngay bên trong chính object**; nó **mượn chỗ của object**,
nên **không tốn thêm không gian nào** cho cấu trúc dữ liệu.

```
head
 ↓
[ next | space... ]   (object không dùng 1)
   ↓
[ next | space... ]   (object không dùng 2)
   ↓
  ...
```

Tuy nhiên, cách này **xung đột với copy-on-write**, vì nó **ghi đè** trong lúc update.

### Danh sách bên ngoài (external list)

Cơ chế còn lại là lưu con trỏ tới các page không dùng trong một **cấu trúc dữ
liệu bên ngoài**. Bản thân cấu trúc bên ngoài đó **cũng chiếm chỗ** — và đó là
vấn đề ta phải giải.

Giả sử free list của ta chỉ là một **log các số hiệu page không dùng**; thêm phần
tử thì chỉ việc append. Vấn đề là **làm sao xoá phần tử đi để nó không phình ra
vô hạn**.

## 7.2 Danh sách liên kết trên disk

### Yêu cầu đối với free list

Hãy hình dung free list như một dãy phần tử, giống một log. Trong cây copy-on-write,
**mỗi lần update đều cần node mới và xoá node cũ**, nên free list **vừa bị thêm
vào vừa bị lấy ra** trong mỗi lần update.

- Nếu **lấy phần tử từ cuối**, thì phần tử mới thêm sẽ **ghi đè lên dữ liệu cũ**,
  đòi hỏi thêm cơ chế crash recovery như đã bàn ở chương 03.
- Nếu **lấy phần tử từ đầu**, thì **làm sao thu hồi chỗ của những phần tử đã lấy đi?**
  Ta lại quay về đúng bài toán ban đầu.

Để giải, **bản thân free list cũng phải dựa trên page, để nó tự quản lý được
chính mình**. Một danh sách dựa trên page thì chính là một **danh sách liên kết**,
chỉ khác là **mỗi page chứa được nhiều phần tử**, giống như node của B+tree.
Cái này còn gọi là **unrolled linked list**.

Tóm lại:

- **Free list là một cấu trúc dữ liệu độc lập: một danh sách liên kết các page.**
  - Khi cần node mới, nó sẽ **thử lấy page từ chính nó**.
  - Node bị gỡ khỏi danh sách được **đưa lại vào chính nó** để tái sử dụng.
- **Mỗi page chứa được nhiều phần tử** (số hiệu page).
  - Page được **update tại chỗ**, nhưng **bên trong một page thì vẫn là append-only**.
- **Phần tử được append vào node đuôi và tiêu thụ từ node đầu.**
  - Làm thế này thì node đuôi dễ giữ tính append-only hơn.

### Bố cục free list trên disk

Mỗi node bắt đầu bằng **một con trỏ tới node kế tiếp**. Các phần tử được append
ngay sau đó.

```go
// định dạng node:
// | next | pointers | unused |
// |  8B  |   n*8B   |  ...   |
type LNode []byte

const FREE_LIST_HEADER = 8
const FREE_LIST_CAP    = (BTREE_PAGE_SIZE - FREE_LIST_HEADER) / 8

// getter & setter
func (node LNode) getNext() uint64
func (node LNode) setNext(next uint64)
func (node LNode) getPtr(idx int) uint64
func (node LNode) setPtr(idx int, ptr uint64)
```

Ta cũng lưu **con trỏ tới cả node đầu lẫn node đuôi** trong meta page. Con trỏ
tới node đuôi là cần thiết để **chèn trong `O(1)`**.

```
              first_item
                  ↓
head_page -> [ next |     xxxxx ]
                 ↓
             [ next | xxxxxxxx ]
                 ↓
tail_page -> [ NULL | xxxx      ]
                        ↑
                    last_item
```

### Update các node của free list

Khi chưa có free list, **meta page là page duy nhất được update tại chỗ** — đó
chính là điều làm cho copy-on-write khiến crash recovery trở nên dễ dàng. Giờ
**có thêm 2 chỗ update tại chỗ** trong node của danh sách: **con trỏ `next`** và
**các phần tử được append**.

Mặc dù node của danh sách được update tại chỗ, **không có dữ liệu nào bị ghi đè
bên trong một page**. Nên nếu một lần update bị gián đoạn, **meta page vẫn trỏ
tới đúng dữ liệu cũ**; **không cần thêm cơ chế crash recovery nào**. Và khác với
meta page, ở đây **không đòi hỏi tính atomic**.

> Theo phân tích này, **danh sách liên kết nhúng cũng hoạt động được** — với điều
> kiện con trỏ `next` được **dành sẵn chỗ trong node B+tree**. Đây là chỗ bạn có
> thể làm khác sách. Tuy nhiên cách đó **làm write amplification tăng gấp đôi**.

## 7.3 Cài đặt free list

### Giao diện free list

```go
type KV struct {
    Path string
    // nội bộ
    fd   int
    tree BTree
    free FreeList // thêm vào
    // ...
}
```

`FreeList` là cấu trúc dữ liệu thêm vào trong `KV`:

```go
type FreeList struct {
    // callback quản lý page trên disk
    get func(uint64) []byte // đọc một page
    new func([]byte) uint64 // append một page mới
    set func(uint64) []byte // update một page đã có

    // dữ liệu được lưu bền vững trong meta page
    headPage uint64 // con trỏ tới node đầu danh sách
    headSeq  uint64 // số thứ tự tăng đơn điệu, dùng để đánh index vào node đầu
    tailPage uint64
    tailSeq  uint64

    // trạng thái trong bộ nhớ
    maxSeq uint64 // `tailSeq` đã lưu, để không tiêu thụ nhầm phần tử vừa thêm
}

// lấy 1 phần tử từ đầu danh sách. trả về 0 nếu thất bại.
func (fl *FreeList) PopHead() uint64

// thêm 1 phần tử vào đuôi
func (fl *FreeList) PushTail(ptr uint64)
```

Giống như `BTree`, việc quản lý page được cô lập qua **3 callback**:

- **`get`** đọc một page — giống như trước.
- **`new`** append một page — trước đây `BTree` dùng.
- **`set`** trả về một **buffer ghi được** để thu gom các update tại chỗ.
- **`del` không có**, vì free list **tự quản lý các page trống của chính nó**.

```go
func (db *KV) Open() error {
    // ...
    // callback của B+tree
    db.tree.get = db.pageRead      // đọc một page
    db.tree.new = db.pageAlloc     // (mới) tái sử dụng từ free list hoặc append
    db.tree.del = db.free.PushTail // (mới) page được giải phóng đi vào free list
    // callback của free list
    db.free.get = db.pageRead      // đọc một page
    db.free.new = db.pageAppend    // append một page
    db.free.set = db.pageWrite     // (mới) update tại chỗ
    // ...
}
```

### Cấu trúc dữ liệu free list

Vì một node chứa **số lượng phần tử thay đổi**, tối đa là `FREE_LIST_CAP`, ta cần
biết **phần tử đầu tiên nằm ở đâu trong node đầu** (`headSeq`), và **phần tử kết
thúc ở đâu trong node đuôi** (`tailSeq`).

`headSeq` và `tailSeq` là **index vào node đầu/đuôi**, chỉ khác là chúng
**tăng đơn điệu**. Nên index thực tế (có quay vòng) là:

```go
func seq2idx(seq uint64) int {
    return int(seq % FREE_LIST_CAP)
}
```

Ta làm chúng **tăng đơn điệu** để chúng trở thành **định danh duy nhất của vị trí
trong danh sách**; muốn ngăn đầu danh sách **vượt qua** đuôi danh sách, chỉ cần
**so sánh 2 số thứ tự** là xong.

Trong một lần update, danh sách **vừa bị thêm vào vừa bị lấy ra**, và khi lấy từ
đầu, ta **không được lấy phải cái vừa thêm vào đuôi**. Nên ta cần:

1. **Đầu mỗi lần update**, lưu `tailSeq` gốc vào `maxSeq`.
2. **Trong lúc update**, `headSeq` **không được vượt quá** `maxSeq`.
3. **Đầu lần update kế tiếp**, `maxSeq` được đẩy lên bằng `tailSeq`.
4. … cứ thế.

```go
// cho phép tiêu thụ những phần tử vừa được thêm vào
func (fl *FreeList) SetMaxSeq() {
    fl.maxSeq = fl.tailSeq
}
```

### Tiêu thụ từ free list

Gỡ một phần tử khỏi node đầu chỉ đơn giản là **tăng `headSeq`**. Và khi node đầu
**trở thành rỗng**, chuyển sang node kế tiếp.

```go
// gỡ 1 phần tử khỏi node đầu, và gỡ luôn node đầu nếu nó rỗng.
func flPop(fl *FreeList) (ptr uint64, head uint64) {
    if fl.headSeq == fl.maxSeq {
        return 0, 0 // không tiến được nữa
    }
    node := LNode(fl.get(fl.headPage))
    ptr = node.getPtr(seq2idx(fl.headSeq)) // phần tử
    fl.headSeq++
    // chuyển sang node kế tiếp nếu node đầu đã rỗng
    if seq2idx(fl.headSeq) == 0 {
        head, fl.headPage = fl.headPage, node.getNext()
        assert(fl.headPage != 0)
    }
    return
}
```

**Free list tự quản lý chính mình**; node đầu vừa bị gỡ được **nạp ngược lại
vào chính nó**.

```go
// lấy 1 phần tử từ đầu danh sách. trả về 0 nếu thất bại.
func (fl *FreeList) PopHead() uint64 {
    ptr, head := flPop(fl)
    if head != 0 { // node đầu rỗng được tái chế
        fl.PushTail(head)
    }
    return ptr
}
```

Chuyện gì xảy ra nếu **node cuối cùng bị gỡ**? Một danh sách liên kết có **0 node**
kéo theo đủ thứ trường hợp đặc biệt khó chịu. Trong thực tế, **thiết kế sao cho
danh sách luôn có ít nhất 1 node sẽ dễ hơn** là phải xử lý các trường hợp đặc
biệt đó. Đó là lý do có dòng `assert(fl.headPage != 0)`.

### Đẩy vào free list

Append một phần tử vào node đuôi chỉ đơn giản là **tăng `tailSeq`**. Và khi node
đuôi **đầy**, ta **lập tức thêm một node đuôi rỗng mới** để đảm bảo luôn có ít
nhất 1 node, phòng khi node đuôi trước đó bị gỡ đi với tư cách node đầu.

```go
func (fl *FreeList) PushTail(ptr uint64) {
    // thêm vào node đuôi
    LNode(fl.set(fl.tailPage)).setPtr(seq2idx(fl.tailSeq), ptr)
    fl.tailSeq++
    // thêm node đuôi mới nếu đã đầy (danh sách không bao giờ rỗng)
    if seq2idx(fl.tailSeq) == 0 {
        // thử tái sử dụng từ đầu danh sách
        next, head := flPop(fl) // có thể gỡ mất node đầu
        if next == 0 {
            // hoặc cấp phát node mới bằng cách append
            next = fl.new(make([]byte, BTREE_PAGE_SIZE))
        }
        // nối tới node đuôi mới
        LNode(fl.set(fl.tailPage)).setNext(next)
        fl.tailPage = next
        // nếu node đầu bị gỡ thì cũng thêm nó vào luôn
        if head != 0 {
            LNode(fl.set(fl.tailPage)).setPtr(0, head)
            fl.tailSeq++
        }
    }
}
```

Một lần nữa, free list **tự quản lý**: nó **thử lấy node từ chính nó** cho node
đuôi mới trước khi phải dùng tới việc append.

## 7.4 KV với free list

### Quản lý page

Giờ page có thể được tái sử dụng, nên **page tái sử dụng bị ghi đè tại chỗ** —
vì vậy ta dùng một **map để thu gom các update đang chờ**.

```go
type KV struct {
    // ...
    page struct {
        flushed uint64            // kích thước database tính theo số page
        nappend uint64            // số page sắp được append
        updates map[uint64][]byte // update đang chờ, gồm cả page được append
    }
}
```

`BTree.new` bây giờ là `KV.pageAlloc` — nó **dùng free list trước**, rồi mới đến
append.

```go
// `BTree.new`, cấp phát một page mới.
func (db *KV) pageAlloc(node []byte) uint64 {
    if ptr := db.free.PopHead(); ptr != 0 { // thử free list
        db.page.updates[ptr] = node
        return ptr
    }
    return db.pageAppend(node) // append
}
```

`KV.pageWrite` trả về **một bản copy ghi được của page** để thu gom update tại chỗ.

```go
// `FreeList.set`, update một page đã có.
func (db *KV) pageWrite(ptr uint64) []byte {
    if node, ok := db.page.updates[ptr]; ok {
        return node // update đang chờ
    }
    node := make([]byte, BTREE_PAGE_SIZE)
    copy(node, db.pageReadFile(ptr)) // khởi tạo từ file
    db.page.updates[ptr] = node
    return node
}
```

Một thay đổi nữa: ta **có thể đọc lại một page sau khi nó đã được update**, nên
`KV.pageRead` phải **tra cứu map update đang chờ trước**.

```go
// `BTree.get`, đọc một page.
func (db *KV) pageRead(ptr uint64) []byte {
    if node, ok := db.page.updates[ptr]; ok {
        return node // update đang chờ
    }
    return db.pageReadFile(ptr)
}

func (db *KV) pageReadFile(ptr uint64) []byte {
    // giống `KV.pageRead` ở chương trước ...
}
```

### Update meta page

Meta page bây giờ chứa thêm **con trỏ free list (đầu và đuôi)**, được update
**atomic cùng với root của cây**.

```
| sig | root_ptr | page_used | head_page | head_seq | tail_page | tail_seq |
| 16B |    8B    |    8B     |    8B     |    8B    |    8B     |    8B    |
```

Nhớ rằng **free list luôn chứa ít nhất 1 node**, nên ta gán cho nó một node rỗng
khi khởi tạo một DB trống.

```go
func readRoot(db *KV, fileSize int64) error {
    if fileSize == 0 { // file rỗng
        // dành trước 2 page: meta page và một node free list
        db.page.flushed = 2
        // thêm một node khởi đầu vào free list để nó không bao giờ rỗng
        db.free.headPage = 1 // page thứ 2
        db.free.tailPage = 1
        return nil // meta page sẽ được ghi ở lần update đầu tiên
    }
    // ...
}
```

Vì `headSeq` bị chặn bởi `maxSeq`, nên `maxSeq` được đẩy lên bằng `tailSeq`
**giữa các lần update**, để cho phép tái sử dụng page từ phiên bản trước.

```go
func updateFile(db *KV) error {
    // ...
    // chuẩn bị free list cho lần update kế tiếp
    db.free.SetMaxSeq()
    return nil
}
```

Ta vẫn giả định **truy cập tuần tự** trong chương này. Khi thêm concurrency sau
này, **`headSeq` sẽ bị chặn bởi reader cũ nhất** thay vì bởi `maxSeq`.

## 7.5 Kết luận về KV store

Những gì ta đã làm:

- Bố cục file cho B+tree copy-on-write.
- Durability và atomicity bằng `fsync`.
- Quản lý page trên disk bằng free list.

Thế là đủ cho một KV store với `get`, `set`, `del`. Nhưng **phần II còn nữa**:

- **Relational DB trên nền KV store.**
- **Transaction đồng thời.**

---

## 📌 Đối chiếu với PostgreSQL *(ghi chú thêm, không có trong sách)*

- Free list của sách tương ứng với **FSM (Free Space Map)** của Postgres — file
  `<relfilenode>_fsm` lưu chỗ trống còn lại của từng page, tổ chức dạng **cây**
  chứ không phải danh sách liên kết.
- Khác biệt căn bản: free list của sách thu hồi **page nguyên vẹn** (vì COW huỷ
  cả page cũ); FSM của Postgres theo dõi **chỗ trống bên trong page** (vì tuple
  chết nằm rải rác trong page còn sống).
- Việc **"`headSeq` sẽ bị chặn bởi reader cũ nhất"** mà sách hẹn ở chương sau
  chính là khái niệm **horizon / `xmin` ngang** của Postgres: **VACUUM không thể
  dọn tuple mới hơn transaction cũ nhất đang chạy**. Đây là lý do một
  transaction mở lâu (`idle in transaction`) làm bảng phình to.
- Postgres còn có **Visibility Map** (`_vm`) — thứ mà kiến trúc COW của sách
  không cần, vì phiên bản cũ nằm ở cây cũ chứ không nằm lẫn trong page hiện tại.
