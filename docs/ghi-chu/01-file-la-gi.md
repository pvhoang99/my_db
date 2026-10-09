# File là gì?

> Viên gạch số 0 — nền móng dưới cả [03-nen-tang-file-he-thong.md](03-nen-tang-file-he-thong.md).

## Hai cách nhìn

**Cách người dùng nhìn:** một file là *"một cái tên, bên trong có nội dung"*.
Giống một tờ giấy có dán nhãn.

**Cách máy nhìn:** không tồn tại thứ gì gọi là "file" cả. Chỉ có **ba thứ rời
rạc** nằm ở ba nơi khác nhau, được hệ điều hành ghép lại cho bạn:

```
        TÊN                    HỒ SƠ                 DỮ LIỆU
   (trong thư mục)            (inode)            (các trang trên đĩa)
┌──────────────────┐    ┌──────────────────┐    ┌──────────────┐
│ "bao-cao.txt"    │───▶│ inode 1234       │───▶│ trang 500    │
│      → 1234      │    │  size, owner,    │    │ trang 501    │
└──────────────────┘    │  danh sách trang │    └──────────────┘
                        └──────────────────┘
```

> **"File" là một ảo giác tiện lợi do hệ điều hành dựng lên** từ ba mảnh đó.

Hiểu điều này thì mọi chuyện lạ lùng sau đây đều trở nên hiển nhiên:

- Một file có thể có **nhiều tên** (vì tên nằm ngoài, ở thư mục).
- Một file có thể **không có tên nào** mà vẫn đọc được (nếu đang mở).
- **Đổi tên file không cần đụng tới dữ liệu** — chỉ sửa một dòng trong thư mục.

---

## Nội dung file chỉ là một dãy byte

Không hơn. File **không có "kiểu"**, không có cấu trúc gì mà hệ điều hành hiểu.
Nó chỉ là `byte[0], byte[1], byte[2], ...`

```bash
printf 'Hello' > /tmp/a.txt
xxd /tmp/a.txt
# 00000000: 4865 6c6c 6f      Hello
#           ^^ ^^ ^^ ^^ ^^
#           5 byte, the thoi
```

### Đuôi file `.txt`, `.jpg` chỉ là một phần của **cái tên**

Hệ điều hành **hoàn toàn không quan tâm** tới đuôi file. Nó chỉ là quy ước cho
con người và cho ứng dụng.

```bash
cp /tmp/a.txt /tmp/a.jpg      # doi duoi thanh .jpg
file /tmp/a.txt /tmp/a.jpg
# /tmp/a.txt: ASCII text
# /tmp/a.jpg: ASCII text      <- van la text! doi ten khong doi noi dung
```

Lệnh `file` đoán kiểu bằng cách **nhìn vào mấy byte đầu** (gọi là *magic bytes*),
chứ không nhìn đuôi. Ví dụ file PNG luôn bắt đầu bằng `89 50 4E 47`.

> **Ý nghĩa cho việc làm database:** file của ta cũng sẽ bắt đầu bằng magic bytes
> để nhận dạng. Chính là dòng này ở chương 6 của sách:
> `const DB_SIG = "BuildYourOwnDB06"`.

---

## Trên Linux, "mọi thứ đều là file"

Đây không phải cách nói ẩn dụ. Rất nhiều thứ **không phải dữ liệu trên đĩa** vẫn
được trình bày dưới dạng file, để bạn dùng chung một bộ lệnh `open/read/write/close`.

```bash
stat -c '%n -> %F' /tmp /dev/null /tmp/a.txt
#  /tmp       -> directory               <- thu muc cung la file
#  /dev/null  -> character special file  <- thiet bi cung la file
#  /tmp/a.txt -> regular file            <- file "binh thuong"
```

Các loại file:

| Loại | Ví dụ | Thực chất là gì |
|---|---|---|
| **regular file** | `a.txt` | Dữ liệu trên đĩa — cái ta hay gọi là "file" |
| **directory** | `/tmp` | Một file chứa **bảng `tên → số inode`** |
| **character device** | `/dev/null` | Một **thiết bị**, đọc/ghi được như file |
| **symbolic link** | `/bin` | Một file chứa **đường dẫn tới file khác** |
| **socket**, **pipe** | | Kênh **truyền dữ liệu giữa các tiến trình** |

