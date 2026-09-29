package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
	"github.com/alicelik/celikpanel/internal/transport"
)

// A BIND switch whose journal already reached target-verified (or committed)
// can never roll back automatically. If its target generation pointer
// (<generation root>/current) has disappeared - a crash inside the in-process
// error path after a durable write that reported failure, or power loss
// between a pointer change and the rollback that should follow it - the target
// cannot be verified either, and BIND, whose configuration includes
// current/zones.conf, cannot start after the next reboot (batch 4 cell c6,
// 2026-09-29). Same-request recovery therefore restores that one pointer, and
// nothing else, when every other piece of the verified target is exactly what
// the journal recorded. Anything that could be an owner's or another
// operation's change is refused and reported instead.
//
// Günlüğü target-verified (veya committed) aşamasına ulaşmış bir BIND geçişi
// otomatik geri alınamaz. Hedef nesil işaretçisi kaybolmuşsa hedef de
// doğrulanamaz ve yapılandırması current/zones.conf'u içeren BIND bir sonraki
// açılışta başlayamaz. Aynı istek kurtarması, doğrulanmış hedefin geri kalan
// her parçası günlüğün kaydettiğiyle birebir aynıysa yalnız o işaretçiyi geri
// koyar; sahibin ya da başka bir işlemin değişikliği olabilecek her durum
// reddedilir ve bildirilir.

type bindTargetPointerRefusalKind int

const (
	bindTargetPointerUnreadable bindTargetPointerRefusalKind = iota + 1
	bindTargetPointerSelectsOther
	bindTargetPointerRecordsChanged
	bindTargetPointerGenerationUnverified
	bindTargetPointerConfigChanged
	bindTargetPointerRestoreFailed
)

// bindTargetPointerRefusal is the unknown result of a refused pointer repair.
// It names what is missing or different, whether BIND is known to be unable to
// start after a reboot, and the owner's next step (D-024).
type bindTargetPointerRefusal struct {
	kind       bindTargetPointerRefusalKind
	requestID  string
	pointer    string
	generation string
	selected   string
	// bootBlocked is true only when BIND's zone anchor was read and includes
	// the missing pointer's zones.conf.
	bootBlocked bool
	cause       error
}

func (refusal *bindTargetPointerRefusal) Unwrap() error { return refusal.cause }

func (refusal *bindTargetPointerRefusal) Error() string {
	var found string
	switch refusal.kind {
	case bindTargetPointerUnreadable:
		found = fmt.Sprintf("BIND's generation pointer %s could not be verified (%v), so the Agent left it unchanged", refusal.pointer, refusal.cause)
	case bindTargetPointerSelectsOther:
		found = fmt.Sprintf("BIND's generation pointer %s selects generation %s instead of generation %s, which this operation verified; it was changed outside this operation, so the Agent left it unchanged", refusal.pointer, refusal.selected, refusal.generation)
	case bindTargetPointerRecordsChanged:
		found = fmt.Sprintf("BIND's generation pointer %s is missing, and the DNS engine records no longer match this operation (%v), so the Agent did not restore it", refusal.pointer, refusal.cause)
	case bindTargetPointerGenerationUnverified:
		found = fmt.Sprintf("BIND's generation pointer %s is missing, and generation %s, which this operation verified, is missing or no longer verifies (%v), so the Agent did not restore it", refusal.pointer, refusal.generation, refusal.cause)
	case bindTargetPointerConfigChanged:
		found = fmt.Sprintf("BIND's generation pointer %s is missing, and BIND's configuration is not the one this operation wrote (%v), so the Agent did not restore it", refusal.pointer, refusal.cause)
	default:
		found = fmt.Sprintf("BIND's generation pointer %s is missing, and restoring it to generation %s failed (%v)", refusal.pointer, refusal.generation, refusal.cause)
	}
	parts := []string{found + "."}
	if consequence := refusal.consequence(); consequence != "" {
		parts = append(parts, consequence)
	}
	return strings.Join(append(parts, refusal.nextStep()), " ")
}

func (refusal *bindTargetPointerRefusal) pointerMissing() bool {
	return refusal.kind != bindTargetPointerUnreadable &&
		refusal.kind != bindTargetPointerSelectsOther
}

func (refusal *bindTargetPointerRefusal) consequence() string {
	switch {
	case !refusal.pointerMissing():
		return ""
	case refusal.bootBlocked:
		return fmt.Sprintf("BIND's configuration includes %s, so named cannot start after a reboot or a BIND restart until the pointer is back; a named that is still running keeps answering until it stops.", filepath.ToSlash(filepath.Join(refusal.pointer, "zones.conf")))
	default:
		return "Whether BIND can start after a reboot was not established."
	}
}

