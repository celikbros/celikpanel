//go:build linux

package main

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
)

func dnsClusterConfigTestCommitment(t *testing.T) mutationpayload.DNSClusterConfigCommitment {
	t.Helper()
	commitment, err := mutationpayload.CanonicalDNSClusterConfig(
		dnsRolePaired, "203.0.113.9", "ns2.example.test",
	)
	if err != nil {
		t.Fatal(err)
	}
	return commitment
}

func beginDNSClusterConfigTestStep(
	t *testing.T,
	commitment mutationpayload.DNSClusterConfigCommitment,
) (*serviceMutationManager, string, context.Context, func()) {
	t.Helper()
	manager, root := newMutationTestManager(t)
	beginMutationTestJobWithIdentity(
		t, manager, "dns_cluster_configure", "pdns", commitment.Qualifier,
	)
	ctx, finish, err := manager.acquireStep(
		ServiceMutationBinding{
			MutationRequestID: testMutationRequestID,
			MutationOwnerID:   testMutationOwnerID,
		},
		newServiceMutationStepClaim(
			serviceMutationStepConfigureDNSCluster,
			"pdns", commitment.Qualifier, "configure",
		),
	)
	if err != nil {
		t.Fatal(err)
	}
	return manager, root, ctx, finish
}

func abandonDNSClusterConfigTestRuntime(t *testing.T, manager *serviceMutationManager) {
	t.Helper()
	manager.mu.Lock()
	runtime := manager.active
	if runtime == nil {
		manager.mu.Unlock()
		t.Fatal("DNS cluster test manager has no active runtime")
	}
	runtime.cancel()
	manager.active = nil
	manager.mu.Unlock()
	if err := runtime.lock.Close(); err != nil {
		t.Fatal(err)
	}
}

func releasePoisonedDNSClusterConfigTestManager(manager *serviceMutationManager) {
	if manager == nil {
		return
	}
	manager.mu.Lock()
	var locks []*serviceMutationFileLock
	if manager.active != nil {
		manager.active.cancel()
		locks = append(locks, manager.active.lock)
		manager.active = nil
	}
	if manager.poisonLock != nil {
		locks = append(locks, manager.poisonLock)
		manager.poisonLock = nil
	}
	manager.mu.Unlock()
	seen := make(map[*serviceMutationFileLock]bool)
	for _, lock := range locks {
		if lock != nil && !seen[lock] {
			_ = lock.Close()
			seen[lock] = true
		}
	}
}

func TestConfigureDNSClusterV2RejectsDurableNonPDNSAuthorityBeforeMutation(t *testing.T) {
	for _, authority := range []string{"bind-state", "switch-journal"} {
		t.Run(authority, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "standby-pdns.sqlite3")
			t.Setenv("CELIKPANEL_PDNS_DB", path)
			commitment := dnsClusterConfigTestCommitment(t)
			manager, _ := newMutationTestManager(t)
			beginMutationTestJobWithIdentity(
				t, manager, "dns_cluster_configure", "pdns",
				commitment.Qualifier,
			)
			installGlobalMutationTestManager(t, manager)
			t.Cleanup(func() { releasePoisonedDNSClusterConfigTestManager(manager) })

			oldAuthority := legacyPowerDNSDurableAuthorityCheck
			oldRuntime := legacyPowerDNSRuntimeSafetyCheck
			raw := "raw " + authority + " detail must stay in logs"
			legacyPowerDNSDurableAuthorityCheck = func(bool) error {
				return errors.New(raw)
			}
			runtimeCalls := 0
			legacyPowerDNSRuntimeSafetyCheck = func(context.Context, bool) error {
				runtimeCalls++
				return nil
			}
			t.Cleanup(func() {
				legacyPowerDNSDurableAuthorityCheck = oldAuthority
				legacyPowerDNSRuntimeSafetyCheck = oldRuntime
			})

			request := &ConfigureDNSClusterV2Request{
				ServiceMutationBinding: ServiceMutationBinding{
					MutationRequestID: testMutationRequestID,
					MutationOwnerID:   testMutationOwnerID,
				},
				Role: commitment.Role, PeerIP: commitment.PeerIP,
				PeerNS: commitment.PeerNS,
			}
			response := ConfigureDNSClusterV2Response{Applied: true, Error: "stale"}
			if err := (&Agent{}).ConfigureDNSClusterV2(request, &response); err != nil {
				t.Fatal(err)
			}
			expected := "PowerDNS cluster configuration is blocked because PowerDNS is not the sole active DNS engine"
			if response.Applied || response.Error != expected ||
				strings.Contains(response.Error, raw) || runtimeCalls != 0 {
				t.Fatalf("response=%+v runtimeCalls=%d", response, runtimeCalls)
			}
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("durable guard touched standby PowerDNS DB: %v", err)
			}
		})
	}
}

