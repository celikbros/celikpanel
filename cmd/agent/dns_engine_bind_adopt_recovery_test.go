package main

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

// takeoverAdoptionManifest is the exact manifest the panel's adopt_unmanaged
// commitment produces: an initial standalone BIND activation with no source
// engine, epoch 0 to 1, dispatched as an ordinary switch.
//
// takeoverAdoptionManifest, panelin adopt_unmanaged taahhüdünün ürettiği kesin
// bildirgedir: kaynak motoru olmayan, 0'dan 1'e çağ, tek sunuculu bir ilk BIND
// etkinleştirmesi; sıradan bir geçiş olarak gönderilir.
func takeoverAdoptionManifest(
	t *testing.T,
) mutationpayload.DNSEngineSwitchManifestCommitment {
	t.Helper()
	manifest, err := mutationpayload.CanonicalDNSEngineSwitchManifestWithPairIdentity(
		transport.DNSEngineSwitchModeSwitch,
		"", transport.DNSEngineBIND,
		0, 1, 0, transport.DNSTopologyStandalone,
		"", "", "", "", "",
		[]transport.DNSEngineSwitchZoneSnapshot{},
	)
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func takeoverAdoptionJournalUnits(
	named, alias dnsUnitSnapshot,
) []dnsUnitSnapshot {
	return []dnsUnitSnapshot{alias, named}
}

func runningBINDUnitSnapshots() []dnsUnitSnapshot {
	return takeoverAdoptionJournalUnits(
		dnsUnitSnapshot{
			Name: "named.service", LoadState: "loaded",
			ActiveState: "active", UnitFileState: "enabled",
		},
		dnsUnitSnapshot{
			Name: "bind9.service", LoadState: "loaded",
			ActiveState: "active", UnitFileState: "enabled",
		},
	)
}

// The whole of R-043 in one question: a crash during a takeover of a server
// that was answering must be recovered by the adoption rollback, which restores
// the configuration and reloads, and never by the switch rollback, which stops
// units. The journal carries the facts that separate the two, and the recovery
// path must read them before it acts.
//
// R-043'ün tamamı tek bir soruda: yanıt veren bir sunucunun devralması
// sırasındaki bir çökme, yapılandırmayı geri yükleyip yeniden yükleyen devralma
// geri almasıyla kurtarılmalıdır; birimleri durduran geçiş geri almasıyla asla.
// Günlük, ikisini ayıran olguları taşır ve kurtarma yolu, davranmadan önce
// onları okumalıdır.
func TestCrashedRunningBINDTakeoverRecoversThroughTheAdoptionRollback(t *testing.T) {
	source := readAgentSource(t, "dns_engine_recovery.go")
	start := strings.Index(source, "func rollbackDNSSwitchJournal(")
	if start < 0 {
		t.Fatal("the DNS switch journal rollback is missing")
	}
	end := strings.Index(source[start:], "\nfunc verifyNoManagedDNSAuthority(")
	if end < 0 {
		t.Fatal("the DNS switch journal rollback end boundary is missing")
	}
	body := source[start : start+end]

	classify := strings.Index(body, "runningBINDAdoptionJournal(")
	adopt := strings.Index(body, "recoverRunningBINDAdoptionJournal(")
	if classify < 0 || adopt < 0 {
		t.Fatalf(
			"the recovery does not dispatch: classify=%d adoption rollback=%d",
			classify, adopt,
		)
	}
	// This is a dispatch, not a replacement: where the switch rollback is still
	// right - a first install, the stopped half of the takeover - it must stay
	// exactly where it was.
	//
	// Bu bir yönlendirmedir, bir yerine koyma değil: geçiş geri almasının hâlâ
	// doğru olduğu yerde - bir ilk kurulum, devralmanın durmuş yarısı - olduğu
	// gibi kalmalıdır.
	for _, kept := range []string{
		"publisher.RestorePointer(",
		"rollbackBINDActivation(",
		"verifyRestoredDNSSwitchSource(",
	} {
		position := strings.Index(body, kept)
		if position < 0 {
			t.Errorf("the switch rollback lost %s", kept)
			continue
		}
		if classify > position || adopt > position {
			t.Errorf(
				"%s is reached before the recovery classifies the journal: "+
					"classify=%d adoption=%d %s=%d",
				kept, classify, adopt, kept, position,
			)
		}
	}
}

// The completed takeover is not rolled back at all. Recovery proves the
// verified target first and finalizes it; only a journal whose target does not
// verify reaches any rollback, so a crash after a successful reload and receipt
// finishes the transaction instead of undoing it.
//
// Tamamlanmış devralma hiç geri alınmaz. Kurtarma önce doğrulanmış hedefi
// kanıtlar ve onu sonlandırır; herhangi bir geri almaya yalnız hedefi
// doğrulanmayan bir günlük ulaşır, dolayısıyla başarılı bir yeniden yükleme ve
// makbuzdan sonraki bir çökme, işlemi geri almak yerine bitirir.
func TestCommittedDNSEngineJournalIsFinalizedRatherThanRolledBack(t *testing.T) {
	source := readAgentSource(t, "dns_engine_recovery.go")
	start := strings.Index(source, "func (hostDNSEngineBackend) RecoverSwitch(")
	if start < 0 {
		t.Fatal("the DNS engine switch recovery entry point is missing")
	}
	end := strings.Index(source[start:], "\nfunc reconcileExistingDNSEngineSwitchJournal(")
	if end < 0 {
		t.Fatal("the DNS engine switch recovery end boundary is missing")
	}
	body := source[start : start+end]
	verify := strings.Index(body, "verifyDNSSwitchJournalTarget(ctx, journal)")
	committed := strings.Index(body, "dnsEngineSwitchRecoveryCommitted, nil")
	rollback := strings.Index(body, "runDNSSwitchRecoveryRollbackWithJournal(")
	if verify < 0 || committed < 0 || rollback < 0 {
		t.Fatalf(
			"verify=%d committed=%d rollback=%d", verify, committed, rollback,
		)
	}
	if verify > rollback || committed > rollback {
		t.Fatal("a verified DNS engine target can reach the rollback")
	}
	reconcile := source[strings.Index(
		source, "func reconcileExistingDNSEngineSwitchJournal(",
	):]
	if !strings.Contains(reconcile, "FinalizeSwitch(") {
		t.Fatal("the reconcile path does not finalize a committed journal")
	}
}

func TestRunningBINDAdoptionJournalClassifiesTheTakeoverByItsUnitPreimage(t *testing.T) {
	manifest := takeoverAdoptionManifest(t)
	stopped := takeoverAdoptionJournalUnits(
		dnsUnitSnapshot{
			Name: "named.service", LoadState: "loaded",
			ActiveState: "inactive", UnitFileState: "disabled",
		},
		dnsUnitSnapshot{
			Name: "bind9.service", LoadState: "loaded",
			ActiveState: "inactive", UnitFileState: "disabled",
		},
	)
	absent := takeoverAdoptionJournalUnits(
		dnsUnitSnapshot{
			Name: "named.service", LoadState: "not-found", ActiveState: "inactive",
		},
		dnsUnitSnapshot{
			Name: "bind9.service", LoadState: "not-found", ActiveState: "inactive",
		},
	)
	// Red Hat ships named.service alone: bind9.service is not on the host at
	// all, and the takeover of a running server is still exactly readable.
	//
	// Red Hat yalnız named.service gönderir: bind9.service sunucuda hiç yoktur
	// ve çalışan bir sunucunun devralması yine tam olarak okunabilir.
	aliasFree := takeoverAdoptionJournalUnits(
		dnsUnitSnapshot{
			Name: "named.service", LoadState: "loaded",
			ActiveState: "active", UnitFileState: "enabled",
		},
		dnsUnitSnapshot{
			Name: "bind9.service", LoadState: "not-found", ActiveState: "inactive",
		},
	)
	disagree := takeoverAdoptionJournalUnits(
		dnsUnitSnapshot{
			Name: "named.service", LoadState: "loaded",
			ActiveState: "inactive", UnitFileState: "disabled",
		},
		dnsUnitSnapshot{
			Name: "bind9.service", LoadState: "loaded",
			ActiveState: "active", UnitFileState: "enabled",
		},
	)

	switchManifest, err := mutationpayload.CanonicalDNSEngineSwitchManifestWithPairIdentity(
		transport.DNSEngineSwitchModeSwitch,
		transport.DNSEnginePowerDNS, transport.DNSEngineBIND,
		1, 2, 3, transport.DNSTopologyStandalone,
		"", "", "", "", "",
		[]transport.DNSEngineSwitchZoneSnapshot{},
	)
	if err != nil {
		t.Fatal(err)
	}

	for name, want := range map[string]struct {
		manifest mutationpayload.DNSEngineSwitchManifestCommitment
		units    []dnsUnitSnapshot
		adoption bool
		refuses  string
	}{
		"a running takeover": {
			manifest: manifest, units: runningBINDUnitSnapshots(), adoption: true,
		},
		"a running takeover without the distribution alias": {
			manifest: manifest, units: aliasFree, adoption: true,
		},
		"the stopped half of the takeover": {
			manifest: manifest, units: stopped,
		},
		"a first install": {
			manifest: manifest, units: absent,
		},
		"a switch away from PowerDNS": {
			manifest: switchManifest, units: runningBINDUnitSnapshots(),
		},
		"a preimage that is not the takeover's units": {
			manifest: manifest,
			units: []dnsUnitSnapshot{{
				Name: "named.service", LoadState: "loaded",
				ActiveState: "active", UnitFileState: "enabled",
			}},
			refuses: "named.service, not bind9.service and named.service",
		},
		"an empty preimage": {
			manifest: manifest, units: []dnsUnitSnapshot{},
			refuses: "preimage names nothing",
		},
		"two loaded units that disagree": {
			manifest: manifest, units: disagree,
			refuses: "named.service is",
		},
	} {
		journal := dnsEngineSwitchJournal{
			TargetEngine: transport.DNSEngineBIND, TargetUnitsBefore: want.units,
		}
		adoption, err := runningBINDAdoptionJournal(want.manifest, journal)
		if want.refuses != "" {
			if err == nil {
				t.Errorf("%s: an unclassifiable journal was accepted", name)
				continue
			}
			if !strings.Contains(err.Error(), want.refuses) {
				t.Errorf("%s: err=%v want it to name %q", name, err, want.refuses)
			}
			if adoption {
				t.Errorf("%s: a refusal still claimed an adoption", name)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if adoption != want.adoption {
			t.Errorf("%s: adoption=%v want=%v", name, adoption, want.adoption)
		}
	}
}

// A journal whose target engine is not BIND never reaches this question at all.
//
// Hedef motoru BIND olmayan bir günlük bu soruya hiç ulaşmaz.
func TestRunningBINDAdoptionJournalIgnoresAPowerDNSTarget(t *testing.T) {
	manifest := takeoverAdoptionManifest(t)
	adoption, err := runningBINDAdoptionJournal(
		manifest,
		dnsEngineSwitchJournal{
			TargetEngine:      transport.DNSEnginePowerDNS,
			TargetUnitsBefore: runningBINDUnitSnapshots(),
		},
	)
	if err != nil || adoption {
		t.Fatalf("adoption=%v err=%v", adoption, err)
	}
}

type recordedBINDAdoptionRecovery struct {
	steps    []string
	failures map[string]error
}

func (recorder *recordedBINDAdoptionRecovery) step(name string) func() error {
	return func() error {
		recorder.steps = append(recorder.steps, name)
		return recorder.failures[name]
	}
}

func (recorder *recordedBINDAdoptionRecovery) evidenceStep(
	name string,
) func(bindAdoptionRuntimeEvidence) error {
	step := recorder.step(name)
	return func(bindAdoptionRuntimeEvidence) error { return step() }
}

func (recorder *recordedBINDAdoptionRecovery) ops() bindAdoptionRecoveryOps {
	capture := recorder.step("capture-evidence")
	return bindAdoptionRecoveryOps{
		captureEvidence: func() (bindAdoptionRuntimeEvidence, error) {
			return bindAdoptionRuntimeEvidence{}, capture()
		},
		proveCurrent:   recorder.step("prove-current"),
		rollback:       recorder.evidenceStep("rollback"),
		restorePointer: recorder.step("restore-pointer"),
		verifyRestored: recorder.evidenceStep("verify-restored"),
	}
}

func TestCrashedBINDTakeoverRecoveryOrdersItsSteps(t *testing.T) {
	recorder := &recordedBINDAdoptionRecovery{failures: map[string]error{}}
	if err := recoverRunningBINDAdoptionJournalWithOps(
		recorder.ops(),
	); err != nil {
		t.Fatalf("recovery failed: %v", err)
	}
	want := []string{
		"capture-evidence", "prove-current", "rollback",
		"restore-pointer", "verify-restored",
	}
	if !reflect.DeepEqual(recorder.steps, want) {
		t.Fatalf("steps=%v want=%v", recorder.steps, want)
	}
}

func TestCrashedBINDTakeoverRecoveryStopsAtItsFirstFailure(t *testing.T) {
	boom := errors.New("boom")
	for failing, want := range map[string][]string{
		"capture-evidence": {"capture-evidence"},
		"prove-current":    {"capture-evidence", "prove-current"},
		"rollback":         {"capture-evidence", "prove-current", "rollback"},
		"restore-pointer": {
			"capture-evidence", "prove-current", "rollback", "restore-pointer",
		},
	} {
		recorder := &recordedBINDAdoptionRecovery{
			failures: map[string]error{failing: boom},
		}
		err := recoverRunningBINDAdoptionJournalWithOps(recorder.ops())
		if !errors.Is(err, boom) {
			t.Fatalf("%s: err=%v want boom", failing, err)
		}
		if !reflect.DeepEqual(recorder.steps, want) {
			t.Fatalf("%s: steps=%v want=%v", failing, recorder.steps, want)
		}
	}
	if err := recoverRunningBINDAdoptionJournalWithOps(
		bindAdoptionRecoveryOps{},
	); err == nil {
		t.Fatal("an incomplete recovery was accepted")
	}
}

type recordedRollbackTargetProof struct {
	steps    []string
	failures map[string]error
}

func (recorder *recordedRollbackTargetProof) step(name string) func() error {
	return func() error {
		recorder.steps = append(recorder.steps, name)
		return recorder.failures[name]
	}
}

func (recorder *recordedRollbackTargetProof) ops() bindRollbackTargetProofOps {
	return bindRollbackTargetProofOps{
		sealed:   recorder.step("sealed"),
		restored: recorder.step("restored"),
	}
}

// The panel may not close a crashed takeover until the host proves it carries
// nothing of CelikPanel's that is live. The switch proves that by being sealed
// and not serving; a takeover proves it while serving, because it is the
// operator's own server and it never stopped. Both are accepted, the sealed one
// first, and a host that can prove neither is refused with both refusals.
//
// Panel, sunucu CelikPanel'in canli hicbir seyini tasimadigini kanitlayana dek
// cokmus bir devralmayi kapatamaz. Gecis bunu muhurlu ve hizmet vermez olarak
// kanitlar; bir devralma ise hizmet verirken kanitlar, cunku o operatorun kendi
// sunucusudur ve hic durmadi. Ikisi de kabul edilir, once muhurlu olan, ve
// hicbirini kanitlayamayan bir sunucu iki retle birden reddedilir.
func TestRollbackEvidenceAcceptsTheSealedTargetOrTheRestoredTakeover(t *testing.T) {
	sealed := &recordedRollbackTargetProof{failures: map[string]error{}}
	if err := verifyDNSEngineRollbackTargetSealWithOps(sealed.ops()); err != nil {
		t.Fatalf("a sealed target was refused: %v", err)
	}
	if !reflect.DeepEqual(sealed.steps, []string{"sealed"}) {
		t.Fatalf("steps=%v; the takeover proof must not run for a sealed target",
			sealed.steps)
	}

	serving := errors.New("named.service is active")
	takeover := &recordedRollbackTargetProof{
		failures: map[string]error{"sealed": serving},
	}
	if err := verifyDNSEngineRollbackTargetSealWithOps(takeover.ops()); err != nil {
		t.Fatalf("a restored takeover was refused: %v", err)
	}
	if !reflect.DeepEqual(takeover.steps, []string{"sealed", "restored"}) {
		t.Fatalf("steps=%v", takeover.steps)
	}

	owned := errors.New("the restored BIND still points at a CelikPanel generation")
	neither := &recordedRollbackTargetProof{
		failures: map[string]error{"sealed": serving, "restored": owned},
	}
	err := verifyDNSEngineRollbackTargetSealWithOps(neither.ops())
	if !errors.Is(err, serving) || !errors.Is(err, owned) {
		t.Fatalf("err=%v; want both refusals named", err)
	}

	if err := verifyDNSEngineRollbackTargetSealWithOps(
		bindRollbackTargetProofOps{},
	); err == nil {
		t.Fatal("an incomplete target proof was accepted")
	}
}

// The takeover's restored-target proof is a proof, not a relaxation: it names
// every place CelikPanel could still own this host and refuses if any of them
// is there, and it ends on the switch's own runtime proof.
//
// Devralmanin geri-yuklenmis-hedef kaniti bir gevsetme degil, bir kanittir:
// CelikPanel'in bu sunucuya hala sahip olabilecegi her yeri adlandirir, biri
// varsa reddeder ve gecisin kendi calisma zamani kanitiyla biter.
func TestRestoredUnmanagedBINDProofNamesEveryOwnershipItRefuses(t *testing.T) {
	source := readAgentSource(t, "dns_engine_bind_adopt.go")
	start := strings.Index(source, "func verifyRestoredUnmanagedRunningBINDTarget(")
	if start < 0 {
		t.Fatal("the restored unmanaged BIND proof is missing")
	}
	end := strings.Index(source[start:], "\nfunc ")
	if end < 0 {
		t.Fatal("the restored unmanaged BIND proof has no end boundary")
	}
	body := source[start : start+end]
	for _, required := range []string{
		"layout.GenerationRoot",
		"bindOptionsMarkerBegin",
		"bindZonesMarkerBegin",
		"verifyOnlyBINDActive(",
	} {
		if !strings.Contains(body, required) {
			t.Errorf("the restored unmanaged BIND proof lost %s", required)
		}
	}
}
