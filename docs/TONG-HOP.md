# Tổng hợp kiến thức — minidb

> Bản đồ toàn bộ những gì đã học, xếp **theo ý tưởng** chứ không theo thứ tự chương.
> Chi tiết từng chủ đề: [`docs/ghi-chu/`](ghi-chu/). Theo thời gian: [`docs/nhat-ky/`](nhat-ky/).
>
> Trạng thái: chương 1–4, 6 (nửa đầu), 10 của sách · **M1 ✅ M2 ✅** · 5.197 dòng Go, test xanh.

---

# A. Một nguyên tắc duy nhất

Nếu chỉ được giữ lại một câu từ tất cả những gì đã học:

> ## Đừng phá dữ liệu cũ. Tạo bản mới bên cạnh, rồi chuyển sang nó bằng **một** thao tác atomic.

Nó xuất hiện **y hệt** ở mọi tầng, chỉ đổi quy mô:

| Ở đâu | "Bản mới bên cạnh" | "Một thao tác atomic" |
|---|---|---|
| **Ghi file** (ch.1, `SaveData2`) | file `db.tmp` | `rename` đổi một dòng trong bảng thư mục |
| **Filesystem** (inode) | inode mới + block mới | dòng `tên → inode` trong thư mục |
| **Log** (ch.1.3) | entry nối vào cuối | checksum quyết định entry có tính hay không |
| **COW B+tree** (ch.3) | chép cả đường đi leaf→root | ghi `root_ptr` |
| **KV store** (ch.6) | page append vào cuối file | ghi **meta page** |
| **Transaction** (ch.11) | cây `pending` riêng | commit = đổi root |
| **SSD** (phần cứng!) | ghi vào page flash trống khác | FTL đổi bảng ánh xạ |
| **PostgreSQL** | tuple mới mang `xmin` mới | WAL record `COMMIT` + CLOG |

**Hệ quả quan trọng:** bài toán "giữ an toàn cả một cấu trúc phức tạp" luôn được
**thu gọn về: ghi an toàn ĐÚNG MỘT con trỏ**. Đó là lý do meta page / root pointer
tồn tại.

---

# B. Vì sao database trông như vậy — 3 đặc tính phần cứng

Mọi thiết kế đều **chảy ra từ ba sự thật** về phần cứng. Không có gì tuỳ ý.

## B1. Đĩa chỉ đọc/ghi theo **khối**, không theo byte

Sector 512B → OS page 4KB → DB page (sách 4KB, **Postgres 8KB**).

| Hệ quả |
|---|
| File 1 byte vẫn **chiếm 4096 byte** |
| Node B-tree phải **vừa đúng một page** — dùng nửa page là phí nửa lần IO |
| Sinh ra **slotted page**: header + mảng ô + vùng trống + dữ liệu |
| **Fanout** = `page_size ÷ (key + con trỏ + offset)` → 4KB/18,4B ≈ **222 nhánh** |

## B2. Đọc **ngẫu nhiên** đắt hơn đọc **tuần tự** rất nhiều

HDD: seek ~5–10ms + chờ quay ~4ms → **~10ms một lần đọc ngẫu nhiên**.
SSD nhanh hơn hàng nghìn lần nhưng **khoảng cách vẫn còn**.

| Hệ quả |
|---|
| **Chi phí tính bằng SỐ PAGE ĐỌC**, không phải số phép so sánh |
| Cây nhị phân vô dụng: đo thật 104.334 từ → **41 tầng** (lý thuyết log₂ chỉ 16,7 — vì BST không tự cân bằng) |
| B+tree cùng dữ liệu → **3 tầng**. 1 tỷ bản ghi → **4 tầng** |
| Tầng trên cây rất bé (332KB cho 1 tỷ bản ghi) → **nằm thường trú RAM** → đọc đĩa thật chỉ **2 lần** |
| Key càng to → fanout càng nhỏ → cây càng cao: `int64` **4 tầng**, URL 120B **7 tầng** |
| → Đó là con số đằng sau lời khuyên "primary key nên nhỏ" |

## B3. `write()` **không** ghi xuống đĩa

Nó chép vào **page cache (RAM)** rồi báo xong. Và page cache là **của chung** —
mọi tiến trình đọc đều qua đúng cái cache đó.

