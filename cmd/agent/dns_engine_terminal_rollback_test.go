//go:build linux

package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
	"github.com/alicelik/celikpanel/internal/transport"
)

func TestTerminalDNSRollbackJournalCleanupRequiresDurableExactVerdict(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "internal", "dnsengineartifact", "testdata", "switch-journal", "alpha81-bind.json"))
	if err != nil {
		t.Fatal(err)
	}
	journal, err := decodeDNSEngineSwitchJournal(raw)
	if err != nil {
		t.Fatal(err)
	}
	journal.Phase = dnsengineartifact.SwitchPhaseRolledBack
	encoded, err := encodeDNSEngineSwitchJournal(journal)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name        string
		terminal    bool
		foreign     bool
		wantRemoved bool
	}{
		{name: "before terminal ledger publication"},
		{name: "exact terminal verdict", terminal: true, wantRemoved: true},
		{name: "foreign terminal verdict", terminal: true, foreign: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, dnsEngineSwitchJournalFile)
			if err := os.WriteFile(path, encoded, 0o600); err != nil {
				t.Fatal(err)
			}
			job := &transport.ServiceMutationJob{
				RequestID: journal.MutationRequestID, OwnerID: journal.MutationOwnerID,
				Kind: "dns_engine_switch", Target: string(journal.TargetEngine),
				PackageName: journal.ManifestQualifier, Status: servicemutationledger.StatusRunning,
				Phase: "leased", Attempt: 1, StartedAt: now, UpdatedAt: now,
				LeaseExpiresAt: now.Add(time.Minute), DeadlineAt: now.Add(time.Hour),
			}
			ledger := servicemutationledger.Ledger{Version: servicemutationledger.Version,
				ActiveRequestID: job.RequestID,
				Jobs:            map[string]*transport.ServiceMutationJob{job.RequestID: job},
			}
			if tc.terminal {
				ledger.ActiveRequestID = ""
				job.Status = servicemutationledger.StatusFailed
				job.Phase = "failed"
				job.ErrorCode = "dns_engine_switch_failed"
				job.ErrorMessage = "Previous DNS state restored."
				job.FinishedAt = job.UpdatedAt
				job.LeaseExpiresAt = time.Time{}
			}
			if tc.foreign {
				job.OwnerID = "ffffffffffffffffffffffffffffffff"
			}
			m := &serviceMutationManager{ledgerPath: filepath.Join(dir, "ledger.json"), ledger: ledger}
			err := m.removeTerminalRolledBackDNSEngineSwitchJournalLocked(job.RequestID)
			if tc.wantRemoved {
				if err != nil {
					t.Fatal(err)
				}
				if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
					t.Fatalf("exact terminal journal remains: %v", statErr)
				}
				return
			}
			if err == nil {
				t.Fatal("journal removed without exact durable verdict")
			}
			if _, statErr := os.Stat(path); statErr != nil {
				t.Fatalf("unproven journal lost: %v", statErr)
			}
		})
	}
}

