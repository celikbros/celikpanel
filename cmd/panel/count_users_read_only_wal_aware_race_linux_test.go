//go:build linux

package main

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/alicelik/celikpanel/internal/auth"
	"golang.org/x/sys/unix"
)

func TestCountUsableUsersReadOnlyWALAwareCountsOnlyLoginCapableAdministrators(t *testing.T) {
	validHash, err := auth.HashPassword("s5-d2-test-only-password")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name                string
		username            string
		passwordHash        string
		role                string
		accountType         string
		status              string
		dropIdentityTrigger bool
		totpEnabled         bool
		want                int
	}{
		{
			name:         "active account administrator",
			username:     "active-admin",
			passwordHash: validHash,
			role:         "admin",
			accountType:  "account",
			status:       "active",
			want:         1,
		},
		{
			name:         "customer is not an administrator",
			username:     "active-customer",
			passwordHash: validHash,
			role:         "customer",
			accountType:  "account",
			status:       "active",
		},
		{
			name:         "reseller is not an administrator",
			username:     "active-reseller",
			passwordHash: validHash,
			role:         "reseller",
			accountType:  "account",
			status:       "active",
		},
		{
			name:         "suspended administrator cannot log in",
			username:     "suspended-admin",
			passwordHash: validHash,
			role:         "admin",
			accountType:  "account",
			status:       "suspended",
		},
		{
			name:         "non-suspended status matches current login gate",
			username:     "pending-admin",
			passwordHash: validHash,
			role:         "admin",
			accountType:  "account",
			status:       "pending",
			want:         1,
		},
		{
			name:         "TOTP second factor does not create a false admission refusal",
			username:     "totp-admin",
			passwordHash: validHash,
			role:         "admin",
			accountType:  "account",
			status:       "active",
			totpEnabled:  true,
			want:         1,
		},
		{
			name:                "additional-user marker is not a canonical administrator",
			username:            "noncanonical-admin",
			passwordHash:        validHash,
			role:                "admin",
			accountType:         "additional_user",
			status:              "active",
			dropIdentityTrigger: true,
		},
		{
			name:         "malformed password hash cannot authenticate",
			username:     "invalid-hash-admin",
			passwordHash: "not-an-argon2id-hash",
			role:         "admin",
			accountType:  "account",
			status:       "active",
		},
		{
			name:         "migration-one dead placeholder cannot authenticate",
			username:     "admin",
			passwordHash: deadPlaceholderAdminPasswordHash,
			role:         "admin",
			accountType:  "account",
			status:       "active",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "panel.sqlite")
			database := openWALAwareTestDatabase(t, path)
			defer database.Close()
			if test.dropIdentityTrigger {
				if _, err := database.GetDB().Exec(`
					DROP TRIGGER validate_additional_user_identity_insert
				`); err != nil {
					t.Fatalf("drop identity trigger for corrupt-row fixture: %v", err)
				}
			}
			if _, err := database.GetDB().Exec(`
				INSERT INTO users (
					username, password_hash, email, role, account_type, status
				) VALUES (?, ?, ?, ?, ?, ?)
			`,
				test.username,
				test.passwordHash,
				test.username+"@example.test",
				test.role,
				test.accountType,
				test.status,
			); err != nil {
				t.Fatalf("write user fixture: %v", err)
			}
			if test.totpEnabled {
				if _, err := database.GetDB().Exec(`
					UPDATE users
					SET totp_enabled = 1, totp_secret = 'JBSWY3DPEHPK3PXP'
					WHERE username = ?
				`, test.username); err != nil {
					t.Fatalf("enable TOTP fixture: %v", err)
				}
			}
			requireNonEmptyWAL(t, path)

			count, err := countUsableUsersReadOnlyWALAware(path)
			if err != nil {
				t.Fatalf("count login-capable administrators: %v", err)
			}
			if count != test.want {
				t.Fatalf("login-capable administrator count = %d, want %d", count, test.want)
			}
		})
	}
}

