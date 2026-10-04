package dnsenginerecovery

import (
	"strings"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
)

// The words the Agent's ledger guidance and dns-switch-status use for what a
// fresh paired PowerDNS primary recovery found not as its install wrote it.
// The Agent's refusal message is FreshPrimaryOwnerChangePrefixV3 + one of the
// three names + FreshPrimaryOwnerChangeSuffixV3 + its own explanation.
const (
	FreshPrimaryChangedConfigV3      = "the PowerDNS configuration"
	FreshPrimaryChangedDatabaseV3    = "the PowerDNS database"
	FreshPrimaryChangedStateV3       = "CelikPanel's DNS state record"
	FreshPrimaryOwnerChangePrefixV3  = "The first install of PowerDNS as the paired primary found that "
	FreshPrimaryOwnerChangeSuffixV3  = " is not as this install wrote it"
	freshPrimaryOwnershipRecordName  = "dns-engine-ownership-pdns.json"
	freshPrimaryOwnershipRecordLimit = 64 << 10
)

// RecordedFreshPrimaryOwnerChangeV3 reports which of the three names the
// Agent's stored refusal message gave. It reads only the message the Agent
// wrote with the constants above; it is not an observation of the host.
func RecordedFreshPrimaryOwnerChangeV3(message string) (string, bool) {
	for _, what := range []string{FreshPrimaryChangedConfigV3, FreshPrimaryChangedDatabaseV3, FreshPrimaryChangedStateV3} {
		if strings.HasPrefix(message, FreshPrimaryOwnerChangePrefixV3+what+FreshPrimaryOwnerChangeSuffixV3) {
			return what, true
		}
	}
	return "", false
}

// FreshPrimaryPrestartShapeJournalV3 is the journal part of the Agent's
// pre-start class: the owner command's journal predicate without a durable
// native receipt. Whether PowerDNS started is proved natively, never from
// the phase.
func FreshPrimaryPrestartShapeJournalV3(j dnsengineartifact.SwitchJournalV1) bool {
	return j.Schema == dnsengineartifact.SwitchJournalSchemaV3 && j.PDNSFreshPlan != nil &&
		j.PDNSFreshPlan.Native == nil && len(j.TargetUnitsBefore) == 1 &&
		FreshPrimaryPrestartJournalV3(j)
}

// FreshPrimaryPrestartEnablePhaseV3 reports the phases in which the enable
// intent may already have unmasked or enabled the target unit.
func FreshPrimaryPrestartEnablePhaseV3(phase string) bool {
	return phase == dnsengineartifact.SwitchPhaseTargetEnableIntent ||
		phase == dnsengineartifact.SwitchPhaseRollingBackTargetEnable
}

// FreshPrimaryPrestartRollbackPhaseV3 reports the pre-start rollback phases.
func FreshPrimaryPrestartRollbackPhaseV3(phase string) bool {
	switch phase {
	case dnsengineartifact.SwitchPhaseRollingBack, dnsengineartifact.SwitchPhaseRollingBackTargetEnable,
		dnsengineartifact.SwitchPhaseRolledBack:
		return true
	}
	return false
}

// FreshPrimaryPrestartTargetUnitV3 admits the unit states a stopped,
// never-started target can have. Before the enable-intent the unit must be
// exactly the frozen preimage; from the enable-intent it may also be loaded
// (unmasked, disabled or enabled). A mask is admitted only when the frozen
// preimage is the guard's persistent mask.
func FreshPrimaryPrestartTargetUnitV3(unit, frozen dnsengineartifact.UnitSnapshot, phase string) bool {
	if unit.Name != "pdns.service" || unit.ActiveState != "inactive" {
		return false
	}
	if unit == frozen {
		return true
	}
	if !FreshPrimaryPrestartEnablePhaseV3(phase) || unit.LoadState != "loaded" {
		return false
	}
	return unit.UnitFileState == "disabled" || unit.UnitFileState == "enabled"
}

// FreshPrimaryNativeObservationViewV3 is the journal view the after-start
// database comparison uses. The enable-intent may already have started
// PowerDNS; only the observation uses TargetStarted, never the durable file.
func FreshPrimaryNativeObservationViewV3(j dnsengineartifact.SwitchJournalV1) dnsengineartifact.SwitchJournalV1 {
	view := j
	if view.Phase == dnsengineartifact.SwitchPhaseTargetEnableIntent &&
		view.PDNSFreshPlan != nil && view.PDNSFreshPlan.Native == nil {
		view.Phase = dnsengineartifact.SwitchPhaseTargetStarted
	}
	return view
}
