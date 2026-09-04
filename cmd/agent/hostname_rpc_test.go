package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func withHostnameFiles(t *testing.T, hosts string) string {
	t.Helper()
	directory := t.TempDir()
	previousHostname, previousHosts := hostnameFilePath, hostsFilePath
	hostnameFilePath = filepath.Join(directory, "hostname")
	hostsFilePath = filepath.Join(directory, "hosts")
	t.Cleanup(func() {
		hostnameFilePath, hostsFilePath = previousHostname, previousHosts
	})
	if hosts != "" {
		if err := os.WriteFile(hostsFilePath, []byte(hosts), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return directory
}

// R-036. The name only counts once it is persistent, live and resolvable: a
// server that answers to a name only until the next boot is not a server the
// mail stack can be issued a certificate for.
func TestApplyServerHostnameIsPersistentLiveAndResolvable(t *testing.T) {
	withHostnameFiles(t, "127.0.0.1\tlocalhost\n127.0.1.1\tALIASUSPC.localdomain\tALIASUSPC\n::1\tip6-localhost\n")
	var applied []string
	previousLive := setLiveHostname
	setLiveHostname = func(_ context.Context, canonical string) error {
		applied = append(applied, canonical)
		return nil
	}
	t.Cleanup(func() { setLiveHostname = previousLive })

	if err := applyServerHostname(context.Background(), "mail.s2.test"); err != nil {
		t.Fatalf("apply server hostname: %v", err)
	}
	if len(applied) != 1 || applied[0] != "mail.s2.test" {
		t.Fatalf("live hostname calls = %v", applied)
	}
	persistent, err := os.ReadFile(hostnameFilePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(persistent) != "mail.s2.test\n" {
		t.Fatalf("persistent hostname = %q", persistent)
	}
	hosts, err := os.ReadFile(hostsFilePath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(hosts), "\n"), "\n")
	if len(lines) != 3 ||
		lines[0] != "127.0.0.1\tlocalhost" ||
		lines[1] != "127.0.1.1\tmail.s2.test\tmail" ||
		lines[2] != "::1\tip6-localhost" {
		t.Fatalf("hosts file = %q", hosts)
	}
}

// Every other line of the hosts file is the operator's, and a server with no
// 127.0.1.1 line at all still gets one.
func TestEnsureHostsEntryPreservesEveryOtherLine(t *testing.T) {
	withHostnameFiles(t, "127.0.0.1\tlocalhost\n10.0.0.5\tinternal.example.test\n")
	if err := ensureHostsEntry("mail.example.test"); err != nil {
		t.Fatal(err)
	}
	hosts, err := os.ReadFile(hostsFilePath)
	if err != nil {
		t.Fatal(err)
	}
	want := "127.0.0.1\tlocalhost\n10.0.0.5\tinternal.example.test\n127.0.1.1\tmail.example.test\tmail\n"
	if string(hosts) != want {
		t.Fatalf("hosts file = %q, want %q", hosts, want)
	}
	// Running it again changes nothing.
	if err := ensureHostsEntry("mail.example.test"); err != nil {
		t.Fatal(err)
	}
	if again, err := os.ReadFile(hostsFilePath); err != nil || string(again) != want {
		t.Fatalf("second pass hosts file = %q (%v)", again, err)
	}
}

// The panel canonicalizes the name; anything else reaching the agent is not a
// name this server may be given, and it is refused before any lease is claimed
// and before any file is touched.
func TestSetServerHostnameRefusesANonCanonicalNameBeforeTouchingTheHost(t *testing.T) {
	directory := withHostnameFiles(t, "")
	for _, name := range []string{
		"", "ALIASUSPC", "mail.s2.test.", "MAIL.S2.TEST", "  mail.s2.test", "192.0.2.10",
	} {
		var response SetServerHostnameResponse
		if err := (&Agent{}).SetServerHostname(
			&transport.SetServerHostnameRequest{Hostname: name}, &response,
		); err != nil {
			t.Fatalf("SetServerHostname(%q) transport error: %v", name, err)
		}
		if response.Error == "" || response.Changed {
			t.Fatalf("SetServerHostname(%q) = %+v", name, response)
		}
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("refused hostname requests wrote %d files", len(entries))
	}
}
