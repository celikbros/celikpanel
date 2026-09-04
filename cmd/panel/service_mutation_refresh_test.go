package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

type ServiceRefreshEmpty struct{}

type ServiceRefreshUninstallRequest struct {
	ID      string
	Package string
}

type ServiceRefreshUninstallResponse struct {
	Removed         bool
	Detail          string
	Error           string
	PartialSuccess  bool
	MutationApplied bool
}

type ServiceRefreshWireResponse struct {
	Wired  bool
	Detail string
	Error  string
}

type ServiceRefreshRoundcubeResponse struct {
	MutationApplied bool
	Removed         bool
	Error           string
}

type ServiceRefreshWebmailRequest struct {
	transport.ServiceMutationBinding
}

type ServiceRefreshWebmailResponse struct {
	Configured bool
	Present    bool
	Error      string
}

type ServiceRefreshNodeRequest struct {
	Version string
}

type ServiceRefreshNodeResponse struct {
	Removed bool
	Error   string
}

func (a *serviceStateAtomicAgent) UninstallService(
	_ *ServiceRefreshUninstallRequest,
	out *ServiceRefreshUninstallResponse,
) error {
	a.uninstallCalls.Add(1)
	if a.uninstallReplyError == "" || a.uninstallMutation {
		a.serviceRemoved.Store(true)
	}
	out.Removed = a.uninstallReplyError == ""
	out.Error = a.uninstallReplyError
	out.PartialSuccess = a.uninstallMutation
	out.MutationApplied = a.uninstallMutation
	return nil
}

func (a *serviceStateAtomicAgent) WireMailFilters(
	_ *ServiceRefreshEmpty,
	out *ServiceRefreshWireResponse,
) error {
	a.wireFilterCalls.Add(1)
	out.Error = a.wireFilterReply
	out.Wired = out.Error == ""
	return nil
}

func (a *serviceStateAtomicAgent) RemoveRoundcube(
	req *ServiceRefreshWebmailRequest,
	out *ServiceRefreshRoundcubeResponse,
) error {
	callNumber := a.roundcubeCalls.Add(1)
	a.roundcubeRequestID = req.ServiceMutationBinding.MutationRequestID
	a.roundcubeOwnerID = req.ServiceMutationBinding.MutationOwnerID
	a.durableMutationRPCFixture.mu.Lock()
	a.roundcubeBindings = append(a.roundcubeBindings, req.ServiceMutationBinding)
	a.durableMutationRPCFixture.mu.Unlock()
	if callNumber == 1 && a.roundcubeCallHook != nil {
		a.roundcubeCallHook()
	}
	if !a.roundcubeRemovedFalse &&
		(callNumber > a.roundcubeRPCFailures || a.roundcubeFailureApplies) {
		a.roundcubePresent.Store(false)
	}
	if callNumber <= a.roundcubeRPCFailures {
		return errors.New("simulated lost Roundcube response")
	}
	if a.roundcubeRemovedFalse {
		a.roundcubePresent.Store(true)
	}
	out.MutationApplied = !a.roundcubeRemovedFalse && a.roundcubeMutation
	if a.roundcubeFailureApplies && a.roundcubeRPCFailures > 0 &&
		callNumber > a.roundcubeRPCFailures {
		// The earlier lost call already retired the tree, so this successful
		// retry is the idempotent already-absent result, not fresh proof that
		// the retry mutated the host.
		out.MutationApplied = false
	}
	out.Removed = !a.roundcubeRemovedFalse
	out.Error = a.roundcubeReplyError
	return nil
}

func (a *serviceStateAtomicAgent) ConfigureWebmail(
	req *ServiceRefreshWebmailRequest,
	out *ServiceRefreshWebmailResponse,
) error {
	callNumber := a.webmailCalls.Add(1)
	a.webmailRequestID = req.ServiceMutationBinding.MutationRequestID
	a.webmailOwnerID = req.ServiceMutationBinding.MutationOwnerID
	a.durableMutationRPCFixture.mu.Lock()
	a.webmailBindings = append(a.webmailBindings, req.ServiceMutationBinding)
	a.durableMutationRPCFixture.mu.Unlock()
	if callNumber <= a.webmailRPCFailures {
		return errors.New("simulated lost webmail configuration response")
	}
	out.Configured = !a.webmailConfiguredFalse
	out.Present = a.webmailPresent
	out.Error = a.webmailReplyError
	return nil
}

