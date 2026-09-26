package main

import (
	"context"
	"errors"
	"testing"

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
