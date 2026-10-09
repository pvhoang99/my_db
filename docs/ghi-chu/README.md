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
