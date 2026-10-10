# Chương 00 — Giới thiệu

> *Build Your Own Database From Scratch in Go*, James Smith, 2nd Edition (2024-06-11).
> Bản dịch tiếng Việt để học, chỉ dùng cá nhân.

## Làm chủ nền tảng bằng cách tự build một DB

### Học cái gì?

Những hệ thống phức tạp như database thực ra được xây trên vài nguyên lý đơn giản.

1. **Atomicity & durability.** Một DB không chỉ là file!
   - Lưu dữ liệu bền vững bằng `fsync`.
   - Khôi phục sau khi crash (crash recovery).
2. **KV store dựa trên B-tree.**
   - Cấu trúc dữ liệu trên disk.
   - Quản lý chỗ trống bằng free list.
3. **Relational DB xây trên nền KV.**
   - Table và index được map xuống B-tree mức thấp như thế nào.
   - Ngôn ngữ truy vấn kiểu SQL; parser & interpreter.
4. **Concurrency control** cho transaction.

### Code một database trong 3000 dòng, từng bước một

Thật đáng kinh ngạc là một chủ đề thú vị và rộng như vậy lại gói gọn được trong
3000 dòng code. Bạn có thể đã từng làm dự án lớn hơn, nhưng không phải kinh
nghiệm nào cũng ngang nhau.

| Số dòng (luỹ kế) | Bước |
|---|---|
| 366 | Cấu trúc dữ liệu B+tree |
| 601 | KV append-only |
| 731 | KV thực dụng với free list |
| 1107 | Table trên nền KV |
| 1294 | Range query |
| 1438 | Secondary index |
| 1461 | Giao diện transaction |
| 1702 | Concurrency control |
| 2795 | Ngôn ngữ truy vấn kiểu SQL |

### Học bằng cách làm: nguyên lý thay vì thuật ngữ

Tài liệu về database đầy rẫy thuật ngữ rối rắm, bị dùng chồng chéo và không có
nghĩa nhất quán. Rất dễ lạc lối khi đọc về chúng. Mặt khác, Feynman từng nói:
*"cái gì tôi không build được thì tôi không hiểu"*. Bạn có build được một database
chỉ bằng cách đọc về database không? Hãy tự kiểm tra sự hiểu của mình!

Có rất nhiều thứ để học, nhưng không phải kiến thức nào cũng quan trọng ngang nhau.
Chỉ cần vài nguyên lý là build được một DB, nên ai cũng thử được.

## Chủ đề 1: durability và atomicity

### Nhiều hơn một định dạng dữ liệu

Điện thoại thông minh dùng SQLite (một DB dạng file) rất nhiều. Tại sao lại lưu
dữ liệu trong SQLite thay vì một định dạng khác, ví dụ JSON? Bởi vì bạn có nguy
cơ **mất dữ liệu** nếu máy crash giữa lúc đang update. File có thể bị ghi dở dang,
bị cắt cụt, hoặc thậm chí biến mất.

Có những kỹ thuật để sửa chuyện này, và chính chúng dẫn tới database.

### Durability và atomicity với `fsync`

- **Atomicity** nghĩa là dữ liệu **hoặc đã được update, hoặc chưa** — không có
  trạng thái lưng chừng ở giữa.
- **Durability** nghĩa là dữ liệu được **đảm bảo còn tồn tại** sau một thời điểm nào đó.

Hai thứ này không phải hai mối quan tâm tách rời, vì ta phải đạt được cả hai.

Thứ đầu tiên cần học là syscall **`fsync`**. Một lệnh ghi file **không** xuống tới
đĩa ngay lập tức; có nhiều tầng đệm ở giữa (OS page cache, và cả RAM trên chính
thiết bị). `fsync` ép toàn bộ dữ liệu đang chờ xuống đĩa và đợi cho tới khi xong.
Điều này làm cho việc ghi trở nên **durable** — nhưng còn **atomicity** thì sao?

## Chủ đề 2: cấu trúc dữ liệu để đánh index

### Kiểm soát độ trễ và chi phí bằng index

DB biến một câu truy vấn thành kết quả mà người dùng không cần biết nó làm cách nào.
Nhưng kết quả không phải mối quan tâm duy nhất — **độ trễ** và **chi phí**
(bộ nhớ, IO, tính toán) cũng quan trọng. Đó là lý do có sự phân biệt giữa
**analytical (OLAP)** và **transactional (OLTP)**.

- **OLAP** có thể đụng tới lượng dữ liệu rất lớn, với các phép aggregation hoặc join.
  Việc đánh index có thể hạn chế hoặc không có.
- **OLTP** đụng tới lượng dữ liệu nhỏ thông qua index. Độ trễ và chi phí thấp.

Chữ "transactional" ở đây **không** liên quan gì tới DB transaction — chỉ là một
thuật ngữ buồn cười.

### Cấu trúc dữ liệu trong RAM vs. trên disk

Đưa một cấu trúc index xuống disk thì gặp thêm nhiều thách thức.
*(Xem cuốn "Build Your Own Redis" của cùng tác giả cho một DB trong bộ nhớ,
dễ hơn nhiều.)*

Một trong những vấn đề là **update dữ liệu trên disk tại chỗ**, vì bạn phải xử lý
các trạng thái hỏng sau khi crash. **Đĩa không chỉ đơn giản là RAM chậm hơn.**

Chữ R trong RAM là "random" (ngẫu nhiên) — và đó là một vấn đề khác với dữ liệu
trên disk, bởi **truy cập ngẫu nhiên chậm hơn nhiều so với truy cập tuần tự**,
kể cả trên SSD. Vì vậy những cấu trúc như cây nhị phân là **không khả thi**, trong
khi **B-tree và LSM-tree** thì ổn. Truy cập đồng thời vào cấu trúc dữ liệu cũng
là một chủ đề cần bàn.

## Chủ đề 3: Relational DB trên nền KV

### Hai tầng giao diện của DB

SQL gần như đồng nghĩa với database. Nhưng **SQL chỉ là giao diện người dùng**,
nó không phải thứ cốt lõi của một DB. Cái quan trọng là những chức năng nằm bên dưới.

Một giao diện khác đơn giản hơn nhiều là **key-value (KV)**. Bạn có thể `get`,
`set`, `delete` một key, và quan trọng nhất là **liệt kê một khoảng key theo
thứ tự đã sắp xếp**. KV đơn giản hơn SQL vì nó thấp hơn một tầng. Relational DB
được xây **trên nền** những giao diện kiểu KV, gọi là **storage engine**.

### Ngôn ngữ truy vấn: parser và interpreter

Bước cuối cùng thực ra dễ, dù số dòng code nhiều hơn. Cả parser lẫn interpreter
đều được viết **chỉ bằng đệ quy**! Bài học này áp dụng được cho gần như mọi ngôn
ngữ máy tính, hoặc khi bạn muốn tự tạo một ngôn ngữ lập trình / DSL riêng.
*(Xem cuốn "From Source Code To Machine Code" để thử thách thêm.)*
