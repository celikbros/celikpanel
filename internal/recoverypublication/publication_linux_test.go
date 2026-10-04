//go:build linux

package recoverypublication

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

type fixture struct {
	Root      string
	Request   Request
	Operation string
	lock      *os.File
}

func write(t *testing.T, path string, raw []byte, mode fs.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}
func checksumManifest(t *testing.T, root string) string {
	t.Helper()
	var names []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && filepath.Base(path) != "SHA256SUMS" {
			rel, e := filepath.Rel(root, path)
			if e != nil {
				return e
			}
			names = append(names, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		raw, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&b, "%s  ./%s\n", digest(raw), name)
	}
	raw := []byte(b.String())
	write(t, filepath.Join(root, "SHA256SUMS"), raw, 0600)
	return digest(raw)
}
func copyFixtureTree(t *testing.T, source, target string) {
	t.Helper()
	err := filepath.WalkDir(source, func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		rel, e := filepath.Rel(source, path)
		if e != nil {
			return e
		}
		out := filepath.Join(target, rel)
		info, e := d.Info()
		if e != nil {
			return e
		}
		if d.IsDir() {
			if e = os.MkdirAll(out, info.Mode().Perm()); e != nil {
				return e
			}
			return os.Chmod(out, info.Mode().Perm())
		}
		raw, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		write(t, out, raw, info.Mode().Perm())
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
func newFixture(t *testing.T, resource, op string) fixture {
	t.Helper()
	if os.Geteuid() != 0 || os.Getegid() != 0 {
		t.Skip("protected root filesystem proof requires root:root")
	}
	root := t.TempDir()
	os.Chmod(root, 0700)
	commit := strings.Repeat("a", 40)
	name := "20260914T120000Z-from-unknown-to-" + commit + "-" + strings.Repeat("b", 32)
	snap := filepath.Join(root, "snapshots", name)
	candidate := filepath.Join(root, "releases", commit[:12]+"-"+strings.Repeat("c", 24))
	for _, dir := range []string{snap, candidate, filepath.Join(root, "prefix"), filepath.Join(root, "transaction")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	oldFiles := map[string]string{"bin/agent": "old-agent", "bin/panel": "old-panel", "bin/owner-helper": "owner-helper", "web/index.html": "old-index", "web/assets/old.js": "old-script"}
	for name, raw := range oldFiles {
		mode := fs.FileMode(0644)
		if strings.HasPrefix(name, "bin/") {
			mode = 0755
		}
		write(t, filepath.Join(snap, name), []byte(raw), mode)
	}
	write(t, filepath.Join(snap, "snapshot.version"), []byte("6\n"), 0600)
	write(t, filepath.Join(snap, "target-release.commit"), []byte(commit+"\n"), 0600)
	write(t, filepath.Join(snap, "unrelated-evidence"), []byte("full outer manifest has other expected data"), 0600)
	for name, raw := range map[string]string{"bin/agent": "new-agent", "bin/panel": "new-panel", "bin/schema17-bridge": "bridge", "web/dist/index.html": "new-index", "web/dist/assets/new.js": "new-script", "release.commit": commit + "\n", "release.version": "1\n", "other-release-file": "other"} {
		mode := fs.FileMode(0644)
		if strings.HasPrefix(name, "bin/") {
			mode = 0755
		}
		write(t, filepath.Join(candidate, name), []byte(raw), mode)
	}
	copyFixtureTree(t, filepath.Join(snap, "bin"), filepath.Join(root, "prefix/bin"))
	copyFixtureTree(t, filepath.Join(snap, "web"), filepath.Join(root, "prefix/web"))
	if op == "rollback" {
		write(t, filepath.Join(root, "prefix/bin/agent"), []byte("new-agent"), 0755)
		write(t, filepath.Join(root, "prefix/bin/panel"), []byte("new-panel"), 0755)
		if err := os.RemoveAll(filepath.Join(root, "prefix/web")); err != nil {
			t.Fatal(err)
		}
		copyFixtureTree(t, filepath.Join(candidate, "web/dist"), filepath.Join(root, "prefix/web"))
	}
	f := fixture{Root: root, Operation: op, Request: Request{Resource: resource, Snapshot: name, CandidateRoot: candidate}}
	f.Request.SnapshotManifest = checksumManifest(t, snap)
	f.Request.CandidateManifest = checksumManifest(t, candidate)
	f.marker(t, op)
	lock, err := os.OpenFile(filepath.Join(root, "transaction/transaction.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err = unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	f.lock = lock
	t.Cleanup(func() { lock.Close() })
	return f
}
func (f fixture) marker(t *testing.T, op string) {
	write(t, filepath.Join(f.Root, "transaction/active"), []byte("version=1\ntoken="+strings.Repeat("d", 64)+"\noperation="+op+"\nsnapshot="+f.Request.Snapshot+"\n"), 0600)
}
func (f fixture) config() config {
	return config{anchor: f.Root, prefix: filepath.Join(f.Root, "prefix"), snapshots: filepath.Join(f.Root, "snapshots"), candidates: filepath.Join(f.Root, "releases"), transaction: filepath.Join(f.Root, "transaction"), fd: int(f.lock.Fd()), databaseOwner: func() (uint32, uint32, error) { return 1001, 1001, nil }, stopped: func() error { return nil }}
}
func (f fixture) journal() string {
	return filepath.Join(f.Root, "prefix", journalName, digest([]byte(strings.Repeat("d", 64))), f.Operation+"-"+f.Request.Resource)
}
func readTree(t *testing.T, root, path string) tree {
	t.Helper()
	if root == path {
		return readWholeFixtureTree(t, root)
	}
	f, err := openPath(root, path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	v, err := scan(f)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func readIntent(t *testing.T, f fixture) intent {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(f.journal(), "intent.json"))
	if err != nil {
		t.Fatal(err)
	}
	var v intent
	if err = decodeExact(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v
}
func TestPublicationAndReplay(t *testing.T) {
	for _, op := range []string{"update", "rollback"} {
		for _, resource := range []string{"bin", "web"} {
			t.Run(op+"/"+resource, func(t *testing.T) {
				f := newFixture(t, resource, op)
				before := readTree(t, f.Root, filepath.Join(f.Root, "prefix", resource))
				if err := publish(f.Request, op, f.config()); err != nil {
					t.Fatal(err)
				}
				i := readIntent(t, f)
				after := readTree(t, f.Root, filepath.Join(f.Root, "prefix", resource))
				retired := readTree(t, f.Root, filepath.Join(f.journal(), i.Stage))
				if !after.equal(i.After) || !retired.equal(before) {
					t.Fatal("exchange did not preserve exact retired tree")
				}
				if err := publish(f.Request, op, f.config()); err != nil {
					t.Fatal(err)
				}
				if !readTree(t, f.Root, filepath.Join(f.Root, "prefix", resource)).equal(after) {
					t.Fatal("replay changed restored identity")
				}
			})
		}
	}
}
func TestIndependentPublicationChild(t *testing.T) {
	if os.Getenv("CP_RECOVERY_PUBLICATION_CHILD") == "" {
		return
	}
	root := os.Getenv("CP_RECOVERY_PUBLICATION_ROOT")
	raw, err := os.ReadFile(filepath.Join(root, "fixture.json"))
	if err != nil {
		t.Fatal(err)
	}
	var f fixture
	if json.Unmarshal(raw, &f) != nil {
		t.Fatal("fixture")
	}
	f.lock = os.NewFile(9, "inherited-test-lock")
	defer f.lock.Close() // Keep the inherited descriptor alive through JSON/tree allocations.
	c := f.config()
	point := os.Getenv("CP_RECOVERY_PUBLICATION_POINT")
	lastPoint := "before-load"
	c.checkpoint = func(name string) {
		lastPoint = name
		if name == point {
			unix.Kill(os.Getpid(), unix.SIGKILL)
		}
	}
	if f.Operation == "material" {
		err = prepareRecoveryMaterial(f.Request, c)
	} else if f.Operation == "verify-completion" {
		err = verifyInstalledCompletion(f.Request.Snapshot, c)
	} else {
		err = publish(f.Request, f.Operation, c)
	}
	if err != nil {
		t.Fatalf("after checkpoint %s: %v", lastPoint, err)
	}
	t.Fatal("requested kill checkpoint was not reached")
}
func crash(t *testing.T, f fixture, point string) {
	t.Helper()
	raw, _ := json.Marshal(f)
	write(t, filepath.Join(f.Root, "fixture.json"), raw, 0600)
	null, err := os.Open("/dev/null")
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()
	cmd := exec.Command(os.Args[0], "-test.run=^TestIndependentPublicationChild$")
	cmd.Env = append(os.Environ(), "CP_RECOVERY_PUBLICATION_CHILD=1", "CP_RECOVERY_PUBLICATION_ROOT="+f.Root, "CP_RECOVERY_PUBLICATION_POINT="+point)
	cmd.ExtraFiles = []*os.File{null, null, null, null, null, null, f.lock}
	out, err := cmd.CombinedOutput()
	exit, ok := err.(*exec.ExitError)
	if !ok || !exit.Sys().(syscall.WaitStatus).Signaled() || exit.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
		t.Fatalf("checkpoint %s did not SIGKILL: %v %s", point, err, out)
	}
}
func TestRealProcessDeathAndRetry(t *testing.T) {
	for _, op := range []string{"update", "rollback"} {
		for _, point := range []string{"stage_file_written", "stage_ready", "intent_durable", "before_exchange", "exchange_done", "exchange_durable", "receipt_staged", "receipt_published", "receipt_durable"} {
			t.Run(op+"/"+point, func(t *testing.T) {
				f := newFixture(t, "web", op)
				crash(t, f, point)
				if err := publish(f.Request, op, f.config()); err != nil {
					t.Fatal(err)
				}
				i := readIntent(t, f)
				if !readTree(t, f.Root, filepath.Join(f.Root, "prefix/web")).equal(i.After) || !readTree(t, f.Root, filepath.Join(f.journal(), i.Stage)).equal(i.Before) {
					t.Fatal("retry lost exact pre/post pair")
				}
				if point == "stage_file_written" || point == "stage_ready" {
					stages, _ := filepath.Glob(filepath.Join(f.journal(), ".stage-*"))
					if len(stages) != 2 {
						t.Fatal("uncommitted stage was erased/adopted instead of retained")
					}
				}
			})
		}
	}
}
func TestForwardThenRollbackSharesPublicationRules(t *testing.T) {
	for _, resource := range []string{"bin", "web"} {
		t.Run(resource, func(t *testing.T) {
			f := newFixture(t, resource, "update")
			old := readTree(t, f.Root, filepath.Join(f.Root, "prefix", resource))
			crash(t, f, "exchange_done")
			f.marker(t, "rollback")
			f.Operation = "rollback"
			if err := publish(f.Request, "rollback", f.config()); err != nil {
				t.Fatal(err)
			}
			got := readTree(t, f.Root, filepath.Join(f.Root, "prefix", resource))
			if got.semantic() != old.semantic() {
				t.Fatal("rollback after forward exchange did not restore old resource")
			}
		})
	}
}
func TestOwnerChangesAndUnsafeObjectsAreRefused(t *testing.T) {
	for _, kind := range []string{"owner-file", "owner-edit", "missing", "partial", "mode", "hardlink", "symlink", "fifo", "wrong-manifest", "wrong-operation"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t, "web", "rollback")
			root := filepath.Join(f.Root, "prefix/web")
			path := filepath.Join(root, "index.html")
			var expected []byte
			switch kind {
			case "owner-file":
				write(t, filepath.Join(root, "owner.txt"), []byte("owner edit"), 0644)
			case "owner-edit":
				write(t, path, []byte("owner edit"), 0644)
			case "missing":
				os.RemoveAll(root)
			case "partial":
				os.Remove(filepath.Join(root, "assets/new.js"))
			case "mode":
				os.Chmod(path, 0666)
			case "hardlink":
				if err := os.Link(path, filepath.Join(f.Root, "owner-link")); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				os.Remove(path)
				os.Symlink(filepath.Join(f.Root, "outside"), path)
			case "fifo":
				os.Remove(path)
				if err := unix.Mkfifo(path, 0600); err != nil {
					t.Fatal(err)
				}
			case "wrong-manifest":
				f.Request.SnapshotManifest = strings.Repeat("e", 64)
			case "wrong-operation":
				f.marker(t, "update")
			}
			if kind != "fifo" && kind != "symlink" {
				expected, _ = os.ReadFile(path)
			}
			if err := publish(f.Request, "rollback", f.config()); err == nil {
				t.Fatal("unsafe owner state accepted")
			}
			if kind != "fifo" && kind != "symlink" {
				actual, _ := os.ReadFile(path)
				if !bytes.Equal(expected, actual) {
					t.Fatal("owner bytes overwritten")
				}
			}
		})
	}
}
func TestOwnerChangeAfterIntentAndExchange(t *testing.T) {
	for _, where := range []string{"live-before", "stage-before", "live-after", "retired-after", "journal-replaced"} {
		t.Run(where, func(t *testing.T) {
			f := newFixture(t, "web", "rollback")
			point := "intent_durable"
			if strings.HasSuffix(where, "after") {
				point = "exchange_done"
			}
			crash(t, f, point)
			i := readIntent(t, f)
			path := filepath.Join(f.Root, "prefix/web/index.html")
			if where == "stage-before" || where == "retired-after" {
				path = filepath.Join(f.journal(), i.Stage, "index.html")
			}
			if where == "journal-replaced" {
				if err := os.Rename(f.journal(), f.journal()+"-owner-retained"); err != nil {
					t.Fatal(err)
				}
				os.Mkdir(f.journal(), 0700)
				write(t, filepath.Join(f.journal(), "owner.txt"), []byte("preserve"), 0600)
			} else {
				write(t, path, []byte("owner-after-proof"), 0644)
			}
			if err := publish(f.Request, "rollback", f.config()); err == nil {
				t.Fatal("intervening owner change accepted")
			}
			if where != "journal-replaced" {
				raw, _ := os.ReadFile(path)
				if string(raw) != "owner-after-proof" {
					t.Fatal("owner edit lost")
				}
			}
		})
	}
}
func TestLastBoundaryRechecksOwnerChanges(t *testing.T) {
	f := newFixture(t, "web", "rollback")
	c := f.config()
	c.checkpoint = func(name string) {
		if name == "before_exchange" {
			write(t, filepath.Join(f.Root, "prefix/web/index.html"), []byte("late-owner"), 0644)
		}
	}
	if err := publish(f.Request, "rollback", c); err == nil {
		t.Fatal("late owner edit accepted")
	}
	raw, _ := os.ReadFile(filepath.Join(f.Root, "prefix/web/index.html"))
	if string(raw) != "late-owner" {
		t.Fatal("late owner edit overwritten")
	}
}
func TestLockAndStoppedServicesAreRequired(t *testing.T) {
	for _, kind := range []string{"unlocked", "wrong-fd", "running"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t, "bin", "rollback")
			c := f.config()
			switch kind {
			case "unlocked":
				unix.Flock(int(f.lock.Fd()), unix.LOCK_UN)
			case "wrong-fd":
				c.fd = -1
			case "running":
				c.stopped = func() error { return ErrUnavailable }
			}
			if err := publish(f.Request, "rollback", c); err == nil {
				t.Fatal("missing barrier accepted")
			}
		})
	}
}

