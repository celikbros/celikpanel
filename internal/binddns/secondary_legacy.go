package binddns

import "errors"

// LegacySecondaryGenerationID reconstructs only the immutable identity emitted
// before explicit secondary catalog zones and options-scoped subscriptions.
// It is for exact journal rollback matching; it does not bless the historical
// invalid configuration or make it eligible for activation/current ownership.
func LegacySecondaryGenerationID(root string, plan TreePlan) (string, error) {
	generation, err := RenderTree(root, plan)
	if err != nil {
		return "", err
	}
	receipt := generation.ReceiptValue
	if receipt.Pairing == nil || receipt.Pairing.Role != PairRoleSecondary {
		return "", errors.New("legacy BIND secondary identity requires a secondary pairing")
	}
	receipt.Pairing.SecondaryConfigVersion = 0
	return manifestGenerationID(receipt.EngineEpoch, root, receipt.Pairing, receipt.Zones)
}
