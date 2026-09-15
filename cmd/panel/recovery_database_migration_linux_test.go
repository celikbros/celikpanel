//go:build linux

package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	paneldb "github.com/alicelik/celikpanel/internal/db"
	"github.com/alicelik/celikpanel/internal/recoverypublication"
	"golang.org/x/sys/unix"
)

type migrationTestAuthority struct {
	ID     recoverypublication.DatabaseAuthorityIdentity
	Source string
	Check  func() error
}

func (a *migrationTestAuthority) Identity() recoverypublication.DatabaseAuthorityIdentity {
	return a.ID
}
func (a *migrationTestAuthority) SnapshotDatabase() (*os.File, error) { return os.Open(a.Source) }
func (a *migrationTestAuthority) Revalidate() error {
	b, e := os.ReadFile(a.Source)
	if e != nil {
		return e
	}
	if databaseMigrationDigest(b) != a.ID.SnapshotDatabaseSHA256 {
		return errors.New("snapshot changed")
	}
	if a.Check != nil {
		return a.Check()
	}
	return nil
}
func (a *migrationTestAuthority) Close() error { return nil }

type migrationFixture struct {
	Root, Parent, Source, Snapshot, Work string
	Owner                                serviceOperationRestoreOwner
	Authority                            *migrationTestAuthority
	Config                               recoveryDatabaseMigrationConfig
	Before                               databaseMigrationFile
}

