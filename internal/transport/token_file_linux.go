//go:build linux

package transport

import (
	"os"

	"golang.org/x/sys/unix"
)

// Open without waiting for a FIFO writer; ReadToken validates the opened FD
// before reading. Existing symlinks to regular token files remain supported.
// FIFO yazıcısını beklemeden aç; ReadToken okumadan önce açılan FD'yi doğrular.
// Normal token dosyasına yönelen mevcut sembolik bağlantılar desteklenir.
func openTokenFile(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(fd), path), nil
}
