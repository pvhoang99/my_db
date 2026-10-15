package storage

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/hoangpv/minidb/internal/common"
)

// Page vừa khởi tạo phải có header giống hệt page heap rỗng của Postgres:
// SELECT * FROM page_header(...) trên một page mới cho lower=24 upper=8192
// special=8192 pagesize=8192 version=4.
func TestPageInitGiongPostgres(t *testing.T) {
	p := NewPage()
	PageInit(p, 0)

	if p.Lower() != 24 || p.Upper() != 8192 || p.Special() != 8192 {
		t.Fatalf("lower=%d upper=%d special=%d, muốn 24/8192/8192", p.Lower(), p.Upper(), p.Special())
	}
	if p.PageSize() != 8192 || p.LayoutVersion() != 4 {
		t.Fatalf("pagesize=%d version=%d", p.PageSize(), p.LayoutVersion())
	}
	if p.MaxOffsetNumber() != 0 {
		t.Fatalf("page mới mà có %d ô", p.MaxOffsetNumber())
	}
	// Page B-tree chừa 16 byte special ở cuối.
	PageInit(p, 16)
	if p.Special() != 8176 || p.Upper() != 8176 {
		t.Fatalf("B-tree page: special=%d upper=%d, muốn 8176", p.Special(), p.Upper())
	}
}

// Dữ liệu mọc từ cuối page lên, line pointer mọc từ đầu xuống.
func TestAddItemMocNguoc(t *testing.T) {
	p := NewPage()
	PageInit(p, 0)

	a := []byte("tuple-mot")        // 9 byte -> chiếm 16 (căn 8)
	b := []byte("tuple-thu-hai!!!") // 16 byte -> chiếm 16

	offA, err := p.AddItem(a)
	if err != nil {
		t.Fatal(err)
	}
	offB, err := p.AddItem(b)
	if err != nil {
		t.Fatal(err)
	}
	if offA != 1 || offB != 2 {
		t.Fatalf("số ô = %d, %d; muốn 1, 2", offA, offB)
	}

	idA, _ := p.ItemId(1)
	idB, _ := p.ItemId(2)
	if idA.Off() != 8192-16 || idA.Len() != 9 || idA.Flags() != LP_NORMAL {
		t.Fatalf("ô 1: off=%d len=%d flags=%d", idA.Off(), idA.Len(), idA.Flags())
	}
	if idB.Off() != 8192-32 || idB.Len() != 16 {
		t.Fatalf("ô 2: off=%d len=%d", idB.Off(), idB.Len())
	}
	if p.Lower() != 24+2*4 || p.Upper() != 8192-32 {
		t.Fatalf("lower=%d upper=%d", p.Lower(), p.Upper())
	}

	got, _ := p.Item(1)
	if !bytes.Equal(got, a) {
		t.Fatalf("đọc lại ô 1 = %q", got)
	}
	if err := p.Verify(); err != nil {
		t.Fatal(err)
	}
}

// Nhồi tới khi đầy: không ghi đè lên nhau, và báo ErrPageFull đúng lúc.
func TestPageDay(t *testing.T) {
	p := NewPage()
	PageInit(p, 0)
	item := bytes.Repeat([]byte("x"), 100) // chiếm 104 byte + 4 byte line pointer

	n := 0
	for {
		if _, err := p.AddItem(item); err != nil {
			if !errors.Is(err, ErrPageFull) {
				t.Fatal(err)
			}
			break
		}
		n++
	}
	// (8192 - 24) / (104 + 4) = 75 dòng
	if n != 75 {
		t.Fatalf("chèn được %d dòng, muốn 75", n)
	}
	if p.FreeSpace() >= 104 {
		t.Fatalf("báo đầy mà còn %d byte", p.FreeSpace())
	}
	for off := common.OffsetNumber(1); off <= p.MaxOffsetNumber(); off++ {
		got, _ := p.Item(off)
		if !bytes.Equal(got, item) {
			t.Fatalf("ô %d bị hỏng", off)
		}
	}
}

func TestItemIdBitfield(t *testing.T) {
	// Ô 3 trên page 0 bảng users: lp_off=8096 flags=1 len=46.
	id := makeItemId(8096, LP_NORMAL, 46)
	if id.Off() != 8096 || id.Flags() != 1 || id.Len() != 46 {
		t.Fatalf("off=%d flags=%d len=%d", id.Off(), id.Flags(), id.Len())
	}
}

// Đọc một page heap THẬT lấy từ PostgreSQL 18 (bảng users, page 0, sau khi đã
// UPDATE dòng id=1). Cách lấy:
//
//	psql -Atc "select encode(get_raw_page('users',0),'hex')" | xxd -r -p > testdata/pg18_users_page0.bin
//
// Số liệu so sánh lấy từ page_header() và heap_page_items() của pageinspect.
func TestDocPagePostgresThat(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "pg18_users_page0.bin"))
	if err != nil {
		t.Fatal(err)
	}
	p := Page(raw)
	if err := p.Verify(); err != nil {
		t.Fatal(err)
	}
	if p.Lower() != 612 || p.Upper() != 664 || p.Special() != 8192 {
		t.Fatalf("lower=%d upper=%d special=%d, pageinspect nói 612/664/8192", p.Lower(), p.Upper(), p.Special())
	}
	if p.MaxOffsetNumber() != 147 {
		t.Fatalf("có %d ô, pageinspect nói 147", p.MaxOffsetNumber())
	}

	cases := []struct {
		off              common.OffsetNumber
		lpOff, flags, ln int
	}{
		// Trước UPDATE, ô 2 nằm ở 8096 và ô 3 ở 8048. Khi dọn tuple chết của ô 1,
		// Postgres dồn page lại (PageRepairFragmentation) nên các tuple dịch về
		// cuối page 48 byte. Ô 1 vẫn giữ lại ở trạng thái DEAD vì index còn trỏ vào.
		{1, 0, LP_DEAD, 0},        // dòng id=1 cũ: đã chết sau UPDATE
		{2, 8144, LP_NORMAL, 46},  // id=2
		{3, 8096, LP_NORMAL, 46},  // id=3
		{147, 664, LP_NORMAL, 46}, // id=1 bản mới sau UPDATE
	}
	for _, c := range cases {
		id, err := p.ItemId(c.off)
		if err != nil {
			t.Fatal(err)
		}
		if id.Off() != c.lpOff || id.Flags() != c.flags || id.Len() != c.ln {
			t.Errorf("ô %d: off=%d flags=%d len=%d, muốn %d/%d/%d",
				c.off, id.Off(), id.Flags(), id.Len(), c.lpOff, c.flags, c.ln)
		}
	}
}
