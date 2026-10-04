package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

type deferredRetryLog struct {
	mu    sync.Mutex
	lines []string
}

func (l *deferredRetryLog) logf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}

func (l *deferredRetryLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.lines...)
}

func immediateDeferredRetry(
	attempts int,
	idle func(context.Context) bool,
	logs *deferredRetryLog,
) startupDeferredRetry {
	return startupDeferredRetry{
		interval: 0,
		attempts: attempts,
		idle:     idle,
		wait:     waitStartupDeferredRetry,
		logf:     logs.logf,
	}
}

func alwaysIdle(context.Context) bool { return true }

func TestStartupDeferredRetryDecisionRetriesOnlyABusyRefusal(t *testing.T) {
	busy := &hostMutationBusyError{reason: transport.HostMutationReasonHostLock}
	for _, testCase := range []struct {
		name string
		err  error
		want bool
	}{
		{"completed", nil, false},
		{"busy sentinel", errHostMutationBusy, true},
		{"busy with reason", busy, true},
		{"wrapped busy", fmt.Errorf("publish full mail SNI snapshot: %w", busy), true},
		{"joined busy", errors.Join(fmt.Errorf("x: %w", busy), errors.New("tlsa")), true},
		{"verified failure", errors.New("mail server hostname is not a valid FQDN"), false},
		{"terminal uncertain", &agentMutationTerminalUncertainError{
			kind: "mail_tls_sync", err: busy,
		}, false},
	} {
		if got := startupWorkDeferredForBusyHost(testCase.err); got != testCase.want {
			t.Fatalf("%s: deferred = %t, want %t", testCase.name, got, testCase.want)
		}
	}
}

func TestStartupDeferredRetryNoteMatchesThePolicy(t *testing.T) {
	window := startupDeferredRetryInterval * startupDeferredRetryAttempts
	if startupDeferredRetryInterval != 30*time.Second || window != 10*time.Minute {
		t.Fatalf("retry policy %s x %d no longer matches the journal note",
			startupDeferredRetryInterval, startupDeferredRetryAttempts)
	}
	if !strings.Contains(startupDeferredRetryNote, "every 30 seconds") ||
		!strings.Contains(startupDeferredRetryNote, "up to 10 minutes") {
		t.Fatalf("note %q does not state the policy", startupDeferredRetryNote)
	}
}

func TestStartupDeferredRetryNothingDeferredStartsNothing(t *testing.T) {
	panel := &Panel{}
	if tasks := panel.deferredStartupMailTasks(startupCertificateDeferral{}, false); len(tasks) != 0 {
		t.Fatalf("tasks without a busy refusal = %d, want 0", len(tasks))
	}
	if panel.startDeferredStartupMailWork(startupCertificateDeferral{}, false) {
		t.Fatal("a retry was started although startup deferred nothing")
	}
	// A pending outbox alone (startup not refused as busy) is not deferred.
	if tasks := panel.deferredStartupMailTasks(
		startupCertificateDeferral{pendingDomains: []int{7}}, false,
	); len(tasks) != 0 {
		t.Fatalf("tasks for an undeferred outbox = %d, want 0", len(tasks))
	}
}

func TestStartupDeferredRetryRunsOnceTheHostIsIdle(t *testing.T) {
	var probes, calls atomic.Int32
	idle := func(context.Context) bool { return probes.Add(1) >= 3 }
	logs := &deferredRetryLog{}
	remaining := immediateDeferredRetry(20, idle, logs).run(
		context.Background(),
		[]startupDeferredTask{{
			name: "mail filter wiring",
			run: func(context.Context) (string, error) {
				calls.Add(1)
				return `milters="inet:localhost:11332" maps=hash`, nil
			},
		}},
	)
	if len(remaining) != 0 || calls.Load() != 1 || probes.Load() != 3 {
		t.Fatalf("remaining=%v calls=%d probes=%d, want none/1/3",
			remaining, calls.Load(), probes.Load())
	}
	lines := logs.snapshot()
	if len(lines) != 1 ||
		!strings.Contains(lines[0], "attempt 3 of 20") ||
		!strings.Contains(lines[0], "mail filter wiring completed") {
		t.Fatalf("journal lines = %q", lines)
	}
}

