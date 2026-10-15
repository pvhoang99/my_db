// Package executor chạy kế hoạch truy vấn. Ở bước M2.4 mới có các kiểu QUÉT BẢNG,
// mỗi kiểu đúng một dòng bạn thấy trong EXPLAIN:
//
//	Seq Scan          đọc mọi page heap, lọc từng dòng        (seqscan)
//	Index Scan        quét index theo khoảng, mỗi ctid lấy dòng ngay   (indexscan)
//	Bitmap Heap Scan  quét index, GOM ctid, rồi đọc heap theo thứ tự page (bitmapscan)
//
// Tương ứng trong Postgres:
//
//	src/backend/executor/nodeSeqscan.c
//	src/backend/executor/nodeIndexscan.c
//	src/backend/executor/nodeBitmapIndexscan.c, nodeBitmapHeapscan.c
//
// Mọi kiểu quét có cùng cách dùng (kiểu "volcano", sẽ hoàn chỉnh ở M6.1):
//
//	s := NewSeqScan(...)
//	defer s.Close()
//	for s.Next() { dùng s.Values() }
//	if err := s.Err(); err != nil { ... }
//
// Muốn đếm "Buffers:" như EXPLAIN thì lấy buffer.Pool.Stats() trước và sau.
package executor

import (
	"sort"

	"github.com/hoangpv/minidb/internal/access/btree"
	"github.com/hoangpv/minidb/internal/access/heap"
	"github.com/hoangpv/minidb/internal/common"
	"github.com/hoangpv/minidb/internal/types"
)

// Scan là một kiểu quét bảng.
type Scan interface {
	Next() bool            // tiến tới dòng kế tiếp; false = hết hoặc lỗi
	Values() []types.Datum // giá trị các cột của dòng hiện tại
	Err() error            // lỗi làm dừng quét, nếu có
	Close()                // bỏ ghim mọi page đang giữ
}

// ---------------------------------------------------------------------------
// Seq Scan

// SeqScan đọc mọi dòng của bảng, chỉ giữ dòng thoả Filter.
//
//	EXPLAIN:  Seq Scan on t
//	            Filter: (...)
//	            Rows Removed by Filter: N
type SeqScan struct {
	s      *heap.Scanner
	filter func([]types.Datum) bool // nil = lấy mọi dòng

	RowsRemovedByFilter int // số dòng đọc lên rồi bỏ đi
}

func NewSeqScan(h *heap.Heap, filter func([]types.Datum) bool) (*SeqScan, error) {
	s, err := h.Scan()
	if err != nil {
		return nil, err
	}
	return &SeqScan{s: s, filter: filter}, nil
}

func (q *SeqScan) Next() bool {
	for q.s.Next() {
		if q.filter == nil || q.filter(q.s.Values()) {
			return true
		}
		q.RowsRemovedByFilter++
	}
	return false
}
func (q *SeqScan) Values() []types.Datum { return q.s.Values() }
func (q *SeqScan) Err() error            { return q.s.Err() }
func (q *SeqScan) Close()                { q.s.Close() }

// ---------------------------------------------------------------------------
// Index Scan

// IndexScan quét index theo khoảng [lo, hi]; với MỖI ctid lấy được, sang heap
// lấy dòng NGAY. Dòng ra đúng thứ tự của index (hợp với ORDER BY ... LIMIT).
//
//	EXPLAIN:  Index Scan using idx on t
//	            Index Cond: ((c >= lo) AND (c <= hi))
//
// ctid kế tiếp cùng page heap thì giữ page đó, không mở lại (heap.Fetcher).
// Thứ tự index khác thứ tự heap thì cứ nhảy qua lại giữa các page: đắt.
type IndexScan struct {
	it     *btree.RangeIter
	f      *heap.Fetcher
	values []types.Datum
	err    error
}

func NewIndexScan(h *heap.Heap, ix *btree.Index, lo, hi *types.Datum) (*IndexScan, error) {
	it, err := ix.RangeScan(lo, hi)
	if err != nil {
		return nil, err
	}
	return &IndexScan{it: it, f: h.NewFetcher()}, nil
}

func (q *IndexScan) Next() bool {
	if !q.it.Next() {
		q.err = q.it.Err()
		return false
	}
	q.values, q.err = q.f.Fetch(q.it.TID())
	return q.err == nil
}
func (q *IndexScan) Values() []types.Datum { return q.values }
func (q *IndexScan) Err() error            { return q.err }
func (q *IndexScan) Close()                { q.it.Close(); q.f.Close() }

// HeapPagesOpened: số lần phải mở một page heap.
func (q *IndexScan) HeapPagesOpened() int { return q.f.PagesOpened }

// ---------------------------------------------------------------------------
// Bitmap Heap Scan

// BitmapHeapScan chạy hai bước, đúng hai dòng lồng nhau trong EXPLAIN:
//
//	Bitmap Heap Scan on t                    <- ② đọc heap theo thứ tự page
//	  Heap Blocks: exact=N
//	  ->  Bitmap Index Scan on idx           <- ① quét index, chỉ ghi lại ctid
//	        Index Cond: (...)
//
// ① gom mọi ctid vào "bitmap": page nào có những ô nào.
// ② đi các page theo thứ tự tăng dần; mỗi page mở ĐÚNG MỘT LẦN, lấy hết các ô của nó.
// Dòng ra theo thứ tự vật lý, MẤT thứ tự của index.
type BitmapHeapScan struct {
	f      *heap.Fetcher
	tids   []common.ItemPointer // ctid đã sắp theo (page, ô)
	pos    int
	values []types.Datum
	err    error

	HeapBlocks int // số page heap khác nhau trong bitmap: "Heap Blocks: exact=N"
}

func NewBitmapHeapScan(h *heap.Heap, ix *btree.Index, lo, hi *types.Datum) (*BitmapHeapScan, error) {
	// ① Bitmap Index Scan: chỉ quét index, chưa đụng tới heap.
	it, err := ix.RangeScan(lo, hi)
	if err != nil {
		return nil, err
	}
	bitmap := map[common.BlockNumber][]common.OffsetNumber{}
	for it.Next() {
		t := it.TID()
		bitmap[t.Block] = append(bitmap[t.Block], t.Offset)
	}
	it.Close()
	if err := it.Err(); err != nil {
		return nil, err
	}

	// Sắp page tăng dần, ô tăng dần trong mỗi page.
	blocks := make([]common.BlockNumber, 0, len(bitmap))
	for b := range bitmap {
		blocks = append(blocks, b)
	}
	sort.Slice(blocks, func(i, j int) bool { return blocks[i] < blocks[j] })
	var tids []common.ItemPointer
	for _, b := range blocks {
		offs := bitmap[b]
		sort.Slice(offs, func(i, j int) bool { return offs[i] < offs[j] })
		for _, o := range offs {
			tids = append(tids, common.ItemPointer{Block: b, Offset: o})
		}
	}
	return &BitmapHeapScan{f: h.NewFetcher(), tids: tids, HeapBlocks: len(blocks)}, nil
}

// Next — ② đọc heap: ctid đã sắp theo page nên Fetcher mở mỗi page đúng một lần.
func (q *BitmapHeapScan) Next() bool {
	if q.pos >= len(q.tids) {
		return false
	}
	q.values, q.err = q.f.Fetch(q.tids[q.pos])
	q.pos++
	return q.err == nil
}
func (q *BitmapHeapScan) Values() []types.Datum { return q.values }
func (q *BitmapHeapScan) Err() error            { return q.err }
func (q *BitmapHeapScan) Close()                { q.f.Close() }
