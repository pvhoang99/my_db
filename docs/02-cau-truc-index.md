# Ch.02 — Indexing Data Structures

## Ba loại truy vấn

1. **Scan** toàn bộ — không dùng index, `O(N)` dù có tối ưu kiểu column store.
2. **Point query** — tìm theo một key cụ thể.
3. **Range query** — tìm theo khoảng, đòi hỏi index **có thứ tự**.

Range query = **seek** (tìm key bắt đầu) + **iterate** (đi tới key kế tiếp theo thứ tự).
Point query chỉ là seek mà không iterate.

→ Hệ quả: chỉ cần **một cấu trúc dữ liệu có sắp xếp** là phục vụ được cả ba.
Đây là lý do hashtable không đủ: nó làm point query tốt nhưng không có thứ tự.

## Hai lựa chọn: B+tree và LSM-tree

**LSM-tree** giải quyết sẵn mấy vấn đề còn nợ ở ch.01 (update cấu trúc trên disk,
tái sử dụng chỗ trống) nhờ cơ chế ghi tuần tự rồi merge dần.
- Level chia thành nhiều file nhỏ không chồng lấn gọi là **SSTable**, để merge
  được từng phần thay vì merge cả khối lớn một lúc.
- Level 1 nhỏ và có giới hạn kích thước nên dùng được **log** — đây chính là
  ví dụ "ghép log với cấu trúc index" mà ch.01 nhắc tới.
- Dữ liệu log được nhân bản trong RAM thành **MemTable** (B-tree/skiplist tuỳ ý)
  để đọc nhanh phần mới ghi.

**B+tree** thì vẫn còn nguyên những vấn đề đó — và sách sẽ dành các chương sau
để giải. Đó là con đường sách chọn.

## Đối chiếu Postgres

- Postgres dùng **B+tree** (`nbtree`) làm index mặc định, không dùng LSM-tree.
- Khác sách: ở Postgres, **dữ liệu không nằm trong B+tree**. Leaf node của index
  chỉ chứa key + **TID** (page, slot) trỏ sang heap file. Sách thì nhét luôn
  value vào leaf (clustered index, giống SQLite/InnoDB).
- Hệ quả thực tế của khác biệt này: Postgres có **index-only scan** (chỉ khi
  visibility map cho phép) và có hiện tượng **HOT update**; còn DB kiểu sách thì
  đọc theo primary key luôn nhanh vì không phải nhảy sang heap.
