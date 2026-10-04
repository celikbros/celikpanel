package dnsenginerecovery

import (
	"errors"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/transport"
)

// BIND's configuration includes <generation root>/current/zones.conf. When
// the current pointer disappears, a named that is running keeps answering from
// what it loaded, but named cannot start after a reboot or a BIND restart
// until the pointer is back. This file holds the read-only classification of
// that pointer for a BIND-target switch journal, shared by the Agent's
// same-request repair (which alone may restore the pointer) and the owner's
// read-only dns-switch-status. Nothing here writes.
//
// BIND yapılandırması <nesil kökü>/current/zones.conf dosyasını içerir.
// İşaretçi kaybolursa çalışan named yüklediğinden yanıt vermeyi sürdürür; ama
// işaretçi geri gelene dek yeniden başlatmada veya açılışta başlayamaz. Bu
// dosya o işaretçinin salt-okur sınıflandırmasıdır; hiçbir şey yazmaz.

// BINDTargetPointerKind is what the pointer check found.
type BINDTargetPointerKind int

const (
	// BINDTargetPointerSelectsTarget: the pointer selects the journal's
	// target generation; the pointer is not what is wrong.
	BINDTargetPointerSelectsTarget BINDTargetPointerKind = iota + 1
	// BINDTargetPointerRepairable: the pointer is absent, and the records,
	// the exact target generation and BIND's configuration all passed the
	// supplied checks.
	BINDTargetPointerRepairable
	// BINDTargetPointerUnreadable: the pointer could not be read or is not a
	// safe pointer.
	BINDTargetPointerUnreadable
	// BINDTargetPointerSelectsOther: the pointer selects another generation.
	BINDTargetPointerSelectsOther
	// BINDTargetPointerRecordsChanged: the pointer is absent and the DNS
	// engine records are not this operation's target.
	BINDTargetPointerRecordsChanged
	// BINDTargetPointerGenerationUnverified: the pointer is absent and the
	// target generation is missing or no longer verifies.
	BINDTargetPointerGenerationUnverified
	// BINDTargetPointerConfigChanged: the pointer is absent and BIND's
	// configuration is not the one the operation wrote.
	BINDTargetPointerConfigChanged
)

// BINDTargetPointerChecks are the read-only observations of one host.
type BINDTargetPointerChecks struct {
	// Current reads the pointer: its generation and whether it exists.
	Current func() (string, bool, error)
	// AnchorIncludesPointer reports whether BIND's zone anchor includes the
	// pointer's zones.conf. It informs the boot consequence only.
	AnchorIncludesPointer func() (bool, error)
	// VerifyRecords proves the records name this operation's target.
	VerifyRecords func() error
	// LoadTarget verifies the exact immutable target generation.
	LoadTarget func() (binddns.Receipt, error)
	// VerifyConfig proves BIND's configuration for that receipt.
	VerifyConfig func(binddns.Receipt) error
}

// BINDTargetPointerFinding is one classification.
type BINDTargetPointerFinding struct {
	Kind BINDTargetPointerKind
	// Selected is the generation the pointer selects (SelectsOther only).
	Selected string
	// BootBlocked is true only when the pointer is absent and the anchor was
	// read and includes the pointer's zones.conf.
	BootBlocked bool
	// Cause is the failed check, if any.
	Cause error
}

// PointerMissing reports whether the finding is about an absent pointer.
func (finding BINDTargetPointerFinding) PointerMissing() bool {
	switch finding.Kind {
	case BINDTargetPointerRepairable, BINDTargetPointerRecordsChanged,
		BINDTargetPointerGenerationUnverified, BINDTargetPointerConfigChanged:
		return true
	}
	return false
}

func bindTargetJournalShape(j dnsengineartifact.SwitchJournalV1) bool {
	return j.TargetEngine == transport.DNSEngineBIND &&
		(j.Schema == dnsengineartifact.SwitchJournalSchemaV1 ||
			j.Schema == dnsengineartifact.SwitchJournalSchemaV2) &&
		dnsengineartifact.ValidGeneration(j.TargetGeneration)
}

