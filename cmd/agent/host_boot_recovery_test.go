//go:build linux

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// R-048 was found on a real machine that lost power five times out of five: the
// agent came back before systemd had finished starting, the host-profile probe
// refused, the startup recovery never ran, and - because it was a one-shot -
// never ran again. The interrupted request kept the ledger and every host
// mutation on that machine was refused until a person restarted the agent.
//
// These tests hold the two rules that followed. A host that is still starting
// is waited out and then decided; a host that can never be read fails the plan
// cleanly and gives the ledger back with its reason; the reason survives the
// next boot; and a plan the machine could still finish is never abandoned.
//
// R-048, elektrigi bes kez kesilen gercek bir makinede bulundu. Bu testler
// ardindan gelen iki kurali tutar.

// scriptedHostRecoveryProbe answers the host-readiness probe from a script, so
// a boot that is still in progress can be reproduced without a systemd.
// scriptedHostRecoveryProbe, yoklamayi bir betikten yanitlar.
type scriptedHostRecoveryProbe struct {
	mu      sync.Mutex
	answers []hostRecoveryReadiness
	rest    hostRecoveryReadiness
	calls   int
}

func (p *scriptedHostRecoveryProbe) next() (hostRecoveryReadiness, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	readiness := p.rest
	if len(p.answers) > 0 {
		readiness = p.answers[0]
		p.answers = p.answers[1:]
	}
	switch readiness {
	case hostRecoveryNotYet:
		return readiness, errors.New(`systemd is not ready: systemd state "starting" exited with status 1`)
	case hostRecoveryNever:
		return readiness, errors.New(`unsupported package manager ""`)
	default:
		return hostRecoveryDecideNow, nil
	}
}

func (p *scriptedHostRecoveryProbe) hold(readiness hostRecoveryReadiness) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.answers = nil
	p.rest = readiness
}

func (p *scriptedHostRecoveryProbe) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func useScriptedHostRecoveryProbe(
	t *testing.T,
	answers []hostRecoveryReadiness,
	rest hostRecoveryReadiness,
) *scriptedHostRecoveryProbe {
	t.Helper()
	probe := &scriptedHostRecoveryProbe{answers: answers, rest: rest}
	previous := hostRecoveryProbe
	hostRecoveryProbe = probe.next
	t.Cleanup(func() { hostRecoveryProbe = previous })
	return probe
}

// useFastHostBootRecoverySchedule keeps the bounded window a bounded window and
// only shortens it, so the schedule under test is the production one.
func useFastHostBootRecoverySchedule(t *testing.T, window, interval time.Duration) {
	t.Helper()
	previousWindow, previousInterval := hostBootRecoveryWindow, hostBootRecoveryInterval
	hostBootRecoveryWindow, hostBootRecoveryInterval = window, interval
	t.Cleanup(func() {
		hostBootRecoveryWindow, hostBootRecoveryInterval = previousWindow, previousInterval
	})
}

// stageInterruptedDNSEngineSwitchAfterPowerLoss leaves exactly what a power cut
// leaves: a committed DNS engine switch journal on disk, its request active in
// the ledger, and no worker process alive.
// stageInterruptedDNSEngineSwitchAfterPowerLoss, bir elektrik kesintisinin
// biraktigi seyi birakir.
func stageInterruptedDNSEngineSwitchAfterPowerLoss(
	t *testing.T,
) (string, SwitchDNSEngineV1Request) {
	t.Helper()
	request := canonicalSwitchRequest(t)
	manager, root := newMutationTestManager(t)
	beginMutationTestJobWithIdentity(
		t, manager, "dns_engine_switch", "bind", request.ManifestQualifier,
	)
	persistActiveCommittedBINDStartupJournal(t, manager, root, request)
	abandonFirewallApplyTestRuntime(t, manager)
	return root, request
}

func reloadHostBootRecoveryManager(
	t *testing.T,
	root string,
) (*serviceMutationManager, error) {
	t.Helper()
	return newServiceMutationManager(
		filepath.Join(root, "state"), filepath.Join(root, "service-mutation.lock"),
	)
}

func awaitHostBootRecoveryWaiter(t *testing.T, manager *serviceMutationManager) {
	t.Helper()
	manager.mu.Lock()
	done := manager.hostBootWait
	manager.mu.Unlock()
	if done == nil {
		t.Fatal("no bounded wait was armed for a host that had not finished starting")
	}
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("the bounded host-boot wait never finished")
	}
}

