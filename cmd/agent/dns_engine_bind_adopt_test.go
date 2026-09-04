package main

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/hostplatform"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

func TestBINDDeclaredZoneNamesReadsOnlyRealDeclarations(t *testing.T) {
	config := `
options {
	directory "/var/cache/bind";
	allow-transfer { none; };
};

// zone "commented-out.test" { type master; };
# zone "hash-commented.test" { type master; };
/* zone "block-commented.test" { type master; }; */

zone "Example.TEST." {
	type master;
	file "/etc/bind/db.example";
};

zone	"second.test" IN {
	type master;
	file "/etc/bind/db.second";
};

zone
	"wrapped.test" {
	type master;
	file "/etc/bind/db.wrapped";
};

zone-statistics yes;

zone "second.test" {
	type master;
};
`
	got := bindDeclaredZoneNames(config)
	want := []string{"example.test", "second.test", "wrapped.test"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("zones=%v want=%v", got, want)
	}
}

func manifestWithDomains(domains ...string) mutationpayload.DNSEngineSwitchManifestCommitment {
	manifest := mutationpayload.DNSEngineSwitchManifestCommitment{}
	for _, domain := range domains {
		manifest.Zones = append(
			manifest.Zones,
			transport.DNSEngineSwitchZoneSnapshot{Domain: domain},
		)
	}
	return manifest
}

// The decision this test pins: a foreign zone is kept, not refused, because
// CelikPanel's BIND generation deletes no zone declaration. The only zone the
// adoption cannot take over is one whose name CelikPanel also publishes, and
// that one is named rather than discovered later inside named-checkconf.
//
// Bu testin sabitledigi karar: yabanci bir bolge reddedilmez, korunur; cunku
// CelikPanel'in BIND nesli hicbir bolge bildirimini silmez. Devralmanin
// devralamadigi tek bolge, adini CelikPanel'in de yayimladigi bolgedir ve o
// bolge, sonradan named-checkconf'un icinde bulunmak yerine adlandirilir.
func TestForeignBINDZoneCollisionsNamesOnlyTheZonesBothDeclare(t *testing.T) {
	foreign := []string{"kept-one.test", "kept-two.test", "shared.test"}
	manifest := manifestWithDomains("shared.test", "panel-only.test")
	got := foreignBINDZoneCollisions(foreign, manifest)
	if !reflect.DeepEqual(got, []string{"shared.test"}) {
		t.Fatalf("collisions=%v want=[shared.test]", got)
	}

	// A zone the ledger is deleting is not published, so it cannot collide.
	deleting := manifestWithDomains("shared.test")
	deleting.Zones[0].Delete = true
	if got := foreignBINDZoneCollisions(foreign, deleting); len(got) != 0 {
		t.Fatalf("deleted ledger zone collided: %v", got)
	}

	// Trailing dots and case are presentation, not identity.
	if got := foreignBINDZoneCollisions(
		[]string{"shared.test"}, manifestWithDomains("SHARED.TEST."),
	); !reflect.DeepEqual(got, []string{"shared.test"}) {
		t.Fatalf("canonicalised collision=%v", got)
	}

	if got := foreignBINDZoneCollisions(
		[]string{"kept-one.test"}, manifestWithDomains("panel-only.test"),
	); len(got) != 0 {
		t.Fatalf("a zone only the server declares was refused: %v", got)
	}
}

func takeoverManifest() mutationpayload.DNSEngineSwitchManifestCommitment {
	return mutationpayload.DNSEngineSwitchManifestCommitment{
		Mode:         transport.DNSEngineSwitchModeSwitch,
		TargetEngine: transport.DNSEngineBIND,
		TargetEpoch:  1,
		Topology:     transport.DNSTopologyStandalone,
	}
}

func TestAdoptableRunningBINDManifestIsTheTakeoversAlone(t *testing.T) {
	if !adoptableRunningBINDManifest(takeoverManifest(), false) {
		t.Fatal("the takeover manifest was not adoptable")
	}
	if adoptableRunningBINDManifest(takeoverManifest(), true) {
		t.Fatal("an existing engine receipt was adoptable")
	}
	mutations := map[string]func(*mutationpayload.DNSEngineSwitchManifestCommitment){
		"a live source engine": func(m *mutationpayload.DNSEngineSwitchManifestCommitment) {
			m.SourceEngine = transport.DNSEnginePowerDNS
			m.SourceEpoch = 1
		},
		"a PowerDNS target": func(m *mutationpayload.DNSEngineSwitchManifestCommitment) {
			m.TargetEngine = transport.DNSEnginePowerDNS
		},
		"a reinstall": func(m *mutationpayload.DNSEngineSwitchManifestCommitment) {
			m.Mode = transport.DNSEngineSwitchModeReinstall
		},
		"a registration adoption": func(m *mutationpayload.DNSEngineSwitchManifestCommitment) {
			m.Mode = transport.DNSEngineSwitchModeAdopt
		},
		"a later epoch": func(m *mutationpayload.DNSEngineSwitchManifestCommitment) {
			m.TargetEpoch = 2
		},
		"a paired topology": func(m *mutationpayload.DNSEngineSwitchManifestCommitment) {
			m.Topology = transport.DNSTopologyPaired
			m.PairRole = transport.DNSPairRolePrimary
		},
		"a peer": func(m *mutationpayload.DNSEngineSwitchManifestCommitment) {
			m.PeerIP = "198.51.100.7"
		},
	}
	for name, mutate := range mutations {
		manifest := takeoverManifest()
		mutate(&manifest)
		if adoptableRunningBINDManifest(manifest, false) {
			t.Errorf("%s was accepted as a running takeover", name)
		}
	}
}

