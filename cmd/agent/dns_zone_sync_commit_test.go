//go:build linux

package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

func dnsZoneSyncTestCommitment(
	t *testing.T,
	domain string,
	generation int64,
	deleteZone bool,
	zoneType string,
) mutationpayload.DNSZoneSyncCommitment {
	t.Helper()
	var records []transport.ZoneRecord
	if !deleteZone {
		records = []transport.ZoneRecord{
			{
				Name: domain, Type: "SOA",
				Content: "ns1.example.net hostmaster." + domain +
					" 2026081201 10800 3600 604800 3600",
				TTL: 3600,
			},
			{Name: domain, Type: "NS", Content: "ns1.example.net", TTL: 3600},
		}
	}
	commitment, err := mutationpayload.CanonicalDNSZoneSync(
		generation, domain, deleteZone, zoneType, records,
	)
	if err != nil {
		t.Fatal(err)
	}
	return commitment
}

func useDNSZoneSyncTestDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pdns.sqlite3")
	t.Setenv("CELIKPANEL_PDNS_DB", path)
	return path
}

func TestSyncDNSZoneLegacyEndpointIsStableZeroTouchStub(t *testing.T) {
	path := useDNSZoneSyncTestDB(t)
	previousCommand := dnsSyncCommand
	commandCalls := 0
	dnsSyncCommand = func(context.Context, string, ...string) ([]byte, error) {
		commandCalls++
		return nil, errors.New("must not run")
	}
	t.Cleanup(func() { dnsSyncCommand = previousCommand })

	response := SyncDNSZoneResponse{
		Synced: true, AppliedGeneration: 99, Error: "stale",
	}
	if err := (&Agent{}).SyncDNSZone(
		&SyncDNSZoneRequest{
			DesiredGeneration: 12,
			Domain:            "legacy.example",
			ZoneType:          "MASTER",
			Records: []ZoneRecord{{
				Name: "legacy.example", Type: "A",
				Content: "192.0.2.10", TTL: 300,
			}},
		},
		&response,
	); err != nil {
		t.Fatal(err)
	}
	if response.Synced || response.AppliedGeneration != 0 ||
		response.Error != syncDNSZoneLegacyUnsupportedError {
		t.Fatalf("legacy response=%+v", response)
	}
	if commandCalls != 0 {
		t.Fatalf("legacy endpoint ran %d commands", commandCalls)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy endpoint touched PowerDNS DB: %v", err)
	}
}