func hostBootRecoveryLedgerJob(
	t *testing.T,
	manager *serviceMutationManager,
	requestID string,
) (*ServiceMutationJob, string, error) {
	t.Helper()
	manager.mu.Lock()
	defer manager.mu.Unlock()
	return cloneServiceMutationJob(manager.ledger.Jobs[requestID]),
		manager.ledger.ActiveRequestID,
		manager.poisoned
}

// TestStartupRecoveryWaitsOutAHostThatIsStillStartingThenDecides is the defect
// itself: five power cuts out of five reached this state, and the recovery
// refused to run because systemd was still `starting`.
func TestStartupRecoveryWaitsOutAHostThatIsStillStartingThenDecides(t *testing.T) {
	root, request := stageInterruptedDNSEngineSwitchAfterPowerLoss(t)
	backend := &fakeDNSEngineBackend{
		recovery:     dnsEngineSwitchRecoveryCommitted,
		finalizeHook: removeDNSEngineSwitchJournal,
	}
	useFakeDNSEngineBackend(t, backend)
	useFastHostBootRecoverySchedule(t, 20*time.Second, 5*time.Millisecond)
	probe := useScriptedHostRecoveryProbe(
		t,
		[]hostRecoveryReadiness{
			hostRecoveryNotYet, hostRecoveryNotYet, hostRecoveryNotYet,
		},
		hostRecoveryDecideNow,
	)

	reloaded, err := reloadHostBootRecoveryManager(t, root)
	if err != nil {
		t.Fatalf("a host that had not finished starting refused to bring the ledger up: %v", err)
	}
	// The agent must be serving while it waits: construction returns before the
	// recovery has decided anything.
	if backend.recoverCalls != 0 {
		t.Fatalf(
			"the recovery ran against a host that had not finished starting: recover=%d",
			backend.recoverCalls,
		)
	}
	awaitHostBootRecoveryWaiter(t, reloaded)

	job, active, poisoned := hostBootRecoveryLedgerJob(t, reloaded, request.MutationRequestID)
	wantPhase := dnsEngineSwitchFinalizedPhasePrefix +
		testMutationRequestID + "/" + request.ManifestQualifier
	if poisoned != nil || active != "" || job == nil ||
		job.Status != serviceMutationStatusSucceeded || job.Phase != wantPhase {
		t.Fatalf(
			"after the host finished starting: poisoned=%v active=%q job=%+v want phase %q",
			poisoned, active, job, wantPhase,
		)
	}
	if backend.recoverCalls != 1 || backend.finalizeCalls != 1 {
		t.Fatalf(
			"the interrupted switch was not decided exactly once: recover=%d finalize=%d",
			backend.recoverCalls, backend.finalizeCalls,
		)
	}
	if probe.callCount() < 4 {
		t.Fatalf("the probe was not retried on a schedule: calls=%d", probe.callCount())
	}
}

// TestStartupRecoveryNeverAbandonsAPlanTheHostCanStillFinish holds the rule the
// wait exists to protect: while the machine is only slow to start, the
// interrupted mutation keeps its lease and its journal, and it is finished
// rather than failed once the machine is readable.
func TestStartupRecoveryNeverAbandonsAPlanTheHostCanStillFinish(t *testing.T) {
	root, request := stageInterruptedDNSEngineSwitchAfterPowerLoss(t)
	backend := &fakeDNSEngineBackend{
		recovery:     dnsEngineSwitchRecoveryCommitted,
		finalizeHook: removeDNSEngineSwitchJournal,
	}
	useFakeDNSEngineBackend(t, backend)
	useFastHostBootRecoverySchedule(t, 20*time.Second, 5*time.Millisecond)
	probe := useScriptedHostRecoveryProbe(t, nil, hostRecoveryNotYet)

	reloaded, err := reloadHostBootRecoveryManager(t, root)
	if err != nil {
		t.Fatal(err)
	}
	// While the host is only "not yet", nothing is decided and nothing is
	// released: the request keeps the ledger it holds.
	deadline := time.Now().Add(2 * time.Second)
	for probe.callCount() < 5 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	job, active, poisoned := hostBootRecoveryLedgerJob(t, reloaded, request.MutationRequestID)
	if poisoned != nil || active != request.MutationRequestID || job == nil ||
		job.Status != serviceMutationStatusRunning || job.ErrorCode != "" {
		t.Fatalf(
			"a plan the host could still finish was disturbed while waiting: poisoned=%v active=%q job=%+v",
			poisoned, active, job,
		)
	}
	if _, exists, journalErr := readDNSEngineSwitchJournalAt(
		filepath.Join(root, "state", dnsEngineSwitchJournalFile),
	); journalErr != nil || !exists {
		t.Fatalf("the interrupted plan's journal was removed while waiting: exists=%v err=%v", exists, journalErr)
	}

	probe.hold(hostRecoveryDecideNow)
	awaitHostBootRecoveryWaiter(t, reloaded)

	job, active, poisoned = hostBootRecoveryLedgerJob(t, reloaded, request.MutationRequestID)
	if poisoned != nil || active != "" || job == nil ||
		job.Status != serviceMutationStatusSucceeded {
		t.Fatalf(
			"the plan was not completed once the host became readable: poisoned=%v active=%q job=%+v",
			poisoned, active, job,
		)
	}
	if backend.finalizeCalls != 1 {
		t.Fatalf("finalize calls=%d want exactly one", backend.finalizeCalls)
	}
}

