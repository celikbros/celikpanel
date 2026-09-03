package main

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	paneldb "github.com/alicelik/celikpanel/internal/db"
	_ "modernc.org/sqlite"
)

// controlPlaneTestKey is a fixed, obviously fake key. Nothing on a real host is
// ever sealed with it.
const controlPlaneTestKey = "cpk1-0000-0000-0000-0000-0000-0000-0000-0000-0000-0000-0000-0000-0000"

// controlPlaneTestMarker is written into the panel database so the restored
// copy can be proved to be the same database, not merely a valid one.
const controlPlaneTestMarker = "control_plane_round_trip_marker"

type controlPlaneTestTree struct {
	Root  controlPlaneRoots
	Files map[string][]byte
}

// newControlPlaneTestTree builds a complete, miniature control plane under one
// temporary directory: a migrated panel database, the secret key, the
// configuration, the agent private state (including two entries that must NOT
// be archived), a DKIM key, a WireGuard configuration and the panel TLS pair.
func newControlPlaneTestTree(t *testing.T) controlPlaneTestTree {
	t.Helper()
	base := t.TempDir()
	roots := controlPlaneRoots{
		DataDir:       filepath.Join(base, "data"),
		ConfDir:       filepath.Join(base, "etc", "celikpanel"),
		AgentStateDir: filepath.Join(base, "agent-private"),
		DKIMDir:       filepath.Join(base, "dkim"),
		WireGuardDir:  filepath.Join(base, "wireguard"),
		// The TLS directory sits inside the data directory exactly as it does
		// in production, so the nested-root case is the one that is tested.
		TLSDir: filepath.Join(base, "data", "tls"),
	}
	for _, directory := range []string{
		roots.DataDir,
		roots.ConfDir,
		roots.AgentStateDir,
		filepath.Join(roots.AgentStateDir, controlPlaneAgentStateSnapshotsDir),
		filepath.Join(roots.DKIMDir, "keys", "example.com"),
		roots.WireGuardDir,
		roots.TLSDir,
	} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatalf("create %s: %v", directory, err)
		}
	}

	databasePath := filepath.Join(roots.DataDir, controlPlaneDatabaseBasename)
	database, err := paneldb.NewSQLiteDB(databasePath)
	if err != nil {
		t.Fatalf("create the panel database: %v", err)
	}
	if _, err := database.GetDB().Exec(
		`INSERT INTO panel_settings(key, value) VALUES (?, ?)`,
		controlPlaneTestMarker,
		"the brain of host A",
	); err != nil {
		database.Close()
		t.Fatalf("seed the panel database: %v", err)
	}
	database.Close()

	files := map[string][]byte{
		filepath.Join(roots.DataDir, controlPlaneSecretKeyBasename): []byte("not-a-real-secret-key\n"),
		filepath.Join(roots.ConfDir, "panel.env"):                   []byte("CELIKPANEL_LISTEN=:2083\n"),
		filepath.Join(roots.ConfDir, "agent.token"):                 []byte("not-a-real-agent-token\n"),
		filepath.Join(roots.ConfDir, "firewall.nft"):                []byte("table inet celikpanel {}\n"),
		filepath.Join(roots.AgentStateDir, "service-mutations.json"): []byte(
			`{"version":2,"entries":[]}` + "\n",
		),
		filepath.Join(roots.AgentStateDir, "dns-engine-state.json"): []byte(
			`{"engine":"bind","epoch":7}` + "\n",
		),
		filepath.Join(roots.AgentStateDir, "dns-engine-ownership-bind.json"): []byte(
			`{"owner":"bind"}` + "\n",
		),
		filepath.Join(roots.DKIMDir, "keys", "example.com", "mail.private"): []byte(
			"-----BEGIN PRIVATE KEY-----\nnot-a-real-dkim-key\n-----END PRIVATE KEY-----\n",
		),
		filepath.Join(roots.WireGuardDir, "wg0.conf"): []byte("[Interface]\nListenPort = 51820\n"),
		filepath.Join(roots.TLSDir, "cert.pem"):       []byte("-----BEGIN CERTIFICATE-----\nnot-real\n"),
		filepath.Join(roots.TLSDir, "key.pem"):        []byte("-----BEGIN EC PRIVATE KEY-----\nnot-real\n"),
	}
	for path, content := range files {
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	// Neither of these may reach the archive: the first is the transient system
	// SQLite snapshot subtree, the second is a staging name.
	for path, content := range map[string][]byte{
		filepath.Join(roots.AgentStateDir, controlPlaneAgentStateSnapshotsDir, "transient.db"): []byte("transient"),
		filepath.Join(roots.AgentStateDir, ".ledger.staging"):                                  []byte("staging"),
	} {
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	files[databasePath] = nil
	return controlPlaneTestTree{Root: roots, Files: files}
}

// newControlPlaneTargetRoots lays out an empty host with the same shape.
func newControlPlaneTargetRoots(t *testing.T) controlPlaneRoots {
	t.Helper()
	base := t.TempDir()
	return controlPlaneRoots{
		DataDir:       filepath.Join(base, "data"),
		ConfDir:       filepath.Join(base, "etc", "celikpanel"),
		AgentStateDir: filepath.Join(base, "agent-private"),
		DKIMDir:       filepath.Join(base, "dkim"),
		WireGuardDir:  filepath.Join(base, "wireguard"),
		TLSDir:        filepath.Join(base, "data", "tls"),
	}
}

func controlPlaneTargetBase(roots controlPlaneRoots) string {
	return filepath.Dir(roots.AgentStateDir)
}

func TestControlPlaneArchiveRoundTrip(t *testing.T) {
	source := newControlPlaneTestTree(t)
	archivePath := filepath.Join(t.TempDir(), "control-plane.cpbak")

	var creationReport bytes.Buffer
	result, err := createControlPlaneArchive(
		archivePath,
		controlPlaneTestKey,
		source.Root,
		&creationReport,
	)
	if err != nil {
		t.Fatalf("create the archive: %v", err)
	}
	if result.Members != len(source.Files) {
		t.Fatalf("archived %d members, want %d\n%s", result.Members, len(source.Files), creationReport.String())
	}
	if len(result.SHA256) != 2*sha256.Size {
		t.Fatalf("archive digest %q is not a sha256", result.SHA256)
	}
	if !strings.Contains(
		creationReport.String(),
		"archive="+archivePath+" members="+strconv.Itoa(result.Members)+" sha256="+result.SHA256,
	) {
		t.Fatalf("the summary line is missing from the report:\n%s", creationReport.String())
	}
	// No secret ever reaches the report.
	for path, content := range source.Files {
		if len(content) == 0 {
			continue
		}
		if bytes.Contains(creationReport.Bytes(), content) {
			t.Fatalf("the report printed the content of %s", path)
		}
	}
	if strings.Contains(creationReport.String(), controlPlaneTestKey) {
		t.Fatalf("the report printed the backup key")
	}

	manifest := readControlPlaneTestManifest(t, archivePath, controlPlaneTestKey)
	if manifest.SchemaVersion != durableServiceOperationSchemaVersion {
		t.Fatalf("manifest schema version %d, want %d", manifest.SchemaVersion, durableServiceOperationSchemaVersion)
	}
	if manifest.Roots != source.Root {
		t.Fatalf("manifest roots %+v, want %+v", manifest.Roots, source.Root)
	}
	// The migration version is read from the staged copy, so it is the level of
	// the database the archive actually carries.
	if manifest.DatabaseMigrationVersion != shippedControlPlaneMigrationVersion(t) {
		t.Fatalf(
			"manifest database migration version %d, want %d",
			manifest.DatabaseMigrationVersion,
			shippedControlPlaneMigrationVersion(t),
		)
	}
	archived := map[string]controlPlaneManifestEntry{}
	for _, entry := range manifest.Members {
		if entry.Type == controlPlaneManifestEntryFile {
			archived[entry.Path] = entry
		}
	}
	for path := range source.Files {
		if _, ok := archived[filepath.Clean(path)]; !ok {
			t.Fatalf("%s is missing from the manifest", path)
		}
	}
	for _, excluded := range []string{
		filepath.Join(source.Root.AgentStateDir, controlPlaneAgentStateSnapshotsDir, "transient.db"),
		filepath.Join(source.Root.AgentStateDir, ".ledger.staging"),
	} {
		if _, ok := archived[filepath.Clean(excluded)]; ok {
			t.Fatalf("%s must never be archived", excluded)
		}
	}

	target := newControlPlaneTargetRoots(t)
	var restoreReport bytes.Buffer
	restored, err := restoreControlPlaneArchive(
		archivePath,
		controlPlaneTestKey,
		target,
		&restoreReport,
	)
	if err != nil {
		t.Fatalf("restore the archive: %v", err)
	}
	if restored.Restored != len(source.Files) {
		t.Fatalf("restored %d members, want %d", restored.Restored, len(source.Files))
	}
	if restored.SchemaVersion != durableServiceOperationSchemaVersion {
		t.Fatalf("restored schema %d, want %d", restored.SchemaVersion, durableServiceOperationSchemaVersion)
	}
	if restored.MigrationVersion != shippedControlPlaneMigrationVersion(t) {
		t.Fatalf(
			"restored migration version %d, want %d",
			restored.MigrationVersion,
			shippedControlPlaneMigrationVersion(t),
		)
	}
	if !strings.Contains(
		restoreReport.String(),
		"restored="+strconv.Itoa(restored.Restored)+
			" schema="+strconv.Itoa(restored.SchemaVersion)+
			" migration="+strconv.Itoa(restored.MigrationVersion)+
			" from="+archivePath,
	) {
		t.Fatalf("the summary line is missing from the restore report:\n%s", restoreReport.String())
	}

	rebase, err := newControlPlaneRebase(source.Root, target)
	if err != nil {
		t.Fatalf("build the rebase: %v", err)
	}
	databasePath := filepath.Join(source.Root.DataDir, controlPlaneDatabaseBasename)
	for path, content := range source.Files {
		placed, err := rebase(path)
		if err != nil {
			t.Fatalf("rebase %s: %v", path, err)
		}
		info, err := os.Lstat(placed)
		if err != nil {
			t.Fatalf("stat the restored %s: %v", placed, err)
		}
		sourceInfo, err := os.Lstat(path)
		if err != nil {
			t.Fatalf("stat the source %s: %v", path, err)
		}
		if info.Mode().Perm() != sourceInfo.Mode().Perm() {
			t.Fatalf(
				"restored %s with mode %s, want %s",
				placed,
				info.Mode().Perm(),
				sourceInfo.Mode().Perm(),
			)
		}
		if formatControlPlaneMode(info.Mode()) != archived[filepath.Clean(path)].Mode {
			t.Fatalf(
				"restored %s with mode %s, the manifest recorded %s",
				placed,
				formatControlPlaneMode(info.Mode()),
				archived[filepath.Clean(path)].Mode,
			)
		}
		if path == databasePath {
			// The database is the online copy, so it is compared by content,
			// not by bytes: an online backup is deliberately not a byte copy.
			continue
		}
		got, err := os.ReadFile(placed)
		if err != nil {
			t.Fatalf("read the restored %s: %v", placed, err)
		}
		if !bytes.Equal(got, content) {
			t.Fatalf("restored %s does not match the source", placed)
		}
	}

	// Owner and group names round-tripped. As a non-root creator every recorded
	// account is this account, so the chown at placement is a no-op that still
	// had to succeed.
	currentOwner, currentGroup, err := controlPlaneOwnership(databasePath, mustLstatControlPlane(t, databasePath))
	if err != nil {
		t.Fatalf("read the current ownership: %v", err)
	}
	for path, entry := range archived {
		if entry.Owner != currentOwner || entry.Group != currentGroup {
			t.Fatalf(
				"%s recorded owner %s:%s, want %s:%s",
				path,
				entry.Owner,
				entry.Group,
				currentOwner,
				currentGroup,
			)
		}
	}

	assertControlPlaneDatabaseMarker(t, filepath.Join(target.DataDir, controlPlaneDatabaseBasename))
	for _, excluded := range []string{
		filepath.Join(target.AgentStateDir, controlPlaneAgentStateSnapshotsDir, "transient.db"),
		filepath.Join(target.AgentStateDir, ".ledger.staging"),
	} {
		if _, err := os.Lstat(excluded); !os.IsNotExist(err) {
			t.Fatalf("%s was restored but must never be archived (err=%v)", excluded, err)
		}
	}
}

func TestControlPlaneArchiveSkipsAbsentOptionalMembers(t *testing.T) {
	source := newControlPlaneTestTree(t)
	optional := filepath.Join(source.Root.ConfDir, "agent.token")
	if err := os.Remove(optional); err != nil {
		t.Fatalf("remove %s: %v", optional, err)
	}
	delete(source.Files, optional)
	if err := os.RemoveAll(source.Root.WireGuardDir); err != nil {
		t.Fatalf("remove the WireGuard directory: %v", err)
	}
	delete(source.Files, filepath.Join(source.Root.WireGuardDir, "wg0.conf"))

	archivePath := filepath.Join(t.TempDir(), "control-plane.cpbak")
	var report bytes.Buffer
	result, err := createControlPlaneArchive(archivePath, controlPlaneTestKey, source.Root, &report)
	if err != nil {
		t.Fatalf("create the archive: %v", err)
	}
	if result.Members != len(source.Files) {
		t.Fatalf("archived %d members, want %d", result.Members, len(source.Files))
	}
	for _, expected := range []string{
		"skipped component=\"agent token\" path=" + optional + " reason=absent",
		"skipped component=\"WireGuard\" path=" + source.Root.WireGuardDir + " reason=absent",
	} {
		if !strings.Contains(report.String(), expected) {
			t.Fatalf("the report is missing %q:\n%s", expected, report.String())
		}
	}
}

func TestControlPlaneArchiveRequiresMandatoryMembers(t *testing.T) {
	for _, mandatory := range []string{controlPlaneSecretKeyBasename, controlPlaneDatabaseBasename} {
		t.Run(mandatory, func(t *testing.T) {
			source := newControlPlaneTestTree(t)
			path := filepath.Join(source.Root.DataDir, mandatory)
			if err := os.Remove(path); err != nil {
				t.Fatalf("remove %s: %v", path, err)
			}
			archivePath := filepath.Join(t.TempDir(), "control-plane.cpbak")
			_, err := createControlPlaneArchive(
				archivePath,
				controlPlaneTestKey,
				source.Root,
				io.Discard,
			)
			if err == nil || !strings.Contains(err.Error(), "missing") {
				t.Fatalf("error=%v, want a missing mandatory member", err)
			}
			if _, err := os.Lstat(archivePath); !os.IsNotExist(err) {
				t.Fatalf("an archive was written anyway (err=%v)", err)
			}
		})
	}
}

func TestControlPlaneArchiveRefusesAWrongKey(t *testing.T) {
	archivePath := newControlPlaneTestArchive(t)
	target := newControlPlaneTargetRoots(t)
	otherKey, err := generateControlPlaneKey()
	if err != nil {
		t.Fatalf("generate a second key: %v", err)
	}
	_, err = restoreControlPlaneArchive(archivePath, otherKey, target, io.Discard)
	if err == nil {
		t.Fatal("a wrong key was accepted")
	}
	if !strings.Contains(err.Error(), "key") {
		t.Fatalf("error=%v, want it to name the key", err)
	}
	assertControlPlaneTargetUntouched(t, target)
}

func TestControlPlaneArchiveRefusesAMalformedKey(t *testing.T) {
	archivePath := newControlPlaneTestArchive(t)
	target := newControlPlaneTargetRoots(t)
	if _, err := restoreControlPlaneArchive(archivePath, "not-a-key", target, io.Discard); err == nil {
		t.Fatal("a malformed key was accepted")
	}
	assertControlPlaneTargetUntouched(t, target)

	source := newControlPlaneTestTree(t)
	writePath := filepath.Join(t.TempDir(), "control-plane.cpbak")
	if _, err := createControlPlaneArchive(writePath, "not-a-key", source.Root, io.Discard); err == nil {
		t.Fatal("the writer accepted a malformed key")
	}
	if _, err := os.Lstat(writePath); !os.IsNotExist(err) {
		t.Fatalf("an archive was written with a malformed key (err=%v)", err)
	}
}

func TestControlPlaneArchiveRefusesATamperedCiphertext(t *testing.T) {
	archivePath := newControlPlaneTestArchive(t)
	content, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read the archive: %v", err)
	}
	_, preamble, err := readControlPlaneArchivePreamble(bytes.NewReader(content))
	if err != nil {
		t.Fatalf("read the archive preamble: %v", err)
	}
	if len(content) <= len(preamble) {
		t.Fatal("the archive carries no ciphertext")
	}
	for _, offset := range []int{
		len(preamble),
		len(preamble) + (len(content)-len(preamble))/2,
		len(content) - 1,
	} {
		flipped := append([]byte(nil), content...)
		flipped[offset] ^= 0x01
		tamperedPath := filepath.Join(t.TempDir(), "tampered.cpbak")
		if err := os.WriteFile(tamperedPath, flipped, 0o600); err != nil {
			t.Fatalf("write the tampered archive: %v", err)
		}
		target := newControlPlaneTargetRoots(t)
		_, err := restoreControlPlaneArchive(tamperedPath, controlPlaneTestKey, target, io.Discard)
		if err == nil {
			t.Fatalf("a bit flip at offset %d was accepted", offset)
		}
		if !errors.Is(err, errControlPlaneArchiveAuthentication) &&
			!strings.Contains(err.Error(), "digest") {
			t.Fatalf("error at offset %d = %v, want an authentication failure", offset, err)
		}
		assertControlPlaneTargetUntouched(t, target)
	}
}

