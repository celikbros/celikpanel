package binddns

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecondaryCatalogConfigurationUsesExplicitZoneAndOptionsScope(t *testing.T) {
	pairing := *testPairing(PairRoleSecondary)
	generation, err := RenderManifest(pairingTestRoot, Manifest{EngineEpoch: 5, Pairing: &pairing})
	if err != nil {
		t.Fatal(err)
	}
	options, err := SecondaryCatalogOptions(pairing)
	if err != nil {
		t.Fatal(err)
	}
	const want = "catalog-zones {\n\tzone \"catalog-c0000214.celikpanel.invalid\" default-primaries { 192.0.2.20; } in-memory yes;\n};\n"
	if options != want || strings.Contains(string(generation.Config), "catalog-zones") ||
		strings.Contains(string(generation.Config), "file ") ||
		generation.ReceiptValue.Pairing.SecondaryConfigVersion != 1 {
		t.Fatalf("invalid subscription split: options=%q config=%q receipt=%+v", options, generation.Config, generation.ReceiptValue.Pairing)
	}
	for _, change := range []func(*Pairing){
		func(p *Pairing) { p.Role = PairRolePrimary },
		func(p *Pairing) { p.PeerIP = "192.0.2.20; }; recursion yes;" },
		func(p *Pairing) { p.LocalIP = p.PeerIP },
		func(p *Pairing) { p.PeerNS = "NS2.example.test" },
	} {
		invalid := pairing
		change(&invalid)
		if _, err := SecondaryCatalogOptions(invalid); err == nil {
			t.Fatalf("accepted invalid pairing: %+v", invalid)
		}
	}
}

func TestSecondaryCatalogRendererDoesNotReuseLegacyGeneration(t *testing.T) {
	generation, err := RenderManifest(pairingTestRoot, Manifest{EngineEpoch: 5, Pairing: testPairing(PairRoleSecondary)})
	if err != nil {
		t.Fatal(err)
	}
	// Reconstruct the exact historical receipt and invalid top-level bytes.
	// It remains valid archival evidence, not a current runnable policy.
	legacy := cloneReceipt(generation.ReceiptValue)
	legacy.Pairing.SecondaryConfigVersion = 0
	legacyID, err := manifestGenerationID(legacy.EngineEpoch, pairingTestRoot, legacy.Pairing, legacy.Zones)
	if err != nil {
		t.Fatal(err)
	}
	legacy.Generation, legacy.ManifestSHA256 = legacyID, legacyID
	legacyConfig := []byte(fmt.Sprintf("// Managed by CelikPanel. DO NOT EDIT.\n// Immutable generation: %s; engine epoch: 5\ncatalog-zones {\n\tzone \"catalog-c0000214.celikpanel.invalid\" {\n\t\tdefault-primaries { 192.0.2.20; };\n\t\tin-memory yes;\n\t};\n};\n", legacyID))
	legacy.ConfigSHA256 = sha256Hex(legacyConfig)
	legacyBytes, err := encodeReceipt(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(legacyBytes), "secondary_config_version") {
		t.Fatal("historical canonical receipt bytes changed")
	}
	old, err := VerifyTree(legacyBytes, legacyConfig, nil)
	if err != nil {
		t.Fatalf("historical evidence must remain readable: %v", err)
	}
	if err := VerifyCurrentConfig(pairingTestRoot, old); err == nil {
		t.Fatal("legacy invalid subscription accepted as current managed policy")
	}
	plan, err := ReconfigurePairing(old, testPairing(PairRoleSecondary))
	if err != nil {
		t.Fatal(err)
	}
	oldID, err := LegacySecondaryGenerationID(pairingTestRoot, plan)
	if err != nil || oldID != legacyID {
		t.Fatalf("rollback identity does not reconstruct exact legacy generation: %s %v", oldID, err)
	}
	corrected, err := RenderTree(pairingTestRoot, plan)
	if err != nil {
		t.Fatal(err)
	}
	if corrected.ID == legacyID || corrected.ID != generation.ID ||
		corrected.ReceiptValue.ConfigSHA256 == legacy.ConfigSHA256 {
		t.Fatal("same-pair corrected renderer reused or relabeled the immutable legacy generation")
	}
	if err := VerifyCurrentConfig(pairingTestRoot, verifyPairingGeneration(t, corrected)); err != nil {
		t.Fatal(err)
	}
	invalid := cloneReceipt(corrected.ReceiptValue)
	invalid.Pairing.SecondaryConfigVersion = 2
	if err := validatePairingReceipt(pairingTestRoot, invalid.Pairing); err == nil {
		t.Fatal("unknown secondary rendering version accepted")
	}
	invalid.Pairing.Role = PairRolePrimary
	if err := validatePairingReceipt(pairingTestRoot, invalid.Pairing); err == nil {
		t.Fatal("secondary rendering version accepted for primary")
	}
}

func TestSecondaryCatalogNamedCheckconf(t *testing.T) {
	checkconf, err := exec.LookPath("named-checkconf")
	if err != nil {
		t.Skip("real BIND parser is not installed")
	}
	pairing := *testPairing(PairRoleSecondary)
	generation, err := RenderManifest(pairingTestRoot, Manifest{EngineEpoch: 5, Pairing: &pairing})
	if err != nil {
		t.Fatal(err)
	}
	options, err := SecondaryCatalogOptions(pairing)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	for _, test := range []struct {
		name, config string
		valid        bool
	}{
		{"zone-only-stage", string(generation.Config), true},
		{"complete-host", "options { recursion no; " + options + "};\n" + string(generation.Config), true},
		{"wrong-top-level-scope", options + string(generation.Config), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			name := filepath.Join(directory, test.name+".conf")
			if err := os.WriteFile(name, []byte(test.config), 0600); err != nil {
				t.Fatal(err)
			}
			output, err := exec.Command(checkconf, name).CombinedOutput()
			if (err == nil) != test.valid {
				t.Fatalf("named-checkconf valid=%t: %v: %s", test.valid, err, output)
			}
		})
	}
}