// VerifiedBINDTargetPointerJournal admits BIND-target switch journals that
// already recorded a verified target (target-verified or committed): the only
// journals whose missing pointer the Agent may restore.
func VerifiedBINDTargetPointerJournal(j dnsengineartifact.SwitchJournalV1) bool {
	return bindTargetJournalShape(j) &&
		(j.Phase == dnsengineartifact.SwitchPhaseTargetVerified ||
			j.Phase == dnsengineartifact.SwitchPhaseCommitted)
}

// UnrecordedBINDTargetJournal admits BIND-target switch journals before any
// verified target or rollback decision was recorded. Their pointer is never
// restored: the target was never recorded as verified.
func UnrecordedBINDTargetJournal(j dnsengineartifact.SwitchJournalV1) bool {
	if !bindTargetJournalShape(j) {
		return false
	}
	switch j.Phase {
	case dnsengineartifact.SwitchPhaseIntent, dnsengineartifact.SwitchPhaseTargetStaged,
		dnsengineartifact.SwitchPhaseSourceStopped, dnsengineartifact.SwitchPhaseTargetStarted:
		return true
	}
	return false
}

// FirstInstallBINDSwitchJournal reports the V1 first-install BIND switch
// shape: no source engine or epoch, no prior state receipt, no prior
// generation, no source units, and no target unit that was active before. Its
// prior state is "no DNS engine": an inverse has no owner source to damage.
func FirstInstallBINDSwitchJournal(j dnsengineartifact.SwitchJournalV1) bool {
	if j.Schema != dnsengineartifact.SwitchJournalSchemaV1 ||
		j.Mode != transport.DNSEngineSwitchModeSwitch ||
		j.TargetEngine != transport.DNSEngineBIND ||
		j.SourceEngine != "" || j.SourceEpoch != 0 ||
		j.StateBefore.Exists || j.HadPrevious || j.PreviousGeneration != "" ||
		len(j.SourceUnitsBefore) != 0 || len(j.TargetUnitsBefore) == 0 {
		return false
	}
	for _, unit := range j.TargetUnitsBefore {
		if unit.ActiveState != "inactive" {
			return false
		}
	}
	return true
}

// ClassifyBINDTargetPointer reads the pointer and, when it is absent, runs
// the remaining checks in order (records, target generation, configuration).
// The error is returned only for incomplete checks; every observation is a
// finding.
func ClassifyBINDTargetPointer(targetGeneration string, checks BINDTargetPointerChecks) (BINDTargetPointerFinding, error) {
	if !dnsengineartifact.ValidGeneration(targetGeneration) || checks.Current == nil ||
		checks.AnchorIncludesPointer == nil || checks.VerifyRecords == nil ||
		checks.LoadTarget == nil || checks.VerifyConfig == nil {
		return BINDTargetPointerFinding{}, errors.New("BIND target pointer checks are incomplete")
	}
	current, exists, err := checks.Current()
	if err != nil {
		return BINDTargetPointerFinding{Kind: BINDTargetPointerUnreadable, Cause: err}, nil
	}
	if exists {
		if current == targetGeneration {
			return BINDTargetPointerFinding{Kind: BINDTargetPointerSelectsTarget}, nil
		}
		return BINDTargetPointerFinding{
			Kind: BINDTargetPointerSelectsOther, Selected: current,
			Cause: binddns.ErrCurrentPointerSelectsOther,
		}, nil
	}
	bootBlocked, anchorErr := checks.AnchorIncludesPointer()
	finding := BINDTargetPointerFinding{BootBlocked: bootBlocked && anchorErr == nil}
	if err := checks.VerifyRecords(); err != nil {
		finding.Kind, finding.Cause = BINDTargetPointerRecordsChanged, err
		return finding, nil
	}
	receipt, err := checks.LoadTarget()
	if err != nil {
		finding.Kind, finding.Cause = BINDTargetPointerGenerationUnverified, err
		return finding, nil
	}
	if err := checks.VerifyConfig(receipt); err != nil {
		finding.Kind, finding.Cause = BINDTargetPointerConfigChanged, err
		return finding, nil
	}
	finding.Kind = BINDTargetPointerRepairable
	return finding, nil
}