func TestControlPlaneArchiveRefusesATruncatedCiphertext(t *testing.T) {
	archivePath := newControlPlaneTestArchive(t)
	content, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read the archive: %v", err)
	}
	truncatedPath := filepath.Join(t.TempDir(), "truncated.cpbak")
	if err := os.WriteFile(truncatedPath, content[:len(content)-64], 0o600); err != nil {
		t.Fatalf("write the truncated archive: %v", err)
	}
	target := newControlPlaneTargetRoots(t)
	if _, err := restoreControlPlaneArchive(
		truncatedPath,
		controlPlaneTestKey,
		target,
		io.Discard,
	); err == nil {
		t.Fatal("a truncated archive was accepted")
	}
	assertControlPlaneTargetUntouched(t, target)
}

func TestControlPlaneArchiveRefusesANewerSchemaVersion(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "future.cpbak")
	manifest := controlPlaneManifest{
		SchemaVersion:            durableServiceOperationSchemaVersion + 1,
		DatabaseMigrationVersion: shippedControlPlaneMigrationVersion(t),
		PanelVersion:             "future",
		PanelCommit:              "future",
		Host:                     "future",
		CreatedAt:                "2099-01-01T00:00:00Z",
		Roots:                    newControlPlaneTargetRoots(t),
	}
	sealControlPlaneTestArchive(t, archivePath, controlPlaneTestKey, func(writer *tar.Writer) {
		writeControlPlaneTestManifest(t, writer, manifest)
		writeControlPlaneTestManifestDigest(t, writer, manifest)
	})
	target := newControlPlaneTargetRoots(t)
	_, err := restoreControlPlaneArchive(archivePath, controlPlaneTestKey, target, io.Discard)
	if err == nil {
		t.Fatal("an archive from a newer release was accepted")
	}
	if !strings.Contains(err.Error(), "schema version") {
		t.Fatalf("error=%v, want it to name the schema version", err)
	}
	assertControlPlaneTargetUntouched(t, target)
}

