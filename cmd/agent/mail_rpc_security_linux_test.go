//go:build linux

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func configureMailMutationTest(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CELIKPANEL_MAIL_DIR", dir)

	oldVBox := postfixVBoxPath
	oldVirtual := postfixVirtualPath
	oldDomains := postfixDomainsPath
	oldUsers := dovecotUsersPath
	oldRoot := mailRootDir
	oldPostmap := mailMutationPostmap
	oldHash := mailHashGenerator
	postfixVBoxPath = filepath.Join(dir, "vmailbox")
	postfixVirtualPath = filepath.Join(dir, "virtual")
	postfixDomainsPath = filepath.Join(dir, "domains")
	dovecotUsersPath = filepath.Join(dir, "users")
	mailRootDir = filepath.Join(dir, "vhosts")
	mailMutationPostmap = func(context.Context, string) error { return nil }
	mailHashGenerator = func(context.Context, string) (string, error) {
		return "{SHA512-CRYPT}$6$test", nil
	}
	t.Cleanup(func() {
		postfixVBoxPath = oldVBox
		postfixVirtualPath = oldVirtual
		postfixDomainsPath = oldDomains
		dovecotUsersPath = oldUsers
		mailRootDir = oldRoot
		mailMutationPostmap = oldPostmap
		mailHashGenerator = oldHash
	})
	return dir
}

func writeMailTestFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("write fixture %s: %v", path, err)
	}
}

func readMailTestFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	return string(content)
}

func TestAddMailAccountRejectsInvalidInputBeforeMutation(t *testing.T) {
	configureMailMutationTest(t)
	writeMailTestFile(t, dovecotUsersPath, "existing@example.com:{CRYPT}hash::::::userdb_quota_rule=*:storage=10M\n", 0o640)
	writeMailTestFile(t, postfixVBoxPath, "existing@example.com example.com/existing/\n", 0o644)
	writeMailTestFile(t, postfixDomainsPath, "example.com OK\n", 0o644)
	beforeUsers := readMailTestFile(t, dovecotUsersPath)
	beforeVBox := readMailTestFile(t, postfixVBoxPath)
	beforeDomains := readMailTestFile(t, postfixDomainsPath)

	hashCalled := false
	mailHashGenerator = func(context.Context, string) (string, error) {
		hashCalled = true
		return "", errors.New("must not be called")
	}
	tests := []MailAccount{
		{Email: "other@@example.com", Password: "long-enough", QuotaMB: 10},
		{Email: "other@example.com", Password: "short", QuotaMB: 10},
		{Email: "other@example.com", Password: "long-enough", QuotaMB: 0},
		{Email: "other@example.com", Password: "long-enough", QuotaMB: transport.MaxMailboxQuotaMB + 1},
	}
	for _, request := range tests {
		response := transport.MailMutationResponse{}
		if err := (&Agent{}).AddMailAccount(&request, &response); err == nil {
			t.Fatalf("invalid request %+v unexpectedly succeeded", request)
		}
		if response.Applied {
			t.Fatalf("invalid request %+v reported applied", request)
		}
	}
	if hashCalled {
		t.Fatal("password hash generator called for invalid request")
	}
	if got := readMailTestFile(t, dovecotUsersPath); got != beforeUsers {
		t.Fatalf("users file changed: %q", got)
	}
	if got := readMailTestFile(t, postfixVBoxPath); got != beforeVBox {
		t.Fatalf("vmailbox file changed: %q", got)
	}
	if got := readMailTestFile(t, postfixDomainsPath); got != beforeDomains {
		t.Fatalf("domains file changed: %q", got)
	}
}

