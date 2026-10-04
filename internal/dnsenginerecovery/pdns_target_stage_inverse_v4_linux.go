//go:build linux

package dnsenginerecovery

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
	"github.com/alicelik/celikpanel/internal/transport"
)

// PDNSTargetStageState is a native observation made under the release and DNS
// mutation locks. NeedsRestore means the exact staged candidate is still at
// its frozen path, the live database and all its sidecars are absent, PowerDNS
// has no current process or port-53 listener, and native config/source
// units can be safely compensated. The journal records a pre-start cut but
// does not prove a process never ran historically.
// Renamed means the same exact candidate inode is at the live database name
// while PowerDNS remains stopped and BIND has not been restored. RenamedEnabled
// additionally requires the durable V4 target-enable rollback phase. Restored
// means the candidate is absent, the frozen BIND source
// is serving again, and both config and units equal their recorded preimages.
type PDNSTargetStageState uint8

const (
	PDNSTargetStageUnknown PDNSTargetStageState = iota
	PDNSTargetStageNeedsRestore
	PDNSTargetStageRenamed
	PDNSTargetStageRenamedEnabled
	PDNSTargetStageRestored
)

// PDNSTargetStageInverseOps is implemented by a fixed-path owner recovery
// adapter holding both installed locks. Read must use ReadSwitchEvidence;
// AssessNative must re-prove the managed BIND source receipt/config, candidate
// inode and bytes at either the private or pre-start live name, exact PowerDNS
// config state, stopped target and native unit state. RestoreNative may return
// the exact pre-start live inode to its private name, then remove only the exact
// candidate under those locks, then restore config and restart the frozen BIND
// source. It must verify each effect before returning. No callback may make an
// unrecorded replacement or remove owner data.
type PDNSTargetStageInverseOps struct {
	Read          func(context.Context) (SwitchEvidence, bool, error)
	ExcludeWorker func(context.Context, SwitchEvidence) error
	AssessNative  func(context.Context, dnsengineartifact.SwitchJournalV1) (PDNSTargetStageState, error)
	RestoreNative func(context.Context, dnsengineartifact.SwitchJournalV1) error
	WritePhase    func(context.Context, dnsengineartifact.SwitchJournalV1, dnsengineartifact.SwitchJournalV1) error
	PublishFailed func(context.Context, dnsengineartifact.SwitchJournalV1) error
	RemoveJournal func(context.Context, dnsengineartifact.SwitchJournalV1) error
	// VerifyTerminalWithoutJournal reads the installed ledger after retirement,
	// requiring the same request, owner, target and manifest to have the exact
	// terminal rolled-back verdict while the journal is absent.
	VerifyTerminalWithoutJournal func(context.Context, dnsengineartifact.SwitchJournalV1) error
}

