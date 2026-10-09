# Ch.01 — From Files To Databases

Câu hỏi của chương: *tại sao không lưu dữ liệu vào một file JSON là xong?*

## Ba cách ghi file, và cái giá của mỗi cách

**1. Ghi đè tại chỗ** (`O_TRUNC` + write + fsync)
- Phải đọc toàn bộ vào RAM, sửa, ghi lại → chỉ dùng được cho dữ liệu nhỏ.
- Crash giữa chừng → file cụt hoặc nửa cũ nửa mới, **mất sạch**.
- Reader đọc đúng lúc đang ghi → thấy dữ liệu rách.

**2. Ghi file tạm rồi `rename`**
- Filesystem map tên file → dữ liệu, nên rename chỉ đổi con trỏ, không đụng dữ liệu cũ.
- Chi phí hằng số bất kể file to cỡ nào.
- Crash giữa chừng → file cũ còn nguyên, khôi phục được.

**3. Append-only log**
- Chỉ nối thêm, không bao giờ ghi đè → dữ liệu cũ luôn an toàn.
- Nhưng: không có index (reader phải đọc hết), và không thu hồi được chỗ của dữ liệu đã xoá.
- → log một mình không đủ làm DB, phải ghép với một cấu trúc index.

## Hai bài học đắt giá

**"Atomic" luôn phải hỏi: atomic *so với cái gì*?**

`rename` là atomic **với reader** (reader thấy hoặc file cũ hoặc file mới, không
bao giờ thấy nửa vời), nhưng **không** atomic với mất điện, và tự nó cũng chưa
durable — còn phải `fsync` lên **thư mục cha** nữa, vì thư mục cũng chỉ là một
cái map tên→file và cũng cần fsync như dữ liệu thường.

**Torn write và checksum**

Log không làm hỏng dữ liệu cũ, nhưng entry cuối cùng có thể bị ghi dở khi crash.
Ba khả năng: (a) append không xảy ra, (b) entry ghi được một nửa, (c) file dài ra
nhưng entry không có ở đó. Cách xử lý: **mỗi entry mang một checksum** — checksum
sai thì coi như update đó chưa từng xảy ra. Đây chính là thứ làm log update trở
thành atomic.

Lưu ý: checksum chống được **torn write** (ghi dở trước khi fsync thành công).
Hỏng sau khi đã fsync thì DB không cứu được.

**fsync có thể thất bại — và còn tệ hơn thế**

Nếu `fsync` lỗi thì update coi như hỏng. Nhưng nếu đọc lại file ngay sau đó,
bạn **vẫn có thể thấy dữ liệu mới** (do OS page cache), tuỳ filesystem. Đây là
cái bẫy nổi tiếng "fsyncgate" mà Postgres từng dính.

## Đối chiếu Postgres

- Postgres **không** dùng chiêu rename; nó sửa page tại chỗ và dựa vào **WAL**.
- Nhưng ý tưởng checksum thì có: WAL record có CRC, và page có `pd_checksum`
  (bật bằng `data_checksums`).
- Vấn đề "ghi nửa page" (torn page) Postgres giải bằng **full-page write**: sau mỗi
  checkpoint, lần đầu tiên một page bị sửa thì ghi nguyên cả page vào WAL.
- `fsync` thất bại: từ PG 12, Postgres **panic** luôn thay vì thử lại, chính vì cái
  bẫy page cache ở trên.

## Còn nợ lại sau chương này

1. Cấu trúc index trên disk và cách update nó.
2. Thu hồi chỗ trống từ file append-only.
3. Cách ghép log với cấu trúc index.
4. Concurrency.
