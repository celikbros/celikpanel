package transport

import (
	"encoding/json"
	"testing"
)

func TestDefaultMailTLSCertificatePathIsStable(t *testing.T) {
	t.Parallel()
	if DefaultMailTLSCertificatePath != "/etc/ssl/celikpanel/_mail/default-cert.pem" {
		t.Fatalf("default mail TLS certificate path = %q", DefaultMailTLSCertificatePath)
	}
}

func TestReconcileMailTLSMutationRequestCarriesDurableBinding(t *testing.T) {
	t.Parallel()

	request := ReconcileMailTLSMutationRequest{
		ServiceMutationBinding: ServiceMutationBinding{
			MutationRequestID: "11111111111111111111111111111111",
			MutationOwnerID:   "22222222222222222222222222222222",
		},
		ExpectedBuildCommit: "release-commit",
		Myhostname:          "mail.example.test",
		SNI: []MailSNIEntry{{
			Names:    []string{"mail.example.test"},
			CertPath: "/managed/example.test/fullchain.pem",
			KeyPath:  "/managed/example.test/privkey.pem",
		}},
	}
	payload, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{
		"mutation_request_id",
		"mutation_owner_id",
		"expected_build_commit",
		"myhostname",
		"sni",
	} {
		if _, ok := decoded[field]; !ok {
			t.Fatalf("wire request omitted %q: %s", field, payload)
		}
	}
}

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