// requireReleasedUndecidedHost proves the whole host is mutable again after an
// undecidable boot: nothing is poisoned, no request holds the ledger, and an
// unrelated mutation can take the lease. That last one is the property R-048
// lost - one interrupted DNS switch refused DNS, the firewall, sites and
// updates alike.
// requireReleasedUndecidedHost, makinenin yeniden degistirilebilir oldugunu
// kanitlar.
func requireReleasedUndecidedHost(
	t *testing.T,
	manager *serviceMutationManager,
	job *ServiceMutationJob,
	wantCode string,
) {
	t.Helper()
	if job == nil || job.Status != serviceMutationStatusFailed ||
		job.Phase != "interrupted" || job.ErrorCode != wantCode {
		t.Fatalf("released job=%+v want failed/interrupted with code %q", job, wantCode)
	}
	if !strings.Contains(job.ErrorMessage, hostRecoveryResidueSentence) {
		t.Fatalf("the release did not say what it left behind: %q", job.ErrorMessage)
	}
	unrelated, err := manager.begin(&ServiceMutationBeginRequest{
		RequestID: strings.Repeat("b", 32),
		OwnerID:   strings.Repeat("c", 32),
		Kind:      "package_install",
		Target:    "nginx",
	})
	if err != nil || unrelated == nil {
		t.Fatalf(
			"the host still refused an unrelated mutation after the release: job=%+v err=%v",
			unrelated, err,
		)
	}
	abandonFirewallApplyTestRuntime(t, manager)
}

// TestStartupRecoveryReleasesTheLedgerWhenTheHostCanNeverBeRead is the clean
// failure: a host the detector refuses durably is not waited for, the plan is
// failed with its reason, and the ledger is given back.
func TestStartupRecoveryReleasesTheLedgerWhenTheHostCanNeverBeRead(t *testing.T) {
	root, request := stageInterruptedDNSEngineSwitchAfterPowerLoss(t)
	backend := &fakeDNSEngineBackend{recovery: dnsEngineSwitchRecoveryCommitted}
	useFakeDNSEngineBackend(t, backend)
	useScriptedHostRecoveryProbe(t, nil, hostRecoveryNever)

	reloaded, err := reloadHostBootRecoveryManager(t, root)
	if err != nil {
		t.Fatalf("a durably unreadable host left the ledger unusable: %v", err)
	}
	job, active, poisoned := hostBootRecoveryLedgerJob(t, reloaded, request.MutationRequestID)
	if poisoned != nil {
		t.Fatalf("a durably unreadable host poisoned the manager: %v", poisoned)
	}
	if active != "" {
		t.Fatalf("the ledger was still held after an undecidable boot: %q", active)
	}
	if backend.recoverCalls != 0 {
		t.Fatalf(
			"the recovery ran against a host it could not read: recover=%d",
			backend.recoverCalls,
		)
	}
	// Nothing was discarded: the mutation's own journal is still on the host.
	if _, exists, journalErr := readDNSEngineSwitchJournalAt(
		filepath.Join(root, "state", dnsEngineSwitchJournalFile),
	); journalErr != nil || !exists {
		t.Fatalf(
			"the release treated a half-applied host as clean: journal exists=%v err=%v",
			exists, journalErr,
		)
	}
	requireReleasedUndecidedHost(
		t, reloaded, job, hostRecoveryReleasedUnsupportedCode,
	)
}

