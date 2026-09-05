package main

import (
	"errors"
	"strings"
	"testing"
)

// The rule that decides whether a host is handed back is now in one place, so
// it is tested in one place. These tests are about the rule, not about any
// path: each path keeps its own tests for the evidence it gathers.
// Makinenin geri verilip verilmeyecegine karar veren kural artik tek yerdedir;
// bu yuzden tek yerde test edilir.

// TestHostMutationRuleReleasesOnlyAProvenHost is the whole safety property in
// one assertion: two outcomes may end a plan, and no other may - including any
// value that is not one of the four, which is what an unclassifiable outcome
// looks like once it reaches here.
func TestHostMutationRuleReleasesOnlyAProvenHost(t *testing.T) {
	allowed := map[hostMutationOutcome]bool{
		hostMutationUntouched: true,
		hostMutationRestored:  true,
	}
	for _, outcome := range []hostMutationOutcome{
		hostMutationUntouched,
		hostMutationRestored,
		hostMutationConverged,
		hostMutationAmbiguous,
		hostMutationOutcome(42),
		hostMutationOutcome(-1),
	} {
		if got := outcome.mayEndPlan(); got != allowed[outcome] {
			t.Fatalf("outcome %d may end a plan = %v, want %v", outcome, got, allowed[outcome])
		}
	}
}

func testFailureVoice() hostMutationFailureVoice {
	return hostMutationFailureVoice{
		untouchedCode: "test_failed_before_host_change",
		restoredCode:  "test_failed_host_restored",
		untouchedLead: "The committed change was abandoned without changing anything.",
		restoredLead:  "The committed change was put back and proved.",
		residue:       "Nothing else was undone.",
		interrupted:   "An earlier attempt was interrupted.",
	}
}

// TestHostMutationCleanFailureTextRefusesAnUnprovenOutcome: the classifier is
// the only door to releasing the ledger, and a host that may be half changed
// may not pass through it. Neither may a plan that actually converged.
func TestHostMutationCleanFailureTextRefusesAnUnprovenOutcome(t *testing.T) {
	voice := testFailureVoice()
	for _, outcome := range []hostMutationOutcome{
		hostMutationAmbiguous,
		hostMutationConverged,
		hostMutationOutcome(42),
	} {
		code, message, clean := voice.cleanFailureText(outcome, errors.New("cause"), true)
		if clean || code != "" || message != "" {
			t.Fatalf(
				"outcome %d was offered a clean failure: code=%q message=%q clean=%v",
				outcome, code, message, clean,
			)
		}
	}
}

// TestHostMutationCleanFailureTextSaysWhatHappenedThenWhy keeps the order the
// product learned twice: the operator's sentence first, what was not undone
// next, and the technical reason last.
func TestHostMutationCleanFailureTextSaysWhatHappenedThenWhy(t *testing.T) {
	voice := testFailureVoice()
	code, message, clean := voice.cleanFailureText(
		hostMutationUntouched, errors.New("nft said no"), false,
	)
	if !clean || code != voice.untouchedCode {
		t.Fatalf("untouched host: code=%q clean=%v", code, clean)
	}
	want := voice.untouchedLead + " " + voice.residue + " Reason: nft said no"
	if message != want {
		t.Fatalf("untouched message = %q, want %q", message, want)
	}
	if strings.Contains(message, voice.interrupted) {
		t.Fatalf("a live failure warned about an interrupted predecessor: %q", message)
	}
	code, message, clean = voice.cleanFailureText(
		hostMutationRestored, errors.New("kernel took nothing"), true,
	)
	if !clean || code != voice.restoredCode {
		t.Fatalf("restored host: code=%q clean=%v", code, clean)
	}
	want = voice.restoredLead + " " + voice.interrupted + " " + voice.residue +
		" Reason: kernel took nothing"
	if message != want {
		t.Fatalf("restored message = %q, want %q", message, want)
	}
}

// TestHostMutationReasonIsBoundedAndNeverEmpty: the reason is recorded durably
// and shown to an operator, so it is bounded - and a failure with nothing to
// say still says something rather than trailing off.
func TestHostMutationReasonIsBoundedAndNeverEmpty(t *testing.T) {
	if got := boundedHostMutationReason(nil); got != "unknown" {
		t.Fatalf("absent cause = %q, want unknown", got)
	}
	if got := boundedHostMutationReason(errors.New("   ")); got != "unknown" {
		t.Fatalf("blank cause = %q, want unknown", got)
	}
	long := strings.Repeat("x", hostMutationFailureReasonLimit*3)
	got := boundedHostMutationReason(errors.New(long))
	if len(got) != hostMutationFailureReasonLimit+3 || !strings.HasSuffix(got, "...") {
		t.Fatalf("long cause was not bounded: len=%d suffix=%q", len(got), got[len(got)-3:])
	}
}

// TestOperatorSentenceComesBeforeTheDiagnostic is the lesson that was learned
// on a real machine and then needed a second time: a tool's diagnostic is long
// enough to push everything after it past the reason limit, so the one
// instruction the operator has to act on goes first and survives the bound.
func TestOperatorSentenceComesBeforeTheDiagnostic(t *testing.T) {
	instruction := "Restart this server, then try again."
	noisy := strings.Repeat("Error: Could not process rule: No such file or directory. ", 20)
	message := operatorFirstFailureSentence(instruction, "apply failed", noisy)
	if !strings.HasPrefix(message, instruction) {
		t.Fatalf("the diagnostic came first: %q", message)
	}
	bounded := boundedHostMutationReason(errors.New(message))
	if !strings.Contains(bounded, instruction) {
		t.Fatalf("the bounded reason lost the operator's instruction: %q", bounded)
	}
	if got := operatorFirstFailureSentence("", "apply failed", ""); got != "apply failed: unknown" {
		t.Fatalf("a failure with no words said %q", got)
	}
}

// TestEveryHostMutationPathSpeaksThroughTheOneRule: the three paths that ask
// the question route their own words through the shared classifier, so a path
// cannot quietly grow a second rule. If a fourth path is added, it belongs
// here.
func TestEveryHostMutationPathSpeaksThroughTheOneRule(t *testing.T) {
	paths := []struct {
		name string
		text func(hostMutationOutcome, error, bool) (string, string, bool)
		code string
	}{
		{"firewall", firewallApplyCleanFailureText, firewallApplyFailedUntouchedCode},
		{"mail TLS", mailTLSSyncCleanFailureText, mailTLSSyncFailedUntouchedCode},
		{"VPN peer sync", vpnPeerSyncCleanFailureText, vpnPeerSyncFailedUntouchedCode},
	}
	for _, path := range paths {
		if _, _, clean := path.text(hostMutationAmbiguous, errors.New("half applied"), false); clean {
			t.Fatalf("%s offered an ambiguous host a clean failure", path.name)
		}
		if _, _, clean := path.text(hostMutationConverged, nil, false); clean {
			t.Fatalf("%s offered a converged plan a clean failure", path.name)
		}
		code, message, clean := path.text(
			hostMutationUntouched, errors.New("this machine cannot"), true,
		)
		if !clean || code != path.code {
			t.Fatalf("%s untouched host: code=%q clean=%v", path.name, code, clean)
		}
		if !strings.Contains(message, "Reason: this machine cannot") {
			t.Fatalf("%s message lost its reason: %q", path.name, message)
		}
		if !strings.Contains(message, "An earlier attempt was interrupted") {
			t.Fatalf("%s message lost the interrupted-attempt warning: %q", path.name, message)
		}
	}
}
