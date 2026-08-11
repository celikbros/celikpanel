//go:build linux

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type mailSubmissionTestEnvironment struct {
	manager    *serviceMutationManager
	binding    ServiceMutationBinding
	commandLog string
	confPath   string
	mapPath    string
}

type mailSubmissionTestCall struct {
	response ConfigureMailSubmissionResponse
	err      error
}

func newMailSubmissionTestEnvironment(t *testing.T, withDovecot bool) *mailSubmissionTestEnvironment {
	t.Helper()
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	confDir := filepath.Join(root, "etc", "dovecot", "conf.d")
	postfixDir := filepath.Join(root, "etc", "postfix")
	for _, dir := range []string{binDir, confDir, postfixDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	commandLog := filepath.Join(root, "commands.log")
	writeMailSubmissionTestCommand(t, binDir, "postconf", `
if [ "$1" = "-M" ] && [ -n "$BLOCK_POSTCONF_STARTED" ]; then
  : > "$BLOCK_POSTCONF_STARTED"
  while [ ! -e "$BLOCK_POSTCONF_RELEASE" ]; do :; done
fi
exit 0`)
	writeMailSubmissionTestCommand(t, binDir, "systemctl", `
if [ "$1" = "restart" ] && [ "$2" = "dovecot" ] && [ -n "$FAIL_DOVECOT_RESTART_ONCE" ] && [ ! -e "$FAIL_DOVECOT_RESTART_ONCE" ]; then
  : > "$FAIL_DOVECOT_RESTART_ONCE"
  printf '%s\n' 'synthetic dovecot restart failure' >&2
  exit 1
fi
exit 0`)
	if withDovecot {
		writeMailSubmissionTestCommand(t, binDir, "doveconf", "exit 0")
	}
	t.Setenv("PATH", binDir)
	t.Setenv("MAIL_SUBMISSION_COMMAND_LOG", commandLog)
	t.Setenv("CELIKPANEL_MAIL_DIR", "")

	previousConfPath := dovecotSubmissionConf
	previousMapPath := postfixLoginMapPath
	previousTimeout := mailTLSCommandTimeout
	dovecotSubmissionConf = filepath.Join(confDir, "97-celikpanel-submission.conf")
	postfixLoginMapPath = filepath.Join(postfixDir, "celikpanel_login_map")
	mailTLSCommandTimeout = defaultMailTLSCommandTimeout
	t.Cleanup(func() {
		dovecotSubmissionConf = previousConfPath
		postfixLoginMapPath = previousMapPath
		mailTLSCommandTimeout = previousTimeout
	})

	manager, _ := newMutationTestManager(t)
	// The race detector makes every durable command registration much slower
	// on a mounted Windows filesystem. Keep the test lease comfortably above
	// that instrumentation cost; cancellation tests still cancel it explicitly.
	manager.leaseDuration = 2 * time.Minute
	installGlobalMutationTestManager(t, manager)
	beginMutationTestJob(t, manager)
	return &mailSubmissionTestEnvironment{
		manager: manager,
		binding: ServiceMutationBinding{
			MutationRequestID: testMutationRequestID,
			MutationOwnerID:   testMutationOwnerID,
		},
		commandLog: commandLog,
		confPath:   dovecotSubmissionConf,
		mapPath:    postfixLoginMapPath,
	}
}

func writeMailSubmissionTestCommand(t *testing.T, binDir, name, body string) {
	t.Helper()
	content := fmt.Sprintf(`#!/bin/sh
printf '%s %%s\n' "$*" >> "$MAIL_SUBMISSION_COMMAND_LOG"
%s
`, name, body)
	if err := os.WriteFile(filepath.Join(binDir, name), []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func callConfigureMailSubmission(
	t *testing.T,
	binding ServiceMutationBinding,
) ConfigureMailSubmissionResponse {
	t.Helper()
	call := runConfigureMailSubmission(binding)
	if call.err != nil {
		t.Fatal(call.err)
	}
	return call.response
}

func runConfigureMailSubmission(binding ServiceMutationBinding) mailSubmissionTestCall {
	var response ConfigureMailSubmissionResponse
	err := (&Agent{}).ConfigureMailSubmission(&ServiceMutationRequest{
		ServiceMutationBinding: binding,
	}, &response)
	return mailSubmissionTestCall{response: response, err: err}
}

func readMailSubmissionCommandLog(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func waitForMailSubmissionStep(t *testing.T, manager *serviceMutationManager) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		manager.mu.Lock()
		steps := 0
		if manager.active != nil {
			steps = manager.active.steps
		}
		manager.mu.Unlock()
		if steps == 1 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("mail submission RPC did not acquire its durable mutation step")
}

func waitForMailSubmissionFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}

func TestConfigureMailSubmissionSucceedsAndReusesSameBinding(t *testing.T) {
	environment := newMailSubmissionTestEnvironment(t, true)
	for attempt := 0; attempt < 2; attempt++ {
		response := callConfigureMailSubmission(t, environment.binding)
		if !response.Configured || response.Error != "" || response.Detail == "" {
			t.Fatalf("attempt %d response=%+v", attempt+1, response)
		}
	}

	conf, err := os.ReadFile(environment.confPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(conf), "/var/spool/postfix/private/auth") {
		t.Fatalf("dovecot submission config=%q", conf)
	}
	loginMap, err := os.ReadFile(environment.mapPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(loginMap) != "/^(.+)$/ ${1}\n" {
		t.Fatalf("postfix login map=%q", loginMap)
	}
	log := readMailSubmissionCommandLog(t, environment.commandLog)
	for _, command := range []string{
		"doveconf -n",
		"postconf -M submission/inet=",
		"postconf -M smtps/inet=",
		"systemctl restart dovecot",
		"systemctl restart postfix",
	} {
		if strings.Count(log, command) != 2 {
			t.Fatalf("command %q was not executed once per same-binding attempt:\n%s", command, log)
		}
	}
}

func TestConfigureMailSubmissionRequiresActiveBinding(t *testing.T) {
	environment := newMailSubmissionTestEnvironment(t, true)
	wrongBinding := environment.binding
	wrongBinding.MutationOwnerID = strings.Repeat("f", 32)
	response := callConfigureMailSubmission(t, wrongBinding)
	if response.Configured || !strings.Contains(response.Error, "does not own the active lease") {
		t.Fatalf("wrong-binding response=%+v", response)
	}
	if log := readMailSubmissionCommandLog(t, environment.commandLog); log != "" {
		t.Fatalf("wrong binding executed host commands:\n%s", log)
	}
	if _, err := os.Stat(environment.confPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("wrong binding mutated dovecot config: %v", err)
	}
}

func TestConfigureMailSubmissionFailsClosedWhenDovecotIsMissing(t *testing.T) {
	environment := newMailSubmissionTestEnvironment(t, false)
	response := callConfigureMailSubmission(t, environment.binding)
	if response.Configured || response.Error != "dovecot is not installed" {
		t.Fatalf("response=%+v", response)
	}
	if log := readMailSubmissionCommandLog(t, environment.commandLog); log != "" {
		t.Fatalf("missing Dovecot executed host commands:\n%s", log)
	}
	for _, path := range []string{environment.confPath, environment.mapPath} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("missing Dovecot mutated %s: %v", path, err)
		}
	}
}

func TestConfigureMailSubmissionSerializesTheWholeTransaction(t *testing.T) {
	environment := newMailSubmissionTestEnvironment(t, true)
	mailMutex.Lock()
	locked := true
	t.Cleanup(func() {
		if locked {
			mailMutex.Unlock()
		}
	})

	result := make(chan mailSubmissionTestCall, 1)
	go func() {
		result <- runConfigureMailSubmission(environment.binding)
	}()
	waitForMailSubmissionStep(t, environment.manager)
	select {
	case call := <-result:
		t.Fatalf("RPC escaped the held mail lock: response=%+v err=%v", call.response, call.err)
	case <-time.After(50 * time.Millisecond):
	}
	if log := readMailSubmissionCommandLog(t, environment.commandLog); log != "" {
		t.Fatalf("RPC executed commands before acquiring mail lock:\n%s", log)
	}

	mailMutex.Unlock()
	locked = false
	select {
	case call := <-result:
		if call.err != nil || !call.response.Configured || call.response.Error != "" {
			t.Fatalf("response=%+v err=%v", call.response, call.err)
		}
	case <-time.After(60 * time.Second):
		_, _ = environment.manager.cancelJob(&ServiceMutationCancelRequest{
			RequestID:     environment.binding.MutationRequestID,
			ExpectedOwner: environment.binding.MutationOwnerID,
			Reason:        "test_cleanup_after_mail_lock_timeout",
		})
		<-result
		t.Fatal("RPC deadlocked after mail lock was released")
	}
}

func TestConfigureMailSubmissionCanceledWhileBusyDoesNotMutate(t *testing.T) {
	environment := newMailSubmissionTestEnvironment(t, true)
	mailMutex.Lock()
	locked := true
	t.Cleanup(func() {
		if locked {
			mailMutex.Unlock()
		}
	})

	result := make(chan mailSubmissionTestCall, 1)
	go func() {
		result <- runConfigureMailSubmission(environment.binding)
	}()
	waitForMailSubmissionStep(t, environment.manager)
	if _, err := environment.manager.cancelJob(&ServiceMutationCancelRequest{
		RequestID:     environment.binding.MutationRequestID,
		ExpectedOwner: environment.binding.MutationOwnerID,
		Reason:        "test_cancel_while_waiting_for_mail_lock",
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case call := <-result:
		if call.err != nil || call.response.Configured || !strings.Contains(call.response.Error, "service mutation lease ended") {
			t.Fatalf("response=%+v err=%v", call.response, call.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceled RPC remained blocked on mailMutex")
	}
	mailMutex.Unlock()
	locked = false

	if log := readMailSubmissionCommandLog(t, environment.commandLog); log != "" {
		t.Fatalf("canceled busy RPC executed commands:\n%s", log)
	}
	for _, path := range []string{environment.confPath, environment.mapPath} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("canceled busy RPC mutated %s: %v", path, err)
		}
	}
}

func TestConfigureMailSubmissionCancellationStopsCommandsAndRollsBackFiles(t *testing.T) {
	environment := newMailSubmissionTestEnvironment(t, true)
	if err := os.WriteFile(environment.confPath, []byte("old dovecot\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(environment.mapPath, []byte("old map\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	started := filepath.Join(filepath.Dir(environment.commandLog), "postconf.started")
	release := filepath.Join(filepath.Dir(environment.commandLog), "postconf.release")
	t.Setenv("BLOCK_POSTCONF_STARTED", started)
	t.Setenv("BLOCK_POSTCONF_RELEASE", release)

	result := make(chan mailSubmissionTestCall, 1)
	go func() {
		result <- runConfigureMailSubmission(environment.binding)
	}()
	waitForMailSubmissionFile(t, started)
	if _, err := environment.manager.cancelJob(&ServiceMutationCancelRequest{
		RequestID:     environment.binding.MutationRequestID,
		ExpectedOwner: environment.binding.MutationOwnerID,
		Reason:        "test_cancel_running_submission_command",
	}); err != nil {
		t.Fatal(err)
	}

	var call mailSubmissionTestCall
	select {
	case call = <-result:
	case <-time.After(3 * time.Second):
		_ = os.WriteFile(release, []byte("release\n"), 0o600)
		_, _ = environment.manager.cancelJob(&ServiceMutationCancelRequest{
			RequestID:     environment.binding.MutationRequestID,
			ExpectedOwner: environment.binding.MutationOwnerID,
			Reason:        "test_cleanup_after_command_cancel_timeout",
		})
		<-result
		t.Fatal("lease cancellation did not stop the running postconf command")
	}
	if call.err != nil || call.response.Configured ||
		!strings.Contains(call.response.Error, "service mutation lease ended") ||
		!strings.Contains(call.response.Error, "managed files restored") {
		t.Fatalf("response=%+v err=%v", call.response, call.err)
	}
	for path, want := range map[string]string{
		environment.confPath: "old dovecot\n",
		environment.mapPath:  "old map\n",
	} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(raw) != want {
			t.Fatalf("%s after cancellation=%q want %q", path, raw, want)
		}
	}
	log := readMailSubmissionCommandLog(t, environment.commandLog)
	if !strings.Contains(log, "doveconf -n") || !strings.Contains(log, "postconf -M submission/inet=") {
		t.Fatalf("expected validation and first Postfix command:\n%s", log)
	}
	if strings.Contains(log, "postconf -M smtps/inet=") || strings.Contains(log, "systemctl restart") {
		t.Fatalf("commands continued after lease cancellation:\n%s", log)
	}
}

func TestConfigureMailSubmissionDovecotRestartFailureIsNotSuccess(t *testing.T) {
	environment := newMailSubmissionTestEnvironment(t, true)
	if err := os.WriteFile(environment.confPath, []byte("old dovecot\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(environment.mapPath, []byte("old map\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	failureMarker := filepath.Join(filepath.Dir(environment.commandLog), "fail-dovecot-once")
	t.Setenv("FAIL_DOVECOT_RESTART_ONCE", failureMarker)

	response := callConfigureMailSubmission(t, environment.binding)
	if response.Configured || response.Detail != "" ||
		!strings.Contains(response.Error, "dovecot restart") ||
		!strings.Contains(response.Error, "managed files restored") ||
		!strings.Contains(response.Error, "Postfix master.cf changes may remain") {
		t.Fatalf("response=%+v", response)
	}
	for path, want := range map[string]string{
		environment.confPath: "old dovecot\n",
		environment.mapPath:  "old map\n",
	} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(raw) != want {
			t.Fatalf("%s after restart failure=%q want %q", path, raw, want)
		}
	}
	log := readMailSubmissionCommandLog(t, environment.commandLog)
	if strings.Count(log, "systemctl restart dovecot") != 2 {
		t.Fatalf("Dovecot was not retried after managed-file rollback:\n%s", log)
	}
	if strings.Contains(log, "systemctl restart postfix") {
		t.Fatalf("Postfix restarted after Dovecot restart failed:\n%s", log)
	}
}
