package dnsengineartifact

import "testing"

func TestFrozenSwitchSourceOwnershipHistoricalAndSeparatedDocuments(t *testing.T) {
	policy := journalTestPolicy()
	journal, err := policy.DecodeSwitchJournal(journalFixture(t, "alpha81-pdns-switch"))
	if err != nil {
		t.Fatal(err)
	}
	source, exists, err := SourceStateFromSwitchJournal(journal)
	if err != nil || !exists {
		t.Fatalf("historical source: %+v, %v, %v", source, exists, err)
	}
	for _, encode := range []func(StateV1) ([]byte, error){CanonicalV1, CanonicalOwnershipDocumentV2} {
		raw, err := encode(source)
		if err != nil {
			t.Fatal(err)
		}
		ownership, _, err := DecodeOwnershipDocument(raw)
		if err != nil {
			t.Fatal(err)
		}
		matched, err := ProveFrozenSwitchSourceOwnership(journal, ownership, true)
		if err != nil || !matched {
			t.Fatalf("exact source ownership refused: %v, %v", matched, err)
		}
	}
	if matched, err := ProveFrozenSwitchSourceOwnership(journal, StateV1{}, false); err != nil || matched {
		t.Fatalf("missing source ownership accepted: %v, %v", matched, err)
	}
	foreign := source
	foreign.MutationOwnerID = "ffffffffffffffffffffffffffffffff"
	if matched, err := ProveFrozenSwitchSourceOwnership(journal, foreign, true); err != nil || matched {
		t.Fatalf("foreign ownership accepted: %v, %v", matched, err)
	}
	if _, err := ProveFrozenSwitchSourceOwnership(journal, StateV1{}, true); err == nil {
		t.Fatal("invalid ownership accepted")
	}
	initial, err := policy.DecodeSwitchJournal(journalFixture(t, "alpha81-bind"))
	if err != nil {
		t.Fatal(err)
	}
	if matched, err := ProveFrozenSwitchSourceOwnership(initial, StateV1{}, false); err != nil || !matched {
		t.Fatalf("initial switch mutual absence refused: %v, %v", matched, err)
	}
	if matched, err := ProveFrozenSwitchSourceOwnership(initial, source, true); err != nil || matched {
		t.Fatalf("unexpected initial source ownership accepted: %v, %v", matched, err)
	}
}