func TestDNSZoneReceiptRejectsExternallyDriftedHostState(t *testing.T) {
	useDNSZoneSyncTestDB(t)
	commitment := dnsZoneSyncTestCommitment(
		t, "drift.example", 3, false, "NATIVE",
	)
	prepared, err := prepareDNSZoneSync(
		context.Background(), testMutationRequestID, commitment,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.close()
	if err := prepared.tx.Commit(); err != nil {
		t.Fatal(err)
	}
	db, err := openPdnsDB()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		UPDATE records SET content = '192.0.2.99'
		WHERE domain_id = (
		 SELECT id FROM domains WHERE name = 'drift.example'
		) AND type = 'NS'
	`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	var beforeMaster string
	if err := db.QueryRow(`
		SELECT COALESCE(group_concat(entry, char(10)), '')
		FROM (
		 SELECT type || ':' || name || ':' || COALESCE(sql, '') AS entry
		 FROM sqlite_master
		 ORDER BY type, name
		)
	`).Scan(&beforeMaster); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()
	if _, _, err := inspectDNSZoneSyncReceipt(
		context.Background(),
		testMutationRequestID,
		commitment.Domain,
		commitment.Qualifier,
	); err == nil {
		t.Fatal("externally drifted PowerDNS state verified from a stale receipt")
	}
}

func TestDNSZoneReceiptRejectsExternallyDriftedZoneMaster(t *testing.T) {
	useDNSZoneSyncTestDB(t)
	commitment := dnsZoneSyncTestCommitment(
		t, "master-drift.example", 4, false, "NATIVE",
	)
	commitDNSZoneSyncTestReceipt(t, testMutationRequestID, commitment)
	db, err := openPdnsDB()
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(
		`UPDATE domains SET master = '192.0.2.55' WHERE name = ?`,
		commitment.Domain,
	)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := inspectDNSZoneSyncReceipt(
		context.Background(),
		testMutationRequestID,
		commitment.Domain,
		commitment.Qualifier,
	); err == nil {
		t.Fatal("externally drifted zone master verified from a stale receipt")
	}
}

func TestFinalizeDNSZoneSyncUsesTrackedContextOrder(t *testing.T) {
	databasePath := useDNSZoneSyncTestDB(t)
	db, err := openPdnsDB()
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	prepareManagedDNSReadinessTest(t, databasePath)
	commitment := dnsZoneSyncTestCommitment(
		t, "signed.example", 7, false, "MASTER",
	)
	previous := dnsSyncCommand
	var calls []string
	dnsSyncCommand = func(
		_ context.Context, name string, args ...string,
	) ([]byte, error) {
		call := name + " " + strings.Join(args, " ")
		calls = append(calls, call)
		if call == "pdnsutil zone show signed.example" {
			return []byte("DS = signed.example. IN DS 1 13 2 AABB"), nil
		}
		return nil, nil
	}
	t.Cleanup(func() { dnsSyncCommand = previous })
	if err := finalizeDNSZoneSync(
		context.Background(), commitment,
	); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"pdnsutil zone show signed.example",
		"pdnsutil zone rectify signed.example",
		"pdns_control purge signed.example$",
		"pdns_control notify signed.example",
	}
	if len(calls) != len(want) {
		t.Fatalf("finalize calls=%v want=%v", calls, want)
	}
	for index := range want {
		if calls[index] != want[index] {
			t.Fatalf("finalize calls=%v want=%v", calls, want)
		}
	}
}

func beginDNSZoneSyncTestStep(
	t *testing.T,
	commitment mutationpayload.DNSZoneSyncCommitment,
) (
	*serviceMutationManager,
	context.Context,
	func(),
) {
	t.Helper()
	manager, _ := newMutationTestManager(t)
	beginMutationTestJobWithIdentity(
		t,
		manager,
		"dns_zone_sync",
		commitment.Domain,
		commitment.Qualifier,
	)
	action := dnsZoneSyncActionSync
	if commitment.Delete {
		action = dnsZoneSyncActionDelete
	}
	ctx, finishStep, err := manager.acquireStep(
		ServiceMutationBinding{
			MutationRequestID: testMutationRequestID,
			MutationOwnerID:   testMutationOwnerID,
		},
		newServiceMutationStepClaim(
			serviceMutationStepSyncDNSZone,
			commitment.Domain,
			commitment.Qualifier,
			action,
		),
	)
	if err != nil {
		t.Fatal(err)
	}
	return manager, ctx, finishStep
}

func abandonDNSZoneSyncTestRuntime(
	t *testing.T,
	manager *serviceMutationManager,
) {
	t.Helper()
	manager.mu.Lock()
	runtime := manager.active
	if runtime == nil {
		manager.mu.Unlock()
		t.Fatal("DNS test manager has no active runtime")
	}
	runtime.cancel()
	manager.active = nil
	manager.mu.Unlock()
	if err := runtime.lock.Close(); err != nil {
		t.Fatal(err)
	}
}

func persistDNSZoneSyncTestPhase(
	t *testing.T,
	manager *serviceMutationManager,
	phase string,
) {
	t.Helper()
	manager.mu.Lock()
	defer manager.mu.Unlock()
	before := cloneServiceMutationLedger(manager.ledger)
	manager.active.job.Phase = phase
	manager.active.job.UpdatedAt = manager.now()
	if err := manager.persistLedgerMutationLocked(before); err != nil {
		t.Fatal(err)
	}
}

func releasePoisonedDNSZoneSyncTestManager(
	manager *serviceMutationManager,
) {
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

func commitDNSZoneSyncTestReceipt(
	t *testing.T,
	requestID string,
	commitment mutationpayload.DNSZoneSyncCommitment,
) {
	t.Helper()
	prepared, err := prepareDNSZoneSync(
		context.Background(), requestID, commitment,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.close()
	if err := prepared.tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestDNSZoneCommitAbsentAfterErrorLeavesPrecommitFailure(t *testing.T) {
	useDNSZoneSyncTestDB(t)
	commitment := dnsZoneSyncTestCommitment(
		t, "precommit.example", 5, false, "NATIVE",
	)
	manager, ctx, finishStep := beginDNSZoneSyncTestStep(t, commitment)
	defer finishStep()
	prepared, err := prepareDNSZoneSync(
		ctx, testMutationRequestID, commitment,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.close()
	previousCommit := dnsZoneSyncCommitTransaction
	dnsZoneSyncCommitTransaction = func(tx *sql.Tx) error {
		if err := tx.Rollback(); err != nil {
			return err
		}
		return errors.New("injected precommit failure")
	}
	t.Cleanup(func() { dnsZoneSyncCommitTransaction = previousCommit })
	applied, err := commitPreparedDNSZoneSync(
		ctx, testMutationRequestID, prepared,
	)
	if err == nil || applied {
		t.Fatalf("precommit applied=%v err=%v", applied, err)
	}
	if manager.poisoned != nil {
		t.Fatalf("proven absent precommit failure poisoned manager: %v", manager.poisoned)
	}
	job := manager.status(testMutationRequestID)
	if job == nil || !strings.HasPrefix(
		job.Phase,
		dnsZoneSyncCommitPhasePrefix+dnsZoneSyncCommitIntent+"/",
	) {
		t.Fatalf("precommit job=%+v", job)
	}
}

func TestDNSZoneCommitErrorWithExactReceiptWins(t *testing.T) {
	useDNSZoneSyncTestDB(t)
	commitment := dnsZoneSyncTestCommitment(
		t, "commit-wins.example", 6, false, "NATIVE",
	)
	manager, ctx, finishStep := beginDNSZoneSyncTestStep(t, commitment)
	defer finishStep()
	prepared, err := prepareDNSZoneSync(
		ctx, testMutationRequestID, commitment,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.close()
	previousCommit := dnsZoneSyncCommitTransaction
	dnsZoneSyncCommitTransaction = func(tx *sql.Tx) error {
		if err := tx.Commit(); err != nil {
			return err
		}
		return errors.New("ambiguous commit return")
	}
	t.Cleanup(func() { dnsZoneSyncCommitTransaction = previousCommit })
	applied, err := commitPreparedDNSZoneSync(
		ctx, testMutationRequestID, prepared,
	)
	if err != nil || !applied {
		t.Fatalf("commit-wins applied=%v err=%v", applied, err)
	}
	job := manager.status(testMutationRequestID)
	if job == nil || !strings.HasPrefix(
		job.Phase,
		dnsZoneSyncCommitPhasePrefix+dnsZoneSyncCommitApplied+"/",
	) {
		t.Fatalf("commit-wins job=%+v", job)
	}
	if manager.active == nil ||
		manager.active.dnsZoneSyncAppliedPhase == "" {
		t.Fatal("exact receipt did not install applied-only runtime guard")
	}
}

func TestDNSZoneCommitErrorWithVerifiedPreviousReceiptIsPrecommit(t *testing.T) {
	useDNSZoneSyncTestDB(t)
	domain := "previous-receipt.example"
	previous := dnsZoneSyncTestCommitment(t, domain, 1, false, "NATIVE")
	commitDNSZoneSyncTestReceipt(t, testMutationSecondRequestID, previous)
	commitment := dnsZoneSyncTestCommitment(t, domain, 2, false, "NATIVE")
	manager, ctx, finishStep := beginDNSZoneSyncTestStep(t, commitment)
	defer finishStep()
	prepared, err := prepareDNSZoneSync(ctx, testMutationRequestID, commitment)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.close()
	previousCommit := dnsZoneSyncCommitTransaction
	dnsZoneSyncCommitTransaction = func(tx *sql.Tx) error {
		if err := tx.Rollback(); err != nil {
			return err
		}
		return errors.New("injected replacement rollback")
	}
	t.Cleanup(func() { dnsZoneSyncCommitTransaction = previousCommit })
	applied, err := commitPreparedDNSZoneSync(ctx, testMutationRequestID, prepared)
	if err == nil || applied {
		t.Fatalf("previous receipt rollback applied=%v err=%v", applied, err)
	}
	if manager.poisoned != nil {
		t.Fatalf("verified previous receipt poisoned manager: %v", manager.poisoned)
	}
	result, verified, inspectErr := inspectDNSZoneSyncReceipt(
		context.Background(), testMutationRequestID, domain, commitment.Qualifier,
	)
	if inspectErr != nil || result != dnsZoneSyncReceiptPreviousExact ||
		verified == nil || verified.Receipt.RequestID != testMutationSecondRequestID {
		t.Fatalf("previous receipt result=%d value=%+v err=%v", result, verified, inspectErr)
	}
}

func TestDNSZoneCommitWithoutReceiptPoisonsAndRetainsLock(t *testing.T) {
	useDNSZoneSyncTestDB(t)
	commitment := dnsZoneSyncTestCommitment(
		t, "missing-receipt.example", 6, false, "NATIVE",
	)
	manager, ctx, finishStep := beginDNSZoneSyncTestStep(t, commitment)
	defer finishStep()
	prepared, err := prepareDNSZoneSync(
		ctx, testMutationRequestID, commitment,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.close()
	previousInspector := dnsZoneSyncReceiptInspector
	dnsZoneSyncReceiptInspector = func(
		context.Context, string, string, string,
	) (
		dnsZoneSyncReceiptResult,
		*verifiedDNSZoneSyncReceipt,
		error,
	) {
		return dnsZoneSyncReceiptAbsent, nil, nil
	}
	t.Cleanup(func() { dnsZoneSyncReceiptInspector = previousInspector })
	applied, err := commitPreparedDNSZoneSync(
		ctx, testMutationRequestID, prepared,
	)
	if err == nil || applied || manager.poisoned == nil ||
		manager.active == nil || manager.active.lock == nil {
		t.Fatalf(
			"missing receipt applied=%v err=%v poisoned=%v active=%v",
			applied, err, manager.poisoned, manager.active,
		)
	}
	t.Cleanup(func() {
		releasePoisonedDNSZoneSyncTestManager(manager)
	})
}

func TestDNSZoneAppliedLifecycleCannotReportFailure(t *testing.T) {
	useDNSZoneSyncTestDB(t)
	commitment := dnsZoneSyncTestCommitment(
		t, "lifecycle.example", 9, false, "NATIVE",
	)
	manager, ctx, finishStep := beginDNSZoneSyncTestStep(t, commitment)
	defer finishStep()
	prepared, err := prepareDNSZoneSync(
		ctx, testMutationRequestID, commitment,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.close()
	applied, err := commitPreparedDNSZoneSync(
		ctx, testMutationRequestID, prepared,
	)
	if err != nil || !applied {
		t.Fatalf("apply=%v err=%v", applied, err)
	}
	cancelled, err := manager.cancelJob(&ServiceMutationCancelRequest{
		RequestID:     testMutationRequestID,
		ExpectedOwner: testMutationOwnerID,
	})
	if err != nil || cancelled == nil ||
		cancelled.Status != serviceMutationStatusRunning {
		t.Fatalf("cancelled=%+v err=%v", cancelled, err)
	}
	manager.mu.Lock()
	runtime := manager.active
	manager.mu.Unlock()
	manager.expire(runtime)
	expired := manager.status(testMutationRequestID)
	if expired == nil || expired.Status != serviceMutationStatusRunning ||
		!strings.HasPrefix(
			expired.Phase,
			dnsZoneSyncCommitPhasePrefix+dnsZoneSyncCommitApplied+"/",
		) {
		t.Fatalf("expire(applied)=%+v", expired)
	}
	finished, err := manager.finish(&ServiceMutationFinishRequest{
		RequestID: testMutationRequestID,
		OwnerID:   testMutationOwnerID,
		Success:   false,
	})
	if err == nil || finished == nil ||
		finished.Status != serviceMutationStatusRunning {
		t.Fatalf("finish(false)=%+v err=%v", finished, err)
	}
	if err := publishDNSZoneSync(
		ctx, testMutationRequestID, commitment,
	); err != nil {
		t.Fatal(err)
	}
	terminal := manager.status(testMutationRequestID)
	if terminal == nil ||
		terminal.Status != serviceMutationStatusSucceeded ||
		!strings.HasPrefix(
			terminal.Phase,
			dnsZoneSyncCommitPhasePrefix+dnsZoneSyncCommitPublished+"/",
		) {
		t.Fatalf("terminal=%+v", terminal)
	}
}

func TestDNSZoneStartupRecoveryPublishesExactReceipt(t *testing.T) {
	databasePath := useDNSZoneSyncTestDB(t)
	commitment := dnsZoneSyncTestCommitment(
		t, "recover.example", 17, false, "NATIVE",
	)
	manager, root := newMutationTestManager(t)
	beginMutationTestJobWithIdentity(
		t,
		manager,
		"dns_zone_sync",
		commitment.Domain,
		commitment.Qualifier,
	)
	commitDNSZoneSyncTestReceipt(
		t, testMutationRequestID, commitment,
	)
	prepareManagedDNSReadinessTest(t, databasePath)
	intent, err := formatDNSZoneSyncCommitPhase(
		dnsZoneSyncCommitIntent,
		testMutationRequestID,
		commitment.Domain,
		commitment.Qualifier,
	)
	if err != nil {
		t.Fatal(err)
	}
	persistDNSZoneSyncTestPhase(t, manager, intent)
	abandonDNSZoneSyncTestRuntime(t, manager)

	previousCommand := dnsSyncCommand
	var calls []string
	dnsSyncCommand = func(
		ctx context.Context, name string, args ...string,
	) ([]byte, error) {
		tracker, _ := ctx.Value(
			serviceMutationExecutionTrackerKey{},
		).(*serviceMutationExecutionTracker)
		if tracker == nil || !tracker.allowCancellingRecovery {
			return nil, errors.New("untracked DNS recovery command")
		}
		calls = append(calls, name+" "+strings.Join(args, " "))
		return []byte("Zone is not secured"), nil
	}
	t.Cleanup(func() { dnsSyncCommand = previousCommand })
	reloaded, err := newServiceMutationManager(
		filepath.Join(root, "state"),
		filepath.Join(root, "service-mutation.lock"),
	)
	if err != nil {
		t.Fatal(err)
	}
	job := reloaded.status(testMutationRequestID)
	if len(calls) == 0 || job == nil ||
		job.Status != serviceMutationStatusSucceeded {
		t.Fatalf("recovery calls=%v job=%+v", calls, job)
	}
	state, requestID, domain, qualifier, err :=
		parseDNSZoneSyncCommitPhase(job.Phase)
	if err != nil || state != dnsZoneSyncCommitPublished ||
		requestID != testMutationRequestID ||
		domain != commitment.Domain ||
		qualifier != commitment.Qualifier {
		t.Fatalf(
			"terminal phase=(%q,%q,%q,%q) err=%v",
			state, requestID, domain, qualifier, err,
		)
	}
}

func TestDNSZoneStartupIntentWithoutReceiptFailsPrecommit(t *testing.T) {
	useDNSZoneSyncTestDB(t)
	// Create a valid readable table; absence must be distinguishable from
	// schema/read ambiguity.
	db, err := openPdnsDB()
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	commitment := dnsZoneSyncTestCommitment(
		t, "absent.example", 4, false, "NATIVE",
	)
	manager, root := newMutationTestManager(t)
	beginMutationTestJobWithIdentity(
		t,
		manager,
		"dns_zone_sync",
		commitment.Domain,
		commitment.Qualifier,
	)
	intent, err := formatDNSZoneSyncCommitPhase(
		dnsZoneSyncCommitIntent,
		testMutationRequestID,
		commitment.Domain,
		commitment.Qualifier,
	)
	if err != nil {
		t.Fatal(err)
	}
	persistDNSZoneSyncTestPhase(t, manager, intent)
	abandonDNSZoneSyncTestRuntime(t, manager)
	previousCommand := dnsSyncCommand
	calls := 0
	dnsSyncCommand = func(
		context.Context, string, ...string,
	) ([]byte, error) {
		calls++
		return nil, nil
	}
	t.Cleanup(func() { dnsSyncCommand = previousCommand })
	reloaded, err := newServiceMutationManager(
		filepath.Join(root, "state"),
		filepath.Join(root, "service-mutation.lock"),
	)
	if err != nil {
		t.Fatal(err)
	}
	job := reloaded.status(testMutationRequestID)
	if calls != 0 || job == nil ||
		job.Status != serviceMutationStatusFailed {
		t.Fatalf("precommit calls=%d job=%+v", calls, job)
	}
}

func TestDNSZoneStartupMissingReceiptAuthorityClassifiesPhase(t *testing.T) {
	for _, test := range []struct {
		name       string
		legacyDB   bool
		phaseState string
		wantPoison bool
	}{
		{name: "no database before prepare"},
		{name: "legacy database before prepare", legacyDB: true},
		{name: "intent lost authority", phaseState: dnsZoneSyncCommitIntent, wantPoison: true},
		{name: "applied lost authority", phaseState: dnsZoneSyncCommitApplied, wantPoison: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := useDNSZoneSyncTestDB(t)
			if test.legacyDB || test.phaseState != "" {
				db, err := sql.Open("sqlite", "file:"+path)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := db.Exec(`CREATE TABLE domains (id INTEGER PRIMARY KEY, name TEXT)`); err != nil {
					db.Close()
					t.Fatal(err)
				}
				db.Close()
			}
			commitment := dnsZoneSyncTestCommitment(
				t, "authority-absent.example", 3, false, "NATIVE",
			)
			manager, root := newMutationTestManager(t)
			beginMutationTestJobWithIdentity(
				t, manager, "dns_zone_sync", commitment.Domain, commitment.Qualifier,
			)
			if test.phaseState != "" {
				phase, err := formatDNSZoneSyncCommitPhase(
					test.phaseState, testMutationRequestID,
					commitment.Domain, commitment.Qualifier,
				)
				if err != nil {
					t.Fatal(err)
				}
				persistDNSZoneSyncTestPhase(t, manager, phase)
			}
			abandonDNSZoneSyncTestRuntime(t, manager)
			reloaded, err := newServiceMutationManager(
				filepath.Join(root, "state"), filepath.Join(root, "service-mutation.lock"),
			)
			if test.wantPoison {
				if err == nil || reloaded == nil || reloaded.poisoned == nil {
					t.Fatalf("authority contradiction manager=%v err=%v", reloaded, err)
				}
				t.Cleanup(func() { releasePoisonedDNSZoneSyncTestManager(reloaded) })
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			job := reloaded.status(testMutationRequestID)
			if job == nil || job.Status != serviceMutationStatusFailed || reloaded.poisoned != nil {
				t.Fatalf("preprepare job=%+v poisoned=%v", job, reloaded.poisoned)
			}
		})
	}
}

func TestDNSZoneStartupVerifiedPreviousReceiptClassifiesCommitPhase(t *testing.T) {
	for _, test := range []struct {
		name       string
		phaseState string
		poison     bool
	}{
		{name: "before prepare"},
		{name: "intent rollback", phaseState: dnsZoneSyncCommitIntent},
		{name: "applied contradiction", phaseState: dnsZoneSyncCommitApplied, poison: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			useDNSZoneSyncTestDB(t)
			domain := "startup-previous.example"
			previous := dnsZoneSyncTestCommitment(t, domain, 1, false, "NATIVE")
			commitDNSZoneSyncTestReceipt(t, testMutationSecondRequestID, previous)
			commitment := dnsZoneSyncTestCommitment(t, domain, 2, false, "NATIVE")
			manager, root := newMutationTestManager(t)
			beginMutationTestJobWithIdentity(
				t, manager, "dns_zone_sync", commitment.Domain, commitment.Qualifier,
			)
			if test.phaseState != "" {
				phase, err := formatDNSZoneSyncCommitPhase(
					test.phaseState, testMutationRequestID,
					commitment.Domain, commitment.Qualifier,
				)
				if err != nil {
					t.Fatal(err)
				}
				persistDNSZoneSyncTestPhase(t, manager, phase)
			}
			abandonDNSZoneSyncTestRuntime(t, manager)
			reloaded, err := newServiceMutationManager(
				filepath.Join(root, "state"),
				filepath.Join(root, "service-mutation.lock"),
			)
			if test.poison {
				if err == nil || reloaded == nil || reloaded.poisoned == nil {
					t.Fatalf("contradiction manager=%v err=%v", reloaded, err)
				}
				t.Cleanup(func() { releasePoisonedDNSZoneSyncTestManager(reloaded) })
				second, secondErr := newServiceMutationManager(
					filepath.Join(root, "state"),
					filepath.Join(root, "service-mutation.lock"),
				)
				if second != nil || !errors.Is(secondErr, errServiceMutationHostBusy) {
					t.Fatalf("poison lock manager=%v err=%v", second, secondErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			job := reloaded.status(testMutationRequestID)
			if job == nil || job.Status != serviceMutationStatusFailed || reloaded.poisoned != nil {
				t.Fatalf("precommit job=%+v poisoned=%v", job, reloaded.poisoned)
			}
		})
	}
}

func TestDNSZoneStartupAppliedWithoutReceiptPoisonsAndRetainsLock(t *testing.T) {
	useDNSZoneSyncTestDB(t)
	db, err := openPdnsDB()
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	commitment := dnsZoneSyncTestCommitment(
		t, "lost.example", 4, false, "NATIVE",
	)
	manager, root := newMutationTestManager(t)
	beginMutationTestJobWithIdentity(
		t,
		manager,
		"dns_zone_sync",
		commitment.Domain,
		commitment.Qualifier,
	)
	applied, err := formatDNSZoneSyncCommitPhase(
		dnsZoneSyncCommitApplied,
		testMutationRequestID,
		commitment.Domain,
		commitment.Qualifier,
	)
	if err != nil {
		t.Fatal(err)
	}
	persistDNSZoneSyncTestPhase(t, manager, applied)
	abandonDNSZoneSyncTestRuntime(t, manager)
	reloaded, err := newServiceMutationManager(
		filepath.Join(root, "state"),
		filepath.Join(root, "service-mutation.lock"),
	)
	if err == nil || reloaded == nil || reloaded.poisoned == nil {
		t.Fatalf("lost applied receipt manager=%v err=%v", reloaded, err)
	}
	t.Cleanup(func() {
		releasePoisonedDNSZoneSyncTestManager(reloaded)
	})
	second, secondErr := newServiceMutationManager(
		filepath.Join(root, "state"),
		filepath.Join(root, "service-mutation.lock"),
	)
	if second != nil ||
		!errors.Is(secondErr, errServiceMutationHostBusy) {
		t.Fatalf("retained lock manager=%v err=%v", second, secondErr)
	}
}

func TestDNSZoneStartupLegacyEmptyQualifierFailsWithoutHostExecution(t *testing.T) {
	useDNSZoneSyncTestDB(t)
	commitment := dnsZoneSyncTestCommitment(
		t, "legacy-recovery.example", 4, false, "NATIVE",
	)
	manager, root := newMutationTestManager(t)
	beginMutationTestJobWithIdentity(
		t,
		manager,
		"dns_zone_sync",
		commitment.Domain,
		commitment.Qualifier,
	)
	manager.mu.Lock()
	before := cloneServiceMutationLedger(manager.ledger)
	manager.active.job.PackageName = ""
	manager.active.job.UpdatedAt = manager.now()
	if err := manager.persistLedgerMutationLocked(before); err != nil {
		manager.mu.Unlock()
		t.Fatal(err)
	}
	manager.mu.Unlock()
	abandonDNSZoneSyncTestRuntime(t, manager)
	previousCommand := dnsSyncCommand
	calls := 0
	dnsSyncCommand = func(
		context.Context, string, ...string,
	) ([]byte, error) {
		calls++
		return nil, nil
	}
	t.Cleanup(func() { dnsSyncCommand = previousCommand })
	reloaded, err := newServiceMutationManager(
		filepath.Join(root, "state"),
		filepath.Join(root, "service-mutation.lock"),
	)
	if err != nil {
		t.Fatal(err)
	}
	job := reloaded.status(testMutationRequestID)
	if calls != 0 || job == nil ||
		job.Status != serviceMutationStatusFailed ||
		job.PackageName != "" {
		t.Fatalf("legacy recovery calls=%d job=%+v", calls, job)
	}
}

func TestDNSZoneLiveWorkerPreservesCommitPhaseThenRecoversForward(t *testing.T) {
	for _, state := range []string{
		dnsZoneSyncCommitIntent,
		dnsZoneSyncCommitApplied,
	} {
		t.Run(state, func(t *testing.T) {
			databasePath := useDNSZoneSyncTestDB(t)
			commitment := dnsZoneSyncTestCommitment(
				t, state+"-worker.example", 21, false, "NATIVE",
			)
			manager, root := newMutationTestManager(t)
			beginMutationTestJobWithIdentity(
				t, manager, "dns_zone_sync", commitment.Domain, commitment.Qualifier,
			)
			commitDNSZoneSyncTestReceipt(t, testMutationRequestID, commitment)
			prepareManagedDNSReadinessTest(t, databasePath)
			phase, err := formatDNSZoneSyncCommitPhase(
				state, testMutationRequestID, commitment.Domain, commitment.Qualifier,
			)
			if err != nil {
				t.Fatal(err)
			}
			started, err := serviceMutationProcessStartIdentity(os.Getpid())
			if err != nil {
				t.Fatal(err)
			}
			manager.mu.Lock()
			before := cloneServiceMutationLedger(manager.ledger)
			manager.active.job.Phase = phase
			manager.active.job.WorkerPID = os.Getpid()
			manager.active.job.WorkerStarted = started
			manager.active.job.WorkerCommand = "dns-zone-sync-test"
			manager.active.job.UpdatedAt = manager.now()
			err = manager.persistLedgerMutationLocked(before)
			manager.mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			abandonDNSZoneSyncTestRuntime(t, manager)

			previousCommand := dnsSyncCommand
			commandCalls := 0
			dnsSyncCommand = func(
				ctx context.Context, _ string, _ ...string,
			) ([]byte, error) {
				tracker, _ := ctx.Value(
					serviceMutationExecutionTrackerKey{},
				).(*serviceMutationExecutionTracker)
				if tracker == nil || !tracker.allowCancellingRecovery {
					return nil, errors.New("DNS recovery command was not tracked")
				}
				commandCalls++
				return []byte("Zone is not secured"), nil
			}
			t.Cleanup(func() { dnsSyncCommand = previousCommand })
			reloaded, err := newServiceMutationManager(
				filepath.Join(root, "state"),
				filepath.Join(root, "service-mutation.lock"),
			)
			if err != nil {
				t.Fatal(err)
			}
			reloaded.mu.Lock()
			orphaned := cloneServiceMutationJob(
				reloaded.ledger.Jobs[testMutationRequestID],
			)
			reloaded.mu.Unlock()
			if commandCalls != 0 || orphaned == nil ||
				orphaned.Status != serviceMutationStatusOrphaned ||
				orphaned.Phase != phase {
				t.Fatalf(
					"live worker calls=%d orphan=%+v want phase=%q",
					commandCalls, orphaned, phase,
				)
			}

			reloaded.mu.Lock()
			before = cloneServiceMutationLedger(reloaded.ledger)
			persisted := reloaded.ledger.Jobs[testMutationRequestID]
			persisted.WorkerPID = 999999999
			persisted.WorkerStarted = "dead-worker"
			persisted.WorkerCommand = "dns-zone-sync-test"
			persisted.UpdatedAt = reloaded.now()
			err = reloaded.persistLedgerMutationLocked(before)
			reloaded.mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			terminal := reloaded.status(testMutationRequestID)
			if commandCalls == 0 || terminal == nil ||
				terminal.Status != serviceMutationStatusSucceeded {
				t.Fatalf(
					"dead worker calls=%d terminal=%+v", commandCalls, terminal,
				)
			}
			publishedState, _, _, _, err :=
				parseDNSZoneSyncCommitPhase(terminal.Phase)
			if err != nil || publishedState != dnsZoneSyncCommitPublished {
				t.Fatalf("terminal phase=%q err=%v", terminal.Phase, err)
			}
		})
	}
}

func TestDNSZoneStartupMalformedReceiptPoisonsAndRetainsLock(t *testing.T) {
	useDNSZoneSyncTestDB(t)
	commitment := dnsZoneSyncTestCommitment(
		t, "malformed-receipt.example", 22, false, "NATIVE",
	)
	manager, root := newMutationTestManager(t)
	beginMutationTestJobWithIdentity(
		t, manager, "dns_zone_sync", commitment.Domain, commitment.Qualifier,
	)
	commitDNSZoneSyncTestReceipt(t, testMutationRequestID, commitment)
	db, err := openPdnsDB()
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		UPDATE celikpanel_dns_zone_sync_receipts
		SET qualifier = 'malformed'
		WHERE domain = ?
	`, commitment.Domain)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	intent, err := formatDNSZoneSyncCommitPhase(
		dnsZoneSyncCommitIntent,
		testMutationRequestID,
		commitment.Domain,
		commitment.Qualifier,
	)
	if err != nil {
		t.Fatal(err)
	}
	persistDNSZoneSyncTestPhase(t, manager, intent)
	abandonDNSZoneSyncTestRuntime(t, manager)
	reloaded, err := newServiceMutationManager(
		filepath.Join(root, "state"),
		filepath.Join(root, "service-mutation.lock"),
	)
	if err == nil || reloaded == nil || reloaded.poisoned == nil {
		t.Fatalf("malformed receipt manager=%v err=%v", reloaded, err)
	}
	t.Cleanup(func() { releasePoisonedDNSZoneSyncTestManager(reloaded) })
	second, secondErr := newServiceMutationManager(
		filepath.Join(root, "state"),
		filepath.Join(root, "service-mutation.lock"),
	)
	if second != nil || !errors.Is(secondErr, errServiceMutationHostBusy) {
		t.Fatalf("retained lock manager=%v err=%v", second, secondErr)
	}
}

