//go:build linux

package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
	"github.com/alicelik/celikpanel/internal/transport"
)

func adoptionWorkerEvidence() dnsenginerecovery.SwitchEvidence {
	request := strings.Repeat("a", 32)
	owner := strings.Repeat("b", 32)
	qualifier := "dns-engine-switch/v1:sha256:" + strings.Repeat("c", 64)
	now := time.Now().UTC()
	return dnsenginerecovery.SwitchEvidence{
		Journal: dnsengineartifact.SwitchJournalV1{
			MutationRequestID: request, MutationOwnerID: owner,
			TargetEngine: transport.DNSEnginePowerDNS, ManifestQualifier: qualifier,
			Phase: dnsengineartifact.SwitchPhaseRollingBack,
		},
		Observation: dnsenginerecovery.EvidenceObservation{
			RequestID: request, Status: dnsenginerecovery.EvidenceActive,
		},
		AcceptedJob: transport.ServiceMutationJob{
			RequestID: request, OwnerID: owner, Kind: "dns_engine_switch",
			Target: string(transport.DNSEnginePowerDNS), PackageName: qualifier,
			Status: servicemutationledger.StatusRunning, Phase: "leased", Attempt: 1,
			StartedAt: now.Add(-time.Minute), UpdatedAt: now.Add(-time.Second),
			LeaseExpiresAt: now.Add(time.Minute), DeadlineAt: now.Add(2 * time.Minute),
		},
	}
}

func TestInstalledPDNSInverseWorkerExclusionRejectsForeignAndRecordedWorker(t *testing.T) {
	evidence := adoptionWorkerEvidence()
	if err := excludeInstalledDNSInverseWorker(context.Background(), evidence); err != nil {
		t.Fatalf("accepted worker-free job rejected: %v", err)
	}
	foreign := evidence
	foreign.AcceptedJob.OwnerID = strings.Repeat("d", 32)
	if err := excludeInstalledDNSInverseWorker(context.Background(), foreign); err == nil {
		t.Fatal("foreign accepted job was admitted")
	}
	recorded := evidence
	recorded.AcceptedJob.WorkerPID = 1
	recorded.AcceptedJob.WorkerStarted = "invalid"
	recorded.AcceptedJob.WorkerCommand = "agent"
	if err := excludeInstalledDNSInverseWorker(context.Background(), recorded); err == nil {
		t.Fatal("unknown recorded worker was admitted")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := excludeInstalledDNSInverseWorker(cancelled, evidence); err == nil {
		t.Fatal("cancelled exclusion was admitted")
	}
}

func TestInstalledPDNSInverseWorkerExclusionRequiresTerminalCheckpoint(t *testing.T) {
	evidence := adoptionWorkerEvidence()
	evidence.Journal.Phase = dnsengineartifact.SwitchPhaseRolledBack
	evidence.Observation.Status = dnsenginerecovery.EvidenceTerminalRolledBack
	evidence.AcceptedJob.WorkerPID = 0
	evidence.AcceptedJob.WorkerStarted = ""
	evidence.AcceptedJob.WorkerCommand = ""
	if err := excludeInstalledDNSInverseWorker(context.Background(), evidence); err != nil {
		t.Fatalf("exact terminal job rejected: %v", err)
	}
	evidence.AcceptedJob.WorkerPID = 42
	if err := excludeInstalledDNSInverseWorker(context.Background(), evidence); err == nil {
		t.Fatal("terminal job with a recorded worker was admitted")
	}
	evidence.AcceptedJob.WorkerPID = 0
	evidence.Journal.Phase = dnsengineartifact.SwitchPhaseRollingBack
	if err := excludeInstalledDNSInverseWorker(context.Background(), evidence); err == nil {
		t.Fatal("terminal job before rollback checkpoint was admitted")
	}
}

func TestInstalledPDNSInverseRejectsInvalidRequestBeforeHostAccess(t *testing.T) {
	if err := completeInstalledPDNSAdoptionInverse(context.Background(), "invalid"); err == nil {
		t.Fatal("invalid request ID reached host access")
	}
}

func TestInstalledPDNSInverseCancellationStopsBeforeHostAccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := completeInstalledPDNSAdoptionInverse(ctx, strings.Repeat("a", 32))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled inverse reached installed paths: %v", err)
	}
}
