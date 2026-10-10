# Chương 11 — Transaction Atomic
*(Atomic Transactions)*

## 11.1 Hiệu ứng được-ăn-cả-ngã-về-không

Secondary index ở chương trước đòi hỏi **update nhiều key một cách atomic**.
Điều này **không chỉ cần cho tính nhất quán nội bộ của DB**, mà còn **hữu ích cho
tính nhất quán dữ liệu của ứng dụng** — hãy nghĩ tới số dư tài khoản so với các
giao dịch của tài khoản.

Ta sẽ **bỏ giao diện get-set-del** và thêm một giao diện mới cho phép **thực thi
atomic cả một nhóm thao tác**. Concurrency sẽ bàn ở chương sau.

### Commit và rollback

Ta thêm giao diện để **đánh dấu điểm bắt đầu và kết thúc** của transaction.
Ở điểm kết thúc, các update **hoặc có hiệu lực (commit)**, **hoặc bị bỏ đi
(rollback)** do lỗi hoặc do người dùng yêu cầu (`Abort`).

```go
// bắt đầu một transaction
func (kv *KV) Begin(tx *KVTX)

// kết thúc transaction: commit các update; rollback nếu có lỗi
func (kv *KV) Commit(tx *KVTX) error

// kết thúc transaction: rollback
func (kv *KV) Abort(tx *KVTX)
```

### Atomicity nhờ copy-on-write

Với copy-on-write, **cả commit lẫn rollback đều chỉ là việc update root pointer**.
Cái này **đã được cài rồi** dưới dạng xử lý lỗi ở chương 06.

```go
type KVTX struct {
    db   *KV
    meta []byte // để rollback
}

func (kv *KV) Begin(tx *KVTX) {
    tx.db = kv
    tx.meta = saveMeta(tx.db)
}

func (kv *KV) Commit(tx *KVTX) error {
    return updateOrRevert(tx.db, tx.meta)
}

func (kv *KV) Abort(tx *KVTX) {
    // chưa ghi gì cả, chỉ cần revert trạng thái trong bộ nhớ
    loadMeta(tx.db, tx.meta)
    // bỏ các dữ liệu tạm
    tx.db.page.nappend = 0
    tx.db.page.updates = map[uint64][]byte{}
}
```

Trước đây, `updateOrRevert()` được gọi **sau mỗi lần update một key**. Giờ nó
được **chuyển vào `KVTX.Commit()`**. B+tree có thể được update **bao nhiêu lần
tuỳ ý** — **cái quan trọng là root pointer**.

```go
// chương TRƯỚC!!!
func (db *KV) Update(req *UpdateReq) (bool, error) {
    meta := saveMeta(db)
    if !db.tree.Update(req) {
        return false, nil
    }
    err := updateOrRevert(db, meta)
    return err == nil, err
}
```

### Cách thay thế: atomicity nhờ logging

Trong cây copy-on-write, **các update được gói gọn bởi root pointer** — khác với
update tại chỗ, nơi **bắt buộc phải có log** để ghi lại các update.

Log được dùng để **rollback các update nếu transaction bị huỷ**. Vấn đề là
**lỗi IO ngăn không cho update tiếp**, nên việc rollback **phải để cho cơ chế
recovery lo** — điều này cũng đúng với copy-on-write (xem `updateOrRevert`).

Update được coi là **durable ngay khi đã `fsync` vào log**. Nên DB có thể
**trả về thành công cho client chỉ sau 1 lần `fsync`**, miễn là log **được xét
tới khi truy vấn** và **cuối cùng được merge vào kho dữ liệu chính**.

## 11.2 Giao diện có transaction

### Chuyển thao tác cây vào transaction

Các thao tác trên cây giờ **gắn với một transaction**, nên chúng được chuyển vào `KVTX`.

```go
func (tx *KVTX) Seek(key []byte, cmp int) *BIter {
    return tx.db.tree.Seek(key, cmp)
}
func (tx *KVTX) Update(req *UpdateReq) bool {
    return tx.db.tree.Update(req)
}
func (tx *KVTX) Del(req *DeleteReq) bool {
    return tx.db.tree.Delete(req)
}
```

Lưu ý rằng những hàm này **không còn trả về lỗi nữa**, vì việc **update disk
thật sự đã chuyển vào `KVTX.Commit()`**.

### Thao tác bảng có transaction

Với giao diện dựa trên bảng, chỉ cần **thêm một kiểu bọc quanh `KVTX`**.

