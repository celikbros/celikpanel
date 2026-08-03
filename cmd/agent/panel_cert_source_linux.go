//go:build linux

package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const (
	panelCertificateSourceMaxFileSize = 1 << 20
	panelCertificateSourceMaxLinkSize = 4096
	panelCertificateSourceMinLifetime = 24 * time.Hour
	panelCertificateSourceResolveRoot = unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS
	panelCertificateSourceResolveLive = unix.RESOLVE_BENEATH | unix.RESOLVE_NO_MAGICLINKS
)

var (
	panelCertificateSourceRoot        = "/etc/letsencrypt"
	panelCertificateSourceExpectedUID = uint32(0)
	panelCertificateSourceExpectedGID = uint32(0)
	panelCertificateSourceOpenat2     = unix.Openat2
	panelCertificateSourceNow         = time.Now
	panelCertificateSourceSystemRoots = x509.SystemCertPool
)

type panelCertificateSourceFile struct {
	content  []byte
	revision string
}

// readPanelCertificateSource supports Certbot's normal live symlinks while
// confining their resolution to an authenticated Let's Encrypt root.
func readPanelCertificateSource(domain string) (
	certificate, privateKey, leafDER []byte,
	notAfter time.Time,
	err error,
) {
	if domain != strings.ToLower(strings.TrimSpace(domain)) || !validPanelCertDomain.MatchString(domain) {
		return nil, nil, nil, time.Time{}, errors.New("invalid panel certificate source domain")
	}
	lineage := panelCertLineageName(domain)
	if !validPanelCertLineage.MatchString(lineage) {
		return nil, nil, nil, time.Time{}, errors.New("invalid panel certificate source lineage")
	}

	rootFD, err := openPanelCertificateSourceRoot()
	if err != nil {
		return nil, nil, nil, time.Time{}, err
	}
	defer unix.Close(rootFD)
	liveFD, err := openPanelCertificateSourceOwnedDirectory(rootFD, path.Join("live", lineage))
	if err != nil {
		return nil, nil, nil, time.Time{}, fmt.Errorf("open panel certificate live lineage: %w", err)
	}
	defer unix.Close(liveFD)
	archiveFD, err := openPanelCertificateSourceOwnedDirectory(rootFD, path.Join("archive", lineage))
	if err != nil {
		return nil, nil, nil, time.Time{}, fmt.Errorf("open panel certificate archive lineage: %w", err)
	}
	defer unix.Close(archiveFD)

	certSource, err := readPanelCertificateSourceFile(rootFD, liveFD, archiveFD, lineage, "fullchain.pem", "fullchain", false)
	if err != nil {
		return nil, nil, nil, time.Time{}, fmt.Errorf("read panel certificate source: %w", err)
	}
	keySource, err := readPanelCertificateSourceFile(rootFD, liveFD, archiveFD, lineage, "privkey.pem", "privkey", true)
	if err != nil {
		return nil, nil, nil, time.Time{}, fmt.Errorf("read panel private-key source: %w", err)
	}
	if certSource.revision != keySource.revision {
		return nil, nil, nil, time.Time{}, errors.New("panel certificate source generation is inconsistent")
	}

	pair, err := tls.X509KeyPair(certSource.content, keySource.content)
	if err != nil {
		return nil, nil, nil, time.Time{}, fmt.Errorf("validate panel certificate source pair: %w", err)
	}
	if len(pair.Certificate) == 0 {
		return nil, nil, nil, time.Time{}, errors.New("validate panel certificate source identity: certificate chain is empty")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, nil, nil, time.Time{}, fmt.Errorf("parse panel certificate source leaf: %w", err)
	}
	now := panelCertificateSourceNow()
	if now.Before(leaf.NotBefore) || !now.Before(leaf.NotAfter) {
		return nil, nil, nil, time.Time{}, errors.New("panel certificate source leaf is not currently valid")
	}
	if leaf.NotAfter.Before(now.Add(panelCertificateSourceMinLifetime)) {
		return nil, nil, nil, time.Time{}, errors.New("panel certificate source leaf has insufficient remaining validity")
	}
	intermediates := x509.NewCertPool()
	for index, raw := range pair.Certificate[1:] {
		certificate, parseErr := x509.ParseCertificate(raw)
		if parseErr != nil {
			return nil, nil, nil, time.Time{}, fmt.Errorf(
				"parse panel certificate source intermediate %d: %w", index+1, parseErr,
			)
		}
		intermediates.AddCert(certificate)
	}
	roots, err := panelCertificateSourceSystemRoots()
	if err != nil {
		return nil, nil, nil, time.Time{}, fmt.Errorf("load panel certificate system trust roots: %w", err)
	}
	if roots == nil {
		return nil, nil, nil, time.Time{}, errors.New("load panel certificate system trust roots: no trust roots returned")
	}
	if _, err := leaf.Verify(x509.VerifyOptions{
		DNSName:       domain,
		Roots:         roots,
		Intermediates: intermediates,
		CurrentTime:   now,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}); err != nil {
		return nil, nil, nil, time.Time{}, fmt.Errorf("verify panel certificate source trust chain: %w", err)
	}
	return bytes.Clone(certSource.content), bytes.Clone(keySource.content), bytes.Clone(leaf.Raw), leaf.NotAfter, nil
}

