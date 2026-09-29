//go:build linux

package dnsenginerecovery

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/bindroot"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
)

type acceptingBINDRunner struct{}

func (acceptingBINDRunner) Run(context.Context, string, ...string) ([]byte, error) { return nil, nil }

// stagedGenerationFixture stages, on a temporary managed root, the exact
// generation a rolling-back standby journal names. Staging writes root-owned
// immutable files, so the test needs root.
func stagedGenerationFixture(t *testing.T) (dnsengineartifact.SwitchJournalV1, bindroot.Layout, bindGenerationFS, *binddns.Publisher) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("staging a root-owned immutable BIND generation requires root")
	}
	root := filepath.Join(t.TempDir(), "celikpanel")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	j := beforeDecisionBINDSwitchJournal(dnsengineartifact.SwitchPhaseRollingBack)
	j.MutationRequestID, j.MutationOwnerID, j.TargetEpoch = strings.Repeat("a", 32), strings.Repeat("b", 32), 2
	layout := bindroot.Layout(root)
	generation, err := binddns.RenderManifest(root, binddns.Manifest{EngineEpoch: j.TargetEpoch})
	if err != nil {
		t.Fatal(err)
	}
	j.TargetGeneration = generation.ID
	publisher, err := binddns.NewPublisher(root, binddns.OSFileSystem{}, acceptingBINDRunner{})
	if err != nil {
		t.Fatal(err)
	}
	if err := publisher.Stage(context.Background(), generation); err != nil {
		t.Fatal(err)
	}
	view := bindGenerationFS{
		root:      root,
		catalog:   func(context.Context) (any, error) { return "catalog", nil },
		publisher: func() (*binddns.Publisher, error) { return publisher, nil },
	}
	return j, layout, view, publisher
}

func TestStagedBINDGenerationIsRemovedOnlyWhenExact(t *testing.T) {
	j, layout, view, publisher := stagedGenerationFixture(t)
	ctx := context.Background()
	final := view.root + "/generations/" + j.TargetGeneration

	// Selected by the pointer: nothing may be removed.
	if err := publisher.Activate(j.TargetGeneration); err != nil {
		t.Fatal(err)
	}
	if state, _, err := classifyStagedBINDGeneration(ctx, j, layout, view); err != nil || state != BINDGenerationNone {
		t.Fatalf("selected generation classified %v %v", state, err)
	}
	if err := os.Remove(view.root + "/current"); err != nil {
		t.Fatal(err)
	}

	// A journal outside the standby class never removes.
	outside := j
	outside.TargetUnitsBefore = []dnsengineartifact.UnitSnapshot{
		{Name: "bind9.service", LoadState: "not-found", ActiveState: "inactive"},
		{Name: "named.service", LoadState: "loaded", ActiveState: "inactive", UnitFileState: "disabled"},
	}
	if removed, err := removeStagedBINDGeneration(ctx, outside, layout, view); err != nil || removed {
		t.Fatalf("existing-BIND journal removed a tree: %v %v", removed, err)
	}

	// An owner-added file makes the tree differ: it is kept.
	if err := os.Chmod(final, 0o755); err != nil {
		t.Fatal(err)
	}
	extra := final + "/owner-notes.txt"
	if err := os.WriteFile(extra, []byte("owner"), 0o444); err != nil {
		t.Fatal(err)
	}
	if state, reason, err := classifyStagedBINDGeneration(ctx, j, layout, view); err != nil || state != BINDGenerationRetained || reason == "" {
		t.Fatalf("owner-modified tree classified %v %q %v", state, reason, err)
	}
	if removed, err := removeStagedBINDGeneration(ctx, j, layout, view); err != nil || removed {
		t.Fatalf("owner-modified tree removed: %v %v", removed, err)
	}
	if _, err := os.Lstat(extra); err != nil {
		t.Fatal("owner file lost")
	}
	if err := os.Remove(extra); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(final, 0o555); err != nil {
		t.Fatal(err)
	}

	// The exact staged tree is removed.
	if state, _, err := classifyStagedBINDGeneration(ctx, j, layout, view); err != nil || state != BINDGenerationStaged {
		t.Fatalf("exact tree classified %v %v", state, err)
	}
	if removed, err := removeStagedBINDGeneration(ctx, j, layout, view); err != nil || !removed {
		t.Fatalf("exact tree not removed: %v %v", removed, err)
	}
	for _, path := range []string{final, BINDGenerationRemovalPath(bindroot.Layout(view.root), j.TargetGeneration)} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("%s left after removal: %v", path, err)
		}
	}
	if state, _, err := classifyStagedBINDGeneration(ctx, j, layout, view); err != nil || state != BINDGenerationNone {
		t.Fatalf("removed tree classified %v %v", state, err)
	}
}

