//go:build linux

package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/hostplatform"
	"github.com/alicelik/celikpanel/internal/transport"
)

type serviceMutationStepAcquireResult struct {
	ctx     context.Context
	release func()
	err     error
}

func mutationTestBinding() ServiceMutationBinding {
	return ServiceMutationBinding{
		MutationRequestID: testMutationRequestID,
		MutationOwnerID:   testMutationOwnerID,
	}
}

func waitForServiceMutationStepMutexBlock(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		buffer := make([]byte, 1<<20)
		n := runtime.Stack(buffer, true)
		for _, stack := range strings.Split(string(buffer[:n]), "\n\n") {
			firstLine, _, _ := strings.Cut(stack, "\n")
			if strings.Contains(firstLine, "[sync.Mutex.Lock]") &&
				strings.Contains(stack, "serviceMutationManager).acquireStep") {
				return
			}
		}
		runtime.Gosched()
		time.Sleep(time.Millisecond)
	}
	t.Fatal("acquireStep did not reach the held step mutex")
}

func assertMutationTestRuntimeHasNoSteps(t *testing.T, manager *serviceMutationManager) {
	t.Helper()
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.active == nil {
		t.Fatal("mutation runtime unexpectedly disappeared")
	}
	if manager.active.steps != 0 {
		t.Fatalf("unauthorized acquire incremented active steps to %d", manager.active.steps)
	}
}

func finishRejectedMutationTestJob(t *testing.T, manager *serviceMutationManager) {
	t.Helper()
	if _, err := manager.finish(&ServiceMutationFinishRequest{
		RequestID: testMutationRequestID,
		OwnerID:   testMutationOwnerID,
		Success:   false,
	}); err != nil {
		t.Fatalf("finish rejected mutation test job: %v", err)
	}
}

func TestServiceMutationAcquireRejectsUnauthorizedClaimBeforeStepMutex(t *testing.T) {
	manager, _ := newMutationTestManager(t)
	beginMutationTestJob(t, manager)

	manager.mu.Lock()
	runtimeState := manager.active
	manager.mu.Unlock()
	runtimeState.stepMu.Lock()
	stepLocked := true
	defer func() {
		if stepLocked {
			runtimeState.stepMu.Unlock()
		}
	}()

	resultCh := make(chan serviceMutationStepAcquireResult, 1)
	go func() {
		ctx, release, err := manager.acquireStep(
			mutationTestBinding(),
			mutationPolicyClaim(serviceMutationStepInstallNodeVersion, "node", "22.14.0", "install"),
		)
		resultCh <- serviceMutationStepAcquireResult{ctx: ctx, release: release, err: err}
	}()

	var result serviceMutationStepAcquireResult
	select {
	case result = <-resultCh:
	case <-time.After(5 * time.Second):
		t.Fatal("unauthorized acquire waited for step mutex instead of failing closed")
	}
	if !errors.Is(result.err, errServiceMutationStepUnauthorized) {
		t.Fatalf("error=%v want stable unauthorized sentinel", result.err)
	}
	if result.ctx != nil || result.release != nil {
		t.Fatalf("unauthorized acquire exposed execution context=%v release=%v", result.ctx, result.release != nil)
	}
	assertMutationTestRuntimeHasNoSteps(t, manager)

	runtimeState.stepMu.Unlock()
	stepLocked = false
	finishRejectedMutationTestJob(t, manager)
}

func TestServiceMutationAcquireReauthorizesAfterStepMutex(t *testing.T) {
	manager, _ := newMutationTestManager(t)
	beginMutationTestJob(t, manager)

	manager.mu.Lock()
	runtimeState := manager.active
	manager.mu.Unlock()
	runtimeState.stepMu.Lock()
	stepLocked := true
	defer func() {
		if stepLocked {
			runtimeState.stepMu.Unlock()
		}
	}()

	resultCh := make(chan serviceMutationStepAcquireResult, 1)
	go func() {
		ctx, release, err := manager.acquireStep(mutationTestBinding(), nginxInstallTestStepClaim())
		resultCh <- serviceMutationStepAcquireResult{ctx: ctx, release: release, err: err}
	}()
	waitForServiceMutationStepMutexBlock(t)

	manager.mu.Lock()
	runtimeState.job.Target = "rspamd"
	manager.mu.Unlock()
	runtimeState.stepMu.Unlock()
	stepLocked = false

	var result serviceMutationStepAcquireResult
	select {
	case result = <-resultCh:
	case <-time.After(5 * time.Second):
		t.Fatal("reauthorization did not finish after releasing step mutex")
	}
	if !errors.Is(result.err, errServiceMutationStepUnauthorized) {
		t.Fatalf("error=%v want stable unauthorized sentinel", result.err)
	}
	if result.ctx != nil || result.release != nil {
		t.Fatalf("unauthorized recheck exposed execution context=%v release=%v", result.ctx, result.release != nil)
	}
	assertMutationTestRuntimeHasNoSteps(t, manager)

	manager.mu.Lock()
	runtimeState.job.Target = "nginx"
	manager.mu.Unlock()
	ctx, release, err := manager.acquireStep(mutationTestBinding(), nginxInstallTestStepClaim())
	if err != nil {
		t.Fatalf("step mutex was not released after denied recheck: %v", err)
	}
	if ctx == nil || release == nil {
		t.Fatalf("authorized retry context=%v release=%v", ctx, release != nil)
	}
	release()
	finishRejectedMutationTestJob(t, manager)
}

