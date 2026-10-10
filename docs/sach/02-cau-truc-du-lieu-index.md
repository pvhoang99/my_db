# Chương 02 — Cấu trúc dữ liệu để đánh index
*(Indexing Data Structures)*

## 2.1 Các loại truy vấn

Hầu hết truy vấn SQL có thể chia thành 3 loại:

1. **Scan** toàn bộ tập dữ liệu. (Không dùng index.)
2. **Point query**: truy vấn index theo một key cụ thể.
3. **Range query**: truy vấn index theo một khoảng. (Index phải đã được sắp xếp.)

Có nhiều cách làm cho việc scan nhanh hơn, chẳng hạn lưu trữ theo cột
(column-based storage). Nhưng scan vẫn là `O(N)` dù nhanh cỡ nào; trọng tâm của
chúng ta là những truy vấn phục vụ được trong `O(log N)` bằng cấu trúc dữ liệu.

Một **range query gồm 2 pha**:

1. **Seek**: tìm key bắt đầu.
2. **Iterate**: tìm key trước/sau theo thứ tự đã sắp xếp.

Một **point query chỉ là seek mà không iterate**; vậy nên **một cấu trúc dữ liệu
có sắp xếp là tất cả những gì ta cần**.

## 2.2 Hashtable

Hashtable là lựa chọn khả thi **nếu bạn chỉ quan tâm tới point query**
(`get`, `set`, `del`), nên ta sẽ không bận tâm tới nó **vì nó thiếu tính thứ tự**.

Tuy nhiên, tự code một hashtable, kể cả loại chạy trong bộ nhớ, vẫn là một bài
tập đáng giá. Nó dễ hơn nhiều so với B-tree mà ta sẽ code sau này, dù vẫn còn
vài thách thức:

- **Làm sao cho hashtable lớn lên?** Khi load factor quá cao, các key phải được
  chuyển sang một hashtable lớn hơn. Chuyển tất cả cùng lúc thì tốn `O(N)` — quá đắt.
  Việc **rehash phải được làm dần dần**, kể cả với ứng dụng chạy trong bộ nhớ như Redis.
- Những thứ đã nhắc tới trước đó: update tại chỗ, tái sử dụng chỗ trống, v.v.

## 2.3 Mảng đã sắp xếp

Loại bỏ hashtable rồi, hãy bắt đầu với cấu trúc có sắp xếp đơn giản nhất:
**mảng đã sắp xếp**. Bạn có thể binary search trên nó trong `O(log N)`.
Với dữ liệu có độ dài thay đổi như chuỗi (KV), hãy dùng **một mảng con trỏ
(offset)** để binary search.

Nhưng **update một mảng đã sắp xếp tốn `O(N)`**, dù là tại chỗ hay không. Nên nó
không thực dụng — tuy vậy nó **mở rộng được** thành những cấu trúc update được khác.

**Cách thứ nhất** để giảm chi phí update: **chẻ mảng thành nhiều mảng nhỏ hơn
không chồng lấn** — tức mảng sắp xếp lồng nhau. Hướng mở rộng này dẫn tới
**B+tree** (cây n-ary nhiều tầng), kèm theo thách thức mới là phải duy trì những
mảng nhỏ đó (chính là các node của cây).

**Cách thứ hai**, một dạng "mảng update được" khác, là **log-structured merge tree
(LSM-tree)**. Các bản update được **đệm trước** vào một mảng nhỏ hơn (hoặc cấu trúc
có sắp xếp nào đó), rồi **merge** vào mảng chính khi nó lớn quá. Chi phí update
được **khấu hao (amortize)** bằng cách đẩy dần mảng nhỏ vào mảng lớn.

## 2.4 B-tree

B-tree là một **cây n-ary cân bằng**, tương đương với cây nhị phân cân bằng.
Mỗi node chứa **số lượng key (và nhánh) thay đổi**, tối đa là `n`, với `n > 2`.

### Giảm truy cập ngẫu nhiên bằng cây thấp hơn

Một cái đĩa chỉ thực hiện được một số lượng IO nhất định mỗi giây (**IOPS**), và
đây chính là yếu tố giới hạn khi tra cứu cây. **Mỗi tầng của cây là một lần đọc
đĩa** khi tra cứu, mà cây n-ary thì thấp hơn cây nhị phân với cùng số key
(`log_n N` so với `log_2 N`) — vì thế cây n-ary được dùng để **giảm số lần đọc
đĩa cho mỗi lần tra cứu**.

Chọn `n` như thế nào? Có một sự đánh đổi:

