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
	verifiedAPTAgentRPCFixture

	success bool
	err     error

	// What the agent actually received, so a test can pin the identity that
	// travelled rather than the one the handler happened to hold.
	// Agent'in gerçekte ne aldığı; böylece bir test, işleyicinin elinde tuttuğu
	// kimliği değil, yolculuk eden kimliği sabitleyebilir.
	received transport.AddCronJobRequest
}

func (a *cronMutationTestAgent) AddCronJob(req *transport.AddCronJobRequest, reply *bool) error {
	a.received = *req
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

// testCronTenant is one concrete customer's site. The IDs matter: they are the
// only unique half of the identity, and the agent re-derives everything else
// from them.
// testCronTenant, tek bir müşterinin sitesidir. Numaralar önemlidir: kimliğin
// benzersiz olan tek yarısı onlardır ve agent geri kalan her şeyi onlardan
// yeniden türetir.
var testCronTenant = transport.CronTenant{SubscriptionID: 12, DomainID: 34, Domain: "example.com"}

// The identity the panel sends must be the tenant, not a username the panel
// derived. A derived username is not unique across tenants, so sending one
// would make the agent's ownership proof impossible.
// Panelin gönderdiği kimlik, panelin türettiği bir kullanıcı adı değil kiracı
// olmalıdır. Türetilmiş kullanıcı adı kiracılar arasında benzersiz değildir;
// birini göndermek agent'in sahiplik kanıtını imkânsız kılardı.
func TestAddCronJobSendsTheTenantIdentityNotAUsername(t *testing.T) {
	agent := &cronMutationTestAgent{success: true}
	panel := newCronMutationTestPanel(t, agent)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/domains/34/cron", strings.NewReader(`{"schedule":"0 3 * * *","command":"true"}`))

	panel.handleAddCronJob(recorder, request, testCronTenant)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%q", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if agent.received.CronTenant != testCronTenant {
		t.Fatalf("agent received %+v, want %+v", agent.received.CronTenant, testCronTenant)
	}
}

func TestAddCronJobDoesNotReportFailedMutationAsSuccess(t *testing.T) {
	panel := newCronMutationTestPanel(t, &cronMutationTestAgent{success: false})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/domains/1/cron", strings.NewReader(`{"schedule":"0 3 * * *","command":"true"}`))

	panel.handleAddCronJob(recorder, request, testCronTenant)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body=%q", recorder.Code, http.StatusInternalServerError, recorder.Body.String())
	}
}

func TestAddCronJobDoesNotLeakRPCFailure(t *testing.T) {
	panel := newCronMutationTestPanel(t, &cronMutationTestAgent{err: errors.New("secret agent detail")})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/domains/1/cron", strings.NewReader(`{"schedule":"0 3 * * *","command":"true"}`))

	panel.handleAddCronJob(recorder, request, testCronTenant)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if strings.Contains(recorder.Body.String(), "secret agent detail") {
		t.Fatal("RPC failure detail leaked to the client")
	}
}
