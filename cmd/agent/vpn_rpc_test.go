package main

import (
	"errors"
	"strings"
	"testing"
)

func TestRunVPNCommitRollbackAttemptsLiveAndDurableRecovery(t *testing.T) {
	liveCalled := false
	diskCalled := false
	err := runVPNCommitRollback(
		true,
		func() error {
			liveCalled = true
			return errors.New("live rollback failed")
		},
		func() error {
			diskCalled = true
			return errors.New("durable rollback failed")
		},
	)
	if !liveCalled || !diskCalled {
		t.Fatalf("rollback calls live=%v durable=%v, want both", liveCalled, diskCalled)
	}
	if err == nil ||
		!strings.Contains(err.Error(), "live rollback failed") ||
		!strings.Contains(err.Error(), "durable rollback failed") {
		t.Fatalf("rollback error=%v, want both failures", err)
	}
}

func TestRunVPNCommitRollbackSkipsLiveRecoveryForDownInterface(t *testing.T) {
	liveCalled := false
	diskCalled := false
	err := runVPNCommitRollback(
		false,
		func() error {
			liveCalled = true
			return nil
		},
		func() error {
			diskCalled = true
			return nil
		},
	)
	if err != nil {
		t.Fatalf("rollback error: %v", err)
	}
	if liveCalled {
		t.Fatal("live rollback ran for an interface that was originally down")
	}
	if !diskCalled {
		t.Fatal("durable rollback did not run")
	}
}
