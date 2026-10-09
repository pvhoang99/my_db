# Nền tảng filesystem — 5 viên gạch dưới mục 1.2 của sách

> Ghi chú học, viết lại bằng lời của mình. Đây là kiến thức nền để hiểu
> mục **1.2 Atomic renaming** trong *Build Your Own Database From Scratch in Go*.

Thứ tự học: mỗi viên dựa trên viên trước.

```
1. Đĩa lưu file thế nào?        → inode
2. Tên file nằm ở đâu?          → thư mục = bảng tên→inode
3. Ghi file thì dữ liệu đi đâu? → page cache, fsync
4. rename() làm gì?             → sửa một dòng trong bảng
5. Vì sao crash không mất dữ liệu?
```

---

## 1. Đĩa lưu file thế nào — và inode là gì

**Ổ đĩa không biết khái niệm "file".** Nó chỉ là một dãy **trang (block)** được
đánh số, mỗi trang thường **4096 byte**. Bạn chỉ ra lệnh được hai việc:
*ghi 4096 byte vào trang số N*, và *đọc trang số N*.

Vì đọc/ghi phải nguyên trang, nên một file 1 byte vẫn **chiếm trọn 4096 byte**:

```bash
printf 'A' > /tmp/x; stat -c '%s byte noi dung, %b x 512 byte tren dia' /tmp/x
# -> 1 byte noi dung, 8 x 512 byte tren dia   (= 4096)
```

Nội dung file nằm rải trong vài trang. Phải có **một tờ phiếu ghi lại** file đó
dùng những trang nào — **tờ phiếu đó chính là `inode`**:

```
┌─────────────────────────────┐
│ Kích thước : 10240 byte     │
│ Chủ sở hữu : hoang          │
│ Sửa lần cuối: 21:05         │
│ Dữ liệu ở  : trang 500-502  │
└─────────────────────────────┘
        ⚠️ KHÔNG có tên file
```

### Các trang có cần liền nhau không? — Không

inode ghi hẳn **danh sách** trang, nên trang nào cũng được. Nhưng liền nhau thì
đọc nhanh hơn (đầu đọc chạy một mạch, không phải nhảy), nên filesystem **luôn cố
xin một dãy liền**. Một dãy liền được ghi gọn thành **extent** = "từ trang 500,
lấy 320 trang". Chuyện bị rải rác gọi là **phân mảnh (fragmentation)**.

```bash
filefrag -v /tmp/to.bin
#  ext:  logical_offset:   physical_offset: length:  expected:
#    0:       0..   319:  61907712..61908031:   320:
#    1:     320..   488:  99005184..99005352:   169:  61908032:  <- phai nhay
```

### Số inode là định danh

Khi format, filesystem **cắt sẵn một vùng** chứa dãy ô inode, mỗi ô ~256 byte.
**Số inode = số thứ tự của ô trong dãy đó.** Nên tra inode chỉ là một phép nhân,
không phải tìm kiếm:

```
vị trí trên đĩa = (đầu bảng inode) + số_inode × 256
```

Hệ quả: **số inode cố định từ lúc format** → có thể **hết inode dù còn chỗ trống**.
Đây là nguyên nhân của lỗi kinh điển "No space left on device" trong khi
`df -h` báo còn cả trăm GB — thường do hàng triệu file tí hon.

```bash
df -i /tmp   # han muc so FILE
df -h /tmp   # han muc DUNG LUONG   -> hai han muc TACH BIET
```

---

## 2. Tên file nằm ở đâu — thư mục là cái gì

**inode không chứa tên file.** Một file không "biết" mình tên gì.

**Thư mục cũng chỉ là một file**, nhưng nội dung của nó là một **bảng 2 cột**:

```
Nội dung thư mục /data:
┌──────────┬────────────┐
│ Tên      │ Số inode   │
├──────────┼────────────┤
│ db       │ 1000       │
│ notes    │ 8899       │
└──────────┴────────────┘
```

Kiểm chứng thư mục đúng là một file, có inode riêng, chiếm trang trên đĩa:

```bash
stat -c 'inode=%i kieu=%F chiem %s byte' /tmp/mydir
# -> inode=15788214 kieu=directory chiem 4096 byte
```

### Mở một file thực ra là 2 bước

```
cat /data/db
   │
   1. Tra bảng thư mục /data, tìm dòng "db"  →  được số 1000
   2. Đọc inode 1000                          →  biết dữ liệu ở trang nào
   3. Đọc các trang đó                        →  ra nội dung
```

> **Cái tên và nội dung chỉ dính với nhau bằng đúng MỘT dòng trong bảng.**

Đây là chìa khoá của cả mục 1.2.

---

## 3. Ghi file thì dữ liệu đi đâu — page cache & fsync

> **`write()` không ghi xuống đĩa.** Nó chép dữ liệu vào **RAM** rồi báo "xong" ngay.

Vùng RAM đó là **page cache** — kho đệm của hệ điều hành, chứa các trang 4KB y
hệt trên đĩa.

```
chương trình
    │ write("hello")
    ▼
┌──────────────┐
│  PAGE CACHE  │  ← tới đây write() đã trả về "xong"
│    (RAM)     │
└──────┬───────┘
       │ vài giây sau, kernel tự ghi xuống khi rảnh
       ▼
   ┌────────┐
   │  ĐĨA   │
   └────────┘
```

**Lý do:** đĩa chậm hơn RAM hàng nghìn lần. Chờ đĩa mỗi lần ghi thì máy không dùng nổi.