func TestEvidenceMetadataAndOrphansRefuseWithoutOverwrite(t *testing.T) {
	for _, kind := range []string{"intent-mode", "intent-hardlink", "intent-unknown-field", "published-mode", "orphan-published", "orphan-stage-symlink", "orphan-temp-fifo", "marker-mode", "source-edit", "source-metadata", "wrong-target", "snapshot-from-commit"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t, "web", "rollback")
			if kind == "snapshot-from-commit" {
				old := filepath.Join(f.Root, "snapshots", f.Request.Snapshot)
				f.Request.Snapshot = strings.Replace(f.Request.Snapshot, "from-unknown", "from-"+strings.Repeat("f", 40), 1)
				if err := os.Rename(old, filepath.Join(f.Root, "snapshots", f.Request.Snapshot)); err != nil {
					t.Fatal(err)
				}
				f.marker(t, "rollback")
				if err := publish(f.Request, "rollback", f.config()); err != nil {
					t.Fatal(err)
				}
				return
			}
			point := "intent_durable"
			if kind == "published-mode" {
				point = "receipt_durable"
			}
			crash(t, f, point)
			path := filepath.Join(f.journal(), "intent.json")
			switch kind {
			case "intent-mode":
				os.Chmod(path, 0644)
			case "intent-hardlink":
				if err := os.Link(path, filepath.Join(f.Root, "owner-link")); err != nil {
					t.Fatal(err)
				}
			case "intent-unknown-field":
				raw, _ := os.ReadFile(path)
				raw = bytes.Replace(raw, []byte("{\"schema\":"), []byte("{\"unknown\":true,\"schema\":"), 1)
				write(t, path, raw, 0600)
			case "published-mode":
				os.Chmod(filepath.Join(f.journal(), "published"), 0644)
			case "orphan-published":
				os.Remove(path)
				write(t, filepath.Join(f.journal(), "published"), []byte("unproven"), 0600)
			case "orphan-stage-symlink":
				os.Symlink(filepath.Join(f.Root, "prefix/web"), filepath.Join(f.journal(), ".stage-"+strings.Repeat("a", 32)))
			case "orphan-temp-fifo":
				unix.Mkfifo(filepath.Join(f.journal(), ".published-"+strings.Repeat("a", 32)), 0600)
			case "marker-mode":
				os.Chmod(filepath.Join(f.Root, "transaction/active"), 0644)
			case "source-edit":
				write(t, filepath.Join(f.Request.CandidateRoot, "web/dist/index.html"), []byte("owner-new-source"), 0644)
			case "source-metadata":
				os.Chmod(filepath.Join(f.Root, "snapshots", f.Request.Snapshot, "web/index.html"), 0664)
			case "wrong-target":
				f.Request.CandidateManifest = strings.Repeat("f", 64)
			}
			before := readTree(t, f.Root, filepath.Join(f.Root, "prefix/web"))
			if err := publish(f.Request, "rollback", f.config()); err == nil {
				t.Fatal("unproven evidence accepted")
			}
			after := readTree(t, f.Root, filepath.Join(f.Root, "prefix/web"))
			if !before.equal(after) {
				t.Fatal("rejected observation changed current resource")
			}
		})
	}
}

