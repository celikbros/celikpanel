package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

// The secondary is installed through the production panel's reviewed DNS
// operation with a fixture agent. Hosting publication still traverses the real
// remote machine authentication and SQL handlers. Actual zone transfer belongs
// to the disposable two-host acceptance fixture, not this test seam.
func secondaryHostingConsumerFixture(t *testing.T, engine transport.DNSEngine) (*remoteConsumerIntegrationFixture, func()) {
	t.Helper()
	f := newRemoteConsumerIntegrationFixture(t)
	engineAgent := newDNSEngineTestAgent()
	attachDNSEngineTestAgent(t, f.origin, engineAgent)
	draft := setupDNSTestDraft()
	draft.Purpose, draft.DNSEngine, draft.DNSRole = "dns", string(engine), transport.DNSPairRoleSecondary
	draft.NS1, draft.NS2 = "ns1.authority.test", "ns2.authority.test"
	draft.LocalIP, draft.PeerIP, draft.PeerNS = "192.0.2.45", "192.0.2.10", draft.NS1
	ctx := context.Background()
	if err := f.origin.startServerSetupDNS(ctx, draft, strings.Repeat("d", 32), serviceOperationActor{UserID: 1}); err != nil {
		t.Fatal(err)
	}
	if err := f.origin.saveSetupRemoteDNSConnection(ctx, f.connectionID); err != nil {
		t.Fatal(err)
	}
	before, err := readDNSEngineDBState(ctx, f.origin.db.GetDB())
	if err != nil || before.ActiveEngine != engine || before.PairRole != transport.DNSPairRoleSecondary || before.PeerIP != draft.PeerIP {
		t.Fatalf("secondary setup state=%+v err=%v", before, err)
	}
	attachRemoteConsumerAgent(t, f.origin)
	assertPreserved := func() {
		t.Helper()
		after, err := readDNSEngineDBState(ctx, f.origin.db.GetDB())
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatalf("hosting changed secondary engine identity: before=%+v after=%+v err=%v", before, after, err)
		}
		var localZones, localWrites int
		if err := f.origin.db.GetDB().QueryRow(`SELECT COUNT(*) FROM pdns_domains`).Scan(&localZones); err != nil || localZones != 0 {
			t.Fatalf("hosting claimed a local authoritative zone: count=%d err=%v", localZones, err)
		}
		if err := f.origin.db.GetDB().QueryRow(`SELECT COUNT(*) FROM dns_zone_sync_state`).Scan(&localWrites); err != nil || localWrites != 0 {
			t.Fatalf("hosting acquired local zone publication state: count=%d err=%v", localWrites, err)
		}
		engineAgent.mu.Lock()
		calls := engineAgent.switchCalls
		engineAgent.mu.Unlock()
		if calls != 1 {
			t.Fatalf("hosting changed installed secondary engine: switch calls=%d", calls)
		}
		mode, err := f.origin.setupDNSManagementMode(ctx)
		if err != nil || mode != setupDNSModeExisting {
			t.Fatalf("hosting DNS default changed: mode=%q err=%v", mode, err)
		}
	}
	assertPreserved()
	return f, assertPreserved
}

func TestRemoteDNSSecondaryHostingPublishesWebAndMailWithoutLocalZoneWrites(t *testing.T) {
	for _, engine := range []transport.DNSEngine{transport.DNSEngineBIND, transport.DNSEnginePowerDNS} {
		t.Run(string(engine), func(t *testing.T) {
			f, assertPreserved := secondaryHostingConsumerFixture(t, engine)
			const domain = "hosting.secondary.test"
			if w := f.create(t, domain, "static"); w.Code != http.StatusOK {
				t.Fatalf("secondary hosting create: %d %s", w.Code, w.Body.String())
			}
			if w := remoteConsumerDNSRequest(t, f.origin, domain, http.MethodPost, "records", `{"name":"@","type":"TXT","content":"verification-token=retain","ttl":300}`); w.Code != http.StatusOK {
				t.Fatalf("secondary hosting record: %d %s", w.Code, w.Body.String())
			}
			r := serviceOperationAdminRequest(t, http.MethodPost, "/api/v1/domains/"+domain+"/mail-auth/apply", `{"record":"spf"}`, 1)
			w := httptest.NewRecorder()
			f.origin.handleMailAuthApply(w, r, domain)
			if w.Code != http.StatusOK {
				t.Fatalf("secondary mail records: %d %s", w.Code, w.Body.String())
			}
			var address, mailAddress, mx, token, spf int
			for _, check := range []struct {
				query string
				args  []any
				count *int
			}{
				{`SELECT COUNT(*) FROM pdns_records WHERE name=? AND type='A' AND content='192.0.2.45'`, []any{domain}, &address},
				{`SELECT COUNT(*) FROM pdns_records WHERE name=? AND type='A' AND content='192.0.2.45'`, []any{"mail." + domain}, &mailAddress},
				{`SELECT COUNT(*) FROM pdns_records WHERE name=? AND type='MX' AND content=?`, []any{domain, "mail." + domain}, &mx},
				{`SELECT COUNT(*) FROM pdns_records WHERE name=? AND type='TXT' AND content IN (?,?)`, []any{domain, "verification-token=retain", `"verification-token=retain"`}, &token},
				{`SELECT COUNT(*) FROM pdns_records WHERE name=? AND type='TXT' AND content IN (?,?)`, []any{domain, spfRecommended(), `"` + spfRecommended() + `"`}, &spf},
			} {
				if err := f.receiver.db.GetDB().QueryRow(check.query, check.args...).Scan(check.count); err != nil || *check.count != 1 {
					t.Fatalf("primary did not receive exact secondary-hosted records: query=%s count=%d err=%v", check.query, *check.count, err)
				}
			}
			var owner, connection string
			if err := f.origin.db.GetDB().QueryRow(`SELECT dns_management,dns_remote_connection_id FROM domains WHERE name=?`, domain).Scan(&owner, &connection); err != nil || owner != setupDNSModeExisting || connection != f.connectionID {
				t.Fatalf("hosting ownership=%s/%s err=%v", owner, connection, err)
			}
			assertPreserved()
			if err := f.origin.removeDomainDNSForDeletion(context.Background(), domain, ""); err != nil {
				t.Fatal(err)
			}
			var remoteZones int
			if err := f.receiver.db.GetDB().QueryRow(`SELECT COUNT(*) FROM pdns_domains WHERE name=?`, domain).Scan(&remoteZones); err != nil || remoteZones != 0 {
				t.Fatalf("primary retained deleted zone: count=%d err=%v", remoteZones, err)
			}
			assertPreserved()
		})
	}
}

