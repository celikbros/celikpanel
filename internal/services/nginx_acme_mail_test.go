package services

import (
	"strings"
	"testing"
)

func TestAdditionalACMEChallengeVhostNeverPublishesWebsiteContent(t *testing.T) {
	ng, err := NewNginxGenerator()
	if err != nil {
		t.Fatal(err)
	}
	out, err := ng.Render(VhostData{
		SiteID:      41,
		Domain:      "example.test",
		ServerNames: []string{"example.test", "www.example.test"},
		ACMEChallengeNames: []string{
			" MAIL.Example.TEST. ",
			"pending-alias.example.test",
			"mail.example.test",
		},
		ACMEChallengeRoot: testACMEChallengeRoot,
		DocumentRoot:      "/srv/example.test/public_html",
		ProjectType:       "static",
		SSLType:           "none",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Embedded templates inherit the checkout's line endings. Normalize the
	// rendered text so this structural assertion is identical on every build host.
	out = strings.ReplaceAll(out, "\r\n", "\n")

	const challengeServerNames = "server_name mail.example.test pending-alias.example.test;"
	if got := strings.Count(out, challengeServerNames); got != 1 {
		t.Fatalf("challenge identities must have exactly one validation-only server; got %d\n%s", got, out)
	}
	if !strings.Contains(out, "server_name example.test www.example.test;") {
		t.Fatalf("website server names missing\n%s", out)
	}
	if strings.Contains(out, "server_name example.test www.example.test mail.example.test") ||
		strings.Contains(out, "server_name example.test www.example.test pending-alias.example.test") {
		t.Fatalf("challenge identity leaked into the website server names\n%s", out)
	}

	mailBlockStart := strings.Index(out, challengeServerNames)
	if mailBlockStart < 0 {
		t.Fatal("validation-only server missing")
	}
	mailBlockEnd := strings.Index(out[mailBlockStart:], "\n}\n")
	if mailBlockEnd < 0 {
		t.Fatalf("mail validation server is not a complete nginx block\n%s", out)
	}
	mailBlock := out[mailBlockStart : mailBlockStart+mailBlockEnd]
	if !strings.Contains(mailBlock, "location ^~ /.well-known/acme-challenge/") {
		t.Fatalf("mail server does not expose HTTP-01\n%s", mailBlock)
	}
	if !strings.Contains(mailBlock, "root "+testACMEChallengeRoot+";") ||
		strings.Contains(mailBlock, "root /srv/example.test/public_html;") {
		t.Fatalf("validation-only server must use only the root-owned challenge root\n%s", mailBlock)
	}
	if !strings.Contains(mailBlock, "location / {\n        return 404;") {
		t.Fatalf("mail server must reject every non-challenge path\n%s", mailBlock)
	}
	if strings.Contains(mailBlock, "index ") ||
		strings.Contains(mailBlock, "proxy_pass ") ||
		strings.Contains(mailBlock, "fastcgi_pass ") ||
		strings.Contains(mailBlock, "return 301 ") {
		t.Fatalf("mail validation server publishes or redirects website traffic\n%s", mailBlock)
	}
}
