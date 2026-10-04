//go:build linux

package main

import (
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/servicemutationledger"
)

func TestMailEnrollmentBootSelectsOnlyRecordedPending(t *testing.T) {
	id := enrollmentLedgerID()
	empty := servicemutationledger.Ledger{Version: 1, Jobs: map[string]*servicemutationledger.ServiceMutationJob{}}
	if pending, err := mailEnrollmentBootPending(&empty, id.RequestID); err != nil || pending {
		t.Fatal("absence admitted", err)
	}
	for _, direction := range []string{"forward", "rollback"} {
		ledger, err := servicemutationledger.AdmitMailEnrollment(&empty, id, time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
		if direction == "rollback" {
			ledger, err = servicemutationledger.AdvanceMailEnrollment(&ledger, id, "rollback", time.Now().UTC())
			if err != nil {
				t.Fatal(err)
			}
		}
		before, _ := servicemutationledger.Encode(&ledger)
		if pending, err := mailEnrollmentBootPending(&ledger, id.RequestID); err != nil || !pending {
			t.Fatal(direction, err)
		}
		after, _ := servicemutationledger.Encode(&ledger)
		if string(before) != string(after) {
			t.Fatal("observation wrote state")
		}
		terminal := "published"
		if direction == "rollback" {
			terminal = "restored"
		}
		ledger, err = servicemutationledger.AdvanceMailEnrollment(&ledger, id, terminal, time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
		if pending, err := mailEnrollmentBootPending(&ledger, id.RequestID); err != nil || pending {
			t.Fatal("terminal replay", err)
		}
		ledger.Jobs[id.RequestID].Kind = "service"
		if _, err = mailEnrollmentBootPending(&ledger, id.RequestID); err == nil {
			t.Fatal("wrong identity consumed")
		}
	}
	for _, args := range [][]string{{"--boot-enrollment", id.RequestID}, {"--boot-enrollment-under-lock", id.RequestID}} {
		if err := validateIndependentMailEntry(args, 0, nil); err != nil {
			t.Fatal(err)
		}
		for _, bad := range [][]string{{args[0]}, {args[0], "../id"}, {args[0], id.RequestID, id.OwnerID, strings.Repeat("a", 64)}} {
			if validateIndependentMailEntry(bad, 0, nil) == nil {
				t.Fatal("boot admitted new intent")
			}
		}
	}
	if _, err := mailEnrollmentBootPending(nil, id.RequestID); err == nil {
		t.Fatal("missing ledger")
	}
}