func TestDNSZoneFinalizeFailureWithExactReceiptIsKnownTerminalFailure(t *testing.T) {
	useDNSZoneSyncTestDB(t)
	commitment := dnsZoneSyncTestCommitment(
		t, "known-finalize-failure.example", 23, false, "MASTER",
	)
	manager, ctx, finishStep := beginDNSZoneSyncTestStep(t, commitment)
	defer finishStep()
	prepared, err := prepareDNSZoneSync(
		ctx, testMutationRequestID, commitment,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.close()
	applied, err := commitPreparedDNSZoneSync(
		ctx, testMutationRequestID, prepared,
	)
	if err != nil || !applied {
		t.Fatalf("apply=%v err=%v", applied, err)
	}
	cause := errors.New("injected notify failure")
	if err := failAppliedDNSZoneSync(
		ctx, testMutationRequestID, commitment, cause,
	); !errors.Is(err, cause) {
		t.Fatalf("finalize failure=%v", err)
	}
	job := manager.status(testMutationRequestID)
	manager.mu.Lock()
	active, poisoned := manager.active, manager.poisoned
	manager.mu.Unlock()
	if job == nil || job.Status != serviceMutationStatusFailed ||
		job.ErrorCode != "dns_zone_finalize_failed" ||
		active != nil || poisoned != nil {
		t.Fatalf(
			"known failure job=%+v active=%v poisoned=%v", job, active, poisoned,
		)
	}
}

func TestDNSZoneTerminalLedgerWriteFailurePoisonsAndRetainsLock(t *testing.T) {
	useDNSZoneSyncTestDB(t)
	commitment := dnsZoneSyncTestCommitment(
		t, "terminal-ledger-failure.example", 24, false, "NATIVE",
	)
	manager, ctx, finishStep := beginDNSZoneSyncTestStep(t, commitment)
	defer finishStep()
	prepared, err := prepareDNSZoneSync(
		ctx, testMutationRequestID, commitment,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.close()
	applied, err := commitPreparedDNSZoneSync(
		ctx, testMutationRequestID, prepared,
	)
	if err != nil || !applied {
		t.Fatalf("apply=%v err=%v", applied, err)
	}
	manager.writeFault = func(point string) error {
		if point == serviceMutationWriteFaultBeforeRename {
			return errors.New("injected terminal ledger failure")
		}
		return nil
	}
	if err := publishDNSZoneSync(
		ctx, testMutationRequestID, commitment,
	); err == nil {
		t.Fatal("terminal publication accepted an unpersisted ledger write")
	}
	manager.mu.Lock()
	poisoned := manager.poisoned != nil
	retained := manager.active != nil && manager.active.lock != nil
	manager.mu.Unlock()
	if !poisoned || !retained {
		t.Fatalf("terminal write poisoned=%v retained=%v", poisoned, retained)
	}
	t.Cleanup(func() { releasePoisonedDNSZoneSyncTestManager(manager) })
}

func TestDNSZoneRPCProductionMethodInventory(t *testing.T) {
	source, err := os.ReadFile("dns_sync_rpc.go")
	if err != nil {
		t.Fatal(err)
	}
	file, err := parser.ParseFile(
		token.NewFileSet(), "dns_sync_rpc.go", source, parser.AllErrors,
	)
	if err != nil {
		t.Fatal(err)
	}
	methods := map[string]int{}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv == nil {
			continue
		}
		methods[function.Name.Name]++
		if function.Name.Name == "syncDNSZone" {
			t.Fatal("obsolete internal V1 DNS mutation implementation remains")
		}
		if function.Name.Name == "SyncDNSZone" {
			ast.Inspect(function.Body, func(node ast.Node) bool {
				if _, ok := node.(*ast.CallExpr); ok {
					t.Errorf("legacy SyncDNSZone stub contains an executable call")
				}
				return true
			})
		}
	}
	if methods["SyncDNSZone"] != 1 || methods["SyncDNSZoneV2"] != 1 {
		t.Fatalf("DNS RPC method inventory=%v", methods)
	}
	if strings.Contains(string(source), "runPDNSUtil") {
		t.Fatal("contextless legacy pdnsutil helper remains in production")
	}
	commitSource, err := os.ReadFile("dns_zone_sync_commit.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(commitSource), "exec.Command(") {
		t.Fatal("DNS V2 production contains a contextless subprocess")
	}
}

func TestPreparedDNSZoneDeleteRollbackAndCommitAreAtomic(t *testing.T) {
	useDNSZoneSyncTestDB(t)
	db, err := openPdnsDB()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO domains (id, name, type)
		VALUES (11, 'delete.example', 'NATIVE');
		INSERT INTO records
		(domain_id, name, type, content, ttl, auth)
		VALUES
		(11, 'delete.example', 'A', '192.0.2.10', 300, 1);
	`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	var beforeMaster string
	if err := db.QueryRow(`
		SELECT COALESCE(group_concat(entry, char(10)), '')
		FROM (
		 SELECT type || ':' || name || ':' || COALESCE(sql, '') AS entry
		 FROM sqlite_master
		 ORDER BY type, name
		)
	`).Scan(&beforeMaster); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()
	commitment := dnsZoneSyncTestCommitment(
		t, "delete.example", 8, true, "NATIVE",
	)
	rolledBack, err := prepareDNSZoneSync(
		context.Background(), testMutationRequestID, commitment,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := rolledBack.close(); err != nil {
		t.Fatal(err)
	}
	db, err = openPdnsDB()
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM domains WHERE name = 'delete.example'`,
	).Scan(&count); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()
	if count != 1 {
		t.Fatalf("rollback domain count=%d", count)
	}
	result, _, err := inspectDNSZoneSyncReceipt(
		context.Background(),
		testMutationRequestID,
		commitment.Domain,
		commitment.Qualifier,
	)
	if err != nil || result != dnsZoneSyncReceiptAbsent {
		t.Fatalf("rollback receipt result=%d err=%v", result, err)
	}
	committed, err := prepareDNSZoneSync(
		context.Background(), testMutationRequestID, commitment,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer committed.close()
	if err := committed.tx.Commit(); err != nil {
		t.Fatal(err)
	}
	result, verified, err := inspectDNSZoneSyncReceipt(
		context.Background(),
		testMutationRequestID,
		commitment.Domain,
		commitment.Qualifier,
	)
	if err != nil || result != dnsZoneSyncReceiptExact ||
		verified == nil || !verified.Commitment.Delete {
		t.Fatalf(
			"delete receipt result=%d value=%+v err=%v",
			result, verified, err,
		)
	}
}

func TestSyncDNSZoneV2RejectsPayloadBeforeDBOrCommands(t *testing.T) {
	path := useDNSZoneSyncTestDB(t)
	previousCommand := dnsSyncCommand
	commandCalls := 0
	dnsSyncCommand = func(context.Context, string, ...string) ([]byte, error) {
		commandCalls++
		return nil, nil
	}
	t.Cleanup(func() { dnsSyncCommand = previousCommand })

	response := SyncDNSZoneV2Response{
		Synced: true, AppliedGeneration: 99, Error: "stale",
	}
	if err := (&Agent{}).SyncDNSZoneV2(
		&SyncDNSZoneV2Request{
			DesiredGeneration: -1,
			Domain:            "payload.example",
			ZoneType:          "MASTER",
			Records: []ZoneRecord{{
				Name: "payload.example", Type: "A",
				Content: "192.0.2.10", TTL: 300,
			}},
		},
		&response,
	); err != nil {
		t.Fatal(err)
	}
	if response.Synced || response.AppliedGeneration != 0 ||
		response.Error == "" {
		t.Fatalf("invalid V2 response=%+v", response)
	}
	if commandCalls != 0 {
		t.Fatalf("invalid V2 payload ran %d commands", commandCalls)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid V2 payload touched PowerDNS DB: %v", err)
	}
}

func TestSyncDNSZoneV2RejectsDurableNonPDNSAuthorityBeforeDBOrCommands(t *testing.T) {
	for _, authority := range []string{"bind-state", "switch-journal"} {
		t.Run(authority, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "standby-pdns.sqlite3")
			t.Setenv("CELIKPANEL_PDNS_DB", path)
			commitment := dnsZoneSyncTestCommitment(
				t, "standby.example", 4, false, "NATIVE",
			)
			manager, _ := newMutationTestManager(t)
			beginMutationTestJobWithIdentity(
				t, manager, "dns_zone_sync",
				commitment.Domain, commitment.Qualifier,
			)
			installGlobalMutationTestManager(t, manager)
			t.Cleanup(func() { releasePoisonedDNSZoneSyncTestManager(manager) })

			oldAuthority := legacyPowerDNSDurableAuthorityCheck
			oldCommand := dnsSyncCommand
			raw := "raw " + authority + " detail must stay in logs"
			legacyPowerDNSDurableAuthorityCheck = func(bool) error {
				return errors.New(raw)
			}
			commandCalls := 0
			dnsSyncCommand = func(context.Context, string, ...string) ([]byte, error) {
				commandCalls++
				return nil, errors.New("unexpected command")
			}
			t.Cleanup(func() {
				legacyPowerDNSDurableAuthorityCheck = oldAuthority
				dnsSyncCommand = oldCommand
			})

			response := SyncDNSZoneV2Response{
				Synced: true, AppliedGeneration: 99, Error: "stale",
			}
			if err := (&Agent{}).SyncDNSZoneV2(
				&SyncDNSZoneV2Request{
					ServiceMutationBinding: transport.ServiceMutationBinding{
						MutationRequestID: testMutationRequestID,
						MutationOwnerID:   testMutationOwnerID,
					},
					DesiredGeneration: commitment.DesiredGeneration,
					Domain:            commitment.Domain,
					ZoneType:          commitment.ZoneType,
					Records:           commitment.Records,
				},
				&response,
			); err != nil {
				t.Fatal(err)
			}
			if response.Synced || response.AppliedGeneration != 0 ||
				response.Error != "DNS zone publication is blocked because PowerDNS is not the active DNS engine" ||
				strings.Contains(response.Error, raw) || commandCalls != 0 {
				t.Fatalf("response=%+v commandCalls=%d", response, commandCalls)
			}
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("durable guard touched standby PowerDNS DB: %v", err)
			}
			manager.mu.Lock()
			steps := manager.active.steps
			phase := manager.active.job.Phase
			manager.mu.Unlock()
			if steps != 0 || phase != "leased" {
				t.Fatalf("durable guard retained step=%d phase=%q", steps, phase)
			}
		})
	}
}