func TestStartupDeferredRetryGivesUpWithTheOwnerLine(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		idle      func(context.Context) bool
		wantCalls int32
	}{
		{"host never idle", func(context.Context) bool { return false }, 0},
		{"admission keeps refusing", alwaysIdle, 4},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var calls atomic.Int32
			logs := &deferredRetryLog{}
			retry := immediateDeferredRetry(4, testCase.idle, logs)
			retry.interval = 30 * time.Second
			retry.wait = func(context.Context, time.Duration) bool { return true }
			remaining := retry.run(context.Background(), []startupDeferredTask{{
				name:        "mail certificate publication",
				ownerAction: "use Retry on its SSL page",
				run: func(context.Context) (string, error) {
					calls.Add(1)
					return "", &hostMutationBusyError{
						reason: transport.HostMutationReasonHostLock,
					}
				},
			}})
			if calls.Load() != testCase.wantCalls {
				t.Fatalf("calls = %d, want %d", calls.Load(), testCase.wantCalls)
			}
			if len(remaining) != 1 || remaining[0] != "mail certificate publication" {
				t.Fatalf("remaining = %v", remaining)
			}
			lines := logs.snapshot()
			if len(lines) != 1 {
				t.Fatalf("journal lines = %q, want one give-up line", lines)
			}
			for _, want := range []string{
				"attempt 4 of 4",
				"still not done after 2 minutes",
				"changed nothing",
				"Mail keeps running",
				"sudo systemctl restart celikpanel-panel",
				"use Retry on its SSL page",
			} {
				if !strings.Contains(lines[0], want) {
					t.Fatalf("give-up line %q lacks %q", lines[0], want)
				}
			}
		})
	}
}

func TestStartupDeferredRetryNeverRepeatsACompletedTask(t *testing.T) {
	var first, second atomic.Int32
	logs := &deferredRetryLog{}
	remaining := immediateDeferredRetry(5, alwaysIdle, logs).run(
		context.Background(),
		[]startupDeferredTask{
			{name: "first", run: func(context.Context) (string, error) {
				first.Add(1)
				return "", nil
			}},
			{name: "second", run: func(context.Context) (string, error) {
				if second.Add(1) == 1 {
					return "", errHostMutationBusy
				}
				return "", nil
			}},
		},
	)
	if len(remaining) != 0 || first.Load() != 1 || second.Load() != 2 {
		t.Fatalf("remaining=%v first=%d second=%d, want none/1/2",
			remaining, first.Load(), second.Load())
	}
	if lines := logs.snapshot(); len(lines) != 2 {
		t.Fatalf("journal lines = %q, want one per productive attempt", lines)
	}
}

func TestStartupDeferredRetryDoesNotRepeatAVerifiedFailure(t *testing.T) {
	var calls atomic.Int32
	logs := &deferredRetryLog{}
	remaining := immediateDeferredRetry(5, alwaysIdle, logs).run(
		context.Background(),
		[]startupDeferredTask{{name: "mail filter wiring",
			run: func(context.Context) (string, error) {
				calls.Add(1)
				return "", errors.New("Postfix is required before a mail filter can be wired")
			}}},
	)
	lines := logs.snapshot()
	if len(remaining) != 0 || calls.Load() != 1 || len(lines) != 1 ||
		!strings.Contains(lines[0], "failed: Postfix is required") ||
		!strings.Contains(lines[0], "next Panel start") {
		t.Fatalf("remaining=%v calls=%d lines=%q", remaining, calls.Load(), lines)
	}
}

func TestStartupDeferredRetryStopsWithTheProcess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var calls atomic.Int32
	logs := &deferredRetryLog{}
	retry := immediateDeferredRetry(5, alwaysIdle, logs)
	retry.interval = time.Hour
	remaining := retry.run(ctx, []startupDeferredTask{{name: "x",
		run: func(context.Context) (string, error) { calls.Add(1); return "", nil }}})
	if len(remaining) != 1 || calls.Load() != 0 || len(logs.snapshot()) != 0 {
		t.Fatalf("remaining=%v calls=%d lines=%q", remaining, calls.Load(), logs.snapshot())
	}
}

