package btree

// Check đi qua toàn bộ cây, kiểm các luật của B-tree và thống kê hình dạng cây.
// Giống bt_index_check() của extension amcheck trong Postgres (bản rút gọn).
//
// Luật được kiểm, trên từng tầng (đi từ page trái nhất sang phải theo right-link):
//
//	- mục trong page sắp tăng dần theo (key, heap TID)
//	- mọi mục <= high key của page
//	- right-link hai chiều khớp nhau: A.next = B thì B.prev = A
//	- mọi page cùng tầng có cùng level; page lá có cờ BTP_LEAF
//	- page ngoài cùng bên phải không có high key

import (
	"fmt"

	"github.com/hoangpv/minidb/internal/common"
	"github.com/hoangpv/minidb/internal/storage"
)

// Shape là hình dạng cây, để so với bt_metap() và bt_multi_page_stats().
type Shape struct {
	Level      uint32 // tầng của page gốc (số tầng = Level + 1)
	Pages      int    // tổng số page của file, kể cả metapage
	Leaves     int    // số page lá
	LeafItems  []int  // số mục dữ liệu (không tính high key) của từng page lá, trái -> phải
	TotalItems int    // tổng số mục ở tầng lá
}

func (ix *Index) Check() (Shape, error) {
	var sh Shape
	meta, err := ix.Meta()
	if err != nil {
		return sh, err
	}
	n, err := ix.file.NumBlocks()
	if err != nil {
		return sh, err
	}
	sh.Pages, sh.Level = int(n), meta.Level
	if meta.Root == P_NONE {
		return sh, nil
	}

	// Đi xuống theo mép trái để tìm page đầu tiên của mỗi tầng.
	leftmost := meta.Root
	for level := int(meta.Level); level >= 0; level-- {
		next, err := ix.checkLevel(leftmost, uint32(level), &sh)
		if err != nil {
			return sh, err
		}
		leftmost = next
	}
	return sh, nil
}

// checkLevel đi hết một tầng từ page first. Trả về page trái nhất của tầng dưới.
func (ix *Index) checkLevel(first common.BlockNumber, level uint32, sh *Shape) (common.BlockNumber, error) {
	var downFirst common.BlockNumber
	prev := P_NONE
	for blk := first; ; {
		buf, err := ix.pool.ReadBuffer(ix.file, blk)
		if err != nil {
			return 0, err
		}
		p := ix.pool.Page(buf)
		op := readOpaque(p)
		err = ix.checkPage(p, op, blk, level, prev)
		if err == nil && blk == first && level > 0 {
			var t IndexTuple
			t, err = readIndexTuple(p, op.FirstDataKey())
			downFirst = t.Downlink()
		}
		if err == nil && level == 0 {
			items := int(p.MaxOffsetNumber()) - int(op.FirstDataKey()) + 1
			sh.Leaves++
			sh.LeafItems = append(sh.LeafItems, items)
			sh.TotalItems += items
		}
		ix.pool.Release(buf)
		if err != nil {
			return 0, fmt.Errorf("page %d (tầng %d): %w", blk, level, err)
		}
		if op.IsRightmost() {
			return downFirst, nil
		}
		prev, blk = blk, op.Next
	}
}

func (ix *Index) checkPage(p storage.Page, op Opaque, blk common.BlockNumber, level uint32, prev common.BlockNumber) error {
	if op.Level != level || op.IsLeaf() != (level == 0) {
		return fmt.Errorf("level=%d leaf=%v, muốn level=%d", op.Level, op.IsLeaf(), level)
	}
	if op.Prev != prev {
		return fmt.Errorf("prev=%d nhưng page bên trái là %d", op.Prev, prev)
	}
	var hk *IndexTuple
	if !op.IsRightmost() {
		t, err := readIndexTuple(p, P_HIKEY)
		if err != nil {
			return err
		}
		hk = &t
	}
	var last *IndexTuple
	for off := op.FirstDataKey(); off <= p.MaxOffsetNumber(); off++ {
		t, err := readIndexTuple(p, off)
		if err != nil {
			return err
		}
		// Mục −∞ (đầu page giữa) không có key: bỏ qua khi so thứ tự.
		if len(t.Key) == 0 {
			continue
		}
		if last != nil {
			if c, err := ix.compareTuples(*last, t); err != nil {
				return err
			} else if c >= 0 {
				return fmt.Errorf("ô %d không lớn hơn ô trước", off)
			}
		}
		if hk != nil {
			if c, err := ix.compareTuples(t, *hk); err != nil {
				return err
			} else if c >= 0 && !(c == 0 && level > 0) {
				return fmt.Errorf("ô %d vượt high key", off)
			}
		}
		tt := t
		last = &tt
	}
	return nil
}

// compareTuples so hai mục theo (key, heap TID); pivot đã cắt heap TID coi như (key, −∞).
func (ix *Index) compareTuples(a, b IndexTuple) (int, error) {
	va, _, err := decodeKey(a.Key, ix.keyType)
	if err != nil {
		return 0, err
	}
	c, err := ix.compare(va, b.Key)
	if err != nil || c != 0 {
		return c, err
	}
	tid := func(t IndexTuple) (common.ItemPointer, bool) {
		if t.isPivot() {
			if t.HeapTID == nil {
				return common.ItemPointer{}, false // −∞
			}
			return *t.HeapTID, true
		}
		return t.TID, true
	}
	ta, oka := tid(a)
	tb, okb := tid(b)
	switch {
	case !oka && !okb:
		return 0, nil
	case !oka:
		return -1, nil
	case !okb:
		return 1, nil
	}
	return compareTID(ta, tb), nil
}
