package safewrite

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// saveFn là chữ ký chung của cả 3 cách ghi.
type saveFn func(path string, data []byte) error

// Cả 3 cách phải cho cùng kết quả khi mọi thứ suôn sẻ.
var impls = []struct {
	name string
	fn   saveFn
}{
	{"SaveData1", SaveData1},
	{"SaveData2", SaveData2},
	{"SaveData3", SaveData3},
}

// safeImpls là những cách KHÔNG ghi đè tại chỗ — chỉ chúng mới an toàn với reader.
var safeImpls = impls[1:]

func TestGhiRoiDocLai(t *testing.T) {
	for _, impl := range impls {
		t.Run(impl.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "db")
			want := []byte("xin chao")

			if err := impl.fn(path, want); err != nil {
				t.Fatalf("ghi: %v", err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("đọc lại: %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("đọc được %q, muốn %q", got, want)
			}
		})
	}
}

func TestGhiDeNhieuLan(t *testing.T) {
	for _, impl := range impls {
		t.Run(impl.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "db")

			// ghi lần lượt dài -> ngắn, để lộ lỗi không cắt cụt file cũ
			for _, want := range [][]byte{
				[]byte("phien ban mot rat dai"),
				[]byte("ngan"),
				[]byte(""),
				[]byte("cuoi cung"),
			} {
				if err := impl.fn(path, want); err != nil {
					t.Fatalf("ghi %q: %v", want, err)
				}
				got, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("đọc lại: %v", err)
				}
				if !bytes.Equal(got, want) {
					t.Fatalf("đọc được %q, muốn %q", got, want)
				}
			}
		})
	}
}

// Ghi xong không được để lại file tạm trong thư mục.
func TestKhongDeLaiFileTam(t *testing.T) {
	for _, impl := range safeImpls {
		t.Run(impl.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "db")

			for i := 0; i < 5; i++ {
				if err := impl.fn(path, []byte("du lieu")); err != nil {
					t.Fatalf("ghi: %v", err)
				}
			}

			ents, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(ents) != 1 || ents[0].Name() != "db" {
				var names []string
				for _, e := range ents {
					names = append(names, e.Name())
				}
				t.Fatalf("thư mục có %v, chỉ nên có [db]", names)
			}
		})
	}
}

// Ghi thất bại thì dữ liệu CŨ phải còn nguyên.
// Ta làm cho nó thất bại bằng cách cấm ghi vào thư mục.
func TestGhiLoiThiDuLieuCuConNguyen(t *testing.T) {
	for _, impl := range safeImpls {
		t.Run(impl.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "db")
			cu := []byte("DU LIEU CU")

			if err := impl.fn(path, cu); err != nil {
				t.Fatalf("ghi lần đầu: %v", err)
			}
			// cấm tạo file mới trong thư mục -> bước tạo file tạm sẽ hỏng
			if err := os.Chmod(dir, 0o500); err != nil {
				t.Skipf("không đổi được quyền thư mục: %v", err)
			}
			t.Cleanup(func() { os.Chmod(dir, 0o700) })

			if err := impl.fn(path, []byte("DU LIEU MOI")); err == nil {
				t.Fatal("muốn ghi thất bại, nhưng lại thành công")
			}

			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("đọc lại: %v", err)
			}
			if !bytes.Equal(got, cu) {
				t.Fatalf("dữ liệu cũ bị hỏng: đọc được %q, muốn %q", got, cu)
			}
		})
	}
}

// ĐÂY LÀ TEST QUAN TRỌNG NHẤT.
//
// Một reader đọc liên tục trong lúc writer ghi liên tục. Reader PHẢI luôn thấy
// một trong hai nội dung HOÀN CHỈNH — không bao giờ được thấy file rỗng hay
// nội dung lai tạp.
//
// SaveData2 và SaveData3 phải qua test này, vì rename là atomic với reader.
func TestReaderKhongBaoGioThayDuLieuRach(t *testing.T) {
	for _, impl := range safeImpls {
		t.Run(impl.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "db")
			a := []byte(strings.Repeat("A", 64*1024))
			b := []byte(strings.Repeat("B", 64*1024))

			if err := impl.fn(path, a); err != nil {
				t.Fatalf("ghi lần đầu: %v", err)
			}

			var dung atomic.Bool
			dung.Store(false)
			var wg sync.WaitGroup

			wg.Add(1)
			go func() { // writer: đổi qua lại giữa a và b
				defer wg.Done()
				for i := 0; !dung.Load(); i++ {
					noidung := a
					if i%2 == 1 {
						noidung = b
					}
					if err := impl.fn(path, noidung); err != nil {
						t.Errorf("ghi: %v", err)
						return
					}
				}
			}()

			var soLanDoc, soLanRach int
			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) {
				got, err := os.ReadFile(path)
				if err != nil {
					t.Errorf("đọc: %v", err)
					break
				}
				soLanDoc++
				if !bytes.Equal(got, a) && !bytes.Equal(got, b) {
					soLanRach++
					if soLanRach == 1 {
						t.Errorf("đọc được dữ liệu RÁCH: dài %d byte (muốn %d), "+
							"bắt đầu bằng %q", len(got), len(a), truncate(got, 16))
					}
				}
			}
			dung.Store(true)
			wg.Wait()

			t.Logf("đã đọc %d lần, rách %d lần", soLanDoc, soLanRach)
			if soLanDoc < 10 {
				t.Fatalf("chỉ đọc được %d lần, test không có ý nghĩa", soLanDoc)
			}
		})
	}
}

// Test MINH HOẠ: SaveData1 ghi đè tại chỗ nên reader CÓ THỂ thấy dữ liệu rách.
// Test này không fail dù kết quả thế nào — nó chỉ in ra con số để bạn thấy.
func TestMinhHoa_SaveData1KhongAnToan(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	a := []byte(strings.Repeat("A", 256*1024))
	b := []byte(strings.Repeat("B", 256*1024))

	if err := SaveData1(path, a); err != nil {
		t.Fatalf("ghi lần đầu: %v", err)
	}

	var dung atomic.Bool
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; !dung.Load(); i++ {
			noidung := a
			if i%2 == 1 {
				noidung = b
			}
			_ = SaveData1(path, noidung)
		}
	}()

	var soLanDoc, soLanRach, soLanRong int
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := os.ReadFile(path)
		soLanDoc++
		switch {
		case len(got) == 0:
			soLanRong++
		case !bytes.Equal(got, a) && !bytes.Equal(got, b):
			soLanRach++
		}
	}
	dung.Store(true)
	wg.Wait()

	t.Logf("SaveData1: đọc %d lần -> %d lần thấy file RỖNG, %d lần thấy dữ liệu RÁCH",
		soLanDoc, soLanRong, soLanRach)
	if soLanRong+soLanRach == 0 {
		t.Log("(lần này không bắt được — chạy lại hoặc tăng kích thước dữ liệu)")
	} else {
		t.Log("=> Đây chính là CỬA SỔ CHẾT của việc ghi đè tại chỗ.")
	}
}

func truncate(b []byte, n int) []byte {
	if len(b) > n {
		return b[:n]
	}
	return b
}
