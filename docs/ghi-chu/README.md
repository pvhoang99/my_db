# Ghi chú học

Ghi chú tự viết trong quá trình đọc sách — **khác với `docs/sach/`** (bản dịch
sách, gitignore vì bản quyền). Thư mục này là kiến thức nền và suy nghĩ của
riêng mình, nên commit được.

Đọc theo thứ tự: mỗi file dựa trên file trước.

| # | File | Nội dung | Liên quan tới sách |
|---|---|---|---|
| 00 | [Đĩa là gì, HDD vs SSD](00-dia-hdd-ssd.md) | Sector, cơ học HDD, flash SSD, vì sao đọc ngẫu nhiên chậm | Nền móng, mục 2.4 |
| 01 | [File là gì](01-file-la-gi.md) | File = tên + inode + dữ liệu; file descriptor; "mọi thứ là file" | Nền móng |
| 02 | [Thư mục là gì](02-thu-muc-la-gi.md) | Bảng tên→inode, `.` và `..`, đường dẫn, hard/symlink, mount | Mục 1.2 |
| 03 | [Nền tảng filesystem](03-nen-tang-file-he-thong.md) | inode, page cache, fsync, rename, phân tích crash | Mục 1.1–1.2 |
| 04 | [Cây nhị phân vs B+tree](04-bst-vs-btree.md) | Vì sao BST không dùng được trên đĩa; fanout tính thế nào; cấu trúc thật 3 tầng | Mục 2.3–2.4 |
| 05 | [Append-only log](05-append-only-log.md) | Ghi thay đổi thay vì trạng thái; torn write; checksum và cái bẫy crc32(b'')==0 | Mục 1.3 |
| 06 | [Ghi file ngoài đời](06-ghi-file-ngoai-doi.md) | 3 hàm SaveData; vì sao `O_TRUNC`; chương trình thật ghi file thế nào | Mục 1.1, 1.2, 1.4 |
| 07 | [B+tree vs LSM-tree](07-btree-vs-lsm.md) | Binary search vs B+tree trên đĩa; LSM qua ví dụ danh bạ; tombstone; abstract KV | Chương 2 |
| 08 | [Split và copy-on-write](08-btree-split-va-copy-on-write.md) | Mảng lồng nhau, split/merge, crash giữa split, COW chỉ chép đường đi | Mục 3.1–3.4 |
| 09 | [Node format và meta page](09-node-format-va-meta-page.md) | Node tận byte, 3 callback, update 2 pha, meta page atomic | Chương 4, mục 6.1–6.5 |
| 10 | [Secondary index](10-secondary-index.md) | Bảng = KV, index = KV phụ, index nhiều cột, atomicity không composable | Mục 8.1, chương 10 |
| 11 | [Postgres index thật](11-postgres-index-thuc-te.md) | pageinspect: heap + ctid, cây 3 tầng, index-only, skip scan, bitmap, UPDATE | Đối chiếu Postgres |
| 12 | [Bên trong một page](12-ben-trong-page.md) | 5 phần của page; page header vs ô vs tuple header; OffsetNumber; thứ tự 6 bước; TOAST và dòng > 8KB | Đối chiếu Postgres, M1 |
