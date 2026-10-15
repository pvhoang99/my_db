// Lệnh pagedump in một page ra màn hình, giống pageinspect của Postgres, nhưng
// đọc THẲNG file trên đĩa. Đọc được cả file của minidb lẫn file bảng của Postgres
// (vì minidb dùng cùng bố cục byte).
//
// Cách dùng:
//
//	go run ./cmd/pagedump [-block N] [-cols int4,text,text,text] FILE
//
//	-block N   page số mấy (mặc định 0)
//	-cols ...  kiểu các cột của bảng, theo thứ tự. Có thì giải mã ra giá trị;
//	           không có thì chỉ in header của từng dòng.
//
// So với pageinspect:
//
//	phần "page header"  <->  SELECT * FROM page_header(get_raw_page('t', N));
//	bảng các ô          <->  SELECT * FROM heap_page_items(get_raw_page('t', N));
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/hoangpv/minidb/internal/access/heap"
	"github.com/hoangpv/minidb/internal/common"
	"github.com/hoangpv/minidb/internal/storage"
	"github.com/hoangpv/minidb/internal/types"
)

func main() {
	block := flag.Uint("block", 0, "page số mấy")
	cols := flag.String("cols", "", "kiểu các cột, ví dụ int4,text,text,text")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "dùng: pagedump [-block N] [-cols int4,text,...] FILE")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}

	desc, err := parseCols(*cols)
	if err != nil {
		fail(err)
	}
	if err := dump(os.Stdout, flag.Arg(0), common.BlockNumber(*block), desc); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "pagedump:", err)
	os.Exit(1)
}

// parseCols biến "int4,text" thành TupleDesc. Chuỗi rỗng -> nil (không giải mã cột).
func parseCols(s string) (heap.TupleDesc, error) {
	if s == "" {
		return nil, nil
	}
	byName := map[string]types.Type{"int4": types.Int4, "int8": types.Int8, "text": types.Text, "bool": types.Bool}
	var desc heap.TupleDesc
	for i, name := range strings.Split(s, ",") {
		t, ok := byName[strings.TrimSpace(name)]
		if !ok {
			return nil, fmt.Errorf("kiểu %q chưa hỗ trợ (có: int4, int8, text, bool)", name)
		}
		desc = append(desc, heap.Column{Name: fmt.Sprintf("c%d", i+1), Type: t})
	}
	return desc, nil
}

// flagName đổi lp_flags thành chữ, giống tài liệu Postgres.
func flagName(f int) string {
	switch f {
	case storage.LP_UNUSED:
		return "UNUSED"
	case storage.LP_NORMAL:
		return "NORMAL"
	case storage.LP_REDIRECT:
		return "REDIRECT"
	case storage.LP_DEAD:
		return "DEAD"
	}
	return "?"
}

func dump(w io.Writer, path string, blk common.BlockNumber, desc heap.TupleDesc) error {
	// 1. Mở file, đọc đúng page cần xem.
	f, err := storage.OpenFile(path)
	if err != nil {
		return err
	}
	defer f.Close()
	n, err := f.NumBlocks()
	if err != nil {
		return err
	}
	if blk >= n {
		return fmt.Errorf("file chỉ có %d page (0..%d)", n, n-1)
	}
	p := storage.NewPage()
	if err := f.ReadPage(blk, p); err != nil {
		return err
	}
	fmt.Fprintf(w, "file %s: %d page, đang xem page %d\n\n", path, n, blk)
	if p.IsNew() {
		fmt.Fprintln(w, "page toàn số 0 (chưa khởi tạo)")
		return nil
	}
	if err := p.Verify(); err != nil {
		return err
	}

	// 2. Page header — cùng tên cột với page_header() của pageinspect.
	// LSN in kiểu Postgres: nửa cao / nửa thấp, hệ 16.
	lsn := p.LSN()
	fmt.Fprintln(w, "PAGE HEADER (24 byte)")
	fmt.Fprintf(w, "  lsn       %X/%X\n", lsn>>32, uint32(lsn))
	fmt.Fprintf(w, "  checksum  %d\n", p.Checksum())
	fmt.Fprintf(w, "  flags     %d\n", p.Flags())
	fmt.Fprintf(w, "  lower     %d\n", p.Lower())
	fmt.Fprintf(w, "  upper     %d\n", p.Upper())
	fmt.Fprintf(w, "  special   %d\n", p.Special())
	fmt.Fprintf(w, "  pagesize  %d\n", p.PageSize())
	fmt.Fprintf(w, "  version   %d\n", p.LayoutVersion())
	fmt.Fprintf(w, "  prune_xid %d\n", p.PruneXid())
	fmt.Fprintf(w, "  => %d ô, trống %d byte (từ byte %d tới %d)\n\n",
		p.MaxOffsetNumber(), int(p.Upper())-int(p.Lower()), p.Lower(), p.Upper())

	// 3. Từng ô và tuple của nó — cùng tên cột với heap_page_items().
	fmt.Fprintln(w, "CÁC Ô")
	fmt.Fprintf(w, "  %4s %7s %-8s %6s | %7s %7s %-9s %10s %6s | %s\n",
		"lp", "lp_off", "lp_flags", "lp_len", "t_xmin", "t_xmax", "t_ctid", "t_infomask", "t_hoff", "giá trị")
	for off := common.FirstOffsetNumber; off <= p.MaxOffsetNumber(); off++ {
		id, err := p.ItemId(off)
		if err != nil {
			return err
		}
		fmt.Fprintf(w, "  %4d %7d %-8s %6d", off, id.Off(), flagName(id.Flags()), id.Len())

		// Ô không NORMAL thì không có tuple để đọc.
		tup, err := p.Item(off)
		if err != nil {
			return err
		}
		if tup == nil {
			fmt.Fprintln(w, " |")
			continue
		}

		// Có -cols thì giải mã cả giá trị; không thì chỉ đọc tuple header.
		var h heap.Header
		var vals []types.Datum
		if desc != nil {
			h, vals, err = heap.DeformTuple(desc, tup)
		} else {
			h, err = heap.ReadHeader(tup)
		}
		if err != nil {
			fmt.Fprintf(w, " | lỗi: %v\n", err)
			continue
		}
		fmt.Fprintf(w, " | %7d %7d %-9s %10d %6d |", h.Xmin, h.Xmax, h.Ctid, h.Infomask, h.Hoff)
		for i, v := range vals {
			sep := " "
			if i > 0 {
				sep = ", "
			}
			fmt.Fprint(w, sep+v.String())
		}
		fmt.Fprintln(w)
	}
	return nil
}