// An interrupted removal leaves the reserved detached directory; the next run
// finishes it instead of reporting an owner change.
func TestStagedBINDGenerationRemovalResumesDetachedTree(t *testing.T) {
	j, layout, view, _ := stagedGenerationFixture(t)
	final := view.root + "/generations/" + j.TargetGeneration
	detached := view.root + "/.rollback-remove-" + j.TargetGeneration
	if err := os.Rename(final, detached); err != nil {
		t.Fatal(err)
	}
	if state, _, err := classifyStagedBINDGeneration(context.Background(), j, layout, view); err != nil || state != BINDGenerationStaged {
		t.Fatalf("detached tree classified %v %v", state, err)
	}
	if removed, err := removeStagedBINDGeneration(context.Background(), j, layout, view); err != nil || !removed {
		t.Fatalf("detached tree not removed: %v %v", removed, err)
	}
	if _, err := os.Lstat(detached); !os.IsNotExist(err) {
		t.Fatalf("detached tree left: %v", err)
	}
}

func TestBINDRuntimeFilesAreListedNotRemoved(t *testing.T) {
	dir := t.TempDir()
	for name, size := range map[string]int{"managed-keys.bind": 3, "managed-keys.bind.jnl": 2, "tmp-1K50Kr2S9C": 0, "tmp-nonempty": 4, "db.owner": 1} {
		if err := os.WriteFile(filepath.Join(dir, name), make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files, err := ListBINDRuntimeFiles(dir)
	if err != nil || len(files) != 3 {
		t.Fatalf("runtime files = %q %v", files, err)
	}
	for _, want := range []string{"managed-keys.bind (3 bytes", "managed-keys.bind.jnl (2 bytes", "tmp-1K50Kr2S9C (0 bytes"} {
		if !strings.Contains(strings.Join(files, "\n"), want) {
			t.Fatalf("missing %q in %q", want, files)
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 5 {
		t.Fatal("listing removed a file")
	}
	if BINDWorkingDirectory(bindroot.APT) != "/var/cache/bind" || BINDWorkingDirectory(bindroot.Pacman) != "/var/named" {
		t.Fatal("working directories changed")
	}
}

func TestBINDSwitchStandbyClassIncludesFrozenGuardMask(t *testing.T) {
	j := beforeDecisionBINDSwitchJournal(dnsengineartifact.SwitchPhaseRollingBack)
	j.TargetUnitsBefore = []dnsengineartifact.UnitSnapshot{
		{Name: "bind9.service", LoadState: "masked", ActiveState: "inactive", UnitFileState: "masked"},
		{Name: "named.service", LoadState: "masked", ActiveState: "inactive", UnitFileState: "masked"},
	}
	if !BINDSwitchNeverStartedTargetJournal(j) {
		t.Fatal("frozen guard mask outside the standby class")
	}
	j.Phase = dnsengineartifact.SwitchPhaseTargetStaged
	if !BINDSwitchNeverStartedBeforeDecisionJournal(j) {
		t.Fatal("frozen guard mask outside the before-decision class")
	}
	j.Phase = dnsengineartifact.SwitchPhaseRollingBack
	for _, mixed := range [][]dnsengineartifact.UnitSnapshot{
		{{Name: "bind9.service", LoadState: "not-found", ActiveState: "inactive"}, {Name: "named.service", LoadState: "masked", ActiveState: "inactive", UnitFileState: "masked"}},
		{{Name: "bind9.service", LoadState: "masked", ActiveState: "inactive", UnitFileState: "masked-runtime"}, {Name: "named.service", LoadState: "masked", ActiveState: "inactive", UnitFileState: "masked-runtime"}},
	} {
		j.TargetUnitsBefore = mixed
		if BINDSwitchNeverStartedTargetJournal(j) {
			t.Fatalf("non-standby preimage admitted: %+v", mixed)
		}
	}
}
