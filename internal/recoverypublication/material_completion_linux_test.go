//go:build linux

package recoverypublication

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func materialCompletionPhase(t *testing.T, f fixture, phase string) {
	t.Helper()
	active := filepath.Join(f.Root, "transaction/active")
	raw, err := os.ReadFile(active)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(active); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"completion.pending", "scheduler-restore.pending"} {
		if phase == "completion-scheduler" || phase == "completion" && name == "completion.pending" || phase == "scheduler" && name == "scheduler-restore.pending" {
			write(t, filepath.Join(f.Root, "transaction", name), raw, 0600)
		}
	}
}
func materialLegacyV1(t *testing.T, f fixture) {
	t.Helper()
	path := materialPath(f)
	raw, err := os.ReadFile(filepath.Join(path, "material.json"))
	var record materialRecord
	if err != nil || decodeExact(raw, &record) != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(path, "data/libexec/get.sh")); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(path, "data/libexec")); err != nil {
		t.Fatal(err)
	}
	record.Schema = MaterialSchema
	record.DataManifest = checksumManifest(t, filepath.Join(path, "data"))
	record.Data = readTree(t, f.Root, filepath.Join(path, "data"))
	write(t, filepath.Join(path, "material.json"), canonical(record), 0600)
}
func completeMaterialFixture(t *testing.T, phase string, legacy bool) fixture {
	f := newMaterialFixture(t)
	prepareMaterial(t, f)
	if legacy {
		materialLegacyV1(t, f)
	}
	applyMaterial(t, f, "bin")
	applyMaterial(t, f, "web")
	materialCompletionPhase(t, f, phase)
	return f
}
func TestCompletionMaterialWithoutEntireCandidate(t *testing.T) {
	for _, phase := range []string{"completion", "completion-scheduler", "scheduler"} {
		t.Run(phase, func(t *testing.T) {
			f := completeMaterialFixture(t, phase, false)
			if err := os.Rename(f.Request.CandidateRoot, f.Request.CandidateRoot+".preserved"); err != nil {
				t.Fatal(err)
			}
			c := f.config()
			c.candidates = f.config().candidates // Only lexical provenance is needed, not the path.
			c.stopped = func() error { t.Fatal("readonly proof requested stopped services"); return nil }
			before := readTree(t, f.Root, f.Root)
			for retry := 0; retry < 2; retry++ {
				got, err := verifyCompletionMaterial(f.Request.Snapshot, c)
				if err != nil || got != filepath.Join(materialPath(f), "data") {
					t.Fatal(got, err)
				}
				if err = verifyInstalledCompletion(f.Request.Snapshot, c); err != nil {
					t.Fatal(err)
				}
			}
			if !before.equal(readTree(t, f.Root, f.Root)) {
				t.Fatal("completion verification mutated evidence or product")
			}
		})
	}
}
func TestCompletionMaterialLegacyV1IsVerifiedAndNeverUpgraded(t *testing.T) {
	t.Run("read-only legacy completion", func(t *testing.T) {
		f := completeMaterialFixture(t, "completion", true)
		if err := os.Rename(f.Request.CandidateRoot, f.Request.CandidateRoot+".preserved"); err != nil {
			t.Fatal(err)
		}
		before := readTree(t, f.Root, f.Root)
		root, err := verifyCompletionMaterial(f.Request.Snapshot, f.config())
		if !errors.Is(err, ErrLegacyCompletionMaterial) || root != "" {
			t.Fatal(root, err)
		}
		if !before.equal(readTree(t, f.Root, f.Root)) {
			t.Fatal("legacy verification wrote data")
		}
		write(t, filepath.Join(f.Root, "prefix/bin/panel"), []byte("owner change"), 0755)
		if _, err = verifyCompletionMaterial(f.Request.Snapshot, f.config()); err == nil || errors.Is(err, ErrLegacyCompletionMaterial) || errors.Is(err, ErrMaterialAbsent) {
			t.Fatal("corrupt legacy proof allowed fallback", err)
		}
	})
	t.Run("no v1 rewrite", func(t *testing.T) {
		f := newMaterialFixture(t)
		prepareMaterial(t, f)
		materialLegacyV1(t, f)
		before := readTree(t, f.Root, f.Root)
		if err := prepareRecoveryMaterial(f.Request, f.config()); err == nil {
			t.Fatal("v1 rewritten")
		}
		if !before.equal(readTree(t, f.Root, f.Root)) {
			t.Fatal("rejected upgrade mutated evidence")
		}
	})
	t.Run("v1 rollback remains available", func(t *testing.T) {
		f := newMaterialFixture(t)
		prepareMaterial(t, f)
		materialLegacyV1(t, f)
		applyMaterial(t, f, "bin")
		applyMaterial(t, f, "web")
		if err := os.Rename(f.Request.CandidateRoot, f.Request.CandidateRoot+".preserved"); err != nil {
			t.Fatal(err)
		}
		f.marker(t, "rollback")
		for _, resource := range []string{"bin", "web"} {
			req := f.Request
			req.Resource = resource
			if err := publish(req, "rollback", f.config()); err != nil {
				t.Fatal(err)
			}
		}
	})
}
func TestCompletionMaterialClosedMarkerBoundary(t *testing.T) {
	for _, phase := range []string{"update-active", "rollback-active", "rollback-completion", "none", "quiesce", "mixed-active", "foreign-token", "foreign-snapshot", "mismatched-pair"} {
		t.Run(phase, func(t *testing.T) {
			f := newMaterialFixture(t)
			prepareMaterial(t, f)
			applyMaterial(t, f, "bin")
			applyMaterial(t, f, "web")
			active := filepath.Join(f.Root, "transaction/active")
			raw, _ := os.ReadFile(active)
			switch phase {
			case "rollback-active":
				f.marker(t, "rollback")
			case "rollback-completion":
				f.marker(t, "rollback")
				materialCompletionPhase(t, f, "completion")
			case "none":
				os.Remove(active)
			case "quiesce":
				os.Rename(active, filepath.Join(f.Root, "transaction/quiesce.pending"))
			case "mixed-active":
				write(t, filepath.Join(f.Root, "transaction/completion.pending"), raw, 0600)
			case "foreign-token", "foreign-snapshot", "mismatched-pair":
				materialCompletionPhase(t, f, "completion")
				changed := strings.ReplaceAll(string(raw), strings.Repeat("d", 64), strings.Repeat("f", 64))
				if phase == "foreign-snapshot" {
					changed = strings.ReplaceAll(string(raw), f.Request.Snapshot, strings.ReplaceAll(f.Request.Snapshot, strings.Repeat("b", 32), strings.Repeat("c", 32)))
				}
				name := "completion.pending"
				if phase == "mismatched-pair" {
					name = "scheduler-restore.pending"
				}
				write(t, filepath.Join(f.Root, "transaction", name), []byte(changed), 0600)
			}
			before := readTree(t, f.Root, f.Root)
			if _, err := verifyCompletionMaterial(f.Request.Snapshot, f.config()); err == nil || errors.Is(err, ErrMaterialAbsent) || errors.Is(err, ErrLegacyCompletionMaterial) {
				t.Fatal("unadmitted marker", err)
			}
			if !before.equal(readTree(t, f.Root, f.Root)) {
				t.Fatal("refusal mutated evidence")
			}
		})
	}
}
func TestCompletionMaterialCorruptionRefusesWithoutMutation(t *testing.T) {
	for _, kind := range []string{"material-missing", "material-invalid", "material-updater", "snapshot-bytes", "bin-receipt-missing", "web-receipt-invalid", "bin-intent-missing", "intent-material-hash", "retired-missing", "current-bytes", "same-bytes-new-inode", "current-xattr", "retired-xattr", "foreign-journal", "published-before-exchange"} {
		t.Run(kind, func(t *testing.T) {
			f := completeMaterialFixture(t, "completion", false)
			journal := f.journal()
			record := readIntent(t, f)
			switch kind {
			case "material-missing":
				os.Rename(materialPath(f), materialPath(f)+".preserved")
			case "material-invalid":
				write(t, filepath.Join(materialPath(f), "material.json"), []byte("{}\n"), 0600)
			case "material-updater":
				write(t, filepath.Join(materialPath(f), "data/libexec/get.sh"), []byte("altered"), 0600)
			case "snapshot-bytes":
				write(t, filepath.Join(f.Root, "snapshots", f.Request.Snapshot, "bin/agent"), []byte("altered"), 0755)
			case "bin-receipt-missing":
				os.Remove(filepath.Join(journal, "published"))
			case "web-receipt-invalid":
				write(t, filepath.Join(filepath.Dir(journal), "update-web/published"), []byte("invalid\n"), 0600)
			case "bin-intent-missing":
				os.Remove(filepath.Join(journal, "intent.json"))
			case "intent-material-hash":
				record.MaterialSHA = strings.Repeat("f", 64)
				write(t, filepath.Join(journal, "intent.json"), canonical(record), 0600)
			case "retired-missing":
				os.Rename(filepath.Join(journal, record.Stage), filepath.Join(journal, ".stage-"+strings.Repeat("f", 32)))
			case "current-bytes":
				write(t, filepath.Join(f.Root, "prefix/bin/agent"), []byte("owner change"), 0755)
			case "same-bytes-new-inode":
				path := filepath.Join(f.Root, "prefix/bin/agent")
				raw, _ := os.ReadFile(path)
				os.Rename(path, path+".preserved")
				write(t, path, raw, 0755)
			case "current-xattr":
				if err := unix.Setxattr(filepath.Join(f.Root, "prefix/bin/agent"), "user.owner", []byte("owner change"), 0); err != nil {
					t.Fatal(err)
				}
			case "retired-xattr":
				if err := unix.Setxattr(filepath.Join(journal, record.Stage, "agent"), "user.owner", []byte("owner change"), 0); err != nil {
					t.Fatal(err)
				}
			case "foreign-journal":
				if err := os.Mkdir(filepath.Join(filepath.Dir(journal), "rollback-bin"), 0700); err != nil {
					t.Fatal(err)
				}
			case "published-before-exchange":
				if err := unix.Renameat2(unix.AT_FDCWD, filepath.Join(journal, record.Stage), unix.AT_FDCWD, filepath.Join(f.Root, "prefix/bin"), unix.RENAME_EXCHANGE); err != nil {
					t.Fatal(err)
				}
			}
			before := readTree(t, f.Root, f.Root)
			if _, err := verifyCompletionMaterial(f.Request.Snapshot, f.config()); err == nil || errors.Is(err, ErrMaterialAbsent) || errors.Is(err, ErrLegacyCompletionMaterial) {
				t.Fatal("corrupt proof allowed fallback", err)
			}
			if !before.equal(readTree(t, f.Root, f.Root)) {
				t.Fatal("refusal mutated owner state")
			}
		})
	}
}
func TestCompletionMaterialTrueAbsenceAndNoMutationAdmission(t *testing.T) {
	f := newMaterialFixture(t)
	materialCompletionPhase(t, f, "completion")
	before := readTree(t, f.Root, f.Root)
	if _, err := verifyCompletionMaterial(f.Request.Snapshot, f.config()); !errors.Is(err, ErrMaterialAbsent) {
		t.Fatal(err)
	}
	for _, op := range []string{"update", "rollback"} {
		if err := publish(f.Request, op, f.config()); err == nil {
			t.Fatal("late publication admitted")
		}
	}
	if err := prepareRecoveryMaterial(f.Request, f.config()); err == nil {
		t.Fatal("late capture admitted")
	}
	if !before.equal(readTree(t, f.Root, f.Root)) {
		t.Fatal("absence proof or rejected mutation wrote data")
	}
}
func TestCompletionMaterialFinalProofCatchesConcurrentOwnerChange(t *testing.T) {
	for _, kind := range []string{"current", "material", "marker", "journal", "journal-child", "journal-sibling"} {
		t.Run(kind, func(t *testing.T) {
			f := completeMaterialFixture(t, "completion", false)
			c := f.config()
			changed := false
			c.checkpoint = func(point string) {
				if point != "completion_resources_read" || changed {
					return
				}
				changed = true
				switch kind {
				case "current":
					write(t, filepath.Join(f.Root, "prefix/bin/agent"), []byte("owner change"), 0755)
				case "material":
					write(t, filepath.Join(materialPath(f), "data/libexec/get.sh"), []byte("owner change"), 0600)
				case "marker":
					os.Rename(filepath.Join(f.Root, "transaction/completion.pending"), filepath.Join(f.Root, "transaction/scheduler-restore.pending"))
				case "journal":
					write(t, filepath.Join(f.journal(), "published"), []byte("owner change"), 0600)
				case "journal-child":
					write(t, filepath.Join(f.journal(), "unknown"), []byte("owner evidence"), 0600)
				case "journal-sibling":
					if err := os.Mkdir(filepath.Join(filepath.Dir(f.journal()), "rollback-bin"), 0700); err != nil {
						t.Fatal(err)
					}
				}
			}
			if _, err := verifyCompletionMaterial(f.Request.Snapshot, c); err == nil || !changed {
				t.Fatal("changed final boundary accepted", err, changed)
			}
		})
	}
}