// A database migrated further than this release ships must be refused before a
// single member is placed. Otherwise it would land and only fail afterwards, or
// be opened by an older panel that cannot run its own schema.
func TestControlPlaneArchiveRefusesANewerDatabaseMigrationVersion(t *testing.T) {
	shipped := shippedControlPlaneMigrationVersion(t)
	for _, test := range []struct {
		name      string
		recorded  int
		wantError string
	}{
		{
			name:      "migrated past this release",
			recorded:  shipped + 1,
			wantError: "ships migrations up to",
		},
		{
			name:      "no migration version recorded",
			recorded:  0,
			wantError: "records no database migration version",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			archivePath := filepath.Join(t.TempDir(), "future-data.cpbak")
			manifest := controlPlaneManifest{
				SchemaVersion:            durableServiceOperationSchemaVersion,
				DatabaseMigrationVersion: test.recorded,
				PanelVersion:             "future",
				PanelCommit:              "future",
				Host:                     "future",
				CreatedAt:                "2099-01-01T00:00:00Z",
				Roots:                    newControlPlaneTargetRoots(t),
			}
			sealControlPlaneTestArchive(t, archivePath, controlPlaneTestKey, func(writer *tar.Writer) {
				writeControlPlaneTestManifest(t, writer, manifest)
				writeControlPlaneTestManifestDigest(t, writer, manifest)
			})
			target := newControlPlaneTargetRoots(t)
			_, err := restoreControlPlaneArchive(archivePath, controlPlaneTestKey, target, io.Discard)
			if err == nil {
				t.Fatalf("an archive recording migration version %d was accepted", test.recorded)
			}
			if !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("error=%v, want substring %q", err, test.wantError)
			}
			assertControlPlaneTargetUntouched(t, target)
		})
	}

	// The exact shipped version is the highest one that is still restorable.
	if err := requireRestorableControlPlaneMigrationVersion(shipped); err != nil {
		t.Fatalf("the shipped migration version was refused: %v", err)
	}
}

