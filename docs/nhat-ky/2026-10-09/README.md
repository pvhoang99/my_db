# Nhật ký học — 09/10/2026

Buổi đầu tiên. Dựng dự án, dịch sách, và học xong **mục 1.1 → 1.3**.

---

## 1. Dựng dự án

- Repo: `github.com/pvhoang99/my_db`, module `github.com/hoangpv/minidb`
- [ROADMAP.md](../../../ROADMAP.md) — 7 milestone M1→M7
- [ARCHITECTURE.md](../../../ARCHITECTURE.md) — khung thư mục theo chuẩn Go, chia package
  bên trong bám theo `src/backend/` của PostgreSQL, kèm **luật phụ thuộc** giữa các tầng
- Chưa viết code — đang ở giai đoạn đọc sách

## 2. Sách tham khảo

**Build Your Own Database From Scratch in Go** — James Smith, 2nd ed 2024, 103 trang.

- PDF ở `book/`, bản dịch tiếng Việt đầy đủ **15 chương** ở `docs/sach/`
  (cả hai **gitignore** vì bản quyền)
- ⚠️ **Phát hiện quan trọng:** sách dựng DB kiểu **SQLite/BoltDB**
  (COW B+tree, clustered, 1 writer), **không phải kiến trúc PostgreSQL**
  (heap + WAL + MVCC xmin/xmax). Mỗi bản dịch có mục
  **📌 Đối chiếu với PostgreSQL** ghi rõ khác biệt
- Bảng so sánh đầy đủ: [docs/00-ke-hoach-doc-sach.md](../../00-ke-hoach-doc-sach.md)

---

## 3. Kiến thức nền đã học

Học theo **thang bậc**, mỗi lần một viên gạch. Ghi chú chi tiết ở `docs/ghi-chu/`:

| # | Ghi chú | Học được gì |
|---|---|---|
| 00 | [Đĩa là gì, HDD vs SSD](../../ghi-chu/00-dia-hdd-ssd.md) | Sector, cơ học HDD (seek + chờ quay), flash SSD |
| 01 | [File là gì](../../ghi-chu/01-file-la-gi.md) | File = tên + inode + dữ liệu; file descriptor |
| 02 | [Thư mục là gì](../../ghi-chu/02-thu-muc-la-gi.md) | Bảng tên→inode, `.`/`..`, hard vs symlink, mount |
| 03 | [Nền tảng filesystem](../../ghi-chu/03-nen-tang-file-he-thong.md) | page cache, fsync, rename, phân tích crash |
| 04 | [BST vs B+tree](../../ghi-chu/04-bst-vs-btree.md) | Vì sao BST vô dụng trên đĩa; fanout tính thế nào |

### Những ý quan trọng nhất trong ngày

**a) Đĩa quyết định mọi thứ**
Đĩa chỉ đọc/ghi được **nguyên trang 4KB**, và **đọc ngẫu nhiên đắt gấp hàng trăm
lần đọc tuần tự**. Mọi thiết kế database (page, B+tree, WAL) đều chảy ra từ đây.

**b) "File" là ảo giác**
Ghép từ 3 mảnh rời: **tên** (ở thư mục) + **inode** (hồ sơ) + **dữ liệu** (các trang).
**Thư mục cũng chỉ là một file**, nội dung là bảng `tên → số inode`.

**c) `write()` không ghi xuống đĩa**
Nó chỉ chép vào **RAM (page cache)** rồi báo xong. **`fsync`** mới ép xuống đĩa thật.
→ Đây là lý do mọi DB đều chậm hơn ghi file thường.

**d) `rename` atomic vì nó chỉ sửa MỘT dòng**
Dữ liệu không bị đụng tới → chi phí hằng số, không có trạng thái nửa vời.

**e) Nguyên tắc xuyên suốt cả quyển sách**
> **Đừng phá dữ liệu cũ. Tạo bản mới bên cạnh, rồi chuyển sang nó bằng một thao
> tác atomic.**

Và thứ tự bắt buộc: **dữ liệu phải chắc chắn an toàn trên đĩa (fsync), RỒI mới
chuyển cái tên sang nó (rename).** Nguyên tắc này sẽ xuất hiện lại y nguyên ở
chương 6 dưới dạng *ghi node mới → fsync → rồi mới ghi con trỏ root*.

---

## 4. Thí nghiệm đã chạy

| Thí nghiệm | Kết quả |
|---|---|
| File 1 byte chiếm bao nhiêu trên đĩa | **4096 byte** — đĩa không ghi lẻ được |
| `filefrag` một file 2MB | Bị chẻ làm **2 extent** — các trang không cần liền nhau |
| `dd` 200MB có/không `fsync` | **0,09s** vs **0,42s** — write() chỉ vào RAM |
| `rename` đổi inode | Cái tên trỏ sang inode khác, **dữ liệu không bị copy** |
| `rm` file đang được mở | **Xoá được tên, nhưng 300MB chưa giải phóng** |
| Reader đọc trong lúc rename | **Vẫn thấy dữ liệu CŨ trọn vẹn** |
| `nlink` của thư mục | 3 thư mục con → `nlink = 5` (`= 2 + số con`) |
| Hard link vs symlink khi xoá gốc | hard link **vẫn chạy**, symlink **gãy** |
| [BST vs B+tree trên 104.334 từ thật](../../../scripts/bst_vs_btree.py) | BST **41 tầng**, B+tree **3 tầng** |

---

## 5. Kiểm tra kiến thức — **5/8**

✅ **Đúng:** vì sao đọc ngẫu nhiên đắt → cây phải thấp · `rename` rẻ vì chỉ sửa
một dòng · `fsync` phải trước `rename` · thiếu `fsync` lên thư mục cha ·
`O_EXCL` là lưới an toàn

