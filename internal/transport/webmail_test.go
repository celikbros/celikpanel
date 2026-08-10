package transport

import "testing"

func TestWebmailSocketPathUsesRootOwnedRunByDefault(t *testing.T) {
	t.Setenv("CELIKPANEL_WEBMAIL_SOCKET", "")
	if got := WebmailSocketPath(); got != "/run/celikpanel-webmail.sock" {
		t.Fatalf("WebmailSocketPath() = %q", got)
	}
}

func TestWebmailSocketPathAllowsIsolatedTestOverride(t *testing.T) {
	t.Setenv("CELIKPANEL_WEBMAIL_SOCKET", "/tmp/celikpanel-webmail-test.sock")
	if got := WebmailSocketPath(); got != "/tmp/celikpanel-webmail-test.sock" {
		t.Fatalf("WebmailSocketPath() = %q", got)
	}
}
