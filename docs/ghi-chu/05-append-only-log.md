# Append-only log — mục 1.3 của sách

> Nối tiếp [03-nen-tang-file-he-thong.md](03-nen-tang-file-he-thong.md) (mục 1.2).

## 1. Vấn đề còn sót lại từ 1.2

Chiêu `rename` ở 1.2 đã cứu được dữ liệu khi crash, nhưng còn **một điểm yếu
chết người**: mỗi lần update phải **ghi lại TOÀN BỘ file**.

```
Sua DUNG MOT dong, bang cach ghi lai ca file (SSD ~500 MB/s):
     1 MB  ->     2 ms
   100 MB  ->   200 ms
     1 GB  ->   2,0 giay
    10 GB  ->  20,5 giay
```

Và nếu app ghi 1000 lần/giây thì… không thể.

## 2. Ý tưởng: đừng ghi **trạng thái**, hãy ghi **thay đổi**

Chỉ **nối thêm vào cuối file** một dòng mô tả việc vừa làm:

```
     0         1         2         3
| set a=1 | set b=2 | set a=3 | del b |      -> trang thai cuoi: a=3
```

File chỉ-nối-thêm này gọi là **log**.

Nó dùng **đúng nguyên tắc của 1.2** — *không phá dữ liệu cũ* — chỉ khác chỗ áp dụng:

| | 1.2 — rename | 1.3 — log |
|---|---|---|
| Giữ dữ liệu cũ bằng cách | ghi ra **file mới** | ghi vào **cuối file** |
| Mỗi lần update ghi | **toàn bộ** | **chỉ phần thay đổi** |

```python
def set_(k, v): open(LOG,'a').write(f'set {k}={v}\n')   # chi NOI THEM
def del_(k):    open(LOG,'a').write(f'del {k}\n')       # chi NOI THEM

def read_all():                    # doc tu dau, ap dung lan luot
    state = {}
    for line in open(LOG):
        op, rest = line.strip().split(' ', 1)
        if op == 'set': k, v = rest.split('=', 1); state[k] = v
        else:           state.pop(rest, None)
    return state
```

> 🔑 Chi phí update giờ tỉ lệ với **kích thước thay đổi**, không còn tỉ lệ với
> **kích thước database** → từ `O(N)` xuống `O(1)`.

## 3. Nhưng log đẻ ra 2 vấn đề mới

Đo thật — 100.000 update nhưng chỉ trên 100 key:

```
Kich thuoc file log     : 1.678.971 byte  (1,60 MB)
Du lieu THUC SU co ich  :     1.690 byte
-> LANG PHI             : 99,90%   (993 lan)
Doc 1 key bat ky        : 50 ms  (phai quet HET file)
```

1. **Không phải cấu trúc index** → đọc một key phải **quét cả log**, `O(N)`
2. **Không thu hồi được chỗ** của dữ liệu cũ/đã xoá → file **phình vô hạn**

| | Ghi lại cả file (1.2) | Append-only log (1.3) |
|---|---|---|
| **Ghi** | ❌ `O(N)` | ✅ `O(1)` |
| **Đọc** | ✅ nhanh | ❌ `O(N)` |
| **Dung lượng** | ✅ gọn | ❌ phình vô hạn |

> **Log một mình KHÔNG ĐỦ để build một DB. Nó phải được kết hợp với một cấu trúc
> index.** → Đó là câu hỏi mở đầu **chương 2**, và câu trả lời sẽ là
> **B+tree** hoặc **LSM-tree**.

## 4. Crash giữa lúc đang nối thêm thì sao?

Log **không làm hỏng dữ liệu cũ** — đó là điểm mạnh. Nhưng **entry cuối cùng**
có thể dở dang. Sách liệt kê 3 khả năng:

| | Chuyện gì xảy ra | File trông ra sao |
|---|---|---|
| **a** | Lệnh append **chưa kịp xảy ra** | Log vẫn tốt, thiếu entry cuối |
| **b** | Entry **ghi được một nửa** | Dòng cuối cụt giữa chừng |
| **c** | **Kích thước** file tăng nhưng **dữ liệu không có ở đó** | Dòng cuối toàn byte `00` |

> Trường hợp (c) nghe lạ nhưng có thật: **metadata (kích thước) và dữ liệu là hai
> thứ riêng biệt**, chúng xuống đĩa không cùng lúc.

## 4b. Torn write là gì

**Torn write** = *"ghi bị xé rách"* — một lệnh ghi bị **đứt giữa chừng**, chỉ một
phần dữ liệu xuống được đĩa.

### Vì sao nó xảy ra

Bạn gọi `write(8192 byte)` và nghĩ đó là **một hành động**. Phần cứng không thấy vậy:

```
Lenh cua ban:   write(8192 byte)
                        |
                        v   he dieu hanh + o dia chia nho ra
   +----+----+----+----+----+----+----+----+ ...  16 sector x 512B
   | s0 | s1 | s2 | s3 | s4 | s5 | s6 | s7 |
   +----+----+----+----+----+----+----+----+
     OK   OK   OK   OK   <-- MAT DIEN O DAY
                          X    X    X    X
```

