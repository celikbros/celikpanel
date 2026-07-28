//go:build linux

package manifestv2

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"golang.org/x/sys/unix"
)

const catalogBuildDatabaseName = "catalog.db"

// openCatalogPublishDirectory pins and validates the directory before any
// staging pathname is created beneath it.
// openCatalogPublishDirectory, altında herhangi bir hazırlama yolu
// oluşturulmadan önce dizini sabitler ve doğrular.
func openCatalogPublishDirectory(path string) (*os.File, error) {
	fd, err := unix.Open(
		path,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		return nil, err
	}
	directory := os.NewFile(uintptr(fd), path)
	if directory == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("wrap catalog publish directory descriptor")
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		_ = directory.Close()
		return nil, err
	}
	if err := validateCatalogDirectoryStat(stat, os.Geteuid()); err != nil {
		_ = directory.Close()
		return nil, err
	}
	return directory, nil
}

// validateCatalogDirectoryStat accepts only a root/current-euid-owned directory
// that untrusted group or other accounts cannot rename entries within.
// validateCatalogDirectoryStat, yalnız root/geçerli-euid sahipliğinde olan ve
// güvenilmeyen grup ya da diğer hesapların girdileri yeniden adlandıramadığı
// dizinleri kabul eder.
func validateCatalogDirectoryStat(stat unix.Stat_t, effectiveUID int) error {
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		return fmt.Errorf("catalog publish parent is not a directory")
	}
	if stat.Uid != 0 && uint64(stat.Uid) != uint64(effectiveUID) {
		return fmt.Errorf(
			"catalog publish parent owner uid %d is neither root nor effective uid %d",
			stat.Uid,
			effectiveUID,
		)
	}
	if stat.Mode&0o022 != 0 {
		return fmt.Errorf(
			"catalog publish parent mode %04o permits group or other writes",
			stat.Mode&0o7777,
		)
	}
	return nil
}

// validateCatalogArtifactStat prevents signing or publishing a substituted
// non-regular or untrusted-writable inode.
// validateCatalogArtifactStat, değiştirilmiş düzenli olmayan veya güvenilmeyen
// hesaplarca yazılabilir bir inode'un imzalanmasını ya da yayımlanmasını önler.
func validateCatalogArtifactStat(stat unix.Stat_t, effectiveUID int) error {
	if stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return fmt.Errorf("catalog artifact is not a regular file")
	}
	if stat.Uid != 0 && uint64(stat.Uid) != uint64(effectiveUID) {
		return fmt.Errorf(
			"catalog artifact owner uid %d is neither root nor effective uid %d",
			stat.Uid,
			effectiveUID,
		)
	}
	if stat.Mode&0o022 != 0 {
		return fmt.Errorf(
			"catalog artifact mode %04o permits group or other writes",
			stat.Mode&0o7777,
		)
	}
	return nil
}

// createCatalogBuildWorkspace creates every staging entry relative to the
// already-validated parent descriptor and keeps the database inode open.
// createCatalogBuildWorkspace, tüm hazırlama girdilerini önceden doğrulanmış
// üst dizin tanıtıcısına göre oluşturur ve veritabanı inode'unu açık tutar.
func createCatalogBuildWorkspace(parent *os.File) (*catalogBuildWorkspace, error) {
	workspace := &catalogBuildWorkspace{
		publishDirectory: parent,
		databaseName:     catalogBuildDatabaseName,
	}
	created := false
	for attempt := 0; attempt < 128; attempt++ {
		randomBytes := make([]byte, 16)
		if _, err := rand.Read(randomBytes); err != nil {
			return nil, fmt.Errorf("generate catalog staging name: %w", err)
		}
		workspace.stagingName = ".celikpanel-manifest-v2-build-" + hex.EncodeToString(randomBytes)
		err := unix.Mkdirat(int(parent.Fd()), workspace.stagingName, 0o700)
		if errors.Is(err, unix.EEXIST) {
			continue
		}
		if err != nil {
			return nil, err
		}
		created = true
		break
	}
	if !created {
		workspace.stagingName = ""
		return nil, fmt.Errorf("allocate unique catalog staging directory")
	}

	stageFD, err := unix.Openat(
		int(parent.Fd()),
		workspace.stagingName,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		_ = cleanupCatalogBuildWorkspace(workspace)
		return nil, err
	}
	workspace.stagingDirectory = os.NewFile(uintptr(stageFD), workspace.stagingName)
	if workspace.stagingDirectory == nil {
		_ = unix.Close(stageFD)
		_ = cleanupCatalogBuildWorkspace(workspace)
		return nil, fmt.Errorf("wrap catalog staging directory descriptor")
	}
	var stageStat unix.Stat_t
	if err := unix.Fstat(stageFD, &stageStat); err != nil {
		_ = cleanupCatalogBuildWorkspace(workspace)
		return nil, err
	}
	if err := validateCatalogDirectoryStat(stageStat, os.Geteuid()); err != nil {
		_ = cleanupCatalogBuildWorkspace(workspace)
		return nil, fmt.Errorf("validate catalog staging directory: %w", err)
	}

	databaseFD, err := unix.Openat(
		stageFD,
		workspace.databaseName,
		unix.O_RDWR|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0o600,
	)
	if err != nil {
		_ = cleanupCatalogBuildWorkspace(workspace)
		return nil, err
	}
	workspace.database = os.NewFile(uintptr(databaseFD), workspace.databaseName)
	if workspace.database == nil {
		_ = unix.Close(databaseFD)
		_ = cleanupCatalogBuildWorkspace(workspace)
		return nil, fmt.Errorf("wrap catalog database descriptor")
	}
	var databaseStat unix.Stat_t
	if err := unix.Fstat(databaseFD, &databaseStat); err != nil {
		_ = cleanupCatalogBuildWorkspace(workspace)
		return nil, err
	}
	if err := validateCatalogArtifactStat(databaseStat, os.Geteuid()); err != nil {
		_ = cleanupCatalogBuildWorkspace(workspace)
		return nil, fmt.Errorf("validate private catalog database: %w", err)
	}
	workspace.databasePath = fmt.Sprintf(
		"/proc/self/fd/%d/%s",
		workspace.stagingDirectory.Fd(),
		workspace.databaseName,
	)
	return workspace, nil
}

