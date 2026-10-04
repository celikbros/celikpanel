//go:build linux

package recoverypublication

import (
	"bytes"
	"errors"
	"golang.org/x/sys/unix"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func alterMaterial(t *testing.T, f fixture, change func(*materialRecord)) {
	t.Helper()
	path := filepath.Join(materialPath(f), "material.json")
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	var r materialRecord
	if e = decodeExact(raw, &r); e != nil {
		t.Fatal(e)
	}
	change(&r)
	write(t, path, canonical(r), 0600)
}
func materialLegacyV2(t *testing.T, f fixture) {
	alterMaterial(t, f, func(r *materialRecord) { r.Schema = MaterialSchemaV2; r.DatabaseBefore = nil })
}
func TestDatabaseAuthorityPinnedSnapshotAndIdentity(t *testing.T) {
	for _, op := range []string{"update", "rollback"} {
		for _, phase := range []string{"active", "completion", "completion-scheduler", "scheduler"} {
			t.Run(op+"/"+phase, func(t *testing.T) {
				f := newMaterialFixture(t)
				prepareMaterial(t, f)
				f.marker(t, op)
				if phase != "active" {
					materialCompletionPhase(t, f, phase)
				}
				if e := os.Rename(f.Request.CandidateRoot, f.Request.CandidateRoot+".missing"); e != nil {
					t.Fatal(e)
				}
				a, e := openDatabaseAuthority(f.Request.Snapshot, f.config())
				if e != nil {
					t.Fatal(e)
				}
				defer a.Close()
				v := a.Identity()
				if v.Snapshot != f.Request.Snapshot || v.Operation != op || v.Phase != phase || v.TokenSHA256 != digest([]byte(strings.Repeat("d", 64))) || v.CandidatePanelSHA256 != digest([]byte("new-panel")) || v.SnapshotDatabaseSHA256 != digest([]byte("fixture snapshot database")) || !ValidManifest(v.MaterialSHA256) {
					t.Fatalf("wrong identity: %+v", v)
				}
				if v.DatabaseBefore.SHA256 != digest([]byte("fixture canonical database")) || v.DatabaseBefore.File.UID != 1001 {
					t.Fatal("beforeimage not original canonical bytes")
				}
				fd, e := a.SnapshotDatabase()
				if e != nil {
					t.Fatal(e)
				}
				raw, e := io.ReadAll(fd)
				fd.Close()
				if e != nil || string(raw) != "fixture snapshot database" {
					t.Fatal(e, string(raw))
				}
				if e = a.Revalidate(); e != nil {
					t.Fatal(e)
				}
				// The database core owns accepted transitions; this authority cannot demand
				// the original canonical bytes after a journaled publication.
				write(t, filepath.Join(f.Root, "database", databaseName), []byte("separate published database state"), 0600)
				if e = a.Revalidate(); e != nil {
					t.Fatalf("authority invented a live equality constraint: %v", e)
				}
				if e = a.Close(); e != nil {
					t.Fatal(e)
				}
				if e = a.Revalidate(); e == nil {
					t.Fatal("closed authority accepted")
				}
				if fd, e = a.SnapshotDatabase(); e == nil || fd != nil {
					t.Fatal("closed authority returned FD")
				}
			})
		}
	}
}
func TestDatabaseAuthorityLegacyAndNoDowngrade(t *testing.T) {
	for _, schema := range []string{MaterialSchema, MaterialSchemaV2} {
		t.Run(schema, func(t *testing.T) {
			f := newMaterialFixture(t)
			prepareMaterial(t, f)
			if schema == MaterialSchema {
				materialLegacyV1(t, f)
			} else {
				materialLegacyV2(t, f)
			}
			if _, e := openDatabaseAuthority(f.Request.Snapshot, f.config()); !errors.Is(e, ErrLegacyDatabaseMaterial) {
				t.Fatal(e)
			}
			write(t, filepath.Join(f.Root, "snapshots", f.Request.Snapshot, "unrelated-evidence"), []byte("damaged evidence"), 0600)
			if e := verifyDatabasePolicy(f.Request.Snapshot, f.config()); e == nil || errors.Is(e, ErrLegacyDatabaseMaterial) {
				t.Fatalf("invalid legacy downgraded: %v", e)
			}
		})
	}
	for _, transition := range []string{"pre-ledger", "schema17"} {
		t.Run(transition, func(t *testing.T) {
			f := newMaterialFixture(t)
			write(t, filepath.Join(f.Root, "snapshots", f.Request.Snapshot, "snapshot-transition.state"), []byte(transition+"\n"), 0600)
			f.Request.SnapshotManifest = checksumManifest(t, filepath.Join(f.Root, "snapshots", f.Request.Snapshot))
			prepareMaterial(t, f)
			if e := verifyDatabasePolicy(f.Request.Snapshot, f.config()); !errors.Is(e, ErrLegacyDatabaseMaterial) {
				t.Fatal(e)
			}
		})
	}
	for _, bad := range []string{"missing-before", "invalid-before", "schema", "legacy-field", "snapshot-extra", "snapshot-symlink", "snapshot-hardlink", "snapshot-payload", "snapshot-sidecar", "transition"} {
		t.Run(bad, func(t *testing.T) {
			f := newMaterialFixture(t)
			prepareMaterial(t, f)
			switch bad {
			case "missing-before":
				alterMaterial(t, f, func(r *materialRecord) { r.DatabaseBefore = nil })
			case "invalid-before":
				alterMaterial(t, f, func(r *materialRecord) { r.DatabaseBefore.SHA256 = "bad" })
			case "schema":
				alterMaterial(t, f, func(r *materialRecord) { r.Schema = "celikpanel/recovery-material/v999" })
			case "legacy-field":
				alterMaterial(t, f, func(r *materialRecord) { r.Schema = MaterialSchemaV2 })
			case "snapshot-extra":
				write(t, filepath.Join(f.Root, "snapshots", f.Request.Snapshot, "foreign"), []byte("x"), 0600)
			case "snapshot-symlink":
				if e := os.Symlink("unrelated-evidence", filepath.Join(f.Root, "snapshots", f.Request.Snapshot, "foreign")); e != nil {
					t.Fatal(e)
				}
			case "snapshot-hardlink":
				if e := os.Link(filepath.Join(f.Root, "snapshots", f.Request.Snapshot, databaseName), filepath.Join(f.Root, "foreign-hardlink")); e != nil {
					t.Fatal(e)
				}
			case "snapshot-payload":
				write(t, filepath.Join(f.Root, "snapshots", f.Request.Snapshot, "unrelated-evidence"), []byte("other bytes"), 0600)
			case "snapshot-sidecar":
				write(t, filepath.Join(f.Root, "snapshots", f.Request.Snapshot, databaseName+"-wal"), []byte("unaccepted"), 0600)
			case "transition":
				write(t, filepath.Join(f.Root, "snapshots", f.Request.Snapshot, "snapshot-transition.state"), []byte("pre-ledger\n"), 0600)
			}
			if e := verifyDatabasePolicy(f.Request.Snapshot, f.config()); e == nil || errors.Is(e, ErrMaterialAbsent) || errors.Is(e, ErrLegacyDatabaseMaterial) {
				t.Fatalf("bad authority downgraded: %v", e)
			}
		})
	}
}
func TestDatabaseAuthorityRevalidatesEveryBoundResource(t *testing.T) {
	for _, changed := range []string{"native-lock", "token", "phase", "material", "full-snapshot", "snapshot-inode", "database-inode", "root-permissions", "marker-inode", "material-inode"} {
		t.Run(changed, func(t *testing.T) {
			f := newMaterialFixture(t)
			prepareMaterial(t, f)
			a, e := openDatabaseAuthority(f.Request.Snapshot, f.config())
			if e != nil {
				t.Fatal(e)
			}
			defer a.Close()
			switch changed {
			case "native-lock":
				if e = unix.Flock(int(f.lock.Fd()), unix.LOCK_UN); e != nil {
					t.Fatal(e)
				}
			case "token":
				path := filepath.Join(f.Root, "transaction/active")
				raw, _ := os.ReadFile(path)
				write(t, path, bytes.ReplaceAll(raw, []byte(strings.Repeat("d", 64)), []byte(strings.Repeat("e", 64))), 0600)
			case "phase":
				materialCompletionPhase(t, f, "completion")
			case "material":
				alterMaterial(t, f, func(r *materialRecord) { r.DatabaseBefore.File.Ino++ })
			case "full-snapshot":
				write(t, filepath.Join(f.Root, "snapshots", f.Request.Snapshot, "unrelated-evidence"), []byte("changed"), 0600)
			case "snapshot-inode":
				path := filepath.Join(f.Root, "snapshots", f.Request.Snapshot)
				if e = os.Rename(path, path+".old"); e != nil {
					t.Fatal(e)
				}
				copyFixtureTree(t, path+".old", path)
			case "database-inode":
				path := filepath.Join(f.Root, "snapshots", f.Request.Snapshot, databaseName)
				raw, _ := os.ReadFile(path)
				if e = os.Remove(path); e != nil {
					t.Fatal(e)
				}
				write(t, path, raw, 0600)
			case "marker-inode", "material-inode":
				path := filepath.Join(f.Root, "transaction/active")
				if changed == "material-inode" {
					path = filepath.Join(materialPath(f), "material.json")
				}
				raw, _ := os.ReadFile(path)
				if e = os.Rename(path, filepath.Join(f.Root, "previous-bound-file")); e != nil {
					t.Fatal(e)
				}
				write(t, path, raw, 0600)
			case "root-permissions":
				if e = os.Chmod(materialPath(f), 0755); e != nil {
					t.Fatal(e)
				}
			}
			if e = a.Revalidate(); e == nil {
				t.Fatal("changed authority admitted")
			}
			if fd, e := a.SnapshotDatabase(); e == nil {
				fd.Close()
				t.Fatal("changed authority returned database")
			}
		})
	}
}
func TestDatabaseMaterialBeforeCaptureRefusesUnsafeCanonicalState(t *testing.T) {
	for _, bad := range []string{"parent-owner", "parent-mode", "file-owner", "file-mode", "file-hardlink", "file-symlink", "wal", "shm", "journal", "file-xattr", "parent-xattr"} {
		t.Run(bad, func(t *testing.T) {
			f := newMaterialFixture(t)
			parent := filepath.Join(f.Root, "database")
			path := filepath.Join(parent, databaseName)
			switch bad {
			case "parent-owner":
				os.Chown(parent, 1002, 1002)
			case "parent-mode":
				os.Chmod(parent, 0770)
			case "file-owner":
				os.Chown(path, 0, 0)
			case "file-mode":
				os.Chmod(path, 0640)
			case "file-hardlink":
				if e := os.Link(path, filepath.Join(f.Root, "db-link")); e != nil {
					t.Fatal(e)
				}
			case "file-symlink":
				os.Remove(path)
				if e := os.Symlink("/nonexistent", path); e != nil {
					t.Fatal(e)
				}
			case "wal", "shm", "journal":
				write(t, path+"-"+bad, []byte("sidecar"), 0600)
			case "file-xattr":
				if e := unix.Setxattr(path, "user.owner", []byte("preserve"), 0); e != nil {
					t.Fatal(e)
				}
			case "parent-xattr":
				if e := unix.Setxattr(parent, "user.owner", []byte("preserve"), 0); e != nil {
					t.Fatal(e)
				}
			}
			if e := prepareRecoveryMaterial(f.Request, f.config()); e == nil {
				t.Fatal("unsafe before captured")
			}
			if _, e := os.Lstat(materialPath(f)); !errors.Is(e, os.ErrNotExist) {
				t.Fatal("authority published before rejecting canonical state", e)
			}
		})
	}
}
func TestDatabaseMaterialCreationPinsBeforeAcrossPublication(t *testing.T) {
	for _, point := range []string{"material_file_written", "material_ready", "material_published", "material_durable"} {
		t.Run(point, func(t *testing.T) {
			f := newMaterialFixture(t)
			c := f.config()
			changed := false
			c.checkpoint = func(got string) {
				if got == point && !changed {
					changed = true
					write(t, filepath.Join(f.Root, "database", databaseName), []byte("owner new bytes"), 0600)
				}
			}
			if e := prepareRecoveryMaterial(f.Request, c); e == nil || !changed {
				t.Fatal("canonical change accepted", e, changed)
			}
			// If material is already durable it remains the original authority; retry
			// must refuse owner changes rather than silently reseal the new bytes.
			if _, e := os.Lstat(materialPath(f)); e == nil {
				if e = prepareRecoveryMaterial(f.Request, f.config()); e == nil {
					t.Fatal("retrospective beforeimage repaired")
				}
			}
		})
	}
}
func TestDatabasePolicyStrictAbsence(t *testing.T) {
	f := newMaterialFixture(t)
	if _, e := openDatabaseAuthority(f.Request.Snapshot, f.config()); !errors.Is(e, ErrMaterialAbsent) {
		t.Fatal(e)
	}
	if e := verifyDatabasePolicy(f.Request.Snapshot, f.config()); !errors.Is(e, ErrLegacyDatabaseMaterial) {
		t.Fatal(e)
	}
	c := f.config()
	c.checkpoint = func(point string) {
		if point == "database_legacy_absence_verified" {
			prepareMaterial(t, f)
		}
	}
	if e := verifyDatabasePolicy(f.Request.Snapshot, c); e == nil || errors.Is(e, ErrLegacyDatabaseMaterial) {
		t.Fatal("material appeared during absence proof", e)
	}
}
func TestDatabasePreflightAllowsLiveWALWithoutGrantingCapture(t *testing.T) {
	f := newMaterialFixture(t)
	parent := filepath.Join(f.Root, "database")
	os.Chown(parent, 1001, 1001)
	write(t, filepath.Join(parent, databaseName+"-wal"), []byte("live WAL"), 0600)
	before, _ := os.ReadFile(filepath.Join(parent, databaseName))
	if e := probeDatabaseMigration(f.config()); e != nil {
		t.Fatal(e)
	}
	if _, e := captureDatabaseBefore(f.config()); e == nil {
		t.Fatal("live WAL acquired before authority")
	}
	after, _ := os.ReadFile(filepath.Join(parent, databaseName))
	if !bytes.Equal(before, after) {
		t.Fatal("probe changed database")
	}
	c := f.config()
	c.checkpoint = func(point string) {
		if point == "database_metadata_probed" {
			os.Chmod(filepath.Join(parent, databaseName), 0640)
		}
	}
	if e := probeDatabaseMigration(c); e == nil {
		t.Fatal("metadata changed during preflight")
	}
}
func TestV2MaterialCompletionCompatibility(t *testing.T) {
	f := newMaterialFixture(t)
	prepareMaterial(t, f)
	materialLegacyV2(t, f)
	applyMaterial(t, f, "bin")
	applyMaterial(t, f, "web")
	materialCompletionPhase(t, f, "completion")
	if _, e := verifyCompletionMaterial(f.Request.Snapshot, f.config()); e != nil {
		t.Fatalf("v2 completion unsupported: %v", e)
	}
	if e := verifyDatabasePolicy(f.Request.Snapshot, f.config()); !errors.Is(e, ErrLegacyDatabaseMaterial) {
		t.Fatal(e)
	}
}

// Whole-fixture assertions include the native panel-owned canonical database.
// The production product-tree scanner deliberately requires root:root; this
// observation helper accepts no symlinks and merely records all fixture objects.
func readWholeFixtureTree(t *testing.T, root string) tree {
	t.Helper()
	result := tree{}
	e := filepath.WalkDir(root, func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		rel, e := filepath.Rel(root, path)
		if e != nil {
			return e
		}
		fd, e := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK|unix.O_NOATIME, 0)
		if e != nil {
			return e
		}
		f := os.NewFile(uintptr(fd), path)
		defer f.Close()
		st, e := fstat(f)
		if e != nil {
			return e
		}
		if st.Mode&unix.S_IFMT != unix.S_IFREG && st.Mode&unix.S_IFMT != unix.S_IFDIR {
			return ErrUnavailable
		}
		attrs, e := readAttributes(f)
		if e != nil {
			return e
		}
		sum := ""
		if !d.IsDir() {
			sum, e = hashPinnedFile(f, maxDatabaseSnapshotFile)
			if e != nil {
				return e
			}
		}
		result.Entries = append(result.Entries, entry{Path: rel, Identity: id(st, rel == "."), SHA: sum, Attributes: attrs})
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	sort.Slice(result.Entries, func(i, j int) bool { return result.Entries[i].Path < result.Entries[j].Path })
	return result
}

func TestV2NoopMaterialCompletionCompatibility(t *testing.T) {
	f := noopMaterialFixture(t, "bin", "web")
	prepareMaterial(t, f)
	materialLegacyV2(t, f)
	before := readTree(t, f.Root, filepath.Join(f.Root, "prefix/bin"))
	applyMaterial(t, f, "bin")
	applyMaterial(t, f, "web")
	materialCompletionPhase(t, f, "completion")
	if _, e := verifyCompletionMaterial(f.Request.Snapshot, f.config()); e != nil {
		t.Fatal(e)
	}
	if !before.equal(readTree(t, f.Root, filepath.Join(f.Root, "prefix/bin"))) {
		t.Fatal("v2 no-op replaced product")
	}
}
func TestDatabasePolicyRefusesResidueWithoutMaterial(t *testing.T) {
	for _, kind := range []string{"directory", "symlink", "file", "bad-base"} {
		t.Run(kind, func(t *testing.T) {
			f := newMaterialFixture(t)
			base := filepath.Join(f.Root, "database/.release-db-migrations")
			if e := os.Mkdir(base, 0710); e != nil {
				t.Fatal(e)
			}
			os.Chown(base, 0, 1001)
			path := filepath.Join(base, digest([]byte(strings.Repeat("d", 64))))
			switch kind {
			case "directory":
				os.Mkdir(path, 0700)
			case "symlink":
				os.Symlink("/missing", path)
			case "file":
				write(t, path, []byte("residue"), 0600)
			case "bad-base":
				os.Chmod(base, 0770)
			}
			if e := verifyDatabasePolicy(f.Request.Snapshot, f.config()); e == nil || errors.Is(e, ErrMaterialAbsent) || errors.Is(e, ErrLegacyDatabaseMaterial) {
				t.Fatal("isolated database residue downgraded", e)
			}
		})
	}
}

// New normal admission must not accept layouts which the installer and native
// StateDirectory startup would later normalize after DatabaseBefore was sealed.
func TestDatabaseNewAdmissionRejectsUnsupportedParentWithoutMutation(t *testing.T) {
	for _, tc := range []struct {
		name     string
		uid, gid int
		mode     os.FileMode
	}{
		{"root0700", 0, 0, 0700}, {"root0750", 0, 0, 0750},
		{"panel0700", 1001, 1001, 0700}, {"panel-root-group", 1001, 0, 0750},
	} {
		for _, boundary := range []string{"preflight", "sealed-snapshot-capture"} {
			t.Run(tc.name+"/"+boundary, func(t *testing.T) {
				f := newMaterialFixture(t)
				parent := filepath.Join(f.Root, "database")
				if e := os.Chown(parent, tc.uid, tc.gid); e != nil {
					t.Fatal(e)
				}
				if e := os.Chmod(parent, tc.mode); e != nil {
					t.Fatal(e)
				}
				if boundary == "preflight" {
					write(t, filepath.Join(parent, databaseName+"-wal"), []byte("owner live WAL"), 0600)
				}
				before := readWholeFixtureTree(t, f.Root)
				var e error
				if boundary == "preflight" {
					e = probeDatabaseMigration(f.config())
				} else {
					e = prepareRecoveryMaterial(f.Request, f.config())
				}
				if !errors.Is(e, ErrUnsupportedDatabaseParent) {
					t.Errorf("unsupported parent not classified before a later installer normalization: %v", e)
				}
				if !before.equal(readWholeFixtureTree(t, f.Root)) {
					t.Error("rejected parent, canonical database, WAL, snapshot, or material namespace changed")
				}
			})
		}
	}
}

func TestDatabaseHistoricalParentAuthorityRemainsReadable(t *testing.T) {
	for _, tc := range []struct {
		name     string
		uid, gid int
		mode     os.FileMode
	}{
		{"root0700", 0, 0, 0700}, {"root0750", 0, 0, 0750}, {"panel0700", 1001, 1001, 0700},
	} {
		for _, op := range []string{"update", "rollback"} {
			t.Run(tc.name+"/"+op, func(t *testing.T) {
				f := newMaterialFixture(t)
				prepareMaterial(t, f)
				parent := filepath.Join(f.Root, "database")
				if e := os.Chown(parent, tc.uid, tc.gid); e != nil {
					t.Fatal(e)
				}
				if e := os.Chmod(parent, tc.mode); e != nil {
					t.Fatal(e)
				}
				// Model already sealed historical v3 authority, not a new admission.
				alterMaterial(t, f, func(r *materialRecord) {
					r.DatabaseBefore.Parent.UID = uint32(tc.uid)
					r.DatabaseBefore.Parent.GID = uint32(tc.gid)
					r.DatabaseBefore.Parent.Mode = unix.S_IFDIR | uint32(tc.mode)
				})
				f.marker(t, op)
				if op == "rollback" {
					materialCompletionPhase(t, f, "completion")
				}
				before := readWholeFixtureTree(t, f.Root)
				a, e := openDatabaseAuthority(f.Request.Snapshot, f.config())
				if e != nil {
					t.Fatal("historical authority refused", e)
				}
				if e = a.Revalidate(); e != nil {
					t.Fatal(e)
				}
				if e = a.Close(); e != nil {
					t.Fatal(e)
				}
				if !before.equal(readWholeFixtureTree(t, f.Root)) {
					t.Fatal("historical reader normalized evidence")
				}
			})
		}
	}
}

func TestDatabaseNewAdmissionPreservesConcurrentParentChange(t *testing.T) {
	for _, boundary := range []string{"preflight", "material_ready"} {
		t.Run(boundary, func(t *testing.T) {
			f := newMaterialFixture(t)
			parent := filepath.Join(f.Root, "database")
			dbPath := filepath.Join(parent, databaseName)
			before := readWholeFixtureTree(t, parent)
			c := f.config()
			changed := false
			c.checkpoint = func(point string) {
				if !changed && ((boundary == "preflight" && point == "database_metadata_probed") || point == boundary) {
					changed = true
					if e := os.Chmod(parent, 0700); e != nil {
						t.Fatal(e)
					}
				}
			}
			var e error
			if boundary == "preflight" {
				e = probeDatabaseMigration(c)
			} else {
				e = prepareRecoveryMaterial(f.Request, c)
			}
			if !changed || e == nil {
				t.Fatal("parent change accepted", changed, e)
			}
			st, statErr := os.Stat(parent)
			if statErr != nil || st.Mode().Perm() != 0700 {
				t.Fatal("owner parent mode was overwritten", statErr)
			}
			after := readWholeFixtureTree(t, parent)
			if !(tree{Entries: []entry{before.entryMap()[databaseName]}}).equal(tree{Entries: []entry{after.entryMap()[databaseName]}}) {
				t.Fatal("canonical database changed", dbPath)
			}
			if _, e := os.Lstat(materialPath(f)); !errors.Is(e, os.ErrNotExist) {
				t.Fatal("material published despite owner parent change", e)
			}
		})
	}
}
