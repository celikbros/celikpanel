//go:build linux

package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPersistentReleaseTransactionCompletionMarkerBlocksMutationBegin(t *testing.T) {
	manager, root := newMutationTestManager(t)
	transactionRoot := filepath.Join(root, "release-transaction")
	if err := os.Mkdir(transactionRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	manager.releaseTransactionPresent = func() (bool, error) {
		return persistentReleaseTransactionPresent(transactionRoot)
	}

	before, err := os.ReadFile(manager.ledgerPath)
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(transactionRoot, "completion.pending")
	if err := os.WriteFile(marker, []byte("pending\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	request := &ServiceMutationBeginRequest{
		RequestID: testMutationRequestID,
		OwnerID:   testMutationOwnerID,
		Kind:      "service_install",
		Target:    "nginx",
	}
	job, err := manager.begin(request)
	if job != nil || !errors.Is(err, errServiceMutationHostBusy) {
		t.Fatalf("completion-pending begin job=%+v err=%v", job, err)
	}
	after, err := os.ReadFile(manager.ledgerPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatal("blocked begin changed the durable mutation ledger")
	}
	probe, err := acquireServiceMutationFileLock(manager.lockPath)
	if err != nil {
		t.Fatalf("blocked begin retained the host mutation lock: %v", err)
	}
	if err := probe.Close(); err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	job, err = manager.begin(request)
	if err != nil || job == nil || job.Status != serviceMutationStatusRunning {
		t.Fatalf("marker-free begin job=%+v err=%v", job, err)
	}
	if _, err := manager.finish(&ServiceMutationFinishRequest{
		RequestID: request.RequestID,
		OwnerID:   request.OwnerID,
		Success:   false,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPersistentReleaseTransactionInspectionFailsClosed(t *testing.T) {
	root := t.TempDir()
	fileRoot := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(fileRoot, []byte("unexpected\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	present, err := persistentReleaseTransactionPresent(fileRoot)
	if present || err == nil {
		t.Fatalf("non-directory transaction root present=%v err=%v", present, err)
	}
}

func TestPersistentReleaseTransactionRecognizesEveryDurableMarker(t *testing.T) {
	for _, marker := range agentReleaseTransactionMarkers {
		t.Run(marker, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(
				filepath.Join(root, marker),
				[]byte("present\n"),
				0o600,
			); err != nil {
				t.Fatal(err)
			}
			present, err := persistentReleaseTransactionPresent(root)
			if err != nil || !present {
				t.Fatalf("marker %s present=%v err=%v", marker, present, err)
			}
		})
	}
}
