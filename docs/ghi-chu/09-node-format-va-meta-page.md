# Node trên đĩa và meta page (chương 4 + chương 6)

> Nối tiếp [08-btree-split-va-copy-on-write.md](08-btree-split-va-copy-on-write.md).
> Liên quan sách **chương 4** và **mục 6.1–6.5**.
> ⏳ **Chưa học kỹ:** chương 5 (delete/merge trong code), mục 6.6 (xử lý lỗi).

## 1. Một node = một mảng byte 4KB

```go
type BNode []byte   // ghi thang xuong dia, khong can serialize
```

```
| type | nkeys | pointers   | offsets    | key-values      | (trong) |
|  2B  |  2B   | nkeys x 8B | nkeys x 2B | klen vlen k v ..|         |
```

### Nhìn tận byte — leaf `{a:1, c:333}`

```
[ 0.. 3] header   type=2 (leaf) nkeys=2
[ 4..19] pointers  2 x 8B, leaf khong dung -> toan 0
[20..21] offset[1] = 6     <- kv 0 KET THUC o byte 6 cua vung kv
[22..23] offset[2] = 14
[24..29] kv 0: klen=1 vlen=1 key="a" val="1"
[30..37] kv 1: klen=1 vlen=3 key="c" val="333"
```

| Phần | Để làm gì |
|---|---|
| `type` | leaf (có value) hay internal (có pointer) |
| `pointers` | số page của node con = "con trỏ" trên đĩa |
| `offsets` | KV dài ngắn khác nhau → nhảy thẳng tới KV thứ n trong O(1) → **binary search được** |
| `klen vlen` | độ dài đi trước dữ liệu (giống `len` của log) |

Chỉ lưu offset **kết thúc** vì offset đầu của KV 0 luôn = 0.

### Chèn `b:22` → gần như MỌI byte đổi chỗ

```
[ 0.. 3] nkeys=3                 <- doi
[ 4..27] pointers 3 x 8B         <- dai them
[28..33] offsets 6, 13, 21       <- doi cho
[34..54] kv a, b(MOI), c         <- a doi tu byte 24 sang 34
```

→ Đằng nào cũng viết lại gần hết node, nên **copy-on-write gần như miễn phí**.
Postgres tránh việc dời này nhờ dữ liệu **mọc ngược từ cuối page** (slotted page).

## 2. Tách cây khỏi đĩa bằng 3 callback

```go
type BTree struct {
    root uint64
    get func(uint64) []byte   // doc page N
    new func([]byte) uint64   // ghi page moi, tra ve so page
    del func(uint64)          // tra page N
}
```

Test: dùng `map` trong RAM. Chương 6: thay bằng file thật.

`treeInsert` đệ quy: xuống tới leaf → chèn → đi lên, mỗi tầng **tạo bản sao mới của
chính nó** → đây là "copy lan lên root" viết thành code. Có thể split thành **3** vì
một KV được phép gần bằng cả node.

## 3. Bố cục file (chương 6)

```
| page 0: META | page 1 | page 2 | ... | (cuoi file) <- page moi chi NOI THEM
```

```
META: sig = "BuildYourOwnDB06" | root = 5 | page_used = 6
```

**Vì sao lưu `page_used` mà không dùng kích thước file?** Sau mất điện, kích thước
file có thể **không khớp** dữ liệu thật (vùng mới toàn số 0 — đúng tình huống 4 của
demo checksum). Chỉ tin `page_used`, vì nó được ghi **sau** fsync dữ liệu.

## 4. Update hai pha — vì sao 2 lần fsync

```
(1) ghi page moi   (2) fsync   (3) ghi meta page   (4) fsync
```

**Thứ tự gọi `write` ≠ thứ tự dữ liệu xuống đĩa.** Bỏ fsync (2) thì meta có thể
xuống trước, page mới chưa xuống → meta trỏ vào rác. fsync (2) là **bức tường**.

| SaveData2/3 | Chương 6 |
|---|---|
| Ghi file tạm | (1) ghi page mới |
| `fp.Sync()` | (2) fsync |
| `rename` | (3) ghi meta |
| fsync thư mục | (4) fsync |

| Crash ở | Kết quả |
|---|---|
| (1), (2) | Cây cũ; page mới là rác, lần sau ghi đè lên luôn |
| (3) | Meta cũ hoặc mới — đều ổn vì page mới **đã** trên đĩa |
| (4) | Cây mới |

Điều kiện: bước (3) phải **atomic**.

## 5. Ghi meta page atomic

1. **Tin phần cứng (sách dùng):** 32 byte nằm gọn 1 sector; phần lớn ổ ghi trọn
   sector hoặc không. Postgres `pg_control` dựa vào điều này.
2. **Double buffering (an toàn hơn):** 2 meta page có **version + checksum**, ghi
   luân phiên. Mở DB: bỏ bản checksum sai, lấy version lớn hơn. SQLite, LMDB dùng.
   = **log xoay vòng 2 entry** — đúng bài học checksum chương 1.

> Chương 3: an toàn cả cây → thu gọn về 1 con trỏ root.
> Chương 6: con trỏ root → fsync đúng thứ tự + meta trong 1 sector (hoặc 2 meta + checksum).

## Câu hỏi tự kiểm tra

1. Vì sao cần mảng offset?
2. Bỏ fsync ở bước (2) thì hỏng thế nào?
3. Crash lúc đang ghi meta mà meta là bản mới — vì sao vẫn an toàn?
