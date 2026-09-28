//go:build linux

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/alicelik/celikpanel/internal/bindroot"
	"github.com/alicelik/celikpanel/internal/secureconfigwriter"
	"golang.org/x/sys/unix"
)

const secureConfigResolve = unix.RESOLVE_BENEATH |
	unix.RESOLVE_NO_SYMLINKS |
	unix.RESOLVE_NO_MAGICLINKS

func secureConfigRelativePath(path string) (string, error) {
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) || clean == string(os.PathSeparator) {
		return "", configPathRefusal("managed configuration path must be an absolute file path: %s", path)
	}
	relative := strings.TrimPrefix(clean, string(os.PathSeparator))
	if relative == "" || relative == "." {
		return "", configPathRefusal("managed configuration path must name a file: %s", path)
	}
	return relative, nil
}

func openSecureConfigRoot() (int, error) {
	fd, err := unix.Open("/", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, fmt.Errorf("open managed configuration root: %w", err)
	}
	return fd, nil
}

func secureConfigOpenError(operation, path string, err error) error {
	if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.EXDEV) {
		return fmt.Errorf("%w: %s refused because the path contains a symbolic link or escapes the managed root (%s): %v", errConfigPathRefused, operation, path, err)
	}
	if errors.Is(err, unix.ENOSYS) {
		return fmt.Errorf("%s refused because secure openat2 path resolution is unavailable: %w", operation, err)
	}
	return fmt.Errorf("%s %s: %w", operation, path, err)
}

func secureReadConfig(path string) ([]byte, error) {
	relative, err := secureConfigRelativePath(path)
	if err != nil {
		return nil, err
	}
	rootFD, err := openSecureConfigRoot()
	if err != nil {
		return nil, err
	}
	defer unix.Close(rootFD)

	fd, err := unix.Openat2(rootFD, relative, &unix.OpenHow{
		Flags:   uint64(unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK),
		Resolve: secureConfigResolve,
	})
	if err != nil {
		return nil, secureConfigOpenError("read managed configuration", path, err)
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		unix.Close(fd)
		return nil, fmt.Errorf("read managed configuration %s: invalid file descriptor", path)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat managed configuration %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, configPathRefusal("read managed configuration refused for non-regular file: %s", path)
	}
	content, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("read managed configuration %s: %w", path, err)
	}
	return content, nil
}

func secureWriteConfig(path string, content []byte, mode os.FileMode) error {
	return secureWriteConfigReplacingSnapshot(path, content, mode, nil)
}

// secureWriteConfigOwnedBy publishes a managed file that must end up owned by
// exactly uid:gid. Everything else about the write is unchanged: no-follow
// temporary creation beneath the parent, explicit chmod, fsync, an exact
// metadata contract proved on the descriptor, then rename and a parent fsync.
// secureWriteConfigOwnedBy, tam olarak uid:gid sahipli olmasi gereken bir
// yonetilen dosyayi yayimlar. Yazmanin geri kalani aynidir.
func secureWriteConfigOwnedBy(
	path string,
	content []byte,
	mode os.FileMode,
	uid, gid uint32,
) error {
	return secureWriteConfigReplacingSnapshotWithOptions(
		path, content, mode, nil,
		secureConfigWriteOptions{
			publishedOwner: &secureConfigOwner{uid: uid, gid: gid},
		},
	)
}

type secureConfigOwner struct {
	uid uint32
	gid uint32
}