func openPanelCertificateSourceRoot() (int, error) {
	clean := filepath.Clean(panelCertificateSourceRoot)
	if clean != panelCertificateSourceRoot || !filepath.IsAbs(clean) || clean == string(os.PathSeparator) {
		return -1, errors.New("panel certificate source root must be an absolute canonical path")
	}
	slashFD, err := unix.Open(string(os.PathSeparator), unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, fmt.Errorf("open panel certificate filesystem root: %w", err)
	}
	defer unix.Close(slashFD)
	rootFD, err := openPanelCertificateSourceAt(
		slashFD,
		filepath.ToSlash(strings.TrimPrefix(clean, string(os.PathSeparator))),
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		panelCertificateSourceResolveRoot,
	)
	if err != nil {
		return -1, fmt.Errorf("open panel certificate source root: %w", err)
	}
	if err := validatePanelCertificateSourceDirectoryFD(rootFD); err != nil {
		unix.Close(rootFD)
		return -1, fmt.Errorf("authenticate panel certificate source root: %w", err)
	}
	return rootFD, nil
}

func openPanelCertificateSourceOwnedDirectory(rootFD int, relative string) (int, error) {
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
		nextFD, openErr := openPanelCertificateSourceAt(
			currentFD,
			component,
			unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
			panelCertificateSourceResolveRoot,
		)
		unix.Close(currentFD)
		if openErr != nil {
			return -1, openErr
		}
		if err := validatePanelCertificateSourceDirectoryFD(nextFD); err != nil {
			unix.Close(nextFD)
			return -1, err
		}
		currentFD = nextFD
	}
	return currentFD, nil
}

func validatePanelCertificateSourceDirectoryFD(fd int) error {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return fmt.Errorf("stat panel certificate source directory: %w", err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		return errors.New("panel certificate source path is not a directory")
	}
	if stat.Uid != panelCertificateSourceExpectedUID || stat.Gid != panelCertificateSourceExpectedGID {
		return errors.New("panel certificate source directory is not root-owned")
	}
	if stat.Mode&0o022 != 0 {
		return errors.New("panel certificate source directory is group/other writable")
	}
	return nil
}

