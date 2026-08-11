package main

import (
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

type cronMutationTestAgent struct {
	success bool
	err     error
}

func (a *cronMutationTestAgent) AddCronJob(_ *transport.AddCronJobRequest, reply *bool) error {
	*reply = a.success
	return a.err
}

func newCronMutationTestPanel(t *testing.T, agent *cronMutationTestAgent) *Panel {
	t.Helper()
	server := rpc.NewServer()
	if err := server.RegisterName("Agent", agent); err != nil {
		t.Fatalf("register cron test agent: %v", err)
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
		t.Fatalf("connect cron test agent: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return &Panel{
		pkgFamilyVal: "apt",
		agentClient:  transport.NewReconnectingClientWithContextConnector(client, connector),
	}
}

func TestAddCronJobDoesNotReportFailedMutationAsSuccess(t *testing.T) {
	panel := newCronMutationTestPanel(t, &cronMutationTestAgent{success: false})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/domains/1/cron", strings.NewReader(`{"schedule":"0 3 * * *","command":"true"}`))

	panel.handleAddCronJob(recorder, request, "site-user")

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body=%q", recorder.Code, http.StatusInternalServerError, recorder.Body.String())
	}
}

func TestAddCronJobDoesNotLeakRPCFailure(t *testing.T) {
	panel := newCronMutationTestPanel(t, &cronMutationTestAgent{err: errors.New("secret agent detail")})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/domains/1/cron", strings.NewReader(`{"schedule":"0 3 * * *","command":"true"}`))

	panel.handleAddCronJob(recorder, request, "site-user")

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if strings.Contains(recorder.Body.String(), "secret agent detail") {
		t.Fatal("RPC failure detail leaked to the client")
	}
}