func (refusal *bindTargetPointerRefusal) nextStep() string {
	avoid := ""
	if refusal.pointerMissing() {
		avoid = "avoids rebooting or restarting BIND, "
	}
	return fmt.Sprintf("The server owner %sruns /usr/libexec/celikpanel/recovery dns-switch-status --quiesced --request-id %s and contacts support with this request id; once the cause is resolved, restarting the Agent retries this same operation.", avoid, refusal.requestID)
}

// ledgerMessage is the bounded, secret-free panel receipt text for the released
// job; the full refusal stays in the Agent log.
func (refusal *bindTargetPointerRefusal) ledgerMessage() string {
	const tail = " New DNS changes are blocked. Run recovery dns-switch-status --quiesced and contact support with this request; restarting the Agent retries this same operation."
	if !refusal.pointerMissing() {
		return "The interrupted DNS switch reached a verified BIND target, but BIND's generation pointer now selects another generation or could not be read, so the Agent left it unchanged." + tail
	}
	var reason string
	switch refusal.kind {
	case bindTargetPointerGenerationUnverified:
		reason = "the generation this operation verified is missing or changed"
	case bindTargetPointerRecordsChanged, bindTargetPointerConfigChanged:
		reason = "BIND's configuration or the DNS engine records changed"
	default:
		reason = "restoring it failed"
	}
	boot := " Whether BIND can start after a reboot is not known; avoid rebooting until this is resolved."
	if refusal.bootBlocked {
		boot = " BIND cannot start after a reboot or BIND restart until the pointer is back; avoid both."
	}
	return "The interrupted DNS switch reached a verified BIND target, but BIND's generation pointer is missing and was not restored: " + reason + "." + boot + tail
}

type bindTargetPointerRepairOps struct {
	pointerPath string
	// current reads the pointer: its generation and whether it exists.
	current func() (string, bool, error)
	// anchorIncludesPointer reports whether BIND's zone anchor includes the
	// pointer's zones.conf; it informs the refusal text only.
	anchorIncludesPointer func() (bool, error)
	// verifyRecords proves source ownership and the exact target state receipt.
	verifyRecords func() error
	// loadTarget verifies the exact immutable target tree and its receipt
	// identity (generation, epoch, pairing) against the journal.
	loadTarget func() (binddns.Receipt, error)
	// verifyConfig proves BIND's configuration is exactly what the switch
	// wrote for that receipt, including the include of the pointer.
	verifyConfig func(binddns.Receipt) error
	// restore selects the generation only while no pointer exists.
	restore func() error
	logf    func(string, ...any)
}

// bindTargetPointerRepairApplies admits only BIND switch journals that already
// recorded a verified target and name its exact generation.
func bindTargetPointerRepairApplies(journal dnsEngineSwitchJournal) bool {
	return dnsenginerecovery.VerifiedBINDTargetPointerJournal(journal)
}

// bindTargetPointerRefusalKinds maps the shared read-only classification
// (dnsenginerecovery.ClassifyBINDTargetPointer, also used by dns-switch-status)
// to the Agent's refusal text.
var bindTargetPointerRefusalKinds = map[dnsenginerecovery.BINDTargetPointerKind]bindTargetPointerRefusalKind{
	dnsenginerecovery.BINDTargetPointerUnreadable:           bindTargetPointerUnreadable,
	dnsenginerecovery.BINDTargetPointerSelectsOther:         bindTargetPointerSelectsOther,
	dnsenginerecovery.BINDTargetPointerRecordsChanged:       bindTargetPointerRecordsChanged,
	dnsenginerecovery.BINDTargetPointerGenerationUnverified: bindTargetPointerGenerationUnverified,
	dnsenginerecovery.BINDTargetPointerConfigChanged:        bindTargetPointerConfigChanged,
}

func repairMissingBINDTargetPointerWithOps(
	journal dnsEngineSwitchJournal,
	ops bindTargetPointerRepairOps,
) (bool, error) {
	if !bindTargetPointerRepairApplies(journal) {
		return false, nil
	}
	if ops.pointerPath == "" || ops.current == nil || ops.anchorIncludesPointer == nil ||
		ops.verifyRecords == nil || ops.loadTarget == nil || ops.verifyConfig == nil ||
		ops.restore == nil || ops.logf == nil {
		return false, errors.New("BIND target pointer repair operations are incomplete")
	}
	finding, err := dnsenginerecovery.ClassifyBINDTargetPointer(
		journal.TargetGeneration,
		dnsenginerecovery.BINDTargetPointerChecks{
			Current:               ops.current,
			AnchorIncludesPointer: ops.anchorIncludesPointer,
			VerifyRecords:         ops.verifyRecords,
			LoadTarget:            ops.loadTarget,
			VerifyConfig:          ops.verifyConfig,
		},
	)
	if err != nil {
		return false, err
	}
	refuse := func(kind bindTargetPointerRefusalKind, cause error) *bindTargetPointerRefusal {
		return &bindTargetPointerRefusal{
			kind: kind, requestID: journal.MutationRequestID,
			pointer: ops.pointerPath, generation: journal.TargetGeneration,
			selected: finding.Selected, bootBlocked: finding.BootBlocked,
			cause: cause,
		}
	}
	switch finding.Kind {
	case dnsenginerecovery.BINDTargetPointerSelectsTarget:
		// The pointer is not what failed; keep the original verdict.
		return false, nil
	case dnsenginerecovery.BINDTargetPointerRepairable:
	default:
		kind, known := bindTargetPointerRefusalKinds[finding.Kind]
		if !known {
			return false, fmt.Errorf("unknown BIND target pointer finding %d", finding.Kind)
		}
		return false, refuse(kind, finding.Cause)
	}
	if err := ops.restore(); err != nil {
		return false, refuse(bindTargetPointerRestoreFailed, err)
	}
	ops.logf(
		"DNS switch recovery for request %s restored BIND's missing generation pointer %s to generation %s, which this operation had already verified; the full target check now runs again.",
		journal.MutationRequestID, ops.pointerPath, journal.TargetGeneration,
	)
	return true, nil
}

