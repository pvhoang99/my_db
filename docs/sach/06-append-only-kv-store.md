# Chương 06 — KV Store chỉ ghi thêm
*(Append-Only KV Store)*

## 6.1 Ta sẽ làm gì

Ta sẽ tạo một KV store với **B+tree copy-on-write được hậu thuẫn bởi một file**.

```go
type KV struct {
    Path string // tên file
    // nội bộ
    fd   int
    tree BTree
    // còn nữa...
}

func (db *KV) Open() error

func (db *KV) Get(key []byte) ([]byte, bool) {
    return db.tree.Get(key)
}
func (db *KV) Set(key []byte, val []byte) error {
    db.tree.Insert(key, val)
    return updateFile(db)
}
func (db *KV) Del(key []byte) (bool, error) {
    deleted := db.tree.Delete(key)
    return deleted, updateFile(db)
}
```

Phạm vi của chương này là **durability + atomicity**:

- File là **append-only**; việc tái sử dụng chỗ để dành cho chương sau.
- Ta **bỏ qua concurrency** và giả định truy cập tuần tự trong 1 tiến trình.

Ta sẽ cài đặt **3 callback của B+tree** làm việc với page trên disk:

```go
type BTree struct {
    root uint64
    get  func(uint64) []byte // đọc một page
    new  func([]byte) uint64 // append một page
    del  func(uint64)        // bỏ qua ở chương này
}
```

## 6.2 Update hai pha

### Atomicity + durability

Như đã bàn ở chương 03, với cây copy-on-write, **root pointer được update một
cách atomic**. Sau đó dùng `fsync` để yêu cầu và xác nhận tính durable.

Nhưng **chỉ atomic ở root pointer thôi là chưa đủ**; để cả cây atomic,
**các node mới phải được ghi bền vững TRƯỚC root pointer**. Mà **thứ tự ghi
không phải là thứ tự dữ liệu được lưu xuống đĩa**, do các yếu tố như caching.
Vì vậy cần **thêm một `fsync` nữa để đảm bảo thứ tự**.

```go
func updateFile(db *KV) error {
    // 1. Ghi các node mới.
    if err := writePages(db); err != nil {
        return err
    }
    // 2. `fsync` để ép thứ tự giữa bước 1 và bước 3.
    if err := syscall.Fsync(db.fd); err != nil {
        return err
    }
    // 3. Update root pointer một cách atomic.
    if err := updateRoot(db); err != nil {
        return err
    }
    // 4. `fsync` để làm mọi thứ bền vững.
    return syscall.Fsync(db.fd)
}
```

### Cách thay thế: durability bằng log

Cơ chế double-write thay thế cũng có **2 pha được fsync**:

1. Ghi các page đã update kèm checksum.
2. `fsync` để làm bản update bền vững (phục vụ crash recovery).
3. Update dữ liệu tại chỗ (apply các double-write).
4. `fsync` để đảm bảo thứ tự giữa bước 3 và bước 1 (tái sử dụng hoặc xoá double-write).

**Khác biệt so với copy-on-write là thứ tự các pha**: với double-write, dữ liệu
đã bền vững **ngay sau lần `fsync` thứ nhất**; DB có thể **trả về thành công
luôn** và làm nốt phần còn lại ở background.

Double-write tương đương với một **log** — cũng chỉ cần 1 lần `fsync` cho mỗi
update. Và nó **có thể là một log thật sự** để **gom đệm nhiều update**, giúp
cải thiện hiệu năng. Đây là một ví dụ nữa về log trong DB, bên cạnh LSM-tree.

Ta **sẽ không dùng log** vì copy-on-write không cần. Nhưng log vẫn mang lại
những lợi ích nói trên; đó là một trong các lý do **log có mặt khắp nơi trong
database**.

### Concurrency của dữ liệu trong bộ nhớ

Atomicity cho dữ liệu trong bộ nhớ (xét về concurrency) đạt được bằng **mutex
(khoá)** hoặc vài **lệnh atomic của CPU**. Có một vấn đề tương tự: **việc đọc/ghi
bộ nhớ có thể không xuất hiện đúng thứ tự** do những yếu tố như thực thi
không theo thứ tự (out-of-order execution).

Với một cây copy-on-write trong bộ nhớ, **các node mới phải được làm cho reader
đồng thời nhìn thấy TRƯỚC khi root pointer được update**. Cái này gọi là
**memory barrier**, và nó **tương tự như `fsync`** — dù `fsync` còn làm nhiều
hơn là chỉ ép thứ tự.

