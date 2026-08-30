//go:build linux

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

const serviceMutationLockFaultAfterCreate = "after_create_before_chown"

const serviceMutationExternalLockFDEnvironment = "CELIKPANEL_MUTATION_LOCK_FD"

var serviceMutationLockFaultHook func(string) error

type serviceMutationFileLock struct {
	file        *os.File
	publication *serviceMutationFileLock
}

// acquireExistingServiceMutationFileLock obtains the common host flock without
// creating or repairing any filesystem object. Comparison-only RPCs use this
// lease so a missing or non-canonical lock fails closed instead of turning a
// read into host mutation.
func acquireExistingServiceMutationFileLock(path string) (*serviceMutationFileLock, error) {
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) {
		return nil, errors.New("service mutation lock path must be absolute")
	}
	lockDir := filepath.Dir(path)
	dirInfo, err := os.Lstat(lockDir)
	if err != nil {
		return nil, fmt.Errorf("inspect service mutation lock directory: %w", err)
	}
	if err := secureServiceMutationStat(lockDir, dirInfo, true); err != nil {
		return nil, err
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("open existing service mutation lock: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("open existing service mutation lock handle")
	}
	keepFile := false
	defer func() {
		if !keepFile {
			_ = file.Close()
		}
	}()
	verify := func() error {
		info, statErr := file.Stat()
		if statErr != nil {
			return fmt.Errorf("inspect existing service mutation lock: %w", statErr)
		}
		if statErr := secureServiceMutationStat(path, info, false); statErr != nil {
			return statErr
		}
		if info.Size() != 0 {
			return fmt.Errorf("%s service mutation lock must be empty", path)
		}
		return verifyServiceMutationLockPathIdentity(path, info)
	}
	if err := verify(); err != nil {
		return nil, err
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, errServiceMutationHostBusy
		}
		return nil, fmt.Errorf("lock existing service mutation file: %w", err)
	}
	if err := verify(); err != nil {
		_ = unix.Flock(fd, unix.LOCK_UN)
		return nil, err
	}
	keepFile = true
	return &serviceMutationFileLock{file: file}, nil
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
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("create service mutation lock file handle")
	}
	keepFile := false
	defer func() {
		if !keepFile {
			_ = file.Close()
		}
	}()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect service mutation lock: %w", err)
	}
	if err := verifyServiceMutationLockPathIdentity(path, info); err != nil {
		return nil, err
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, errServiceMutationHostBusy
		}
		return nil, fmt.Errorf("lock service mutation file: %w", err)
	}
	if created {
		if err := verifyNewServiceMutationLockResidue(path, info); err != nil {
			return nil, err
		}
		if serviceMutationLockFaultHook != nil {
			if err := serviceMutationLockFaultHook(serviceMutationLockFaultAfterCreate); err != nil {
				return nil, fmt.Errorf("injected service mutation lock creation failure: %w", err)
			}
		}
	} else if err := secureServiceMutationStat(path, info, false); err != nil {
		if recoverErr := verifyRecoverableServiceMutationLockResidue(path, info); recoverErr != nil {
			return nil, errors.Join(err, recoverErr)
		}
	} else if info.Size() != 0 {
		return nil, fmt.Errorf("%s service mutation lock must be empty", path)
	}
	if created || !serviceMutationLockHasRequiredMetadata(info) {
		if err := completeServiceMutationLockMetadata(path, lockDir, file); err != nil {
			return nil, err
		}
	}
	info, err = file.Stat()
	if err != nil {
		return nil, fmt.Errorf("reinspect service mutation lock: %w", err)
	}
	if err := secureServiceMutationStat(path, info, false); err != nil {
		return nil, err
	}
	if info.Size() != 0 {
		return nil, fmt.Errorf("%s service mutation lock must remain empty", path)
	}
	if err := verifyServiceMutationLockPathIdentity(path, info); err != nil {
		return nil, err
	}
	keepFile = true
	return &serviceMutationFileLock{file: file}, nil
}

func serviceMutationLockHasRequiredMetadata(info os.FileInfo) bool {
	if info == nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Size() != 0 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok &&
		stat.Uid == serviceMutationRequiredOwnerUID &&
		stat.Gid == serviceMutationRequiredOwnerGID &&
		stat.Nlink == 1
}