func shippedControlPlaneMigrationVersion(t *testing.T) int {
	t.Helper()
	shipped, err := paneldb.HighestEmbeddedMigrationVersion()
	if err != nil {
		t.Fatalf("read the shipped migration version: %v", err)
	}
	if shipped < 1 {
		t.Fatalf("this build reports %d shipped migrations", shipped)
	}
	return shipped
}

func TestControlPlaneArchiveRefusesAnEscapingMemberName(t *testing.T) {
	for _, name := range []string{
		"../escape",
		"var/lib/../../escape",
		"/absolute/escape",
	} {
		t.Run(name, func(t *testing.T) {
			archivePath := filepath.Join(t.TempDir(), "escape.cpbak")
			manifest := controlPlaneManifest{
				SchemaVersion:            durableServiceOperationSchemaVersion,
				DatabaseMigrationVersion: shippedControlPlaneMigrationVersion(t),
				Roots:                    newControlPlaneTargetRoots(t),
			}
			sealControlPlaneTestArchive(t, archivePath, controlPlaneTestKey, func(writer *tar.Writer) {
				writeControlPlaneTestManifest(t, writer, manifest)
				if err := writer.WriteHeader(&tar.Header{
					Format:   tar.FormatPAX,
					Typeflag: tar.TypeReg,
					Name:     name,
					Mode:     0o600,
					Size:     int64(len("owned")),
				}); err != nil {
					t.Fatalf("write the escaping entry: %v", err)
				}
				if _, err := writer.Write([]byte("owned")); err != nil {
					t.Fatalf("write the escaping entry: %v", err)
				}
				writeControlPlaneTestManifestDigest(t, writer, manifest)
			})
			target := newControlPlaneTargetRoots(t)
			_, err := restoreControlPlaneArchive(archivePath, controlPlaneTestKey, target, io.Discard)
			if err == nil {
				t.Fatalf("the member name %q was accepted", name)
			}
			assertControlPlaneTargetUntouched(t, target)
			escaped := filepath.Join(filepath.Dir(controlPlaneTargetBase(target)), "escape")
			if _, err := os.Lstat(escaped); !os.IsNotExist(err) {
				t.Fatalf("%s was written outside the target tree (err=%v)", escaped, err)
			}
		})
	}
}