Các nguyên thuỷ đồng bộ như mutex, hay bất kỳ syscall nào của OS, đều **ép thứ tự
bộ nhớ một cách di động (portable)**, nên bạn không phải nghịch với atomic hay
barrier đặc thù của CPU (mà dù sao chúng cũng không đủ cho concurrency).

## 6.3 Database nằm trên một file

### Bố cục file

DB của ta là **một file duy nhất chia thành các "page"**. Mỗi page là một node
của B+tree, **ngoại trừ page đầu tiên**; page đầu chứa **con trỏ tới root node
mới nhất** và vài dữ liệu phụ trợ — ta gọi nó là **meta page**.

```
|     the_meta_page      | pages... | root_node | pages... | (end_of_file)
| root_ptr |  page_used  |              ^                        ^
     |            |                     |                        |
     +------------|---------------------+                        |
                  |                                              |
                  +----------------------------------------------+
```

Node mới chỉ đơn giản được **append vào như một log**. Nhưng ta **không thể dùng
kích thước file để đếm số page**, bởi vì sau khi mất điện, **kích thước file
(metadata) có thể không nhất quán với dữ liệu file**. Chuyện này phụ thuộc
filesystem; ta tránh nó bằng cách **lưu số page trong meta page**.

### `fsync` lên thư mục

Như đã nhắc ở chương 01, **phải dùng `fsync` lên thư mục cha sau khi rename**.
Điều này cũng đúng khi **tạo file mới**, vì có **2 thứ cần được làm bền vững**:
dữ liệu file, và thư mục tham chiếu tới file đó.

Ta sẽ chủ động `fsync` sau khi có khả năng đã tạo file mới bằng `O_CREATE`.
Để `fsync` một thư mục, hãy mở thư mục ở chế độ `O_RDONLY`.

```go
func createFileSync(file string) (int, error) {
    // lấy fd của thư mục
    flags := os.O_RDONLY | syscall.O_DIRECTORY
    dirfd, err := syscall.Open(path.Dir(file), flags, 0o644)
    if err != nil {
        return -1, fmt.Errorf("open directory: %w", err)
    }
    defer syscall.Close(dirfd)

    // mở hoặc tạo file
    flags = os.O_RDWR | os.O_CREATE
    fd, err := syscall.Openat(dirfd, path.Base(file), flags, 0o644)
    if err != nil {
        return -1, fmt.Errorf("open file: %w", err)
    }

    // fsync thư mục
    if err = syscall.Fsync(dirfd); err != nil {
        _ = syscall.Close(fd) // có thể để lại một file rỗng
        return -1, fmt.Errorf("fsync directory: %w", err)
    }
    return fd, nil
}
```

`dirfd` được dùng bởi `openat` để mở file đích, **đảm bảo file đến từ đúng thư
mục ta đã mở trước đó** — phòng khi đường dẫn thư mục bị thay thế ở giữa chừng
(race condition). Dù đây không phải mối lo của ta vì ta không kỳ vọng chạy
đa tiến trình.

### `mmap`, page cache và IO

`mmap` là cách **đọc/ghi một file như thể nó là một buffer trong bộ nhớ**.
IO đĩa diễn ra **ngầm và tự động** với `mmap`.

```go
func Mmap(fd int, offset int64, length int, ...) (data []byte, err error)
```

Để hiểu `mmap`, hãy ôn lại vài kiến thức cơ bản về hệ điều hành. **Một page của
OS là đơn vị nhỏ nhất để ánh xạ giữa địa chỉ ảo và địa chỉ vật lý.** Tuy nhiên,
không gian địa chỉ ảo của một tiến trình **không phải lúc nào cũng được bộ nhớ
vật lý hậu thuẫn hoàn toàn**; một phần bộ nhớ tiến trình có thể bị swap ra đĩa,
và khi tiến trình cố truy cập nó:

1. CPU kích hoạt một **page fault**, trao quyền điều khiển cho OS.
2. OS khi đó:
   1. Đọc dữ liệu đã swap vào bộ nhớ vật lý.
   2. Ánh xạ lại địa chỉ ảo tới nó.
   3. Trả quyền điều khiển lại cho tiến trình.
3. Tiến trình tiếp tục chạy với địa chỉ ảo đã được ánh xạ tới RAM thật.

`mmap` hoạt động theo cách tương tự: tiến trình nhận một dải địa chỉ từ `mmap`,
và khi nó chạm vào một page trong dải đó thì **page fault**, OS đọc dữ liệu vào
cache rồi ánh xạ lại page tới cache đó. **Đó là IO tự động trong kịch bản chỉ đọc.**

