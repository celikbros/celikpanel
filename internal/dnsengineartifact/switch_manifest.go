package dnsengineartifact

import (
	"errors"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
)

// SwitchJournalManifest reconstructs the exact immutable request committed by
// a switch journal. Recovery readers must not substitute a later editable plan.
func SwitchJournalManifest(journal SwitchJournalV1) (mutationpayload.DNSEngineSwitchManifestCommitment, error) {
	manifest, err := mutationpayload.CanonicalDNSEngineSwitchManifestWithPairIdentity(
		journal.Mode,
		journal.SourceEngine, journal.TargetEngine,
		journal.SourceEpoch, journal.TargetEpoch, journal.SourceRevision,
		journal.Topology, journal.PairRole, journal.LocalIP, journal.LocalNS,
		journal.PeerIP, journal.PeerNS, journal.Zones,
	)
	if err != nil || manifest.Qualifier != journal.ManifestQualifier ||
		manifest.SnapshotBytes != journal.SnapshotBytes {
		return mutationpayload.DNSEngineSwitchManifestCommitment{},
			errors.New("DNS engine switch journal does not reconstruct its manifest")
	}
	return manifest, nil
}
