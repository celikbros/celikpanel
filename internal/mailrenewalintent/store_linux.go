//go:build linux

package mailrenewalintent

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"

	"github.com/alicelik/celikpanel/internal/servicemutationledger"
	"golang.org/x/sys/unix"
)

func FileName(request string) (string, error) {
	if !validID(request) {
		return "", errors.New("invalid renewal before-image request")
	}
	return "mail-renewal-before-" + request + ".json", nil
}

// Write persists an immutable before-image under caller-held host, ledger and
// certificate publication exclusion. Existing owner evidence is never replaced.
// The caller's revalidate must prove fresh source/selection and accepted scope;
// possession of a digest or this filesystem API is not admission authority.
func Write(path string, value Before, gid uint32, revalidate func() error) error {
	return write(path, value, gid, revalidate, nil)
}
func write(path string, value Before, gid uint32, revalidate func() error, checkpoint func(string)) error {
	raw, err := Canonical(value)
	if err != nil {
		return err
	}
	name, err := FileName(value.RequestID)
	if err != nil {
		return err
	}
	if os.Geteuid() != 0 || !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Base(path) != name || revalidate == nil {
		return errors.New("invalid renewal before-image publication boundary")
	}
	parent := filepath.Dir(path)
	owner := servicemutationledger.FileOwner{UID: 0, GID: gid}
	dirFD, err := unix.Openat2(unix.AT_FDCWD, parent, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC, Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS})
	if err != nil {
		return err
	}
	defer unix.Close(dirFD)
	var directory unix.Stat_t
	if unix.Fstat(dirFD, &directory) != nil || directory.Mode != unix.S_IFDIR|0700 || directory.Uid != 0 || directory.Gid != gid {
		return errors.New("renewal before-image requires existing protected state directory")
	}
	verify := func() error {
		if err := revalidate(); err != nil {
			return err
		}
		next, err := unix.Openat2(unix.AT_FDCWD, parent, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC, Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS})
		if err != nil {
			return err
		}
		defer unix.Close(next)
		var held, named unix.Stat_t
		if unix.Fstat(dirFD, &held) != nil || unix.Fstat(next, &named) != nil || held.Dev != directory.Dev || held.Ino != directory.Ino || named.Dev != held.Dev || named.Ino != held.Ino || held.Mode != directory.Mode || held.Uid != 0 || held.Gid != gid || named.Mode != held.Mode || named.Uid != held.Uid || named.Gid != held.Gid {
			return errors.New("renewal before-image parent changed")
		}
		return nil
	}
	if err = verify(); err != nil {
		return err
	}
	old, found, err := servicemutationledger.ReadFile(path, MaxSize, owner)
	if err != nil {
		return err
	}
	if found {
		if !bytes.Equal(raw, old) {
			return errors.New("renewal before-image differs; preserve the original operation and owner selection")
		}
		if err = verify(); err != nil {
			return err
		}
		if err = unix.Fsync(dirFD); err != nil {
			return err
		}
		again, present, err := servicemutationledger.ReadFile(path, MaxSize, owner)
		if err != nil {
			return err
		}
		if !present || !bytes.Equal(again, old) {
			return errors.New("renewal before-image changed during confirmation")
		}
		return verify()
	}
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return err
	}
	stage := ".mail-renewal-before-" + hex.EncodeToString(nonce[:]) + ".json"
	fileFD, err := unix.Openat(dirFD, stage, unix.O_RDWR|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fileFD), "renewal-before-stage")
	defer file.Close()
	if err = unix.Fchown(fileFD, 0, int(gid)); err != nil {
		return err
	}
	if err = file.Chmod(0600); err != nil {
		return err
	}
	if _, err = file.Write(raw); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if checkpoint != nil {
		checkpoint("staged")
	}
	if err = verify(); err != nil {
		return err
	}
	var staged unix.Stat_t
	if unix.Fstat(fileFD, &staged) != nil {
		return errors.New("renewal before-image stage unavailable")
	}
	checkStage := func(entry string) error {
		var named unix.Stat_t
		if unix.Fstatat(dirFD, entry, &named, unix.AT_SYMLINK_NOFOLLOW) != nil || staged.Dev != named.Dev || staged.Ino != named.Ino || named.Mode != unix.S_IFREG|0600 || named.Uid != 0 || named.Gid != gid || named.Nlink != 1 || named.Size != int64(len(raw)) {
			return errors.New("renewal before-image stage changed")
		}
		got, present, err := servicemutationledger.ReadFile(filepath.Join(parent, entry), MaxSize, owner)
		if err != nil {
			return err
		}
		if !present || !bytes.Equal(got, raw) {
			return errors.New("renewal before-image bytes changed")
		}
		return verify()
	}
	if err = checkStage(stage); err != nil {
		return err
	}
	if err = unix.Renameat2(dirFD, stage, dirFD, name, unix.RENAME_NOREPLACE); err != nil {
		return err
	}
	if checkpoint != nil {
		checkpoint("published")
	}
	if err = checkStage(name); err != nil {
		return err
	}
	if err = unix.Fsync(dirFD); err != nil {
		return err
	}
	if checkpoint != nil {
		checkpoint("parent_durable")
	}
	return checkStage(name)
}

// Read consumes only the canonical immutable record at its request-bound name.
// Missing evidence is reported as absent; callers must never synthesize it while
// reconciling an already active operation.
func Read(path string, gid uint32) (Before, bool, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return Before{}, false, errors.New("invalid renewal before-image path")
	}
	raw, found, err := servicemutationledger.ReadFile(path, MaxSize, servicemutationledger.FileOwner{UID: 0, GID: gid})
	if err != nil || !found {
		return Before{}, found, err
	}
	value, err := Decode(raw)
	if err != nil {
		return Before{}, true, err
	}
	name, err := FileName(value.RequestID)
	if err != nil || filepath.Base(path) != name {
		return Before{}, true, errors.New("renewal before-image filename differs from its request")
	}
	return value, true, nil
}
