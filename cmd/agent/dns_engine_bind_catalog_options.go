package main

import (
	"errors"

	"github.com/alicelik/celikpanel/internal/binddns"
)

func bindSecondaryOptionsPairing(role, localIP, localNS, peerIP, peerNS string) *binddns.Pairing {
	if role != binddns.PairRoleSecondary {
		return nil
	}
	return &binddns.Pairing{Role: role, LocalIP: localIP, LocalNS: localNS, PeerIP: peerIP, PeerNS: peerNS}
}

// Only the reviewed manifest, verified immutable receipt, or exact recovery
// journal may supply this identity. It is never inferred from mutable settings.
func managedBINDCatalogOptions(transferPeer string, pairings []*binddns.Pairing) (string, error) {
	if len(pairings) > 1 {
		return "", errors.New("BIND options have multiple pairing identities")
	}
	if len(pairings) == 0 || pairings[0] == nil {
		return "", nil
	}
	pairing := *pairings[0]
	if pairing.Role != binddns.PairRoleSecondary || transferPeer != pairing.PeerIP {
		return "", errors.New("BIND catalog and transfer policy identities differ")
	}
	return binddns.SecondaryCatalogOptions(pairing)
}

func bindSecondaryOptionsFromReceipt(receipt *binddns.PairingReceipt) (*binddns.Pairing, error) {
	if receipt == nil || receipt.Role != binddns.PairRoleSecondary {
		return nil, nil
	}
	// Versions 1 and 2 share the options-scoped subscription; they differ only
	// in the catalog zone's own transfer clause inside the immutable tree.
	if !binddns.AcceptedSecondaryConfigVersion(receipt.SecondaryConfigVersion) {
		return nil, errors.New("BIND secondary receipt does not prove the current catalog subscription policy")
	}
	return bindSecondaryOptionsPairing(receipt.Role, receipt.LocalIP, receipt.LocalNS, receipt.PeerIP, receipt.PeerNS), nil
}

// bindExpectedGenerationForTarget renders a switch plan with the current
// policy. When that is not the recorded target and the plan is a secondary, it
// also renders the earlier accepted peer-only catalog policy (version 1), so a
// journal, completed switch or state an earlier release wrote is recognised as
// the product's own output instead of an owner change. The caller still
// compares the returned ID and content with the recorded target.
func bindExpectedGenerationForTarget(
	root string,
	plan binddns.TreePlan,
	target string,
) (binddns.Generation, error) {
	current, err := binddns.RenderTree(root, plan)
	if err != nil || current.ID == target {
		return current, err
	}
	if previous, previousErr := binddns.RenderPreviousSecondaryTree(root, plan); previousErr == nil &&
		previous.ID == target {
		return previous, nil
	}
	return current, nil
}

func bindSecondaryOptionsFromJournal(layout bindHostLayout, journal dnsEngineSwitchJournal) (*binddns.Pairing, error) {
	if journal.PairRole != binddns.PairRoleSecondary {
		return nil, nil
	}
	manifest, err := switchJournalManifest(journal)
	if err != nil {
		return nil, err
	}
	plan, err := bindSwitchTreePlanWithPrimaryCatalogSerial(manifest, switchJournalBinding(journal), journal.PrimaryCatalogSerial)
	if err != nil {
		return nil, err
	}
	current, err := bindExpectedGenerationForTarget(layout.GenerationRoot, plan, journal.TargetGeneration)
	if err != nil {
		return nil, err
	}
	if current.ID == journal.TargetGeneration {
		return bindSecondaryOptionsFromReceipt(current.ReceiptValue.Pairing)
	}
	legacyID, err := binddns.LegacySecondaryGenerationID(layout.GenerationRoot, plan)
	if err != nil {
		return nil, err
	}
	if legacyID == journal.TargetGeneration {
		return nil, nil
	}
	return nil, errors.New("BIND secondary recovery target differs from both exact current and historical manifest generations")
}
