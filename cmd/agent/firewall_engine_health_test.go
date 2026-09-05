package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
)

// stageKernelModuleFixture points the kernel probe at a tree this test owns.
// release is what the machine reports as running; trees are the module trees
// that exist on disk.
// stageKernelModuleFixture, cekirdek yoklamasini bu testin sahip oldugu bir
// agaca yonlendirir.
func stageKernelModuleFixture(t *testing.T, release string, trees ...string) {
	t.Helper()
	root := t.TempDir()
	modules := filepath.Join(root, "modules")
	if err := os.MkdirAll(modules, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tree := range trees {
		dir := filepath.Join(modules, tree)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "modules.dep"), []byte("\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	releasePath := filepath.Join(root, "osrelease")
	if release != "" {
		if err := os.WriteFile(releasePath, []byte(release+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	previousRelease, previousRoot := firewallKernelReleasePath, firewallKernelModulesRoot
	firewallKernelReleasePath, firewallKernelModulesRoot = releasePath, modules
	t.Cleanup(func() {
		firewallKernelReleasePath, firewallKernelModulesRoot = previousRelease, previousRoot
	})
}

// stageIntactKernelFixture is a healthy machine: the running kernel still has
// its modules. Every test that must NOT be excused by R-054's reason uses it,
// so no assertion here accidentally rests on the host this suite runs on.
// stageIntactKernelFixture saglikli bir makinedir.
func stageIntactKernelFixture(t *testing.T) {
	t.Helper()
	stageKernelModuleFixture(t, "6.16.7-arch1-1", "6.16.7-arch1-1")
}

// stageReplacedKernelFixture is the machine R-054 was found on: an upgrade
// installed a new kernel and took the running kernel's modules with it.
// stageReplacedKernelFixture, R-054'un bulundugu makinedir.
func stageReplacedKernelFixture(t *testing.T) {
	t.Helper()
	stageKernelModuleFixture(t, "6.16.7-arch1-1", "6.17.1-arch1-1")
}

func TestRunningKernelModulesMissingAccusesOnlyAReplacedKernel(t *testing.T) {
	tests := []struct {
		name    string
		release string
		trees   []string
		want    bool
	}{
		{
			name:    "kernel replaced in place",
			release: "6.16.7-arch1-1",
			trees:   []string{"6.17.1-arch1-1"},
			want:    true,
		},
		{
			name:    "running kernel still has its modules",
			release: "6.16.7-arch1-1",
			trees:   []string{"6.16.7-arch1-1", "6.17.1-arch1-1"},
			want:    false,
		},
		{
			name:    "host keeps no module trees at all",
			release: "6.16.7-arch1-1",
			want:    false,
		},
		{
			name:  "running release cannot be read",
			trees: []string{"6.17.1-arch1-1"},
			want:  false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stageKernelModuleFixture(t, test.release, test.trees...)
			if got := runningKernelModulesMissing(); got != test.want {
				t.Fatalf("runningKernelModulesMissing() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestRunningKernelReleaseRefusesAPathItCouldFollow(t *testing.T) {
	stageKernelModuleFixture(t, "", "6.17.1-arch1-1")
	if err := os.WriteFile(firewallKernelReleasePath, []byte("../../etc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if release := runningKernelRelease(); release != "" {
		t.Fatalf("an escaping release string was accepted: %q", release)
	}
}

func TestClassifyFirewallEngineFaultNamesTheReplacedKernel(t *testing.T) {
	t.Run("structural proof wins", func(t *testing.T) {
		stageReplacedKernelFixture(t)
		fault := classifyFirewallEngineFault(nil, errors.New("exit status 1"))
		if fault != firewallEngineFaultModulesMissing {
			t.Fatalf("fault = %v, want modules missing", fault)
		}
		if !strings.Contains(firewallEngineFaultSentence(fault), "Restart this server") {
			t.Fatalf("the reason does not tell the operator to restart: %q",
				firewallEngineFaultSentence(fault))
		}
	})
	t.Run("nft own words when the host is otherwise healthy", func(t *testing.T) {
		stageIntactKernelFixture(t)
		fault := classifyFirewallEngineFault(
			[]byte("cache initialization failed: Invalid argument"),
			errors.New("exit status 1"),
		)
		if fault != firewallEngineFaultKernelUnreachable {
			t.Fatalf("fault = %v, want kernel unreachable", fault)
		}
	})
	t.Run("an ordinary refusal explains nothing", func(t *testing.T) {
		stageIntactKernelFixture(t)
		fault := classifyFirewallEngineFault(
			[]byte("Error: syntax error, unexpected string"),
			errors.New("exit status 1"),
		)
		if fault != firewallEngineFaultNone {
			t.Fatalf("fault = %v, want none", fault)
		}
	})
	t.Run("success is never a fault", func(t *testing.T) {
		stageReplacedKernelFixture(t)
		if fault := classifyFirewallEngineFault(nil, nil); fault != firewallEngineFaultNone {
			t.Fatalf("fault = %v, want none", fault)
		}
	})
}

func firewallEngineTestCommitment(
	t *testing.T,
	enabled, persist bool,
) mutationpayload.FirewallApplyCommitment {
	t.Helper()
	var tcp, udp []int
	if enabled {
		tcp, udp = []int{80, 443}, []int{53}
	}
	commitment, err := mutationpayload.CanonicalFirewallApply(enabled, persist, tcp, udp)
	if err != nil {
		t.Fatal(err)
	}
	return commitment
}

func firewallEngineTestJournal(
	t *testing.T,
	enabled, persist bool,
) *firewallApplyJournal {
	t.Helper()
	commitment := firewallEngineTestCommitment(t, enabled, persist)
	journal := &firewallApplyJournal{
		Version:   firewallApplyJournalVersion,
		RequestID: strings.Repeat("a", 32),
		Qualifier: commitment.Qualifier,
		Enabled:   commitment.Enabled,
		Persist:   commitment.Persist,
		TCPPorts:  commitment.TCPPorts,
		UDPPorts:  commitment.UDPPorts,
	}
	if enabled {
		journal.SSHPorts = []int{22}
	}
	if persist {
		journal.PriorRestoreUnit = firewallRestoreUnitDisabled
	}
	if err := validateFirewallApplyJournal(journal); err != nil {
		t.Fatal(err)
	}
	return journal
}

// TestConvergeFirewallApplyLeavesTheHostUntouchedWhenTheEngineCannotAnswer is
// R-054's first half at the commit boundary: a machine whose kernel cannot be
// reached is discovered before the snapshot or the boot unit is written, so
// the plan ends untouched and may be failed cleanly.
func TestConvergeFirewallApplyLeavesTheHostUntouchedWhenTheEngineCannotAnswer(t *testing.T) {
	stageReplacedKernelFixture(t)
	runner := &fakeFirewallCommandRunner{
		listErr: errors.New("cache initialization failed: Invalid argument"),
	}
	store := &fakeFirewallStateStore{}
	journal := firewallEngineTestJournal(t, true, true)
	outcome, err := convergeFirewallApplyPlan(context.Background(), journal, runner, store)
	if err == nil {
		t.Fatal("an unreachable firewall engine converged")
	}
	if outcome != firewallHostUntouched {
		t.Fatalf("outcome = %v, want untouched", outcome)
	}
	if !strings.Contains(err.Error(), "Restart this server") {
		t.Fatalf("failure does not name the restart: %v", err)
	}
	if store.saves != 0 || store.removes != 0 || len(runner.commands) != 0 {
		t.Fatalf(
			"an unreachable engine still changed the host: saves=%d removes=%d commands=%+v",
			store.saves, store.removes, runner.commands,
		)
	}
}

// TestConvergeFirewallApplyPutsBackPersistenceWhenTheKernelTookNoRule covers
// the host R-054 poisoned: table discovery worked, the plan wrote its durable
// half, and only then did the kernel refuse the ruleset. Everything written is
// put back and read back, so the plan may still end cleanly.
func TestConvergeFirewallApplyPutsBackPersistenceWhenTheKernelTookNoRule(t *testing.T) {
	stageReplacedKernelFixture(t)
	prior := encodeFirewallSnapshot([]int{8080}, nil, []int{22})
	runner := &fakeFirewallCommandRunner{
		applyErr: errors.New("exit status 1"),
	}
	store := &fakeFirewallStateStore{data: append([]byte(nil), prior...), exists: true}
	journal := firewallEngineTestJournal(t, true, true)
	journal.PriorSnapshotExists = true
	journal.PriorSnapshot = append([]byte(nil), prior...)
	if err := validateFirewallApplyJournal(journal); err != nil {
		t.Fatal(err)
	}
	outcome, err := convergeFirewallApplyPlan(context.Background(), journal, runner, store)
	if err == nil {
		t.Fatal("a kernel that took no rule reported convergence")
	}
	if outcome != firewallHostRestored {
		t.Fatalf("outcome = %v, want restored", outcome)
	}
	if !store.exists || !bytes.Equal(store.data, prior) {
		t.Fatalf("the earlier firewall policy was not put back: exists=%v data=%q",
			store.exists, store.data)
	}
	if runner.unitEnabled {
		t.Fatal("the boot restore unit was left armed for a plan that never applied")
	}
	if code, message, clean := firewallApplyCleanFailureText(outcome, err, false); !clean ||
		code != firewallApplyFailedRestoredCode ||
		!strings.Contains(message, "no rule reached the kernel") {
		t.Fatalf("clean failure text: clean=%v code=%q message=%q", clean, code, message)
	}
}

// TestConvergeFirewallApplyStaysAmbiguousOnAHealthyKernel keeps the
// fail-closed rule: an nft failure that this machine cannot explain may still
// have half applied a ruleset, so it holds the host exactly as it always did.
func TestConvergeFirewallApplyStaysAmbiguousOnAHealthyKernel(t *testing.T) {
	stageIntactKernelFixture(t)
	runner := &fakeFirewallCommandRunner{applyErr: errors.New("exit status 1")}
	store := &fakeFirewallStateStore{}
	journal := firewallEngineTestJournal(t, true, true)
	outcome, err := convergeFirewallApplyPlan(context.Background(), journal, runner, store)
	if err == nil {
		t.Fatal("a failed nft apply reported convergence")
	}
	if outcome != firewallHostAmbiguous {
		t.Fatalf("outcome = %v, want ambiguous", outcome)
	}
	if _, _, clean := firewallApplyCleanFailureText(outcome, err, false); clean {
		t.Fatal("an ambiguous host was offered a clean failure")
	}
}

// TestConvergeFirewallApplyStaysAmbiguousWithoutAProvableUnitState: the plan
// may only be called restored when it can prove where it put the boot unit
// back to. A journal from before that was recorded cannot, and fails closed.
func TestConvergeFirewallApplyStaysAmbiguousWithoutAProvableUnitState(t *testing.T) {
	stageReplacedKernelFixture(t)
	runner := &fakeFirewallCommandRunner{applyErr: errors.New("exit status 1")}
	store := &fakeFirewallStateStore{}
	journal := firewallEngineTestJournal(t, true, true)
	journal.PriorRestoreUnit = ""
	outcome, err := convergeFirewallApplyPlan(context.Background(), journal, runner, store)
	if err == nil {
		t.Fatal("an unprovable restoration reported convergence")
	}
	if outcome != firewallHostAmbiguous {
		t.Fatalf("outcome = %v, want ambiguous", outcome)
	}
}

// TestConvergeFirewallApplyStillConvergesAWorkingHost: the whole point of a
// terminal failure is that it is reached only when the plan cannot succeed. A
// plan that can still succeed is still applied.
func TestConvergeFirewallApplyStillConvergesAWorkingHost(t *testing.T) {
	stageReplacedKernelFixture(t)
	runner := &fakeFirewallCommandRunner{}
	store := &fakeFirewallStateStore{}
	journal := firewallEngineTestJournal(t, true, true)
	outcome, err := convergeFirewallApplyPlan(context.Background(), journal, runner, store)
	if err != nil || outcome != firewallHostConverged {
		t.Fatalf("a working host did not converge: outcome=%v err=%v", outcome, err)
	}
	if !runner.unitEnabled || !store.exists {
		t.Fatalf("convergence did not persist: unit=%v snapshot=%v",
			runner.unitEnabled, store.exists)
	}
}

// TestPrepareFirewallApplyRefusesAnUnloadableEngineBeforeAnyCommit is the
// cheapest half of R-054: on the ordinary path the refusal happens before the
// durable intent exists, so the host never becomes busy at all.
func TestPrepareFirewallApplyRefusesAnUnloadableEngineBeforeAnyCommit(t *testing.T) {
	stageReplacedKernelFixture(t)
	runner := &fakeFirewallCommandRunner{
		listErr: errors.New("cache initialization failed: Invalid argument"),
	}
	store := &fakeFirewallStateStore{}
	_, err := prepareFirewallApplyJournal(
		context.Background(),
		firewallEngineTestCommitment(t, true, true),
		runner,
		store,
	)
	if err == nil {
		t.Fatal("a host that cannot load nftables was allowed to commit a firewall plan")
	}
	if !strings.Contains(err.Error(), "Restart this server") {
		t.Fatalf("the refusal does not name the restart: %v", err)
	}
	if firewallEngineFaultOf(err) != firewallEngineFaultModulesMissing {
		t.Fatalf("the refusal lost its classification: %v", err)
	}
}

func TestPrepareFirewallApplyRecordsWhereItFoundTheRestoreUnit(t *testing.T) {
	stageIntactKernelFixture(t)
	for _, unitEnabled := range []bool{false, true} {
		runner := &fakeFirewallCommandRunner{unitEnabled: unitEnabled}
		store := &fakeFirewallStateStore{}
		journal, err := prepareFirewallApplyJournal(
			context.Background(),
			firewallEngineTestCommitment(t, true, true),
			runner,
			store,
		)
		if err != nil {
			t.Fatal(err)
		}
		want := firewallRestoreUnitDisabled
		if unitEnabled {
			want = firewallRestoreUnitEnabled
		}
		if journal.PriorRestoreUnit != want {
			t.Fatalf("PriorRestoreUnit = %q, want %q", journal.PriorRestoreUnit, want)
		}
	}
}

// TestFirewallEngineFailureKeepsTheInstructionAheadOfTheDiagnostic guards what
// the first live run found: nft answers a refused ruleset with several lines of
// carets and file positions, the recorded failure reason is bounded, and with
// the diagnostic first the operator read "...a package upgrade repl..." and
// never reached the sentence telling them to restart the server.
func TestFirewallEngineFailureKeepsTheInstructionAheadOfTheDiagnostic(t *testing.T) {
	stageReplacedKernelFixture(t)
	noisy := []byte(strings.Repeat(
		"/dev/stdin:5:5-12: Error: Could not process rule: No such file or directory\n"+
			"    ct state established,related accept\n    ^^^^^^^^\n", 6,
	))
	engineErr := newFirewallEngineError("nft apply failed", noisy, errors.New("exit status 1"))
	if strings.ContainsAny(engineErr.Error(), "\n\r\t") {
		t.Fatalf("the failure carries raw command layout: %q", engineErr.Error())
	}
	_, message, clean := firewallApplyCleanFailureText(firewallHostUntouched, engineErr, false)
	if !clean {
		t.Fatal("an untouched host was not offered a clean failure")
	}
	if !strings.Contains(message, "Restart this server, then turn the firewall on again.") {
		t.Fatalf("the bounded reason lost the operator's instruction: %q", message)
	}
}

func TestFirewallApplyJournalRejectsAnImpossibleRestoreUnitState(t *testing.T) {
	journal := firewallEngineTestJournal(t, true, true)
	journal.PriorRestoreUnit = "masked"
	if err := validateFirewallApplyJournal(journal); err == nil {
		t.Fatal("an unrestorable unit state was accepted")
	}
	journal = firewallEngineTestJournal(t, true, false)
	journal.PriorRestoreUnit = firewallRestoreUnitEnabled
	if err := validateFirewallApplyJournal(journal); err == nil {
		t.Fatal("a non-persisting plan recorded a unit state it never touches")
	}
}
