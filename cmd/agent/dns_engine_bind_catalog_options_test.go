package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

func secondaryCatalogOptionsTestPair() *binddns.Pairing {
	return &binddns.Pairing{Role: binddns.PairRoleSecondary, LocalIP: "192.0.2.20", LocalNS: "ns2.example.test", PeerIP: "192.0.2.10", PeerNS: "ns1.example.test"}
}

func TestManagedBINDSecondaryCatalogInsideOwnedOptions(t *testing.T) {
	pairing := secondaryCatalogOptionsTestPair()
	base := "// operator prefix\noptions {\n\tdirectory \"/var/cache/bind\";\n\tversion \"operator\";\n};\n// operator suffix\n"
	configured, err := managedBINDOptions(base, pairing.PeerIP, pairing)
	if err != nil {
		t.Fatal(err)
	}
	open, close, err := bindOptionsBlock(configured)
	if err != nil {
		t.Fatal(err)
	}
	clause := strings.Index(configured, "catalog-zones {")
	if clause <= open || clause >= close || strings.Count(configured, "catalog-zones {") != 1 {
		t.Fatal("catalog subscription escaped the options context")
	}
	if !strings.Contains(configured, "version \"operator\";") || !strings.HasPrefix(configured, "// operator prefix") || !strings.HasSuffix(configured, "// operator suffix\n") {
		t.Fatal("operator configuration was changed")
	}
	again, err := managedBINDOptions(configured, pairing.PeerIP, pairing)
	if err != nil || again != configured {
		t.Fatalf("exact secondary config not idempotent: %v", err)
	}
	other := *pairing
	other.PeerIP = "192.0.2.11"
	if _, err := managedBINDOptions(configured, other.PeerIP, &other); err == nil {
		t.Fatal("different immutable catalog identity adopted")
	}
	if _, err := managedBINDOptions(configured, ""); err == nil {
		t.Fatal("secondary options silently stripped by a nonpaired caller")
	}
	if _, err := managedBINDOptions(base, "192.0.2.11", pairing); err == nil {
		t.Fatal("catalog and transfer identities disagreed")
	}
}

func TestManagedBINDCatalogRefusesForeignPolicyEvenDuringTakeover(t *testing.T) {
	pairing := secondaryCatalogOptionsTestPair()
	for _, managed := range []bool{false, true} {
		base := "options { version \"operator\"; };\n"
		if managed {
			var err error
			base, err = managedBINDOptions(base, pairing.PeerIP, pairing)
			if err != nil {
				t.Fatal(err)
			}
		}
		at := strings.LastIndex(base, "};")
		base = base[:at] + "catalog-zones { zone \"foreign.example.test\" in-memory yes; };\n" + base[at:]
		adopted, _, err := adoptForeignBINDOptions(base, "/etc/bind/named.conf.options", pairing.PeerIP)
		if err != nil {
			continue // A malformed takeover is safely refused without host writes.
		}
		if !strings.Contains(adopted, "foreign.example.test") {
			t.Fatal("takeover removed unreviewed foreign catalog")
		}
		if _, err := managedBINDOptions(adopted, pairing.PeerIP, pairing); err == nil {
			t.Fatal("foreign catalog accepted outside ownership markers")
		}
	}
}

