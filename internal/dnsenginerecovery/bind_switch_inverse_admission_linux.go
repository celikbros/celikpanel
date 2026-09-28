//go:build linux

package dnsenginerecovery

import (
	"errors"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/transport"
)

// ValidateInactiveBINDSwitchInverseEvidence is the first admission boundary for
// the managed PowerDNS-to-BIND inverse. It consumes a secured SwitchEvidence
// read, not caller-supplied journal bytes. Its success does not authorize a
// native effect: the caller must also hold both installed locks, exclude the
// accepted worker and prove the current generation/config and native units.
// A previously running owner BIND has a different inverse and is rejected.
func ValidateInactiveBINDSwitchInverseEvidence(evidence SwitchEvidence) error {
	j, o := evidence.Journal, evidence.Observation
	if j.Schema != dnsengineartifact.SwitchJournalSchemaV2 || j.InversePlan == nil ||
		j.InversePlan.Kind != dnsengineartifact.BINDSwitchInversePlanKindV2 ||
		j.InversePlan.SourcePDNS == nil ||
		len(j.InversePlan.BINDUnchangedConfig) != 2 ||
		j.Mode != transport.DNSEngineSwitchModeSwitch ||
		j.SourceEngine != transport.DNSEnginePowerDNS ||
		j.TargetEngine != transport.DNSEngineBIND ||
		j.Topology != transport.DNSTopologyStandalone ||
		j.PairRole != "" || j.LocalIP != "" || j.PeerIP != "" ||
		!j.StateBefore.Exists ||
		(j.Phase != dnsengineartifact.SwitchPhaseRollingBack &&
			j.Phase != dnsengineartifact.SwitchPhaseRolledBack) ||
		o.InverseKind != NativeInverseBINDSwitch ||
		o.EvidenceSHA256 == "" ||
		o.RequestID != j.MutationRequestID ||
		o.Phase != j.Phase ||
		o.SourceEngine != string(j.SourceEngine) ||
		o.TargetEngine != string(j.TargetEngine) ||
		o.TargetGeneration != j.TargetGeneration ||
		o.TargetEpoch != j.TargetEpoch ||
		o.SourceOwnership != SourceOwnershipExact ||
		(o.TargetReceipt != TargetReceiptExact && o.SourceReceipt != SourceReceiptExact) ||
		(o.TargetReceipt == TargetReceiptExact && o.SourceReceipt != SourceReceiptDifferent) ||
		(o.SourceReceipt == SourceReceiptExact && o.TargetReceipt == TargetReceiptExact) ||
		(!activeDNSInverseStatus(o.Status) && o.Status != EvidenceTerminalRolledBack) ||
		(o.Status == EvidenceTerminalRolledBack &&
			(j.Phase != dnsengineartifact.SwitchPhaseRolledBack || o.SourceReceipt != SourceReceiptExact)) {
		return errors.New("inactive BIND switch inverse lacks exact retained rollback evidence")
	}
	if len(j.TargetUnitsBefore) != 2 || len(j.SourceUnitsBefore) != 1 ||
		j.SourceUnitsBefore[0].Name != "pdns.service" ||
		j.SourceUnitsBefore[0].ActiveState != "active" {
		return errors.New("inactive BIND switch inverse has unexpected unit preimages")
	}
	seen := map[string]bool{}
	for _, unit := range j.TargetUnitsBefore {
		if unit.Name != "named.service" && unit.Name != "bind9.service" {
			return errors.New("inactive BIND switch inverse has an unexpected target unit")
		}
		if seen[unit.Name] || unit.ActiveState != "inactive" {
			return errors.New("inactive BIND switch inverse has an active or duplicate target preimage")
		}
		seen[unit.Name] = true
	}
	if !seen["named.service"] || !seen["bind9.service"] {
		return errors.New("inactive BIND switch inverse lacks both target unit preimages")
	}
	return nil
}
