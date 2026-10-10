# B+tree: split, merge và copy-on-write (chương 3)

> Nối tiếp [07-btree-vs-lsm.md](07-btree-vs-lsm.md). Liên quan sách **mục 3.1–3.4**.
> ⏳ **Chưa học:** double-write và nguyên lý chung (phần cuối 3.4) — xem mục 6.

## 1. B+tree = mảng lồng nhau (mục lục sách)

```
muc luc:      [ 1,        4,        9 ]        <- internal node: chi duong
                |         |         |
           [1, 2, 3]   [4, 6]   [9, 11, 12]     <- leaf node: du lieu that
```

- Mảng đầu phụ trách **mọi số < 4** (không phải "từ 1"). Chèn 0 → vào mảng đầu.
- Nên **3 nhánh chỉ cần 2 key** — key đầu bỏ được. Postgres bỏ thật
  (xem [11](11-postgres-index-thuc-te.md): mục đầu ở root có `data` rỗng = −∞).
- Dữ liệu lớn → **mục lục cho mục lục** → nhiều tầng. 1 tỷ key ≈ 4 tầng.
- **B+**: chỉ leaf chứa value; internal chỉ chứa key chỉ đường → nhét nhiều nhánh hơn.

## 2. Ba luật

| Luật | Ai giữ |
|---|---|
| 1. Mọi leaf cùng độ sâu | Cây chỉ cao/thấp **ở root** |
| 2. Node không quá to (≤ 1 page) | **Split** khi chèn |
| 3. Node không rỗng | **Merge** khi xoá |

## 3. Split (mỗi mảng tối đa 3 số)

```
Chen 7 vao [4,5,6] -> [4,5,6,7] tran -> che [4,5] va [6,7]
Cha them moc 6: [1,4,6,9] -> tran -> che [1,4] va [6,9] -> can ROOT MOI:

                     [ 1,        6 ]                <- root MOI
                      |          |
               [ 1,   4 ]      [ 6,   9 ]
                |     |         |     |
          [1,2,3]  [4,5]     [6,7]  [9,11,12]
```

- Split **lan lên** chỉ khi cha cũng tràn. Chèn 10 sau đó: leaf tách `[9,10]` `[11,12]`,
  cha thành `[6,9,11]` — **vừa, dừng luôn**, root không đụng tới.
- **Cây mọc lên từ gốc** → mọi leaf sâu thêm 1 **cùng lúc** → luôn cân bằng.
- Phần lớn lần chèn **không** split; split tới root rất hiếm.

**Merge** = split chạy ngược. Node rỗng (hoặc dưới ngưỡng) gộp vào node bên cạnh;
lan lên root; root còn 1 nhánh thì bỏ root → cây thấp đi.

## 4. Vấn đề: sửa tại chỗ trên đĩa + crash

Split cần ghi **nhiều page**:

```
(1) page 13 <- [9, 10]         ghi DE len node cu
(2) page 20 <- [11, 12]        page moi
(3) page 7  <- [6, 9, 11]      muc luc cha
```

Mất điện sau (1)(2), trước (3):

```
page 7:  [6, 9]       <- chua sua, khong biet page 20
page 13: [9, 10]      <- da ghi de, 11 va 12 mat o day
page 20: [11, 12]     <- KHONG AI TRO TOI
```

Tìm 12 → page 7 → page 13 → **không thấy**. Cây ở trạng thái **lai**.

> ⚠️ **Hiểu nhầm của mình:** tưởng "vẫn tìm thấy vì phải atomic". Atomic **không tự có**
> — phải **tự xây** (bài học chương 1). Ghi đè thẳng nhiều page = SaveData1 quy mô lớn.

## 5. Copy-on-write — chỉ chép đường đi

**Tự suy ra được từ chương 1:** "ghi cây mới vào folder khác rồi rename folder". Đúng
nguyên lý, nhưng chép **cả cây**. COW chỉ chép **các node trên đường từ leaf lên root**:

```
     root CU                              root MOI (page 24)
     [1, 6]                                [1, 6]
     /     \                              /      \
    /       [6, 9]  (cu)                 /    [6, 9, 11]  (page 23)
   /         /   \                      /      /    |    \
[1, 4]  [6,7,8] [9,11,12] (cu)    [1, 4]  [6,7,8] [9,10] [11,12]
  ^        ^                        ^        ^     (p21)  (p22)
  +--------+---- DUNG CHUNG --------+--------+
```

"Rename" của cây = **đổi con trỏ root ở page 0**: `root = 5` → `root = 24`.

### Vì sao phải chép cả cha, ông…

Giống **fsync thư mục**: cha giữ **tham chiếu** (như thư mục giữ `tên → inode`).
Chép leaf mà cha không đổi → cha vẫn trỏ page cũ → leaf mới không ai tìm thấy. Mà
sửa cha tại chỗ = ghi đè → phải chép cha → ông phải trỏ bản chép → … lên tới root.

### Crash ở đâu cũng an toàn

| Crash lúc | Page 0 | Kết quả |
|---|---|---|
| Đang ghi page 21–24 | root cũ | Cây cũ đầy đủ; page mới = rác vô hại (free list thu hồi) |
| Sau khi đổi page 0 | root mới | Cây mới đầy đủ |

Ví dụ crash sau khi ghi 21, 22, 23: tìm **10 không thấy** (đúng — chưa commit), tìm
**12 vẫn thấy** (page 13 không ai đụng). Tương đương crash khi đang ghi `db.tmp/`.

> **An toàn cho cả cây được thu gọn về: ghi an toàn MỘT con trỏ root.**
> Cách ghi con trỏ đó: [09-node-format-va-meta-page.md](09-node-format-va-meta-page.md).

**Lợi:** snapshot miễn phí (reader cứ đi theo root cũ), recovery dễ.
**Giá:** write amplification (sửa 1 leaf ghi ~4 page), page cũ thành rác (cần free list).

## 6. ⏳ Chưa học — để lần sau

- **Double-write** (Postgres full-page write, MySQL doublewrite): vẫn ghi đè tại chỗ,
  nhưng ghi bản mới ra chỗ riêng + fsync trước; crash thì chép đè lại (**idempotent**).
- **Nguyên lý chung:** *luôn có đủ thông tin để dựng lại HOẶC trạng thái cũ, HOẶC
  trạng thái mới.*

## Câu hỏi tự kiểm tra

1. Vì sao cây mọc từ gốc chứ không mọc từ lá?
2. Chèn 0 vào cây ở mục 1 thì vào đâu?
3. COW: vì sao không thể chỉ chép mỗi leaf?
4. Crash sau khi ghi xong page mới nhưng chưa đổi page 0 — thấy cây nào?
