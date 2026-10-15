package btree

// Chèn vào B-tree, kể cả tách page (split) và mọc page gốc mới.
//
// Tương ứng trong Postgres:
//
//	nbtinsert.c    _bt_doinsert, _bt_insertonpg, _bt_split, _bt_insert_parent, _bt_newlevel
//	nbtsplitloc.c  _bt_findsplitloc (chọn điểm tách)
//
// Luồng chèn một key:
//
//	1. đi xuống như Search, ghi lại các page đã đi qua (stack) để lát còn quay lên
//	2. ở page lá: chèn mục vào ĐÚNG vị trí theo (key, heap TID)
//	3. page đầy -> split:
//	     page L [ ... nửa trái ... | high key ] --next--> page R mới [ ... nửa phải ... ]
//	     page cha: thêm pivot [key đầu của R -> R]   (page cha đầy thì split tiếp)
//	4. split tới page gốc -> tạo page gốc mới, cây cao thêm 1 tầng
//
// Chọn điểm tách (giống Postgres):
//
//	page ngoài cùng bên phải (nơi id tăng dần luôn rơi vào) -> để trái đầy 90%
//	(page giữa: 70%), vì đoán rằng các key sau sẽ tiếp tục vào bên phải
//	còn lại -> tách 50/50 theo số byte
//
// Chưa làm (so với Postgres): WAL (M3), khoá cho nhiều người ghi cùng lúc,
// deduplication, cắt bớt key ở pivot (suffix truncation) cho index nhiều cột.

import (
	"bytes"
	"fmt"

	"github.com/hoangpv/minidb/internal/access/heap"
	"github.com/hoangpv/minidb/internal/buffer"
	"github.com/hoangpv/minidb/internal/common"
	"github.com/hoangpv/minidb/internal/storage"
	"github.com/hoangpv/minidb/internal/types"
)

// Fillfactor khi chèn vào cuối page ngoài cùng bên phải.
// Postgres: BTREE_DEFAULT_FILLFACTOR = 90 (lá), BTREE_NONLEAF_FILLFACTOR = 70.
const (
	leafFillfactor    = 90
	nonleafFillfactor = 70
)

// Create tạo file index mới chỉ có metapage (cây rỗng, chưa có page gốc).
// Page gốc được tạo ở lần chèn đầu tiên. Tương ứng btbuildempty().
func Create(path string, keyType types.Type, pool *buffer.Pool) (*Index, error) {
	ix, err := Open(path, keyType, pool)
	if err != nil {
		return nil, err
	}
	n, err := ix.file.NumBlocks()
	if err != nil {
		return nil, err
	}
	if n != 0 {
		return nil, fmt.Errorf("btree: %s đã có dữ liệu", path)
	}
	buf, _, err := pool.ExtendBuffer(ix.file) // page 0
	if err != nil {
		return nil, err
	}
	writeMeta(pool.Page(buf), Meta{Root: P_NONE})
	pool.MarkDirty(buf)
	pool.Release(buf)
	return ix, nil
}

