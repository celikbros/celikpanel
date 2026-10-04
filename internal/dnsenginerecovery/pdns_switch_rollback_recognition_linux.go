//go:build linux

package dnsenginerecovery

import (
	"errors"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/transport"
)

// RecognizeInactivePDNSSwitchRollbackEvidence identifies the retained,
// standalone BIND-to-PowerDNS rollback decision with an initially inactive
// PowerDNS target. It classifies a secured SwitchEvidence read only. A nil
// result is not inverse admission or permission to change the host: the v1
// journal has no frozen target config after-image, so an independent executor
// cannot distinguish its partial write from a later owner edit. Any future
// admission must require a distinct versioned after-plan, both locks, worker
// exclusion and native ownership proofs.
func RecognizeInactivePDNSSwitchRollbackEvidence(evidence SwitchEvidence) error {
	j, o := evidence.Journal, evidence.Observation
	if j.Mode != transport.DNSEngineSwitchModeSwitch ||
		j.SourceEngine != transport.DNSEngineBIND ||
		j.TargetEngine != transport.DNSEnginePowerDNS ||
		j.Topology != transport.DNSTopologyStandalone ||
		j.PairRole != "" || j.LocalIP != "" || j.PeerIP != "" ||
		!j.StateBefore.Exists ||
		(j.Phase != dnsengineartifact.SwitchPhaseRollingBack &&
			j.Phase != dnsengineartifact.SwitchPhaseRolledBack) ||
		o.InverseKind != NativeInversePDNSSwitch ||
		o.EvidenceSHA256 == "" ||
		o.RequestID != j.MutationRequestID ||
		o.Phase != j.Phase ||
		o.SourceEngine != string(j.SourceEngine) ||
		o.TargetEngine != string(j.TargetEngine) ||
		o.TargetEpoch != j.TargetEpoch ||
		o.SourceOwnership != SourceOwnershipExact ||
		(o.TargetReceipt != TargetReceiptExact && o.SourceReceipt != SourceReceiptExact) ||
		(o.TargetReceipt == TargetReceiptExact && o.SourceReceipt != SourceReceiptDifferent) ||
		(o.SourceReceipt == SourceReceiptExact && o.TargetReceipt == TargetReceiptExact) ||
		(!activeDNSInverseStatus(o.Status) && o.Status != EvidenceTerminalRolledBack) ||
		(o.Status == EvidenceTerminalRolledBack &&
			(j.Phase != dnsengineartifact.SwitchPhaseRolledBack || o.SourceReceipt != SourceReceiptExact)) {
		return errors.New("inactive PowerDNS switch rollback record lacks exact retained evidence")
	}
	if len(j.TargetUnitsBefore) != 1 ||
		j.TargetUnitsBefore[0].Name != "pdns.service" ||
		j.TargetUnitsBefore[0].ActiveState != "inactive" ||
		len(j.SourceUnitsBefore) != 2 ||
		j.SourceUnitsBefore[0].Name != "bind9.service" ||
		j.SourceUnitsBefore[1].Name != "named.service" ||
		(j.SourceUnitsBefore[0].ActiveState != "active" &&
			j.SourceUnitsBefore[1].ActiveState != "active") {
		return errors.New("inactive PowerDNS switch rollback record has unexpected unit preimages")
	}
	return nil
}
