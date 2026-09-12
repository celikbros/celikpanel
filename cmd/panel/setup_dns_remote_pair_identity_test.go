package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func newRemoteDNSPairIdentityFixture(t *testing.T) (*remoteDNSHTTPFixture, *dnsZoneV3TestAgent) {
	t.Helper()
	f := newRemoteDNSHTTPFixture(t)
	activatePairedPrimaryBINDForV3Test(t, f.receiver)
	agent := newDNSZoneV3TestAgent()
	agent.pairReady = true
	attachDNSZoneV3TestAgent(t, f.receiver, agent)
	oldNameservers := remoteDNSNameserversReady
	t.Cleanup(func() { remoteDNSNameserversReady = oldNameservers })
	remoteDNSNameserversReady = func(*Panel, context.Context) (bool, error) { return true, nil }
	remoteDNSReadLocalAuthority = (*Panel).remoteDNSLocalAuthority
	return f, agent
}

func TestRemoteDNSAuthorityPairIdentityUsesVerifiedDurableState(t *testing.T) {
	f, agent := newRemoteDNSPairIdentityFixture(t)
	ctx := context.Background()
	// The process address and editable settings cannot redefine the committed
	// primary/secondary relationship that the agent just proved.
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.99")
	if err := f.receiver.setSetting(ctx, settingDNSPeerIP, "192.0.2.98"); err != nil {
		t.Fatal(err)
	}
	proof, err := f.receiver.remoteDNSLocalAuthority(ctx)
	if err != nil || !proof.Ready || proof.PrimaryIP != "192.0.2.10" || proof.SecondaryIP != "192.0.2.20" ||
		proof.Engine != string(transport.DNSEngineBIND) || proof.Nameservers[0] != "ns1.celikhost.com" || proof.Nameservers[1] != "ns2.celikhost.com" {
		t.Fatalf("authority does not reflect the proven durable pair: %+v %v", proof, err)
	}
	agent.mu.Lock()
	agent.pairReady = false
	agent.mu.Unlock()
	proof, err = f.receiver.remoteDNSLocalAuthority(ctx)
	if err == nil || proof.Ready || proof.PrimaryIP != "" || proof.SecondaryIP != "" {
		t.Fatalf("stale pair retained usable identity: %+v %v", proof, err)
	}
}

func TestRemoteDNSAuthorityRejectsIdentityChangedDuringReadiness(t *testing.T) {
	f, _ := newRemoteDNSPairIdentityFixture(t)
	remoteDNSNameserversReady = func(p *Panel, ctx context.Context) (bool, error) {
		return true, p.setSetting(ctx, settingNS1, "changed.example.test")
	}
	proof, err := f.receiver.remoteDNSLocalAuthority(context.Background())
	if err == nil || proof.Ready || proof.PrimaryIP != "" || proof.SecondaryIP != "" {
		t.Fatalf("mixed identity snapshot became usable authority: %+v %v", proof, err)
	}
}

func TestRemoteDNSAuthorityRejectsRevisionChangedDuringReadiness(t *testing.T) {
	f, _ := newRemoteDNSPairIdentityFixture(t)
	remoteDNSNameserversReady = func(p *Panel, ctx context.Context) (bool, error) {
		_, err := p.db.GetDB().ExecContext(ctx, `UPDATE dns_engine_state SET revision=revision+1 WHERE singleton_id=1`)
		return true, err
	}
	proof, err := f.receiver.remoteDNSLocalAuthority(context.Background())
	if err == nil || proof.Ready || proof.PrimaryIP != "" || proof.SecondaryIP != "" {
		t.Fatalf("changed durable snapshot became usable authority: %+v %v", proof, err)
	}
}

func TestRemoteDNSPairIdentityIsOptInAndDoesNotExposeCredentials(t *testing.T) {
	f, _ := newRemoteDNSPairIdentityFixture(t)
	connection := f.connect(t)
	ctx := context.Background()
	ordinary, err := f.origin.remoteDNSConnectionReadiness(ctx, f.id)
	if err != nil || !ordinary.Ready || ordinary.PrimaryIP != "" || ordinary.SecondaryIP != "" {
		t.Fatalf("ordinary connector response changed shape: %+v %v", ordinary, err)
	}
	paired, err := f.origin.remoteDNSConnectionPairReadiness(ctx, f.id)
	if err != nil || paired.PrimaryIP != "192.0.2.10" || paired.SecondaryIP != "192.0.2.20" {
		t.Fatalf("opted-in connector lacks exact pair identity: %+v %v", paired, err)
	}
	for _, includePair := range []bool{false, true} {
		body, err := json.Marshal(remoteDNSStatusRequest{IncludePairIdentity: includePair})
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodPost, connection.Endpoint+"/api/v1/dns/remote/receiver/status", bytes.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+connection.ID+"."+connection.credential)
		w := httptest.NewRecorder()
		f.receiver.handleRemoteDNSMachine(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("status returned %d: %s", w.Code, w.Body.String())
		}
		for _, secret := range []string{connection.credential, f.code, remoteDNSHash(connection.credential)} {
			if strings.Contains(w.Body.String(), secret) {
				t.Fatal("authority response leaked authorization material")
			}
		}
		if !includePair {
			// Alpha70's transport rejects unknown fields. A new primary must
			// preserve that contract while the user updates each panel separately.
			var legacy struct {
				ClientID    string   `json:"client_id"`
				Ready       bool     `json:"ready"`
				Engine      string   `json:"engine"`
				Epoch       int64    `json:"epoch"`
				Nameservers []string `json:"nameservers"`
			}
			decoder := json.NewDecoder(w.Body)
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&legacy); err != nil || !legacy.Ready {
				t.Fatalf("legacy origin could not read new authority: %v", err)
			}
		}
	}
}

func TestRemoteDNSAuthorityLegacyProofStillValid(t *testing.T) {
	proof := remoteDNSAuthority{ClientID: strings.Repeat("a", 32), Ready: true, Engine: "bind", Epoch: 1, Nameservers: []string{"ns1.example.test", "ns2.example.test"}}
	if err := validateRemoteDNSAuthority(proof, proof.ClientID); err != nil {
		t.Fatalf("legacy authority was made to require pair identity: %v", err)
	}
}