func TestBootReplaysRetainedTerminalDNSRollbackBeforeCleanup(t *testing.T) {
	useTestServiceMutationOwner(t)
	raw, err := os.ReadFile(filepath.Join("..", "..", "internal", "dnsengineartifact", "testdata", "switch-journal", "alpha81-bind.json"))
	if err != nil {
		t.Fatal(err)
	}
	journal, err := decodeDNSEngineSwitchJournal(raw)
	if err != nil {
		t.Fatal(err)
	}
	journal.Phase = dnsengineartifact.SwitchPhaseRolledBack
	encoded, err := encodeDNSEngineSwitchJournal(journal)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, dnsEngineSwitchJournalFile)
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	job := &transport.ServiceMutationJob{
		RequestID: journal.MutationRequestID, OwnerID: journal.MutationOwnerID,
		Kind: "dns_engine_switch", Target: string(journal.TargetEngine),
		PackageName: journal.ManifestQualifier, Status: servicemutationledger.StatusFailed,
		Phase: "failed", ErrorCode: "dns_engine_switch_failed",
		ErrorMessage: "Previous DNS state restored.", Attempt: 1,
		StartedAt: now, UpdatedAt: now, FinishedAt: now, DeadlineAt: now.Add(time.Hour),
	}
	ledger := servicemutationledger.Ledger{Version: servicemutationledger.Version,
		Jobs: map[string]*transport.ServiceMutationJob{job.RequestID: job},
	}
	m := &serviceMutationManager{ledgerPath: filepath.Join(dir, "ledger.json"), ledger: ledger}
	lock, err := acquireServiceMutationFileLock(filepath.Join(dir, "service-mutation.lock"))
	if err != nil {
		t.Fatal(err)
	}
	backend := &fakeDNSEngineBackend{recovery: dnsEngineSwitchRecoveryRolledBack}
	useFakeDNSEngineBackend(t, backend)
	m.mu.Lock()
	handled, recoveryErr := m.recoverReleasedUndecidedDNSEngineSwitchLocked(lock)
	m.mu.Unlock()
	if !handled || recoveryErr != nil || backend.recoverCalls != 1 {
		t.Fatalf("boot recovery handled=%v err=%v native reproof=%d", handled, recoveryErr, backend.recoverCalls)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("terminal journal remained after replay: %v", statErr)
	}
	if ledger.Jobs[job.RequestID].Status != servicemutationledger.StatusFailed {
		t.Fatal("boot replay rewrote the terminal verdict")
	}

	// If native reproof is unknown at the next boot, the DNS journal stays
	// frozen while unrelated host mutations retain their lock path.
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	backend.recoverErr = errors.New("native DNS state unavailable")
	lock, err = acquireServiceMutationFileLock(filepath.Join(dir, "service-mutation.lock"))
	if err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	handled, recoveryErr = m.recoverReleasedUndecidedDNSEngineSwitchLocked(lock)
	m.mu.Unlock()
	if !handled || recoveryErr != nil || m.poisoned != nil {
		t.Fatalf("unknown DNS reproof blocked unrelated management: handled=%v err=%v poisoned=%v", handled, recoveryErr, m.poisoned)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("unknown DNS reproof discarded journal: %v", statErr)
	}
	probe, err := acquireServiceMutationFileLock(filepath.Join(dir, "service-mutation.lock"))
	if err != nil {
		t.Fatalf("idle DNS uncertainty retained global host lock: %v", err)
	}
	if err := probe.Close(); err != nil {
		t.Fatal(err)
	}
}

