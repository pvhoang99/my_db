# Bên trong một page — hỏi đáp

> Nối tiếp [11-postgres-index-thuc-te.md](11-postgres-index-thuc-te.md). Code: `internal/storage/page.go`,
> `internal/access/heap/`. Hình động 6 bước: https://claude.ai/artifact/WA7JYzAUeUSECo9xiwJZ6s

## 1. Một page chia làm 5 phần

```
byte 0       24                    lower           upper              special    8192
     +-------+---------------------+---------------+------------------+----------+
     |  (1)  |        (2)          |      (3)      |       (4)        |   (5)    |
     | PAGE  |   MANG O            |   CHO TRONG   |   DU LIEU        | SPECIAL  |
     |HEADER |   o1 o2 o3 ...  ->  |               |  <- ... d2 d1    |          |
     +-------+---------------------+---------------+------------------+----------+
```

| # | Phần | Kích thước | Chứa gì |
|---|---|---|---|
| 1 | Page header | luôn **24 byte** | `lower`, `upper`, `special`, LSN, checksum |
| 2 | Mảng ô | **4 byte × số dòng** — mọc từ đầu xuống | mỗi ô trỏ tới một dòng |
| 3 | Chỗ trống | `upper − lower` | hết chỗ này là page đầy |
| 4 | Dữ liệu | tuỳ các dòng — mọc từ cuối lên | các tuple |
| 5 | Special | heap **0**, B-tree **16 byte** | B-tree: page trước/sau, tầng, cờ |

`lower`, `upper`, `special` là **ranh giới** giữa các phần.

Page 0 thật của `users`: header 0–24 · 147 ô 24–612 · trống 52 byte 612–664 · dữ liệu 664–8192.

## 2. Ba thứ hay bị lẫn: page header, ô, tuple header

| | Ở đâu | Chứa gì | Trỏ tới dòng? |
|---|---|---|---|
| **Page header** 24B | đầu page, 1 cái/page | ranh giới, LSN, checksum | **Không** |
| **Ô** 4B | sau page header, 1 ô/dòng | `lp_off`, `lp_len`, `lp_flags` | **Có — đây là con trỏ** |
| **Tuple header** 23B | đầu mỗi dòng | `xmin`, `xmax`, `ctid`, cờ, `t_hoff` | Không (ctid trỏ chính nó / bản mới hơn) |

> Page header giữ ranh giới, ô giữ con trỏ, tuple header giữ thông tin phiên bản.

Lấy dòng `(0,2)`: page 0 → ô số 2 (`lp_off=8144, len=46`) → byte 8144: tuple header rồi dữ liệu cột từ `t_hoff`.

## 3. OffsetNumber và ô

- **OffsetNumber = số thứ tự ô** trong page, nửa sau của ctid `(page, ô)`.
- Đếm **từ 1** (0 = không hợp lệ) → dòng đầu tiên là `(0,1)`.
- Kiểu `uint16` là đủ: tối đa 2048 ô/page; heap giới hạn **291**.
- ⚠️ **Đừng nhầm với `lp_off`**: OffsetNumber = ô **số mấy** (2); `lp_off` = dòng ở **byte thứ mấy** (8144).

**Mỗi ô luôn 4 byte; dòng thì dài ngắn khác nhau** (độ dài ghi trong `lp_len`). Nhờ ô cố định nên
tìm ô n tính thẳng được: byte `24 + (n−1)×4`.

**Mảng ô KHÔNG cố định**: dài `số ô × 4 = lower − 24`, mỗi INSERT dài thêm 4 byte. Mảng ô và
dữ liệu **cùng chia** chỗ trống — không dành sẵn vùng nào. Mảng gần như **không co lại**: ô của
dòng đã xoá vẫn nằm đó (trạng thái UNUSED) và được dùng lại.

### Vì sao cần ô ở giữa, không cho ctid trỏ thẳng tới byte?

Vì dữ liệu **dịch chỗ** khi Postgres dồn page. Có ô thì chỉ sửa `lp_off`, còn **số ô giữ nguyên**
→ ctid không đổi → index không phải sửa. Đây là ý nghĩa của **slotted page**.