// ValidateStagedPDNSTargetInverseEvidence admits only an already decided V4
// rollback with a frozen candidate. An intent without candidate identity and
// every post-activation state require different native recovery contracts.
func ValidateStagedPDNSTargetInverseEvidence(e SwitchEvidence) error {
	j, o := e.Journal, e.Observation
	if j.Schema != dnsengineartifact.SwitchJournalSchemaV4 || j.PDNSTargetPlan == nil ||
		j.PDNSTargetPlan.Kind != dnsengineartifact.PDNSTargetInversePlanKindV4 ||
		j.PDNSTargetPlan.Candidate == nil || j.InversePlan != nil ||
		j.Mode != transport.DNSEngineSwitchModeSwitch ||
		j.SourceEngine != transport.DNSEngineBIND || j.TargetEngine != transport.DNSEnginePowerDNS ||
		j.Topology != transport.DNSTopologyStandalone || j.PairRole != "" ||
		j.LocalIP != "" || j.PeerIP != "" || !j.StateBefore.Exists ||
		(j.Phase != dnsengineartifact.SwitchPhaseRollingBack && j.Phase != dnsengineartifact.SwitchPhaseRollingBackTargetEnable && j.Phase != dnsengineartifact.SwitchPhaseRolledBack) ||
		o.InverseKind != NativeInversePDNSSwitch ||
		!dnsengineartifact.ValidGeneration(o.EvidenceSHA256) ||
		o.RequestID != j.MutationRequestID || o.Phase != j.Phase ||
		o.SourceEngine != string(j.SourceEngine) || o.TargetEngine != string(j.TargetEngine) ||
		o.TargetGeneration != j.TargetGeneration || o.TargetEpoch != j.TargetEpoch ||
		o.SourceOwnership != SourceOwnershipExact || o.SourceReceipt != SourceReceiptExact ||
		o.TargetReceipt == TargetReceiptExact ||
		(!activeDNSInverseStatus(o.Status) && o.Status != EvidenceTerminalRolledBack) ||
		(o.Status == EvidenceTerminalRolledBack && j.Phase != dnsengineartifact.SwitchPhaseRolledBack) {
		return errors.New("staged PowerDNS target lacks exact V4 rollback evidence")
	}
	if len(j.SourceUnitsBefore) != 2 || len(j.TargetUnitsBefore) != 1 ||
		j.SourceUnitsBefore[0].Name != "bind9.service" ||
		j.SourceUnitsBefore[1].Name != "named.service" ||
		(j.SourceUnitsBefore[0].ActiveState != "active" && j.SourceUnitsBefore[1].ActiveState != "active") ||
		j.TargetUnitsBefore[0].Name != "pdns.service" || j.TargetUnitsBefore[0].ActiveState != "inactive" {
		return errors.New("staged PowerDNS target has unexpected native unit preimages")
	}
	return nil
}

