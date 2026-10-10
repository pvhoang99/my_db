# Chương 01 — Từ File đến Database
*(From Files To Databases)*

Hãy bắt đầu từ file, và xem xét những thách thức ta gặp phải.

## 1.1 Update file tại chỗ

Giả sử bạn cần lưu một ít dữ liệu xuống đĩa; đây là cách làm điển hình:

```go
func SaveData1(path string, data []byte) error {
    fp, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0664)
    if err != nil {
        return err
    }
    defer fp.Close()
    _, err = fp.Write(data)
    if err != nil {
        return err
    }
    return fp.Sync() // fsync
}
```

Đoạn code này tạo file nếu nó chưa tồn tại, hoặc **cắt cụt (truncate)** file cũ
trước khi ghi nội dung mới. Và quan trọng nhất: **dữ liệu không bền vững cho tới
khi bạn gọi `fsync`** (`fp.Sync()` trong Go).

Cách này có vài hạn chế nghiêm trọng:

1. Nó update **toàn bộ nội dung một lúc**; chỉ dùng được cho dữ liệu nhỏ xíu.
   Đây chính là lý do bạn không dùng Excel làm database.
2. Nếu cần update file cũ, bạn phải **đọc hết vào bộ nhớ**, sửa, rồi **ghi đè**
   lên file cũ. Chuyện gì xảy ra nếu ứng dụng crash **giữa lúc đang ghi đè**?
3. Nếu ứng dụng cần truy cập dữ liệu **đồng thời**, làm sao ngăn reader đọc phải
   dữ liệu lẫn lộn, và ngăn các writer xung đột với nhau? Đó là lý do hầu hết
   database đều theo mô hình **client-server** — bạn cần một server để điều phối
   các client đồng thời. *(Concurrency còn phức tạp hơn nữa khi không có server,
   xem SQLite.)*

## 1.2 Đổi tên file một cách atomic

### Thay thế dữ liệu atomic bằng cách rename file

Rất nhiều vấn đề được giải quyết chỉ bằng cách **không update dữ liệu tại chỗ**.
Bạn ghi ra một file mới rồi xoá file cũ.

Không đụng tới dữ liệu của file cũ nghĩa là:

1. Nếu việc update bị gián đoạn, bạn vẫn khôi phục được từ file cũ vì nó còn nguyên vẹn.
2. Reader đồng thời sẽ không đọc phải dữ liệu ghi dở.

Vấn đề là **làm sao reader tìm được file mới**. Một pattern phổ biến là **rename**
file mới thành đường dẫn của file cũ.

```go
func SaveData2(path string, data []byte) error {
    tmp := fmt.Sprintf("%s.tmp.%d", path, randomInt())
    fp, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0664)
    if err != nil {
        return err
    }
    defer func() {
        fp.Close()
        if err != nil {
            os.Remove(tmp)
        }
    }()

    _, err = fp.Write(data)
    if err != nil {
        return err
    }
    err = fp.Sync() // fsync
    if err != nil {
        return err
    }
    return os.Rename(tmp, path)
}
```

Rename một file đè lên file đã tồn tại sẽ **thay thế nó một cách atomic**; không
cần xoá file cũ (và xoá là không đúng).

> ⚠️ Hãy chú ý tới nghĩa của thuật ngữ. Mỗi khi bạn thấy câu **"X là atomic"**,
> bạn phải hỏi ngay: **"X atomic so với cái gì?"**

Trong trường hợp này:

- Rename là atomic **so với reader đồng thời**; một reader sẽ mở được hoặc file cũ,
  hoặc file mới.
- Rename **KHÔNG** atomic **so với mất điện**; thậm chí nó còn chưa durable.
  Bạn cần thêm một lệnh **`fsync` lên thư mục cha** — chuyện này sẽ bàn sau.

### Tại sao rename lại hoạt động được?

Filesystem lưu một **ánh xạ từ tên file sang dữ liệu file**, nên việc thay thế
file bằng rename chỉ đơn giản là **trỏ cái tên sang dữ liệu mới**, mà không đụng
gì tới dữ liệu cũ. Đó là lý do atomic rename khả thi trong filesystem. Và chi phí
của thao tác này là **hằng số**, bất kể dữ liệu lớn cỡ nào.

Trên Linux, file cũ bị thay thế **vẫn có thể còn tồn tại** nếu nó đang được một
reader mở; nó chỉ không còn truy cập được qua tên file nữa. Reader có thể làm
việc an toàn trên bất cứ phiên bản dữ liệu nào nó đang giữ, còn writer thì không
bị reader chặn. Tuy nhiên, vẫn phải có cách ngăn **nhiều writer đồng thời**.
Mức độ concurrency ở đây là **multi-reader-single-writer** (nhiều đọc, một ghi),
và đó cũng chính là thứ chúng ta sẽ cài đặt.

## 1.3 Log chỉ ghi thêm (append-only)