Lợi ích của thiết kế này: bạn học **một bộ lệnh duy nhất**, dùng được cho mọi thứ.
Ghi ra màn hình, ghi vào file, gửi qua mạng — đều là `write()`.

---

## Mở file: file descriptor

Khi chương trình gọi `open("/tmp/a.txt")`, kernel **không trả về cái tên, cũng
không trả về inode**. Nó trả về **một con số** gọi là **file descriptor (fd)**.

Con số đó là **vé gửi đồ**: kernel giữ một bảng riêng cho mỗi tiến trình, và fd
là số thứ tự trong bảng đó.

```bash
exec 7< /tmp/a.txt        # mo file, nhan fd so 7
ls -l /proc/self/fd
#  0 -> /dev/null         <- stdin
#  1 -> pipe:[660895]     <- stdout
#  2 -> /dev/null         <- stderr
#  7 -> /tmp/a.txt        <- file minh vua mo
exec 7<&-                 # dong fd
```

Mọi tiến trình luôn có sẵn 3 fd đầu tiên:

| fd | Tên | Dùng để |
|---|---|---|
| 0 | **stdin** | nhận dữ liệu vào |
| 1 | **stdout** | in kết quả ra |
| 2 | **stderr** | in lỗi ra |

### Vì sao fd lại quan trọng với database

Vì **fd trỏ thẳng vào inode, không trỏ vào tên**. Một khi đã mở thành công, fd
**bám chặt vào dữ liệu đó**, bất kể cái tên sau này bị đổi hay bị xoá:

```bash
exec 9< /tmp/live      # reader mo file
rm /tmp/live           # xoa het ten cua no
cat <&9                # VAN DOC DUOC! du lieu chua bi giai phong
exec 9<&-              # dong fd -> gio moi that su bi xoa
```

Kernel chỉ thu hồi dữ liệu khi **cả hai** về 0:
- số **tên** trỏ tới inode (`nlink`), và
- số **fd** đang mở nó.

> Đây chính là thứ khiến chiêu `rename` ở **mục 1.2 của sách** an toàn với reader:
> một reader đã mở file **trước** lúc rename vẫn đọc trọn vẹn dữ liệu cũ, không
> bị gián đoạn, và writer cũng không phải đợi nó.

---

## 5 thao tác cơ bản với file

Toàn bộ việc dùng file quy về 5 lệnh (syscall) này:

| Syscall | Việc nó làm |
|---|---|
| `open` | Tra tên → inode, trả về **fd** |
| `read` | Đọc byte từ vị trí hiện tại |
| `write` | Ghi byte vào vị trí hiện tại (**chỉ vào RAM!**) |
| `lseek` | Nhảy tới vị trí khác trong file |
| `close` | Trả lại fd |

Và một lệnh thứ 6 mà database **không thể sống thiếu**:

| `fsync` | **Ép dữ liệu trong RAM xuống đĩa thật, và đợi cho tới khi xong** |
|---|---|

Chi tiết về `fsync` và page cache: xem
[03-nen-tang-file-he-thong.md § viên gạch 3](03-nen-tang-file-he-thong.md).

---

## Tóm tắt

1. **"File" là ảo giác** ghép từ 3 mảnh: **tên** (ở thư mục) + **inode** (hồ sơ)
   + **dữ liệu** (các trang trên đĩa).
2. Nội dung file **chỉ là một dãy byte**, không có kiểu. **Đuôi file chỉ là tên.**
3. Trên Linux **mọi thứ đều là file** — thư mục, thiết bị, socket — để dùng chung
   một bộ lệnh.
4. `open` trả về **file descriptor**, và fd **bám vào inode chứ không bám vào tên**.
5. Dữ liệu chỉ bị xoá khi **hết tên VÀ hết fd đang mở**.

## Câu hỏi tự kiểm tra

1. Đổi `anh.png` thành `anh.txt` thì file còn mở bằng trình xem ảnh được không? Vì sao?
2. Một file đang được chương trình khác mở, mình `rm` nó — dung lượng đĩa có được
   giải phóng ngay không?
3. `open()` trả về cái gì, và vì sao nó không trả về tên file?