func TestControlPlaneMemberNameRejections(t *testing.T) {
	for _, name := range []string{
		"",
		"/",
		"..",
		"../escape",
		"a/../../escape",
		"/absolute",
		`windows\path`,
		"a//b",
		"a/./b",
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateControlPlaneMemberName(name); err == nil {
				t.Fatalf("the member name %q was accepted", name)
			}
		})
	}
	for _, name := range []string{
		"var/lib/celikpanel/celikpanel.db",
		"etc/celikpanel/panel.env",
		"var/lib/celikpanel/",
	} {
		if err := validateControlPlaneMemberName(name); err != nil {
			t.Fatalf("the member name %q was rejected: %v", name, err)
		}
	}
}

func TestControlPlaneRestoreRefusesAHostThatIsNotFresh(t *testing.T) {
	archivePath := newControlPlaneTestArchive(t)

	t.Run("existing database", func(t *testing.T) {
		target := newControlPlaneTargetRoots(t)
		if err := os.MkdirAll(target.DataDir, 0o700); err != nil {
			t.Fatalf("create the target data directory: %v", err)
		}
		existing := filepath.Join(target.DataDir, controlPlaneDatabaseBasename)
		if err := os.WriteFile(existing, []byte("this host already lives here"), 0o600); err != nil {
			t.Fatalf("write the existing database: %v", err)
		}
		_, err := restoreControlPlaneArchive(archivePath, controlPlaneTestKey, target, io.Discard)
		if err == nil {
			t.Fatal("a host with a panel database was accepted")
		}
		if !strings.Contains(err.Error(), "fresh host") {
			t.Fatalf("error=%v, want it to say a fresh host is required", err)
		}
		content, err := os.ReadFile(existing)
		if err != nil || string(content) != "this host already lives here" {
			t.Fatalf("the existing database was touched: content=%q err=%v", content, err)
		}
	})

	for _, marker := range controlPlaneFreshHostMarkers {
		t.Run("existing "+marker, func(t *testing.T) {
			target := newControlPlaneTargetRoots(t)
			if err := os.MkdirAll(target.AgentStateDir, 0o700); err != nil {
				t.Fatalf("create the target agent state directory: %v", err)
			}
			markerPath := filepath.Join(target.AgentStateDir, marker)
			if err := os.WriteFile(markerPath, []byte("{}"), 0o600); err != nil {
				t.Fatalf("write %s: %v", markerPath, err)
			}
			_, err := restoreControlPlaneArchive(archivePath, controlPlaneTestKey, target, io.Discard)
			if err == nil {
				t.Fatalf("a host with %s was accepted", marker)
			}
			if !strings.Contains(err.Error(), "fresh host") {
				t.Fatalf("error=%v, want it to say a fresh host is required", err)
			}
			if _, err := os.Lstat(filepath.Join(target.DataDir, controlPlaneDatabaseBasename)); !os.IsNotExist(err) {
				t.Fatalf("a database was restored anyway (err=%v)", err)
			}
		})
	}
}