// TestStartupRecoveryReleasesTheLedgerWhenTheWindowCloses is the second half of
// the same rule: a host that never finishes starting must not end a boot with a
// lease no one is left to release.
func TestStartupRecoveryReleasesTheLedgerWhenTheWindowCloses(t *testing.T) {
	root, request := stageInterruptedDNSEngineSwitchAfterPowerLoss(t)
	backend := &fakeDNSEngineBackend{recovery: dnsEngineSwitchRecoveryCommitted}
	useFakeDNSEngineBackend(t, backend)
	useFastHostBootRecoverySchedule(t, 50*time.Millisecond, 5*time.Millisecond)
	useScriptedHostRecoveryProbe(t, nil, hostRecoveryNotYet)

	reloaded, err := reloadHostBootRecoveryManager(t, root)
	if err != nil {
		t.Fatal(err)
	}
	awaitHostBootRecoveryWaiter(t, reloaded)

	job, active, poisoned := hostBootRecoveryLedgerJob(t, reloaded, request.MutationRequestID)
	if poisoned != nil || active != "" {
		t.Fatalf(
			"the boot ended still holding the lease: poisoned=%v active=%q", poisoned, active,
		)
	}
	if backend.recoverCalls != 0 {
		t.Fatalf("recover=%d want no host decision at all", backend.recoverCalls)
	}
	requireReleasedUndecidedHost(t, reloaded, job, hostRecoveryReleasedWindowCode)
}

// TestReleasedUndecidedReasonSurvivesTheNextBoot proves the release did not
// lose anything. The next agent start reads the same reason off the disk, does
// not poison on the journal the release deliberately left behind, and - once
// the host can be read - finishes that journal on the host rather than leaving
// the machine half-changed.
func TestReleasedUndecidedReasonSurvivesTheNextBoot(t *testing.T) {
	root, request := stageInterruptedDNSEngineSwitchAfterPowerLoss(t)
	backend := &fakeDNSEngineBackend{recovery: dnsEngineSwitchRecoveryCommitted}
	useFakeDNSEngineBackend(t, backend)
	useScriptedHostRecoveryProbe(t, nil, hostRecoveryNever)

	if _, err := reloadHostBootRecoveryManager(t, root); err != nil {
		t.Fatal(err)
	}

	// Boot two: still unreadable. The reason is exactly where it was written.
	stillBroken, err := reloadHostBootRecoveryManager(t, root)
	if err != nil {
		t.Fatalf("the second boot could not bring the ledger up: %v", err)
	}
	job, active, poisoned := hostBootRecoveryLedgerJob(t, stillBroken, request.MutationRequestID)
	if poisoned != nil || active != "" || job == nil ||
		job.Status != serviceMutationStatusFailed ||
		job.ErrorCode != hostRecoveryReleasedUnsupportedCode ||
		!strings.Contains(job.ErrorMessage, hostRecoveryResidueSentence) {
		t.Fatalf(
			"the reason did not survive the next boot: poisoned=%v active=%q job=%+v",
			poisoned, active, job,
		)
	}

	// Boot three: the host is readable again. The journal the release left is
	// reconciled instead of poisoning an idle ledger, and the terminal receipt
	// the operator was given is not rewritten behind their back.
	useScriptedHostRecoveryProbe(t, nil, hostRecoveryDecideNow)
	backend.finalizeHook = removeDNSEngineSwitchJournal
	repaired, err := reloadHostBootRecoveryManager(t, root)
	if err != nil {
		t.Fatalf("a boot with a released journal beside an idle ledger failed: %v", err)
	}
	job, active, poisoned = hostBootRecoveryLedgerJob(t, repaired, request.MutationRequestID)
	if poisoned != nil || active != "" || job == nil ||
		job.Status != serviceMutationStatusFailed ||
		job.ErrorCode != hostRecoveryReleasedUnsupportedCode {
		t.Fatalf(
			"the released receipt was not preserved: poisoned=%v active=%q job=%+v",
			poisoned, active, job,
		)
	}
	if backend.recoverCalls != 1 || backend.finalizeCalls != 1 {
		t.Fatalf(
			"the released journal was not reconciled on the host: recover=%d finalize=%d",
			backend.recoverCalls, backend.finalizeCalls,
		)
	}
	if _, err := os.Lstat(
		filepath.Join(root, "state", dnsEngineSwitchJournalFile),
	); !os.IsNotExist(err) {
		t.Fatalf("the reconciled journal is still on the host: %v", err)
	}
}
