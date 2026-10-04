package binddns

import (
	"bytes"
	"strings"
	"testing"
)

// pair4 P4-1: the owner's read-only inspector on a CelikPanel-managed BIND
// secondary transfers the catalog over loopback. The current secondary
// rendering (version 2) allows exactly that for the catalog zone only.
const wantLoopbackCatalogStanza = "zone \"catalog-c0000214.celikpanel.invalid\" {\n\ttype secondary;\n\tprimaries { 192.0.2.20; };\n\tallow-transfer { 192.0.2.20/32; 127.0.0.1; ::1; };\n};\n"

const wantPeerOnlyCatalogStanza = "zone \"catalog-c0000214.celikpanel.invalid\" {\n\ttype secondary;\n\tprimaries { 192.0.2.20; };\n};\n"

func TestSecondaryCatalogAllowsLoopbackTransferForCatalogOnly(t *testing.T) {
	generation, err := RenderManifest(pairingTestRoot, Manifest{EngineEpoch: 5, Pairing: testPairing(PairRoleSecondary)})
	if err != nil {
		t.Fatal(err)
	}
	config := string(generation.Config)
	if !strings.HasSuffix(config, wantLoopbackCatalogStanza) ||
		strings.Count(config, "allow-transfer") != 1 ||
		generation.ReceiptValue.Pairing.SecondaryConfigVersion != CurrentSecondaryConfigVersion ||
		CurrentSecondaryConfigVersion != SecondaryConfigVersionLoopbackCatalog {
		t.Fatalf("current secondary rendering lacks the loopback catalog transfer:\n%s\n%+v", config, generation.ReceiptValue.Pairing)
	}
	// The primary's rendering is unchanged by the secondary policy.
	primary, err := RenderManifest(pairingTestRoot, Manifest{EngineEpoch: 5, Pairing: testPairing(PairRolePrimary)})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(primary.Config), "127.0.0.1") || strings.Contains(string(primary.Config), "::1") ||
		primary.ReceiptValue.Pairing.SecondaryConfigVersion != 0 {
		t.Fatalf("primary rendering changed: %s", primary.Config)
	}
}

// The previous rendering (peer-only catalog, version 1) is the product's own
// earlier output: accepted as managed policy, reconstructed byte for byte for
// re-staging, and upgraded to version 2 by the next generation written.
func TestPreviousSecondaryRenderingIsAcceptedAndUpgradedAtNextWrite(t *testing.T) {
	pairing := testPairing(PairRoleSecondary)
	plan, err := NewTreePlan(Manifest{EngineEpoch: 5, Pairing: pairing})
	if err != nil {
		t.Fatal(err)
	}
	current, err := RenderTree(pairingTestRoot, plan)
	if err != nil {
		t.Fatal(err)
	}
	previous, err := RenderPreviousSecondaryTree(pairingTestRoot, plan)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(previous.Config), wantPeerOnlyCatalogStanza) ||
		strings.Contains(string(previous.Config), "allow-transfer") ||
		previous.ReceiptValue.Pairing.SecondaryConfigVersion != SecondaryConfigVersionPeerOnlyCatalog ||
		previous.ID == current.ID {
		t.Fatalf("previous rendering is not the exact earlier output:\n%s", previous.Config)
	}
	old := verifyPairingGeneration(t, previous)
	if err := VerifyCurrentConfig(pairingTestRoot, old); err != nil {
		t.Fatalf("earlier managed rendering refused as an owner change: %v", err)
	}
	again, err := RenderTree(pairingTestRoot, planFromVerifiedTree(old))
	if err != nil || again.ID != previous.ID || !bytes.Equal(again.Config, previous.Config) ||
		!bytes.Equal(again.Receipt, previous.Receipt) {
		t.Fatalf("earlier generation cannot be re-verified exactly: %v", err)
	}
	next, err := ReconfigurePairing(old, pairing)
	if err != nil {
		t.Fatal(err)
	}
	upgraded, err := RenderTree(pairingTestRoot, next)
	if err != nil {
		t.Fatal(err)
	}
	if upgraded.ID != current.ID || !bytes.Equal(upgraded.Config, current.Config) ||
		!strings.HasSuffix(string(upgraded.Config), wantLoopbackCatalogStanza) {
		t.Fatalf("next managed write did not render the current policy:\n%s", upgraded.Config)
	}
	if err := VerifyCurrentConfig(pairingTestRoot, verifyPairingGeneration(t, upgraded)); err != nil {
		t.Fatalf("new rendering refused: %v", err)
	}
	if _, err := RenderPreviousSecondaryTree(pairingTestRoot, planFromVerifiedTree(
		verifyPairingGeneration(t, mustRender(t, Manifest{EngineEpoch: 5, Pairing: testPairing(PairRolePrimary)})),
	)); err == nil {
		t.Fatal("previous secondary rendering accepted a primary plan")
	}
}

