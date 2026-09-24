//go:build linux

package dnsenginerecovery

import "testing"

func TestStableQuiescedObservationFailsClosedOnChangedEvidenceOrUnit(t *testing.T) {
	seen := EvidenceObservation{
		EvidenceSHA256: "fingerprint", Status: EvidenceWorkerRecorded,
		RequestID: "request", Phase: "target-started",
		NativeUnits: []string{"named.service"},
	}
	units := []NativeUnitObservation{{
		Name: "named.service", LoadState: "loaded",
		ActiveState: "active", UnitFileState: "enabled",
	}}
	if !StableQuiescedObservation(seen, seen, units, units) {
		t.Fatal("same secured bytes and unit properties rejected")
	}
	changed := seen
	changed.EvidenceSHA256 = "different"
	if StableQuiescedObservation(seen, changed, units, units) {
		t.Fatal("changed evidence accepted")
	}
	changed = seen
	changed.Status = EvidenceLeaseExpired
	if StableQuiescedObservation(seen, changed, units, units) {
		t.Fatal("changed classification accepted")
	}
	changedUnit := append([]NativeUnitObservation(nil), units...)
	changedUnit[0].ActiveState = "inactive"
	if StableQuiescedObservation(seen, seen, units, changedUnit) {
		t.Fatal("changed native unit accepted")
	}
	seen.EvidenceSHA256 = ""
	if StableQuiescedObservation(seen, seen, units, units) {
		t.Fatal("missing exact-byte fingerprint accepted")
	}
}

func TestStableQuiescedSwitchEvidenceBindsJournalAndObservation(t *testing.T) {
	before := SwitchEvidence{Observation: EvidenceObservation{EvidenceSHA256: "verified"}}
	before.Journal.Phase = "target-started"
	units := []NativeUnitObservation{{Name: "named.service", LoadState: "loaded"}}
	if !StableQuiescedSwitchEvidence(before, before, units, units) {
		t.Fatal("stable paired evidence rejected")
	}
	changed := before
	changed.Journal.Phase = "target-verified"
	if StableQuiescedSwitchEvidence(before, changed, units, units) {
		t.Fatal("changed frozen journal accepted despite unchanged observation")
	}
}