func TestUnrelatedRPCsCannotBorrowNginxInstallLease(t *testing.T) {
	tests := []struct {
		name string
		call func(*testing.T, ServiceMutationBinding) (string, bool)
	}{
		{
			name: "install postfix",
			call: func(t *testing.T, binding ServiceMutationBinding) (string, bool) {
				response := InstallServiceResponse{Installed: true, Detail: "stale", Unit: "stale"}
				if err := (&Agent{}).InstallService(&InstallServiceRequest{
					ServiceMutationBinding: binding,
					ID:                     "postfix",
				}, &response); err != nil {
					t.Fatal(err)
				}
				mutated := response.Installed || response.Detail != "" || response.Unit != ""
				return response.Error, mutated
			},
		},
		{
			name: "install node",
			call: func(t *testing.T, binding ServiceMutationBinding) (string, bool) {
				response := NodeInstallResponse{Installed: true}
				if err := (&Agent{}).InstallNodeVersion(&NodeInstallRequest{
					ServiceMutationBinding: binding,
					Version:                "22.14.0",
				}, &response); err != nil {
					t.Fatal(err)
				}
				return response.Error, response.Installed
			},
		},
		{
			name: "enable repository",
			call: func(t *testing.T, binding ServiceMutationBinding) (string, bool) {
				response := RepoStatusResponse{
					Enabled: true, Repairable: true, PartialSuccess: true,
					MutationApplied: true, Source: "stale",
				}
				if err := (&Agent{}).EnableRepo(&EnableRepoRequest{
					ServiceMutationBinding: binding,
					RepoID:                 "docker",
				}, &response); err != nil {
					t.Fatal(err)
				}
				mutated := response.Enabled || response.Repairable || response.PartialSuccess ||
					response.MutationApplied || response.Source != ""
				return response.Error, mutated
			},
		},
		{
			name: "disable repository",
			call: func(t *testing.T, binding ServiceMutationBinding) (string, bool) {
				response := RepoStatusResponse{
					Enabled: true, Repairable: true, PartialSuccess: true,
					MutationApplied: true, Source: "stale",
				}
				if err := (&Agent{}).DisableRepo(&EnableRepoRequest{
					ServiceMutationBinding: binding,
					RepoID:                 "docker",
				}, &response); err != nil {
					t.Fatal(err)
				}
				mutated := response.Enabled || response.Repairable || response.PartialSuccess ||
					response.MutationApplied || response.Source != ""
				return response.Error, mutated
			},
		},
		{
			name: "restart postfix",
			call: func(t *testing.T, binding ServiceMutationBinding) (string, bool) {
				response := transport.ServiceActionResult{Success: true}
				if err := (&Agent{}).ServiceMutationAction(&ServiceMutationActionRequest{
					ServiceMutationBinding: binding,
					ServiceName:            "postfix",
					Action:                 "restart",
				}, &response); err != nil {
					t.Fatal(err)
				}
				return response.Error, response.Success
			},
		},
		{
			name: "sync dns zone",
			call: func(t *testing.T, binding ServiceMutationBinding) (string, bool) {
				response := SyncDNSZoneResponse{Synced: true}
				if err := (&Agent{}).SyncDNSZoneV2(&SyncDNSZoneV2Request{
					ServiceMutationBinding: binding,
					DesiredGeneration:      1,
					Domain:                 "example.test",
					Delete:                 true,
					ZoneType:               "NATIVE",
				}, &response); err != nil {
					t.Fatal(err)
				}
				return response.Error, response.Synced
			},
		},
		{
			name: "persist firewall",
			call: func(t *testing.T, binding ServiceMutationBinding) (string, bool) {
				response := FirewallStatusResponse{
					Enabled: true, TCPPorts: []int{1}, UDPPorts: []int{2},
					SSHPorts: []int{3}, SnapshotVersion: 99,
				}
				if err := (&Agent{}).ApplyFirewallV2(&ApplyFirewallRequest{
					ServiceMutationBinding: binding,
					Enabled:                true,
					Persist:                true,
					TCPPorts:               []int{80, 443},
				}, &response); err != nil {
					t.Fatal(err)
				}
				mutated := response.Enabled || len(response.TCPPorts) != 0 ||
					len(response.UDPPorts) != 0 || len(response.SSHPorts) != 0 ||
					response.SnapshotVersion != 0
				return response.Error, mutated
			},
		},
		{
			name: "issue certificate",
			call: func(t *testing.T, binding ServiceMutationBinding) (string, bool) {
				response := IssuePanelCertV2Response{
					Issued: true, ExpiresAt: time.Now(), Detail: "stale", ErrorCode: "stale",
				}
				if err := (&Agent{}).IssuePanelCertificateV2(&IssuePanelCertV2Request{
					MutationRequestID:   binding.MutationRequestID,
					MutationOwnerID:     binding.MutationOwnerID,
					Domain:              "panel.example.test",
					Email:               "admin@example.test",
					TLSDir:              managedPanelTLSDir,
					ExpectedBuildCommit: strings.TrimSpace(buildCommit),
				}, &response); err != nil {
					t.Fatal(err)
				}
				mutated := response.Issued || !response.ExpiresAt.IsZero() ||
					response.Detail != "" || response.ErrorCode != ""
				return response.Error, mutated
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager, _ := newMutationTestManager(t)
			installGlobalMutationTestManager(t, manager)
			beginMutationTestJob(t, manager)
			_ = installPanelCertificateActivationMemoryStore(t)

			seamCalls := 0
			commandCalls := 0
			originalDetect := detectHostPlatform
			originalRuntimeRoot := runtimesBaseDir
			originalDNSCommand := dnsSyncCommand
			originalWorkerHook := serviceMutationWorkerFaultHook
			originalSystemctlResolver := serviceMutationSystemctlResolver
			originalFirewallPaths := trustedFirewallCommandPaths
			originalCertLookPath := panelCertLookPath
			originalCertRun := panelCertRunMutationCommand
			originalCertLock := panelCertWithPublishLock
			t.Cleanup(func() {
				detectHostPlatform = originalDetect
				runtimesBaseDir = originalRuntimeRoot
				dnsSyncCommand = originalDNSCommand
				serviceMutationWorkerFaultHook = originalWorkerHook
				serviceMutationSystemctlResolver = originalSystemctlResolver
				trustedFirewallCommandPaths = originalFirewallPaths
				panelCertLookPath = originalCertLookPath
				panelCertRunMutationCommand = originalCertRun
				panelCertWithPublishLock = originalCertLock
			})

			detectHostPlatform = func() (hostplatform.Profile, error) {
				seamCalls++
				return hostplatform.Profile{}, errors.New("blocked host platform probe")
			}
			runtimesBaseDir = t.TempDir()
			nodePath := nodeBinPath("22.14.0")
			if err := os.MkdirAll(filepath.Dir(nodePath), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(nodePath, []byte("test node"), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("CELIKPANEL_PDNS_DB", filepath.Join(t.TempDir(), "pdns.sqlite3"))
			t.Setenv("PATH", t.TempDir())
			dnsSyncCommand = func(context.Context, string, ...string) ([]byte, error) {
				commandCalls++
				return nil, errors.New("blocked DNS command")
			}
			serviceMutationWorkerFaultHook = func(string, *exec.Cmd) error {
				commandCalls++
				return errors.New("blocked mutation command")
			}
			serviceMutationSystemctlResolver = func() (string, error) {
				seamCalls++
				return "", errors.New("blocked systemctl resolver")
			}
			trustedFirewallCommandPaths = map[string][]string{}
			panelCertLookPath = func(name string) (string, error) {
				seamCalls++
				if name == "certbot" {
					return "/test/certbot", nil
				}
				return "", exec.ErrNotFound
			}
			panelCertRunMutationCommand = func(
				context.Context,
				time.Duration,
				string,
				...string,
			) ([]byte, error) {
				commandCalls++
				return nil, errors.New("blocked certificate command")
			}
			panelCertWithPublishLock = func(action func() error) error {
				seamCalls++
				return action()
			}

			scopeError, mutated := tt.call(t, mutationTestBinding())
			if !strings.Contains(scopeError, errServiceMutationStepUnauthorized.Error()) {
				t.Fatalf("scope error=%q want stable unauthorized sentinel", scopeError)
			}
			if mutated {
				t.Fatal("unauthorized RPC reported a successful or applied mutation")
			}
			if seamCalls != 0 || commandCalls != 0 {
				t.Fatalf("unauthorized RPC reached host seams=%d commands=%d", seamCalls, commandCalls)
			}
			assertMutationTestRuntimeHasNoSteps(t, manager)
			finishRejectedMutationTestJob(t, manager)
		})
	}
}

func TestServiceMutationActionUsesResolvedAbsoluteSystemctl(t *testing.T) {
	manager, _ := newMutationTestManager(t)
	installGlobalMutationTestManager(t, manager)
	beginMutationTestJobWithIdentity(t, manager, "service_restart", "nginx", "")

	root := t.TempDir()
	systemctlPath := filepath.Join(root, "systemctl")
	if err := os.Symlink("/bin/true", systemctlPath); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())

	originalResolver := serviceMutationSystemctlResolver
	originalWorkerHook := serviceMutationWorkerFaultHook
	resolverCalls := 0
	commandPath := ""
	var commandArgs []string
	serviceMutationSystemctlResolver = func() (string, error) {
		resolverCalls++
		return systemctlPath, nil
	}
	serviceMutationWorkerFaultHook = func(_ string, command *exec.Cmd) error {
		commandPath = command.Path
		commandArgs = append([]string(nil), command.Args...)
		return nil
	}
	t.Cleanup(func() {
		serviceMutationSystemctlResolver = originalResolver
		serviceMutationWorkerFaultHook = originalWorkerHook
	})

	var response transport.ServiceActionResult
	if err := (&Agent{}).ServiceMutationAction(&ServiceMutationActionRequest{
		ServiceMutationBinding: mutationTestBinding(),
		ServiceName:            "nginx.service",
		Action:                 "restart",
	}, &response); err != nil {
		t.Fatal(err)
	}
	if !response.Success || response.Error != "" {
		t.Fatalf("response=%+v", response)
	}
	if resolverCalls != 1 {
		t.Fatalf("systemctl resolver calls=%d want=1", resolverCalls)
	}
	if !filepath.IsAbs(commandPath) || len(commandArgs) < 3 ||
		commandArgs[len(commandArgs)-3] != systemctlPath ||
		commandArgs[len(commandArgs)-2] != "restart" ||
		commandArgs[len(commandArgs)-1] != "nginx" {
		t.Fatalf("resolved systemctl path=%q args=%v", commandPath, commandArgs)
	}
	if _, err := manager.finish(&ServiceMutationFinishRequest{
		RequestID: testMutationRequestID,
		OwnerID:   testMutationOwnerID,
		Success:   true,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestServiceMutationActionFailsClosedWhenSystemctlResolutionFails(t *testing.T) {
	manager, _ := newMutationTestManager(t)
	installGlobalMutationTestManager(t, manager)
	beginMutationTestJobWithIdentity(t, manager, "service_restart", "nginx", "")

	originalResolver := serviceMutationSystemctlResolver
	originalWorkerHook := serviceMutationWorkerFaultHook
	resolverCalls := 0
	commandCalls := 0
	serviceMutationSystemctlResolver = func() (string, error) {
		resolverCalls++
		return "", errors.New("untrusted systemctl")
	}
	serviceMutationWorkerFaultHook = func(string, *exec.Cmd) error {
		commandCalls++
		return nil
	}
	t.Cleanup(func() {
		serviceMutationSystemctlResolver = originalResolver
		serviceMutationWorkerFaultHook = originalWorkerHook
	})

	response := transport.ServiceActionResult{Success: true}
	if err := (&Agent{}).ServiceMutationAction(&ServiceMutationActionRequest{
		ServiceMutationBinding: mutationTestBinding(),
		ServiceName:            "nginx",
		Action:                 "restart",
	}, &response); err != nil {
		t.Fatal(err)
	}
	if response.Success || response.Error != "systemd client failed security validation" {
		t.Fatalf("response=%+v", response)
	}
	if resolverCalls != 1 || commandCalls != 0 {
		t.Fatalf("resolver calls=%d command calls=%d", resolverCalls, commandCalls)
	}
	assertMutationTestRuntimeHasNoSteps(t, manager)
	finishRejectedMutationTestJob(t, manager)
}
