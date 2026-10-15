package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hoangpv/minidb/internal/access/heap"
	"github.com/hoangpv/minidb/internal/buffer"
	"github.com/hoangpv/minidb/internal/types"
)

// pagedump đọc được file do chính minidb ghi ra: chèn đúng 3 dòng như bước 1–3
// của hình "Bên trong một page", rồi in page 0.
func TestDumpFileMinidb(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hoc_thutu")
	desc := heap.TupleDesc{{Name: "id", Type: types.Int4}, {Name: "ten", Type: types.Text}}
	h, err := heap.Open(path, desc, buffer.New(8))
	if err != nil {
		t.Fatal(err)
	}
	rows := []struct {
		id  int32
		ten string
	}{{1, "An"}, {2, "Binh dai hon mot chut"}, {3, "Chi"}}
	for i, r := range rows {
		if _, err := h.Insert([]types.Datum{types.NewInt4(r.id), types.NewText(r.ten)}, uint32(856+i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := dump(&out, path, 0, desc); err != nil {
		t.Fatal(err)
	}
	t.Log("\n" + out.String())

	// Cùng số liệu Postgres cho ra ở bước 3: lower=36, upper=8072, ô 2 ở 8104 dài 50.
	for _, want := range []string{"lower     36", "upper     8072", "8104 NORMAL       50", "3, Chi"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("thiếu %q", want)
		}
	}
}
