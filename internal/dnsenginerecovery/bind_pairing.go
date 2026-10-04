package dnsenginerecovery

import (
	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

// ExactBINDPairingForSwitchJournal compares a verified native BIND generation's
// directional pairing with the frozen operation manifest and journal. A match
// is only one target check; the caller must also verify generation content,
// native config, DNS answers and operation authority under the host lock.
func ExactBINDPairingForSwitchJournal(
	receipt binddns.Receipt,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	journal dnsengineartifact.SwitchJournalV1,
) bool {
	if manifest.TargetEngine != transport.DNSEngineBIND || journal.TargetEngine != transport.DNSEngineBIND {
		return false
	}
	if journal.Topology != manifest.Topology ||
		(manifest.Topology != transport.DNSTopologyPaired && manifest.Topology != transport.DNSTopologyStandalone) {
		return false
	}
	if manifest.Topology != transport.DNSTopologyPaired {
		return receipt.Pairing == nil && manifest.PairRole == "" &&
			manifest.LocalIP == "" && manifest.LocalNS == "" &&
			manifest.PeerIP == "" && manifest.PeerNS == "" &&
			journal.PairRole == "" &&
			journal.LocalIP == "" && journal.LocalNS == "" &&
			journal.PeerIP == "" && journal.PeerNS == "" &&
			journal.PrimaryCatalogSerial == 0
	}
	pairing := receipt.Pairing
	if pairing == nil || pairing.Role != manifest.PairRole ||
		pairing.LocalIP != manifest.LocalIP || pairing.LocalNS != manifest.LocalNS ||
		pairing.PeerIP != manifest.PeerIP || pairing.PeerNS != manifest.PeerNS ||
		journal.PairRole != manifest.PairRole ||
		journal.LocalIP != manifest.LocalIP || journal.LocalNS != manifest.LocalNS ||
		journal.PeerIP != manifest.PeerIP || journal.PeerNS != manifest.PeerNS {
		return false
	}
	if pairing.Role == binddns.PairRolePrimary {
		return journal.PrimaryCatalogSerial > 0 &&
			pairing.CatalogSerial == journal.PrimaryCatalogSerial
	}
	return pairing.Role == binddns.PairRoleSecondary &&
		journal.PrimaryCatalogSerial == 0 && pairing.CatalogSerial == 1
}
