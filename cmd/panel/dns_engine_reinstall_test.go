package main

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

// loseDNSEngineRuntimeForTest is what a restored control plane looks like from
// the panel's side: the ledger still names the engine, the machine has no copy
// of it.
func loseDNSEngineRuntimeForTest(
	agent *dnsEngineTestAgent,
	engine transport.DNSEngine,
) {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	runtime := agent.runtimes[engine]
	runtime.Installed, runtime.Running, runtime.Managed = false, false, false
	runtime.PairReady = false
	agent.runtimes[engine] = runtime
}

// installBINDForReinstallTest brings a panel to the exact state host B was
// restored into: BIND active at epoch 1 with one applied zone, and then no BIND
// on the machine at all.
func installBINDForReinstallTest(
	t *testing.T,
	panel *Panel,
	agent *dnsEngineTestAgent,
) int64 {
	t.Helper()
	preview, recorder := requestDNSEnginePreview(
		t, panel, transport.DNSEngineBIND, nil, 0,
	)
	if recorder.Code != http.StatusOK || len(preview.Blockers) != 0 {
		t.Fatalf("install preview status=%d body=%s",
			recorder.Code, recorder.Body.String())
	}
	commit := commitDNSEngineSwitch(
		t, panel, strings.Repeat("1", 32), transport.DNSEngineBIND,
		nil, 0, preview.PreviewToken, false,
	)
	if commit.Code != http.StatusOK {
		t.Fatalf("install commit status=%d body=%s",
			commit.Code, commit.Body.String())
	}
	state, err := readDNSEngineDBState(context.Background(), panel.db.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	if state.ActiveEngine != transport.DNSEngineBIND || state.EngineEpoch != 1 {
		t.Fatalf("install left state=%+v", state)
	}
	return state.Revision
}

// The finding this test exists for: on a host whose ledger says BIND owns it
// and whose disk has no BIND, the only offer the engine screen could make was
// an install, and that install refused itself with target_already_active and
// source_degraded. Both statements were true and neither led anywhere.
//
// Bu testin var olma sebebi olan bulgu: defteri BIND'in sahibi olduğunu söyleyen
// ve diskinde BIND bulunmayan bir sunucuda motor ekranının yapabildiği tek
// öneri bir kurulumdu; o kurulum da kendini target_already_active ve
// source_degraded ile reddediyordu. İki ifade de doğruydu ve hiçbiri bir yere
// çıkmıyordu.
func TestDNSEngineReinstallsTheActiveEngineWhoseRuntimeIsGone(t *testing.T) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "standalone")
	seedPendingDNSZoneForTest(t, panel, "drill.example")
	agent := newDNSEngineTestAgent()
	attachDNSEngineTestAgent(t, panel, agent)
	revision := installBINDForReinstallTest(t, panel, agent)

	loseDNSEngineRuntimeForTest(agent, transport.DNSEngineBIND)
	snapshot, err := panel.dnsEngineSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ActiveEngine == nil ||
		*snapshot.ActiveEngine != transport.DNSEngineBIND ||
		snapshot.EngineEpoch != 1 || snapshot.State != dnsEngineStateDegraded {
		t.Fatalf("restored-host snapshot=%+v", snapshot)
	}

	preview, recorder := requestDNSEnginePreview(
		t, panel, transport.DNSEngineBIND, transport.DNSEngineBIND, revision,
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("reinstall preview status=%d body=%s",
			recorder.Code, recorder.Body.String())
	}
	if preview.Action != dnsEngineActionReinstall ||
		len(preview.Blockers) != 0 ||
		!validServiceOperationID(preview.PreviewToken) ||
		preview.RequiresDowntimeAcknowledgement {
		t.Fatalf("reinstall preview=%+v body=%s",
			preview, recorder.Body.String())
	}
	for _, unwanted := range []string{
		"stop_source", "brief_dns_interruption", "keep_source_standby",
	} {
		for _, impact := range preview.Impacts {
			if impact == unwanted {
				t.Fatalf("reinstall promises %q: %+v", unwanted, preview.Impacts)
			}
		}
	}

	commit := commitDNSEngineSwitch(
		t, panel, strings.Repeat("2", 32), transport.DNSEngineBIND,
		transport.DNSEngineBIND, revision, preview.PreviewToken, false,
	)
	if commit.Code != http.StatusOK {
		t.Fatalf("reinstall commit status=%d body=%s",
			commit.Code, commit.Body.String())
	}

	agent.mu.Lock()
	requests := append(
		[]transport.SwitchDNSEngineV1Request(nil), agent.switchRequests...,
	)
	agent.mu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("agent saw %d switch requests, want the install and the reinstall", len(requests))
	}
	last := requests[1]
	if last.Mode != transport.DNSEngineSwitchModeReinstall ||
		last.SourceEngine != transport.DNSEngineBIND ||
		last.TargetEngine != transport.DNSEngineBIND ||
		last.SourceEpoch != 1 || last.TargetEpoch != 1 ||
		last.Topology != transport.DNSTopologyStandalone ||
		len(last.Zones) != 1 {
		t.Fatalf("reinstall manifest=%+v", last)
	}

	state, err := readDNSEngineDBState(context.Background(), panel.db.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	// The epoch belongs to the tenure, and the tenure never ended. Advancing it
	// would tell every zone receipt on the host that it now serves a period
	// that never started.
	if state.ActiveEngine != transport.DNSEngineBIND ||
		state.EngineEpoch != 1 || state.Revision != revision ||
		state.CurrentSwitchID != "" {
		t.Fatalf("reinstall changed durable authority: %+v", state)
	}
	var snapshots int
	if err := panel.db.GetDB().QueryRow(
		`SELECT count(*) FROM dns_engine_switch_snapshots`,
	).Scan(&snapshots); err != nil {
		t.Fatal(err)
	}
	if snapshots != 1 {
		t.Fatalf("reinstall wrote %d switch snapshots, want only the install's", snapshots)
	}
	var engine string
	var epoch int64
	var status string
	if err := panel.db.GetDB().QueryRow(`
		SELECT applications.engine, applications.engine_epoch, state.status
		FROM dns_zone_engine_applications AS applications
		JOIN dns_zone_sync_state AS state
		  ON state.zone_name = applications.zone_name
		WHERE applications.zone_name = 'drill.example'`,
	).Scan(&engine, &epoch, &status); err != nil {
		t.Fatal(err)
	}
	if engine != string(transport.DNSEngineBIND) || epoch != 1 ||
		status != "applied" {
		t.Fatalf("reinstalled zone engine=%s epoch=%d status=%s",
			engine, epoch, status)
	}
}