- `n` **lớn hơn** → ít lần đọc đĩa hơn mỗi lần tra cứu (độ trễ và throughput tốt hơn).
- `n` **lớn hơn** → node lớn hơn, mà node lớn thì **update chậm hơn** (bàn sau).

### IO theo đơn vị page

Dù bạn có thể đọc số byte bất kỳ tại offset bất kỳ trong file, **đĩa không hoạt
động như vậy**. Đơn vị IO cơ bản của đĩa không phải byte mà là **sector** —
những khối liên tục 512 byte trên HDD đời cũ.

Tuy nhiên, sector của đĩa không phải mối bận tâm của ứng dụng, bởi IO file thông
thường không tương tác trực tiếp với đĩa. Hệ điều hành cache/đệm các thao tác
đọc/ghi đĩa trong **page cache**, gồm những khối bộ nhớ 4KB gọi là **page**.

Dù thế nào thì vẫn **luôn có một đơn vị IO tối thiểu**. DB cũng có thể tự định
nghĩa đơn vị IO riêng (cũng gọi là page), và nó **có thể lớn hơn page của OS**.

Việc tồn tại đơn vị IO tối thiểu hàm ý rằng **node của cây nên được cấp phát theo
bội số của đơn vị đó**; một đơn vị chỉ dùng một nửa nghĩa là **phí một nửa IO**.
Lại thêm một lý do nữa để không chọn `n` nhỏ!

### Biến thể B+tree

Trong ngữ cảnh database, nói "B-tree" thực ra là nói tới một biến thể của nó:
**B+tree**. Trong B+tree, **internal node không chứa value**; value **chỉ tồn
tại ở leaf node**. Điều này làm cây **thấp hơn**, vì internal node có nhiều chỗ
hơn để chứa nhánh.

B+tree dùng làm cấu trúc trong bộ nhớ cũng hợp lý, vì đơn vị IO tối thiểu giữa
RAM và CPU cache là **64 byte (cache line)**. Nhưng lợi ích về hiệu năng không
lớn bằng trên đĩa, vì 64 byte thì chẳng nhét được bao nhiêu.

### Chi phí không gian của cấu trúc dữ liệu

Một lý do nữa khiến cây nhị phân không thực dụng là **số lượng con trỏ**: mỗi key
cần ít nhất 1 con trỏ đi vào từ node cha. Trong khi đó ở B+tree, **nhiều key trong
một leaf node dùng chung 1 con trỏ đi vào**.

Các key trong một leaf node còn có thể được đóng gói ở dạng nén hoặc compact
để tiết kiệm thêm không gian.

## 2.5 Lưu trữ kiểu log (log-structured storage)

### Update bằng merge: khấu hao chi phí

Ví dụ phổ biến nhất của log-structured storage là **LSM-tree**. Ý tưởng chính của
nó **không phải "log" cũng không phải "tree"** — mà là **"merge"**!

Hãy bắt đầu với 2 file: một file **nhỏ** chứa các bản update gần đây, và một file
**lớn** chứa phần còn lại của dữ liệu. Update đi vào file nhỏ trước, nhưng nó
không thể phình mãi được; khi chạm ngưỡng, nó sẽ được **merge** vào file lớn.

```
writes => | new updates | => | accumulated data |
               file 1              file 2
```

Merge 2 file đã sắp xếp cho ra một file mới hơn, lớn hơn, thay thế file lớn cũ
và làm file nhỏ co lại.

Merge tốn `O(N)`, nhưng **có thể chạy đồng thời với reader và writer**.

### Giảm write amplification bằng nhiều tầng

Đệm các bản update vẫn tốt hơn là ghi lại toàn bộ dữ liệu mỗi lần. Nhưng nếu ta
mở rộng cơ chế này ra **nhiều tầng** thì sao?

```
|level 1|
    ||
    \/
|------level 2------|
          ||
          \/
|-----------------level 3-----------------|
```

Trong cơ chế 2 tầng, file lớn bị ghi lại mỗi khi file nhỏ chạm ngưỡng; phần ghi
dư thừa đó gọi là **write amplification**, và nó **càng tệ khi file lớn càng to**.

Nếu dùng nhiều tầng hơn, ta có thể giữ cho tầng 2 luôn nhỏ bằng cách merge nó
vào tầng 3 — y như cách ta giữ tầng 1 nhỏ.

Về trực giác, các tầng **tăng theo cấp số nhân**, và tăng theo luỹ thừa 2
(merge các tầng có kích thước tương đương) cho write amplification thấp nhất.
Nhưng có sự đánh đổi giữa **write amplification** và **số tầng** (ảnh hưởng tới
hiệu năng truy vấn).