### Update tăng dần một cách an toàn bằng log

Một cách để update tăng dần là **chỉ nối thêm (append)** các bản update vào cuối
file. Cái này gọi là **"log"** vì nó chỉ ghi thêm. Nó an toàn hơn update tại chỗ
vì **không dữ liệu nào bị ghi đè**; bạn luôn khôi phục được dữ liệu cũ sau khi crash.

Reader phải xét **toàn bộ** các entry trong log khi dùng nó. Ví dụ, đây là một KV
dựa trên log với 4 entry:

```
      0         1         2        3
| set a=1 | set b=2 | set a=3 | del b |
```

Trạng thái cuối cùng là `a=3`.

Log là thành phần thiết yếu của nhiều database. Tuy nhiên, log **chỉ là mô tả của
từng lần update**, điều đó có nghĩa là:

- Nó **không phải cấu trúc index**; reader phải đọc hết mọi entry.
- Nó **không có cách nào thu hồi chỗ trống** từ dữ liệu đã bị xoá.

Vậy nên **log một mình là không đủ** để build một DB; nó phải được **kết hợp với
một cấu trúc index khác**.

### Update log atomic bằng checksum

Tuy log không làm hỏng dữ liệu cũ, bạn vẫn phải xử lý **entry cuối cùng** nếu nó
bị hỏng sau khi crash. Có nhiều khả năng:

1. Lệnh append cuối cùng đơn giản là **không xảy ra**; log vẫn tốt.
2. Entry cuối cùng **bị ghi được một nửa**.
3. **Kích thước** của log tăng lên nhưng entry cuối cùng **không có ở đó**.

Cách xử lý mọi trường hợp này là **thêm một checksum vào mỗi entry của log**.
Nếu checksum sai, thì coi như lần update đó **chưa từng xảy ra** — điều này làm
cho việc update log trở nên **atomic** (cả so với reader lẫn so với durability).

Tình huống này nói về **ghi dở dang (incomplete write)** — thuật ngữ DB gọi là
**torn write** — xảy ra **trước** khi `fsync` thành công. Checksum cũng phát hiện
được các dạng hỏng dữ liệu khác xảy ra **sau** `fsync`, nhưng đó là thứ mà một DB
không thể khôi phục được.

## 1.4 Những cái bẫy của `fsync`

Sau khi rename file hoặc tạo file mới, bạn **phải gọi `fsync` lên thư mục cha**.
Một thư mục cũng chỉ là một ánh xạ từ tên file sang file, và giống như dữ liệu
file, nó **không bền vững cho tới khi bạn `fsync`**.
*(Xem ví dụ: [slide OSDI'14, trang 31](https://www.usenix.org/sites/default/files/conference/protected-files/osdi14_slides_pillai.pdf#page=31))*

Một vấn đề khác với `fsync` là **xử lý lỗi**. Nếu `fsync` thất bại thì lần update
của DB coi như thất bại — nhưng nếu bạn **đọc lại file ngay sau đó** thì sao?
Bạn **vẫn có thể nhận được dữ liệu mới dù `fsync` đã lỗi** (vì OS page cache)!
Hành vi này phụ thuộc vào filesystem.
*(Xem: [USENIX ATC'20 — Rebello et al.](https://www.usenix.org/conference/atc20/presentation/rebello))*

## 1.5 Tóm tắt những thách thức của database

**Những gì ta đã học:**

1. Vấn đề của việc update tại chỗ.
   - Tránh update tại chỗ bằng cách rename file.
   - Tránh update tại chỗ bằng cách dùng log.
2. Log append-only.
   - Update tăng dần.
   - Chưa phải giải pháp trọn vẹn; không có index và không tái sử dụng được chỗ trống.
3. Cách dùng `fsync`.

**Những gì còn là câu hỏi:**

1. Các cấu trúc dữ liệu để đánh index, và cách update chúng.
2. Tái sử dụng chỗ trống từ file append-only.
3. Kết hợp log với một cấu trúc index.
4. Concurrency.

---

## 📌 Đối chiếu với PostgreSQL *(ghi chú thêm, không có trong sách)*

- Postgres **không** dùng chiêu rename file; nó **sửa page tại chỗ** và dựa vào **WAL**.
- Ý tưởng checksum thì có: WAL record có CRC, và page có `pd_checksum`
  (bật bằng tham số `data_checksums`).
- Vấn đề ghi dở nửa page (**torn page**) được Postgres giải bằng **full-page write**:
  sau mỗi checkpoint, lần đầu tiên một page bị sửa thì ghi **nguyên cả page** vào WAL.
- Cái bẫy `fsync` thất bại ở mục 1.4 chính là sự cố **"fsyncgate"** nổi tiếng mà
  Postgres từng dính. Từ PostgreSQL 12, khi `fsync` lỗi thì Postgres **panic** luôn
  thay vì thử lại, chính vì không thể tin vào page cache nữa.
