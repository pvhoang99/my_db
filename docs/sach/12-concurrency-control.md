# Chương 12 — Điều khiển đồng thời
*(Concurrency Control)*

## 12.1 Các mức độ concurrency

### Vấn đề: reader và writer đan xen nhau

Các client đồng thời có thể **vào và ra transaction tuỳ ý**, yêu cầu đọc và ghi
ở giữa. Để đơn giản hoá phân tích, hãy giả định rằng **vào/ra/đọc/ghi là những
bước atomic**, nhờ vậy các TX đồng thời chỉ là **những bước đan xen nhau**.

Ta cũng **phân biệt TX chỉ-đọc với TX đọc-ghi**, bởi vì:

- **Reader đồng thời là bài toán dễ hơn nhiều** so với writer đồng thời.
- **Nhiều ứng dụng nặng về đọc**, nơi hiệu năng đọc quan trọng hơn.

### Readers-writer lock (RWLock)

Nếu không biết làm concurrency thế nào, **bạn luôn có thể thêm một mutex (khoá)**
để tuần tự hoá mọi truy cập dữ liệu. Để hiệu năng đọc tốt hơn, có thể dùng
**readers-writer lock**: cho phép **nhiều reader đồng thời**, nhưng **chỉ một writer**.

- Nếu không có writer nào, không gì thay đổi được → **reader đồng thời là OK**.
- Khi một writer muốn vào, **nó đợi tới khi mọi reader đã rời đi**.
- **Reader bị writer chặn**, nhưng **không bị reader khác chặn**.

Tính hữu dụng của cách này **có hạn**: **không có concurrency giữa các writer**,
và **TX chạy lâu thì rất tệ** vì reader và writer chặn lẫn nhau.

### Read-copy-update (RCU)

Để **reader và writer không chặn nhau**, ta cho **mỗi bên làm việc trên phiên bản
dữ liệu của riêng mình**.

- Có **một con trỏ tới dữ liệu bất biến**; reader chỉ việc **chộp lấy nó làm snapshot**.
- **Một writer duy nhất** update bản copy của nó, **rồi lật con trỏ** sang bản mới.

Ta **được mức concurrency này miễn phí** vì ta đang dùng copy-on-write. Nhưng
**một writer duy nhất vẫn chưa đủ**, bởi vì **vòng đời của một TX do client kiểm
soát** — và nó có thể dài tuỳ ý.

### Optimistic concurrency control (điều khiển lạc quan)

Nhiều writer đồng thời dẫn tới **xung đột**, ví dụ:

| Seq | TX1 | TX2 |
|---|---|---|
| 1 | | `write a := 1` |
| 2 | `read a` | |
| 3 | | `commit` |
| 4 | `write b := a` | |
| 5 | `commit` | |

TX1 **phụ thuộc vào đúng cái key mà TX2 sửa**, nên **cả hai không thể cùng thành công**.

Lưu ý rằng **một số thao tác tưởng như "chỉ ghi" thực ra lại có phụ thuộc đọc**.
Ví dụ, giao diện update/delete của ta **báo lại key có được update/xoá hay không** —
điều đó **phụ thuộc vào trạng thái trước đó của key**. Nên kịch bản sau **cũng là
xung đột**:

| Seq | TX1 | TX2 |
|---|---|---|
| 1 | | `delete a` |
| 2 | `write a := 1` | |
| 3 | | `commit` |
| 4 | `commit` | |

Một cách xử lý xung đột là **cứ huỷ TX khi phát hiện xung đột**:

1. TX bắt đầu.
2. **Đọc thì đọc trên snapshot**, nhưng **ghi thì đệm lại cục bộ**.
3. **Trước khi commit, kiểm chứng rằng không có xung đột** với các TX đã commit.
4. TX kết thúc.
   - Nếu có xung đột → **huỷ và rollback**.
   - Ngược lại → **chuyển các lần ghi đã đệm vào DB**.

Lưu ý rằng **kiểm chứng và commit phải là một bước atomic**. Cái này gọi là
**optimistic concurrency control (điều khiển đồng thời lạc quan)**, vì nó
**giả định xung đột là hiếm** và **không làm gì để ngăn chúng**. Ta sẽ cài đặt
cách này, nhưng vẫn nên biết các lựa chọn khác.

