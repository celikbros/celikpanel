//go:build linux

package recoverypublication

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func newMaterialFixture(t *testing.T) fixture {
	t.Helper()
	f := newFixture(t, "bin", "update")
	for _, name := range materialFiles {
		if name == "release.version" || name == "release.commit" {
			continue
		}
		raw := "fixture-only data: " + name + "\n"
		if name == "release.tree" {
			raw = strings.Repeat("e", 40) + "\n"
		}
		write(t, filepath.Join(f.Request.CandidateRoot, name), []byte(raw), 0644)
	}
	write(t, filepath.Join(f.Root, "snapshots", f.Request.Snapshot, "target-release.tree"), []byte(strings.Repeat("e", 40)+"\n"), 0600)
	f.Request.CandidateManifest = checksumManifest(t, f.Request.CandidateRoot)
	f.Request.SnapshotManifest = checksumManifest(t, filepath.Join(f.Root, "snapshots", f.Request.Snapshot))
	return f
}
func materialPath(f fixture) string {
	return filepath.Join(materialBase(f.config()), digest([]byte(f.Request.Snapshot)))
}
func prepareMaterial(t *testing.T, f fixture) {
	t.Helper()
	if err := prepareRecoveryMaterial(f.Request, f.config()); err != nil {
		t.Fatal(err)
	}
}
func applyMaterial(t *testing.T, f fixture, resource string) {
	t.Helper()
	r := f.Request
	r.Resource = resource
	if err := publish(r, "update", f.config()); err != nil {
		t.Fatal(err)
	}
}
func TestMaterialIndependentRollbackAfterWholeCandidateDisappears(t *testing.T) {
	f := newMaterialFixture(t)
	prepareMaterial(t, f)
	prepareMaterial(t, f) // Durable preparation retry does not change the evidence.
	for _, resource := range []string{"bin", "web"} {
		applyMaterial(t, f, resource)
	}
	if err := os.Rename(f.Request.CandidateRoot, f.Request.CandidateRoot+".unavailable"); err != nil {
		t.Fatal(err)
	}
	f.marker(t, "rollback")
	c := f.config()
	c.stopped = func() error { return ErrUnavailable }
	m, err := readMaterial(f.Request.Snapshot, c)
	if err != nil {
		t.Fatalf("read-only material verify required stopped services: %v", err)
	}
	m.close()
	for _, resource := range []string{"bin", "web"} {
		r := f.Request
		r.Resource = resource
		if err = publish(r, "rollback", f.config()); err != nil {
			t.Fatalf("%s: %v", resource, err)
		}
		actual := readTree(t, f.Root, filepath.Join(f.Root, "prefix", resource))
		old := readTree(t, f.Root, filepath.Join(f.Root, "snapshots", r.Snapshot, resource))
		if actual.semantic() != old.semantic() {
			t.Fatalf("%s not restored", resource)
		}
		if err = publish(r, "rollback", f.config()); err != nil {
			t.Fatal(err)
		}
		if !actual.equal(readTree(t, f.Root, filepath.Join(f.Root, "prefix", resource))) {
			t.Fatal("retry changed restored resource")
		}
	}
}
func TestMaterialDataContract(t *testing.T) {
	f := newMaterialFixture(t)
	prepareMaterial(t, f)
	m, err := readMaterial(f.Request.Snapshot, f.config())
	if err != nil {
		t.Fatal(err)
	}
	defer m.close()
	if m.record.Schema != MaterialSchema {
		t.Fatal("schema")
	}
	for _, name := range materialFiles {
		raw, e := os.ReadFile(filepath.Join(m.path, "data", name))
		if e != nil {
			t.Fatal(e)
		}
		source, e := os.ReadFile(filepath.Join(f.Request.CandidateRoot, name))
		if e != nil || string(raw) != string(source) {
			t.Fatalf("source %s mismatch", name)
		}
	}
	for _, name := range []string{"bin", "web", "rollback.sh", "install.sh"} {
		if _, err := os.Lstat(filepath.Join(m.path, "data", name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("unexpected executable payload %s", name)
		}
	}
	applyMaterial(t, f, "bin")
	i := readIntent(t, f)
	if i.Schema != MaterialIntentSchema || i.MaterialSHA != m.sha {
		t.Fatal("forward intent not material-bound")
	}
}
func TestMaterialPrepareRefusesRetrospectiveCapture(t *testing.T) {
	for _, mode := range []string{"candidate-current", "legacy-intent", "rollback-marker", "services-running"} {
		t.Run(mode, func(t *testing.T) {
			f := newMaterialFixture(t)
			c := f.config()
			switch mode {
			case "candidate-current":
				write(t, filepath.Join(f.Root, "prefix/bin/agent"), []byte("new-agent"), 0755)
			case "legacy-intent":
				if err := publish(f.Request, "update", c); err != nil {
					t.Fatal(err)
				}
			case "rollback-marker":
				f.marker(t, "rollback")
			case "services-running":
				c.stopped = func() error { return ErrUnavailable }
			}
			if err := prepareRecoveryMaterial(f.Request, c); err == nil {
				t.Fatal("retrospective capture admitted")
			}
			if _, err := os.Lstat(materialPath(f)); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("material published after failed preparation")
			}
		})
	}
}
func TestMaterialMissingDoesNotDowngradeV2Authority(t *testing.T) {
	f := newMaterialFixture(t)
	prepareMaterial(t, f)
	applyMaterial(t, f, "bin")
	if err := os.Rename(materialPath(f), materialPath(f)+".missing"); err != nil {
		t.Fatal(err)
	}
	// The missing material blocks even web, which has not acquired its own intent.
	r := f.Request
	r.Resource = "web"
	if err := publish(r, "update", f.config()); err == nil || errors.Is(err, ErrMaterialAbsent) {
		t.Fatalf("downgraded: %v", err)
	}
	f.marker(t, "rollback")
	if _, err := readMaterial(r.Snapshot, f.config()); err == nil || errors.Is(err, ErrMaterialAbsent) {
		t.Fatalf("runner can downgrade: %v", err)
	}
}
func TestMaterialMissingRejectsOrphanPublishedWithoutMutation(t *testing.T) {
	for _, journal := range []string{"update-bin", "update-web", "rollback-bin", "rollback-web"} {
		for _, receipt := range []string{"complete", "empty"} {
			t.Run(journal+"/"+receipt, func(t *testing.T) {
				f := newMaterialFixture(t)
				path := filepath.Join(filepath.Dir(f.journal()), journal)
				if err := os.MkdirAll(path, 0700); err != nil {
					t.Fatal(err)
				}
				raw := []byte("format=celikpanel-resource-publication-v1\nintent=" + strings.Repeat("f", 64) + "\n")
				if receipt == "empty" {
					raw = nil
				}
				write(t, filepath.Join(path, "published"), raw, 0600)
				before := readTree(t, f.Root, f.Root)
				c := f.config()
				c.stopped = func() error { t.Fatal("reader requested stopped services"); return nil }
				m, err := readMaterial(f.Request.Snapshot, c)
				if m != nil {
					m.close()
				}
				if err == nil || errors.Is(err, ErrMaterialAbsent) {
					t.Fatalf("orphan committed receipt admitted as legacy absence: %v", err)
				}
				if !before.equal(readTree(t, f.Root, f.Root)) {
					t.Fatal("read changed owner state or evidence")
				}
				if err = prepareRecoveryMaterial(f.Request, f.config()); err == nil || errors.Is(err, ErrMaterialAbsent) {
					t.Fatalf("preparation accepted orphan committed receipt: %v", err)
				}
				if !before.equal(readTree(t, f.Root, f.Root)) {
					t.Fatal("preparation changed owner state or evidence")
				}
			})
		}
	}
}

