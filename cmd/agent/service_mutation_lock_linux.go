//go:build linux

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

type serviceMutationFileLock struct {
	file *os.File
}

func acquireServiceMutationFileLock(path string) (*serviceMutationFileLock, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("service mutation lock path must be absolute")
	}
	lockDir := filepath.Dir(path)
	if err := ensureSecureServiceMutationLockDirectory(lockDir); err != nil {
		return nil, err
	}
	created := false
	fd, err := unix.Open(
		path,
		unix.O_CREAT|unix.O_EXCL|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0o600,
	)
	if err == nil {
		created = true
	} else if errors.Is(err, unix.EEXIST) {
		fd, err = unix.Open(path, unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	}
	if err != nil {
		return nil, fmt.Errorf("open service mutation lock: %w", err)
	}
	if created {
		if err := unix.Fchmod(fd, 0o600); err != nil {
			_ = unix.Close(fd)
			return nil, fmt.Errorf("secure new service mutation lock: %w", err)
		}
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("create service mutation lock file handle")
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("inspect service mutation lock: %w", err)
	}
	if err := secureServiceMutationStat(path, info, false); err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, errServiceMutationHostBusy
		}
		return nil, fmt.Errorf("lock service mutation file: %w", err)
	}
	return &serviceMutationFileLock{file: file}, nil
}

func ensureSecureServiceMutationLockDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect service mutation lock directory: %w", err)
	}
	if err := secureServiceMutationStat(path, info, true); err != nil {
		return err
	}
	return nil
}

func (l *serviceMutationFileLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	fd := int(l.file.Fd())
	unlockErr := unix.Flock(fd, unix.LOCK_UN)
	closeErr := l.file.Close()
	l.file = nil
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}

func probeServiceMutationFileLockIdle(path string) error {
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) {
		return errors.New("service mutation lock path must be absolute")
	}
	lockDir := filepath.Dir(path)
	info, err := os.Lstat(lockDir)
	if err != nil {
		return fmt.Errorf("inspect service mutation lock directory: %w", err)
	}
	if err := secureServiceMutationStat(lockDir, info, true); err != nil {
		return err
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open service mutation lock for idle check: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return errors.New("open service mutation lock idle-check handle")
	}
	defer file.Close()
	lockInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("inspect service mutation lock: %w", err)
	}
	if err := secureServiceMutationStat(path, lockInfo, false); err != nil {
		return err
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return errServiceMutationHostBusy
		}
		return fmt.Errorf("probe service mutation lock: %w", err)
	}
	if err := unix.Flock(fd, unix.LOCK_UN); err != nil {
		return fmt.Errorf("release service mutation lock probe: %w", err)
	}
	return nil
}

func syncServiceMutationDirectory(path string) error {
	dirPath := filepath.Dir(path)
	if err := ensureSecureServiceMutationStateDirectory(dirPath); err != nil {
		return err
	}
	fd, err := unix.Open(dirPath, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_DIRECTORY, 0)
	if err != nil {
		return err
	}
	dir := os.NewFile(uintptr(fd), dirPath)
	if dir == nil {
		_ = unix.Close(fd)
		return errors.New("open service mutation directory handle")
	}
	defer dir.Close()
	return dir.Sync()
}

func packageManagerMutationBusy() (bool, error) {
	busy, err := linuxPackageProcessBusy()
	if err != nil || busy {
		return busy, err
	}
	for _, path := range []string{
		"/var/lib/dpkg/lock-frontend",
		"/var/lib/dpkg/lock",
		"/var/cache/apt/archives/lock",
	} {
		busy, err = linuxFcntlLockBusy(path)
		if err != nil || busy {
			return busy, err
		}
	}
	if _, err := os.Lstat("/var/lib/pacman/db.lck"); err == nil {
		return true, nil
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("inspect pacman lock: %w", err)
	}
	return false, nil
}

func linuxFcntlLockBusy(path string) (bool, error) {
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if errors.Is(err, unix.ENOENT) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("open package manager lock %s: %w", path, err)
	}
	defer unix.Close(fd)
	lock := unix.Flock_t{
		Type:   unix.F_WRLCK,
		Whence: int16(os.SEEK_SET),
		Start:  0,
		Len:    0,
	}
	if err := unix.FcntlFlock(uintptr(fd), unix.F_SETLK, &lock); err != nil {
		if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EACCES) {
			return true, nil
		}
		return false, fmt.Errorf("probe package manager lock %s: %w", path, err)
	}
	lock.Type = unix.F_UNLCK
	if err := unix.FcntlFlock(uintptr(fd), unix.F_SETLK, &lock); err != nil {
		return false, fmt.Errorf("release package manager lock probe %s: %w", path, err)
	}
	return false, nil
}

func linuxPackageProcessBusy() (bool, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false, fmt.Errorf("read /proc: %w", err)
	}
	packageProcesses := map[string]struct{}{
		"apt": {}, "apt-get": {}, "dpkg": {}, "dpkg-deb": {},
		"pacman": {}, "makepkg": {},
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == "" || name[0] < '0' || name[0] > '9' {
			continue
		}
		raw, readErr := os.ReadFile(filepath.Join("/proc", name, "comm"))
		if readErr != nil {
			if os.IsNotExist(readErr) {
				continue
			}
			return false, fmt.Errorf("read package process identity: %w", readErr)
		}
		process := string(raw)
		for len(process) > 0 && (process[len(process)-1] == '\n' || process[len(process)-1] == '\r') {
			process = process[:len(process)-1]
		}
		if _, found := packageProcesses[process]; found {
			return true, nil
		}
	}
	return false, nil
}
