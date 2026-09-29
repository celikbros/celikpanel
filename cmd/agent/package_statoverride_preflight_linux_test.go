//go:build linux

package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/bindroot"
)

type fakeStatOverrideDB struct {
	entries   []string
	users     map[string]bool
	groups    map[string]bool
	lookupErr map[string]error
	removeErr error
	keepOnRm  bool
	removed   []string
	logs      []string
	lookups   int
}

func (db *fakeStatOverrideDB) ops() dpkgStatOverridePreflightOps {
	listOf := func(filter string) ([]byte, error) {
		var lines []string
		for _, entry := range db.entries {
			if filter == "" || strings.HasSuffix(entry, " "+filter) {
				lines = append(lines, entry+"\n")
			}
		}
		if len(lines) == 0 {
			return nil, testBINDExitError(1)
		}
		return []byte(strings.Join(lines, "")), nil
	}
	return dpkgStatOverridePreflightOps{
		list:     func() ([]byte, error) { return listOf("") },
		listPath: func(path string) ([]byte, error) { return listOf(path) },
		remove: func(path string) ([]byte, error) {
			db.removed = append(db.removed, path)
			if !db.keepOnRm {
				kept := db.entries[:0]
				for _, entry := range db.entries {
					if !strings.HasSuffix(entry, " "+path) {
						kept = append(kept, entry)
					}
				}
				db.entries = kept
			}
			return nil, db.removeErr
		},
		lookup: func(database, name string) (bool, error) {
			db.lookups++
			if err := db.lookupErr[database+":"+name]; err != nil {
				return false, err
			}
			if database == "passwd" {
				return db.users[name], nil
			}
			return db.groups[name], nil
		},
		logf: func(format string, args ...any) {
			db.logs = append(db.logs, fmt.Sprintf(format, args...))
		},
	}
}

func baseStatOverrideDB() *fakeStatOverrideDB {
	return &fakeStatOverrideDB{
		users:  map[string]bool{"root": true, "messagebus": true},
		groups: map[string]bool{"root": true, "messagebus": true, "bind": true},
	}
}

const legacyBINDEntry = "root bind 1775 /var/cache/bind"

func TestPackagePreflightRemovesOnlyOwnStaleBINDOverride(t *testing.T) {
	// Batch 6b cell c6: the owner purged bind9, the group went with it.
	db := baseStatOverrideDB()
	delete(db.groups, "bind")
	db.entries = []string{
		legacyBINDEntry,
		"root messagebus 4754 /usr/lib/dbus-1.0/dbus-daemon-launch-helper",
	}
	if err := runDpkgStatOverridePreflight(db.ops()); err != nil {
		t.Fatalf("own stale override was not repaired: %v", err)
	}
	if len(db.removed) != 1 || db.removed[0] != bindroot.APTStatOverridePath {
		t.Fatalf("removed = %v, want exactly /var/cache/bind", db.removed)
	}
	if len(db.entries) != 1 || !strings.Contains(db.entries[0], "messagebus") {
		t.Fatalf("an unrelated override changed: %v", db.entries)
	}
	if len(db.logs) != 1 || db.logs[0] != ownStaleBINDStatOverrideRemovedLog ||
		!strings.Contains(db.logs[0], "if you remove bind9 yourself, also run `dpkg-statoverride --remove /var/cache/bind`") {
		t.Fatalf("logs = %q, want the one plain sentence", db.logs)
	}
}

func TestPackagePreflightLeavesValidDatabaseAlone(t *testing.T) {
	for name, entries := range map[string][]string{
		"empty":            nil,
		"legacy-group-set": {legacyBINDEntry},
		"numeric-ids":      {"#0 #104 1775 /var/cache/bind", "#1234 #5678 0755 /srv/x"},
		"valid-names":      {"root messagebus 4754 /usr/lib/dbus-1.0/dbus-daemon-launch-helper"},
	} {
		t.Run(name, func(t *testing.T) {
			db := baseStatOverrideDB()
			db.entries = entries
			if err := runDpkgStatOverridePreflight(db.ops()); err != nil {
				t.Fatalf("valid database refused: %v", err)
			}
			if len(db.removed) != 0 {
				t.Fatalf("removed %v from a valid database", db.removed)
			}
			if name == "numeric-ids" && db.lookups != 0 {
				t.Fatalf("numeric ids were looked up %d times", db.lookups)
			}
		})
	}
}

