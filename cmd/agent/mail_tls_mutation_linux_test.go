//go:build linux

package main

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestReconcileMailTLSMutationCanceledWhileBusyDoesNotRun(t *testing.T) {
	manager, _ := newMutationTestManager(t)
	manager.leaseDuration = time.Minute
	installGlobalMutationTestManager(t, manager)
	beginMutationTestJob(t, manager)

	previousLookup := lookupMailTLSCommand
	lookupCalled := false
	lookupMailTLSCommand = func(name string) (string, error) {
		lookupCalled = true
		return "", fmt.Errorf("unexpected lookup for %s", name)
	}
	t.Cleanup(func() { lookupMailTLSCommand = previousLookup })

	mailMutex.Lock()
	locked := true
	t.Cleanup(func() {
		if locked {
			mailMutex.Unlock()
		}
	})

	type callResult struct {
		response SecureMailTLSResponse
		err      error
	}
	result := make(chan callResult, 1)
	go func() {
		var response SecureMailTLSResponse
		err := (&Agent{}).ReconcileMailTLSMutation(
			&ReconcileMailTLSMutationRequest{
				ServiceMutationBinding: ServiceMutationBinding{
					MutationRequestID: testMutationRequestID,
					MutationOwnerID:   testMutationOwnerID,
				},
				Myhostname: "mail.example.test",
			},
			&response,
		)
		result <- callResult{response: response, err: err}
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		manager.mu.Lock()
		steps := 0
		if manager.active != nil {
			steps = manager.active.steps
		}
		manager.mu.Unlock()
		if steps == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("mail TLS reconciliation did not acquire its durable mutation step")
		}
		time.Sleep(5 * time.Millisecond)
	}

	if _, err := manager.cancelJob(&ServiceMutationCancelRequest{
		RequestID:     testMutationRequestID,
		ExpectedOwner: testMutationOwnerID,
		Reason:        "test_cancel_while_waiting_for_mail_lock",
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case call := <-result:
		if call.err != nil || call.response.Configured ||
			!strings.Contains(call.response.Error, "service mutation lease expired") {
			t.Fatalf("response=%+v err=%v", call.response, call.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceled mail TLS reconciliation remained blocked on mailMutex")
	}

	manager.mu.Lock()
	steps := 0
	if manager.active != nil {
		steps = manager.active.steps
	}
	manager.mu.Unlock()
	if steps != 0 {
		t.Fatalf("canceled mail TLS reconciliation leaked %d active steps", steps)
	}
	if lookupCalled {
		t.Fatal("canceled busy mail TLS reconciliation reached host preflight")
	}

	mailMutex.Unlock()
	locked = false
	if !mailMutex.TryLock() {
		t.Fatal("canceled busy mail TLS reconciliation wedged mailMutex")
	}
	mailMutex.Unlock()
}
