//go:build linux && dns_kill_matrix

package main

import (
	"bufio"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

const dnsKillMatrixAfterHookSentinelEnv = "CELIKPANEL_DNS_KILL_MATRIX_TEST_AFTER_HOOK"

// Once the selected boundary is reached, the journal writer and the code that
// called it must never continue in this process, whatever the marker, ready
// notification or stop request reported. Continuing would run the operation's
// error path (for a first BIND install: removing the generation pointer) after
// the boundary the controller records (batch 4 cell c6, 2026-09-29).
func TestDNSKillMatrixBoundaryNeverReturnsIntoTheJournalWriter(t *testing.T) {
	injected := errors.New("injected boundary failure")
	for _, test := range []struct {
		name                  string
		marker, ready, stop   error
		wantResumedIdentifier bool
	}{
		{name: "stopped then resumed", wantResumedIdentifier: true},
		{name: "marker publication failed", marker: injected},
		{name: "ready notification failed", ready: injected},
		{name: "stop request failed", stop: injected},
	} {
		t.Run(test.name, func(t *testing.T) {
			journal := testBINDSwitchJournal(t)
			config := dnsKillMatrixConfig{
				CellID:    "bind.target-staged.after.standalone.reachable",
				Driver:    dnsEngineSwitchFaultDriverBIND,
				Point:     dnsEngineSwitchJournalFaultAfterWrite,
				Phase:     dnsSwitchPhaseTargetStaged,
				RequestID: journal.MutationRequestID,
				Nonce:     strings.Repeat("a", 64),
				Marker:    filepath.Join(t.TempDir(), "boundary.json"),
				ReadyFD:   9,
			}
			parked := make(chan error, 1)
			release := make(chan struct{})
			runtime := &dnsKillMatrixRuntime{
				config: config,
				ops: dnsKillMatrixRuntimeOps{
					pid:         func() int { return 4321 },
					startTicks:  func(int) (string, error) { return "987654", nil },
					writeMarker: func(string, dnsKillMatrixMarker) error { return test.marker },
					notifyReady: func(int, string) error { return test.ready },
					stopProcess: func(int) error { return test.stop },
					now:         time.Now,
					park: func(reason error) {
						parked <- reason
						<-release
						// Leave the writer goroutine without returning into it.
						goruntime.Goexit()
					},
				},
			}
			var persisted atomic.Int32
			var continued atomic.Bool
			done := make(chan struct{})
			go func() {
				defer close(done)
				stored, exists := dnsEngineSwitchJournal{}, false
				_ = writeDNSEngineSwitchJournalWithOps(
					journal,
					func([]byte) error {
						persisted.Add(1)
						stored, exists = journal, true
						return nil
					},
					func() (dnsEngineSwitchJournal, bool, error) { return stored, exists, nil },
					func(point string, observed dnsEngineSwitchJournal) error {
						return runtime.hook(config.Driver, point, observed)
					},
				)
				// Stands for the operation's own continuation, including its
				// error path. It must never execute.
				continued.Store(true)
			}()
			var reason error
			select {
			case reason = <-parked:
			case <-time.After(10 * time.Second):
				t.Fatal("boundary did not park the journal writer")
			}
			if test.wantResumedIdentifier != errors.Is(reason, dnsKillMatrixResumedError) ||
				(!test.wantResumedIdentifier && !errors.Is(reason, injected)) {
				t.Fatalf("park reason = %v", reason)
			}
			if persisted.Load() != 1 {
				t.Fatalf("journal persisted %d times before the boundary", persisted.Load())
			}
			time.Sleep(50 * time.Millisecond)
			if continued.Load() {
				t.Fatal("the journal writer's caller continued past the boundary")
			}
			close(release)
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("parked writer goroutine did not exit")
			}
			if continued.Load() {
				t.Fatal("the journal writer's caller continued after the park ended")
			}
		})
	}
}