// The reload is the running adoption's whole promise: the host answers
// throughout. A restarted process breaks that promise even when systemd
// reports success, so the proof is the main pid, not the exit code.
//
// Yeniden yukleme, calisan devralmanin butun sozudur: sunucu boyunca yanit
// verir. Yeniden baslamis bir surec, systemd basari bildirse bile bu sozu
// bozar; dolayisiyla kanit cikis kodu degil ana surectir.
func TestReloadAdoptedBINDRefusesARestartedProcess(t *testing.T) {
	before := dnsUnitProcesses{MainPID: 4242, SubState: "running"}
	reloads := 0
	reload := func() error { reloads++; return nil }

	if err := reloadAdoptedBINDWithOps(
		before, reload,
		func() (dnsUnitProcesses, error) { return before, nil },
	); err != nil {
		t.Fatalf("an unchanged running process was refused: %v", err)
	}
	if reloads != 1 {
		t.Fatalf("reloads=%d want 1", reloads)
	}

	restarted := []dnsUnitProcesses{
		{MainPID: 4243, SubState: "running"},
		{MainPID: 0, SubState: "dead"},
		{MainPID: 4242, SubState: "failed"},
	}
	for _, after := range restarted {
		err := reloadAdoptedBINDWithOps(
			before, reload,
			func() (dnsUnitProcesses, error) { return after, nil },
		)
		if err == nil {
			t.Fatalf("a service gap was accepted: after=%+v", after)
		}
		if !strings.Contains(err.Error(), "did not stay up across its reload") {
			t.Fatalf("unexpected reload error: %v", err)
		}
	}

	// Nothing to preserve means there was nothing running to adopt.
	if err := reloadAdoptedBINDWithOps(
		dnsUnitProcesses{}, reload,
		func() (dnsUnitProcesses, error) { return before, nil },
	); err == nil {
		t.Fatal("a stopped engine was reloaded as an adoption")
	}
}

type recordedBINDAdoptionRollback struct {
	steps    []string
	failures map[string]error
}

func (recorder *recordedBINDAdoptionRollback) step(name string) func() error {
	return func() error {
		recorder.steps = append(recorder.steps, name)
		return recorder.failures[name]
	}
}

func (recorder *recordedBINDAdoptionRollback) ops() bindAdoptionRollbackOps {
	return bindAdoptionRollbackOps{
		restoreConfigs: recorder.step("restore-configs"),
		reload:         recorder.step("reload"),
		verifyConfigs:  recorder.step("verify-configs"),
		verifyRuntime:  recorder.step("verify-runtime"),
		restoreState:   recorder.step("restore-state"),
		restoreUnits:   recorder.step("restore-units"),
	}
}

// A failed adoption must put the configuration back and leave the server
// answering as it did before. The files come back first and the daemon is told
// to re-read them second, because a reload before the restore would publish the
// half-applied configuration the rollback exists to undo.
//
// Basarisiz bir devralma yapilandirmayi geri koymali ve sunucuyu onceki gibi
// yanit verir birakmalidir. Once dosyalar geri gelir, sonra artalan surece
// onlari yeniden okumasi soylenir; cunku geri yuklemeden onceki bir yeniden
// yukleme, geri almanin var olma sebebi olan yarim uygulanmis yapilandirmayi
// yayimlardi.
func TestBINDAdoptionRollbackRestoresTheConfigurationBeforeItReloads(t *testing.T) {
	recorder := &recordedBINDAdoptionRollback{failures: map[string]error{}}
	if err := rollbackRunningBINDAdoptionWithOps(recorder.ops()); err != nil {
		t.Fatalf("rollback failed: %v", err)
	}
	want := []string{
		"restore-configs", "reload", "verify-configs", "verify-runtime",
		"restore-state", "restore-units",
	}
	if !reflect.DeepEqual(recorder.steps, want) {
		t.Fatalf("steps=%v want=%v", recorder.steps, want)
	}
}

