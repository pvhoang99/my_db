# Chương 03 — B-Tree & Khôi phục sau crash
*(B-Tree & Crash Recovery)*

## 3.1 B-tree nhìn như một cây n-ary cân bằng

### Cây cân bằng theo chiều cao

Nhiều cây nhị phân thực dụng, như **AVL tree** hay **RB tree**, được gọi là
**cây cân bằng theo chiều cao (height-balanced)**, nghĩa là chiều cao của cây
(từ gốc tới lá) bị giới hạn ở `O(log N)`, nên một lần tra cứu tốn `O(log N)`.

**B-tree cũng cân bằng theo chiều cao**; chiều cao là **như nhau với mọi leaf node**.

### Tổng quát hoá từ cây nhị phân

Cây n-ary có thể được tổng quát hoá từ cây nhị phân (và ngược lại). Một ví dụ là
**cây 2-3-4** — một B-tree mà mỗi node có 2, 3 hoặc 4 con. Cây 2-3-4 tương đương
với RB tree. Tuy nhiên ta sẽ không đi sâu vào chi tiết vì chúng **không cần thiết**
để hiểu B-tree.

Hình dung một B+tree 2 tầng của dãy đã sắp xếp `[1, 2, 3, 4, 6, 9, 11, 12]`:

```
        [1,  4,  9]
        /    |    \
       v     v     v
 [1, 2, 3] [4, 6] [9, 11, 12]
```

Trong B+tree, **chỉ leaf node chứa value**; **key bị lặp lại ở internal node**
để chỉ ra khoảng key của subtree. Trong ví dụ này, node `[1, 4, 9]` cho biết
3 subtree của nó nằm trong các khoảng `[1, 4)`, `[4, 9)` và `[9, +∞)`.

Tuy nhiên, **3 khoảng chỉ cần 2 key**, nên key đầu tiên (số 1) có thể bỏ đi, và
3 khoảng trở thành `(-∞, 4)`, `[4, 9)`, `(9, +∞)`.

## 3.2 B-tree nhìn như các mảng lồng nhau

### Mảng lồng nhau 2 tầng

Không cần biết chi tiết về RB tree hay cây 2-3-4, **B-tree có thể hiểu được từ
mảng đã sắp xếp**.

Vấn đề của mảng đã sắp xếp là **update tốn `O(N)`**. Nếu ta **chẻ mảng thành `m`
mảng nhỏ hơn không chồng lấn**, thì update còn `O(N/m)`. Nhưng ta phải biết cần
update/truy vấn mảng nhỏ nào trước đã. Vậy nên ta cần **thêm một mảng đã sắp xếp
chứa tham chiếu tới các mảng nhỏ** — **đó chính là internal node trong B+tree**.

```
[[1,2,3], [4,6], [9,11,12]]
```

Chi phí tra cứu vẫn là `O(log N)` với **2 lần binary search**. Nếu chọn `m = √N`,
update trở thành `O(√N)` — đó đã là mức tốt nhất mà mảng sắp xếp 2 tầng làm được.

### Nhiều tầng mảng lồng nhau

`O(√N)` là **không chấp nhận được** với database. Nhưng nếu ta thêm nhiều tầng
nữa bằng cách chẻ mảng nhỏ hơn nữa, chi phí sẽ giảm tiếp.

Giả sử ta cứ chẻ tầng mãi cho tới khi **mọi mảng đều không lớn hơn một hằng số `s`**,
ta sẽ có `log(N/s)` tầng, và chi phí tra cứu là `O(log(N/s) + log(s))` —
vẫn là `O(log N)`.

Với insert và delete: sau khi tìm được leaf node, việc update leaf node đó
**phần lớn thời gian** chỉ tốn hằng số `O(s)`. Vấn đề còn lại là **duy trì các
bất biến**: node không được lớn hơn `s` và không được rỗng.

## 3.3 Duy trì một B+tree

