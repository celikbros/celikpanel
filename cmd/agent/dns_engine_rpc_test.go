//go:build linux

package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

type fakeDNSEngineBackend struct {
	readiness []transport.DNSBackendRuntimeState
	readyErr  error
	syncErr   error
	switchErr error
	result    transport.SwitchDNSEngineV1Response
}

func (backend *fakeDNSEngineBackend) Readiness(context.Context) ([]transport.DNSBackendRuntimeState, error) {
	return backend.readiness, backend.readyErr
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
	context.Context,
	mutationpayload.DNSEngineSwitchManifestCommitment,
	transport.ServiceMutationBinding,
) (transport.SwitchDNSEngineV1Response, error) {
	return backend.result, backend.switchErr
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
		"", transport.DNSEngineBIND, 0, 1, 0,
		transport.DNSTopologyStandalone,
		[]transport.DNSEngineSwitchZoneSnapshot{},
	)
	if err != nil {
		t.Fatal(err)
	}
	return SwitchDNSEngineV1Request{
		ServiceMutationBinding: mutationTestBinding(),
		SourceEngine:           commitment.SourceEngine, TargetEngine: commitment.TargetEngine,
		SourceEpoch: commitment.SourceEpoch, TargetEpoch: commitment.TargetEpoch,
		SourceRevision: commitment.SourceRevision, Topology: commitment.Topology,
		Zones: commitment.Zones, SnapshotBytes: commitment.SnapshotBytes,
		ManifestQualifier: commitment.Qualifier,
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
	if response.Error != "DNS engine switch failed; the previous DNS service was restored" ||
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

func TestDNSEngineStateCanonicalBinding(t *testing.T) {
	state := dnsEngineStateReceipt{
		Schema: dnsEngineStateSchema, Engine: transport.DNSEngineBIND,
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

func TestVerifyBINDPublicListenersRequiresNamedTCPAndUDP(t *testing.T) {
	valid := strings.Join([]string{
		`udp UNCONN 0 0 0.0.0.0:53 0.0.0.0:* users:(("named",pid=10,fd=1))`,
		`tcp LISTEN 0 4096 [::]:53 [::]:* users:(("named",pid=10,fd=2))`,
		`udp UNCONN 0 0 127.0.0.53:53 0.0.0.0:* users:(("systemd-resolve",pid=8,fd=3))`,
	}, "\n")
	if err := verifyBINDPublicListeners(valid); err != nil {
		t.Fatal(err)
	}
	if err := verifyBINDPublicListeners(strings.Replace(valid, `"named"`, `"pdns_server"`, 1)); err == nil {
		t.Fatal("public PowerDNS listener was accepted as BIND")
	}
	if err := verifyBINDPublicListeners(strings.Split(valid, "\n")[0]); err == nil {
		t.Fatal("UDP-only BIND listener set was accepted")
	}
}

func TestVerifyPDNSPublicListenersRequiresPDNSTCPAndUDP(t *testing.T) {
	valid := strings.Join([]string{
		`udp UNCONN 0 0 192.0.2.8:53 0.0.0.0:* users:(("pdns_server",pid=10,fd=1))`,
		`tcp LISTEN 0 4096 [2001:db8::8]:53 [::]:* users:(("pdns_server",pid=10,fd=2))`,
		`udp UNCONN 0 0 127.0.0.53:53 0.0.0.0:* users:(("systemd-resolve",pid=8,fd=3))`,
	}, "\n")
	if err := verifyPDNSPublicListeners(valid); err != nil {
		t.Fatal(err)
	}
	if err := verifyPDNSPublicListeners(strings.Replace(valid, `"pdns_server"`, `"named"`, 1)); err == nil {
		t.Fatal("public BIND listener was accepted as PowerDNS")
	}
	if err := verifyPDNSPublicListeners(strings.Split(valid, "\n")[0]); err == nil {
		t.Fatal("UDP-only PowerDNS listener set was accepted")
	}
}
