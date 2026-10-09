#!/usr/bin/env python3
"""Demo: append-only log voi checksum, va 4 tinh huong crash.
Chay: python3 scripts/log_checksum_demo.py"""
import zlib
LOG='/tmp/kv3.log'

def entry(p: bytes) -> bytes:
    return len(p).to_bytes(4,'little') + zlib.crc32(p).to_bytes(4,'little') + p

def replay(path):
    data = open(path,'rb').read()
    state, pos, n, stop = {}, 0, 0, None
    while pos < len(data):
        if pos+8 > len(data): stop="header cut giua chung"; break
        ln  = int.from_bytes(data[pos:pos+4],'little')
        crc = int.from_bytes(data[pos+4:pos+8],'little')
        if ln == 0:                         # <-- BAN VA
            stop="gap vung toan byte 00 (len=0)"; break
        body = data[pos+8:pos+8+ln]
        if len(body) < ln: stop=f"payload thieu ({len(body)}/{ln} byte)"; break
        if zlib.crc32(body) != crc: stop="CHECKSUM SAI"; break
        op, rest = body.decode().split(' ',1)
        if op=='set': k,v=rest.split('=',1); state[k]=v
        else: state.pop(rest,None)
        n+=1; pos+=8+ln
    return state, n, stop

GOOD=[b'set a=1',b'set b=2',b'set a=3',b'del b']
open(LOG,'wb').write(b''.join(entry(x) for x in GOOD))
full=open(LOG,'rb').read()
last=len(entry(GOOD[-1]))
print(f"Log lanh lan {len(full)} byte -> replay: {replay(LOG)[0]}  ({replay(LOG)[1]} entry)\n")

for ten, noidung in [
    ("(a) append CHUA KIP xay ra",      full[:-last]),
    ("(b) entry ghi DUOC MOT NUA",      full[:-4]),
    ("(c) file DAI RA, du lieu = 00",   full[:-last] + b'\x00'*last),
    ("(d) HONG 1 BIT trong entry cuoi", bytes(bytearray(full[:-1]) + bytes([full[-1]^1]))),
]:
    open(LOG,'wb').write(noidung)
    st,n,stop = replay(LOG)
    print(f"{ten}")
    print(f"    -> {st}   |  {n} entry hop le  |  dung vi: {stop or 'het file'}")
print(f"\nMOI TRUONG HOP deu ra trang thai HOP LE: a=3 b=2 (tuc 3 entry dau).")