func TestPackagePreflightRefusesForeignBrokenOverrideBeforeAnyChange(t *testing.T) {
	for name, entries := range map[string][]string{
		"missing-group":        {"root ghostgroup 0750 /srv/app"},
		"missing-user":         {"ghostuser root 0700 /srv/data"},
		"own-and-foreign":      {legacyBINDEntry, "root ghostgroup 0750 /srv/app"},
		"different-bind-shape": {"root bind 2775 /var/cache/bind"},
	} {
		t.Run(name, func(t *testing.T) {
			db := baseStatOverrideDB()
			delete(db.groups, "bind")
			db.entries = append([]string(nil), entries...)
			err := runDpkgStatOverridePreflight(db.ops())
			sentence := operatorSentenceOf(err)
			if err == nil || sentence == "" {
				t.Fatalf("broken foreign override was not refused with an operator sentence: %v", err)
			}
			if len(db.removed) != 0 {
				t.Fatalf("refusal changed the database: removed %v", db.removed)
			}
			if !strings.Contains(sentence, "dpkg-statoverride --remove ") ||
				!strings.Contains(sentence, "did not create that override and changed nothing") ||
				!strings.Contains(sentence, "then starts the same change again") {
				t.Fatalf("refusal does not name the owner's command and next step: %q", sentence)
			}
		})
	}
	db := baseStatOverrideDB()
	db.entries = []string{"root g1 0750 /srv/a", "u2 root 0700 /srv/b", "root g3 0750 /srv/c"}
	sentence := operatorSentenceOf(runDpkgStatOverridePreflight(db.ops()))
	if !strings.Contains(sentence, "/srv/a names the group 'g1'") ||
		!strings.Contains(sentence, "2 more override(s)") || !strings.Contains(sentence, "/srv/b, /srv/c") {
		t.Fatalf("multi-entry refusal = %q", sentence)
	}
}

func TestPackagePreflightTreatsUnknownAsUnknown(t *testing.T) {
	t.Run("lookup-error", func(t *testing.T) {
		db := baseStatOverrideDB()
		db.entries = []string{"root ldapgroup 0750 /srv/app"}
		db.lookupErr = map[string]error{"group:ldapgroup": errors.New("nss timeout")}
		if err := runDpkgStatOverridePreflight(db.ops()); err != nil {
			t.Fatalf("an unresolved lookup was treated as broken: %v", err)
		}
		if len(db.logs) != 1 || !strings.Contains(db.logs[0], "left it to the package manager") {
			t.Fatalf("unknown lookup was not logged: %q", db.logs)
		}
	})
	t.Run("unreadable-list", func(t *testing.T) {
		db := baseStatOverrideDB()
		ops := db.ops()
		ops.list = func() ([]byte, error) { return []byte("garbage without fields\n"), nil }
		if err := runDpkgStatOverridePreflight(ops); err != nil {
			t.Fatalf("an unreadable list was treated as broken: %v", err)
		}
	})
	t.Run("list-command-failure", func(t *testing.T) {
		db := baseStatOverrideDB()
		ops := db.ops()
		ops.list = func() ([]byte, error) { return nil, testBINDExitError(2) }
		if err := runDpkgStatOverridePreflight(ops); err != nil {
			t.Fatalf("a failed list was treated as broken: %v", err)
		}
	})
}

func TestPackagePreflightOwnRemovalFailureNamesCommand(t *testing.T) {
	db := baseStatOverrideDB()
	delete(db.groups, "bind")
	db.entries = []string{legacyBINDEntry}
	db.keepOnRm = true
	db.removeErr = errors.New("dpkg lock busy")
	err := runDpkgStatOverridePreflight(db.ops())
	sentence := operatorSentenceOf(err)
	if !strings.Contains(sentence, "`dpkg-statoverride --remove /var/cache/bind`") ||
		!strings.Contains(sentence, "could not remove it") {
		t.Fatalf("failed own removal = %v / %q", err, sentence)
	}
}

func TestParseDpkgStatOverrideListKeepsPathsWithSpaces(t *testing.T) {
	entries, err := parseDpkgStatOverrideList([]byte("root root 0755 /srv/with space/dir\n"), nil)
	if err != nil || len(entries) != 1 || entries[0].path != "/srv/with space/dir" {
		t.Fatalf("entries = %#v, err = %v", entries, err)
	}
	if _, err := parseDpkgStatOverrideList([]byte("root root 0755 /x"), nil); err == nil {
		t.Fatal("unterminated output was accepted")
	}
	if _, err := parseDpkgStatOverrideList([]byte("root root 9999 /x\n"), nil); err == nil {
		t.Fatal("non-octal mode was accepted")
	}
}

