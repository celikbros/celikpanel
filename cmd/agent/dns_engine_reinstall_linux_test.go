//go:build linux

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

// reinstallManifestForTest is the manifest the panel sends to repair a host
// whose active engine is missing: one engine, one epoch, standalone. It is
// built through the ordinary canonicalizer on purpose — the shape has to be
// expressible in the released payload contract, not only in a struct literal.
//
// reinstallManifestForTest, etkin motoru eksik bir sunucuyu onarmak için
// panelin gönderdiği bildirgedir: tek motor, tek çağ, tek sunucu. Bilerek
// olağan kanonikleştiriciden geçirilir — biçim yalnız bir yapı değişmezinde
// değil, yayımlanmış yük sözleşmesinde ifade edilebilir olmalıdır.
func reinstallManifestForTest(
	t *testing.T,
	epoch int64,
) mutationpayload.DNSEngineSwitchManifestCommitment {
	t.Helper()
	manifest, err := mutationpayload.CanonicalDNSEngineSwitchManifestWithPairIdentity(
		"reinstall",
		transport.DNSEngineBIND, transport.DNSEngineBIND,
		epoch, epoch, 3,
		transport.DNSTopologyStandalone, "", "", "", "", "",
		nil,
	)
	if err != nil {
		t.Fatalf("a reinstall must be expressible as a canonical manifest: %v", err)
	}
	return manifest
}