func readPanelCertificateSourceFile(
	rootFD, liveFD, archiveFD int,
	lineage, liveName, archivePrefix string,
	privateKey bool,
) (panelCertificateSourceFile, error) {
	var linkStat unix.Stat_t
	if err := unix.Fstatat(liveFD, liveName, &linkStat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return panelCertificateSourceFile{}, fmt.Errorf("stat Certbot live link: %w", err)
	}
	if linkStat.Mode&unix.S_IFMT != unix.S_IFLNK {
		return panelCertificateSourceFile{}, errors.New("Certbot live certificate entry is not a symbolic link")
	}
	if linkStat.Uid != panelCertificateSourceExpectedUID || linkStat.Gid != panelCertificateSourceExpectedGID {
		return panelCertificateSourceFile{}, errors.New("Certbot live certificate link is not root-owned")
	}
	target, err := readPanelCertificateSourceLink(liveFD, liveName)
	if err != nil {
		return panelCertificateSourceFile{}, err
	}
	archiveName, revision, err := validatePanelCertificateSourceLinkTarget(target, lineage, archivePrefix)
	if err != nil {
		return panelCertificateSourceFile{}, err
	}

	archiveFileFD, err := openPanelCertificateSourceAt(
		archiveFD,
		archiveName,
		unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NONBLOCK|unix.O_NOFOLLOW,
		panelCertificateSourceResolveRoot,
	)
	if err != nil {
		return panelCertificateSourceFile{}, fmt.Errorf("open Certbot archive certificate: %w", err)
	}
	defer unix.Close(archiveFileFD)
	liveFileFD, err := openPanelCertificateSourceAt(
		rootFD,
		path.Join("live", lineage, liveName),
		unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NONBLOCK,
		panelCertificateSourceResolveLive,
	)
	if err != nil {
		return panelCertificateSourceFile{}, fmt.Errorf("resolve Certbot live certificate safely: %w", err)
	}
	defer unix.Close(liveFileFD)

	archiveStat, err := validatePanelCertificateSourceFileFD(archiveFileFD, privateKey)
	if err != nil {
		return panelCertificateSourceFile{}, err
	}
	liveStat, err := validatePanelCertificateSourceFileFD(liveFileFD, privateKey)
	if err != nil {
		return panelCertificateSourceFile{}, err
	}
	if archiveStat.Dev != liveStat.Dev || archiveStat.Ino != liveStat.Ino {
		return panelCertificateSourceFile{}, errors.New("Certbot live certificate link changed or targets another lineage")
	}
	content, err := readPanelCertificateSourceFD(archiveFileFD, archiveStat)
	if err != nil {
		return panelCertificateSourceFile{}, err
	}
	return panelCertificateSourceFile{content: content, revision: revision}, nil
}

func readPanelCertificateSourceLink(directoryFD int, name string) (string, error) {
	buffer := make([]byte, panelCertificateSourceMaxLinkSize)
	n, err := unix.Readlinkat(directoryFD, name, buffer)
	if err != nil {
		return "", fmt.Errorf("read Certbot live certificate link: %w", err)
	}
	if n == 0 || n == len(buffer) || bytes.IndexByte(buffer[:n], 0) >= 0 {
		return "", errors.New("Certbot live certificate link target is invalid")
	}
	return string(buffer[:n]), nil
}

func validatePanelCertificateSourceLinkTarget(target, lineage, archivePrefix string) (string, string, error) {
	if target == "" || path.IsAbs(target) || path.Clean(target) != target {
		return "", "", errors.New("Certbot live certificate link target is not canonical and relative")
	}
	resolved := path.Clean(path.Join("live", lineage, target))
	if path.Dir(resolved) != path.Join("archive", lineage) {
		return "", "", errors.New("Certbot live certificate link leaves its archive lineage")
	}
	archiveName := path.Base(resolved)
	revision, ok := panelCertificateSourceRevision(archiveName, archivePrefix)
	if !ok {
		return "", "", errors.New("Certbot live certificate link has an invalid archive generation")
	}
	return archiveName, revision, nil
}

func panelCertificateSourceRevision(name, prefix string) (string, bool) {
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

func validatePanelCertificateSourceFileFD(fd int, privateKey bool) (unix.Stat_t, error) {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return unix.Stat_t{}, fmt.Errorf("stat panel certificate source file: %w", err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return unix.Stat_t{}, errors.New("panel certificate source is not a regular file")
	}
	if stat.Uid != panelCertificateSourceExpectedUID || stat.Gid != panelCertificateSourceExpectedGID {
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
	if stat.Size < 1 || stat.Size > panelCertificateSourceMaxFileSize {
		return unix.Stat_t{}, errors.New("panel certificate source file has invalid size")
	}
	return stat, nil
}

func readPanelCertificateSourceFD(fd int, before unix.Stat_t) ([]byte, error) {
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
	content, err := io.ReadAll(io.LimitReader(file, panelCertificateSourceMaxFileSize+1))
	if err != nil {
		return nil, fmt.Errorf("read panel certificate source file: %w", err)
	}
	if len(content) > panelCertificateSourceMaxFileSize || int64(len(content)) != before.Size {
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

func openPanelCertificateSourceAt(directoryFD int, relative string, flags int, resolve uint64) (int, error) {
	fd, err := panelCertificateSourceOpenat2(directoryFD, relative, &unix.OpenHow{
		Flags: uint64(flags), Resolve: resolve,
	})
	if errors.Is(err, unix.ENOSYS) {
		return -1, fmt.Errorf("secure panel certificate source access requires Linux openat2: %w", err)
	}
	return fd, err
}
