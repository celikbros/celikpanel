//go:build !linux

package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallRoundcubeFailsClosedBeforeFilesystemMutationOnNonLinux(t *testing.T) {
	const (
		requestID = "00112233445566778899aabbccddeeff"
		ownerID   = "ffeeddccbbaa99887766554433221100"
	)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	job := &ServiceMutationJob{
		RequestID: requestID,
		OwnerID:   ownerID,
		Kind:      "service_install",
		Target:    "roundcube",
		Status:    serviceMutationStatusRunning,
	}
	manager := &serviceMutationManager{
		active: &serviceMutationRuntime{job: job, ctx: ctx, cancel: cancel},
	}
	globalServiceMutationMu.Lock()
	previousManager := globalServiceMutationManager
	previousManagerErr := globalServiceMutationErr
	globalServiceMutationManager = manager
	globalServiceMutationErr = nil
	globalServiceMutationMu.Unlock()
	t.Cleanup(func() {
		globalServiceMutationMu.Lock()
		globalServiceMutationManager = previousManager
		globalServiceMutationErr = previousManagerErr
		globalServiceMutationMu.Unlock()
	})

	root := t.TempDir()
	missingParent := filepath.Join(root, "must-not-be-created")
	previousBaseDir := webmailBaseDir
	webmailBaseDir = filepath.Join(missingParent, "roundcube")
	t.Cleanup(func() { webmailBaseDir = previousBaseDir })

	commandStarts := 0
	previousWorkerFaultHook := serviceMutationWorkerFaultHook
	serviceMutationWorkerFaultHook = func(string, *exec.Cmd) error {
		commandStarts++
		return errors.New("unexpected install command")
	}
	t.Cleanup(func() { serviceMutationWorkerFaultHook = previousWorkerFaultHook })

	request := &WebmailMutationRequest{ServiceMutationBinding: ServiceMutationBinding{
		MutationRequestID: requestID,
		MutationOwnerID:   ownerID,
	}}
	var response InstallRoundcubeResponse
	if err := (&Agent{}).InstallRoundcube(request, &response); err != nil {
		t.Fatalf("InstallRoundcube RPC error: %v", err)
	}
	if !strings.Contains(response.Error, errRoundcubeLifecycleUnsupported.Error()) {
		t.Fatalf("InstallRoundcube error = %q, want unsupported", response.Error)
	}
	if response.Installed {
		t.Fatal("unsupported Roundcube install reported success")
	}
	if _, err := os.Lstat(missingParent); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unsupported Roundcube install changed filesystem: %v", err)
	}
	if commandStarts != 0 {
		t.Fatalf("unsupported Roundcube install started %d commands", commandStarts)
	}
}

func TestRoundcubeLifecycleMutationsFailClosedOnNonLinux(t *testing.T) {
	parent := t.TempDir()
	stage := filepath.Join(parent, "stage")
	final := filepath.Join(parent, "roundcube")
	retired := filepath.Join(parent, ".roundcube.retired")
	for path, content := range map[string]string{
		filepath.Join(stage, "stage.txt"):     "stage",
		filepath.Join(final, "final.txt"):     "final",
		filepath.Join(retired, "retired.txt"): "retired",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if err := publishRoundcubeStage(stage, final); !errors.Is(err, errRoundcubeLifecycleUnsupported) {
		t.Fatalf("publish error = %v, want unsupported", err)
	}
	result, err := retireRoundcubeTree(final)
	if !errors.Is(err, errRoundcubeLifecycleUnsupported) {
		t.Fatalf("retire error = %v, want unsupported", err)
	}
	if result.Removed || result.MutationApplied {
		t.Fatalf("unsupported retirement result = %+v, want no mutation", result)
	}
	if err := reconcileRoundcubeArtifacts(final, ""); !errors.Is(err, errRoundcubeLifecycleUnsupported) {
		t.Fatalf("reconcile error = %v, want unsupported", err)
	}
	created, err := createRoundcubeInstallStage(filepath.Join(parent, "new-roundcube"))
	if !errors.Is(err, errRoundcubeLifecycleUnsupported) || created != "" {
		t.Fatalf("create stage = %q, %v; want empty unsupported", created, err)
	}

	for path, content := range map[string]string{
		filepath.Join(stage, "stage.txt"):     "stage",
		filepath.Join(final, "final.txt"):     "final",
		filepath.Join(retired, "retired.txt"): "retired",
	} {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read preserved path %s: %v", path, err)
		}
		if string(got) != content {
			t.Fatalf("preserved content at %s = %q, want %q", path, got, content)
		}
	}
	if _, err := os.Lstat(filepath.Join(parent, ".new-roundcube.stage")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unsupported stage creation changed filesystem: %v", err)
	}
}
