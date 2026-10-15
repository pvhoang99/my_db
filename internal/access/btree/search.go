package btree

// Tìm kiếm trên B-tree: đi từ page gốc xuống page lá, rồi lấy ctid.
// Đây là nửa đầu của "Index Scan" trong EXPLAIN; nửa sau là heap.Fetch(ctid).
//
//	① metapage (page 0)  -> page gốc ở đâu
//	② page giữa/gốc       -> chọn mục cuối cùng có key <= key cần tìm, đi xuống page con
//	③ page lá             -> các mục có key = key cần tìm -> ctid
//
// Giới hạn của bước M2.2 (sẽ mở rộng ở M2.3–M2.4):
//   - index MỘT cột, kiểu int4/int8/text; text so theo byte (cần COLLATE "C")
//   - chỉ tìm bằng (=), chưa tìm theo khoảng
//   - chưa đọc posting list (deduplication) — báo ErrNotSupported nếu gặp

import (
	"bytes"
	"fmt"

	"github.com/hoangpv/minidb/internal/access/heap"
	"github.com/hoangpv/minidb/internal/buffer"
	"github.com/hoangpv/minidb/internal/common"
	"github.com/hoangpv/minidb/internal/storage"
	"github.com/hoangpv/minidb/internal/types"
)

// Index là một index B-tree trên một cột.
type Index struct {
	file    *storage.File // file của index trên đĩa
	pool    *buffer.Pool  // buffer pool dùng chung (cùng pool với heap)
	keyType types.Type    // kiểu của cột được đánh index
}

// Open mở file index. keyType là kiểu cột (index chỉ lưu byte, không lưu kiểu).
func Open(path string, keyType types.Type, pool *buffer.Pool) (*Index, error) {
	if keyType != types.Int4 && keyType != types.Int8 && keyType != types.Text {
		return nil, errNotSupported("key kiểu " + keyType.String())
	}
	f, err := storage.OpenFile(path)
	if err != nil {
		return nil, err
	}
	return &Index{file: f, pool: pool, keyType: keyType}, nil
}

// Close bỏ các page của index khỏi pool rồi đóng file.
func (ix *Index) Close() error {
	if err := ix.pool.DropFile(ix.file); err != nil {
		ix.file.Close()
		return err
	}
	return ix.file.Close()
}

// Meta đọc metapage.
func (ix *Index) Meta() (Meta, error) {
	buf, err := ix.pool.ReadBuffer(ix.file, 0)
	if err != nil {
		return Meta{}, err
	}
	defer ix.pool.Release(buf)
	return readMeta(ix.pool.Page(buf))
}

// SearchResult là kết quả một lần tìm.
type SearchResult struct {
	TIDs []common.ItemPointer // ctid của các dòng có key bằng key cần tìm

	// Đường đi: các page index đã đọc, theo thứ tự (gồm cả metapage).
	// len(Path) chính là số page index trong "Buffers:" của Index Scan.
	Path []common.BlockNumber
}

// Search tìm mọi ctid có key = key. Tương ứng _bt_search() + _bt_first() + _bt_next().
func (ix *Index) Search(key types.Datum) (SearchResult, error) {
	var res SearchResult
	if key.Type != ix.keyType || key.IsNull {
		return res, fmt.Errorf("btree: key phải là %s khác NULL", ix.keyType)
	}

	// ① Metapage: page gốc ở đâu.
	meta, err := ix.Meta()
	if err != nil {
		return res, err
	}
	res.Path = append(res.Path, 0)
	if meta.Root == P_NONE {
		return res, nil // index rỗng
	}

	// ② Đi xuống từ page gốc tới page lá.
	blk := meta.Root
	for {
		buf, err := ix.pool.ReadBuffer(ix.file, blk)
		if err != nil {
			return res, err
		}
		res.Path = append(res.Path, blk)
		p := ix.pool.Page(buf)
		op := readOpaque(p)

		// Lehman–Yao: trong lúc ta đi xuống, page có thể vừa bị split; key cần
		// tìm khi đó nằm ở page bên phải. Key > high key thì đi sang phải.
		if right, err := ix.beyondHighKey(p, op, key); err != nil {
			ix.pool.Release(buf)
			return res, err
		} else if right {
			ix.pool.Release(buf)
			blk = op.Next
			continue
		}

		if op.IsLeaf() {
			ix.pool.Release(buf)
			break
		}

		// Page giữa: chọn mục cuối cùng có key <= key cần tìm, đi xuống page con.
		child, err := ix.chooseChild(p, op, key)
		ix.pool.Release(buf)
		if err != nil {
			return res, err
		}
		blk = child
	}

	// ③ Page lá: gom các mục có key = key, đi sang phải nếu còn.
	return ix.collectEqual(blk, key, res)
}

