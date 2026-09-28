//go:build linux && celikpanel_dns_v3_native

package main

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

// This is an explicit disposable-guest entrypoint. The public paired-primary
// setup and Switch RPC gates remain closed. A separate root-owned receipt binds
// every zone mutation to one boot, scenario, request, owner and payload.
const freshV3ZoneNativeSchema = "celikpanel/pdns-v3-zone-native-test-authorization/v1"

type freshV3ZoneNativeStep struct {
	RequestID string `json:"request_id"`
	OwnerID   string `json:"owner_id"`
	Qualifier string `json:"qualifier"`
}

type freshV3ZoneNativeAuthorization struct {
	Schema         string                  `json:"schema"`
	CellID         string                  `json:"cell_id"`
	MachineID      string                  `json:"machine_id"`
	BootID         string                  `json:"boot_id"`
	ScenarioSHA256 string                  `json:"scenario_sha256"`
	Nonce          string                  `json:"nonce"`
	Stage          *int                    `json:"stage"`
	Steps          []freshV3ZoneNativeStep `json:"steps"`
}

func freshV3ZoneNativeCommitments() ([]mutationpayload.DNSZoneSyncV3Commitment, error) {
	const domain = "s2.s1-kill.test"
	build := func(generation int64, serial string, edited, deleted bool) (mutationpayload.DNSZoneSyncV3Commitment, error) {
		var records []transport.ZoneRecord
		if !deleted {
			records = []transport.ZoneRecord{
				{Name: domain, Type: "SOA", Content: "ns1.s1-kill.test hostmaster.s1-kill.test " + serial + " 10800 3600 604800 3600", TTL: 300},
				{Name: domain, Type: "NS", Content: "ns1.s1-kill.test", TTL: 300},
				{Name: "www." + domain, Type: "A", Content: "192.0.2.10", TTL: 300},
			}
			if edited {
				records = append(records, transport.ZoneRecord{Name: "changed." + domain, Type: "A", Content: "192.0.2.10", TTL: 300})
			}
		}
		return mutationpayload.CanonicalDNSZoneSyncV3(transport.DNSEnginePowerDNS, 1, generation, domain, deleted, "MASTER", records)
	}
	specs := []struct {
		generation      int64
		serial          string
		edited, deleted bool
	}{
		{1, "2026092801", false, false},
		{2, "2026092802", true, false},
		{3, "", false, true},
		{4, "2026092803", false, false},
	}
	out := make([]mutationpayload.DNSZoneSyncV3Commitment, 0, len(specs))
	for _, spec := range specs {
		commitment, err := build(spec.generation, spec.serial, spec.edited, spec.deleted)
		if err != nil {
			return nil, err
		}
		out = append(out, commitment)
	}
	return out, nil
}