func TestConfigurePowerDNSSQLiteRejectsDurableNonPDNSAuthorityBeforeMutation(t *testing.T) {
	for _, authority := range []string{"bind-state", "switch-journal"} {
		t.Run(authority, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "standby-pdns.sqlite3")
			t.Setenv("CELIKPANEL_PDNS_DB", path)
			manager, _ := newMutationTestManager(t)
			beginMutationTestJobWithIdentity(
				t, manager, "pdns_configure", "pdns", "",
			)
			installGlobalMutationTestManager(t, manager)
			t.Cleanup(func() { releasePoisonedDNSZoneSyncTestManager(manager) })
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
			request := &ServiceMutationRequest{ServiceMutationBinding: transport.ServiceMutationBinding{
				MutationRequestID: testMutationRequestID,
				MutationOwnerID:   testMutationOwnerID,
			}}
			response := SyncDNSZoneResponse{Synced: true, Error: "stale"}
			if err := (&Agent{}).ConfigurePowerDNSSQLite(request, &response); err != nil {
				t.Fatal(err)
			}
			expected := "PowerDNS configuration is blocked because the DNS engine state is not safe"
			if response.Synced || response.Error != expected ||
				strings.Contains(response.Error, raw) || runtimeCalls != 0 {
				t.Fatalf("response=%+v runtimeCalls=%d", response, runtimeCalls)
			}
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("durable guard touched standby PowerDNS DB: %v", err)
			}
		})
	}
}

