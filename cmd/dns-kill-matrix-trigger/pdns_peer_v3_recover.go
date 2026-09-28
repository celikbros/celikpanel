package main

// Disposable BIND-primary/PowerDNS-secondary exact V3 deletion recovery.
// This never calls SyncDNSZoneV3 or creates another mutation identity.

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"time"

	"github.com/alicelik/celikpanel/internal/pdnspeerjournal"
	"github.com/alicelik/celikpanel/internal/transport"
)

func runRPCPDNSPeerV3RecoverCommand(arguments []string) {
	flags := flag.NewFlagSet("rpc-pdns-peer-v3-recover", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	timeout := flags.Duration("timeout", 2*time.Minute, "bounded exact recovery")
	if flags.Parse(arguments) != nil || flags.NArg() != 0 || *timeout <= 0 {
		usageError("rpc-pdns-peer-v3-recover arguments are invalid")
	}
	request, begin, err := pdnsPeerV3Preflight("delete")
	if err != nil {
		emitAndExit(triggerEvent{Event: "pdns-peer-v3-recover-preflight-refused", Error: err.Error()}, exitUsage)
	}
	result := pdnsPeerV3Result{Schema: "celikpanel/native-pdns-peer-v3/v1", CellID: pdnsPeerV3Cell, Step: "delete-recover", RequestID: begin.RequestID, OwnerID: begin.OwnerID, Qualifier: begin.PackageName}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	result.Outcome, result.JobStatus, result.JobPhase, result.Heartbeats, err = recoverPDNSPeerV3(ctx, request, begin, callProductionAgent)
	if err != nil {
		result.Error = err.Error()
	}
	encoded, _ := json.Marshal(result)
	_, _ = os.Stdout.Write(append(encoded, '\n'))
	if err != nil || result.Outcome != "verified_published" {
		os.Exit(exitUncertain)
	}
}

func recoverPDNSPeerV3(ctx context.Context, request transport.SyncDNSZoneV3Request,
	begin transport.ServiceMutationBeginRequest, call rpcCallFunc) (string, string, string, int, error) {
	return recoverPDNSPeerV3At(ctx, request, begin, call, func() bool {
		_, err := pdnspeerjournal.Read()
		return pdnspeerjournal.IsCode(err, pdnspeerjournal.Missing)
	})
}

func recoverPDNSPeerV3At(ctx context.Context, request transport.SyncDNSZoneV3Request,
	begin transport.ServiceMutationBeginRequest, call rpcCallFunc, retired func() bool) (string, string, string, int, error) {
	if ctx == nil || call == nil || !request.Delete || request.DesiredGeneration != 3 ||
		request.Engine != transport.DNSEngineBIND || request.EngineEpoch != 1 ||
		request.Domain != pdnsPeerV3Zone || request.MutationRequestID != begin.RequestID ||
		request.MutationOwnerID != begin.OwnerID || begin.Resume ||
		begin.Kind != mutationKindDNSZoneSync || begin.Target != pdnsPeerV3Zone ||
		begin.PackageName == "" || !validMutationIdentity(begin.RequestID) ||
		!validMutationIdentity(begin.OwnerID) {
		return "", "", "", 0, errors.New("invalid exact PDNS-peer V3 recovery identity")
	}
	var before transport.ServiceMutationResponse
	if err := call(ctx, "Agent.ServiceMutationStatus", &transport.ServiceMutationStatusRequest{RequestID: begin.RequestID}, &before); err != nil {
		return "", "", "", 0, err
	}
	if err := responseError(before); err != nil {
		return "", "", "", 0, err
	}
	if !exactPendingDeletionJob(before.Job, begin) {
		return "", "", "", 0, errors.New("exact PDNS-peer deletion is not pending at its reviewed propagation phase")
	}
	resume := begin
	resume.Resume = true
	var started transport.ServiceMutationResponse
	if err := call(ctx, "Agent.BeginServiceMutation", &resume, &started); err != nil {
		return "", before.Job.Status, before.Job.Phase, 0, err
	}
	if err := responseError(started); err != nil {
		return "", before.Job.Status, before.Job.Phase, 0, err
	}
	if err := validateRunningJob(started.Job, begin, false); err != nil || started.Job.Phase != deletionTrialRecoveringPhase(begin) {
		return "", before.Job.Status, before.Job.Phase, 0, errors.New("resumed deletion lacks exact recovering phase and lease")
	}
	heartbeatCtx, stopHeartbeat := context.WithCancel(ctx)
	done := startHeartbeat(heartbeatCtx, begin, heartbeatIntervalDefault, call)
	recovery := transport.RecoverDNSZoneV3Request{ServiceMutationBinding: request.ServiceMutationBinding, Domain: pdnsPeerV3Zone, Qualifier: begin.PackageName}
	var recovered transport.RecoverDNSZoneV3Response
	callErr := call(ctx, "Agent.RecoverDNSZoneV3", &recovery, &recovered)
	stopHeartbeat()
	heartbeat := <-done
	statusCtx, statusCancel := context.WithTimeout(context.WithoutCancel(ctx), controlTimeout)
	defer statusCancel()
	var observed transport.ServiceMutationResponse
	if err := call(statusCtx, "Agent.ServiceMutationStatus", &transport.ServiceMutationStatusRequest{RequestID: begin.RequestID}, &observed); err != nil {
		return "unknown", "", "", heartbeat.count, err
	}
	if err := responseError(observed); err != nil {
		return "unknown", "", "", heartbeat.count, err
	}
	if observed.Job == nil || !jobIdentityMatches(observed.Job, begin) {
		return "unknown", "", "", heartbeat.count, errors.New("recovery status identity differs")
	}
	status, phase := observed.Job.Status, observed.Job.Phase
	if callErr != nil {
		return "unknown", status, phase, heartbeat.count, callErr
	}
	if recovered.Error != "" {
		return "refused", status, phase, heartbeat.count, errors.New("V3 recovery refused; preserve exact job")
	}
	if recovered.RecoveryPending && !recovered.Recovered && exactPendingDeletionJob(observed.Job, begin) {
		if heartbeat.err != nil {
			return "unknown", status, phase, heartbeat.count, errors.New("recovery heartbeat outcome unknown; preserve exact job")
		}
		return "pending_exact_operation", status, phase, heartbeat.count, nil
	}
	if recovered.Recovered && !recovered.RecoveryPending {
		if err := validateSucceededJobAtPhase(observed.Job, begin, deletionTrialPublishedPhase(begin)); err != nil {
			return "unknown", status, phase, heartbeat.count, err
		}
		if retired == nil || !retired() {
			return "unknown", status, phase, heartbeat.count, errors.New("terminal ledger exists but PowerDNS challenge is not retired")
		}
		return "verified_published", status, phase, heartbeat.count, nil
	}
	return "unknown", status, phase, heartbeat.count, errors.New("V3 recovery response or durable status is mixed")
}
