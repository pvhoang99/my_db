package btree

// Quét theo khoảng: mọi mục có lo <= key <= hi, theo đúng thứ tự trong index.
// Đây là phần "Index Cond: (id >= 1) AND (id <= 20000)" của Index Scan và
// Bitmap Index Scan trong EXPLAIN.
//
//	① đi xuống tới page lá chứa lo (lo = nil: page lá ngoài cùng bên trái)
//	② tìm nhị phân mục đầu tiên >= lo
//	③ đi tuần tự sang phải, qua right-link sang page lá kế tiếp,
//	   tới khi gặp key > hi
//
// Tương ứng _bt_first() (bước ①②) và _bt_next() / _bt_steppage() (bước ③).
// Iterator giữ ghim page lá đang đọc, bỏ ghim khi sang page sau.

import (
	"github.com/hoangpv/minidb/internal/buffer"
	"github.com/hoangpv/minidb/internal/common"
	"github.com/hoangpv/minidb/internal/types"
)

// RangeIter đi qua các mục trong khoảng [lo, hi] (cả hai đầu đều lấy).
type RangeIter struct {
	ix *Index
	hi *types.Datum // nil = không có cận trên

	buf    buffer.Buffer       // page lá đang đọc (đang ghim)
	pinned bool                //
	op     Opaque              // special của page lá đang đọc
	off    common.OffsetNumber // ô sẽ đọc tiếp theo
	last   common.OffsetNumber // ô cuối của page

	tid  common.ItemPointer // ctid của mục hiện tại
	err  error
	done bool
}

// RangeScan bắt đầu quét các mục có lo <= key <= hi. lo/hi = nil nghĩa là không giới hạn.
func (ix *Index) RangeScan(lo, hi *types.Datum) (*RangeIter, error) {
	it := &RangeIter{ix: ix, hi: hi}
	meta, err := ix.Meta()
	if err != nil {
		return nil, err
	}
	if meta.Root == P_NONE {
		it.done = true
		return it, nil
	}

	// ① Đi xuống tới page lá.
	blk := meta.Root
	for {
		buf, err := ix.pool.ReadBuffer(ix.file, blk)
		if err != nil {
			return nil, err
		}
		p := ix.pool.Page(buf)
		op := readOpaque(p)
		if lo != nil {
			if right, err := ix.beyondHighKey(p, op, *lo); err != nil {
				ix.pool.Release(buf)
				return nil, err
			} else if right {
				ix.pool.Release(buf)
				blk = op.Next
				continue
			}
		}
		if op.IsLeaf() {
			it.buf, it.pinned, it.op = buf, true, op
			it.last = p.MaxOffsetNumber()
			break
		}
		var child common.BlockNumber
		if lo == nil {
			// Không có cận dưới: luôn đi mục đầu tiên (−∞) -> page trái nhất.
			var t IndexTuple
			t, err = readIndexTuple(p, op.FirstDataKey())
			child = t.Downlink()
		} else {
			child, err = ix.chooseChild(p, op, *lo)
		}
		ix.pool.Release(buf)
		if err != nil {
			return nil, err
		}
		blk = child
	}

	// ② Mục đầu tiên >= lo trên page lá.
	it.off = it.op.FirstDataKey()
	if lo != nil {
		p := ix.pool.Page(it.buf)
		lo2, hi2 := it.off, it.last+1
		for lo2 < hi2 {
			mid := lo2 + (hi2-lo2)/2
			t, err := readIndexTuple(p, mid)
			if err != nil {
				it.Close()
				return nil, err
			}
			c, err := ix.compare(*lo, t.Key)
			if err != nil {
				it.Close()
				return nil, err
			}
			if c > 0 {
				lo2 = mid + 1
			} else {
				hi2 = mid
			}
		}
		it.off = lo2
	}
	return it, nil
}

// Next tiến tới mục kế tiếp trong khoảng. false = hết (hoặc lỗi, xem Err).
func (it *RangeIter) Next() bool {
	for !it.done {
		// ③ Hết page: sang page lá bên phải theo right-link.
		if it.off > it.last {
			if it.op.IsRightmost() {
				it.Close()
				return false
			}
			next := it.op.Next
			it.ix.pool.Release(it.buf)
			it.pinned = false
			buf, err := it.ix.pool.ReadBuffer(it.ix.file, next)
			if err != nil {
				it.err = err
				it.done = true
				return false
			}
			p := it.ix.pool.Page(buf)
			it.buf, it.pinned, it.op = buf, true, readOpaque(p)
			it.off, it.last = it.op.FirstDataKey(), p.MaxOffsetNumber()
			continue
		}

		p := it.ix.pool.Page(it.buf)
		t, err := readIndexTuple(p, it.off)
		if err != nil {
			it.err = err
			it.Close()
			return false
		}
		it.off++
		if it.hi != nil {
			c, err := it.ix.compare(*it.hi, t.Key)
			if err != nil {
				it.err = err
				it.Close()
				return false
			}
			if c < 0 { // key > hi: ra khỏi khoảng
				it.Close()
				return false
			}
		}
		if t.Info&INDEX_ALT_TID_MASK != 0 && t.TID.Offset&BT_IS_POSTING != 0 {
			it.err = errNotSupported("posting list (deduplication)")
			it.Close()
			return false
		}
		it.tid = t.TID
		return true
	}
	return false
}

// TID là ctid của mục hiện tại.
func (it *RangeIter) TID() common.ItemPointer { return it.tid }

// Err là lỗi làm dừng lượt quét, nếu có.
func (it *RangeIter) Err() error { return it.err }

// Close bỏ ghim page lá đang đọc. Gọi được nhiều lần.
func (it *RangeIter) Close() {
	if it.pinned {
		it.ix.pool.Release(it.buf)
		it.pinned = false
	}
	it.done = true
}
