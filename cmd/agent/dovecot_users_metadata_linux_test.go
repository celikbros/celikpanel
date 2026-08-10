//go:build linux

package main

import (
	"context"
	"errors"
	"os"
	"os/user"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func TestDovecotUsersMetadataAcceptsValidDevFile(t *testing.T) {
	configureMailMutationTest(t)
	const content = "user@example.com:{CRYPT}$6$valid-test-hash\n"
	writeMailTestFile(t, dovecotUsersPath, content, 0o640)

	got, exists, err := readDovecotUsersFileForMutation(dovecotUsersPath, true)
	if err != nil {
		t.Fatalf("valid dev passwd-file rejected: %v", err)
	}
	if !exists || string(got) != content {
		t.Fatalf("snapshot exists=%v content=%q", exists, got)
	}
	mode, uid, gid, err := dovecotUsersTestMetadata(dovecotUsersPath)
	if err != nil {
		t.Fatal(err)
	}
	if mode != 0o640 || uid != os.Geteuid() || gid != os.Getegid() {
		t.Fatalf(
			"metadata mode=%#o uid=%d gid=%d, want 0640 %d:%d",
			mode, uid, gid, os.Geteuid(), os.Getegid(),
		)
	}
}

func TestDovecotUsersMetadataRejectsBeforeSecretHashing(t *testing.T) {
	for _, state := range []string{"world_readable", "missing"} {
		for _, operation := range []string{"add", "password"} {
			t.Run(state+"/"+operation, func(t *testing.T) {
				configureMailMutationTest(t)
				previousCommit := buildCommit
				buildCommit = "dovecot-metadata-test"
				t.Cleanup(func() { buildCommit = previousCommit })

				const storedHash = "$6$stored-secret-hash"
				const before = "user@example.com:{CRYPT}" + storedHash +
					"::::::userdb_quota_rule=*:storage=100M\n"
				writeMailTestFile(t, dovecotUsersPath, before, 0o640)
				if state == "world_readable" {
					if err := os.Chmod(dovecotUsersPath, 0o644); err != nil {
						t.Fatal(err)
					}
				} else if err := os.Remove(dovecotUsersPath); err != nil {
					t.Fatal(err)
				}

				hashCalled := false
				mailHashGenerator = func(context.Context, string) (string, error) {
					hashCalled = true
					return mailPasswordTestHash("1234567890abcdef", 'A'), nil
				}

				const newPassword = "new-secret-password"
				var err error
				switch operation {
				case "add":
					var response transport.MailMutationResponse
					err = (&Agent{}).AddMailAccount(&MailAccount{
						Email: "new@example.com", Password: newPassword, QuotaMB: 100,
					}, &response)
					if response.Applied {
						t.Fatal("refused add reported applied")
					}
				case "password":
					var response transport.MailMutationResponse
					err = (&Agent{}).UpdateMailPassword(&transport.UpdateMailPasswordRequest{
						ExpectedBuildCommit: "dovecot-metadata-test",
						Email:               "user@example.com",
						NewPassword:         newPassword,
					}, &response)
					if response.Applied {
						t.Fatal("refused password update reported applied")
					}
				default:
					t.Fatalf("unknown operation %q", operation)
				}

				if err == nil {
					t.Fatal("unsafe passwd-file unexpectedly accepted")
				}
				if hashCalled {
					t.Fatal("password hash generator called before metadata refusal")
				}
				if strings.Contains(err.Error(), storedHash) ||
					strings.Contains(err.Error(), newPassword) {
					t.Fatalf("secret-bearing error: %v", err)
				}
				if state == "missing" {
					if !strings.Contains(err.Error(), "is missing") {
						t.Fatalf("missing error = %v", err)
					}
					if _, statErr := os.Stat(dovecotUsersPath); !errors.Is(statErr, os.ErrNotExist) {
						t.Fatalf("missing passwd-file was created: %v", statErr)
					}
				} else {
					if !strings.Contains(err.Error(), "unsafe metadata") {
						t.Fatalf("metadata error = %v", err)
					}
					if got := readMailTestFile(t, dovecotUsersPath); got != before {
						t.Fatalf("refused mutation changed passwd-file: %q", got)
					}
					info, statErr := os.Stat(dovecotUsersPath)
					if statErr != nil || info.Mode().Perm() != 0o644 {
						t.Fatalf("refusal repaired metadata: mode=%v err=%v", info, statErr)
					}
				}
			})
		}
	}
}

func TestSyncMailConfigRejectsUnsafeDovecotWithoutPublishing(t *testing.T) {
	for _, state := range []string{"world_readable", "missing"} {
		t.Run(state, func(t *testing.T) {
			configureMailMutationTest(t)
			if state == "world_readable" {
				if err := os.Chmod(dovecotUsersPath, 0o644); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Remove(dovecotUsersPath); err != nil {
				t.Fatal(err)
			}

			writeCalled := false
			postmapCalled := false
			mailMutationWrite = func(string, []byte, os.FileMode) error {
				writeCalled = true
				return errors.New("unexpected write")
			}
			mailMutationPostmap = func(context.Context, string) error {
				postmapCalled = true
				return errors.New("unexpected postmap")
			}

			var response transport.MailMutationResponse
			err := (&Agent{}).SyncMailConfig(&MailConfigSyncRequest{
				Accounts: []MailAccount{{Email: "user@example.com"}},
				Domains:  []string{"example.com"},
			}, &response)
			if err == nil {
				t.Fatal("unsafe sync unexpectedly succeeded")
			}
			if response.Applied || writeCalled || postmapCalled {
				t.Fatalf(
					"response=%+v writeCalled=%v postmapCalled=%v",
					response, writeCalled, postmapCalled,
				)
			}
			for _, path := range []string{
				postfixVBoxPath, postfixVirtualPath, postfixDomainsPath,
			} {
				if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
					t.Fatalf("sync published %s: %v", path, statErr)
				}
			}
		})
	}
}

func TestDovecotUsersMetadataRejectsWrongDevOwnershipWhenPermitted(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("changing fixture ownership requires root")
	}
	configureMailMutationTest(t)
	writeMailTestFile(t, dovecotUsersPath, "user@example.com:{CRYPT}$6$secret\n", 0o640)
	defer func() {
		_ = os.Chown(dovecotUsersPath, os.Geteuid(), os.Getegid())
	}()

	tests := []struct {
		name string
		uid  int
		gid  int
	}{
		{name: "uid", uid: os.Geteuid() + 1, gid: os.Getegid()},
		{name: "gid", uid: os.Geteuid(), gid: os.Getegid() + 1},
	}
	for _, test := range tests {
		if err := os.Chown(dovecotUsersPath, test.uid, test.gid); err != nil {
			t.Fatalf("set wrong %s: %v", test.name, err)
		}
		err := validateDovecotUsersFileMetadata(dovecotUsersPath, true)
		if err == nil || !strings.Contains(err.Error(), "unsafe metadata") {
			t.Fatalf("wrong %s error = %v", test.name, err)
		}
		if err := os.Chown(dovecotUsersPath, os.Geteuid(), os.Getegid()); err != nil {
			t.Fatalf("restore ownership after %s: %v", test.name, err)
		}
	}
}

