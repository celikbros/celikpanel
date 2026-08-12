package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/rpc"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/core"
	paneldb "github.com/alicelik/celikpanel/internal/db"
	"github.com/alicelik/celikpanel/internal/transport"
)

type RepoHandlerTestRequest struct {
	RepoID string
}

type repoHandlerTestAgent struct {
	durableMutationRPCFixture
	status      RepoStatusResp
	statusErr   error
	packages    RepoPackagesResp
	packagesErr error
	enable      RepoStatusResp
	enableErr   error
	disable     RepoStatusResp
	disableErr  error
}

func (a *repoHandlerTestAgent) RepoStatus(_ *RepoHandlerTestRequest, out *RepoStatusResp) error {
	*out = a.status
	return a.statusErr
}

func (a *repoHandlerTestAgent) RepoPackages(_ *RepoHandlerTestRequest, out *RepoPackagesResp) error {
	*out = a.packages
	return a.packagesErr
}

func (a *repoHandlerTestAgent) EnableRepo(_ *RepoHandlerTestRequest, out *RepoStatusResp) error {
	*out = a.enable
	return a.enableErr
}

func (a *repoHandlerTestAgent) DisableRepo(_ *RepoHandlerTestRequest, out *RepoStatusResp) error {
	*out = a.disable
	return a.disableErr
}

func (a *repoHandlerTestAgent) ServiceMutationStatus(_ *ServiceOperationMutationStatusRequest, out *ServiceOperationMutationResponse) error {
	out.Job = nil
	out.Error = ""
	return nil
}

