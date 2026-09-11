//go:build linux

package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestCertbotProcessFromDedicatedAgentGroup(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("real Certbot ownership regression requires root")
	}
	if os.Getenv("CELIKPANEL_TEST_CERTBOT_DEDICATED_GROUP") == "1" {
		if os.Getegid() != 65534 {
			t.Fatal("test did not retain the dedicated agent group")
		}
		runCertbotDedicatedGroupRegression(t)
		return
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, "-test.run=^TestCertbotProcessFromDedicatedAgentGroup$", "-test.v")
	command.Env = append(os.Environ(), "CELIKPANEL_TEST_CERTBOT_DEDICATED_GROUP=1")
	command.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 0, Gid: 65534, Groups: []uint32{65534, 65533}}}
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("dedicated agent group regression: %v\n%s", err, output)
	}
}

func runCertbotDedicatedGroupRegression(t *testing.T) {
	manager, root := newMutationTestManager(t)
	program := []byte("#!/bin/sh\nset -eu\n/usr/bin/id -u\n/usr/bin/id -g\n/usr/bin/id -G\nmkdir -p \"$1/live\"\ntouch \"$1/live/certificate\"\nif [ \"${2:-}\" = hold ]; then echo $$ > \"$1/pid\"; sleep 30; touch \"$1/escaped\"; fi\n")
	certbot := filepath.Join(root, "certbot")
	other := filepath.Join(root, "other-command")
	for _, path := range []string{certbot, other} {
		if err := os.WriteFile(path, program, 0700); err != nil {
			t.Fatal(err)
		}
	}
	assertRootSource := func(path string, output []byte) {
		t.Helper()
		if strings.TrimSpace(string(output)) != "0\n0\n0" {
			t.Fatalf("Certbot inherited agent group or supplementary groups: %q", output)
		}
		for _, name := range []string{"live", "live/certificate"} {
			info, err := os.Stat(filepath.Join(path, name))
			if err != nil {
				t.Fatal(err)
			}
			stat := info.Sys().(*syscall.Stat_t)
			if stat.Uid != 0 || stat.Gid != 0 {
				t.Fatalf("Certbot source owner uid=%d gid=%d", stat.Uid, stat.Gid)
			}
		}
	}
	untracked := filepath.Join(root, "untracked")
	output, err := runServiceMutationCombinedOutput(context.Background(), certbot, untracked)
	if err != nil {
		t.Fatal(err)
	}
	assertRootSource(untracked, output)
	control, err := runServiceMutationCombinedOutput(context.Background(), other, filepath.Join(root, "control"))
	if err != nil || !strings.HasPrefix(string(control), "0\n65534\n") {
		t.Fatalf("unrelated command lost its original group: %q %v", control, err)
	}
	beginMutationTestJob(t, manager)
	ctx, done, err := manager.acquireStep(ServiceMutationBinding{MutationRequestID: testMutationRequestID, MutationOwnerID: testMutationOwnerID}, nginxInstallTestStepClaim())
	if err != nil {
		t.Fatal(err)
	}
	tracked := filepath.Join(root, "tracked")
	output, err = runServiceMutationCombinedOutput(ctx, certbot, tracked)
	if err != nil {
		t.Fatal(err)
	}
	assertRootSource(tracked, output)
	hold := filepath.Join(root, "hold")
	result := make(chan error, 1)
	go func() { _, err := runServiceMutationCombinedOutput(ctx, certbot, hold, "hold"); result <- err }()
	var workerPID int
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		job := manager.status(testMutationRequestID)
		raw, readErr := os.ReadFile(filepath.Join(hold, "pid"))
		if readErr == nil && job != nil && job.WorkerPID > 0 && job.WorkerCommand == "certbot" {
			workerPID, _ = strconv.Atoi(strings.TrimSpace(string(raw)))
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if workerPID == 0 {
		t.Fatal("actual Certbot worker was not durably registered")
	}
	if _, err = manager.cancelJob(&ServiceMutationCancelRequest{RequestID: testMutationRequestID, ExpectedOwner: testMutationOwnerID, Reason: "fixture cancellation"}); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-result:
		if err == nil {
			t.Fatal("cancelled Certbot reported success")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled supervised Certbot remained running")
	}
	done()
	if _, err = os.Stat(filepath.Join(hold, "escaped")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("Certbot escaped process-group cancellation")
	}
	if raw, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(workerPID), "stat")); err == nil {
		rest := string(raw)[strings.LastIndex(string(raw), ")")+2:]
		if !strings.HasPrefix(rest, "Z ") {
			t.Fatalf("cancelled real worker remains alive: %s", rest)
		}
	}
	if job := manager.status(testMutationRequestID); job == nil || job.WorkerPID != 0 || job.Status != serviceMutationStatusFailed {
		t.Fatalf("cancellation lost durable terminal state: %+v", job)
	}
}

func TestCertbotIdentityPreservesProcessSafetyAttributes(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root")
	}
	cmd := exec.Command("/usr/bin/certbot")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	if err := configureCertbotProcessIdentity(cmd); err != nil {
		t.Fatal(err)
	}
	if !cmd.SysProcAttr.Setpgid || cmd.SysProcAttr.Pdeathsig != syscall.SIGKILL || cmd.SysProcAttr.Credential == nil || cmd.SysProcAttr.Credential.NoSetGroups {
		t.Fatal("Certbot identity replaced lifecycle protection or retained supplementary groups")
	}
}
