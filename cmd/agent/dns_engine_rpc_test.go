//go:build linux

package main

import (
	"context"
	"errors"
	"net"
	"net/rpc"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	finalizeCalls      int
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
	context.Context,
	transport.DNSEngine,
	string,
	transport.ServiceMutationBinding,
) (dnsEngineSwitchRecoveryOutcome, error) {
	backend.recoverCalls++
	return backend.recovery, backend.recoverErr
}

func (backend *fakeDNSEngineBackend) FinalizeSwitch(
	ctx context.Context,
	_ transport.DNSEngine,
	_ string,
	_ transport.ServiceMutationBinding,
) error {
	backend.finalizeCalls++
	_, backend.finalizeTracked = ctx.Value(serviceMutationExecutionTrackerKey{}).(*serviceMutationExecutionTracker)
	deadline, bounded := ctx.Deadline()
	backend.finalizeBounded = bounded
	if bounded {
		backend.finalizeRemaining = time.Until(deadline)
	}
	return nil
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

func TestDNSEngineSwitchStartupForwardCompletesExactCommittedJournal(t *testing.T) {
	request := canonicalSwitchRequest(t)
	manager, root := newMutationTestManager(t)
	beginMutationTestJobWithIdentity(
		t, manager, "dns_engine_switch", "bind", request.ManifestQualifier,
	)
	abandonFirewallApplyTestRuntime(t, manager)
	backend := &fakeDNSEngineBackend{recovery: dnsEngineSwitchRecoveryCommitted}
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
