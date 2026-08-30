//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/rpc"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/hostplatform"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

type fakeDNSEngineBackend struct {
	readiness          []transport.DNSBackendRuntimeState
	readinessBounded   bool
	readinessRemaining time.Duration
	port53Conflict     bool
	readyErr           error
	syncErr            error
	switchErr          error
	switchHook         func() error
	result             transport.SwitchDNSEngineV1Response
	switchCalls        int
	switchManifest     mutationpayload.DNSEngineSwitchManifestCommitment
	recovery           dnsEngineSwitchRecoveryOutcome
	recoverErr         error
	recoverCalls       int
	recoveryTarget     transport.DNSEngine
	recoveryQualifier  string
	recoveryBinding    transport.ServiceMutationBinding
	recoveryHook       func() error
	finalizeCalls      int
	finalizeTarget     transport.DNSEngine
	finalizeQualifier  string
	finalizeBinding    transport.ServiceMutationBinding
	finalizeErr        error
	finalizeHook       func() error
	finalizeTracked    bool
	finalizeBounded    bool
	finalizeRemaining  time.Duration
}

func (backend *fakeDNSEngineBackend) Readiness(
	ctx context.Context,
) (transport.DNSBackendReadinessResponse, error) {
	deadline, bounded := ctx.Deadline()
	backend.readinessBounded = bounded
	if bounded {
		backend.readinessRemaining = time.Until(deadline)
	}
	return transport.DNSBackendReadinessResponse{
		Engines: backend.readiness, Port53Conflict: backend.port53Conflict,
	}, backend.readyErr
}

func (backend *fakeDNSEngineBackend) Sync(
	context.Context,
	mutationpayload.DNSZoneSyncV3Commitment,
	transport.ServiceMutationBinding,
) (string, error) {
	return strings.Repeat("a", 64), backend.syncErr
}

func (backend *fakeDNSEngineBackend) RecoverZone(
	context.Context,
	string,
	string,
	transport.ServiceMutationBinding,
) (bool, error) {
	return false, nil
}

func (backend *fakeDNSEngineBackend) Switch(
	_ context.Context,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	_ transport.ServiceMutationBinding,
) (transport.SwitchDNSEngineV1Response, error) {
	backend.switchCalls++
	backend.switchManifest = manifest
	if backend.switchHook != nil {
		if err := backend.switchHook(); err != nil {
			return backend.result, err
		}
	}
	return backend.result, backend.switchErr
}

func (backend *fakeDNSEngineBackend) RecoverSwitch(
	_ context.Context,
	target transport.DNSEngine,
	qualifier string,
	binding transport.ServiceMutationBinding,
) (dnsEngineSwitchRecoveryOutcome, error) {
	backend.recoverCalls++
	backend.recoveryTarget = target
	backend.recoveryQualifier = qualifier
	backend.recoveryBinding = binding
	if backend.recoveryHook != nil {
		if err := backend.recoveryHook(); err != nil {
			return backend.recovery, err
		}
	}
	return backend.recovery, backend.recoverErr
}

func (backend *fakeDNSEngineBackend) FinalizeSwitch(
	ctx context.Context,
	target transport.DNSEngine,
	qualifier string,
	binding transport.ServiceMutationBinding,
) error {
	backend.finalizeCalls++
	backend.finalizeTarget = target
	backend.finalizeQualifier = qualifier
	backend.finalizeBinding = binding
	_, backend.finalizeTracked = ctx.Value(serviceMutationExecutionTrackerKey{}).(*serviceMutationExecutionTracker)
	deadline, bounded := ctx.Deadline()
	backend.finalizeBounded = bounded
	if bounded {
		backend.finalizeRemaining = time.Until(deadline)
	}
	if backend.finalizeHook != nil {
		if err := backend.finalizeHook(); err != nil {
			return err
		}
	}
	return backend.finalizeErr
}

func useFakeDNSEngineBackend(t *testing.T, backend dnsEngineBackend) {
	t.Helper()
	previous := agentDNSEngineBackend
	agentDNSEngineBackend = backend
	t.Cleanup(func() { agentDNSEngineBackend = previous })
}

func canonicalSwitchRequest(t *testing.T) SwitchDNSEngineV1Request {
	t.Helper()
	commitment, err := mutationpayload.CanonicalDNSEngineSwitchManifest(
		transport.DNSEngineSwitchModeSwitch,
		"", transport.DNSEngineBIND, 0, 1, 0,
		transport.DNSTopologyStandalone,
		[]transport.DNSEngineSwitchZoneSnapshot{},
	)
	if err != nil {
		t.Fatal(err)
	}
	return SwitchDNSEngineV1Request{
		ServiceMutationBinding: mutationTestBinding(),
		Mode:                   commitment.Mode,
		SourceEngine:           commitment.SourceEngine, TargetEngine: commitment.TargetEngine,
		SourceEpoch: commitment.SourceEpoch, TargetEpoch: commitment.TargetEpoch,
		SourceRevision: commitment.SourceRevision, Topology: commitment.Topology,
		Zones: commitment.Zones, SnapshotBytes: commitment.SnapshotBytes,
		ManifestQualifier: commitment.Qualifier,
	}
}

func pairedZeroZoneSwitchRequest(t *testing.T) SwitchDNSEngineV1Request {
	t.Helper()
	commitment, err := mutationpayload.CanonicalDNSEngineSwitchManifestWithPairIdentity(
		transport.DNSEngineSwitchModeSwitch,
		"", transport.DNSEngineBIND, 0, 1, 1,
		transport.DNSTopologyPaired,
		transport.DNSPairRolePrimary,
		"192.0.2.10", "ns1.example.test",
		"198.51.100.20", "ns2.example.test",
		[]transport.DNSEngineSwitchZoneSnapshot{},
	)
	if err != nil {
		t.Fatal(err)
	}
	return SwitchDNSEngineV1Request{
		ServiceMutationBinding: mutationTestBinding(),
		Mode:                   commitment.Mode,
		SourceEngine:           commitment.SourceEngine,
		TargetEngine:           commitment.TargetEngine,
		SourceEpoch:            commitment.SourceEpoch,
		TargetEpoch:            commitment.TargetEpoch,
		SourceRevision:         commitment.SourceRevision,
		Topology:               commitment.Topology,
		PairRole:               commitment.PairRole,
		LocalIP:                commitment.LocalIP,
		LocalNS:                commitment.LocalNS,
		PeerIP:                 commitment.PeerIP,
		PeerNS:                 commitment.PeerNS,
		Zones:                  commitment.Zones,
		SnapshotBytes:          commitment.SnapshotBytes,
		ManifestQualifier:      commitment.Qualifier,
	}
}

func TestSwitchDNSEnginePublishesExactTerminalReceipt(t *testing.T) {
	request := canonicalSwitchRequest(t)
	manager, _ := newMutationTestManager(t)
	installGlobalMutationTestManager(t, manager)
	beginMutationTestJobWithIdentity(
		t, manager, "dns_engine_switch", "bind", request.ManifestQualifier,
	)
	backend := &fakeDNSEngineBackend{result: transport.SwitchDNSEngineV1Response{
		Applied: true, ActiveEngine: transport.DNSEngineBIND,
		ActiveEpoch: 1, AppliedZones: 0,
	}}
	useFakeDNSEngineBackend(t, backend)

	var response SwitchDNSEngineV1Response
	if err := (&Agent{}).SwitchDNSEngineV1(&request, &response); err != nil {
		t.Fatal(err)
	}
	if !response.Applied || response.ActiveEngine != transport.DNSEngineBIND || response.ActiveEpoch != 1 {
		t.Fatalf("response=%+v", response)
	}
	job := manager.status(testMutationRequestID)
	wantPhase := dnsEngineSwitchFinalizedPhasePrefix + testMutationRequestID + "/" + request.ManifestQualifier
	if job == nil || job.Status != serviceMutationStatusSucceeded || job.Phase != wantPhase {
		t.Fatalf("terminal job=%+v want phase %q", job, wantPhase)
	}
	if backend.finalizeCalls != 1 ||
		backend.finalizeTracked ||
		!backend.finalizeBounded ||
		backend.finalizeRemaining <= 0 ||
		backend.finalizeRemaining > dnsEngineSwitchRecoveryLimit {
		t.Fatalf(
			"finalize calls=%d tracked=%v bounded=%v remaining=%s",
			backend.finalizeCalls,
			backend.finalizeTracked,
			backend.finalizeBounded,
			backend.finalizeRemaining,
		)
	}
}

func TestSwitchDNSEngineReceiptWriteFailureKeepsDurableActiveLedgerAfterLockRelease(
	t *testing.T,
) {
	request := canonicalSwitchRequest(t)
	manager, root := newMutationTestManager(t)
	installGlobalMutationTestManager(t, manager)
	beginMutationTestJobWithIdentity(
		t, manager, "dns_engine_switch", "bind", request.ManifestQualifier,
	)
	backend := &fakeDNSEngineBackend{
		result: transport.SwitchDNSEngineV1Response{
			Applied: true, ActiveEngine: transport.DNSEngineBIND,
			ActiveEpoch: 1, AppliedZones: 0,
		},
	}
	useFakeDNSEngineBackend(t, backend)
	fired := false
	setServiceMutationWriteFault(manager, func(point string) error {
		if point == serviceMutationWriteFaultBeforeRename && !fired {
			fired = true
			return errors.New("injected finalized receipt write failure")
		}
		return nil
	})

	var response SwitchDNSEngineV1Response
	if err := (&Agent{}).SwitchDNSEngineV1(&request, &response); err != nil {
		t.Fatal(err)
	}
	if !fired ||
		response.Error != "DNS engine switch finished but its durable receipt could not be reverified" {
		t.Fatalf("receipt failure fired=%v response=%+v", fired, response)
	}
	manager.mu.Lock()
	poisoned := manager.poisoned != nil
	active := manager.active
	manager.mu.Unlock()
	if !poisoned || active == nil || active.lock != nil ||
		!exactActiveDNSEngineSwitchJob(
			active.job,
			request.MutationRequestID,
			request.MutationOwnerID,
			request.TargetEngine,
			request.ManifestQualifier,
		) {
		t.Fatalf(
			"receipt failure did not retain only durable active authority: poisoned=%v active=%+v",
			poisoned, active,
		)
	}
	durable, err := manager.loadLedgerFromDisk()
	if err != nil {
		t.Fatal(err)
	}
	if durable.ActiveRequestID != request.MutationRequestID ||
		!exactActiveDNSEngineSwitchJob(
			durable.Jobs[request.MutationRequestID],
			request.MutationRequestID,
			request.MutationOwnerID,
			request.TargetEngine,
			request.ManifestQualifier,
		) {
		t.Fatalf("receipt failure exposed terminal durable state: %+v", durable)
	}
	if concurrent, beginErr := manager.begin(&ServiceMutationBeginRequest{
		RequestID: strings.Repeat("d", 32),
		OwnerID:   strings.Repeat("e", 32),
		Kind:      "service_install",
		Target:    "nginx",
	}); !errors.Is(beginErr, errServiceMutationManagerPoisoned) || concurrent != nil {
		t.Fatalf("poisoned manager accepted work: job=%+v err=%v", concurrent, beginErr)
	}
	if lock, lockErr := acquireServiceMutationFileLock(manager.lockPath); lockErr != nil {
		t.Fatalf("host lock remained after finalization proof: %v", lockErr)
	} else if err := lock.Close(); err != nil {
		t.Fatal(err)
	}

	releasePoisonedFirewallApplyTestManager(manager)
	backend.recovery = dnsEngineSwitchRecoveryFinalized
	reloaded, err := newServiceMutationManager(
		filepath.Join(root, "state"), filepath.Join(root, "service-mutation.lock"),
	)
	if err != nil {
		t.Fatalf("finalized receipt startup recovery failed: %v", err)
	}
	job := reloaded.status(request.MutationRequestID)
	wantPhase, err := formatDNSEngineSwitchFinalizedPhase(
		request.MutationRequestID, request.ManifestQualifier,
	)
	if err != nil {
		t.Fatal(err)
	}
	if backend.recoverCalls != 1 || backend.finalizeCalls != 1 ||
		job == nil || job.Status != serviceMutationStatusSucceeded ||
		job.Phase != wantPhase {
		t.Fatalf(
			"finalized receipt recovery backend=%+v job=%+v want phase=%q",
			backend, job, wantPhase,
		)
	}
	again, err := newServiceMutationManager(
		filepath.Join(root, "state"), filepath.Join(root, "service-mutation.lock"),
	)
	if err != nil {
		t.Fatalf("finalized receipt idempotent restart failed: %v", err)
	}
	againJob := again.status(request.MutationRequestID)
	if backend.recoverCalls != 1 || backend.finalizeCalls != 1 ||
		againJob == nil || againJob.Status != serviceMutationStatusSucceeded ||
		againJob.Phase != wantPhase {
		t.Fatalf(
			"finalized receipt was not idempotent: backend=%+v job=%+v want phase=%q",
			backend, againJob, wantPhase,
		)
	}
}

