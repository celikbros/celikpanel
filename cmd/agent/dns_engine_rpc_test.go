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
	wantPhase := dnsEngineSwitchPublishedPhasePrefix + testMutationRequestID + "/" + request.ManifestQualifier
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

func TestSwitchDNSEngineRetainsHostLeaseThroughFinalization(t *testing.T) {
	request := canonicalSwitchRequest(t)
	manager, _ := newMutationTestManager(t)
	installGlobalMutationTestManager(t, manager)
	beginMutationTestJobWithIdentity(
		t, manager, "dns_engine_switch", "bind", request.ManifestQualifier,
	)
	entered := make(chan struct{})
	release := make(chan struct{})
	backend := &fakeDNSEngineBackend{
		result: transport.SwitchDNSEngineV1Response{
			Applied: true, ActiveEngine: transport.DNSEngineBIND,
			ActiveEpoch: 1, AppliedZones: 0,
		},
		finalizeHook: func() error {
			close(entered)
			<-release
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
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("DNS finalizer did not start")
	}

	job, err := manager.begin(&ServiceMutationBeginRequest{
		RequestID: strings.Repeat("d", 32),
		OwnerID:   strings.Repeat("e", 32),
		Kind:      "service_install",
		Target:    "nginx",
	})
	if !errors.Is(err, errServiceMutationBusy) || job == nil ||
		job.RequestID != request.MutationRequestID ||
		job.Status != serviceMutationStatusSucceeded {
		t.Fatalf("concurrent ordinary request job=%+v err=%v", job, err)
	}
	if lock, lockErr := acquireServiceMutationFileLock(manager.lockPath); !errors.Is(lockErr, errServiceMutationHostBusy) {
		if lock != nil {
			_ = lock.Close()
		}
		t.Fatalf("second host lock acquired during DNS finalization: %v", lockErr)
	}
	close(release)
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
	if job == nil || job.Status != serviceMutationStatusSucceeded ||
		!strings.HasPrefix(job.Phase, dnsEngineSwitchPublishedPhasePrefix) {
		t.Fatalf("finalization failure lost terminal receipt: %+v", job)
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
	wantPhase := dnsEngineSwitchPublishedPhasePrefix +
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
	useFakeDNSEngineBackend(t, &fakeDNSEngineBackend{switchErr: errors.New(secret)})

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

func persistActiveCommittedBINDStartupJournal(
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
	if err := writeDNSEngineSwitchJournal(journal); err != nil {
		t.Fatal(err)
	}
	return journal
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
	wantPhase := dnsEngineSwitchPublishedPhasePrefix + testMutationRequestID + "/" + request.ManifestQualifier
	if backend.recoverCalls != 1 || backend.finalizeCalls != 1 || job == nil ||
		job.Status != serviceMutationStatusSucceeded || job.Phase != wantPhase {
		t.Fatalf("recover=%d finalize=%d job=%+v", backend.recoverCalls, backend.finalizeCalls, job)
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
	if job == nil || job.Status != serviceMutationStatusSucceeded ||
		!strings.HasPrefix(job.Phase, dnsEngineSwitchPublishedPhasePrefix) {
		t.Fatalf("recovered finalization lost terminal receipt: %+v", job)
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
	wantPhase, phaseErr := formatDNSEngineSwitchPublishedPhase(
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
	wantPhase, err := formatDNSEngineSwitchPublishedPhase(
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
			name: "failed-terminal-job",
			mutate: func(
				_ *dnsEngineSwitchJournal,
				ledger *serviceMutationLedger,
			) {
				for _, job := range ledger.Jobs {
					job.Status = serviceMutationStatusFailed
					job.Phase = "failed"
					job.ErrorCode = "injected_failure"
					job.ErrorMessage = "injected terminal failure"
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