**3 bất biến phải giữ khi update B+tree:**

1. **Mọi leaf node có cùng chiều cao.**
2. **Kích thước node bị chặn bởi một hằng số.**
3. **Node không rỗng.**

### Làm cây lớn lên bằng cách split node

Bất biến thứ 2 bị vi phạm khi insert vào một leaf node; ta khôi phục nó bằng cách
**split node đó thành các node nhỏ hơn**.

```
   parent              parent
   /  |  \     =>     / | | \
  L1  L2  L6         L1 L3 L4 L6
       *                 *  *
```

Sau khi split một leaf node, **node cha của nó có thêm một nhánh mới**, mà cái
này cũng có thể vượt quá giới hạn kích thước, nên **cha cũng có thể phải split**.
Việc split có thể **lan truyền lên tới tận root node**, làm **chiều cao tăng thêm 1**.

```
    root                   new_root
   /  |  \                  /    \
  L1  L2  L6    =>        N1      N2
                         /  \    /  \
                        L1  L3  L4  L6
```

Điều này **giữ được bất biến thứ nhất**, vì mọi leaf đều cao thêm 1 **cùng một lúc**.

### Làm cây co lại bằng cách merge node

Delete có thể dẫn tới **node rỗng**. Bất biến thứ 3 được khôi phục bằng cách
**merge node rỗng vào một node anh em (sibling)**. Merge là thao tác ngược của
split. Nó cũng có thể lan truyền lên tới root node, nên **chiều cao cây có thể giảm**.

> 💡 Khi code B-tree, **có thể merge sớm hơn** để giảm lãng phí chỗ: bạn có thể
> merge một node **chưa rỗng** khi kích thước của nó tụt xuống dưới một ngưỡng.

## 3.4 B-Tree trên disk

Bạn đã có thể code một B+tree trong bộ nhớ chỉ với những nguyên lý trên. Nhưng
B-tree **trên disk** đòi hỏi thêm vài cân nhắc.

### Cấp phát theo block

Một chi tiết còn thiếu là **làm sao giới hạn kích thước node**. Với B+tree trong
bộ nhớ, bạn chỉ cần giới hạn **số key tối đa** trong một node; kích thước node
tính theo byte không đáng lo, vì bạn cấp phát bao nhiêu byte cũng được.

Với cấu trúc dữ liệu trên disk, **không có `malloc`/`free`, cũng không có garbage
collector** để dựa vào; việc **cấp phát và tái sử dụng chỗ hoàn toàn do ta tự lo**.

Việc tái sử dụng chỗ có thể làm bằng **free list** nếu **mọi lần cấp phát đều
cùng một kích thước** — ta sẽ cài đặt sau. Còn bây giờ, **mọi node của B-tree đều
cùng kích thước**.

### B-tree copy-on-write để update an toàn

Ta đã thấy 3 cách update dữ liệu trên đĩa chống được crash: rename file, log,
LSM-tree. **Bài học rút ra là: đừng phá huỷ bất kỳ dữ liệu cũ nào trong lúc update.**
Ý tưởng này áp dụng được cho cây: **tạo một bản copy của node rồi sửa trên bản copy**.

Insert hay delete đều bắt đầu từ một leaf node; sau khi tạo bản copy đã sửa,
**node cha phải được update để trỏ tới node mới** — việc này cũng làm trên bản
copy của cha. Việc copy **lan truyền lên tới root node**, cho ra một **root mới**.

- **Cây gốc vẫn còn nguyên vẹn** và truy cập được từ root cũ.
- **Root mới**, cùng các bản copy đã update dọc đường xuống tới leaf, **dùng chung
  mọi node còn lại** với cây gốc.

```
      d              d          D*
     / \            / \        / \
    b   e    ==>   b   e   +  B*  e
   / \            / \        / \
  a   c          a   c      a   C*
              (bản gốc)   (bản đã update)
```

