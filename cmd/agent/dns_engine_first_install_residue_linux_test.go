//go:build linux

package main

import (
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

// Register R-033, found live on a fresh Arch host. The very first DNS engine
// install failed after package install (the BIND directory rule refused
// /var/named), so the host held an install receipt and nothing else: no
// state, no ownership, nothing serving. The abort proof called that
// "inconsistent without active state", the ledger was poisoned, and the
// poison was recomputed from the same receipt on every boot. Nothing
// happened on that host; recovery must be able to say so.
//
// Defter R-033, taze bir Arch sunucusunda canlı bulundu. İlk DNS motoru
// kurulumu paket kurulumundan sonra düştü (BIND dizin kuralı /var/named'i
// reddetti); sunucuda bir kurulum makbuzu ve başka hiçbir şey kaldı: durum
// yok, sahiplik yok, hizmet veren yok. İptal kanıtı buna "etkin durum olmadan
// tutarsız" dedi, defter zehirlendi ve zehir her açılışta aynı makbuzdan
// yeniden hesaplandı. O sunucuda hiçbir şey olmadı; kurtarma bunu
// söyleyebilmelidir.
func TestFirstInstallResidueOnAFreshHostIsRecoverable(t *testing.T) {
	prepareDNSEngineOwnershipTest(t)
	if err := writeDNSEngineInstallOwnership(dnsEngineInstallOwnershipReceipt{
		Schema:            dnsEngineInstallOwnershipSchema,
		Engine:            transport.DNSEngineBIND,
		PackageManager:    "pacman",
		Packages:          []string{"bind"},
		MissingBefore:     []string{"bind"},
		ManifestQualifier: residueQualifier,
		MutationRequestID: residueRequestID,
		MutationOwnerID:   residueOwnerID,
	}); err != nil {
		t.Fatalf("stage install receipt: %v", err)
	}
	finalized, err := exactFinalizedDNSEngineSwitchProvenanceOnHost(
		transport.DNSEngineBIND, residueQualifier,
		transport.ServiceMutationBinding{
			MutationRequestID: residueRequestID, MutationOwnerID: residueOwnerID,
		},
	)
	if err != nil {
		t.Fatalf("an install receipt alone on a fresh host must be residue, not an error: %v", err)
	}
	if finalized {
		t.Fatal("nothing was finalized on a host with no state")
	}
}

// An ownership receipt without any active state is still a contradiction and
// still fails closed: ownership is written only by a committed publish.
// Etkin durumu olmayan sahiplik makbuzu yine çelişkidir ve kapalı kalır:
// sahiplik yalnız committed bir yayımla yazılır.
func TestOwnershipWithoutStateStillFailsClosed(t *testing.T) {
	prepareDNSEngineOwnershipTest(t)
	if err := writeDNSEngineOwnership(legacyDurableDNSState(transport.DNSEngineBIND)); err != nil {
		t.Fatalf("stage stray ownership: %v", err)
	}
	if _, err := exactFinalizedDNSEngineSwitchProvenanceOnHost(
		transport.DNSEngineBIND, residueQualifier,
		transport.ServiceMutationBinding{
			MutationRequestID: residueRequestID, MutationOwnerID: residueOwnerID,
		},
	); err == nil {
		t.Fatal("ownership without state must fail closed")
	}
}
