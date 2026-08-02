//go:build linux

package main

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

const panelCertSecureResolve = unix.RESOLVE_BENEATH |
	unix.RESOLVE_NO_SYMLINKS |
	unix.RESOLVE_NO_MAGICLINKS

const (
	managedPanelCertVersionPrefix  = ".panel-cert-"
	maxPanelCertificateDomainBytes = 256
)

func init() {
	panelCertificateActivationPublishMaterial = installPanelCertMaterial
}

func installPanelCertFiles(domain, tlsDir string) error {
	certificate, privateKey, _, _, err := readPanelCertificateSource(domain)
	if err != nil {
		return err
	}
	return installPanelCertMaterial(domain, tlsDir, certificate, privateKey)
}

func installPanelCertMaterial(
	domain, tlsDir string,
	certificate, privateKey []byte,
) error {
	if tlsDir != managedPanelTLSDir {
		return fmt.Errorf("invalid TLS directory")
	}
	if !validPanelCertDomain.MatchString(domain) {
		return fmt.Errorf("invalid panel certificate domain")
	}
	pair, err := tls.X509KeyPair(certificate, privateKey)
	if err != nil {
		return fmt.Errorf("validate panel certificate pair: %w", err)
	}
	if len(pair.Certificate) == 0 {
		return fmt.Errorf("validate panel certificate identity: certificate chain is empty")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return fmt.Errorf("validate panel certificate identity: %w", err)
	}
	if err := leaf.VerifyHostname(domain); err != nil {
		return fmt.Errorf("validate panel certificate identity: %w", err)
	}

	dirFD, panelGID, err := openManagedPanelTLSDirectory(tlsDir)
	if err != nil {
		return err
	}
	defer unix.Close(dirFD)
	if err := publishPanelCertificateVersion(
		dirFD, 0, panelGID, domain, certificate, privateKey,
	); err != nil {
		return fmt.Errorf("publish panel certificate pair: %w", err)
	}
	return nil
}