### Cách thay thế: pessimistic concurrency control (điều khiển bi quan)

Với điều khiển lạc quan, **TX không tiến triển được khi có xung đột** — điều này
không mấy hữu ích từ góc nhìn ứng dụng, vì tất cả những gì nó làm được là
**thử lại trong một vòng lặp**. Cách khác để xử lý xung đột là **ngăn chặn chúng
bằng khoá**. Các TX sẽ **giành khoá trên những thứ chúng phụ thuộc**, nhờ vậy
những TX có khả năng xung đột sẽ **đợi lẫn nhau**.

Nghe có vẻ hay hơn nhiều, đặc biệt ở ví dụ cuối, nơi write/delete tiến triển được
không vấn đề gì. Tuy nhiên, **cách này vẫn không đảm bảo tiến triển**, vì giờ TX
có thể **thất bại do deadlock**.

**Deadlock** là khi 2 bên **đợi nhau nhả một khoá (khác nhau) mà bên kia đang
giữ**. Chuyện này cũng xảy ra với nhiều hơn 2 bên, miễn là **có một chu trình
trong đồ thị phụ thuộc**. Trong lập trình đồng thời, **khoá nên được giành theo
một thứ tự định trước** để tránh chu trình. Nhưng **với DB thì không thể**, vì
client có thể giành khoá theo thứ tự bất kỳ — nên **DB phải phát hiện và giải
quyết deadlock**, và đó là **một bài toán đồ thị**.

### So sánh các cơ chế điều khiển đồng thời

| | reader-reader | reader-writer | writer-writer | xung đột |
|---|---|---|---|---|
| **RWLock** | pass | block | block | — |
| **RCU** | pass | pass | block | — |
| **Optimistic** | pass | pass | pass | **abort** |
| **Pessimistic** | pass | lock | lock | **prevent** |

## 12.2 Snapshot isolation cho reader

**Isolation level** nói về việc **một TX nhìn thấy thay đổi từ các TX khác như
thế nào**. Đây **không phải vấn đề với copy-on-write**, vì một TX **làm việc trên
một snapshot của B+tree**.

### Thu gom các update cục bộ

Một transaction giữ **một snapshot của DB** và **các update cục bộ**.

- **Snapshot** chỉ là **một root pointer**, nhờ copy-on-write.
- **Các update cục bộ** được giữ trong **một B+tree nằm trong bộ nhớ**.

```go
type KVTX struct {
    // snapshot chỉ đọc
    snapshot BTree
    // các update KV đã thu gom:
    // value có thêm 1 byte cờ ở đầu để đánh dấu key đã bị xoá.
    pending BTree
    // ...
}
```

Cả hai cây đều được khởi tạo ở **đầu TX**.

```go
// bắt đầu một transaction
func (kv *KV) Begin(tx *KVTX) {
    // snapshot chỉ đọc, chỉ gồm root của cây và callback đọc page
    tx.snapshot.root = kv.tree.root
    tx.snapshot.get = ... // đọc từ các page đã mmap ...

    // cây trong bộ nhớ để thu gom update
    pages := [][]byte(nil)
    tx.pending.get = func(ptr uint64) []byte { return pages[ptr-1] }
    tx.pending.new = func(node []byte) uint64 {
        pages = append(pages, node)
        return uint64(len(pages))
    }
    tx.pending.del = func(uint64) {}
}
```

Để biểu diễn **key đã bị xoá**, value trong `KVTX.pending` có **1 byte cờ ở đầu**:

```go
FLAG_DELETED = byte(1)
FLAG_UPDATED = byte(2)
```

### Đọc lại được cái mình vừa ghi

Trong một TX, client **phải đọc lại được cái nó vừa ghi**, kể cả khi chưa commit.
Nên truy vấn **phải tra `KVTX.pending` TRƯỚC `KVTX.snapshot`**. Đó chính là lý do
các lần ghi được giữ trong **một B+tree** thay vì chỉ một danh sách.

