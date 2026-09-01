//go:build linux

package main

import (
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

// stageJournalFreeSwitchResidue builds the exact durable shape an interrupted
// PowerDNS-to-BIND switch leaves when it got as far as installing packages: a
// valid PowerDNS state, PowerDNS's own matching ownership receipt, an install
// receipt for BIND, and no switch journal. The caller decides whether BIND also
// holds an ownership receipt, which is the whole question.
// stageJournalFreeSwitchResidue, paket kurmaya kadar gelmiş, kesintiye uğramış
// bir PowerDNS-BIND geçişinin bıraktığı kalıcı biçimi birebir kurar: geçerli bir
// PowerDNS durumu, PowerDNS'in kendi eşleşen sahiplik makbuzu, BIND için bir
// kurulum makbuzu ve hiç geçiş günlüğü yok. Hedefin ayrıca sahiplik makbuzu
// taşıyıp taşımadığına çağıran karar verir; bütün soru budur.
func stageJournalFreeSwitchResidue(t *testing.T, targetAlsoOwns bool) {
	t.Helper()
	prepareDNSEngineOwnershipTest(t)

	active := legacyDurableDNSState(transport.DNSEnginePowerDNS)
	if err := writeDNSEngineState(active); err != nil {
		t.Fatalf("stage active state: %v", err)
	}
	if err := writeDNSEngineOwnership(active); err != nil {
		t.Fatalf("stage active ownership: %v", err)
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
		t.Fatalf("stage install receipt: %v", err)
	}
	if targetAlsoOwns {
		if err := writeDNSEngineOwnership(
			legacyDurableDNSState(transport.DNSEngineBIND),
		); err != nil {
			t.Fatalf("stage target ownership: %v", err)
		}
	}
}

const (
	residueRequestID = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	residueOwnerID   = "ffffffffffffffffffffffffffffffff"
)

var residueQualifier = "dns-engine-switch/v1:sha256:" + strings.Repeat("e", 64)

// The R-019 wedge. A switch that installed BIND packages and then failed before
// writing any journal left an install receipt behind. The provenance proof
// called that an error, startup recovery turned the error into a poisoned
// mutation manager, and because the poison is recomputed from durable state
// that nothing ever changes, every subsequent boot reproduced it. DNS kept
// serving and the panel kept answering, but the host could never accept another
// mutation.
//
// Authority never moved here: the active state is PowerDNS, PowerDNS owns it,
// and BIND owns nothing. That is provably "nothing happened", so recovery must
// be able to say so and fail the job cleanly.
//
// R-019 tıkanması. BIND paketlerini kurup sonra hiçbir günlük yazmadan düşen
// bir geçiş, geride bir kurulum makbuzu bıraktı. Köken kanıtı buna hata dedi,
// başlangıç kurtarması hatayı zehirlenmiş bir mutasyon yöneticisine çevirdi ve
// zehir hiç değişmeyen kalıcı durumdan yeniden hesaplandığı için sonraki her
// açılış onu yeniden üretti. DNS hizmet vermeye, panel cevap vermeye devam etti;
// ama sunucu bir daha hiçbir mutasyonu kabul edemedi.
//
// Burada yetki hiç el değiştirmedi: aktif durum PowerDNS, sahibi PowerDNS ve
// BIND hiçbir şeye sahip değil. Bu, kanıtlanabilir biçimde "hiçbir şey olmadı"
// demektir; kurtarma bunu söyleyebilmeli ve işi temizce düşürebilmelidir.
func TestJournalFreeInstallResidueIsRecoverableNotAmbiguous(t *testing.T) {
	stageJournalFreeSwitchResidue(t, false)

	finalized, err := exactFinalizedDNSEngineSwitchProvenanceOnHost(
		transport.DNSEngineBIND,
		residueQualifier,
		transport.ServiceMutationBinding{
			MutationRequestID: residueRequestID,
			MutationOwnerID:   residueOwnerID,
		},
	)
	if err != nil {
		t.Fatalf("install-only residue must not be an error: %v", err)
	}
	if finalized {
		t.Fatal("nothing was finalized; authority never moved")
	}
}

// The other half, and the reason the relaxation is narrow. A target that holds
// an ownership receipt while a different engine is active is a genuinely
// half-finished handover — the Boston shape — and must keep failing closed.
// Gevşetmenin neden dar olduğunu gösteren öbür yarı. Başka bir motor etkinken
// sahiplik makbuzu taşıyan bir hedef, gerçekten yarım kalmış bir devirdir —
// Boston biçimi — ve kapalı arıza vermeyi sürdürmelidir.
func TestJournalFreeTargetOwnershipStillFailsClosed(t *testing.T) {
	stageJournalFreeSwitchResidue(t, true)

	if _, err := exactFinalizedDNSEngineSwitchProvenanceOnHost(
		transport.DNSEngineBIND,
		residueQualifier,
		transport.ServiceMutationBinding{
			MutationRequestID: residueRequestID,
			MutationOwnerID:   residueOwnerID,
		},
	); err == nil {
		t.Fatal("a target that owns authority while another engine is active must fail closed")
	}
}

// The source side is still proven. If the active state and its own ownership
// receipt disagree, or the active ownership is missing, that remains an error
// no matter what the target holds — the relaxation must not become a way past
// a corrupted source.
// Kaynak taraf hâlâ kanıtlanır. Aktif durum ile kendi sahiplik makbuzu
// uyuşmuyorsa ya da aktif sahiplik eksikse, hedefte ne olursa olsun bu bir hata
// olarak kalır — gevşetme, bozuk bir kaynağı aşmanın yolu hâline gelmemelidir.
func TestInstallResidueDoesNotExcuseABrokenSource(t *testing.T) {
	t.Run("active ownership missing", func(t *testing.T) {
		prepareDNSEngineOwnershipTest(t)
		if err := writeDNSEngineState(
			legacyDurableDNSState(transport.DNSEnginePowerDNS),
		); err != nil {
			t.Fatal(err)
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
		if _, err := exactFinalizedDNSEngineSwitchProvenanceOnHost(
			transport.DNSEngineBIND, residueQualifier,
			transport.ServiceMutationBinding{
				MutationRequestID: residueRequestID,
				MutationOwnerID:   residueOwnerID,
			},
		); err == nil {
			t.Fatal("a state with no active ownership must fail closed")
		}
	})

	t.Run("active ownership disagrees with state", func(t *testing.T) {
		prepareDNSEngineOwnershipTest(t)
		active := legacyDurableDNSState(transport.DNSEnginePowerDNS)
		if err := writeDNSEngineState(active); err != nil {
			t.Fatal(err)
		}
		drifted := active
		drifted.EngineEpoch = active.EngineEpoch + 1
		if err := writeDNSEngineOwnership(drifted); err != nil {
			t.Fatal(err)
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
		if _, err := exactFinalizedDNSEngineSwitchProvenanceOnHost(
			transport.DNSEngineBIND, residueQualifier,
			transport.ServiceMutationBinding{
				MutationRequestID: residueRequestID,
				MutationOwnerID:   residueOwnerID,
			},
		); err == nil {
			t.Fatal("an active ownership that differs from the state must fail closed")
		}
	})
}
