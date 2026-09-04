package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/hostplatform"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

func adoptionTestManifest(
	t *testing.T,
) mutationpayload.DNSEngineSwitchManifestCommitment {
	t.Helper()
	manifest, err := mutationpayload.CanonicalDNSEngineSwitchManifest(
		transport.DNSEngineSwitchModeSwitch,
		transport.DNSEngineBIND, transport.DNSEnginePowerDNS,
		1, 2, 3, transport.DNSTopologyStandalone, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func adoptionTestBinding() transport.ServiceMutationBinding {
	return transport.ServiceMutationBinding{
		MutationRequestID: strings.Repeat("a", 32),
		MutationOwnerID:   strings.Repeat("b", 32),
	}
}

// A switch that finds the target's packages already installed installs nothing,
// and until R-026 it therefore recorded nothing. The receipt it must write now
// is the same receipt an installation writes — same schema, same engine, same
// package set, same manifest and mutation identity — and differs in the one
// fact that is actually different: it had nothing to install.
//
// Hedefin paketlerini zaten kurulu bulan bir geçiş hiçbir şey kurmaz ve R-026'ya
// kadar bu yüzden hiçbir şey kaydetmiyordu. Artık yazması gereken makbuz,
// kurulumun yazdığı makbuzun aynısıdır — aynı şema, aynı motor, aynı paket
// kümesi, aynı manifest ve mutasyon kimliği — ve yalnızca gerçekten farklı olan
// tek olguda ayrılır: kuracak bir şeyi yoktu.
func TestAdoptedDNSEngineInstallOwnershipRecordsThisMutation(t *testing.T) {
	manifest := adoptionTestManifest(t)
	binding := adoptionTestBinding()
	packages := []string{"pdns-server", "pdns-backend-sqlite3"}

	adopted, err := newDNSEngineInstallOwnership(
		transport.DNSEnginePowerDNS, hostplatform.PackageManagerAPT,
		packages, nil, manifest, binding,
	)
	if err != nil {
		t.Fatalf("an adoption of already-present packages was refused: %v", err)
	}
	if !adopted.AdoptedPresent || len(adopted.MissingBefore) != 0 ||
		adopted.Schema != dnsEngineInstallOwnershipSchema ||
		adopted.Engine != transport.DNSEnginePowerDNS ||
		adopted.PackageManager != string(hostplatform.PackageManagerAPT) ||
		adopted.ManifestQualifier != manifest.Qualifier ||
		adopted.MutationRequestID != binding.MutationRequestID ||
		adopted.MutationOwnerID != binding.MutationOwnerID {
		t.Fatalf("adoption receipt is not this mutation's provenance: %+v", adopted)
	}
	if !exactDNSEngineInstallOwnership(
		adopted, true, transport.DNSEnginePowerDNS,
		hostplatform.PackageManagerAPT, packages,
	) {
		t.Fatalf("adoption receipt does not describe the host package set: %+v", adopted)
	}

	encoded, err := encodeDNSEngineInstallOwnership(adopted)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeDNSEngineInstallOwnership(encoded)
	if err != nil {
		t.Fatalf("adoption receipt does not survive its own encoding: %v", err)
	}
	if !decoded.AdoptedPresent || len(decoded.MissingBefore) != 0 {
		t.Fatalf("decoded adoption receipt lost its provenance kind: %+v", decoded)
	}
	if !bytes.Contains(encoded, []byte(`"missing_before":[]`)) ||
		!bytes.Contains(encoded, []byte(`"adopted_present":true`)) {
		t.Fatalf("adoption receipt is not self-describing on disk: %s", encoded)
	}
}

// Receipts already on disk were written before the adoption flag existed. The
// canonical-JSON rule compares a stored receipt against its own re-encoding, so
// a field that appeared in every receipt would make every one of them
// unreadable on the next boot — an install receipt must still encode to exactly
// the bytes it encoded to before.
//
// Diskte duran makbuzlar devralma bayrağı var olmadan önce yazıldı. Kanonik
// JSON kuralı saklanan makbuzu kendi yeniden kodlamasıyla karşılaştırır; her
// makbuzda beliren bir alan bu yüzden hepsini bir sonraki açılışta okunamaz
// kılardı — kurulum makbuzu hâlâ tam olarak eskiden kodlandığı baytlara
// kodlanmalıdır.
func TestInstalledDNSEngineOwnershipKeepsItsReleasedEncoding(t *testing.T) {
	manifest := adoptionTestManifest(t)
	binding := adoptionTestBinding()
	installed, err := newDNSEngineInstallOwnership(
		transport.DNSEnginePowerDNS, hostplatform.PackageManagerAPT,
		[]string{"pdns-server", "pdns-backend-sqlite3"},
		[]string{"pdns-server"}, manifest, binding,
	)
	if err != nil {
		t.Fatal(err)
	}
	if installed.AdoptedPresent {
		t.Fatalf("an installation was recorded as an adoption: %+v", installed)
	}
	encoded, err := encodeDNSEngineInstallOwnership(installed)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("adopted_present")) {
		t.Fatalf("install receipt gained a field released receipts do not have: %s", encoded)
	}
	released := `{"schema":"` + dnsEngineInstallOwnershipSchema +
		`","engine":"pdns","package_manager":"apt","packages":` +
		`["pdns-backend-sqlite3","pdns-server"],"missing_before":["pdns-server"],` +
		`"manifest_qualifier":"` + manifest.Qualifier +
		`","mutation_request_id":"` + binding.MutationRequestID +
		`","mutation_owner_id":"` + binding.MutationOwnerID + `"}` + "\n"
	if string(encoded) != released {
		t.Fatalf("install receipt encoding changed:\n got: %s\nwant: %s", encoded, released)
	}
	if _, err := decodeDNSEngineInstallOwnership([]byte(released)); err != nil {
		t.Fatalf("a receipt written by a released agent no longer decodes: %v", err)
	}
}

// The provenance kind and the missing set are one fact stated twice, so they
// can never be allowed to disagree: an adoption that installed something, or an
// installation that installed nothing, is a receipt this agent does not write
// and must not read back.
//
// Köken türü ile eksik kümesi iki kez söylenmiş tek bir olgudur; bu yüzden
// çelişmelerine asla izin verilemez: bir şey kurmuş bir devralma ya da hiçbir
// şey kurmamış bir kurulum, bu ajanın yazmadığı ve geri okumaması gereken bir
// makbuzdur.
func TestDNSEngineInstallOwnershipKindAndMissingSetCannotDisagree(t *testing.T) {
	manifest := adoptionTestManifest(t)
	binding := adoptionTestBinding()
	base, err := newDNSEngineInstallOwnership(
		transport.DNSEnginePowerDNS, hostplatform.PackageManagerAPT,
		[]string{"pdns-server", "pdns-backend-sqlite3"},
		[]string{"pdns-server"}, manifest, binding,
	)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*dnsEngineInstallOwnershipReceipt)
	}{
		{
			name: "an adoption that names installed packages",
			mutate: func(receipt *dnsEngineInstallOwnershipReceipt) {
				receipt.AdoptedPresent = true
			},
		},
		{
			name: "an installation that installed nothing",
			mutate: func(receipt *dnsEngineInstallOwnershipReceipt) {
				receipt.MissingBefore = []string{}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			receipt := base
			receipt.Packages = append([]string(nil), base.Packages...)
			receipt.MissingBefore = append([]string(nil), base.MissingBefore...)
			test.mutate(&receipt)
			if err := validateDNSEngineInstallOwnership(receipt); err == nil {
				t.Fatalf("contradictory install ownership was accepted: %+v", receipt)
			}
			if _, err := encodeDNSEngineInstallOwnership(receipt); err == nil {
				t.Fatal("contradictory install ownership was encoded")
			}
		})
	}
}