func TestBINDSecondaryCatalogSnapshotVerificationAndJournalIdentity(t *testing.T) {
	pairing := secondaryCatalogOptionsTestPair()
	layout := bindHostLayout{OptionsConfig: "/etc/bind/named.conf.options", AnchorConfig: "/etc/bind/named.conf.local", GenerationRoot: aptBINDGenerationRoot}
	before := map[string][]byte{layout.OptionsConfig: []byte("options { directory \"/var/cache/bind\"; };\n"), layout.AnchorConfig: []byte("// retained operator anchor\n")}
	reader := func(values map[string][]byte) bindConfigSnapshotReader {
		return func(path string, mode os.FileMode, absent bool) (dnsFileSnapshot, error) {
			return dnsFileSnapshot{Path: path, Data: append([]byte(nil), values[path]...), Exists: true, Mode: uint32(mode.Perm())}, nil
		}
	}
	mutation, err := prepareBINDConfigMutationWithSnapshotReader(layout, pairing.PeerIP, bindOptionsExclusive, reader(before), pairing)
	if err != nil {
		t.Fatal(err)
	}
	receipt := binddns.Receipt{Pairing: &binddns.PairingReceipt{Role: pairing.Role, LocalIP: pairing.LocalIP, LocalNS: pairing.LocalNS, PeerIP: pairing.PeerIP, PeerNS: pairing.PeerNS, SecondaryConfigVersion: 1}}
	if err := verifyManagedBINDRuntimeConfigExactWithSnapshotReader(layout, receipt, false, reader(mutation.desired)); err != nil {
		t.Fatalf("receipt-derived exact config rejected: %v", err)
	}
	if err := verifyManagedBINDRuntimeConfigExactWithSnapshotReader(layout, receipt, false, reader(before)); err == nil {
		t.Fatal("missing catalog option counted as configured")
	}
	wrong := receipt
	altered := *receipt.Pairing
	altered.PeerIP = "192.0.2.11"
	wrong.Pairing = &altered
	if err := verifyManagedBINDRuntimeConfigExactWithSnapshotReader(layout, wrong, false, reader(mutation.desired)); err == nil {
		t.Fatal("runtime accepted another receipt identity")
	}
	manifest, err := mutationpayload.CanonicalDNSEngineSwitchManifestWithPairIdentity(transport.DNSEngineSwitchModeSwitch, "", transport.DNSEngineBIND, 0, 1, 0, transport.DNSTopologyPaired, pairing.Role, pairing.LocalIP, pairing.LocalNS, pairing.PeerIP, pairing.PeerNS, nil)
	if err != nil {
		t.Fatal(err)
	}
	journal := dnsEngineSwitchJournal{Mode: manifest.Mode, SourceEngine: manifest.SourceEngine, TargetEngine: manifest.TargetEngine, SourceEpoch: manifest.SourceEpoch, TargetEpoch: manifest.TargetEpoch, SourceRevision: manifest.SourceRevision, Topology: manifest.Topology, ManifestQualifier: manifest.Qualifier, SnapshotBytes: manifest.SnapshotBytes, Zones: manifest.Zones, PairRole: pairing.Role, LocalIP: pairing.LocalIP, LocalNS: pairing.LocalNS, PeerIP: pairing.PeerIP, PeerNS: pairing.PeerNS, ConfigBefore: mutation.originalSnapshots()}
	plan, err := bindSwitchTreePlanWithPrimaryCatalogSerial(manifest, switchJournalBinding(journal), 0)
	if err != nil {
		t.Fatal(err)
	}
	generation, err := binddns.RenderTree(layout.GenerationRoot, plan)
	if err != nil {
		t.Fatal(err)
	}
	journal.TargetGeneration = generation.ID

	recovered, err := bindConfigMutationFromJournal(layout, pairing.PeerIP, journal)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range mutation.paths {
		if !bytes.Equal(recovered.desired[path], mutation.desired[path]) || !bytes.Equal(recovered.original[path], before[path]) {
			t.Fatalf("rollback reconstruction lost reviewed identity or original bytes: %s", path)
		}
	}

	legacyID, err := binddns.LegacySecondaryGenerationID(layout.GenerationRoot, plan)
	if err != nil {
		t.Fatal(err)
	}
	journal.TargetGeneration = legacyID
	legacy, err := bindConfigMutationFromJournal(layout, pairing.PeerIP, journal)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(legacy.desired[layout.OptionsConfig]), "catalog-zones") {
		t.Fatal("historical rollback reconstructed a different new options preimage")
	}
	baseExpected, err := prepareBINDConfigMutationWithSnapshotReader(layout, pairing.PeerIP, bindOptionsExclusive, reader(before))
	if err != nil || !bytes.Equal(legacy.desired[layout.OptionsConfig], baseExpected.desired[layout.OptionsConfig]) {
		t.Fatal("historical rollback lost exact former desired options")
	}
	journal.TargetGeneration = "g-" + strings.Repeat("a", 64)
	if _, err := bindConfigMutationFromJournal(layout, pairing.PeerIP, journal); err == nil {
		t.Fatal("unrelated target generation selected a rollback policy")
	}
	oldReceipt := receipt
	oldPair := *receipt.Pairing
	oldPair.SecondaryConfigVersion = 0
	oldReceipt.Pairing = &oldPair
	if err := verifyManagedBINDRuntimeConfigExactWithSnapshotReader(layout, oldReceipt, false, reader(legacy.desired)); err == nil {
		t.Fatal("historical invalid subscription counted as current-ready")
	}
}