| Hệ quả |
|---|
| Dữ liệu **hiện ra với người khác ngay**, dù chưa `fsync` |
| **Mất điện = mất dữ liệu**, dù đã báo thành công |
| `fsync` ép xuống đĩa **và đợi** → đó là lý do mọi DB chậm hơn ghi file thường |
| Nhưng `fsync` **không phải con đường duy nhất**: kernel tự đẩy mỗi 5s, trang quá 30s thì ép, vượt 10% RAM thì ghi ngầm |
| → `fsync` là con đường duy nhất bạn **biết chắc và kiểm soát được thời điểm** |

---

# C. Bốn câu hỏi lớn

## C1. Làm sao ghi mà không mất dữ liệu? *(chương 1)*

**Hai vấn đề VUÔNG GÓC — đây là ý quan trọng nhất của chương 1:**

| Vấn đề | Công cụ | **Không** giúp gì |
|---|---|---|
| Reader thấy dữ liệu nửa vời | **`rename`** | ❌ không chống mất điện |
| Mất điện thì còn gì | **`fsync`** | ❌ không chống reader thấy rách |

→ Mỗi khi thấy **"X là atomic"**, phải hỏi ngay: **"atomic so với CÁI GÌ?"**

`rename` atomic **với reader** ✅ · atomic **với mất điện** ❌

**Ba cách ghi, đo thật bằng reader song song:**

| | Cách làm | Kết quả |
|---|---|---|
| `SaveData1` | `O_TRUNC` ghi đè tại chỗ | 37.711 lần đọc → **2.530 lần thấy file RỖNG + 132 lần RÁCH** |
| `SaveData2` | tạm + `fsync` + `rename` | 118.682 lần đọc → **0** |
| `SaveData3` | + `fsync` **thư mục cha** | 119.900 lần đọc → **0** |

**Vì sao cần fsync thư mục:** dữ liệu nằm trong file, **cái tên nằm trong thư mục**
— cả hai đều là dữ liệu trên đĩa, **cả hai đều cần fsync**. Thiếu bước này thì
mất điện xong: dữ liệu mới an toàn nhưng **cái tên vẫn trỏ bản cũ** → không ai tìm thấy.

### Điểm cam kết (commit point)

Database **không** hứa "không bao giờ mất update". Nó hứa đúng hai điều:

> 1. Cái gì **đã báo thành công** thì **không bao giờ mất** *(durability)*
> 2. Cái gì **chưa báo thành công** thì **không để lại trạng thái nửa vời** *(atomicity)*

→ Mất điện giữa `fsync`: **mất, và không sao cả.** Chưa hứa gì, bản cũ còn nguyên.
→ Cái **không** được phép là rơi vào **trạng thái thứ ba**: nửa cũ nửa mới.

### Torn write

Một `write()` **không atomic dưới mắt phần cứng** — nó bị chẻ thành nhiều sector.
Mất điện giữa chừng → nửa mới nửa cũ.

| | Torn **log** | Torn **page** |
|---|---|---|
| Phần rách ở | **cuối** | **giữa** dữ liệu |
| Bản cũ | **còn nguyên** phía trước | **đã bị ghi đè — mất rồi** |
| Xử lý | vứt entry cuối ✅ | **vứt thì về đâu?** ❌ |

→ Chống torn page: **COW** (sách, SQLite) hoặc **full-page write / double-write** (Postgres, MySQL).

### Checksum

Định dạng entry: `| len 4B | crc32 4B | payload |`. Sai checksum → **coi như update
chưa từng xảy ra** → log update trở nên **atomic**.

**⚠️ Bẫy đã dẫm:** `crc32(b'') == 0`, nên vùng toàn byte `00` (len=0, crc=0) **lọt
qua kiểm tra**. Phải coi `len == 0` là hết log. Postgres tránh bằng `xl_prev`.

**Giới hạn:** checksum **phát hiện** chứ **không sửa** — nó là bản tóm tắt 4 byte,
không chứa đủ thông tin dựng lại dữ liệu đúng.
→ **Checksum là báo cháy, không phải bình chữa cháy.** Muốn sửa cần **dư thừa**:
backup, replica, RAID/ZFS, ECC.

→ Torn write cứu được **không phải nhờ checksum**, mà nhờ **nó ở cuối và chưa được
cam kết**. Checksum chỉ **chỉ ra ranh giới**.

## C2. Làm sao tìm nhanh mà vẫn sửa được? *(chương 2)*

