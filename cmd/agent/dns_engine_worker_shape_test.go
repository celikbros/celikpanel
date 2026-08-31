package main

import (
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

// These proofs are deliberately platform-independent — no fixture, no
// filesystem, no /proc — so they run on every development machine, not only
// under the linux-tagged integration fixtures.
// Bu kanıtlar bilerek platformdan bağımsızdır — fikstür yok, dosya sistemi
// yok, /proc yok — böylece yalnız linux etiketli bütünleşme fikstürleri
// altında değil, her geliştirme makinesinde koşarlar.

// The worker-aware shape proof loosens exactly one thing — the owning job's
// canonical registered worker — and nothing else. Everything foreign, partial
// or noncanonical must still fail, and the strict proof must still reject a
// worker-bearing job outright.
// İşçiden haberdar biçim kanıtı tam olarak tek şeyi gevşetir — sahibi olan
// işin kanonik kayıtlı işçisi — başka hiçbir şeyi değil. Yabancı, eksik ya da
// kanonik olmayan her şey yine düşmeli ve katı kanıt, işçi taşıyan bir işi
// yine doğrudan reddetmelidir.
func TestDNSEngineSwitchWorkerShapeProofRejectsEverythingForeign(t *testing.T) {
	now := time.Now()
	base := func() *ServiceMutationJob {
		return &ServiceMutationJob{
			RequestID:      strings.Repeat("a", 32),
			OwnerID:        strings.Repeat("b", 32),
			Kind:           "dns_engine_switch",
			Target:         string(transport.DNSEngineBIND),
			PackageName:    "qualifier",
			Status:         serviceMutationStatusRunning,
			Phase:          "leased",
			Attempt:        1,
			StartedAt:      now.Add(-time.Minute),
			UpdatedAt:      now,
			LeaseExpiresAt: now.Add(20 * time.Second),
			DeadlineAt:     now.Add(10 * time.Minute),
			WorkerPID:      4242,
			WorkerStarted:  "boot-7:1234567",
			WorkerCommand:  "apt-get",
		}
	}
	accepts := func(job *ServiceMutationJob) bool {
		return exactActiveDNSEngineSwitchJobWithRegisteredWorker(
			job, strings.Repeat("a", 32), strings.Repeat("b", 32),
			transport.DNSEngineBIND, "qualifier",
		)
	}

	if !accepts(base()) {
		t.Fatal("the owning job's canonical registered worker must be accepted")
	}
	// A dead worker is indistinguishable by design: no liveness probe exists.
	// The base case IS the dead-worker case — PID 4242 does not exist here.
	// Ölü işçi tasarım gereği ayırt edilemez: canlılık sondası yok. Temel
	// durum ölü işçi durumunun ta kendisidir — PID 4242 burada mevcut değil.

	if exactActiveDNSEngineSwitchJob(
		base(), strings.Repeat("a", 32), strings.Repeat("b", 32),
		transport.DNSEngineBIND, "qualifier",
	) {
		t.Fatal("the strict proof must keep rejecting a worker-bearing job")
	}

	mutations := map[string]func(*ServiceMutationJob){
		"zero pid":            func(j *ServiceMutationJob) { j.WorkerPID = 0 },
		"negative pid":        func(j *ServiceMutationJob) { j.WorkerPID = -1 },
		"empty start token":   func(j *ServiceMutationJob) { j.WorkerStarted = "" },
		"padded start token":  func(j *ServiceMutationJob) { j.WorkerStarted = " boot-7:1234567" },
		"empty command":       func(j *ServiceMutationJob) { j.WorkerCommand = "" },
		"padded command":      func(j *ServiceMutationJob) { j.WorkerCommand = " apt-get" },
		"path-bearing":        func(j *ServiceMutationJob) { j.WorkerCommand = "usr/bin/apt-get" },
		"oversized command":   func(j *ServiceMutationJob) { j.WorkerCommand = strings.Repeat("x", 65) },
		"wrong status":        func(j *ServiceMutationJob) { j.Status = serviceMutationStatusCancelling },
		"wrong phase":         func(j *ServiceMutationJob) { j.Phase = "installing" },
		"error code set":      func(j *ServiceMutationJob) { j.ErrorCode = "boom" },
		"finished":            func(j *ServiceMutationJob) { j.FinishedAt = now },
		"lease before update": func(j *ServiceMutationJob) { j.LeaseExpiresAt = j.UpdatedAt.Add(-time.Second) },
	}
	for name, mutate := range mutations {
		job := base()
		mutate(job)
		if accepts(job) {
			t.Fatalf("%s must be rejected", name)
		}
	}
	if accepts(nil) {
		t.Fatal("nil job must be rejected")
	}
	wrong := base()
	if exactActiveDNSEngineSwitchJobWithRegisteredWorker(
		wrong, strings.Repeat("z", 32), strings.Repeat("b", 32),
		transport.DNSEngineBIND, "qualifier",
	) {
		t.Fatal("a foreign request identity must be rejected")
	}
}

// A worker registered while the job is already cancelling (rollback recovery)
// must not resurrect an expired lease: the expired-cancelling proof requires
// UpdatedAt at or past LeaseExpiresAt, and extending the lease there would
// break lease-expiry recovery.
// İş zaten iptal edilirken (geri alma kurtarması) kaydedilen bir işçi, süresi
// dolmuş kiralamayı diriltmemelidir: süresi-dolmuş-iptal kanıtı UpdatedAt'in
// LeaseExpiresAt'e eşit ya da ötesinde olmasını şart koşar ve orada kiralamayı
// uzatmak, kiralama-dolumu kurtarmasını bozardı.
func TestWorkerTransitionLeaseExtensionOnlyWhileRunning(t *testing.T) {
	manager := &serviceMutationManager{leaseDuration: serviceMutationLeaseDuration}
	now := time.Now()
	job := &ServiceMutationJob{
		Status:         serviceMutationStatusCancelling,
		UpdatedAt:      now,
		LeaseExpiresAt: now.Add(-time.Second),
		DeadlineAt:     now.Add(10 * time.Minute),
	}
	extendWorkerTransitionLease(job, manager, now)
	if !job.LeaseExpiresAt.Equal(now.Add(-time.Second)) {
		t.Fatalf("a cancelling job must not have its lease extended: %+v", job)
	}
	job.Status = serviceMutationStatusRunning
	extendWorkerTransitionLease(job, manager, now)
	if job.LeaseExpiresAt.Before(job.UpdatedAt) {
		t.Fatalf("a running job must have its lease renewed: %+v", job)
	}
}
