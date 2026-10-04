package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

func testDeletionTrial(t *testing.T) (transport.SyncDNSZoneV3Request, transport.ServiceMutationBeginRequest) {
	t.Helper()
	commitment, err := mutationpayload.CanonicalDNSZoneSyncV3(
		transport.DNSEngineBIND, 1, 2, deletionTrialDomain, true, "NATIVE", nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	begin := transport.ServiceMutationBeginRequest{
		RequestID: "11111111111111111111111111111111",
		OwnerID:   "22222222222222222222222222222222",
		Kind:      mutationKindDNSZoneSync, Target: deletionTrialDomain,
		PackageName: commitment.Qualifier,
	}
	request := transport.SyncDNSZoneV3Request{
		ServiceMutationBinding: transport.ServiceMutationBinding{
			MutationRequestID: begin.RequestID, MutationOwnerID: begin.OwnerID,
		},
		Engine: transport.DNSEngineBIND, EngineEpoch: 1,
		DesiredGeneration: 2, Domain: deletionTrialDomain,
		Delete: true, ZoneType: "NATIVE",
	}
	return request, begin
}

func TestNativeV3DeletePendingKeepsExactOperationWithoutFinish(t *testing.T) {
	request, begin := testDeletionTrial(t)
	methods := []string{}
	call := func(_ context.Context, method string, input, output any) error {
		methods = append(methods, method)
		switch method {
		case "Agent.BeginServiceMutation":
			if *input.(*transport.ServiceMutationBeginRequest) != begin {
				return errors.New("wrong begin")
			}
			output.(*transport.ServiceMutationResponse).Job = runningJob(begin)
		case "Agent.SyncDNSZoneV3":
			zone := input.(*transport.SyncDNSZoneV3Request)
			if zone.Domain != request.Domain || !zone.Delete || len(zone.Records) != 0 ||
				zone.MutationRequestID != begin.RequestID || zone.MutationOwnerID != begin.OwnerID {
				return errors.New("wrong delete")
			}
			*output.(*transport.SyncDNSZoneV3Response) = transport.SyncDNSZoneV3Response{
				RecoveryPending: true, Engine: transport.DNSEngineBIND,
				EngineEpoch: 1, AppliedGeneration: 2,
			}
		case "Agent.ServiceMutationStatus":
			if input.(*transport.ServiceMutationStatusRequest).RequestID != begin.RequestID {
				return errors.New("wrong status")
			}
			job := runningJob(begin)
			job.Status = "pending"
			job.Phase = "commit/dns-zone-sync/v3/propagation-pending/" + begin.RequestID + "/" + deletionTrialDomain + "/" + begin.PackageName
			output.(*transport.ServiceMutationResponse).Job = job
		default:
			return errors.New("unexpected second mutation or Finish: " + method)
		}
		return nil
	}
	result, err := runRPCDeleteV3(context.Background(), request, begin, call)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != "pending_exact_operation" || result.JobStatus != "pending" || len(methods) != 3 {
		t.Fatalf("result=%+v methods=%v", result, methods)
	}
}

func TestNativeV3DeleteRejectsMismatchedPendingStatusWithoutFinish(t *testing.T) {
	request, begin := testDeletionTrial(t)
	methods := []string{}
	call := func(_ context.Context, method string, _ any, output any) error {
		methods = append(methods, method)
		switch method {
		case "Agent.BeginServiceMutation":
			output.(*transport.ServiceMutationResponse).Job = runningJob(begin)
		case "Agent.SyncDNSZoneV3":
			*output.(*transport.SyncDNSZoneV3Response) = transport.SyncDNSZoneV3Response{
				RecoveryPending: true, Engine: transport.DNSEngineBIND,
				EngineEpoch: 1, AppliedGeneration: 2,
			}
		case "Agent.ServiceMutationStatus":
			job := runningJob(begin)
			job.Status, job.Phase = "pending", "wrong-phase"
			output.(*transport.ServiceMutationResponse).Job = job
		default:
			return errors.New("unexpected mutation: " + method)
		}
		return nil
	}
	if _, err := runRPCDeleteV3(context.Background(), request, begin, call); err == nil {
		t.Fatal("mismatched pending phase was accepted")
	}
	if len(methods) != 3 {
		t.Fatalf("methods=%v", methods)
	}
}

func pendingDeletionTrialJob(begin transport.ServiceMutationBeginRequest) *transport.ServiceMutationJob {
	job := runningJob(begin)
	job.Status = "pending"
	job.Phase = deletionTrialPendingPhase(begin)
	job.LeaseExpiresAt = time.Time{}
	job.FinishedAt = job.UpdatedAt
	return job
}

func recoveringDeletionTrialJob(begin transport.ServiceMutationBeginRequest) *transport.ServiceMutationJob {
	job := runningJob(begin)
	job.Phase = deletionTrialRecoveringPhase(begin)
	return job
}

func TestNativeV3RecoverPendingKeepsSameOperationWithoutFinish(t *testing.T) {
	request, begin := testDeletionTrial(t)
	methods := []string{}
	call := func(_ context.Context, method string, input, output any) error {
		methods = append(methods, method)
		switch method {
		case "Agent.ServiceMutationStatus":
			if input.(*transport.ServiceMutationStatusRequest).RequestID != begin.RequestID {
				return errors.New("wrong status identity")
			}
			output.(*transport.ServiceMutationResponse).Job = pendingDeletionTrialJob(begin)
		case "Agent.BeginServiceMutation":
			got := *input.(*transport.ServiceMutationBeginRequest)
			if !got.Resume || got.RequestID != begin.RequestID || got.OwnerID != begin.OwnerID ||
				got.Kind != begin.Kind || got.Target != begin.Target || got.PackageName != begin.PackageName {
				return errors.New("new or mismatched mutation identity")
			}
			output.(*transport.ServiceMutationResponse).Job = recoveringDeletionTrialJob(begin)
		case "Agent.RecoverDNSZoneV3":
			got := input.(*transport.RecoverDNSZoneV3Request)
			if got.MutationRequestID != begin.RequestID || got.MutationOwnerID != begin.OwnerID ||
				got.Domain != deletionTrialDomain || got.Qualifier != begin.PackageName {
				return errors.New("wrong recovery binding")
			}
			*output.(*transport.RecoverDNSZoneV3Response) = transport.RecoverDNSZoneV3Response{RecoveryPending: true}
		default:
			return errors.New("unexpected new mutation or Finish: " + method)
		}
		return nil
	}
	result, err := runRPCDeleteV3Recover(context.Background(), request, begin, call)
	if err != nil || result.Outcome != "pending_exact_operation" || result.JobStatus != "pending" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	want := []string{"Agent.ServiceMutationStatus", "Agent.BeginServiceMutation", "Agent.RecoverDNSZoneV3", "Agent.ServiceMutationStatus"}
	if len(methods) != len(want) {
		t.Fatalf("methods=%v", methods)
	}
	for i := range want {
		if methods[i] != want[i] {
			t.Fatalf("methods=%v", methods)
		}
	}
}

func TestNativeV3RecoverTerminalNeedsExactPublishedReceipt(t *testing.T) {
	request, begin := testDeletionTrial(t)
	statuses := 0
	call := func(_ context.Context, method string, input, output any) error {
		switch method {
		case "Agent.ServiceMutationStatus":
			statuses++
			if statuses == 1 {
				output.(*transport.ServiceMutationResponse).Job = pendingDeletionTrialJob(begin)
			} else {
				job := succeededJob(begin)
				job.Phase = deletionTrialPublishedPhase(begin)
				output.(*transport.ServiceMutationResponse).Job = job
			}
		case "Agent.BeginServiceMutation":
			if !input.(*transport.ServiceMutationBeginRequest).Resume {
				return errors.New("resume flag absent")
			}
			output.(*transport.ServiceMutationResponse).Job = recoveringDeletionTrialJob(begin)
		case "Agent.RecoverDNSZoneV3":
			*output.(*transport.RecoverDNSZoneV3Response) = transport.RecoverDNSZoneV3Response{Recovered: true}
		default:
			return errors.New("unexpected mutation or Finish: " + method)
		}
		return nil
	}
	result, err := runRPCDeleteV3Recover(context.Background(), request, begin, call)
	if err != nil || result.Outcome != "verified_published" || result.JobStatus != "succeeded" ||
		result.JobPhase != deletionTrialPublishedPhase(begin) || statuses != 2 {
		t.Fatalf("result=%+v err=%v statuses=%d", result, err, statuses)
	}
}

func TestNativeV3RecoverRejectsWrongPriorStatusBeforeMutation(t *testing.T) {
	request, begin := testDeletionTrial(t)
	for _, change := range []struct {
		name string
		edit func(*transport.ServiceMutationJob)
	}{
		{"phase", func(job *transport.ServiceMutationJob) { job.Phase = "wrong" }},
		{"owner", func(job *transport.ServiceMutationJob) { job.OwnerID = "33333333333333333333333333333333" }},
		{"terminal", func(job *transport.ServiceMutationJob) { job.Status = "succeeded" }},
	} {
		t.Run(change.name, func(t *testing.T) {
			calls := 0
			call := func(_ context.Context, method string, _ any, output any) error {
				calls++
				if method != "Agent.ServiceMutationStatus" {
					return errors.New("recovery mutated before exact pending check")
				}
				job := pendingDeletionTrialJob(begin)
				change.edit(job)
				output.(*transport.ServiceMutationResponse).Job = job
				return nil
			}
			if _, err := runRPCDeleteV3Recover(context.Background(), request, begin, call); err == nil || calls != 1 {
				t.Fatalf("wrong prior status accepted, calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestNativeV3RecoverUnknownDoesNotFinish(t *testing.T) {
	request, begin := testDeletionTrial(t)
	statuses := 0
	call := func(_ context.Context, method string, _ any, output any) error {
		switch method {
		case "Agent.ServiceMutationStatus":
			statuses++
			output.(*transport.ServiceMutationResponse).Job = pendingDeletionTrialJob(begin)
		case "Agent.BeginServiceMutation":
			output.(*transport.ServiceMutationResponse).Job = recoveringDeletionTrialJob(begin)
		case "Agent.RecoverDNSZoneV3":
			return errors.New("socket lost after recovery request")
		default:
			return errors.New("unexpected mutation or Finish: " + method)
		}
		return nil
	}
	result, err := runRPCDeleteV3Recover(context.Background(), request, begin, call)
	if err == nil || result.JobStatus != "pending" || statuses != 2 {
		t.Fatalf("unknown recovery was accepted: result=%+v err=%v statuses=%d", result, err, statuses)
	}
}

func TestNativeV3RecoverRejectsWrongResumedPhaseBeforeRecovery(t *testing.T) {
	request, begin := testDeletionTrial(t)
	methods := []string{}
	call := func(_ context.Context, method string, _ any, output any) error {
		methods = append(methods, method)
		switch method {
		case "Agent.ServiceMutationStatus":
			output.(*transport.ServiceMutationResponse).Job = pendingDeletionTrialJob(begin)
		case "Agent.BeginServiceMutation":
			output.(*transport.ServiceMutationResponse).Job = runningJob(begin) // leased is not recovering
		default:
			return errors.New("recovery or Finish was called after wrong resumed phase")
		}
		return nil
	}
	if _, err := runRPCDeleteV3Recover(context.Background(), request, begin, call); err == nil ||
		len(methods) != 2 || methods[1] != "Agent.BeginServiceMutation" {
		t.Fatalf("wrong resumed phase accepted: methods=%v err=%v", methods, err)
	}
}