CPU cũng **ghi nhận lại (gọi là dirty bit)** khi tiến trình sửa một page, để OS
có thể ghi page đó xuống đĩa sau. **`fsync` được dùng để yêu cầu và chờ IO đó.**
Đây là cách ghi dữ liệu qua `mmap`; nó **không khác `write` là mấy trên Linux**,
vì `write` cũng đi vào chính page cache đó.

Bạn **không bắt buộc phải dùng `mmap`**, nhưng hiểu cái cơ bản này là quan trọng.

## 6.4 Quản lý page trên disk

Ta sẽ dùng `mmap` để cài đặt các callback quản lý page, đơn giản vì nó tiện.

```go
func (db *KV) Open() error {
    db.tree.get = db.pageRead   // đọc một page
    db.tree.new = db.pageAppend // append một page
    db.tree.del = func(uint64) {}
    // ...
}
```

### Gọi `mmap`

Một `mmap` được hậu thuẫn bởi file có thể là **read-only**, **read-write**, hoặc
**copy-on-write**. Để tạo mmap chỉ đọc, dùng cờ `PROT_READ` và `MAP_SHARED`.

```go
syscall.Mmap(fd, offset, size, syscall.PROT_READ, syscall.MAP_SHARED)
```

**Dải được ánh xạ có thể lớn hơn kích thước file hiện tại** — đây là một thực tế
ta khai thác được, vì file sẽ còn lớn lên.

### `mmap` một file đang lớn dần

`mremap` ánh xạ lại một vùng sang dải lớn hơn, giống như `realloc`. Đó là một
cách xử lý file đang lớn dần. Tuy nhiên **địa chỉ có thể thay đổi**, điều này
sẽ gây cản trở cho reader đồng thời ở các chương sau. Giải pháp của ta là
**thêm các ánh xạ mới để phủ phần file mở rộng ra**.

```go
type KV struct {
    // ...
    mmap struct {
        total  int      // kích thước mmap, có thể lớn hơn kích thước file
        chunks [][]byte // nhiều mmap, có thể không liên tục
    }
}

// `BTree.get`, đọc một page.
func (db *KV) pageRead(ptr uint64) []byte {
    start := uint64(0)
    for _, chunk := range db.mmap.chunks {
        end := start + uint64(len(chunk))/BTREE_PAGE_SIZE
        if ptr < end {
            offset := BTREE_PAGE_SIZE * (ptr - start)
            return chunk[offset : offset+BTREE_PAGE_SIZE]
        }
        start = end
    }
    panic("bad ptr")
}
```

> Bạn có thể thắc mắc: sao không tạo luôn một ánh xạ cực lớn (ví dụ 1TB) rồi
> quên chuyện file lớn dần đi, vì địa chỉ ảo chưa dùng tới thì chẳng tốn gì?
> **Cách đó ổn với một DB đồ chơi trên hệ 64-bit.**

### Thu gom các page được update

Callback `BTree.new` thu gom các page mới sinh ra từ việc update B+tree, và
**cấp phát số hiệu page từ cuối DB**.

```go
type KV struct {
    // ...
    page struct {
        flushed uint64   // kích thước database tính theo số page
        temp    [][]byte // các page vừa cấp phát
    }
}

func (db *KV) pageAppend(node []byte) uint64 {
    ptr := db.page.flushed + uint64(len(db.page.temp)) // chỉ việc append
    db.page.temp = append(db.page.temp, node)
    return ptr
}
```

Những page này được ghi (append) vào file **sau khi** B+tree update xong.

```go
func writePages(db *KV) error {
    // mở rộng mmap nếu cần
    size := (int(db.page.flushed) + len(db.page.temp)) * BTREE_PAGE_SIZE
    if err := extendMmap(db, size); err != nil {
        return err
    }
    // ghi các page dữ liệu vào file
    offset := int64(db.page.flushed * BTREE_PAGE_SIZE)
    if _, err := unix.Pwritev(db.fd, db.page.temp, offset); err != nil {
        return err
    }
    // bỏ dữ liệu trong bộ nhớ
    db.page.flushed += uint64(len(db.page.temp))
    db.page.temp = db.page.temp[:0]
    return nil
}
```

`pwritev` là một biến thể của `write` **có offset và nhận nhiều buffer đầu vào**.
Ta phải kiểm soát offset vì lát nữa còn phải ghi meta page. Nhiều buffer đầu vào
sẽ được kernel gộp lại.

## 6.5 Meta page

### Đọc meta page

Ta cũng thêm vài **magic byte** vào meta page để nhận dạng loại file.

