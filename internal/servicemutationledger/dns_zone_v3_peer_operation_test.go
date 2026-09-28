package servicemutationledger

import (
	"strings"
	"testing"
	"time"
)

const (
	peerProofTestRequest = "11111111111111111111111111111111"
	peerProofTestOwner   = "22222222222222222222222222222222"
	peerProofTestDomain  = "child.example.test"
)

var peerProofTestQualifier = "dns-zone-sync/v3:sha256:" + strings.Repeat("a", 64)

func peerProofTestLedger(t *testing.T, state, status string, attempt int) *Ledger {
	t.Helper()
	phase, err := FormatDNSZoneSyncV3Phase(
		state, peerProofTestRequest, peerProofTestDomain, peerProofTestQualifier,
	)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	job := &ServiceMutationJob{
		RequestID: peerProofTestRequest, OwnerID: peerProofTestOwner,
		Kind: "dns_zone_sync", Target: peerProofTestDomain,
		PackageName: peerProofTestQualifier, Status: status, Phase: phase,
		Attempt: attempt, StartedAt: now, UpdatedAt: now, DeadlineAt: now.Add(time.Hour),
	}
	ledger := &Ledger{
		Version: Version,
		Jobs:    map[string]*ServiceMutationJob{peerProofTestRequest: job},
	}
	if StatusActive(status) {
		ledger.ActiveRequestID = peerProofTestRequest
		job.LeaseExpiresAt = now.Add(time.Minute)
	} else {
		job.FinishedAt = now
	}
	if err := Validate(ledger); err != nil {
		t.Fatal(err)
	}
	return ledger
}

func classifyPeerProofTest(ledger *Ledger, attempt int) DNSZoneV3PeerOperation {
	return ClassifyDNSZoneV3PeerOperation(
		ledger, peerProofTestRequest, peerProofTestOwner,
		peerProofTestDomain, peerProofTestQualifier, attempt,
	)
}

func TestDNSZoneV3PeerOperationExactLifecycle(t *testing.T) {
	for _, tc := range []struct {
		name, state, status string
		attempt             int
		want                DNSZoneV3PeerOperation
	}{
		{"initial applied", DnsZoneSyncV3Applied, StatusRunning, 1, DNSZoneV3PeerOperationActiveApplied},
		{"cancelling applied", DnsZoneSyncV3Applied, StatusCancelling, 1, DNSZoneV3PeerOperationActiveApplied},
		{"orphaned applied", DnsZoneSyncV3Applied, StatusOrphaned, 1, DNSZoneV3PeerOperationMismatch},
		{"owner retry recovering", DnsZoneSyncV3Recovering, StatusRunning, 2, DNSZoneV3PeerOperationActiveRecovering},
		{"orphaned recovering", DnsZoneSyncV3Recovering, StatusOrphaned, 2, DNSZoneV3PeerOperationMismatch},
		{"pending requires retry", DnsZoneSyncV3PropagationPending, StatusPending, 1, DNSZoneV3PeerOperationPendingRetryRequired},
		{"historical published", DnsZoneSyncV3Published, StatusSucceeded, 2, DNSZoneV3PeerOperationHistoricalPublished},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ledger := peerProofTestLedger(t, tc.state, tc.status, tc.attempt)
			if got := classifyPeerProofTest(ledger, tc.attempt); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDNSZoneV3PeerOperationRejectsForeignOrStaleAttempt(t *testing.T) {
	ledger := peerProofTestLedger(t, DnsZoneSyncV3Recovering, StatusRunning, 2)
	for name, request := range map[string]struct {
		requestID, ownerID, domain, qualifier string
		attempt                               int
	}{
		"wrong request":      {strings.Repeat("3", 32), peerProofTestOwner, peerProofTestDomain, peerProofTestQualifier, 2},
		"cross owner":        {peerProofTestRequest, strings.Repeat("3", 32), peerProofTestDomain, peerProofTestQualifier, 2},
		"wrong domain":       {peerProofTestRequest, peerProofTestOwner, "other.example.test", peerProofTestQualifier, 2},
		"wrong qualifier":    {peerProofTestRequest, peerProofTestOwner, peerProofTestDomain, "dns-zone-sync/v3:sha256:" + strings.Repeat("b", 64), 2},
		"old attempt replay": {peerProofTestRequest, peerProofTestOwner, peerProofTestDomain, peerProofTestQualifier, 1},
		"future attempt":     {peerProofTestRequest, peerProofTestOwner, peerProofTestDomain, peerProofTestQualifier, 3},
		"zero attempt":       {peerProofTestRequest, peerProofTestOwner, peerProofTestDomain, peerProofTestQualifier, 0},
	} {
		t.Run(name, func(t *testing.T) {
			got := ClassifyDNSZoneV3PeerOperation(ledger,
				request.requestID, request.ownerID, request.domain,
				request.qualifier, request.attempt)
			if got != DNSZoneV3PeerOperationMismatch {
				t.Fatalf("foreign or stale operation classified as %q", got)
			}
		})
	}
	ledger = peerProofTestLedger(t, DnsZoneSyncV3Published, StatusSucceeded, 2)
	if got := classifyPeerProofTest(ledger, 1); got != DNSZoneV3PeerOperationMismatch {
		t.Fatalf("old attempt after publication classified as %q", got)
	}
}

func TestDNSZoneV3PeerOperationRejectsSpoofedStatusPhaseAndPointer(t *testing.T) {
	for name, edit := range map[string]func(*Ledger){
		"lost active pointer":  func(l *Ledger) { l.ActiveRequestID = "" },
		"wrong active pointer": func(l *Ledger) { l.ActiveRequestID = strings.Repeat("3", 32) },
		"wrong status":         func(l *Ledger) { l.Jobs[peerProofTestRequest].Status = StatusPending },
		"published phase while active": func(l *Ledger) {
			l.Jobs[peerProofTestRequest].Phase, _ = FormatDNSZoneSyncV3Phase(
				DnsZoneSyncV3Published, peerProofTestRequest, peerProofTestDomain, peerProofTestQualifier)
		},
		"foreign phase identity": func(l *Ledger) {
			l.Jobs[peerProofTestRequest].Phase, _ = FormatDNSZoneSyncV3Phase(
				DnsZoneSyncV3Applied, strings.Repeat("3", 32), peerProofTestDomain, peerProofTestQualifier)
		},
		"foreign durable owner": func(l *Ledger) { l.Jobs[peerProofTestRequest].OwnerID = strings.Repeat("3", 32) },
	} {
		t.Run(name, func(t *testing.T) {
			ledger := peerProofTestLedger(t, DnsZoneSyncV3Applied, StatusRunning, 1)
			edit(ledger)
			if got := classifyPeerProofTest(ledger, 1); got != DNSZoneV3PeerOperationMismatch {
				t.Fatalf("spoofed ledger classified as %q", got)
			}
		})
	}
	if got := classifyPeerProofTest(nil, 1); got != DNSZoneV3PeerOperationMismatch {
		t.Fatalf("nil ledger classified as %q", got)
	}
}