// CompleteStagedPDNSTargetInverseV4 runs the durable portion of a bounded
// independent inverse. The native adapter must reject a missing, changed or
// started candidate, owner edits, and any live PowerDNS database. This function
// never infers a successful inverse from a completed step list or mere service
// inactivity. It retains the journal on every unknown result.
func CompleteStagedPDNSTargetInverseV4(ctx context.Context, ops PDNSTargetStageInverseOps) error {
	if ctx == nil || ops.Read == nil || ops.ExcludeWorker == nil || ops.AssessNative == nil ||
		ops.RestoreNative == nil || ops.WritePhase == nil || ops.PublishFailed == nil ||
		ops.RemoveJournal == nil || ops.VerifyTerminalWithoutJournal == nil {
		return errors.New("staged PowerDNS inverse requires complete owner recovery operations")
	}
	read := func() (SwitchEvidence, error) {
		if err := ctx.Err(); err != nil {
			return SwitchEvidence{}, err
		}
		e, present, err := ops.Read(ctx)
		if err != nil || !present {
			return SwitchEvidence{}, errors.Join(errors.New("exact staged PowerDNS evidence is unavailable"), err)
		}
		if err := ValidateStagedPDNSTargetInverseEvidence(e); err != nil {
			return SwitchEvidence{}, err
		}
		return e, nil
	}
	first, err := read()
	if err != nil {
		return err
	}
	if err := ops.ExcludeWorker(ctx, first); err != nil {
		return fmt.Errorf("exclude accepted DNS worker: %w", err)
	}
	state, err := ops.AssessNative(ctx, first.Journal)
	if err != nil || state == PDNSTargetStageUnknown {
		return errors.Join(errors.New("staged PowerDNS native preimage is unknown or owner-modified"), err)
	}
	if state == PDNSTargetStageRenamedEnabled && first.Journal.Phase != dnsengineartifact.SwitchPhaseRollingBackTargetEnable {
		return errors.New("enabled PowerDNS target lacks a durable enable-intent rollback decision")
	}
	before, err := read()
	if err != nil || !samePDNSTargetStageEvidence(first, before) {
		return errors.Join(errors.New("staged PowerDNS evidence changed before native inverse"), err)
	}
	if err := ops.ExcludeWorker(ctx, before); err != nil {
		return fmt.Errorf("recheck accepted DNS worker: %w", err)
	}
	if before.Observation.Status == EvidenceTerminalRolledBack && state != PDNSTargetStageRestored {
		return errors.New("terminal PowerDNS rollback lacks restored native proof")
	}
	if state == PDNSTargetStageNeedsRestore || state == PDNSTargetStageRenamed || state == PDNSTargetStageRenamedEnabled {
		if before.Journal.Phase != dnsengineartifact.SwitchPhaseRollingBack && before.Journal.Phase != dnsengineartifact.SwitchPhaseRollingBackTargetEnable {
			return errors.New("rolled-back PowerDNS checkpoint still needs native restoration")
		}
		if err := ops.RestoreNative(ctx, before.Journal); err != nil {
			return fmt.Errorf("restore exact staged PowerDNS preimage: %w", err)
		}
	}
	restored, err := read()
	if err != nil || !reflect.DeepEqual(first.Journal, restored.Journal) ||
		restored.Observation.SourceReceipt != SourceReceiptExact || restored.Observation.SourceOwnership != SourceOwnershipExact ||
		restored.Observation.Status != before.Observation.Status || !reflect.DeepEqual(restored.AcceptedJob, before.AcceptedJob) {
		return errors.Join(errors.New("PowerDNS source evidence changed during native inverse"), err)
	}
	if err := ops.ExcludeWorker(ctx, restored); err != nil {
		return fmt.Errorf("recheck accepted DNS worker after native inverse: %w", err)
	}
	if state, err = ops.AssessNative(ctx, restored.Journal); err != nil || state != PDNSTargetStageRestored {
		return errors.Join(errors.New("staged PowerDNS native restoration could not be proved"), err)
	}
	if restored.Journal.Phase == dnsengineartifact.SwitchPhaseRollingBack || restored.Journal.Phase == dnsengineartifact.SwitchPhaseRollingBackTargetEnable {
		if restored.Observation.Status == EvidenceTerminalRolledBack {
			return errors.New("terminal verdict precedes rolled-back checkpoint")
		}
		next := restored.Journal
		next.Phase = dnsengineartifact.SwitchPhaseRolledBack
		if err := ops.WritePhase(ctx, restored.Journal, next); err != nil {
			return fmt.Errorf("publish PowerDNS rolled-back checkpoint: %w", err)
		}
	}
	checkpoint, err := read()
	if err != nil || !samePDNSTargetStageJournal(first.Journal, checkpoint.Journal) ||
		checkpoint.Journal.Phase != dnsengineartifact.SwitchPhaseRolledBack ||
		checkpoint.Observation.Status != restored.Observation.Status || !reflect.DeepEqual(checkpoint.AcceptedJob, restored.AcceptedJob) {
		return errors.Join(errors.New("PowerDNS rollback checkpoint was not retained"), err)
	}
	if checkpoint.Observation.Status != EvidenceTerminalRolledBack {
		if !activeDNSInverseStatus(checkpoint.Observation.Status) {
			return errors.New("PowerDNS rollback lost its exact active job")
		}
		if err := ops.ExcludeWorker(ctx, checkpoint); err != nil {
			return fmt.Errorf("recheck accepted DNS worker before verdict: %w", err)
		}
		if err := ops.PublishFailed(ctx, checkpoint.Journal); err != nil {
			return fmt.Errorf("publish exact PowerDNS rollback verdict: %w", err)
		}
	}
	terminal, err := read()
	if err != nil || !samePDNSTargetStageJournal(first.Journal, terminal.Journal) ||
		terminal.Journal.Phase != dnsengineartifact.SwitchPhaseRolledBack ||
		terminal.Observation.Status != EvidenceTerminalRolledBack || terminal.Observation.SourceReceipt != SourceReceiptExact ||
		terminal.Observation.SourceOwnership != SourceOwnershipExact {
		return errors.Join(errors.New("PowerDNS terminal rollback verdict could not be re-proved"), err)
	}
	if state, err = ops.AssessNative(ctx, terminal.Journal); err != nil || state != PDNSTargetStageRestored {
		return errors.Join(errors.New("BIND source changed before PowerDNS journal retirement"), err)
	}
	final, err := read()
	if err != nil || !samePDNSTargetStageEvidence(terminal, final) {
		return errors.Join(errors.New("PowerDNS evidence changed before journal retirement"), err)
	}
	if err := ops.ExcludeWorker(ctx, final); err != nil {
		return fmt.Errorf("recheck accepted DNS worker before journal retirement: %w", err)
	}
	if err := ops.RemoveJournal(ctx, final.Journal); err != nil {
		return fmt.Errorf("retire exact PowerDNS rollback journal: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	_, present, err := ops.Read(ctx)
	if err != nil || present {
		return errors.Join(errors.New("PowerDNS rollback journal remains or its absence is unknown after retirement"), err)
	}
	if err := ops.VerifyTerminalWithoutJournal(ctx, final.Journal); err != nil {
		return fmt.Errorf("verify exact terminal PowerDNS rollback without journal: %w", err)
	}
	return nil
}

func samePDNSTargetStageJournal(before, after dnsengineartifact.SwitchJournalV1) bool {
	after.Phase = before.Phase
	return reflect.DeepEqual(before, after)
}

func samePDNSTargetStageEvidence(before, after SwitchEvidence) bool {
	return reflect.DeepEqual(before, after)
}

// VerifyExactDNSRollbackTerminalWithoutJournal is the installed-file postcondition
// for a protected rollback: the exact journal is gone and the same request's
// canonical ledger carries the terminal rolled-back verdict. The caller holds
// the release and DNS host locks across removal and this observation.
func VerifyExactDNSRollbackTerminalWithoutJournal(policy dnsengineartifact.JournalPolicy, owner servicemutationledger.FileOwner, expected dnsengineartifact.SwitchJournalV1) error {
	if !policy.RequireOwner || policy.StateUID != owner.UID || policy.StateGID != owner.GID ||
		filepath.Base(policy.StatePath) != "dns-engine-state.json" || expected.Phase != dnsengineartifact.SwitchPhaseRolledBack {
		return errors.New("DNS rollback terminal proof requires trusted owner and rolled-back journal")
	}
	if err := policy.ValidateSwitchJournal(expected); err != nil {
		return err
	}
	id := dnsengineartifact.SwitchIdentity{RequestID: expected.MutationRequestID, OwnerID: expected.MutationOwnerID, Target: expected.TargetEngine, Qualifier: expected.ManifestQualifier}
	if err := id.Validate(); err != nil {
		return err
	}
	root := filepath.Dir(policy.StatePath)
	journalPath := filepath.Join(root, "dns-engine-switch-journal.json")
	ledgerPath := filepath.Join(root, "service-mutations.json")
	absent := func() error {
		_, present, err := servicemutationledger.ReadFile(journalPath, dnsengineartifact.SwitchJournalLimit, owner)
		if err != nil || present {
			return errors.Join(errors.New("DNS rollback journal is present or unreadable after retirement"), err)
		}
		return nil
	}
	if err := absent(); err != nil {
		return err
	}
	raw, present, err := servicemutationledger.ReadFile(ledgerPath, servicemutationledger.MaxSize, owner)
	if err != nil || !present {
		return errors.Join(errors.New("DNS rollback terminal ledger is absent or unreadable"), err)
	}
	ledger, err := servicemutationledger.Decode(raw)
	if err != nil {
		return err
	}
	if !id.TerminalRolledBackJob(ledger) {
		return errors.New("DNS rollback ledger lacks the exact same-request terminal verdict")
	}
	return absent()
}