func TestRemoteDNSSecondaryHostingLostReplyReconcilesExactGeneration(t *testing.T) {
	for _, engine := range []transport.DNSEngine{transport.DNSEngineBIND, transport.DNSEnginePowerDNS} {
		t.Run(string(engine), func(t *testing.T) {
			f, assertPreserved := secondaryHostingConsumerFixture(t, engine)
			const domain = "lost.secondary.test"
			if w := f.create(t, domain, "static"); w.Code != http.StatusOK {
				t.Fatal(w.Body.String())
			}
			f.losePublishResponse = true
			w := remoteConsumerDNSRequest(t, f.origin, domain, http.MethodPost, "records", `{"name":"@","type":"TXT","content":"exact-generation","ttl":300}`)
			if w.Code == http.StatusOK {
				t.Fatal("lost response was incorrectly reported as published")
			}
			var generation, applied int64
			if err := f.origin.db.GetDB().QueryRow(`SELECT generation,applied_generation FROM remote_dns_zones`).Scan(&generation, &applied); err != nil || generation == applied {
				t.Fatalf("lost response discarded pending generation: %d/%d err=%v", generation, applied, err)
			}
			publications := f.published
			if err := f.origin.syncRemoteDomainDNS(context.Background(), domain, false); err != nil {
				t.Fatal(err)
			}
			if f.published != publications {
				t.Fatal("lost response retry published another DNS generation")
			}
			if err := f.origin.db.GetDB().QueryRow(`SELECT applied_generation FROM remote_dns_zones`).Scan(&applied); err != nil || applied != generation {
				t.Fatalf("exact receipt was not reconciled: %d/%d err=%v", applied, generation, err)
			}
			assertPreserved()
		})
	}
}

func TestRemoteDNSSecondaryHostingRevocationPreservesPublishedZones(t *testing.T) {
	for _, engine := range []transport.DNSEngine{transport.DNSEngineBIND, transport.DNSEnginePowerDNS} {
		t.Run(string(engine), func(t *testing.T) {
			f, assertPreserved := secondaryHostingConsumerFixture(t, engine)
			const domain = "revoked.secondary.test"
			if w := f.create(t, domain, "static"); w.Code != http.StatusOK {
				t.Fatal(w.Body.String())
			}
			publications := f.published
			w := httptest.NewRecorder()
			f.receiver.handleRemoteDNSAdmin(w, remoteDNSAdminRequest(t, http.MethodDelete, "/api/v1/dns/remote/clients?id="+f.connectionID))
			if w.Code != http.StatusOK {
				t.Fatalf("receiver revocation: %d %s", w.Code, w.Body.String())
			}
			if w := f.create(t, "new.secondary.test", "static"); w.Code != http.StatusConflict {
				t.Fatalf("revoked client created host: %d %s", w.Code, w.Body.String())
			}
			if w := remoteConsumerDNSRequest(t, f.origin, domain, http.MethodPost, "records", `{"name":"@","type":"TXT","content":"must-not-publish","ttl":300}`); w.Code == http.StatusOK {
				t.Fatal("revoked client changed published DNS")
			}
			var remoteZones, forbiddenRecords, hosted int
			if err := f.receiver.db.GetDB().QueryRow(`SELECT COUNT(*) FROM pdns_domains WHERE name=?`, domain).Scan(&remoteZones); err != nil || remoteZones != 1 {
				t.Fatalf("revocation deleted existing authority: count=%d err=%v", remoteZones, err)
			}
			if err := f.receiver.db.GetDB().QueryRow(`SELECT COUNT(*) FROM pdns_records WHERE content='must-not-publish'`).Scan(&forbiddenRecords); err != nil || forbiddenRecords != 0 || f.published != publications {
				t.Fatalf("revocation allowed publication: count=%d publications=%d/%d err=%v", forbiddenRecords, f.published, publications, err)
			}
			if err := f.origin.db.GetDB().QueryRow(`SELECT COUNT(*) FROM domains`).Scan(&hosted); err != nil || hosted != 1 {
				t.Fatalf("revocation changed existing hosting: count=%d err=%v", hosted, err)
			}
			assertPreserved()
		})
	}
}
