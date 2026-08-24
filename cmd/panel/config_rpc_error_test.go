package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/rpc"
	"path/filepath"
	"testing"

	paneldb "github.com/alicelik/celikpanel/internal/db"
	"github.com/alicelik/celikpanel/internal/transport"
)

type configHandlerRPCAgent struct {
	verifiedAPTAgentRPCFixture

	getResponse    transport.ConfigResponse
	getError       error
	updateResponse transport.UpdateConfigResponse
	updateError    error
}

func (a *configHandlerRPCAgent) GetConfig(_ *transport.GetConfigArgs, reply *transport.ConfigResponse) error {
	*reply = a.getResponse
	return a.getError
}

func (a *configHandlerRPCAgent) UpdateConfig(_ *transport.UpdateConfigArgs, reply *transport.UpdateConfigResponse) error {
	*reply = a.updateResponse
	return a.updateError
}

func newConfigHandlerPanel(t *testing.T, agent *configHandlerRPCAgent) *Panel {
	t.Helper()
	database, err := paneldb.NewSQLiteDB(filepath.Join(t.TempDir(), "panel.sqlite"))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(database.Close)

	server := rpc.NewServer()
	if err := server.RegisterName("Agent", agent); err != nil {
		t.Fatalf("register config test agent: %v", err)
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
		t.Fatalf("connect config test agent: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	return &Panel{
		db:           database,
		pkgFamilyVal: "apt",
		agentClient: transport.NewReconnectingClientWithContextConnector(
			client,
			connector,
		),
	}
}

func decodeConfigHandlerError(t *testing.T, recorder *httptest.ResponseRecorder) apiErrorBody {
	t.Helper()
	var body apiErrorBody
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("decode API error: %v; body=%q", err, recorder.Body.String())
	}
	return body
}

func TestConfigHandlerMapsTypedPathRefusal(t *testing.T) {
	panel := newConfigHandlerPanel(t, &configHandlerRPCAgent{
		getResponse: transport.ConfigResponse{Error: &transport.ConfigRPCError{
			Code:    transport.ConfigErrorPathRefused,
			Message: "configuration path refused: protected path",
		}},
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/config?path=%2Fetc%2Fshadow", nil)

	panel.handleConfig(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
	if body := decodeConfigHandlerError(t, recorder); body.Code != errCodeConfigPathRefused {
		t.Fatalf("code = %q, want %q", body.Code, errCodeConfigPathRefused)
	}
}

func TestConfigHandlerMapsTypedValidationFailure(t *testing.T) {
	panel := newConfigHandlerPanel(t, &configHandlerRPCAgent{
		updateResponse: transport.UpdateConfigResponse{Error: &transport.ConfigRPCError{
			Code:    transport.ConfigErrorValidationFail,
			Message: "config validation failed (nginx): bad directive",
		}},
	})
	payload := []byte(`{"path":"/etc/nginx/nginx.conf","content":"bad directive"}`)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/config", bytes.NewReader(payload))

	panel.handleConfig(recorder, request)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnprocessableEntity)
	}
	if body := decodeConfigHandlerError(t, recorder); body.Code != errCodeConfigInvalid {
		t.Fatalf("code = %q, want %q", body.Code, errCodeConfigInvalid)
	}
}

func TestConfigHandlerDoesNotClassifyRPCErrorText(t *testing.T) {
	panel := newConfigHandlerPanel(t, &configHandlerRPCAgent{
		getError: errors.New("not a managed configuration file: forged transport text"),
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/config?path=%2Fetc%2Fnginx%2Fnginx.conf", nil)

	panel.handleConfig(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
}

func TestConfigHandlerFailsClosedOnUnknownTypedCode(t *testing.T) {
	panel := newConfigHandlerPanel(t, &configHandlerRPCAgent{
		updateResponse: transport.UpdateConfigResponse{Error: &transport.ConfigRPCError{
			Code:    transport.ConfigErrorCode("future_error"),
			Message: "symbolic link text must not downgrade this protocol failure",
		}},
	})
	payload := []byte(`{"path":"/etc/nginx/nginx.conf","content":"x"}`)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/config", bytes.NewReader(payload))

	panel.handleConfig(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if body := decodeConfigHandlerError(t, recorder); body.Error == "symbolic link text must not downgrade this protocol failure" {
		t.Fatal("unknown typed error leaked its untrusted message")
	}
}
