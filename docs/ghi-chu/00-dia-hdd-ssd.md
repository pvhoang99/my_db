# Đĩa là gì — và SSD khác HDD ra sao

> Viên gạch nền móng nhất. Dưới cả [01-file-la-gi.md](01-file-la-gi.md).

## 1. Đĩa là gì, và vì sao cần nó

Máy tính có hai loại chỗ để chứa dữ liệu:

| | **RAM** | **Đĩa** |
|---|---|---|
| Mất điện | 💀 **mất sạch** | ✅ **vẫn còn** |
| Tốc độ | ~100 GB/s | ~0,5–7 GB/s |
| Độ trễ | ~100 **nano**giây | ~100 **micro**giây (SSD) → ~10 **mili**giây (HDD) |
| Dung lượng | 8–64 GB | 500 GB – nhiều TB |
| Giá / GB | đắt | rẻ hơn ~50 lần |

Thuật ngữ: RAM là **volatile** (bay hơi), đĩa là **non-volatile** (bền vững).

Chú ý cột **độ trễ** — chênh nhau tới **100.000 lần** giữa RAM và HDD. Đây là
con số quan trọng nhất trong toàn bộ việc thiết kế database.

> **Đây chính là lý do database tồn tại:** dữ liệu phải nằm trên đĩa mới sống sót
> qua mất điện, nhưng đĩa chậm kinh khủng — nên cả ngành chỉ xoay quanh một câu
> hỏi: *làm sao chạm vào đĩa càng ít càng tốt, mà vẫn không mất dữ liệu?*

---

## 2. Đĩa chỉ làm việc theo khối, không theo byte

Bạn **không thể** bảo đĩa *"sửa giúp byte thứ 1.000.000"*. Đơn vị nhỏ nhất mà
phần cứng đọc/ghi được gọi là **sector** — thường **512 byte** (ổ đời mới dùng
4096 byte).

```bash
cat /sys/block/nvme0n1/queue/logical_block_size    # 512
```

Muốn sửa 1 byte, thực tế phải: **đọc cả sector → sửa trong RAM → ghi lại cả sector**.

Hệ điều hành còn gom to hơn nữa, thành **trang (page) 4096 byte**, vì làm việc
theo khối lớn thì hiệu quả hơn. Database lại gom to hơn tiếp (PostgreSQL dùng
**8KB**, sách dùng **4KB**).

> Đây là gốc rễ của chuyện *"file 1 byte vẫn chiếm 4096 byte"* ở
> [02-nen-tang-file-he-thong.md](02-nen-tang-file-he-thong.md).

---

## 3. HDD — ổ cứng cơ

### Cấu tạo

Giống **máy hát đĩa than**:

```
        ┌─── cần (actuator arm) ───┐
        │                      ┌───┤
   ╔════╪══════════════════╗   │ đầu đọc
   ║    ●                  ║   │
   ║   đĩa từ quay         ║◄──┘
   ║   (5400 / 7200 vòng/phút)
   ╚═══════════════════════╝
```

- **Đĩa từ (platter)** quay liên tục, dữ liệu ghi trên các vòng tròn đồng tâm.
- **Đầu đọc** gắn trên một cái cần, trượt ra vào để chọn vòng.

### Đọc một sector mất bao lâu?

Phải chờ **hai chuyển động cơ học**:

1. **Seek** — cần trượt tới đúng vòng: **~5–10 ms**
2. **Chờ quay** — đợi sector cần đọc quay tới dưới đầu đọc.
   Ổ 7200 vòng/phút → một vòng 8,3 ms → trung bình đợi nửa vòng: **~4 ms**
3. Đọc dữ liệu: gần như tức thì

**Tổng: ~10 ms cho một lần đọc ngẫu nhiên.**

### Hệ quả: đọc tuần tự vs ngẫu nhiên chênh nhau khủng khiếp

| | HDD |
|---|---|
| Đọc **tuần tự** (các sector liền nhau) | ~150 MB/s — tốt |
| Đọc **ngẫu nhiên** (nhảy lung tung) | ~100 lần/giây |

Làm một phép tính cho thấy rõ:

```
Đọc 1000 trang 4KB (tổng 4 MB):
  - nếu liền nhau  :  4 MB / 150 MB/s      = 0,03 giây
  - nếu rải rác    :  1000 × 10 ms         = 10 giây      (chậm 300 lần!)
```

> **Đây là lý do cây nhị phân không dùng được cho database.** Cây nhị phân với
> 1 triệu phần tử cao 20 tầng → 20 lần nhảy ngẫu nhiên → 0,2 giây cho **một**
> lần tra cứu. B+tree chỉ cao 3–4 tầng → nhanh gấp 5 lần. Chính là mục **2.4**
> của sách.

---

## 4. SSD — ổ thể rắn

### Cấu tạo

**Không có bộ phận chuyển động nào.** Chỉ là các **chip nhớ flash NAND**, giống
thẻ nhớ USB nhưng nhanh hơn nhiều.

```bash
lsblk -d -o NAME,SIZE,ROTA,MODEL
#  nvme0n1  476,9G  0  SAMSUNG MZVLB512HBJQ-000L7
#                   ^
#                   ROTA=0 -> khong quay -> SSD
```

Không có seek, không phải chờ quay → **đọc ngẫu nhiên nhanh gấp hàng nghìn lần HDD**.

| | HDD | SSD (NVMe) |
|---|---|---|
| Đọc tuần tự | ~150 MB/s | ~3.000–7.000 MB/s |
| Đọc ngẫu nhiên | ~100 lần/s | ~500.000 lần/s |
| Độ trễ | ~10 ms | ~0,1 ms |

### Nhưng SSD có một đặc tính rất lạ

> **SSD không ghi đè tại chỗ được.**