// The real stop is thread-directed, so the calling goroutine never runs past
// the hook; and a resumed (SIGCONT) process parks instead of continuing into
// the operation. The controller then sees a sleeping process, never a stopped
// one, and refuses the cell rather than recording a false boundary.
func TestDNSKillMatrixRealStopNeverRunsCodeAfterTheHook(t *testing.T) {
	if os.Getenv(dnsKillMatrixHelperProcessEnv) == "1" {
		sentinel := os.Getenv(dnsKillMatrixAfterHookSentinelEnv)
		if sentinel == "" {
			return
		}
		journal := testBINDSwitchJournal(t)
		journal.Phase = dnsSwitchPhaseSourceStopped
		journal.MutationRequestID = os.Getenv(dnsKillMatrixEnvRequestID)
		if dnsEngineSwitchJournalFaultHook == nil {
			t.Fatal("tagged helper process has no DNS kill-matrix hook")
		}
		err := dnsEngineSwitchJournalFaultHook(
			dnsEngineSwitchFaultDriverBIND,
			dnsEngineSwitchJournalFaultBeforeWrite, journal,
		)
		_ = os.WriteFile(sentinel, []byte("continued\n"), 0o600)
		t.Fatalf("real DNS kill-matrix hook returned into the operation: %v", err)
	}

	root := t.TempDir()
	markerPath := filepath.Join(root, "boundary.json")
	sentinel := filepath.Join(root, "after-hook")
	logPath := filepath.Join(root, "child.log")
	logFile, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	readyReader, readyWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer readyReader.Close()
	defer readyWriter.Close()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	selector := testDNSKillMatrixEnvironment(markerPath)
	selector[dnsKillMatrixEnvReadyFD] = "3"
	command := exec.Command(
		executable, "-test.run=^TestDNSKillMatrixRealStopNeverRunsCodeAfterTheHook$",
	)
	command.Env = append(
		dnsKillMatrixHelperEnvironment(selector, root),
		dnsKillMatrixAfterHookSentinelEnv+"="+sentinel,
	)
	command.ExtraFiles = []*os.File{readyWriter}
	command.Stdout = logFile
	command.Stderr = logFile
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	_ = readyWriter.Close()
	waited := false
	var waitErr error
	waitChild := func(kill bool) error {
		if waited {
			return waitErr
		}
		if kill && command.Process != nil {
			_ = syscall.Kill(command.Process.Pid, syscall.SIGKILL)
		}
		waitErr = command.Wait()
		waited = true
		return waitErr
	}
	t.Cleanup(func() { _ = waitChild(true) })
	fail := func(format string, arguments ...any) {
		_ = waitChild(true)
		_ = logFile.Close()
		childLog, _ := os.ReadFile(logPath)
		t.Fatalf(format+"\nchild output:\n%s", append(arguments, childLog)...)
	}
	sentinelAbsent := func(label string) {
		if _, err := os.Lstat(sentinel); err == nil {
			fail("%s: code after the hook ran", label)
		} else if !errors.Is(err, os.ErrNotExist) {
			fail("%s: inspect sentinel: %v", label, err)
		}
	}

	if err := readyReader.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		fail("set helper ready deadline: %v", err)
	}
	readyLine, err := bufio.NewReader(readyReader).ReadString('\n')
	if err != nil || readyLine != selector[dnsKillMatrixEnvNonce]+"\n" {
		fail("helper ready nonce = %q, err = %v", readyLine, err)
	}
	waitState := func(want func(string) bool, label string) string {
		deadline := time.Now().Add(5 * time.Second)
		last := ""
		for {
			state, stateErr := dnsKillMatrixTestProcessState(command.Process.Pid)
			if stateErr == nil {
				last = state
				if want(state) {
					return state
				}
			}
			if time.Now().After(deadline) {
				fail("%s; last state=%q err=%v", label, last, stateErr)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	waitState(func(state string) bool { return state == "T" }, "helper did not stop")
	sentinelAbsent("stopped")

	if err := syscall.Kill(command.Process.Pid, syscall.SIGCONT); err != nil {
		fail("resume stopped helper: %v", err)
	}
	waitState(func(state string) bool { return state != "T" && state != "t" }, "helper did not resume")
	time.Sleep(300 * time.Millisecond)
	sentinelAbsent("resumed")
	if state, err := dnsKillMatrixTestProcessState(command.Process.Pid); err != nil ||
		state == "T" || state == "t" || state == "Z" || state == "X" {
		fail("resumed helper state = %q, err = %v; want it alive and parked", state, err)
	}

	if err := syscall.Kill(command.Process.Pid, syscall.SIGKILL); err != nil {
		fail("send SIGKILL to parked helper: %v", err)
	}
	status, err := dnsKillMatrixNormalizedShellStatus(waitChild(false))
	if err != nil || status != 137 {
		fail("parked helper exit status = %d, err = %v; want 137", status, err)
	}
	sentinelAbsent("killed")
}