// busyAdmissionStartupAgent refuses the first busyBegins durable admissions
// exactly as the Agent does while an update holds the host.
type busyAdmissionStartupAgent struct {
	*startupPendingAgent

	busyBegins atomic.Int32
	begins     atomic.Int32
	wires      atomic.Int32
}

func (a *busyAdmissionStartupAgent) BeginServiceMutation(
	req *ServiceOperationMutationBeginRequest,
	resp *ServiceOperationMutationResponse,
) error {
	a.begins.Add(1)
	if a.busyBegins.Load() > 0 {
		a.busyBegins.Add(-1)
		resp.ErrorCode = transport.HostMutationBusy
		resp.Error = "another server change or package-manager task is still running"
		return nil
	}
	return a.startupPendingAgent.BeginServiceMutation(req, resp)
}

func (a *busyAdmissionStartupAgent) MailFilterWiringState(
	_ *transport.Empty,
	resp *transport.MailFilterWiringStateResponse,
) error {
	resp.MailServerInstalled = true
	return nil
}

func (a *busyAdmissionStartupAgent) WireMailFilters(
	_ *transport.ServiceMutationRequest,
	resp *transport.WireMailFiltersResponse,
) error {
	a.wires.Add(1)
	resp.Wired = true
	resp.Detail = `milters="inet:localhost:11332" maps=hash`
	return nil
}

func newBusyAdmissionPendingFixture(
	t *testing.T,
	mailError string,
) (*Panel, *busyAdmissionStartupAgent, int) {
	t.Helper()
	previousHostname := readMailTLSHostname
	readMailTLSHostname = func() (string, error) { return "startup.panel.test", nil }
	t.Cleanup(func() { readMailTLSHostname = previousHostname })

	panel, subscriptionID := newStartupVhostBatchFixture(t)
	const domainName = "deferred-startup.example"
	domainID := addStartupHostedDomain(t, panel, subscriptionID, domainName, "static")
	certPath := fmt.Sprintf("/certs/deferred-startup/%d/fullchain.pem", domainID)
	keyPath := fmt.Sprintf("/certs/deferred-startup/%d/privkey.pem", domainID)
	if _, err := panel.db.GetDB().Exec(`
		UPDATE sites SET ssl_enabled = false, ssl_type = 'custom',
		       ssl_cert_path = ?, ssl_key_path = ?
		WHERE domain_id = ?`, certPath, keyPath, domainID); err != nil {
		t.Fatal(err)
	}
	if _, err := panel.db.GetDB().Exec(`
		INSERT INTO ssl_certificates (
			domain_id, type, cert_path, key_path, chain_path,
			issuer, subject, issued_at, expires_at,
			auto_renew, secure_mail, renewal_status, status
		) VALUES (?, 'custom', ?, ?, '', 'Test CA', ?, ?, ?, false, false, ?, 'active')`,
		domainID, certPath, keyPath, domainName,
		time.Now().UTC().Add(-time.Hour).Format(time.RFC3339),
		time.Now().UTC().Add(90*24*time.Hour).Format(time.RFC3339),
		sslPendingDependents,
	); err != nil {
		t.Fatal(err)
	}
	agent := &busyAdmissionStartupAgent{startupPendingAgent: &startupPendingAgent{
		serviceOperationTestAgent: newServiceOperationTestAgent(),
		certificates: map[string]StartupPendingInspectResponse{
			certPath: {
				Valid: true, Trusted: true, TrustChecked: true,
				DNSNames: []string{domainName, "www." + domainName},
			},
		},
		mailError: mailError,
	}}
	attachStartupVhostBatchAgent(t, panel, agent)
	return panel, agent, domainID
}

func readDeferredStartupPending(t *testing.T, panel *Panel, domainID int) string {
	t.Helper()
	var pending string
	if err := panel.db.GetDB().QueryRow(`
		SELECT COALESCE(renewal_status, '') FROM ssl_certificates
		WHERE domain_id = ? AND status = 'active'`, domainID).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	return pending
}

