// Package safewrite cài đặt 3 cách ghi một file xuống disk, từ nguy hiểm tới an toàn.
//
// Đây là bài tập của chương 1 trong sách. Ba hàm có cùng chữ ký và cùng kết quả
// khi mọi thứ suôn sẻ — khác biệt chỉ lộ ra khi **crash giữa chừng**:
//
//	SaveData1  ghi đè tại chỗ          -> có CỬA SỔ CHẾT, mất cả dữ liệu cũ
//	SaveData2  file tạm + rename       -> mục 1.2, luôn còn một bản nguyên vẹn
//	SaveData3  + fsync thư mục cha     -> mục 1.4 bẫy 1, cái tên cũng bền vững
//
// Xem ghi chú: docs/ghi-chu/03-nen-tang-file-he-thong.md
package safewrite

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
)

// ErrNotImplemented chỉ để code build được trước khi bạn điền thân hàm.
var ErrNotImplemented = errors.New("safewrite: chưa cài đặt")

// SaveData1 ghi đè file tại chỗ. ĐÂY LÀ CÁCH SAI — có ở đây để thấy nó sai ở đâu.
//
// Dòng thời gian khi hàm này chạy:
//
//	OpenFile(... O_TRUNC)  -> file về 0 byte. DỮ LIỆU CŨ CHẾT TẠI ĐÂY.
//	     ! mất điện ở đây = mất sạch, không còn gì
//	Write(data)            -> dữ liệu mới bắt đầu vào
//	     ! mất điện ở đây = file cụt một nửa
//	Sync()                 -> tới đây mới thật sự an toàn
//
// Khoảng từ O_TRUNC tới Sync() là CỬA SỔ CHẾT: dữ liệu cũ đã mất, dữ liệu mới
// chưa xong. Chạy TestMinhHoa_SaveData1KhongAnToan để thấy nó bằng số liệu.
func SaveData1(path string, data []byte) error {
	// O_WRONLY: chỉ ghi.  O_CREATE: chưa có thì tạo.  O_TRUNC: cắt cụt về 0 byte.
	// 0o664 chỉ có tác dụng khi file được TẠO MỚI (và còn bị umask cắt bớt).
	fp, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o664)
	if err != nil {
		return err
	}
	// Đóng file khi hàm kết thúc, dù thành công hay lỗi.
	// Bỏ qua lỗi của Close() được, vì Sync() bên dưới đã ép dữ liệu xuống đĩa.
	defer fp.Close()

	if _, err := fp.Write(data); err != nil {
		return err
	}

	// Chưa gọi Sync() thì dữ liệu mới chỉ nằm trong page cache (RAM).
	return fp.Sync()
}