func TestDNSClusterIntentPersistsExactJournalAndPhase(t *testing.T) {
	commitment := dnsClusterConfigTestCommitment(t)
	manager, _, ctx, finish := beginDNSClusterConfigTestStep(t, commitment)
	defer finish()
	journal, err := commitDNSClusterConfigIntent(ctx, commitment)
	if err != nil {
		t.Fatal(err)
	}
	persisted, exists, err := readDNSClusterConfigJournal(
		dnsClusterConfigJournalPath(manager),
	)
	if err != nil || !exists {
		t.Fatalf("read journal exists=%v err=%v", exists, err)
	}
	if !dnsClusterConfigJobMatchesJournal(manager.status(testMutationRequestID), persisted) ||
		journal.Role != commitment.Role || journal.PeerIP != commitment.PeerIP ||
		journal.PeerNS != commitment.PeerNS || journal.Qualifier != commitment.Qualifier {
		t.Fatalf("journal=%+v persisted=%+v", journal, persisted)
	}
	job := manager.status(testMutationRequestID)
	state, requestID, qualifier, err := parseDNSClusterConfigCommitPhase(job.Phase)
	if err != nil || state != dnsClusterConfigCommitIntent ||
		requestID != testMutationRequestID || qualifier != commitment.Qualifier {
		t.Fatalf("intent phase=%q err=%v", job.Phase, err)
	}
}

func TestDNSClusterCommittedLifecycleCannotReportFailure(t *testing.T) {
	commitment := dnsClusterConfigTestCommitment(t)
	manager, _, ctx, finish := beginDNSClusterConfigTestStep(t, commitment)
	defer finish()
	journal, err := commitDNSClusterConfigIntent(ctx, commitment)
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := manager.cancelJob(&ServiceMutationCancelRequest{
		RequestID: testMutationRequestID, ExpectedOwner: testMutationOwnerID,
	})
	if err != nil || cancelled.Status != serviceMutationStatusRunning {
		t.Fatalf("cancelled=%+v err=%v", cancelled, err)
	}
	manager.mu.Lock()
	runtime := manager.active
	manager.mu.Unlock()
	manager.expire(runtime)
	expired := manager.status(testMutationRequestID)
	if expired == nil || expired.Status != serviceMutationStatusRunning {
		t.Fatalf("expired=%+v", expired)
	}
	finished, finishErr := manager.finish(&ServiceMutationFinishRequest{
		RequestID: testMutationRequestID, OwnerID: testMutationOwnerID,
		Success: false,
	})
	if finishErr == nil || finished == nil ||
		finished.Status != serviceMutationStatusRunning {
		t.Fatalf("finish(false)=%+v err=%v", finished, finishErr)
	}
	if err := publishDNSClusterConfig(ctx, journal); err != nil {
		t.Fatal(err)
	}
	terminal := manager.status(testMutationRequestID)
	if terminal == nil || terminal.Status != serviceMutationStatusSucceeded ||
		!strings.HasPrefix(terminal.Phase, dnsClusterConfigCommitPhasePrefix+dnsClusterConfigCommitPublished+"/") {
		t.Fatalf("terminal=%+v", terminal)
	}
}

