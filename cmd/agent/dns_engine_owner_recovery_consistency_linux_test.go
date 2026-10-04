//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
)

// The owner inverse commands may finish the Agent's deliberately released DNS
// switch while the Agent keeps running. They hold the same host mutation lock,
// write only the journal (rolled-back checkpoint, then retirement) and leave
// the Agent's terminal ledger verdict as it is. This test holds the Agent to a
// consistent terminal state afterwards: no poison, no second rollback, no
// rewritten verdict, and new DNS work admitted once the journal is retired.
// It is a component test with a stubbed DNS backend, not native evidence.
func TestAgentStaysConsistentAfterOwnerCommandRetiresReleasedJournal(t *testing.T) {
	for _, tc := range []struct {
		name    string
		retired bool
	}{
		{name: "owner command completed", retired: true},
		{name: "owner command interrupted after rolled-back checkpoint"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := canonicalSwitchRequest(t)
			manager, root := newMutationTestManager(t)
			beginMutationTestJobWithIdentity(t, manager, "dns_engine_switch", "bind", request.ManifestQualifier)
			journal := persistActiveCommittedBINDStartupJournal(t, manager, root, request)
			journal.Phase = dnsengineartifact.SwitchPhaseRollingBack
			if err := writeDNSEngineSwitchJournal(journal); err != nil {
				t.Fatal(err)
			}
			persistActiveDNSEngineSwitchStartupLedger(t, manager, journal, false, nil)
			abandonFirewallApplyTestRuntime(t, manager)
			// The Agent cannot execute this inverse itself (as for every V2
			// journal) and releases its lease with the deliberate reason.
			backend := &fakeDNSEngineBackend{recoverErr: errors.New("v2 BIND switch journal requires its independent inverse adapter")}
			useFakeDNSEngineBackend(t, backend)
			lock, err := acquireServiceMutationFileLock(manager.lockPath)
			if err != nil {
				t.Fatal(err)
			}
			manager.mu.Lock()
			handled, releaseErr := manager.recoverPersistedDNSEngineSwitchLocked(manager.ledger.Jobs[request.MutationRequestID], lock)
			manager.mu.Unlock()
			if !handled || releaseErr != nil {
				t.Fatalf("Agent did not release the undecidable DNS lease: handled=%v err=%v", handled, releaseErr)
			}
			id := dnsengineartifact.SwitchIdentity{RequestID: journal.MutationRequestID, OwnerID: journal.MutationOwnerID, Target: journal.TargetEngine, Qualifier: journal.ManifestQualifier}
			released, err := os.ReadFile(manager.ledgerPath)
			if err != nil {
				t.Fatal(err)
			}
			durable, err := decodeServiceMutationLedger(released)
			if err != nil || !id.ReleasedUndecidedJob(durable) || durable.Jobs[id.RequestID].ErrorCode != dnsengineartifact.ReleasedNativeUnknownCode {
				t.Fatalf("ledger is not the Agent's deliberate release: %v", err)
			}
			storedMessage := durable.Jobs[id.RequestID].ErrorMessage
			installGlobalMutationTestManager(t, manager)
			reportedMessage := func() string {
				t.Helper()
				var response ServiceMutationResponse
				if err := (&Agent{}).ServiceMutationStatus(&ServiceMutationStatusRequest{RequestID: id.RequestID}, &response); err != nil || response.Job == nil {
					t.Fatalf("status RPC failed: job=%+v err=%v", response.Job, err)
				}
				if response.Job.ErrorCode != dnsengineartifact.ReleasedNativeUnknownCode || response.Job.Status != serviceMutationStatusFailed {
					t.Fatalf("status RPC changed the terminal verdict: %+v", response.Job)
				}
				return response.Job.ErrorMessage
			}
			// While the journal is retained the stored blocking text is
			// reported unchanged.
			if got := reportedMessage(); got != storedMessage || !strings.Contains(got, "new DNS changes are blocked") {
				t.Fatalf("retained journal reported %q, want the stored %q", got, storedMessage)
			}

			// The owner command takes the host mutation lock. While it holds
			// it the running Agent cannot start another mutation.
			ownerLock, err := acquireServiceMutationFileLock(manager.lockPath)
			if err != nil {
				t.Fatalf("owner command could not take the released host lock: %v", err)
			}
			if _, err := manager.begin(&ServiceMutationBeginRequest{
				RequestID: strings.Repeat("1", 32), OwnerID: strings.Repeat("2", 32),
				Kind: "dns_engine_switch", Target: "bind", PackageName: request.ManifestQualifier,
			}); err == nil {
				t.Fatal("Agent started a mutation while the owner command held the host lock")
			}
			journal.Phase = dnsengineartifact.SwitchPhaseRolledBack
			if err := writeDNSEngineSwitchJournal(journal); err != nil {
				t.Fatal(err)
			}
			if tc.retired {
				if err := removeDNSEngineSwitchJournal(); err != nil {
					t.Fatal(err)
				}
			}
			if err := ownerLock.Close(); err != nil {
				t.Fatal(err)
			}
			requireUnchangedLedger := func(when string) {
				t.Helper()
				raw, err := os.ReadFile(manager.ledgerPath)
				if err != nil || !bytes.Equal(raw, released) {
					t.Fatalf("%s: the Agent's terminal verdict was rewritten: %v", when, err)
				}
			}
			requireUnchangedLedger("after owner command")
			// The status answer is computed at read time: a retired journal
			// is reported as reconciled, a retained one keeps the stored text.
			wantMessage := storedMessage
			if tc.retired {
				wantMessage = fmt.Sprintf(releasedDNSSwitchReconciledMessage, id.RequestID)
			}
			if got := reportedMessage(); got != wantMessage {
				t.Fatalf("status RPC reported %q, want %q", got, wantMessage)
			}
			requireUnchangedLedger("after status read")

			// The running Agent holds no stale hold on the request.
			manager.mu.Lock()
			poisoned, active, activeID := manager.poisoned, manager.active, manager.ledger.ActiveRequestID
			manager.mu.Unlock()
			if poisoned != nil || active != nil || activeID != "" {
				t.Fatalf("running Agent kept a hold: poisoned=%v active=%v id=%q", poisoned, active != nil, activeID)
			}
			preflight := reconcileExistingDNSEngineSwitchJournal(context.Background())
			if tc.retired && preflight != nil {
				t.Fatalf("retired journal still blocks new DNS work: %v", preflight)
			}
			if !tc.retired && preflight == nil {
				t.Fatal("a retained rolled-back journal admitted new DNS work")
			}

			// Next Agent start. A retired journal leaves nothing to reconcile.
			// An interrupted command leaves the rolled-back checkpoint beside a
			// terminal job; the Agent's only step is its own same-operation
			// reconciliation, which refuses a V2 inverse and keeps the journal
			// for the owner command to finish.
			backend.recoverCalls, backend.finalizeCalls = 0, 0
			useScriptedHostRecoveryProbe(t, nil, hostRecoveryDecideNow)
			reloaded, err := reloadHostBootRecoveryManager(t, root)
			if err != nil {
				t.Fatalf("next Agent start failed: %v", err)
			}
			wantRecover := 1
			if tc.retired {
				wantRecover = 0
			}
			if backend.recoverCalls != wantRecover || backend.finalizeCalls != 0 {
				t.Fatalf("next start ran recover=%d finalize=%d, want recover=%d finalize=0", backend.recoverCalls, backend.finalizeCalls, wantRecover)
			}
			job, activeID, poisoned := hostBootRecoveryLedgerJob(t, reloaded, request.MutationRequestID)
			if poisoned != nil || activeID != "" || job == nil || job.ErrorCode != dnsengineartifact.ReleasedNativeUnknownCode {
				t.Fatalf("next start lost the terminal verdict: poisoned=%v active=%q job=%+v", poisoned, activeID, job)
			}
			requireUnchangedLedger("after next start")
			_, statErr := os.Lstat(filepath.Join(root, "state", dnsEngineSwitchJournalFile))
			if tc.retired != os.IsNotExist(statErr) {
				t.Fatalf("journal presence after next start is wrong: retired=%v stat=%v", tc.retired, statErr)
			}
			if !tc.retired {
				return
			}

			// The next DNS operation is admitted on the reloaded Agent.
			if _, err := reloaded.begin(&ServiceMutationBeginRequest{
				RequestID: strings.Repeat("1", 32), OwnerID: strings.Repeat("2", 32),
				Kind: "dns_engine_switch", Target: "bind", PackageName: request.ManifestQualifier,
			}); err != nil {
				t.Fatalf("new DNS operation refused after owner recovery: %v", err)
			}
			abandonFirewallApplyTestRuntime(t, reloaded)
		})
	}
}

