package dnsenginerecovery

import (
	"errors"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/transport"
)

// NativeInverseKind is the native compensation required by the frozen switch,
// not an authorization to perform it. In particular, an already-running owner
// BIND is restored by reload, never by the initial-install unit stop path.
type NativeInverseKind string

const (
	NativeInverseBINDRunningAdoption NativeInverseKind = "bind-running-adoption"
	NativeInverseBINDSwitch          NativeInverseKind = "bind-switch"
	NativeInversePDNSAdoption        NativeInverseKind = "pdns-adoption"
	NativeInversePDNSSwitch          NativeInverseKind = "pdns-switch"
)

// InactiveBINDSwitchInverseJournal is the journal-only part of the owner
// PowerDNS-to-BIND inverse admission (recover-dns-bind-switch).
// ValidateInactiveBINDSwitchInverseEvidence applies it before its secured
// observation checks. A caller holding only a journal may use it to name that
// owner command; it grants no recovery or native mutation authority.
func InactiveBINDSwitchInverseJournal(j dnsengineartifact.SwitchJournalV1) error {
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
			j.Phase != dnsengineartifact.SwitchPhaseRolledBack) {
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

// BINDSwitchNeverStartedTargetJournal reports whether the owner
// PowerDNS-to-BIND inverse journal (InactiveBINDSwitchInverseJournal) froze
// both BIND target units absent, so this operation created them. Only then
// may the inverse accept the pre-start target states, absent or the package
// guard's persistent mask, and only after proving them natively.
//
// A V2 journal does not record the phase that preceded its rollback decision:
// the Agent's startup Reconcile and its in-process rollback both overwrite the
// forward phase with rolling-back, and a startup decision is written for any
// pre-verified phase because the state receipt stays the source until
// target-verified. This predicate therefore does not prove the target never
// started. The inverse relies on native evidence instead: activation lifts the
// guard's persistent mask before it enables or starts named, and neither the
// forward path nor a rollback of this operation re-creates it for an absent
// preimage. A loaded target keeps the unchanged loaded-unit proof.
func BINDSwitchNeverStartedTargetJournal(j dnsengineartifact.SwitchJournalV1) bool {
	if InactiveBINDSwitchInverseJournal(j) != nil {
		return false
	}
	for _, unit := range j.TargetUnitsBefore {
		if unit.LoadState != "not-found" || unit.UnitFileState != "" || unit.ActiveState != "inactive" {
			return false
		}
	}
	return true
}

// PDNSAdoptionInverseJournal is the journal-only part of the owner PowerDNS
// adoption inverse admission (recover-dns-pdns-adoption). The secured evidence
// admission applies it before its observation checks. It names a command only;
// it grants no recovery or native mutation authority.
func PDNSAdoptionInverseJournal(j dnsengineartifact.SwitchJournalV1) error {
	if j.Mode != transport.DNSEngineSwitchModeAdopt ||
		j.SourceEngine != "" || j.TargetEngine != transport.DNSEnginePowerDNS ||
		j.StateBefore.Exists ||
		(j.Phase != dnsengineartifact.SwitchPhaseRollingBack &&
			j.Phase != dnsengineartifact.SwitchPhaseRolledBack) {
		return errors.New("PowerDNS adoption inverse lacks its exact durable rollback evidence")
	}
	return nil
}

// FreshPrimaryPrestartJournalV3 reports whether a V3 fresh paired PowerDNS
// primary journal records only pre-start work: an unsealed intent, or a sealed
// candidate with no durable native receipt at a pre-start or rollback phase.
// The owner command recover-dns-pdns-fresh-prestart requires this shape and
// then separately proves the native target stopped; target-enable-intent may
// already be serving, so this shape alone never authorizes the inverse.
func FreshPrimaryPrestartJournalV3(j dnsengineartifact.SwitchJournalV1) bool {
	if j.Schema != dnsengineartifact.SwitchJournalSchemaV3 || j.PDNSFreshPlan == nil {
		return false
	}
	if j.PDNSFreshPlan.Candidate == nil {
		switch j.Phase {
		case dnsengineartifact.SwitchPhaseIntent, dnsengineartifact.SwitchPhaseRollingBack,
			dnsengineartifact.SwitchPhaseRolledBack:
			return true
		}
		return false
	}
	if j.PDNSFreshPlan.Native != nil {
		return false
	}
	switch j.Phase {
	case dnsengineartifact.SwitchPhaseTargetStaged, dnsengineartifact.SwitchPhaseTargetEnableIntent,
		dnsengineartifact.SwitchPhaseRollingBack, dnsengineartifact.SwitchPhaseRollingBackTargetEnable,
		dnsengineartifact.SwitchPhaseRolledBack:
		return true
	}
	return false
}

// PlanNativeInverse selects the exact native inverse from a previously
// validated, accepted journal. It reconstructs the immutable manifest and
// fails closed if it disagrees. The caller must still hold the operation locks,
// exclude a live worker, prove owner/native state and retain all checkpoints.
func PlanNativeInverse(journal dnsengineartifact.SwitchJournalV1) (NativeInverseKind, error) {
	manifest, err := dnsengineartifact.SwitchJournalManifest(journal)
	if err != nil {
		return "", err
	}
	switch journal.TargetEngine {
	case transport.DNSEngineBIND:
		if journal.Mode != transport.DNSEngineSwitchModeSwitch &&
			journal.Mode != transport.DNSEngineSwitchModeReinstall {
			return "", errors.New("BIND journal mode has no native inverse")
		}
		running, err := dnsengineartifact.RunningBINDAdoptionJournal(manifest, journal)
		if err != nil {
			return "", err
		}
		if running {
			return NativeInverseBINDRunningAdoption, nil
		}
		return NativeInverseBINDSwitch, nil
	case transport.DNSEnginePowerDNS:
		switch journal.Mode {
		case transport.DNSEngineSwitchModeAdopt:
			return NativeInversePDNSAdoption, nil
		case transport.DNSEngineSwitchModeSwitch, transport.DNSEngineSwitchModeReinstall:
			return NativeInversePDNSSwitch, nil
		default:
			return "", errors.New("PowerDNS journal mode has no native inverse")
		}
	default:
		return "", errors.New("DNS engine rollback target is unsupported")
	}
}