❌ **Sai 2 câu, cùng MỘT gốc:**

> Tưởng **fd trỏ vào cái tên file**. Thực tế **fd trỏ thẳng vào inode**.
> Cái tên chỉ dùng **đúng một lần lúc `open`**, xong là vứt.

Nên: `rm` file đang mở thì **vẫn đọc được, dung lượng chưa giải phóng**; và
`rename` đè lên thì reader cũ **vẫn thấy dữ liệu cũ trọn vẹn**.
→ Đã bổ sung mục ⚠️ *Hiểu nhầm thường gặp* vào
[01-file-la-gi.md](../../ghi-chu/01-file-la-gi.md).

Và chính điều này **là lý do mục 1.2 hoạt động được**: reader đọc bản cũ, writer
thay bản mới — không ai đợi ai. Đây là hạt giống của **RCU** (chương 12) và
**MVCC** của PostgreSQL.

❌ Câu còn lại: `nlink = 2 + số thư mục con` (nhầm thành `1 +`, quên dòng `.`).

---

## 6. Mục 1.3 — append-only log (học xong)

**Vấn đề còn sót từ 1.2:** rename phải **ghi lại toàn bộ file** mỗi lần update.
Sửa một dòng trong DB 10GB → ghi lại 10GB (~20 giây). Không dùng được.

**Giải pháp — append-only log:** đừng ghi **trạng thái**, hãy ghi **thay đổi**.

```
     0         1         2         3
| set a=1 | set b=2 | set a=3 | del b |     -> trang thai cuoi: a=3
```

Chi phí update giờ tỉ lệ với **kích thước thay đổi**, không còn tỉ lệ với
**kích thước database** → từ `O(N)` xuống `O(1)`.

**Nhưng đẻ ra 2 vấn đề mới** (tự suy ra được, đúng cả hai):

Đo thật — 100.000 update trên chỉ 100 key:

```
Kich thuoc file log     : 1.678.971 byte (1.60 MB)
Du lieu THUC SU co ich  :     1.690 byte
-> LANG PHI             : 99.90%   (993 lan)
Doc 1 key bat ky        : 50 ms  (phai quet HET file)
```

1. **Không có index** → đọc một key phải **quét cả log**, `O(N)`
2. **Không thu hồi được chỗ** → file **phình vô hạn**

| | Ghi lại cả file (1.2) | Append-only log (1.3) |
|---|---|---|
| **Ghi** | ❌ `O(N)` | ✅ `O(1)` |
| **Đọc** | ✅ nhanh | ❌ `O(N)` |
| **Dung lượng** | ✅ gọn | ❌ phình vô hạn |

→ **Log một mình không đủ. Phải kết hợp với một cấu trúc index.**
Đó chính là câu hỏi mở đầu **chương 2**, và câu trả lời là **B+tree** hoặc **LSM-tree**.

### Crash giữa lúc đang nối thêm — torn write & checksum

Log không phá dữ liệu cũ, nhưng **entry cuối** có thể dở dang. 3 khả năng sách nêu:
(a) append chưa kịp xảy ra · (b) entry ghi được một nửa · (c) kích thước file tăng
nhưng dữ liệu không có ở đó *(metadata và dữ liệu là hai thứ riêng, xuống đĩa
không cùng lúc)*.

**Giải pháp:** mỗi entry mang một **checksum** — `| len 4B | crc32 4B | payload |`.
Replay gặp checksum sai thì **dừng**, coi như từ đó trở đi chưa từng xảy ra.

Thử cả 4 tình huống crash trên log `[a=1, b=2, a=3, del b]`:

```
(a) append chua kip xay ra    -> a=3, b=2   (het file)
(b) entry ghi duoc mot nua    -> a=3, b=2   (payload thieu)
(c) file dai ra, du lieu = 00 -> a=3, b=2   (len=0)
(d) hong 1 bit                -> a=3, b=2   (CHECKSUM SAI)
```

Mọi đường đều ra **cùng một trạng thái hợp lệ** → đó là nghĩa của *"checksum làm
cho update log trở nên **atomic**"*: entry **hoặc được tính trọn vẹn, hoặc bị bỏ
hoàn toàn**.

### ⚠️ Cái bẫy gặp khi tự code demo

Vùng toàn byte `00` đọc ra `len=0, crc=0`. Mà **`zlib.crc32(b'') == 0`** →
**vượt qua được kiểm tra checksum!** Phải vá bằng cách coi `len == 0` là hết log.
Postgres tránh bằng cách đưa `xl_prev` vào mỗi WAL record.

### Giới hạn của checksum

| Loại hỏng | Checksum |
|---|---|
| **Torn write** (ghi dở **trước** `fsync` thành công) | ✅ phát hiện & khôi phục được |
| Hỏng **sau** `fsync` (bit rot, đĩa lỗi) | ⚠️ phát hiện được nhưng **không cứu được** |

📝 Chi tiết: [05-append-only-log.md](../../ghi-chu/05-append-only-log.md)

---

## 7. Lần sau học gì

1. **Mục 1.4** — những cái bẫy của `fsync` (gồm `fsync` lên thư mục cha còn nợ
   từ 1.2, và sự cố **fsyncgate** của PostgreSQL)
2. **Mục 1.5** — tóm tắt chương 1
3. Rồi sang **chương 2** — hashtable, mảng sắp xếp, B+tree vs LSM-tree

## Còn nợ

- [ ] Trả lời: vì sao fanout của leaf (249) lớn hơn của internal (222)?
- [ ] Ôn lại mục ⚠️ *fd bám vào inode* trong `01-file-la-gi.md`
- [ ] Chốt kiến trúc để code (bám sách hay bám Postgres) — **sau khi đọc hết sách**