func TestControlPlaneArchiveRefusesASymbolicLink(t *testing.T) {
	source := newControlPlaneTestTree(t)
	target := filepath.Join(source.Root.ConfDir, "panel.env")
	link := filepath.Join(source.Root.AgentStateDir, "linked-state.json")
	if err := os.Remove(filepath.Join(source.Root.AgentStateDir, "dns-engine-ownership-bind.json")); err != nil {
		t.Fatalf("remove the ownership receipt: %v", err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("this host cannot create symbolic links: %v", err)
	}
	_, err := createControlPlaneArchive(
		filepath.Join(t.TempDir(), "control-plane.cpbak"),
		controlPlaneTestKey,
		source.Root,
		io.Discard,
	)
	if err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("error=%v, want a symbolic link refusal", err)
	}
}

func TestControlPlaneArchiveRefusesAnExistingDestination(t *testing.T) {
	source := newControlPlaneTestTree(t)
	archivePath := filepath.Join(t.TempDir(), "control-plane.cpbak")
	if err := os.WriteFile(archivePath, []byte("older archive"), 0o600); err != nil {
		t.Fatalf("write the existing archive: %v", err)
	}
	if _, err := createControlPlaneArchive(
		archivePath,
		controlPlaneTestKey,
		source.Root,
		io.Discard,
	); err == nil {
		t.Fatal("an existing archive was overwritten")
	}
	content, err := os.ReadFile(archivePath)
	if err != nil || string(content) != "older archive" {
		t.Fatalf("the existing archive was touched: content=%q err=%v", content, err)
	}
}