// The payload contract has to admit the shape before anything else can. On the
// unfixed tree this fails at the first line: source and target were required to
// differ and the target epoch was required to be the next one, so "put this
// engine back where it already is" could not be said at all.
//
// Yük sözleşmesi biçimi kabul etmeden başka hiçbir şey olamaz. Onarılmamış
// ağaçta bu, ilk satırda düşer: kaynak ile hedefin farklı, hedef çağın da bir
// sonraki olması isteniyordu; dolayısıyla "bu motoru zaten bulunduğu yere geri
// koy" hiç söylenemiyordu.
func TestDNSEngineReinstallManifestIsCanonicalAndDistinct(t *testing.T) {
	reinstall := reinstallManifestForTest(t, 1)
	if !mutationpayload.ReinstallsActiveDNSEngine(reinstall) {
		t.Fatalf("reinstall manifest is not recognised as one: %+v", reinstall)
	}
	install, err := mutationpayload.CanonicalDNSEngineSwitchManifestWithPairIdentity(
		transport.DNSEngineSwitchModeSwitch,
		"", transport.DNSEngineBIND, 0, 1, 3,
		transport.DNSTopologyStandalone, "", "", "", "", "", nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	// The mode is inside the digest, so a reinstall lease can never be spent on
	// a first install of the same engine at the same epoch with the same zones.
	if reinstall.Qualifier == install.Qualifier {
		t.Fatal("a reinstall and a first install share one qualifier")
	}
	if mutationpayload.ReinstallsActiveDNSEngine(install) {
		t.Fatal("a first install was read as a reinstall")
	}
	for _, bad := range []struct {
		name                     string
		source, target           transport.DNSEngine
		sourceEpoch, targetEpoch int64
		topology                 string
	}{
		{"different engines", transport.DNSEnginePowerDNS, transport.DNSEngineBIND, 1, 1, transport.DNSTopologyStandalone},
		{"advancing epoch", transport.DNSEngineBIND, transport.DNSEngineBIND, 1, 2, transport.DNSTopologyStandalone},
		{"epoch zero", transport.DNSEngineBIND, transport.DNSEngineBIND, 0, 0, transport.DNSTopologyStandalone},
		{"paired topology", transport.DNSEngineBIND, transport.DNSEngineBIND, 1, 1, transport.DNSTopologyPaired},
	} {
		t.Run(bad.name, func(t *testing.T) {
			if _, err := mutationpayload.CanonicalDNSEngineSwitchManifestWithPairIdentity(
				"reinstall", bad.source, bad.target,
				bad.sourceEpoch, bad.targetEpoch, 3,
				bad.topology, "", "", "", "", "", nil,
			); err == nil {
				t.Fatal("a reinstall manifest outside its exact identity was accepted")
			}
		})
	}
}

// stageAbsentActiveDNSEngine builds host B's durable shape exactly: BIND is the
// active state at epoch 1, BIND's ownership receipt sits at the same epoch with
// the generation that install wrote, the state's generation has since moved on
// with published zones, there is no install receipt, and no unit is running.
//
// stageAbsentActiveDNSEngine, B sunucusunun kalıcı biçimini birebir kurar: BIND
// epoch 1'de etkin durumdur, BIND'ın sahiplik makbuzu aynı çağda ama kurulumun
// yazdığı nesille durur, durumun nesli yayımlanan bölgelerle o zamandan beri
// ilerlemiştir, kurulum makbuzu yoktur ve çalışan birim yoktur.
func stageAbsentActiveDNSEngine(t *testing.T) (dnsEngineStateReceipt, dnsEngineStateReceipt) {
	t.Helper()
	prepareDNSEngineOwnershipTest(t)
	ownership := legacyDurableDNSState(transport.DNSEngineBIND)
	state := ownership
	state.Generation = strings.Repeat("6", 64)
	if err := writeDNSEngineState(state); err != nil {
		t.Fatalf("stage active state: %v", err)
	}
	if err := writeDNSEngineOwnership(ownership); err != nil {
		t.Fatalf("stage active ownership: %v", err)
	}
	return state, ownership
}

func stubAbsentPort53Conflict(t *testing.T) {
	t.Helper()
	previous := dnsPort53ConflictCheck
	dnsPort53ConflictCheck = func(context.Context, bool, bool) (bool, error) {
		return false, nil
	}
	t.Cleanup(func() { dnsPort53ConflictCheck = previous })
}

var inactiveDNSUnitForTest = dnsUnitState{ActiveState: "inactive"}

// The drill's host B, exactly: dns_engine_state.json says bind epoch 1, the
// ownership receipt says bind epoch 1 with an older generation because zone
// publication advanced the state and nothing rewrites ownership, and no BIND
// package exists. That is a repairable host, and the source proof must say so.
//
// Tatbikatın B sunucusu, birebir: dns_engine_state.json bind epoch 1 der,
// sahiplik makbuzu — bölge yayımı durumu ilerlettiği ve sahipliği hiçbir şey
// yeniden yazmadığı için daha eski bir nesille — bind epoch 1 der ve hiçbir
// BIND paketi yoktur. Bu onarılabilir bir sunucudur ve kaynak kanıtı bunu
// söylemelidir.
func TestDNSEngineReinstallSourceProofAcceptsTheAbsentOwnedEngine(t *testing.T) {
	state, ownership := stageAbsentActiveDNSEngine(t)
	if state == ownership {
		t.Fatal("the fixture must reproduce the live generation difference")
	}
	stubAbsentPort53Conflict(t)
	if err := verifyDNSEngineReinstallSource(
		context.Background(), reinstallManifestForTest(t, 1), state, true,
		inactiveDNSUnitForTest, inactiveDNSUnitForTest, inactiveDNSUnitForTest,
	); err != nil {
		t.Fatalf("the restored host's own shape was refused: %v", err)
	}
}

// Every refusal below is a way the reinstall could otherwise do harm on a host
// that is not the absent-engine shape.
//
// Aşağıdaki her ret, yeniden kurulumun, motoru eksik olmayan bir sunucuda aksi
// hâlde zarar verebileceği bir yoldur.
func TestDNSEngineReinstallSourceProofFailsClosedOutsideItsShape(t *testing.T) {
	t.Run("a running BIND is not an absent one", func(t *testing.T) {
		state, _ := stageAbsentActiveDNSEngine(t)
		stubAbsentPort53Conflict(t)
		if err := verifyDNSEngineReinstallSource(
			context.Background(), reinstallManifestForTest(t, 1), state, true,
			dnsUnitState{ActiveState: "active"}, inactiveDNSUnitForTest,
			inactiveDNSUnitForTest,
		); err == nil {
			t.Fatal("a serving BIND was reinstalled under it")
		}
	})
	t.Run("a running PowerDNS is not an absent BIND", func(t *testing.T) {
		state, _ := stageAbsentActiveDNSEngine(t)
		stubAbsentPort53Conflict(t)
		if err := verifyDNSEngineReinstallSource(
			context.Background(), reinstallManifestForTest(t, 1), state, true,
			inactiveDNSUnitForTest, inactiveDNSUnitForTest,
			dnsUnitState{ActiveState: "active"},
		); err == nil {
			t.Fatal("BIND was installed under a serving PowerDNS")
		}
	})
	t.Run("another public port-53 authority", func(t *testing.T) {
		state, _ := stageAbsentActiveDNSEngine(t)
		previous := dnsPort53ConflictCheck
		dnsPort53ConflictCheck = func(context.Context, bool, bool) (bool, error) {
			return true, nil
		}
		t.Cleanup(func() { dnsPort53ConflictCheck = previous })
		if err := verifyDNSEngineReinstallSource(
			context.Background(), reinstallManifestForTest(t, 1), state, true,
			inactiveDNSUnitForTest, inactiveDNSUnitForTest, inactiveDNSUnitForTest,
		); err == nil {
			t.Fatal("BIND was installed against a foreign port-53 authority")
		}
	})
	t.Run("an engine this panel never installed", func(t *testing.T) {
		prepareDNSEngineOwnershipTest(t)
		state := legacyDurableDNSState(transport.DNSEngineBIND)
		if err := writeDNSEngineState(state); err != nil {
			t.Fatal(err)
		}
		stubAbsentPort53Conflict(t)
		if err := verifyDNSEngineReinstallSource(
			context.Background(), reinstallManifestForTest(t, 1), state, true,
			inactiveDNSUnitForTest, inactiveDNSUnitForTest, inactiveDNSUnitForTest,
		); err == nil {
			t.Fatal("an engine with no ownership receipt was reinstalled")
		}
	})
	t.Run("an epoch the host does not hold", func(t *testing.T) {
		state, _ := stageAbsentActiveDNSEngine(t)
		stubAbsentPort53Conflict(t)
		if err := verifyDNSEngineReinstallSource(
			context.Background(), reinstallManifestForTest(t, 2), state, true,
			inactiveDNSUnitForTest, inactiveDNSUnitForTest, inactiveDNSUnitForTest,
		); err == nil {
			t.Fatal("a reinstall at the wrong epoch was accepted")
		}
	})
	t.Run("a host with no active engine at all", func(t *testing.T) {
		prepareDNSEngineOwnershipTest(t)
		stubAbsentPort53Conflict(t)
		if err := verifyDNSEngineReinstallSource(
			context.Background(), reinstallManifestForTest(t, 1),
			dnsEngineStateReceipt{}, false,
			inactiveDNSUnitForTest, inactiveDNSUnitForTest, inactiveDNSUnitForTest,
		); err == nil {
			t.Fatal("a first install was accepted as a reinstall")
		}
	})
}

func reinstallJournalForTest(
	t *testing.T,
	source dnsEngineStateReceipt,
) dnsEngineSwitchJournal {
	t.Helper()
	if err := writeDNSEngineState(source); err != nil {
		t.Fatalf("stage the journal's frozen source state: %v", err)
	}
	before, err := captureDNSEngineStateSnapshot(true)
	if err != nil {
		t.Fatal(err)
	}
	manifest := reinstallManifestForTest(t, source.EngineEpoch)
	return dnsEngineSwitchJournal{
		Schema: dnsEngineSwitchJournalSchema, Phase: dnsSwitchPhaseCommitted,
		Mode:              manifest.Mode,
		MutationRequestID: residueRequestID,
		MutationOwnerID:   residueOwnerID,
		ManifestQualifier: manifest.Qualifier,
		SourceEngine:      manifest.SourceEngine,
		TargetEngine:      manifest.TargetEngine,
		SourceEpoch:       manifest.SourceEpoch,
		TargetEpoch:       manifest.TargetEpoch,
		SourceRevision:    manifest.SourceRevision,
		Topology:          manifest.Topology,
		Zones:             manifest.Zones,
		SnapshotBytes:     manifest.SnapshotBytes,
		StateBefore:       before,
	}
}

// reinstallSourceStateForTest is the pre-operation receipt a reinstall journal
// freezes: the tenure this host already holds, at the revision and identity the
// manifest names.
//
// reinstallSourceStateForTest, bir yeniden kurulum günlüğünün dondurduğu
// işlem-öncesi makbuzdur: bu sunucunun zaten tuttuğu dönem, bildirgenin
// adlandırdığı revizyon ve kimlikle.
func reinstallSourceStateForTest(
	t *testing.T,
	generation string,
) dnsEngineStateReceipt {
	t.Helper()
	manifest := reinstallManifestForTest(t, 1)
	state := legacyDurableDNSState(transport.DNSEngineBIND)
	state.Generation = generation
	state.SourceRevision = manifest.SourceRevision
	state.ManifestQualifier = manifest.Qualifier
	state.MutationRequestID = residueRequestID
	state.MutationOwnerID = residueOwnerID
	return state
}

// The epoch rule that tells history from a contradiction cannot see a
// reinstall: the receipt it replaces is at the SAME epoch, which is exactly the
// shape the rule was written to reject. Without the third clause the reinstall
// would fail at finalization, with the packages already installed and BIND
// already serving — the worst possible place to refuse.
//
// Tarihi çelişkiden ayıran çağ kuralı yeniden kurulumu göremez: yerine geçtiği
// makbuz AYNI çağdadır ve bu tam olarak kuralın reddetmek için yazıldığı
// biçimdir. Üçüncü koşul olmadan yeniden kurulum, paketler çoktan kurulmuş ve
// BIND çoktan hizmet verirken sonlandırmada düşerdi — reddetmek için mümkün
// olan en kötü yer.
func TestDNSEngineReinstallOwnershipIsProvenanceAtTheSameEpoch(t *testing.T) {
	prepareDNSEngineOwnershipTest(t)
	ownership := reinstallSourceStateForTest(t, strings.Repeat("d", 64))
	committed := ownership
	committed.Generation = strings.Repeat("7", 64)
	committed.ManifestQualifier = residueQualifier

	journal := reinstallJournalForTest(t, ownership)
	if err := acceptableCommittedDNSEngineOwnership(
		ownership, committed, journal,
	); err != nil {
		t.Fatalf("the receipt a reinstall replaces was called a contradiction: %v", err)
	}

	// The allowance is bound to the exact frozen source state, not to "same
	// epoch". A receipt at that epoch that is not the one the journal froze is
	// still two states claiming one epoch.
	stranger := ownership
	stranger.Generation = strings.Repeat("8", 64)
	if err := acceptableCommittedDNSEngineOwnership(
		stranger, committed, journal,
	); err == nil {
		t.Fatal("an unrelated equal-epoch receipt was accepted under a reinstall")
	}

	// And the allowance never leaks into a switch. Without a reinstall journal
	// the same pair is the contradiction it always was.
	switchJournal := journal
	switchJournal.Mode = transport.DNSEngineSwitchModeSwitch
	if err := acceptableCommittedDNSEngineOwnership(
		ownership, committed, switchJournal,
	); err == nil {
		t.Fatal("a switch accepted an equal-epoch receipt that differs")
	}
}

// A reinstall re-establishes the tenure it repaired, so the durable receipt it
// leaves must be indistinguishable from the one the host lost. Writing the
// operation's own mode there would stamp the tenure with the accident.
//
// Yeniden kurulum, onardığı dönemi yeniden kurar; bu yüzden bıraktığı kalıcı
// makbuz, sunucunun kaybettiğinden ayırt edilemez olmalıdır. Oraya işlemin
// kendi kipini yazmak, dönemi kazayla damgalardı.
func TestDNSEngineReinstallLeavesASwitchTenureReceipt(t *testing.T) {
	manifest := reinstallManifestForTest(t, 1)
	if got := dnsEngineTenureModeForManifest(manifest); got != transport.DNSEngineSwitchModeSwitch {
		t.Fatalf("reinstall tenure mode = %q, want %q",
			got, transport.DNSEngineSwitchModeSwitch)
	}
	state := reinstallSourceStateForTest(t, strings.Repeat("d", 64))
	state.Mode = dnsEngineTenureModeForManifest(manifest)
	if err := validateDNSEngineState(state); err != nil {
		t.Fatalf("the receipt a reinstall writes is not a valid state: %v", err)
	}
	journal := reinstallJournalForTest(t, state)
	journal.TargetGeneration = state.Generation
	if !exactDNSEngineStateForJournal(state, journal) {
		t.Fatal("a reinstall's committed journal does not match the receipt it writes")
	}
	// The tenure mode is the only thing relaxed. A receipt that claims the
	// operation's own mode is still not the tenure the journal committed.
	stamped := state
	stamped.Mode = transport.DNSEngineSwitchModeReinstall
	if exactDNSEngineStateForJournal(stamped, journal) {
		t.Fatal("a receipt stamped with the operation mode was accepted as the tenure")
	}
}

// Both of these were found by the first live reinstall on the drill's restored
// host, after apt had already put BIND on the machine. Each one alone ends the
// operation in the worst place there is: packages installed, nothing serving,
// and — because the abort proof then failed too — a mutation ledger poisoned by
// durable state that reproduces the poison on every boot.
//
// İkisi de, apt BIND'ı makineye çoktan koyduktan sonra, tatbikatın geri
// yüklenmiş sunucusundaki ilk canlı yeniden kurulumda bulundu. Her biri tek
// başına işlemi olabilecek en kötü yerde bitirir: paketler kurulu, hiçbir şey
// hizmet vermiyor ve — iptal kanıtı da düştüğü için — her açılışta zehri
// yeniden üreten kalıcı durumla zehirlenmiş bir mutasyon defteri.
// aptBINDConfigSnapshotsForTest is the Debian BIND configuration preimage a
// journal freezes before it edits anything.
//
// aptBINDConfigSnapshotsForTest, bir günlüğün herhangi bir şeyi düzenlemeden
// önce dondurduğu Debian BIND yapılandırma ön görüntüsüdür.
func aptBINDConfigSnapshotsForTest() []dnsFileSnapshot {
	snapshots := make([]dnsFileSnapshot, 0, 2)
	for _, path := range []string{
		"/etc/bind/named.conf.local", "/etc/bind/named.conf.options",
	} {
		data := []byte("// frozen preimage of " + path)
		snapshots = append(snapshots, dnsFileSnapshot{
			Path: path, Exists: true, Mode: 0o644,
			OwnerKnown: true, UID: 0, GID: 102,
			SHA256: digestDNSBytes(data), Data: data,
		})
	}
	return snapshots
}

func TestDNSEngineReinstallJournalRecordsOneUnitSetForOneEngine(t *testing.T) {
	prepareDNSEngineOwnershipTest(t)
	source := reinstallSourceStateForTest(t, strings.Repeat("d", 64))
	journal := reinstallJournalForTest(t, source)
	journal.Phase = dnsSwitchPhaseIntent
	journal.TargetGeneration = strings.Repeat("9", 64)
	journal.ConfigBefore = aptBINDConfigSnapshotsForTest()
	journal.TargetUnitsBefore = []dnsUnitSnapshot{
		{Name: "bind9.service", LoadState: "not-found", ActiveState: "inactive"},
		{Name: "named.service", LoadState: "not-found", ActiveState: "inactive"},
	}
	journal.SourceUnitsBefore = nil

	if err := validateDNSEngineSwitchJournal(journal); err != nil {
		t.Fatalf("a reinstall journal that froze its one unit set was refused: %v", err)
	}

	// The rule is exact in both directions. A reinstall that recorded a
	// separate source unit set recorded the same units twice, which is not the
	// operation this journal describes; and forgetting the target set would
	// leave the rollback nothing to restore.
	//
	// Kural her iki yönde de tamdır. Ayrı bir kaynak birim kümesi kaydeden bir
	// yeniden kurulum aynı birimleri iki kez kaydetmiştir ve bu, günlüğün
	// tarif ettiği işlem değildir; hedef kümesini unutmak ise geri almaya
	// eski hâline döndürecek hiçbir şey bırakmaz.
	for name, mutate := range map[string]func(*dnsEngineSwitchJournal){
		"units recorded twice": func(candidate *dnsEngineSwitchJournal) {
			candidate.SourceUnitsBefore = journal.TargetUnitsBefore
		},
		"no unit snapshots at all": func(candidate *dnsEngineSwitchJournal) {
			candidate.TargetUnitsBefore = nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := journal
			mutate(&candidate)
			err := validateDNSEngineSwitchJournal(candidate)
			if err == nil ||
				!strings.Contains(err.Error(), "unit snapshot set is incomplete") {
				t.Fatalf("accepted a journal with %s: %v", name, err)
			}
		})
	}
}