## 4. Thứ tự trong một page — 6 bước đo thật

```
1 chen An    lower=28 upper=8160   o1->8160
2 chen Binh  lower=32 upper=8104   o2->8104         (o moi ben PHAI, du lieu moi ben TRAI)
3 chen Chi   lower=36 upper=8072   o3->8072
4 DELETE Binh                      o2 van con, chi ghi xmax=859 — khong byte nao bi xoa
5 VACUUM     lower=36 upper=8128   o2 -> UNUSED; Chi dich 8072 -> 8128 (don page); o3 KHONG doi so
6 chen Dung  lower=36 upper=8088   DUNG LAI o2 -> lower khong doi
   SELECT ctid,*:  (0,1) An | (0,2) Dung | (0,3) Chi   <- Dung chen sau cung nhung dung thu 2
```

| | Thứ tự | Đổi khi |
|---|---|---|
| Mảng ô | ô 1, 2, 3… theo thứ tự chèn | ô mới ở cuối, **hoặc dùng lại ô trống** |
| Dữ liệu | dòng mới luôn bên trái dòng cũ | VACUUM/dọn page **dồn lại** |
| ctid | (page, số ô) | **không bao giờ** khi dữ liệu dịch |
| SELECT không ORDER BY | theo thứ tự ô | có thể khác thứ tự chèn |

> **Ô là chỗ ngồi đánh số, dữ liệu là người ngồi.** Người đổi chỗ được, số ghế cố định, ghế trống thì người mới ngồi vào.

## 5. Một page chứa nhiều dòng

Không phải một page một dòng. **Bảng → nhiều page → mỗi page nhiều dòng → mỗi dòng một ô.**

| Dòng | Số dòng/page |
|---|---|
| `users` id nhỏ (~48B) | 146 |
| `users` id lớn (~56B) | 136 |
| ~2KB | ~4 |

## 6. Dòng và tuple

> **Tuple = một phiên bản của một dòng, lưu trên đĩa.**

UPDATE không sửa tại chỗ mà tạo tuple mới → một dòng có thể có nhiều tuple
(`id=1`: ô 1 chết, ô 147 sống). "Tuple" còn dùng cho mục trong index (index tuple).

## 7. Dòng lớn hơn 8KB?

- **Tuple trên page: tối đa ~8160 byte**, không bao giờ cắt ra hai page.
  Thử 1.100 cột `int8`: `ERROR: row is too big: size 8824, maximum size 8160`.
- **Dòng bạn thấy: có thể rất lớn, nhờ TOAST.** Tuple vượt ~2KB thì cột dài (`text`, `bytea`,
  `jsonb`) bị **nén**, vẫn to thì **cắt miếng ~2KB đưa sang bảng phụ** `pg_toast.pg_toast_<oid>`.

Đo thật — `text` 50KB ngẫu nhiên:

```
bang chinh:  1 o, lp_len = 46  = header 24 + int4 4 + con tro TOAST 18
bang TOAST:  26 mieng (25 x 1996 + 1300 = 51.200 byte), 7 page, moi mieng 1 o rieng
  ctid  chunk_id  chunk_seq
  (0,1)  19978       0
  (0,2)  19978       1  ...
```

Đọc lại: ô của dòng chính → con trỏ TOAST (`chunk_id`) → index bảng phụ → ctid 26 miếng → ghép theo `chunk_seq`.

Chỉ lỗi khi phần **không TOAST được** (cột dài cố định: int, bool, timestamp) vượt 8KB — cần
>1000 cột số; Postgres giới hạn 1.600 cột/bảng.

**minidb:** chưa có TOAST → `ErrTupleTooLarge` cho tuple > 8164 byte.

## Câu hỏi tự kiểm tra

1. Page có `lower=100`. Có bao nhiêu ô?
2. ctid `(3,7)`: OffsetNumber là bao nhiêu? `lp_off` có phải 7 không?
3. Vì sao VACUUM dồn page mà index không phải sửa?
4. Vì sao `SELECT * FROM t` không có `ORDER BY` có thể ra khác thứ tự chèn?
5. Bảng 1 cột `text` 1MB chèn được không? Bảng 1.100 cột `int8` thì sao?