func TestControlPlaneStreamRejectsAnEmptyPlaintextOnly(t *testing.T) {
	key, err := parseControlPlaneKey(controlPlaneTestKey)
	if err != nil {
		t.Fatalf("parse the test key: %v", err)
	}
	header, err := newControlPlaneArchiveHeader("2026-09-03T00:00:00Z")
	if err != nil {
		t.Fatalf("build a header: %v", err)
	}
	header.Chunk = 1024
	preamble, err := encodeControlPlaneArchivePreamble(header)
	if err != nil {
		t.Fatalf("encode the preamble: %v", err)
	}
	aead, err := newControlPlaneArchiveAEAD(key, header)
	if err != nil {
		t.Fatalf("build the cipher: %v", err)
	}
	// Sizes around the chunk boundary are where a stream format goes wrong.
	for _, size := range []int{0, 1, 1023, 1024, 1025, 2048, 2049, 5000} {
		plaintext := bytes.Repeat([]byte{byte(size % 251)}, size)
		var sealed bytes.Buffer
		writer := newControlPlaneStreamWriter(&sealed, aead, preamble, header.Chunk)
		if _, err := writer.Write(plaintext); err != nil {
			t.Fatalf("seal %d bytes: %v", size, err)
		}
		if err := writer.Close(); err != nil {
			t.Fatalf("close the stream for %d bytes: %v", size, err)
		}
		got, err := io.ReadAll(newControlPlaneStreamReader(
			bytes.NewReader(sealed.Bytes()),
			aead,
			preamble,
			header.Chunk,
		))
		if err != nil {
			t.Fatalf("open %d bytes: %v", size, err)
		}
		if !bytes.Equal(got, plaintext) {
			t.Fatalf("the stream round trip changed %d bytes", size)
		}
	}
}

// newControlPlaneTestArchive builds one real archive from one real tree.
func newControlPlaneTestArchive(t *testing.T) string {
	t.Helper()
	source := newControlPlaneTestTree(t)
	archivePath := filepath.Join(t.TempDir(), "control-plane.cpbak")
	if _, err := createControlPlaneArchive(
		archivePath,
		controlPlaneTestKey,
		source.Root,
		io.Discard,
	); err != nil {
		t.Fatalf("create the archive: %v", err)
	}
	return archivePath
}

// readControlPlaneTestManifest opens an archive the way a restore does and
// returns only its manifest, so a test can assert on what was recorded without
// the production code growing an inspection mode it does not need yet.
func readControlPlaneTestManifest(
	t *testing.T,
	archivePath string,
	keyText string,
) controlPlaneManifest {
	t.Helper()
	key, err := parseControlPlaneKey(keyText)
	if err != nil {
		t.Fatalf("parse the key: %v", err)
	}
	file, err := os.Open(archivePath)
	if err != nil {
		t.Fatalf("open the archive: %v", err)
	}
	defer file.Close()
	header, preamble, err := readControlPlaneArchivePreamble(file)
	if err != nil {
		t.Fatalf("read the preamble: %v", err)
	}
	aead, err := newControlPlaneArchiveAEAD(key, header)
	if err != nil {
		t.Fatalf("build the cipher: %v", err)
	}
	reader := tar.NewReader(newControlPlaneStreamReader(file, aead, preamble, header.Chunk))
	entry, err := reader.Next()
	if err != nil {
		t.Fatalf("read the first archive entry: %v", err)
	}
	if entry.Name != controlPlaneManifestName {
		t.Fatalf("the first archive entry is %q, want %q", entry.Name, controlPlaneManifestName)
	}
	raw, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read the manifest: %v", err)
	}
	var manifest controlPlaneManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("decode the manifest: %v", err)
	}
	// The last entry is the manifest digest, and it must match.
	var digestText string
	for {
		next, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("read an archive entry: %v", err)
		}
		if next.Name != controlPlaneManifestDigestName {
			continue
		}
		content, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("read the manifest digest: %v", err)
		}
		digestText = strings.TrimSpace(string(content))
	}
	digest := sha256.Sum256(raw)
	if digestText != hex.EncodeToString(digest[:]) {
		t.Fatalf("the manifest digest entry does not match the manifest")
	}
	return manifest
}

