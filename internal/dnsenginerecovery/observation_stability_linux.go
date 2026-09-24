//go:build linux

package dnsenginerecovery

import "reflect"

// StableQuiescedObservation checks whether two secured evidence reads and two
// native unit queries agreed while the caller held its host locks. The locks
// do not bind owner edits outside CelikPanel, and agreement is not authority
// to run an inverse or restart DNS.
func StableQuiescedObservation(before, after EvidenceObservation, beforeUnits, afterUnits []NativeUnitObservation) bool {
	return before.EvidenceSHA256 != "" &&
		before.EvidenceSHA256 == after.EvidenceSHA256 &&
		len(beforeUnits) != 0 &&
		reflect.DeepEqual(before, after) &&
		reflect.DeepEqual(beforeUnits, afterUnits)
}