func TestDNSEngineReinstallAbortLeavesRecoverableInstallResidue(t *testing.T) {
	state, _ := stageAbsentActiveDNSEngine(t)
	if err := writeDNSEngineOwnership(state); err != nil {
		t.Fatal(err)
	}
	binding := transport.ServiceMutationBinding{
		MutationRequestID: residueRequestID,
		MutationOwnerID:   residueOwnerID,
	}
	if err := writeDNSEngineInstallOwnership(dnsEngineInstallOwnershipReceipt{
		Schema:            dnsEngineInstallOwnershipSchema,
		Engine:            transport.DNSEngineBIND,
		PackageManager:    "apt",
		Packages:          []string{"bind9"},
		MissingBefore:     []string{"bind9"},
		ManifestQualifier: residueQualifier,
		MutationRequestID: residueRequestID,
		MutationOwnerID:   residueOwnerID,
	}); err != nil {
		t.Fatal(err)
	}
	finalized, err := exactFinalizedDNSEngineSwitchProvenanceOnHost(
		transport.DNSEngineBIND, residueQualifier, binding,
	)
	if err != nil {
		t.Fatalf("an aborted reinstall's own install receipt poisoned the ledger: %v", err)
	}
	if finalized {
		t.Fatal("nothing was finalized; the reinstall never reached its state receipt")
	}

	// An install receipt on the active engine that names a DIFFERENT
	// transaction is not this abort's residue and still fails closed.
	if _, err := exactFinalizedDNSEngineSwitchProvenanceOnHost(
		transport.DNSEngineBIND,
		"dns-engine-switch/v1:sha256:"+strings.Repeat("a", 64), binding,
	); err == nil {
		t.Fatal("a foreign install receipt on the active engine was dismissed")
	}
}
