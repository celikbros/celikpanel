//go:build !linux

package transport

import (
	"fmt"
	"os"
)

// Development platforms check the target before opening; Linux uses a
// nonblocking FD and validates the actual opened file in ReadToken.
// Geliştirme platformları açmadan önce hedefi kontrol eder; Linux engellemesiz
// FD kullanır ve ReadToken gerçekten açılan dosyayı doğrular.
func openTokenFile(path string) (*os.File, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("token file is not a regular file")
	}
	return os.Open(path)
}