```go
// point query. gộp các update đã thu gom với snapshot
func (tx *KVTX) Get(key []byte) ([]byte, bool) {
    val, ok := tx.pending.Get(key)
    switch {
    case ok && val[0] == FLAG_UPDATED: // được update trong TX này
        return val[1:], true
    case ok && val[0] == FLAG_DELETED: // bị xoá trong TX này
        return nil, false
    case !ok: // đọc từ snapshot
        return tx.snapshot.Get(key)
    default:
        panic("unreachable")
    }
}
```

Với range query, ta thêm **một kiểu iterator mới gộp cả hai cây**.

```go
// iterator gộp các update đang chờ với snapshot
type CombinedIter struct {
    top *BIter // KVTX.pending
    bot *BIter // KVTX.snapshot
    // ...
}
```

### Số hiệu phiên bản trong free list

Vì **reader có thể đang giữ các phiên bản cũ của DB**, free list **không được
phát đi những page thuộc các phiên bản đó**. Ta giải bằng cách **gán cho mỗi
phiên bản một số hiệu tăng đơn điệu**. Cái này (về mặt logic) còn gọi là
**timestamp**.

- Ta **theo dõi các TX đang chạy** và **phiên bản mà chúng dựa trên**.
- **Mỗi page được thêm vào free list đều đi kèm số hiệu phiên bản.**
- **Danh sách không bao giờ phát đi page mới hơn TX cũ nhất.**

Cách này hoạt động bằng cách **kiểm tra phiên bản khi tiêu thụ từ đầu danh sách**.
Nhớ rằng free list là **FILO (first-in-last-out)**, nên **page từ phiên bản cũ
nhất sẽ được tiêu thụ trước**.

**Sửa đổi 1:** thêm số hiệu phiên bản vào `KVTX` và `KV`.

```go
type KVTX struct {
    // snapshot chỉ đọc
    snapshot BTree
    version  uint64 // dựa trên KV.version
    // ...
}

type KV struct {
    // ...
    version uint64   // số hiệu phiên bản tăng đơn điệu; lưu trong meta page
    ongoing []uint64 // số hiệu phiên bản của các TX đồng thời
}
```

**Sửa đổi 2:** bổ sung cho free list.

```go
// | next |  pointer + version  | unused |
// |  8B  |      n*(8B+8B)      |  ...   |
type FreeList struct {
    // ...
    maxSeq uint64 // `tailSeq` đã lưu, chặn việc tiêu thụ phần tử vừa thêm
    maxVer uint64 // phiên bản của reader cũ nhất
    curVer uint64 // số hiệu phiên bản khi commit
}
```

- **`maxVer`** được duy trì bằng **phiên bản cũ nhất trong `KV.ongoing`** khi một
  TX thoát. Nó **ngăn việc tái sử dụng page**, bên cạnh `maxSeq` đã có.
- **`curVer`** được writer đặt thành **phiên bản kế tiếp** khi commit.

## 12.3 Xử lý xung đột cho writer

### Phát hiện xung đột bằng lịch sử

**Sửa đổi 1:** mọi lần đọc đều được thêm vào `KVTX.reads` (cả point lẫn range query).

```go
// start <= key <= stop
type KeyRange struct {
    start []byte
    stop  []byte
}

type KVTX struct {
    // ...
    reads []KeyRange
}

func (tx *KVTX) Get(key []byte) ([]byte, bool) {
    tx.reads = append(tx.reads, KeyRange{key, key}) // phụ thuộc
    // ...
}
```

**Sửa đổi 2:** mỗi lần commit thành công đều được thêm vào `KV.history`.

```go
type KV struct {
    // ...
    history []CommittedTX // các key đã đổi; để phát hiện xung đột
}

type CommittedTX struct {
    version uint64
    writes  []KeyRange // đã sắp xếp
}

func (kv *KV) Commit(tx *KVTX) error {
    // ...
    if len(writes) > 0 {
        kv.history = append(kv.history, CommittedTX{kv.version, writes})
    }
    return nil
}
```

Việc phát hiện xung đột hoạt động bằng cách **kiểm tra sự chồng lấn giữa những
thứ TX phụ thuộc và phần lịch sử mới hơn phiên bản gốc của nó**.

```go
func detectConflicts(kv *KV, tx *KVTX) bool {
    for i := len(kv.history) - 1; i >= 0; i-- {
        if !versionBefore(tx.version, kv.history[i].version) {
            break // đã sắp xếp
        }
        if rangesOverlap(tx.reads, kv.history[i].writes) {
            return true
        }
    }
    return false
}
```

