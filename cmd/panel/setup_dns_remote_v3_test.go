package main

import (
	"context"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func TestRemoteDNSReceiverUsesExactV3AndRecoversBeforePairReady(t *testing.T) {
	f := newRemoteDNSHTTPFixture(t)
	activatePairedPrimaryBINDForV3Test(t, f.receiver)
	agent := newDNSZoneV3TestAgent()
	agent.pairReady = true
	attachDNSZoneV3TestAgent(t, f.receiver, agent)
	oldNameservers := remoteDNSNameserversReady
	t.Cleanup(func() { remoteDNSNameserversReady = oldNameservers })
	remoteDNSNameserversReady = func(*Panel, context.Context) (bool, error) { return true, nil }
	remoteDNSReadLocalAuthority = (*Panel).remoteDNSLocalAuthority
	remoteDNSPublishReceived = func(p *Panel, ctx context.Context, domain string, deleted bool) error {
		return p.syncZoneToDNSLocked(ctx, domain, deleted)
	}
	f.connect(t)
	f.domain(t, "customer.test", nil)
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.44")
	configureDNSZoneV3PendingSync(agent, false)
	baseSync := agent.syncResponseHook
	agent.syncResponseHook = func(request transport.SyncDNSZoneV3Request, response *transport.SyncDNSZoneV3Response) error {
		err := baseSync(request, response)
		agent.mu.Lock()
		agent.pairReady = false
		agent.mu.Unlock()
		return err
	}
	configureDNSZoneV3PendingRecovery(agent)
	if err := f.origin.ensureRemoteDomainDNS(context.Background(), "customer.test"); err == nil {
		t.Fatal("pending paired V3 propagation became remote success")
	}
	lease, err := readDNSZoneEngineLease(context.Background(), f.receiver.db.GetDB(), "customer.test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.origin.remoteDNSConnectionReadiness(context.Background(), f.id); err == nil {
		t.Fatal("unready pair became infrastructure proof")
	}
	agent.mu.Lock()
	agent.recoverHook = nil
	agent.mu.Unlock()
	// Current readiness is still false, but this exact previously authorized
	// pending generation must reach the local V3 reconciliation machinery.
	_ = f.origin.syncRemoteDomainDNS(context.Background(), "customer.test", false)
	var generation, applied int64
	if err = f.origin.db.GetDB().QueryRow(`SELECT generation,applied_generation FROM remote_dns_zones`).Scan(&generation, &applied); err != nil || generation != 1 || applied != 1 {
		t.Fatalf("pending V3 generation did not recover while pair was unready: %v %d/%d", err, applied, generation)
	}
	agent.mu.Lock()
	requests := append([]transport.SyncDNSZoneV3Request(nil), agent.requests...)
	recoveries := append([]transport.RecoverDNSZoneV3Request(nil), agent.recoverRequests...)
	agent.pairReady = true
	agent.mu.Unlock()
	if len(requests) != 1 || len(recoveries) < 1 {
		t.Fatalf("V3 request/recovery sequence: %d/%d", len(requests), len(recoveries))
	}
	for _, recovery := range recoveries {
		if recovery.MutationRequestID != lease.RequestID || recovery.MutationOwnerID != lease.OwnerID || recovery.Qualifier != lease.Qualifier {
			t.Fatal("remote retry changed V3 recovery authority")
		}
	}
	addressCorrect := false
	for _, record := range requests[0].Records {
		if record.Name == "customer.test" && record.Type == "A" && record.Content == "192.0.2.44" {
			addressCorrect = true
		}
	}
	if !addressCorrect || requests[0].Engine != transport.DNSEngineBIND || requests[0].EngineEpoch != lease.EngineEpoch {
		t.Fatal("remote records did not cross exact receiver engine boundary")
	}
	if err = f.origin.syncRemoteDomainDNS(context.Background(), "customer.test", false); err != nil {
		t.Fatal(err)
	}
}