func TestSyncDNSZoneV2ReadinessDriftRejectsDeleteBeforeDBOrCommands(t *testing.T) {
	path := filepath.Join(t.TempDir(), "detached-pdns.sqlite3")
	t.Setenv("CELIKPANEL_PDNS_DB", path)
	sentinel := []byte("detached database sentinel")
	if err := os.WriteFile(path, sentinel, 0o600); err != nil {
		t.Fatal(err)
	}
	prepareManagedDNSReadinessTest(t, path)
	if err := os.WriteFile(dnsMainConf, []byte("include-dir=/unmanaged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	commitment := dnsZoneSyncTestCommitment(
		t, "readiness-delete.example", 9, true, "NATIVE",
	)
	manager, _ := newMutationTestManager(t)
	beginMutationTestJobWithIdentity(
		t, manager, "dns_zone_sync", commitment.Domain, commitment.Qualifier,
	)
	installGlobalMutationTestManager(t, manager)
	t.Cleanup(func() { releasePoisonedDNSZoneSyncTestManager(manager) })
	previousCommand := dnsSyncCommand
	commandCalls := 0
	dnsSyncCommand = func(context.Context, string, ...string) ([]byte, error) {
		commandCalls++
		return nil, nil
	}
	t.Cleanup(func() { dnsSyncCommand = previousCommand })
	var response SyncDNSZoneV2Response
	if err := (&Agent{}).SyncDNSZoneV2(
		&SyncDNSZoneV2Request{
			ServiceMutationBinding: transport.ServiceMutationBinding{
				MutationRequestID: testMutationRequestID,
				MutationOwnerID:   testMutationOwnerID,
			},
			DesiredGeneration: commitment.DesiredGeneration,
			Domain:            commitment.Domain, Delete: true, ZoneType: commitment.ZoneType,
		},
		&response,
	); err != nil {
		t.Fatal(err)
	}
	if response.Error == "" || response.Synced || response.AppliedGeneration != 0 || commandCalls != 0 {
		t.Fatalf("readiness response=%+v commands=%d", response, commandCalls)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, sentinel) {
		t.Fatalf("readiness changed detached DB=%q err=%v", got, err)
	}
	manager.mu.Lock()
	steps := manager.active.steps
	phase := manager.active.job.Phase
	manager.mu.Unlock()
	if steps != 0 || phase != "leased" {
		t.Fatalf("readiness retained step=%d phase=%q", steps, phase)
	}
}

func TestSyncDNSZoneV2PostCommitAuthorityDriftPoisonsAndRetainsLock(t *testing.T) {
	path := useDNSZoneSyncTestDB(t)
	db, err := openPdnsDB()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	prepareManagedDNSReadinessTest(t, path)
	commitment := dnsZoneSyncTestCommitment(
		t, "postcommit-authority.example", 10, false, "NATIVE",
	)
	manager, _ := newMutationTestManager(t)
	beginMutationTestJobWithIdentity(
		t, manager, "dns_zone_sync", commitment.Domain, commitment.Qualifier,
	)
	installGlobalMutationTestManager(t, manager)
	previousCommit := dnsZoneSyncCommitTransaction
	dnsZoneSyncCommitTransaction = func(tx *sql.Tx) error {
		if err := tx.Commit(); err != nil {
			return err
		}
		return os.WriteFile(dnsMainConf, []byte("include-dir=/unmanaged\n"), 0o644)
	}
	t.Cleanup(func() { dnsZoneSyncCommitTransaction = previousCommit })
	previousCommand := dnsSyncCommand
	commandCalls := 0
	dnsSyncCommand = func(context.Context, string, ...string) ([]byte, error) {
		commandCalls++
		return nil, nil
	}
	t.Cleanup(func() { dnsSyncCommand = previousCommand })
	var response SyncDNSZoneV2Response
	if err := (&Agent{}).SyncDNSZoneV2(
		&SyncDNSZoneV2Request{
			ServiceMutationBinding: transport.ServiceMutationBinding{
				MutationRequestID: testMutationRequestID,
				MutationOwnerID:   testMutationOwnerID,
			},
			DesiredGeneration: commitment.DesiredGeneration,
			Domain:            commitment.Domain, ZoneType: commitment.ZoneType,
			Records: commitment.Records,
		},
		&response,
	); err != nil {
		t.Fatal(err)
	}
	manager.mu.Lock()
	poisoned := manager.poisoned != nil
	retained := manager.active != nil && manager.active.lock != nil
	manager.mu.Unlock()
	if response.Error == "" || response.Synced || response.AppliedGeneration != 0 ||
		commandCalls != 0 || !poisoned || !retained {
		t.Fatalf("postcommit response=%+v commands=%d poisoned=%v retained=%v",
			response, commandCalls, poisoned, retained)
	}
	result, verified, inspectErr := inspectDNSZoneSyncReceipt(
		context.Background(), testMutationRequestID,
		commitment.Domain, commitment.Qualifier,
	)
	if inspectErr != nil || result != dnsZoneSyncReceiptExact || verified == nil {
		t.Fatalf("postcommit receipt result=%d value=%+v err=%v", result, verified, inspectErr)
	}
	t.Cleanup(func() { releasePoisonedDNSZoneSyncTestManager(manager) })
}

func TestPreparedDNSZoneSyncCommitsZoneAndReceiptAtomically(t *testing.T) {
	useDNSZoneSyncTestDB(t)
	db, err := openPdnsDB()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO domains
		(id, name, type, notified_serial, options)
		VALUES
		(7, 'atomic.example', 'MASTER', 2026081200, 'keep-me');
		INSERT INTO records
		(domain_id, name, type, content, ttl, auth)
		VALUES
		(7, 'atomic.example', 'SOA',
		 'old.example hostmaster.atomic.example 1 10800 3600 604800 3600',
		 3600, 1);
		INSERT INTO domainmetadata (domain_id, kind, content)
		VALUES (7, 'NSEC3PARAM', '1 0 0 -');
		INSERT INTO cryptokeys
		(domain_id, flags, active, published, content)
		VALUES (7, 257, 1, 1, 'private-key-material');
		INSERT INTO comments
		(domain_id, name, type, modified_at, comment)
		VALUES (7, 'atomic.example', 'SOA', 1, 'keep this comment');
	`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	commitment := dnsZoneSyncTestCommitment(
		t, "atomic.example", 42, false, "MASTER",
	)
	prepared, err := prepareDNSZoneSync(
		context.Background(), testMutationRequestID, commitment,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.close()
	if err := prepared.tx.Commit(); err != nil {
		t.Fatal(err)
	}
	result, verified, err := inspectDNSZoneSyncReceipt(
		context.Background(),
		testMutationRequestID,
		commitment.Domain,
		commitment.Qualifier,
	)
	if err != nil {
		t.Fatal(err)
	}
	if result != dnsZoneSyncReceiptExact || verified == nil ||
		!sameDNSZoneSyncCommitment(verified.Commitment, commitment) {
		t.Fatalf("verified receipt result=%d value=%+v", result, verified)
	}

	db, err = openPdnsDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var id, notified int
	var zoneType, options string
	if err := db.QueryRow(`
		SELECT id, type, notified_serial, options
		FROM domains WHERE name = ?
	`, commitment.Domain).Scan(
		&id, &zoneType, &notified, &options,
	); err != nil {
		t.Fatal(err)
	}
	if id != 7 || zoneType != "MASTER" ||
		notified != 2026081200 || options != "keep-me" {
		t.Fatalf(
			"domain state id=%d type=%q notified=%d options=%q",
			id, zoneType, notified, options,
		)
	}
	for table, want := range map[string]int{
		"domainmetadata": 1,
		"cryptokeys":     1,
		"comments":       1,
		"records":        len(commitment.Records),
	} {
		var got int
		if err := db.QueryRow(
			"SELECT COUNT(*) FROM " + table + " WHERE domain_id = 7",
		).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("%s rows=%d want=%d", table, got, want)
		}
	}
}

func TestPrepareDNSZoneSyncRejectsUnsafeReceiptSchemaBeforeZoneMutation(t *testing.T) {
	path := useDNSZoneSyncTestDB(t)
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		CREATE TABLE domains (
		 id INTEGER PRIMARY KEY,
		 name VARCHAR(255) NOT NULL COLLATE NOCASE,
		 master VARCHAR(128) DEFAULT NULL,
		 last_check INTEGER DEFAULT NULL,
		 type VARCHAR(8) NOT NULL,
		 notified_serial INTEGER DEFAULT NULL,
		 account VARCHAR(40) DEFAULT NULL,
		 options VARCHAR(65535) DEFAULT NULL,
		 catalog VARCHAR(255) DEFAULT NULL
		);
		CREATE UNIQUE INDEX name_index ON domains(name);
		CREATE TABLE records (
		 id INTEGER PRIMARY KEY,
		 domain_id INTEGER,
		 name VARCHAR(255),
		 type VARCHAR(10),
		 content VARCHAR(65535),
		 ttl INTEGER,
		 prio INTEGER,
		 disabled BOOLEAN DEFAULT 0,
		 ordername VARCHAR(255),
		 auth BOOL DEFAULT 1
		);
		CREATE TABLE CELIKPANEL_DNS_ZONE_SYNC_RECEIPTS (
		 domain TEXT PRIMARY KEY,
		 request_id TEXT,
		 qualifier TEXT,
		 desired_generation INTEGER,
		 action TEXT,
		 zone_type TEXT,
		 schema TEXT
		);
		INSERT INTO domains (id, name, type, options)
		VALUES (71, 'unsafe-schema.example', 'NATIVE', 'preserve');
		INSERT INTO records
		(domain_id, name, type, content, ttl, prio, disabled, auth)
		VALUES
		(71, 'unsafe-schema.example', 'A', '192.0.2.71', 300, 0, 0, 1);
	`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	var beforeMaster string
	if err := db.QueryRow(`
		SELECT COALESCE(group_concat(entry, char(10)), '')
		FROM (
		 SELECT type || ':' || name || ':' || COALESCE(sql, '') AS entry
		 FROM sqlite_master
		 ORDER BY type, name
		)
	`).Scan(&beforeMaster); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()
	commitment := dnsZoneSyncTestCommitment(
		t, "unsafe-schema.example", 71, false, "NATIVE",
	)
	if prepared, err := prepareDNSZoneSync(
		context.Background(), testMutationRequestID, commitment,
	); err == nil || prepared != nil {
		if prepared != nil {
			prepared.close()
		}
		t.Fatalf("unsafe receipt schema prepared=%v err=%v", prepared, err)
	}
	db, err = sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var afterMaster string
	if err := db.QueryRow(`
		SELECT COALESCE(group_concat(entry, char(10)), '')
		FROM (
		 SELECT type || ':' || name || ':' || COALESCE(sql, '') AS entry
		 FROM sqlite_master
		 ORDER BY type, name
		)
	`).Scan(&afterMaster); err != nil {
		t.Fatal(err)
	}
	if afterMaster != beforeMaster {
		t.Fatalf("unsafe receipt schema changed sqlite_master\nbefore:\n%s\nafter:\n%s", beforeMaster, afterMaster)
	}
	var zoneType, options, recordContent string
	if err := db.QueryRow(`
		SELECT d.type, d.options, r.content
		FROM domains d JOIN records r ON r.domain_id = d.id
		WHERE d.id = 71
	`).Scan(&zoneType, &options, &recordContent); err != nil {
		t.Fatal(err)
	}
	if zoneType != "NATIVE" || options != "preserve" ||
		recordContent != "192.0.2.71" {
		t.Fatalf(
			"unsafe schema mutated zone type=%q options=%q record=%q",
			zoneType, options, recordContent,
		)
	}
	var receipts int
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM celikpanel_dns_zone_sync_receipts
	`).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if receipts != 0 {
		t.Fatalf("unsafe schema wrote %d receipts", receipts)
	}
}

func TestPrepareDNSZoneSyncRejectsReceiptAuthoritySideEffectsZeroTouch(t *testing.T) {
	canonicalColumns := `(
		domain TEXT NOT NULL PRIMARY KEY,
		request_id TEXT NOT NULL,
		qualifier TEXT NOT NULL,
		desired_generation INTEGER NOT NULL,
		action TEXT NOT NULL,
		zone_type TEXT NOT NULL,
		schema TEXT NOT NULL
	) STRICT, WITHOUT ROWID`
	for _, test := range []struct {
		name  string
		table string
		extra string
	}{
		{
			name: "NOCASE primary key",
			table: `(
			 domain TEXT NOT NULL PRIMARY KEY COLLATE NOCASE,
			 request_id TEXT NOT NULL, qualifier TEXT NOT NULL,
			 desired_generation INTEGER NOT NULL, action TEXT NOT NULL,
			 zone_type TEXT NOT NULL, schema TEXT NOT NULL
			) STRICT, WITHOUT ROWID`,
		},
		{
			name: "trigger", table: canonicalColumns,
			extra: `CREATE TRIGGER receipt_side_effect AFTER INSERT ON celikpanel_dns_zone_sync_receipts
			BEGIN UPDATE sentinel SET value = 'mutated'; END;`,
		},
		{
			name: "extra index", table: canonicalColumns,
			extra: `CREATE INDEX receipt_extra ON celikpanel_dns_zone_sync_receipts(request_id);`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := useDNSZoneSyncTestDB(t)
			db, err := sql.Open("sqlite", "file:"+path)
			if err != nil {
				t.Fatal(err)
			}
			setup := `CREATE TABLE sentinel(value TEXT NOT NULL); INSERT INTO sentinel VALUES ('preserve');
			CREATE TABLE celikpanel_dns_zone_sync_receipts ` + test.table + ";" + test.extra
			if _, err := db.Exec(setup); err != nil {
				db.Close()
				t.Fatal(err)
			}
			var before string
			if err := db.QueryRow(`
				SELECT COALESCE(group_concat(entry, char(10)), '') FROM (
				 SELECT type || ':' || name || ':' || COALESCE(sql, '') AS entry
				 FROM sqlite_master ORDER BY type, name
				)`).Scan(&before); err != nil {
				db.Close()
				t.Fatal(err)
			}
			db.Close()
			commitment := dnsZoneSyncTestCommitment(
				t, "schema-side-effect.example", 80, false, "NATIVE",
			)
			prepared, err := prepareDNSZoneSync(
				context.Background(), testMutationRequestID, commitment,
			)
			if err == nil || prepared != nil {
				if prepared != nil {
					prepared.close()
				}
				t.Fatalf("unsafe authority prepared=%v err=%v", prepared, err)
			}
			db, err = sql.Open("sqlite", "file:"+path+"?mode=ro")
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			var after, sentinel string
			if err := db.QueryRow(`
				SELECT COALESCE(group_concat(entry, char(10)), '') FROM (
				 SELECT type || ':' || name || ':' || COALESCE(sql, '') AS entry
				 FROM sqlite_master ORDER BY type, name
				)`).Scan(&after); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRow(`SELECT value FROM sentinel`).Scan(&sentinel); err != nil {
				t.Fatal(err)
			}
			if after != before || sentinel != "preserve" {
				t.Fatalf("unsafe authority changed database\nbefore:\n%s\nafter:\n%s\nsentinel=%q",
					before, after, sentinel)
			}
		})
	}
}