func migrationAPIIdentity(i databaseMigrationIdentity) recoverypublication.DatabaseFileIdentity {
	return recoverypublication.DatabaseFileIdentity{Dev: i.Dev, Ino: i.Ino, Mode: i.Mode, UID: i.UID, GID: i.GID, Links: i.Links, Size: i.Size, MtimeSec: i.Mtime.Sec, MtimeNsec: i.Mtime.Nsec, CtimeSec: i.Ctime.Sec, CtimeNsec: i.Ctime.Nsec}
}
func migrationMust(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func newMigrationFixture(t *testing.T) *migrationFixture {
	t.Helper()
	root := newSecureSnapshotTestRoot(t)
	owner := unusedServiceOperationRestoreOwner(t)
	source := filepath.Join(root, "snapshot.db")
	d, e := sql.Open("sqlite", source+"?_pragma=foreign_keys(1)")
	migrationMust(t, e)
	_, e = d.Exec(referenceSchemaMigrationsSQLForTest(t, 41))
	migrationMust(t, e)
	files, e := os.ReadDir("../../internal/db/migrations")
	migrationMust(t, e)
	for _, file := range files {
		version, e := strconv.Atoi(strings.SplitN(file.Name(), "_", 2)[0])
		migrationMust(t, e)
		if version > 41 {
			continue
		}
		b, e := os.ReadFile(filepath.Join("../../internal/db/migrations", file.Name()))
		migrationMust(t, e)
		tx, e := d.Begin()
		migrationMust(t, e)
		_, e = tx.Exec(string(b))
		migrationMust(t, e)
		_, e = tx.Exec("INSERT INTO schema_migrations(version,filename,sha256) VALUES(?,?,?)", version, file.Name(), databaseMigrationDigest(b))
		migrationMust(t, e)
		migrationMust(t, tx.Commit())
	}
	migrationMust(t, d.Close())
	migrationMust(t, os.Chmod(source, 0600))
	migrationMust(t, validateServiceOperationSnapshot(source, serviceOperationSnapshotSchemaNormal))
	parent := filepath.Join(root, "panel")
	migrationMust(t, os.Mkdir(parent, 0750))
	migrationMust(t, os.Chmod(parent, 0750))
	migrationMust(t, os.Chown(parent, int(owner.uid), int(owner.gid)))
	b, e := os.ReadFile(source)
	migrationMust(t, e)
	target := filepath.Join(parent, databaseMigrationDB)
	migrationMust(t, os.WriteFile(target, b, 0600))
	migrationMust(t, os.Chown(target, int(owner.uid), int(owner.gid)))
	p, e := databaseMigrationOpenDirectory(parent)
	migrationMust(t, e)
	defer p.Close()
	before, e := databaseMigrationInspect(p, databaseMigrationDB, owner.uid, owner.gid)
	migrationMust(t, e)
	st, e := databaseMigrationStat(p)
	migrationMust(t, e)
	snapshot := "migration-fixture-snapshot"
	id := recoverypublication.DatabaseAuthorityIdentity{Snapshot: snapshot, TokenSHA256: strings.Repeat("1", 64), SnapshotManifestSHA256: strings.Repeat("2", 64), MaterialSHA256: strings.Repeat("3", 64), CandidatePanelSHA256: strings.Repeat("4", 64), SnapshotDatabaseSHA256: databaseMigrationDigest(b), Operation: "update", Phase: "active", DatabaseBefore: recoverypublication.DatabaseBeforeEvidence{Parent: recoverypublication.DatabaseFileIdentity{Dev: st.Dev, Ino: st.Ino, Mode: st.Mode, UID: st.Uid, GID: st.Gid}, File: migrationAPIIdentity(before.Identity), SHA256: before.SHA256}}
	a := &migrationTestAuthority{ID: id, Source: source}
	c := recoveryDatabaseMigrationConfig{parent: parent, owner: owner, authority: func(string) (recoveryDatabaseAuthority, error) { return a, nil }, stopped: func() error { return nil }, readers: func([]databaseMigrationFile) error { return nil }}
	return &migrationFixture{Root: root, Parent: parent, Source: source, Snapshot: snapshot, Owner: owner, Authority: a, Config: c, Before: before}
}
func (f *migrationFixture) target() string { return filepath.Join(f.Parent, databaseMigrationDB) }
func (f *migrationFixture) auth() string {
	return filepath.Join(f.Parent, databaseMigrationRootName, f.Authority.ID.TokenSHA256, "authority")
}
func (f *migrationFixture) prepare(t *testing.T) {
	t.Helper()
	var e error
	f.Work, e = prepareRecoveryDatabaseMigrationWith(f.Snapshot, f.Config)
	migrationMust(t, e)
}
func (f *migrationFixture) migrate(t *testing.T) {
	t.Helper()
	db, e := paneldb.NewSQLiteDB(filepath.Join(f.Work, databaseMigrationDB))
	migrationMust(t, e)
	db.Close()
	migrationMust(t, normalizeStandaloneSQLiteSnapshot(filepath.Join(f.Work, databaseMigrationDB)))
}
func (f *migrationFixture) publish(t *testing.T) {
	t.Helper()
	migrationMust(t, publishRecoveryDatabaseMigrationWith(f.Snapshot, f.Config))
}
func (f *migrationFixture) rollback(t *testing.T) {
	t.Helper()
	f.Authority.ID.Operation = "rollback"
	f.Authority.ID.Phase = "active"
	migrationMust(t, restoreRecoveryDatabaseMigrationWith(f.Snapshot, f.Config))
	migrationMust(t, verifyRecoveryDatabaseMigrationWith(f.Snapshot, f.Config))
}
func (f *migrationFixture) current(t *testing.T) databaseMigrationFile {
	t.Helper()
	p, e := databaseMigrationOpenDirectory(f.Parent)
	migrationMust(t, e)
	defer p.Close()
	r, e := databaseMigrationInspect(p, databaseMigrationDB, f.Owner.uid, f.Owner.gid)
	migrationMust(t, e)
	return r
}
func (f *migrationFixture) assertBefore(t *testing.T) {
	t.Helper()
	if !databaseMigrationSameFile(f.current(t), f.Before, true) {
		t.Fatal("canonical before-image not preserved")
	}
}
func migrationTreeBytes(t *testing.T, path string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	migrationMust(t, filepath.WalkDir(path, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if !d.IsDir() {
			b, e := os.ReadFile(p)
			if e != nil {
				return e
			}
			rel, _ := filepath.Rel(path, p)
			out[rel] = b
		}
		return nil
	}))
	return out
}
func TestRecoveryDatabaseMigrationRoundTrip(t *testing.T) {
	f := newMigrationFixture(t)
	f.prepare(t)
	work, e := prepareRecoveryDatabaseMigrationWith(f.Snapshot, f.Config)
	migrationMust(t, e)
	if work != f.Work {
		t.Fatal("changed work identity")
	}
	f.migrate(t)
	raw := migrationTreeBytes(t, f.Work)
	if _, e = prepareRecoveryDatabaseMigrationWith(f.Snapshot, f.Config); e == nil {
		t.Fatal("re-admitted changed migration")
	}
	f.publish(t)
	f.publish(t)
	migrationMust(t, verifyRecoveryDatabaseMigrationWith(f.Snapshot, f.Config))
	if f.current(t).Identity.Ino == f.Before.Identity.Ino {
		t.Fatal("publication did not exchange inode")
	}
	migrationMust(t, checkCompletedUpdateDatabase(f.target()))
	f.rollback(t)
	f.rollback(t)
	f.assertBefore(t)
	after := migrationTreeBytes(t, f.Work)
	if !bytes.Equal(databaseMigrationCanonical(raw), databaseMigrationCanonical(after)) {
		t.Fatal("raw migration evidence changed")
	}
}
func TestRecoveryDatabaseMigrationIncompleteWorkPreserved(t *testing.T) {
	for _, prepared := range []bool{false, true} {
		t.Run(fmt.Sprint(prepared), func(t *testing.T) {
			f := newMigrationFixture(t)
			if prepared {
				f.prepare(t)
				for _, suffix := range []string{"-wal", "-shm", "-journal"} {
					migrationMust(t, os.WriteFile(filepath.Join(f.Work, databaseMigrationDB+suffix), []byte("interrupted-original"), 0600))
				}
			}
			var raw map[string][]byte
			if prepared {
				raw = migrationTreeBytes(t, f.Work)
			}
			f.rollback(t)
			f.assertBefore(t)
			if prepared && !bytes.Equal(databaseMigrationCanonical(raw), databaseMigrationCanonical(migrationTreeBytes(t, f.Work))) {
				t.Fatal("incomplete work was changed")
			}
		})
	}
}
func TestRecoveryDatabaseMigrationRefusesOwnerChanges(t *testing.T) {
	for _, kind := range []string{"canonical-content", "canonical-inode", "canonical-xattr", "parent-xattr", "canonical-wal", "material", "snapshot", "unknown-journal", "missing-seal", "published-orphan"} {
		t.Run(kind, func(t *testing.T) {
			f := newMigrationFixture(t)
			f.prepare(t)
			f.migrate(t)
			switch kind {
			case "canonical-content":
				fd, e := os.OpenFile(f.target(), os.O_WRONLY, 0)
				migrationMust(t, e)
				_, e = fd.WriteAt([]byte("owner"), 0)
				migrationMust(t, e)
				fd.Close()
			case "canonical-inode":
				b, e := os.ReadFile(f.target())
				migrationMust(t, e)
				migrationMust(t, os.Rename(f.target(), f.target()+".owner"))
				migrationMust(t, os.WriteFile(f.target(), b, 0600))
				migrationMust(t, os.Chown(f.target(), int(f.Owner.uid), int(f.Owner.gid)))
			case "canonical-xattr":
				migrationMust(t, unix.Setxattr(f.target(), "user.owner", []byte("retained"), 0))
			case "parent-xattr":
				migrationMust(t, unix.Setxattr(f.Parent, "user.owner", []byte("retained"), 0))
			case "canonical-wal":
				migrationMust(t, os.WriteFile(f.target()+"-wal", []byte("owner WAL"), 0600))
			case "material":
				f.Authority.ID.MaterialSHA256 = strings.Repeat("a", 64)
			case "snapshot":
				migrationMust(t, os.WriteFile(f.Source, []byte("changed snapshot"), 0600))
			case "unknown-journal":
				migrationMust(t, os.WriteFile(filepath.Join(f.auth(), "owner-note"), []byte("retain"), 0600))
			case "missing-seal":
				f.publish(t)
				migrationMust(t, os.Rename(filepath.Join(f.auth(), "seal.json"), filepath.Join(f.Root, "preserved-seal")))
			case "published-orphan":
				migrationMust(t, os.WriteFile(filepath.Join(f.auth(), "published.json"), []byte("{}\n"), 0600))
			}
			before := migrationTreeBytes(t, f.Parent)
			if e := publishRecoveryDatabaseMigrationWith(f.Snapshot, f.Config); e == nil {
				t.Fatal("owner/corrupt state accepted")
			}
			after := migrationTreeBytes(t, f.Parent)
			if !bytes.Equal(databaseMigrationCanonical(before), databaseMigrationCanonical(after)) {
				t.Fatal("refusal changed evidence")
			}
		})
	}
}
func TestRecoveryDatabaseMigrationLateWritesAndFinalBoundary(t *testing.T) {
	for _, operation := range []string{"update", "rollback"} {
		for _, fault := range []string{"none", "replace-at-final", "retired-at-final", "publication-at-final", "publication-inode-at-final", "history"} {
			t.Run(operation+"/"+fault, func(t *testing.T) {
				f := newMigrationFixture(t)
				f.prepare(t)
				f.migrate(t)
				f.publish(t)
				if operation == "rollback" {
					f.rollback(t)
				}
				f.Authority.ID.Phase = "scheduler"
				f.Config.stopped = func() error { return errors.New("must not check stopped late") }
				f.Config.readers = func([]databaseMigrationFile) error { return errors.New("must not stop runtime writes late") }
				d, e := sql.Open("sqlite", f.target())
				migrationMust(t, e)
				_, e = d.Exec("UPDATE server_setup_state SET revision=revision+1 WHERE id=1")
				migrationMust(t, e)
				if fault == "history" {
					_, e = d.Exec("UPDATE schema_migrations SET filename='owner' WHERE version=1")
					migrationMust(t, e)
				}
				migrationMust(t, d.Close())
				if fault != "none" && fault != "history" {
					f.Config.checkpoint = func(p string) {
						if p != "database_verify_before_final_proof" {
							return
						}
						switch fault {
						case "replace-at-final":
							b, e := os.ReadFile(f.target())
							migrationMust(t, e)
							migrationMust(t, os.Rename(f.target(), f.target()+".owner"))
							migrationMust(t, os.WriteFile(f.target(), b, 0600))
							migrationMust(t, os.Chown(f.target(), int(f.Owner.uid), int(f.Owner.gid)))
						case "retired-at-final":
							b, e := os.ReadFile(filepath.Join(f.auth(), "publication.json"))
							migrationMust(t, e)
							var r databaseMigrationPublication
							migrationMust(t, json.Unmarshal(b, &r))
							migrationMust(t, os.WriteFile(filepath.Join(f.auth(), r.Build, databaseMigrationDB), []byte("owner"), 0600))
						case "publication-inode-at-final":
							path := filepath.Join(f.auth(), "publication.json")
							b, e := os.ReadFile(path)
							migrationMust(t, e)
							migrationMust(t, os.Rename(path, filepath.Join(f.Root, "old-publication")))
							migrationMust(t, os.WriteFile(path, b, 0600))
						case "publication-at-final":
							migrationMust(t, os.WriteFile(filepath.Join(f.auth(), "publication.json"), []byte("{}\n"), 0600))
						}
					}
				}
				e = verifyRecoveryDatabaseMigrationWith(f.Snapshot, f.Config)
				if fault == "none" {
					migrationMust(t, e)
				} else if e == nil {
					t.Fatal("late owner change was accepted")
				}
			})
		}
	}
}

