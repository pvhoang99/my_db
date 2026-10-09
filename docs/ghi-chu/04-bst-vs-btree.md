# Cây nhị phân vs B+tree — vì sao database không dùng BST

> Dựa trên [00-dia-hdd-ssd.md](00-dia-hdd-ssd.md) (đĩa đọc theo trang).
> Liên quan sách mục **2.3, 2.4**.

## 1. Cây nhị phân tìm kiếm (BST) là gì

Mỗi node chứa **1 key**, có tối đa **2 con**. Con trái nhỏ hơn, con phải lớn hơn.

```
         50
        /  \
      30    70
     /  \   / \
   20   40 60  80
```

Tìm 40: so với 50 → rẽ trái; so với 30 → rẽ phải; thấy. **3 bước.**

## 2. Vì sao BST không dùng được trên đĩa

**Mỗi tầng của cây = một lần đọc đĩa ngẫu nhiên.** Mà BST rất cao.

Và lý thuyết còn lạc quan hơn thực tế. Đo thật trên **104.334 từ tiếng Anh**
(`/usr/share/dict/words`), chèn theo thứ tự ngẫu nhiên:

```
Lý thuyết log₂(104.334)   =  16,7 tầng
Chiều cao THẬT đo được    =  41 tầng      ← gấp 2,4 lần!
Độ sâu trung bình         =  21,3 tầng
```

BST **không tự cân bằng**: nhánh may thì ngắn, nhánh xui thì dài.
Và tệ nhất: chèn dữ liệu **đã sắp xếp sẵn** (rất hay gặp — import theo ID tăng dần)
thì BST **thoái hoá thành danh sách liên kết**, cao đúng **104.334 tầng**.

> Chạy lại thí nghiệm: `python3 scripts/bst_vs_btree.py`

## 3. Ý tưởng của B+tree — chỉ một câu

Nhớ lại: **đĩa đọc ít nhất một trang 4096 byte**. Đọc 1 byte hay 4096 byte đều
tốn **đúng một lần IO**.

Vậy sao mỗi node chỉ chứa **1 key**? Quá lãng phí.

> **B+tree: mỗi node = một trang 4KB, nhét được bao nhiêu key thì nhét.**

Không phải thuật toán hay hơn — mà là **tận dụng trọn đơn vị IO**.

## 4. Fanout tính ra sao — nguồn gốc con số 249 và 222

```python
leaf_cap = PAGE // (avg_key + TID + OVH)   # 4096 // (8.4 + 6 + 2) = 249
int_cap  = PAGE // (avg_key + PTR + OVH)   # 4096 // (8.4 + 8 + 2) = 222
```

**Mỗi mục trong LEAF** (key + chỉ chỗ tới bản ghi thật):

| Thành phần | Byte |
|---|---|
| key | 8,4 — độ dài trung bình thật của 104.334 từ |
| TID (trỏ tới bản ghi: trang nào, slot nào) | 6 |
| offset (sổ sách của trang) | 2 |
| **Tổng** | **16,4** → `4096 / 16,4` = **249 mục** |

**Mỗi mục trong INTERNAL** (key + chỉ chỗ tới node con):

| Thành phần | Byte |
|---|---|
| key | 8,4 |
| con trỏ node con (`uint64`) | 8 |
| offset | 2 |
| **Tổng** | **18,4** → `4096 / 18,4` = **222 mục** |

Chênh 249 vs 222 chỉ vì **con trỏ node con (8B) to hơn TID (6B)**.

### ⚠️ Fanout phụ thuộc kích thước key — bài học thực dụng

Tra cứu trong **1 tỷ bản ghi**, trang 4KB:

| Kiểu primary key | byte | fanout | **số tầng** |
|---|---|---|---|
| `int32` | 4 | 292 | **4** |
| `int64` | 8 | 227 | **4** |
| UUID (binary) | 16 | 157 | **5** |
| email | 30 | 102 | **5** |
| UUID (text) | 36 | 89 | **5** |
| URL dài | 120 | 31 | **7** |

> **Key càng to → mỗi mục càng béo → nhét được càng ít → cây càng cao → mọi truy
> vấn đều chậm hơn, mãi mãi.**

Đây chính là con số đằng sau lời khuyên ở **chương 8** của sách về
auto-generated row ID: *"ID nhỏ, độ rộng cố định → internal node chứa được nhiều
key hơn → cây thấp hơn"*. Không phải sở thích, mà là **số tầng cây**.

## 5. Cấu trúc thật — 104.334 từ trong 3 tầng

### Vì sao đúng 3 tầng: cứ chia cho fanout tới khi còn 1 node

```
104.334 ÷ 249 = 420   leaf
    420 ÷ 222 =   2   internal
      2 ÷ 222 =   1   gốc      ← dừng
```

Mỗi phép chia là một tầng. Vì chia cho **hàng trăm** nên số tầng tăng cực chậm.

### Tầng 1 và 2 chứa gì — chỉ là biển chỉ đường

