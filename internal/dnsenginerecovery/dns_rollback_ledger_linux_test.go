//go:build linux

package dnsenginerecovery

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/processidentity"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
	"github.com/alicelik/celikpanel/internal/transport"
)

func rollbackVerdictFixture(t *testing.T) (dnsengineartifact.JournalPolicy, servicemutationledger.FileOwner, dnsengineartifact.SwitchJournalV1, time.Time, string, string) {
	t.Helper()
	policy, owner, journal, _ := adoptionStateRemovalFixture(t)
	journal.Phase = dnsengineartifact.SwitchPhaseRolledBack
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	started := now.Add(-time.Hour)
	job := &transport.ServiceMutationJob{
		RequestID:      journal.MutationRequestID,
		OwnerID:        journal.MutationOwnerID,
		Kind:           "dns_engine_switch",
		Target:         string(journal.TargetEngine),
		PackageName:    journal.ManifestQualifier,
		Status:         servicemutationledger.StatusRunning,
		Phase:          "leased",
		Attempt:        1,
		StartedAt:      started,
		UpdatedAt:      started,
		LeaseExpiresAt: now.Add(time.Hour),
		DeadlineAt:     now.Add(2 * time.Hour),
	}
	ledger := servicemutationledger.Ledger{
		Version:         servicemutationledger.Version,
		ActiveRequestID: job.RequestID,
		Jobs:            map[string]*transport.ServiceMutationJob{job.RequestID: job},
	}
	ledgerRaw, err := servicemutationledger.Encode(&ledger)
	if err != nil {
		t.Fatal(err)
	}
	journalRaw, err := policy.EncodeSwitchJournal(journal)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Dir(policy.StatePath)
	journalPath := filepath.Join(root, "dns-engine-switch-journal.json")
	ledgerPath := filepath.Join(root, "service-mutations.json")
	if err := os.WriteFile(journalPath, journalRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ledgerPath, ledgerRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	return policy, owner, journal, now, journalPath, ledgerPath
}

func TestPublishExactDNSRollbackVerdictClosesOnlyAcceptedJob(t *testing.T) {
	policy, owner, journal, now, journalPath, ledgerPath := rollbackVerdictFixture(t)
	beforeJournal, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := PublishExactDNSRollbackVerdict(policy, owner, journal, now); err != nil {
		t.Fatal(err)
	}
	afterJournal, err := os.ReadFile(journalPath)
	if err != nil || !bytes.Equal(beforeJournal, afterJournal) {
		t.Fatal("terminal publication removed or altered the rollback journal")
	}
	raw, err := os.ReadFile(ledgerPath)
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := servicemutationledger.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	id := dnsengineartifact.SwitchIdentity{
		RequestID: journal.MutationRequestID, OwnerID: journal.MutationOwnerID,
		Target: journal.TargetEngine, Qualifier: journal.ManifestQualifier,
	}
	if !id.TerminalRolledBackJob(ledger) || ledger.Jobs[id.RequestID].ErrorCode != "dns_engine_switch_rolled_back_by_owner_recovery" {
		t.Fatal("exact terminal rollback verdict was not published")
	}
	if err := PublishExactDNSRollbackVerdict(policy, owner, journal, now); err == nil {
		t.Fatal("terminal job was accepted as a second publication")
	}
}

func TestPublishExactDNSRollbackVerdictRejectsChangedJournalOrLedger(t *testing.T) {
	t.Run("journal-mismatch", func(t *testing.T) {
		policy, owner, journal, now, _, ledgerPath := rollbackVerdictFixture(t)
		before, err := os.ReadFile(ledgerPath)
		if err != nil {
			t.Fatal(err)
		}
		journal.MutationOwnerID = "cccccccccccccccccccccccccccccccc"
		if err := PublishExactDNSRollbackVerdict(policy, owner, journal, now); err == nil {
			t.Fatal("foreign journal published a terminal verdict")
		}
		after, err := os.ReadFile(ledgerPath)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("rejected journal changed the ledger")
		}
	})
	t.Run("other-active-request", func(t *testing.T) {
		policy, owner, journal, now, _, ledgerPath := rollbackVerdictFixture(t)
		before, err := os.ReadFile(ledgerPath)
		if err != nil {
			t.Fatal(err)
		}
		ledger, err := servicemutationledger.Decode(before)
		if err != nil {
			t.Fatal(err)
		}
		other := *ledger.Jobs[journal.MutationRequestID]
		other.RequestID = "cccccccccccccccccccccccccccccccc"
		other.OwnerID = "dddddddddddddddddddddddddddddddd"
		ledger.Jobs[other.RequestID] = &other
		ledger.Jobs[journal.MutationRequestID].Status = servicemutationledger.StatusFailed
		ledger.Jobs[journal.MutationRequestID].Phase = "interrupted"
		ledger.Jobs[journal.MutationRequestID].ErrorCode = "prior_failure"
		ledger.Jobs[journal.MutationRequestID].ErrorMessage = "Prior failure."
		ledger.Jobs[journal.MutationRequestID].UpdatedAt = now
		ledger.Jobs[journal.MutationRequestID].FinishedAt = now
		ledger.Jobs[journal.MutationRequestID].LeaseExpiresAt = time.Time{}
		ledger.ActiveRequestID = other.RequestID
		changed, err := servicemutationledger.Encode(&ledger)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(ledgerPath, changed, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := PublishExactDNSRollbackVerdict(policy, owner, journal, now); err == nil {
			t.Fatal("foreign active job was overwritten")
		}
		after, err := os.ReadFile(ledgerPath)
		if err != nil || !bytes.Equal(changed, after) {
			t.Fatal("rejected job changed the ledger")
		}
	})
}

func TestPublishExactDNSRollbackVerdictRefusesLiveRecordedWorker(t *testing.T) {
	policy, owner, journal, now, _, ledgerPath := rollbackVerdictFixture(t)
	before, err := os.ReadFile(ledgerPath)
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := servicemutationledger.Decode(before)
	if err != nil {
		t.Fatal(err)
	}
	token, err := processidentity.StartToken(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	job := ledger.Jobs[journal.MutationRequestID]
	job.WorkerPID = os.Getpid()
	job.WorkerStarted = token
	job.WorkerCommand = "go-test"
	changed, err := servicemutationledger.Encode(&ledger)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ledgerPath, changed, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := PublishExactDNSRollbackVerdict(policy, owner, journal, now); err == nil {
		t.Fatal("live recorded worker was closed by independent recovery")
	}
	after, err := os.ReadFile(ledgerPath)
	if err != nil || !bytes.Equal(changed, after) {
		t.Fatal("rejected live worker changed the accepted ledger")
	}
}