// sealControlPlaneTestArchive writes an archive whose tar body the caller
// controls, so a restore can be shown refusing bodies the writer would never
// produce.
func sealControlPlaneTestArchive(
	t *testing.T,
	archivePath string,
	keyText string,
	build func(*tar.Writer),
) {
	t.Helper()
	key, err := parseControlPlaneKey(keyText)
	if err != nil {
		t.Fatalf("parse the key: %v", err)
	}
	header, err := newControlPlaneArchiveHeader("2026-09-03T00:00:00Z")
	if err != nil {
		t.Fatalf("build a header: %v", err)
	}
	preamble, err := encodeControlPlaneArchivePreamble(header)
	if err != nil {
		t.Fatalf("encode the preamble: %v", err)
	}
	aead, err := newControlPlaneArchiveAEAD(key, header)
	if err != nil {
		t.Fatalf("build the cipher: %v", err)
	}
	var sealed bytes.Buffer
	sealed.Write(preamble)
	stream := newControlPlaneStreamWriter(&sealed, aead, preamble, header.Chunk)
	writer := tar.NewWriter(stream)
	build(writer)
	if err := writer.Close(); err != nil {
		t.Fatalf("close the tar body: %v", err)
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("close the stream: %v", err)
	}
	if err := os.WriteFile(archivePath, sealed.Bytes(), 0o600); err != nil {
		t.Fatalf("write the archive: %v", err)
	}
}

func writeControlPlaneTestManifest(
	t *testing.T,
	writer *tar.Writer,
	manifest controlPlaneManifest,
) {
	t.Helper()
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("encode the manifest: %v", err)
	}
	if err := writer.WriteHeader(&tar.Header{
		Format:   tar.FormatPAX,
		Typeflag: tar.TypeReg,
		Name:     controlPlaneManifestName,
		Mode:     0o600,
		Size:     int64(len(raw)),
	}); err != nil {
		t.Fatalf("write the manifest header: %v", err)
	}
	if _, err := writer.Write(raw); err != nil {
		t.Fatalf("write the manifest: %v", err)
	}
}

func writeControlPlaneTestManifestDigest(
	t *testing.T,
	writer *tar.Writer,
	manifest controlPlaneManifest,
) {
	t.Helper()
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("encode the manifest: %v", err)
	}
	digest := sha256.Sum256(raw)
	content := hex.EncodeToString(digest[:]) + "\n"
	if err := writer.WriteHeader(&tar.Header{
		Format:   tar.FormatPAX,
		Typeflag: tar.TypeReg,
		Name:     controlPlaneManifestDigestName,
		Mode:     0o600,
		Size:     int64(len(content)),
	}); err != nil {
		t.Fatalf("write the manifest digest header: %v", err)
	}
	if _, err := writer.Write([]byte(content)); err != nil {
		t.Fatalf("write the manifest digest: %v", err)
	}
}

func assertControlPlaneTargetUntouched(t *testing.T, target controlPlaneRoots) {
	t.Helper()
	base := controlPlaneTargetBase(target)
	found := []string{}
	err := filepath.WalkDir(base, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if !entry.IsDir() {
			found = append(found, path)
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("inspect the target tree: %v", err)
	}
	sort.Strings(found)
	if len(found) != 0 {
		t.Fatalf("the target tree was written to before the archive was trusted: %v", found)
	}
}

func assertControlPlaneDatabaseMarker(t *testing.T, databasePath string) {
	t.Helper()
	database, err := sql.Open("sqlite", sqliteSnapshotURI(databasePath, true))
	if err != nil {
		t.Fatalf("open the restored database: %v", err)
	}
	defer database.Close()
	var value string
	if err := database.QueryRow(
		`SELECT value FROM panel_settings WHERE key = ?`,
		controlPlaneTestMarker,
	).Scan(&value); err != nil {
		t.Fatalf("read the marker from the restored database: %v", err)
	}
	if value != "the brain of host A" {
		t.Fatalf("the restored database carries %q", value)
	}
}

func mustLstatControlPlane(t *testing.T, path string) os.FileInfo {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info
}
