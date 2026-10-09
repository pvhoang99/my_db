# Thư mục là gì?

> Tiếp theo [01-file-la-gi.md](01-file-la-gi.md). Đọc trước khi vào
> [03-nen-tang-file-he-thong.md](03-nen-tang-file-he-thong.md).

## 1. Thư mục cũng chỉ là một file

Trực giác thông thường: *"thư mục là cái hộp, file nằm bên trong hộp"*.
**Sai.** Không có cái hộp nào cả, và **không có gì "nằm trong" thư mục**.

> **Thư mục là một file.** Nó có inode riêng, chiếm trang trên đĩa như mọi file khác.
> Chỉ khác ở **nội dung**: thay vì chứa văn bản hay ảnh, nó chứa **một cái bảng**.

```bash
stat -c '%n -> kieu=%F, inode=%i, chiem %s byte' /tmp/tm
# /tmp/tm -> kieu=directory, inode=19020999, chiem 4096 byte
```

Nội dung của nó là **bảng hai cột**:

```
Nội dung file thư mục /tmp/tm:
┌──────────┬────────────┐
│ Tên      │ Số inode   │
├──────────┼────────────┤
│ con1     │ 19021061   │
│ con2     │ 19021062   │
│ goc.txt  │ 19021063   │
└──────────┴────────────┘
```

Mỗi dòng trong bảng gọi là một **directory entry**, hay **hard link**.

**Hệ quả quan trọng:** file **không nằm trong** thư mục. File nằm ở đâu đó trên
đĩa, còn thư mục chỉ **giữ một dòng ghi tên và số inode của nó**. Giống như
**danh bạ điện thoại** — tên bạn bè không "nằm trong" cuốn danh bạ, cuốn danh bạ
chỉ ghi lại số của họ.

---

## 2. `.` và `..` là hai dòng có thật

Xem nội dung đầy đủ của một thư mục vừa tạo:

```bash
mkdir /tmp/tm; mkdir /tmp/tm/con1 /tmp/tm/con2
ls -ai /tmp/tm
#  19020999 .        <- tro vao chinh no
#  15466497 ..       <- tro vao thu muc cha (/tmp)
#  19021061 con1
#  19021062 con2
```

`.` và `..` **không phải ký hiệu đặc biệt do shell bịa ra** — chúng là **hai dòng
thật** trong bảng:

- `.` → trỏ vào **chính inode của thư mục này**
- `..` → trỏ vào **inode của thư mục cha**

Nhờ vậy bạn mới gõ `cd ..` được — kernel chỉ việc tra dòng `..` trong bảng.

### Giải thích con số `nlink` kỳ lạ của thư mục

`nlink` = số cái tên trỏ tới một inode. Theo dõi khi tạo thư mục con:

```bash
stat -c 'nlink=%h' /tmp/tm    # thu muc rong        -> nlink=2
mkdir /tmp/tm/con1            #                     -> nlink=3
mkdir /tmp/tm/con2            #                     -> nlink=4
```

Vì sao thư mục rỗng đã có **2**, chứ không phải 1?

```
1.  Tên "tm" trong bảng của thư mục cha /tmp
2.  Dòng "." bên trong chính nó
```

Và mỗi thư mục con thêm **1**, vì dòng `..` của đứa con cũng trỏ về nó.

```
nlink của một thư mục = 2 + số thư mục con
```

Một cách kiểm tra nhanh: `nlink` của `/tmp/tm` là 4 → nó có **2 thư mục con**.

---

## 3. Cây thư mục và đường dẫn

Vì thư mục chứa được tên của thư mục khác, ta có **cấu trúc cây**. Gốc của cây là
thư mục `/` (**root**).

```
/
├── home
│   └── pham_hoang
│       └── go_database
│           └── README.md
├── tmp
└── usr
```

**Đường dẫn** chỉ là **lộ trình đi trong cây**:

| Loại | Ví dụ | Ý nghĩa |
|---|---|---|
| **Tuyệt đối** | `/home/pham_hoang/go_database` | Bắt đầu từ `/` |
| **Tương đối** | `docs/ghi-chu` | Bắt đầu từ thư mục hiện tại |
| | `../minidb` | Lùi lên cha (`..`) rồi đi tiếp |

### Kernel giải một đường dẫn như thế nào

Khi bạn mở `/home/hoang/db`, kernel **tra bảng từng bước một**:

```
1. Bắt đầu ở inode của /            (kernel luôn biết inode này)
2. Đọc bảng của /,      tìm "home"  →  được inode 100
3. Đọc bảng của inode 100, tìm "hoang" → được inode 250
4. Đọc bảng của inode 250, tìm "db"    → được inode 999
5. Mở inode 999  ✅
```

Gọi là **path resolution**. Đường dẫn càng sâu thì càng nhiều lần tra bảng —
đó là lý do hệ điều hành **cache mạnh** bước này (gọi là *dentry cache*).

---

## 4. Hard link và symbolic link

Đây là hai thứ **rất khác nhau**, hay bị nhầm.

### Hard link — thêm một dòng nữa trỏ vào **cùng inode**

