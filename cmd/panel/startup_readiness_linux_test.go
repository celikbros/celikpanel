//go:build linux

package main

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	paneldb "github.com/alicelik/celikpanel/internal/db"
	"github.com/alicelik/celikpanel/internal/secrets"
)

type startupReadinessFixture struct {
	root, data, web string
}

func newStartupReadinessFixture(t *testing.T, withUser bool) startupReadinessFixture {
	t.Helper()
	repository, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	latest, err := paneldb.HighestEmbeddedMigrationVersion()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	f := startupReadinessFixture{root: root, data: filepath.Join(root, "data"), web: filepath.Join(root, "web")}
	for _, directory := range []string{f.data, f.web} {
		if err := os.MkdirAll(directory, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	database := filepath.Join(f.data, "celikpanel.db")
	seedRecoveryCheckerDatabase(t, repository, database, latest)
	if withUser {
		handle, err := sql.Open("sqlite", sqliteSnapshotURI(database, false))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := handle.Exec(`INSERT INTO users (username, password_hash, email, role) VALUES ('owner', 'fixture', 'owner@example.invalid', 'admin')`); err != nil {
			handle.Close()
			t.Fatal(err)
		}
		if err := handle.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := secrets.LoadOrCreate(filepath.Join(f.data, "secret.key")); err != nil {
		t.Fatal(err)
	}
	if err := generateSelfSigned(filepath.Join(f.data, "tls", "panel.crt"), filepath.Join(f.data, "tls", "panel.key")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.web, "index.html"), []byte("<!doctype html><title>CelikPanel</title>"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CELIKPANEL_DATA_DIR", f.data)
	t.Setenv("CELIKPANEL_WEB_DIR", f.web)
	t.Setenv("CELIKPANEL_LISTEN", ":2083")
	t.Setenv("CELIKPANEL_TLS", "1")
	for _, name := range []string{"CELIKPANEL_TLS_CERT", "CELIKPANEL_TLS_KEY", "CELIKPANEL_TLS_DIR", "CELIKPANEL_PANEL_INSECURE_COOKIES_FLAG", "CELIKPANEL_PANEL_DEMO_FLAG"} {
		t.Setenv(name, "")
	}
	return f
}

// Every entry's path, type, mode, size, modification time and content. Access
// time is deliberately excluded: reading is not a write.
func startupReadinessTreeState(t *testing.T, root string) []string {
	t.Helper()
	var state []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		line := fmt.Sprintf("%s|%s|%d|%d", strings.TrimPrefix(path, root), info.Mode(), info.Size(), info.ModTime().UnixNano())
		if info.Mode().IsRegular() {
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			line += fmt.Sprintf("|%x", sha256.Sum256(raw))
		}
		state = append(state, line)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(state)
	return state
}

func startupTestDeps(licenseErr error) startupReadinessDeps {
	deps := defaultStartupReadinessDeps()
	deps.license = func(string) error { return licenseErr }
	return deps
}

func TestStartupReadinessPassesOnInstalledLayoutWithoutWriting(t *testing.T) {
	f := newStartupReadinessFixture(t, true)
	before := startupReadinessTreeState(t, f.root)
	report, err := checkStartupReadiness(startupTestDeps(nil))
	if err != nil {
		t.Fatalf("valid fixture refused: %v", err)
	}
	if after := startupReadinessTreeState(t, f.root); !reflect.DeepEqual(before, after) {
		t.Fatalf("startup readiness check changed the tree\nbefore=%v\nafter=%v", before, after)
	}
	if report.scheme != "https" || report.listen != ":2083" || len(report.pins) == 0 {
		t.Fatalf("unexpected report: %+v", report)
	}
	for _, pin := range report.pins {
		if !strings.HasPrefix(pin, "sha256//") || len(pin) != len("sha256//")+44 {
			t.Fatalf("pin is not a curl SPKI pin: %q", pin)
		}
	}
	if !reflect.DeepEqual(report.wouldCreate, []string{"sqlite-wal", "sqlite-shm"}) {
		t.Fatalf("would-create report = %v", report.wouldCreate)
	}
	var out bytes.Buffer
	writeStartupReadinessReport(&out, report)
	if !strings.HasSuffix(out.String(), "ready\n") || !strings.Contains(out.String(), "scheme=https\n") {
		t.Fatalf("report output = %q", out.String())
	}
}

func TestStartupReadinessFailsWithTypedReasonAndNoWrite(t *testing.T) {
	for _, test := range []struct {
		name, code string
		withUser   bool
		licenseErr error
		mutate     func(t *testing.T, f startupReadinessFixture)
	}{
		{name: "mismatched-tls-pair", code: "tls_pair_invalid", withUser: true, mutate: func(t *testing.T, f startupReadinessFixture) {
			other := t.TempDir()
			if err := generateSelfSigned(filepath.Join(other, "panel.crt"), filepath.Join(other, "panel.key")); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(filepath.Join(other, "panel.key"))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(f.data, "tls", "panel.key"), raw, 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "missing-tls-pair", code: "tls_pair_missing", withUser: true, mutate: func(t *testing.T, f startupReadinessFixture) {
			if err := os.RemoveAll(filepath.Join(f.data, "tls")); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "incomplete-tls-pair", code: "tls_settings_invalid", withUser: true, mutate: func(t *testing.T, f startupReadinessFixture) {
			if err := os.Remove(filepath.Join(f.data, "tls", "panel.key")); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "unreadable-database", code: "database_unverified", withUser: true, mutate: func(t *testing.T, f startupReadinessFixture) {
			if err := os.WriteFile(filepath.Join(f.data, "celikpanel.db"), bytes.Repeat([]byte("not sqlite "), 64), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "missing-database", code: "database_missing", withUser: true, mutate: func(t *testing.T, f startupReadinessFixture) {
			if err := os.Remove(filepath.Join(f.data, "celikpanel.db")); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "foreign-table", code: "database_unverified", withUser: true, mutate: func(t *testing.T, f startupReadinessFixture) {
			handle, err := sql.Open("sqlite", sqliteSnapshotURI(filepath.Join(f.data, "celikpanel.db"), false))
			if err != nil {
				t.Fatal(err)
			}
			defer handle.Close()
			if _, err := handle.Exec("CREATE TABLE owner_extra(value TEXT)"); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "no-users", code: "no_users", withUser: false},
		{name: "missing-web-index", code: "web_index_missing", withUser: true, mutate: func(t *testing.T, f startupReadinessFixture) {
			if err := os.Remove(filepath.Join(f.web, "index.html")); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "missing-secret-key", code: "secret_key_missing", withUser: true, mutate: func(t *testing.T, f startupReadinessFixture) {
			if err := os.Remove(filepath.Join(f.data, "secret.key")); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "license-configuration", code: "license_configuration_failed", withUser: true, licenseErr: errors.New("machine identity unavailable")},
		{name: "plain-http-secure-cookies", code: "tls_required", withUser: true, mutate: func(t *testing.T, f startupReadinessFixture) {
			t.Setenv("CELIKPANEL_TLS", "0")
		}},
		{name: "invalid-listen", code: "listen_address_invalid", withUser: true, mutate: func(t *testing.T, f startupReadinessFixture) {
			t.Setenv("CELIKPANEL_LISTEN", "2083")
		}},
		{name: "missing-data-directory", code: "data_directory_missing", withUser: true, mutate: func(t *testing.T, f startupReadinessFixture) {
			t.Setenv("CELIKPANEL_DATA_DIR", filepath.Join(f.root, "absent"))
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newStartupReadinessFixture(t, test.withUser)
			if test.mutate != nil {
				test.mutate(t, f)
			}
			before := startupReadinessTreeState(t, f.root)
			_, err := checkStartupReadiness(startupTestDeps(test.licenseErr))
			var failure *startupReadinessFailure
			if !errors.As(err, &failure) || failure.code != test.code {
				t.Fatalf("error = %v, want typed code %s", err, test.code)
			}
			line := failure.Error()
			if strings.Contains(line, "\n") || len(line) > 240 || strings.Contains(line, f.root) ||
				!strings.HasPrefix(line, "panel startup check failed: "+test.code+": ") {
				t.Fatalf("reason line is not one bounded, path-free product line: %q", line)
			}
			if after := startupReadinessTreeState(t, f.root); !reflect.DeepEqual(before, after) {
				t.Fatalf("failed check changed the tree\nbefore=%v\nafter=%v", before, after)
			}
		})
	}
}

func TestStartupReadinessEntryIsAClosedMode(t *testing.T) {
	var out, errOut bytes.Buffer
	if handled, _ := runStartupReadinessEntry([]string{"--migrate-only"}, &out, &errOut); handled {
		t.Fatal("unrelated arguments were handled")
	}
	for _, args := range [][]string{
		{startupReadinessFlag, "--demo"},
		{"--insecure-cookies", startupReadinessFlag},
		{startupReadinessFlag + "=true"},
		{startupReadinessFlag, startupReadinessFlag},
	} {
		out.Reset()
		errOut.Reset()
		handled, status := runStartupReadinessEntry(args, &out, &errOut)
		if !handled || status != 2 || out.Len() != 0 ||
			errOut.String() != "panel startup check failed: usage: the startup readiness check accepts no other argument\n" {
			t.Fatalf("%v: handled=%v status=%d stdout=%q stderr=%q", args, handled, status, out.String(), errOut.String())
		}
	}
}
