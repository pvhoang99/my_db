PAGE, SECTOR = 4096, 512

def make_page(nkeys, ch):
    """Trang don gian: 4 byte header ghi so key, phan con lai la du lieu."""
    return nkeys.to_bytes(4,'little') + (ch*(PAGE-4))

def doc_page(p):
    n = int.from_bytes(p[:4],'little')
    body = p[4:]
    thuc_te = len(set(body))           # bao nhieu loai ky tu -> phat hien tron lan
    return n, body[:1].decode(), body[-1:].decode(), thuc_te

cu  = make_page(3, b'A')       # trang CU : 3 key, du lieu toan 'A'
moi = make_page(9, b'B')       # trang MOI: 9 key, du lieu toan 'B'

print("Ghi de trang CU bang trang MOI. Mat dien sau khi ghi duoc 5/8 sector.\n")
rach = moi[:5*SECTOR] + cu[5*SECTOR:]      # 5 sector dau la MOI, con lai van CU

for ten, p in [("Trang CU  ", cu), ("Trang MOI ", moi), ("Trang RACH", rach)]:
    n, d, c, loai = doc_page(p)
    print(f"{ten}: header noi co {n} key | byte dau='{d}' byte cuoi='{c}' | so loai ky tu trong body = {loai}")

print("\n>>> Trang RACH: header noi 9 key (moi), nhung nua cuoi du lieu van la cu.")
print(">>> Khong con tuong ung voi BAT KY phien ban nao. Cau truc HONG.")
print(">>> Checksum phat hien duoc -> nhung biet roi thi lam gi? Du lieu cu da bi de len mat roi.")