func TestCountUsableUsersReadOnlyWALAwareRetriesConcurrentCommittedWALWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panel.sqlite")
	database := openWALAwareTestDatabase(t, path)
	defer database.Close()
	validHash := mustAdmissionPasswordHash(t)
	if _, err := database.GetDB().Exec(`
		INSERT INTO users (username, password_hash, email, role)
		VALUES ('race-admin', ?, 'race-admin@example.test', 'admin')
	`, validHash); err != nil {
		t.Fatalf("write initial WAL-only administrator: %v", err)
	}
	requireNonEmptyWAL(t, path)
	initialWAL, err := os.Stat(path + "-wal")
	if err != nil {
		t.Fatal(err)
	}

	var (
		attempts          []int
		afterWriterSource map[string]concurrentAdmissionSourceState
	)
	count, err := countUsableUsersReadOnlyWALAwareWithAttemptHook(
		path,
		func(attempt int) error {
			attempts = append(attempts, attempt)
			if attempt != 1 {
				return nil
			}
			if _, err := database.GetDB().Exec(`
				INSERT INTO panel_settings(key, value)
				VALUES ('s5-d2-concurrent-write', 'committed-after-pin')
			`); err != nil {
				return fmt.Errorf("commit deterministic concurrent WAL write: %w", err)
			}
			currentWAL, err := os.Stat(path + "-wal")
			if err != nil {
				return fmt.Errorf("inspect WAL after deterministic write: %w", err)
			}
			if currentWAL.Size() <= initialWAL.Size() {
				return fmt.Errorf(
					"deterministic WAL write did not append: before=%d after=%d",
					initialWAL.Size(),
					currentWAL.Size(),
				)
			}
			afterWriterSource = captureConcurrentAdmissionSourceState(t, path)
			for _, suffix := range []string{"", "-wal", "-shm"} {
				if !afterWriterSource[suffix].exists {
					return fmt.Errorf("deterministic fixture has no SQLite %q source", suffix)
				}
			}
			return nil
		},
	)
	if err != nil {
		t.Fatalf("healthy database was refused after a concurrent committed WAL write: %v", err)
	}
	if count != 1 {
		t.Fatalf("usable user count = %d, want 1", count)
	}
	if !reflect.DeepEqual(attempts, []int{1, 2}) {
		t.Fatalf("attempt sequence = %v, want [1 2]", attempts)
	}
	assertConcurrentAdmissionSourceStateUnchanged(t, path, afterWriterSource)
}

func TestCountUsableUsersReadOnlyWALAwareRetriesBoundedlyAndRejectsStableCorruption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corrupt.sqlite")
	if err := os.WriteFile(path, make([]byte, 4096), 0o600); err != nil {
		t.Fatal(err)
	}
	before := captureConcurrentAdmissionSourceState(t, path)
	var attempts []int
	if _, err := countUsableUsersReadOnlyWALAwareWithAttemptHook(
		path,
		func(attempt int) error {
			attempts = append(attempts, attempt)
			return nil
		},
	); err == nil {
		t.Fatal("stable corrupt database was accepted")
	}
	wantAttempts := make([]int, maxReadOnlyWALAwareUserCountAttempts)
	for index := range wantAttempts {
		wantAttempts[index] = index + 1
	}
	if !reflect.DeepEqual(attempts, wantAttempts) {
		t.Fatalf("attempt sequence = %v, want %v", attempts, wantAttempts)
	}
	assertConcurrentAdmissionSourceStateUnchanged(t, path, before)
}

func mustAdmissionPasswordHash(t *testing.T) string {
	t.Helper()
	passwordHash, err := auth.HashPassword("admission-test-only-password")
	if err != nil {
		t.Fatal(err)
	}
	return passwordHash
}

type concurrentAdmissionSourceState struct {
	exists  bool
	stat    unix.Stat_t
	content [sha256.Size]byte
}

func captureConcurrentAdmissionSourceState(
	t *testing.T,
	databasePath string,
) map[string]concurrentAdmissionSourceState {
	t.Helper()
	states := make(map[string]concurrentAdmissionSourceState, 3)
	for _, suffix := range []string{"", "-wal", "-shm"} {
		path := databasePath + suffix
		fd, err := unix.Open(
			path,
			unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOATIME|unix.O_NOFOLLOW,
			0,
		)
		if err == unix.ENOENT {
			states[suffix] = concurrentAdmissionSourceState{}
			continue
		}
		if err != nil {
			t.Fatalf("open SQLite source %q without atime: %v", path, err)
		}
		file := os.NewFile(uintptr(fd), path)
		if file == nil {
			_ = unix.Close(fd)
			t.Fatalf("wrap SQLite source descriptor %q", path)
		}
		var stat unix.Stat_t
		if err := unix.Fstat(fd, &stat); err != nil {
			_ = file.Close()
			t.Fatalf("inspect SQLite source %q: %v", path, err)
		}
		digest := sha256.New()
		if _, err := io.Copy(digest, file); err != nil {
			_ = file.Close()
			t.Fatalf("hash SQLite source %q: %v", path, err)
		}
		if err := file.Close(); err != nil {
			t.Fatalf("close SQLite source %q: %v", path, err)
		}
		var content [sha256.Size]byte
		copy(content[:], digest.Sum(nil))
		states[suffix] = concurrentAdmissionSourceState{
			exists:  true,
			stat:    stat,
			content: content,
		}
	}
	return states
}

func assertConcurrentAdmissionSourceStateUnchanged(
	t *testing.T,
	databasePath string,
	want map[string]concurrentAdmissionSourceState,
) {
	t.Helper()
	got := captureConcurrentAdmissionSourceState(t, databasePath)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("read-only admission changed main/WAL/SHM after the writer stopped\nwant: %#v\ngot:  %#v", want, got)
	}
}
