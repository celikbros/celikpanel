//go:build linux

package main

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/alicelik/celikpanel/internal/hostingpath"
	"golang.org/x/sys/unix"
)

const acmeChallengeTrustedParent = "/var/lib"

// prepareACMEChallengeRoot creates the identity-derived, agent-owned HTTP-01
// webroot. The tenant cannot write this hierarchy, so neither certbot nor
// nginx ever follows content or symlinks from public_html.
func prepareACMEChallengeRoot(subscriptionID, domainID int) (string, error) {
	challengeRoot, err := hostingpath.ACMEChallengeRoot(subscriptionID, domainID)
	if err != nil {
		return "", err
	}
	if err := hostingpath.ValidateACMEChallengeRoot(
		challengeRoot, subscriptionID, domainID,
	); err != nil {
		return "", err
	}
	if err := secureEnsureACMEChallengeDirectory(challengeRoot); err != nil {
		return "", err
	}
	return challengeRoot, nil
}

func secureEnsureACMEChallengeDirectory(challengeRoot string) error {
	if !filepath.IsAbs(challengeRoot) || filepath.Clean(challengeRoot) != challengeRoot {
		return fmt.Errorf("ACME challenge root must be an absolute canonical path")
	}
	relativeRoot, err := filepath.Rel(acmeChallengeTrustedParent, challengeRoot)
	if err != nil || relativeRoot == "." || relativeRoot == ".." ||
		strings.HasPrefix(relativeRoot, ".."+string(filepath.Separator)) {
		return fmt.Errorf("ACME challenge root leaves the trusted agent directory")
	}

	parentFD, err := openTrustedRootOwnedDirectory(acmeChallengeTrustedParent)
	if err != nil {
		return fmt.Errorf("open trusted ACME parent: %w", err)
	}
	defer unix.Close(parentFD)

	relativeChallengeDir := path.Join(
		filepath.ToSlash(relativeRoot), ".well-known", "acme-challenge",
	)
	if err := secureEnsureACMEChallengeDirectoryAt(parentFD, relativeChallengeDir); err != nil {
		return fmt.Errorf("prepare root-owned ACME challenge directory: %w", err)
	}
	return nil
}

// openTrustedRootOwnedDirectory opens every component from / with openat2 and
// refuses ownership or permission states that would let a non-root process
// replace a component after validation.
func openTrustedRootOwnedDirectory(absolutePath string) (int, error) {
	if !filepath.IsAbs(absolutePath) || filepath.Clean(absolutePath) != absolutePath {
		return -1, fmt.Errorf("trusted directory must be an absolute canonical path")
	}
	currentFD, err := unix.Open(
		"/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0,
	)
	if err != nil {
		return -1, err
	}
	if err := validateRootOwnedDirectoryFD(currentFD); err != nil {
		unix.Close(currentFD)
		return -1, err
	}

	for _, component := range splitSSLRelativePath(
		filepath.ToSlash(strings.TrimPrefix(absolutePath, string(filepath.Separator))),
	) {
		nextFD, openErr := openSSLConfinedAt(
			currentFD, component,
			unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0,
		)
		unix.Close(currentFD)
		if openErr != nil {
			return -1, openErr
		}
		if err := validateRootOwnedDirectoryFD(nextFD); err != nil {
			unix.Close(nextFD)
			return -1, err
		}
		currentFD = nextFD
	}
	return currentFD, nil
}

// secureEnsureACMEChallengeDirectoryAt creates a directory tree below an
// already trusted descriptor. Each lookup is beneath/no-symlink openat2; each
// resulting inode is root:root and non-writable by tenants before the next
// component is considered.
func secureEnsureACMEChallengeDirectoryAt(rootFD int, relativeDir string) error {
	if relativeDir == "" || relativeDir == "." || path.IsAbs(relativeDir) ||
		path.Clean(relativeDir) != relativeDir {
		return fmt.Errorf("ACME challenge directory must be a canonical relative path")
	}

	currentFD, err := duplicateSSLFD(rootFD)
	if err != nil {
		return err
	}
	defer func() {
		if currentFD >= 0 {
			_ = unix.Close(currentFD)
		}
	}()
	if err := validateRootOwnedDirectoryFD(currentFD); err != nil {
		return err
	}

	for _, component := range splitSSLRelativePath(relativeDir) {
		nextFD, openErr := openSSLConfinedAt(
			currentFD, component,
			unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0,
		)
		created := false
		if errors.Is(openErr, syscall.ENOENT) {
			if mkdirErr := unix.Mkdirat(currentFD, component, 0o755); mkdirErr != nil &&
				!errors.Is(mkdirErr, syscall.EEXIST) {
				return mkdirErr
			}
			nextFD, openErr = openSSLConfinedAt(
				currentFD, component,
				unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0,
			)
			created = openErr == nil
		}
		if openErr != nil {
			return openErr
		}
		if created {
			if err := unix.Fchown(nextFD, 0, 0); err != nil {
				unix.Close(nextFD)
				return err
			}
		}
		if err := validateRootOwnedDirectoryFD(nextFD); err != nil {
			unix.Close(nextFD)
			return err
		}
		if err := unix.Fchmod(nextFD, 0o755); err != nil {
			unix.Close(nextFD)
			return err
		}
		unix.Close(currentFD)
		currentFD = nextFD
	}
	return nil
}

func validateRootOwnedDirectoryFD(fd int) error {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		return fmt.Errorf("ACME path component is not a directory")
	}
	if stat.Uid != 0 || stat.Gid != 0 {
		return fmt.Errorf("ACME path component must be owned by root:root")
	}
	if stat.Mode&0o022 != 0 {
		return fmt.Errorf("ACME path component must not be group/other writable")
	}
	return nil
}
