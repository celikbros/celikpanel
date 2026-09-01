package main

import (
	"context"
	"errors"
	"net"
	"net/rpc"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

// heldMutationAgent is an agent that has poisoned itself: it reports the job
// exactly as a real one does — still running, still leased — and reports the
// hold alongside it. A real held agent behaves this way because status()
// deliberately does not fail, while heartbeat, finish and cancel all refuse.
// heldMutationAgent, kendini zehirlemiş bir agent'tır: işi gerçek bir agent
// gibi bildirir — hâlâ çalışıyor, hâlâ kiralı — ve tutulmayı da yanında
// bildirir. Gerçek bir tutulan agent böyle davranır, çünkü status() bilerek
// hata vermez; kalp atışı, bitirme ve iptal ise reddeder.
type heldMutationAgent struct {
	verifiedAPTAgentRPCFixture

	hold  string
	polls atomic.Int64
}

func (a *heldMutationAgent) ServiceMutationStatus(
	request *transport.ServiceMutationStatusRequest,
	response *transport.ServiceMutationResponse,
) error {
	a.polls.Add(1)
	response.Job = &transport.ServiceMutationJob{
		RequestID:      request.RequestID,
		OwnerID:        heldMutationOwnerID,
		Kind:           dnsEngineSwitchKind,
		Target:         "bind",
		PackageName:    heldMutationQualifier,
		Status:         "running",
		Phase:          "leased",
		Attempt:        1,
		StartedAt:      time.Now().Add(-time.Minute),
		UpdatedAt:      time.Now(),
		LeaseExpiresAt: time.Now().Add(20 * time.Second),
		DeadlineAt:     time.Now().Add(45 * time.Minute),
	}
	response.MutationHold = a.hold
	return nil
}

const (
	heldMutationRequestID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	heldMutationOwnerID   = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	heldMutationQualifier = "dns-engine-switch/v1:sha256:" +
		"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
)

func newHeldMutationPanel(t *testing.T, agent *heldMutationAgent) *Panel {
	t.Helper()
	server := rpc.NewServer()
	if err := server.RegisterName("Agent", agent); err != nil {
		t.Fatalf("register held mutation agent: %v", err)
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
		t.Fatalf("connect held mutation agent: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return &Panel{
		pkgFamilyVal: "apt",
		agentClient:  transport.NewReconnectingClientWithContextConnector(client, connector),
	}
}

// The R-019 hang, reduced to its mechanism.
//
// A DNS engine switch failed in the agent within seconds and poisoned the
// mutation manager. The panel then entered its terminal reconcile wait, whose
// budget for a DNS engine switch is thirty minutes and whose context is
// deliberately detached from the caller. The agent's status kept answering
// "running / leased" — truthfully, because a held agent cannot move a job — so
// the loop polled roughly seven thousand times and returned nothing until its
// own deadline. The operator, holding an open HTTP request the whole time, saw
// a broken stream instead of the failure the agent had known about from the
// start.
//
// The wait must end the moment the agent says it is held.
//
// R-019 asılması, mekanizmasına indirgenmiş hâli.
//
// Bir DNS motoru geçişi agent içinde saniyeler içinde düştü ve mutasyon
// yöneticisini zehirledi. Panel ardından uç uzlaştırma bekleyişine girdi; bu
// bekleyişin DNS motoru geçişi için bütçesi otuz dakika ve bağlamı bilerek
// çağırandan koparılmış. Agent'ın durumu "çalışıyor / kiralı" demeye devam
// etti — doğru söylüyordu, çünkü tutulan bir agent bir işi kımıldatamaz — ve
// döngü yaklaşık yedi bin kez yokladı, kendi son tarihine kadar hiçbir şey
// döndürmedi. Bütün o süre boyunca açık bir HTTP isteği tutan operatör,
// agent'ın en baştan bildiği arıza yerine kopmuş bir bağlantı gördü.
//
// Bekleyiş, agent tutulduğunu söylediği anda bitmelidir.
func TestTerminalWaitStopsWhenTheAgentIsHeld(t *testing.T) {
	for _, hold := range []string{
		transport.MutationHoldLedgerAmbiguous,
		transport.MutationHoldLedgerUnavailable,
	} {
		t.Run(hold, func(t *testing.T) {
			agent := &heldMutationAgent{hold: hold}
			panel := newHeldMutationPanel(t, agent)
			identity := agentMutationIdentity{
				RequestID:   heldMutationRequestID,
				OwnerID:     heldMutationOwnerID,
				Kind:        dnsEngineSwitchKind,
				Target:      "bind",
				PackageName: heldMutationQualifier,
			}

			// The real budget for this kind. Before the fix the wait consumed
			// all of it; the test would take thirty minutes rather than fail.
			// Bu tür için gerçek bütçe. Düzeltmeden önce bekleyiş bunun
			// tamamını tüketiyordu; test düşmek yerine otuz dakika sürerdi.
			if got := panelMutationTerminalReconcileTimeout(identity); got != dnsEngineSwitchTimeout {
				t.Fatalf("reconcile budget for a DNS engine switch = %v, want %v", got, dnsEngineSwitchTimeout)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			started := time.Now()
			job, err := panel.waitExpectedAgentMutationTerminal(ctx, identity)
			elapsed := time.Since(started)

			if !errors.Is(err, errAgentMutationHeld) {
				t.Fatalf("waiting on a held agent must stop with the held error, got %v", err)
			}
			if elapsed > 2*time.Second {
				t.Fatalf("the wait took %v; a held agent must end it at once", elapsed)
			}
			if polls := agent.polls.Load(); polls > 2 {
				t.Fatalf("the wait polled %d times; one answer is enough to know the agent is held", polls)
			}
			// The job is still returned: the caller is entitled to see the
			// frozen operation it was waiting on.
			// İş yine döndürülür: çağıran, beklediği donmuş işlemi görmeyi hak
			// eder.
			if job == nil || job.Status != "running" {
				t.Fatalf("the held job must still be reported, got %+v", job)
			}
		})
	}
}

// An agent that is not held must be polled as before. The fix must not turn an
// ordinary in-progress mutation into a failure.
// Tutulmayan bir agent eskisi gibi yoklanmalıdır. Düzeltme, sıradan ve devam
// eden bir mutasyonu arızaya çevirmemelidir.
func TestTerminalWaitStillPollsAnUnheldAgent(t *testing.T) {
	agent := &heldMutationAgent{hold: ""}
	panel := newHeldMutationPanel(t, agent)
	identity := agentMutationIdentity{
		RequestID:   heldMutationRequestID,
		OwnerID:     heldMutationOwnerID,
		Kind:        dnsEngineSwitchKind,
		Target:      "bind",
		PackageName: heldMutationQualifier,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
	defer cancel()

	_, err := panel.waitExpectedAgentMutationTerminal(ctx, identity)
	if errors.Is(err, errAgentMutationHeld) {
		t.Fatal("an unheld agent must not be reported as held")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("an unheld running job must be polled until the caller's deadline, got %v", err)
	}
	if polls := agent.polls.Load(); polls < 2 {
		t.Fatalf("the wait polled %d times; it must keep polling an unheld agent", polls)
	}
}