```go
const DB_SIG = "BuildYourOwnDB06" // không tương thích giữa các chương

// | sig | root_ptr | page_used |
// | 16B |    8B    |    8B     |
func saveMeta(db *KV) []byte {
    var data [32]byte
    copy(data[:16], []byte(DB_SIG))
    binary.LittleEndian.PutUint64(data[16:], db.tree.root)
    binary.LittleEndian.PutUint64(data[24:], db.page.flushed)
    return data[:]
}
func loadMeta(db *KV, data []byte)
```

Meta page được **giữ chỗ trước** nếu file đang rỗng.

```go
func readRoot(db *KV, fileSize int64) error {
    if fileSize == 0 { // file rỗng
        db.page.flushed = 1 // meta page được khởi tạo ở lần ghi đầu tiên
        return nil
    }
    // đọc page
    data := db.mmap.chunks[0]
    loadMeta(db, data)
    // kiểm chứng page
    // ...
    return nil
}
```

### Update meta page

Ghi một lượng nhỏ dữ liệu đã căn theo page xuống đĩa thật, **chỉ sửa đúng một
sector**, thì **rất có khả năng là atomic ở mức phần cứng khi mất điện**.
Một số database thật **dựa vào điều này**. Ta cũng update meta page theo cách đó.

```go
// 3. Update meta page. Nó phải atomic.
func updateRoot(db *KV) error {
    if _, err := syscall.Pwrite(db.fd, saveMeta(db), 0); err != nil {
        return fmt.Errorf("write meta page: %w", err)
    }
    return nil
}
```

Tuy nhiên, **"atomic" mang nghĩa khác nhau ở từng mức**, như bạn đã thấy với
`rename`. **`write` không atomic so với reader đồng thời ở mức syscall.**
Đây nhiều khả năng là do cách page cache hoạt động.

Ta sẽ xét tới atomicity đọc/ghi khi thêm transaction đồng thời, nhưng ta **đã
thấy một giải pháp rồi**: trong LSM-tree, **tầng 1 là thứ duy nhất bị update**,
và nó được **nhân bản thành MemTable** — chuyển bài toán concurrency vào bộ nhớ.
Ta có thể **giữ một bản copy của meta page trong bộ nhớ và đồng bộ nó bằng mutex**,
nhờ vậy tránh được việc đọc/ghi đĩa đồng thời.

Ngay cả khi phần cứng **không** atomic khi mất điện, atomicity vẫn đạt được bằng
**log + checksum**. Ta có thể **luân phiên giữa 2 meta page có checksum** cho mỗi
lần update, để đảm bảo **ít nhất một trong hai còn tốt** sau khi mất điện.
Cái này gọi là **double buffering** — một log xoay vòng với 2 entry.