type secureConfigWriteOptions struct {
	parentPolicy  *bindConfigOwnerPolicy
	requiredOwner *secureConfigOwner
	// publishedOwner forces the published file's uid:gid instead of
	// inheriting the agent process's own credentials. The unit runs the
	// agent as User=root with Group=celikpanel, so anything it creates is
	// group celikpanel unless the owner is chosen deliberately; managed TLS
	// material has to belong to the same identity as the directory that
	// holds it, which is what its readback verification asserts.
	// publishedOwner, yayımlanan dosyanın uid:gid'ini agent surecinin kendi
	// kimliginden devralmak yerine zorlar. Birim agent'i User=root ve
	// Group=celikpanel ile calistirir; bu yuzden olusturdugu her dosya, sahibi
	// bilerek secilmedikce celikpanel grubundadir. Yonetilen TLS malzemesi,
	// dogrulamasinin ileri surdugu gibi, kendisini tutan dizinle ayni kimlige
	// ait olmalidir.
	publishedOwner         *secureConfigOwner
	beforeFinalParentProof func()
}

func sameSecureConfigStat(left, right unix.Stat_t) bool {
	return left.Dev == right.Dev && left.Ino == right.Ino &&
		left.Mode == right.Mode && left.Uid == right.Uid && left.Gid == right.Gid &&
		left.Nlink == right.Nlink && left.Size == right.Size &&
		left.Mtim == right.Mtim && left.Ctim == right.Ctim
}

func inspectBINDConfigParentFD(
	parentFD int,
	policy bindConfigOwnerPolicy,
) (unix.Stat_t, error) {
	layout := bindroot.Pacman
	if policy.apt {
		layout = bindroot.APT
	}
	return bindroot.InspectConfigParentFD(parentFD, layout, policy.bindGID)
}

func verifyBINDConfigParentFD(
	parentFD int,
	policy bindConfigOwnerPolicy,
) error {
	_, err := inspectBINDConfigParentFD(parentFD, policy)
	return err
}

func verifyBINDConfigParentPath(
	path string,
	policy bindConfigOwnerPolicy,
) error {
	relative, err := secureConfigRelativePath(path)
	if err != nil {
		return err
	}
	rootFD, err := openSecureConfigRoot()
	if err != nil {
		return err
	}
	defer unix.Close(rootFD)
	parentFD, err := unix.Openat2(rootFD, filepath.Dir(relative), &unix.OpenHow{
		Flags:   uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW),
		Resolve: secureConfigResolve,
	})
	if err != nil {
		return secureConfigOpenError("verify BIND config parent", path, err)
	}
	defer unix.Close(parentFD)
	return verifyBINDConfigParentFD(parentFD, policy)
}

func secureWriteConfigReplacingSnapshot(
	path string,
	content []byte,
	mode os.FileMode,
	expected *dnsFileSnapshot,
) error {
	return secureWriteConfigReplacingSnapshotWithBINDParent(
		path, content, mode, expected, nil,
	)
}

func secureWriteConfigReplacingSnapshotWithOwner(
	path string,
	content []byte,
	mode os.FileMode,
	expected *dnsFileSnapshot,
	requiredUID, requiredGID uint32,
) error {
	return secureWriteConfigReplacingSnapshotWithOwnerAndHook(
		path, content, mode, expected, requiredUID, requiredGID, nil,
	)
}

func secureWriteConfigReplacingSnapshotWithOwnerAndHook(
	path string,
	content []byte,
	mode os.FileMode,
	expected *dnsFileSnapshot,
	requiredUID, requiredGID uint32,
	beforeFinalParentProof func(),
) error {
	return secureWriteConfigReplacingSnapshotWithOptions(
		path,
		content,
		mode,
		expected,
		secureConfigWriteOptions{
			requiredOwner:          &secureConfigOwner{uid: requiredUID, gid: requiredGID},
			beforeFinalParentProof: beforeFinalParentProof,
		},
	)
}

func secureWriteBINDConfigReplacingSnapshot(
	path string,
	content []byte,
	mode os.FileMode,
	expected *dnsFileSnapshot,
	policy bindConfigOwnerPolicy,
) error {
	return secureWriteConfigReplacingSnapshotWithBINDParent(
		path, content, mode, expected, &policy,
	)
}

