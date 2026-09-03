//go:build linux

package main

import (
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

// stageReturnToFormerEngine builds the BIND -> PowerDNS -> BIND host at the
// pre-intent boundary: PowerDNS is the active state at epoch 2 and owns it,
// BIND's install receipt exists (the interrupted switch got that far), and
// BIND's ownership receipt from its own epoch is still on disk because nothing
// ever retires ownership receipts. targetEpoch selects which BIND receipt the
// host holds.
// stageReturnToFormerEngine, BIND -> PowerDNS -> BIND sunucusunu intent-öncesi
// sınırda kurar: PowerDNS epoch 2'de etkin durumdur ve ona sahiptir, BIND'ın
// kurulum makbuzu vardır (kesintili geçiş oraya kadar geldi) ve sahiplik
// makbuzlarını hiçbir şey emekliye ayırmadığı için BIND'ın kendi çağından
// sahiplik makbuzu hâlâ diskte durur. targetEpoch, sunucunun tuttuğu BIND
// makbuzunu seçer.
func stageReturnToFormerEngine(t *testing.T, targetEpoch int64) {
	t.Helper()
	prepareDNSEngineOwnershipTest(t)
	active := legacyDurableDNSState(transport.DNSEnginePowerDNS)
	active.EngineEpoch = 2
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
	former := legacyDurableDNSState(transport.DNSEngineBIND)
	former.EngineEpoch = targetEpoch
	if err := writeDNSEngineOwnership(former); err != nil {
		t.Fatalf("stage former BIND ownership: %v", err)
	}
}

func proveReturnToFormerEngine(t *testing.T) (bool, error) {
	t.Helper()
	return exactFinalizedDNSEngineSwitchProvenanceOnHost(
		transport.DNSEngineBIND,
		residueQualifier,
		transport.ServiceMutationBinding{
			MutationRequestID: residueRequestID,
			MutationOwnerID:   residueOwnerID,
		},
	)
}

// S-8 Boston setup, register R-032. A host that ran BIND at epoch 1, switched
// to PowerDNS at epoch 2 and then had a switch back to BIND killed after
// package install holds BIND's stranded epoch-1 ownership receipt beside the
// fresh install receipt. That is history, not a half-finished handover:
// authority provably sits with PowerDNS, and the stranded receipt is older
// than the active state by construction. Recovery must fail the job cleanly,
// exactly as it does when no former receipt exists.
//
// S-8 Boston kurulumu, defter R-032. Epoch 1'de BIND çalıştırmış, epoch 2'de
// PowerDNS'e geçmiş ve sonra BIND'a dönüşü paket kurulumundan sonra öldürülmüş
// bir sunucu, taze kurulum makbuzunun yanında BIND'ın terk edilmiş epoch-1
// sahiplik makbuzunu tutar. Bu tarihtir, yarım kalmış devir değil: yetki
// kanıtlanabilir biçimde PowerDNS'tedir ve terk edilmiş makbuz yapı gereği
// etkin durumdan eskidir. Kurtarma işi, eski makbuz hiç yokmuş gibi temizce
// düşürmelidir.
func TestReturnToAFormerEngineIsRecoverableResidue(t *testing.T) {
	stageReturnToFormerEngine(t, 1)
	finalized, err := proveReturnToFormerEngine(t)
	if err != nil {
		t.Fatalf("a former engine's older ownership receipt must not poison the return switch: %v", err)
	}
	if finalized {
		t.Fatal("nothing was finalized; authority never moved")
	}
}

// The Boston shape proper is unchanged: a target receipt at the active
// state's epoch is two states claiming one epoch, and a receipt from a newer
// epoch is a receipt from ahead of the committed state. Both stay closed.
// Asıl Boston biçimi değişmedi: etkin durumun çağındaki bir hedef makbuzu tek
// çağı iddia eden iki durumdur; daha yeni çağdan bir makbuz committed durumun
// ilerisinden gelen bir makbuzdur. İkisi de kapalı kalır.
func TestATargetReceiptAtOrAheadOfTheActiveEpochStillFailsClosed(t *testing.T) {
	for name, epoch := range map[string]int64{"equal epoch": 2, "newer epoch": 3} {
		t.Run(name, func(t *testing.T) {
			stageReturnToFormerEngine(t, epoch)
			if _, err := proveReturnToFormerEngine(t); err == nil {
				t.Fatalf("a target receipt at %s while another engine is active must fail closed", name)
			}
		})
	}
}