// repairMissingBINDTargetPointer is the host adapter for
// dnsenginerecovery.Operations.RepairVerifiedTarget. The caller holds the
// accepted operation's host mutation lock (startup or same-request recovery).
func repairMissingBINDTargetPointer(
	ctx context.Context,
	journal dnsEngineSwitchJournal,
) (bool, error) {
	if !bindTargetPointerRepairApplies(journal) {
		return false, nil
	}
	manifest, err := switchJournalManifest(journal)
	if err != nil {
		return false, err
	}
	profile, err := verifiedHostProfileForAnyFamily()
	if err != nil {
		return false, err
	}
	layout, err := bindLayout(profile)
	if err != nil {
		return false, err
	}
	publisher, _, err := newHostBINDPublisher(ctx, layout)
	if err != nil {
		return false, err
	}
	legacyPairedTarget := false
	return repairMissingBINDTargetPointerWithOps(journal, bindTargetPointerRepairOps{
		pointerPath:           filepath.Join(layout.GenerationRoot, "current"),
		current:               publisher.Current,
		anchorIncludesPointer: bindAnchorIncludesCurrentPointer(layout),
		verifyRecords: func() error {
			if err := verifyDNSSwitchSourceOwnership(journal); err != nil {
				return err
			}
			state, exists, err := readDNSEngineState()
			if err != nil {
				return err
			}
			if !exists || !exactDNSEngineStateForJournal(state, journal) {
				return errors.New("DNS engine target state receipt is absent or different")
			}
			legacyPairedTarget = isLegacyDNSEngineState(state) &&
				manifest.Topology == transport.DNSTopologyPaired
			return nil
		},
		loadTarget: func() (binddns.Receipt, error) {
			tree, err := publisher.LoadGeneration(journal.TargetGeneration)
			if err != nil {
				return binddns.Receipt{}, err
			}
			receipt := tree.CurrentReceipt()
			if receipt.Generation != journal.TargetGeneration ||
				receipt.EngineEpoch != journal.TargetEpoch ||
				!dnsenginerecovery.ExactBINDPairingForSwitchJournal(receipt, manifest, journal) {
				return binddns.Receipt{}, errors.New("BIND generation receipt differs from the journal")
			}
			return receipt, nil
		},
		verifyConfig: func(receipt binddns.Receipt) error {
			return verifyManagedBINDRuntimeConfigExact(ctx, layout, receipt, legacyPairedTarget)
		},
		restore: func() error {
			return runBINDMutationWithMaskParentProof(
				verifyBINDMaskParentMetadata,
				func() error { return publisher.RestoreMissingPointer(journal.TargetGeneration) },
			)
		},
		logf: log.Printf,
	})
}

// releasedDNSSwitchUnknownMessage is the panel receipt text for a released,
// undecided DNS switch. A refused BIND pointer repair, an unrecorded BIND
// target without its pointer and an unfinished fresh paired PowerDNS primary
// have their own texts.
func releasedDNSSwitchUnknownMessage(recoveryErr error) string {
	var refusal *bindTargetPointerRefusal
	if errors.As(recoveryErr, &refusal) {
		return refusal.ledgerMessage()
	}
	var unrecorded *bindUnrecordedTargetRefusal
	if errors.As(recoveryErr, &unrecorded) {
		return unrecorded.ledgerMessage()
	}
	var freshPrimary *freshPrimaryV3RecoveryError
	if errors.As(recoveryErr, &freshPrimary) {
		return freshPrimary.ledgerMessage()
	}
	return "The interrupted DNS switch could not be verified after the Agent restarted. Its exact journal remains for DNS recovery, and new DNS changes are blocked. The server administrator should inspect the native DNS service and run recovery dns-switch-status --quiesced; after resolving the reported cause, restart the Agent to retry this same operation. Unrelated host changes can continue."
}
