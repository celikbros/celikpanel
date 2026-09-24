package dnsengineartifact

// ProveFrozenSwitchSourceOwnership applies the frozen-source comparison to a
// validated ownership receipt. The caller must obtain it from the source
// engine's trusted per-engine path. Exact equality is necessary for the
// historical inverse, but does not prove native DNS or mutation authority.
func ProveFrozenSwitchSourceOwnership(journal SwitchJournalV1, ownership StateV1, ownershipExists bool) (bool, error) {
	return ProveFrozenSwitchSourceState(journal, ownership, ownershipExists)
}