func secureWriteConfigReplacingSnapshotWithBINDParent(
	path string,
	content []byte,
	mode os.FileMode,
	expected *dnsFileSnapshot,
	parentPolicy *bindConfigOwnerPolicy,
) error {
	return secureWriteConfigReplacingSnapshotWithBINDParentAndHook(
		path, content, mode, expected, parentPolicy, nil,
	)
}

func secureWriteConfigReplacingSnapshotWithBINDParentAndHook(
	path string,
	content []byte,
	mode os.FileMode,
	expected *dnsFileSnapshot,
	parentPolicy *bindConfigOwnerPolicy,
	beforeFinalParentProof func(),
) error {
	return secureWriteConfigReplacingSnapshotWithOptions(
		path,
		content,
		mode,
		expected,
		secureConfigWriteOptions{
			parentPolicy:           parentPolicy,
			beforeFinalParentProof: beforeFinalParentProof,
		},
	)
}

func secureWriteConfigReplacingSnapshotWithOptions(
	path string,
	content []byte,
	mode os.FileMode,
	expected *dnsFileSnapshot,
	options secureConfigWriteOptions,
) error {
	var parentValidator func(int) (unix.Stat_t, error)
	if options.parentPolicy != nil {
		policy := *options.parentPolicy
		parentValidator = func(fd int) (unix.Stat_t, error) {
			return inspectBINDConfigParentFD(fd, policy)
		}
	}
	var requiredOwner, publishedOwner *secureconfigwriter.Owner
	if options.requiredOwner != nil {
		requiredOwner = &secureconfigwriter.Owner{UID: options.requiredOwner.uid, GID: options.requiredOwner.gid}
	}
	if options.publishedOwner != nil {
		publishedOwner = &secureconfigwriter.Owner{UID: options.publishedOwner.uid, GID: options.publishedOwner.gid}
	}
	return secureconfigwriter.Write(path, content, mode, expected, secureconfigwriter.Options{
		RequiredOwner:          requiredOwner,
		PublishedOwner:         publishedOwner,
		ParentValidator:        parentValidator,
		BeforeFinalParentProof: options.beforeFinalParentProof,
		PathRefused:            errConfigPathRefused,
	})
}

func secureRemoveConfig(path string) error {
	relative, err := secureConfigRelativePath(path)
	if err != nil {
		return err
	}
	parent := filepath.Dir(relative)
	base := filepath.Base(relative)

	rootFD, err := openSecureConfigRoot()
	if err != nil {
		return err
	}
	defer unix.Close(rootFD)

	parentFD, err := unix.Openat2(rootFD, parent, &unix.OpenHow{
		// A readable directory descriptor is required because the successful
		// unlink is followed by fsync(2). O_PATH descriptors cannot be synced
		// and would turn an otherwise successful removal into EBADF.
		Flags:   uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW),
		Resolve: secureConfigResolve,
	})
	if err != nil {
		return secureConfigOpenError("remove managed configuration", path, err)
	}
	defer unix.Close(parentFD)

	targetFD, err := unix.Openat2(parentFD, base, &unix.OpenHow{
		Flags:   uint64(unix.O_PATH | unix.O_CLOEXEC | unix.O_NOFOLLOW),
		Resolve: secureConfigResolve,
	})
	if err != nil {
		return secureConfigOpenError("remove managed configuration", path, err)
	}
	target := os.NewFile(uintptr(targetFD), path)
	if target == nil {
		unix.Close(targetFD)
		return fmt.Errorf("remove managed configuration %s: invalid file descriptor", path)
	}
	info, err := target.Stat()
	target.Close()
	if err != nil {
		return fmt.Errorf("stat managed configuration %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return configPathRefusal("remove managed configuration refused for non-regular file: %s", path)
	}

	if err := unix.Unlinkat(parentFD, base, 0); err != nil {
		return secureConfigOpenError("remove managed configuration", path, err)
	}
	if err := unix.Fsync(parentFD); err != nil {
		return fmt.Errorf("sync managed configuration directory %s: %w", filepath.Dir(path), err)
	}
	return nil
}