// The reinstall must never become a way to re-run an engine that is serving
// fine. With the runtime present, asking for the active engine is still the
// switch-to-yourself the old blocker was written for.
//
// Yeniden kurulum, gayet iyi hizmet veren bir motoru yeniden çalıştırmanın yolu
// hâline asla gelmemelidir. Çalışma zamanı mevcutken etkin motoru istemek,
// eski engelleyicinin yazıldığı kendine-geçiş olmayı sürdürür.
func TestDNSEngineReinstallIsNotOfferedWhileTheRuntimeIsPresent(t *testing.T) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "standalone")
	agent := newDNSEngineTestAgent()
	attachDNSEngineTestAgent(t, panel, agent)
	revision := installBINDForReinstallTest(t, panel, agent)

	preview, recorder := requestDNSEnginePreview(
		t, panel, transport.DNSEngineBIND, transport.DNSEngineBIND, revision,
	)
	if recorder.Code != http.StatusOK ||
		preview.Action == dnsEngineActionReinstall ||
		!hasDNSEngineBlocker(preview, "target_already_active") ||
		preview.PreviewToken != "" {
		t.Fatalf("healthy active engine preview=%+v body=%s",
			preview, recorder.Body.String())
	}
	agent.mu.Lock()
	calls := agent.switchCalls
	agent.mu.Unlock()
	if calls != 1 {
		t.Fatalf("healthy active engine reached the agent %d times", calls)
	}
}

// A reinstall that fails after apt has run leaves the packages behind, and on a
// host whose active engine is BIND the readiness probe reports that leftover as
// installed and unmanaged — there is no matching generation tree yet, so
// nothing proves it managed. If that shape were excluded, the retry would be
// offered exactly once and never again, which is how a failed first install
// used to strand a host (register R-028/R-029). It is the same repair either
// way, and the agent's own ownership proof still gates it.
//
// apt çalıştıktan sonra düşen bir yeniden kurulum paketleri geride bırakır ve
// etkin motoru BIND olan bir sunucuda hazırlık yoklaması bu kalıntıyı kurulu ve
// yönetilmeyen olarak bildirir — henüz eşleşen bir nesil ağacı yoktur,
// dolayısıyla onu yönetilen kılan hiçbir şey yoktur. Bu biçim dışlansaydı,
// yeniden deneme tam bir kez sunulur ve bir daha hiç sunulmazdı; başarısız bir
// ilk kurulumun sunucuyu mahsur bıraktığı biçim buydu (defter R-028/R-029).
// Onarım her iki durumda da aynıdır ve agent'ın kendi sahiplik kanıtı hâlâ
// kapıda durur.
func TestDNSEngineReinstallRetriesAfterItsOwnPackagesLanded(t *testing.T) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "standalone")
	agent := newDNSEngineTestAgent()
	attachDNSEngineTestAgent(t, panel, agent)
	revision := installBINDForReinstallTest(t, panel, agent)

	agent.mu.Lock()
	runtime := agent.runtimes[transport.DNSEngineBIND]
	runtime.Running, runtime.Managed = false, false
	agent.runtimes[transport.DNSEngineBIND] = runtime
	agent.mu.Unlock()

	preview, recorder := requestDNSEnginePreview(
		t, panel, transport.DNSEngineBIND, transport.DNSEngineBIND, revision,
	)
	if recorder.Code != http.StatusOK ||
		preview.Action != dnsEngineActionReinstall ||
		len(preview.Blockers) != 0 {
		t.Fatalf("failed-attempt retry preview=%+v body=%s",
			preview, recorder.Body.String())
	}
	var current sql.NullString
	if err := panel.db.GetDB().QueryRow(
		`SELECT current_switch_id FROM dns_engine_state WHERE singleton_id = 1`,
	).Scan(&current); err != nil {
		t.Fatal(err)
	}
	if current.Valid {
		t.Fatalf("a preview attached a switch: %v", current.String)
	}
}
