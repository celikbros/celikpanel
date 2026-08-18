//go:build linux

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

type dnsZoneV3LifecycleBackend struct {
	syncFn       func(context.Context, mutationpayload.DNSZoneSyncV3Commitment, transport.ServiceMutationBinding) (string, error)
	recoverFn    func(context.Context, string, string, transport.ServiceMutationBinding) (bool, error)
	recoverCalls int
}

func (*dnsZoneV3LifecycleBackend) Readiness(context.Context) (transport.DNSBackendReadinessResponse, error) {
	return transport.DNSBackendReadinessResponse{}, nil
}

func (b *dnsZoneV3LifecycleBackend) Sync(
	ctx context.Context,
	commitment mutationpayload.DNSZoneSyncV3Commitment,
	binding transport.ServiceMutationBinding,
) (string, error) {
	if b.syncFn == nil {
		return "", errors.New("unexpected DNS V3 sync")
	}
	return b.syncFn(ctx, commitment, binding)
}

func (b *dnsZoneV3LifecycleBackend) RecoverZone(
	ctx context.Context,
	domain, qualifier string,
	binding transport.ServiceMutationBinding,
) (bool, error) {
	b.recoverCalls++
	if b.recoverFn == nil {
		return false, errors.New("unexpected DNS V3 recovery")
	}
	return b.recoverFn(ctx, domain, qualifier, binding)
}

func (*dnsZoneV3LifecycleBackend) Switch(
	context.Context,
	mutationpayload.DNSEngineSwitchManifestCommitment,
	transport.ServiceMutationBinding,
) (transport.SwitchDNSEngineV1Response, error) {
	return transport.SwitchDNSEngineV1Response{}, errors.New("unexpected DNS engine switch")
}

func (*dnsZoneV3LifecycleBackend) RecoverSwitch(
	context.Context,
	transport.DNSEngine,
	string,
	transport.ServiceMutationBinding,
) (dnsEngineSwitchRecoveryOutcome, error) {
	return dnsEngineSwitchRecoveryAbsent, errors.New("unexpected DNS engine switch recovery")
}

func (*dnsZoneV3LifecycleBackend) FinalizeSwitch(
	context.Context,
	transport.DNSEngine,
	string,
	transport.ServiceMutationBinding,
) error {
	return errors.New("unexpected DNS engine switch finalization")
}

func dnsZoneV3LifecycleRequest(t *testing.T) (SyncDNSZoneV3Request, mutationpayload.DNSZoneSyncV3Commitment) {
	t.Helper()
	domain := "pending-v3.example.test"
	commitment, err := mutationpayload.CanonicalDNSZoneSyncV3(
		transport.DNSEngineBIND,
		7,
		11,
		domain,
		false,
		"MASTER",
		testPDNSEngineRecords(domain),
	)
	if err != nil {
		t.Fatal(err)
	}
	return SyncDNSZoneV3Request{
		ServiceMutationBinding: mutationTestBinding(),
		Engine:                 transport.DNSEngineBIND,
		EngineEpoch:            7,
		DesiredGeneration:      11,
		Domain:                 domain,
		ZoneType:               "MASTER",
		Records:                append([]transport.ZoneRecord(nil), commitment.Records...),
	}, commitment
}

func beginDNSZoneV3LifecycleJob(
	t *testing.T,
	manager *serviceMutationManager,
	commitment mutationpayload.DNSZoneSyncV3Commitment,
) *ServiceMutationJob {
	t.Helper()
	return beginMutationTestJobWithIdentity(
		t,
		manager,
		"dns_zone_sync",
		commitment.Domain,
		commitment.Qualifier,
	)
}

func exactPendingDNSZoneV3Backend() *dnsZoneV3LifecycleBackend {
	return &dnsZoneV3LifecycleBackend{
		syncFn: func(
			ctx context.Context,
			commitment mutationpayload.DNSZoneSyncV3Commitment,
			_ transport.ServiceMutationBinding,
		) (string, error) {
			if err := markDNSZoneSyncV3Applied(
				ctx, commitment.Domain, commitment.Qualifier,
			); err != nil {
				return "", err
			}
			return "", dnsZoneV3RecoveryPending(errors.New("peer unavailable"))
		},
	}
}

