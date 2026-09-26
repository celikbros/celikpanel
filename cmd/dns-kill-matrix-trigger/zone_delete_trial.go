package main

// This command is deliberately limited to one disposable paired BIND fixture.
// It exercises the real Agent V3 deletion RPC without inventing a second
// operation after a pending/unknown result.

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	deletionTrialCell   = "bind__intent__after-write__paired-primary__peer-reachable"
	deletionTrialDomain = "s1-kill.test"
	deletionTrialMarker = "/etc/celikpanel-dns-kill-matrix"
)

type deletionTrialResult struct {
	Schema     string `json:"schema"`
	CellID     string `json:"cell_id"`
	RequestID  string `json:"request_id"`
	OwnerID    string `json:"owner_id"`
	Qualifier  string `json:"qualifier"`
	Outcome    string `json:"outcome"`
	JobStatus  string `json:"job_status"`
	JobPhase   string `json:"job_phase"`
	Heartbeats int    `json:"heartbeats"`
	Error      string `json:"error,omitempty"`
}

func deletionTrialRequest(s scenario, switchRequest transport.SwitchDNSEngineV1Request, receipt identityReceipt) (transport.SyncDNSZoneV3Request, transport.ServiceMutationBeginRequest, error) {
	marker, err := os.ReadFile(deletionTrialMarker)
	if err != nil || string(marker) != "schema=celikpanel/dns-kill-fixture-plan/v1\ncell_id="+deletionTrialCell+"\nnode=arch\n" {
		return transport.SyncDNSZoneV3Request{}, transport.ServiceMutationBeginRequest{}, errors.New("not the exact disposable Arch DNS fixture")
	}
	if receipt.Schema != identityReceiptSchema || receipt.CellID != deletionTrialCell ||
		receipt.Driver != "bind" || receipt.SourceFixture != "uninitialized" ||
		!validMutationIdentity(receipt.RequestID) ||
		s.Driver != "bind" || s.SourceFixture != "uninitialized" ||
		s.Mode != transport.DNSEngineSwitchModeSwitch ||
		s.TargetEngine != transport.DNSEngineBIND || s.TargetEpoch != 1 ||
		s.Topology != transport.DNSTopologyPaired || s.PairRole != "primary" ||
		s.LocalIP != "192.0.2.11" || s.PeerIP != "192.0.2.10" ||
		len(switchRequest.Zones) != 1 ||
		switchRequest.Zones[0].Domain != deletionTrialDomain ||
		switchRequest.Zones[0].Delete ||
		switchRequest.Zones[0].DesiredGeneration != 1 ||
		receipt.ManifestQualifier != switchRequest.ManifestQualifier {
		return transport.SyncDNSZoneV3Request{}, transport.ServiceMutationBeginRequest{}, errors.New("deletion trial source identity is not exact")
	}
	requestID := deriveNormalizationRequestIdentity(receipt.RequestID, "zone-delete/"+deletionTrialDomain)
	ownerID, err := deterministicOwnerIdentity(receipt.CellID, requestID)
	if err != nil {
		return transport.SyncDNSZoneV3Request{}, transport.ServiceMutationBeginRequest{}, err
	}
	commitment, err := mutationpayload.CanonicalDNSZoneSyncV3(
		transport.DNSEngineBIND, 1, 2, deletionTrialDomain, true,
		switchRequest.Zones[0].ZoneType, nil,
	)
	if err != nil {
		return transport.SyncDNSZoneV3Request{}, transport.ServiceMutationBeginRequest{}, err
	}
	request := transport.SyncDNSZoneV3Request{
		ServiceMutationBinding: transport.ServiceMutationBinding{
			MutationRequestID: requestID, MutationOwnerID: ownerID,
		},
		Engine: transport.DNSEngineBIND, EngineEpoch: 1,
		DesiredGeneration: 2, Domain: deletionTrialDomain,
		Delete: true, ZoneType: commitment.ZoneType,
	}
	begin := transport.ServiceMutationBeginRequest{
		RequestID: requestID, OwnerID: ownerID,
		Kind: mutationKindDNSZoneSync, Target: deletionTrialDomain,
		PackageName: commitment.Qualifier,
	}
	return request, begin, nil
}

