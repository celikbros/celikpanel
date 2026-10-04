//go:build linux

package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// Exercise the executable built from the production source list, including the
// same filesystem ownership and inherited flock proofs as the normal Agent.
func TestRecoveryAgentCheckerStandalone(t *testing.T) {
	repository, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(repository, "deploy/recovery/agent-checker.sources"))
	if err != nil {
		t.Fatal(err)
	}
	sources := strings.Fields(string(raw))
	seen := map[string]bool{}
	for _, source := range sources {
		if !strings.HasPrefix(source, "cmd/agent/") || strings.Contains(source, "..") ||
			!strings.HasSuffix(source, ".go") || strings.HasSuffix(source, "_test.go") ||
			strings.HasSuffix(source, "_rpc.go") || source == "cmd/agent/main.go" || seen[source] {
			t.Fatalf("unsafe or duplicate recovery source %q", source)
		}
		seen[source] = true
	}
	for _, required := range []string{"recovery_check_entry.go", "service_mutation_idle.go", "service_mutation_ledger_contract.go", "service_mutation_secure_linux.go", "service_mutation_lock_linux.go"} {
		if !seen["cmd/agent/"+required] {
			t.Fatalf("shared production source missing: %s", required)
		}
	}
	binaryDir := t.TempDir()
	binary := filepath.Join(binaryDir, "recovery-agent-checker")
	build := exec.Command("go", append([]string{"build", "-o", binary}, sources...)...)
	build.Dir = repository
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build checker: %v\n%s", err, output)
	}
	if os.Geteuid() != 0 {
		output, err := exec.Command(binary, "--check-service-mutation-idle").CombinedOutput()
		if err == nil || !strings.Contains(string(output), "root owner authentication is required") {
			t.Fatalf("non-root checker: %v %s", err, output)
		}
		t.Skip("native filesystem and inherited-lock acceptance requires root")
	}
	busy, err := realPackageManagerMutationBusy()
	if err != nil || busy {
		t.Skipf("native package manager idle proof unavailable: busy=%v err=%v", busy, err)
	}
	gid := os.Getgid()
	if configured, ok := lookupGroupID("celikpanel"); ok {
		gid = configured
	}
	canonical, err := canonicalInitialServiceMutationLedger()
	if err != nil {
		t.Fatal(err)
	}
	type fixture struct{ root, state, lock, ledger string }
	newFixture := func(t *testing.T) fixture {
		t.Helper()
		root := t.TempDir()
		f := fixture{root: root, state: filepath.Join(root, "state"), lock: filepath.Join(root, "runtime", "service-mutation.lock")}
		f.ledger = filepath.Join(f.state, serviceMutationLedgerFileName)
		for _, path := range []string{root, f.state, filepath.Dir(f.lock)} {
			if err := os.MkdirAll(path, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Chown(path, 0, gid); err != nil {
				t.Fatal(err)
			}
		}
		for path, content := range map[string][]byte{f.lock: {}, f.ledger: canonical, filepath.Join(root, "owner-sentinel"): []byte("preserve owner state\n")} {
			if err := os.WriteFile(path, content, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chown(path, 0, gid); err != nil {
				t.Fatal(err)
			}
		}
		return f
	}
	run := func(t *testing.T, f fixture, wantSuccess bool, inherited *os.File, args ...string) string {
		t.Helper()
		before := recoveryCheckerTree(t, f.root)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, binary, args...)
		command.Env = []string{"CELIKPANEL_AGENT_STATE_DIR=" + f.state, "CELIKPANEL_MUTATION_LOCK=" + f.lock}
		if inherited != nil {
			command.ExtraFiles = []*os.File{inherited}
			command.Env = append(command.Env, "CELIKPANEL_MUTATION_LOCK_FD=3")
		}
		output, err := command.CombinedOutput()
		if ctx.Err() != nil {
			t.Fatalf("checker hung instead of rejecting unsafe input: %v", ctx.Err())
		}
		if (err == nil) != wantSuccess {
			t.Fatalf("checker %v success=%v expected=%v: %v\n%s", args, err == nil, wantSuccess, err, output)
		}
		if after := recoveryCheckerTree(t, f.root); !reflect.DeepEqual(before, after) {
			t.Fatalf("read-only checker changed fixture: before=%v after=%v", before, after)
		}
		return string(output)
	}
	flags := []string{"--check-service-mutation-idle", "--check-pre-ledger-service-mutation-idle", "--check-initial-service-mutation-ledger"}
	t.Run("canonical-and-inherited-lock", func(t *testing.T) {
		f := newFixture(t)
		for _, flag := range flags {
			run(t, f, true, nil, flag)
		}
		lock, err := os.OpenFile(f.lock, os.O_RDWR, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer lock.Close()
		for _, flag := range flags {
			run(t, f, false, lock, flag+"-under-external-lock")
		}
		if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
			t.Fatal(err)
		}
		defer unix.Flock(int(lock.Fd()), unix.LOCK_UN)
		for _, flag := range flags {
			run(t, f, true, lock, flag+"-under-external-lock")
			run(t, f, false, nil, flag)
			run(t, f, false, nil, flag+"-under-external-lock")
		}
	})
	t.Run("pre-ledger-missing-does-not-create", func(t *testing.T) {
		f := newFixture(t)
		if err := os.Remove(f.ledger); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(f.state); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(f.lock); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Dir(f.lock)); err != nil {
			t.Fatal(err)
		}
		run(t, f, true, nil, flags[1])
		run(t, f, false, nil, flags[0])
	})
	t.Run("strict-arguments", func(t *testing.T) {
		f := newFixture(t)
		for _, args := range [][]string{nil, {"--unknown"}, {flags[0], flags[0]}, {flags[0] + "=true"}, {flags[0], "extra"}} {
			run(t, f, false, nil, args...)
		}
	})
	t.Run("active-operation-is-not-idle", func(t *testing.T) {
		f := newFixture(t)
		started := time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)
		job := &ServiceMutationJob{RequestID: strings.Repeat("a", 32), OwnerID: strings.Repeat("b", 32), Kind: "service_install", Target: "nginx", Status: serviceMutationStatusRunning, Phase: "test", Attempt: 1, StartedAt: started, UpdatedAt: started.Add(time.Minute), DeadlineAt: started.Add(time.Hour), LeaseExpiresAt: started.Add(10 * time.Minute)}
		ledger := serviceMutationLedger{Version: serviceMutationLedgerVersion, ActiveRequestID: job.RequestID, Jobs: map[string]*ServiceMutationJob{job.RequestID: job}}
		data, err := json.Marshal(&ledger)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := decodeServiceMutationLedger(data); err != nil {
			t.Fatalf("invalid active fixture: %v", err)
		}
		if err := os.WriteFile(f.ledger, data, 0600); err != nil {
			t.Fatal(err)
		}
		for _, flag := range flags {
			run(t, f, false, nil, flag)
		}
	})
	for name, content := range map[string][]byte{"noncanonical": append(append([]byte{}, canonical...), '\n'), "unknown-schema": []byte(`{"version":2,"jobs":{}}`), "unknown-field": []byte(`{"version":1,"jobs":{},"unproven":true}`)} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			if err := os.WriteFile(f.ledger, content, 0600); err != nil {
				t.Fatal(err)
			}
			for _, flag := range flags {
				run(t, f, false, nil, flag)
			}
		})
	}
	for _, target := range []string{"ledger", "lock", "initial-stage"} {
		t.Run("FIFO-"+target, func(t *testing.T) {
			f := newFixture(t)
			path, flag := f.ledger, flags[0]
			if target == "lock" {
				path = f.lock
			}
			if target == "initial-stage" {
				if err := os.Remove(f.ledger); err != nil {
					t.Fatal(err)
				}
				path, flag = filepath.Join(f.state, ".service-mutations-initial-123.json"), flags[1]
			} else if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := unix.Mkfifo(path, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chown(path, 0, gid); err != nil {
				t.Fatal(err)
			}
			run(t, f, false, nil, flag)
		})
	}
}

// Access time may change during a legitimate read. File bytes, identity,
// ownership, permissions, modification time and directory entries must not.
func recoveryCheckerTree(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		stat := info.Sys().(*syscall.Stat_t)
		value := fmt.Sprintf("%d/%d/%d/%d/%d/%d", info.Mode(), stat.Uid, stat.Gid, stat.Ino, stat.Nlink, info.ModTime().UnixNano())
		if info.Mode().IsRegular() {
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			value += fmt.Sprintf("/%x", sha256.Sum256(raw))
		}
		result[path] = value
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
