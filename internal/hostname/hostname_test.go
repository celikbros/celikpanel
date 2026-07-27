package hostname

import (
	"errors"
	"strings"
	"testing"
)

func TestCanonicalFQDN(t *testing.T) {
	got, err := CanonicalFQDN("  WWW.Example.TEST. ")
	if err != nil {
		t.Fatalf("CanonicalFQDN: %v", err)
	}
	if got != "www.example.test" {
		t.Fatalf("CanonicalFQDN = %q, want www.example.test", got)
	}
}

func TestMailFQDNCanonicalizesAndRejectsOversizedDerivedName(t *testing.T) {
	got, err := MailFQDN(" Example.TEST. ")
	if err != nil {
		t.Fatalf("MailFQDN: %v", err)
	}
	if got != "mail.example.test" {
		t.Fatalf("MailFQDN = %q, want mail.example.test", got)
	}

	maxPrimary := strings.Join([]string{
		strings.Repeat("a", 63),
		strings.Repeat("b", 63),
		strings.Repeat("c", 63),
		strings.Repeat("d", 61),
	}, ".")
	if len(maxPrimary) != 253 {
		t.Fatalf("test primary length = %d, want 253", len(maxPrimary))
	}
	if _, err := CanonicalFQDN(maxPrimary); err != nil {
		t.Fatalf("max-length primary should itself be valid: %v", err)
	}
	if _, err := MailFQDN(maxPrimary); !errors.Is(err, ErrInvalid) {
		t.Fatalf("oversized derived mail hostname error = %v, want ErrInvalid", err)
	}
}

func TestCanonicalFQDNRejectsUnsafeNames(t *testing.T) {
	for _, value := range []string{
		"",
		"localhost",
		"127.0.0.1",
		"bad_name.example",
		"-bad.example",
		"bad-.example",
		"bad..example",
		strings.Repeat("a", 64) + ".example",
	} {
		if _, err := CanonicalFQDN(value); !errors.Is(err, ErrInvalid) {
			t.Fatalf("CanonicalFQDN(%q) error = %v, want ErrInvalid", value, err)
		}
	}
}

func TestIsNamespaceConflict(t *testing.T) {
	for _, message := range []string{
		"hostname namespace conflict: already owned",
		"constraint failed: UNIQUE constraint failed: hostname_reservations.hostname (1555)",
		"UNIQUE constraint failed: domains.name",
		"UNIQUE constraint failed: domain_aliases.alias",
	} {
		if !IsNamespaceConflict(errors.New(message)) {
			t.Fatalf("IsNamespaceConflict(%q) = false", message)
		}
	}
	if IsNamespaceConflict(errors.New("database is locked")) {
		t.Fatal("unrelated database error classified as a namespace conflict")
	}
}
