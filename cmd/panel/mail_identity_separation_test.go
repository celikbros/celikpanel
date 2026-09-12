package main

import (
	"context"
	"errors"
	"testing"
)

func TestMailTLSSnapshotUsesSavedIdentityWithoutReadingSystemHostname(t *testing.T) {
	p, _ := newMailTLSIsolationFixture(t)
	previous := readMailTLSHostname
	t.Cleanup(func() { readMailTLSHostname = previous })
	readMailTLSHostname = func() (string, error) {
		t.Fatal("saved mail identity fell back to the OS hostname")
		return "", errors.New("unavailable")
	}
	for _, name := range []string{"mail.frankfurt.celikhost.com", "mail.boston.celikhost.com"} {
		if err := p.setSetting(context.Background(), settingMailHostname, name); err != nil {
			t.Fatal(err)
		}
		host, _, err := p.loadMailTLSSnapshotLocked(context.Background(), 0)
		if err != nil || host != name {
			t.Fatalf("mail TLS snapshot = %q, %v", host, err)
		}
	}
	if err := p.setSetting(context.Background(), settingMailHostname, "invalid mail identity"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.loadMailTLSSnapshotLocked(context.Background(), 0); err == nil {
		t.Fatal("invalid saved identity must block rather than change mail identity")
	}
}

func TestMailTLSSnapshotPreservesLegacyOSIdentityWithoutSavedSetting(t *testing.T) {
	p, _ := newMailTLSIsolationFixture(t)
	previous := readMailTLSHostname
	t.Cleanup(func() { readMailTLSHostname = previous })
	readMailTLSHostname = func() (string, error) { return "legacy.example.test", nil }
	host, _, err := p.loadMailTLSSnapshotLocked(context.Background(), 0)
	if err != nil || host != "legacy.example.test" {
		t.Fatalf("legacy identity changed: %q, %v", host, err)
	}
}
