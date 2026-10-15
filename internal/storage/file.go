package storage

// File là một file trên đĩa được chia thành các page 8KB liền nhau.
//
//	byte:  0        8192      16384     24576
//	       | page 0 | page 1  | page 2  | ...
//
// Page n nằm ở byte n*BLCKSZ. Không có mục lục, không có header file: biết số
// page là tính ra vị trí ngay. Ví dụ page 73 nằm ở byte 73 * 8192 = 598.016.
//
// File không biết bên trong page có gì: với nó, page chỉ là 8192 byte.
// Hiểu nội dung page là việc của page.go.
//
// Tương ứng trong Postgres: src/backend/storage/smgr/md.c ("magnetic disk"
// storage manager). Postgres còn chẻ một bảng thành nhiều file 1GB
// (19863, 19863.1, 19863.2, ...); minidb dùng một file duy nhất.
//
// Ghi chú về bền vững (xem ghi chú 03 và 06):
//   - WritePage chỉ đưa dữ liệu vào page cache của OS, CHƯA xuống đĩa.
//   - Muốn chắc chắn phải gọi Sync. Postgres không fsync sau mỗi lần ghi page;
//     nó dựa vào WAL (M3) và fsync gom lại lúc checkpoint.
//   - Tạo file mới thì phải fsync cả thư mục cha (bẫy của mục 1.4).
//   - Tầng này KHÔNG atomic: mất điện giữa lúc ghi một page 8KB có thể để lại
//     page ghi được một nửa (torn page). Postgres cũng vậy; an toàn đến từ WAL
//     và full-page write ở tầng trên (M3).

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/hoangpv/minidb/internal/common"
)

type File struct {
	f    *os.File // file thật của hệ điều hành, mở chế độ đọc + ghi
	path string   // đường dẫn file, chỉ dùng để in thông báo lỗi cho dễ đọc
}

// OpenFile mở file, tạo mới nếu chưa có. Khi tạo mới thì fsync thư mục cha để
// chắc chắn cái tên file cũng bền vững.
func OpenFile(path string) (*File, error) {
	// Xem file đã có chưa, để biết lần này có phải tạo mới không.
	_, statErr := os.Stat(path)
	created := os.IsNotExist(statErr)

	// O_RDWR: đọc và ghi. O_CREATE: chưa có thì tạo. 0o600: chỉ chủ file đọc/ghi được.
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}

	// Tạo file mới = thêm một dòng "tên -> inode" vào thư mục. Dòng đó cũng chỉ
	// nằm trong RAM cho tới khi fsync thư mục (giống SaveData3 ở chương 1).
	if created {
		if err := syncDir(filepath.Dir(path)); err != nil {
			f.Close()
			return nil, err
		}
	}
	return &File{f: f, path: path}, nil
}

// NumBlocks là số page trong file = kích thước file / 8192.
// Ví dụ file 6.029.312 byte -> 736 page.
// Tương ứng: mdnblocks(). Postgres cũng tính đúng như vậy.
func (fl *File) NumBlocks() (common.BlockNumber, error) {
	st, err := fl.f.Stat()
	if err != nil {
		return 0, err
	}
	// File dài lẻ (không chia hết cho 8192) nghĩa là có page ghi dở, thường do
	// crash lúc ExtendPage. Hiện báo lỗi; M3.2 sẽ xử lý mềm như Postgres
	// (bỏ qua phần lẻ, để WAL dựng lại page đó).
	if st.Size()%BLCKSZ != 0 {
		return 0, fmt.Errorf("storage: %s dài %d byte, không chia hết cho %d", fl.path, st.Size(), BLCKSZ)
	}
	return common.BlockNumber(st.Size() / BLCKSZ), nil
}

// ReadPage đọc page blk vào p (p phải dài đúng BLCKSZ).
// Tương ứng: mdreadv().
func (fl *File) ReadPage(blk common.BlockNumber, p Page) error {
	// ReadAt = lệnh pread của OS: đọc len(p) byte bắt đầu từ vị trí chỉ định,
	// không cần nhảy con trỏ file trước.
	n, err := fl.f.ReadAt(p, int64(blk)*BLCKSZ)
	if err != nil {
		return fmt.Errorf("storage: đọc page %d: %w (được %d byte)", blk, err, n)
	}
	return nil
}

// WritePage ghi đè page blk đã có. Tương ứng: mdwritev().
// Page mới (chưa có trong file) phải dùng ExtendPage.
func (fl *File) WritePage(blk common.BlockNumber, p Page) error {
	// Chỉ cho ghi đè page đã tồn tại, để không vô tình tạo lỗ hổng giữa file.
	n, err := fl.NumBlocks()
	if err != nil {
		return err
	}
	if blk >= n {
		return fmt.Errorf("storage: page %d chưa tồn tại (file có %d page), dùng ExtendPage", blk, n)
	}
	// WriteAt = pwrite: ghi 8192 byte vào đúng vị trí của page blk.
	// Ghi xong mới chỉ nằm trong page cache, chưa chắc đã xuống đĩa.
	_, err = fl.f.WriteAt(p, int64(blk)*BLCKSZ)
	return err
}

// ExtendPage ghi p vào cuối file thành page mới, trả về số page của nó.
// Tương ứng: mdextend().
func (fl *File) ExtendPage(p Page) (common.BlockNumber, error) {
	// File đang có n page (0..n-1), nên page mới là page số n.
	blk, err := fl.NumBlocks()
	if err != nil {
		return 0, err
	}
	// Ghi vào ngay sau page cuối, file dài thêm 8192 byte.
	if _, err := fl.f.WriteAt(p, int64(blk)*BLCKSZ); err != nil {
		return 0, err
	}
	return blk, nil
}

// Sync ép mọi page đã ghi xuống đĩa thật (fsync). Tương ứng: mdimmedsync().
func (fl *File) Sync() error { return fl.f.Sync() }

// Close đóng file. Không tự Sync: ai cần bền vững thì gọi Sync trước.
func (fl *File) Close() error { return fl.f.Close() }

// syncDir fsync một thư mục. Mở thư mục ở chế độ chỉ đọc vẫn fsync được.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