Mảng đã sắp xếp: tìm `O(log N)` nhưng **sửa `O(N)`**. Hai lối thoát:

| | **B+tree** | **LSM-tree** |
|---|---|---|
| Ý tưởng | **chẻ mảng thành nhiều mảng con lồng nhau** | **đệm thay đổi rồi MERGE dần** |
| Ghi | tại chỗ / COW | **nối thêm** → nhanh |
| Đọc | 3–4 page | phải gộp **mọi tầng** → chậm hơn |
| Thu hồi chỗ | free list | **compaction** (merge tự dọn) |
| Xoá | xoá thật | **tombstone** (cờ ở tầng mới) |

**LSM chính là chương 1 làm ở quy mô lớn**: tầng 1 là log + MemTable, các tầng dưới
bất biến. Và `1.2 → 1.3` dạy đúng bài học đó: ghi nhanh thì đọc chậm và ngược lại —
cần **ghép log với một cấu trúc index**.

## C3. Làm sao sửa cây trên đĩa mà an toàn? *(chương 3–4, 6)*

**Ba bất biến của B+tree:**
1. Mọi leaf **cùng chiều cao**
2. Node **không vượt** kích thước hằng số → vỡ thì **split**
3. Node **không rỗng** → vỡ thì **merge**

Split/merge **lan truyền lên tới root** → cây cao thêm / thấp đi 1 tầng, và
bất biến 1 vẫn giữ vì **mọi leaf đổi cùng lúc**.

**Vấn đề:** split sửa nhiều page, crash giữa chừng → cây hỏng.
**Lời giải COW:** chép **chỉ đường đi leaf→root**, các subtree khác **dùng chung**.

```
      d              d          D*
     / \            / \        / \
    b   e    ==>   b   e   +  B*  e      (chép B, D; dùng chung a, e)
   / \            / \        / \
  a   c          a   c      a   C*
```

Được **miễn phí**: snapshot isolation (giữ root cũ) · crash recovery (dùng root cũ) ·
mô hình **1 writer – nhiều reader**, reader không chặn writer *(= RCU, hạt giống MVCC)*.
Trả giá: **write amplification** `O(log N)` mỗi update.

**Node = một mảng byte**, không có bước serialize:
```
| type | nkeys | pointers | offsets | key-values | unused |
|  2B  |  2B   | nkeys*8B | nkeys*2B|    ...     |        |
```
Chèn một key vào giữa → **gần như mọi byte đổi chỗ** (vì offset là tương đối).

**Tách cây khỏi đĩa bằng 3 callback** `get/new/del` → test được toàn bộ logic cây
**không cần đụng đĩa**. (Áp dụng được y nguyên cho mọi dự án.)

**Update 2 pha — vì sao 2 lần `fsync`:**
```
1. ghi các node mới
2. fsync          ← ép THỨ TỰ giữa 1 và 3 (thứ tự ghi ≠ thứ tự xuống đĩa)
3. ghi root pointer (atomic)
4. fsync          ← làm mọi thứ bền vững
```

## C4. Làm sao biến bảng/index thành KV? *(chương 8, 10)*

| | key | value |
|---|---|---|
| bảng `t1` | `prefix1, k1, k2` | `v1, v2` |
| index `idx1(v1)` | `prefix2, v1, k1, k2` | *(rỗng)* |

- **Primary key** đi vào key, phần còn lại vào value
- **Secondary index = thêm cặp KV**, sắp theo cột khác, value trỏ về primary key
- Index **không unique** → **nhét primary key vào key** cho nó duy nhất
- Index **nhiều cột** `(city, name)` = danh bạ sắp theo (họ, tên): dùng được cho
  `city=?` và `city=? AND name=?`, **không** dùng được cho `name=?` *(prefix rule)*
- **Mã hoá bảo toàn thứ tự**: số big-endian + **lật bit dấu**; chuỗi kết thúc null
  và escape; cột thiếu → `+∞`/`−∞`

> ### ⚠️ Atomicity KHÔNG cộng dồn được
> Từng thao tác atomic + từng thao tác atomic **≠** cả nhóm atomic.
> Update một dòng = sửa heap + xoá key index cũ + chèn key index mới. Crash ở giữa
> → index lệch heap. **Đây là lý do phải có transaction**, và là lý do giao diện
> `get/set/del` đơn giản rất hạn chế.