Đĩa chỉ **đảm bảo atomic ở mức một sector** (512 byte) — và nhiều ổ còn không
đảm bảo nổi cả điều đó. Ghi nhiều sector thì **không có đảm bảo nào**: mất điện
giữa chừng để lại **một nửa mới, một nửa cũ**. Chữ *"torn"* (rách) là vì thế.

### Hai kiểu, và kiểu thứ hai tệ hơn nhiều

Mô phỏng ghi đè một trang 4KB, mất điện sau 5/8 sector:

```
Trang CU  : header noi 3 key | 'A'...'A'
Trang MOI : header noi 9 key | 'B'...'B'
Trang RACH: header noi 9 key | 'B'...'A'   <- nua moi, nua cu
```

Trang rách **không tương ứng với bất kỳ phiên bản nào**. Header nói 9 key nhưng
nửa sau dữ liệu vẫn của bản cũ. **Cấu trúc hỏng.**

| | Torn log | Torn page |
|---|---|---|
| Vị trí phần rách | **cuối** log | **giữa** dữ liệu |
| Dữ liệu cũ | **còn nguyên** ở phía trước | **đã bị ghi đè — mất rồi** |
| Cách xử lý | vứt entry cuối → xong ✅ | vứt thì **về đâu?** ❌ |

> 🔑 **Log chỉ nối thêm nên không bao giờ phá bản cũ.** Còn ghi đè page thì bản cũ
> **biến mất ngay khi bắt đầu ghi** — rách là mất cả hai.

### Tránh torn page bằng chính nguyên tắc của 1.2

Nhớ lại: ***đừng phá dữ liệu cũ***. Hai cách, sẽ gặp cả hai:

| Cách | Làm gì | Ai dùng |
|---|---|---|
| **Copy-on-write** | Ghi ra **page mới**, không đụng page cũ, rồi chuyển con trỏ | **Sách** (ch.3), SQLite, BoltDB |
| **Double-write / full-page write** | **Lưu bản sao page trước** vào log + fsync, rồi mới ghi đè. Crash thì apply mù bản sao | **PostgreSQL**, MySQL |

Cả hai cùng một nguyên lý mà chương 3 sẽ phát biểu:

> **Tại mọi thời điểm, phải luôn có đủ thông tin để dựng lại hoặc trạng thái cũ,
> hoặc trạng thái mới.**

Torn page chính là **lý do tồn tại** của `full_page_writes` trong PostgreSQL —
tham số mà nếu tắt đi để WAL nhỏ lại, bạn đánh đổi bằng nguy cơ hỏng dữ liệu.

> 📌 Một câu: **torn write là hậu quả của việc một lệnh ghi của bạn không phải
> một hành động atomic dưới mắt phần cứng.**

## 5. Giải pháp: checksum cho mỗi entry

Định dạng một entry:

```
| len (4B) | crc32 (4B) | payload |
```

Khi replay: entry nào **checksum sai thì DỪNG LẠI**, coi như từ đó trở đi
chưa từng xảy ra.

```python
def entry(p: bytes) -> bytes:
    return len(p).to_bytes(4,'little') + zlib.crc32(p).to_bytes(4,'little') + p

def replay(path):
    data = open(path,'rb').read(); state, pos = {}, 0
    while pos < len(data):
        if pos+8 > len(data): break                 # header cut giua chung
        ln  = int.from_bytes(data[pos:pos+4],'little')
        crc = int.from_bytes(data[pos+4:pos+8],'little')
        if ln == 0: break                           # <-- xem muc 6!
        body = data[pos+8:pos+8+ln]
        if len(body) < ln: break                    # payload thieu
        if zlib.crc32(body) != crc: break           # CHECKSUM SAI
        ...apply(body)...
        pos += 8+ln
    return state
```

Kết quả thử cả 4 tình huống crash trên cùng một log `[a=1, b=2, a=3, del b]`:

```
(a) append chua kip xay ra    -> a=3, b=2   (het file)
(b) entry ghi duoc mot nua    -> a=3, b=2   (payload thieu)
(c) file dai ra, du lieu = 00 -> a=3, b=2   (len=0)
(d) hong 1 bit                -> a=3, b=2   (CHECKSUM SAI)
```

**Mọi đường đều ra cùng một trạng thái hợp lệ.**

> Đây là ý nghĩa câu sách nói: *"nếu checksum sai thì coi như lần update đó
> **chưa từng xảy ra** — điều này làm cho việc update log trở nên **atomic**."*
>
> **Atomic** ở đây = một entry **hoặc được tính trọn vẹn, hoặc bị bỏ hoàn toàn**.
> Không bao giờ "áp dụng được một nửa".

## 6. ⚠️ Cái bẫy: `crc32` của chuỗi rỗng bằng 0

Ở trường hợp (c), vùng toàn byte `00` được đọc thành `len = 0`, `crc = 0`.
Mà:

```python
zlib.crc32(b'') == 0   # True!
```

→ Vùng toàn số 0 **trông y hệt một entry rỗng hợp lệ** và **vượt qua được
kiểm tra checksum**.

