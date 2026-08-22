//go:build linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func finishRollbackEvidenceJobForTest(
	t *testing.T,
	manager *serviceMutationManager,
) *transport.DNSEngineRollbackEvidenceRequest {
	t.Helper()
	request, manifest := rollbackEvidenceRequestForTest(t, "", 0)
	if _, err := manager.begin(&ServiceMutationBeginRequest{
		RequestID:   request.MutationRequestID,
		OwnerID:     request.MutationOwnerID,
		Kind:        "dns_engine_switch",
		Target:      string(manifest.TargetEngine),
		PackageName: manifest.Qualifier,
	}); err != nil {
		t.Fatalf("begin rollback evidence job: %v", err)
	}
	job, err := manager.finish(&ServiceMutationFinishRequest{
		RequestID:   request.MutationRequestID,
		OwnerID:     request.MutationOwnerID,
		Success:     false,
		FailureCode: "service_operation_lease_lost",
		Message:     "bounded failure",
	})
	if err != nil {
		t.Fatalf("finish rollback evidence job: %v", err)
	}
	if !exactFailedDNSEngineEvidenceJob(job, request, manifest) {
		t.Fatalf("test job is not exact terminal failure: %+v", job)
	}
	return request
}

func TestDNSEngineRollbackEvidenceRPCIsReadOnlyUnderHostFlock(t *testing.T) {
	withRollbackEvidenceReadersForTest(t)
	manager, _ := newMutationTestManager(t)
	request := finishRollbackEvidenceJobForTest(t, manager)
	installGlobalMutationTestManager(t, manager)
	manifest, err := canonicalDNSEngineRollbackEvidence(request)
	if err != nil {
		t.Fatal(err)
	}
	installReceipt := installOwnershipForEvidenceTest(request, manifest)
	installBefore := installReceipt
	installBefore.Packages = append([]string(nil), installReceipt.Packages...)
	installBefore.MissingBefore = append(
		[]string(nil), installReceipt.MissingBefore...,
	)
	readRollbackEvidenceJournal = func() (dnsEngineSwitchJournal, bool, error) {
		return dnsEngineSwitchJournal{}, false, nil
	}
	readRollbackEvidenceState = func() (dnsEngineStateReceipt, bool, error) {
		return dnsEngineStateReceipt{}, false, nil
	}
	readRollbackEvidenceOwnership = func(
		transport.DNSEngine,
	) (dnsEngineStateReceipt, bool, error) {
		return dnsEngineStateReceipt{}, false, nil
	}
	readRollbackEvidenceInstallOwnership = func(
		transport.DNSEngine,
	) (dnsEngineInstallOwnershipReceipt, bool, error) {
		return installReceipt, true, nil
	}
	sealHeldHostFlock := false
	verifyRollbackEvidenceTargetSeal = func(
		context.Context, transport.DNSEngine, dnsEngineRollbackTargetHost,
	) error {
		competing, err := acquireServiceMutationFileLock(manager.lockPath)
		if err == nil {
			_ = competing.Close()
			return errors.New("seal ran without the common host flock")
		}
		sealHeldHostFlock = errors.Is(err, errServiceMutationHostBusy)
		if !sealHeldHostFlock {
			return err
		}
		return nil
	}

	ledgerBefore, err := os.ReadFile(manager.ledgerPath)
	if err != nil {
		t.Fatal(err)
	}
	memoryBefore := cloneServiceMutationLedger(manager.ledger)
	stagePath := filepath.Join(
		filepath.Dir(manager.ledgerPath),
		".firewall-apply-journal-evidence.json",
	)
	stageBefore := []byte("{}")
	if err := os.WriteFile(stagePath, stageBefore, 0o600); err != nil {
		t.Fatal(err)
	}

	var response transport.DNSEngineRollbackEvidenceResponse
	if err := (&Agent{}).DNSEngineRollbackEvidenceV1(
		request, &response,
	); err != nil {
		t.Fatal(err)
	}
	if response.Outcome != transport.DNSEngineRollbackSafe ||
		len(response.ReceiptCommitment) != 64 || !sealHeldHostFlock {
		t.Fatalf("evidence response=%+v", response)
	}
	ledgerAfter, err := os.ReadFile(manager.ledgerPath)
	if err != nil {
		t.Fatal(err)
	}
	stageAfter, err := os.ReadFile(stagePath)
	if err != nil {
		t.Fatalf("evidence cleaned orphan sentinel: %v", err)
	}
	manager.mu.Lock()
	memoryAfter := cloneServiceMutationLedger(manager.ledger)
	poisoned, poisonLock, active := manager.poisoned, manager.poisonLock, manager.active
	manager.mu.Unlock()
	if !bytes.Equal(ledgerBefore, ledgerAfter) ||
		!bytes.Equal(stageBefore, stageAfter) ||
		!reflect.DeepEqual(memoryBefore, memoryAfter) ||
		!reflect.DeepEqual(installBefore, installReceipt) ||
		poisoned != nil || poisonLock != nil || active != nil {
		t.Fatalf(
			"evidence mutated state: ledger=%v stage=%v memory=%v install=%v poisoned=%v lock=%v active=%v",
			!bytes.Equal(ledgerBefore, ledgerAfter),
			!bytes.Equal(stageBefore, stageAfter),
			!reflect.DeepEqual(memoryBefore, memoryAfter),
			!reflect.DeepEqual(installBefore, installReceipt),
			poisoned, poisonLock != nil, active != nil,
		)
	}
}

