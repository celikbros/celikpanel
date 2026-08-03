package hostname

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

var ErrInvalid = errors.New("invalid hostname")

// CanonicalFQDN returns the single storage form used for primary domains and
// explicit aliases. DNS names are case-insensitive; keeping one lowercase form
// prevents filesystem, vhost and certificate state from drifting apart.
func CanonicalFQDN(raw string) (string, error) {
	name := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(raw), "."))
	if err := Validate(name); err != nil {
		return "", err
	}
	if !strings.Contains(name, ".") || net.ParseIP(name) != nil {
		return "", fmt.Errorf("%w %q: a fully qualified DNS name is required", ErrInvalid, name)
	}
	return name, nil
}

// MailFQDN derives and validates the reserved mail identity for a domain.
// Validating it at the API/repository boundary prevents a primary name near
// the DNS length limit from failing later inside a reservation trigger.
func MailFQDN(rawDomain string) (string, error) {
	domain, err := CanonicalFQDN(rawDomain)
	if err != nil {
		return "", err
	}
	mailName, err := CanonicalFQDN("mail." + domain)
	if err != nil {
		return "", fmt.Errorf(
			"%w %q: the derived mail hostname exceeds DNS limits",
			ErrInvalid,
			domain,
		)
	}
	return mailName, nil
}

// Validate checks an already-canonical ASCII DNS hostname.
func Validate(name string) error {
	if name == "" || len(name) > 253 {
		return fmt.Errorf("%w %q", ErrInvalid, name)
	}
	for _, label := range strings.Split(name, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return fmt.Errorf("%w %q", ErrInvalid, name)
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
				return fmt.Errorf("%w %q", ErrInvalid, name)
			}
		}
	}
	return nil
}

// IsNamespaceConflict recognizes both the explicit reservation trigger and
// the older same-table UNIQUE constraints. It intentionally uses error text:
// SQLite's driver does not expose a stable portable constraint-name API.
func IsNamespaceConflict(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{
		"hostname namespace conflict",
		"unique constraint failed: hostname_reservations.hostname",
		"unique constraint failed: domains.name",
		"unique constraint failed: domain_aliases.alias",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}
