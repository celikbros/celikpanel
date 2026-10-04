//go:build linux

package recoveryruntime

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/agentnativecontract"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

// A binary declaring only the previous separated-evidence policy must not be
// admitted against V3 just because the current recovery decoder understands it.
func TestDNSApplicationCompatibilityRequiresExplicitV3Support(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root descriptor fixture")
	}
	for _, scenario := range []string{"state", "ownership", "journal", "state-with-v1-journal"} {
		t.Run(scenario, func(t *testing.T) {
			root, err := os.MkdirTemp("/run", "celikpanel-dns-v3-compat-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(root)
			bin, private := filepath.Join(root, "bin"), filepath.Join(root, "private")
			for _, dir := range []string{bin, private} {
				if err := os.Mkdir(dir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			put := func(path string, raw []byte, mode os.FileMode) {
				t.Helper()
				if err := os.WriteFile(path, raw, mode); err != nil {
					t.Fatal(err)
				}
			}
			binary := []byte("prior separated-evidence Agent: must only be read")
			put(filepath.Join(bin, "agent"), binary, 0755)
			contract, err := agentnativecontract.New(binary, strings.Repeat("a", 40))
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := agentnativecontract.Encode(contract)
			if err != nil {
				t.Fatal(err)
			}
			put(filepath.Join(bin, agentnativecontract.FileName), encoded, 0644)
			statePath := filepath.Join(private, "dns-engine-state.json")
			policy := dnsengineartifact.JournalPolicy{StatePath: statePath, StateUID: 0, StateGID: 0, RequireOwner: true,
				PDNSMainPath: "/etc/powerdns/pdns.conf", PDNSManagedPath: "/etc/powerdns/pdns.d/celikpanel.conf",
				PDNSClusterPath: "/etc/powerdns/pdns.d/celikpanel-cluster.conf", PDNSDatabasePath: "/var/lib/powerdns/pdns.sqlite3"}
			original, err := os.ReadFile("../dnsengineartifact/testdata/switch-journal/alpha81-pdns-switch.json")
			if err != nil {
				t.Fatal(err)
			}
			original = bytes.ReplaceAll(original, []byte("/var/lib/celikpanel-agent-private/dns-engine-state.json"), []byte(statePath))
			base, err := policy.DecodeSwitchJournal(original)
			if err != nil {
				t.Fatal(err)
			}
			manifest, err := mutationpayload.CanonicalDNSEngineSwitchManifestWithPairIdentity(
				transport.DNSEngineSwitchModeSwitch, "", transport.DNSEnginePowerDNS, 0, 1, 0,
				transport.DNSTopologyPaired, transport.DNSPairRolePrimary, "192.0.2.10", "ns1.example.test", "192.0.2.11", "ns2.example.test", nil)
			if err != nil {
				t.Fatal(err)
			}
			state := dnsengineartifact.StateV1{Schema: dnsengineartifact.StateSchemaV1, Mode: manifest.Mode,
				Engine: manifest.TargetEngine, EngineEpoch: 1, PairRole: manifest.PairRole, PairLocalIP: manifest.LocalIP, PairPeerIP: manifest.PeerIP,
				PrimaryCatalogSerial: 1790542951, ManifestQualifier: manifest.Qualifier, MutationRequestID: base.MutationRequestID, MutationOwnerID: base.MutationOwnerID,
				NativeCatalogV3: dnsengineartifact.NativeCatalogDebian49V3}
			switch scenario {
			case "state", "state-with-v1-journal":
				raw, e := dnsengineartifact.CanonicalStateDocumentV3(state)
				if e != nil {
					t.Fatal(e)
				}
				put(statePath, raw, 0600)
				if scenario == "state-with-v1-journal" {
					put(filepath.Join(private, "dns-engine-switch-journal.json"), original, 0600)
				}
			case "ownership":
				raw, e := dnsengineartifact.CanonicalOwnershipDocumentV3(state)
				if e != nil {
					t.Fatal(e)
				}
				put(filepath.Join(private, "dns-engine-ownership-pdns.json"), raw, 0600)
			case "journal":
				base.SourceEngine = ""
				base.SourceEpoch = 0
				base.TargetEpoch = 1
				base.SourceRevision = 0
				base.Topology = manifest.Topology
				base.PairRole = manifest.PairRole
				base.LocalIP = manifest.LocalIP
				base.LocalNS = manifest.LocalNS
				base.PeerIP = manifest.PeerIP
				base.PeerNS = manifest.PeerNS
				base.ManifestQualifier = manifest.Qualifier
				base.SnapshotBytes = manifest.SnapshotBytes
				base.Zones = manifest.Zones
				base.PrimaryCatalogSerial = 1
				base.StateBefore = dnsengineartifact.FileSnapshot{Path: statePath}
				base.SourceUnitsBefore = nil
				base.TargetUnitsBefore = []dnsengineartifact.UnitSnapshot{{Name: "pdns.service", LoadState: "loaded", ActiveState: "inactive", UnitFileState: "disabled"}}
				after := append([]dnsengineartifact.FileSnapshot(nil), base.ConfigBefore...)
				for _, i := range []int{1, 2} {
					after[i] = dnsengineartifact.FileSnapshot{Path: base.ConfigBefore[i].Path, Exists: true, Mode: 0644, OwnerKnown: true, Data: []byte("managed=1\n"), SHA256: dnsengineartifact.DigestBytes([]byte("managed=1\n"))}
				}
				journal, e := policy.BuildPDNSFreshPrimaryJournalV3(base, after)
				if e != nil {
					t.Fatal(e)
				}
				raw, e := policy.EncodeSwitchJournal(journal)
				if e != nil {
					t.Fatal(e)
				}
				put(filepath.Join(private, "dns-engine-switch-journal.json"), raw, 0600)
			}
			before := map[string][]byte{}
			entries, e := os.ReadDir(private)
			if e != nil {
				t.Fatal(e)
			}
			for _, entry := range entries {
				p := filepath.Join(private, entry.Name())
				raw, e := os.ReadFile(p)
				if e != nil {
					t.Fatal(e)
				}
				before[p] = raw
			}
			if err := CheckDNSApplicationCompatibility(bin, private); !errors.Is(err, errDNSApplicationV3) {
				t.Fatalf("got %v, want explicit V3 incompatibility", err)
			}
			for p, want := range before {
				got, e := os.ReadFile(p)
				if e != nil || !bytes.Equal(got, want) {
					t.Fatalf("evidence changed at %s: %v", p, e)
				}
			}
		})
	}
}
