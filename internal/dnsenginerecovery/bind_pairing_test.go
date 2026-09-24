package dnsenginerecovery

import (
	"testing"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

func TestExactBINDPairingForSwitchJournalBindsDirectionAndSerial(t *testing.T) {
	standalone := mutationpayload.DNSEngineSwitchManifestCommitment{Topology: transport.DNSTopologyStandalone, TargetEngine: transport.DNSEngineBIND}
	journal := dnsengineartifact.SwitchJournalV1{TargetEngine: transport.DNSEngineBIND, Topology: transport.DNSTopologyStandalone}
	unexpectedPeer := standalone
	unexpectedPeer.PeerIP = "2.25.80.4"
	if ExactBINDPairingForSwitchJournal(binddns.Receipt{}, unexpectedPeer, journal) {
		t.Fatal("standalone manifest with peer accepted")
	}
	wrongEngine := standalone
	wrongEngine.TargetEngine = transport.DNSEnginePowerDNS
	if ExactBINDPairingForSwitchJournal(binddns.Receipt{}, wrongEngine, journal) {
		t.Fatal("non-BIND target accepted")
	}
	invalid := standalone
	invalid.Topology = "unknown"
	if ExactBINDPairingForSwitchJournal(binddns.Receipt{}, invalid, journal) {
		t.Fatal("unknown topology accepted")
	}
	if !ExactBINDPairingForSwitchJournal(binddns.Receipt{}, standalone, journal) {
		t.Fatal("standalone without pairing rejected")
	}
	if ExactBINDPairingForSwitchJournal(binddns.Receipt{Pairing: &binddns.PairingReceipt{}}, standalone, journal) {
		t.Fatal("standalone accepted a paired native receipt")
	}
	journal.PeerIP = "2.25.80.4"
	if ExactBINDPairingForSwitchJournal(binddns.Receipt{}, standalone, journal) {
		t.Fatal("standalone accepted a paired journal")
	}

	manifest := mutationpayload.DNSEngineSwitchManifestCommitment{
		Topology: transport.DNSTopologyPaired, TargetEngine: transport.DNSEngineBIND,
		PairRole: binddns.PairRolePrimary,
		LocalIP:  "72.62.38.15", LocalNS: "ns1.celikhost.com",
		PeerIP: "2.25.80.4", PeerNS: "ns2.celikhost.com",
	}
	journal = dnsengineartifact.SwitchJournalV1{
		TargetEngine: transport.DNSEngineBIND, Topology: transport.DNSTopologyPaired,
		PairRole: manifest.PairRole, LocalIP: manifest.LocalIP, LocalNS: manifest.LocalNS,
		PeerIP: manifest.PeerIP, PeerNS: manifest.PeerNS, PrimaryCatalogSerial: 2,
	}
	receipt := binddns.Receipt{Pairing: &binddns.PairingReceipt{
		Role: manifest.PairRole, LocalIP: manifest.LocalIP, LocalNS: manifest.LocalNS,
		PeerIP: manifest.PeerIP, PeerNS: manifest.PeerNS, CatalogSerial: 2,
	}}
	if !ExactBINDPairingForSwitchJournal(receipt, manifest, journal) {
		t.Fatal("exact primary pairing rejected")
	}
	changed := receipt
	changedPair := *receipt.Pairing
	changedPair.PeerIP = "192.0.2.10"
	changed.Pairing = &changedPair
	if ExactBINDPairingForSwitchJournal(changed, manifest, journal) {
		t.Fatal("changed native peer accepted")
	}
	changedPair = *receipt.Pairing
	changedPair.CatalogSerial = 3
	changed.Pairing = &changedPair
	if ExactBINDPairingForSwitchJournal(changed, manifest, journal) {
		t.Fatal("changed primary catalog serial accepted")
	}
	journal.PeerNS = "foreign.example"
	if ExactBINDPairingForSwitchJournal(receipt, manifest, journal) {
		t.Fatal("changed journal peer accepted")
	}

	manifest.PairRole = binddns.PairRoleSecondary
	journal.PairRole, journal.PeerNS, journal.PrimaryCatalogSerial = manifest.PairRole, manifest.PeerNS, 0
	receipt.Pairing.Role, receipt.Pairing.CatalogSerial = manifest.PairRole, 1
	if !ExactBINDPairingForSwitchJournal(receipt, manifest, journal) {
		t.Fatal("exact secondary pairing rejected")
	}
	receipt.Pairing.CatalogSerial = 2
	if ExactBINDPairingForSwitchJournal(receipt, manifest, journal) {
		t.Fatal("secondary with local catalog serial accepted")
	}
}