```bash
dd if=/dev/zero of=/tmp/t1 bs=1M count=200              # 0.09 giay  (chi vao RAM)
dd if=/dev/zero of=/tmp/t2 bs=1M count=200 conv=fsync   # 0.42 giay  (xuong dia that)
```

### Vấn đề

Chương trình nhận "ghi thành công", nhưng dữ liệu **vẫn trong RAM**.
**Mất điện lúc này = mất dữ liệu**, dù đã báo thành công.

### `fsync`

`fsync` nói với kernel: *"ghi xuống đĩa thật ngay, và đừng trả lời tôi cho tới
khi xong."*

```
write()   →  "xong!"  (thật ra mới vào RAM)
fsync()   →  ...đợi...  →  "giờ mới thật sự xuống đĩa"
```

> Đây là lý do sách viết: *"dữ liệu không bền vững cho tới khi bạn gọi `fsync`"*,
> và cũng là lý do mọi database đều chậm hơn ghi file thường.

---

## 4. `rename()` làm chính xác việc gì

```
TRƯỚC:                          SAU rename("db.tmp", "db"):
┌────────┬──────────┐           ┌────────┬──────────┐
│ db     │ inode 1000│ cũ        │ db     │ inode 2000│ ← đổi số!
│ db.tmp │ inode 2000│ mới       └────────┴──────────┘
└────────┴──────────┘             inode 1000 hết tên → được giải phóng
```

**Không một byte dữ liệu nào bị đụng tới.** Chỉ sửa một dòng trong danh bạ.

```bash
ls -li /tmp/rn    #  db -> 19020993,  db.tmp -> 19020994
mv -f /tmp/rn/db.tmp /tmp/rn/db
ls -li /tmp/rn    #  db -> 19020994        <- cai ten da tro sang inode khac
```

### Hai hệ quả

1. **Chi phí hằng số.** File 1 byte hay 1 TB, rename cũng chỉ sửa một dòng.
2. **Không có trạng thái nửa vời.** Kernel sửa dòng đó như một thao tác
   **không chia cắt được** → reader hoặc thấy số cũ, hoặc thấy số mới,
   **không bao giờ thấy rác**.

Chữ "không chia cắt được" chính là **atomic**.

---

## 5. Vì sao cách này cứu được dữ liệu khi crash

### Cách sai (mục 1.1)

```
1. Mở db, XOÁ SẠCH nội dung cũ    ← dữ liệu cũ chết ngay đây
2. Ghi nội dung mới
3. fsync
```

Giữa bước 1 và 3: dữ liệu cũ **đã mất**, dữ liệu mới **chưa xong**.
Crash lúc đó → **không còn gì cả**.

### Cách đúng (mục 1.2)

```
1. Ghi vào file MỚI db.tmp     ← db cũ không bị đụng tới
2. fsync db.tmp                ← ép dữ liệu mới xuống đĩa thật
3. rename(db.tmp, db)          ← sửa một dòng
```

| Crash ở đâu | Bảng thư mục | Kết quả |
|---|---|---|
| Giữa bước 1 | `db → inode cũ` | ✅ Dữ liệu cũ nguyên vẹn (thừa file rác `db.tmp`) |
| Giữa bước 2 | `db → inode cũ` | ✅ Dữ liệu cũ nguyên vẹn |
| Trước bước 3 | `db → inode cũ` | ✅ Dữ liệu cũ nguyên vẹn |
| Sau bước 3 | `db → inode mới` | ✅ Dữ liệu mới đầy đủ (đã fsync ở bước 2) |

**Không ô nào mất dữ liệu.** Mọi thời điểm đều có **hoặc bản cũ nguyên vẹn,
hoặc bản mới nguyên vẹn**.

> 💡 **Nguyên tắc:** Đừng phá dữ liệu cũ. Tạo bản mới bên cạnh, rồi chuyển sang
> nó bằng một thao tác atomic.

### Vì sao `fsync` phải đứng TRƯỚC `rename`

Nếu đảo lại, sẽ có khoảnh khắc:

- Bảng thư mục: `db → inode mới` ✅ (tên đã chuyển)
- Dữ liệu của inode mới: **còn trong RAM** ❌

Mất điện lúc đó → tên trỏ tới file rỗng/dở, mà inode cũ **đã bị gỡ tên**. Mất sạch.

> **Dữ liệu phải chắc chắn an toàn trên đĩa, RỒI mới được chuyển cái tên sang nó.**

Nguyên tắc này xuất hiện lại y nguyên ở **chương 6**:
*ghi các node mới → fsync → rồi mới ghi con trỏ root*.

---

## Còn dang dở — để dành cho mục 1.4

Sách nói `rename` **atomic với reader**, nhưng **chưa durable với mất điện**:
bản thân **bảng thư mục cũng nằm trong page cache**, nên cũng cần `fsync`
lên **thư mục cha**.

> ⚠️ Câu đáng nhớ nhất chương 1: mỗi khi thấy **"X là atomic"**, phải hỏi ngay
> **"atomic so với CÁI GÌ?"**
>
> | `rename` atomic so với | |
> |---|---|
> | Reader đồng thời | ✅ Có |
> | Mất điện | ❌ Không (cần fsync thư mục) |

## Câu hỏi tự kiểm tra

1. Vì sao file tạm phải đặt **cùng thư mục** với file đích, không để ở `/tmp`?
2. Vì sao `rename` một file 1 TB cũng nhanh như file 1 byte?
3. `write()` trả về thành công rồi mất điện — dữ liệu còn không? Vì sao?
4. Hết inode thì `df -h` có báo đầy đĩa không?
