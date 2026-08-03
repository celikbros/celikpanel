// Package hostingpath owns the immutable filesystem layout shared by the
// unprivileged panel and the root agent. Paths are derived from database
// identities, never from tenant-controlled text.
package hostingpath

import (
	"fmt"
	"path"
	"strings"
	"unicode/utf8"
)

const (
	subscriptionsRoot     = "/var/www/celikpanel/subscriptions"
	acmeChallengeBaseRoot = "/var/lib/celikpanel-agent/acme-http-01"
)

const (
	// MaxRelativePathBytes caps tenant-controlled file-manager paths before
	// they reach the privileged agent.
	MaxRelativePathBytes = 4096
	// MaxFileNameBytes matches the common NAME_MAX value used by Linux
	// filesystems and prevents oversized upload leaf names.
	MaxFileNameBytes = 255
)

func SiteHome(subscriptionID, domainID int) (string, error) {
	if subscriptionID <= 0 || domainID <= 0 {
		return "", fmt.Errorf("subscription and domain IDs must be positive")
	}
	return path.Join(
		subscriptionsRoot,
		fmt.Sprintf("%d", subscriptionID),
		"sites",
		fmt.Sprintf("%d", domainID),
	), nil
}

func DocumentRoot(subscriptionID, domainID int) (string, error) {
	home, err := SiteHome(subscriptionID, domainID)
	if err != nil {
		return "", err
	}
	return path.Join(home, "public_html"), nil
}

func ValidateDocumentRoot(candidate string, subscriptionID, domainID int) error {
	expected, err := DocumentRoot(subscriptionID, domainID)
	if err != nil {
		return err
	}
	if candidate != expected {
		return fmt.Errorf("document root must be %s", expected)
	}
	return nil
}

// ACMEChallengeRoot returns the root-owned HTTP-01 webroot for one immutable
// subscription/domain identity. It deliberately lives outside every tenant
// home: certbot runs as root and must never write challenge files into
// tenant-owned public_html.
func ACMEChallengeRoot(subscriptionID, domainID int) (string, error) {
	if subscriptionID <= 0 || domainID <= 0 {
		return "", fmt.Errorf("subscription and domain IDs must be positive")
	}
	return path.Join(
		acmeChallengeBaseRoot,
		"subscriptions",
		fmt.Sprintf("%d", subscriptionID),
		"domains",
		fmt.Sprintf("%d", domainID),
	), nil
}

func ValidateACMEChallengeRoot(candidate string, subscriptionID, domainID int) error {
	expected, err := ACMEChallengeRoot(subscriptionID, domainID)
	if err != nil {
		return err
	}
	if candidate != expected {
		return fmt.Errorf("ACME challenge root must be %s", expected)
	}
	return nil
}

// NormalizeRelativePath converts a user-facing relative path into the only
// form accepted by the file-manager RPC contract: "." for the root or a
// slash-separated, clean path below it. Absolute paths, backslashes, NUL
// bytes and any path that would leave the root are rejected.
func NormalizeRelativePath(candidate string) (string, error) {
	if len(candidate) > MaxRelativePathBytes {
		return "", fmt.Errorf("relative path is too long")
	}
	if strings.IndexByte(candidate, 0) >= 0 {
		return "", fmt.Errorf("relative path contains NUL")
	}
	if !utf8.ValidString(candidate) || containsPathControl(candidate) {
		return "", fmt.Errorf("relative path contains invalid text")
	}
	if strings.Contains(candidate, `\`) {
		return "", fmt.Errorf("relative path must use forward slashes")
	}
	if candidate == "" {
		return ".", nil
	}
	if path.IsAbs(candidate) {
		return "", fmt.Errorf("absolute paths are not allowed")
	}

	cleaned := path.Clean(candidate)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("relative path leaves document root")
	}
	return cleaned, nil
}

// ValidateRelativePath is deliberately stricter than NormalizeRelativePath.
// The panel may normalize browser input, while the privileged agent accepts
// only the canonical value produced by that panel.
func ValidateRelativePath(candidate string) (string, error) {
	cleaned, err := NormalizeRelativePath(candidate)
	if err != nil {
		return "", err
	}
	if candidate != cleaned {
		return "", fmt.Errorf("relative path is not canonical")
	}
	return cleaned, nil
}

// ValidateFileName accepts one filesystem leaf only. It is used separately
// from directory paths for uploads so a browser-supplied filename cannot
// smuggle traversal or an absolute path into filepath.Join.
func ValidateFileName(name string) error {
	if name == "" {
		return fmt.Errorf("file name is required")
	}
	if len(name) > MaxFileNameBytes {
		return fmt.Errorf("file name is too long")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("invalid file name")
	}
	if strings.IndexByte(name, 0) >= 0 ||
		strings.ContainsAny(name, `/\`) ||
		!utf8.ValidString(name) ||
		containsPathControl(name) {
		return fmt.Errorf("file name must be a single path component")
	}
	return nil
}

func containsPathControl(value string) bool {
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}
