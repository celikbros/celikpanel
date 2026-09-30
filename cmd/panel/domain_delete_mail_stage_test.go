package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/rpc"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/alicelik/celikpanel/internal/transport"
)

// mailStageFailingAgent answers DeleteMailDomain with a server-side error, as
// the Arch Agent did in pair 4 t2 (P4-2).
type mailStageFailingAgent struct {
	*domainDeletionRPCAgent
	failuresRemaining int
	failure           string
}

func (a *mailStageFailingAgent) DeleteMailDomain(
	req *transport.DeleteMailDomainRequest,
	resp *transport.DeleteMailDomainResponse,
) error {
	a.callsMu.Lock()
	a.mailRequests = append(a.mailRequests, *req)
	fail := a.failuresRemaining > 0
	if fail {
		a.failuresRemaining--
	}
	a.callsMu.Unlock()
	if fail {
		return errors.New(a.failure)
	}
	resp.Applied = true
	return nil
}

func mailRequestCount(agent *domainDeletionRPCAgent) int {
	agent.callsMu.Lock()
	defer agent.callsMu.Unlock()
	return len(agent.mailRequests)
}

func decodeDeletionBody(t *testing.T, body []byte) map[string]string {
	t.Helper()
	var decoded map[string]string
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode deletion body %s: %v", body, err)
	}
	return decoded
}

func TestDeleteDNSOnlyDomainWithoutMailRecordsSkipsMailStage(t *testing.T) {
	p := newDNSPanelForTest(t)
	domainID, _ := seedDomainDeletionLedger(t, p, "dns-only.example", "dnsonly")
	withPanelBuildCommit(t, "domain-delete-test")
	agent := &domainDeletionRPCAgent{commit: "domain-delete-test"}
	attachDomainDeletionRPCAgent(t, p, agent)

	recorder := deleteDomainForSagaTest(t, p, domainID)
	if recorder.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if got := mailRequestCount(agent); got != 0 {
		t.Fatalf("mail cleanup requests = %d, want 0 (no mail runtime for this domain)", got)
	}
	var remaining int
	if err := p.db.GetDB().QueryRow(`SELECT COUNT(*) FROM domains WHERE id = ?`, domainID).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatal("deletion did not pass the DNS stage and finalize")
	}
}

func TestDeleteDomainRunsMailStageWhenPanelInstalledMail(t *testing.T) {
	p := newDNSPanelForTest(t)
	domainID, _ := seedDomainDeletionLedger(t, p, "mail-host.example", "dnsonly")
	if _, err := p.db.GetDB().Exec(`
		INSERT INTO service_operations (id, kind, service_id, status, phase, started_at, created_at, updated_at, request_id)
		VALUES ('mail-install', 'service_install', 'postfix', 'succeeded', 'done', '2026-09-30', '2026-09-30', '2026-09-30', 'mail-install')`); err != nil {
		t.Fatal(err)
	}
	withPanelBuildCommit(t, "domain-delete-test")
	agent := &domainDeletionRPCAgent{commit: "domain-delete-test"}
	attachDomainDeletionRPCAgent(t, p, agent)

	recorder := deleteDomainForSagaTest(t, p, domainID)
	if recorder.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if got := mailRequestCount(agent); got != 1 {
		t.Fatalf("mail cleanup requests = %d, want 1", got)
	}
}