```go
type DBTX struct {
    kv KVTX
    db *DB
}

func (db *DB) Begin(tx *DBTX)
func (db *DB) Commit(tx *DBTX) error
func (db *DB) Abort(tx *DBTX)
```

Rồi chuyển các thao tác bảng vào lớp bọc đó.

```go
func (tx *DBTX) Scan(table string, req *Scanner) error
func (tx *DBTX) Set(table string, rec Record, mode int) (bool, error)
func (tx *DBTX) Delete(table string, rec Record) (bool, error)
```

Những thao tác này **không còn phải xử lý lỗi IO**, nên **không cần xử lý lỗi khi
update secondary index** nữa.

## 11.3 Những tối ưu tuỳ chọn

Một relational DB chạy được là **một cột mốc lớn**, dù nó mới chỉ hỗ trợ thao tác
tuần tự. Muốn thử thách thêm thì có vài tối ưu đáng cân nhắc.

### Giảm việc copy khi update nhiều key

Copy-on-write **copy các node từ leaf lên root trong một lần update**. Cách này
**chưa tối ưu khi update nhiều key**, bởi vì các node của những cây trung gian
bị **cấp phát, update đúng một lần, rồi xoá luôn** ngay trong cùng một transaction.

**Tối ưu**: chỉ **copy một node đúng một lần trong một transaction**, và dùng
**update tại chỗ** trên những node đã copy.

### Range delete

Giờ ta đã update được nhiều key. Nhưng **xoá một số lượng lớn key**, chẳng hạn
**drop một bảng**, vẫn là vấn đề về mặt tài nguyên. Cách ngây thơ để drop bảng là
**duyệt và xoá từng key một**. Cách này **đọc toàn bộ bảng vào bộ nhớ** và
**làm việc vô ích**, vì các node bị update đi update lại trước khi bị xoá.

Một số DB **dùng file riêng cho mỗi bảng**, nên không gặp vấn đề này. Còn ở ta,
**một B+tree duy nhất chứa mọi thứ**, nên ta có thể cài một thao tác
**range delete** — **giải phóng toàn bộ leaf node trong một khoảng mà thậm chí
không cần nhìn vào chúng**.

### Nén tiền tố chung

Trong bất kỳ dữ liệu đã sắp xếp nào, **các key gần nhau rất có khả năng dùng chung
một tiền tố**. Và trong cách dùng relational DB điển hình, **key nhiều cột cũng
sinh ra tiền tố chung**. Vậy nên có **cơ hội nén key bên trong một node**.

**Prefix compression** làm việc cài đặt khó hơn (và vui hơn), đặc biệt là khi
**kích thước node trở nên khó dự đoán** cho việc merge và split.

---

## 📌 Đối chiếu với PostgreSQL *(ghi chú thêm, không có trong sách)*

- `Begin`/`Commit`/`Abort` của sách = `BEGIN`/`COMMIT`/`ROLLBACK`. Nhưng cơ chế
  bên dưới **khác hẳn**: sách commit bằng **một phép ghi root pointer**;
  Postgres commit bằng cách **ghi một WAL record `COMMIT` rồi fsync**, và đánh
  dấu transaction là committed trong **`pg_xact` (CLOG)**.
- **Rollback ở Postgres gần như miễn phí**: không cần hoàn tác gì cả — tuple do
  transaction đó tạo ra mang `xmin` của nó, và vì transaction không bao giờ được
  đánh dấu commit nên **không ai nhìn thấy chúng**. Rác để **VACUUM** dọn sau.
  Đây là hệ quả trực tiếp của MVCC kiểu xmin/xmax.
- Mục **"atomicity nhờ logging"** của sách mô tả đúng hành vi Postgres: **1 lần
  `fsync` WAL là đủ để trả về thành công** cho client (và `synchronous_commit = off`
  bỏ luôn cả lần fsync đó, đổi durability lấy tốc độ).
- Tối ưu **"chỉ copy một node một lần trong một transaction"** mà sách đề xuất
  chính là thứ Postgres có sẵn nhờ **buffer pool**: page được **pin trong bộ nhớ
  và sửa tại chỗ nhiều lần**, chỉ ghi xuống disk một lần.
- **Range delete** tương ứng với `TRUNCATE` / `DROP TABLE` của Postgres — chỉ
  việc **unlink file `relfilenode`**, `O(1)`, không đụng tới một tuple nào.
- **Prefix compression**: Postgres không nén tiền tố trong B-tree, nhưng từ
  **PG 13** có **B-tree deduplication** — gom các TID có cùng key vào một
  **posting list**, giải quyết đúng vấn đề lãng phí chỗ mà sách nêu.