// chooseChild tìm trên page giữa mục cuối cùng có key <= key cần tìm, trả về page con.
// Mục dữ liệu đầu tiên là −∞ nên luôn thoả; tìm nhị phân trên các mục còn lại.
// Tương ứng _bt_binsrch() trên page không phải lá.
//
//	page gốc hoc_m22_id:  [−∞ -> p1] [367 -> p2] [733 -> p4]
//	tìm 500:  −∞ <= 500, 367 <= 500, 733 > 500  -> mục 367 -> đi xuống page 2
func (ix *Index) chooseChild(p storage.Page, op Opaque, key types.Datum) (common.BlockNumber, error) {
	first, last := op.FirstDataKey(), p.MaxOffsetNumber()
	if first > last {
		return 0, fmt.Errorf("btree: page giữa không có mục nào")
	}
	// Bất biến: mục ở ô lo có key <= key cần tìm (ban đầu lo = −∞).
	lo, hi := first, last
	for lo < hi {
		mid := lo + (hi-lo+1)/2 // làm tròn lên để vòng lặp luôn tiến
		t, err := readIndexTuple(p, mid)
		if err != nil {
			return 0, err
		}
		c, err := ix.comparePivot(key, t)
		if err != nil {
			return 0, err
		}
		if c >= 0 { // key cần tìm >= key của mục mid -> mục mid thoả
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	t, err := readIndexTuple(p, lo)
	if err != nil {
		return 0, err
	}
	return t.Downlink(), nil
}

// collectEqual bắt đầu từ page lá blk, gom ctid của mọi mục có key = key.
// Các mục bằng nhau có thể kéo sang page bên phải, nên đi theo right-link.
func (ix *Index) collectEqual(blk common.BlockNumber, key types.Datum, res SearchResult) (SearchResult, error) {
	for {
		buf, err := ix.pool.ReadBuffer(ix.file, blk)
		if err != nil {
			return res, err
		}
		if blk != res.Path[len(res.Path)-1] {
			res.Path = append(res.Path, blk)
		}
		p := ix.pool.Page(buf)
		op := readOpaque(p)

		// Tìm nhị phân mục ĐẦU TIÊN có key >= key cần tìm.
		first, last := op.FirstDataKey(), p.MaxOffsetNumber()
		lo, hi := first, last+1
		for lo < hi {
			mid := lo + (hi-lo)/2
			t, err := readIndexTuple(p, mid)
			if err != nil {
				ix.pool.Release(buf)
				return res, err
			}
			c, err := ix.compare(key, t.Key)
			if err != nil {
				ix.pool.Release(buf)
				return res, err
			}
			if c > 0 {
				lo = mid + 1
			} else {
				hi = mid
			}
		}

		// Đi tiếp các mục bằng key.
		for off := lo; off <= last; off++ {
			t, err := readIndexTuple(p, off)
			if err != nil {
				ix.pool.Release(buf)
				return res, err
			}
			if c, err := ix.compare(key, t.Key); err != nil || c != 0 {
				ix.pool.Release(buf)
				return res, err // gặp key lớn hơn: xong
			}
			if t.Info&INDEX_ALT_TID_MASK != 0 && t.TID.Offset&BT_IS_POSTING != 0 {
				ix.pool.Release(buf)
				return res, errNotSupported("posting list (deduplication)")
			}
			res.TIDs = append(res.TIDs, t.TID)
		}

		// Hết page mà vẫn bằng: có thể còn ở page bên phải (nếu high key >= key).
		next := op.Next
		more := false
		if !op.IsRightmost() {
			hk, err := readIndexTuple(p, P_HIKEY)
			if err == nil {
				c, cerr := ix.compare(key, hk.Key)
				err = cerr
				more = c >= 0
			}
			if err != nil {
				ix.pool.Release(buf)
				return res, err
			}
		}
		ix.pool.Release(buf)
		if !more {
			return res, nil
		}
		blk = next
	}
}

// beyondHighKey: key cần tìm có lớn hơn high key của page không (thì phải sang phải).
// Tương ứng _bt_moveright().
func (ix *Index) beyondHighKey(p storage.Page, op Opaque, key types.Datum) (bool, error) {
	if op.IsRightmost() {
		return false, nil // page ngoài cùng bên phải: không có high key, không giới hạn
	}
	hk, err := readIndexTuple(p, P_HIKEY)
	if err != nil {
		return false, err
	}
	c, err := ix.comparePivot(key, hk)
	return c > 0, err
}

// comparePivot so key cần tìm (không có heap TID) với một pivot, theo đúng luật
// _bt_compare() của Postgres khi key bằng nhau:
//
//	pivot đã cắt heap TID (−∞): key cần tìm coi như LỚN hơn  -> đi sang phải
//	pivot còn giữ heap TID:     key cần tìm coi như NHỎ hơn  -> đi sang trái
//
// Lý do: pivot cắt TID nghĩa là mọi mục có key này đều nằm bên phải; pivot giữ
// TID nghĩa là các mục bằng key bị chia ra hai bên, nên phải bắt đầu từ bên trái.
func (ix *Index) comparePivot(key types.Datum, t IndexTuple) (int, error) {
	c, err := ix.compare(key, t.Key)
	if err != nil || c != 0 {
		return c, err
	}
	if t.HeapTID != nil {
		return -1, nil
	}
	return 1, nil
}

// compare so key cần tìm với phần key của một mục: âm / 0 / dương.
// Mục có phần key rỗng (−∞, hoặc key bị cắt bớt) coi như nhỏ hơn mọi key.
func (ix *Index) compare(key types.Datum, raw []byte) (int, error) {
	if len(raw) == 0 {
		return 1, nil // key cần tìm > −∞
	}
	v, _, err := decodeKey(raw, ix.keyType)
	if err != nil {
		return 0, err
	}
	switch ix.keyType {
	case types.Int4, types.Int8:
		switch {
		case key.Int < v.Int:
			return -1, nil
		case key.Int > v.Int:
			return 1, nil
		}
		return 0, nil
	case types.Text:
		return bytes.Compare([]byte(key.Str), []byte(v.Str)), nil
	}
	return 0, errNotSupported("so sánh kiểu " + ix.keyType.String())
}

// decodeKey đọc phần key của một mục. Key bắt đầu ở byte 8 của mục — bội số
// của 8 — nên căn lề tính từ đầu phần key cho kết quả giống hệt Postgres.
func decodeKey(raw []byte, t types.Type) (types.Datum, int, error) {
	return heap.DecodeDatum(raw, 0, t)
}
