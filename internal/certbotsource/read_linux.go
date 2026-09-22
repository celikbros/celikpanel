//go:build linux

// Package certbotsource reads the native Certbot live/archive layout. It grants
// no ownership or publication authority and performs no issuance or mutation.
package certbotsource

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	MaxFileSize = 1 << 20
	MaxLinkSize = 4096
	ResolveRoot = unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS
	resolveLive = unix.RESOLVE_BENEATH | unix.RESOLVE_NO_MAGICLINKS
)

// Reader is an internal trusted-caller configuration. Production uses the fixed
// /etc/letsencrypt root, UID/GID zero and unix.Openat2. Alternate inputs let the
// actual Agent filesystem/error tests exercise this same reader in private trees.
type Reader struct {
	Root     string
	UID, GID uint32
	Openat2  func(int, string, *unix.OpenHow) (int, error)
}
type sourceFile struct {
	content  []byte
	revision string
}

var managedLineage = regexp.MustCompile(`^celikpanel-(panel|mail)-[a-f0-9]{24}$`)

// ReadPair reads one matching archive revision. The caller must first derive
// lineage from its approved purpose/domain, then verify key, domain, trust and
// lifetime before using the returned private bytes. Never log those bytes.
func (r Reader) ReadPair(lineage string) ([]byte, []byte, error) {
	if !managedLineage.MatchString(lineage) || r.Openat2 == nil {
		return nil, nil, errors.New("invalid managed Certbot reader or lineage")
	}
	root, err := r.openRoot()
	if err != nil {
		return nil, nil, err
	}
	defer unix.Close(root)
	live, err := r.openOwnedDirectory(root, path.Join("live", lineage))
	if err != nil {
		return nil, nil, fmt.Errorf("open panel certificate live lineage: %w", err)
	}
	defer unix.Close(live)
	archive, err := r.openOwnedDirectory(root, path.Join("archive", lineage))
	if err != nil {
		return nil, nil, fmt.Errorf("open panel certificate archive lineage: %w", err)
	}
	defer unix.Close(archive)
	certificate, err := r.readFile(root, live, archive, lineage, "fullchain.pem", "fullchain", false)
	if err != nil {
		return nil, nil, fmt.Errorf("read panel certificate source: %w", err)
	}
	key, err := r.readFile(root, live, archive, lineage, "privkey.pem", "privkey", true)
	if err != nil {
		return nil, nil, fmt.Errorf("read panel private-key source: %w", err)
	}
	if certificate.revision != key.revision {
		return nil, nil, errors.New("panel certificate source generation is inconsistent")
	}
	return bytes.Clone(certificate.content), bytes.Clone(key.content), nil
}

func (r Reader) openRoot() (int, error) {
	clean := filepath.Clean(r.Root)
	if clean != r.Root || !filepath.IsAbs(clean) || clean == string(os.PathSeparator) {
		return -1, errors.New("panel certificate source root must be an absolute canonical path")
	}
	slashFD, err := unix.Open(string(os.PathSeparator), unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, fmt.Errorf("open panel certificate filesystem root: %w", err)
	}
	defer unix.Close(slashFD)
	rootFD, err := r.OpenAt(
		slashFD,
		filepath.ToSlash(strings.TrimPrefix(clean, string(os.PathSeparator))),
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		ResolveRoot,
	)
	if err != nil {
		return -1, fmt.Errorf("open panel certificate source root: %w", err)
	}
	if err := r.validateDirectoryFD(rootFD); err != nil {
		unix.Close(rootFD)
		return -1, fmt.Errorf("authenticate panel certificate source root: %w", err)
	}
	return rootFD, nil
}