func TestAllBinProcessDeathWindows(t *testing.T) {
	for _, op := range []string{"update", "rollback"} {
		for _, point := range []string{"stage_file_written", "stage_ready", "intent_durable", "exchange_done", "receipt_staged", "receipt_published"} {
			t.Run(op+"/"+point, func(t *testing.T) {
				f := newFixture(t, "bin", op)
				crash(t, f, point)
				if err := publish(f.Request, op, f.config()); err != nil {
					t.Fatal(err)
				}
				i := readIntent(t, f)
				if !readTree(t, f.Root, filepath.Join(f.Root, "prefix/bin")).equal(i.After) {
					t.Fatal("bin replay identity")
				}
				raw, err := os.ReadFile(filepath.Join(f.Root, "prefix/bin/owner-helper"))
				if err != nil || string(raw) != "owner-helper" {
					t.Fatal("captured owner extra was not preserved")
				}
			})
		}
	}
}

func TestRollbackHonorsForwardRetiredEvidence(t *testing.T) {
	for _, point := range []string{"intent_durable", "exchange_done", "receipt_durable"} {
		for _, edit := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/edit=%t", point, edit), func(t *testing.T) {
				f := newFixture(t, "web", "update")
				crash(t, f, point)
				i := readIntent(t, f)
				if edit {
					write(t, filepath.Join(f.journal(), i.Stage, "index.html"), []byte("owner-stage"), 0644)
				}
				f.marker(t, "rollback")
				f.Operation = "rollback"
				err := publish(f.Request, "rollback", f.config())
				if edit && err == nil {
					t.Fatal("rollback ignored owner changes to forward evidence")
				}
				if !edit && err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func setUserAttr(t *testing.T, path, name, value string) {
	t.Helper()
	if err := unix.Setxattr(path, name, []byte(value), 0); err != nil {
		t.Fatal(err)
	}
}
func TestBoundedXattrsPreservedAndOwnerChangesRefused(t *testing.T) {
	for _, op := range []string{"update", "rollback"} {
		t.Run(op, func(t *testing.T) {
			f := newFixture(t, "web", op)
			source := filepath.Join(f.Root, "snapshots", f.Request.Snapshot, "web")
			if op == "update" {
				source = filepath.Join(f.Request.CandidateRoot, "web/dist")
			}
			setUserAttr(t, source, "user.celikpanel-test", "directory evidence")
			setUserAttr(t, filepath.Join(source, "index.html"), "user.celikpanel-test", "file evidence")
			// For rollback candidate current is unchanged; update starts old current.
			crash(t, f, "exchange_done")
			if err := publish(f.Request, op, f.config()); err != nil {
				t.Fatal(err)
			}
			i := readIntent(t, f)
			for _, name := range []string{".", "index.html"} {
				attrs := i.After.entryMap()[name].Attributes
				if len(attrs) != 1 || attrs[0].Name != "user.celikpanel-test" || attrs[0].SHA != digest(attrs[0].Value) {
					t.Fatal("attribute missing from durable intent")
				}
			}
			setUserAttr(t, filepath.Join(f.Root, "prefix/web/index.html"), "user.celikpanel-test", "owner changed")
			if err := publish(f.Request, op, f.config()); err == nil {
				t.Fatal("owner attribute change ignored")
			}
		})
	}
}
func TestPrequiesceEligibilityIsReadOnlyAndRejectsUnsupportedXattrs(t *testing.T) {
	for _, kind := range []string{"valid", "user-attribute", "unknown-namespace", "selinux", "acl", "too-many", "too-large", "parent-default-acl"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t, "bin", "update")
			file := filepath.Join(f.Root, "prefix/bin/agent")
			switch kind {
			case "user-attribute":
				setUserAttr(t, file, "user.note", "owner evidence")
			case "unknown-namespace":
				if err := unix.Setxattr(file, "trusted.celikpanel-test", []byte("unknown"), 0); err != nil {
					t.Skipf("host rejects trusted namespace fixture: %v", err)
				}
			case "selinux":
				if err := unix.Setxattr(file, "security.selinux", []byte("system_u:object_r:var_t:s0\x00"), 0); err != nil {
					t.Skipf("host does not support a SELinux fixture: %v", err)
				}
				fh, err := os.Open(file)
				if err != nil {
					t.Fatal(err)
				}
				_, attrErr := readAttributes(fh)
				fh.Close()
				if attrErr == nil {
					// Some kernels accept security.selinux writes while omitting the
					// label from listxattr because SELinux is disabled. No fixture exists.
					raw := make([]byte, 8192)
					n, listErr := unix.Listxattr(file, raw)
					if listErr != nil {
						t.Fatal(listErr)
					}
					if !bytes.Contains(raw[:n], []byte("security.selinux\x00")) {
						t.Skip("kernel does not expose a SELinux label")
					}
				}
			case "too-many":
				for i := 0; i < 33; i++ {
					setUserAttr(t, file, fmt.Sprintf("user.fixture%d", i), "v")
				}
			case "too-large":
				// A regular file supports at most one filesystem xattr block here; the
				// explicit per-value limit is separately covered by the parser's bounds.
				t.Skip("filesystem cannot reliably store an oversized xattr")
			case "acl", "parent-default-acl":
				// Linux POSIX ACL v2: user::rwx,user:12345:r--,group::r-x,mask::r-x,other::r-x.
				value := []byte{2, 0, 0, 0, 1, 0, 7, 0, 255, 255, 255, 255, 2, 0, 4, 0, 57, 48, 0, 0, 4, 0, 5, 0, 255, 255, 255, 255, 16, 0, 5, 0, 255, 255, 255, 255, 32, 0, 5, 0, 255, 255, 255, 255}
				name := "system.posix_acl_access"
				if kind == "parent-default-acl" {
					file = filepath.Join(f.Root, "prefix")
					name = "system.posix_acl_default"
				}
				if err := unix.Setxattr(file, name, value, 0); err != nil {
					t.Skipf("host rejects ACL fixture: %v", err)
				}
			}
			raw, err := os.ReadFile(file)
			if kind == "parent-default-acl" {
				raw = nil
				err = nil
			}
			if err != nil {
				t.Fatal(err)
			}
			info, err := os.Lstat(file)
			if err != nil {
				t.Fatal(err)
			}
			got := probeCurrentResources(f.config())
			okay := kind == "valid" || kind == "user-attribute"
			if okay && got != nil {
				t.Fatal(got)
			}
			if !okay && got == nil {
				t.Fatal("unsupported xattrs admitted before downtime")
			}
			after, err := os.Lstat(file)
			if err != nil || !os.SameFile(info, after) || info.Mode() != after.Mode() {
				t.Fatal("probe changed identity")
			}
			if kind != "parent-default-acl" {
				next, _ := os.ReadFile(file)
				if !bytes.Equal(raw, next) {
					t.Fatal("probe changed bytes")
				}
			}
			if _, err := os.Lstat(filepath.Join(f.Root, "prefix", journalName)); !os.IsNotExist(err) {
				t.Fatal("eligibility probe created a publication journal")
			}
		})
	}
}
