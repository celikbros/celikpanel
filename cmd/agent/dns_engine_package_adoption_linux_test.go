//go:build linux

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/hostplatform"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

// stagePreinstalledDNSEngineTargetSwitch reproduces the host the S-9 T5
// campaign measured on Debian 13: a committed switch to PowerDNS on a machine
// whose PowerDNS packages were already installed before the switch began. The
// switch therefore installed nothing, and on the unfixed tree that means the
// target carries neither an install receipt nor an active ownership receipt at
// the moment finalization asks for provenance.
//
// stagePreinstalledDNSEngineTargetSwitch, S-9 T5 kampanyasının Debian 13
// üzerinde ölçtüğü sunucuyu yeniden üretir: PowerDNS paketleri geçiş başlamadan
// önce zaten kurulu olan bir makinede committed bir PowerDNS geçişi. Geçiş bu
// yüzden hiçbir şey kurmaz ve onarılmamış ağaçta bu, sonlandırma kökeni
// istediği anda hedefin ne kurulum makbuzu ne de etkin sahiplik makbuzu
// taşıması demektir.
func stagePreinstalledDNSEngineTargetSwitch(t *testing.T) (
	dnsEngineSwitchJournal,
	dnsEngineStateReceipt,
	mutationpayload.DNSEngineSwitchManifestCommitment,
	[]string,
	hostplatform.Profile,
) {
	t.Helper()
	journal, _, state, _ := signedUpdatePDNSBostonCommittedRecoveryFixture(t)
	if err := os.MkdirAll(serviceMutationStateDirectory(), 0o700); err != nil {
		t.Fatal(err)
	}
	journal.StateBefore.Path = filepath.Clean(dnsEngineStatePath())
	if err := validateDNSEngineSwitchJournal(journal); err != nil {
		t.Fatalf("invalid preinstalled-target switch fixture: %v", err)
	}
	manifest, err := switchJournalManifest(journal)
	if err != nil {
		t.Fatal(err)
	}
	profile := testUbuntuBINDProfile()
	packages, err := managedDNSEnginePackagesForProfile(
		profile, transport.DNSEnginePowerDNS,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeDNSEngineState(state); err != nil {
		t.Fatal(err)
	}
	if _, exists, err := readDNSEngineInstallOwnership(
		transport.DNSEnginePowerDNS,
	); err != nil || exists {
		t.Fatalf("preinstalled-target fixture already has install provenance: exists=%v err=%v", exists, err)
	}
	if _, exists, err := readDNSEngineOwnership(
		transport.DNSEnginePowerDNS,
	); err != nil || exists {
		t.Fatalf("preinstalled-target fixture already has active ownership: exists=%v err=%v", exists, err)
	}
	return journal, state, manifest, packages, profile
}

// The whole defect and the whole fix, in one sequence. Before the switch takes
// ownership of the packages it found, finalization refuses the committed
// transaction with the exact hold the live host raised; after it does, the same
// finalization runs to its end — publish the active ownership, retire the
// install receipt, and prove the journal-free finalized shape.
//
// Kusurun ve onarımın tamamı, tek bir dizide. Geçiş bulduğu paketlerin
// sahipliğini almadan önce sonlandırma, committed işlemi canlı sunucunun
// verdiği askı iletisinin aynısıyla reddeder; aldıktan sonra aynı sonlandırma
// sonuna kadar çalışır — etkin sahipliği yayımla, kurulum makbuzunu emekliye
// ayır ve günlüksüz sonlandırılmış biçimi kanıtla.
func TestPreinstalledDNSEngineTargetFinalizesThroughAdoptedProvenance(t *testing.T) {
	journal, state, manifest, packages, profile :=
		stagePreinstalledDNSEngineTargetSwitch(t)
	binding := switchJournalBinding(journal)

	_, _, _, err := exactCommittedDNSEngineProvenanceOnHost(journal, profile)
	if err == nil || !strings.Contains(
		err.Error(),
		"committed DNS engine switch has no exact install or active ownership provenance",
	) {
		t.Fatalf("unexpected pre-adoption provenance verdict: %v", err)
	}

	if err := assumeExistingDNSEnginePackageOwnership(
		transport.DNSEnginePowerDNS, profile.PackageManager,
		packages, manifest, binding,
	); err != nil {
		t.Fatalf("the switch could not take the present packages under management: %v", err)
	}

	adopted, exists, err := readDNSEngineInstallOwnership(transport.DNSEnginePowerDNS)
	if err != nil || !exists {
		t.Fatalf("read adopted install ownership: exists=%v err=%v", exists, err)
	}
	if !adopted.AdoptedPresent || len(adopted.MissingBefore) != 0 ||
		adopted.ManifestQualifier != manifest.Qualifier ||
		adopted.MutationRequestID != binding.MutationRequestID ||
		adopted.MutationOwnerID != binding.MutationOwnerID {
		t.Fatalf("adoption receipt is not this switch's provenance: %+v", adopted)
	}

	provedState, installExists, ownershipExists, err :=
		exactCommittedDNSEngineProvenanceOnHost(journal, profile)
	if err != nil || provedState != state || !installExists || ownershipExists {
		t.Fatalf(
			"adopted provenance state=%+v install=%v ownership=%v err=%v",
			provedState, installExists, ownershipExists, err,
		)
	}

	if err := publishCommittedDNSEngineTargetOwnership(journal); err != nil {
		t.Fatal(err)
	}
	provedState, installExists, ownershipExists, err =
		exactCommittedDNSEngineProvenanceOnHost(journal, profile)
	if err != nil || provedState != state || !installExists || !ownershipExists {
		t.Fatalf(
			"published provenance state=%+v install=%v ownership=%v err=%v",
			provedState, installExists, ownershipExists, err,
		)
	}
	if err := retireDNSEngineInstallOwnership(journal); err != nil {
		t.Fatal(err)
	}
	provedState, installExists, ownershipExists, err =
		exactCommittedDNSEngineProvenanceOnHost(journal, profile)
	if err != nil || provedState != state || installExists || !ownershipExists {
		t.Fatalf(
			"retired provenance state=%+v install=%v ownership=%v err=%v",
			provedState, installExists, ownershipExists, err,
		)
	}
	finalized, err := exactFinalizedDNSEngineSwitchProvenanceOnHost(
		transport.DNSEnginePowerDNS, journal.ManifestQualifier, binding,
	)
	if err != nil || !finalized {
		t.Fatalf("finalized=%v err=%v", finalized, err)
	}
}

// Recording the adoption is not a way for any receipt to pass. The receipt must
// name this transaction — its manifest qualifier and its request and owner id —
// and finalization compares all three against the journal it is finalizing, so
// a receipt written by another mutation, or by an earlier attempt at this one,
// is refused exactly as before.
//
// Devralmayı kaydetmek, herhangi bir makbuzun geçmesinin yolu değildir. Makbuz
// bu işlemi adlandırmak zorundadır — manifest niteleyicisi ile istek ve sahip
// kimliği — ve sonlandırma üçünü de sonlandırdığı günlükle karşılaştırır; başka
// bir mutasyonun ya da bu işlemin daha önceki bir denemesinin yazdığı bir makbuz
// eskisi gibi reddedilir.
func TestAdoptedDNSEngineProvenanceRefusesAForeignReceipt(t *testing.T) {
	journal, _, manifest, packages, profile :=
		stagePreinstalledDNSEngineTargetSwitch(t)
	binding := switchJournalBinding(journal)
	foreignManifest, err := mutationpayload.CanonicalDNSEngineSwitchManifestWithPairIdentity(
		manifest.Mode, manifest.SourceEngine, manifest.TargetEngine,
		manifest.SourceEpoch, manifest.TargetEpoch, manifest.SourceRevision+1,
		manifest.Topology, manifest.PairRole, manifest.LocalIP, manifest.LocalNS,
		manifest.PeerIP, manifest.PeerNS, manifest.Zones,
	)
	if err != nil {
		t.Fatal(err)
	}
	if foreignManifest.Qualifier == manifest.Qualifier {
		t.Fatal("the foreign manifest fixture is not distinct")
	}

	tests := []struct {
		name     string
		manifest mutationpayload.DNSEngineSwitchManifestCommitment
		binding  transport.ServiceMutationBinding
	}{
		{
			name:     "another mutation's request id",
			manifest: manifest,
			binding: transport.ServiceMutationBinding{
				MutationRequestID: strings.Repeat("e", 32),
				MutationOwnerID:   binding.MutationOwnerID,
			},
		},
		{
			name:     "another mutation's owner id",
			manifest: manifest,
			binding: transport.ServiceMutationBinding{
				MutationRequestID: binding.MutationRequestID,
				MutationOwnerID:   strings.Repeat("f", 32),
			},
		},
		{
			name:     "a stale manifest qualifier",
			manifest: foreignManifest,
			binding:  binding,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			foreign, err := newDNSEngineInstallOwnership(
				transport.DNSEnginePowerDNS, profile.PackageManager,
				packages, nil, test.manifest, test.binding,
			)
			if err != nil {
				t.Fatal(err)
			}
			if err := writeDNSEngineInstallOwnership(foreign); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := removeDNSEngineInstallOwnership(
					transport.DNSEnginePowerDNS,
				); err != nil {
					t.Fatal(err)
				}
			})
			_, _, _, err = exactCommittedDNSEngineProvenanceOnHost(journal, profile)
			if err == nil || !strings.Contains(
				err.Error(),
				"committed DNS engine install ownership differs from its journal",
			) {
				t.Fatalf("a foreign adoption receipt was accepted as provenance: %v", err)
			}
		})
	}
}

