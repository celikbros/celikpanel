//go:build linux

package main

import (
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

// The exact shape of kill-matrix run 8 (risk R-017): the backend switch is
// inside its finalizing interval, package installation has durably registered
// apt-get as the job's sole worker, and the panel's ordinary five-second
// heartbeat arrives. Before the worker-aware guard this poisoned the manager
// and cancelled the switch; the switch must instead survive its own package
// install, renew its lease, and complete.
// Kill-matrix koşu 8'in birebir biçimi (risk R-017): arka uç geçişi sonlanma
// aralığının içinde, paket kurulumu apt-get'i işin tek işçisi olarak kalıcı
// kaydetmiş ve panelin sıradan beş saniyelik kalp atışı geliyor. İşçiden
// haberdar nöbetten önce bu, yöneticiyi zehirleyip geçişi iptal ediyordu;
// geçiş bunun yerine kendi paket kurulumundan sağ çıkmalı, kiralamasını
// yenilemeli ve tamamlanmalıdır.
func TestDNSEngineSwitchSurvivesHeartbeatDuringRegisteredPackageWorker(t *testing.T) {
	request := canonicalSwitchRequest(t)
	manager, root := newMutationTestManager(t)
	installGlobalMutationTestManager(t, manager)
	beginMutationTestJobWithIdentity(
		t, manager, "dns_engine_switch", "bind", request.ManifestQualifier,
	)
	journal := activeCommittedBINDStartupJournalFixture(
		t, manager, root, request,
	)
	switchEntered := make(chan struct{})
	releaseSwitch := make(chan struct{})
	finalizeEntered := make(chan struct{})
	releaseFinalize := make(chan struct{})
	backend := &fakeDNSEngineBackend{
		result: transport.SwitchDNSEngineV1Response{
			Applied: true, ActiveEngine: transport.DNSEngineBIND,
			ActiveEpoch: 1, AppliedZones: 0,
		},
		switchHook: func() error {
			close(switchEntered)
			<-releaseSwitch
			return writeDNSEngineSwitchJournal(journal)
		},
		finalizeHook: func() error {
			close(finalizeEntered)
			<-releaseFinalize
			return removeDNSEngineSwitchJournal()
		},
	}
	useFakeDNSEngineBackend(t, backend)

	done := make(chan SwitchDNSEngineV1Response, 1)
	go func() {
		var response SwitchDNSEngineV1Response
		_ = (&Agent{}).SwitchDNSEngineV1(&request, &response)
		done <- response
	}()
	select {
	case <-switchEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("DNS switch hook did not start")
	}

	manager.mu.Lock()
	runtime := manager.active
	manager.mu.Unlock()
	if runtime == nil {
		t.Fatal("DNS switch runtime is absent")
	}

	// The REAL registration path, exactly as a package install uses it. The
	// PID is fictitious on purpose: no such process exists, which is also the
	// reap-to-clear instant — cmd.Wait() has collected the child but clear()
	// has not run. The guard must not care, because it never probes liveness.
	// GERÇEK kayıt yolu, bir paket kurulumunun kullandığı hâliyle. PID bilerek
	// hayalidir: böyle bir süreç yok; bu aynı zamanda toplama-temizleme
	// ânıdır — cmd.Wait() çocuğu toplamış ama clear() henüz koşmamıştır.
	// Nöbet bunu umursamamalıdır, çünkü canlılığı hiç yoklamaz.
	tracker := &serviceMutationExecutionTracker{manager: manager, runtime: runtime}
	if err := tracker.register(4242, "boot-7:1234567", "apt-get"); err != nil {
		t.Fatalf("register package worker: %v", err)
	}
	manager.mu.Lock()
	afterRegister := cloneServiceMutationJob(runtime.job)
	manager.mu.Unlock()
	if afterRegister.LeaseExpiresAt.Before(afterRegister.UpdatedAt) {
		t.Fatalf("registration broke the lease invariant: %+v", afterRegister)
	}

	// Simulate a stalled panel: the install has outlived the original lease.
	// The persisted shape stays legal (lease still at-or-after updated), and
	// the next heartbeat must renew rather than find a poisonable corpse.
	// Duraklamış paneli canlandır: kurulum özgün kiralamayı aşmış. Kalıcı
	// biçim meşru kalır (kiralama hâlâ güncellemenin gerisinde değil) ve bir
	// sonraki kalp atışı zehirlenecek bir ceset bulmak yerine yenilemelidir.
	manager.mu.Lock()
	before := cloneServiceMutationLedger(manager.ledger)
	staleNow := manager.now()
	runtime.job.StartedAt = staleNow.Add(-40 * time.Second)
	runtime.job.UpdatedAt = staleNow.Add(-30 * time.Second)
	runtime.job.LeaseExpiresAt = staleNow.Add(-10 * time.Second)
	if err := manager.persistLedgerMutationLocked(before); err != nil {
		manager.mu.Unlock()
		t.Fatalf("persist stalled lease fixture: %v", err)
	}
	manager.mu.Unlock()

	heartbeatJob, heartbeatErr := manager.heartbeat(&ServiceMutationHeartbeatRequest{
		RequestID: request.MutationRequestID,
		OwnerID:   request.MutationOwnerID,
		Phase:     "must-not-overwrite-leased",
	})
	if heartbeatErr != nil {
		t.Fatalf("heartbeat during registered worker: %v", heartbeatErr)
	}
	if heartbeatJob == nil || heartbeatJob.Status != serviceMutationStatusRunning ||
		heartbeatJob.Phase != "leased" ||
		heartbeatJob.WorkerPID != 4242 || heartbeatJob.WorkerCommand != "apt-get" {
		t.Fatalf("protected heartbeat job=%+v", heartbeatJob)
	}
	if heartbeatJob.LeaseExpiresAt.Before(heartbeatJob.UpdatedAt) ||
		!heartbeatJob.LeaseExpiresAt.After(staleNow) {
		t.Fatalf("protected heartbeat did not renew the lease: %+v", heartbeatJob)
	}
	manager.mu.Lock()
	poisoned := manager.poisoned
	manager.mu.Unlock()
	if poisoned != nil {
		t.Fatalf("heartbeat poisoned the manager: %v", poisoned)
	}

	// The expiry watchdog and a cancellation attempt must both stay
	// protective while the worker is registered, exactly as they do in the
	// worker-free finalizing interval.
	// Süre dolumu bekçisi ve bir iptal denemesi, işçisiz sonlanma aralığında
	// olduğu gibi, işçi kayıtlıyken de koruyucu kalmalıdır.
	manager.expire(runtime)
	cancelJob, cancelErr := manager.cancelJob(&ServiceMutationCancelRequest{
		RequestID:     request.MutationRequestID,
		ExpectedOwner: request.MutationOwnerID,
		Reason:        "must-not-cancel-mid-install",
	})
	if cancelErr != nil || cancelJob == nil ||
		cancelJob.Status != serviceMutationStatusRunning {
		t.Fatalf("cancel during registered worker job=%+v err=%v", cancelJob, cancelErr)
	}
	manager.mu.Lock()
	stillActive := manager.active == runtime && manager.poisoned == nil &&
		runtime.job.Status == serviceMutationStatusRunning
	manager.mu.Unlock()
	if !stillActive {
		t.Fatal("expiry or cancellation broke the guarded switch")
	}

	// clear() is the second durable worker transition; it must leave the
	// ledger shape legal even though it advances UpdatedAt.
	// clear() ikinci kalıcı işçi geçişidir; UpdatedAt değerini ilerletse de
	// defter biçimini meşru bırakmalıdır.
	if err := tracker.clear(4242); err != nil {
		t.Fatalf("clear package worker: %v", err)
	}
	manager.mu.Lock()
	afterClear := cloneServiceMutationJob(runtime.job)
	manager.mu.Unlock()
	if afterClear.WorkerPID != 0 || afterClear.LeaseExpiresAt.Before(afterClear.UpdatedAt) {
		t.Fatalf("clear broke the lease invariant: %+v", afterClear)
	}

	close(releaseSwitch)
	select {
	case <-finalizeEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("DNS finalizer did not start")
	}
	close(releaseFinalize)
	select {
	case response := <-done:
		if response.Error != "" || !response.Applied {
			t.Fatalf("response=%+v", response)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("DNS finalizer did not finish")
	}
	final := manager.status(request.MutationRequestID)
	if final == nil || final.Status != serviceMutationStatusSucceeded {
		t.Fatalf("final job=%+v", final)
	}
}