### Index trong LSM-tree

Mỗi tầng chứa cấu trúc index của riêng nó, mà **có thể chỉ cần là một mảng đã
sắp xếp**, vì các tầng **không bao giờ bị update** (trừ tầng 1). Nhưng binary
search thì cũng chẳng hơn cây nhị phân là bao về mặt truy cập ngẫu nhiên, nên
lựa chọn hợp lý là **dùng B-tree bên trong mỗi tầng** — đó chính là phần "tree"
trong LSM-tree. Dù sao thì cấu trúc dữ liệu ở đây vẫn **đơn giản hơn nhiều vì
không phải update**.

Để hiểu rõ hơn ý tưởng "merge", bạn có thể thử áp dụng nó cho hashtable — gọi là
log-structured hashtable.

### Truy vấn trên LSM-tree

Key có thể nằm ở **bất kỳ tầng nào**, nên để truy vấn LSM-tree, kết quả từ **mọi
tầng phải được gộp lại** (n-way merge đối với range query).

Với point query, có thể dùng **Bloom filter** như một tối ưu để giảm số tầng
phải tìm.

Vì các tầng không bao giờ bị update, nên **có thể tồn tại phiên bản cũ của key ở
các tầng cũ hơn**, và key đã xoá thì được **đánh dấu bằng một cờ đặc biệt ở tầng
mới hơn** (gọi là **tombstone**). Do đó, **tầng mới hơn luôn được ưu tiên** khi
truy vấn.

Quá trình merge **tự nhiên thu hồi lại chỗ trống** từ các key cũ hoặc đã xoá.
Vì vậy nó còn được gọi là **compaction**.

### LSM-tree ngoài đời thực: SSTable, MemTable và log

Đây là mấy thuật ngữ về chi tiết triển khai LSM-tree. Bạn không cần biết chúng
để build một cái từ nguyên lý, nhưng chúng giải quyết những vấn đề thực tế.

- Mỗi tầng được chẻ thành nhiều file không chồng lấn gọi là **SSTable**, thay vì
  một file lớn, để việc **merge làm được từng phần**. Điều này giảm lượng chỗ
  trống cần có khi merge các tầng lớn, và trải quá trình merge ra theo thời gian.
- **Tầng 1 được update trực tiếp**, nên dùng **log** là lựa chọn khả thi vì tầng 1
  bị giới hạn kích thước. Đây chính là phần **"log"** trong LSM-tree — một ví dụ
  của việc kết hợp log với cấu trúc index khác.
- Nhưng kể cả khi log nhỏ, vẫn cần một cấu trúc index đàng hoàng. Dữ liệu của log
  được **nhân bản trong một index nằm trong bộ nhớ gọi là MemTable**, có thể là
  B-tree, skiplist, hay bất cứ thứ gì. Nó là một lượng dữ liệu nhỏ, có giới hạn,
  nằm trong RAM, và có thêm lợi ích là **tăng tốc kịch bản đọc những gì vừa ghi**.

## 2.6 Tóm tắt về cấu trúc dữ liệu index

Có 2 lựa chọn: **B+tree** và **LSM-tree**.

LSM-tree giải quyết sẵn nhiều thách thức từ chương trước, chẳng hạn làm sao update
cấu trúc dữ liệu trên đĩa và làm sao tái sử dụng chỗ trống. Trong khi đó những
thách thức này **vẫn còn nguyên với B+tree** — và đó là thứ sẽ được khám phá sau.

---

## 📌 Đối chiếu với PostgreSQL *(ghi chú thêm, không có trong sách)*

- Postgres dùng **B+tree** (`nbtree`) làm index mặc định, **không** dùng LSM-tree.
  Page của Postgres là **8KB**, gấp đôi 4KB mà sách chọn.
- **Khác biệt lớn nhất so với sách**: ở Postgres, **dữ liệu không nằm trong B+tree**.
  Leaf node của index chỉ chứa **key + TID** `(page, slot)` trỏ sang **heap file**.
  Sách thì nhét luôn value vào leaf — tức **clustered index**, giống SQLite/InnoDB.
- Hệ quả thực tế: Postgres có **index-only scan** (chỉ khi visibility map cho phép)
  và có hiện tượng **HOT update**; còn DB kiểu sách thì đọc theo primary key luôn
  nhanh vì không phải nhảy sang heap.
- Postgres cũng có các loại index khác ngoài B-tree: **Hash** (đúng như mục 2.2,
  chỉ point query), **GIN**, **GiST**, **BRIN**, **SP-GiST**.