type migrationCrashConfig struct {
	Parent, Source, Snapshot string
	UID, GID                 uint32
	Identity                 recoverypublication.DatabaseAuthorityIdentity
	Action, Point            string
}

func TestRecoveryDatabaseMigrationSIGKILLHelper(t *testing.T) {
	path := os.Getenv("CELIKPANEL_DB_MIGRATION_CRASH_FIXTURE")
	if path == "" {
		return
	}
	b, e := os.ReadFile(path)
	migrationMust(t, e)
	var f migrationCrashConfig
	migrationMust(t, json.Unmarshal(b, &f))
	a := &migrationTestAuthority{ID: f.Identity, Source: f.Source}
	c := recoveryDatabaseMigrationConfig{parent: f.Parent, owner: serviceOperationRestoreOwner{uid: f.UID, gid: f.GID}, authority: func(string) (recoveryDatabaseAuthority, error) { return a, nil }, stopped: func() error { return nil }, readers: func([]databaseMigrationFile) error { return nil }, checkpoint: func(p string) {
		if p == f.Point {
			_ = unix.Kill(os.Getpid(), unix.SIGKILL)
			panic("SIGKILL returned")
		}
	}}
	switch f.Action {
	case "migrate-wal":
		work := filepath.Join(f.Parent, databaseMigrationRootName, f.Identity.TokenSHA256, "work", databaseMigrationDB)
		keeper, e := sql.Open("sqlite", work+"?_pragma=journal_mode(WAL)&_pragma=wal_autocheckpoint(0)")
		migrationMust(t, e)
		migrationMust(t, keeper.Ping())
		candidate, e := paneldb.NewSQLiteDB(work)
		migrationMust(t, e)
		candidate.Close()
		_, e = keeper.Exec("UPDATE server_setup_state SET revision=revision+1 WHERE id=1")
		migrationMust(t, e)
		for _, suffix := range []string{"-wal", "-shm"} {
			migrationMust(t, os.Chown(work+suffix, int(f.UID), int(f.GID)))
			migrationMust(t, os.Chmod(work+suffix, 0600))
		}
		c.checkpoint("migration-wal-created")
		keeper.Close()

	case "prepare":
		_, e = prepareRecoveryDatabaseMigrationWith(f.Snapshot, c)
	case "publish":
		e = publishRecoveryDatabaseMigrationWith(f.Snapshot, c)
	case "restore":
		e = restoreRecoveryDatabaseMigrationWith(f.Snapshot, c)
	}
	t.Fatalf("checkpoint not reached: %v", e)
}
func migrationKillAt(t *testing.T, f *migrationFixture, action, point string) {
	t.Helper()
	config := migrationCrashConfig{Parent: f.Parent, Source: f.Source, Snapshot: f.Snapshot, UID: f.Owner.uid, GID: f.Owner.gid, Identity: f.Authority.ID, Action: action, Point: point}
	path := filepath.Join(f.Root, "crash.json")
	migrationMust(t, os.WriteFile(path, databaseMigrationCanonical(config), 0600))
	cmd := exec.Command(os.Args[0], "-test.run=^TestRecoveryDatabaseMigrationSIGKILLHelper$")
	cmd.Env = append(os.Environ(), "CELIKPANEL_DB_MIGRATION_CRASH_FIXTURE="+path)
	out, e := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(e, &exit) {
		t.Fatalf("not killed: %v %s", e, out)
	}
	status, ok := exit.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatalf("not SIGKILL: %v %s", e, out)
	}
}
func TestRecoveryDatabaseMigrationSIGKILLRoundTrip(t *testing.T) {
	cases := []struct{ action, point string }{
		{"prepare", "database_root_created"}, {"prepare", "database_token_created"}, {"prepare", "database_authority_created"}, {"prepare", "database_work_created"}, {"prepare", "database_admission_staged"}, {"prepare", "database_work_copied"}, {"prepare", "database_admission_durable"},
		{"publish", "database_seal_intent_durable"}, {"publish", "database_work_sealed"}, {"publish", "database_build_copied"}, {"publish", "database_publication_intent_durable"}, {"publish", "database_publish_before_exchange"}, {"publish", "database_publish_exchange_done"}, {"publish", "database_publish_exchange_durable"}, {"publish", "database_published_durable"},
		{"restore", "database_restore_intent_durable"}, {"restore", "database_restore_before_exchange"}, {"restore", "database_restore_exchange_done"}, {"restore", "database_restore_exchange_durable"}, {"restore", "database_restored_durable"}}
	for _, tc := range cases {
		t.Run(tc.point, func(t *testing.T) {
			f := newMigrationFixture(t)
			if tc.action != "prepare" {
				f.prepare(t)
				f.migrate(t)
			}
			if tc.action == "restore" {
				f.publish(t)
				f.Authority.ID.Operation = "rollback"
			}
			migrationKillAt(t, f, tc.action, tc.point)
			f.Config.checkpoint = nil
			f.rollback(t)
			f.assertBefore(t)
			f.rollback(t)
		})
	}
}