// A statement that is neither this product's current nor its earlier
// rendering stays an owner change, including a self-consistent receipt.
func TestOwnerEditedSecondaryTransferStatementStaysAnOwnerChange(t *testing.T) {
	plan, err := NewTreePlan(Manifest{EngineEpoch: 5, Pairing: testPairing(PairRoleSecondary)})
	if err != nil {
		t.Fatal(err)
	}
	current, err := RenderTree(pairingTestRoot, plan)
	if err != nil {
		t.Fatal(err)
	}
	previous, err := RenderPreviousSecondaryTree(pairingTestRoot, plan)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name      string
		base      Generation
		from, to  string
		wantMatch bool
	}{
		{"owner widened the earlier rendering", previous,
			"primaries { 192.0.2.20; };\n", "primaries { 192.0.2.20; };\n\tallow-transfer { any; };\n", true},
		{"owner added loopback in another form", previous,
			"primaries { 192.0.2.20; };\n", "primaries { 192.0.2.20; };\n\tallow-transfer { 127.0.0.1; 192.0.2.20; };\n", true},
		{"owner edited the current rendering", current,
			"127.0.0.1; ::1; };", "127.0.0.1; ::1; 198.51.100.7; };", true},
		{"owner removed the loopback allowance", current,
			"\tallow-transfer { 192.0.2.20/32; 127.0.0.1; ::1; };\n", "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if strings.Contains(string(test.base.Config), test.from) != test.wantMatch {
				t.Fatalf("fixture does not contain %q", test.from)
			}
			edited := []byte(strings.Replace(string(test.base.Config), test.from, test.to, 1))
			receipt := cloneReceipt(test.base.ReceiptValue)
			receipt.ConfigSHA256 = sha256Hex(edited)
			encoded, err := encodeReceipt(receipt)
			if err != nil {
				t.Fatal(err)
			}
			tree, err := VerifyTree(encoded, edited, map[string][]byte{})
			if err != nil {
				t.Fatal(err)
			}
			if err := VerifyCurrentConfig(pairingTestRoot, tree); err == nil {
				t.Fatal("owner-edited transfer statement accepted as managed policy")
			}
		})
	}
	unknown := cloneReceipt(current.ReceiptValue)
	unknown.Pairing.SecondaryConfigVersion = 3
	if err := validatePairingReceipt(pairingTestRoot, unknown.Pairing); err == nil {
		t.Fatal("unknown secondary rendering version accepted")
	}
	for version, accepted := range map[int]bool{0: false, 1: true, 2: true, 3: false} {
		if AcceptedSecondaryConfigVersion(version) != accepted {
			t.Fatalf("AcceptedSecondaryConfigVersion(%d) != %t", version, accepted)
		}
	}
}

func mustRender(t *testing.T, manifest Manifest) Generation {
	t.Helper()
	generation, err := RenderManifest(pairingTestRoot, manifest)
	if err != nil {
		t.Fatal(err)
	}
	return generation
}