func runInitialDNSZoneV3Pending(
	t *testing.T,
	manager *serviceMutationManager,
	backend *dnsZoneV3LifecycleBackend,
) (SyncDNSZoneV3Request, mutationpayload.DNSZoneSyncV3Commitment, *ServiceMutationJob) {
	t.Helper()
	request, commitment := dnsZoneV3LifecycleRequest(t)
	beginDNSZoneV3LifecycleJob(t, manager, commitment)
	useFakeDNSEngineBackend(t, backend)
	var response SyncDNSZoneV3Response
	if err := (&Agent{}).SyncDNSZoneV3(&request, &response); err != nil {
		t.Fatal(err)
	}
	if !response.RecoveryPending || response.Synced || response.Error != "" ||
		response.Engine != request.Engine || response.EngineEpoch != request.EngineEpoch ||
		response.AppliedGeneration != request.DesiredGeneration {
		t.Fatalf("pending response=%+v", response)
	}
	job := manager.status(testMutationRequestID)
	wantPhase, err := formatDNSZoneSyncV3Phase(
		dnsZoneSyncV3PropagationPending,
		testMutationRequestID,
		commitment.Domain,
		commitment.Qualifier,
	)
	if err != nil {
		t.Fatal(err)
	}
	manager.mu.Lock()
	active := manager.active
	activeRequestID := manager.ledger.ActiveRequestID
	ledger := cloneServiceMutationLedger(manager.ledger)
	manager.mu.Unlock()
	if job == nil || job.Status != serviceMutationStatusPending ||
		job.Phase != wantPhase || active != nil || activeRequestID != "" {
		t.Fatalf("pending job=%+v active=%+v ledger=%+v", job, active, ledger)
	}
	return request, commitment, job
}