func TestDomainHasProductMailRuntimeUsesOnlyPanelRecords(t *testing.T) {
	p := newDNSPanelForTest(t)
	domainID, _ := seedDomainDeletionLedger(t, p, "records.example", "dnsonly")
	db := p.db.GetDB()
	check := func(want bool) {
		t.Helper()
		got, err := domainHasProductMailRuntime(context.Background(), db, domainID)
		if err != nil || got != want {
			t.Fatalf("has mail runtime = %v, %v; want %v", got, err, want)
		}
	}
	check(false)
	for _, statement := range []string{
		// A failed install and an unrelated service are not a mail runtime.
		`INSERT INTO service_operations (id, kind, service_id, status, phase, started_at, created_at, updated_at) VALUES ('f', 'service_install', 'postfix', 'failed', 'failed', 'x', 'x', 'x')`,
		`INSERT INTO service_operations (id, kind, service_id, status, phase, started_at, created_at, updated_at) VALUES ('n', 'service_install', 'nginx', 'succeeded', 'done', 'x', 'x', 'x')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	check(false)
	for _, statement := range []string{
		fmt.Sprintf(`INSERT INTO email_forwardings (domain_id, source, destination) VALUES (%d, 'a@records.example', 'b@else.test')`, domainID),
		`DELETE FROM email_forwardings`,
		fmt.Sprintf(`INSERT INTO mail_catch_all (domain_id, destination) VALUES (%d, 'c@else.test')`, domainID),
		`DELETE FROM mail_catch_all`,
		fmt.Sprintf(`INSERT INTO email_accounts (domain_id, address, password_hash) VALUES (%d, 'u@records.example', 'x')`, domainID),
		`DELETE FROM email_accounts`,
		`INSERT INTO service_operations (id, kind, service_id, status, phase, started_at, created_at, updated_at) VALUES ('m', 'mail_profile_install', 'web_mail', 'succeeded', 'done', 'x', 'x', 'x')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(statement, "INSERT") {
			check(true)
		} else {
			check(false)
		}
	}
}

func TestMailStageFailureIsVerifiedTypedAndSaved(t *testing.T) {
	p := newDNSPanelForTest(t)
	domain := "arch-mail.example"
	domainID, _ := seedDomainDeletionLedger(t, p, domain, "dnsonly")
	if _, err := p.db.GetDB().Exec(`
		INSERT INTO email_accounts (domain_id, address, password_hash, quota_mb)
		VALUES (?, ?, 'managed-by-agent', 100)`, domainID, "user@"+domain); err != nil {
		t.Fatal(err)
	}
	withPanelBuildCommit(t, "domain-delete-test")
	inner := &domainDeletionRPCAgent{commit: "domain-delete-test"}
	agent := &mailStageFailingAgent{
		domainDeletionRPCAgent: inner,
		failuresRemaining:      1,
		failure: "publish mailbox directory: open mail root for domain quarantine: mail storage refused: " +
			"a symbolic link is in the path (/var/mail/vhosts): too many levels of symbolic links\nsecond line {SHA512-CRYPT}$6$salt$hash",
	}
	attachDomainDeletionRPCAgent(t, p, inner)
	// Re-register with the failing DeleteMailDomain override.
	server := rpc.NewServer()
	if err := server.RegisterName("Agent", agent); err != nil {
		t.Fatal(err)
	}
	p.agentClient = pipeAgentClientForTest(t, server)

	first := deleteDomainForSagaTest(t, p, domainID)
	requireDeletionPendingStage(t, first, domainDeletionStageMailRuntime)
	body := decodeDeletionBody(t, first.Body.Bytes())
	if body["reason"] != domainDeletionReasonMailCleanupFailed {
		t.Fatalf("reason = %q; body=%v", body["reason"], body)
	}
	line := body["error_line"]
	if !strings.Contains(line, "too many levels of symbolic links") || strings.Contains(line, "second line") ||
		strings.Contains(line, "$6$") || utf8.RuneCountInString(line) > domainDeletionErrorLineMax {
		t.Fatalf("error_line = %q", line)
	}
	if body["message"] != domainMailCleanupFailedEnglish {
		t.Fatalf("message = %q", body["message"])
	}
	if len(inner.syncRequests) != 0 {
		t.Fatalf("DNS stage started after a mail failure: %d", len(inner.syncRequests))
	}

	code, saved := readDeletionStatusForTest(t, p, domainID)
	if code != http.StatusOK || saved.Status != domainDeletionStatusFailed ||
		saved.Stage != domainDeletionStageMailRuntime || saved.Reason != domainDeletionReasonMailCleanupFailed ||
		saved.ErrorLine != line {
		t.Fatalf("saved status = %d %+v", code, saved)
	}

	// The same retry repeats the mail stage, succeeds and clears the record.
	second := deleteDomainForSagaTest(t, p, domainID)
	if second.Code != http.StatusOK {
		t.Fatalf("retry status=%d body=%s", second.Code, second.Body.String())
	}
	var remaining int
	if err := p.db.GetDB().QueryRow(`SELECT COUNT(*) FROM panel_settings WHERE key = ?`,
		domainDeletionFailureKey(domainID)).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("failure record survived the completed deletion")
	}
	if got := mailRequestCount(inner); got != 2 {
		t.Fatalf("mail cleanup requests = %d, want 2", got)
	}
}