func attachRepoHandlerTestAgent(t *testing.T, panel *Panel, agent *repoHandlerTestAgent) {
	t.Helper()
	panel.pkgFamilyVal = "apt"
	server := rpc.NewServer()
	if err := server.RegisterName("Agent", agent); err != nil {
		t.Fatalf("register repo test agent: %v", err)
	}
	connector := func(ctx context.Context) (*rpc.Client, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		serverConn, clientConn := net.Pipe()
		go server.ServeConn(serverConn)
		return rpc.NewClient(clientConn), nil
	}
	client, err := connector(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	panel.agentClient = transport.NewReconnectingClientWithContextConnector(client, connector)
	t.Cleanup(func() { _ = client.Close() })
}

func testManagedRepo(t *testing.T) *core.ManagedRepo {
	t.Helper()
	service := core.GetManagedServiceByID("postgresql")
	if service == nil || service.Repo == nil {
		t.Fatal("postgresql managed repository missing from catalog")
	}
	return service.Repo
}

func TestNormalizeRepoErrorCodeRejectsUntrustedValues(t *testing.T) {
	if got := normalizeRepoErrorCode("agent-invented-code", errCodeRepoStatusFailed); got != errCodeRepoStatusFailed {
		t.Fatalf("normalizeRepoErrorCode = %q, want fallback", got)
	}
	if got := normalizeRepoErrorCode(errCodeRepoKeyUntrusted, errCodeRepoStatusFailed); got != errCodeRepoKeyUntrusted {
		t.Fatalf("normalizeRepoErrorCode = %q, want pinned code", got)
	}
}

func TestRepoInfoNeverSerializesRawAgentError(t *testing.T) {
	const secret = "apt stderr: /private/path/key.gpg"
	panel := &Panel{}
	attachRepoHandlerTestAgent(t, panel, &repoHandlerTestAgent{
		status: RepoStatusResp{Error: secret, ErrorCode: errCodeRepoKeyUntrusted},
	})

	payload, err := json.Marshal(panel.repoInfo(testManagedRepo(t)))
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	if strings.Contains(text, secret) {
		t.Fatalf("raw agent diagnostic leaked to browser JSON: %s", text)
	}
	if !strings.Contains(text, errCodeRepoKeyUntrusted) {
		t.Fatalf("stable error code missing from browser JSON: %s", text)
	}
}

func TestRepoInfoDistinguishesStatusAndPackageTransportFailures(t *testing.T) {
	t.Run("status", func(t *testing.T) {
		panel := &Panel{}
		attachRepoHandlerTestAgent(t, panel, &repoHandlerTestAgent{statusErr: errors.New("transport secret")})
		if got := panel.repoInfo(testManagedRepo(t)).ErrorCode; got != errCodeRepoStatusUnavailable {
			t.Fatalf("ErrorCode = %q, want %q", got, errCodeRepoStatusUnavailable)
		}
	})

	t.Run("packages", func(t *testing.T) {
		panel := &Panel{}
		attachRepoHandlerTestAgent(t, panel, &repoHandlerTestAgent{
			status:      RepoStatusResp{Enabled: true},
			packagesErr: errors.New("transport secret"),
		})
		if got := panel.repoInfo(testManagedRepo(t)).ErrorCode; got != errCodeRepoPackagesUnavailable {
			t.Fatalf("ErrorCode = %q, want %q", got, errCodeRepoPackagesUnavailable)
		}
	})
}

func TestRepoMutationHandlerDistinguishesRollbackFromPartialMutation(t *testing.T) {
	tests := []struct {
		name            string
		status          RepoStatusResp
		wantHTTP        int
		wantAudit       string
		wantPartial     bool
		wantMutation    bool
		forbiddenDetail string
	}{
		{
			name: "rollback completed",
			status: RepoStatusResp{
				Error: "apt stderr: internal mirror path", ErrorCode: errCodeRepoDisableFailed,
			},
			wantHTTP:        http.StatusConflict,
			wantAudit:       "repo.disable.failed:pgdg \u2014 " + errCodeRepoDisableFailed,
			forbiddenDetail: "internal mirror path",
		},
		{
			name: "rollback incomplete",
			status: RepoStatusResp{
				Error: "rollback stderr: secret path", ErrorCode: errCodeRepoDisableFailed,
				PartialSuccess: true, MutationApplied: true,
			},
			wantHTTP:        http.StatusBadGateway,
			wantAudit:       "repo.disable.partial:pgdg \u2014 " + errCodeRepoDisableFailed,
			wantPartial:     true,
			wantMutation:    true,
			forbiddenDetail: "secret path",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database, err := paneldb.NewSQLiteDB(filepath.Join(t.TempDir(), "panel.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(database.Close)
			panel := &Panel{db: database}
			attachRepoHandlerTestAgent(t, panel, &repoHandlerTestAgent{disable: test.status})

			req := httptest.NewRequest(http.MethodPost, "/api/v1/repo", strings.NewReader(`{"service_id":"postgresql","action":"disable"}`))
			req = req.WithContext(context.WithValue(req.Context(), callerKey, &Caller{Role: roleAdmin}))
			recorder := httptest.NewRecorder()
			panel.handleRepo(recorder, req)
			if recorder.Code != test.wantHTTP {
				t.Fatalf("HTTP status = %d, want %d; body=%s", recorder.Code, test.wantHTTP, recorder.Body.String())
			}
			var body apiErrorBody
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Code != errCodeRepoDisableFailed {
				t.Fatalf("error code = %q, want %q", body.Code, errCodeRepoDisableFailed)
			}
			if body.PartialSuccess != test.wantPartial || body.MutationApplied != test.wantMutation {
				t.Fatalf("partial/mutation = %t/%t, want %t/%t", body.PartialSuccess, body.MutationApplied, test.wantPartial, test.wantMutation)
			}
			if strings.Contains(recorder.Body.String(), test.forbiddenDetail) {
				t.Fatalf("raw agent diagnostic leaked to browser: %s", recorder.Body.String())
			}

			var action string
			if err := database.GetDB().QueryRow(`SELECT action FROM audit_logs ORDER BY id DESC LIMIT 1`).Scan(&action); err != nil {
				t.Fatal(err)
			}
			if action != test.wantAudit {
				t.Fatalf("audit action = %q, want %q", action, test.wantAudit)
			}
		})
	}
}
