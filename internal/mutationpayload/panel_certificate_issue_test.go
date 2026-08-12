package mutationpayload

import (
	"strings"
	"testing"
)

func TestCanonicalPanelCertificateIssueNormalizesOnlyDocumentedFields(t *testing.T) {
	got, err := CanonicalPanelCertificateIssue(
		" Panel.Example.Test. ",
		" Admin@Example.TEST ",
		"/var/lib/celikpanel/tls",
		" abcdef0123456789 ",
	)
	if err != nil {
		t.Fatal(err)
	}
	if got.Domain != "panel.example.test" ||
		got.Email != "Admin@example.test" ||
		got.TLSDir != "/var/lib/celikpanel/tls" ||
		got.ExpectedBuildCommit != "abcdef0123456789" ||
		!ValidPanelCertificateIssueQualifier(got.Qualifier) {
		t.Fatalf("unexpected commitment: %#v", got)
	}

	equivalent, err := CanonicalPanelCertificateIssue(
		"panel.example.test",
		"Admin@example.test",
		"/var/lib/celikpanel/tls",
		"abcdef0123456789",
	)
	if err != nil {
		t.Fatal(err)
	}
	if equivalent != got {
		t.Fatalf("equivalent request changed commitment:\n got %#v\nwant %#v", equivalent, got)
	}
}

func TestCanonicalPanelCertificateIssueQualifierBindsEveryEffectiveField(t *testing.T) {
	base, err := CanonicalPanelCertificateIssue(
		"panel.example.test",
		"Admin@example.test",
		"/var/lib/celikpanel/tls",
		"abcdef0123456789",
	)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		domain string
		email  string
		build  string
	}{
		{name: "domain", domain: "other.example.test", email: "Admin@example.test", build: "abcdef0123456789"},
		{name: "email local case", domain: "panel.example.test", email: "admin@example.test", build: "abcdef0123456789"},
		{name: "email address", domain: "panel.example.test", email: "Admin@other.example.test", build: "abcdef0123456789"},
		{name: "build", domain: "panel.example.test", email: "Admin@example.test", build: "fedcba9876543210"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := CanonicalPanelCertificateIssue(
				test.domain,
				test.email,
				"/var/lib/celikpanel/tls",
				test.build,
			)
			if err != nil {
				t.Fatal(err)
			}
			if got.Qualifier == base.Qualifier {
				t.Fatalf("changed %s did not change qualifier", test.name)
			}
		})
	}
}

func TestCanonicalPanelCertificateIssueDevelopmentBuildHasOneForm(t *testing.T) {
	empty, err := CanonicalPanelCertificateIssue(
		"panel.example.test", "admin@example.test", "/var/lib/celikpanel/tls", "",
	)
	if err != nil {
		t.Fatal(err)
	}
	unknown, err := CanonicalPanelCertificateIssue(
		"panel.example.test", "admin@example.test", "/var/lib/celikpanel/tls", " unknown ",
	)
	if err != nil {
		t.Fatal(err)
	}
	if empty != unknown || empty.ExpectedBuildCommit != "unknown" {
		t.Fatalf("development forms differ: empty=%#v unknown=%#v", empty, unknown)
	}
}

func TestCanonicalPanelCertificateIssueRejectsInvalidInputs(t *testing.T) {
	valid := func() (string, string, string, string) {
		return "panel.example.test", "admin@example.test", "/var/lib/celikpanel/tls", "abcdef"
	}
	tests := []struct {
		name   string
		mutate func(*string, *string, *string, *string)
	}{
		{name: "single label domain", mutate: func(d, _, _, _ *string) { *d = "localhost" }},
		{name: "IP domain", mutate: func(d, _, _, _ *string) { *d = "127.0.0.1" }},
		{name: "empty email", mutate: func(_, e, _, _ *string) { *e = "" }},
		{name: "display name email", mutate: func(_, e, _, _ *string) { *e = "Admin <admin@example.test>" }},
		{name: "comment email", mutate: func(_, e, _, _ *string) { *e = "admin(comment)@example.test" }},
		{name: "multiple at email", mutate: func(_, e, _, _ *string) { *e = `"a@b"@example.test` }},
		{name: "unicode email", mutate: func(_, e, _, _ *string) { *e = "admın@example.test" }},
		{name: "single label email domain", mutate: func(_, e, _, _ *string) { *e = "admin@localhost" }},
		{name: "TLS whitespace", mutate: func(_, _, p, _ *string) { *p = " /var/lib/celikpanel/tls" }},
		{name: "TLS cleaned equivalent", mutate: func(_, _, p, _ *string) { *p = "/var/lib/celikpanel/./tls" }},
		{name: "build control", mutate: func(_, _, _, b *string) { *b = "abc\ndef" }},
		{name: "build too long", mutate: func(_, _, _, b *string) { *b = strings.Repeat("a", panelCertificateBuildMaxBytes+1) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			domain, email, tlsDir, build := valid()
			test.mutate(&domain, &email, &tlsDir, &build)
			if _, err := CanonicalPanelCertificateIssue(domain, email, tlsDir, build); err == nil {
				t.Fatal("expected request to be rejected")
			}
		})
	}
}

func TestValidPanelCertificateIssueQualifierIsExact(t *testing.T) {
	commitment, err := CanonicalPanelCertificateIssue(
		"panel.example.test", "admin@example.test", "/var/lib/celikpanel/tls", "abcdef",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !ValidPanelCertificateIssueQualifier(commitment.Qualifier) {
		t.Fatal("canonical qualifier was rejected")
	}
	for _, invalid := range []string{
		"",
		strings.ToUpper(commitment.Qualifier),
		commitment.Qualifier + "0",
		commitment.Qualifier[:len(commitment.Qualifier)-1],
		"panel-certificate-issue/v2:sha256:" + strings.Repeat("0", 64),
	} {
		if ValidPanelCertificateIssueQualifier(invalid) {
			t.Fatalf("accepted invalid qualifier %q", invalid)
		}
	}
}