Đây là hình dung của việc update leaf `c`. Các node được copy viết **hoa**
(D, B, C), còn các subtree dùng chung viết **thường** (a, e).

Cái này gọi là cấu trúc dữ liệu **copy-on-write**. Nó còn được mô tả là
**immutable**, **append-only** (không đúng nghĩa đen), hoặc **persistent**
(không liên quan gì tới durability).

> ⚠️ Hãy lưu ý rằng **thuật ngữ trong database không có nghĩa nhất quán**.

Còn 2 vấn đề nữa với B-tree copy-on-write:

1. **Làm sao tìm được root của cây**, khi nó thay đổi sau mỗi lần update?
   Bài toán crash safety được **thu gọn về việc update đúng một con trỏ duy nhất** —
   ta sẽ giải sau.
2. **Làm sao tái sử dụng node từ các phiên bản cũ?** Đó là việc của **free list**.

### Ưu điểm của B-tree copy-on-write

Một ưu điểm của việc giữ lại các phiên bản cũ là ta có **snapshot isolation
hoàn toàn miễn phí**. Một transaction bắt đầu với một phiên bản của cây, và
**sẽ không nhìn thấy thay đổi từ các phiên bản khác**.

Và **crash recovery trở nên dễ như không**: cứ dùng phiên bản cũ gần nhất là xong.

Một ưu điểm nữa là nó **hợp với mô hình concurrency multi-reader-single-writer**,
và **reader không chặn writer**. Ta sẽ khám phá những điều này sau.

### Cách thay thế: update tại chỗ với double-write

Dù crash recovery là hiển nhiên trong cấu trúc copy-on-write, chúng có thể không
được ưa chuộng vì **write amplification cao**. Mỗi lần update phải copy cả đường
đi `O(log N)`, trong khi hầu hết các update tại chỗ chỉ đụng tới **1 leaf node**.

Vẫn có thể làm update tại chỗ mà vẫn có crash recovery, không cần copy-on-write:

1. **Lưu một bản sao của toàn bộ các node sắp bị update** ra chỗ khác. Cái này
   giống copy-on-write, nhưng **không copy node cha**.
2. **`fsync` các bản sao đã lưu.** *(Tới điểm này đã có thể trả lời client.)*
3. **Thực sự update cấu trúc dữ liệu tại chỗ.**
4. **`fsync` phần đã update.**

Sau khi crash, cấu trúc dữ liệu có thể đang ở trạng thái update dở, nhưng ta
**không thực sự biết là dở tới đâu**. Việc ta làm là **cứ apply mù các bản sao đã
lưu**, để cấu trúc kết thúc ở trạng thái mới — **bất kể trạng thái hiện tại là gì**.

```
| a=1 b=2 |
    ||  1. Lưu bản sao của toàn bộ node sắp update.
    \/
| a=1 b=2 |  +  | a=2 b=4 |
   data          bản sao mới
    ||  2. fsync các bản sao đã lưu.
    \/
| a=1 b=2 |  +  | a=2 b=4 |
   data        bản sao (đã fsync)
    ||  3. Update cấu trúc tại chỗ. Nhưng ta crash ở đây!
    \/
| ??????? |  +  | a=2 b=4 |
 data (hỏng)    bản sao (tốt)
    ||  Recovery: apply bản sao đã lưu.
    \/
| a=2 b=4 |  +  | a=2 b=4 |
 data (mới)     giờ thành vô dụng
```

Những bản sao đã update được lưu lại đó gọi là **double-write** trong thuật ngữ
của MySQL. Nhưng nếu chính double-write bị hỏng thì sao? Xử lý y như với log:
**checksum**.

- Nếu checksum phát hiện double-write hỏng → **bỏ qua nó**. Vì nó xảy ra trước
  lần `fsync` thứ nhất, nên dữ liệu chính vẫn đang ở **trạng thái cũ và tốt**.