// Insert thêm mục (key -> htid). htid là địa chỉ dòng trong heap.
// Key trùng được phép: các mục sắp theo (key, htid). Tương ứng _bt_doinsert().
func (ix *Index) Insert(key types.Datum, htid common.ItemPointer) error {
	if key.Type != ix.keyType || key.IsNull {
		return fmt.Errorf("btree: key phải là %s khác NULL", ix.keyType)
	}
	enc := heap.EncodeDatum(nil, key)
	item := makeLeafTuple(enc, htid)
	if len(item) > maxItemSize {
		return fmt.Errorf("btree: key quá lớn (%d byte, tối đa %d)", len(item), maxItemSize)
	}

	meta, err := ix.Meta()
	if err != nil {
		return err
	}
	// Cây rỗng: page lá đầu tiên đồng thời là page gốc.
	if meta.Root == P_NONE {
		return ix.newRootLeaf(item)
	}

	// 1. Đi xuống, ghi lại đường đi (stack) các page giữa đã qua.
	ins := insertKey{key: key, htid: htid}
	var stack []common.BlockNumber
	blk := meta.Root
	for {
		buf, err := ix.pool.ReadBuffer(ix.file, blk)
		if err != nil {
			return err
		}
		p := ix.pool.Page(buf)
		op := readOpaque(p)
		// (key, htid) > high key: mục thuộc page bên phải.
		if !op.IsRightmost() {
			hk, err := readIndexTuple(p, P_HIKEY)
			if err == nil {
				var c int
				c, err = ix.compareInsert(ins, hk)
				if err == nil && c > 0 {
					ix.pool.Release(buf)
					blk = op.Next
					continue
				}
			}
			if err != nil {
				ix.pool.Release(buf)
				return err
			}
		}
		if op.IsLeaf() {
			ix.pool.Release(buf)
			break
		}
		child, err := ix.childForInsert(p, op, ins)
		ix.pool.Release(buf)
		if err != nil {
			return err
		}
		stack = append(stack, blk)
		blk = child
	}

	// 2–4. Chèn vào page lá (split nếu cần, lan lên trên).
	return ix.insertOnPage(blk, item, ins, nil, stack)
}

// maxItemSize: mục lớn nhất cho phép, để một page luôn chứa được ít nhất 3 mục
// (giống giới hạn BTMaxItemSize của Postgres: khoảng 1/3 page).
const maxItemSize = (storage.BLCKSZ - storage.SizeOfPageHeaderData - 16) / 3 &^ 7

// insertKey là "khoá chèn": key cộng heap TID, để mọi mục có vị trí duy nhất.
type insertKey struct {
	key  types.Datum
	htid common.ItemPointer
}

// compareInsert so khoá chèn với một mục, kể cả heap TID khi key bằng nhau.
// Pivot đã cắt heap TID coi như (key, −∞), nên khoá chèn có key bằng thì lớn hơn.
func (ix *Index) compareInsert(ins insertKey, t IndexTuple) (int, error) {
	c, err := ix.compare(ins.key, t.Key)
	if err != nil || c != 0 {
		return c, err
	}
	var other common.ItemPointer
	switch {
	case t.isPivot() && t.HeapTID == nil:
		return 1, nil
	case t.isPivot():
		other = *t.HeapTID
	default:
		other = t.TID
	}
	return compareTID(ins.htid, other), nil
}

func compareTID(a, b common.ItemPointer) int {
	switch {
	case a.Block != b.Block:
		if a.Block < b.Block {
			return -1
		}
		return 1
	case a.Offset < b.Offset:
		return -1
	case a.Offset > b.Offset:
		return 1
	}
	return 0
}