func freshV3ZoneNativePreflight(ctx context.Context) (freshV3ZoneNativeAuthorization, []mutationpayload.DNSZoneSyncV3Commitment, error) {
	var zero freshV3ZoneNativeAuthorization
	sourceAuth, _, err := freshV3NativePreflight(true)
	if err != nil {
		return zero, nil, err
	}
	raw, err := freshV3NativeRootFile(freshV3NativeRoot+"/v3-zone-native-authorization.json", 0o600, 1<<16)
	if err != nil {
		return zero, nil, err
	}
	var auth freshV3ZoneNativeAuthorization
	if err := freshV3NativeDecodeExact(raw, &auth); err != nil {
		return zero, nil, err
	}
	if auth.Schema != freshV3ZoneNativeSchema || auth.CellID != freshV3NativeCell ||
		auth.MachineID != sourceAuth.MachineID || auth.BootID != sourceAuth.BootID ||
		auth.ScenarioSHA256 != sourceAuth.ScenarioSHA256 || len(auth.Nonce) < 32 ||
		len(auth.Nonce) > 128 || len(auth.Nonce)%2 != 0 || strings.ToLower(auth.Nonce) != auth.Nonce {
		return zero, nil, errors.New("zone native authorization differs from the exact disposable boot")
	}
	if _, err := hex.DecodeString(auth.Nonce); err != nil {
		return zero, nil, err
	}
	commitments, err := freshV3ZoneNativeCommitments()
	if err != nil {
		return zero, nil, err
	}
	if len(auth.Steps) != len(commitments) || auth.Stage == nil ||
		*auth.Stage < 0 || *auth.Stage >= len(commitments) {
		return zero, nil, errors.New("zone native authorization has an invalid single stage")
	}
	ids := make(map[string]bool, len(auth.Steps)*2)
	for index, step := range auth.Steps {
		if !validMutationIdentity(step.RequestID) || !validMutationIdentity(step.OwnerID) ||
			step.Qualifier != commitments[index].Qualifier || ids[step.RequestID] || ids[step.OwnerID] {
			return zero, nil, fmt.Errorf("zone native authorization step %d is not exact", index)
		}
		ids[step.RequestID], ids[step.OwnerID] = true, true
	}
	state, exists, err := readDNSEngineState()
	if err != nil || !exists || state.Engine != transport.DNSEnginePowerDNS ||
		state.EngineEpoch != 1 || state.PairRole != transport.DNSPairRolePrimary ||
		state.PairLocalIP != "192.0.2.10" || state.PairPeerIP != "192.0.2.11" ||
		state.NativeCatalogV3 != dnsengineartifact.NativeCatalogDebian49V3 {
		return zero, nil, errors.Join(errors.New("fresh V3 paired primary is not the exact active fixture"), err)
	}
	if _, exists, err := readDNSEngineSwitchJournal(); err != nil || exists {
		return zero, nil, errors.Join(errors.New("native V3 switch journal is not retired"), err)
	}
	ready, err := powerDNSPrimaryPairReady(ctx, state)
	if err != nil || !ready {
		return zero, nil, errors.Join(errors.New("native V3 pair is not ready before zone mutation"), err)
	}
	return auth, commitments, nil
}