func TestSavedDeletionStatusIgnoresForeignOrMalformedFailureRecord(t *testing.T) {
	p := newDNSPanelForTest(t)
	domainID, _ := seedDomainDeletionLedger(t, p, "foreign.example", "dnsonly")
	if _, err := p.markDomainDeletionPending(context.Background(), domainID); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{
		`not json`,
		fmt.Sprintf(`{"version":1,"domain_id":%d,"domain":"other.example","stage":"mail_runtime_cleanup","reason":"mail_runtime_cleanup_failed","error_line":"x","recorded_at":"t"}`, domainID),
		fmt.Sprintf(`{"version":2,"domain_id":%d,"domain":"foreign.example","stage":"mail_runtime_cleanup","reason":"mail_runtime_cleanup_failed","error_line":"x","recorded_at":"t"}`, domainID),
		fmt.Sprintf(`{"version":1,"domain_id":%d,"domain":"foreign.example","stage":"site_cleanup","reason":"mail_runtime_cleanup_failed","error_line":"x","recorded_at":"t"}`, domainID),
	} {
		if _, err := p.db.GetDB().Exec(`INSERT INTO panel_settings (key, value) VALUES (?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value`, domainDeletionFailureKey(domainID), value); err != nil {
			t.Fatal(err)
		}
		code, saved := readDeletionStatusForTest(t, p, domainID)
		if code != http.StatusOK || saved.Status != "unknown" || saved.Reason != "" {
			t.Fatalf("value %s: saved status = %d %+v", value, code, saved)
		}
	}
}

func TestClassifyMailCleanupFailureSeparatesVerifiedFromUnknown(t *testing.T) {
	if _, ok := classifyDomainMailCleanupFailure(fmt.Errorf("remove mail domain runtime: %w", rpc.ServerError("boom"))); !ok {
		t.Fatal("agent-answered error was not verified")
	}
	if _, ok := classifyDomainMailCleanupFailure(errDomainMailCleanupNotConfirmed); !ok {
		t.Fatal("unconfirmed convergence was not verified")
	}
	for _, err := range []error{
		fmt.Errorf("remove mail domain runtime: %w", context.DeadlineExceeded),
		fmt.Errorf("remove mail domain runtime: %w", rpc.ErrShutdown),
		errors.New("verify domain deletion marker before mail cleanup: sql: no rows"),
	} {
		if _, ok := classifyDomainMailCleanupFailure(err); ok {
			t.Fatalf("%v was classified as a verified failure", err)
		}
	}
}

func TestBoundedDeletionErrorLine(t *testing.T) {
	long := strings.Repeat("é", 400)
	if got := boundedDeletionErrorLine(long); utf8.RuneCountInString(got) > domainDeletionErrorLineMax || !utf8.ValidString(got) {
		t.Fatalf("long line = %d runes", utf8.RuneCountInString(got))
	}
	if got := boundedDeletionErrorLine("user@x:{SHA512-CRYPT}$6$abc$def rejected"); strings.Contains(got, "$6$") || strings.Contains(got, "SHA512") {
		t.Fatalf("hash leaked: %q", got)
	}
	if got := boundedDeletionErrorLine("\n"); got == "" {
		t.Fatal("empty line must still name that no detail exists")
	}
}

func pipeAgentClientForTest(t *testing.T, server *rpc.Server) *transport.ReconnectingClient {
	t.Helper()
	connector := func(ctx context.Context) (*rpc.Client, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		serverConn, clientConn := net.Pipe()
		go server.ServeConn(serverConn)
		return rpc.NewClient(clientConn), nil
	}
	rawClient, err := connector(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rawClient.Close() })
	return transport.NewReconnectingClientWithContextConnector(rawClient, connector)
}