func (r Reader) openOwnedDirectory(rootFD int, relative string) (int, error) {
	if relative == "" || relative == "." || path.IsAbs(relative) || path.Clean(relative) != relative {
		return -1, errors.New("panel certificate source directory must be canonical and relative")
	}
	currentFD, err := unix.Dup(rootFD)
	if err != nil {
		return -1, fmt.Errorf("duplicate panel certificate source root: %w", err)
	}
	unix.CloseOnExec(currentFD)
	for _, component := range strings.Split(relative, "/") {
		if component == "" || component == "." || component == ".." {
			unix.Close(currentFD)
			return -1, errors.New("invalid panel certificate source directory component")
		}
		nextFD, openErr := r.OpenAt(
			currentFD,
			component,
			unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
			ResolveRoot,
		)
		unix.Close(currentFD)
		if openErr != nil {
			return -1, openErr
		}
		if err := r.validateDirectoryFD(nextFD); err != nil {
			unix.Close(nextFD)
			return -1, err
		}
		currentFD = nextFD
	}
	return currentFD, nil
}

func (r Reader) validateDirectoryFD(fd int) error {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return fmt.Errorf("stat panel certificate source directory: %w", err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		return errors.New("panel certificate source path is not a directory")
	}
	if stat.Uid != r.UID || stat.Gid != r.GID {
		return errors.New("panel certificate source directory is not root-owned")
	}
	if stat.Mode&0o022 != 0 {
		return errors.New("panel certificate source directory is group/other writable")
	}
	return nil
}

func (r Reader) readFile(
	rootFD, liveFD, archiveFD int,
	lineage, liveName, archivePrefix string,
	privateKey bool,
) (sourceFile, error) {
	var linkStat unix.Stat_t
	if err := unix.Fstatat(liveFD, liveName, &linkStat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return sourceFile{}, fmt.Errorf("stat Certbot live link: %w", err)
	}
	if linkStat.Mode&unix.S_IFMT != unix.S_IFLNK {
		return sourceFile{}, errors.New("Certbot live certificate entry is not a symbolic link")
	}
	if linkStat.Uid != r.UID || linkStat.Gid != r.GID {
		return sourceFile{}, errors.New("Certbot live certificate link is not root-owned")
	}
	target, err := readLink(liveFD, liveName)
	if err != nil {
		return sourceFile{}, err
	}
	archiveName, revision, err := validateLinkTarget(target, lineage, archivePrefix)
	if err != nil {
		return sourceFile{}, err
	}

	archiveFileFD, err := r.OpenAt(
		archiveFD,
		archiveName,
		unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NONBLOCK|unix.O_NOFOLLOW,
		ResolveRoot,
	)
	if err != nil {
		return sourceFile{}, fmt.Errorf("open Certbot archive certificate: %w", err)
	}
	defer unix.Close(archiveFileFD)
	liveFileFD, err := r.OpenAt(
		rootFD,
		path.Join("live", lineage, liveName),
		unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NONBLOCK,
		resolveLive,
	)
	if err != nil {
		return sourceFile{}, fmt.Errorf("resolve Certbot live certificate safely: %w", err)
	}
	defer unix.Close(liveFileFD)

	archiveStat, err := r.ValidateFileFD(archiveFileFD, privateKey)
	if err != nil {
		return sourceFile{}, err
	}
	liveStat, err := r.ValidateFileFD(liveFileFD, privateKey)
	if err != nil {
		return sourceFile{}, err
	}
	if archiveStat.Dev != liveStat.Dev || archiveStat.Ino != liveStat.Ino {
		return sourceFile{}, errors.New("Certbot live certificate link changed or targets another lineage")
	}
	content, err := readFD(archiveFileFD, archiveStat)
	if err != nil {
		return sourceFile{}, err
	}
	return sourceFile{content: content, revision: revision}, nil
}

func readLink(directoryFD int, name string) (string, error) {
	buffer := make([]byte, MaxLinkSize)
	n, err := unix.Readlinkat(directoryFD, name, buffer)
	if err != nil {
		return "", fmt.Errorf("read Certbot live certificate link: %w", err)
	}
	if n == 0 || n == len(buffer) || bytes.IndexByte(buffer[:n], 0) >= 0 {
		return "", errors.New("Certbot live certificate link target is invalid")
	}
	return string(buffer[:n]), nil
}