func openManagedPanelTLSDirectory(tlsDir string) (int, int, error) {
	panelUser, err := user.Lookup("celikpanel")
	if err != nil {
		return -1, -1, fmt.Errorf("lookup celikpanel user: %w", err)
	}
	panelUID, err := strconv.Atoi(panelUser.Uid)
	if err != nil {
		return -1, -1, fmt.Errorf("parse celikpanel uid: %w", err)
	}
	panelGID, ok := lookupGroupID("celikpanel")
	if !ok {
		return -1, -1, fmt.Errorf("lookup celikpanel group")
	}

	rootFD, err := unix.Open(
		"/", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0,
	)
	if err != nil {
		return -1, -1, fmt.Errorf("open filesystem root: %w", err)
	}
	defer unix.Close(rootFD)

	parentRelative := strings.TrimPrefix(filepath.Dir(tlsDir), "/")
	parentFD, err := unix.Openat2(rootFD, parentRelative, &unix.OpenHow{
		Flags: uint64(
			unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW,
		),
		Resolve: panelCertSecureResolve,
	})
	if err != nil {
		return -1, -1, fmt.Errorf("open panel data directory safely: %w", err)
	}
	defer unix.Close(parentFD)

	name := filepath.Base(tlsDir)
	dirFD, err := openPanelCertDirectoryAt(parentFD, name)
	if errors.Is(err, unix.ENOENT) {
		if err := unix.Mkdirat(parentFD, name, 0o750); err != nil &&
			!errors.Is(err, unix.EEXIST) {
			return -1, -1, fmt.Errorf("create panel TLS directory: %w", err)
		}
		dirFD, err = openPanelCertDirectoryAt(parentFD, name)
	}
	if err != nil {
		return -1, -1, fmt.Errorf("open panel TLS directory safely: %w", err)
	}

	var stat unix.Stat_t
	if err := unix.Fstat(dirFD, &stat); err != nil {
		unix.Close(dirFD)
		return -1, -1, fmt.Errorf("stat panel TLS directory: %w", err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		unix.Close(dirFD)
		return -1, -1, fmt.Errorf("panel TLS path is not a directory")
	}
	if int(stat.Uid) != 0 && int(stat.Uid) != panelUID {
		unix.Close(dirFD)
		return -1, -1, fmt.Errorf(
			"panel TLS directory has unexpected owner uid %d", stat.Uid,
		)
	}
	// Take ownership through the already-open descriptor. Even if the
	// unprivileged owner renames the directory concurrently, no path is
	// followed and all subsequent writes remain confined to this descriptor.
	if err := unix.Fchown(dirFD, 0, panelGID); err != nil {
		unix.Close(dirFD)
		return -1, -1, fmt.Errorf("own panel TLS directory: %w", err)
	}
	if err := unix.Fchmod(dirFD, 0o750); err != nil {
		unix.Close(dirFD)
		return -1, -1, fmt.Errorf("protect panel TLS directory: %w", err)
	}
	return dirFD, panelGID, nil
}

func openPanelCertDirectoryAt(parentFD int, name string) (int, error) {
	return unix.Openat2(parentFD, name, &unix.OpenHow{
		Flags: uint64(
			unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW,
		),
		Resolve: panelCertSecureResolve,
	})
}

func publishPanelCertificateVersion(
	dirFD, ownerUID, ownerGID int,
	domain string,
	certificate, privateKey []byte,
) (err error) {
	if !validPanelCertDomain.MatchString(domain) {
		return fmt.Errorf("invalid panel certificate identity")
	}
	versionName, err := randomPanelCertEntry(".panel-cert-")
	if err != nil {
		return err
	}
	if err := unix.Mkdirat(dirFD, versionName, 0o750); err != nil {
		return fmt.Errorf("create certificate version directory: %w", err)
	}
	versionFD, err := openPanelCertDirectoryAt(dirFD, versionName)
	if err != nil {
		_ = unix.Unlinkat(dirFD, versionName, unix.AT_REMOVEDIR)
		return fmt.Errorf("open certificate version directory: %w", err)
	}
	published := false
	defer func() {
		if !published {
			_ = unix.Unlinkat(versionFD, "panel.crt", 0)
			_ = unix.Unlinkat(versionFD, "panel.key", 0)
			_ = unix.Unlinkat(versionFD, "panel.domain", 0)
			unix.Close(versionFD)
			_ = unix.Unlinkat(dirFD, versionName, unix.AT_REMOVEDIR)
			return
		}
		unix.Close(versionFD)
	}()
	if err := unix.Fchown(versionFD, ownerUID, ownerGID); err != nil {
		return fmt.Errorf("own certificate version directory: %w", err)
	}
	if err := unix.Fchmod(versionFD, 0o750); err != nil {
		return fmt.Errorf("protect certificate version directory: %w", err)
	}
	if err := writePanelCertificateFile(
		versionFD, "panel.crt", ownerUID, ownerGID, 0o640, certificate,
	); err != nil {
		return err
	}
	if err := writePanelCertificateFile(
		versionFD, "panel.key", ownerUID, ownerGID, 0o640, privateKey,
	); err != nil {
		return err
	}
	// The root-only identity marker is committed in the same immutable
	// version as the key pair. Renewal authorization therefore cannot be
	// forged by the unprivileged panel user or drift from the active pair.
	if err := writePanelCertificateFile(
		versionFD, "panel.domain", ownerUID, ownerGID, 0o600,
		[]byte(domain+"\n"),
	); err != nil {
		return err
	}
	if err := unix.Fsync(versionFD); err != nil {
		return fmt.Errorf("sync certificate version directory: %w", err)
	}

	linkName, err := randomPanelCertEntry(".current-")
	if err != nil {
		return err
	}
	if err := unix.Symlinkat(versionName, dirFD, linkName); err != nil {
		return fmt.Errorf("create active certificate link: %w", err)
	}
	linkPublished := false
	defer func() {
		if !linkPublished {
			_ = unix.Unlinkat(dirFD, linkName, 0)
		}
	}()
	if err := unix.Renameat(dirFD, linkName, dirFD, "current"); err != nil {
		return fmt.Errorf("activate certificate pair atomically: %w", err)
	}
	linkPublished = true
	published = true
	if err := unix.Fsync(dirFD); err != nil {
		return fmt.Errorf("sync panel TLS directory: %w", err)
	}
	return nil
}

func writePanelCertificateFile(
	dirFD int,
	name string,
	ownerUID, ownerGID int,
	mode uint32,
	content []byte,
) error {
	fd, err := unix.Openat(
		dirFD,
		name,
		unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		mode,
	)
	if err != nil {
		return fmt.Errorf("create %s: %w", name, err)
	}
	file := os.NewFile(uintptr(fd), name)
	if file == nil {
		unix.Close(fd)
		return fmt.Errorf("create %s: invalid file descriptor", name)
	}
	closed := false
	defer func() {
		if !closed {
			_ = file.Close()
		}
	}()
	if err := unix.Fchown(fd, ownerUID, ownerGID); err != nil {
		return fmt.Errorf("own %s: %w", name, err)
	}
	if err := unix.Fchmod(fd, mode); err != nil {
		return fmt.Errorf("protect %s: %w", name, err)
	}
	if _, err := io.Copy(file, bytes.NewReader(content)); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync %s: %w", name, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close %s: %w", name, err)
	}
	closed = true
	return nil
}