*(Sách dẫn link tới một [thread trên mailing list của PostgreSQL](https://www.postgresql.org/message-id/flat/17064-bb0d7904ef72add3%40postgresql.org)
và một [câu hỏi StackOverflow](https://stackoverflow.com/questions/35595685/).)*

## 6.6 Xử lý lỗi

### Các kịch bản sau khi IO lỗi

Mức tối thiểu của xử lý lỗi là **lan truyền lỗi lên** bằng `if err != nil`.
Tiếp theo, hãy xét khả năng **tiếp tục dùng DB sau khi IO lỗi** (`fsync` hoặc `write`).

- **Đọc sau một lần update thất bại?**
  - Lựa chọn hợp lý là **hành xử như thể chẳng có gì xảy ra**.
- **Update lại sau một lần thất bại?**
  - Nếu lỗi vẫn còn, **dự kiến sẽ thất bại tiếp**.
  - Nếu lỗi chỉ là tạm thời, **ta có khôi phục được từ lỗi trước đó không?**
- **Khởi động lại DB sau khi vấn đề được giải quyết?**
  - Đây chỉ là **crash recovery**; đã bàn ở chương 03.

### Quay về phiên bản trước

Có một [khảo sát](https://www.usenix.org/system/files/atc20-rebello.pdf) về việc
xử lý lỗi `fsync`. Từ đó ta học được rằng **chủ đề này phụ thuộc filesystem**.
Nếu ta đọc lại sau khi `fsync` lỗi, **một số filesystem trả về chính dữ liệu đã
thất bại**, vì page cache không khớp với đĩa. Nên **đọc lại những gì ghi hỏng là
chuyện có vấn đề**.

Nhưng vì ta dùng **copy-on-write**, đây **không phải vấn đề**; ta chỉ cần
**quay về root cũ của cây** để tránh phần dữ liệu có vấn đề. Root của cây được
lưu trong meta page, nhưng ta **không bao giờ đọc meta page từ đĩa sau khi đã mở
DB**, nên ta chỉ cần **revert con trỏ root trong bộ nhớ**.

```go
func (db *KV) Set(key []byte, val []byte) error {
    meta := saveMeta(db) // lưu trạng thái trong bộ nhớ (root của cây)
    db.tree.Insert(key, val)
    return updateOrRevert(db, meta)
}

func updateOrRevert(db *KV, meta []byte) error {
    // update 2 pha
    err := updateFile(db)
    // revert nếu lỗi
    if err != nil {
        // trạng thái trong bộ nhớ revert được ngay để vẫn cho phép đọc
        loadMeta(db, meta)
        // bỏ các dữ liệu tạm
        db.page.temp = db.page.temp[:0]
    }
    return err
}
```

Vậy là sau một lần ghi thất bại, **vẫn có thể dùng DB ở chế độ chỉ đọc**. Việc đọc
cũng có thể lỗi, nhưng ta đang dùng `mmap` — khi đọc lỗi thì **tiến trình bị kill
bằng `SIGBUS`**. Đó là **một trong những nhược điểm của `mmap`**.

### Khôi phục từ lỗi ghi tạm thời

Một số lỗi ghi chỉ là tạm thời, ví dụ **"no space left"**. Nếu một lần update
thất bại rồi lần sau thành công, **trạng thái cuối cùng vẫn tốt**. Vấn đề nằm ở
**trạng thái trung gian**: giữa 2 lần update đó, **nội dung meta page trên đĩa
là không xác định!**

Nếu `fsync` lỗi ở meta page, meta page trên đĩa có thể là **phiên bản mới hoặc
phiên bản cũ**, trong khi root của cây trong bộ nhớ là phiên bản cũ. Khi đó lần
update thành công thứ 2 sẽ **ghi đè lên các page dữ liệu của phiên bản mới hơn**,
và nếu crash thì có thể để lại **trạng thái trung gian hỏng**.

Giải pháp là **ghi lại meta page đã biết là tốt gần nhất khi recovery**.

```go
type KV struct {
    // ...
    failed bool // lần update trước có thất bại không?
}

func updateOrRevert(db *KV, meta []byte) error {
    // đảm bảo meta page trên đĩa khớp với bản trong bộ nhớ sau khi có lỗi
    if db.failed {
        // ghi và fsync meta page trước đó
        // ...
        db.failed = false
    }

    err := updateFile(db)
    if err != nil {
        // meta page trên đĩa đang ở trạng thái không xác định;
        // đánh dấu để ghi lại ở lần recovery sau.
        db.failed = true
        // ...
    }
    return err
}
```

Ta **dựa vào filesystem để báo lỗi cho đúng**, nhưng
[có bằng chứng](https://danluu.com/filesystem-errors/) rằng **chúng không làm vậy**.
Nên liệu toàn bộ hệ thống có xử lý lỗi đúng hay không thì **vẫn còn đáng ngờ**.

## 6.7 Tóm tắt về KV store append-only

- Bố cục file cho B+tree copy-on-write.
- Durability và atomicity bằng `fsync`.
- Xử lý lỗi.

**B+tree trên disk là một bước tiến lớn.** Ta chỉ cần thêm **free list** nữa là
nó trở nên thực dụng.

---

## 📌 Đối chiếu với PostgreSQL *(ghi chú thêm, không có trong sách)*

- **Meta page** của sách tương ứng **`pg_control`** của Postgres — file nhỏ chứa
  trạng thái toàn cục, và Postgres cũng dựa vào **tính atomic ở mức sector** cho nó.
  Chính thread mailing list mà sách dẫn là của Postgres.
- **Double buffering** (luân phiên 2 meta page có checksum) chính là cách SQLite
  và nhiều DB khác làm. Postgres thay vào đó có **CRC trong `pg_control`**.
- **Postgres KHÔNG dùng `mmap`** cho dữ liệu — nó có **shared buffer pool** tự
  quản lý (`shared_buffers`), với clock-sweep để chọn page nạn nhân. Lý do đúng
  như sách nói: `mmap` không kiểm soát được thời điểm ghi, lỗi đọc thì
  **`SIGBUS` giết tiến trình**, và không điều phối được việc ghi với WAL.
- Thứ tự **"ghi node mới → fsync → ghi root → fsync"** của sách chính là tinh thần
  của quy tắc **WAL**: *log record phải xuống đĩa trước page dữ liệu*. Postgres
  cài nó bằng cách so `pd_lsn` của page với LSN đã flush.
- Chuyện `fsync` lỗi ở mục 6.6 chính là **fsyncgate**; từ PG 12 Postgres **panic**
  khi `fsync` thất bại, vì không thể revert kiểu COW như sách làm được.