**Bản vá:** coi `len == 0` là **dấu hiệu hết log**.

Đây là thứ mọi hệ thống WAL thật đều phải xử lý — **checksum một mình không đủ**
để chống vùng dữ liệu toàn số 0. Các cách khác thường dùng:

- **Magic bytes** ở đầu mỗi record (chương 6 của sách dùng `DB_SIG` cho meta page)
- Đưa **vị trí/offset của chính record** vào phần được tính checksum, nên một
  record toàn số 0 sẽ không khớp
- **Số thứ tự tăng dần** cho mỗi record

## 7. Giới hạn của checksum

| Loại hỏng | Checksum làm được gì |
|---|---|
| **Torn write** — ghi dở **TRƯỚC** khi `fsync` thành công | ✅ **Phát hiện và khôi phục được.** Vứt entry hỏng là xong |
| Hỏng **SAU** khi `fsync` (bit rot, đĩa lỗi) | ⚠️ **Phát hiện được, nhưng KHÔNG cứu được** |

### Vì sao phát hiện được mà không cứu được?

Checksum là **một con số tóm tắt**: 1000 byte nén lại thành 4 byte.
Nén như vậy thì **thông tin đã mất**. Nó chỉ đủ trả lời *"dữ liệu có còn nguyên
không?"* (có/không), **không đủ** để trả lời *"vậy dữ liệu đúng phải là gì?"*

> Giống như nhớ *"tổng các chữ số trong số điện thoại của tôi là 37"*. Ai chép sai
> một chữ số thì bạn **phát hiện được**, nhưng **không khôi phục được** số đúng.

### Vậy tại sao torn write lại cứu được?

Vì khi đó **không cần khôi phục gì** — chỉ cần **vứt đi**:

| | Entry hỏng nằm ở đâu | Làm gì |
|---|---|---|
| **Torn write** | **cuối log**, chưa ai được báo "thành công" | **Vứt** → về trạng thái cũ hợp lệ ✅ |
| **Hỏng sau `fsync`** | **giữa log**, đã báo client "thành công" | Vứt thì **mất dữ liệu đã cam kết** ❌ |

→ Torn write được cứu **không phải nhờ checksum**, mà nhờ **nó nằm ở cuối và chưa
được cam kết**. Checksum chỉ đóng vai **người gác cổng chỉ ra ranh giới**.

### Muốn cứu thì cần DƯ THỪA (redundancy)

Phải giữ thêm **bản sao của thông tin**, chứ không phải chỉ bản tóm tắt:

| Cách | Giữ dư thừa gì | Sửa được không |
|---|---|---|
| **Checksum** | 4 byte tóm tắt | ❌ chỉ phát hiện |
| **Backup** | cả một bản cũ | ✅ khôi phục về thời điểm sao lưu |
| **Replica** | cả một bản đầy đủ, luôn mới | ✅ đọc từ bản kia |
| **RAID / ZFS** | bản sao hoặc mã sửa lỗi | ✅ tự lành |
| **ECC RAM** | bit kiểm tra thêm | ✅ sửa 1 bit sai |

PostgreSQL bật `data_checksums` thì **phát hiện** page hỏng và **báo lỗi, từ chối
đọc** — chứ không sửa. Khôi phục là việc của bạn: **restore từ backup** hoặc
**failover sang replica**.

> 💡 **Checksum là báo cháy, không phải bình chữa cháy.**

---

## 📌 Đối chiếu với PostgreSQL

- **WAL của Postgres chính là một append-only log** — đúng ý tưởng mục này.
  Mỗi WAL record có **CRC32C**, và recovery dừng lại ở record đầu tiên hỏng.
- Vấn đề *"log phình vô hạn"* Postgres giải bằng **checkpoint**: định kỳ đẩy
  hết page bẩn xuống đĩa, rồi **xoá/tái sử dụng** các file WAL cũ hơn checkpoint.
  (Sách sẽ giải bằng **free list** ở chương 7.)
- Vấn đề *"không có index, phải quét cả log"* Postgres giải bằng cách **không
  đọc từ WAL** khi truy vấn — WAL chỉ dùng để **redo khi recovery**. Dữ liệu thật
  nằm trong **heap + B-tree**. Đây đúng là lời khuyên của sách: *log phải được
  kết hợp với một cấu trúc index*.
- Cái bẫy `crc32(b'') == 0`: Postgres tránh bằng cách đưa **`xl_prev`** (con trỏ
  tới record trước) vào mỗi WAL record — một vùng toàn số 0 sẽ có `xl_prev` sai
  và bị loại ngay.

## Câu hỏi tự kiểm tra

1. Vì sao log ghi nhanh hơn chiêu rename của 1.2, nhưng đọc lại chậm hơn?
2. Checksum làm cho update log trở nên "atomic" theo nghĩa nào?
3. Vì sao một vùng toàn byte `00` lại vượt qua được kiểm tra `crc32`?
5. Vì sao torn **log** cứu được mà torn **page** thì không?
4. Checksum có cứu được dữ liệu bị hỏng **sau** khi `fsync` thành công không?