func TestDNSClusterStartupRecoversCommittedJournalForward(t *testing.T) {
	commitment := dnsClusterConfigTestCommitment(t)
	manager, root, ctx, finish := beginDNSClusterConfigTestStep(t, commitment)
	if _, err := commitDNSClusterConfigIntent(ctx, commitment); err != nil {
		t.Fatal(err)
	}
	finish()
	abandonDNSClusterConfigTestRuntime(t, manager)
	previousRecover := recoverDNSClusterConfigHost
	calls := 0
	recoverDNSClusterConfigHost = func(
		ctx context.Context, journal *dnsClusterConfigJournal,
	) error {
		tracker, _ := ctx.Value(serviceMutationExecutionTrackerKey{}).(*serviceMutationExecutionTracker)
		if tracker == nil || !tracker.allowCancellingRecovery {
			return errors.New("DNS cluster recovery was not tracked")
		}
		calls++
		return validateDNSClusterConfigJournal(journal)
	}
	t.Cleanup(func() { recoverDNSClusterConfigHost = previousRecover })
	reloaded, err := newServiceMutationManager(
		filepath.Join(root, "state"), filepath.Join(root, "service-mutation.lock"),
	)
	if err != nil {
		t.Fatal(err)
	}
	job := reloaded.status(testMutationRequestID)
	if calls != 1 || job == nil || job.Status != serviceMutationStatusSucceeded ||
		!strings.HasPrefix(job.Phase, dnsClusterConfigCommitPhasePrefix+dnsClusterConfigCommitPublished+"/") {
		t.Fatalf("calls=%d job=%+v", calls, job)
	}
}

func TestDNSClusterStartupJournalMismatchPoisonsAndRetainsLock(t *testing.T) {
	commitment := dnsClusterConfigTestCommitment(t)
	manager, root, ctx, finish := beginDNSClusterConfigTestStep(t, commitment)
	if _, err := commitDNSClusterConfigIntent(ctx, commitment); err != nil {
		t.Fatal(err)
	}
	finish()
	if err := writeDNSClusterConfigJournal(
		dnsClusterConfigJournalPath(manager),
		&dnsClusterConfigJournal{
			Version:   dnsClusterConfigJournalVersion,
			RequestID: testMutationSecondRequestID,
			Role:      commitment.Role, PeerIP: commitment.PeerIP,
			PeerNS: commitment.PeerNS, Qualifier: commitment.Qualifier,
		},
	); err != nil {
		t.Fatal(err)
	}
	abandonDNSClusterConfigTestRuntime(t, manager)
	reloaded, err := newServiceMutationManager(
		filepath.Join(root, "state"), filepath.Join(root, "service-mutation.lock"),
	)
	if err == nil || reloaded == nil || reloaded.poisoned == nil {
		t.Fatalf("mismatch manager=%v err=%v", reloaded, err)
	}
	t.Cleanup(func() { releasePoisonedDNSClusterConfigTestManager(reloaded) })
	second, secondErr := newServiceMutationManager(
		filepath.Join(root, "state"), filepath.Join(root, "service-mutation.lock"),
	)
	if second != nil || !errors.Is(secondErr, errServiceMutationHostBusy) {
		t.Fatalf("retained lock manager=%v err=%v", second, secondErr)
	}
}