func (a *serviceStateAtomicAgent) FirewallStatus(
	_ *ServiceRefreshEmpty,
	out *FirewallStatusResp,
) error {
	if a.firewallStatusError != nil {
		return a.firewallStatusError
	}
	// A disabled firewall needs no policy rewrite after uninstall.
	// Kapalı güvenlik duvarı kaldırma sonrasında politika yazımı gerektirmez.
	out.Enabled = false
	out.EngineAvailable = true
	return nil
}

func TestRoundcubeUninstallUsesOneDurableBindingAndAuditsAfterCleanup(t *testing.T) {
	panel, agent, database, userID := newServiceStateAtomicPanel(t)
	recorder := httptest.NewRecorder()
	panel.handleServiceUninstall(recorder, serviceStateAtomicRequest(
		http.MethodPost, "/api/v1/service/uninstall", `{"service_id":"roundcube"}`, userID,
	))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Success bool `json:"success"`
		Removed bool `json:"removed"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Success || !response.Removed {
		t.Fatalf("response=%+v raw=%s", response, recorder.Body.String())
	}
	if agent.roundcubeCalls.Load() != 1 || agent.webmailCalls.Load() != 1 {
		t.Fatalf("remove calls=%d configure calls=%d", agent.roundcubeCalls.Load(), agent.webmailCalls.Load())
	}
	if agent.roundcubeRequestID == "" || agent.roundcubeOwnerID == "" ||
		agent.roundcubeRequestID != agent.webmailRequestID || agent.roundcubeOwnerID != agent.webmailOwnerID {
		t.Fatalf("remove binding=%q/%q configure binding=%q/%q",
			agent.roundcubeRequestID, agent.roundcubeOwnerID, agent.webmailRequestID, agent.webmailOwnerID)
	}
	agent.durableMutationRPCFixture.mu.Lock()
	jobCount := len(agent.durableMutationRPCFixture.jobs)
	agent.durableMutationRPCFixture.mu.Unlock()
	if jobCount != 1 {
		t.Fatalf("durable jobs=%d want 1", jobCount)
	}
	var action string
	if err := database.GetDB().QueryRow(`SELECT action FROM audit_logs`).Scan(&action); err != nil {
		t.Fatal(err)
	}
	if action != "service.uninstall:roundcube" {
		t.Fatalf("audit action=%q", action)
	}
}

func TestRoundcubeUninstallAlreadyAbsentIsIdempotentSuccessWithoutMutationClaim(t *testing.T) {
	panel, agent, database, userID := newServiceStateAtomicPanel(t)
	agent.roundcubeMutation = false
	payload, err := json.Marshal(map[string]string{`service_id`: `roundcube`})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	panel.handleServiceUninstall(recorder, serviceStateAtomicRequest(
		http.MethodPost, `/api/v1/service/uninstall`, string(payload), userID,
	))
	if recorder.Code != http.StatusOK {
		t.Fatalf(`status=%d body=%s`, recorder.Code, recorder.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body[`success`] != true || body[`removed`] != true {
		t.Fatalf(`body=%v raw=%s`, body, recorder.Body.String())
	}
	for _, key := range []string{`error`, `partial_success`, `mutation_applied`} {
		if _, present := body[key]; present {
			t.Fatalf(`idempotent success emitted %q: %s`, key, recorder.Body.String())
		}
	}
	if agent.roundcubeCalls.Load() != 1 || agent.webmailCalls.Load() != 1 || agent.getServicesCalls.Load() != 1 {
		t.Fatalf(`remove=%d configure=%d scans=%d`, agent.roundcubeCalls.Load(), agent.webmailCalls.Load(), agent.getServicesCalls.Load())
	}
	agent.durableMutationRPCFixture.mu.Lock()
	removeBindings := append([]transport.ServiceMutationBinding(nil), agent.roundcubeBindings...)
	webmailBindings := append([]transport.ServiceMutationBinding(nil), agent.webmailBindings...)
	jobCount := len(agent.durableMutationRPCFixture.jobs)
	agent.durableMutationRPCFixture.mu.Unlock()
	if len(removeBindings) != 1 || len(webmailBindings) != 1 ||
		removeBindings[0] != webmailBindings[0] ||
		removeBindings[0].MutationRequestID == `` || removeBindings[0].MutationOwnerID == `` {
		t.Fatalf(`remove bindings=%+v configure bindings=%+v`, removeBindings, webmailBindings)
	}
	if jobCount != 1 {
		t.Fatalf(`durable jobs=%d want 1`, jobCount)
	}
	var action string
	if err := database.GetDB().QueryRow(`SELECT action FROM audit_logs`).Scan(&action); err != nil {
		t.Fatal(err)
	}
	if action != `service.uninstall:roundcube` {
		t.Fatalf(`audit action=%q`, action)
	}
}

func TestRoundcubeUninstallRetriesLostResponsesWithTheSameDurableBinding(t *testing.T) {
	panel, agent, database, userID := newServiceStateAtomicPanel(t)
	agent.roundcubePresent.Store(true)
	agent.roundcubeRPCFailures = 1
	agent.roundcubeFailureApplies = true
	agent.webmailRPCFailures = 1

	recorder := httptest.NewRecorder()
	panel.handleServiceUninstall(recorder, serviceStateAtomicRequest(
		http.MethodPost, "/api/v1/service/uninstall", `{"service_id":"roundcube"}`, userID,
	))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if agent.roundcubeCalls.Load() != 2 || agent.webmailCalls.Load() != 2 {
		t.Fatalf("remove calls=%d configure calls=%d", agent.roundcubeCalls.Load(), agent.webmailCalls.Load())
	}
	agent.durableMutationRPCFixture.mu.Lock()
	removeBindings := append([]transport.ServiceMutationBinding(nil), agent.roundcubeBindings...)
	webmailBindings := append([]transport.ServiceMutationBinding(nil), agent.webmailBindings...)
	jobCount := len(agent.durableMutationRPCFixture.jobs)
	agent.durableMutationRPCFixture.mu.Unlock()
	if len(removeBindings) != 2 || len(webmailBindings) != 2 {
		t.Fatalf("remove bindings=%+v configure bindings=%+v", removeBindings, webmailBindings)
	}
	wantBinding := removeBindings[0]
	if wantBinding.MutationRequestID == "" || wantBinding.MutationOwnerID == "" {
		t.Fatalf("empty durable binding=%+v", wantBinding)
	}
	for _, binding := range append(removeBindings[1:], webmailBindings...) {
		if binding != wantBinding {
			t.Fatalf("binding=%+v want=%+v", binding, wantBinding)
		}
	}
	if jobCount != 1 {
		t.Fatalf("durable jobs=%d want 1", jobCount)
	}
	var successAudits int
	if err := database.GetDB().QueryRow(
		`SELECT COUNT(*) FROM audit_logs WHERE action = 'service.uninstall:roundcube'`,
	).Scan(&successAudits); err != nil {
		t.Fatal(err)
	}
	if successAudits != 1 {
		t.Fatalf("success audits=%d want 1", successAudits)
	}
}

func TestRoundcubeUninstallSurvivesClientCancellationAndPublishesFreshState(t *testing.T) {
	panel, agent, database, userID := newServiceStateAtomicPanel(t)
	agent.roundcubePresent.Store(true)
	const oldData = `{"observations":[{"id":"roundcube","is_installed":true,"status":"installed"}]}`
	const oldScannedAt = "2026-07-20T01:02:03Z"
	if _, err := database.GetDB().Exec(
		`INSERT INTO service_scan_cache (id, data, scanned_at) VALUES (1, ?, ?)`, oldData, oldScannedAt,
	); err != nil {
		t.Fatal(err)
	}

	request := serviceStateAtomicRequest(
		http.MethodPost, "/api/v1/service/uninstall", `{"service_id":"roundcube"}`, userID,
	)
	canceledContext, cancelRequest := context.WithCancel(request.Context())
	request = request.WithContext(canceledContext)
	agent.roundcubeCallHook = cancelRequest

	recorder := httptest.NewRecorder()
	panel.handleServiceUninstall(recorder, request)

	if canceledContext.Err() != context.Canceled {
		t.Fatalf("request context error=%v", canceledContext.Err())
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if agent.getServicesCalls.Load() != 1 {
		t.Fatalf("fresh scans=%d want 1", agent.getServicesCalls.Load())
	}
	var gotData, gotScannedAt string
	if err := database.GetDB().QueryRow(
		`SELECT data, scanned_at FROM service_scan_cache WHERE id = 1`,
	).Scan(&gotData, &gotScannedAt); err != nil {
		t.Fatal(err)
	}
	if gotData == oldData || gotScannedAt == oldScannedAt {
		t.Fatalf("fresh snapshot was not persisted: data=%q scanned_at=%q", gotData, gotScannedAt)
	}
	var auditAction string
	var auditUserID int
	if err := database.GetDB().QueryRow(
		`SELECT action, user_id FROM audit_logs`,
	).Scan(&auditAction, &auditUserID); err != nil {
		t.Fatal(err)
	}
	if auditAction != "service.uninstall:roundcube" || auditUserID != userID {
		t.Fatalf("audit action=%q user_id=%d", auditAction, auditUserID)
	}
}

func TestRoundcubeUninstallPostRemovalErrorStillConfiguresAndReportsPartial(t *testing.T) {
	panel, agent, database, userID := newServiceStateAtomicPanel(t)
	agent.roundcubePresent.Store(true)
	agent.roundcubeReplyError = "Roundcube tree retired but post-removal cleanup failed"

	recorder := httptest.NewRecorder()
	panel.handleServiceUninstall(recorder, serviceStateAtomicRequest(
		http.MethodPost, "/api/v1/service/uninstall", `{"service_id":"roundcube"}`, userID,
	))

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var body apiErrorBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != errCodeWebmailUninstallPartial || !body.PartialSuccess || !body.MutationApplied {
		t.Fatalf("body=%+v raw=%s", body, recorder.Body.String())
	}
	if agent.roundcubeCalls.Load() != 1 || agent.webmailCalls.Load() != 1 {
		t.Fatalf("remove calls=%d configure calls=%d", agent.roundcubeCalls.Load(), agent.webmailCalls.Load())
	}
	agent.durableMutationRPCFixture.mu.Lock()
	jobCount := len(agent.durableMutationRPCFixture.jobs)
	agent.durableMutationRPCFixture.mu.Unlock()
	if jobCount != 1 {
		t.Fatalf("durable jobs=%d want 1", jobCount)
	}
	var partialAudits, successAudits int
	if err := database.GetDB().QueryRow(
		`SELECT COUNT(*) FROM audit_logs WHERE action LIKE 'service.uninstall.partial:roundcube — %'`,
	).Scan(&partialAudits); err != nil {
		t.Fatal(err)
	}
	if err := database.GetDB().QueryRow(
		`SELECT COUNT(*) FROM audit_logs WHERE action = 'service.uninstall:roundcube'`,
	).Scan(&successAudits); err != nil {
		t.Fatal(err)
	}
	if partialAudits != 1 || successAudits != 0 {
		t.Fatalf("partial audits=%d success audits=%d", partialAudits, successAudits)
	}
}

func TestRoundcubeUninstallUncertainFreshScanInstalledFailsWithoutMutationClaim(t *testing.T) {
	panel, agent, database, userID := newServiceStateAtomicPanel(t)
	agent.roundcubePresent.Store(true)
	agent.roundcubeRPCFailures = 2

	recorder := httptest.NewRecorder()
	panel.handleServiceUninstall(recorder, serviceStateAtomicRequest(
		http.MethodPost, "/api/v1/service/uninstall", `{"service_id":"roundcube"}`, userID,
	))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var body apiErrorBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.MutationApplied || body.PartialSuccess || body.Code != errCodeInternal {
		t.Fatalf("installed state was reported as an applied mutation: body=%+v raw=%s", body, recorder.Body.String())
	}
	if agent.roundcubeCalls.Load() != 2 || agent.webmailCalls.Load() != 1 {
		t.Fatalf("remove calls=%d configure calls=%d", agent.roundcubeCalls.Load(), agent.webmailCalls.Load())
	}
	var failedAudits, partialAudits, successAudits int
	if err := database.GetDB().QueryRow(
		`SELECT COUNT(*) FROM audit_logs WHERE action LIKE 'service.uninstall.failed:roundcube — %'`,
	).Scan(&failedAudits); err != nil {
		t.Fatal(err)
	}
	if err := database.GetDB().QueryRow(
		`SELECT COUNT(*) FROM audit_logs WHERE action LIKE 'service.uninstall.partial:roundcube%'`,
	).Scan(&partialAudits); err != nil {
		t.Fatal(err)
	}
	if err := database.GetDB().QueryRow(
		`SELECT COUNT(*) FROM audit_logs WHERE action = 'service.uninstall:roundcube'`,
	).Scan(&successAudits); err != nil {
		t.Fatal(err)
	}
	if failedAudits != 1 || partialAudits != 0 || successAudits != 0 {
		t.Fatalf("failed audits=%d partial audits=%d success audits=%d", failedAudits, partialAudits, successAudits)
	}
}

func TestRoundcubeUninstallFalseConfirmationsFailClosed(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*serviceStateAtomicAgent)
		wantCode  string
		wantCalls int32
	}{
		{
			name: "removal not confirmed",
			configure: func(agent *serviceStateAtomicAgent) {
				agent.roundcubeRemovedFalse = true
			},
			wantCode: errCodeInternal, wantCalls: 0,
		},
		{
			name: "cleanup not confirmed",
			configure: func(agent *serviceStateAtomicAgent) {
				agent.webmailConfiguredFalse = true
			},
			wantCode: errCodeWebmailUninstallPartial, wantCalls: 1,
		},
		{
			name: "roundcube still present",
			configure: func(agent *serviceStateAtomicAgent) {
				agent.webmailPresent = true
			},
			wantCode: errCodeWebmailUninstallPartial, wantCalls: 1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			panel, agent, database, userID := newServiceStateAtomicPanel(t)
			test.configure(agent)
			recorder := httptest.NewRecorder()
			panel.handleServiceUninstall(recorder, serviceStateAtomicRequest(
				http.MethodPost, "/api/v1/service/uninstall", `{"service_id":"roundcube"}`, userID,
			))

			if recorder.Code == http.StatusOK || strings.Contains(recorder.Body.String(), `"success":true`) {
				t.Fatalf("false confirmation reported success: status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			var body apiErrorBody
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Code != test.wantCode {
				t.Fatalf("code=%q want=%q body=%s", body.Code, test.wantCode, recorder.Body.String())
			}
			if got := agent.webmailCalls.Load(); got != test.wantCalls {
				t.Fatalf("configure calls=%d want=%d", got, test.wantCalls)
			}
			var successAudits int
			if err := database.GetDB().QueryRow(
				`SELECT COUNT(*) FROM audit_logs WHERE action = 'service.uninstall:roundcube'`,
			).Scan(&successAudits); err != nil {
				t.Fatal(err)
			}
			if successAudits != 0 {
				t.Fatalf("success audits=%d want 0", successAudits)
			}
		})
	}
}

func TestRoundcubeUninstallFinalizationFailureRefreshesCacheAndReportsPartial(t *testing.T) {
	panel, agent, database, userID := newServiceStateAtomicPanel(t)
	agent.finishReplyError = "durable lease finalization refused"
	const oldData = `{"observations":[{"id":"roundcube","is_installed":true,"status":"active (running)"}]}`
	const oldScannedAt = "2026-07-20T01:02:03Z"
	if _, err := database.GetDB().Exec(
		`INSERT INTO service_scan_cache (id, data, scanned_at) VALUES (1, ?, ?)`, oldData, oldScannedAt,
	); err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	panel.handleServiceUninstall(recorder, serviceStateAtomicRequest(
		http.MethodPost, "/api/v1/service/uninstall", `{"service_id":"roundcube"}`, userID,
	))
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var body apiErrorBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != errCodeWebmailUninstallPartial || !body.PartialSuccess || !body.MutationApplied {
		t.Fatalf("unexpected partial body=%+v raw=%s", body, recorder.Body.String())
	}
	if agent.getServicesCalls.Load() != 1 {
		t.Fatalf("fresh scans=%d want 1", agent.getServicesCalls.Load())
	}
	var gotData, gotScannedAt string
	if err := database.GetDB().QueryRow(
		`SELECT data, scanned_at FROM service_scan_cache WHERE id = 1`,
	).Scan(&gotData, &gotScannedAt); err != nil {
		t.Fatal(err)
	}
	if gotData == oldData || gotScannedAt == oldScannedAt {
		t.Fatalf("fresh snapshot was not persisted: data=%q scanned_at=%q", gotData, gotScannedAt)
	}
	var partialAudits, successAudits int
	if err := database.GetDB().QueryRow(
		`SELECT COUNT(*) FROM audit_logs WHERE action LIKE 'service.uninstall.partial:roundcube — %'`,
	).Scan(&partialAudits); err != nil {
		t.Fatal(err)
	}
	if err := database.GetDB().QueryRow(
		`SELECT COUNT(*) FROM audit_logs WHERE action = 'service.uninstall:roundcube'`,
	).Scan(&successAudits); err != nil {
		t.Fatal(err)
	}
	if partialAudits != 1 || successAudits != 0 {
		t.Fatalf("partial audits=%d success audits=%d", partialAudits, successAudits)
	}
}

func TestRoundcubeUninstallPartialKeepsRefreshFailurePrecedence(t *testing.T) {
	panel, agent, _, userID := newServiceStateAtomicPanel(t)
	agent.webmailConfiguredFalse = true
	agent.getServicesError = errors.New("state probe unavailable")
	recorder := httptest.NewRecorder()
	panel.handleServiceUninstall(recorder, serviceStateAtomicRequest(
		http.MethodPost, "/api/v1/service/uninstall", `{"service_id":"roundcube"}`, userID,
	))

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var body apiErrorBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != errCodeServiceStateRefreshFailed || !body.PartialSuccess || !body.MutationApplied {
		t.Fatalf("refresh failure lost precedence: body=%+v raw=%s", body, recorder.Body.String())
	}
}

func TestRoundcubeUninstallPreMutationFailureAndScanFailureDoesNotClaimMutation(t *testing.T) {
	panel, agent, _, userID := newServiceStateAtomicPanel(t)
	agent.roundcubeRemovedFalse = true
	agent.getServicesError = errors.New("state probe unavailable")

	recorder := httptest.NewRecorder()
	panel.handleServiceUninstall(recorder, serviceStateAtomicRequest(
		http.MethodPost, "/api/v1/service/uninstall", `{"service_id":"roundcube"}`, userID,
	))

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var body apiErrorBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != errCodeServiceStateRefreshFailed || body.PartialSuccess || body.MutationApplied {
		t.Fatalf("pre-mutation failure claimed partial success or a mutation: body=%+v raw=%s", body, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), `"partial_success"`) ||
		strings.Contains(recorder.Body.String(), `"mutation_applied"`) {
		t.Fatalf("false outcome flags must be omitted: %s", recorder.Body.String())
	}
	if !strings.Contains(body.Error, "outcome and current service state could not be verified") {
		t.Fatalf("ambiguous outcome message=%q", body.Error)
	}
}

func TestRoundcubeUninstallAlreadyAbsentCleanupFailureDoesNotClaimMutation(t *testing.T) {
	panel, agent, _, userID := newServiceStateAtomicPanel(t)
	agent.roundcubeMutation = false
	agent.webmailConfiguredFalse = true

	recorder := httptest.NewRecorder()
	panel.handleServiceUninstall(recorder, serviceStateAtomicRequest(
		http.MethodPost, "/api/v1/service/uninstall", `{"service_id":"roundcube"}`, userID,
	))

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var body apiErrorBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != errCodeWebmailUninstallPartial || !body.PartialSuccess || body.MutationApplied {
		t.Fatalf("already-absent cleanup response=%+v raw=%s", body, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), `"mutation_applied"`) {
		t.Fatalf("false mutation flag must be omitted: %s", recorder.Body.String())
	}
	if !strings.Contains(body.Error, "removal is no longer detected") {
		t.Fatalf("already-absent-safe message=%q", body.Error)
	}
}

func TestRoundcubeUninstallLostResponseThenAlreadyAbsentScanFailureDoesNotClaimMutation(t *testing.T) {
	panel, agent, _, userID := newServiceStateAtomicPanel(t)
	agent.roundcubePresent.Store(true)
	agent.roundcubeRPCFailures = 1
	agent.roundcubeFailureApplies = true
	agent.getServicesError = errors.New("state probe unavailable")

	recorder := httptest.NewRecorder()
	panel.handleServiceUninstall(recorder, serviceStateAtomicRequest(
		http.MethodPost, "/api/v1/service/uninstall", `{"service_id":"roundcube"}`, userID,
	))

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var body apiErrorBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != errCodeServiceStateRefreshFailed || body.PartialSuccess || body.MutationApplied {
		t.Fatalf("lost response/idempotent retry claimed an outcome: body=%+v raw=%s", body, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), `"partial_success"`) ||
		strings.Contains(recorder.Body.String(), `"mutation_applied"`) {
		t.Fatalf("unverified outcome flags must be omitted: %s", recorder.Body.String())
	}
}

func TestUninstallFirewallFailurePublishesFreshSnapshotAsPartialSuccess(t *testing.T) {
	panel, agent, database, userID := newServiceStateAtomicPanel(t)
	const oldData = `{"observations":[{"id":"redis","is_installed":true,"status":"active (running)"}]}`
	const oldScannedAt = "2026-07-20T01:02:03Z"
	if _, err := database.GetDB().Exec(
		`INSERT INTO service_scan_cache (id, data, scanned_at) VALUES (1, ?, ?)`,
		oldData, oldScannedAt,
	); err != nil {
		t.Fatal(err)
	}
	agent.firewallStatusError = errors.New("firewall status unavailable")

	recorder := httptest.NewRecorder()
	panel.handleServiceUninstall(recorder, serviceStateAtomicRequest(
		http.MethodPost,
		"/api/v1/service/uninstall",
		`{"service_id":"redis"}`,
		userID,
	))

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var body apiErrorBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != errCodeFirewallSyncFailed || !body.PartialSuccess || !body.MutationApplied {
		t.Fatalf("unexpected partial-success body=%+v raw=%s", body, recorder.Body.String())
	}

	var gotData, gotScannedAt string
	if err := database.GetDB().QueryRow(
		`SELECT data, scanned_at FROM service_scan_cache WHERE id = 1`,
	).Scan(&gotData, &gotScannedAt); err != nil {
		t.Fatal(err)
	}
	if gotData == oldData || gotScannedAt == oldScannedAt {
		t.Fatalf("fresh snapshot was not persisted: data=%q scanned_at=%q", gotData, gotScannedAt)
	}
	var snapshot scanCacheDoc
	if err := json.Unmarshal([]byte(gotData), &snapshot); err != nil {
		t.Fatal(err)
	}
	for _, observation := range snapshot.Observations {
		if observation.ID == "redis" && observation.IsInstalled {
			t.Fatalf("fresh snapshot still reports removed redis installed: %+v", observation)
		}
	}

	// The cached GET is what the frontend reloads after this partial success;
	// it must expose the verified post-removal snapshot, not the old row.
	// Bu kısmi başarıdan sonra ön yüzün yeniden yüklediği kaynak cache GET'idir;
	// eski satırı değil, kaldırma sonrası doğrulanmış snapshot'ı sunmalıdır.
	getRecorder := httptest.NewRecorder()
	panel.handleManagedServices(getRecorder, serviceStateAtomicRequest(
		http.MethodGet, "/api/v1/managed-services", "", userID,
	))
	if getRecorder.Code != http.StatusOK {
		t.Fatalf("cached GET status=%d body=%s", getRecorder.Code, getRecorder.Body.String())
	}
	var payload managedServicesPayload
	if err := json.Unmarshal(getRecorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	for _, service := range payload.Services {
		if service.ID == "redis" && service.Installed() {
			t.Fatalf("cached GET kept the removed redis row installed: %+v", service)
		}
	}
}

func TestSpamFilterUninstallReportsMailFilterSyncPartialSuccess(t *testing.T) {
	panel, agent, _, userID := newServiceStateAtomicPanel(t)
	agent.wireFilterReply = "postfix reload failed"

	recorder := httptest.NewRecorder()
	panel.handleServiceUninstall(recorder, serviceStateAtomicRequest(
		http.MethodPost,
		"/api/v1/service/uninstall",
		`{"service_id":"rspamd"}`,
		userID,
	))
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var body apiErrorBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != errCodeMailFilterSyncFailed || !body.PartialSuccess || !body.MutationApplied {
		t.Fatalf("unexpected partial-success body=%+v raw=%s", body, recorder.Body.String())
	}
	if agent.wireFilterCalls.Load() != 1 {
		t.Fatalf("wire filter calls=%d want 1", agent.wireFilterCalls.Load())
	}
	if agent.getServicesCalls.Load() != 1 {
		t.Fatalf("fresh scans=%d want 1", agent.getServicesCalls.Load())
	}
}

func TestPurgeFailureAfterStopReportsVerifiedUninstallPartialSuccess(t *testing.T) {
	panel, agent, _, userID := newServiceStateAtomicPanel(t)
	agent.uninstallReplyError = "package removal failed: apt purge failed"
	agent.uninstallMutation = true

	recorder := httptest.NewRecorder()
	panel.handleServiceUninstall(recorder, serviceStateAtomicRequest(
		http.MethodPost,
		"/api/v1/service/uninstall",
		`{"service_id":"redis"}`,
		userID,
	))
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var body apiErrorBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != errCodeServiceUninstallPartial || !body.PartialSuccess || !body.MutationApplied {
		t.Fatalf("unexpected partial-success body=%+v raw=%s", body, recorder.Body.String())
	}
	if agent.getServicesCalls.Load() != 1 {
		t.Fatalf("fresh scans=%d want 1", agent.getServicesCalls.Load())
	}
}

func (a *serviceStateAtomicAgent) RemoveNodeVersion(
	_ *ServiceRefreshNodeRequest,
	out *ServiceRefreshNodeResponse,
) error {
	a.nodeRemoveCalls.Add(1)
	out.Removed = true
	return nil
}

func TestSuccessfulRemovalsReportRefreshFailureWithoutReplacingCache(t *testing.T) {
	tests := []struct {
		name          string
		request       func(int) *http.Request
		handle        func(*Panel, http.ResponseWriter, *http.Request)
		successAudit  string
		failurePrefix string
		callCount     func(*serviceStateAtomicAgent) int32
	}{
		{
			name: "normal uninstall",
			request: func(userID int) *http.Request {
				return serviceStateAtomicRequest(http.MethodPost, "/api/v1/service/uninstall", `{"service_id":"redis"}`, userID)
			},
			handle: func(panel *Panel, w http.ResponseWriter, r *http.Request) {
				panel.handleServiceUninstall(w, r)
			},
			successAudit:  "service.uninstall:redis",
			failurePrefix: "service.uninstall.refresh.failed:redis — ",
			callCount:     func(agent *serviceStateAtomicAgent) int32 { return agent.uninstallCalls.Load() },
		},
		{
			name: "roundcube uninstall",
			request: func(userID int) *http.Request {
				return serviceStateAtomicRequest(http.MethodPost, "/api/v1/service/uninstall", `{"service_id":"roundcube"}`, userID)
			},
			handle: func(panel *Panel, w http.ResponseWriter, r *http.Request) {
				panel.handleServiceUninstall(w, r)
			},
			successAudit:  "service.uninstall:roundcube",
			failurePrefix: "service.uninstall.refresh.failed:roundcube — ",
			callCount:     func(agent *serviceStateAtomicAgent) int32 { return agent.roundcubeCalls.Load() },
		},
		{
			name: "node remove",
			request: func(userID int) *http.Request {
				return serviceStateAtomicRequest(http.MethodDelete, "/api/v1/runtimes/node/22.4.1", "", userID)
			},
			handle: func(panel *Panel, w http.ResponseWriter, r *http.Request) {
				panel.handleNodeRuntimeSub(w, r)
			},
			successAudit:  "runtime.node.remove:22.4.1",
			failurePrefix: "runtime.node.remove.refresh.failed:22.4.1 — ",
			callCount:     func(agent *serviceStateAtomicAgent) int32 { return agent.nodeRemoveCalls.Load() },
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			panel, agent, database, userID := newServiceStateAtomicPanel(t)
			const oldData = `{"observations":[{"id":"redis","is_installed":true,"status":"active (running)"}]}`
			const oldScannedAt = "2026-07-20T01:02:03Z"
			if _, err := database.GetDB().Exec(
				`INSERT INTO service_scan_cache (id, data, scanned_at) VALUES (1, ?, ?)`,
				oldData, oldScannedAt,
			); err != nil {
				t.Fatal(err)
			}
			agent.getServicesError = errors.New("state probe unavailable")

			recorder := httptest.NewRecorder()
			test.handle(panel, recorder, test.request(userID))

			if recorder.Code != http.StatusBadGateway {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			var body apiErrorBody
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Code != errCodeServiceStateRefreshFailed || !body.PartialSuccess || !body.MutationApplied {
				t.Fatalf("unexpected partial-success body=%+v raw=%s", body, recorder.Body.String())
			}
			if got := test.callCount(agent); got != 1 {
				t.Fatalf("host mutation calls=%d want 1", got)
			}

			var gotData, gotScannedAt string
			if err := database.GetDB().QueryRow(
				`SELECT data, scanned_at FROM service_scan_cache WHERE id = 1`,
			).Scan(&gotData, &gotScannedAt); err != nil {
				t.Fatal(err)
			}
			if gotData != oldData || gotScannedAt != oldScannedAt {
				t.Fatalf("cache changed to data=%q scanned_at=%q", gotData, gotScannedAt)
			}

			rows, err := database.GetDB().Query(`SELECT action FROM audit_logs ORDER BY id`)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			var actions []string
			for rows.Next() {
				var action string
				if err := rows.Scan(&action); err != nil {
					t.Fatal(err)
				}
				actions = append(actions, action)
			}
			if len(actions) != 2 || actions[0] != test.successAudit || !strings.HasPrefix(actions[1], test.failurePrefix) {
				t.Fatalf("audit actions=%q", actions)
			}
		})
	}
}