func TestMaterialMissingPreservesUncommittedJournalDebris(t *testing.T) {
	f := newMaterialFixture(t)
	path := f.journal()
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(path, ".intent-"+strings.Repeat("e", 32)), []byte("interrupted intent write"), 0600)
	write(t, filepath.Join(path, ".published-"+strings.Repeat("f", 32)), []byte("interrupted receipt write"), 0600)
	if err := os.Mkdir(filepath.Join(path, ".stage-"+strings.Repeat("a", 32)), 0700); err != nil {
		t.Fatal(err)
	}
	before := readTree(t, f.Root, path)
	if _, err := readMaterial(f.Request.Snapshot, f.config()); !errors.Is(err, ErrMaterialAbsent) {
		t.Fatalf("uncommitted debris became publication authority: %v", err)
	}
	prepareMaterial(t, f)
	if !before.equal(readTree(t, f.Root, path)) {
		t.Fatal("uncommitted evidence changed or was adopted")
	}
}

func TestMaterialMalformedNeverLegacyFallback(t *testing.T) {
	for _, fault := range []string{"json", "data-content", "data-mode", "extra-file", "symlink", "snapshot", "token", "material-sha-intent"} {
		t.Run(fault, func(t *testing.T) {
			f := newMaterialFixture(t)
			prepareMaterial(t, f)
			applyMaterial(t, f, "bin")
			switch fault {
			case "json":
				file, err := os.OpenFile(filepath.Join(materialPath(f), "material.json"), os.O_APPEND|os.O_WRONLY, 0)
				if err != nil {
					t.Fatal(err)
				}
				file.WriteString(" ")
				file.Close()
			case "data-content":
				write(t, filepath.Join(materialPath(f), "data/deploy/release-recovery.protocol"), []byte("owner edit"), 0600)
			case "data-mode":
				if err := os.Chmod(filepath.Join(materialPath(f), "data/release.commit"), 0644); err != nil {
					t.Fatal(err)
				}
			case "extra-file":
				write(t, filepath.Join(materialPath(f), "extra"), []byte("unmodeled"), 0600)
			case "symlink":
				if err := os.Rename(materialPath(f), materialPath(f)+".original"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(materialPath(f)+".original", materialPath(f)); err != nil {
					t.Fatal(err)
				}
			case "snapshot":
				write(t, filepath.Join(f.Root, "snapshots", f.Request.Snapshot, "bin/agent"), []byte("corrupt old"), 0755)
			case "token":
				raw, _ := os.ReadFile(filepath.Join(f.Root, "transaction/active"))
				write(t, filepath.Join(f.Root, "transaction/active"), []byte(strings.ReplaceAll(string(raw), strings.Repeat("d", 64), strings.Repeat("f", 64))), 0600)
			case "material-sha-intent":
				i := readIntent(t, f)
				i.MaterialSHA = strings.Repeat("f", 64)
				write(t, filepath.Join(f.journal(), "intent.json"), canonical(i), 0600)
			}
			if fault != "token" {
				f.marker(t, "rollback")
			} // Keep the mismatched token when testing it.
			before := readTree(t, f.Root, filepath.Join(f.Root, "prefix/bin"))
			if err := publish(f.Request, "rollback", f.config()); err == nil {
				t.Fatal("bad evidence admitted")
			}
			if !before.equal(readTree(t, f.Root, filepath.Join(f.Root, "prefix/bin"))) {
				t.Fatal("failed proof changed owner tree")
			}
		})
	}
}
func TestMaterialForwardIntentProtectsOwnerChanges(t *testing.T) {
	for _, where := range []string{"current", "retired", "no-forward"} {
		t.Run(where, func(t *testing.T) {
			f := newMaterialFixture(t)
			prepareMaterial(t, f)
			if where == "no-forward" {
				write(t, filepath.Join(f.Root, "prefix/bin/agent"), []byte("new-agent"), 0755)
				write(t, filepath.Join(f.Root, "prefix/bin/panel"), []byte("new-panel"), 0755)
			} else {
				applyMaterial(t, f, "bin")
				path := filepath.Join(f.Root, "prefix/bin/owner-helper")
				if where == "retired" {
					i := readIntent(t, f)
					path = filepath.Join(f.journal(), i.Stage, "owner-helper")
				}
				write(t, path, []byte("owner private edit"), 0755)
			}
			f.marker(t, "rollback")
			if err := publish(f.Request, "rollback", f.config()); !errors.Is(err, ErrOwnerChanged) {
				t.Fatalf("owner proof missing: %v", err)
			}
		})
	}
}
func TestMaterialMarkerPhasesAndNoWrite(t *testing.T) {
	for _, phase := range []string{"active", "completion", "completion-scheduler", "scheduler", "update-completion", "mixed-active", "mismatch", "unknown"} {
		t.Run(phase, func(t *testing.T) {
			f := newMaterialFixture(t)
			prepareMaterial(t, f)
			f.marker(t, "rollback")
			active := filepath.Join(f.Root, "transaction/active")
			raw, _ := os.ReadFile(active)
			if phase != "active" && phase != "mixed-active" && phase != "unknown" {
				if err := os.Remove(active); err != nil {
					t.Fatal(err)
				}
			}
			switch phase {
			case "completion", "completion-scheduler", "mixed-active", "mismatch":
				write(t, filepath.Join(f.Root, "transaction/completion.pending"), raw, 0600)
			case "update-completion":
				write(t, filepath.Join(f.Root, "transaction/completion.pending"), []byte(strings.ReplaceAll(string(raw), "operation=rollback", "operation=update")), 0600)
			case "unknown":
				write(t, filepath.Join(f.Root, "transaction/unknown"), raw, 0600)
			}
			if phase == "scheduler" || phase == "completion-scheduler" || phase == "mismatch" {
				if phase == "mismatch" {
					raw = []byte(strings.ReplaceAll(string(raw), strings.Repeat("d", 64), strings.Repeat("e", 64)))
				}
				write(t, filepath.Join(f.Root, "transaction/scheduler-restore.pending"), raw, 0600)
			}
			before := readTree(t, f.Root, filepath.Join(f.Root, "transaction"))
			c := f.config()
			c.stopped = func() error { t.Fatal("reader requested stopped services"); return nil }
			m, err := readMaterial(f.Request.Snapshot, c)
			if m != nil {
				m.close()
			}
			allowed := phase == "active" || phase == "completion" || phase == "completion-scheduler" || phase == "scheduler"
			if (err == nil) != allowed {
				t.Fatalf("phase %s: %v", phase, err)
			}
			if !before.equal(readTree(t, f.Root, filepath.Join(f.Root, "transaction"))) {
				t.Fatal("read mutated transaction")
			}
		})
	}
}
func TestMaterialRealSIGKILLAndRetry(t *testing.T) {
	for _, point := range []string{"material_file_written", "material_ready", "material_published", "material_durable"} {
		t.Run(point, func(t *testing.T) {
			f := newMaterialFixture(t)
			f.Operation = "material"
			crash(t, f, point)
			ready := point == "material_published" || point == "material_durable"
			m, err := readMaterial(f.Request.Snapshot, f.config())
			if m != nil {
				m.close()
			}
			if ready && err != nil || !ready && !errors.Is(err, ErrMaterialAbsent) {
				t.Fatalf("unpublished stage admitted or published proof lost: %v", err)
			}
			prepareMaterial(t, f)
			applyMaterial(t, f, "bin")
			if err := os.Rename(f.Request.CandidateRoot, f.Request.CandidateRoot+".removed"); err != nil {
				t.Fatal(err)
			}
			f.marker(t, "rollback")
			if err := publish(f.Request, "rollback", f.config()); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestMaterialPublicationSIGKILLWithoutCandidate(t *testing.T) {
	for _, point := range []string{"intent_durable", "exchange_done", "receipt_published"} {
		t.Run(point, func(t *testing.T) {
			f := newMaterialFixture(t)
			f.Request.Resource = "web"
			prepareMaterial(t, f)
			crash(t, f, point)
			if err := os.Rename(f.Request.CandidateRoot, f.Request.CandidateRoot+".removed"); err != nil {
				t.Fatal(err)
			}
			f.marker(t, "rollback")
			if err := publish(f.Request, "rollback", f.config()); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestMaterialPreparationRevalidatesOriginalInputs(t *testing.T) {
	for _, fault := range []string{"source-metadata", "current-owner", "snapshot-old"} {
		t.Run(fault, func(t *testing.T) {
			f := newMaterialFixture(t)
			c := f.config()
			c.checkpoint = func(point string) {
				if point != "material_ready" {
					return
				}
				switch fault {
				case "source-metadata":
					write(t, filepath.Join(f.Request.CandidateRoot, "deploy/release-recovery.protocol"), []byte("changed after source read"), 0644)
				case "current-owner":
					write(t, filepath.Join(f.Root, "prefix/bin/owner-helper"), []byte("owner new content"), 0755)
				case "snapshot-old":
					write(t, filepath.Join(f.Root, "snapshots", f.Request.Snapshot, "web/index.html"), []byte("snapshot changed"), 0644)
				}
			}
			if err := prepareRecoveryMaterial(f.Request, c); err == nil {
				t.Fatal("changed inputs published")
			}
			if _, err := os.Lstat(materialPath(f)); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("material published")
			}
		})
	}
}
func TestMaterialLockAndMalformedAbsentPath(t *testing.T) {
	f := newMaterialFixture(t)
	if err := os.MkdirAll(filepath.Dir(materialBase(f.config())), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/absent", materialBase(f.config())); err != nil {
		t.Fatal(err)
	}
	if _, err := readMaterial(f.Request.Snapshot, f.config()); err == nil || errors.Is(err, ErrMaterialAbsent) {
		t.Fatal("unsafe path treated as absence")
	}
	if err := unix.Flock(int(f.lock.Fd()), unix.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	if _, err := readMaterial(f.Request.Snapshot, f.config()); err == nil || errors.Is(err, ErrMaterialAbsent) {
		t.Fatal("missing lock admitted")
	}
}

func TestMaterialOtherTokenCannotHideSealedSnapshot(t *testing.T) {
	f := newMaterialFixture(t)
	prepareMaterial(t, f)
	f.marker(t, "rollback")
	raw, _ := os.ReadFile(filepath.Join(f.Root, "transaction/active"))
	write(t, filepath.Join(f.Root, "transaction/active"), []byte(strings.ReplaceAll(string(raw), strings.Repeat("d", 64), strings.Repeat("f", 64))), 0600)
	if _, err := readMaterial(f.Request.Snapshot, f.config()); err == nil || errors.Is(err, ErrMaterialAbsent) {
		t.Fatalf("changed token downgraded material: %v", err)
	}
}
func TestMaterialUnrelatedHistoryCannotBlockNewPreparation(t *testing.T) {
	for _, kind := range []string{"unrelated-file", "malformed-record", "unsafe-sibling", "foreign-record", "many-records"} {
		t.Run(kind, func(t *testing.T) {
			f := newMaterialFixture(t)
			base := materialBase(f.config())
			if err := os.MkdirAll(base, 0700); err != nil {
				t.Fatal(err)
			}
			sibling := filepath.Join(base, strings.Repeat("a", 64))
			switch kind {
			case "unrelated-file":
				write(t, filepath.Join(base, "owner-private-history"), []byte("unknown retained bytes"), 0600)
			case "malformed-record":
				if err := os.Mkdir(sibling, 0700); err != nil {
					t.Fatal(err)
				}
				write(t, filepath.Join(sibling, "material.json"), []byte("broken old record"), 0600)
			case "unsafe-sibling":
				if err := os.Symlink("/unavailable", sibling); err != nil {
					t.Fatal(err)
				}
			case "foreign-record":
				if err := os.Mkdir(sibling, 0700); err != nil {
					t.Fatal(err)
				}
				write(t, filepath.Join(sibling, "material.json"), canonical(materialRecord{Schema: MaterialSchema, Snapshot: f.Request.Snapshot, TokenHash: strings.Repeat("f", 64)}), 0600)
			case "many-records":
				for i := 0; i < 1026; i++ {
					if err := os.Mkdir(filepath.Join(base, fmt.Sprintf("%064x", i)), 0700); err != nil {
						t.Fatal(err)
					}
				}
			}
			before := map[string]os.FileInfo{}
			entries, err := os.ReadDir(base)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				info, e := os.Lstat(filepath.Join(base, entry.Name()))
				if e != nil {
					t.Fatal(e)
				}
				before[entry.Name()] = info
			}
			m, err := readMaterial(f.Request.Snapshot, f.config())
			if m != nil {
				m.close()
			}
			if !errors.Is(err, ErrMaterialAbsent) {
				t.Fatalf("unrelated history blocked verified absence: %v", err)
			}
			prepareMaterial(t, f)
			for name, previous := range before {
				now, e := os.Lstat(filepath.Join(base, name))
				if e != nil || !os.SameFile(previous, now) || previous.Mode() != now.Mode() || previous.Size() != now.Size() || previous.ModTime() != now.ModTime() {
					t.Fatalf("history %s changed", name)
				}
			}
			applyMaterial(t, f, "bin")
			f.marker(t, "rollback")
			if err := os.Rename(f.Request.CandidateRoot, f.Request.CandidateRoot+".unavailable"); err != nil {
				t.Fatal(err)
			}
			if err := publish(f.Request, "rollback", f.config()); err != nil {
				t.Fatalf("unrelated history blocked rollback: %v", err)
			}
		})
	}
}
func TestMaterialSelectedPathAndPrivateBaseRemainRequired(t *testing.T) {
	for _, fault := range []string{"selected-file", "selected-symlink", "selected-empty-dir", "base-mode"} {
		t.Run(fault, func(t *testing.T) {
			f := newMaterialFixture(t)
			base := materialBase(f.config())
			if err := os.MkdirAll(base, 0700); err != nil {
				t.Fatal(err)
			}
			switch fault {
			case "selected-file":
				write(t, materialPath(f), []byte("unknown selected material"), 0600)
			case "selected-symlink":
				if err := os.Symlink("/unavailable", materialPath(f)); err != nil {
					t.Fatal(err)
				}
			case "selected-empty-dir":
				if err := os.Mkdir(materialPath(f), 0700); err != nil {
					t.Fatal(err)
				}
			case "base-mode":
				if err := os.Chmod(base, 0755); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := readMaterial(f.Request.Snapshot, f.config()); err == nil || errors.Is(err, ErrMaterialAbsent) {
				t.Fatalf("selected evidence downgraded: %v", err)
			}
			if err := prepareRecoveryMaterial(f.Request, f.config()); err == nil {
				t.Fatal("invalid selected evidence overwritten")
			}
		})
	}
}