func TestStartupCertificateDependentsRefusedAsBusyCompleteOnceIdle(t *testing.T) {
	panel, agent, domainID := newBusyAdmissionPendingFixture(t, "")
	agent.busyBegins.Store(1)

	deferral := panel.reconcileCertificateRuntimeAtStartup()
	if !deferral.deferred || len(deferral.pendingDomains) != 1 ||
		deferral.pendingDomains[0] != domainID {
		t.Fatalf("startup deferral = %+v, want the busy step with domain %d", deferral, domainID)
	}
	if got := readDeferredStartupPending(t, panel, domainID); got != sslPendingDependents {
		t.Fatalf("pending after busy startup = %q, want %q", got, sslPendingDependents)
	}
	agent.mu.Lock()
	publishedAtStartup := agent.secureCalls
	agent.mu.Unlock()
	if publishedAtStartup != 0 {
		t.Fatalf("mail publications while busy = %d, want 0", publishedAtStartup)
	}

	logs := &deferredRetryLog{}
	remaining := immediateDeferredRetry(3, alwaysIdle, logs).run(
		context.Background(),
		panel.deferredStartupMailTasks(deferral, false),
	)
	if len(remaining) != 0 {
		t.Fatalf("remaining = %v, lines = %q", remaining, logs.snapshot())
	}
	if got := readDeferredStartupPending(t, panel, domainID); got != "" {
		t.Fatalf("pending after deferred retry = %q, want cleared", got)
	}
	agent.mu.Lock()
	published := agent.secureCalls
	agent.mu.Unlock()
	if published != 1 {
		t.Fatalf("mail publications after retry = %d, want 1", published)
	}
	lines := logs.snapshot()
	if len(lines) != 1 || !strings.Contains(lines[0], "mail certificate publication completed") {
		t.Fatalf("journal lines = %q", lines)
	}

	// Already done: the same outbox domain is not touched again.
	begins := agent.begins.Load()
	if err := panel.retryDeferredStartupCertificate(context.Background(), domainID); err != nil {
		t.Fatalf("retry of a completed outbox row: %v", err)
	}
	agent.mu.Lock()
	publishedAgain := agent.secureCalls
	agent.mu.Unlock()
	if agent.begins.Load() != begins || publishedAgain != 1 {
		t.Fatalf("completed row caused a mutation: begins %d->%d publications %d",
			begins, agent.begins.Load(), publishedAgain)
	}
}

func TestStartupCertificateDependentsVerifiedFailureIsNotDeferred(t *testing.T) {
	panel, _, domainID := newBusyAdmissionPendingFixture(t, "forced startup mail failure")
	deferral := panel.reconcileCertificateRuntimeAtStartup()
	if deferral.deferred {
		t.Fatalf("verified failure was deferred: %+v", deferral)
	}
	if got := readDeferredStartupPending(t, panel, domainID); got != sslPendingDependents {
		t.Fatalf("pending = %q, want %q kept for the owner", got, sslPendingDependents)
	}
}

func TestStartupMilterWiringRefusedAsBusyIsWiredOnceIdle(t *testing.T) {
	panel, agent, _ := newBusyAdmissionPendingFixture(t, "")
	agent.busyBegins.Store(1)
	if !panel.wireMailFiltersSynchronouslyAtStartup() {
		t.Fatal("busy milter wiring was not reported as deferred")
	}
	if agent.wires.Load() != 0 {
		t.Fatalf("wiring calls while busy = %d, want 0", agent.wires.Load())
	}
	logs := &deferredRetryLog{}
	remaining := immediateDeferredRetry(3, alwaysIdle, logs).run(
		context.Background(),
		panel.deferredStartupMailTasks(startupCertificateDeferral{}, true),
	)
	lines := logs.snapshot()
	if len(remaining) != 0 || agent.wires.Load() != 1 || len(lines) != 1 ||
		!strings.Contains(lines[0], `mail filter wiring completed (milters="inet:localhost:11332" maps=hash)`) {
		t.Fatalf("remaining=%v wires=%d lines=%q", remaining, agent.wires.Load(), lines)
	}
	if panel.wireMailFiltersSynchronouslyAtStartup() {
		t.Fatal("idle startup wiring reported a deferral")
	}
}
