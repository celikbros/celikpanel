package transport

import "testing"

func TestWebmailSocketPathUsesRootOwnedRunByDefault(t *testing.T) {
	if got := WebmailSocketPath(); got != "/run/celikpanel-webmail.sock" {
		t.Fatalf("WebmailSocketPath() = %q", got)
	}
}

func TestWebmailSocketPathIgnoresEnvironmentOverride(t *testing.T) {
	t.Setenv("CELIKPANEL_WEBMAIL_SOCKET", "/tmp/celikpanel-webmail-test.sock")
	if got := WebmailSocketPath(); got != "/run/celikpanel-webmail.sock" {
		t.Fatalf("WebmailSocketPath() = %q", got)
	}
}
