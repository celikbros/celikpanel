//go:build linux

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func readMutationReadinessForRoot(
	t *testing.T,
	root string,
) transport.HostMutationReadinessResponse {
	t.Helper()
	t.Setenv("CELIKPANEL_AGENT_STATE_DIR", filepath.Join(root, "state"))
	t.Setenv("CELIKPANEL_MUTATION_LOCK", filepath.Join(root, "service-mutation.lock"))
	var response transport.HostMutationReadinessResponse
	if err := (&Agent{}).ServiceMutationReadiness(&transport.Empty{}, &response); err != nil {
		t.Fatalf("read mutation readiness: %v", err)
	}
	return response
}

func TestServiceMutationReadinessSharesReadOnlyIdleProof(t *testing.T) {
	t.Run("idle proof is read only", func(t *testing.T) {
		_, root := newMutationTestManager(t)
		ledgerPath := filepath.Join(root, "state", serviceMutationLedgerFileName)
		before, err := os.ReadFile(ledgerPath)
		if err != nil {
			t.Fatal(err)
		}
		beforeInfo, err := os.Stat(ledgerPath)
		if err != nil {
			t.Fatal(err)
		}

		response := readMutationReadinessForRoot(t, root)
		if !response.Ready || response.Code != "" || response.Reason != "" {
			t.Fatalf("idle readiness = %+v", response)
		}
		after, err := os.ReadFile(ledgerPath)
		if err != nil {
			t.Fatal(err)
		}
		afterInfo, err := os.Stat(ledgerPath)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(after, before) || !afterInfo.ModTime().Equal(beforeInfo.ModTime()) {
			t.Fatal("readiness probe changed the durable mutation ledger")
		}
	})

	t.Run("active durable mutation is busy", func(t *testing.T) {
		manager, root := newMutationTestManager(t)
		beginMutationTestJob(t, manager)
		response := readMutationReadinessForRoot(t, root)
		if response.Ready || response.Code != transport.HostMutationBusy ||
			response.Reason != transport.HostMutationReasonAgentMutation {
			t.Fatalf("active readiness = %+v", response)
		}
	})

	t.Run("external host lock is busy", func(t *testing.T) {
		_, root := newMutationTestManager(t)
		lock, err := acquireServiceMutationFileLock(filepath.Join(root, "service-mutation.lock"))
		if err != nil {
			t.Fatal(err)
		}
		defer lock.Close()
		response := readMutationReadinessForRoot(t, root)
		if response.Ready || response.Code != transport.HostMutationBusy ||
			response.Reason != transport.HostMutationReasonHostLock {
			t.Fatalf("host-lock readiness = %+v", response)
		}
	})

	t.Run("package manager is busy", func(t *testing.T) {
		_, root := newMutationTestManager(t)
		original := packageManagerMutationBusyProbe
		packageManagerMutationBusyProbe = func() (bool, error) { return true, nil }
		t.Cleanup(func() { packageManagerMutationBusyProbe = original })
		response := readMutationReadinessForRoot(t, root)
		if response.Ready || response.Code != transport.HostMutationBusy ||
			response.Reason != transport.HostMutationReasonPackageManager {
			t.Fatalf("package readiness = %+v", response)
		}
	})

	t.Run("unverified state fails closed", func(t *testing.T) {
		root := mutationTestRoot(t)
		response := readMutationReadinessForRoot(t, root)
		if response.Ready || response.Code != transport.HostMutationUnavailable ||
			response.Reason != transport.HostMutationReasonStateUnverified {
			t.Fatalf("unverified readiness = %+v", response)
		}
		if _, err := os.Lstat(filepath.Join(root, "state")); !os.IsNotExist(err) {
			t.Fatalf("readiness created missing state: %v", err)
		}
	})
}

func TestBeginServiceMutationReturnsStructuredBusyRefusal(t *testing.T) {
	manager, _ := newMutationTestManager(t)
	installGlobalMutationTestManager(t, manager)
	beginMutationTestJob(t, manager)

	response := ServiceMutationResponse{}
	err := (&Agent{}).BeginServiceMutation(&ServiceMutationBeginRequest{
		RequestID: strings.Repeat("c", 32),
		OwnerID:   strings.Repeat("d", 32),
		Kind:      "service_install",
		Target:    "postfix",
	}, &response)
	if err != nil {
		t.Fatalf("busy refusal escaped as RPC error: %v", err)
	}
	if response.ErrorCode != transport.HostMutationBusy || response.Error != hostMutationBusyMessage {
		t.Fatalf("busy response = %+v", response)
	}
	if response.Job == nil {
		t.Fatalf("busy response lost active job: %+v", response.Job)
	}
}

func TestBeginServiceMutationReturnsStructuredBusyDuringManagerInitialization(t *testing.T) {
	root := mutationTestRoot(t)
	stateDir := filepath.Join(root, "state")
	lockPath := filepath.Join(root, "service-mutation.lock")
	if err := initializeServiceMutationLedger(stateDir, lockPath); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CELIKPANEL_AGENT_STATE_DIR", stateDir)
	t.Setenv("CELIKPANEL_MUTATION_LOCK", lockPath)
	installGlobalMutationTestManager(t, nil)

	lock, err := acquireServiceMutationFileLock(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()

	response := ServiceMutationResponse{}
	if err := (&Agent{}).BeginServiceMutation(&ServiceMutationBeginRequest{
		RequestID: strings.Repeat("e", 32),
		OwnerID:   strings.Repeat("f", 32),
		Kind:      "service_install",
		Target:    "postfix",
	}, &response); err != nil {
		t.Fatalf("transient host lock escaped as RPC error: %v", err)
	}
	if response.ErrorCode != transport.HostMutationBusy || response.Error != hostMutationBusyMessage {
		t.Fatalf("host-lock response = %+v", response)
	}
	if response.Job != nil {
		t.Fatalf("host-lock response unexpectedly granted a job: %+v", response.Job)
	}
}