Flash NAND có quy tắc bất đối xứng:

- **Đọc và ghi** theo đơn vị **page**: 4–16 KB
- **Xoá** chỉ làm được theo đơn vị **erase block**: **vài MB**, gồm hàng trăm page
- Và **chỉ ghi được vào page đã xoá sạch**

Nghĩa là muốn sửa một page 4KB, về nguyên tắc phải **xoá cả một block vài MB** —
quá đắt. Nên SSD làm cách khác:

```
Muốn sửa page A:
  1. Ghi nội dung mới vào một page TRỐNG khác  →  A'
  2. Đổi bảng ánh xạ:  "địa chỉ A"  →  trỏ sang A'
  3. Đánh dấu page A cũ là RÁC
  4. Lúc nào rảnh, dọn rác: gom các page còn sống sang block khác,
     rồi xoá nguyên block cũ
```

### 💡 Chỗ này rất đáng chú ý

Hai điều bạn vừa học lại xuất hiện ở đây, **ngay trong phần cứng**:

1. **Bảng ánh xạ "địa chỉ logic → vị trí vật lý"** — gọi là **FTL**
   (Flash Translation Layer). Giống hệt vai trò của **inode** và của
   **thư mục `tên → số inode`**.
2. **Ghi ra chỗ mới rồi đổi con trỏ, thay vì sửa tại chỗ** — chính là
   **copy-on-write**, thứ mà **chương 3 của sách** sẽ dạy.

> SSD đã tự làm copy-on-write bên trong từ lâu rồi. Ý tưởng ở chương 3 không hề
> mới lạ — nó là một khuôn mẫu lặp lại ở mọi tầng của hệ thống.

### Ba hệ quả thực tế của SSD

| Hiện tượng | Là gì |
|---|---|
| **Write amplification** | Ghi 4KB nhưng ổ thực sự ghi nhiều hơn (do dọn rác) |
| **Wear leveling** | Mỗi ô flash chỉ chịu được ~1.000–100.000 lần xoá → ổ phải **rải đều** việc ghi ra khắp chip |
| **Chậm dần khi gần đầy** | Hết page trống → phải dọn rác ngay trước mỗi lần ghi |

Đây cũng là lý do có lệnh **TRIM**: hệ điều hành báo cho SSD *"những page này
thuộc file đã xoá rồi, dọn trước đi"* để lúc cần ghi không phải đợi.

---

## 5. Bảng so sánh tổng hợp

| | **HDD** | **SSD** |
|---|---|---|
| Nguyên lý | đĩa từ quay + đầu đọc cơ | chip nhớ flash NAND |
| Bộ phận chuyển động | ✅ có | ❌ không |
| Đọc ngẫu nhiên | **rất chậm** (~100/s) | **rất nhanh** (~500.000/s) |
| Đọc tuần tự | ~150 MB/s | ~3.000–7.000 MB/s |
| Độ trễ | ~10 ms | ~0,1 ms |
| Ghi đè tại chỗ | ✅ được | ❌ **không** (phải xoá cả block) |
| Tuổi thọ | theo cơ học | theo **số lần ghi** |
| Giá / GB | rẻ | đắt hơn ~4–6 lần |
| Chống sốc | kém | tốt |

---

## 6. Vì sao những điều này quan trọng với database

**1. Đơn vị IO tối thiểu → sinh ra khái niệm "page".**
Node của B-tree phải là **bội số của đơn vị IO**. Một node dùng nửa page là
**phí nửa lần IO**. (Sách mục 2.4.)

**2. Ngẫu nhiên vẫn chậm hơn tuần tự — kể cả trên SSD.**
Chênh lệch nhỏ hơn HDD nhiều, nhưng vẫn còn. Nên quy tắc *"cây càng thấp càng
tốt"* vẫn đúng → **B+tree** thay vì cây nhị phân.

**3. `fsync` không phải lúc nào cũng thật.**
Bản thân **ổ đĩa cũng có cache riêng** (DRAM trên ổ). Nhiều ổ báo "ghi xong"
khi dữ liệu mới vào cache của ổ, chưa xuống NAND/platter. Mất điện → mất.
Đây là lý do database quan tâm tới **write barrier** và cờ **FUA** (Force Unit
Access), và là một trong những chỗ khó nhất để làm đúng.

**4. SSD cũng làm copy-on-write.**
Nên khi sách dạy COW ở chương 3, bạn biết rằng phần cứng bên dưới cũng đang làm
đúng y như vậy — có khi là **hai tầng COW chồng lên nhau**.

---

## Tóm tắt

1. Đĩa = chỗ chứa **bền vững** nhưng **chậm hơn RAM hàng chục nghìn lần**.
   Đó là lý do database tồn tại.
2. Đĩa chỉ làm việc **theo khối** (sector 512B/4KB), không theo byte.
3. **HDD**: có đĩa quay + đầu đọc cơ → **ngẫu nhiên cực chậm** (~10 ms/lần).
4. **SSD**: chip flash, không bộ phận chuyển động → **ngẫu nhiên nhanh gấp nghìn lần**,
   nhưng **không ghi đè tại chỗ được** → bên trong nó tự làm **copy-on-write**.
5. Mọi thiết kế của database (page, B+tree, WAL) đều sinh ra từ mấy đặc tính này.

## Câu hỏi tự kiểm tra

1. Vì sao cây nhị phân không dùng được cho index trên đĩa, mà phải dùng B+tree?
2. SSD nhanh hơn HDD ở điểm nào nhiều nhất — tuần tự hay ngẫu nhiên? Vì sao?
3. Vì sao SSD phải có "dọn rác" (garbage collection), còn HDD thì không?
4. `fsync` trả về thành công rồi mất điện — có chắc chắn dữ liệu còn không?
