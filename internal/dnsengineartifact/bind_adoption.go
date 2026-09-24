package dnsengineartifact

import (
	"fmt"
	"strings"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

// AdoptableRunningBINDManifest is the exact initial standalone switch shape
// reused by the historical unmanaged-BIND takeover. It does not prove that
// BIND is currently running or grant any recovery authority.
func AdoptableRunningBINDManifest(manifest mutationpayload.DNSEngineSwitchManifestCommitment, stateExists bool) bool {
	return !stateExists &&
		manifest.Mode == transport.DNSEngineSwitchModeSwitch &&
		manifest.TargetEngine == transport.DNSEngineBIND &&
		manifest.SourceEngine == "" &&
		manifest.SourceEpoch == 0 && manifest.TargetEpoch == 1 &&
		manifest.Topology == transport.DNSTopologyStandalone &&
		manifest.PairRole == "" && manifest.LocalIP == "" &&
		manifest.LocalNS == "" && manifest.PeerIP == "" && manifest.PeerNS == ""
}

// RunningBINDAdoptionJournal classifies the frozen target-unit preimage. An
// already-answering owner BIND must be restored without stopping its unit.
// Unknown or contradictory preimages fail closed. Callers first decode and
// validate the journal under the accepted operation's host lock.
func RunningBINDAdoptionJournal(manifest mutationpayload.DNSEngineSwitchManifestCommitment, journal SwitchJournalV1) (bool, error) {
	if journal.TargetEngine != transport.DNSEngineBIND || !AdoptableRunningBINDManifest(manifest, false) {
		return false, nil
	}
	units := make(map[string]UnitSnapshot, len(journal.TargetUnitsBefore))
	names := make([]string, 0, len(journal.TargetUnitsBefore))
	for _, snapshot := range journal.TargetUnitsBefore {
		if _, duplicate := units[snapshot.Name]; duplicate {
			return false, fmt.Errorf("BIND takeover journal cannot be classified: its target unit preimage names %s twice", snapshot.Name)
		}
		units[snapshot.Name] = snapshot
		names = append(names, snapshot.Name)
	}
	named, hasNamed := units["named.service"]
	alias, hasAlias := units["bind9.service"]
	if !hasNamed || !hasAlias || len(units) != 2 {
		found := "nothing"
		if len(names) != 0 {
			found = strings.Join(names, ", ")
		}
		return false, fmt.Errorf("BIND takeover journal cannot be classified: its target unit preimage names %s, not bind9.service and named.service", found)
	}
	namedActive := named.ActiveState == "active"
	aliasActive := alias.ActiveState == "active"
	if !namedActive && !aliasActive {
		return false, nil
	}
	if namedActive != aliasActive {
		quiet := named
		if namedActive {
			quiet = alias
		}
		if quiet.LoadState != "not-found" {
			return false, fmt.Errorf("BIND takeover journal cannot be classified: named.service was %q and bind9.service was %q before the mutation, and %s is loaded on this host", named.ActiveState, alias.ActiveState, quiet.Name)
		}
	}
	return true, nil
}