func TestDNSClusterTerminalLedgerWriteFailurePoisonsAndRetainsLock(t *testing.T) {
	commitment := dnsClusterConfigTestCommitment(t)
	manager, _, ctx, finish := beginDNSClusterConfigTestStep(t, commitment)
	defer finish()
	journal, err := commitDNSClusterConfigIntent(ctx, commitment)
	if err != nil {
		t.Fatal(err)
	}
	manager.writeFault = func(point string) error {
		if point == serviceMutationWriteFaultBeforeRename {
			return errors.New("injected terminal ledger failure")
		}
		return nil
	}
	if err := publishDNSClusterConfig(ctx, journal); err == nil {
		t.Fatal("terminal publication accepted an unpersisted ledger write")
	}
	manager.mu.Lock()
	poisoned := manager.poisoned != nil
	retained := manager.active != nil && manager.active.lock != nil
	manager.mu.Unlock()
	if !poisoned || !retained {
		t.Fatalf("terminal write poisoned=%v retained=%v", poisoned, retained)
	}
	t.Cleanup(func() { releasePoisonedDNSClusterConfigTestManager(manager) })
}

func TestConfigureDNSClusterV2PreIntentReadinessFailureIsZeroTouch(t *testing.T) {
	t.Setenv("CELIKPANEL_PDNS_DB", filepath.Join(t.TempDir(), "missing.sqlite3"))
	commitment := dnsClusterConfigTestCommitment(t)
	manager, _, ctx, finish := beginDNSClusterConfigTestStep(t, commitment)
	defer finish()
	oldLookPath := dnsClusterLookPath
	oldConf := dnsClusterConf
	dnsClusterLookPath = func(string) (string, error) { return "", errors.New("missing") }
	dnsClusterConf = filepath.Join(t.TempDir(), "celikpanel-cluster.conf")
	sentinel := []byte("unchanged\n")
	if err := os.WriteFile(dnsClusterConf, sentinel, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		dnsClusterLookPath = oldLookPath
		dnsClusterConf = oldConf
	})
	var response ConfigureDNSClusterV2Response
	if err := configureDNSClusterV2(ctx, commitment, &response); err != nil {
		t.Fatal(err)
	}
	if response.Error == "" || response.Applied {
		t.Fatalf("readiness response=%+v", response)
	}
	if _, err := os.Lstat(dnsClusterConfigJournalPath(manager)); !os.IsNotExist(err) {
		t.Fatalf("pre-intent readiness created journal: %v", err)
	}
	if got, err := os.ReadFile(dnsClusterConf); err != nil || string(got) != string(sentinel) {
		t.Fatalf("pre-intent readiness changed config=%q err=%v", got, err)
	}
	job := manager.status(testMutationRequestID)
	if job == nil || job.Phase != "leased" || manager.poisoned != nil {
		t.Fatalf("pre-intent job=%+v poisoned=%v", job, manager.poisoned)
	}
}

func TestConfigureDNSClusterV2PostIntentEffectiveDriftPoisons(t *testing.T) {
	t.Setenv("CELIKPANEL_PDNS_DB", filepath.Join(t.TempDir(), "pdns.sqlite3"))
	prepareDNSClusterRuntimeTest(t)
	db, err := openPdnsDB()
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	commitment := dnsClusterConfigTestCommitment(t)
	manager, _, ctx, finish := beginDNSClusterConfigTestStep(t, commitment)
	defer finish()
	dnsClusterRestart = func(context.Context) ([]byte, error) {
		return nil, os.WriteFile(dnsMainConf, []byte("include-dir=/unmanaged\n"), 0o644)
	}
	var response ConfigureDNSClusterV2Response
	if err := configureDNSClusterV2(ctx, commitment, &response); err != nil {
		t.Fatal(err)
	}
	manager.mu.Lock()
	poisoned := manager.poisoned != nil
	retained := manager.active != nil && manager.active.lock != nil
	manager.mu.Unlock()
	if response.Error == "" || response.Applied || !poisoned || !retained {
		t.Fatalf("drift response=%+v poisoned=%v retained=%v", response, poisoned, retained)
	}
	if job := manager.status(testMutationRequestID); job == nil ||
		strings.Contains(job.Phase, dnsClusterConfigCommitPublished) {
		t.Fatalf("drift job=%+v", job)
	}
	t.Cleanup(func() { releasePoisonedDNSClusterConfigTestManager(manager) })
}

