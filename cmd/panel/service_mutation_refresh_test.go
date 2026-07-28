package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
	Removed bool
	Error   string
}

type ServiceRefreshWebmailResponse struct {
	Configured bool
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
	_ *ServiceRefreshEmpty,
	out *ServiceRefreshRoundcubeResponse,
) error {
	a.roundcubeCalls.Add(1)
	out.Removed = true
	return nil
}

func (a *serviceStateAtomicAgent) ConfigureWebmail(
	_ *ServiceRefreshEmpty,
	out *ServiceRefreshWebmailResponse,
) error {
	out.Configured = true
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
		if service.ID == "redis" && service.IsInstalled {
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