```bash
ln /tmp/tm/goc.txt /tmp/tm/hard.txt
ls -li /tmp/tm/*.txt
#  19021063 ... 2 ... goc.txt      <- CUNG inode 19021063
#  19021063 ... 2 ... hard.txt     <- CUNG inode, nlink=2
```

**Hai cái tên hoàn toàn ngang hàng nhau.** Không có cái nào là "bản gốc", không
có cái nào là "bản sao". Chúng chỉ là hai dòng trong bảng cùng ghi một số inode.

### Symbolic link (symlink) — một file **riêng** chứa đường dẫn

```bash
ln -s /tmp/tm/goc.txt /tmp/tm/sym.txt
ls -li /tmp/tm/sym.txt
#  19021064 lrwxrwxrwx 1 ... sym.txt -> /tmp/tm/goc.txt
#  ^^^^^^^^ inode KHAC                  ^^^^^^^^^^^^^^^ noi dung la mot doan text
```

Symlink là **một file thật sự, có inode riêng**, và nội dung của nó chỉ là
**một dòng chữ ghi đường dẫn**. Giống một tờ giấy ghi *"xem ở chỗ kia"*.

### Phân biệt bằng một thí nghiệm

```bash
rm /tmp/tm/goc.txt           # xoa cai ten goc

cat /tmp/tm/hard.txt         # -> "noi dung goc"   ✅ VAN DOC DUOC
cat /tmp/tm/sym.txt          # -> No such file     ❌ GAY
```

Vì sao?

- **Hard link** trỏ thẳng vào **inode**. Xoá một tên thì `nlink` chỉ giảm từ 2
  xuống 1 — dữ liệu còn nguyên.
- **Symlink** chỉ chứa **dòng chữ** `/tmp/tm/goc.txt`. Cái tên đó biến mất thì
  symlink trỏ vào hư không — gọi là **broken link**.

| | Hard link | Symlink |
|---|---|---|
| Có inode riêng? | ❌ dùng chung | ✅ có |
| Trỏ vào cái gì | **inode** | **đường dẫn (dòng chữ)** |
| Xoá bản gốc | vẫn dùng được | gãy |
| Qua được filesystem khác? | ❌ không | ✅ được |
| Link tới thư mục? | ❌ bị cấm | ✅ được |

> Hard link không qua được filesystem khác vì **số inode chỉ có ý nghĩa bên trong
> một filesystem**. inode 1234 của ổ này chẳng liên quan gì tới inode 1234 của ổ kia.

---

## 5. Mount point — nơi hai filesystem nối vào nhau

Máy bạn có nhiều filesystem, mỗi cái có **bảng inode riêng**:

```bash
df -h --output=source,fstype,target
#  /dev/nvme0n1p4  ext4   /          <- o SSD chinh
#  /dev/nvme0n1p3  vfat   /boot/efi  <- phan vung khac
#  tmpfs           tmpfs  /dev/shm   <- nam trong RAM!
```

Chúng được **gắn (mount)** vào cây thư mục tại một thư mục nào đó, gọi là
**mount point**. Khi path resolution đi tới mount point, kernel **nhảy sang
filesystem khác** và tiếp tục tra bảng ở đó.

Nên cây thư mục **nhìn thì liền một khối**, nhưng thực ra **ghép từ nhiều
filesystem**.

### Hệ quả trực tiếp cho mục 1.2 của sách

> **`rename` chỉ atomic khi nguồn và đích nằm trên cùng một filesystem.**

Vì `rename` chỉ sửa một dòng `tên → số inode` **trong một bảng**. Khác filesystem
thì số inode vô nghĩa, nên hệ điều hành buộc phải **copy từng byte rồi xoá** —
và bạn **mất sạch tính atomic**.

Đó chính là lý do sách viết file tạm là `db.tmp` **đặt ngay cạnh file đích**:

```go
tmp := fmt.Sprintf("%s.tmp.%d", path, randomInt())
//                  ^^^^ cung thu muc -> chac chan cung filesystem
```

**Không phải tuỳ tiện, mà là bắt buộc.**

---

## Tóm tắt

1. **Thư mục là một file**, nội dung là **bảng `tên → số inode`**.
2. File **không nằm trong** thư mục — thư mục chỉ **giữ tên và số inode** của nó.
3. `.` và `..` là **hai dòng thật** → giải thích `nlink = 2 + số thư mục con`.
4. **Đường dẫn** = lộ trình trong cây; kernel **tra bảng từng bước** (path resolution).
5. **Hard link** = thêm một dòng trỏ cùng inode. **Symlink** = file riêng chứa
   một dòng chữ đường dẫn.
6. **Mount point** ghép nhiều filesystem thành một cây → và đó là lý do
   **`rename` phải trong cùng filesystem**.

## Câu hỏi tự kiểm tra

1. Thư mục có `nlink = 7` thì nó có mấy thư mục con?
2. Xoá file gốc đi, hard link còn dùng được không? Symlink thì sao? Vì sao khác nhau?
3. Vì sao không tạo được hard link trỏ sang file ở ổ đĩa khác?
4. Vì sao `mv` một file 10GB trong cùng thư mục thì tức thì, nhưng `mv` sang ổ
   khác lại mất cả phút?
