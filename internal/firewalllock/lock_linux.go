//go:build linux

package firewalllock

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"strings"
)

// Acquire takes the fixed root-owned, nonblocking process lock. Closing the file
// releases it. No policy, service or application state is read or changed.
func Acquire() (*os.File, error) { return acquireAt("/run/celikpanel-firewall-boot") }

func acquireAt(directory string) (*os.File, error) {
	if os.Geteuid() != 0 {
		return nil, errors.New("root is required for firewall exclusion")
	}
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory || directory == "/" {
		return nil, errors.New("invalid firewall lock directory")
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	defer func() { unix.Close(fd) }()
	parts := strings.Split(strings.TrimPrefix(directory, "/"), "/")
	for index, name := range parts {
		if err = trustedDirectory(fd); err != nil {
			return nil, err
		}
		if index == len(parts)-1 {
			if err = unix.Mkdirat(fd, name, 0755); err != nil && !errors.Is(err, unix.EEXIST) {
				return nil, err
			}
		}
		next, err := unix.Openat(fd, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			return nil, err
		}
		unix.Close(fd)
		fd = next
	}
	if err = trustedDirectory(fd); err != nil {
		return nil, err
	}
	filefd, err := unix.Openat(fd, "restore.lock", unix.O_RDWR|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(filefd), "native firewall exclusion")
	var st unix.Stat_t
	// Agent runs as root:celikpanel; the independent boot consumer as root:root.
	// 0600 makes group ownership irrelevant; neither path normalizes owner state.
	if unix.Fstat(filefd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&07777 != 0600 || st.Uid != 0 || st.Nlink != 1 {
		file.Close()
		return nil, errors.New("unsafe firewall lock metadata")
	}
	if err = unix.Flock(filefd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		file.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, ErrBusy
		}
		return nil, err
	}
	return file, nil
}

func trustedDirectory(fd int) error {
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Uid != 0 || st.Mode&0022 != 0 {
		return errors.New("unsafe firewall lock directory")
	}
	return nil
}
