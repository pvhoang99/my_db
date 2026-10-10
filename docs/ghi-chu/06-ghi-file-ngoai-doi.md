# Ghi file ngoài đời — 3 hàm SaveData và ai dùng cách nào

> Nối tiếp [03-nen-tang-file-he-thong.md](03-nen-tang-file-he-thong.md) và
> [05-append-only-log.md](05-append-only-log.md).
> Code: `internal/storage/safewrite/`. Liên quan sách **mục 1.1, 1.2, 1.4**.

## 1. Ba hàm, cùng chữ ký, khác nhau khi crash

| Hàm | Làm gì | Crash giữa chừng |
|---|---|---|
| `SaveData1` | `O_TRUNC` → `Write` → `Sync` | ❌ file **rỗng** hoặc **dở dang** |
| `SaveData2` | file tạm cùng thư mục (`O_EXCL`) → `Write` → `Sync` → `rename` | ✅ với reader · ⚠️ chưa chắc với mất điện |
| `SaveData3` | `SaveData2` + **fsync thư mục cha** | ✅ cả reader lẫn mất điện |

Đo thật (`go test -race`, 10/10/2026):

```
SaveData1: doc 5300 lan -> 1573 lan thay file RONG, 2 lan thay du lieu RACH
SaveData2: doc 14592 lan, rach 0 lan
SaveData3: doc 14647 lan, rach 0 lan
```

### Lý do từng bước của SaveData2

| Bước | Vì sao |
|---|---|
| Ghi vào file **khác** | Không động vào dữ liệu cũ |
| **Cùng thư mục** | `rename` chỉ sửa bảng `tên → inode`; khác filesystem thì OS phải copy → mất atomic |
| `O_EXCL` | File tạm phải mới tinh; trùng tên thì báo lỗi, không âm thầm phá file người khác |
| `Sync` **trước** `rename` | Đảo lại thì có lúc tên trỏ vào file mà dữ liệu còn trong RAM |
| `defer` xoá file tạm khi lỗi | Không để rác. ⚠️ Phải dùng `err =`, không `err :=`, để closure thấy lỗi |

### Vì sao SaveData3 cần fsync thư mục

> **Dữ liệu nằm trong file, cái tên nằm trong thư mục — cả hai đều cần fsync.**

- `fp.Sync()` → **nội dung** bền vững
- `dir.Sync()` → **cái tên** trỏ tới nội dung mới bền vững

Test hiện tại **không phân biệt được** SaveData2 và SaveData3 — chúng chỉ khác nhau
khi **mất điện**, còn test chỉ kiểm reader đồng thời. Muốn thấy phải mô phỏng mất
điện (VM, `dm-flakey`).

## 2. Vì sao SaveData1 phải có `O_TRUNC`

(Chữ **O**, *Open + TRUNCate*, không phải số 0.)

Ghi vào file có sẵn chỉ **đè từ byte 0**; phần đuôi cũ vẫn còn:

```
File cu:   "phien ban mot rat dai"     Ghi: "ngan"

khong TRUNC: [ngann ban mot rat dai]   <- dau moi + duoi cu
co TRUNC:    [ngan]
```

`TestGhiDeNhieuLan` (ghi dài → ngắn) được viết để bắt đúng lỗi này.

**Nhưng `O_TRUNC` cũng chính là thứ mở CỬA SỔ CHẾT.** Bỏ nó đi (ghi đè rồi
`Truncate` sau) thì crash cho ra **nửa mới nửa cũ** — còn tệ hơn vì trông vẫn
hợp lệ. Gốc rễ không phải `O_TRUNC`, mà là **ghi đè tại chỗ**.

## 3. Ngoài đời, chương trình ghi file thế nào?

Phân biệt bằng **số inode**: giữ nguyên = ghi đè tại chỗ; đổi = file mới + rename.

```
inode ban dau:          17565818
sau 'echo > f':         17565818   <- ghi de tai cho (kieu SaveData1)
sau 'sed -i':           17565822   <- file tam + rename (kieu SaveData2)
sau python open('w'):   17565822   <- ghi de tai cho (kieu SaveData1)
```

`strace sed -i` — gần như y hệt SaveData2, nhưng **không fsync**:

```
openat("f", O_RDONLY)
openat("./sedmNax0Q", O_RDWR|O_CREAT|O_EXCL, 0600)
rename("./sedmNax0Q", "f")
```

| Cách | Ai dùng |
|---|---|
| Ghi đè tại chỗ | `echo >`, Python `open('w')`, Go `os.WriteFile` — **mặc định của thư viện chuẩn** |
| File tạm + rename | `sed -i`, git (`index.lock`), package manager, nhiều editor |
| Không cái nào ở trên | **Database** — file quá lớn để ghi lại cả file |

> **Cách tệ nhất lại phổ biến nhất, vì nó là mặc định.**

## 4. Còn sót lại trong code (review 10/10)

1. `SaveData3`: rename xong mà `d.Sync()` lỗi → reader **đã thấy** dữ liệu mới
   nhưng hàm trả lỗi. "Lỗi" không có nghĩa "chưa có gì xảy ra".
2. Bị `kill -9` giữa chừng → `defer` không chạy → file `db.tmp.*` **mồ côi**.
   DB thật dọn chúng lúc khởi động.
3. Comment `TODO: cài đặt.` và `ErrNotImplemented` đã cũ.

## Câu hỏi tự kiểm tra

1. Vì sao test reader-đồng-thời không phân biệt được SaveData2 và SaveData3?
2. `sed -i` an toàn với reader hay với mất điện?
3. Vì sao database không dùng được chiêu rename?