**Lịch sử được cắt bớt khi TX cũ nhất thoát.**

## 12.4 Giảm bớt việc khoá

Ta có thể dùng **một khoá duy nhất cho mọi phương thức của `KVTX`**. Nhưng **có
cách giảm bớt việc khoá**. Ví dụ, ta **không cần tuần tự hoá các phương thức
đọc/ghi**, bởi vì:

- **Ghi chỉ làm việc trên `KVTX.pending`**, chúng **không bao giờ chạm vào `KV`**.
- **Đọc chỉ chạm vào `KV.mmap.chunks`** — những slice do `mmap` trả về.

Bước commit **có thể sửa `KV.mmap.chunks`** bằng cách append, nên ta sẽ
**dùng một bản copy cục bộ cho mỗi TX**. Slice này là **append-only**, nên
**copy nông là đủ**.

```go
func (kv *KV) Begin(tx *KVTX) {
    kv.mutex.Lock()
    defer kv.mutex.Unlock()

    // snapshot chỉ đọc, chỉ gồm root của cây và callback đọc page
    tx.snapshot.root = kv.tree.root
    chunks := kv.mmap.chunks // copy ra để tránh bị writer sửa
    tx.snapshot.get = func(ptr uint64) []byte { return mmapRead(ptr, chunks) }
    // ...
}
```

Nhờ vậy, **các phương thức đọc/ghi không cần khoá và chạy song song được**.
Điều đó tốt, vì **đọc có thể gây page fault và chặn luồng**.

Tới đây, **chỉ `Begin`, `Commit` và `Abort` bị tuần tự hoá**. Nhưng xét rằng
`Commit` có dính IO, ta có thể đi xa hơn: **nhả khoá trong lúc chờ IO** để cho
phép TX khác vào và TX chỉ-đọc thoát ra. Bước commit **vẫn phải tuần tự hoá với
các bước commit khác** bằng một khoá riêng. **Phần này để lại làm bài tập.**

---

## 📌 Đối chiếu với PostgreSQL *(ghi chú thêm, không có trong sách)*

- Bảng so sánh 4 cơ chế là **bản đồ rất tốt** để định vị Postgres: Postgres dùng
  **MVCC + khoá bi quan cho write-write**, và **lạc quan cho `SERIALIZABLE`**
  (**SSI** — Serializable Snapshot Isolation).
- **Snapshot của sách = một root pointer.** **Snapshot của Postgres** = bộ ba
  `(xmin, xmax, danh sách xip)` — tập các transaction đang chạy tại thời điểm
  chụp. Tốn kém hơn nhiều, nhưng cho phép **nhiều writer đồng thời**, điều mà
  kiến trúc 1-writer của sách không làm được.
- **Isolation level**: sách chỉ có **một mức** (snapshot isolation). Postgres có
  `READ COMMITTED` (mặc định — chụp snapshot **mới ở mỗi câu lệnh**),
  `REPEATABLE READ` (= snapshot isolation của sách, snapshot cho **cả transaction**),
  và `SERIALIZABLE`.
- **`maxVer` = phiên bản reader cũ nhất** chính là **horizon / `xmin` ngang** của
  Postgres. Hệ quả **giống hệt nhau**: một TX mở lâu **chặn việc thu hồi chỗ** —
  ở sách là free list, ở Postgres là VACUUM. Đây là lý do
  `idle in transaction` làm bảng phình to.
- **Phát hiện xung đột bằng `reads` × `history`** của sách chính là ý tưởng
  **SSI** của Postgres, nhưng Postgres theo dõi **predicate lock** (`SIREAD`) và
  phát hiện **cấu trúc nguy hiểm** trong đồ thị phụ thuộc, thay vì so khoảng key.
  Khi phát hiện, Postgres báo lỗi **`could not serialize access`** —
  đúng tinh thần "abort rồi để client thử lại" mà sách nói.
- **Deadlock**: Postgres có **deadlock detector** chạy sau `deadlock_timeout`
  (mặc định 1 giây), đúng như sách mô tả — **tìm chu trình trong đồ thị chờ**.
