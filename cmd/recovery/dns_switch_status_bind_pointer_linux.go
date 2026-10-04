//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/bindroot"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
)

// dns-switch-status reads BIND's generation pointer for a BIND-target journal
// with the Agent's own classification (dnsenginerecovery.
// ClassifyBINDTargetPointer) and says, in plain words, what is wrong, what it
// means for named and what the owner does next. It never writes: only the
// Agent's same-request recovery may restore a pointer.
//
// dns-switch-status, BIND hedefli bir günlük için nesil işaretçisini Agent'ın
// kendi sınıflandırmasıyla okur ve neyin yanlış olduğunu, named için ne
// anlama geldiğini ve sahibin sonraki adımını düz sözcüklerle söyler. Hiçbir
// şey yazmaz.

// bindPointerStatus is one read-only pointer observation.
type bindPointerStatus struct {
	pointerPath string
	finding     dnsenginerecovery.BINDTargetPointerFinding
	// namedState is named.service's ActiveState from the status probe, or ""
	// when it is unknown.
	namedState string
}

// bindPointerStatusApplies selects the journals whose pointer status names:
// a recorded verified BIND target, or an unrecorded one whose records already
// name the target (the older-Agent shape).
func bindPointerStatusApplies(e dnsenginerecovery.SwitchEvidence) bool {
	if dnsenginerecovery.VerifiedBINDTargetPointerJournal(e.Journal) {
		return true
	}
	return dnsenginerecovery.UnrecordedBINDTargetJournal(e.Journal) &&
		e.Observation.TargetReceipt == dnsenginerecovery.TargetReceiptExact
}

// observeInstalledBINDTargetPointer classifies the installed pointer. The
// configuration check is the status command's disk-level include check, which
// is weaker than the Agent's; the text says the Agent's own checks decide.
func observeInstalledBINDTargetPointer(ctx context.Context, e dnsenginerecovery.SwitchEvidence) (dnsenginerecovery.BINDTargetPointerFinding, string, error) {
	layout, gid, err := installedBINDLayout()
	if err != nil {
		return dnsenginerecovery.BINDTargetPointerFinding{}, "", err
	}
	publisher, err := binddns.NewOSPublisher(string(layout))
	if err != nil {
		return dnsenginerecovery.BINDTargetPointerFinding{}, "", err
	}
	manifest, err := dnsengineartifact.SwitchJournalManifest(e.Journal)
	if err != nil {
		return dnsenginerecovery.BINDTargetPointerFinding{}, "", err
	}
	exactPairing := func(receipt binddns.Receipt) bool {
		return dnsenginerecovery.ExactBINDPairingForSwitchJournal(receipt, manifest, e.Journal)
	}
	finding, err := dnsenginerecovery.ClassifyBINDTargetPointer(
		e.Journal.TargetGeneration,
		bindPointerStatusChecks(ctx, e, publisher, layout, gid, exactPairing),
	)
	return finding, filepath.Join(string(layout), "current"), err
}

func bindPointerStatusChecks(
	ctx context.Context,
	e dnsenginerecovery.SwitchEvidence,
	publisher *binddns.Publisher,
	layout bindroot.Layout,
	gid uint32,
	exactPairing func(binddns.Receipt) bool,
) dnsenginerecovery.BINDTargetPointerChecks {
	config := func() error { return verifyInstalledBINDConfig(ctx, layout, gid) }
	return dnsenginerecovery.BINDTargetPointerChecks{
		Current: publisher.Current,
		AnchorIncludesPointer: func() (bool, error) {
			err := config()
			return err == nil, err
		},
		VerifyRecords: func() error {
			if e.Observation.TargetReceipt != dnsenginerecovery.TargetReceiptExact {
				return fmt.Errorf("the DNS engine state receipt is %s", e.Observation.TargetReceipt)
			}
			switch e.Observation.SourceOwnership {
			case dnsenginerecovery.SourceOwnershipNotApplicable, dnsenginerecovery.SourceOwnershipExact:
				return nil
			}
			return fmt.Errorf("the source ownership receipt is %s", e.Observation.SourceOwnership)
		},
		LoadTarget: func() (binddns.Receipt, error) {
			tree, err := publisher.LoadGeneration(e.Journal.TargetGeneration)
			if err != nil {
				return binddns.Receipt{}, err
			}
			receipt := tree.CurrentReceipt()
			if receipt.Generation != e.Journal.TargetGeneration ||
				receipt.EngineEpoch != e.Journal.TargetEpoch || !exactPairing(receipt) {
				return binddns.Receipt{}, errors.New("the generation's receipt differs from the journal")
			}
			return receipt, nil
		},
		VerifyConfig: func(binddns.Receipt) error { return config() },
	}
}

func namedUnitState(units []dnsenginerecovery.NativeUnitObservation, unitErr error) string {
	if unitErr != nil {
		return ""
	}
	for _, unit := range units {
		if unit.Name == "named.service" {
			return unit.ActiveState
		}
	}
	return ""
}

func bindPointerBootText(status bindPointerStatus) string {
	text := "Whether BIND can start after a reboot was not established, because the managed include in BIND's configuration could not be confirmed."
	if status.finding.BootBlocked {
		text = fmt.Sprintf("BIND's configuration includes %s, so named keeps answering while it runs, but it cannot start after a reboot or a BIND restart until the pointer is back.", filepath.ToSlash(filepath.Join(status.pointerPath, "zones.conf")))
	}
	switch status.namedState {
	case "active":
		text += " named.service is running now."
	case "inactive", "failed":
		text += " named.service is not running now, so BIND is not answering DNS on this server."
	}
	return text
}

