//go:build linux

package main

import (
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
)

func TestLegacyReconcileMailTLSMutationReturnsBeforeLeaseOrMailLock(t *testing.T) {
	manager, _ := newMutationTestManager(t)
	manager.leaseDuration = time.Minute
	installGlobalMutationTestManager(t, manager)
	beginMutationTestJobWithIdentity(t, manager, "mail_profile_install", "core-mail", "")

	previousLookup := lookupMailTLSCommand
	lookupCalled := false
	lookupMailTLSCommand = func(name string) (string, error) {
		lookupCalled = true
		return name, nil
	}
	t.Cleanup(func() { lookupMailTLSCommand = previousLookup })

	mailMutex.Lock()
	defer mailMutex.Unlock()
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
	if err != nil || response.Configured ||
		!strings.Contains(response.Error, "legacy mail TLS mutation is unsupported") {
		t.Fatalf("response=%+v err=%v", response, err)
	}

	manager.mu.Lock()
	steps := 0
	if manager.active != nil {
		steps = manager.active.steps
	}
	manager.mu.Unlock()
	if steps != 0 {
		t.Fatalf("legacy mail TLS reconciliation acquired %d mutation steps", steps)
	}
	if lookupCalled {
		t.Fatal("legacy mail TLS reconciliation reached host preflight")
	}
}

func TestSyncMailTLSV2RejectsPanelRootQualifierWhenAgentTrustsAnotherRoot(t *testing.T) {
	panelCommitment, err := mutationpayload.CanonicalMailTLSSync(
		"/etc/ssl/celikpanel",
		"mail.panel.test",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	manager, _ := newMutationTestManager(t)
	manager.leaseDuration = time.Minute
	installGlobalMutationTestManager(t, manager)
	beginMutationTestJobWithIdentity(
		t,
		manager,
		"mail_tls_sync",
		"mail-tls",
		panelCommitment.Qualifier,
	)
	t.Setenv("CELIKPANEL_CUSTOM_CERT_ROOT", t.TempDir())

	previousLookup := lookupMailTLSCommand
	lookupCalled := false
	lookupMailTLSCommand = func(name string) (string, error) {
		lookupCalled = true
		return name, nil
	}
	t.Cleanup(func() { lookupMailTLSCommand = previousLookup })

	var response SecureMailTLSResponse
	err = (&Agent{}).SyncMailTLSV2(&SyncMailTLSV2Request{
		ServiceMutationBinding: ServiceMutationBinding{
			MutationRequestID: testMutationRequestID,
			MutationOwnerID:   testMutationOwnerID,
		},
		Myhostname: "mail.panel.test",
	}, &response)
	if err != nil {
		t.Fatal(err)
	}
	if response.Configured || !strings.Contains(response.Error, "does not authorize") {
		t.Fatalf("root-mismatched response = %+v", response)
	}
	manager.mu.Lock()
	steps := manager.active.steps
	manager.mu.Unlock()
	if steps != 0 {
		t.Fatalf("root-mismatched call acquired %d mutation steps", steps)
	}
	if lookupCalled {
		t.Fatal("root-mismatched call reached host preflight")
	}
}
