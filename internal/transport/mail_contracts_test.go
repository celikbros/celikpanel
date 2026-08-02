package transport

import "testing"

func TestCanonicalMailAddressRejectsAmbiguousInput(t *testing.T) {
	t.Parallel()
	invalid := []string{
		"",
		"user",
		"user@@example.com",
		"user@example.com@evil.test",
		"user name@example.com",
		".user@example.com",
		"user..name@example.com",
		"user@example..com",
		"user@-example.com",
		"user@example.com.",
	}
	for _, input := range invalid {
		input := input
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			if _, err := CanonicalMailAddress(input); err == nil {
				t.Fatalf("CanonicalMailAddress(%q) unexpectedly succeeded", input)
			}
		})
	}
}

func TestCanonicalMailboxForDomainIsTenantBound(t *testing.T) {
	t.Parallel()
	got, err := CanonicalMailboxForDomain("Alice", "Example.COM")
	if err != nil {
		t.Fatalf("canonical local mailbox: %v", err)
	}
	if got != "alice@example.com" {
		t.Fatalf("canonical mailbox = %q", got)
	}
	for _, input := range []string{"alice@evil.test", "alice@example.com@evil.test"} {
		if _, err := CanonicalMailboxForDomain(input, "example.com"); err == nil {
			t.Fatalf("cross-tenant mailbox %q unexpectedly accepted", input)
		}
	}
}

func TestCanonicalForwardSourceSupportsOnlyExplicitCatchAll(t *testing.T) {
	t.Parallel()
	got, err := CanonicalForwardSource("@Example.COM")
	if err != nil {
		t.Fatalf("canonical catch-all: %v", err)
	}
	if got != "@example.com" {
		t.Fatalf("canonical catch-all = %q", got)
	}
	for _, input := range []string{"@", "@example.com@evil.test", "@.example.com"} {
		if _, err := CanonicalForwardSource(input); err == nil {
			t.Fatalf("invalid catch-all %q unexpectedly accepted", input)
		}
	}
}
