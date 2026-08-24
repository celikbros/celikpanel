package main

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/rpc"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

type serviceConfigRPCAgent struct {
	verifiedAPTAgentRPCFixture

	phpError   error
	mysqlError error
	phpCalls   int
	mysqlCalls int
}

func (a *serviceConfigRPCAgent) UpdatePHPConfig(_ transport.UpdatePHPConfigRequest, _ *transport.Empty) error {
	a.phpCalls++
	return a.phpError
}

func (a *serviceConfigRPCAgent) UpdateMySQLConfig(_ transport.UpdateMySQLConfigRequest, _ *transport.Empty) error {
	a.mysqlCalls++
	return a.mysqlError
}

func newServiceConfigPanel(t *testing.T, agent *serviceConfigRPCAgent) *Panel {
	t.Helper()
	server := rpc.NewServer()
	if err := server.RegisterName("Agent", agent); err != nil {
		t.Fatal(err)
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
	t.Cleanup(func() { _ = client.Close() })
	return &Panel{
		pkgFamilyVal: "apt",
		agentClient:  transport.NewReconnectingClientWithContextConnector(client, connector),
	}
}

func TestUpdatePHPConfigDoesNotReturnSuccessWhenAgentActivationFails(t *testing.T) {
	agent := &serviceConfigRPCAgent{phpError: errors.New("PHP-FPM reload failed; previous configuration restored")}
	panel := newServiceConfigPanel(t, agent)
	body := `{"php_version":"8.3","memory_limit":"256M","max_execution_time":60,"upload_max_filesize":"8M","post_max_size":"16M","max_input_vars":2000}`
	recorder := httptest.NewRecorder()
	panel.handleUpdatePHPConfig(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/php/config", strings.NewReader(body)))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), `"status":"success"`) || agent.phpCalls != 1 {
		t.Fatalf("handler reported false success or skipped RPC: calls=%d body=%s", agent.phpCalls, recorder.Body.String())
	}
}

func TestUpdateMySQLConfigDoesNotReturnSuccessWhenRestartFails(t *testing.T) {
	agent := &serviceConfigRPCAgent{mysqlError: errors.New("MariaDB restart failed; previous configuration restored")}
	panel := newServiceConfigPanel(t, agent)
	body := `{"max_connections":300,"innodb_buffer_pool_size":"512M","query_cache_size":"0","max_allowed_packet":"64M"}`
	recorder := httptest.NewRecorder()
	panel.handleUpdateMySQLConfig(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/mysql/config", strings.NewReader(body)))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), `"status":"success"`) || agent.mysqlCalls != 1 {
		t.Fatalf("handler reported false success or skipped RPC: calls=%d body=%s", agent.mysqlCalls, recorder.Body.String())
	}
}

func TestUpdateServiceConfigRejectsUnknownAndMultipleJSONBeforeRPC(t *testing.T) {
	tests := []struct {
		name string
		body []byte
	}{
		{name: "unknown field", body: []byte(`{"php_version":"8.3","unknown":true}`)},
		{name: "multiple values", body: []byte(`{} {}`)},
		{name: "oversized", body: bytes.Repeat([]byte(" "), maxServiceConfigRequestBody+1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			agent := &serviceConfigRPCAgent{}
			panel := newServiceConfigPanel(t, agent)
			recorder := httptest.NewRecorder()
			panel.handleUpdatePHPConfig(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/php/config", bytes.NewReader(test.body)))
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", recorder.Code, recorder.Body.String())
			}
			if agent.phpCalls != 0 {
				t.Fatalf("invalid request reached agent %d times", agent.phpCalls)
			}
		})
	}
}