// SaveData2 ghi ra file tạm rồi rename đè lên. Đây là mục 1.2 của sách.
//
// Các bước:
//  1. tạo tên file tạm: fmt.Sprintf("%s.tmp.%d", path, <số ngẫu nhiên>)
//     ⚠️ phải CÙNG THƯ MỤC với path — vì sao? (gợi ý: rename và filesystem)
//  2. os.OpenFile(tmp, O_WRONLY|O_CREATE|O_EXCL, 0o664)
//     ⚠️ vì sao là O_EXCL chứ không phải O_TRUNC?
//  3. defer: đóng file; và NẾU CÓ LỖI thì os.Remove(tmp) để khỏi để lại rác
//  4. ghi data
//  5. fp.Sync()   <- fsync DỮ LIỆU, phải xong TRƯỚC bước 6
//  6. os.Rename(tmp, path)
//
// ⚠️ Bẫy Go ở bước 3: closure trong defer phải đọc được biến err ở ngoài thì mới
// biết có lỗi hay không. Dùng `fp, err := ...` rồi gán tiếp bằng `=`, đừng tạo
// biến err mới bên trong.
//
// TODO: cài đặt.
func SaveData2(path string, data []byte) error {
	// File tạm phải nằm CÙNG THƯ MỤC với file đích: rename chỉ sửa một dòng
	// trong bảng `tên -> số inode` của thư mục, mà số inode chỉ có ý nghĩa
	// trong phạm vi một filesystem. Khác filesystem thì OS buộc phải copy
	// từng byte rồi xoá -> mất sạch tính atomic.
	tmp := fmt.Sprintf("%s.tmp.%d", path, rand.Uint32())

	// O_EXCL: "nếu file này đã tồn tại thì BÁO LỖI". Không dùng O_TRUNC vì ta
	// đang tạo file hoàn toàn mới, chẳng có gì để cắt cụt. O_EXCL là lưới an
	// toàn phòng khi tên ngẫu nhiên lỡ trùng file tạm của tiến trình khác —
	// thà hỏng ngay còn hơn âm thầm phá dữ liệu của người ta.
	fp, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o664)
	if err != nil {
		return err
	}

	// Dọn rác: nếu hàm kết thúc với lỗi thì xoá file tạm đi, đừng để lại.
	//
	// ⚠️ Closure này đọc biến `err` của HÀM NGOÀI. Nên từ đây trở xuống mọi
	// phép gán lỗi phải dùng `=`, KHÔNG được dùng `:=` — `:=` tạo biến err mới
	// trong phạm vi hẹp hơn, closure sẽ không thấy và file tạm bị bỏ lại.
	defer func() {
		fp.Close()
		if err != nil {
			os.Remove(tmp)
		}
	}()

	if _, err = fp.Write(data); err != nil {
		return err
	}

	// DURABILITY: ép dữ liệu xuống đĩa thật. Phải xong TRƯỚC rename — nếu đảo
	// lại thì sẽ có khoảnh khắc cái tên đã trỏ sang file mới mà dữ liệu còn
	// trong RAM; mất điện lúc đó là tên trỏ vào file rỗng, mà bản cũ đã mất tên.
	if err = fp.Sync(); err != nil {
		return err
	}

	// VISIBILITY: đổi một dòng trong bảng thư mục. Atomic với reader — họ thấy
	// hoặc bản cũ, hoặc bản mới, không bao giờ thấy nửa vời.
	//
	// Viết tách ra thay vì `return os.Rename(...)` để phép gán đi vào đúng biến
	// `err` mà defer ở trên đang theo dõi.
	err = os.Rename(tmp, path)
	return err
}

// SaveData3 giống SaveData2, nhưng fsync thêm THƯ MỤC CHA. Đây là mục 1.4 bẫy 1.
//
// Vì sao cần: thư mục cũng chỉ là một file chứa bảng `tên -> số inode`. Bảng đó
// cũng nằm trong page cache. rename() sửa bảng đó, nhưng nếu không fsync thư mục
// thì sau khi mất điện cái tên vẫn trỏ về inode CŨ — dữ liệu mới nằm đó mà
// không ai tìm thấy.
//
// Thêm vào sau bước rename:
//  7. d, err := os.Open(filepath.Dir(path))   // mở thư mục ở chế độ chỉ đọc
//  8. d.Sync()
//  9. d.Close()
//
// TODO: cài đặt.
func SaveData3(path string, data []byte) error {
	// Toàn bộ phần ghi dữ liệu + rename giống hệt SaveData2.
	// Viết theo kiểu gọi lại để thấy rõ SaveData3 CHỈ THÊM ĐÚNG MỘT BƯỚC.
	if err := SaveData2(path, data); err != nil {
		return err
	}

	// Bước thêm: fsync THƯ MỤC CHA.
	//
	// SaveData2 đã làm hai việc đụng tới bảng `tên -> inode` của thư mục:
	// tạo file tạm (thêm một dòng), và rename (sửa một dòng). Cả hai thay đổi
	// đó mới chỉ nằm trong page cache. Một lần fsync thư mục ở đây bao luôn cả hai.
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer d.Close()

	// Thư mục mở ở chế độ chỉ đọc vẫn fsync được — ta không ghi vào nó, ta chỉ
	// yêu cầu kernel đẩy phần metadata đã thay đổi xuống đĩa.
	return d.Sync()
}
