package main

import (
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
)

func TestV4AgentJournalWriterRequiresEnableIntentBeforeTargetStart(t *testing.T) {
	intent := testV4PDNSTargetJournal(t)
	stage, err := dnsJournalPolicy().AttachPDNSTargetCandidateV4(intent, *intent.PDNSTargetPlan.Candidate)
	if err != nil {
		t.Fatal(err)
	}
	stage.Phase = dnsengineartifact.SwitchPhaseSourceStopped
	for _, tc := range []struct {
		name, next string
		allowed    bool
	}{
		{"skip-intent", dnsengineartifact.SwitchPhaseTargetStarted, false},
		{"durable-intent", dnsengineartifact.SwitchPhaseTargetEnableIntent, true},
		{"generic-rollback", dnsengineartifact.SwitchPhaseRollingBack, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current := stage
			persists := 0
			next := current
			next.Phase = tc.next
			err := writeDNSEngineSwitchJournalWithOps(next,
				func(encoded []byte) error {
					persists++
					decoded, err := dnsJournalPolicy().DecodeSwitchJournal(encoded)
					if err == nil {
						current = decoded
					}
					return err
				},
				func() (dnsEngineSwitchJournal, bool, error) { return current, true, nil },
				nil,
			)
			if tc.allowed {
				if err != nil || persists != 1 || current.Phase != tc.next {
					t.Fatalf("valid forward checkpoint failed: err=%v writes=%d phase=%s", err, persists, current.Phase)
				}
			} else if err == nil || persists != 0 || !strings.Contains(err.Error(), "v4") {
				t.Fatalf("unsafe forward checkpoint was published: err=%v writes=%d", err, persists)
			}
		})
	}
}