func TestDovecotUsersDevOwnershipUsesEffectiveNonRootIdentity(t *testing.T) {
	t.Setenv("CELIKPANEL_MAIL_DIR", "/dev/test-mail")
	previousIdentity := dovecotUsersEffectiveIdentity
	dovecotUsersEffectiveIdentity = func() (int, int) { return 1201, 1202 }
	t.Cleanup(func() { dovecotUsersEffectiveIdentity = previousIdentity })

	uid, gid, err := expectedDovecotUsersOwnership("/dev/test-mail/dovecot-users")
	if err != nil {
		t.Fatal(err)
	}
	if uid != 1201 || gid != 1202 {
		t.Fatalf("dev identity = %d:%d, want 1201:1202", uid, gid)
	}
}

func TestDovecotUsersProductionOwnershipUsesDovecotGroup(t *testing.T) {
	t.Setenv("CELIKPANEL_MAIL_DIR", "")
	previousLookup := lookupDovecotUsersGroup
	t.Cleanup(func() { lookupDovecotUsersGroup = previousLookup })

	lookupDovecotUsersGroup = func(name string) (*user.Group, error) {
		if name != "dovecot" {
			t.Fatalf("group lookup name = %q", name)
		}
		return &user.Group{Name: name, Gid: "4242"}, nil
	}
	uid, gid, err := expectedDovecotUsersOwnership("/etc/dovecot/users")
	if err != nil {
		t.Fatal(err)
	}
	if uid != 0 || gid != 4242 {
		t.Fatalf("production ownership = %d:%d, want 0:4242", uid, gid)
	}

	lookupDovecotUsersGroup = func(string) (*user.Group, error) {
		return nil, errors.New("lookup unavailable")
	}
	if _, _, err := expectedDovecotUsersOwnership("/etc/dovecot/users"); err == nil {
		t.Fatal("group lookup failure unexpectedly accepted")
	}

	lookupDovecotUsersGroup = func(string) (*user.Group, error) {
		return &user.Group{Name: "dovecot", Gid: "invalid"}, nil
	}
	if _, _, err := expectedDovecotUsersOwnership("/etc/dovecot/users"); err == nil {
		t.Fatal("invalid dovecot group id unexpectedly accepted")
	}
}