func TestRecoveryDatabaseMigrationCommittedWALPreserved(t *testing.T) {
	for _, publish := range []bool{false, true} {
		t.Run(fmt.Sprint(publish), func(t *testing.T) {
			f := newMigrationFixture(t)
			f.prepare(t)
			migrationKillAt(t, f, "migrate-wal", "migration-wal-created")
			before := migrationTreeBytes(t, f.Work)
			if len(before[databaseMigrationDB+"-wal"]) == 0 || len(before[databaseMigrationDB+"-shm"]) == 0 {
				t.Fatal("real SQLite WAL/SHM evidence missing")
			}
			if publish {
				f.publish(t)
				migrationMust(t, checkCompletedUpdateDatabase(f.target()))
			}
			f.rollback(t)
			f.assertBefore(t)
			if !bytes.Equal(databaseMigrationCanonical(before), databaseMigrationCanonical(migrationTreeBytes(t, f.Work))) {
				t.Fatal("raw WAL/SHM were altered")
			}
		})
	}
}
func TestRecoveryDatabaseMigrationLateWAL(t *testing.T) {
	f := newMigrationFixture(t)
	f.prepare(t)
	f.migrate(t)
	f.publish(t)
	f.Authority.ID.Phase = "completion"
	db, e := sql.Open("sqlite", f.target()+"?_pragma=journal_mode(WAL)&_pragma=wal_autocheckpoint(0)")
	migrationMust(t, e)
	defer db.Close()
	_, e = db.Exec("UPDATE server_setup_state SET revision=revision+1 WHERE id=1")
	migrationMust(t, e)
	for _, suffix := range []string{"-wal", "-shm"} {
		migrationMust(t, os.Chown(f.target()+suffix, int(f.Owner.uid), int(f.Owner.gid)))
		migrationMust(t, os.Chmod(f.target()+suffix, 0600))
	}
	before := migrationTreeBytes(t, f.Parent)
	migrationMust(t, verifyRecoveryDatabaseMigrationWith(f.Snapshot, f.Config))
	after := migrationTreeBytes(t, f.Parent)
	if !bytes.Equal(databaseMigrationCanonical(before), databaseMigrationCanonical(after)) {
		t.Fatal("late verification altered live database or WAL")
	}
}
func TestRecoveryDatabaseMigrationStopsBeforeMutationOnAuthorityOrUsers(t *testing.T) {
	for _, what := range []string{"service", "users", "authority"} {
		t.Run(what, func(t *testing.T) {
			f := newMigrationFixture(t)
			before := migrationTreeBytes(t, f.Parent)
			switch what {
			case "service":
				f.Config.stopped = func() error { return errors.New("active service") }
			case "users":
				f.Config.readers = func([]databaseMigrationFile) error { return errors.New("open inode") }
			case "authority":
				f.Authority.Check = func() error { return errors.New("foreign lock") }
			}
			if _, e := prepareRecoveryDatabaseMigrationWith(f.Snapshot, f.Config); e == nil {
				t.Fatal("unsafe admission accepted")
			}
			if !bytes.Equal(databaseMigrationCanonical(before), databaseMigrationCanonical(migrationTreeBytes(t, f.Parent))) {
				t.Fatal("refusal mutated state")
			}
		})
	}
}