func noopMaterialFixture(t *testing.T, resources ...string) fixture {
	f := newMaterialFixture(t)
	for _, resource := range resources {
		source := filepath.Join(f.Root, "snapshots", f.Request.Snapshot, resource)
		if resource == "bin" {
			for _, name := range []string{"agent", "panel"} {
				raw, err := os.ReadFile(filepath.Join(source, name))
				if err != nil {
					t.Fatal(err)
				}
				write(t, filepath.Join(f.Request.CandidateRoot, "bin", name), raw, 0755)
			}
		} else {
			path := filepath.Join(f.Request.CandidateRoot, "web/dist")
			if err := os.RemoveAll(path); err != nil {
				t.Fatal(err)
			}
			copyFixtureTree(t, source, path)
		}
	}
	f.Request.CandidateManifest = checksumManifest(t, f.Request.CandidateRoot)
	return f
}
func TestCompletionMaterialNoopKeepsProductIdentity(t *testing.T) {
	for _, mode := range []string{"bin", "web", "both"} {
		t.Run(mode, func(t *testing.T) {
			resources := []string{mode}
			if mode == "both" {
				resources = []string{"bin", "web"}
			}
			f := noopMaterialFixture(t, resources...)
			prepareMaterial(t, f)
			before := map[string]tree{}
			for _, resource := range resources {
				before[resource] = readTree(t, f.Root, filepath.Join(f.Root, "prefix", resource))
			}
			applyMaterial(t, f, "bin")
			applyMaterial(t, f, "web")
			for _, resource := range resources {
				actual := readTree(t, f.Root, filepath.Join(f.Root, "prefix", resource))
				if !actual.equal(before[resource]) {
					t.Fatal("no-op changed installed identity", resource)
				}
				tf := f
				tf.Request.Resource = resource
				record := readIntent(t, tf)
				if record.Schema != MaterialNoopIntentSchema || record.Stage != "" || !record.Before.equal(record.After) {
					t.Fatal("no exact no-op authority", record.Schema, record.Stage)
				}
			}
			materialCompletionPhase(t, f, "completion")
			if err := os.Rename(f.Request.CandidateRoot, f.Request.CandidateRoot+".preserved"); err != nil {
				t.Fatal(err)
			}
			if err := verifyInstalledCompletion(f.Request.Snapshot, f.config()); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestCompletionMaterialNoopSIGKILLAndRetry(t *testing.T) {
	for _, point := range []string{"intent_durable", "receipt_staged", "receipt_published", "noop_verified"} {
		t.Run(point, func(t *testing.T) {
			f := noopMaterialFixture(t, "bin")
			prepareMaterial(t, f)
			before := readTree(t, f.Root, filepath.Join(f.Root, "prefix/bin"))
			crash(t, f, point)
			if !before.equal(readTree(t, f.Root, filepath.Join(f.Root, "prefix/bin"))) {
				t.Fatal("crashed no-op changed product")
			}
			applyMaterial(t, f, "bin")
			applyMaterial(t, f, "web")
			materialCompletionPhase(t, f, "completion")
			if err := os.Rename(f.Request.CandidateRoot, f.Request.CandidateRoot+".preserved"); err != nil {
				t.Fatal(err)
			}
			if err := verifyInstalledCompletion(f.Request.Snapshot, f.config()); err != nil {
				t.Fatal(err)
			}
			if !before.equal(readTree(t, f.Root, filepath.Join(f.Root, "prefix/bin"))) {
				t.Fatal("no-op retry changed product")
			}
		})
	}
	t.Run("rollback after no-op intent only", func(t *testing.T) {
		f := noopMaterialFixture(t, "bin")
		prepareMaterial(t, f)
		before := readTree(t, f.Root, filepath.Join(f.Root, "prefix/bin"))
		crash(t, f, "intent_durable")
		if err := os.Rename(f.Request.CandidateRoot, f.Request.CandidateRoot+".preserved"); err != nil {
			t.Fatal(err)
		}
		f.marker(t, "rollback")
		if err := publish(f.Request, "rollback", f.config()); err != nil {
			t.Fatal(err)
		}
		if !before.equal(readTree(t, f.Root, filepath.Join(f.Root, "prefix/bin"))) {
			t.Fatal("no-op rollback changed product")
		}
	})
}
func TestCompletionMaterialLegacyNoopOnlySelectsLegacy(t *testing.T) {
	f := noopMaterialFixture(t, "bin", "web")
	prepareMaterial(t, f)
	materialLegacyV1(t, f)
	before := readTree(t, f.Root, filepath.Join(f.Root, "prefix/bin"))
	applyMaterial(t, f, "bin")
	applyMaterial(t, f, "web")
	for _, resource := range []string{"bin", "web"} {
		tf := f
		tf.Request.Resource = resource
		if _, err := os.Lstat(filepath.Join(tf.journal(), "intent.json")); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("legacy no-op gained new authority", err)
		}
	}
	if !before.equal(readTree(t, f.Root, filepath.Join(f.Root, "prefix/bin"))) {
		t.Fatal("legacy noop changed identity")
	}
	materialCompletionPhase(t, f, "completion")
	if _, err := verifyCompletionMaterial(f.Request.Snapshot, f.config()); !errors.Is(err, ErrLegacyCompletionMaterial) {
		t.Fatal(err)
	}
	write(t, filepath.Join(f.journal(), "published"), []byte("orphan"), 0600)
	if _, err := verifyCompletionMaterial(f.Request.Snapshot, f.config()); err == nil || errors.Is(err, ErrLegacyCompletionMaterial) {
		t.Fatal("corrupt old no-op allowed fallback", err)
	}
}
func TestCompletionMaterialNoopCannotAdoptOwnerChange(t *testing.T) {
	for _, kind := range []string{"inode", "xattr", "receipt-missing"} {
		t.Run(kind, func(t *testing.T) {
			f := noopMaterialFixture(t, "bin")
			prepareMaterial(t, f)
			applyMaterial(t, f, "bin")
			applyMaterial(t, f, "web")
			materialCompletionPhase(t, f, "completion")
			path := filepath.Join(f.Root, "prefix/bin/agent")
			switch kind {
			case "inode":
				raw, _ := os.ReadFile(path)
				os.Rename(path, path+".owner")
				write(t, path, raw, 0755)
			case "xattr":
				if err := unix.Setxattr(path, "user.owner", []byte("change"), 0); err != nil {
					t.Fatal(err)
				}
			case "receipt-missing":
				os.Remove(filepath.Join(f.journal(), "published"))
			}
			before := readTree(t, f.Root, f.Root)
			if err := verifyInstalledCompletion(f.Request.Snapshot, f.config()); err == nil {
				t.Fatal("unproved no-op completion")
			}
			if !before.equal(readTree(t, f.Root, f.Root)) {
				t.Fatal("no-op reader mutated evidence")
			}
		})
	}
}
func TestCompletionMaterialAbsenceRechecksFinalBoundary(t *testing.T) {
	for _, kind := range []string{"marker", "lock", "material"} {
		t.Run(kind, func(t *testing.T) {
			f := newMaterialFixture(t)
			materialCompletionPhase(t, f, "completion")
			c := f.config()
			changed := false
			c.checkpoint = func(point string) {
				if point != "material_absence_read" || changed {
					return
				}
				changed = true
				switch kind {
				case "marker":
					os.Rename(filepath.Join(f.Root, "transaction/completion.pending"), filepath.Join(f.Root, "transaction/scheduler-restore.pending"))
				case "lock":
					if err := unix.Flock(int(f.lock.Fd()), unix.LOCK_UN); err != nil {
						t.Fatal(err)
					}
				case "material":
					if err := os.MkdirAll(materialPath(f), 0700); err != nil {
						t.Fatal(err)
					}
				}
			}
			if _, err := verifyCompletionMaterial(f.Request.Snapshot, c); !changed || err == nil || errors.Is(err, ErrMaterialAbsent) {
				t.Fatal("stale absence proof", changed, err)
			}
		})
	}
}
func TestCompletionMaterialReadonlySIGKILLAndRetry(t *testing.T) {
	f := completeMaterialFixture(t, "completion", false)
	if err := os.Rename(f.Request.CandidateRoot, f.Request.CandidateRoot+".preserved"); err != nil {
		t.Fatal(err)
	}
	product := readTree(t, f.Root, filepath.Join(f.Root, "prefix"))
	material := readTree(t, f.Root, materialPath(f))
	f.Operation = "verify-completion"
	crash(t, f, "completion_resources_read")
	if err := verifyInstalledCompletion(f.Request.Snapshot, f.config()); err != nil {
		t.Fatal(err)
	}
	if !product.equal(readTree(t, f.Root, filepath.Join(f.Root, "prefix"))) || !material.equal(readTree(t, f.Root, materialPath(f))) {
		t.Fatal("interrupted read changed durable state")
	}
}

func TestMaterialNoopPublicationFinalBoundaryRefusesOwnerChange(t *testing.T) {
	for _, point := range []string{"receipt_staged", "receipt_published", "noop_verified"} {
		for _, kind := range []string{"journal-object", "current-bytes"} {
			t.Run(point+"/"+kind, func(t *testing.T) {
				f := noopMaterialFixture(t, "bin")
				prepareMaterial(t, f)
				c := f.config()
				changed := false
				c.checkpoint = func(name string) {
					if name != point || changed {
						return
					}
					changed = true
					if kind == "journal-object" {
						write(t, filepath.Join(f.journal(), "owner-evidence"), []byte("preserve"), 0600)
					} else {
						write(t, filepath.Join(f.Root, "prefix/bin/agent"), []byte("owner edit"), 0755)
					}
				}
				if err := publish(f.Request, "update", c); err == nil || !changed {
					t.Fatal("no-op publication accepted owner change", err, changed)
				}
				if kind == "journal-object" {
					if raw, err := os.ReadFile(filepath.Join(f.journal(), "owner-evidence")); err != nil || string(raw) != "preserve" {
						t.Fatal("owner evidence lost")
					}
				} else {
					if raw, err := os.ReadFile(filepath.Join(f.Root, "prefix/bin/agent")); err != nil || string(raw) != "owner edit" {
						t.Fatal("owner product edit lost")
					}
				}
			})
		}
	}
}