// bindTargetPointerStatusText is the read-only guidance. It is empty when the
// pointer is not what is wrong (or, for an unrecorded journal, not missing).
func bindTargetPointerStatusText(j dnsengineartifact.SwitchJournalV1, status bindPointerStatus) string {
	request := j.MutationRequestID
	const unchanged = " This status check changed nothing."
	finding := status.finding
	if dnsenginerecovery.UnrecordedBINDTargetJournal(j) {
		if !finding.PointerMissing() {
			return ""
		}
		observed := fmt.Sprintf("This DNS switch stopped at phase %s, before its BIND target was recorded as verified, but the DNS engine records already name the BIND target and BIND's generation pointer %s is missing; an Agent of an earlier release can leave this state when it stopped inside its own rollback.", j.Phase, status.pointerPath)
		if dnsenginerecovery.FirstInstallBINDSwitchJournal(j) {
			return observed + " " + bindPointerBootText(status) + fmt.Sprintf(" This is a first install with no previous DNS engine. Next step: the server owner restarts the CelikPanel Agent (systemctl restart celikpanel-agent) without rebooting first; its recovery of this same request rolls the first install back to no DNS engine, after proving BIND stopped and that no DNS service is left listening on port 53. Then rerun this check with --quiesced --request-id %s.", request) + unchanged
		}
		return observed + " " + bindPointerBootText(status) + fmt.Sprintf(" The Agent does not restore the pointer of a target that was never recorded as verified, and it does not roll back automatically because the prior DNS state cannot be proved. Next step: the server owner avoids rebooting or restarting BIND, keeps the journal, checks which DNS service is running (systemctl status named pdns), and contacts support with request id %s; no owner recovery command applies to this state.", request) + unchanged
	}
	if !dnsenginerecovery.VerifiedBINDTargetPointerJournal(j) {
		return ""
	}
	switch finding.Kind {
	case dnsenginerecovery.BINDTargetPointerSelectsTarget:
		return ""
	case dnsenginerecovery.BINDTargetPointerRepairable:
		return fmt.Sprintf("BIND's generation pointer %s is missing. This operation already recorded BIND generation %s as verified, and in read-only checks that generation, the DNS engine records and BIND's managed include still match it. ", status.pointerPath, j.TargetGeneration) +
			bindPointerBootText(status) +
			fmt.Sprintf(" Next step: the server owner restarts the CelikPanel Agent (systemctl restart celikpanel-agent) without rebooting or restarting BIND first; its recovery of this same request restores the missing pointer to generation %s only if its own full checks pass, then verifies the target again. Then rerun this check with --quiesced --request-id %s.", j.TargetGeneration, request) + unchanged
	case dnsenginerecovery.BINDTargetPointerRecordsChanged, dnsenginerecovery.BINDTargetPointerGenerationUnverified, dnsenginerecovery.BINDTargetPointerConfigChanged:
		var reason, check string
		switch finding.Kind {
		case dnsenginerecovery.BINDTargetPointerRecordsChanged:
			reason = "the DNS engine records no longer name this operation's verified target"
			check = "whether another operation or a restore changed the DNS engine records"
		case dnsenginerecovery.BINDTargetPointerGenerationUnverified:
			reason = fmt.Sprintf("generation %s, which this operation verified, is missing or no longer verifies", j.TargetGeneration)
			check = fmt.Sprintf("whether %s was deleted or edited", filepath.ToSlash(filepath.Join(filepath.Dir(status.pointerPath), "generations", j.TargetGeneration)))
		default:
			reason = "BIND's configuration no longer carries the managed include this operation wrote"
			check = "whether BIND's configuration files were edited by hand"
		}
		return fmt.Sprintf("BIND's generation pointer %s is missing, and %s (%v), so the Agent will not restore it automatically. ", status.pointerPath, reason, finding.Cause) +
			bindPointerBootText(status) +
			fmt.Sprintf(" Next step: the server owner avoids rebooting or restarting BIND, keeps the journal, checks %s, and contacts support with request id %s. Restarting the Agent repeats the same checks and changes nothing while they fail.", check, request) + unchanged
	case dnsenginerecovery.BINDTargetPointerSelectsOther:
		return fmt.Sprintf("BIND's generation pointer %s selects generation %s, not generation %s, which this operation recorded as verified. It was changed outside this operation, so no automatic repair applies and restarting the Agent will not change it. A running named keeps serving what it loaded; after a restart or reboot it loads generation %s. Next step: the server owner checks who changed the pointer (another CelikPanel operation, a restore from backup or a manual edit) and whether generation %s is the DNS data this server should serve, keeps the journal, and contacts support with request id %s.", status.pointerPath, finding.Selected, j.TargetGeneration, finding.Selected, finding.Selected, request) + unchanged
	case dnsenginerecovery.BINDTargetPointerUnreadable:
		return fmt.Sprintf("BIND's generation pointer %s could not be read safely (%v), so whether BIND can start after a reboot is not known and no automatic repair applies. Next step: the server owner avoids rebooting or restarting BIND, checks that %s is a root-owned symbolic link to generations/<generation>, keeps the journal, and contacts support with request id %s.", status.pointerPath, finding.Cause, status.pointerPath, request) + unchanged
	}
	return ""
}