```
TẦNG 1 (gốc)   [ "A" → ●     |   "holder's" → ● ]
                   │                  │
       ┌───────────┘                  └────────────────┐
       ▼                                               ▼
TẦNG 2 [ "A"→● "Afghanistan's"→● ... ]   [ "holder's"→● "honoraria"→● "host"→● ... ]
          │                                               │
          ▼                                               ▼
TẦNG 3 [A, A's, AA, ... Afghanistan]     [honoraria, ... hospital, ... hospitals]
       ↑ 249 từ THẬT                      ↑ 249 từ THẬT
```

Internal node **không chứa dữ liệu**, chỉ chứa cặp `(key, con trỏ tới node con)`.
Key ở đó là **bản sao của từ đầu tiên trong mỗi node con**, dùng làm **mốc phân chia**.

### Tra cứu thật: tìm `"hospital"`

```
Lần 1 → gốc:        2 key ['A', "holder's"]
                    "hospital" >= "holder's"  -> rẽ nhánh #1

Lần 2 → internal:   tìm key lớn nhất <= "hospital"  ->  "honoraria"
                    (key kế tiếp là "host", nên nó nằm giữa hai key này)
                    -> rẽ xuống leaf tương ứng

Lần 3 → leaf:       leaf chứa "honoraria".."hospitals", 249 từ
                    tìm trong bộ nhớ -> vị trí 237 -> "hospital" ✅
```

### Hai điều đáng chú ý

**1. Key ở tầng trên bị lặp lại.** `"honoraria"` xuất hiện hai lần: làm biển chỉ
đường ở tầng 2, và làm dữ liệu thật ở leaf. Đúng như sách nói *"key bị lặp lại ở
internal node để chỉ ra khoảng của subtree"*.

**2. Chi phí biển chỉ đường rất rẻ:**

| | Số key |
|---|---|
| Tầng 1 + 2 (dẫn đường) | **422** |
| Tầng 3 (dữ liệu thật) | **104.334** |

Tốn thêm **0,4%** để khỏi phải quét 104.334 từ. Toàn bộ dữ liệu **chỉ nằm ở leaf**
— đó là ý nghĩa của chữ **"+"** trong B+tree.

## 6. Tầng trên nằm trong RAM → đọc đĩa còn ít hơn chiều cao

| Số bản ghi | Cao | 2 tầng trên | **Đọc đĩa THỰC TẾ** |
|---|---|---|---|
| 100.000 | 3 tầng | 12 KB | **1 lần** |
| 1.000.000 | 3 tầng | 80 KB | **1 lần** |
| 1.000.000.000 | 4 tầng | 332 KB | **2 lần** |
| 10.000.000.000 | 5 tầng | 20 KB | **3 lần** |

Hai tầng trên luôn bé tí → **nằm thường trú trong RAM**, không tính vào đọc đĩa.

## 7. Con số cuối cùng

Tra cứu 1 bản ghi trong **1 tỷ bản ghi**, trên HDD:

| | Chiều cao thực tế | Đọc đĩa thật | Thời gian |
|---|---|---|---|
| **BST** | ~70 tầng | ~70 lần | **~0,7 giây** |
| **B+tree** | 4 tầng | **2 lần** | **~0,02 giây** |

**Chênh ~35 lần** — và đó mới là *một* truy vấn.

Ba thứ cộng lại:

1. **Fanout lớn** — 222 nhánh/node thay vì 2, vì tận dụng trọn trang 4KB
2. **Luôn cân bằng** — mọi leaf cùng độ cao, không có nhánh xui
3. **Tầng trên bé** → nằm trong RAM → đọc đĩa thật còn ít hơn chiều cao

## 8. So sánh tóm tắt

| | BST | B+tree |
|---|---|---|
| Mỗi node chứa | 1 key | **hàng trăm key** (= 1 trang) |
| Số con | 2 | **hàng trăm** |
| Cao (1 tỷ bản ghi) | ~70 tầng thực tế | **4 tầng** |
| Dữ liệu nằm ở | mọi node | **chỉ ở leaf** |
| Tự cân bằng | ❌ | ✅ mọi leaf cùng độ cao |
| Dùng cho | trong RAM | **trên đĩa** |

---

## Những chỗ đã đơn giản hoá

Con số 249/222 là ước lượng. Thực tế còn:

- **Header của trang** — mỗi trang mất ~16–24 byte cho `type`, `nkeys`, `lsn`…
- **Key dài không đều** — 8,4 byte chỉ là trung bình
- **Page không đầy 100%** — B+tree thường chỉ lấp ~70% để chừa chỗ insert
  (PostgreSQL gọi là `fillfactor`, mặc định **90%** cho index)
- **Nén tiền tố** — `hospital`/`hospitals` dùng chung đầu, nén lại nhét được nhiều hơn
  (sách nhắc ở mục tối ưu chương 11)

Và: **PostgreSQL dùng trang 8KB**, gấp đôi 4KB của sách → **fanout gấp đôi**
→ cây còn thấp hơn.

## Câu hỏi tự kiểm tra

1. Vì sao fanout của leaf (249) lại lớn hơn fanout của internal (222)?
2. Đổi primary key từ `int64` sang UUID dạng text, cây cao thêm mấy tầng? Vì sao?
3. Một B+tree có 4 tầng nhưng thực tế chỉ tốn 2 lần đọc đĩa — vì sao?
4. Nếu trang là 8KB thay vì 4KB, fanout và chiều cao thay đổi thế nào?