func verifyNewServiceMutationLockResidue(path string, info os.FileInfo) error {
	if info == nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Size() != 0 {
		return fmt.Errorf("%s new lock must be an empty regular 0600 file", path)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	creatorGID := uint32(os.Getegid())
	if !ok ||
		stat.Uid != uint32(os.Geteuid()) ||
		(stat.Gid != creatorGID && stat.Gid != serviceMutationRequiredOwnerGID) ||
		stat.Nlink != 1 {
		return fmt.Errorf("%s new lock has unsafe creator metadata", path)
	}
	return nil
}

// A failed first creation may leave only the exact root:root empty lock that
// exists between O_EXCL and fchown; every broader residue fails closed.
// Başarısız ilk oluşturma yalnızca O_EXCL ile fchown arasında var olan tam
// root:root boş kilidi bırakabilir; daha geniş tüm kalıntılar kapalı hata verir.
func verifyRecoverableServiceMutationLockResidue(path string, info os.FileInfo) error {
	if os.Geteuid() != 0 || serviceMutationRequiredOwnerUID != 0 ||
		info == nil || !info.Mode().IsRegular() ||
		info.Mode().Perm() != 0o600 || info.Size() != 0 {
		return fmt.Errorf("%s is not a recoverable root:root empty lock residue", path)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || stat.Gid != 0 || stat.Nlink != 1 {
		return fmt.Errorf("%s is not a recoverable root:root empty lock residue", path)
	}
	return nil
}

func completeServiceMutationLockMetadata(path, lockDir string, file *os.File) error {
	if file == nil {
		return errors.New("missing service mutation lock handle")
	}
	before, err := file.Stat()
	if err != nil {
		return fmt.Errorf("inspect service mutation lock before repair: %w", err)
	}
	if err := verifyServiceMutationLockPathIdentity(path, before); err != nil {
		return err
	}
	if err := unix.Fchown(
		int(file.Fd()),
		int(serviceMutationRequiredOwnerUID),
		int(serviceMutationRequiredOwnerGID),
	); err != nil {
		return fmt.Errorf("set service mutation lock ownership: %w", err)
	}
	if err := unix.Fchmod(int(file.Fd()), 0o600); err != nil {
		return fmt.Errorf("secure service mutation lock: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync service mutation lock: %w", err)
	}
	if err := syncServiceMutationLockDirectory(lockDir); err != nil {
		return err
	}
	return nil
}

func verifyServiceMutationLockPathIdentity(path string, fdInfo os.FileInfo) error {
	pathInfo, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect service mutation lock path: %w", err)
	}
	if !pathInfo.Mode().IsRegular() {
		return fmt.Errorf("%s must be a real regular file", path)
	}
	fdStat, fdOK := fdInfo.Sys().(*syscall.Stat_t)
	pathStat, pathOK := pathInfo.Sys().(*syscall.Stat_t)
	if !fdOK || !pathOK || fdStat.Dev != pathStat.Dev || fdStat.Ino != pathStat.Ino {
		return fmt.Errorf("%s changed while opening the service mutation lock", path)
	}
	return nil
}

// External-lock modes accept only a descriptor inherited from the caller that
// already owns the exact common flock; merely naming an unlocked descriptor is
// not authority to skip the normal lock probe.
// Harici-kilit kipleri yalnız çağırandan miras kalan ve tam ortak flock'a zaten
// sahip descriptor'ı kabul eder; kilitsiz descriptor adı vermek normal kilit
// sınamasını atlama yetkisi değildir.
func verifyInheritedServiceMutationFileLock(path string) error {
	raw := strings.TrimSpace(os.Getenv(serviceMutationExternalLockFDEnvironment))
	if raw == "" {
		return fmt.Errorf("%s is required", serviceMutationExternalLockFDEnvironment)
	}
	fd64, err := strconv.ParseInt(raw, 10, 32)
	if err != nil || fd64 < 3 {
		return fmt.Errorf("%s must name an inherited descriptor", serviceMutationExternalLockFDEnvironment)
	}
	return verifyInheritedServiceMutationFileLockFD(path, int(fd64))
}

func verifyInheritedServiceMutationFileLockFD(path string, fd int) error {
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) || fd < 3 {
		return errors.New("inherited service mutation lock proof is invalid")
	}
	if err := ensureSecureServiceMutationLockDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	dupFD, err := unix.FcntlInt(uintptr(fd), unix.F_DUPFD_CLOEXEC, 3)
	if err != nil {
		return fmt.Errorf("duplicate inherited service mutation lock descriptor: %w", err)
	}
	file := os.NewFile(uintptr(dupFD), path)
	if file == nil {
		_ = unix.Close(dupFD)
		return errors.New("open inherited service mutation lock descriptor")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("inspect inherited service mutation lock descriptor: %w", err)
	}
	if err := secureServiceMutationStat(path, info, false); err != nil {
		return err
	}
	if info.Size() != 0 {
		return fmt.Errorf("%s service mutation lock must be empty", path)
	}
	if err := verifyServiceMutationLockPathIdentity(path, info); err != nil {
		return err
	}
	fdInfo, err := os.ReadFile(filepath.Join("/proc/self/fdinfo", strconv.Itoa(dupFD)))
	if err != nil {
		return fmt.Errorf("inspect inherited service mutation flock ownership: %w", err)
	}
	if !serviceMutationFDInfoHasExclusiveFlock(fdInfo) {
		return errors.New("inherited service mutation lock descriptor does not already own the flock")
	}
	probeFD, err := unix.Open(path, unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open independent service mutation lock proof: %w", err)
	}
	defer unix.Close(probeFD)
	if err := unix.Flock(probeFD, unix.LOCK_EX|unix.LOCK_NB); err == nil {
		_ = unix.Flock(probeFD, unix.LOCK_UN)
		return errors.New("inherited service mutation descriptor does not exclude an independent opener")
	} else if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
		return fmt.Errorf("prove inherited service mutation flock contention: %w", err)
	}
	return nil
}

