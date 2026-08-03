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

type serviceCandidateTestAgent struct{}

func (*serviceCandidateTestAgent) ServiceCandidateVersion(_ *transport.InstallServiceRequest, _ *string) error {
	return errors.New("private package-manager detail")
}

func TestServiceCandidateDoesNotHideAgentFailure(t *testing.T) {
	server := rpc.NewServer()
	if err := server.RegisterName("Agent", new(serviceCandidateTestAgent)); err != nil {
		t.Fatalf("register service candidate test agent: %v", err)
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
		t.Fatalf("connect service candidate test agent: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	panel := &Panel{agentClient: transport.NewReconnectingClientWithContextConnector(client, connector)}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/service/candidate?id=nginx", nil)

	panel.handleServiceCandidate(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body=%q", recorder.Code, http.StatusInternalServerError, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "private package-manager detail") {
		t.Fatal("agent failure detail leaked to the client")
	}
}
