package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

// seedPendingDNSZoneForTest creates a zone the way a fresh install sees one:
// a domain with a full record set in the panel's own zone tables, and a sync
// row that is pending because no engine has ever existed to apply it.
// seedPendingDNSZoneForTest, taze kurulumun gördüğü biçimde bir bölge yaratır:
// panelin kendi bölge tablolarında tam kayıt kümesiyle bir alan adı ve onu
// uygulayacak bir motor hiç var olmadığı için bekleyen bir eşitleme satırı.
func seedPendingDNSZoneForTest(t *testing.T, p *Panel, zone string) {
	t.Helper()
	result, err := p.db.GetDB().Exec(
		`INSERT INTO pdns_domains (name, type) VALUES (?, 'NATIVE')`, zone,
	)
	if err != nil {
		t.Fatal(err)
	}
	domainID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range []struct{ name, kind, content string }{
		{zone, "SOA", "ns1.s7.test. hostmaster." + zone + ". 4 10800 3600 604800 3600"},
		{zone, "NS", "ns1.s7.test."},
		{zone, "A", "192.0.2.10"},
	} {
		if _, err := p.db.GetDB().Exec(`
			INSERT INTO pdns_records (
			  domain_id, name, type, content, ttl, prio, disabled
			) VALUES (?, ?, ?, ?, 300, 0, 0)`,
			domainID, record.name, record.kind, record.content,
		); err != nil {
			t.Fatal(err)
		}
	}
	// The schema triggers create and advance the sync row themselves; the
	// premise of every test here is that it is pending with nothing to apply it.
	// Şema tetikleyicileri eşitleme satırını kendileri yaratıp ilerletir;
	// buradaki her testin öncülü, onu uygulayacak bir şey olmadan bekliyor olması.
	var status string
	var desired, applied int
	if err := p.db.GetDB().QueryRow(`
		SELECT status, desired_generation, applied_generation
		FROM dns_zone_sync_state WHERE zone_name = ?`, zone,
	).Scan(&status, &desired, &applied); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || desired <= applied {
		t.Fatalf("seeded zone %s is not pending: status=%s desired=%d applied=%d",
			zone, status, desired, applied)
	}
}

// S-7 T1, register R-029. On a host that has never run a DNS engine every
// zone is pending by construction: nothing exists that could apply it. Identity
// staging refused exactly that state as "a publication is pending", and the
// first-install preview repeated the refusal as the blocker pending_zone_sync,
// so the ordinary order of operations - add a domain, then set up DNS - could
// never reach the first engine install. The only way out was deleting the
// zones.
//
// S-7 T1, defter R-029. Hiç DNS motoru çalıştırmamış bir sunucuda her bölge
// yapısı gereği bekler: onu uygulayabilecek hiçbir şey yoktur. Kimlik
// hazırlama tam da bu durumu "bir yayın bekliyor" diye reddediyordu ve ilk
// kurulum önizlemesi reddi pending_zone_sync engelleyicisiyle tekrarlıyordu;
// böylece sıradan işlem sırası - önce alan adı ekle, sonra DNS'i kur - ilk
// motor kurulumuna asla ulaşamıyordu. Tek çıkış bölgeleri silmekti.
func TestDNSSetupStagesFreshIdentityWhilePendingZonesExist(t *testing.T) {
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
	p := newDNSPanelForTest(t)
	agent := newDNSEngineTestAgent()
	attachDNSEngineTestAgent(t, p, agent)
	seedDNSSetupAuditUser(t, p)
	seedPendingDNSZoneForTest(t, p, "arch-bind.s7.test")

	recorder := httptest.NewRecorder()
	p.handleDNSSetup(recorder, dnsSetupAdminRequest(
		`{"ns1":"ns1.s7.test","ns2":"ns2.s7.test","role":"standalone","peer_ip":"","peer_ns":""}`,
	))
	if recorder.Code != http.StatusOK ||
		!strings.Contains(recorder.Body.String(), dnsSetupResultIdentityStaged) {
		t.Fatalf("fresh stage with a pending zone status=%d body=%s",
			recorder.Code, recorder.Body.String())
	}
	assertDNSSetupSettings(t, p, map[string]string{
		settingNS1: "ns1.s7.test", settingNS2: "ns2.s7.test", settingDNSRole: "standalone",
	})

	// The staged identity must lead straight into the first BIND install,
	// which publishes the pending zone itself.
	// Hazırlanan kimlik doğrudan ilk BIND kurulumuna götürmelidir; bekleyen
	// bölgeyi kurulumun kendisi yayımlar.
	preview, previewRecorder := requestDNSEnginePreview(
		t, p, transport.DNSEngineBIND, nil, 1,
	)
	if previewRecorder.Code != http.StatusOK || preview.Action != "install" ||
		len(preview.Blockers) != 0 || preview.Topology != transport.DNSTopologyStandalone ||
		preview.PendingZoneCount != 1 {
		t.Fatalf("first install preview after staging=%+v status=%d body=%s",
			preview, previewRecorder.Code, previewRecorder.Body.String())
	}
	agent.mu.Lock()
	switchCalls := agent.switchCalls
	agent.mu.Unlock()
	if switchCalls != 0 {
		t.Fatalf("staging and preview mutated the host: switch=%d", switchCalls)
	}
}