func TestDeleteMailDomainRejectsWriteLessUnsafeDovecotMetadata(t *testing.T) {
	request := configureMailDomainDeletionTest(t)
	const before = "keep@other.test:{CRYPT}$6$do-not-log-this-hash\n"
	writeMailTestFile(t, dovecotUsersPath, before, 0o640)
	if err := os.Chmod(dovecotUsersPath, 0o644); err != nil {
		t.Fatal(err)
	}
	writeCalled := false
	mailMutationWrite = func(string, []byte, os.FileMode) error {
		writeCalled = true
		return errors.New("unexpected write")
	}

	var response transport.DeleteMailDomainResponse
	err := (&Agent{}).DeleteMailDomain(&request, &response)
	if err == nil || !strings.Contains(err.Error(), "unsafe metadata") {
		t.Fatalf("domain deletion error = %v", err)
	}
	if response.Applied || response.Quarantined || writeCalled {
		t.Fatalf("response=%+v writeCalled=%v", response, writeCalled)
	}
	if strings.Contains(err.Error(), "do-not-log-this-hash") {
		t.Fatalf("domain deletion error leaked hash: %v", err)
	}
	if got := readMailTestFile(t, dovecotUsersPath); got != before {
		t.Fatalf("refused domain deletion changed passwd-file: %q", got)
	}
}

func TestDovecotUsersMetadataRollsBackWithMailTransaction(t *testing.T) {
	configureMailMutationTest(t)
	const before = "existing@example.com:{CRYPT}$6$existing-hash\n"
	writeMailTestFile(t, dovecotUsersPath, before, 0o640)
	writeMailTestFile(t, postfixVBoxPath, "existing@example.com example.com/existing/\n", 0o644)
	writeMailTestFile(t, postfixDomainsPath, "example.com OK\n", 0o644)
	beforeMode, beforeUID, beforeGID, err := dovecotUsersTestMetadata(dovecotUsersPath)
	if err != nil {
		t.Fatal(err)
	}
	mailMutationPostmap = func(context.Context, string) error {
		if err := os.Chmod(dovecotUsersPath, 0o644); err != nil {
			return err
		}
		return errors.New("postmap failed")
	}

	var response transport.MailMutationResponse
	err = (&Agent{}).AddMailAccount(&MailAccount{
		Email: "new@example.com", Password: "new-secret-password", QuotaMB: 100,
	}, &response)
	if err == nil || response.Applied {
		t.Fatalf("postmap failure = %v response=%+v", err, response)
	}
	afterMode, afterUID, afterGID, statErr := dovecotUsersTestMetadata(dovecotUsersPath)
	if statErr != nil {
		t.Fatal(statErr)
	}
	if afterMode != beforeMode || afterUID != beforeUID || afterGID != beforeGID {
		t.Fatalf(
			"rollback metadata = %#o %d:%d, want %#o %d:%d",
			afterMode, afterUID, afterGID, beforeMode, beforeUID, beforeGID,
		)
	}
	if got := readMailTestFile(t, dovecotUsersPath); got != before {
		t.Fatalf("rollback content = %q, want %q", got, before)
	}
}

func dovecotUsersTestMetadata(path string) (os.FileMode, int, int, error) {
	_, mode, uid, gid, err := secureSnapshotMailFile(path)
	return mode, uid, gid, err
}
