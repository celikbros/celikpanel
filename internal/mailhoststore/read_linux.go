//go:build linux

// Package mailhoststore reads historical native mail certificate generations.
// The caller pins a trusted parent descriptor and holds its publication lock.
// Reading evidence never grants renewal, configuration or service authority.
package mailhoststore

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/hostname"
	"github.com/alicelik/celikpanel/internal/mailhostartifact"
	"golang.org/x/sys/unix"
)

const PEMMaxSize = 1 << 20
const secureResolve = unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS

type PairVerifier func([]byte, []byte, string) ([]byte, time.Time, error)

func canonicalDomain(value string) bool {
	s, e := hostname.CanonicalFQDN(value)
	return e == nil && s == value
}
func validVersionName(name string) bool {
	const prefix = ".panel-cert-"
	if name != filepath.Base(name) || len(name) != len(prefix)+32 || !strings.HasPrefix(name, prefix) {
		return false
	}
	_, err := hex.DecodeString(name[len(prefix):])
	return err == nil
}
func openDirectoryAt(parent int, name string) (int, error) {
	fd, err := unix.Openat2(parent, name, &unix.OpenHow{Flags: uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW), Resolve: secureResolve})
	if err != nil {
		return -1, err
	}
	var st unix.Stat_t
	if err = unix.Fstat(fd, &st); err != nil || st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Uid != 0 || st.Mode&0022 != 0 {
		unix.Close(fd)
		return -1, errors.New("mail host certificate version directory is not root-authenticated")
	}
	return fd, nil
}

func ReadCurrentAt(
	dirFD int,
	verify PairVerifier,
) (
	version string,
	receipt mailhostartifact.Receipt,
	leafDER []byte,
	notAfter time.Time,
	found bool,
	err error,
) {
	version, currentFound, err := readCurrentVersionAt(dirFD)
	if err != nil || !currentFound {
		return "", mailhostartifact.Receipt{}, nil, time.Time{}, false, err
	}
	var currentBefore unix.Stat_t
	if err = unix.Fstatat(dirFD, "current", &currentBefore, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return version, mailhostartifact.Receipt{}, nil, time.Time{}, false, err
	}
	versionFD, err := openDirectoryAt(dirFD, version)
	if err != nil {
		return "", mailhostartifact.Receipt{}, nil, time.Time{}, false,
			fmt.Errorf("open current mail host certificate version: %w", err)
	}
	defer unix.Close(versionFD)
	receipt, receiptFound, err := ReadReceiptAt(versionFD)
	if err != nil {
		return version, mailhostartifact.Receipt{}, nil, time.Time{}, false, err
	}
	if !receiptFound {
		return version, mailhostartifact.Receipt{}, nil, time.Time{}, false, errors.New("current host certificate lacks its exact receipt")
	}

	leafDER, notAfter, err = VerifyVersionAt(
		versionFD, receipt, verify,
	)
	if err != nil {
		return version, mailhostartifact.Receipt{}, nil, time.Time{}, false, err
	}
	var currentAfter, openedVersion, namedVersion unix.Stat_t
	next, stillFound, linkErr := readCurrentVersionAt(dirFD)
	if linkErr != nil || !stillFound || next != version || unix.Fstatat(dirFD, "current", &currentAfter, unix.AT_SYMLINK_NOFOLLOW) != nil || !sameEvidenceStat(currentBefore, currentAfter) || unix.Fstat(versionFD, &openedVersion) != nil || unix.Fstatat(dirFD, version, &namedVersion, unix.AT_SYMLINK_NOFOLLOW) != nil || !sameEvidenceStat(openedVersion, namedVersion) {
		return version, mailhostartifact.Receipt{}, nil, time.Time{}, false, errors.New("current mail host certificate generation changed while read")
	}
	return version, receipt, leafDER, notAfter, true, nil
}

func ReadReceiptAt(
	versionFD int,
) (mailhostartifact.Receipt, bool, error) {
	fd, err := unix.Openat2(
		versionFD,
		mailhostartifact.ReceiptName,
		&unix.OpenHow{
			Flags: uint64(
				unix.O_RDONLY |
					unix.O_CLOEXEC |
					unix.O_NOFOLLOW |
					unix.O_NONBLOCK,
			),
			Resolve: secureResolve,
		},
	)
	if errors.Is(err, unix.ENOENT) {
		return mailhostartifact.Receipt{}, false, nil
	}
	if err != nil {
		return mailhostartifact.Receipt{}, false, fmt.Errorf(
			"open mail host certificate issue receipt: %w", err,
		)
	}
	file := os.NewFile(uintptr(fd), mailhostartifact.ReceiptName)
	if file == nil {
		unix.Close(fd)
		return mailhostartifact.Receipt{}, false, errors.New(
			"open mail host certificate issue receipt: invalid descriptor",
		)
	}
	defer file.Close()
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return mailhostartifact.Receipt{}, false, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG ||
		stat.Uid != 0 ||
		stat.Gid != 0 ||
		stat.Mode&0o7777 != 0o600 ||
		stat.Nlink != 1 ||
		stat.Size < 1 ||
		stat.Size > mailhostartifact.ReceiptMaxSize {
		return mailhostartifact.Receipt{}, false, errors.New(
			"mail host certificate issue receipt must be root-owned single-link 0600",
		)
	}
	raw, err := io.ReadAll(io.LimitReader(
		file, mailhostartifact.ReceiptMaxSize+1,
	))
	if err != nil {
		return mailhostartifact.Receipt{}, false, err
	}
	if int64(len(raw)) != stat.Size {
		return mailhostartifact.Receipt{}, false, errors.New(
			"mail host certificate issue receipt changed while read",
		)
	}
	if err = verifyReadIdentity(versionFD, mailhostartifact.ReceiptName, fd, stat); err != nil {
		return mailhostartifact.Receipt{}, false, err
	}
	receipt, err := mailhostartifact.DecodeReceipt(raw)
	return receipt, err == nil, err
}

