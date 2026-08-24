package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/rpc"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/transport"
)

type fail2banMutationTestAgent struct {
	verifiedAPTAgentRPCFixture
}

func (*fail2banMutationTestAgent) Fail2banToggleJail(_ *core.Fail2banJailRequest, reply *bool) error {
	*reply = false
	return nil
}

func newFail2banMutationTestPanel(t *testing.T) *Panel {
	t.Helper()
	server := rpc.NewServer()
	if err := server.RegisterName("Agent", new(fail2banMutationTestAgent)); err != nil {
		t.Fatalf("register fail2ban test agent: %v", err)
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
		t.Fatalf("connect fail2ban test agent: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return &Panel{
		pkgFamilyVal: "apt",
		agentClient:  transport.NewReconnectingClientWithContextConnector(client, connector),
	}
}

func TestFail2banFalseMutationIsServerError(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/fail2ban/jails",
		strings.NewReader(`{"name":"sshd","enabled":true}`),
	)

	newFail2banMutationTestPanel(t).handleFail2banJails(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body=%q", recorder.Code, http.StatusInternalServerError, recorder.Body.String())
	}
}

func TestFail2banRejectsUnsupportedMethod(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/fail2ban/jails", nil)

	new(Panel).handleFail2banJails(recorder, request)

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
	}
}
