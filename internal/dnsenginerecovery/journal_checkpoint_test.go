package dnsenginerecovery

import (
	"errors"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
)

func TestRemoveJournalCheckpointRefusesForeignOrChangedJournal(t *testing.T) {
	policy, expected, _, _ := inspectionFixture(t)
	changed := expected
	changed.MutationOwnerID = strings.Repeat("f", 32)
	calls := 0
	err := RemoveJournalCheckpoint(policy, expected, JournalRemovalOps{
		Read:   func() (dnsengineartifact.SwitchJournalV1, bool, error) { return changed, true, nil },
		Remove: func() error { calls++; return nil },
	})
	if err == nil || calls != 0 {
		t.Fatalf("changed journal removed: calls=%d err=%v", calls, err)
	}
}

func TestRemoveJournalCheckpointResolvesUncertainUnlinkOnlyWhenAbsent(t *testing.T) {
	policy, expected, _, _ := inspectionFixture(t)
	for _, test := range []struct {
		name         string
		afterPresent bool
		wantError    bool
	}{
		{"gone", false, false}, {"retained", true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			reads, removes := 0, 0
			err := RemoveJournalCheckpoint(policy, expected, JournalRemovalOps{
				Read: func() (dnsengineartifact.SwitchJournalV1, bool, error) {
					reads++
					if reads == 1 {
						return expected, true, nil
					}
					return expected, test.afterPresent, nil
				},
				Remove: func() error { removes++; return errors.New("unlink outcome unknown") },
			})
			if (err != nil) != test.wantError || reads != 2 || removes != 1 {
				t.Fatalf("uncertain unlink: reads=%d removes=%d err=%v", reads, removes, err)
			}
		})
	}
}
