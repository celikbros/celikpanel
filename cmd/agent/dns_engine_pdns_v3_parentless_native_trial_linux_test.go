//go:build linux && celikpanel_dns_v3_native

package main

import (
	"context"
	"os"
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

// This test mutates only an explicitly authorized, disposable QEMU primary.
// The public paired-primary setup and switch gates remain closed.
const parentlessNativeSchema = "celikpanel/pdns-v3-parentless-native-test-authorization/v1"

type parentlessNativeAuthorization struct {
	Schema          string `json:"schema"`
	CellID          string `json:"cell_id"`
	MachineID       string `json:"machine_id"`
	BootID          string `json:"boot_id"`
	ScenarioSHA256  string `json:"scenario_sha256"`
	SourceRequestID string `json:"source_request_id"`
	RequestID       string `json:"request_id"`
	OwnerID         string `json:"owner_id"`
	Domain          string `json:"domain"`
	Generation      int64  `json:"generation"`
}

func TestNativeFreshPDNSPrimaryV3ParentlessDelete(t *testing.T) {
	if os.Getenv("CELIKPANEL_V3_PARENTLESS_NATIVE_TRIAL") != freshV3NativeCell {
		t.Skip("explicit disposable parentless V3 deletion only")
	}
	source, manifest, err := freshV3NativePreflight(true)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := freshV3NativeRootFile(freshV3NativeRoot+"/v3-parentless-native-authorization.json", 0o600, 4096)
	if err != nil {
		t.Fatal(err)
	}
	var auth parentlessNativeAuthorization
	if err := freshV3NativeDecodeExact(raw, &auth); err != nil {
		t.Fatal(err)
	}
	if auth.Schema != parentlessNativeSchema || auth.CellID != freshV3NativeCell ||
		auth.MachineID != source.MachineID || auth.BootID != source.BootID ||
		auth.ScenarioSHA256 != source.ScenarioSHA256 || auth.SourceRequestID != source.RequestID ||
		!validMutationIdentity(auth.RequestID) || !validMutationIdentity(auth.OwnerID) ||
		auth.RequestID == source.RequestID || auth.OwnerID == source.OwnerID ||
		auth.Domain != "s1-kill.test" || auth.Generation != 2 {
		t.Fatal("parentless deletion authorization is not exact for this disposable boot")
	}
	if len(manifest.Zones) != 1 || manifest.Zones[0].Domain != auth.Domain ||
		manifest.Zones[0].Delete || manifest.Zones[0].DesiredGeneration != 1 {
		t.Fatal("parentless source zone differs from exact switch snapshot")
	}
	ctx := context.Background()
	state, exists, err := readDNSEngineState()
	if err != nil || !exists || state.Engine != transport.DNSEnginePowerDNS ||
		state.EngineEpoch != 1 || state.PairRole != transport.DNSPairRolePrimary ||
		state.PairLocalIP != "192.0.2.10" || state.PairPeerIP != "192.0.2.11" ||
		state.NativeCatalogV3 != dnsengineartifact.NativeCatalogDebian49V3 {
		t.Fatalf("native primary state differs: exists=%v err=%v", exists, err)
	}
	if _, exists, err := readDNSEngineSwitchJournal(); err != nil || exists {
		t.Fatalf("native switch journal has not retired: exists=%v err=%v", exists, err)
	}
	original, exact, err := readPDNSV3ZoneSnapshot(ctx, pdnsDBPath(), state, auth.Domain,
		manifest.Zones[0].ZoneQualifier, transport.ServiceMutationBinding{
			MutationRequestID: source.RequestID, MutationOwnerID: source.OwnerID,
		})
	if err != nil || !exact || original.Delete || original.DesiredGeneration != 1 {
		t.Fatalf("original native zone receipt is not exact: exact=%v err=%v", exact, err)
	}
	commitment, err := mutationpayload.CanonicalDNSZoneSyncV3(
		transport.DNSEnginePowerDNS, 1, auth.Generation, auth.Domain, true, "MASTER", nil)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := agentServiceMutationManager()
	if err != nil {
		t.Fatal(err)
	}
	if manager.status(auth.RequestID) != nil {
		t.Fatal("parentless deletion request already exists; reconcile it")
	}
	job, err := manager.begin(&ServiceMutationBeginRequest{
		RequestID: auth.RequestID, OwnerID: auth.OwnerID, Kind: "dns_zone_sync",
		Target: auth.Domain, PackageName: commitment.Qualifier,
	})
	if err != nil || job == nil || job.Status != serviceMutationStatusRunning {
		t.Fatalf("exact parentless deletion lease unavailable: job=%+v err=%v", job, err)
	}
	binding := transport.ServiceMutationBinding{
		MutationRequestID: auth.RequestID, MutationOwnerID: auth.OwnerID,
	}
	request := SyncDNSZoneV3Request{
		ServiceMutationBinding: binding, Engine: transport.DNSEnginePowerDNS,
		EngineEpoch: 1, DesiredGeneration: auth.Generation, Domain: auth.Domain,
		Delete: true, ZoneType: commitment.ZoneType,
	}
	var response SyncDNSZoneV3Response
	if err := (&Agent{}).SyncDNSZoneV3(&request, &response); err != nil {
		t.Fatal(err)
	}
	if response.RecoveryPending {
		t.Fatalf("exact parentless deletion remains pending: request=%s code=%s", auth.RequestID, response.PendingCode)
	}
	if response.Error != "" || !response.Synced || response.Engine != transport.DNSEnginePowerDNS ||
		response.EngineEpoch != 1 || response.AppliedGeneration != auth.Generation {
		t.Fatalf("parentless deletion result is not verified: %+v", response)
	}
	terminal, err := manager.finish(&ServiceMutationFinishRequest{
		RequestID: auth.RequestID, OwnerID: auth.OwnerID, Success: true,
	})
	if err != nil || terminal == nil || terminal.Status != serviceMutationStatusSucceeded {
		t.Fatalf("parentless deletion lacks terminal receipt: job=%+v err=%v", terminal, err)
	}
	deleted, exact, err := readPDNSV3ZoneSnapshot(ctx, pdnsDBPath(), state, auth.Domain,
		commitment.Qualifier, binding)
	if err != nil || !exact || !deleted.Delete || deleted.DesiredGeneration != auth.Generation {
		t.Fatalf("native deletion receipt is not exact: exact=%v err=%v", exact, err)
	}
	ready, err := powerDNSPrimaryPairReady(ctx, state)
	if err != nil || !ready {
		t.Fatalf("native pair lost catalog readiness: %v", err)
	}
	t.Logf("native parentless V3 deletion terminal: request=%s generation=%d", auth.RequestID, auth.Generation)
}
