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
	if j.Phase != dnsengineartifact.SwitchPhaseRollingBack && j.Phase != dnsengineartifact.SwitchPhaseRolledBack {
		return errors.New("inactive BIND switch inverse lacks exact retained rollback evidence")
	}
	return inactiveBINDSwitchJournalShape(j)
}

// inactiveBINDSwitchJournalShape is InactiveBINDSwitchInverseJournal without
// its phase condition: the managed standalone PowerDNS-to-BIND V2 switch with
// an active PowerDNS source and two inactive BIND target preimages.
func inactiveBINDSwitchJournalShape(j dnsengineartifact.SwitchJournalV1) error {
	if j.Schema != dnsengineartifact.SwitchJournalSchemaV2 || j.InversePlan == nil ||
		j.InversePlan.Kind != dnsengineartifact.BINDSwitchInversePlanKindV2 ||
		j.InversePlan.SourcePDNS == nil ||
		len(j.InversePlan.BINDUnchangedConfig) != 2 ||
		j.Mode != transport.DNSEngineSwitchModeSwitch ||
		j.SourceEngine != transport.DNSEnginePowerDNS ||
		j.TargetEngine != transport.DNSEngineBIND ||
		j.Topology != transport.DNSTopologyStandalone ||
		j.PairRole != "" || j.LocalIP != "" || j.PeerIP != "" ||
		!j.StateBefore.Exists {
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
// both BIND target units in a standby state no earlier switch left serving:
// both absent (this operation created BIND), or both under the package
// guard's persistent mask, inactive (the rollback standby an earlier rollback
// of such a switch leaves; a retry freezes it). Only then may the inverse
// accept the pre-start target states, absent or the guard's persistent mask,
// and only after proving them natively; and only then does the rollback end
// with the target under that mask.
//
// A V2 journal does not record the phase that preceded its rollback decision:
// the Agent's startup Reconcile and its in-process rollback both overwrite the
// forward phase with rolling-back, and a startup decision is written for any
// pre-verified phase because the state receipt stays the source until
// target-verified. This predicate therefore does not prove the target never
// started, and neither does a mask observed at rolling-back or rolled-back:
// the rollback itself stops, disables and re-masks a target that started.
// Before the rollback decision a present mask still means BIND never started
// in this operation, because activation lifts it before enable/start. The
// inverse's safety rests on the stopped proof (inactive/dead, zero PIDs,
// empty cgroup, no named process, source-only listeners), not on history. A
// loaded target keeps the unchanged loaded-unit proof.
func BINDSwitchNeverStartedTargetJournal(j dnsengineartifact.SwitchJournalV1) bool {
	return InactiveBINDSwitchInverseJournal(j) == nil && bindSwitchTargetsFrozenStandby(j)
}

// bindSwitchTargetsFrozenStandby reports both target snapshots absent, or
// both under the persistent mask; a mixed pair is not a standby state.
func bindSwitchTargetsFrozenStandby(j dnsengineartifact.SwitchJournalV1) bool {
	absent, masked := 0, 0
	for _, unit := range j.TargetUnitsBefore {
		switch {
		case unit.ActiveState != "inactive":
			return false
		case unit.LoadState == "not-found" && unit.UnitFileState == "":
			absent++
		case unit.LoadState == "masked" && unit.UnitFileState == "masked":
			masked++
		default:
			return false
		}
	}
	n := len(j.TargetUnitsBefore)
	return n > 0 && (absent == n || masked == n)
}

// BINDSwitchBeforeRollbackDecisionJournal reports whether j has the exact
// journal shape recover-dns-bind-switch admits (InactiveBINDSwitchInverseJournal)
// but is still at a forward phase before target-verified, so no rollback
// decision is recorded. At these phases a starting Agent writes the rollback
// decision for the same request; no owner inverse command is admitted until
// then. It is a read-only classification for status guidance and grants no
// recovery or native mutation authority.
func BINDSwitchBeforeRollbackDecisionJournal(j dnsengineartifact.SwitchJournalV1) bool {
	switch j.Phase {
	case dnsengineartifact.SwitchPhaseIntent, dnsengineartifact.SwitchPhaseTargetStaged,
		dnsengineartifact.SwitchPhaseSourceStopped, dnsengineartifact.SwitchPhaseTargetStarted:
		return inactiveBINDSwitchJournalShape(j) == nil
	}
	return false
}

// BINDSwitchNeverStartedBeforeDecisionJournal narrows
// BINDSwitchBeforeRollbackDecisionJournal to journals that froze both BIND
// units in the standby class of BINDSwitchNeverStartedTargetJournal (absent,
// or under the guard's persistent mask) at a phase before target-started.
// Status may then read the target with the typed never-started observation.
// target-started is excluded: that phase records that BIND was started, so a
// masked unit there is not the pre-start class. Like
// BINDSwitchNeverStartedTargetJournal, the journal alone does not prove BIND
// never started; the native observation does.
func BINDSwitchNeverStartedBeforeDecisionJournal(j dnsengineartifact.SwitchJournalV1) bool {
	return j.Phase != dnsengineartifact.SwitchPhaseTargetStarted &&
		BINDSwitchBeforeRollbackDecisionJournal(j) && bindSwitchTargetsFrozenStandby(j)
}

// BINDV1AgentRecoveredJournal reports a V1 BIND-target journal the Agent
// itself resolves before its target is verified: a first install, a
// reinstall, the stopped half of a takeover, a paired secondary or an older V1
// PowerDNS-to-BIND switch, at intent, target-staged, source-stopped or
// target-started. At these phases no rollback decision is recorded; a live
// worker continues the request, and a restarted Agent reconciles it (it
// verifies the target or proves it absent and runs the V1 inverse). No owner
// command admits these journals. Running-BIND adoption is excluded: it has its
// own owner command and its own guidance. It is a read-only classification
// for status guidance and grants no recovery or native mutation authority.
func BINDV1AgentRecoveredJournal(j dnsengineartifact.SwitchJournalV1) bool {
	if j.Schema != dnsengineartifact.SwitchJournalSchemaV1 ||
		j.TargetEngine != transport.DNSEngineBIND ||
		(j.Mode != transport.DNSEngineSwitchModeSwitch &&
			j.Mode != transport.DNSEngineSwitchModeReinstall) {
		return false
	}
	switch j.Phase {
	case dnsengineartifact.SwitchPhaseIntent, dnsengineartifact.SwitchPhaseTargetStaged,
		dnsengineartifact.SwitchPhaseSourceStopped, dnsengineartifact.SwitchPhaseTargetStarted:
	default:
		return false
	}
	manifest, err := dnsengineartifact.SwitchJournalManifest(j)
	if err != nil {
		return false
	}
	running, err := dnsengineartifact.RunningBINDAdoptionJournal(manifest, j)
	return err == nil && !running
}

// BINDV1BeforeActivationJournal narrows BINDV1AgentRecoveredJournal to the
// phases before target-started. There the V1 producer holds both BIND units
// under the package guard's persistent mask (the install seal) until
// activation lifts it, so status may read the target with the typed
// never-started observation; the native observation, not the phase, proves it.
func BINDV1BeforeActivationJournal(j dnsengineartifact.SwitchJournalV1) bool {
	return j.Phase != dnsengineartifact.SwitchPhaseTargetStarted && BINDV1AgentRecoveredJournal(j)
}

// PDNSV1FirstInstallAgentRecoveredJournal reports a V1 PowerDNS first install
// the restarted Agent resolves by itself: switch mode, no source engine, no
// source epoch and no prior engine state receipt, standalone or paired
// secondary, with pdns.service not active before the operation, at a forward
// phase (intent through committed). The Agent's Reconcile verifies the target
// first: a verified target (always from target-verified, and from
// target-started when the state receipt was already written) goes forward to
// committed and is finalized as succeeded; otherwise the absent state receipt
// proves the frozen empty source, the Agent records the rollback decision,
// runs the V1 PowerDNS inverse and proves that no managed DNS authority
// remains, and the ledger records dns_engine_switch_rolled_back_after_restart.
// A verified target is never rolled back. Excluded: the V3 paired primary, the
// paired-secondary reconfiguration of an active PowerDNS (its rollback proof
// has its own rules), journals with a source engine or a prior state receipt,
// adoption and every decided phase. It is a read-only classification for
// status guidance and grants no recovery or native mutation authority.
func PDNSV1FirstInstallAgentRecoveredJournal(j dnsengineartifact.SwitchJournalV1) bool {
	if j.Schema != dnsengineartifact.SwitchJournalSchemaV1 ||
		j.TargetEngine != transport.DNSEnginePowerDNS ||
		j.Mode != transport.DNSEngineSwitchModeSwitch ||
		j.SourceEngine != "" || j.SourceEpoch != 0 || j.StateBefore.Exists {
		return false
	}
	switch {
	case j.Topology == transport.DNSTopologyStandalone:
	case j.Topology == transport.DNSTopologyPaired && j.PairRole == transport.DNSPairRoleSecondary:
	default:
		return false
	}
	switch j.Phase {
	case dnsengineartifact.SwitchPhaseIntent, dnsengineartifact.SwitchPhaseTargetStaged,
		dnsengineartifact.SwitchPhaseSourceStopped, dnsengineartifact.SwitchPhaseTargetStarted,
		dnsengineartifact.SwitchPhaseTargetVerified, dnsengineartifact.SwitchPhaseCommitted:
	default:
		return false
	}
	// An active pdns.service preimage is the Agent's own discriminator of the
	// paired-secondary reconfiguration, which shares this manifest shape.
	for _, unit := range j.TargetUnitsBefore {
		if unit.ActiveState == "active" {
			return false
		}
	}
	_, err := dnsengineartifact.SwitchJournalManifest(j)
	return err == nil
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