// childForInsert: trên page giữa, pivot cuối cùng <= khoá chèn -> page con.
func (ix *Index) childForInsert(p storage.Page, op Opaque, ins insertKey) (common.BlockNumber, error) {
	lo, hi := op.FirstDataKey(), p.MaxOffsetNumber()
	for lo < hi {
		mid := lo + (hi-lo+1)/2
		t, err := readIndexTuple(p, mid)
		if err != nil {
			return 0, err
		}
		c, err := ix.compareInsert(ins, t)
		if err != nil {
			return 0, err
		}
		if c >= 0 {
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

// insertOnPage chèn item vào page blk. Tương ứng _bt_insertonpg().
//
//   - page lá: vị trí tìm theo khoá chèn ins
//   - page giữa (afterChild != nil): pivot mới đặt ngay SAU pivot đang trỏ tới
//     page con vừa bị tách
//
// stack là các page phía trên blk (cha, ông, ...), dùng khi phải split lan lên.
func (ix *Index) insertOnPage(blk common.BlockNumber, item []byte, ins insertKey,
	afterChild *common.BlockNumber, stack []common.BlockNumber) error {

	buf, err := ix.pool.ReadBuffer(ix.file, blk)
	if err != nil {
		return err
	}
	p := ix.pool.Page(buf)
	op := readOpaque(p)

	// Tìm ô sẽ chèn vào.
	var off common.OffsetNumber
	if afterChild == nil {
		off, err = ix.leafInsertPos(p, op, ins)
	} else {
		off, err = findDownlink(p, op, *afterChild)
		off++
	}
	if err != nil {
		ix.pool.Release(buf)
		return err
	}

	// Còn chỗ: chèn thẳng, xong.
	if p.FreeSpace() >= len(item) {
		err := p.AddItemAt(item, off)
		if err == nil {
			ix.pool.MarkDirty(buf)
		}
		ix.pool.Release(buf)
		return err
	}

	// Hết chỗ: tách page, rồi đưa pivot lên page cha.
	sep, right, err := ix.split(buf, blk, op, item, off)
	ix.pool.Release(buf)
	if err != nil {
		return err
	}
	if len(stack) == 0 {
		return ix.newRoot(blk, right, sep, op.Level+1) // vừa tách page gốc
	}
	parent := stack[len(stack)-1]
	return ix.insertOnPage(parent, sep, ins, &blk, stack[:len(stack)-1])
}

// leafInsertPos: ô đầu tiên có mục > khoá chèn (tìm nhị phân).
func (ix *Index) leafInsertPos(p storage.Page, op Opaque, ins insertKey) (common.OffsetNumber, error) {
	lo, hi := op.FirstDataKey(), p.MaxOffsetNumber()+1
	for lo < hi {
		mid := lo + (hi-lo)/2
		t, err := readIndexTuple(p, mid)
		if err != nil {
			return 0, err
		}
		c, err := ix.compareInsert(ins, t)
		if err != nil {
			return 0, err
		}
		if c > 0 {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo, nil
}

// findDownlink tìm ô của pivot trỏ tới page con child.
func findDownlink(p storage.Page, op Opaque, child common.BlockNumber) (common.OffsetNumber, error) {
	for off := op.FirstDataKey(); off <= p.MaxOffsetNumber(); off++ {
		t, err := readIndexTuple(p, off)
		if err != nil {
			return 0, err
		}
		if t.Downlink() == child {
			return off, nil
		}
	}
	return 0, fmt.Errorf("btree: page cha không có pivot trỏ tới page %d", child)
}

// split tách page L (đang ghim ở buf) khi chèn item vào ô off.
// Trả về pivot cần đưa lên page cha và số page bên phải R. Tương ứng _bt_split().
//
//	trước:  L [hk?] [m1 m2 ... mn]                         next -> X
//	sau:    L [hk = pivot] [m1 ... mk]        next -> R
//	        R [hk cũ?]     [mk+1 ... mn]      next -> X        X.prev = R
func (ix *Index) split(buf buffer.Buffer, blk common.BlockNumber, op Opaque,
	item []byte, off common.OffsetNumber) ([]byte, common.BlockNumber, error) {

	p := ix.pool.Page(buf)

	// 1. Gom mọi mục (trừ high key) theo thứ tự, chèn item mới vào đúng chỗ.
	var oldHighKey []byte
	if !op.IsRightmost() {
		raw, _ := p.Item(P_HIKEY)
		oldHighKey = append([]byte(nil), raw...)
	}
	var items [][]byte
	for o := op.FirstDataKey(); o <= p.MaxOffsetNumber()+1; o++ {
		if o == off {
			items = append(items, item)
		}
		if o <= p.MaxOffsetNumber() {
			raw, err := p.Item(o)
			if err != nil {
				return nil, 0, err
			}
			items = append(items, append([]byte(nil), raw...))
		}
	}

	// 2. Chọn điểm tách k: L giữ items[:k], R giữ items[k:].
	// Postgres (nbtsplitloc.c): page ngoài cùng bên phải LUÔN dùng fillfactor
	// (lá 90%, page giữa 70%), dù key mới rơi vào đâu; các page khác tách 50/50.
	ff := 50
	if op.IsRightmost() {
		ff = nonleafFillfactor
		if op.IsLeaf() {
			ff = leafFillfactor
		}
	}
	k := chooseSplit(items, ff)
	left, right := items[:k], items[k:]

	// 3. Pivot đưa lên page cha = key đầu tiên của R, trỏ tới R.
	sepKey, sepTID, err := ix.separator(op.IsLeaf(), left[len(left)-1], right[0])
	if err != nil {
		return nil, 0, err
	}
	// Page giữa: mục đầu của R trở thành −∞ (key của nó đã lên làm pivot).
	if !op.IsLeaf() {
		first, _ := tupleFromRaw(right[0])
		right = append([][]byte{makePivot(nil, first.Downlink(), nil)}, right[1:]...)
	}

	// 4. Tạo page R mới ở cuối file.
	rbuf, rblk, err := ix.pool.ExtendBuffer(ix.file)
	if err != nil {
		return nil, 0, err
	}
	defer ix.pool.Release(rbuf)
	rp := ix.pool.Page(rbuf)
	flags := op.Flags &^ BTP_ROOT // sau khi tách, không page nào trong hai page là gốc nữa
	initPage(rp, Opaque{Prev: blk, Next: op.Next, Level: op.Level, Flags: flags})
	if oldHighKey != nil {
		if err := rp.AddItemAt(oldHighKey, 1); err != nil {
			return nil, 0, err
		}
	}
	if err := appendAll(rp, right); err != nil {
		return nil, 0, err
	}

	// 5. Dựng lại L trên một page tạm, rồi chép đè lên L trong pool.
	tmp := storage.NewPage()
	initPage(tmp, Opaque{Prev: op.Prev, Next: rblk, Level: op.Level, Flags: flags})
	if err := tmp.AddItemAt(makePivot(sepKey, P_NONE, sepTID), 1); err != nil { // high key mới
		return nil, 0, err
	}
	if err := appendAll(tmp, left); err != nil {
		return nil, 0, err
	}
	copy(p, tmp)
	ix.pool.MarkDirty(buf)
	ix.pool.MarkDirty(rbuf)

	// 6. Page X bên phải cũ giờ có hàng xóm trái là R.
	if op.Next != P_NONE {
		xbuf, err := ix.pool.ReadBuffer(ix.file, op.Next)
		if err != nil {
			return nil, 0, err
		}
		xp := ix.pool.Page(xbuf)
		xo := readOpaque(xp)
		xo.Prev = rblk
		writeOpaque(xp, xo)
		ix.pool.MarkDirty(xbuf)
		ix.pool.Release(xbuf)
	}

	return makePivot(sepKey, rblk, sepTID), rblk, nil
}

// separator chọn key cho pivot ngăn giữa L và R.
//
//   - page lá, mục cuối của L và mục đầu của R KHÁC key: pivot chỉ cần key, bỏ
//     heap TID (suffix truncation). Mọi mục có key này đều nằm bên phải.
//   - page lá, CÙNG key (dãy key trùng bị chia đôi): pivot phải giữ heap TID của
//     mục đầu R để biết chia ở đâu.
//   - page giữa: lấy nguyên key (và heap TID nếu có) của pivot đầu R.
func (ix *Index) separator(leaf bool, lastLeft, firstRight []byte) ([]byte, *common.ItemPointer, error) {
	r, err := tupleFromRaw(firstRight)
	if err != nil {
		return nil, nil, err
	}
	key := append([]byte(nil), r.Key...)
	if !leaf {
		return key, r.HeapTID, nil
	}
	l, err := tupleFromRaw(lastLeft)
	if err != nil {
		return nil, nil, err
	}
	same, err := ix.sameKey(l.Key, r.Key)
	if err != nil {
		return nil, nil, err
	}
	if same {
		tid := r.TID
		return key, &tid, nil
	}
	return key, nil, nil
}

func (ix *Index) sameKey(a, b []byte) (bool, error) {
	va, _, err := decodeKey(a, ix.keyType)
	if err != nil {
		return false, err
	}
	c, err := ix.compare(va, b)
	return c == 0, err
}

// chooseSplit chọn k sao cho phần trái (k mục + high key) chiếm khoảng ff% tổng
// chỗ. Mỗi mục tốn (độ dài làm tròn 8) + 4 byte cho ô. High key của L xấp xỉ
// bằng mục đầu tiên của R. Bản rút gọn của _bt_findsplitloc().
func chooseSplit(items [][]byte, ff int) int {
	cost := func(b []byte) int { return alignUp8(len(b)) + storage.ItemIdSize }
	total := 0
	for _, it := range items {
		total += cost(it)
	}
	target := total * ff / 100
	best, used := 1, 0
	for k := 1; k < len(items); k++ {
		used += cost(items[k-1])
		if used+cost(items[k]) <= target {
			best = k
		}
	}
	return best
}

// newRoot tạo page gốc mới trỏ tới hai page left, right; cây cao thêm một tầng.
// Tương ứng _bt_newlevel().
//
//	page gốc mới: [−∞ -> left] [sep -> right]
func (ix *Index) newRoot(left, right common.BlockNumber, sep []byte, level uint32) error {
	buf, blk, err := ix.pool.ExtendBuffer(ix.file)
	if err != nil {
		return err
	}
	p := ix.pool.Page(buf)
	initPage(p, Opaque{Level: level, Flags: BTP_ROOT})
	err = appendAll(p, [][]byte{makePivot(nil, left, nil), sep})
	ix.pool.MarkDirty(buf)
	ix.pool.Release(buf)
	if err != nil {
		return err
	}
	return ix.setRoot(blk, level)
}

// newRootLeaf: lần chèn đầu tiên, tạo một page vừa là lá vừa là gốc.
func (ix *Index) newRootLeaf(item []byte) error {
	buf, blk, err := ix.pool.ExtendBuffer(ix.file)
	if err != nil {
		return err
	}
	p := ix.pool.Page(buf)
	initPage(p, Opaque{Level: 0, Flags: BTP_LEAF | BTP_ROOT})
	err = p.AddItemAt(item, 1)
	ix.pool.MarkDirty(buf)
	ix.pool.Release(buf)
	if err != nil {
		return err
	}
	return ix.setRoot(blk, 0)
}

// setRoot ghi page gốc mới vào metapage (page 0).
func (ix *Index) setRoot(root common.BlockNumber, level uint32) error {
	buf, err := ix.pool.ReadBuffer(ix.file, 0)
	if err != nil {
		return err
	}
	writeMeta(ix.pool.Page(buf), Meta{Root: root, Level: level, FastRoot: root})
	ix.pool.MarkDirty(buf)
	ix.pool.Release(buf)
	return nil
}

// Flush ghi mọi page bẩn xuống file và fsync.
func (ix *Index) Flush() error {
	if err := ix.pool.FlushAll(); err != nil {
		return err
	}
	return ix.file.Sync()
}

// --- tiện ích ---

func appendAll(p storage.Page, items [][]byte) error {
	for _, it := range items {
		if err := p.AddItemAt(it, p.MaxOffsetNumber()+1); err != nil {
			return err
		}
	}
	return nil
}

// tupleFromRaw đọc một mục từ byte thô (không qua page).
func tupleFromRaw(raw []byte) (IndexTuple, error) {
	p := storage.NewPage()
	initPage(p, Opaque{})
	if err := p.AddItemAt(raw, 1); err != nil {
		return IndexTuple{}, err
	}
	t, err := readIndexTuple(p, 1)
	if err != nil {
		return t, err
	}
	t.Key = bytes.Clone(t.Key)
	return t, nil
}