// The sequence the acceptance campaign ran: a switch is killed, the agent rolls
// it back, and the operator retries the same switch. The rollback restores
// config, state and units and leaves the packages installed, so the retry again
// installs nothing. Both post-rollback host shapes must converge — the one where
// the first attempt's receipt survived, and the one where there was never a
// receipt at all because the first attempt found nothing missing either.
//
// Kabul kampanyasının çalıştırdığı dizi: bir geçiş öldürülür, ajan onu geri
// alır ve operatör aynı geçişi yeniden dener. Geri alma yapılandırmayı, durumu
// ve unit'leri onarır, paketleri kurulu bırakır; yeniden deneme de bu yüzden
// hiçbir şey kurmaz. Geri almadan sonraki iki sunucu biçimi de yakınsamak
// zorundadır — ilk denemenin makbuzunun hayatta kaldığı biçim ve ilk deneme de
// eksik bir şey bulamadığı için hiç makbuzun olmadığı biçim.
func TestRolledBackSwitchRetryAdoptsThePresentPackages(t *testing.T) {
	for _, test := range []struct {
		name            string
		receiptSurvived bool
	}{
		{name: "the first attempt left no receipt to rebind"},
		{name: "the first attempt's receipt survived the rollback", receiptSurvived: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			firstJournal, _, manifest, packages, profile :=
				stagePreinstalledDNSEngineTargetSwitch(t)
			firstBinding := switchJournalBinding(firstJournal)

			if err := assumeExistingDNSEnginePackageOwnership(
				transport.DNSEnginePowerDNS, profile.PackageManager,
				packages, manifest, firstBinding,
			); err != nil {
				t.Fatalf("first attempt could not take ownership: %v", err)
			}
			if !test.receiptSurvived {
				if err := removeDNSEngineInstallOwnership(
					transport.DNSEnginePowerDNS,
				); err != nil {
					t.Fatal(err)
				}
			}

			retryJournal, retryState := retriedPDNSSwitchTransaction(
				t, firstJournal, strings.Repeat("c", 32), strings.Repeat("d", 32),
			)
			retryBinding := switchJournalBinding(retryJournal)
			if err := writeDNSEngineState(retryState); err != nil {
				t.Fatal(err)
			}
			if _, _, _, err := exactCommittedDNSEngineProvenanceOnHost(
				retryJournal, profile,
			); err == nil {
				t.Fatal("the retry had provenance before it took ownership")
			}

			if err := assumeExistingDNSEnginePackageOwnership(
				transport.DNSEnginePowerDNS, profile.PackageManager,
				packages, manifest, retryBinding,
			); err != nil {
				t.Fatalf("retry could not take the present packages under management: %v", err)
			}
			receipt, exists, err := readDNSEngineInstallOwnership(
				transport.DNSEnginePowerDNS,
			)
			if err != nil || !exists {
				t.Fatalf("read retry install ownership: exists=%v err=%v", exists, err)
			}
			if !receipt.AdoptedPresent || len(receipt.MissingBefore) != 0 ||
				receipt.MutationRequestID != retryBinding.MutationRequestID ||
				receipt.MutationOwnerID != retryBinding.MutationOwnerID {
				t.Fatalf("retry receipt does not name the retry: %+v", receipt)
			}

			provedState, installExists, ownershipExists, err :=
				exactCommittedDNSEngineProvenanceOnHost(retryJournal, profile)
			if err != nil || provedState != retryState ||
				!installExists || ownershipExists {
				t.Fatalf(
					"retry provenance state=%+v install=%v ownership=%v err=%v",
					provedState, installExists, ownershipExists, err,
				)
			}
			if err := publishCommittedDNSEngineTargetOwnership(retryJournal); err != nil {
				t.Fatal(err)
			}
			if err := retireDNSEngineInstallOwnership(retryJournal); err != nil {
				t.Fatal(err)
			}
			finalized, err := exactFinalizedDNSEngineSwitchProvenanceOnHost(
				transport.DNSEnginePowerDNS, retryJournal.ManifestQualifier,
				retryBinding,
			)
			if err != nil || !finalized {
				t.Fatalf("retry finalized=%v err=%v", finalized, err)
			}
		})
	}
}