func TestDNSZoneV3PendingReleasesHostLockAndExactResumeSucceeds(t *testing.T) {
	manager, root := newMutationTestManager(t)
	installGlobalMutationTestManager(t, manager)
	clock := time.Now().UTC()
	manager.now = func() time.Time { return clock }
	backend := exactPendingDNSZoneV3Backend()
	backend.recoverFn = func(
		context.Context, string, string, transport.ServiceMutationBinding,
	) (bool, error) {
		return true, nil
	}
	request, commitment, pending := runInitialDNSZoneV3Pending(t, manager, backend)

	finished, err := manager.finish(&ServiceMutationFinishRequest{
		RequestID: testMutationRequestID,
		OwnerID:   testMutationOwnerID,
		Success:   false,
	})
	if err != nil || finished == nil || finished.Status != serviceMutationStatusPending {
		t.Fatalf("Finish(false) pending job=%+v err=%v", finished, err)
	}
	lock, err := acquireServiceMutationFileLock(filepath.Join(root, "service-mutation.lock"))
	if err != nil {
		t.Fatalf("pending job retained global host lock: %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	otherCommitment, err := mutationpayload.CanonicalDNSZoneSyncV3(
		transport.DNSEngineBIND, 7, 12, "other-pending-v3.example.test",
		false, "MASTER", testPDNSEngineRecords("other-pending-v3.example.test"),
	)
	if err != nil {
		t.Fatal(err)
	}
	blockedDNS, err := manager.begin(&ServiceMutationBeginRequest{
		RequestID: strings.Repeat("3", 32), OwnerID: strings.Repeat("4", 32),
		Kind: "dns_zone_sync", Target: otherCommitment.Domain,
		PackageName: otherCommitment.Qualifier,
	})
	if err == nil || blockedDNS != nil || manager.status(strings.Repeat("3", 32)) != nil {
		t.Fatalf("new DNS mutation crossed pending gate: job=%+v err=%v", blockedDNS, err)
	}
	blockedSwitch, err := manager.begin(&ServiceMutationBeginRequest{
		RequestID: strings.Repeat("5", 32), OwnerID: strings.Repeat("6", 32),
		Kind: "dns_engine_switch", Target: "pdns",
	})
	if err == nil || blockedSwitch != nil {
		t.Fatalf("DNS engine switch crossed pending gate: job=%+v err=%v", blockedSwitch, err)
	}

	clock = pending.DeadlineAt.Add(time.Hour)
	unrelated, err := manager.begin(&ServiceMutationBeginRequest{
		RequestID: testMutationSecondRequestID,
		OwnerID:   strings.Repeat("2", 32),
		Kind:      "service_install",
		Target:    "apache",
	})
	if err != nil || unrelated == nil || unrelated.Status != serviceMutationStatusRunning {
		t.Fatalf("unrelated begin=%+v err=%v", unrelated, err)
	}
	if _, err := manager.finish(&ServiceMutationFinishRequest{
		RequestID: unrelated.RequestID,
		OwnerID:   unrelated.OwnerID,
		Success:   false,
	}); err != nil {
		t.Fatal(err)
	}

	wrongOwner, err := manager.begin(&ServiceMutationBeginRequest{
		RequestID:   testMutationRequestID,
		OwnerID:     strings.Repeat("1", 32),
		Kind:        "dns_zone_sync",
		Target:      commitment.Domain,
		PackageName: commitment.Qualifier,
		Resume:      true,
	})
	if err == nil || wrongOwner == nil || wrongOwner.Status != serviceMutationStatusPending {
		t.Fatalf("wrong owner resume=%+v err=%v", wrongOwner, err)
	}

	resumed, err := manager.begin(&ServiceMutationBeginRequest{
		RequestID:   testMutationRequestID,
		OwnerID:     testMutationOwnerID,
		Kind:        "dns_zone_sync",
		Target:      commitment.Domain,
		PackageName: commitment.Qualifier,
		Resume:      true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Attempt != pending.Attempt+1 || !resumed.StartedAt.Equal(clock) ||
		!resumed.DeadlineAt.After(pending.DeadlineAt) {
		t.Fatalf("resumed job=%+v pending=%+v clock=%v", resumed, pending, clock)
	}
	var recovered RecoverDNSZoneV3Response
	if err := (&Agent{}).RecoverDNSZoneV3(&RecoverDNSZoneV3Request{
		ServiceMutationBinding: request.ServiceMutationBinding,
		Domain:                 commitment.Domain,
		Qualifier:              commitment.Qualifier,
	}, &recovered); err != nil {
		t.Fatal(err)
	}
	terminal := manager.status(testMutationRequestID)
	if !recovered.Recovered || recovered.RecoveryPending || recovered.Error != "" ||
		terminal == nil || terminal.Status != serviceMutationStatusSucceeded {
		t.Fatalf("recovery=%+v terminal=%+v", recovered, terminal)
	}
}

func TestDNSZoneV3RecoveryPendingSurvivesCancelAndDeadline(t *testing.T) {
	manager, root := newMutationTestManager(t)
	installGlobalMutationTestManager(t, manager)
	clock := time.Now().UTC()
	manager.now = func() time.Time { return clock }
	backend := exactPendingDNSZoneV3Backend()
	backend.recoverFn = func(
		context.Context, string, string, transport.ServiceMutationBinding,
	) (bool, error) {
		return false, dnsZoneV3RecoveryPending(errors.New("peer still unavailable"))
	}
	request, commitment, _ := runInitialDNSZoneV3Pending(t, manager, backend)

	resume := func() *ServiceMutationJob {
		t.Helper()
		job, err := manager.begin(&ServiceMutationBeginRequest{
			RequestID: testMutationRequestID, OwnerID: testMutationOwnerID,
			Kind: "dns_zone_sync", Target: commitment.Domain,
			PackageName: commitment.Qualifier, Resume: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		return job
	}

	resume()
	var response RecoverDNSZoneV3Response
	if err := (&Agent{}).RecoverDNSZoneV3(&RecoverDNSZoneV3Request{
		ServiceMutationBinding: request.ServiceMutationBinding,
		Domain:                 commitment.Domain, Qualifier: commitment.Qualifier,
	}, &response); err != nil {
		t.Fatal(err)
	}
	if !response.RecoveryPending || response.Recovered ||
		manager.status(testMutationRequestID).Status != serviceMutationStatusPending {
		t.Fatalf("repeated pending response=%+v job=%+v", response, manager.status(testMutationRequestID))
	}

	resume()
	cancelled, err := manager.cancelJob(&ServiceMutationCancelRequest{
		RequestID: testMutationRequestID, ExpectedOwner: testMutationOwnerID,
	})
	if err != nil || cancelled == nil || cancelled.Status != serviceMutationStatusPending {
		t.Fatalf("cancelled recovery=%+v err=%v", cancelled, err)
	}

	resumed := resume()
	manager.mu.Lock()
	runtime := manager.active
	manager.mu.Unlock()
	clock = resumed.DeadlineAt.Add(time.Hour)
	manager.expire(runtime)
	expired := manager.status(testMutationRequestID)
	if expired == nil || expired.Status != serviceMutationStatusPending || manager.active != nil {
		t.Fatalf("expired recovery=%+v active=%+v", expired, manager.active)
	}
	lock, err := acquireServiceMutationFileLock(filepath.Join(root, "service-mutation.lock"))
	if err != nil {
		t.Fatalf("expired pending recovery retained lock: %v", err)
	}
	_ = lock.Close()
}

func TestDNSZoneV3AppliedExpiryRaceCannotBecomeTerminalFailure(t *testing.T) {
	manager, _ := newMutationTestManager(t)
	installGlobalMutationTestManager(t, manager)
	request, commitment := dnsZoneV3LifecycleRequest(t)
	beginDNSZoneV3LifecycleJob(t, manager, commitment)
	applied := make(chan struct{})
	backend := &dnsZoneV3LifecycleBackend{
		syncFn: func(
			ctx context.Context,
			commitment mutationpayload.DNSZoneSyncV3Commitment,
			_ transport.ServiceMutationBinding,
		) (string, error) {
			if err := markDNSZoneSyncV3Applied(ctx, commitment.Domain, commitment.Qualifier); err != nil {
				return "", err
			}
			close(applied)
			<-ctx.Done()
			return "", dnsZoneV3RecoveryPending(errors.New("peer proof cancelled"))
		},
	}
	useFakeDNSEngineBackend(t, backend)
	responseCh := make(chan SyncDNSZoneV3Response, 1)
	go func() {
		var response SyncDNSZoneV3Response
		_ = (&Agent{}).SyncDNSZoneV3(&request, &response)
		responseCh <- response
	}()
	<-applied
	manager.mu.Lock()
	runtime := manager.active
	manager.mu.Unlock()
	manager.expire(runtime)
	select {
	case response := <-responseCh:
		if !response.RecoveryPending || response.Error != "" {
			t.Fatalf("expiry-race response=%+v", response)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("expiry-race SyncDNSZoneV3 did not return")
	}
	job := manager.status(testMutationRequestID)
	if job == nil || job.Status != serviceMutationStatusPending {
		t.Fatalf("expiry-race job=%+v", job)
	}
}

func TestDNSZoneV3PrecommitFailureRetiresAttempt(t *testing.T) {
	manager, _ := newMutationTestManager(t)
	installGlobalMutationTestManager(t, manager)
	request, commitment := dnsZoneV3LifecycleRequest(t)
	beginDNSZoneV3LifecycleJob(t, manager, commitment)
	useFakeDNSEngineBackend(t, &dnsZoneV3LifecycleBackend{
		syncFn: func(
			context.Context,
			mutationpayload.DNSZoneSyncV3Commitment,
			transport.ServiceMutationBinding,
		) (string, error) {
			return "", errors.New("permanent precommit rejection")
		},
	})
	var response SyncDNSZoneV3Response
	if err := (&Agent{}).SyncDNSZoneV3(&request, &response); err != nil {
		t.Fatal(err)
	}
	if response.Error == "" || response.RecoveryPending || response.Synced ||
		response.Engine != "" || response.EngineEpoch != 0 ||
		response.AppliedGeneration != 0 {
		t.Fatalf("precommit response=%+v", response)
	}
	failed, err := manager.finish(&ServiceMutationFinishRequest{
		RequestID: testMutationRequestID, OwnerID: testMutationOwnerID,
		Success: false,
	})
	if err != nil || failed == nil || failed.Status != serviceMutationStatusFailed {
		t.Fatalf("precommit terminal=%+v err=%v", failed, err)
	}
}

func TestDNSZoneV3TypedPendingWithoutAppliedAuthorityPoisons(t *testing.T) {
	manager, root := newMutationTestManager(t)
	installGlobalMutationTestManager(t, manager)
	defer releasePoisonedFirewallApplyTestManager(manager)
	request, commitment := dnsZoneV3LifecycleRequest(t)
	beginDNSZoneV3LifecycleJob(t, manager, commitment)
	useFakeDNSEngineBackend(t, &dnsZoneV3LifecycleBackend{
		syncFn: func(
			context.Context,
			mutationpayload.DNSZoneSyncV3Commitment,
			transport.ServiceMutationBinding,
		) (string, error) {
			return "", dnsZoneV3RecoveryPending(errors.New("fabricated pending"))
		},
	})
	var response SyncDNSZoneV3Response
	if err := (&Agent{}).SyncDNSZoneV3(&request, &response); err != nil {
		t.Fatal(err)
	}
	manager.mu.Lock()
	poisoned := manager.poisoned
	active := manager.active
	job := cloneServiceMutationJob(manager.ledger.Jobs[testMutationRequestID])
	manager.mu.Unlock()
	if response.Error == "" || response.RecoveryPending || response.Synced ||
		response.Engine != "" || response.EngineEpoch != 0 ||
		response.AppliedGeneration != 0 || poisoned == nil || active == nil ||
		job == nil || strings.Contains(job.Phase, "/applied/") {
		t.Fatalf("fabricated pending response=%+v poison=%v active=%+v job=%+v",
			response, poisoned, active, job)
	}
	if lock, err := acquireServiceMutationFileLock(filepath.Join(root, "service-mutation.lock")); !errors.Is(err, errServiceMutationHostBusy) {
		if lock != nil {
			_ = lock.Close()
		}
		t.Fatalf("fabricated pending released fail-closed host lock: %v", err)
	}
}

func TestDNSZoneV3PendingPersistFailurePoisonsAndRetainsAppliedReceipt(t *testing.T) {
	manager, root := newMutationTestManager(t)
	installGlobalMutationTestManager(t, manager)
	defer releasePoisonedFirewallApplyTestManager(manager)
	request, commitment := dnsZoneV3LifecycleRequest(t)
	beginDNSZoneV3LifecycleJob(t, manager, commitment)
	backend := &dnsZoneV3LifecycleBackend{
		syncFn: func(
			ctx context.Context,
			commitment mutationpayload.DNSZoneSyncV3Commitment,
			_ transport.ServiceMutationBinding,
		) (string, error) {
			if err := markDNSZoneSyncV3Applied(ctx, commitment.Domain, commitment.Qualifier); err != nil {
				return "", err
			}
			manager.mu.Lock()
			manager.writeFault = func(stage string) error {
				if stage == serviceMutationWriteFaultBeforeRename {
					return errors.New("injected pending receipt failure")
				}
				return nil
			}
			manager.mu.Unlock()
			return "", dnsZoneV3RecoveryPending(errors.New("peer unavailable"))
		},
	}
	useFakeDNSEngineBackend(t, backend)
	var response SyncDNSZoneV3Response
	if err := (&Agent{}).SyncDNSZoneV3(&request, &response); err != nil {
		t.Fatal(err)
	}
	if response.Error == "" || response.RecoveryPending ||
		response.Engine != "" || response.EngineEpoch != 0 {
		t.Fatalf("persist failure response=%+v", response)
	}
	if manager.poisoned == nil || manager.active == nil {
		t.Fatalf("persist failure did not poison active manager: poison=%v active=%+v", manager.poisoned, manager.active)
	}
	if _, err := manager.finish(&ServiceMutationFinishRequest{
		RequestID: testMutationRequestID, OwnerID: testMutationOwnerID,
		Success: false,
	}); !errors.Is(err, errServiceMutationManagerPoisoned) {
		t.Fatalf("Finish(false) after applied persist failure err=%v", err)
	}
	raw, err := os.ReadFile(manager.ledgerPath)
	if err != nil {
		t.Fatal(err)
	}
	durable, err := decodeServiceMutationLedger(raw)
	if err != nil {
		t.Fatal(err)
	}
	job := durable.Jobs[testMutationRequestID]
	if job == nil || job.Status != serviceMutationStatusRunning ||
		!strings.Contains(job.Phase, "/applied/") {
		t.Fatalf("durable applied receipt=%+v", job)
	}
	if lock, err := acquireServiceMutationFileLock(filepath.Join(root, "service-mutation.lock")); !errors.Is(err, errServiceMutationHostBusy) {
		if lock != nil {
			_ = lock.Close()
		}
		t.Fatalf("poisoned pending persist released host lock: %v", err)
	}
}

func TestDNSZoneV3PendingAfterRenameFailureRemainsRecoverable(t *testing.T) {
	manager, root := newMutationTestManager(t)
	installGlobalMutationTestManager(t, manager)
	request, commitment := dnsZoneV3LifecycleRequest(t)
	beginDNSZoneV3LifecycleJob(t, manager, commitment)
	fired := false
	backend := &dnsZoneV3LifecycleBackend{
		syncFn: func(
			ctx context.Context,
			commitment mutationpayload.DNSZoneSyncV3Commitment,
			_ transport.ServiceMutationBinding,
		) (string, error) {
			if err := markDNSZoneSyncV3Applied(ctx, commitment.Domain, commitment.Qualifier); err != nil {
				return "", err
			}
			manager.mu.Lock()
			manager.writeFault = func(stage string) error {
				if stage == serviceMutationWriteFaultAfterRename && !fired {
					fired = true
					return errors.New("injected ambiguous pending receipt failure")
				}
				return nil
			}
			manager.mu.Unlock()
			return "", dnsZoneV3RecoveryPending(errors.New("peer unavailable"))
		},
	}
	useFakeDNSEngineBackend(t, backend)
	var response SyncDNSZoneV3Response
	if err := (&Agent{}).SyncDNSZoneV3(&request, &response); err != nil {
		t.Fatal(err)
	}
	if !fired || response.Error == "" || response.RecoveryPending ||
		manager.poisoned == nil || manager.active == nil {
		t.Fatalf("after-rename response=%+v fired=%v poison=%v active=%+v",
			response, fired, manager.poisoned, manager.active)
	}
	raw, err := os.ReadFile(manager.ledgerPath)
	if err != nil {
		t.Fatal(err)
	}
	durable, err := decodeServiceMutationLedger(raw)
	if err != nil {
		t.Fatal(err)
	}
	durableJob := durable.Jobs[testMutationRequestID]
	if durableJob == nil || durableJob.Status == serviceMutationStatusFailed ||
		(durableJob.Status != serviceMutationStatusPending &&
			!strings.Contains(durableJob.Phase, "/applied/")) {
		t.Fatalf("after-rename durable receipt=%+v", durableJob)
	}
	manager.mu.Lock()
	runtime := manager.active
	manager.active = nil
	if runtime != nil {
		runtime.cancel()
	}
	manager.mu.Unlock()
	if runtime == nil || runtime.lock == nil {
		t.Fatal("after-rename poisoned manager lost its retained lock")
	}
	if err := runtime.lock.Close(); err != nil {
		t.Fatal(err)
	}
	backend.recoverFn = func(
		context.Context, string, string, transport.ServiceMutationBinding,
	) (bool, error) {
		return false, dnsZoneV3RecoveryPending(errors.New("peer remains unavailable"))
	}
	reloaded, err := newServiceMutationManager(
		filepath.Join(root, "state"), filepath.Join(root, "service-mutation.lock"),
	)
	if err != nil {
		t.Fatalf("reload recoverable after-rename receipt: %v", err)
	}
	job := reloaded.status(testMutationRequestID)
	if job == nil || job.Status != serviceMutationStatusPending ||
		reloaded.active != nil || reloaded.ledger.ActiveRequestID != "" {
		t.Fatalf("reloaded after-rename job=%+v active=%+v ledger=%+v",
			job, reloaded.active, reloaded.ledger)
	}
}

func TestDNSZoneV3RecoveryMissingReceiptPoisonsAndNeverReturnsPending(t *testing.T) {
	manager, root := newMutationTestManager(t)
	installGlobalMutationTestManager(t, manager)
	defer releasePoisonedFirewallApplyTestManager(manager)
	backend := exactPendingDNSZoneV3Backend()
	backend.recoverFn = func(
		context.Context, string, string, transport.ServiceMutationBinding,
	) (bool, error) {
		return false, nil
	}
	request, commitment, _ := runInitialDNSZoneV3Pending(t, manager, backend)
	if _, err := manager.begin(&ServiceMutationBeginRequest{
		RequestID: testMutationRequestID, OwnerID: testMutationOwnerID,
		Kind: "dns_zone_sync", Target: commitment.Domain,
		PackageName: commitment.Qualifier, Resume: true,
	}); err != nil {
		t.Fatal(err)
	}
	var response RecoverDNSZoneV3Response
	if err := (&Agent{}).RecoverDNSZoneV3(&RecoverDNSZoneV3Request{
		ServiceMutationBinding: request.ServiceMutationBinding,
		Domain:                 commitment.Domain, Qualifier: commitment.Qualifier,
	}, &response); err != nil {
		t.Fatal(err)
	}
	if response.Error == "" || response.RecoveryPending || response.Recovered ||
		manager.poisoned == nil || manager.active == nil {
		t.Fatalf("missing receipt response=%+v poison=%v active=%+v", response, manager.poisoned, manager.active)
	}
	if lock, err := acquireServiceMutationFileLock(filepath.Join(root, "service-mutation.lock")); !errors.Is(err, errServiceMutationHostBusy) {
		if lock != nil {
			_ = lock.Close()
		}
		t.Fatalf("ambiguous recovery released host lock: %v", err)
	}
}

func persistAppliedDNSZoneV3Crash(
	t *testing.T,
	manager *serviceMutationManager,
	commitment mutationpayload.DNSZoneSyncV3Commitment,
) {
	t.Helper()
	beginDNSZoneV3LifecycleJob(t, manager, commitment)
	ctx, done, err := manager.acquireStep(
		mutationTestBinding(),
		newServiceMutationStepClaim(
			serviceMutationStepSyncDNSZoneV3,
			commitment.Domain,
			commitment.Qualifier,
			"sync",
		),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := markDNSZoneSyncV3Applied(ctx, commitment.Domain, commitment.Qualifier); err != nil {
		done()
		t.Fatal(err)
	}
	done()
	abandonFirewallApplyTestRuntime(t, manager)
}

func TestDNSZoneV3AgentStartupTurnsAppliedPeerTimeoutIntoPending(t *testing.T) {
	manager, root := newMutationTestManager(t)
	_, commitment := dnsZoneV3LifecycleRequest(t)
	persistAppliedDNSZoneV3Crash(t, manager, commitment)
	backend := &dnsZoneV3LifecycleBackend{
		recoverFn: func(
			context.Context, string, string, transport.ServiceMutationBinding,
		) (bool, error) {
			return false, dnsZoneV3RecoveryPending(errors.New("peer unavailable at startup"))
		},
	}
	useFakeDNSEngineBackend(t, backend)
	reloaded, err := newServiceMutationManager(
		filepath.Join(root, "state"),
		filepath.Join(root, "service-mutation.lock"),
	)
	if err != nil {
		t.Fatal(err)
	}
	job := reloaded.status(testMutationRequestID)
	if backend.recoverCalls != 1 || job == nil ||
		job.Status != serviceMutationStatusPending ||
		reloaded.ledger.ActiveRequestID != "" || reloaded.active != nil {
		t.Fatalf("startup recover calls=%d job=%+v ledger=%+v active=%+v", backend.recoverCalls, job, reloaded.ledger, reloaded.active)
	}
	lock, err := acquireServiceMutationFileLock(filepath.Join(root, "service-mutation.lock"))
	if err != nil {
		t.Fatalf("startup pending retained host lock: %v", err)
	}
	_ = lock.Close()
	again, err := newServiceMutationManager(
		filepath.Join(root, "state"),
		filepath.Join(root, "service-mutation.lock"),
	)
	if err != nil || again.status(testMutationRequestID).Status != serviceMutationStatusPending ||
		backend.recoverCalls != 1 {
		t.Fatalf("pending restart manager=%+v err=%v recoverCalls=%d", again, err, backend.recoverCalls)
	}
}

func TestDNSZoneV3AgentStartupAppliedReceiptLossPoisons(t *testing.T) {
	manager, root := newMutationTestManager(t)
	_, commitment := dnsZoneV3LifecycleRequest(t)
	persistAppliedDNSZoneV3Crash(t, manager, commitment)
	backend := &dnsZoneV3LifecycleBackend{
		recoverFn: func(
			context.Context, string, string, transport.ServiceMutationBinding,
		) (bool, error) {
			return false, nil
		},
	}
	useFakeDNSEngineBackend(t, backend)
	reloaded, err := newServiceMutationManager(
		filepath.Join(root, "state"),
		filepath.Join(root, "service-mutation.lock"),
	)
	if !errors.Is(err, errServiceMutationManagerPoisoned) || reloaded == nil ||
		reloaded.poisoned == nil {
		t.Fatalf("startup receipt loss manager=%+v err=%v", reloaded, err)
	}
	releasePoisonedFirewallApplyTestManager(reloaded)
}

func TestDNSZoneV3StartupSuccessProtectsExactReceiptUnderHistoryPressure(t *testing.T) {
	manager, root := newMutationTestManager(t)
	_, commitment := dnsZoneV3LifecycleRequest(t)
	persistAppliedDNSZoneV3Crash(t, manager, commitment)
	manager.mu.Lock()
	ledger := cloneServiceMutationLedger(manager.ledger)
	manager.mu.Unlock()
	base := ledger.Jobs[testMutationRequestID]
	if base == nil {
		t.Fatal("startup history fixture lost applied job")
	}
	finished := base.StartedAt.Add(time.Minute)
	for index := 0; index < serviceMutationHistoryLimit-1; index++ {
		requestID := fmt.Sprintf("%032x", index+8192)
		pending := cloneServiceMutationJob(base)
		pending.RequestID = requestID
		pending.Status = serviceMutationStatusPending
		pending.Phase, _ = formatDNSZoneSyncV3Phase(
			dnsZoneSyncV3PropagationPending,
			requestID,
			commitment.Domain,
			commitment.Qualifier,
		)
		pending.UpdatedAt = finished
		pending.FinishedAt = finished
		pending.LeaseExpiresAt = time.Time{}
		pending.WorkerPID = 0
		pending.WorkerStarted = ""
		pending.WorkerCommand = ""
		ledger.Jobs[requestID] = pending
	}
	oldTerminalID := strings.Repeat("f", 32)
	oldTerminal := cloneServiceMutationJob(base)
	oldTerminal.RequestID = oldTerminalID
	oldTerminal.Status = serviceMutationStatusFailed
	oldTerminal.Phase = "failed"
	oldTerminal.ErrorCode = "old_failure"
	oldTerminal.ErrorMessage = "future-dated old terminal"
	oldTerminal.UpdatedAt = finished.Add(24 * time.Hour)
	oldTerminal.FinishedAt = oldTerminal.UpdatedAt
	oldTerminal.LeaseExpiresAt = time.Time{}
	oldTerminal.WorkerPID = 0
	oldTerminal.WorkerStarted = ""
	oldTerminal.WorkerCommand = ""
	ledger.Jobs[oldTerminalID] = oldTerminal
	if err := validateServiceMutationLedger(&ledger); err != nil {
		t.Fatalf("startup history fixture ledger: %v", err)
	}
	raw, err := json.Marshal(&ledger)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manager.ledgerPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	backend := &dnsZoneV3LifecycleBackend{
		recoverFn: func(
			context.Context, string, string, transport.ServiceMutationBinding,
		) (bool, error) {
			return true, nil
		},
	}
	useFakeDNSEngineBackend(t, backend)
	reloaded, err := newServiceMutationManager(
		filepath.Join(root, "state"), filepath.Join(root, "service-mutation.lock"),
	)
	if err != nil {
		t.Fatal(err)
	}
	job := reloaded.status(testMutationRequestID)
	if job == nil || job.Status != serviceMutationStatusSucceeded ||
		!strings.Contains(job.Phase, "/published/") {
		t.Fatalf("startup protected receipt=%+v", job)
	}
	if reloaded.status(oldTerminalID) != nil {
		t.Fatal("startup trim retained future-dated old terminal over exact published receipt")
	}
}

func TestDNSZoneV3PendingLedgerRejectsForeignPhase(t *testing.T) {
	_, commitment := dnsZoneV3LifecycleRequest(t)
	started := time.Now().UTC()
	phase, err := formatDNSZoneSyncV3Phase(
		dnsZoneSyncV3PropagationPending,
		testMutationRequestID,
		commitment.Domain,
		commitment.Qualifier,
	)
	if err != nil {
		t.Fatal(err)
	}
	job := &ServiceMutationJob{
		RequestID: testMutationRequestID, OwnerID: testMutationOwnerID,
		Kind: "dns_zone_sync", Target: commitment.Domain,
		PackageName: commitment.Qualifier,
		Status:      serviceMutationStatusPending, Phase: phase, Attempt: 1,
		StartedAt: started, UpdatedAt: started.Add(time.Minute),
		FinishedAt: started.Add(time.Minute), DeadlineAt: started.Add(time.Hour),
	}
	valid := serviceMutationLedger{
		Version: serviceMutationLedgerVersion,
		Jobs:    map[string]*ServiceMutationJob{testMutationRequestID: job},
	}
	if err := validateServiceMutationLedger(&valid); err != nil {
		t.Fatal(err)
	}
	for _, hostile := range []string{"", "failed", "commit/dns-zone-sync/v3/published/" + testMutationRequestID + "/" + commitment.Domain + "/" + commitment.Qualifier} {
		ledger := cloneServiceMutationLedger(valid)
		ledger.Jobs[testMutationRequestID].Phase = hostile
		if err := validateServiceMutationLedger(&ledger); err == nil {
			t.Fatalf("pending ledger accepted hostile phase %q", hostile)
		}
	}
}

func TestDNSZoneV3NewestSuccessSurvivesPendingHistoryPressure(t *testing.T) {
	manager, _ := newMutationTestManager(t)
	installGlobalMutationTestManager(t, manager)
	backend := exactPendingDNSZoneV3Backend()
	backend.recoverFn = func(
		context.Context, string, string, transport.ServiceMutationBinding,
	) (bool, error) {
		return true, nil
	}
	request, commitment, pending := runInitialDNSZoneV3Pending(t, manager, backend)
	manager.mu.Lock()
	before := cloneServiceMutationLedger(manager.ledger)
	for index := 0; index < serviceMutationHistoryLimit-1; index++ {
		requestID := fmt.Sprintf("%032x", index+4096)
		clone := cloneServiceMutationJob(pending)
		clone.RequestID = requestID
		clone.Phase, _ = formatDNSZoneSyncV3Phase(
			dnsZoneSyncV3PropagationPending,
			requestID,
			commitment.Domain,
			commitment.Qualifier,
		)
		manager.ledger.Jobs[requestID] = clone
	}
	if err := manager.persistLedgerMutationLocked(before); err != nil {
		manager.mu.Unlock()
		t.Fatal(err)
	}
	manager.mu.Unlock()
	if _, err := manager.begin(&ServiceMutationBeginRequest{
		RequestID: testMutationRequestID, OwnerID: testMutationOwnerID,
		Kind: "dns_zone_sync", Target: commitment.Domain,
		PackageName: commitment.Qualifier, Resume: true,
	}); err != nil {
		t.Fatal(err)
	}
	oldTerminalID := strings.Repeat("e", 32)
	manager.mu.Lock()
	oldTerminal := cloneServiceMutationJob(pending)
	oldTerminal.RequestID = oldTerminalID
	oldTerminal.Status = serviceMutationStatusFailed
	oldTerminal.Phase = "failed"
	oldTerminal.ErrorCode = "old_failure"
	oldTerminal.ErrorMessage = "old terminal receipt"
	oldTerminal.UpdatedAt = manager.now().Add(24 * time.Hour)
	oldTerminal.FinishedAt = oldTerminal.UpdatedAt
	manager.ledger.Jobs[oldTerminalID] = oldTerminal
	manager.mu.Unlock()
	var response RecoverDNSZoneV3Response
	if err := (&Agent{}).RecoverDNSZoneV3(&RecoverDNSZoneV3Request{
		ServiceMutationBinding: request.ServiceMutationBinding,
		Domain:                 commitment.Domain, Qualifier: commitment.Qualifier,
	}, &response); err != nil {
		t.Fatal(err)
	}
	terminal := manager.status(testMutationRequestID)
	if !response.Recovered || terminal == nil ||
		terminal.Status != serviceMutationStatusSucceeded ||
		!strings.Contains(terminal.Phase, "/published/") {
		t.Fatalf("history-pressure response=%+v terminal=%+v jobs=%d", response, terminal, len(manager.ledger.Jobs))
	}
	if manager.status(oldTerminalID) != nil {
		t.Fatal("history trim retained the later-dated old terminal over the protected published receipt")
	}
}
