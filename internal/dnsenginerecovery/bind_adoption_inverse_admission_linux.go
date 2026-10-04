//go:build linux

package dnsenginerecovery

import (
	"errors"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
)

// ValidateRunningBINDAdoptionInverseEvidence admits only the exact, secured
// no-stop rollback evidence. It grants no standalone native mutation authority.
func ValidateRunningBINDAdoptionInverseEvidence(e SwitchEvidence) error {
	j, o := e.Journal, e.Observation
	kind, err := PlanNativeInverse(j)
	if err != nil || kind != NativeInverseBINDRunningAdoption ||
		j.Schema != dnsengineartifact.SwitchJournalSchemaV2 || j.InversePlan == nil ||
		j.InversePlan.SourceBIND == nil || j.InversePlan.SourcePDNS != nil ||
		j.InversePlan.HostLayout != "apt" || len(j.InversePlan.BINDUnchangedConfig) != 2 ||
		j.StateBefore.Exists || j.HadPrevious || len(j.SourceUnitsBefore) != 0 ||
		(j.Phase != dnsengineartifact.SwitchPhaseRollingBack && j.Phase != dnsengineartifact.SwitchPhaseRolledBack) ||
		o.InverseKind != kind || o.EvidenceSHA256 == "" || o.RequestID != j.MutationRequestID ||
		o.Phase != j.Phase || o.SourceEngine != "" || o.TargetEngine != "bind" ||
		o.TargetGeneration != j.TargetGeneration || o.TargetEpoch != j.TargetEpoch ||
		o.SourceOwnership != SourceOwnershipNotApplicable ||
		(o.TargetReceipt != TargetReceiptExact && o.TargetReceipt != TargetReceiptAbsent) ||
		(o.TargetReceipt == TargetReceiptExact && o.SourceReceipt != SourceReceiptDifferent) ||
		(o.TargetReceipt == TargetReceiptAbsent && o.SourceReceipt != SourceReceiptMutualAbsence) ||
		(!activeDNSInverseStatus(o.Status) && o.Status != EvidenceTerminalRolledBack && !AgentReleasedDNSInverseEvidence(e)) ||
		(j.Phase == dnsengineartifact.SwitchPhaseRolledBack && o.SourceReceipt != SourceReceiptMutualAbsence) ||
		(o.Status == EvidenceTerminalRolledBack && j.Phase != dnsengineartifact.SwitchPhaseRolledBack) {
		return errors.New("running BIND adoption inverse lacks exact retained rollback evidence")
	}
	for _, u := range j.TargetUnitsBefore {
		if u.Name == "bind9.service" && u.LoadState == "not-found" && u.ActiveState == "inactive" && u.UnitFileState == "" {
			continue
		}
		if u.LoadState != "loaded" || u.ActiveState != "active" || u.UnitFileState != "enabled" {
			return errors.New("running BIND adoption inverse requires unchanged enabled owner units")
		}
	}
	return nil
}