func TestClassifyGetentLookup(t *testing.T) {
	if found, err := classifyGetentLookup([]byte("bind:x:104:\n"), nil); !found || err != nil {
		t.Fatalf("existing group = %v, %v", found, err)
	}
	if found, err := classifyGetentLookup(nil, testBINDExitError(2)); found || err != nil {
		t.Fatalf("missing group = %v, %v", found, err)
	}
	if _, err := classifyGetentLookup(nil, testBINDExitError(1)); err == nil {
		t.Fatal("getent usage failure was treated as a verified absence")
	}
}

func TestPackageManagerFailureCarriesItsOwnReason(t *testing.T) {
	out := []byte("Reading package lists...\ndpkg: unrecoverable fatal error, aborting:\n" +
		" unknown system group 'bind' in statoverride file; the system group got removed\n" +
		"E: Sub-process /usr/bin/dpkg returned an error code (2)\n")
	err := fmt.Errorf("install BIND in no-start mode: %w",
		newPackageManagerCommandError([]string{"bind9"}, out, errors.New("exit status 100")))
	if !strings.HasPrefix(err.Error(), "install BIND in no-start mode: exit status 100: Reading package lists...") {
		t.Fatalf("the logged error changed shape: %q", err.Error())
	}
	sentence := operatorSentenceOf(err)
	if !strings.HasPrefix(sentence, "The server's package manager did not install bind9 (exit status 100).") ||
		!strings.Contains(sentence, ownerBINDRemovalStatOverrideAdvice) ||
		!strings.HasSuffix(sentence, "Package manager: dpkg: unknown system group 'bind' in statoverride file; the system group got removed") {
		t.Fatalf("operator sentence = %q", sentence)
	}
	wire := dnsEngineSwitchIncompleteText(err)
	if !strings.HasPrefix(wire, dnsEngineSwitchIncompleteNamedPrefix) ||
		!strings.Contains(wire, "unknown system group 'bind'") {
		t.Fatalf("wire text = %q", wire)
	}
	if got := dnsEngineSwitchIncompleteText(errors.New("anything else")); got != dnsEngineSwitchIncompleteAgentLog {
		t.Fatalf("an unnamed failure changed its wire text: %q", got)
	}
	long := dnsEngineSwitchIncompleteText(&hostOperatorRefusal{sentence: strings.Repeat("x", 2000) + "\nforged line"})
	if strings.Contains(long, "\n") || len([]rune(long)) > len(dnsEngineSwitchIncompleteNamedPrefix)+dnsEngineSwitchOperatorSentenceLimit+3 {
		t.Fatalf("wire sentence was not bounded to one line: %d runes", len([]rune(long)))
	}
}

// The preflight must run before the first effect of every DNS operation that
// installs packages: the source-ownership receipt, the install receipt, the
// guard mask and apt itself. A refusal therefore leaves nothing to reconcile.
func TestPackagePreflightPrecedesEveryDNSInstallEffect(t *testing.T) {
	for _, site := range []struct{ file, function string }{
		{"dns_engine_host.go", "func (hostDNSEngineBackend) Switch("},
		{"dns_engine_pdns_switch.go", "func switchToPDNSOnCertifiedProfile("},
	} {
		raw, err := os.ReadFile(site.file)
		if err != nil {
			t.Fatal(err)
		}
		source := string(raw)
		start := strings.Index(source, site.function)
		if start < 0 {
			t.Fatalf("%s is missing", site.function)
		}
		body := source[start+len(site.function):]
		if end := strings.Index(body, "\nfunc "); end > 0 {
			body = body[:end]
		}
		preflight := strings.Index(body, "packageStatOverridePreflight(ctx)")
		if preflight < 0 {
			t.Fatalf("%s does not run the package manager preflight", site.function)
		}
		for _, effect := range []string{
			"publishDNSEngineSourceOwnership(",
			"assumeExistingDNSEnginePackageOwnership(",
			"installOwnedDNSEnginePackages(",
			"PackagesWithGuard(",
		} {
			if at := strings.Index(body, effect); at < 0 || at < preflight {
				t.Fatalf("%s: %s is not after the preflight (at %d, preflight %d)", site.function, effect, at, preflight)
			}
		}
	}
}
