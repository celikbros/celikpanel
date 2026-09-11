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
	if receipt.SecondaryConfigVersion != 1 {
		return nil, errors.New("BIND secondary receipt does not prove the current catalog subscription policy")
	}
	return bindSecondaryOptionsPairing(receipt.Role, receipt.LocalIP, receipt.LocalNS, receipt.PeerIP, receipt.PeerNS), nil
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
	current, err := binddns.RenderTree(layout.GenerationRoot, plan)
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