func TestRecoveryDatabaseMigrationBeforeOnlyAbsenceFinalBoundary(t *testing.T) {
	for _, kind := range []string{"root", "token", "authority", "admission", "publication"} {
		for _, late := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/late=%v", kind, late), func(t *testing.T) {
				f := newMigrationFixture(t)
				switch kind {
				case "root":
				case "token":
					migrationKillAt(t, f, "prepare", "database_root_created")
				case "authority":
					migrationKillAt(t, f, "prepare", "database_token_created")
				case "admission":
					migrationKillAt(t, f, "prepare", "database_authority_created")
				case "publication":
					f.prepare(t)
				}
				f.Authority.ID.Operation = "rollback"
				if late {
					f.Authority.ID.Phase = "scheduler"
				}
				f.Config.checkpoint = func(point string) {
					if point != "database_verify_before_final_proof" {
						return
					}
					base := filepath.Join(f.Parent, databaseMigrationRootName)
					token := filepath.Join(base, f.Authority.ID.TokenSHA256)
					switch kind {
					case "root":
						migrationMust(t, os.Mkdir(base, 0710))
						migrationMust(t, os.Chown(base, 0, int(f.Owner.gid)))
						migrationMust(t, os.Chmod(base, 0710))
					case "token":
						migrationMust(t, os.Mkdir(token, 0710))
						migrationMust(t, os.Chown(token, 0, int(f.Owner.gid)))
						migrationMust(t, os.Chmod(token, 0710))
					case "authority":
						migrationMust(t, os.Mkdir(f.auth(), 0700))
					default:
						migrationMust(t, os.WriteFile(filepath.Join(f.auth(), "published.json"), []byte("{}\n"), 0600))
					}
				}
				if e := verifyRecoveryDatabaseMigrationWith(f.Snapshot, f.Config); e == nil {
					t.Fatal("changed absence chain accepted")
				}
				f.assertBefore(t)
			})
		}
	}
}
func TestRecoveryDatabaseMigrationSealRefusesLateOwnerChangeBeforeMetadata(t *testing.T) {
	f := newMigrationFixture(t)
	f.prepare(t)
	f.migrate(t)
	var triggered bool
	f.Config.checkpoint = func(point string) {
		if point != "database_seal_intent_durable" {
			return
		}
		triggered = true
		migrationMust(t, os.WriteFile(filepath.Join(f.Work, databaseMigrationDB), []byte("owner edit"), 0600))
	}
	if e := publishRecoveryDatabaseMigrationWith(f.Snapshot, f.Config); e == nil {
		t.Fatal("changed work accepted")
	}
	if !triggered {
		t.Fatal("seal boundary not reached")
	}
	f.assertBefore(t)
	info, e := os.Stat(f.Work)
	migrationMust(t, e)
	st := info.Sys().(*syscall.Stat_t)
	if st.Uid != f.Owner.uid || st.Gid != f.Owner.gid {
		t.Fatal("unknown owner state normalized before refusal")
	}
}