func VerifyVersionAt(
	versionFD int,
	receipt mailhostartifact.Receipt,
	verify PairVerifier,
) ([]byte, time.Time, error) {
	if verify == nil {
		return nil, time.Time{}, errors.New("mail host certificate verifier is required")
	}
	domain, err := ReadDomainAt(versionFD, 0)
	if err != nil {
		return nil, time.Time{}, err
	}
	if domain != receipt.Domain {
		return nil, time.Time{}, errors.New(
			"mail host certificate issue receipt domain mismatch",
		)
	}
	certificate, err := ReadRegularFileAt(
		versionFD, "fullchain.pem", 0o600, PEMMaxSize,
	)
	if err != nil {
		return nil, time.Time{}, err
	}
	key, err := ReadRegularFileAt(versionFD, "privkey.pem", 0600, PEMMaxSize)
	if err != nil {
		return nil, time.Time{}, err
	}
	leaf, expires, err := verify(certificate, key, receipt.Domain)
	if err != nil {
		return nil, time.Time{}, err
	}
	if mailhostartifact.LeafSHA256(leaf) != receipt.LeafSHA256 {
		return nil, time.Time{}, errors.New("host certificate receipt leaf mismatch")
	}
	return leaf, expires, nil
}

func ReadRegularFileAt(
	dirFD int,
	name string,
	mode uint32,
	maxSize int64,
) ([]byte, error) {
	fd, err := unix.Openat2(dirFD, name, &unix.OpenHow{
		Flags: uint64(
			unix.O_RDONLY |
				unix.O_CLOEXEC |
				unix.O_NOFOLLOW |
				unix.O_NONBLOCK,
		),
		Resolve: secureResolve,
	})
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), name)
	if file == nil {
		unix.Close(fd)
		return nil, errors.New("invalid mail host certificate file descriptor")
	}
	defer file.Close()
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return nil, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG ||
		stat.Uid != 0 ||
		stat.Mode&0o7777 != mode ||
		stat.Nlink != 1 ||
		stat.Size < 1 ||
		stat.Size > maxSize {
		return nil, errors.New(
			"mail host certificate issue version file is not trusted",
		)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxSize+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != stat.Size {
		return nil, errors.New(
			"mail host certificate issue version changed while read",
		)
	}
	if err = verifyReadIdentity(dirFD, name, fd, stat); err != nil {
		return nil, err
	}
	return data, nil
}

func ReadDomainAt(fd, uid int) (string, error) {
	if uid != 0 {
		return "", errors.New("host certificate owner must be root")
	}
	raw, err := ReadRegularFileAt(fd, "mail.domain", 0600, 254)
	if err != nil {
		return "", err
	}
	domain := strings.TrimSuffix(string(raw), "\n")
	if string(raw) != domain+"\n" || !canonicalDomain(domain) {
		return "", errors.New("invalid host certificate identity")
	}
	return domain, nil
}

func readCurrentVersionAt(dirFD int) (string, bool, error) {
	var stat unix.Stat_t
	if err := unix.Fstatat(
		dirFD, "current", &stat, unix.AT_SYMLINK_NOFOLLOW,
	); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return "", false, nil
		}
		return "", false, fmt.Errorf(
			"inspect current panel certificate link: %w", err,
		)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFLNK || stat.Uid != 0 {
		return "", false, errors.New(
			"current panel certificate link is not trusted",
		)
	}
	target := make([]byte, 256)
	n, err := unix.Readlinkat(dirFD, "current", target)
	if err != nil {
		return "", false, fmt.Errorf("read current panel certificate link: %w", err)
	}
	if n == len(target) {
		return "", false, errors.New("current panel certificate link is too long")
	}
	version := string(target[:n])
	if !validVersionName(version) {
		return "", false, errors.New(
			"current panel certificate link target is invalid",
		)
	}
	return version, true, nil
}

func sameEvidenceStat(a, b unix.Stat_t) bool {
	return a.Dev == b.Dev && a.Ino == b.Ino && a.Mode == b.Mode && a.Uid == b.Uid && a.Gid == b.Gid && a.Nlink == b.Nlink && a.Size == b.Size && a.Mtim == b.Mtim && a.Ctim == b.Ctim
}
func verifyReadIdentity(parent int, name string, fd int, before unix.Stat_t) error {
	var after, named unix.Stat_t
	if err := unix.Fstat(fd, &after); err != nil {
		return err
	}
	if err := unix.Fstatat(parent, name, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if !sameEvidenceStat(before, after) || !sameEvidenceStat(after, named) {
		return errors.New("mail host certificate evidence changed while read")
	}
	return nil
}
