package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

func pdnsRecoverFixture(t *testing.T) (transport.SyncDNSZoneV3Request, transport.ServiceMutationBeginRequest) {
	t.Helper()
	source, receipt, state := pdnsPeerV3Fixture()
	request, begin, err := pdnsPeerV3Request("delete", source, receipt, state)
	if err != nil {
		t.Fatal(err)
	}
	return request, begin
}

func TestPDNSPeerRecoverRejectsWrongIdentityBeforeRPC(t *testing.T) {
	request, begin := pdnsRecoverFixture(t)
	request.MutationOwnerID = "ffffffffffffffffffffffffffffffff"
	called := false
	call := func(context.Context, string, any, any) error { called = true; return nil }
	if _, _, _, _, err := recoverPDNSPeerV3At(context.Background(), request, begin, call, func() bool { return true }); err == nil || called {
		t.Fatalf("wrong identity reached RPC: %v", err)
	}
}

func TestPDNSPeerRecoverRejectsNonpendingBeforeBegin(t *testing.T) {
	request, begin := pdnsRecoverFixture(t)
	for _, status := range []string{"succeeded", "failed", "running"} {
		t.Run(status, func(t *testing.T) {
			calls := 0
			call := func(_ context.Context, method string, _ any, out any) error {
				calls++
				if method != "Agent.ServiceMutationStatus" {
					return errors.New("unexpected mutation")
				}
				job := pendingDeletionTrialJob(begin)
				job.Status = status
				out.(*transport.ServiceMutationResponse).Job = job
				return nil
			}
			if _, _, _, _, err := recoverPDNSPeerV3At(context.Background(), request, begin, call, func() bool { return true }); err == nil || calls != 1 {
				t.Fatalf("nonpending accepted: calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestPDNSPeerRecoverRequiresTerminalAndRetiredJournal(t *testing.T) {
	request, begin := pdnsRecoverFixture(t)
	for _, retired := range []bool{false, true} {
		t.Run(map[bool]string{false: "journal-outstanding", true: "journal-retired"}[retired], func(t *testing.T) {
			statuses := 0
			call := func(_ context.Context, method string, input, out any) error {
				switch method {
				case "Agent.ServiceMutationStatus":
					if input.(*transport.ServiceMutationStatusRequest).RequestID != begin.RequestID {
						return errors.New("wrong request")
					}
					statuses++
					if statuses == 1 {
						out.(*transport.ServiceMutationResponse).Job = pendingDeletionTrialJob(begin)
					} else {
						job := pendingDeletionTrialJob(begin)
						job.Status = "succeeded"
						job.Phase = deletionTrialPublishedPhase(begin)
						job.FinishedAt = time.Now()
						out.(*transport.ServiceMutationResponse).Job = job
					}
				case "Agent.BeginServiceMutation":
					got := input.(*transport.ServiceMutationBeginRequest)
					if !got.Resume || got.RequestID != begin.RequestID || got.OwnerID != begin.OwnerID {
						return errors.New("not same request")
					}
					out.(*transport.ServiceMutationResponse).Job = recoveringDeletionTrialJob(begin)
				case "Agent.RecoverDNSZoneV3":
					got := input.(*transport.RecoverDNSZoneV3Request)
					if got.MutationRequestID != begin.RequestID || got.MutationOwnerID != begin.OwnerID || got.Qualifier != begin.PackageName {
						return errors.New("wrong recovery context")
					}
					out.(*transport.RecoverDNSZoneV3Response).Recovered = true
				default:
					return errors.New("unexpected mutation: " + method)
				}
				return nil
			}
			outcome, status, _, _, err := recoverPDNSPeerV3At(context.Background(), request, begin, call, func() bool { return retired })
			if retired {
				if err != nil || outcome != "verified_published" || status != "succeeded" || statuses != 2 {
					t.Fatalf("terminal rejected: outcome=%s status=%s statuses=%d err=%v", outcome, status, statuses, err)
				}
			} else if err == nil || outcome == "verified_published" {
				t.Fatalf("outstanding journal accepted: outcome=%s err=%v", outcome, err)
			}
		})
	}
}
