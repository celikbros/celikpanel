package hostcmd

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// exitErrorWithStderr builds the value os/exec hands back from Output(): an
// exit status with the command's explanation tucked inside it, where R-054
// found it and where every layer above had stopped looking.
//
// exitErrorWithStderr, os/exec'in Output()'tan geri verdigi degeri kurar.
func exitErrorWithStderr(stderr string) error {
	return &exec.ExitError{Stderr: []byte(stderr)}
}

// TestTheReasonSurvivesAnEmptyOutput is the defect itself, in one assertion. A
// command that wrote nothing to stdout and everything to stderr used to reach
// an operator as "exit status 1".
//
// TestTheReasonSurvivesAnEmptyOutput kusurun kendisidir.
func TestTheReasonSurvivesAnEmptyOutput(t *testing.T) {
	err := exitErrorWithStderr("Error: Could not process rule: No such file or directory")

	if got := Reason(nil, err); !strings.Contains(got, "Could not process rule") {
		t.Fatalf("Reason discarded the only explanation there was: %q", got)
	}
	if got := Diagnostic(nil, err); !strings.Contains(got, "Could not process rule") {
		t.Fatalf("Diagnostic discarded the only explanation there was: %q", got)
	}
}

// TestDiagnosticIsOneLine: this text ends up inside a JSON error a browser
// renders, and a tool points at the offending line with its own newlines and
// carets.
func TestDiagnosticIsOneLine(t *testing.T) {
	out := []byte("nft: line 3\n  ^^^^\n  bad rule\n")
	got := Diagnostic(out, errors.New("exit status 1"))
	if strings.ContainsAny(got, "\n\r\t") {
		t.Fatalf("the diagnostic carried control characters: %q", got)
	}
	if !strings.Contains(got, "bad rule") {
		t.Fatalf("flattening lost part of the diagnostic: %q", got)
	}
}

// TestReasonPrefersTheOutputAndFallsBackInOrder: the exit error is the last
// resort, never the first. That ordering is the whole of R-054.
func TestReasonPrefersTheOutputAndFallsBackInOrder(t *testing.T) {
	withBoth := exitErrorWithStderr("from stderr")
	if got := Reason([]byte("from stdout"), withBoth); got != "from stdout" {
		t.Fatalf("Reason = %q, want the output", got)
	}
	if got := Reason(nil, withBoth); got != "from stderr" {
		t.Fatalf("Reason = %q, want the hidden stderr", got)
	}
	if got := Reason(nil, errors.New("exit status 1")); got != "exit status 1" {
		t.Fatalf("Reason = %q, want the exit error as the last resort", got)
	}
	if got := Reason(nil, nil); got != "" {
		t.Fatalf("Reason invented %q for a command that did not fail", got)
	}
}

// TestSafetyIsTheDefault: a caller that says nothing about secrets gets the
// classified path, and the command's own words do not travel.
//
// TestSafetyIsTheDefault: gizlilik hakkinda bir sey soylemeyen cagiran
// siniflandirilmis yolu alir.
func TestSafetyIsTheDefault(t *testing.T) {
	leaky := []byte("ERROR: syntax error at or near \"WITH\"\nLINE 1: CREATE USER \"bob\" WITH PASSWORD 'hunter2';")
	cause := errors.New("exit status 1")

	failure := Fail("failed to create user", leaky, cause, nil)
	if strings.Contains(failure.Error(), "hunter2") {
		t.Fatalf("the password travelled: %q", failure.Error())
	}
	if !strings.Contains(failure.Error(), "read") {
		t.Fatalf("a withheld reason did not say it had been read: %q", failure.Error())
	}
	if !errors.Is(failure, cause) {
		t.Fatal("the cause is no longer reachable")
	}
}