- Nếu double-write tốt → apply nó sẽ **luôn** cho ra dữ liệu chính tốt.

Một số DB thực sự lưu double-write **trong log**, gọi là **physical logging**.
Có 2 kiểu logging:

- **Logical logging** mô tả thao tác mức cao, chẳng hạn "insert một key". Những
  thao tác như vậy **chỉ apply được khi DB đang ở trạng thái lành lặn**.
- **Physical logging** ghi update mức thấp của page trên đĩa. **Chỉ physical
  logging mới dùng được cho recovery.**

*(Sách dẫn link: [Full page writes — PostgreSQL Wiki](https://wiki.postgresql.org/wiki/Full_page_writes))*

### 💡 Nguyên lý khôi phục sau crash

Hãy so sánh double-write với copy-on-write:

- **Double-write** làm cho update trở nên **idempotent**; DB có thể **thử lại**
  việc update bằng cách apply các bản sao đã lưu, vì chúng là **node nguyên vẹn**.
- **Copy-on-write** **chuyển atomic** mọi thứ sang phiên bản mới.

Chúng dựa trên hai ý tưởng khác nhau:

- Double-write đảm bảo **có đủ thông tin để tạo ra phiên bản mới**.
- Copy-on-write đảm bảo **phiên bản cũ được bảo toàn**.

Thế nếu với double-write, ta lưu **các node gốc** thay vì các node đã update thì sao?
Đó chính là **cách thứ 3** để khôi phục khỏi hỏng hóc, và nó khôi phục về **phiên
bản cũ**, giống copy-on-write.

> **Ta có thể gộp cả 3 cách vào một ý tưởng duy nhất:
> tại bất kỳ thời điểm nào, luôn phải có đủ thông tin để dựng lại
> HOẶC trạng thái cũ, HOẶC trạng thái mới.**

Ngoài ra, **luôn phải copy một chút gì đó**, nên **node càng lớn thì update càng chậm**.

Ta sẽ dùng **copy-on-write** vì nó đơn giản hơn, nhưng bạn có thể chọn khác ở đây.

## 3.5 Những gì ta đã học

**Nguyên lý B+tree:**

- Cây n-ary, kích thước node bị giới hạn bởi một hằng số.
- Mọi leaf có cùng chiều cao.
- Split khi insert, merge khi delete.

**Cấu trúc dữ liệu trên disk:**

- Cấu trúc dữ liệu copy-on-write.
- Khôi phục sau crash bằng double-write.

**Giờ ta có thể bắt đầu code. 3 bước để tạo một KV bền vững dựa trên B+tree:**

1. Code cấu trúc dữ liệu B+tree.
2. Đưa B+tree xuống disk.
3. Thêm free list.

---

## 📌 Đối chiếu với PostgreSQL *(ghi chú thêm, không có trong sách)*

- Postgres chọn **update tại chỗ + physical logging**, tức nhánh **double-write**,
  **không** phải copy-on-write.
- **Full-page write** của Postgres chính xác là double-write: sau mỗi checkpoint,
  lần đầu tiên một page bị sửa thì ghi **nguyên cả page** vào WAL. Chính link
  trong sách trỏ tới trang wiki này.
- WAL của Postgres là **lai**: phần lớn record là physical/physiological (sửa byte
  nào trên page nào), đủ để **redo mù**.
- **Hệ quả của việc không dùng COW**: snapshot isolation **không miễn phí** →
  Postgres phải tự cài MVCC bằng **`xmin`/`xmax`** trên từng tuple, và phải có
  **VACUUM** để dọn tuple chết. Sách không cần VACUUM vì free list lo hết.
- Về write amplification: Postgres né được chi phí copy `O(log N)` mỗi lần update,
  nhưng **trả giá bằng full-page write** sau mỗi checkpoint. Đó là lý do
  `checkpoint_timeout` đặt quá nhỏ sẽ làm WAL phình to.