func TestBINDAdoptionRollbackStopsAtItsFirstFailure(t *testing.T) {
	boom := errors.New("boom")
	cases := map[string][]string{
		"restore-configs": {"restore-configs"},
		"reload":          {"restore-configs", "reload"},
		"verify-configs":  {"restore-configs", "reload", "verify-configs"},
		"verify-runtime": {
			"restore-configs", "reload", "verify-configs", "verify-runtime",
		},
	}
	for failing, want := range cases {
		recorder := &recordedBINDAdoptionRollback{
			failures: map[string]error{failing: boom},
		}
		err := rollbackRunningBINDAdoptionWithOps(recorder.ops())
		if !errors.Is(err, boom) {
			t.Fatalf("%s: err=%v want boom", failing, err)
		}
		if !reflect.DeepEqual(recorder.steps, want) {
			t.Fatalf("%s: steps=%v want=%v", failing, recorder.steps, want)
		}
	}
	if err := rollbackRunningBINDAdoptionWithOps(
		bindAdoptionRollbackOps{},
	); err == nil {
		t.Fatal("an incomplete rollback was accepted")
	}
}

func readAgentSource(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// R-039's safety rule, pinned in the source: adoption has its own evidence
// path. The switch's not-serving proof and its port-53 pre-mutation guard are
// the wrong proofs for an operation that never stops the engine, so the
// adoption neither calls them nor weakens them - and it never stops, starts,
// restarts or masks the server it is adopting. The dispatch is checked too: the
// takeover leaves the switch transaction before that transaction reaches its
// own proofs, which is why they stay exactly as written.
//
// R-039'un kaynakta sabitlenen guvenlik kurali: devralmanin kendi kanit yolu
// vardir. Gecisin hizmet-vermiyor kaniti ve 53 numarali baglanti noktasi
// on-mutasyon korumasi, motoru hic durdurmayan bir islem icin yanlis
// kanitlardir; dolayisiyla devralma onlari ne cagirir ne zayiflatir - ve
// devraldigi sunucuyu hic durdurmaz, baslatmaz, yeniden baslatmaz, maskelemez.
// Gonderim de denetlenir: devralma, gecis islemini o islem kendi kanitlarina
// ulasmadan once terk eder; onlar bu yuzden yazildigi gibi kalir.
func TestRunningBINDAdoptionKeepsTheSwitchProofsOutOfItsPath(t *testing.T) {
	adoption := readAgentSource(t, "dns_engine_bind_adopt.go")
	for _, forbidden := range []string{
		"proveBINDTargetNotServing(",
		"proveBINDTargetNotServingWithOps(",
		"verifyBINDTargetNotServing(",
		"verifyBINDSealedTargetNotServing(",
		"verifyBINDAbsentTargetNotServing(",
		"runDNSPort53PreMutationGuard(",
		"runVerifiedBINDTargetInstall(",
		"runBINDPostInstallContinuation(",
		"installBINDPackagesWithGuard(",
		"activateBINDTargetWithVerifiedIdentity(",
		"stopBINDUnitsFailClosed(",
		"rollbackBINDActivation(",
		"verifyRestoredDNSSwitchSource(",
		`"stop"`,
		`"restart"`,
		`"mask"`,
		`"disable"`,
	} {
		if strings.Contains(adoption, forbidden) {
			t.Errorf("the running adoption reaches %s", forbidden)
		}
	}
	for _, required := range []string{
		"verifyOnlyBINDActive(",
		"verifyBINDConfigMutationPreimage(",
		"assumeExistingDNSEnginePackageOwnership(",
		"reloadAdoptedBIND(",
		`"reload"`,
	} {
		if !strings.Contains(adoption, required) {
			t.Errorf("the running adoption lost %s", required)
		}
	}

	host := readAgentSource(t, "dns_engine_host.go")
	dispatch := strings.Index(host, "return adoptRunningBIND(")
	proof := strings.Index(host, "targetInstallProof, err := proveBINDTargetNotServing(")
	guard := strings.Index(host, "return runDNSPort53PreMutationGuard(")
	if dispatch < 0 || proof < 0 || guard < 0 {
		t.Fatalf(
			"switch dispatch=%d not-serving proof=%d port-53 guard=%d",
			dispatch, proof, guard,
		)
	}
	if dispatch > proof || dispatch > guard {
		t.Fatal("the running takeover reaches the switch proofs before it branches")
	}
}

func TestBINDAdoptionMutationGateRefusesAnIncompleteCall(t *testing.T) {
	profile := hostplatform.Profile{}
	if err := mutateBINDAdoptionAfterProof(
		nil, profile, "systemctl",
		bindConfigMutation{}, bindAdoptionRuntimeEvidence{},
		func() error { return nil },
	); err == nil {
		t.Fatal("a context-free adoption mutation was accepted")
	}
	if err := mutateBINDAdoptionAfterProof(
		context.Background(), profile, "systemctl",
		bindConfigMutation{}, bindAdoptionRuntimeEvidence{}, nil,
	); err == nil {
		t.Fatal("an adoption mutation without a callback was accepted")
	}
}