---

# D. Nguyên lý ⇄ PostgreSQL thật

Sách dựng DB kiểu **SQLite/BoltDB**. Postgres khác ở **gần như mọi tầng**:

| | Sách | **PostgreSQL** |
|---|---|---|
| Page | 4KB | **8KB** |
| Dữ liệu nằm ở | **trong B+tree** (clustered) | **heap file** riêng; index chứa `(key, TID)` |
| Update page | **copy-on-write** | **tại chỗ** |
| Chống mất dữ liệu | COW + ghi root atomic | **WAL** + redo; **full-page write** sau checkpoint |
| Thu hồi chỗ | **free list** | **VACUUM** + FSM + Visibility Map |
| MVCC | giữ root cũ (miễn phí) | **`xmin`/`xmax`** mỗi tuple + snapshot + CLOG |
| Concurrency | **1 writer** | nhiều writer + lock manager + SSI |
| Buffer | `mmap` | **shared buffer pool** tự quản (clock-sweep) |
| So sánh key | mã hoá byte bảo toàn thứ tự | **operator class** gọi hàm so sánh → phụ thuộc **collation** |
| Scan tuần tự | quay ngược lên cha | **right-link** (B-link tree, Lehman–Yao) |
| Merge node | có merge | **gần như không merge** → index **bloat**, cần `REINDEX` |
| Key trùng | không cho | cho; **TID là cột khoá ẩn** (PG12+) |
| Client | thư viện nhúng | server + **wire protocol v3** |

### Đã kiểm chứng bằng tay trên PostgreSQL 18

- `pageinspect`: **heap + ctid**, hình dạng cây **3 tầng** thật
- **Index-only scan** (chỉ khi Visibility Map cho phép)
- **Skip scan** — PG 18 mới có (sách PG Internals viết cho PG 14 bảo "chưa có")
- **Bitmap scan** khi kết quả nhiều
- **UPDATE làm dòng đổi địa chỉ** (ctid mới) → sinh ra HOT update
- **Checksum chỉ có trên đĩa**: `get_raw_page` (bản RAM) cho `0`, file trên đĩa cho
  `51463` → **page trong RAM ≠ page trên đĩa**

### Bên trong một page (đã mổ tận byte)

**5 phần:** header (24B) · mảng ô · vùng trống · dữ liệu (**mọc ngược từ cuối**) · special

> **Ô là chỗ ngồi đánh số, dữ liệu là người ngồi.**
> Người đổi chỗ được, số ghế cố định, ghế trống thì người mới ngồi vào.

- **Ba thứ hay lẫn:** *page header* giữ ranh giới · *ô* giữ con trỏ (4B) · *tuple header* giữ thông tin phiên bản
- `ctid = (page, OffsetNumber)`, **OffsetNumber đếm từ 1**, khác `lp_off` (byte thứ mấy)
- Vì sao cần ô ở giữa: **VACUUM dồn page được mà ctid không đổi**
- Một page **nhiều dòng**; một **dòng** có thể **nhiều tuple** (phiên bản)
  → **Tuple = một phiên bản của một dòng, lưu trên đĩa**
- `DELETE` **không xoá byte nào** (chỉ ghi `xmax`); VACUUM dồn page, ô thành `UNUSED` rồi dùng lại
  → `SELECT` không `ORDER BY` **có thể khác thứ tự chèn**
- Dòng > 8KB: tuple thì không (max ~8160B) nhưng **dòng thấy được thì có** — nhờ **TOAST**

---

# E. Những chỗ đã nhầm — ôn lại kỹ

Rút từ 3 lần kiểm tra kiến thức (5/8 · 6/8).

| Nhầm gì | Đúng là |
|---|---|
| **fd trỏ vào tên file** | **fd trỏ thẳng vào inode.** Tên chỉ dùng một lần lúc `open`. → `rm` file đang mở **vẫn đọc được, dung lượng chưa giải phóng**; `rename` đè lên thì reader cũ **vẫn thấy bản cũ trọn vẹn** |
| `lower = 100` → **"100 ô"** | `lower/upper/special` là **VỊ TRÍ BYTE**. Số ô = `(100 − 24) ÷ 4 = 19` |
| `int8` sau `int4` ở **byte 28** | **byte 32** — `int8` phải ở bội số của 8 → **4 byte đệm**. Thứ tự cột ảnh hưởng kích thước dòng |
| `nlink` thư mục = `1 + số con` | `= 2 + số thư mục con` (thiếu dòng `.`) |
| Tưởng split "vẫn tìm thấy vì phải atomic" | **Atomic không tự có.** Phải tự thiết kế ra nó |
| Tưởng `fsync` là đường duy nhất xuống đĩa | Kernel **tự đẩy** theo lịch riêng; `fsync` chỉ là đường **kiểm soát được** |