func TestSwitchDNSEngineFinalReceiptRetainsPublicationLeaseAfterHostRelease(
	t *testing.T,
) {
	request := canonicalSwitchRequest(t)
	manager, _ := newMutationTestManager(t)
	installGlobalMutationTestManager(t, manager)
	beginMutationTestJobWithIdentity(
		t, manager, "dns_engine_switch", "bind", request.ManifestQualifier,
	)
	useFakeDNSEngineBackend(t, &fakeDNSEngineBackend{
		result: transport.SwitchDNSEngineV1Response{
			Applied: true, ActiveEngine: transport.DNSEngineBIND,
			ActiveEpoch: 1, AppliedZones: 0,
		},
	})
	entered := make(chan struct{})
	release := make(chan struct{})
	fired := false
	setServiceMutationWriteFault(manager, func(point string) error {
		if point == serviceMutationWriteFaultBeforeRename && !fired {
			fired = true
			close(entered)
			<-release
		}
		return nil
	})

	done := make(chan SwitchDNSEngineV1Response, 1)
	go func() {
		var response SwitchDNSEngineV1Response
		_ = (&Agent{}).SwitchDNSEngineV1(&request, &response)
		done <- response
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("final DNS receipt publication did not reach its rename boundary")
	}

	hostProbe, err := acquireServiceMutationFileLock(manager.lockPath)
	if err != nil {
		t.Fatalf("DNS final receipt did not release the host lock: %v", err)
	}
	if err := hostProbe.Close(); err != nil {
		t.Fatal(err)
	}
	if competing, err := acquireServiceMutationHostAndPublicationLocks(
		manager.lockPath,
	); !errors.Is(err, errServiceMutationHostBusy) {
		if competing != nil {
			_ = competing.Close()
		}
		t.Fatalf("competing ledger publisher entered final receipt window: %v", err)
	}
	hostProbe, err = acquireServiceMutationFileLock(manager.lockPath)
	if err != nil {
		t.Fatalf("failed competing publisher retained the host lock: %v", err)
	}
	if err := hostProbe.Close(); err != nil {
		t.Fatal(err)
	}
	durable, err := manager.loadLedgerFromDisk()
	if err != nil {
		t.Fatal(err)
	}
	if durable.ActiveRequestID != request.MutationRequestID ||
		!exactActiveDNSEngineSwitchJob(
			durable.Jobs[request.MutationRequestID],
			request.MutationRequestID,
			request.MutationOwnerID,
			request.TargetEngine,
			request.ManifestQualifier,
		) {
		t.Fatalf("pre-rename receipt window exposed terminal state: %+v", durable)
	}

	close(release)
	select {
	case response := <-done:
		if response.Error != "" || !response.Applied {
			t.Fatalf("response=%+v", response)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("final DNS receipt publication did not finish")
	}
	job := manager.status(request.MutationRequestID)
	wantPhase, err := formatDNSEngineSwitchFinalizedPhase(
		request.MutationRequestID, request.ManifestQualifier,
	)
	if err != nil {
		t.Fatal(err)
	}
	if job == nil || job.Status != serviceMutationStatusSucceeded ||
		job.Phase != wantPhase {
		t.Fatalf("final receipt job=%+v want phase=%q", job, wantPhase)
	}
}

func TestSwitchDNSEngineRetainsHostLeaseThroughFinalization(t *testing.T) {
	request := canonicalSwitchRequest(t)
	manager, root := newMutationTestManager(t)
	installGlobalMutationTestManager(t, manager)
	beginMutationTestJobWithIdentity(
		t, manager, "dns_engine_switch", "bind", request.ManifestQualifier,
	)
	journal := activeCommittedBINDStartupJournalFixture(
		t, manager, root, request,
	)
	switchEntered := make(chan struct{})
	releaseSwitch := make(chan struct{})
	finalizeEntered := make(chan struct{})
	releaseFinalize := make(chan struct{})
	backend := &fakeDNSEngineBackend{
		result: transport.SwitchDNSEngineV1Response{
			Applied: true, ActiveEngine: transport.DNSEngineBIND,
			ActiveEpoch: 1, AppliedZones: 0,
		},
		switchHook: func() error {
			close(switchEntered)
			<-releaseSwitch
			return writeDNSEngineSwitchJournal(journal)
		},
		finalizeHook: func() error {
			close(finalizeEntered)
			<-releaseFinalize
			return removeDNSEngineSwitchJournal()
		},
	}
	useFakeDNSEngineBackend(t, backend)

	done := make(chan SwitchDNSEngineV1Response, 1)
	go func() {
		var response SwitchDNSEngineV1Response
		_ = (&Agent{}).SwitchDNSEngineV1(&request, &response)
		done <- response
	}()
	select {
	case <-switchEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("DNS switch result hook did not start")
	}

	manager.mu.Lock()
	runtime := manager.active
	manager.mu.Unlock()
	if runtime == nil {
		t.Fatal("DNS switch runtime is absent")
	}
	journalPath := filepath.Join(
		filepath.Dir(manager.ledgerPath), dnsEngineSwitchJournalFile,
	)
	if _, exists, err := readDNSEngineSwitchJournalAt(journalPath); err != nil || exists {
		t.Fatalf("pre-commit hook unexpectedly has a journal: exists=%v err=%v", exists, err)
	}
	assertProtected := func(stage string) {
		t.Helper()
		manager.expire(runtime)
		heartbeatJob, heartbeatErr := manager.heartbeat(&ServiceMutationHeartbeatRequest{
			RequestID: request.MutationRequestID,
			OwnerID:   request.MutationOwnerID,
			Phase:     "must-not-overwrite-leased",
		})
		if heartbeatErr != nil {
			t.Fatalf("%s heartbeat: %v", stage, heartbeatErr)
		}
		cancelJob, cancelErr := manager.cancelJob(&ServiceMutationCancelRequest{
			RequestID:     request.MutationRequestID,
			ExpectedOwner: request.MutationOwnerID,
			Reason:        "must-not-cancel-finalizing-dns",
		})
		if cancelErr != nil {
			t.Fatalf("%s cancel: %v", stage, cancelErr)
		}
		finishJob, finishErr := manager.finish(&ServiceMutationFinishRequest{
			RequestID:   request.MutationRequestID,
			OwnerID:     request.MutationOwnerID,
			Success:     false,
			FailureCode: "must-not-finish-finalizing-dns",
			Message:     "must not finish finalizing DNS",
		})
		if finishErr == nil {
			t.Fatalf("%s finish unexpectedly succeeded: %+v", stage, finishJob)
		}
		manager.mu.Lock()
		protected := manager.active == runtime &&
			runtime.dnsEngineSwitchFinalizing &&
			manager.poisoned == nil
		manager.mu.Unlock()
		for _, job := range []*ServiceMutationJob{
			heartbeatJob, cancelJob, finishJob,
			manager.status(request.MutationRequestID),
		} {
			if job == nil || job.Status != serviceMutationStatusRunning ||
				job.Phase != "leased" || job.ErrorCode != "" ||
				job.ErrorMessage != "" || !job.FinishedAt.IsZero() {
				t.Fatalf("%s protected job=%+v", stage, job)
			}
		}
		if !protected {
			t.Fatalf("%s finalizing runtime lost protection", stage)
		}
	}
	assertProtected("Switch hook before committed journal write")

	close(releaseSwitch)
	select {
	case <-finalizeEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("DNS finalizer did not start")
	}
	assertProtected("blocked finalizer")

	job, err := manager.begin(&ServiceMutationBeginRequest{
		RequestID: strings.Repeat("d", 32),
		OwnerID:   strings.Repeat("e", 32),
		Kind:      "service_install",
		Target:    "nginx",
	})
	if !errors.Is(err, errServiceMutationBusy) || job == nil ||
		job.RequestID != request.MutationRequestID ||
		job.Status != serviceMutationStatusRunning ||
		job.Phase != "leased" {
		t.Fatalf("concurrent ordinary request job=%+v err=%v", job, err)
	}
	if lock, lockErr := acquireServiceMutationFileLock(manager.lockPath); !errors.Is(lockErr, errServiceMutationHostBusy) {
		if lock != nil {
			_ = lock.Close()
		}
		t.Fatalf("second host lock acquired during DNS finalization: %v", lockErr)
	}
	close(releaseFinalize)
	select {
	case response := <-done:
		if response.Error != "" || !response.Applied {
			t.Fatalf("response=%+v", response)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("DNS finalizer did not finish")
	}
	if lock, lockErr := acquireServiceMutationFileLock(manager.lockPath); lockErr != nil {
		t.Fatalf("host lock remained after exact finalization: %v", lockErr)
	} else if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := newServiceMutationManager(
		filepath.Join(root, "state"), filepath.Join(root, "service-mutation.lock"),
	)
	if err != nil {
		t.Fatal(err)
	}
	wantPhase, err := formatDNSEngineSwitchFinalizedPhase(
		request.MutationRequestID, request.ManifestQualifier,
	)
	if err != nil {
		t.Fatal(err)
	}
	job = reloaded.status(request.MutationRequestID)
	if job == nil || job.Status != serviceMutationStatusSucceeded ||
		job.Phase != wantPhase || reloaded.active != nil ||
		reloaded.poisoned != nil {
		t.Fatalf("restarted final receipt job=%+v active=%v poisoned=%v", job, reloaded.active != nil, reloaded.poisoned)
	}
}

func TestSwitchDNSEngineProvenAbortKeepsExpiryWatchdogAlive(t *testing.T) {
	request := canonicalSwitchRequest(t)
	manager, _ := newMutationTestManager(t)
	manager.leaseDuration = 100 * time.Millisecond
	manager.overallDuration = 10 * time.Second
	installGlobalMutationTestManager(t, manager)
	beginMutationTestJobWithIdentity(
		t, manager, "dns_engine_switch", "bind", request.ManifestQualifier,
	)

	switchEntered := make(chan struct{})
	releaseSwitch := make(chan struct{})
	backend := &fakeDNSEngineBackend{
		switchErr: errors.New("injected pre-commit switch failure"),
		recovery:  dnsEngineSwitchRecoveryAbsent,
		switchHook: func() error {
			close(switchEntered)
			<-releaseSwitch
			return nil
		},
	}
	useFakeDNSEngineBackend(t, backend)

	done := make(chan SwitchDNSEngineV1Response, 1)
	go func() {
		var response SwitchDNSEngineV1Response
		_ = (&Agent{}).SwitchDNSEngineV1(&request, &response)
		done <- response
	}()
	select {
	case <-switchEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("DNS switch did not enter its guarded backend")
	}

	// The watchdog's first one-second tick observes the expired lease while
	// the pre-commit guard is held. It must keep retrying instead of exiting.
	time.Sleep(1200 * time.Millisecond)
	manager.mu.Lock()
	runtime := manager.active
	guarded := runtime != nil && runtime.dnsEngineSwitchFinalizing &&
		runtime.job.Status == serviceMutationStatusRunning &&
		!manager.now().Before(runtime.job.LeaseExpiresAt)
	manager.mu.Unlock()
	if !guarded {
		t.Fatal("expired DNS switch was not retained by its pre-commit guard")
	}

	close(releaseSwitch)
	select {
	case response := <-done:
		if response.Error != "DNS engine switch did not complete; inspect the agent log" {
			t.Fatalf("response=%+v", response)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("proven pre-commit abort did not return")
	}

	deadline := time.Now().Add(4 * time.Second)
	for {
		manager.mu.Lock()
		active := manager.active
		poisoned := manager.poisoned
		job := cloneServiceMutationJob(manager.ledger.Jobs[request.MutationRequestID])
		manager.mu.Unlock()
		if poisoned != nil {
			t.Fatalf("proven abort poisoned manager: %v", poisoned)
		}
		if active == nil && job != nil && job.Status == serviceMutationStatusFailed {
			if job.ErrorCode != serviceMutationErrorLeaseExpired ||
				job.ErrorMessage != serviceMutationMessageLeaseExpired {
				t.Fatalf("expired terminal receipt=%+v", job)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("watchdog did not resume after proven abort: active=%v job=%+v", active != nil, job)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if lock, err := acquireServiceMutationFileLock(manager.lockPath); err != nil {
		t.Fatalf("host lock remained after resumed expiry: %v", err)
	} else if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSwitchDNSEnginePreCommitFailureClearsGuardOnlyAfterExactAbortReproof(
	t *testing.T,
) {
	tests := []struct {
		name       string
		outcome    dnsEngineSwitchRecoveryOutcome
		recoverErr error
		wantPoison bool
	}{
		{name: "absent", outcome: dnsEngineSwitchRecoveryAbsent},
		{name: "rolled back", outcome: dnsEngineSwitchRecoveryRolledBack},
		{name: "committed is ambiguous", outcome: dnsEngineSwitchRecoveryCommitted, wantPoison: true},
		{name: "finalized is ambiguous", outcome: dnsEngineSwitchRecoveryFinalized, wantPoison: true},
		{name: "recovery error is ambiguous", outcome: dnsEngineSwitchRecoveryAbsent, recoverErr: errors.New("injected reproof failure"), wantPoison: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := canonicalSwitchRequest(t)
			manager, _ := newMutationTestManager(t)
			installGlobalMutationTestManager(t, manager)
			beginMutationTestJobWithIdentity(
				t, manager, "dns_engine_switch", "bind", request.ManifestQualifier,
			)
			backend := &fakeDNSEngineBackend{
				switchErr:  errors.New("injected pre-commit switch failure"),
				recovery:   test.outcome,
				recoverErr: test.recoverErr,
			}
			useFakeDNSEngineBackend(t, backend)

			var response SwitchDNSEngineV1Response
			if err := (&Agent{}).SwitchDNSEngineV1(&request, &response); err != nil {
				t.Fatal(err)
			}
			if backend.switchCalls != 1 || backend.recoverCalls != 1 {
				t.Fatalf("switch=%d recover=%d", backend.switchCalls, backend.recoverCalls)
			}
			manager.mu.Lock()
			runtime := manager.active
			poisoned := manager.poisoned
			guarded := runtime != nil && runtime.dnsEngineSwitchFinalizing
			manager.mu.Unlock()
			if test.wantPoison {
				defer releasePoisonedFirewallApplyTestManager(manager)
				if poisoned == nil || !guarded ||
					response.Error != "DNS engine switch outcome could not be verified; inspect the agent log" {
					t.Fatalf("response=%+v poisoned=%v guarded=%v", response, poisoned, guarded)
				}
				return
			}
			if poisoned != nil || guarded ||
				response.Error != "DNS engine switch did not complete; inspect the agent log" {
				t.Fatalf("response=%+v poisoned=%v guarded=%v", response, poisoned, guarded)
			}
			finished, finishErr := manager.finish(&ServiceMutationFinishRequest{
				RequestID:   request.MutationRequestID,
				OwnerID:     request.MutationOwnerID,
				Success:     false,
				FailureCode: "expected_test_cleanup",
				Message:     "expected test cleanup",
			})
			if finishErr != nil || finished == nil ||
				finished.Status != serviceMutationStatusFailed {
				t.Fatalf("finish job=%+v err=%v", finished, finishErr)
			}
		})
	}
}

func TestSwitchDNSEngineFinalizationLedgerTOCTOUFailsClosed(t *testing.T) {
	request := canonicalSwitchRequest(t)
	manager, _ := newMutationTestManager(t)
	installGlobalMutationTestManager(t, manager)
	beginMutationTestJobWithIdentity(
		t, manager, "dns_engine_switch", "bind", request.ManifestQualifier,
	)
	backend := &fakeDNSEngineBackend{
		result: transport.SwitchDNSEngineV1Response{
			Applied: true, ActiveEngine: transport.DNSEngineBIND,
			ActiveEpoch: 1, AppliedZones: 0,
		},
		finalizeHook: func() error {
			manager.mu.Lock()
			defer manager.mu.Unlock()
			before := cloneServiceMutationLedger(manager.ledger)
			manager.active.job.OwnerID = strings.Repeat("f", 32)
			return manager.persistLedgerMutationProtectedLocked(
				before, manager.active.job.RequestID,
			)
		},
	}
	useFakeDNSEngineBackend(t, backend)

	var response SwitchDNSEngineV1Response
	if err := (&Agent{}).SwitchDNSEngineV1(&request, &response); err != nil {
		t.Fatal(err)
	}
	if response.Error != "DNS engine switch finished but its durable receipt could not be reverified" {
		t.Fatalf("TOCTOU response=%+v", response)
	}
	manager.mu.Lock()
	poisoned := manager.poisoned != nil
	active := manager.active != nil
	manager.mu.Unlock()
	if !poisoned || !active {
		t.Fatalf("TOCTOU did not retain fail-closed runtime: poisoned=%v active=%v", poisoned, active)
	}
	releasePoisonedFirewallApplyTestManager(manager)
}

func TestSwitchDNSEngineFinalizationFailureRetainsHostLeaseAndPoisons(t *testing.T) {
	request := canonicalSwitchRequest(t)
	manager, _ := newMutationTestManager(t)
	installGlobalMutationTestManager(t, manager)
	beginMutationTestJobWithIdentity(
		t, manager, "dns_engine_switch", "bind", request.ManifestQualifier,
	)
	backend := &fakeDNSEngineBackend{
		result: transport.SwitchDNSEngineV1Response{
			Applied: true, ActiveEngine: transport.DNSEngineBIND,
			ActiveEpoch: 1, AppliedZones: 0,
		},
		finalizeErr: errors.New("injected DNS ownership finalization failure"),
	}
	useFakeDNSEngineBackend(t, backend)

	var response SwitchDNSEngineV1Response
	if err := (&Agent{}).SwitchDNSEngineV1(&request, &response); err != nil {
		t.Fatal(err)
	}
	if response.Error != "DNS engine switch reached its verified target but finalization did not complete" {
		t.Fatalf("finalization response=%+v", response)
	}
	manager.mu.Lock()
	poisoned := manager.poisoned != nil
	active := manager.active != nil
	manager.mu.Unlock()
	if !poisoned || !active {
		t.Fatalf(
			"finalization failure did not retain fail-closed runtime: poisoned=%v active=%v",
			poisoned, active,
		)
	}
	job := manager.status(testMutationRequestID)
	if !exactActiveDNSEngineSwitchJob(
		job,
		request.MutationRequestID,
		request.MutationOwnerID,
		request.TargetEngine,
		request.ManifestQualifier,
	) {
		t.Fatalf("finalization failure exposed a terminal receipt: %+v", job)
	}
	if concurrent, err := manager.begin(&ServiceMutationBeginRequest{
		RequestID: strings.Repeat("d", 32),
		OwnerID:   strings.Repeat("e", 32),
		Kind:      "service_install",
		Target:    "nginx",
	}); !errors.Is(err, errServiceMutationManagerPoisoned) || concurrent != nil {
		t.Fatalf("concurrent begin job=%+v err=%v", concurrent, err)
	}
	if lock, err := acquireServiceMutationFileLock(manager.lockPath); !errors.Is(err, errServiceMutationHostBusy) {
		if lock != nil {
			_ = lock.Close()
		}
		t.Fatalf("host lock escaped finalization poison: %v", err)
	}
	releasePoisonedFirewallApplyTestManager(manager)
}

func TestSwitchDNSEngineResidualCommittedJournalRetainsHostLeaseAndPoisons(
	t *testing.T,
) {
	request := canonicalSwitchRequest(t)
	manager, root := newMutationTestManager(t)
	installGlobalMutationTestManager(t, manager)
	beginMutationTestJobWithIdentity(
		t, manager, "dns_engine_switch", "bind", request.ManifestQualifier,
	)
	journal := persistActiveCommittedBINDStartupJournal(
		t, manager, root, request,
	)
	backend := &fakeDNSEngineBackend{
		result: transport.SwitchDNSEngineV1Response{
			Applied: true, ActiveEngine: transport.DNSEngineBIND,
			ActiveEpoch: 1, AppliedZones: 0,
		},
	}
	useFakeDNSEngineBackend(t, backend)

	var response SwitchDNSEngineV1Response
	if err := (&Agent{}).SwitchDNSEngineV1(&request, &response); err != nil {
		t.Fatal(err)
	}
	if response.Error != "DNS engine switch finished but its durable receipt could not be reverified" {
		t.Fatalf("residual journal response=%+v", response)
	}
	manager.mu.Lock()
	poisoned := manager.poisoned != nil
	active := manager.active != nil
	manager.mu.Unlock()
	if !poisoned || !active {
		t.Fatalf(
			"residual journal did not retain fail-closed runtime: poisoned=%v active=%v",
			poisoned, active,
		)
	}
	journalPath := filepath.Join(
		filepath.Dir(manager.ledgerPath), dnsEngineSwitchJournalFile,
	)
	actual, exists, readErr := readDNSEngineSwitchJournalAt(journalPath)
	if readErr != nil || !exists || !reflect.DeepEqual(actual, journal) {
		t.Fatalf("residual journal changed: exists=%v journal=%+v err=%v", exists, actual, readErr)
	}
	if lock, lockErr := acquireServiceMutationFileLock(manager.lockPath); !errors.Is(lockErr, errServiceMutationHostBusy) {
		if lock != nil {
			_ = lock.Close()
		}
		t.Fatalf("residual journal released host lock: %v", lockErr)
	}
	releasePoisonedFirewallApplyTestManager(manager)
}

func TestSwitchDNSEngineAcceptsGobCollapsedZeroZonesWithDurableLease(t *testing.T) {
	request := pairedZeroZoneSwitchRequest(t)
	if request.Zones == nil {
		t.Fatal("canonical zero-zone manifest must use an explicit empty slice")
	}
	manager, root := newMutationTestManager(t)
	installGlobalMutationTestManager(t, manager)
	beginMutationTestJobWithIdentity(
		t, manager, "dns_engine_switch", "bind", request.ManifestQualifier,
	)
	backend := &fakeDNSEngineBackend{result: transport.SwitchDNSEngineV1Response{
		Applied: true, ActiveEngine: transport.DNSEngineBIND,
		ActiveEpoch: 1, AppliedZones: 0,
	}}
	useFakeDNSEngineBackend(t, backend)

	server := rpc.NewServer()
	if err := server.RegisterName("Agent", &Agent{}); err != nil {
		t.Fatal(err)
	}
	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() {
		_ = clientConn.Close()
		_ = serverConn.Close()
	})
	go server.ServeConn(serverConn)
	client := rpc.NewClient(clientConn)
	t.Cleanup(func() { _ = client.Close() })

	var response SwitchDNSEngineV1Response
	if err := client.Call("Agent.SwitchDNSEngineV1", &request, &response); err != nil {
		t.Fatal(err)
	}
	if response.Error != "" || !response.Applied ||
		response.ActiveEngine != transport.DNSEngineBIND ||
		response.ActiveEpoch != 1 || response.AppliedZones != 0 {
		t.Fatalf("response=%+v", response)
	}
	if backend.switchCalls != 1 ||
		backend.switchManifest.Qualifier != request.ManifestQualifier ||
		backend.switchManifest.Topology != transport.DNSTopologyPaired ||
		backend.switchManifest.PairRole != transport.DNSPairRolePrimary ||
		len(backend.switchManifest.Zones) != 0 {
		t.Fatalf(
			"backend calls=%d manifest=%+v",
			backend.switchCalls, backend.switchManifest,
		)
	}

	reloaded, err := newServiceMutationManager(
		filepath.Join(root, "state"), filepath.Join(root, "service-mutation.lock"),
	)
	if err != nil {
		t.Fatal(err)
	}
	job := reloaded.status(testMutationRequestID)
	wantPhase := dnsEngineSwitchFinalizedPhasePrefix +
		testMutationRequestID + "/" + request.ManifestQualifier
	if job == nil || job.Status != serviceMutationStatusSucceeded ||
		job.Phase != wantPhase {
		t.Fatalf("durable terminal job=%+v want phase %q", job, wantPhase)
	}
}

func TestEqualDNSEngineSwitchWireZonesRejectsNonEmptyTamper(t *testing.T) {
	canonical := []transport.DNSEngineSwitchZoneSnapshot{{
		Ordinal: 0, Domain: "example.test", DesiredGeneration: 7,
		ZoneType: "PRIMARY",
		Records: []transport.ZoneRecord{{
			Name: "example.test", Type: "A", Content: "192.0.2.10", TTL: 300,
		}},
		ZoneQualifier: "dns-zone-sync/v3:sha256:" + strings.Repeat("a", 64),
	}}
	ordinalTamper := append([]transport.DNSEngineSwitchZoneSnapshot(nil), canonical...)
	ordinalTamper[0].Ordinal = 1
	if equalDNSEngineSwitchWireZones(ordinalTamper, canonical) {
		t.Fatal("non-empty ordinal tamper was accepted")
	}
	recordTamper := append([]transport.DNSEngineSwitchZoneSnapshot(nil), canonical...)
	recordTamper[0].Records = append([]transport.ZoneRecord(nil), canonical[0].Records...)
	recordTamper[0].Records[0].Content = "192.0.2.11"
	if equalDNSEngineSwitchWireZones(recordTamper, canonical) {
		t.Fatal("non-empty record tamper was accepted")
	}
}

func TestSwitchDNSEngineHidesBackendCommandDetail(t *testing.T) {
	request := canonicalSwitchRequest(t)
	manager, _ := newMutationTestManager(t)
	installGlobalMutationTestManager(t, manager)
	beginMutationTestJobWithIdentity(
		t, manager, "dns_engine_switch", "bind", request.ManifestQualifier,
	)
	secret := "named-checkconf /etc/bind/private failed: token=do-not-leak"
	useFakeDNSEngineBackend(t, &fakeDNSEngineBackend{
		switchErr: errors.New(secret),
		recovery:  dnsEngineSwitchRecoveryAbsent,
	})

	var response SwitchDNSEngineV1Response
	if err := (&Agent{}).SwitchDNSEngineV1(&request, &response); err != nil {
		t.Fatal(err)
	}
	if response.Error != "DNS engine switch did not complete; inspect the agent log" ||
		strings.Contains(response.Error, "do-not-leak") || strings.Contains(response.Error, "/etc/bind") {
		t.Fatalf("unsafe client error %q", response.Error)
	}
}

func TestDNSBackendReadinessHidesProbeDetail(t *testing.T) {
	useFakeDNSEngineBackend(t, &fakeDNSEngineBackend{
		readyErr: errors.New("/root/private host detail"),
	})
	var response DNSBackendReadinessResponse
	if err := (&Agent{}).DNSBackendReadiness(&transport.Empty{}, &response); err != nil {
		t.Fatal(err)
	}
	if response.Error != "DNS backend readiness could not be verified" ||
		strings.Contains(response.Error, "/root") {
		t.Fatalf("unsafe readiness response %+v", response)
	}
}

func TestDNSBackendReadinessReportsOnlyBoundedPort53Conflict(t *testing.T) {
	backend := &fakeDNSEngineBackend{
		readiness: []transport.DNSBackendRuntimeState{
			{Engine: transport.DNSEngineBIND, Unit: "named.service"},
			{Engine: transport.DNSEnginePowerDNS, Unit: "pdns.service"},
		},
		port53Conflict: true,
	}
	useFakeDNSEngineBackend(t, backend)
	var response DNSBackendReadinessResponse
	if err := (&Agent{}).DNSBackendReadiness(&transport.Empty{}, &response); err != nil {
		t.Fatal(err)
	}
	if !response.Port53Conflict || response.Error != "" || len(response.Engines) != 2 {
		t.Fatalf("unexpected readiness response: %+v", response)
	}
	if dnsBackendReadinessTimeout != 10*time.Second ||
		!backend.readinessBounded ||
		backend.readinessRemaining <= 0 ||
		backend.readinessRemaining > dnsBackendReadinessTimeout {
		t.Fatalf(
			"readiness timeout=%s bounded=%v remaining=%s",
			dnsBackendReadinessTimeout,
			backend.readinessBounded,
			backend.readinessRemaining,
		)
	}
}

func TestSwitchDNSEngineRejectsNoncanonicalManifestBeforeLease(t *testing.T) {
	request := canonicalSwitchRequest(t)
	request.SnapshotBytes++
	backend := &fakeDNSEngineBackend{}
	useFakeDNSEngineBackend(t, backend)
	var response SwitchDNSEngineV1Response
	if err := (&Agent{}).SwitchDNSEngineV1(&request, &response); err != nil {
		t.Fatal(err)
	}
	if response.Error != "DNS engine switch request is not the exact canonical manifest" {
		t.Fatalf("response=%+v", response)
	}
}

func activeCommittedBINDStartupJournalFixture(
	t *testing.T,
	manager *serviceMutationManager,
	root string,
	request SwitchDNSEngineV1Request,
) dnsEngineSwitchJournal {
	t.Helper()
	journal := testBINDSwitchJournal(t)
	t.Setenv("CELIKPANEL_AGENT_STATE_DIR", filepath.Join(root, "state"))
	journal.Phase = dnsSwitchPhaseCommitted
	journal.MutationRequestID = request.MutationRequestID
	journal.MutationOwnerID = request.MutationOwnerID
	journal.ManifestQualifier = request.ManifestQualifier
	journal.StateBefore.Path = filepath.Clean(dnsEngineStatePath())
	if err := validateDNSEngineSwitchJournal(journal); err != nil {
		t.Fatalf("invalid active committed BIND journal fixture: %v", err)
	}
	wantPath := filepath.Join(
		filepath.Dir(manager.ledgerPath), dnsEngineSwitchJournalFile,
	)
	if got := dnsEngineSwitchJournalPath(); got != wantPath {
		t.Fatalf("DNS journal path=%q want %q", got, wantPath)
	}
	return journal
}

func persistActiveCommittedBINDStartupJournal(
	t *testing.T,
	manager *serviceMutationManager,
	root string,
	request SwitchDNSEngineV1Request,
) dnsEngineSwitchJournal {
	t.Helper()
	journal := activeCommittedBINDStartupJournalFixture(
		t, manager, root, request,
	)
	if err := writeDNSEngineSwitchJournal(journal); err != nil {
		t.Fatal(err)
	}
	return journal
}

func persistActiveDNSEngineSwitchStartupLedger(
	t *testing.T,
	manager *serviceMutationManager,
	journal dnsEngineSwitchJournal,
	expiredCancelling bool,
	mutate func(*ServiceMutationJob),
) {
	t.Helper()
	now := manager.now()
	job := &ServiceMutationJob{
		RequestID:      journal.MutationRequestID,
		OwnerID:        journal.MutationOwnerID,
		Kind:           "dns_engine_switch",
		Target:         string(journal.TargetEngine),
		PackageName:    journal.ManifestQualifier,
		Status:         serviceMutationStatusRunning,
		Phase:          "leased",
		Attempt:        1,
		StartedAt:      now.Add(-2 * time.Minute),
		UpdatedAt:      now,
		LeaseExpiresAt: now.Add(time.Minute),
		DeadlineAt:     now.Add(time.Hour),
	}
	if expiredCancelling {
		job.Status = serviceMutationStatusCancelling
		job.Phase = serviceMutationPhaseCancellingExpiredLease
		job.LeaseExpiresAt = now.Add(-time.Minute)
		job.ErrorCode = serviceMutationErrorLeaseExpired
		job.ErrorMessage = serviceMutationMessageLeaseExpired
	}
	if mutate != nil {
		mutate(job)
	}
	ledger := serviceMutationLedger{
		Version:         serviceMutationLedgerVersion,
		ActiveRequestID: job.RequestID,
		Jobs:            map[string]*ServiceMutationJob{job.RequestID: job},
	}
	if err := validateServiceMutationLedger(&ledger); err != nil {
		t.Fatalf("invalid active DNS startup ledger fixture: %v", err)
	}
	manager.mu.Lock()
	before := cloneServiceMutationLedger(manager.ledger)
	manager.ledger = ledger
	err := manager.persistLedgerMutationLocked(before)
	manager.mu.Unlock()
	if err != nil {
		t.Fatalf("persist active DNS startup ledger: %v", err)
	}
}

func TestDNSEngineSwitchStartupForwardCompletesExactCommittedJournal(t *testing.T) {
	request := canonicalSwitchRequest(t)
	manager, root := newMutationTestManager(t)
	beginMutationTestJobWithIdentity(
		t, manager, "dns_engine_switch", "bind", request.ManifestQualifier,
	)
	persistActiveCommittedBINDStartupJournal(t, manager, root, request)
	abandonFirewallApplyTestRuntime(t, manager)
	backend := &fakeDNSEngineBackend{
		recovery:     dnsEngineSwitchRecoveryCommitted,
		finalizeHook: removeDNSEngineSwitchJournal,
	}
	useFakeDNSEngineBackend(t, backend)

	reloaded, err := newServiceMutationManager(
		filepath.Join(root, "state"), filepath.Join(root, "service-mutation.lock"),
	)
	if err != nil {
		t.Fatal(err)
	}
	job := reloaded.status(testMutationRequestID)
	wantPhase := dnsEngineSwitchFinalizedPhasePrefix + testMutationRequestID + "/" + request.ManifestQualifier
	if backend.recoverCalls != 1 || backend.finalizeCalls != 1 || job == nil ||
		job.Status != serviceMutationStatusSucceeded || job.Phase != wantPhase {
		t.Fatalf("recover=%d finalize=%d job=%+v", backend.recoverCalls, backend.finalizeCalls, job)
	}
}

func TestDNSEngineSwitchStartupUpgradesExactExpiredLeaseWithCommittedJournal(
	t *testing.T,
) {
	request := canonicalSwitchRequest(t)
	manager, root := newMutationTestManager(t)
	journal := persistActiveCommittedBINDStartupJournal(
		t, manager, root, request,
	)
	persistActiveDNSEngineSwitchStartupLedger(
		t, manager, journal, true, nil,
	)
	backend := &fakeDNSEngineBackend{
		recovery:     dnsEngineSwitchRecoveryCommitted,
		finalizeHook: removeDNSEngineSwitchJournal,
	}
	useFakeDNSEngineBackend(t, backend)

	reloaded, err := newServiceMutationManager(
		filepath.Join(root, "state"), filepath.Join(root, "service-mutation.lock"),
	)
	if err != nil {
		t.Fatal(err)
	}
	wantPhase, err := formatDNSEngineSwitchFinalizedPhase(
		journal.MutationRequestID, journal.ManifestQualifier,
	)
	if err != nil {
		t.Fatal(err)
	}
	job := reloaded.status(journal.MutationRequestID)
	if backend.recoverCalls != 1 || backend.finalizeCalls != 1 ||
		job == nil || job.Status != serviceMutationStatusSucceeded ||
		job.Phase != wantPhase || reloaded.active != nil ||
		reloaded.poisoned != nil {
		t.Fatalf(
			"recover=%d finalize=%d job=%+v active=%v poisoned=%v",
			backend.recoverCalls, backend.finalizeCalls, job,
			reloaded.active != nil, reloaded.poisoned,
		)
	}
}

func TestDNSEngineSwitchStartupUpgradesJournalFreeFinalizedHostCrash(
	t *testing.T,
) {
	manager, root := newMutationTestManager(t)
	journal, _, state, _ := signedUpdatePDNSBostonCommittedRecoveryFixture(t)
	stateDir := filepath.Join(root, "state")
	t.Setenv("CELIKPANEL_AGENT_STATE_DIR", stateDir)
	journal.StateBefore.Path = filepath.Clean(dnsEngineStatePath())
	if err := validateDNSEngineSwitchJournal(journal); err != nil {
		t.Fatalf("invalid journal-free crash fixture: %v", err)
	}
	if err := writeDNSEngineState(state); err != nil {
		t.Fatal(err)
	}
	if err := writeDNSEngineOwnership(state); err != nil {
		t.Fatal(err)
	}
	if _, exists, err := readDNSEngineInstallOwnership(
		journal.TargetEngine,
	); err != nil || exists {
		t.Fatalf("journal-free crash retains install ownership: exists=%v err=%v", exists, err)
	}
	if _, exists, err := readDNSEngineSwitchJournal(); err != nil || exists {
		t.Fatalf("journal-free crash unexpectedly has journal: exists=%v err=%v", exists, err)
	}
	persistActiveDNSEngineSwitchStartupLedger(
		t, manager, journal, true, nil,
	)
	useFakeDNSEngineBackend(t, hostDNSEngineBackend{})

	reloaded, err := newServiceMutationManager(
		stateDir, filepath.Join(root, "service-mutation.lock"),
	)
	if err != nil {
		t.Fatal(err)
	}
	wantPhase, err := formatDNSEngineSwitchFinalizedPhase(
		journal.MutationRequestID, journal.ManifestQualifier,
	)
	if err != nil {
		t.Fatal(err)
	}
	job := reloaded.status(journal.MutationRequestID)
	if job == nil || job.Status != serviceMutationStatusSucceeded ||
		job.Phase != wantPhase || reloaded.active != nil ||
		reloaded.poisoned != nil {
		t.Fatalf(
			"journal-free recovered job=%+v active=%v poisoned=%v",
			job, reloaded.active != nil, reloaded.poisoned,
		)
	}
}

func TestDNSEngineSwitchStartupExpiredLeaseMismatchFailsClosed(
	t *testing.T,
) {
	tests := []struct {
		name   string
		mutate func(*ServiceMutationJob)
	}{
		{
			name: "error message mismatch",
			mutate: func(job *ServiceMutationJob) {
				job.ErrorMessage = "arbitrary cancellation"
			},
		},
		{
			name: "updated timestamp predates expiry",
			mutate: func(job *ServiceMutationJob) {
				job.UpdatedAt = job.LeaseExpiresAt.Add(-time.Second)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := canonicalSwitchRequest(t)
			manager, root := newMutationTestManager(t)
			journal := persistActiveCommittedBINDStartupJournal(
				t, manager, root, request,
			)
			persistActiveDNSEngineSwitchStartupLedger(
				t, manager, journal, true, test.mutate,
			)
			backend := &fakeDNSEngineBackend{
				recovery:     dnsEngineSwitchRecoveryCommitted,
				finalizeHook: removeDNSEngineSwitchJournal,
			}
			useFakeDNSEngineBackend(t, backend)

			reloaded, err := newServiceMutationManager(
				filepath.Join(root, "state"),
				filepath.Join(root, "service-mutation.lock"),
			)
			if err == nil || reloaded == nil || reloaded.poisoned == nil {
				t.Fatalf("mismatched startup manager=%+v err=%v", reloaded, err)
			}
			defer releasePoisonedFirewallApplyTestManager(reloaded)
			if backend.recoverCalls != 1 || backend.finalizeCalls != 0 {
				t.Fatalf(
					"mismatch reached finalization: recover=%d finalize=%d",
					backend.recoverCalls, backend.finalizeCalls,
				)
			}
			actual, exists, readErr := readDNSEngineSwitchJournal()
			if readErr != nil || !exists || !reflect.DeepEqual(actual, journal) {
				t.Fatalf(
					"fail-closed startup changed journal: exists=%v journal=%+v err=%v",
					exists, actual, readErr,
				)
			}
		})
	}
}

func TestDNSEngineSwitchStartupRetainsHostLeaseThroughRecoveredFinalization(
	t *testing.T,
) {
	request := canonicalSwitchRequest(t)
	manager, root := newMutationTestManager(t)
	competitor, err := newServiceMutationManager(
		filepath.Join(root, "state"), filepath.Join(root, "service-mutation.lock"),
	)
	if err != nil {
		t.Fatal(err)
	}
	beginMutationTestJobWithIdentity(
		t, manager, "dns_engine_switch", "bind", request.ManifestQualifier,
	)
	persistActiveCommittedBINDStartupJournal(t, manager, root, request)
	abandonFirewallApplyTestRuntime(t, manager)
	entered := make(chan struct{})
	release := make(chan struct{})
	backend := &fakeDNSEngineBackend{
		recovery: dnsEngineSwitchRecoveryCommitted,
		finalizeHook: func() error {
			close(entered)
			<-release
			return removeDNSEngineSwitchJournal()
		},
	}
	useFakeDNSEngineBackend(t, backend)

	type startupResult struct {
		manager *serviceMutationManager
		err     error
	}
	done := make(chan startupResult, 1)
	go func() {
		reloaded, reloadErr := newServiceMutationManager(
			filepath.Join(root, "state"), filepath.Join(root, "service-mutation.lock"),
		)
		done <- startupResult{manager: reloaded, err: reloadErr}
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("recovered DNS finalizer did not start")
	}

	concurrent, beginErr := competitor.begin(&ServiceMutationBeginRequest{
		RequestID: strings.Repeat("d", 32),
		OwnerID:   strings.Repeat("e", 32),
		Kind:      "service_install",
		Target:    "nginx",
	})
	if !errors.Is(beginErr, errServiceMutationHostBusy) || concurrent != nil {
		t.Fatalf("concurrent begin job=%+v err=%v", concurrent, beginErr)
	}
	if lock, lockErr := acquireServiceMutationFileLock(manager.lockPath); !errors.Is(lockErr, errServiceMutationHostBusy) {
		if lock != nil {
			_ = lock.Close()
		}
		t.Fatalf("second host lock acquired during recovered finalization: %v", lockErr)
	}
	close(release)
	select {
	case result := <-done:
		if result.err != nil || result.manager == nil {
			t.Fatalf("recovered startup manager=%+v err=%v", result.manager, result.err)
		}
		job := result.manager.status(testMutationRequestID)
		if job == nil || job.Status != serviceMutationStatusSucceeded {
			t.Fatalf("recovered terminal job=%+v", job)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("recovered DNS finalizer did not finish")
	}
	if lock, lockErr := acquireServiceMutationFileLock(manager.lockPath); lockErr != nil {
		t.Fatalf("host lock remained after recovered finalization: %v", lockErr)
	} else if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestDNSEngineSwitchStartupFinalizeFailureRetainsPoisonLock(t *testing.T) {
	request := canonicalSwitchRequest(t)
	manager, root := newMutationTestManager(t)
	beginMutationTestJobWithIdentity(
		t, manager, "dns_engine_switch", "bind", request.ManifestQualifier,
	)
	journal := persistActiveCommittedBINDStartupJournal(t, manager, root, request)
	abandonFirewallApplyTestRuntime(t, manager)
	backend := &fakeDNSEngineBackend{
		recovery:    dnsEngineSwitchRecoveryCommitted,
		finalizeErr: errors.New("injected recovered finalization failure"),
	}
	useFakeDNSEngineBackend(t, backend)

	reloaded, err := newServiceMutationManager(
		filepath.Join(root, "state"), filepath.Join(root, "service-mutation.lock"),
	)
	if err == nil || reloaded == nil || reloaded.poisoned == nil ||
		reloaded.poisonLock == nil {
		t.Fatalf("recovered finalization manager=%+v err=%v", reloaded, err)
	}
	defer releasePoisonedFirewallApplyTestManager(reloaded)
	job := reloaded.status(testMutationRequestID)
	if !exactActiveDNSEngineSwitchJob(
		job,
		request.MutationRequestID,
		request.MutationOwnerID,
		request.TargetEngine,
		request.ManifestQualifier,
	) {
		t.Fatalf("recovered finalization exposed a terminal receipt: %+v", job)
	}
	journalPath := filepath.Join(
		filepath.Dir(reloaded.ledgerPath), dnsEngineSwitchJournalFile,
	)
	actual, exists, readErr := readDNSEngineSwitchJournalAt(journalPath)
	if readErr != nil || !exists || !reflect.DeepEqual(actual, journal) {
		t.Fatalf("recovered finalization changed journal: exists=%v journal=%+v err=%v", exists, actual, readErr)
	}
	if lock, lockErr := acquireServiceMutationFileLock(reloaded.lockPath); !errors.Is(lockErr, errServiceMutationHostBusy) {
		if lock != nil {
			_ = lock.Close()
		}
		t.Fatalf("poisoned startup released host lock: %v", lockErr)
	}
}

func TestDNSEngineSwitchStartupClosesVerifiedRollbackAsFailure(t *testing.T) {
	request := canonicalSwitchRequest(t)
	manager, root := newMutationTestManager(t)
	beginMutationTestJobWithIdentity(
		t, manager, "dns_engine_switch", "bind", request.ManifestQualifier,
	)
	abandonFirewallApplyTestRuntime(t, manager)
	backend := &fakeDNSEngineBackend{recovery: dnsEngineSwitchRecoveryRolledBack}
	useFakeDNSEngineBackend(t, backend)

	reloaded, err := newServiceMutationManager(
		filepath.Join(root, "state"), filepath.Join(root, "service-mutation.lock"),
	)
	if err != nil {
		t.Fatal(err)
	}
	job := reloaded.status(testMutationRequestID)
	if backend.recoverCalls != 1 || backend.finalizeCalls != 0 || job == nil ||
		job.Status != serviceMutationStatusFailed ||
		job.ErrorCode != "dns_engine_switch_rolled_back_after_restart" {
		t.Fatalf("recover=%d finalize=%d job=%+v", backend.recoverCalls, backend.finalizeCalls, job)
	}
}

func persistBostonCommittedPowerDNSStartupFixture(
	t *testing.T,
	manager *serviceMutationManager,
	root string,
	mutate func(*dnsEngineSwitchJournal, *serviceMutationLedger),
) (
	dnsEngineSwitchJournal,
	dnsEngineStateReceipt,
	dnsEngineInstallOwnershipReceipt,
) {
	t.Helper()
	journal, ledger, state, install :=
		signedUpdatePDNSBostonCommittedRecoveryFixture(t)
	stateDir := filepath.Join(root, "state")
	t.Setenv("CELIKPANEL_AGENT_STATE_DIR", stateDir)
	journal.StateBefore.Path = filepath.Clean(dnsEngineStatePath())
	if mutate != nil {
		mutate(&journal, &ledger)
	}
	if err := validateDNSEngineSwitchJournal(journal); err != nil {
		t.Fatalf("invalid persisted Boston DNS journal fixture: %v", err)
	}
	if err := validateServiceMutationLedger(&ledger); err != nil {
		t.Fatalf("invalid persisted Boston service ledger fixture: %v", err)
	}
	wantJournalPath := filepath.Join(
		filepath.Dir(manager.ledgerPath), dnsEngineSwitchJournalFile,
	)
	if got := dnsEngineSwitchJournalPath(); got != wantJournalPath {
		t.Fatalf("DNS journal path=%q want live ledger sibling %q", got, wantJournalPath)
	}
	if err := writeDNSEngineState(state); err != nil {
		t.Fatalf("write persisted Boston DNS state: %v", err)
	}
	if err := writeDNSEngineInstallOwnership(install); err != nil {
		t.Fatalf("write persisted Boston install ownership: %v", err)
	}
	if _, exists, err := readDNSEngineOwnership(
		transport.DNSEnginePowerDNS,
	); err != nil || exists {
		t.Fatalf("Boston fixture unexpectedly has active ownership: exists=%v err=%v", exists, err)
	}
	manager.mu.Lock()
	before := cloneServiceMutationLedger(manager.ledger)
	manager.ledger = cloneSignedUpdateLedger(ledger)
	err := manager.persistLedgerMutationLocked(before)
	manager.mu.Unlock()
	if err != nil {
		t.Fatalf("persist Boston terminal service ledger: %v", err)
	}
	if err := writeDNSEngineSwitchJournal(journal); err != nil {
		t.Fatalf("write persisted Boston committed DNS journal: %v", err)
	}
	if mutate == nil {
		provedState, installExists, ownershipExists, err :=
			exactCommittedDNSEngineProvenanceOnHost(
				journal, testUbuntuBINDProfile(),
			)
		if err != nil || provedState != state ||
			!installExists || ownershipExists {
			t.Fatalf(
				"Boston initial provenance state=%+v install=%v ownership=%v err=%v",
				provedState, installExists, ownershipExists, err,
			)
		}
	}
	return journal, state, install
}

func TestDNSEngineSwitchStartupAutoFinalizesBostonCommittedPowerDNS(t *testing.T) {
	manager, root := newMutationTestManager(t)
	journal, state, _ := persistBostonCommittedPowerDNSStartupFixture(
		t, manager, root, nil,
	)
	backend := &fakeDNSEngineBackend{
		recovery: dnsEngineSwitchRecoveryCommitted,
		finalizeHook: func() error {
			if err := writeDNSEngineOwnership(state); err != nil {
				return err
			}
			actual, exists, err := readDNSEngineOwnership(
				transport.DNSEnginePowerDNS,
			)
			if err != nil {
				return err
			}
			if !exists || actual != state {
				return errors.New("Boston active ownership handoff did not persist exactly")
			}
			provedState, installExists, ownershipExists, err :=
				exactCommittedDNSEngineProvenanceOnHost(
					journal, testUbuntuBINDProfile(),
				)
			if err != nil || provedState != state ||
				!installExists || !ownershipExists {
				return errors.New("Boston dual ownership handoff provenance is not exact")
			}
			if err := removeDNSEngineInstallOwnership(
				transport.DNSEnginePowerDNS,
			); err != nil {
				return err
			}
			provedState, installExists, ownershipExists, err =
				exactCommittedDNSEngineProvenanceOnHost(
					journal, testUbuntuBINDProfile(),
				)
			if err != nil || provedState != state ||
				installExists || !ownershipExists {
				return errors.New("Boston final ownership handoff provenance is not exact")
			}
			return removeDNSEngineSwitchJournal()
		},
	}
	useFakeDNSEngineBackend(t, backend)

	reloaded, err := newServiceMutationManager(
		filepath.Join(root, "state"), filepath.Join(root, "service-mutation.lock"),
	)
	if err != nil {
		t.Fatalf("startup did not auto-finalize committed Boston PowerDNS: %v", err)
	}
	wantBinding := switchJournalBinding(journal)
	if backend.recoverCalls != 1 || backend.finalizeCalls != 1 ||
		backend.recoveryTarget != transport.DNSEnginePowerDNS ||
		backend.finalizeTarget != transport.DNSEnginePowerDNS ||
		backend.recoveryQualifier != journal.ManifestQualifier ||
		backend.finalizeQualifier != journal.ManifestQualifier ||
		backend.recoveryBinding != wantBinding ||
		backend.finalizeBinding != wantBinding {
		t.Fatalf("unexpected Boston recovery calls: backend=%+v", backend)
	}
	job := reloaded.status(journal.MutationRequestID)
	wantPhase, phaseErr := formatDNSEngineSwitchFinalizedPhase(
		journal.MutationRequestID, journal.ManifestQualifier,
	)
	if phaseErr != nil {
		t.Fatal(phaseErr)
	}
	if job == nil || job.Status != serviceMutationStatusSucceeded ||
		job.Phase != wantPhase || job.OwnerID != journal.MutationOwnerID {
		t.Fatalf("Boston terminal job changed during startup: %+v", job)
	}
	ownership, ownershipExists, ownershipErr := readDNSEngineOwnership(
		transport.DNSEnginePowerDNS,
	)
	if ownershipErr != nil || !ownershipExists || ownership != state {
		t.Fatalf("Boston active ownership=%+v exists=%v err=%v", ownership, ownershipExists, ownershipErr)
	}
	if _, installExists, installErr := readDNSEngineInstallOwnership(
		transport.DNSEnginePowerDNS,
	); installErr != nil || installExists {
		t.Fatalf("Boston install ownership was not retired: exists=%v err=%v", installExists, installErr)
	}
	journalPath := filepath.Join(
		filepath.Dir(reloaded.ledgerPath), dnsEngineSwitchJournalFile,
	)
	if _, exists, readErr := readDNSEngineSwitchJournalAt(journalPath); readErr != nil || exists {
		t.Fatalf("Boston committed journal remains: exists=%v err=%v", exists, readErr)
	}

	if _, err := newServiceMutationManager(
		filepath.Join(root, "state"), filepath.Join(root, "service-mutation.lock"),
	); err != nil {
		t.Fatalf("idempotent startup after Boston finalization failed: %v", err)
	}
	if backend.recoverCalls != 1 || backend.finalizeCalls != 1 {
		t.Fatalf("idempotent startup replayed finalization: recover=%d finalize=%d", backend.recoverCalls, backend.finalizeCalls)
	}
}

func TestHostDNSEngineFinalizeSwitchCompletesBostonPowerDNSFilesystemHandoff(
	t *testing.T,
) {
	manager, root := newMutationTestManager(t)
	journal, state, _ := persistBostonCommittedPowerDNSStartupFixture(
		t, manager, root, nil,
	)
	for _, path := range []string{journal.PDNSCandidatePath, journal.PDNSBackupPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("switch-artifact"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	previousProfile := finalizeDNSEngineVerifiedHostProfile
	previousVerify := finalizeDNSEngineVerifyTarget
	finalizeDNSEngineVerifiedHostProfile = func() (hostplatform.Profile, error) {
		return testUbuntuBINDProfile(), nil
	}
	finalizeDNSEngineVerifyTarget = func(context.Context, dnsEngineSwitchJournal) error {
		return nil
	}
	t.Cleanup(func() {
		finalizeDNSEngineVerifiedHostProfile = previousProfile
		finalizeDNSEngineVerifyTarget = previousVerify
	})
	if err := (hostDNSEngineBackend{}).FinalizeSwitch(
		context.Background(), journal.TargetEngine,
		journal.ManifestQualifier, switchJournalBinding(journal),
	); err != nil {
		t.Fatalf("real host finalizer failed: %v", err)
	}
	ownership, exists, err := readDNSEngineOwnership(transport.DNSEnginePowerDNS)
	if err != nil || !exists || ownership != state {
		t.Fatalf("active ownership=%+v exists=%v err=%v", ownership, exists, err)
	}
	if _, exists, err := readDNSEngineInstallOwnership(
		transport.DNSEnginePowerDNS,
	); err != nil || exists {
		t.Fatalf("install ownership remains: exists=%v err=%v", exists, err)
	}
	for _, path := range []string{journal.PDNSCandidatePath, journal.PDNSBackupPath} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("PowerDNS switch artifact remains at %s: %v", path, err)
		}
	}
	journalPath := filepath.Join(filepath.Dir(manager.ledgerPath), dnsEngineSwitchJournalFile)
	if _, exists, err := readDNSEngineSwitchJournalAt(journalPath); err != nil || exists {
		t.Fatalf("committed journal remains: exists=%v err=%v", exists, err)
	}
	if err := (hostDNSEngineBackend{}).FinalizeSwitch(
		context.Background(), journal.TargetEngine,
		journal.ManifestQualifier, switchJournalBinding(journal),
	); err != nil {
		t.Fatalf("journal-free exact finalization was not idempotent: %v", err)
	}
	outcome, err := (hostDNSEngineBackend{}).RecoverSwitch(
		context.Background(), journal.TargetEngine,
		journal.ManifestQualifier, switchJournalBinding(journal),
	)
	if err != nil || outcome != dnsEngineSwitchRecoveryFinalized {
		t.Fatalf("finalized host recovery outcome=%q err=%v", outcome, err)
	}
}

func TestHostDNSEngineFinalizeSwitchWithoutJournalFailsClosedUnlessExact(
	t *testing.T,
) {
	t.Run("clean absence is unproven", func(t *testing.T) {
		request := canonicalSwitchRequest(t)
		_, root := newMutationTestManager(t)
		t.Setenv("CELIKPANEL_AGENT_STATE_DIR", filepath.Join(root, "state"))
		err := (hostDNSEngineBackend{}).FinalizeSwitch(
			context.Background(), request.TargetEngine,
			request.ManifestQualifier, request.ServiceMutationBinding,
		)
		if err == nil {
			t.Fatal("journal-free finalization accepted clean but unproven host state")
		}
	})

	t.Run("partial ownership is inconsistent", func(t *testing.T) {
		_, root := newMutationTestManager(t)
		journal, _, state, _ := signedUpdatePDNSBostonCommittedRecoveryFixture(t)
		t.Setenv("CELIKPANEL_AGENT_STATE_DIR", filepath.Join(root, "state"))
		if err := os.MkdirAll(filepath.Join(root, "state"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := writeDNSEngineOwnership(state); err != nil {
			t.Fatal(err)
		}
		err := (hostDNSEngineBackend{}).FinalizeSwitch(
			context.Background(), journal.TargetEngine,
			journal.ManifestQualifier, switchJournalBinding(journal),
		)
		if err == nil {
			t.Fatal("journal-free finalization accepted ownership without active state")
		}
	})
}

func TestHostDNSEngineRecoverSwitchWithoutJournalClassifiesStableSource(
	t *testing.T,
) {
	setupStableSource := func(t *testing.T) SwitchDNSEngineV1Request {
		t.Helper()
		request := canonicalSwitchRequest(t)
		_, _, source, _ := signedUpdatePDNSBostonCommittedRecoveryFixture(t)
		if source.Engine == request.TargetEngine {
			t.Fatal("stable source fixture unexpectedly names the switch target")
		}
		if err := os.MkdirAll(serviceMutationStateDirectory(), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := writeDNSEngineState(source); err != nil {
			t.Fatal(err)
		}
		if err := writeDNSEngineOwnership(source); err != nil {
			t.Fatal(err)
		}

		staleTargetOwnership := source
		staleTargetOwnership.Engine = request.TargetEngine
		staleTargetOwnership.Generation = strings.Repeat("c", 64)
		staleTargetOwnership.ManifestQualifier = request.ManifestQualifier
		staleTargetOwnership.MutationRequestID = strings.Repeat("d", 32)
		staleTargetOwnership.MutationOwnerID = strings.Repeat("e", 32)
		if err := writeDNSEngineOwnership(staleTargetOwnership); err != nil {
			t.Fatal(err)
		}
		return request
	}

	t.Run("stable source and stale target ownership are absent", func(t *testing.T) {
		request := setupStableSource(t)
		outcome, err := (hostDNSEngineBackend{}).RecoverSwitch(
			context.Background(), request.TargetEngine,
			request.ManifestQualifier, request.ServiceMutationBinding,
		)
		if err != nil || outcome != dnsEngineSwitchRecoveryAbsent {
			t.Fatalf("stable source recovery outcome=%q err=%v", outcome, err)
		}
		if err := (hostDNSEngineBackend{}).FinalizeSwitch(
			context.Background(), request.TargetEngine,
			request.ManifestQualifier, request.ServiceMutationBinding,
		); err == nil {
			t.Fatal("stable source was accepted as exact journal-free finalization")
		}
	})

	t.Run("target install residue remains fail closed", func(t *testing.T) {
		request := setupStableSource(t)
		install, err := newDNSEngineInstallOwnership(
			request.TargetEngine, hostplatform.PackageManagerAPT,
			[]string{"bind9"}, []string{"bind9"},
			mutationpayload.DNSEngineSwitchManifestCommitment{
				Mode:         request.Mode,
				TargetEngine: request.TargetEngine,
				Qualifier:    request.ManifestQualifier,
			},
			request.ServiceMutationBinding,
		)
		if err != nil {
			t.Fatal(err)
		}
		if err := writeDNSEngineInstallOwnership(install); err != nil {
			t.Fatal(err)
		}
		outcome, err := (hostDNSEngineBackend{}).RecoverSwitch(
			context.Background(), request.TargetEngine,
			request.ManifestQualifier, request.ServiceMutationBinding,
		)
		if err == nil || outcome != dnsEngineSwitchRecoveryAbsent {
			t.Fatalf("target residue recovery outcome=%q err=%v", outcome, err)
		}
	})
}

func TestDNSEngineSwitchStartupAutoFinalizesPacmanPowerDNSAdoptWithoutInstallReceipt(
	t *testing.T,
) {
	manager, root := newMutationTestManager(t)
	journal, ledger, state := signedUpdatePDNSAdoptCommittedRecoveryFixture(t)
	t.Setenv("CELIKPANEL_AGENT_STATE_DIR", filepath.Join(root, "state"))
	journal.StateBefore.Path = filepath.Clean(dnsEngineStatePath())
	if err := validateDNSEngineSwitchJournal(journal); err != nil {
		t.Fatal(err)
	}
	if err := writeDNSEngineState(state); err != nil {
		t.Fatal(err)
	}
	manager.mu.Lock()
	before := cloneServiceMutationLedger(manager.ledger)
	manager.ledger = cloneSignedUpdateLedger(ledger)
	err := manager.persistLedgerMutationLocked(before)
	manager.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if err := writeDNSEngineSwitchJournal(journal); err != nil {
		t.Fatal(err)
	}
	if _, exists, err := readDNSEngineInstallOwnership(
		transport.DNSEnginePowerDNS,
	); err != nil || exists {
		t.Fatalf("pacman adopt unexpectedly has install receipt: exists=%v err=%v", exists, err)
	}
	profile := hostplatform.Profile{PackageManager: hostplatform.PackageManagerPacman}
	backend := &fakeDNSEngineBackend{
		recovery: dnsEngineSwitchRecoveryCommitted,
		finalizeHook: func() error {
			proved, installExists, ownershipExists, err :=
				exactCommittedDNSEngineProvenanceOnHost(journal, profile)
			if err != nil || proved != state || installExists || ownershipExists {
				return fmt.Errorf(
					"pacman adopt initial provenance state=%+v install=%v ownership=%v: %w",
					proved, installExists, ownershipExists, err,
				)
			}
			if err := writeDNSEngineOwnership(state); err != nil {
				return err
			}
			proved, installExists, ownershipExists, err =
				exactCommittedDNSEngineProvenanceOnHost(journal, profile)
			if err != nil || proved != state || installExists || !ownershipExists {
				return fmt.Errorf(
					"pacman adopt final provenance state=%+v install=%v ownership=%v: %w",
					proved, installExists, ownershipExists, err,
				)
			}
			return removeDNSEngineSwitchJournal()
		},
	}
	useFakeDNSEngineBackend(t, backend)

	reloaded, err := newServiceMutationManager(
		filepath.Join(root, "state"), filepath.Join(root, "service-mutation.lock"),
	)
	if err != nil {
		t.Fatalf("pacman adopt startup recovery failed: %v", err)
	}
	job := reloaded.status(journal.MutationRequestID)
	wantPhase, err := formatDNSEngineSwitchFinalizedPhase(
		journal.MutationRequestID, journal.ManifestQualifier,
	)
	if err != nil {
		t.Fatal(err)
	}
	if backend.recoverCalls != 1 || backend.finalizeCalls != 1 ||
		job == nil || job.Status != serviceMutationStatusSucceeded || job.Phase != wantPhase {
		t.Fatalf("pacman adopt startup backend=%+v job=%+v", backend, job)
	}
	if _, exists, err := readDNSEngineOwnership(
		transport.DNSEnginePowerDNS,
	); err != nil || !exists {
		t.Fatalf("pacman adopt ownership was not published: exists=%v err=%v", exists, err)
	}
	if _, err := newServiceMutationManager(
		filepath.Join(root, "state"), filepath.Join(root, "service-mutation.lock"),
	); err != nil {
		t.Fatalf("pacman adopt idempotent restart failed: %v", err)
	}
	if backend.recoverCalls != 1 || backend.finalizeCalls != 1 {
		t.Fatalf("pacman adopt replayed recovery: recover=%d finalize=%d", backend.recoverCalls, backend.finalizeCalls)
	}
}

func TestDNSEngineSwitchStartupBostonCommittedPowerDNSMismatchFailsClosed(
	t *testing.T,
) {
	tests := []struct {
		name   string
		mutate func(*dnsEngineSwitchJournal, *serviceMutationLedger)
	}{
		{
			name: "foreign-terminal-owner",
			mutate: func(
				_ *dnsEngineSwitchJournal,
				ledger *serviceMutationLedger,
			) {
				for _, job := range ledger.Jobs {
					job.OwnerID = strings.Repeat("c", 32)
				}
			},
		},
		{
			name: "inexact-failed-terminal-job",
			mutate: func(
				_ *dnsEngineSwitchJournal,
				ledger *serviceMutationLedger,
			) {
				for _, job := range ledger.Jobs {
					job.Status = serviceMutationStatusFailed
					job.Phase = "failed"
					job.ErrorCode = "injected_failure"
					job.ErrorMessage = "injected terminal failure"
					job.UpdatedAt = job.FinishedAt.Add(-time.Second)
				}
			},
		},
		{
			name: "mismatched-committed-journal-owner",
			mutate: func(
				journal *dnsEngineSwitchJournal,
				_ *serviceMutationLedger,
			) {
				journal.MutationOwnerID = strings.Repeat("c", 32)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager, root := newMutationTestManager(t)
			journal, _, _ := persistBostonCommittedPowerDNSStartupFixture(
				t, manager, root, test.mutate,
			)
			backend := &fakeDNSEngineBackend{
				recovery:     dnsEngineSwitchRecoveryCommitted,
				finalizeHook: removeDNSEngineSwitchJournal,
			}
			useFakeDNSEngineBackend(t, backend)

			reloaded, err := newServiceMutationManager(
				filepath.Join(root, "state"),
				filepath.Join(root, "service-mutation.lock"),
			)
			if err == nil || reloaded == nil || reloaded.poisoned == nil {
				t.Fatalf("mismatched Boston startup manager=%+v err=%v", reloaded, err)
			}
			defer releasePoisonedFirewallApplyTestManager(reloaded)
			if backend.recoverCalls != 0 || backend.finalizeCalls != 0 {
				t.Fatalf("mismatched Boston evidence reached backend: recover=%d finalize=%d", backend.recoverCalls, backend.finalizeCalls)
			}
			journalPath := filepath.Join(
				filepath.Dir(reloaded.ledgerPath), dnsEngineSwitchJournalFile,
			)
			actual, exists, readErr := readDNSEngineSwitchJournalAt(journalPath)
			if readErr != nil || !exists || !reflect.DeepEqual(actual, journal) {
				t.Fatalf("fail-closed startup changed journal: exists=%v journal=%+v err=%v", exists, actual, readErr)
			}
			if _, exists, readErr := readDNSEngineInstallOwnership(
				transport.DNSEnginePowerDNS,
			); readErr != nil || !exists {
				t.Fatalf("fail-closed startup retired install ownership: exists=%v err=%v", exists, readErr)
			}
			if _, exists, readErr := readDNSEngineOwnership(
				transport.DNSEnginePowerDNS,
			); readErr != nil || exists {
				t.Fatalf("fail-closed startup published active ownership: exists=%v err=%v", exists, readErr)
			}
		})
	}
}

func TestDNSEngineSwitchStartupReprovesBostonEvidenceBeforeFinalize(
	t *testing.T,
) {
	tests := []struct {
		name   string
		mutate func(dnsEngineSwitchJournal, *serviceMutationManager) error
	}{
		{
			name: "journal-drift",
			mutate: func(
				journal dnsEngineSwitchJournal,
				_ *serviceMutationManager,
			) error {
				journal.MutationOwnerID = strings.Repeat("c", 32)
				return writeDNSEngineSwitchJournal(journal)
			},
		},
		{
			name: "ledger-drift",
			mutate: func(
				_ dnsEngineSwitchJournal,
				manager *serviceMutationManager,
			) error {
				manager.mu.Lock()
				defer manager.mu.Unlock()
				before := cloneServiceMutationLedger(manager.ledger)
				for _, job := range manager.ledger.Jobs {
					job.OwnerID = strings.Repeat("c", 32)
				}
				return manager.persistLedgerMutationLocked(before)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager, root := newMutationTestManager(t)
			journal, _, _ := persistBostonCommittedPowerDNSStartupFixture(
				t, manager, root, nil,
			)
			backend := &fakeDNSEngineBackend{
				recovery: dnsEngineSwitchRecoveryCommitted,
				recoveryHook: func() error {
					return test.mutate(journal, manager)
				},
				finalizeHook: removeDNSEngineSwitchJournal,
			}
			useFakeDNSEngineBackend(t, backend)

			reloaded, err := newServiceMutationManager(
				filepath.Join(root, "state"),
				filepath.Join(root, "service-mutation.lock"),
			)
			if err == nil || reloaded == nil || reloaded.poisoned == nil {
				t.Fatalf("drifted Boston startup manager=%+v err=%v", reloaded, err)
			}
			defer releasePoisonedFirewallApplyTestManager(reloaded)
			if backend.recoverCalls != 1 || backend.finalizeCalls != 0 {
				t.Fatalf("drifted evidence calls recover=%d finalize=%d", backend.recoverCalls, backend.finalizeCalls)
			}
			if _, exists, readErr := readDNSEngineInstallOwnership(
				transport.DNSEnginePowerDNS,
			); readErr != nil || !exists {
				t.Fatalf("evidence drift retired install ownership: exists=%v err=%v", exists, readErr)
			}
			if _, exists, readErr := readDNSEngineOwnership(
				transport.DNSEnginePowerDNS,
			); readErr != nil || exists {
				t.Fatalf("evidence drift published active ownership: exists=%v err=%v", exists, readErr)
			}
		})
	}
}

func TestDNSEngineSwitchStartupBostonFinalizeFailureRetainsExactEvidence(
	t *testing.T,
) {
	manager, root := newMutationTestManager(t)
	journal, _, _ := persistBostonCommittedPowerDNSStartupFixture(
		t, manager, root, nil,
	)
	backend := &fakeDNSEngineBackend{
		recovery:    dnsEngineSwitchRecoveryCommitted,
		finalizeErr: errors.New("injected finalization failure"),
	}
	useFakeDNSEngineBackend(t, backend)
	reloaded, err := newServiceMutationManager(
		filepath.Join(root, "state"), filepath.Join(root, "service-mutation.lock"),
	)
	if err == nil || reloaded == nil || reloaded.poisoned == nil {
		t.Fatalf("finalization failure manager=%+v err=%v", reloaded, err)
	}
	defer releasePoisonedFirewallApplyTestManager(reloaded)
	if backend.recoverCalls != 1 || backend.finalizeCalls != 1 {
		t.Fatalf("finalization failure calls recover=%d finalize=%d", backend.recoverCalls, backend.finalizeCalls)
	}
	journalPath := filepath.Join(
		filepath.Dir(reloaded.ledgerPath), dnsEngineSwitchJournalFile,
	)
	actual, exists, readErr := readDNSEngineSwitchJournalAt(journalPath)
	if readErr != nil || !exists || !reflect.DeepEqual(actual, journal) {
		t.Fatalf("finalization failure changed journal: exists=%v journal=%+v err=%v", exists, actual, readErr)
	}
	if _, exists, readErr := readDNSEngineInstallOwnership(
		transport.DNSEnginePowerDNS,
	); readErr != nil || !exists {
		t.Fatalf("finalization failure retired install ownership: exists=%v err=%v", exists, readErr)
	}
	if _, exists, readErr := readDNSEngineOwnership(
		transport.DNSEnginePowerDNS,
	); readErr != nil || exists {
		t.Fatalf("finalization failure published active ownership: exists=%v err=%v", exists, readErr)
	}
}

func TestDNSEngineStateCanonicalBinding(t *testing.T) {
	state := dnsEngineStateReceipt{
		Schema: dnsEngineStateSchema, Mode: transport.DNSEngineSwitchModeSwitch,
		Engine:      transport.DNSEngineBIND,
		EngineEpoch: 7, Generation: strings.Repeat("b", 64), SourceRevision: 12,
		ManifestQualifier: "dns-engine-switch/v1:sha256:" + strings.Repeat("c", 64),
		MutationRequestID: strings.Repeat("d", 32),
		MutationOwnerID:   strings.Repeat("e", 32),
	}
	encoded, err := encodeDNSEngineState(state)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeDNSEngineState(encoded)
	if err != nil || decoded != state {
		t.Fatalf("decoded=%+v err=%v", decoded, err)
	}
	if _, err := decodeDNSEngineState(append([]byte(" "), encoded...)); err == nil {
		t.Fatal("noncanonical engine state was accepted")
	}
	state.MutationOwnerID = strings.ToUpper(state.MutationOwnerID)
	if _, err := encodeDNSEngineState(state); err == nil {
		t.Fatal("uppercase mutation owner identity was accepted")
	}
}

func TestDNSEngineStateRejectsAdoptedBIND(t *testing.T) {
	state := dnsEngineStateReceipt{
		Schema: dnsEngineStateSchema, Mode: transport.DNSEngineSwitchModeAdopt,
		Engine: transport.DNSEngineBIND, EngineEpoch: 1,
		Generation: strings.Repeat("b", 64), SourceRevision: 1,
		ManifestQualifier: "dns-engine-switch/v1:sha256:" + strings.Repeat("a", 64),
		MutationRequestID: strings.Repeat("c", 32),
		MutationOwnerID:   strings.Repeat("d", 32),
	}
	if err := validateDNSEngineState(state); err == nil {
		t.Fatal("adopt mode accepted a BIND engine state")
	}
}

func TestDNSEngineStateBindsPrimaryCatalogSerialToPairRole(t *testing.T) {
	state := dnsEngineStateReceipt{
		Schema: dnsEngineStateSchema, Mode: transport.DNSEngineSwitchModeSwitch,
		Engine: transport.DNSEnginePowerDNS, EngineEpoch: 3,
		SourceRevision:    2,
		ManifestQualifier: "dns-engine-switch/v1:sha256:" + strings.Repeat("a", 64),
		MutationRequestID: strings.Repeat("b", 32),
		MutationOwnerID:   strings.Repeat("c", 32),
	}
	state.PairRole = transport.DNSPairRolePrimary
	if err := validateDNSEngineState(state); err == nil {
		t.Fatal("paired primary state accepted a missing address tuple")
	}
	state.PairLocalIP = "192.0.2.10"
	state.PairPeerIP = "192.0.2.20"
	if err := validateDNSEngineState(state); err == nil {
		t.Fatal("paired primary state accepted a missing catalog serial")
	}
	state.PrimaryCatalogSerial = 41
	if err := validateDNSEngineState(state); err != nil {
		t.Fatal(err)
	}
	state.PairRole = transport.DNSPairRoleSecondary
	if err := validateDNSEngineState(state); err == nil {
		t.Fatal("paired secondary state accepted a primary catalog serial")
	}
	state.PairRole = ""
	if err := validateDNSEngineState(state); err == nil {
		t.Fatal("standalone state accepted directional pair identity")
	}
	state.PrimaryCatalogSerial = 0
	state.PairLocalIP = ""
	state.PairPeerIP = ""
	if err := validateDNSEngineState(state); err != nil {
		t.Fatal(err)
	}
	state.PairLocalIP = "192.0.2.10"
	if err := validateDNSEngineState(state); err == nil {
		t.Fatal("standalone state accepted a partial pair address tuple")
	}
	state.PairRole = transport.DNSPairRoleSecondary
	state.PairPeerIP = "192.0.2.10"
	if err := validateDNSEngineState(state); err == nil {
		t.Fatal("paired state accepted identical local and peer addresses")
	}
	state.PairLocalIP = "192.0.2.010"
	state.PairPeerIP = "192.0.2.20"
	if err := validateDNSEngineState(state); err == nil {
		t.Fatal("paired state accepted a noncanonical local address")
	}
	state.PairLocalIP = "127.0.0.1"
	if err := validateDNSEngineState(state); err == nil {
		t.Fatal("paired state accepted a non-global local address")
	}
	state.Mode = transport.DNSEngineSwitchModeAdopt
	state.PairRole = transport.DNSPairRolePrimary
	state.PairLocalIP = "192.0.2.10"
	state.PairPeerIP = "192.0.2.20"
	state.PrimaryCatalogSerial = 41
	if err := validateDNSEngineState(state); err == nil {
		t.Fatal("legacy adoption state claimed directional primary catalog authority")
	}
}

func TestVerifyBINDPublicListenersRequiresNamedTCPAndUDP(t *testing.T) {
	valid := strings.Join([]string{
		`udp UNCONN 0 0 0.0.0.0:53 0.0.0.0:* users:(("named",pid=10,fd=1))`,
		`tcp LISTEN 0 4096 [::]:53 [::]:* users:(("named",pid=10,fd=2))`,
		`udp UNCONN 0 0 127.0.0.53%lo:53 0.0.0.0:* users:(("systemd-resolve",pid=8,fd=3))`,
	}, "\n")
	if err := verifyBINDPublicListeners(valid, 10); err != nil {
		t.Fatal(err)
	}
	if err := verifyBINDPublicListeners(strings.Replace(valid, `"named"`, `"pdns_server"`, 1), 10); err == nil {
		t.Fatal("public PowerDNS listener was accepted as BIND")
	}
	if err := verifyBINDPublicListeners(strings.Split(valid, "\n")[0], 10); err == nil {
		t.Fatal("UDP-only BIND listener set was accepted")
	}
}

func TestVerifyPDNSPublicListenersRequiresPDNSTCPAndUDP(t *testing.T) {
	valid := strings.Join([]string{
		`udp UNCONN 0 0 192.0.2.8:53 0.0.0.0:* users:(("pdns_server",pid=10,fd=1))`,
		`tcp LISTEN 0 4096 [2001:db8::8]:53 [::]:* users:(("pdns_server",pid=10,fd=2))`,
		`udp UNCONN 0 0 127.0.0.53%lo:53 0.0.0.0:* users:(("systemd-resolve",pid=8,fd=3))`,
	}, "\n")
	if err := verifyPDNSPublicListeners(valid, 10); err != nil {
		t.Fatal(err)
	}
	if err := verifyPDNSPublicListeners(strings.Replace(valid, `"pdns_server"`, `"named"`, 1), 10); err == nil {
		t.Fatal("public BIND listener was accepted as PowerDNS")
	}
	if err := verifyPDNSPublicListeners(strings.Split(valid, "\n")[0], 10); err == nil {
		t.Fatal("UDP-only PowerDNS listener set was accepted")
	}
}