// cleanupCatalogBuildWorkspace removes only names beneath the pinned staging
// and parent descriptors, then makes their deletion durable.
// cleanupCatalogBuildWorkspace, yalnız sabitlenmiş hazırlama ve üst dizin
// tanıtıcıları altındaki adları kaldırır ve silinmelerini kalıcılaştırır.
func cleanupCatalogBuildWorkspace(workspace *catalogBuildWorkspace) error {
	if workspace == nil {
		return nil
	}
	var cleanupErrors []error
	if workspace.database != nil {
		if err := workspace.database.Close(); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("close catalog staging database: %w", err))
		}
		workspace.database = nil
	}
	if workspace.stagingDirectory != nil {
		err := unix.Unlinkat(
			int(workspace.stagingDirectory.Fd()),
			workspace.databaseName,
			0,
		)
		if err != nil && !errors.Is(err, unix.ENOENT) {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("remove catalog staging database: %w", err))
		}
		if err := workspace.stagingDirectory.Sync(); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("sync catalog staging directory: %w", err))
		}
		if err := workspace.stagingDirectory.Close(); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("close catalog staging directory: %w", err))
		}
		workspace.stagingDirectory = nil
	}
	if workspace.publishDirectory != nil && workspace.stagingName != "" {
		err := unix.Unlinkat(
			int(workspace.publishDirectory.Fd()),
			workspace.stagingName,
			unix.AT_REMOVEDIR,
		)
		if err != nil && !errors.Is(err, unix.ENOENT) {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("remove catalog staging directory: %w", err))
		}
		if err := workspace.publishDirectory.Sync(); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("sync catalog publish directory cleanup: %w", err))
		}
	}
	return errors.Join(cleanupErrors...)
}

// openCatalogSigningArtifact opens the basename through a verified parent
// dirfd and pins that exact regular inode for hashing and signing.
// openCatalogSigningArtifact, taban adını doğrulanmış üst dizin dirfd'si
// üzerinden açar ve tam olarak o düzenli inode'u karma ve imzalama için sabitler.
func openCatalogSigningArtifact(path string) (*os.File, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	name := filepath.Base(absolute)
	if err := validateCatalogBasename(name); err != nil {
		return nil, fmt.Errorf("validate catalog signing basename: %w", err)
	}
	parent, err := openCatalogPublishDirectory(filepath.Dir(absolute))
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	fd, err := unix.Openat(
		int(parent.Fd()),
		name,
		unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), absolute)
	if file == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("wrap catalog signing artifact descriptor")
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := validateCatalogArtifactStat(stat, os.Geteuid()); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

// lockCatalogPublishDirectory serializes cooperating publishers while a link
// is created, synced, and possibly removed after a durability failure.
// lockCatalogPublishDirectory, bağlantı oluşturulurken, eşzamanlanırken ve
// kalıcılık hatasından sonra gerekirse kaldırılırken iş birliği yapan
// yayımcıları sıraya koyar.
func lockCatalogPublishDirectory(directory *os.File) error {
	for {
		err := unix.Flock(int(directory.Fd()), unix.LOCK_EX)
		if err != unix.EINTR {
			return err
		}
	}
}

func unlockCatalogPublishDirectory(directory *os.File) {
	_ = unix.Flock(int(directory.Fd()), unix.LOCK_UN)
}

// linkCatalogFile follows the already-open source through a pinned procfs
// directory and creates the destination by parent dirfd plus basename.
// linkCatalogFile, önceden açılmış kaynağı sabitlenmiş procfs dizini üzerinden
// izler ve hedefi üst dizin fd'si ile taban adını kullanarak oluşturur.
func linkCatalogFile(source *os.File, directory *os.File, destinationName string) error {
	procDirectory, err := unix.Open("/proc/self/fd", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(procDirectory)
	return unix.Linkat(
		procDirectory,
		strconv.FormatUint(uint64(source.Fd()), 10),
		int(directory.Fd()),
		destinationName,
		unix.AT_SYMLINK_FOLLOW,
	)
}

func catalogFileAtMatches(source *os.File, directory *os.File, destinationName string) (bool, error) {
	var sourceStat, destinationStat unix.Stat_t
	if err := unix.Fstat(int(source.Fd()), &sourceStat); err != nil {
		return false, err
	}
	if err := unix.Fstatat(
		int(directory.Fd()),
		destinationName,
		&destinationStat,
		unix.AT_SYMLINK_NOFOLLOW,
	); err != nil {
		return false, err
	}
	return sourceStat.Dev == destinationStat.Dev &&
		sourceStat.Ino == destinationStat.Ino &&
		destinationStat.Mode&unix.S_IFMT == unix.S_IFREG, nil
}

func removeCatalogFileAt(directory *os.File, destinationName string) error {
	return unix.Unlinkat(int(directory.Fd()), destinationName, 0)
}