func TestAddMailAccountRollsBackAllFilesWhenPostmapFails(t *testing.T) {
	configureMailMutationTest(t)
	writeMailTestFile(t, dovecotUsersPath, "old-users\n", 0o640)
	writeMailTestFile(t, postfixVBoxPath, "old-vbox\n", 0o644)
	writeMailTestFile(t, postfixDomainsPath, "old.example OK\n", 0o644)
	writeMailTestFile(t, postfixVBoxPath+".db", "old-index", 0o644)
	before := map[string]string{
		dovecotUsersPath:        readMailTestFile(t, dovecotUsersPath),
		postfixVBoxPath:         readMailTestFile(t, postfixVBoxPath),
		postfixDomainsPath:      readMailTestFile(t, postfixDomainsPath),
		postfixVBoxPath + ".db": readMailTestFile(t, postfixVBoxPath+".db"),
	}
	mailMutationPostmap = func(_ context.Context, path string) error {
		writeMailTestFile(t, path+".db", "broken-index", 0o644)
		return errors.New("postmap failed")
	}

	request := MailAccount{Email: "new@example.com", Password: "long-enough", QuotaMB: 100}
	response := transport.MailMutationResponse{}
	if err := (&Agent{}).AddMailAccount(&request, &response); err == nil {
		t.Fatal("postmap failure unexpectedly succeeded")
	}
	if response.Applied {
		t.Fatal("failed mutation reported applied")
	}
	for path, want := range before {
		if got := readMailTestFile(t, path); got != want {
			t.Fatalf("%s after rollback = %q, want %q", path, got, want)
		}
	}
}

func TestAddMailAccountPublishesCompleteState(t *testing.T) {
	configureMailMutationTest(t)
	request := MailAccount{Email: "User@Example.COM", Password: "long-enough", QuotaMB: 250}
	response := transport.MailMutationResponse{}
	if err := (&Agent{}).AddMailAccount(&request, &response); err != nil {
		t.Fatalf("AddMailAccount: %v", err)
	}
	if !response.Applied {
		t.Fatal("successful mutation was not marked applied")
	}
	if got := readMailTestFile(t, dovecotUsersPath); !strings.Contains(got, "user@example.com:{SHA512-CRYPT}$6$test") || !strings.Contains(got, "storage=250M") {
		t.Fatalf("unexpected users file: %q", got)
	}
	if got := readMailTestFile(t, postfixVBoxPath); got != "user@example.com example.com/user/\n" {
		t.Fatalf("unexpected vmailbox file: %q", got)
	}
	if got := readMailTestFile(t, postfixDomainsPath); got != "example.com OK\n" {
		t.Fatalf("unexpected domains file: %q", got)
	}
}

func TestUpdateMailForwardingRejectsDuplicateSource(t *testing.T) {
	configureMailMutationTest(t)
	writeMailTestFile(t, postfixVirtualPath, "old@example.com target@example.net\n", 0o644)
	request := transport.UpdateMailForwardingRequest{Forwardings: []transport.MailForwarding{
		{Source: "same@example.com", Destination: "one@example.net"},
		{Source: "SAME@example.com", Destination: "two@example.net"},
	}}
	response := transport.MailMutationResponse{}
	if err := (&Agent{}).UpdateMailForwarding(&request, &response); err == nil {
		t.Fatal("duplicate forwarding source unexpectedly succeeded")
	}
	if response.Applied {
		t.Fatal("duplicate forwarding reported applied")
	}
	if got := readMailTestFile(t, postfixVirtualPath); got != "old@example.com target@example.net\n" {
		t.Fatalf("virtual map changed: %q", got)
	}
}

func TestImportMailAccountNeverOverwritesExistingMailbox(t *testing.T) {
	configureMailMutationTest(t)
	writeMailTestFile(t, dovecotUsersPath, "user@example.com:{CRYPT}$6$oldhashvalue::::::userdb_quota_rule=*:storage=10M\n", 0o640)
	writeMailTestFile(t, postfixVBoxPath, "user@example.com example.com/user/\n", 0o644)
	request := transport.ImportMailAccountRequest{
		Email: "user@example.com", CryptHash: "$6$newhashvalue", QuotaMB: 20,
	}
	response := transport.MailMutationResponse{}
	if err := (&Agent{}).ImportMailAccount(&request, &response); err == nil {
		t.Fatal("duplicate import unexpectedly succeeded")
	}
	if response.Applied {
		t.Fatal("duplicate import reported applied")
	}
	if got := readMailTestFile(t, dovecotUsersPath); !strings.Contains(got, "$6$oldhashvalue") || strings.Contains(got, "$6$newhashvalue") {
		t.Fatalf("existing mailbox was overwritten: %q", got)
	}
}