// The relaxation is exactly "pending because nothing could apply it". A zone
// with a live publication lease is in flight, and staging must still refuse
// to change the identity underneath it.
// Gevşetme tam olarak "uygulayacak bir şey olmadığı için bekliyor"dur. Canlı
// yayın kirası taşıyan bir bölge uçuştadır ve hazırlama, kimliği onun altında
// değiştirmeyi yine reddetmelidir.
func TestDNSSetupFreshIdentityStillRefusesAnInFlightPublication(t *testing.T) {
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
	p := newDNSPanelForTest(t)
	agent := newDNSEngineTestAgent()
	attachDNSEngineTestAgent(t, p, agent)
	seedDNSSetupAuditUser(t, p)
	seedPendingDNSZoneForTest(t, p, "inflight.s7.test")
	if _, err := p.db.GetDB().Exec(`
		UPDATE dns_zone_sync_state
		SET lease_request_id = '11111111111111111111111111111111',
		    lease_owner_id = '22222222222222222222222222222222',
		    lease_generation = 4, lease_action = 'sync', lease_zone_type = 'NATIVE',
		    lease_qualifier = 'dns-zone-sync/v1:sha256:' || lower(hex(zeroblob(32))),
		    lease_expires_at = datetime('now', '+20 seconds')
		WHERE zone_name = 'inflight.s7.test'
	`); err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	p.handleDNSSetup(recorder, dnsSetupAdminRequest(
		`{"ns1":"ns1.s7.test","ns2":"ns2.s7.test","role":"standalone","peer_ip":"","peer_ns":""}`,
	))
	if recorder.Code != http.StatusConflict ||
		!strings.Contains(recorder.Body.String(), errCodeDNSEngineWorkflowRequired) {
		t.Fatalf("in-flight publication must refuse staging: status=%d body=%s",
			recorder.Code, recorder.Body.String())
	}
	assertDNSSetupSettings(t, p, map[string]string{
		settingNS1: "", settingNS2: "", settingDNSRole: "",
	})
}

// A host that already runs a legacy PowerDNS is not fresh: its pending zones
// may be mid-publication, and the stricter gate still applies there.
// Zaten eski bir PowerDNS çalıştıran sunucu taze değildir: bekleyen bölgeleri
// yayının ortasında olabilir ve daha sıkı kapı orada hâlâ geçerlidir.
func TestDNSSetupAdoptionIdentityStillRefusesPendingZones(t *testing.T) {
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
	p := newDNSPanelForTest(t)
	agent := newDNSEngineTestAgent()
	pdns := agent.runtimes[transport.DNSEnginePowerDNS]
	pdns.Installed, pdns.Running, pdns.Managed = true, true, true
	agent.runtimes[transport.DNSEnginePowerDNS] = pdns
	attachDNSEngineTestAgent(t, p, agent)
	seedDNSSetupAuditUser(t, p)
	seedPendingDNSZoneForTest(t, p, "legacy.s7.test")

	recorder := httptest.NewRecorder()
	p.handleDNSSetup(recorder, dnsSetupAdminRequest(
		`{"ns1":"ns1.s7.test","ns2":"ns2.s7.test","role":"standalone","peer_ip":"","peer_ns":""}`,
	))
	if recorder.Code != http.StatusConflict ||
		!strings.Contains(recorder.Body.String(), errCodeDNSEngineWorkflowRequired) {
		t.Fatalf("adoption staging with a pending zone must still refuse: status=%d body=%s",
			recorder.Code, recorder.Body.String())
	}
}

// With a source engine active, pending zones still block a switch: pending
// there means the source has not caught up, and the switch must not copy an
// unsettled zone set.
// Kaynak motor etkinken bekleyen bölgeler geçişi yine engeller: orada bekleme
// kaynağın yetişmediği anlamına gelir ve geçiş oturmamış bir bölge kümesini
// kopyalamamalıdır.
func TestDNSEnginePreviewStillBlocksPendingZonesWhenASourceIsActive(t *testing.T) {
	snapshot := dnsEngineSnapshot{
		Revision: 3, EngineEpoch: 1,
		ActiveEngine:     enginePointer(transport.DNSEnginePowerDNS),
		State:            dnsEngineStateReady,
		Topology:         transport.DNSTopologyStandalone,
		ZoneCount:        2,
		PendingZoneCount: 1,
	}
	blockers := dnsEnginePreviewBlockers(
		snapshot, transport.DNSEngineBIND, transport.DNSEnginePowerDNS, 3,
	)
	found := false
	for _, blocker := range blockers {
		if blocker.Code == "pending_zone_sync" {
			found = true
		}
	}
	if !found {
		t.Fatalf("a switch away from an active source must still block on pending zones: %+v", blockers)
	}

	fresh := snapshot
	fresh.ActiveEngine, fresh.EngineEpoch, fresh.State = nil, 0, dnsEngineStateUnconfigured
	for _, blocker := range dnsEnginePreviewBlockers(fresh, transport.DNSEngineBIND, "", 3) {
		if blocker.Code == "pending_zone_sync" {
			t.Fatalf("a source-less first install must not block on its own pending zones: %+v", blocker)
		}
	}
}
