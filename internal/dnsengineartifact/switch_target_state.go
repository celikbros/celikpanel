package dnsengineartifact

import "github.com/alicelik/celikpanel/internal/transport"

// ExactSwitchTargetStateV1 compares a decoded current state receipt with the
// frozen switch target. Historical paired target receipts had an empty pair
// tuple; that exception applies only after target verification or commit. A
// matching receipt alone does not prove the generation tree or running DNS.
func ExactSwitchTargetStateV1(state StateV1, journal SwitchJournalV1) bool {
	legacyState := state.PairRole == "" && state.PairLocalIP == "" &&
		state.PairPeerIP == "" && state.PrimaryCatalogSerial == 0
	legacyTarget := legacyState &&
		(journal.Phase == SwitchPhaseTargetVerified ||
			journal.Phase == SwitchPhaseCommitted) &&
		(journal.PairRole == transport.DNSPairRolePrimary ||
			journal.PairRole == transport.DNSPairRoleSecondary)
	pairRoleMatches := state.PairRole == journal.PairRole || legacyTarget
	pairAddressesMatch := state.PairLocalIP == journal.LocalIP &&
		state.PairPeerIP == journal.PeerIP
	if legacyTarget || (state.PairRole == "" && state.PrimaryCatalogSerial == 0) {
		pairAddressesMatch = state.PairLocalIP == "" && state.PairPeerIP == ""
	}
	catalogSerialMatches := state.PrimaryCatalogSerial == journal.PrimaryCatalogSerial ||
		(legacyTarget && state.PrimaryCatalogSerial == 0)
	// A reinstall repairs the same tenure and writes its original switch mode.
	journalTenureMode := journal.Mode
	if journal.Mode == transport.DNSEngineSwitchModeReinstall {
		journalTenureMode = transport.DNSEngineSwitchModeSwitch
	}
	if state.Schema != StateSchemaV1 || state.Engine != journal.TargetEngine ||
		state.Mode != journalTenureMode ||
		state.EngineEpoch != journal.TargetEpoch || state.SourceRevision != journal.SourceRevision ||
		state.ManifestQualifier != journal.ManifestQualifier ||
		!pairRoleMatches || !pairAddressesMatch || !catalogSerialMatches ||
		state.MutationRequestID != journal.MutationRequestID ||
		state.MutationOwnerID != journal.MutationOwnerID {
		return false
	}
	if journal.TargetEngine == transport.DNSEngineBIND {
		return state.Generation == journal.TargetGeneration
	}
	return state.Generation == ""
}