func TestDNSEngineRollbackEvidenceRPCFailsClosedWhenHostFlockBusyOrMissing(
	t *testing.T,
) {
	t.Run("busy", func(t *testing.T) {
		withRollbackEvidenceReadersForTest(t)
		manager, _ := newMutationTestManager(t)
		request := finishRollbackEvidenceJobForTest(t, manager)
		installGlobalMutationTestManager(t, manager)
		sealCalls := 0
		verifyRollbackEvidenceTargetSeal = func(
			context.Context, transport.DNSEngine, dnsEngineRollbackTargetHost,
		) error {
			sealCalls++
			return nil
		}
		lock, err := acquireServiceMutationFileLock(manager.lockPath)
		if err != nil {
			t.Fatal(err)
		}
		var response transport.DNSEngineRollbackEvidenceResponse
		if err := (&Agent{}).DNSEngineRollbackEvidenceV1(
			request, &response,
		); err != nil {
			t.Fatal(err)
		}
		if err := lock.Close(); err != nil {
			t.Fatal(err)
		}
		if response.Outcome != transport.DNSEngineRollbackUnverified ||
			response.ReceiptCommitment != "" || sealCalls != 0 {
			t.Fatalf("busy evidence response=%+v seal_calls=%d",
				response, sealCalls)
		}
	})

	t.Run("missing does not create", func(t *testing.T) {
		withRollbackEvidenceReadersForTest(t)
		manager, _ := newMutationTestManager(t)
		request := finishRollbackEvidenceJobForTest(t, manager)
		installGlobalMutationTestManager(t, manager)
		if err := os.Remove(manager.lockPath); err != nil {
			t.Fatal(err)
		}
		var response transport.DNSEngineRollbackEvidenceResponse
		if err := (&Agent{}).DNSEngineRollbackEvidenceV1(
			request, &response,
		); err != nil {
			t.Fatal(err)
		}
		if response.Outcome != transport.DNSEngineRollbackUnverified ||
			response.ReceiptCommitment != "" {
			t.Fatalf("missing-lock evidence response=%+v", response)
		}
		if _, err := os.Lstat(manager.lockPath); !os.IsNotExist(err) {
			t.Fatalf("evidence recreated missing lock: %v", err)
		}
	})

	t.Run("unsafe metadata is not repaired", func(t *testing.T) {
		withRollbackEvidenceReadersForTest(t)
		manager, _ := newMutationTestManager(t)
		request := finishRollbackEvidenceJobForTest(t, manager)
		installGlobalMutationTestManager(t, manager)
		if err := os.Chmod(manager.lockPath, 0o640); err != nil {
			t.Fatal(err)
		}
		sealCalls := 0
		verifyRollbackEvidenceTargetSeal = func(
			context.Context, transport.DNSEngine, dnsEngineRollbackTargetHost,
		) error {
			sealCalls++
			return nil
		}
		var response transport.DNSEngineRollbackEvidenceResponse
		if err := (&Agent{}).DNSEngineRollbackEvidenceV1(
			request, &response,
		); err != nil {
			t.Fatal(err)
		}
		info, err := os.Lstat(manager.lockPath)
		if err != nil {
			t.Fatal(err)
		}
		if response.Outcome != transport.DNSEngineRollbackUnverified ||
			response.ReceiptCommitment != "" || sealCalls != 0 ||
			info.Mode().Perm() != 0o640 {
			t.Fatalf("unsafe-metadata evidence=%+v seal=%d mode=%#o",
				response, sealCalls, info.Mode().Perm())
		}
	})
}

func TestDNSEngineRollbackEvidenceRPCRejectsLedgerDriftAcrossHostProof(t *testing.T) {
	withRollbackEvidenceReadersForTest(t)
	manager, _ := newMutationTestManager(t)
	request := finishRollbackEvidenceJobForTest(t, manager)
	installGlobalMutationTestManager(t, manager)
	readRollbackEvidenceJournal = func() (dnsEngineSwitchJournal, bool, error) {
		return dnsEngineSwitchJournal{}, false, nil
	}
	readRollbackEvidenceState = func() (dnsEngineStateReceipt, bool, error) {
		return dnsEngineStateReceipt{}, false, nil
	}
	readRollbackEvidenceOwnership = func(
		transport.DNSEngine,
	) (dnsEngineStateReceipt, bool, error) {
		return dnsEngineStateReceipt{}, false, nil
	}
	readRollbackEvidenceInstallOwnership = func(
		transport.DNSEngine,
	) (dnsEngineInstallOwnershipReceipt, bool, error) {
		return dnsEngineInstallOwnershipReceipt{}, false, nil
	}
	verifyRollbackEvidenceTargetSeal = func(
		context.Context, transport.DNSEngine, dnsEngineRollbackTargetHost,
	) error {
		ledger, err := manager.loadLedgerFromDisk()
		if err != nil {
			return err
		}
		ledger.Jobs[request.MutationRequestID].ErrorMessage =
			"same identity, changed terminal receipt"
		encoded, err := json.Marshal(&ledger)
		if err != nil {
			return err
		}
		return os.WriteFile(manager.ledgerPath, encoded, 0o600)
	}
	var response transport.DNSEngineRollbackEvidenceResponse
	if err := (&Agent{}).DNSEngineRollbackEvidenceV1(
		request, &response,
	); err != nil {
		t.Fatal(err)
	}
	if response.Outcome != transport.DNSEngineRollbackIdentityMismatch ||
		response.ReceiptCommitment != "" {
		t.Fatalf("ledger-drift evidence response=%+v", response)
	}
}

func TestDNSEngineRollbackEvidenceSourceHasNoStatusOrManagerInitialization(
	t *testing.T,
) {
	raw, err := os.ReadFile("dns_engine_rollback_evidence.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	for _, forbidden := range []string{
		".status(",
		"agentServiceMutationManager()",
		"tryResolvePersistedOrphan",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("comparison-only evidence contains %q", forbidden)
		}
	}
}
