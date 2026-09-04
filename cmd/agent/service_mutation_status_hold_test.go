package main

import (
	"errors"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

// installStatusHoldTestManager pins the process-global manager and its cached
// bring-up error for one test and restores both afterwards. It is
// platform-neutral on purpose: the status contract has no Linux dependency.
// installStatusHoldTestManager, süreç-geneli yöneticiyi ve önbelleklenmiş
// kaldırma hatasını bir test için sabitler, sonra ikisini de geri yükler.
// Bilerek platformdan bağımsızdır: durum sözleşmesinin Linux bağımlılığı yok.
func installStatusHoldTestManager(t *testing.T, manager *serviceMutationManager, err error) {
	t.Helper()
	globalServiceMutationMu.Lock()
	previousManager := globalServiceMutationManager
	previousErr := globalServiceMutationErr
	globalServiceMutationManager = manager
	globalServiceMutationErr = err
	globalServiceMutationMu.Unlock()
	t.Cleanup(func() {
		globalServiceMutationMu.Lock()
		globalServiceMutationManager = previousManager
		globalServiceMutationErr = previousErr
		globalServiceMutationMu.Unlock()
	})
}

// S-7 T5. After a SIGKILL mid-rollback the ordinary agent restarted and its
// socket answered, but the first read-only status probe got an RPC error and
// nothing else: the manager could not be brought up, and ServiceMutationStatus
// returned that bring-up error instead of answering. A read-only probe must
// answer with the hold code, which is exactly what a caller needs to tell a
// serving-but-refusing agent from a dead one.
//
// S-7 T5. Geri alma ortasında SIGKILL'den sonra sıradan agent yeniden başladı
// ve soketi yanıt verdi; ama ilk salt-okunur durum sondası bir RPC hatası ve
// başka hiçbir şey aldı: yönetici ayağa kaldırılamamıştı ve
// ServiceMutationStatus yanıt vermek yerine o kaldırma hatasını döndürüyordu.
// Salt-okunur bir sonda tutulma koduyla yanıt vermelidir; hizmet verip
// reddeden bir agent'ı ölü olandan ayırmak için çağıranın ihtiyacı tam budur.
func TestServiceMutationStatusReportsLedgerUnavailableInsteadOfFailing(t *testing.T) {
	installStatusHoldTestManager(t, nil, errors.New(
		"acquire service mutation reconciliation lock: inspect service mutation lock directory: lstat /run/celikpanel-s7-t5: no such file or directory",
	))

	var response ServiceMutationResponse
	err := (&Agent{}).ServiceMutationStatus(
		&ServiceMutationStatusRequest{RequestID: "2d18409f24899698cace7524d9d6c72a"},
		&response,
	)
	if err != nil {
		t.Fatalf("a read-only status probe must answer, got RPC error: %v", err)
	}
	if response.MutationHold != transport.MutationHoldLedgerUnavailable {
		t.Fatalf("hold=%q, want %q", response.MutationHold, transport.MutationHoldLedgerUnavailable)
	}
	if response.Job != nil || response.ErrorCode != "" || response.Error != "" {
		t.Fatalf("an agent without a ledger must report no job and no internal text: %+v", response)
	}
}

// A manager that came up poisoned at startup is returned with its error
// cached. Status must still answer: no job when the ledger is empty, and the
// ledger_ambiguous hold that names the refusal.
// Başlangıçta zehirli kalkan yönetici, hatası önbelleklenmiş olarak döner.
// Durum yine de yanıt vermelidir: defter boşsa iş yok, reddi adlandıran
// ledger_ambiguous tutulması var.
func TestServiceMutationStatusReportsStartupPoisonAsAmbiguousHold(t *testing.T) {
	poisoned := &serviceMutationManager{
		poisoned: errors.New("validate DNS cluster journal stages during startup: residue"),
		ledger: serviceMutationLedger{
			Version: serviceMutationLedgerVersion,
			Jobs:    map[string]*ServiceMutationJob{},
		},
	}
	installStatusHoldTestManager(t, poisoned, poisoned.poisoned)

	var response ServiceMutationResponse
	err := (&Agent{}).ServiceMutationStatus(&ServiceMutationStatusRequest{}, &response)
	if err != nil {
		t.Fatalf("a startup-poisoned agent must still answer a status probe, got: %v", err)
	}
	if response.MutationHold != transport.MutationHoldLedgerAmbiguous {
		t.Fatalf("hold=%q, want %q", response.MutationHold, transport.MutationHoldLedgerAmbiguous)
	}
	if response.Job != nil || response.Error != "" {
		t.Fatalf("empty poisoned ledger must report no job and no internal text: %+v", response)
	}
}

// The transient case is unchanged: another process holding the host lock is
// reported as busy, not as a hold and not as an RPC error.
// Geçici durum değişmedi: host kilidini tutan başka bir süreç tutulma ya da
// RPC hatası olarak değil, meşgul olarak bildirilir.
func TestServiceMutationStatusStillReportsHostBusyAsBusy(t *testing.T) {
	installStatusHoldTestManager(t, nil, errServiceMutationHostBusy)

	var response ServiceMutationResponse
	if err := (&Agent{}).ServiceMutationStatus(&ServiceMutationStatusRequest{}, &response); err != nil {
		t.Fatalf("busy must not be an RPC error: %v", err)
	}
	if response.ErrorCode != transport.HostMutationBusy || response.MutationHold != "" {
		t.Fatalf("busy response=%+v", response)
	}
}