// A durable failed verdict is authoritative even when retirement of the
// DNS-specific rollback checkpoint cannot be proved. Other host mutations
// must retain their lock path; the checkpoint stays for DNS owner review.
func TestTerminalDNSRollbackCleanupFailureDoesNotPoisonHost(t *testing.T) {
	manager, root := newMutationTestManager(t)
	job := beginMutationTestJob(t, manager)
	raw, err := os.ReadFile(filepath.Join("..", "..", "internal", "dnsengineartifact", "testdata", "switch-journal", "alpha81-bind.json"))
	if err != nil {
		t.Fatal(err)
	}
	journal, err := decodeDNSEngineSwitchJournal(raw)
	if err != nil {
		t.Fatal(err)
	}
	journal.MutationRequestID = job.RequestID
	journal.Phase = dnsengineartifact.SwitchPhaseRolledBack
	// A valid journal belonging to a different owner must never be removed.
	if journal.MutationOwnerID == job.OwnerID {
		journal.MutationOwnerID = "ffffffffffffffffffffffffffffffff"
	}
	encoded, err := encodeDNSEngineSwitchJournal(journal)
	if err != nil {
		t.Fatal(err)
	}
	journalPath := filepath.Join(root, "state", dnsEngineSwitchJournalFile)
	if err := os.WriteFile(journalPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	manager.mu.Lock()
	job = manager.active.job
	job.Kind = "dns_engine_switch"
	job.Target = string(journal.TargetEngine)
	job.PackageName = journal.ManifestQualifier
	if err := manager.writeLocked(); err != nil {
		manager.mu.Unlock()
		t.Fatal(err)
	}
	err = manager.finishRuntimeTerminalLocked(manager.active, false,
		"failed", "dns_engine_switch_failed", "Previous DNS state restored.")
	poisoned, active := manager.poisoned, manager.active
	manager.mu.Unlock()
	if err != nil || poisoned != nil || active != nil {
		t.Fatalf("DNS cleanup failure stranded host: err=%v poisoned=%v active=%v", err, poisoned, active)
	}
	if _, err := os.Stat(journalPath); err != nil {
		t.Fatalf("uncertain DNS journal was discarded: %v", err)
	}
	ledgerRaw, err := os.ReadFile(filepath.Join(root, "state", serviceMutationLedgerFileName))
	if err != nil {
		t.Fatal(err)
	}
	durable, err := decodeServiceMutationLedger(ledgerRaw)
	if err != nil {
		t.Fatal(err)
	}
	if durable.ActiveRequestID != "" || durable.Jobs[job.RequestID] == nil ||
		durable.Jobs[job.RequestID].Status != servicemutationledger.StatusFailed {
		t.Fatalf("durable terminal verdict missing: %+v", durable)
	}
	lock, err := acquireServiceMutationFileLock(filepath.Join(root, "service-mutation.lock"))
	if err != nil {
		t.Fatalf("DNS-only uncertainty retained the host lock: %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestBootDNSRollbackCleanupFailureDoesNotPoisonHost(t *testing.T) {
	manager, root := newMutationTestManager(t)
	job := beginMutationTestJob(t, manager)
	raw, err := os.ReadFile(filepath.Join("..", "..", "internal", "dnsengineartifact", "testdata", "switch-journal", "alpha81-bind.json"))
	if err != nil {
		t.Fatal(err)
	}
	journal, err := decodeDNSEngineSwitchJournal(raw)
	if err != nil {
		t.Fatal(err)
	}
	journal.MutationRequestID = job.RequestID
	journal.MutationOwnerID = "ffffffffffffffffffffffffffffffff"
	journal.Phase = dnsengineartifact.SwitchPhaseRolledBack
	encoded, err := encodeDNSEngineSwitchJournal(journal)
	if err != nil {
		t.Fatal(err)
	}
	journalPath := filepath.Join(root, "state", dnsEngineSwitchJournalFile)
	if err := os.WriteFile(journalPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	manager.mu.Lock()
	job = manager.active.job
	job.Kind = "dns_engine_switch"
	job.Target = string(journal.TargetEngine)
	job.PackageName = journal.ManifestQualifier
	if err := manager.writeLocked(); err != nil {
		manager.mu.Unlock()
		t.Fatal(err)
	}
	manager.active.cancel()
	if err := manager.active.lock.Close(); err != nil {
		manager.mu.Unlock()
		t.Fatal(err)
	}
	manager.active = nil
	manager.mu.Unlock()
	lock, err := acquireServiceMutationFileLock(filepath.Join(root, "service-mutation.lock"))
	if err != nil {
		t.Fatal(err)
	}
	backend := &fakeDNSEngineBackend{recovery: dnsEngineSwitchRecoveryRolledBack}
	useFakeDNSEngineBackend(t, backend)
	manager.mu.Lock()
	handled, err := manager.recoverPersistedDNSEngineSwitchLocked(job, lock)
	poisoned := manager.poisoned
	manager.mu.Unlock()
	if !handled || err != nil || poisoned != nil || backend.recoverCalls != 1 {
		t.Fatalf("boot cleanup held host: handled=%v err=%v poisoned=%v recovery=%d", handled, err, poisoned, backend.recoverCalls)
	}
	if _, err := os.Stat(journalPath); err != nil {
		t.Fatalf("uncertain DNS journal was discarded: %v", err)
	}
	ledgerRaw, err := os.ReadFile(filepath.Join(root, "state", serviceMutationLedgerFileName))
	if err != nil {
		t.Fatal(err)
	}
	durable, err := decodeServiceMutationLedger(ledgerRaw)
	if err != nil {
		t.Fatal(err)
	}
	if durable.ActiveRequestID != "" || durable.Jobs[job.RequestID] == nil ||
		durable.Jobs[job.RequestID].Status != servicemutationledger.StatusFailed {
		t.Fatalf("boot failed verdict missing: %+v", durable)
	}
	probe, err := acquireServiceMutationFileLock(filepath.Join(root, "service-mutation.lock"))
	if err != nil {
		t.Fatalf("boot DNS cleanup retained host lock: %v", err)
	}
	if err := probe.Close(); err != nil {
		t.Fatal(err)
	}
}
