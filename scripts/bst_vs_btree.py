#!/usr/bin/env python3
"""
Đo chiều cao THẬT của cây nhị phân (BST) và B+tree trên cùng một bộ dữ liệu thật.
Dữ liệu: /usr/share/dict/words — danh sách từ tiếng Anh.

Chạy:  python3 scripts/bst_vs_btree.py
"""
import math, random, sys

PAGE = 4096          # kích thước 1 trang = 1 node B+tree
PTR  = 8             # con trỏ tới node con: 8 byte
TID  = 6             # con trỏ tới bản ghi thật trong heap: 6 byte
OVH  = 2             # overhead mỗi mục (offset trong slotted page)

HDD_MS, SSD_MS = 10.0, 0.1   # độ trễ một lần đọc ngẫu nhiên

# ---------- dữ liệu thật ----------
words = sorted({w.strip() for w in open('/usr/share/dict/words', encoding='utf-8', errors='ignore') if w.strip()})
n = len(words)
avg_key = sum(len(w.encode()) for w in words) / n

# ---------- 1. BST thật, chèn theo thứ tự ngẫu nhiên ----------
def bst_height(keys):
    """Dựng BST thật (lặp, không đệ quy) rồi trả về chiều cao thật."""
    root = None
    nodes = {}          # id -> (key, left, right)
    depth = {}
    maxd = 0
    for k in keys:
        if root is None:
            root = k; nodes[k] = [None, None]; depth[k] = 1; maxd = 1
            continue
        cur, d = root, 1
        while True:
            d += 1
            side = 0 if k < cur else 1
            nxt = nodes[cur][side]
            if nxt is None:
                nodes[cur][side] = k
                nodes[k] = [None, None]
                depth[k] = d
                maxd = max(maxd, d)
                break
            cur = nxt
    return maxd, sum(depth.values()) / len(depth)

shuffled = words[:]
random.seed(42)
random.shuffle(shuffled)
bst_max, bst_avg = bst_height(shuffled)

# ---------- 2. B+tree thật, bulk-load từ dưới lên ----------
leaf_cap = int(PAGE // (avg_key + TID + OVH))       # leaf: key + con trỏ tới bản ghi
int_cap  = int(PAGE // (avg_key + PTR + OVH))       # internal: key + con trỏ node con

levels = []
cnt = math.ceil(n / leaf_cap)
levels.append(('leaf', cnt))
while cnt > 1:
    cnt = math.ceil(cnt / int_cap)
    levels.append(('internal', cnt))
levels.reverse()
bp_height = len(levels)
bp_nodes  = sum(c for _, c in levels)

# ---------- in kết quả ----------
W = 68
print('=' * W)
print(f'DỮ LIỆU THẬT: {n:,} từ tiếng Anh  (/usr/share/dict/words)')
print(f'Key trung bình {avg_key:.1f} byte, trang {PAGE} byte')
print('=' * W)

print(f'\n[1] CÂY NHỊ PHÂN (BST) — dựng thật, chèn ngẫu nhiên')
print(f'    Chiều cao thật        : {bst_max} tầng')
print(f'    Độ sâu trung bình     : {bst_avg:.1f} tầng')
print(f'    (lý thuyết log2 N     : {math.log2(n):.1f})')

print(f'\n[2] B+TREE — bulk-load thật, node = 1 trang {PAGE}B')
print(f'    Sức chứa 1 leaf       : {leaf_cap} key')
print(f'    Sức chứa 1 internal   : {int_cap} nhánh')
print(f'    Chiều cao thật        : {bp_height} tầng')
print(f'    Tổng số node          : {bp_nodes:,}  ({bp_nodes*PAGE/1024/1024:.1f} MB)')
print(f'    Cấu trúc từng tầng    :')
for i, (kind, c) in enumerate(levels):
    print(f'        tầng {i+1}: {c:>7,} node {kind}')

print(f'\n[3] MỘT LẦN TRA CỨU TỐN BAO NHIÊU LẦN ĐỌC ĐĨA')
print(f'    {"":22}{"BST":>10}{"B+tree":>10}{"nhanh hơn":>12}')
print(f'    {"-"*54}')
print(f'    {"số lần đọc đĩa":22}{bst_max:>10}{bp_height:>10}{bst_max/bp_height:>11.1f}x')
print(f'    {"trên HDD (10ms)":22}{bst_max*HDD_MS:>9.0f}ms{bp_height*HDD_MS:>9.0f}ms{bst_max/bp_height:>11.1f}x')
print(f'    {"trên SSD (0.1ms)":22}{bst_max*SSD_MS:>9.1f}ms{bp_height*SSD_MS:>9.1f}ms{bst_max/bp_height:>11.1f}x')

print(f'\n[4] NẾU DỮ LIỆU LỚN HƠN (ngoại suy, cùng kích thước key)')
print(f'    {"số bản ghi":>16}{"BST":>10}{"B+tree":>10}')
print(f'    {"-"*36}')
for m in (10**6, 10**8, 10**9, 10**10):
    hb = math.ceil(math.log2(m))
    lv, c = 1, math.ceil(m / leaf_cap)
    while c > 1:
        c = math.ceil(c / int_cap); lv += 1
    print(f'    {m:>16,}{hb:>8} tầng{lv:>8} tầng')
