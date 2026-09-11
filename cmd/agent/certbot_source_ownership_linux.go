//go:build linux

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// Earlier agent versions launched Certbot with the socket-sharing group. An
// explicitly admitted reissuance may remove that legacy group from its known
// directories. Certbot then creates a fresh root:root archive generation; no
// existing certificate/private-key file is adopted or recursively changed.
func prepareManagedCertbotSourceOwnership(domain, lineage string) error {
	if !validPanelCertDomain.MatchString(domain) || (lineage != panelCertLineageName(domain) && lineage != mailHostCertLineageName(domain)) {
		return errors.New("invalid managed Certbot source ownership identity")
	}
	legacyGID := 0
	if gid, ok := lookupGroupID("celikpanel"); ok {
		legacyGID = gid
	}
	return normalizeManagedCertbotSourceDirectories(panelCertificateSourceRoot, lineage, legacyGID)
}

func normalizeManagedCertbotSourceDirectories(root, lineage string, legacyGID int) error {
	if os.Geteuid() != 0 {
		return errors.New("managed Certbot source ownership requires root")
	}
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || root == "/" || lineage != filepath.Base(lineage) || lineage == "." || lineage == ".." || legacyGID < 0 {
		return errors.New("invalid managed Certbot source directory")
	}
	slash, err := unix.Open("/", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer unix.Close(slash)
	fds := []int{}
	defer func() {
		for _, fd := range fds {
			_ = unix.Close(fd)
		}
	}()
	// Validate every existing directory before changing any metadata. Each
	// descriptor is opened without following any symlink, including ancestors.
	for _, suffix := range []string{"", "live", "archive", "renewal", "renewal-hooks", "renewal-hooks/deploy", "live/" + lineage, "archive/" + lineage} {
		path := root
		if suffix != "" {
			path += "/" + suffix
		}
		fd, openErr := openPanelCertificateSourceAt(slash, strings.TrimPrefix(path, "/"), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, panelCertificateSourceResolveRoot)
		if errors.Is(openErr, unix.ENOENT) {
			continue
		}
		if openErr != nil {
			return fmt.Errorf("open managed Certbot directory for reissuance: %w", openErr)
		}
		fds = append(fds, fd)
		var stat unix.Stat_t
		if err := unix.Fstat(fd, &stat); err != nil {
			return err
		}
		if stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Uid != 0 || (stat.Gid != 0 && stat.Gid != uint32(legacyGID)) || stat.Mode&0022 != 0 {
			return errors.New("managed Certbot directory has untrusted ownership or permissions; repair it before reissuing")
		}
	}
	for _, fd := range fds {
		var stat unix.Stat_t
		if err := unix.Fstat(fd, &stat); err != nil {
			return err
		}
		if stat.Uid != 0 || (stat.Gid != 0 && stat.Gid != uint32(legacyGID)) || stat.Mode&0022 != 0 {
			return errors.New("managed Certbot directory changed before ownership repair")
		}
		if stat.Gid == 0 {
			continue
		}
		if err := unix.Fchown(fd, 0, 0); err != nil {
			return err
		}
		if err := unix.Fsync(fd); err != nil {
			return err
		}
	}
	return nil
}
