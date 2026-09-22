package main

import (
	"errors"
	"strings"
	"testing"
)

func TestMailRenewalSelectedBudgetNeverResetsOrBroadensOwnerIntent(t *testing.T) {
	for _, attempt := range []int{1, 2, 3, 4, 100, int(^uint(0) >> 1)} {
		job := &ServiceMutationJob{RequestID: testMutationRequestID, Attempt: attempt}
		before := *job
		err := admitMailRenewalSelectedRecovery(job, "")
		if (err == nil) != (attempt < 3) {
			t.Fatalf("attempt %d automatic admission: %v", attempt, err)
		}
		if attempt >= 3 && attempt < int(^uint(0)>>1) {
			var budget *mailRenewalRecoveryBudgetError
			if !errors.As(err, &budget) || !strings.Contains(err.Error(), "--retry-selected "+job.RequestID) {
				t.Fatal("missing owner continuation")
			}
		}
		err = admitMailRenewalSelectedRecovery(job, job.RequestID)
		if (err == nil) != (attempt < int(^uint(0)>>1)) {
			t.Fatalf("owner attempt %d: %v", attempt, err)
		}
		if admitMailRenewalSelectedRecovery(job, testMutationSecondRequestID) == nil {
			t.Fatal("another operation accepted")
		}
		if *job != before {
			t.Fatal("read-only admission changed durable attempt evidence")
		}
	}
	for _, job := range []*ServiceMutationJob{nil, {RequestID: "bad", Attempt: 1}, {RequestID: testMutationRequestID, Attempt: 0}} {
		if admitMailRenewalSelectedRecovery(job, "") == nil {
			t.Fatal("malformed budget accepted")
		}
	}
}