> 🔑 **Hai lỗi cùng một gốc** (`lower=100` và "0,5 entry"): **đổi đơn vị trước khi tính.**

---

# F. Code đã viết ⇄ khái niệm

| File | Khái niệm |
|---|---|
| `internal/storage/safewrite/` | Chương 1: 3 cách ghi file + test reader song song |
| `internal/storage/page.go, file.go` | **M1** — slotted page 8KB, 5 phần |
| `internal/access/heap/tuple.go, heap.go` | **M1** — tuple header, `Insert`/`Fetch(ctid)`/`Scan` |
| `cmd/pagedump/` | **M1** — đọc **thẳng file Postgres**, khớp từng số với `pageinspect` |
| `internal/buffer/pool.go` | **M2** — buffer pool (🔍 `Buffers: hit/read`) |
| `internal/access/btree/` | **M2** — page, `search`, `insert` + split, `scan`, `check` |
| `internal/executor/scan.go` | **M2** — Seq Scan · Index Scan · Bitmap Scan |

**Thành tích đáng nhớ:** chèn 100.000 dòng → **736 page, mọi `ctid` khớp Postgres
từng con một** (`147 → (1,1)`, `100000 → (735,30)`). Page 0 chứa 146 dòng, các page
sau 136 — vì id nhỏ thì dòng nhỏ. Seq scan đọc đúng 736 page = `Buffers: 736`.

---

# G. Kỹ năng (quan trọng hơn kiến thức)

**1. Biến lời khẳng định thành con số.**
Sách nói "cách này an toàn hơn" → dễ gật đầu rồi quên. `37.711 → 2.530 lần rỗng`
vs `118.682 → 0` thì **không còn tin, mà biết**. Và test đó còn **bảo vệ bạn** sau này.

> Thói quen phân biệt người **làm** database với người **dùng** database:
> **mọi lời hứa về độ bền đều phải có một bài test chứng minh.**

**2. Đối chiếu từng con số với hệ thật.** `pagedump` của bạn đọc file Postgres và
khớp từng byte — đó là cách duy nhất biết mình hiểu đúng.

**3. Tách logic khỏi IO bằng callback.** 3 callback `get/new/del` cho phép test cây
không cần đĩa.

**4. Database là nơi trừu tượng ngừng bảo vệ bạn.** Để viết đúng 20 dòng ghi file
cần biết: inode, bảng thư mục, page cache, ranh giới filesystem, `O_EXCL`, và phạm
vi biến trong closure Go. Bình thường `os.WriteFile` là đủ. Ở đây thì không.

---

# H. Bản đồ còn lại

| | Nội dung | Câu hỏi nó trả lời |
|---|---|---|
| **M3** | WAL, crash recovery | Postgres sửa page **tại chỗ** mà vẫn an toàn khi mất điện thế nào? |
| **M4** | MVCC, VACUUM | Vì sao `UPDATE` tạo dòng mới? Ai thấy bản nào? |
| **M5** | Parser, catalog | SQL biến thành cấu trúc dữ liệu thế nào? |
| **M6** | Planner, executor, EXPLAIN | Postgres chọn cách chạy thế nào? |
| **M7** | Wire protocol | `psql` nói chuyện với server thế nào? |

**Hoặc** đi sâu tầng index theo [kế hoạch 01](01-ke-hoach-hoc-index-song-song.md):
Lehman–Yao, high key, chọn điểm split, xoá page 2 pha, WAL của split.

### Món nợ
- [ ] Chèn 1 dòng 46 byte vào page trống → `lower`, `upper` bằng bao nhiêu?
- [ ] Bảng chèn theo thứ tự `city`, `WHERE city='HN'` đọc khoảng mấy page?
- [ ] double-write (→ M3.3) · HOT/VACUUM (→ M4) · **fsyncgate** (nợ từ 09/10)