// Each invocation performs exactly one authorized stage. The controller must
// retain native observations before authorizing the next stage, especially
// between terminal deletion and re-addition.
func TestNativeFreshPDNSPrimaryV3ZoneLifecycle(t *testing.T) {
	if os.Getenv("CELIKPANEL_V3_ZONE_NATIVE_TRIAL") != freshV3NativeCell {
		t.Skip("explicit disposable PowerDNS V3 zone lifecycle only")
	}
	ctx := context.Background()
	auth, commitments, err := freshV3ZoneNativePreflight(ctx)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := agentServiceMutationManager()
	if err != nil {
		t.Fatal(err)
	}
	index := *auth.Stage
	for prior := 0; prior < index; prior++ {
		step, commitment := auth.Steps[prior], commitments[prior]
		job := manager.status(step.RequestID)
		if job == nil || job.Status != serviceMutationStatusSucceeded ||
			job.OwnerID != step.OwnerID || job.Kind != "dns_zone_sync" ||
			job.Target != commitment.Domain || job.PackageName != commitment.Qualifier {
			t.Fatalf("stage %d lacks an exact prior terminal receipt; do not start another zone request: %+v", index, job)
		}
	}
	if index > 0 {
		prior, commitment := auth.Steps[index-1], commitments[index-1]
		state, exists, err := readDNSEngineState()
		if err != nil || !exists {
			t.Fatalf("prior stage state is unavailable: %v", err)
		}
		snapshot, exact, err := readPDNSV3ZoneSnapshot(ctx, pdnsDBPath(), state,
			commitment.Domain, commitment.Qualifier, transport.ServiceMutationBinding{
				MutationRequestID: prior.RequestID, MutationOwnerID: prior.OwnerID,
			})
		if err != nil || !exact || snapshot.Delete != commitment.Delete ||
			snapshot.DesiredGeneration != commitment.DesiredGeneration {
			t.Fatalf("stage %d lacks the exact prior host zone receipt: exact=%v snapshot=%+v err=%v", index, exact, snapshot, err)
		}
	}
	step, commitment := auth.Steps[index], commitments[index]
	job, err := manager.begin(&ServiceMutationBeginRequest{
		RequestID: step.RequestID, OwnerID: step.OwnerID,
		Kind: "dns_zone_sync", Target: commitment.Domain, PackageName: commitment.Qualifier,
	})
	if err != nil || job == nil || job.Status != serviceMutationStatusRunning {
		t.Fatalf("stage %d did not acquire an exact zone lease: job=%+v err=%v", index, job, err)
	}
	request := SyncDNSZoneV3Request{
		ServiceMutationBinding: transport.ServiceMutationBinding{MutationRequestID: step.RequestID, MutationOwnerID: step.OwnerID},
		Engine:                 transport.DNSEnginePowerDNS, EngineEpoch: 1,
		DesiredGeneration: commitment.DesiredGeneration, Domain: commitment.Domain,
		Delete: commitment.Delete, ZoneType: commitment.ZoneType,
		Records: append([]transport.ZoneRecord(nil), commitment.Records...),
	}
	var response SyncDNSZoneV3Response
	if err := (&Agent{}).SyncDNSZoneV3(&request, &response); err != nil {
		t.Fatalf("stage %d RPC: %v", index, err)
	}
	if response.Error != "" || response.RecoveryPending || !response.Synced ||
		response.Engine != transport.DNSEnginePowerDNS || response.EngineEpoch != 1 ||
		response.AppliedGeneration != commitment.DesiredGeneration {
		t.Fatalf("stage %d retained its exact durable request; reconcile it before proceeding: %+v", index, response)
	}
	terminal, err := manager.finish(&ServiceMutationFinishRequest{
		RequestID: step.RequestID, OwnerID: step.OwnerID, Success: true,
	})
	if err != nil || terminal == nil || terminal.Status != serviceMutationStatusSucceeded {
		t.Fatalf("stage %d did not publish terminal receipt: job=%+v err=%v", index, terminal, err)
	}
	state, exists, err := readDNSEngineState()
	if err != nil || !exists || state.Engine != transport.DNSEnginePowerDNS || state.PairRole != transport.DNSPairRolePrimary {
		t.Fatalf("stage %d lost paired producer state: state=%+v err=%v", index, state, err)
	}
	ready, err := powerDNSPrimaryPairReady(ctx, state)
	if err != nil || !ready {
		t.Fatalf("stage %d lost native catalog+member pair readiness: %v", index, err)
	}
	if !commitment.Delete {
		expected, err := expectedDNSZoneAuthorities([]transport.DNSEngineSwitchZoneSnapshot{{
			Domain: commitment.Domain, DesiredGeneration: commitment.DesiredGeneration,
			ZoneType: commitment.ZoneType, Records: commitment.Records,
			ZoneQualifier: commitment.Qualifier,
		}})
		if err != nil {
			t.Fatal(err)
		}
		for _, address := range []string{state.PairLocalIP, state.PairPeerIP} {
			if err := verifyDNSZoneAuthoritiesAt(ctx, address, expected, probeDNSZoneSOA); err != nil {
				t.Fatalf("stage %d native local/peer SOA proof at %s: %v", index, address, err)
			}
		}
	}
	t.Logf("native V3 zone stage %d terminal: request=%s generation=%d delete=%v", index, step.RequestID, commitment.DesiredGeneration, commitment.Delete)
}

// Reopens only the retained stage-2 request after a previous process could not
// classify local deletion. It must not grant a new zone lease or repeat SQL.
func TestNativeFreshPDNSPrimaryV3RecoverDeletedZone(t *testing.T) {
	if os.Getenv("CELIKPANEL_V3_ZONE_NATIVE_TRIAL") != freshV3NativeCell ||
		os.Getenv("CELIKPANEL_V3_ZONE_NATIVE_MODE") != "recover-delete" {
		t.Skip("explicit disposable same-request deletion recovery only")
	}
	auth, commitments, err := freshV3ZoneNativePreflight(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if *auth.Stage != 2 || len(commitments) != 4 {
		t.Fatal("recovery is not bound to deletion stage")
	}
	manager, err := agentServiceMutationManager()
	if err != nil {
		t.Fatal(err)
	}
	step, commitment := auth.Steps[2], commitments[2]
	job := manager.status(step.RequestID)
	if job == nil || job.OwnerID != step.OwnerID || job.Kind != "dns_zone_sync" ||
		job.Target != commitment.Domain || job.PackageName != commitment.Qualifier ||
		job.Status != serviceMutationStatusPending {
		t.Fatalf("exact deletion must remain pending for native peer proof: %+v", job)
	}
	t.Logf("same deletion request reconciled to pending: request=%s code=%s", step.RequestID, job.ErrorCode)
}