// The read-time message follows journal presence for this exact request only;
// an unreadable journal is not absence, and other jobs are never touched.
func TestReleasedDNSSwitchStatusMessageFollowsExactJournalPresence(t *testing.T) {
	request := canonicalSwitchRequest(t)
	manager, root := newMutationTestManager(t)
	journal := activeCommittedBINDStartupJournalFixture(t, manager, root, request)
	now := manager.now()
	stored := "The interrupted DNS switch could not be verified after the Agent restarted. Its exact journal remains for DNS recovery, and new DNS changes are blocked."
	job := &ServiceMutationJob{
		RequestID: journal.MutationRequestID, OwnerID: journal.MutationOwnerID,
		Kind: "dns_engine_switch", Target: string(journal.TargetEngine), PackageName: journal.ManifestQualifier,
		Status: serviceMutationStatusFailed, Phase: "interrupted", Attempt: 1,
		StartedAt: now.Add(-time.Hour), UpdatedAt: now, FinishedAt: now, DeadlineAt: now.Add(time.Hour),
		ErrorCode: dnsengineartifact.ReleasedNativeUnknownCode, ErrorMessage: stored,
	}
	reconciled := fmt.Sprintf(releasedDNSSwitchReconciledMessage, job.RequestID)
	check := func(name string, job *ServiceMutationJob, want string) {
		t.Helper()
		before := *job
		got := manager.presentReleasedDNSSwitchJob(job)
		if got.ErrorMessage != want || *job != before {
			t.Fatalf("%s: reported %q (want %q); stored job changed=%v", name, got.ErrorMessage, want, *job != before)
		}
	}
	check("no journal", job, reconciled)
	if len(reconciled) > 512 {
		t.Fatal("reconciled message exceeds the panel's receipt bound")
	}
	other := journal
	other.MutationRequestID = strings.Repeat("9", 32)
	if err := writeDNSEngineSwitchJournal(other); err != nil {
		t.Fatal(err)
	}
	check("another request's journal", job, reconciled)
	if err := writeDNSEngineSwitchJournal(journal); err != nil {
		t.Fatal(err)
	}
	check("this request's journal", job, stored)
	if err := os.WriteFile(dnsEngineSwitchJournalPath(), []byte("{unreadable"), 0o600); err != nil {
		t.Fatal(err)
	}
	check("unreadable journal", job, stored)
	if err := os.Remove(dnsEngineSwitchJournalPath()); err != nil {
		t.Fatal(err)
	}
	window := *job
	window.ErrorCode = dnsengineartifact.ReleasedHostWindowCode
	check("host-window release", &window, stored)
	if manager.presentReleasedDNSSwitchJob(nil) != nil {
		t.Fatal("absent job was invented")
	}
}
