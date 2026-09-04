package main

import (
	"os"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

// The host path and the signed-update path both decide whether a surviving
// ownership receipt may stand for a committed transaction. They must decide it
// identically: the signed-update walker runs on every release, so a rule that
// holds in one and not the other means an ordinary engine switch-back succeeds
// and the host's next update dies. Both must reach the decision through the one
// shared function; a hand-written copy is the defect.
func TestBothProvenanceCheckersShareTheOwnershipRule(t *testing.T) {
	for _, file := range []string{
		"dns_engine_ownership.go",
		"dns_engine_bind_update.go",
	} {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		source := string(raw)
		if !strings.Contains(source, "acceptableCommittedDNSEngineOwnership(ownership, state, journal)") {
			t.Errorf(
				"%s does not reach the ownership decision through the shared rule; "+
					"a second copy will drift the way this one already did",
				file,
			)
		}
	}

	// The rule's text lives once. Two copies of the sentence mean two copies of
	// the rule, whatever the call sites look like.
	raw, err := os.ReadFile("dns_engine_bind_update.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "differs from its exact active state") {
		t.Error("the signed-update path re-states the rule instead of calling it")
	}
}

// A receipt at an older epoch of the same engine is what returning to a
// previously used engine always leaves behind. At the gate it is accepted; at
// the publish point it is overwritten. Both are "the current state has not been
// published yet" — neither is a contradiction.
func TestSupersededOwnershipIsAcceptedAtTheGate(t *testing.T) {
	state := dnsEngineStateReceipt{
		Schema:            dnsEngineStateSchema,
		Mode:              transport.DNSEngineSwitchModeSwitch,
		Engine:            transport.DNSEngineBIND,
		EngineEpoch:       3,
		Generation:        strings.Repeat("c", 64),
		SourceRevision:    9,
		ManifestQualifier: "dns-engine-switch/v1:sha256:" + strings.Repeat("a", 64),
		MutationRequestID: "11111111111111111111111111111111",
		MutationOwnerID:   "22222222222222222222222222222222",
	}
	stale := dnsEngineStateReceipt{
		Schema:            dnsEngineStateSchema,
		Mode:              transport.DNSEngineSwitchModeSwitch,
		Engine:            transport.DNSEngineBIND,
		EngineEpoch:       1,
		Generation:        strings.Repeat("d", 64),
		SourceRevision:    2,
		ManifestQualifier: "dns-engine-switch/v1:sha256:" + strings.Repeat("b", 64),
		MutationRequestID: "33333333333333333333333333333333",
		MutationOwnerID:   "44444444444444444444444444444444",
	}

	if err := acceptableCommittedDNSEngineOwnership(stale, state, dnsEngineSwitchJournal{}); err != nil {
		t.Fatalf("an older epoch of the same engine is provenance, not a contradiction: %v", err)
	}
	if !supersededDNSEngineOwnership(stale, state) {
		t.Fatal("the publish point must recognise the same receipt as overwritable")
	}

	// A receipt claiming the same epoch with different content is two states
	// claiming one epoch — a real contradiction, and it still refuses.
	conflicting := stale
	conflicting.EngineEpoch = state.EngineEpoch
	if err := acceptableCommittedDNSEngineOwnership(conflicting, state, dnsEngineSwitchJournal{}); err == nil {
		t.Fatal("an equal-epoch receipt with different content must refuse")
	}
	if supersededDNSEngineOwnership(conflicting, state) {
		t.Fatal("an equal-epoch receipt must never be silently overwritten")
	}

	// A receipt from ahead of the committed state is not ours to overwrite.
	ahead := stale
	ahead.EngineEpoch = state.EngineEpoch + 1
	if err := acceptableCommittedDNSEngineOwnership(ahead, state, dnsEngineSwitchJournal{}); err == nil {
		t.Fatal("a receipt ahead of the committed state must refuse")
	}

	// Another engine's receipt is never this engine's provenance.
	other := stale
	other.Engine = transport.DNSEnginePowerDNS
	if err := acceptableCommittedDNSEngineOwnership(other, state, dnsEngineSwitchJournal{}); err == nil {
		t.Fatal("another engine's receipt must refuse")
	}
}
