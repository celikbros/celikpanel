package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// Every fail-closed hold keeps its identity (errors.Is) and says its own cause,
// that every change on this server is held, and the next step. Only an
// ambiguous or published ledger write may say so.
func TestServiceMutationHoldTextNamesItsCause(t *testing.T) {
	for _, test := range []struct {
		name      string
		cause     error
		want      string
		ambiguous bool
	}{
		{
			name: "ledger write may have published",
			cause: fmt.Errorf("persist job: %w", &serviceMutationLedgerWriteError{
				state: serviceMutationLedgerWriteAmbiguous, err: errors.New("fsync failed"),
			}),
			want: "a write of the service mutation ledger may have been published", ambiguous: true,
		},
		{
			name: "dns switch decision",
			cause: &dnsSwitchFailClosedDecision{err: fmt.Errorf(
				"finalize active DNS engine switch: %w", errors.New("abort unproven"),
			)},
			want: "a DNS engine switch could not prove its outcome",
		},
		{
			name:  "unreadable journal at startup",
			cause: errors.New("read DNS engine switch journal: unexpected EOF"),
			want:  "an operation's durable record or host outcome could not be verified",
		},
		{
			name: "ledger write that was not published is not ambiguous",
			cause: &serviceMutationLedgerWriteError{
				state: serviceMutationLedgerWriteNotPublished, err: errors.New("rename refused"),
			},
			want: "an operation's durable record or host outcome could not be verified",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			manager := &serviceMutationManager{}
			for _, err := range []error{manager.poisonLocked(test.cause), manager.healthErrorLocked()} {
				if !errors.Is(err, errServiceMutationManagerPoisoned) {
					t.Fatalf("hold lost its stable identity: %v", err)
				}
				text := err.Error()
				if !strings.Contains(text, test.want) ||
					!strings.Contains(text, "Every change on this server is held") ||
					!strings.Contains(text, "Next step: the server owner") ||
					!strings.Contains(text, test.cause.Error()) {
					t.Fatalf("hold text = %q", text)
				}
				if strings.Contains(text, "may have been published") != test.ambiguous {
					t.Fatalf("hold text claims the wrong ledger state: %q", text)
				}
			}
		})
	}
}