func runRPCDeleteV3Command(arguments []string) {
	flags := flag.NewFlagSet("rpc-delete-v3", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	scenarioPath := flags.String("scenario", "", "exact fixture scenario")
	identityPath := flags.String("identity-receipt", "", "prior switch identity receipt")
	timeout := flags.Duration("timeout", 2*time.Minute, "bounded deletion time")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 ||
		*scenarioPath == "" || *identityPath == "" || *timeout <= 0 {
		usageError("rpc-delete-v3 arguments are invalid")
	}
	receipt, err := readIdentityReceipt(*identityPath)
	if err != nil {
		emitAndExit(triggerEvent{Event: "delete-identity-rejected", Error: err.Error()}, exitUsage)
	}
	source, switchRequest, err := loadScenario(*scenarioPath, "bind")
	if err != nil {
		emitAndExit(triggerEvent{Event: "delete-scenario-rejected", Error: err.Error()}, exitUsage)
	}
	request, begin, err := deletionTrialRequest(source, switchRequest, receipt)
	if err != nil {
		emitAndExit(triggerEvent{Event: "delete-trial-rejected", Error: err.Error()}, exitUsage)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	result, err := runRPCDeleteV3(ctx, request, begin, callProductionAgent)
	if err != nil {
		result.Outcome = "unverified_exact_operation"
		result.Error = err.Error()
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		emitAndExit(triggerEvent{Event: "delete-result-encode-failed", Error: err.Error()}, exitUncertain)
	}
	_, _ = os.Stdout.Write(append(encoded, '\n'))
	if result.Error != "" {
		os.Exit(exitUncertain)
	}
}

// Kept injectable so local tests can assert that pending does not call Finish.
func runRPCDeleteV3(ctx context.Context, request transport.SyncDNSZoneV3Request, begin transport.ServiceMutationBeginRequest, call rpcCallFunc) (deletionTrialResult, error) {
	result := deletionTrialResult{
		Schema: "celikpanel-dns-v3-native-delete-trial/v1", CellID: deletionTrialCell,
		RequestID: begin.RequestID, OwnerID: begin.OwnerID, Qualifier: begin.PackageName,
	}
	if call == nil || ctx == nil || !validMutationIdentity(begin.RequestID) ||
		!validMutationIdentity(begin.OwnerID) || request.Domain != deletionTrialDomain ||
		!request.Delete || request.DesiredGeneration != 2 ||
		request.MutationRequestID != begin.RequestID ||
		request.MutationOwnerID != begin.OwnerID ||
		begin.Kind != mutationKindDNSZoneSync || begin.Target != deletionTrialDomain {
		return result, errors.New("invalid exact native deletion operation")
	}
	var started transport.ServiceMutationResponse
	if err := call(ctx, "Agent.BeginServiceMutation", &begin, &started); err != nil {
		return result, fmt.Errorf("begin exact deletion: %w", err)
	}
	if err := responseError(started); err != nil {
		return result, fmt.Errorf("begin exact deletion: %w", err)
	}
	if err := validateRunningJob(started.Job, begin, true); err != nil {
		return result, err
	}
	heartbeatCtx, stopHeartbeat := context.WithCancel(ctx)
	done := startHeartbeat(heartbeatCtx, begin, heartbeatIntervalDefault, call)
	var published transport.SyncDNSZoneV3Response
	callErr := call(ctx, "Agent.SyncDNSZoneV3", &request, &published)
	stopHeartbeat()
	heartbeat := <-done
	result.Heartbeats = heartbeat.count
	var observed transport.ServiceMutationResponse
	statusErr := call(ctx, "Agent.ServiceMutationStatus", &transport.ServiceMutationStatusRequest{RequestID: begin.RequestID}, &observed)
	if statusErr != nil {
		return result, fmt.Errorf("deletion result unobserved: %w", statusErr)
	}
	if err := responseError(observed); err != nil {
		return result, fmt.Errorf("deletion status unavailable: %w", err)
	}
	if observed.Job == nil || observed.Job.RequestID != begin.RequestID ||
		observed.Job.OwnerID != begin.OwnerID || observed.Job.Kind != begin.Kind ||
		observed.Job.Target != begin.Target || observed.Job.PackageName != begin.PackageName {
		return result, errors.New("deletion status does not match the accepted operation")
	}
	result.JobStatus, result.JobPhase = observed.Job.Status, observed.Job.Phase
	if callErr != nil {
		return result, fmt.Errorf("V3 deletion RPC outcome unknown: %w", callErr)
	}
	if published.Error != "" {
		return result, errors.New("V3 deletion was refused; preserve its exact job for owner review")
	}
	pendingPhase := "commit/dns-zone-sync/v3/propagation-pending/" + begin.RequestID + "/" + deletionTrialDomain + "/" + begin.PackageName
	if published.RecoveryPending && !published.Synced &&
		published.Engine == request.Engine && published.EngineEpoch == request.EngineEpoch &&
		published.AppliedGeneration == request.DesiredGeneration &&
		observed.Job.Status == "pending" && observed.Job.Phase == pendingPhase {
		result.Outcome = "pending_exact_operation"
		return result, nil
	}
	if !published.Synced || published.RecoveryPending ||
		published.Engine != request.Engine || published.EngineEpoch != request.EngineEpoch ||
		published.AppliedGeneration != request.DesiredGeneration {
		return result, errors.New("V3 deletion response is mixed or unverified")
	}
	terminal, err := finishMutationWithResponse(ctx, begin, true, call)
	if err != nil {
		return result, fmt.Errorf("finish exact deletion: %w", err)
	}
	publishedPhase := "commit/dns-zone-sync/v3/published/" + begin.RequestID + "/" + deletionTrialDomain + "/" + begin.PackageName
	if err := validateSucceededJobAtPhase(terminal, begin, publishedPhase); err != nil {
		return result, err
	}
	result.Outcome, result.JobStatus, result.JobPhase = "verified_published", terminal.Status, terminal.Phase
	return result, nil
}
