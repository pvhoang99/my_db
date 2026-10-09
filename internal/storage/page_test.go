package storage

import (
	"bytes"
	"errors"
	"testing"
)

func TestPageInsertAndGet(t *testing.T) {
	p := NewPage()
	if p.NumSlots() != 0 {
		t.Fatalf("page mới phải rỗng, có %d slot", p.NumSlots())
	}

	tuples := [][]byte{[]byte("hello"), []byte("postgres"), []byte("")}
	for i, tp := range tuples {
		slot, err := p.Insert(tp)
		if err != nil {
			t.Fatalf("Insert(%d): %v", i, err)
		}
		if slot != i {
			t.Fatalf("Insert(%d) trả slot %d, muốn %d", i, slot, i)
		}
	}

	for i, want := range tuples {
		got, err := p.Get(i)
		if err != nil {
			t.Fatalf("Get(%d): %v", i, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("Get(%d) = %q, muốn %q", i, got, want)
		}
	}
}

func TestPageDelete(t *testing.T) {
	p := NewPage()
	s0, _ := p.Insert([]byte("a"))
	s1, _ := p.Insert([]byte("b"))

	if err := p.Delete(s0); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := p.Get(s0); !errors.Is(err, ErrSlotDead) {
		t.Fatalf("Get slot đã xoá trả %v, muốn ErrSlotDead", err)
	}
	if err := p.Delete(s0); !errors.Is(err, ErrSlotDead) {
		t.Fatalf("Delete hai lần trả %v, muốn ErrSlotDead", err)
	}
	// Slot number phải ổn định sau khi xoá — TID không được dịch chuyển.
	if got, err := p.Get(s1); err != nil || !bytes.Equal(got, []byte("b")) {
		t.Fatalf("Get(%d) = %q, %v", s1, got, err)
	}
	if p.NumSlots() != 2 {
		t.Fatalf("NumSlots = %d, muốn 2 (slot dead vẫn chiếm chỗ)", p.NumSlots())
	}
}

func TestPageFillUntilFull(t *testing.T) {
	p := NewPage()
	tuple := bytes.Repeat([]byte("x"), 100)

	n := 0
	for {
		if _, err := p.Insert(tuple); err != nil {
			if !errors.Is(err, ErrPageFull) {
				t.Fatalf("Insert thứ %d lỗi lạ: %v", n, err)
			}
			break
		}
		n++
		if n > PageSize {
			t.Fatal("page không bao giờ đầy")
		}
	}

	// Mỗi tuple tốn 100 byte dữ liệu + 4 byte ItemId.
	want := (PageSize - pageHeaderSize) / (100 + itemIDSize)
	if n != want {
		t.Fatalf("nhét được %d tuple, muốn %d", n, want)
	}
	if p.FreeSpace() >= 100+itemIDSize {
		t.Fatalf("page báo đầy nhưng còn %d byte trống", p.FreeSpace())
	}
}

func TestPageTupleTooBig(t *testing.T) {
	p := NewPage()
	if _, err := p.Insert(bytes.Repeat([]byte("x"), PageSize)); !errors.Is(err, ErrTupleTooBig) {
		t.Fatalf("Insert tuple quá khổ trả %v, muốn ErrTupleTooBig", err)
	}
}

func TestPageInvalidSlot(t *testing.T) {
	p := NewPage()
	if _, err := p.Get(0); !errors.Is(err, ErrSlotInvalid) {
		t.Fatalf("Get(0) trên page rỗng trả %v, muốn ErrSlotInvalid", err)
	}
	if err := p.Delete(-1); !errors.Is(err, ErrSlotInvalid) {
		t.Fatalf("Delete(-1) trả %v, muốn ErrSlotInvalid", err)
	}
}

func TestPageLSN(t *testing.T) {
	p := NewPage()
	p.SetLSN(0xDEADBEEF)
	if p.LSN() != 0xDEADBEEF {
		t.Fatalf("LSN = %x", p.LSN())
	}
	p.Init()
	if p.LSN() != 0 {
		t.Fatalf("Init phải reset LSN, còn %x", p.LSN())
	}
}
