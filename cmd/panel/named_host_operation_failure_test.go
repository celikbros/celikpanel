package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// R-056. "The privileged host operation did not complete." is the right answer
// when the panel does not know what happened, and the wrong one when the host
// already said. These assert exactly that boundary.
func TestStandaloneAgentMutationFailureCarriesANamedReason(t *testing.T) {
	hostReason := errors.New("Postfix is required before a mail filter can be wired")
	named := namedHostOperationFailure(
		mailFilterWiringFailureSentence(hostReason.Error()),
		hostReason,
	)

	failure := standaloneAgentMutationFailure(named, nil)
	if failure == nil {
		t.Fatal("a failed call produced no failure")
	}
	if failure.Code != errCodeServiceOperationFailed {
		t.Fatalf("code = %q, want %q", failure.Code, errCodeServiceOperationFailed)
	}
	if failure.Message == "The privileged host operation did not complete." {
		t.Fatal("the ledger message still names nothing")
	}
	if !strings.Contains(failure.Message, "Postfix mail filter chain") {
		t.Fatalf("message does not name what was attempted: %q", failure.Message)
	}
	if !strings.Contains(failure.Message, hostReason.Error()) {
		t.Fatalf("message does not name why it stopped: %q", failure.Message)
	}
	if !errors.Is(failure.Cause, hostReason) {
		t.Fatalf("cause lost the host's own error: %v", failure.Cause)
	}
}

// A named reason is about the work, not about the lease. When the lease itself
// is what went wrong, the mutation framework's own words still win: the panel
// genuinely does not know whether the work ran.
func TestStandaloneAgentMutationFailureKeepsLeaseWordsWhenTheLeaseFailed(t *testing.T) {
	named := namedHostOperationFailure("this should not be shown", errors.New("host said no"))

	tests := []struct {
		name         string
		heartbeatErr error
		wantCode     string
		wantMessage  string
	}{
		{
			"lost lease",
			errAgentMutationLeaseLost,
			errCodeServiceOperationLeaseLost,
			"The privileged host operation lost its agent lease.",
		},
		{
			"terminal lease failure",
			errAgentMutationTerminalFailed,
			errCodeServiceOperationFailed,
			"The privileged host operation did not complete.",
		},
		{
			"unverifiable heartbeat",
			errors.New("heartbeat transport failed"),
			errCodeServiceOperationHeartbeatUncertain,
			"The privileged host operation's agent lease could not be verified.",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			failure := standaloneAgentMutationFailure(named, tc.heartbeatErr)
			if failure == nil {
				t.Fatal("no failure")
			}
			if failure.Code != tc.wantCode {
				t.Fatalf("code = %q, want %q", failure.Code, tc.wantCode)
			}
			if failure.Message != tc.wantMessage {
				t.Fatalf("message = %q, want %q", failure.Message, tc.wantMessage)
			}
		})
	}
}

// An unnamed failure is unchanged: this is an addition, not a rewrite of what
// every other host mutation says.
func TestStandaloneAgentMutationFailureUnnamedIsUnchanged(t *testing.T) {
	failure := standaloneAgentMutationFailure(errors.New("backend exploded"), nil)
	if failure == nil {
		t.Fatal("no failure")
	}
	if failure.Message != "The privileged host operation did not complete." {
		t.Fatalf("message = %q, want the generic one", failure.Message)
	}
}

func TestNamedHostOperationFailure(t *testing.T) {
	cause := errors.New("root cause")

	if got := namedHostOperationFailure("", cause); got != cause {
		t.Fatalf("an empty sentence changed the error: %v", got)
	}
	if got := namedHostOperationFailure("   ", cause); got != cause {
		t.Fatalf("a blank sentence changed the error: %v", got)
	}
	named := namedHostOperationFailure("a sentence", cause)
	if named.Error() != "a sentence" {
		t.Fatalf("Error() = %q, want the sentence", named.Error())
	}
	if !errors.Is(named, cause) {
		t.Fatal("the cause is no longer reachable")
	}
	if got := namedHostOperationSentence(fmt.Errorf("wrapped: %w", named)); got != "a sentence" {
		t.Fatalf("sentence through wrapping = %q", got)
	}
	if got := namedHostOperationSentence(cause); got != "" {
		t.Fatalf("an unnamed error produced a sentence: %q", got)
	}
	if got := namedHostOperationSentence(nil); got != "" {
		t.Fatalf("nil produced a sentence: %q", got)
	}
}

// R-056's own sentence: what was attempted, then why it stopped.
func TestMailFilterWiringFailureSentence(t *testing.T) {
	sentence := mailFilterWiringFailureSentence("Postfix is required before a mail filter can be wired")
	for _, want := range []string{
		"Postfix mail filter chain",
		"nothing on this server was changed",
		"Reason: Postfix is required before a mail filter can be wired",
	} {
		if !strings.Contains(sentence, want) {
			t.Fatalf("sentence %q does not contain %q", sentence, want)
		}
	}
}

// The host's words are part of a durable, operator-visible record, so they are
// bounded and cannot forge a line - and what is cut is the tail, never the
// sentence the panel authored.
func TestNamedHostOperationSentenceIsBoundedAndClean(t *testing.T) {
	noisy := "line one\nline two\rline three\x00tail"
	named := namedHostOperationFailure(noisy, errors.New("cause"))
	got := namedHostOperationSentence(named)
	if strings.ContainsAny(got, "\n\r\x00") {
		t.Fatalf("sentence still carries control characters: %q", got)
	}
	if !strings.Contains(got, "line one") {
		t.Fatalf("sentence lost its text: %q", got)
	}

	lead := "Composing this server's Postfix mail filter chain did not finish. Reason: "
	long := namedHostOperationFailure(lead+strings.Repeat("x", 900), errors.New("cause"))
	bounded := namedHostOperationSentence(long)
	if len([]rune(bounded)) > namedHostOperationSentenceLimit+3 {
		t.Fatalf("sentence is %d runes, want at most %d", len([]rune(bounded)), namedHostOperationSentenceLimit+3)
	}
	if !strings.HasPrefix(bounded, lead) {
		t.Fatalf("truncation ate the authored sentence: %q", bounded)
	}
}