func randomPanelCertEntry(prefix string) (string, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate certificate version name: %w", err)
	}
	return prefix + hex.EncodeToString(random), nil
}

// deployRenewedPanelCertFiles publishes a renewed managed lineage only when
// its deterministic name matches the root-authenticated identity currently
// served by the panel. Unrelated global certbot lineages are safe no-ops.
func deployRenewedPanelCertFiles(lineageName, tlsDir string) (bool, error) {
	if tlsDir != managedPanelTLSDir || !validPanelCertLineage.MatchString(lineageName) {
		return false, fmt.Errorf("invalid panel certificate renewal request")
	}
	return enqueueRenewedPanelCertificateActivation(lineageName)
}

func withPanelCertPublishLock(action func() error) error {
	if action == nil {
		return fmt.Errorf("panel certificate publication action is required")
	}
	const lockPath = "/run/celikpanel-panel-cert.lock"
	fd, err := unix.Open(
		lockPath,
		unix.O_RDWR|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0o600,
	)
	if err != nil {
		return fmt.Errorf("open panel certificate publication lock: %w", err)
	}
	defer unix.Close(fd)
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return fmt.Errorf("stat panel certificate publication lock: %w", err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Uid != 0 ||
		stat.Nlink != 1 || stat.Mode&0o077 != 0 {
		return fmt.Errorf("panel certificate publication lock is not trusted")
	}
	if err := unix.Fchmod(fd, 0o600); err != nil {
		return fmt.Errorf("protect panel certificate publication lock: %w", err)
	}
	if err := unix.Flock(fd, unix.LOCK_EX); err != nil {
		return fmt.Errorf("lock panel certificate publication: %w", err)
	}
	defer unix.Flock(fd, unix.LOCK_UN)
	return action()
}

func activePanelCertificateIdentity(tlsDir string) (string, bool, error) {
	if tlsDir != managedPanelTLSDir {
		return "", false, fmt.Errorf("invalid active panel certificate request")
	}
	dirFD, err := openTrustedPanelTLSDirectoryOwned(tlsDir, 0)
	if err != nil {
		return "", false, err
	}
	defer unix.Close(dirFD)
	return readActivePanelCertificateIdentityAt(dirFD, 0)
}

func openTrustedPanelTLSDirectoryOwned(tlsDir string, expectedOwnerUID int) (int, error) {
	rootFD, err := unix.Open(
		"/", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0,
	)
	if err != nil {
		return -1, fmt.Errorf("open filesystem root: %w", err)
	}
	defer unix.Close(rootFD)
	dirFD, err := unix.Openat2(rootFD, strings.TrimPrefix(tlsDir, "/"), &unix.OpenHow{
		Flags: uint64(
			unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW,
		),
		Resolve: panelCertSecureResolve,
	})
	if err != nil {
		return -1, fmt.Errorf("open trusted panel TLS directory: %w", err)
	}
	var stat unix.Stat_t
	if err := unix.Fstat(dirFD, &stat); err != nil {
		unix.Close(dirFD)
		return -1, fmt.Errorf("stat trusted panel TLS directory: %w", err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR ||
		int(stat.Uid) != expectedOwnerUID || stat.Mode&0o022 != 0 {
		unix.Close(dirFD)
		return -1, fmt.Errorf("panel TLS directory is not root-authenticated")
	}
	return dirFD, nil
}

func readActivePanelCertificateIdentityAt(
	dirFD, expectedOwnerUID int,
) (string, bool, error) {
	var linkStat unix.Stat_t
	if err := unix.Fstatat(
		dirFD, "current", &linkStat, unix.AT_SYMLINK_NOFOLLOW,
	); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("inspect active panel certificate link: %w", err)
	}
	if linkStat.Mode&unix.S_IFMT != unix.S_IFLNK ||
		int(linkStat.Uid) != expectedOwnerUID {
		return "", false, fmt.Errorf("active panel certificate link is not trusted")
	}
	linkTarget := make([]byte, 256)
	n, err := unix.Readlinkat(dirFD, "current", linkTarget)
	if err != nil {
		return "", false, fmt.Errorf("read active panel certificate link: %w", err)
	}
	if n == len(linkTarget) {
		return "", false, fmt.Errorf("active panel certificate link is too long")
	}
	version := string(linkTarget[:n])
	if !validManagedPanelCertVersionName(version) {
		return "", false, fmt.Errorf("active panel certificate link has an invalid target")
	}
	versionFD, err := openPanelCertDirectoryAt(dirFD, version)
	if err != nil {
		return "", false, fmt.Errorf("open active panel certificate version: %w", err)
	}
	defer unix.Close(versionFD)
	var versionStat unix.Stat_t
	if err := unix.Fstat(versionFD, &versionStat); err != nil {
		return "", false, fmt.Errorf("stat active panel certificate version: %w", err)
	}
	if versionStat.Mode&unix.S_IFMT != unix.S_IFDIR ||
		int(versionStat.Uid) != expectedOwnerUID || versionStat.Mode&0o022 != 0 {
		return "", false, fmt.Errorf("active panel certificate version is not trusted")
	}
	domain, err := readPanelCertificateDomainAt(versionFD, expectedOwnerUID)
	if err != nil {
		return "", false, err
	}
	return domain, true, nil
}

func validManagedPanelCertVersionName(name string) bool {
	if name != filepath.Base(name) ||
		len(name) != len(managedPanelCertVersionPrefix)+32 ||
		!strings.HasPrefix(name, managedPanelCertVersionPrefix) {
		return false
	}
	_, err := hex.DecodeString(name[len(managedPanelCertVersionPrefix):])
	return err == nil
}

func readPanelCertificateDomainAt(dirFD, expectedOwnerUID int) (string, error) {
	fd, err := unix.Openat2(dirFD, "panel.domain", &unix.OpenHow{
		Flags:   uint64(unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW),
		Resolve: panelCertSecureResolve,
	})
	if err != nil {
		return "", fmt.Errorf("open active panel certificate identity: %w", err)
	}
	file := os.NewFile(uintptr(fd), "panel.domain")
	if file == nil {
		unix.Close(fd)
		return "", fmt.Errorf("invalid panel certificate identity descriptor")
	}
	defer file.Close()
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return "", fmt.Errorf("stat active panel certificate identity: %w", err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG ||
		int(stat.Uid) != expectedOwnerUID || stat.Mode&0o777 != 0o600 {
		return "", fmt.Errorf("active panel certificate identity is not trusted")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxPanelCertificateDomainBytes+1))
	if err != nil {
		return "", fmt.Errorf("read active panel certificate identity: %w", err)
	}
	if len(data) > maxPanelCertificateDomainBytes {
		return "", fmt.Errorf("active panel certificate identity exceeds size limit")
	}
	domain := strings.TrimSuffix(string(data), "\n")
	if string(data) != domain+"\n" || !validPanelCertDomain.MatchString(domain) {
		return "", fmt.Errorf("active panel certificate identity is invalid")
	}
	return domain, nil
}

func writePanelCertDeployHook(domain, tlsDir string) error {
	if tlsDir != managedPanelTLSDir || !validPanelCertDomain.MatchString(domain) {
		return fmt.Errorf("invalid panel certificate deploy hook request")
	}
	const hookDir = "/etc/letsencrypt/renewal-hooks/deploy"
	if err := ensureRootOwnedPanelCertHookDirectory(hookDir); err != nil {
		return err
	}

	script := renderPanelCertDeployHook()
	hookPath := filepath.Join(hookDir, "celikpanel-panel-cert")
	if err := publishPanelCertDeployHook(hookDir, filepath.Base(hookPath), []byte(script)); err != nil {
		return fmt.Errorf("write certbot deploy hook: %w", err)
	}
	if err := protectPanelCertDeployHook(hookPath); err != nil {
		return err
	}
	return nil
}

func renderPanelCertDeployHook() string {
	return `#!/bin/sh
set -eu
# Managed by CelikPanel. The agent publishes only the currently active panel
# identity; renewals for unrelated global certbot lineages are safe no-ops.
lineage=${RENEWED_LINEAGE:-}
case "$lineage" in
  /etc/letsencrypt/live/celikpanel-panel-*)
    lineage_name=${lineage#/etc/letsencrypt/live/}
    case "$lineage_name" in
      ""|*/*) exit 0 ;;
    esac
    exec /opt/celikpanel/bin/agent --deploy-panel-certificate "$lineage_name"
    ;;
esac
exit 0
`
}

// publishPanelCertDeployHook prepares the replacement inode with its final
// root-only ownership and executable mode before it becomes reachable at the
// well-known certbot hook path. Preserving metadata from an older hook here is
// unsafe: a process that could open an attacker-owned predecessor must never
// retain a writable descriptor to the newly published root hook.
func publishPanelCertDeployHook(dirPath, base string, content []byte) error {
	return publishPanelCertDeployHookOwned(dirPath, base, content, 0, 0)
}

func publishPanelCertDeployHookOwned(
	dirPath, base string,
	content []byte,
	ownerUID, ownerGID int,
) error {
	if filepath.Base(base) != base || base == "." || base == "" {
		return fmt.Errorf("invalid certbot deploy hook name")
	}
	relative, err := secureConfigRelativePath(dirPath)
	if err != nil {
		return err
	}
	rootFD, err := openSecureConfigRoot()
	if err != nil {
		return err
	}
	defer unix.Close(rootFD)

	dirFD, err := unix.Openat2(rootFD, relative, &unix.OpenHow{
		Flags: uint64(
			unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW,
		),
		Resolve: secureConfigResolve,
	})
	if err != nil {
		return secureConfigOpenError("open certbot deploy hook directory", dirPath, err)
	}
	defer unix.Close(dirFD)

	tempName, err := randomPanelCertEntry("." + base + ".celikpanel-")
	if err != nil {
		return err
	}
	fd, err := unix.Openat(
		dirFD,
		tempName,
		unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0o600,
	)
	if err != nil {
		return secureConfigOpenError("create certbot deploy hook replacement", filepath.Join(dirPath, tempName), err)
	}
	published := false
	defer func() {
		if !published {
			_ = unix.Unlinkat(dirFD, tempName, 0)
		}
	}()

	file := os.NewFile(uintptr(fd), filepath.Join(dirPath, tempName))
	if file == nil {
		unix.Close(fd)
		return fmt.Errorf("create certbot deploy hook replacement: invalid file descriptor")
	}
	closed := false
	defer func() {
		if !closed {
			_ = file.Close()
		}
	}()
	if err := unix.Fchown(fd, ownerUID, ownerGID); err != nil {
		return fmt.Errorf("own certbot deploy hook replacement: %w", err)
	}
	if err := unix.Fchmod(fd, 0o755); err != nil {
		return fmt.Errorf("protect certbot deploy hook replacement: %w", err)
	}
	if _, err := io.Copy(file, bytes.NewReader(content)); err != nil {
		return fmt.Errorf("write certbot deploy hook replacement: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync certbot deploy hook replacement: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close certbot deploy hook replacement: %w", err)
	}
	closed = true
	if err := unix.Renameat(dirFD, tempName, dirFD, base); err != nil {
		return secureConfigOpenError("publish certbot deploy hook", filepath.Join(dirPath, base), err)
	}
	published = true
	if err := unix.Fsync(dirFD); err != nil {
		return fmt.Errorf("sync certbot deploy hook directory: %w", err)
	}
	return nil
}

func ensureRootOwnedPanelCertHookDirectory(path string) error {
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) || clean == "/" {
		return fmt.Errorf("invalid certbot deploy hook directory")
	}
	parts := strings.Split(strings.TrimPrefix(clean, "/"), "/")
	currentFD, err := unix.Open(
		"/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0,
	)
	if err != nil {
		return fmt.Errorf("open certbot deploy hook root: %w", err)
	}
	defer func() { _ = unix.Close(currentFD) }()

	for _, part := range parts {
		nextFD, openErr := openPanelCertDirectoryAt(currentFD, part)
		if errors.Is(openErr, unix.ENOENT) {
			if mkdirErr := unix.Mkdirat(currentFD, part, 0o755); mkdirErr != nil &&
				!errors.Is(mkdirErr, unix.EEXIST) {
				return fmt.Errorf("create certbot deploy hook directory component: %w", mkdirErr)
			}
			nextFD, openErr = openPanelCertDirectoryAt(currentFD, part)
		}
		if openErr != nil {
			return fmt.Errorf("open certbot deploy hook directory safely: %w", openErr)
		}

		var stat unix.Stat_t
		if statErr := unix.Fstat(nextFD, &stat); statErr != nil {
			_ = unix.Close(nextFD)
			return fmt.Errorf("stat certbot deploy hook directory: %w", statErr)
		}
		if stat.Mode&unix.S_IFMT != unix.S_IFDIR ||
			stat.Uid != 0 ||
			stat.Mode&0o022 != 0 {
			_ = unix.Close(nextFD)
			return fmt.Errorf("certbot deploy hook directory chain is not root-owned and protected")
		}
		_ = unix.Close(currentFD)
		currentFD = nextFD
	}
	return nil
}

func protectPanelCertDeployHook(path string) error {
	relative, err := secureConfigRelativePath(path)
	if err != nil {
		return err
	}
	rootFD, err := openSecureConfigRoot()
	if err != nil {
		return err
	}
	defer unix.Close(rootFD)

	fd, err := unix.Openat2(rootFD, relative, &unix.OpenHow{
		Flags:   uint64(unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW),
		Resolve: secureConfigResolve,
	})
	if err != nil {
		return secureConfigOpenError("protect certbot deploy hook", path, err)
	}
	defer unix.Close(fd)

	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return fmt.Errorf("stat certbot deploy hook: %w", err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return fmt.Errorf("certbot deploy hook is not a regular file")
	}
	if err := unix.Fchown(fd, 0, 0); err != nil {
		return fmt.Errorf("own certbot deploy hook: %w", err)
	}
	if err := unix.Fchmod(fd, 0o755); err != nil {
		return fmt.Errorf("protect certbot deploy hook: %w", err)
	}
	if err := unix.Fsync(fd); err != nil {
		return fmt.Errorf("sync certbot deploy hook: %w", err)
	}
	return nil
}