// TestAClassifierCarriesTheMeaningAndNotTheText is the discipline R-053
// established: the meaning survives, the statement does not.
func TestAClassifierCarriesTheMeaningAndNotTheText(t *testing.T) {
	leaky := []byte("LINE 1: CREATE USER \"bob\" WITH PASSWORD 'hunter2';\nERROR: role already exists")
	classify := func(text string) string {
		if strings.Contains(text, "already exists") {
			return "the role is already there"
		}
		return ""
	}
	failure := Fail("failed to create user", leaky, errors.New("exit status 1"), classify)
	if got := failure.Error(); got != "failed to create user: the role is already there" {
		t.Fatalf("message = %q", got)
	}
	if strings.Contains(failure.Error(), "hunter2") {
		t.Fatalf("the password travelled: %q", failure.Error())
	}
	if got := ClassOf(failure); got != "the role is already there" {
		t.Fatalf("ClassOf = %q", got)
	}
	wrapped := errors.Join(errors.New("create physical database user"), failure)
	if got := ClassOf(wrapped); got != "the role is already there" {
		t.Fatalf("the classification did not survive wrapping: %q", got)
	}
}

// TestAskingForTheRawTextRequiresSayingWhy: the raw-forwarding path is the one
// that must be asked for, and an unjustified ask fails closed rather than
// leaking.
//
// TestAskingForTheRawTextRequiresSayingWhy: ham iletim yolu istenmesi gereken
// yoldur ve gerekcesiz bir istek sizdirmak yerine kapali duser.
func TestAskingForTheRawTextRequiresSayingWhy(t *testing.T) {
	out := []byte("nft: could not process rule")
	cause := errors.New("exit status 1")

	justified := FailVerbatim("nft apply failed", out, cause, "nft only ever sees a ruleset this agent composed")
	if !strings.Contains(justified.Error(), "could not process rule") {
		t.Fatalf("a justified disclosure lost the words: %q", justified.Error())
	}
	var failure *Failure
	if !errors.As(justified, &failure) {
		t.Fatal("FailVerbatim did not produce a *Failure")
	}
	if disclosed, because := failure.Disclosed(); !disclosed || because == "" {
		t.Fatalf("the disclosure did not keep its reason: %v %q", disclosed, because)
	}

	unjustified := FailVerbatim("nft apply failed", out, cause, "   ")
	if strings.Contains(unjustified.Error(), "could not process rule") {
		t.Fatalf("an unjustified disclosure leaked the words: %q", unjustified.Error())
	}
	if got := Verbatim(out, cause, ""); got != "" {
		t.Fatalf("Verbatim without a reason returned %q", got)
	}
}

// TestNothingIsInventedForASuccess: neither constructor manufactures a failure
// where there was none.
func TestNothingIsInventedForASuccess(t *testing.T) {
	if got := Fail("prefix", []byte("out"), nil, nil); got != nil {
		t.Fatalf("Fail invented %v", got)
	}
	if got := FailVerbatim("prefix", []byte("out"), nil, "because"); got != nil {
		t.Fatalf("FailVerbatim invented %v", got)
	}
}

// TestTheOperatorsSentenceSurvivesTheBound is the rule the first live R-054 run
// taught: the instruction goes first, because the bound cuts the tail.
func TestTheOperatorsSentenceSurvivesTheBound(t *testing.T) {
	instruction := "Restart this server, then turn the firewall on again."
	noisy := strings.Repeat("Error: Could not process rule: No such file or directory. ", 20)

	message := OperatorFirst(instruction, "nft apply failed", noisy)
	if !strings.HasPrefix(message, instruction) {
		t.Fatalf("the diagnostic came first: %q", message)
	}
	if bounded := Bounded(message, 400); !strings.Contains(bounded, instruction) {
		t.Fatalf("the bound cut the instruction: %q", bounded)
	}
	if got := OperatorFirst("", "nft apply failed", ""); got != "nft apply failed: unknown" {
		t.Fatalf("a failure with no words said %q", got)
	}
	if got := Bounded("   ", 400); got != "unknown" {
		t.Fatalf("a blank reason became %q", got)
	}
	long := strings.Repeat("x", 1200)
	if got := Bounded(long, 400); len(got) != 403 || !strings.HasSuffix(got, "...") {
		t.Fatalf("the bound did not hold: len=%d", len(got))
	}
	if got := Bounded(long, 0); got != long {
		t.Fatal("an unbounded reason was cut anyway")
	}
}