// retriedPDNSSwitchTransaction rebuilds the same switch under a new mutation
// identity, which is what the operator's retry is: the same manifest, a new
// request and owner id, and therefore new staging paths.
//
// retriedPDNSSwitchTransaction, aynı geçişi yeni bir mutasyon kimliği altında
// yeniden kurar; operatörün yeniden denemesi tam olarak budur: aynı manifest,
// yeni istek ve sahip kimliği, dolayısıyla yeni hazırlama yolları.
func retriedPDNSSwitchTransaction(
	t *testing.T,
	journal dnsEngineSwitchJournal,
	requestID, ownerID string,
) (dnsEngineSwitchJournal, dnsEngineStateReceipt) {
	t.Helper()
	retry := clonePDNSReconfigureRecoveryJournal(journal)
	retry.MutationRequestID = requestID
	retry.MutationOwnerID = ownerID
	retry.PDNSCandidatePath = filepath.Clean(pdnsSwitchCandidatePath(requestID))
	retry.PDNSBackupPath = filepath.Clean(pdnsSwitchBackupPath(requestID))
	if err := validateDNSEngineSwitchJournal(retry); err != nil {
		t.Fatalf("invalid retry journal fixture: %v", err)
	}
	state := dnsEngineStateReceipt{
		Schema:               dnsEngineStateSchema,
		Mode:                 retry.Mode,
		Engine:               retry.TargetEngine,
		EngineEpoch:          retry.TargetEpoch,
		Generation:           retry.TargetGeneration,
		PairRole:             retry.PairRole,
		PairLocalIP:          retry.LocalIP,
		PairPeerIP:           retry.PeerIP,
		PrimaryCatalogSerial: retry.PrimaryCatalogSerial,
		SourceRevision:       retry.SourceRevision,
		ManifestQualifier:    retry.ManifestQualifier,
		MutationRequestID:    retry.MutationRequestID,
		MutationOwnerID:      retry.MutationOwnerID,
	}
	if err := validateDNSEngineState(state); err != nil {
		t.Fatalf("invalid retry state fixture: %v", err)
	}
	if !exactDNSEngineStateForJournal(state, retry) {
		t.Fatal("retry state does not bind its committed journal")
	}
	return retry, state
}