func serviceMutationFDInfoHasExclusiveFlock(raw []byte) bool {
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 9 && fields[0] == "lock:" &&
			fields[2] == "FLOCK" && fields[3] == "ADVISORY" &&
			fields[4] == "WRITE" && fields[len(fields)-2] == "0" &&
			fields[len(fields)-1] == "EOF" {
			return true
		}
	}
	return false
}

func syncServiceMutationLockDirectory(path string) error {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_DIRECTORY, 0)
	if err != nil {
		return fmt.Errorf("open service mutation lock directory for sync: %w", err)
	}
	dir := os.NewFile(uintptr(fd), path)
	if dir == nil {
		_ = unix.Close(fd)
		return errors.New("open service mutation lock directory handle")
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return fmt.Errorf("sync service mutation lock directory: %w", err)
	}
	return nil
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

func (l *serviceMutationFileLock) closeOwn() error {
	if l == nil || l.file == nil {
		return nil
	}
	fd := int(l.file.Fd())
	unlockErr := unix.Flock(fd, unix.LOCK_UN)
	closeErr := l.file.Close()
	l.file = nil
	return errors.Join(unlockErr, closeErr)
}

// closeHostRetainingPublication releases only the outer host mutation lease.
// A DNS finalizer uses this narrow transition to make the host available while
// retaining the cross-process ledger publication lease until its terminal
// receipt is durable. Every acquisition remains host -> publication.
func (l *serviceMutationFileLock) closeHostRetainingPublication() (
	*serviceMutationFileLock,
	error,
) {
	if l == nil {
		return nil, nil
	}
	publication := l.publication
	l.publication = nil
	return publication, l.closeOwn()
}

func (l *serviceMutationFileLock) Close() error {
	if l == nil {
		return nil
	}
	hostErr := l.closeOwn()
	publication := l.publication
	l.publication = nil
	if publication == nil {
		return hostErr
	}
	return errors.Join(hostErr, publication.Close())
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

// syncDNSClusterConfigDirectory durably publishes changes in PowerDNS' managed
// include directory. Unlike the service-mutation state directory, pdns.d is
// normally mode 0755 and need not use the CelikPanel service group; it must
// still be a trusted, non-writable real directory owned by the required UID.
func validateDNSClusterConfigDirectory(dirPath string) error {
	dirPath = filepath.Clean(dirPath)
	info, err := os.Lstat(dirPath)
	if err != nil {
		return err
	}
	return secureRequiredOwnerDirectoryStat(
		dirPath,
		info,
		dnsClusterConfigRequiredOwnerUID,
	)
}

func syncDNSClusterConfigDirectory(path string) error {
	dirPath := filepath.Dir(path)
	if err := validateDNSClusterConfigDirectory(dirPath); err != nil {
		return err
	}
	fd, err := unix.Open(dirPath, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_DIRECTORY, 0)
	if err != nil {
		return err
	}
	dir := os.NewFile(uintptr(fd), dirPath)
	if dir == nil {
		_ = unix.Close(fd)
		return errors.New("open DNS cluster configuration directory handle")
	}
	defer dir.Close()
	return dir.Sync()
}

func realPackageManagerMutationBusy() (bool, error) {
	busy, err := linuxPackageProcessBusy()
	if err != nil || busy {
		return busy, err
	}
	for _, path := range linuxPackageManagerFcntlLockPaths() {
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

func linuxPackageManagerFcntlLockPaths() []string {
	return []string{
		"/var/lib/dpkg/lock-frontend",
		"/var/lib/dpkg/lock",
		"/var/cache/apt/archives/lock",
		// Upstream RPM/DNF reports identify the historical and newer database
		// locations below. The RHEL preview remains unreachable until live
		// distro certification; absence is harmless and contention fails closed.
		"/var/lib/rpm/.rpm.lock",
		"/usr/lib/sysimage/rpm/.rpm.lock",
	}
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
	return linuxPackageProcessBusyAt("/proc")
}

func linuxPackageProcessBusyAt(procRoot string) (bool, error) {
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return false, fmt.Errorf("read process table: %w", err)
	}
	packageProcesses := map[string]struct{}{
		"apt": {}, "apt-get": {}, "dpkg": {}, "dpkg-deb": {},
		"pacman": {}, "makepkg": {},
		"dnf": {}, "dnf5": {}, "yum": {}, "microdnf": {},
		"rpm": {}, "rpmdb": {},
		"packagekitd": {}, "packagekit": {}, "pkcon": {},
		// Linux comm names are limited to 15 bytes, so dnfdaemon-server may
		// be observed in either its full or kernel-truncated spelling.
		"dnfdaemon-server": {}, "dnfdaemon-serve": {},
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == "" || name[0] < '0' || name[0] > '9' {
			continue
		}
		raw, readErr := os.ReadFile(filepath.Join(procRoot, name, "comm"))
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