func TestPublishDNSClusterConfigRejectsUntrustedDirectoryBeforeMutation(t *testing.T) {
	for _, trust := range []string{"owner", "mode"} {
		for _, mutation := range []string{"create", "replace", "remove"} {
			t.Run(trust+"/"+mutation, func(t *testing.T) {
				confPath := prepareDNSClusterRuntimeTest(t)
				dir := filepath.Dir(confPath)
				priorConfig := []byte("prior cluster configuration\n")
				if mutation != "create" {
					if err := os.WriteFile(confPath, priorConfig, 0o644); err != nil {
						t.Fatal(err)
					}
				}
				stagePath := filepath.Join(dir, ".celikpanel-cluster-stage-preserve.tmp")
				priorStage := []byte("unpublished stage\n")
				if err := os.WriteFile(stagePath, priorStage, 0o600); err != nil {
					t.Fatal(err)
				}

				expectedError := "must be owned by uid"
				switch trust {
				case "owner":
					dnsClusterConfigRequiredOwnerUID = uint32(os.Geteuid()) ^ 1
				case "mode":
					expectedError = "must not be writable by group or others"
					if err := os.Chmod(dir, 0o770); err != nil {
						t.Fatal(err)
					}
				}
				desired := "replacement cluster configuration\n"
				if mutation == "remove" {
					desired = ""
				}
				err := publishDNSClusterConfigFile(desired)
				if err == nil || !strings.Contains(err.Error(), expectedError) {
					t.Fatalf("untrusted directory publication error = %v, want %q", err, expectedError)
				}
				if mutation == "create" {
					if _, statErr := os.Lstat(confPath); !errors.Is(statErr, os.ErrNotExist) {
						t.Fatalf("untrusted directory created config: %v", statErr)
					}
				} else {
					gotConfig, readErr := os.ReadFile(confPath)
					if readErr != nil || string(gotConfig) != string(priorConfig) {
						t.Fatalf("untrusted directory changed config: got=%q err=%v", gotConfig, readErr)
					}
				}
				gotStage, readErr := os.ReadFile(stagePath)
				if readErr != nil || string(gotStage) != string(priorStage) {
					t.Fatalf("untrusted directory cleaned stage: got=%q err=%v", gotStage, readErr)
				}
				entries, readErr := os.ReadDir(dir)
				if readErr != nil {
					t.Fatal(readErr)
				}
				var stages int
				for _, entry := range entries {
					if strings.HasPrefix(entry.Name(), ".celikpanel-cluster-stage-") {
						stages++
					}
				}
				if stages != 1 {
					t.Fatalf("untrusted directory stage count = %d, want 1", stages)
				}
			})
		}
	}
}

func TestConfigureDNSClusterLegacyIsStableZeroTouch(t *testing.T) {
	oldRestart := dnsClusterRestart
	oldApply := dnsClusterApplyAutoprimaryTx
	restartCalls, applyCalls := 0, 0
	dnsClusterRestart = func(context.Context) ([]byte, error) {
		restartCalls++
		return nil, nil
	}
	dnsClusterApplyAutoprimaryTx = func(
		_ *sql.Tx, _ *DNSClusterRequest,
	) error {
		applyCalls++
		return nil
	}
	t.Cleanup(func() {
		dnsClusterRestart = oldRestart
		dnsClusterApplyAutoprimaryTx = oldApply
	})
	var response DNSClusterResponse
	if err := (&Agent{}).ConfigureDNSCluster(
		&DNSClusterRequest{Role: dnsRolePaired, PeerIP: "203.0.113.9", PeerNS: "ns2.example.test"},
		&response,
	); err != nil {
		t.Fatal(err)
	}
	if response.Applied || response.Error != configureDNSClusterLegacyUnsupportedError ||
		restartCalls != 0 || applyCalls != 0 {
		t.Fatalf("legacy response=%+v restart=%d apply=%d", response, restartCalls, applyCalls)
	}
}