func validateLinkTarget(target, lineage, archivePrefix string) (string, string, error) {
	if target == "" || path.IsAbs(target) || path.Clean(target) != target {
		return "", "", errors.New("Certbot live certificate link target is not canonical and relative")
	}
	resolved := path.Clean(path.Join("live", lineage, target))
	if path.Dir(resolved) != path.Join("archive", lineage) {
		return "", "", errors.New("Certbot live certificate link leaves its archive lineage")
	}
	archiveName := path.Base(resolved)
	revision, ok := revision(archiveName, archivePrefix)
	if !ok {
		return "", "", errors.New("Certbot live certificate link has an invalid archive generation")
	}
	return archiveName, revision, nil
}

func revision(name, prefix string) (string, bool) {
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".pem") {
		return "", false
	}
	revision := strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".pem")
	if revision == "" || revision[0] < '1' || revision[0] > '9' {
		return "", false
	}
	for _, character := range revision {
		if character < '0' || character > '9' {
			return "", false
		}
	}
	return revision, true
}

func (r Reader) ValidateFileFD(fd int, privateKey bool) (unix.Stat_t, error) {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return unix.Stat_t{}, fmt.Errorf("stat panel certificate source file: %w", err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return unix.Stat_t{}, errors.New("panel certificate source is not a regular file")
	}
	if stat.Uid != r.UID || stat.Gid != r.GID {
		return unix.Stat_t{}, errors.New("panel certificate source file is not root-owned")
	}
	if stat.Nlink != 1 {
		return unix.Stat_t{}, errors.New("panel certificate source file is not single-link")
	}
	if stat.Mode&0o022 != 0 {
		return unix.Stat_t{}, errors.New("panel certificate source file is group/other writable")
	}
	if privateKey {
		permissions := stat.Mode & 0o7777
		if permissions&0o400 == 0 {
			return unix.Stat_t{}, errors.New("panel certificate source private key is not owner-readable")
		}
		if permissions&(0o077|0o100|0o7000) != 0 {
			return unix.Stat_t{}, errors.New("panel certificate source private key has unsafe permissions")
		}
	}
	if stat.Size < 1 || stat.Size > MaxFileSize {
		return unix.Stat_t{}, errors.New("panel certificate source file has invalid size")
	}
	return stat, nil
}

func readFD(fd int, before unix.Stat_t) ([]byte, error) {
	duplicate, err := unix.Dup(fd)
	if err != nil {
		return nil, fmt.Errorf("duplicate panel certificate source file: %w", err)
	}
	unix.CloseOnExec(duplicate)
	file := os.NewFile(uintptr(duplicate), "panel-certificate-source")
	if file == nil {
		unix.Close(duplicate)
		return nil, errors.New("read panel certificate source: invalid file descriptor")
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, MaxFileSize+1))
	if err != nil {
		return nil, fmt.Errorf("read panel certificate source file: %w", err)
	}
	if len(content) > MaxFileSize || int64(len(content)) != before.Size {
		return nil, errors.New("panel certificate source file changed while it was read")
	}
	var after unix.Stat_t
	if err := unix.Fstat(fd, &after); err != nil {
		return nil, fmt.Errorf("restat panel certificate source file: %w", err)
	}
	if before.Dev != after.Dev || before.Ino != after.Ino || before.Size != after.Size || before.Mtim != after.Mtim || before.Ctim != after.Ctim {
		return nil, errors.New("panel certificate source file changed while it was read")
	}
	return content, nil
}

func (r Reader) OpenAt(directoryFD int, relative string, flags int, resolve uint64) (int, error) {
	if r.Openat2 == nil {
		return -1, errors.New("secure certificate source resolver is unavailable")
	}
	fd, err := r.Openat2(directoryFD, relative, &unix.OpenHow{
		Flags: uint64(flags), Resolve: resolve,
	})
	if errors.Is(err, unix.ENOSYS) {
		return -1, fmt.Errorf("secure panel certificate source access requires Linux openat2: %w", err)
	}
	return fd, err
}
