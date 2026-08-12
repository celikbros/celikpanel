//go:build linux

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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
	oldWrite := mailMutationWrite
	oldHash := mailHashGenerator
	postfixVBoxPath = filepath.Join(dir, "vmailbox")
	postfixVirtualPath = filepath.Join(dir, "virtual")
	postfixDomainsPath = filepath.Join(dir, "domains")
	dovecotUsersPath = filepath.Join(dir, "users")
	mailRootDir = filepath.Join(dir, "vhosts")
	writeMailTestFile(t, dovecotUsersPath, "", 0o640)
	mailMutationPostmap = func(context.Context, string) error { return nil }
	mailHashGenerator = func(context.Context, string) (string, error) {
		return mailPasswordTestHash("1234567890abcdef", 'A'), nil
	}
	t.Cleanup(func() {
		postfixVBoxPath = oldVBox
		postfixVirtualPath = oldVirtual
		postfixDomainsPath = oldDomains
		dovecotUsersPath = oldUsers
		mailRootDir = oldRoot
		mailMutationPostmap = oldPostmap
		mailMutationWrite = oldWrite
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
		{Email: "other@example.com", Password: "long-enough\ninjected", QuotaMB: 10},
		{Email: "other@example.com", Password: "long-enough\rinjected", QuotaMB: 10},
		{Email: "other@example.com", Password: "long-enough\x00injected", QuotaMB: 10},
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
	if got := readMailTestFile(t, dovecotUsersPath); !strings.Contains(got, "user@example.com:"+mailPasswordTestHash("1234567890abcdef", 'A')) || !strings.Contains(got, "storage=250M") {
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

func TestUpdateMailPasswordPublishesOnlyPasswordField(t *testing.T) {
	configureMailMutationTest(t)
	oldBuildCommit := buildCommit
	buildCommit = "mail-password-test"
	t.Cleanup(func() { buildCommit = oldBuildCommit })

	oldHash := "{CRYPT}$6$1234567890abcdef$" + strings.Repeat("B", 86)
	newHash := mailPasswordTestHash("fedcba0987654321", 'C')
	before := "# keep\n" +
		"user@example.com:" + oldHash + "::::::userdb_quota_rule=*:storage=250M:keep=yes\n" +
		"other@example.com:" + oldHash + "::::::userdb_quota_rule=*:storage=50M\n"
	writeMailTestFile(t, dovecotUsersPath, before, 0o640)

	const secret = "new-password-value"
	mailHashGenerator = func(_ context.Context, password string) (string, error) {
		if password != secret {
			return "", errors.New("unexpected password")
		}
		return newHash, nil
	}
	request := transport.UpdateMailPasswordRequest{
		ExpectedBuildCommit: "mail-password-test",
		Email:               "User@Example.COM",
		NewPassword:         secret,
	}
	response := transport.MailMutationResponse{}
	if err := (&Agent{}).UpdateMailPassword(&request, &response); err != nil {
		t.Fatalf("UpdateMailPassword: %v", err)
	}
	if !response.Applied {
		t.Fatal("successful password update was not marked applied")
	}
	want := strings.Replace(before, oldHash, newHash, 1)
	if got := readMailTestFile(t, dovecotUsersPath); got != want {
		t.Fatalf("dovecot users changed outside password field\ngot:  %q\nwant: %q", got, want)
	}
	info, err := os.Stat(dovecotUsersPath)
	if err != nil {
		t.Fatalf("stat dovecot users: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o640 {
		t.Fatalf("dovecot users mode = %#o, want 0640", got)
	}
}

func TestUpdateMailPasswordFailsClosedWithoutLeakingSecret(t *testing.T) {
	configureMailMutationTest(t)
	oldBuildCommit := buildCommit
	buildCommit = "mail-password-test"
	t.Cleanup(func() { buildCommit = oldBuildCommit })

	oldHash := "{CRYPT}$6$1234567890abcdef$" + strings.Repeat("B", 86)
	before := "user@example.com:" + oldHash + "::::::userdb_quota_rule=*:storage=250M\n"
	writeMailTestFile(t, dovecotUsersPath, before, 0o640)
	const secret = "do-not-disclose-this-password"
	request := transport.UpdateMailPasswordRequest{
		ExpectedBuildCommit: "mail-password-test",
		Email:               "user@example.com",
		NewPassword:         secret,
	}

	tests := []struct {
		name      string
		generator func(context.Context, string) (string, error)
	}{
		{
			name: "generator error",
			generator: func(_ context.Context, password string) (string, error) {
				return "", errors.New("failed for " + password)
			},
		},
		{
			name: "malformed generator output",
			generator: func(context.Context, string) (string, error) {
				return "{SHA512-CRYPT}$6$malformed", nil
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mailHashGenerator = test.generator
			response := transport.MailMutationResponse{}
			err := (&Agent{}).UpdateMailPassword(&request, &response)
			if err == nil {
				t.Fatal("hash failure unexpectedly succeeded")
			}
			if response.Applied {
				t.Fatal("hash failure reported applied")
			}
			if strings.Contains(err.Error(), secret) {
				t.Fatal("password was disclosed in the RPC error")
			}
			if got := readMailTestFile(t, dovecotUsersPath); got != before {
				t.Fatalf("dovecot users changed after hash failure: %q", got)
			}
		})
	}
}

func TestUpdateMailPasswordDoesNotQueueBehindMailMutation(t *testing.T) {
	configureMailMutationTest(t)
	oldBuildCommit := buildCommit
	buildCommit = "mail-password-test"
	t.Cleanup(func() { buildCommit = oldBuildCommit })

	oldHash := "{CRYPT}$6$1234567890abcdef$" + strings.Repeat("B", 86)
	before := "user@example.com:" + oldHash + "::::::userdb_quota_rule=*:storage=250M\n"
	writeMailTestFile(t, dovecotUsersPath, before, 0o640)
	request := transport.UpdateMailPasswordRequest{
		ExpectedBuildCommit: "mail-password-test",
		Email:               "user@example.com",
		NewPassword:         "new-password-value",
	}
	hashCalled := false
	mailHashGenerator = func(context.Context, string) (string, error) {
		hashCalled = true
		return mailPasswordTestHash("fedcba0987654321", 'C'), nil
	}

	type result struct {
		response transport.MailMutationResponse
		err      error
	}
	mailMutex.Lock()
	locked := true
	t.Cleanup(func() {
		if locked {
			mailMutex.Unlock()
		}
	})
	resultChannel := make(chan result, 1)
	go func() {
		response := transport.MailMutationResponse{}
		err := (&Agent{}).UpdateMailPassword(&request, &response)
		resultChannel <- result{response: response, err: err}
	}()

	var got result
	select {
	case got = <-resultChannel:
	case <-time.After(time.Second):
		mailMutex.Unlock()
		locked = false
		got = <-resultChannel
		t.Fatalf("password update queued behind the mail lock: %v", got.err)
	}
	mailMutex.Unlock()
	locked = false
	if got.err == nil || !strings.Contains(got.err.Error(), "busy") {
		t.Fatalf("busy password update error = %v", got.err)
	}
	if got.response.Applied {
		t.Fatal("busy password update reported applied")
	}
	if hashCalled {
		t.Fatal("busy password update entered the hash generator")
	}
	if gotFile := readMailTestFile(t, dovecotUsersPath); gotFile != before {
		t.Fatalf("dovecot users changed while mail lock was busy: %q", gotFile)
	}
}

func TestUpdateMailPasswordSlowRequestCannotOverwriteNewerPassword(t *testing.T) {
	configureMailMutationTest(t)
	oldBuildCommit := buildCommit
	buildCommit = "mail-password-test"
	t.Cleanup(func() { buildCommit = oldBuildCommit })

	oldHash := "{CRYPT}$6$1234567890abcdef$" + strings.Repeat("B", 86)
	hashA := mailPasswordTestHash("aaaaaaaaaaaaaaaa", 'A')
	hashC := mailPasswordTestHash("cccccccccccccccc", 'C')
	before := "user@example.com:" + oldHash + "::::::userdb_quota_rule=*:storage=250M\n"
	writeMailTestFile(t, dovecotUsersPath, before, 0o640)

	aHashStarted := make(chan struct{})
	releaseA := make(chan struct{})
	bHashCalled := make(chan struct{}, 1)
	var releaseOnce sync.Once
	releaseSlowHash := func() {
		releaseOnce.Do(func() { close(releaseA) })
	}
	t.Cleanup(releaseSlowHash)
	mailHashGenerator = func(_ context.Context, password string) (string, error) {
		switch password {
		case "password-from-a":
			close(aHashStarted)
			<-releaseA
			return hashA, nil
		case "password-from-b":
			bHashCalled <- struct{}{}
			return mailPasswordTestHash("bbbbbbbbbbbbbbbb", 'B'), nil
		case "password-from-c":
			return hashC, nil
		default:
			return "", errors.New("unexpected test password")
		}
	}

	type result struct {
		response transport.MailMutationResponse
		err      error
	}
	request := func(password string) transport.UpdateMailPasswordRequest {
		return transport.UpdateMailPasswordRequest{
			ExpectedBuildCommit: "mail-password-test",
			Email:               "user@example.com",
			NewPassword:         password,
		}
	}
	aResult := make(chan result, 1)
	go func() {
		response := transport.MailMutationResponse{}
		req := request("password-from-a")
		err := (&Agent{}).UpdateMailPassword(&req, &response)
		aResult <- result{response: response, err: err}
	}()

	select {
	case <-aHashStarted:
	case <-time.After(time.Second):
		releaseSlowHash()
		t.Fatal("slow password request did not enter the hash generator")
	}

	responseB := transport.MailMutationResponse{}
	requestB := request("password-from-b")
	errB := (&Agent{}).UpdateMailPassword(&requestB, &responseB)
	if errB == nil || !strings.Contains(errB.Error(), "busy") {
		t.Fatalf("newer request B error = %v, want busy", errB)
	}
	if responseB.Applied {
		t.Fatal("busy request B reported applied")
	}
	select {
	case <-bHashCalled:
		t.Fatal("busy request B entered the hash generator")
	default:
	}
	if got := readMailTestFile(t, dovecotUsersPath); got != before {
		t.Fatalf("busy request B changed dovecot users: %q", got)
	}

	releaseSlowHash()
	var gotA result
	select {
	case gotA = <-aResult:
	case <-time.After(time.Second):
		t.Fatal("released request A did not finish")
	}
	if gotA.err != nil || !gotA.response.Applied {
		t.Fatalf("request A result = %+v", gotA)
	}
	if got := readMailTestFile(t, dovecotUsersPath); !strings.Contains(got, hashA) {
		t.Fatalf("request A hash was not published: %q", got)
	}

	responseC := transport.MailMutationResponse{}
	requestC := request("password-from-c")
	if err := (&Agent{}).UpdateMailPassword(&requestC, &responseC); err != nil {
		t.Fatalf("request C: %v", err)
	}
	if !responseC.Applied {
		t.Fatal("request C was not marked applied")
	}
	if got := readMailTestFile(t, dovecotUsersPath); !strings.Contains(got, hashC) || strings.Contains(got, hashA) {
		t.Fatalf("request C did not become the final password state: %q", got)
	}
}

func TestUpdateMailPasswordRollsBackFailedAtomicWrite(t *testing.T) {
	configureMailMutationTest(t)
	oldBuildCommit := buildCommit
	buildCommit = "mail-password-test"
	t.Cleanup(func() { buildCommit = oldBuildCommit })

	oldHash := "{CRYPT}$6$1234567890abcdef$" + strings.Repeat("B", 86)
	before := "user@example.com:" + oldHash + "::::::userdb_quota_rule=*:storage=250M\n"
	writeMailTestFile(t, dovecotUsersPath, before, 0o640)
	mailMutationWrite = func(path string, _ []byte, _ os.FileMode) error {
		if err := os.WriteFile(path, []byte("partial write"), 0o600); err != nil {
			return err
		}
		return errors.New("simulated atomic publish failure")
	}
	request := transport.UpdateMailPasswordRequest{
		ExpectedBuildCommit: "mail-password-test",
		Email:               "user@example.com",
		NewPassword:         "new-password-value",
	}
	response := transport.MailMutationResponse{}
	if err := (&Agent{}).UpdateMailPassword(&request, &response); err == nil {
		t.Fatal("failed write unexpectedly succeeded")
	}
	if response.Applied {
		t.Fatal("failed write reported applied")
	}
	if got := readMailTestFile(t, dovecotUsersPath); got != before {
		t.Fatalf("dovecot users were not rolled back: %q", got)
	}
}
