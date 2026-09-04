package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

// stoppedUnmanagedBINDRuntimes is the R-038 host: apt put bind9 on disk, the
// unit is down, nothing was ever configured and the panel owns none of it.
//
// stoppedUnmanagedBINDRuntimes, R-038 sunucusudur: apt bind9'u diske koymuş,
// birim kapalı, hiçbir şey yapılandırılmamış ve panel hiçbirinin sahibi değil.
func stoppedUnmanagedBINDRuntimes() map[transport.DNSEngine]transport.DNSBackendRuntimeState {
	return map[transport.DNSEngine]transport.DNSBackendRuntimeState{
		transport.DNSEnginePowerDNS: {
			Engine: transport.DNSEnginePowerDNS, Unit: "pdns.service",
		},
		transport.DNSEngineBIND: {
			Engine: transport.DNSEngineBIND, Unit: "named.service",
			Installed: true,
		},
	}
}

func stagedStandaloneSnapshot(
	runtimes map[transport.DNSEngine]transport.DNSBackendRuntimeState,
	state string,
) dnsEngineSnapshot {
	return dnsEngineSnapshot{
		Revision: 1, EngineEpoch: 0, State: state,
		Topology: transport.DNSTopologyStandalone,
		runtime:  runtimes,
	}
}

func TestDNSEngineTakeoverIsOfferedOnlyForTheStoppedUnmanagedShape(t *testing.T) {
	pdns := transport.DNSEnginePowerDNS
	runningBIND := stoppedUnmanagedBINDRuntimes()
	running := runningBIND[transport.DNSEngineBIND]
	running.Running = true
	runningBIND[transport.DNSEngineBIND] = running

	managedStandby := stoppedUnmanagedBINDRuntimes()
	managed := managedStandby[transport.DNSEngineBIND]
	managed.Managed = true
	managedStandby[transport.DNSEngineBIND] = managed

	unmanagedPDNS := stoppedUnmanagedBINDRuntimes()
	unmanagedPDNS[transport.DNSEnginePowerDNS] = transport.DNSBackendRuntimeState{
		Engine: transport.DNSEnginePowerDNS, Unit: "pdns.service",
		Installed: true,
	}

	tests := []struct {
		name       string
		snapshot   dnsEngineSnapshot
		target     transport.DNSEngine
		wantAction string
	}{
		{
			name:       `exact stopped unmanaged BIND`,
			snapshot:   stagedStandaloneSnapshot(stoppedUnmanagedBINDRuntimes(), dnsEngineStateUnconfigured),
			target:     transport.DNSEngineBIND,
			wantAction: dnsEngineActionAdoptUnmanaged,
		},
		{
			name: `unmanaged BIND is serving`,
			snapshot: stagedStandaloneSnapshot(
				runningBIND, dnsEngineStateUnmanaged,
			),
			target: transport.DNSEngineBIND,
			// R-039: the running half is the same action and the same
			// acknowledgement; the agent adopts it in place.
			wantAction: dnsEngineActionAdoptUnmanaged,
		},
		{
			// A running engine CelikPanel owns is never a takeover. There is
			// nothing foreign to take over and no consent to collect.
			name: `panel-managed BIND is serving`,
			snapshot: func() dnsEngineSnapshot {
				runtimes := stoppedUnmanagedBINDRuntimes()
				bind := runtimes[transport.DNSEngineBIND]
				bind.Running, bind.Managed = true, true
				runtimes[transport.DNSEngineBIND] = bind
				return stagedStandaloneSnapshot(
					runtimes, dnsEngineStateUnmanaged,
				)
			}(),
			target: transport.DNSEngineBIND, wantAction: `switch`,
		},
		{
			// Another engine is answering, so this is a switch with a live
			// source, not a takeover of the target.
			name: `another engine is serving`,
			snapshot: func() dnsEngineSnapshot {
				runtimes := stoppedUnmanagedBINDRuntimes()
				runtimes[transport.DNSEnginePowerDNS] = transport.DNSBackendRuntimeState{
					Engine: transport.DNSEnginePowerDNS, Unit: "pdns.service",
					Installed: true, Running: true,
				}
				return stagedStandaloneSnapshot(
					runtimes, dnsEngineStateUnmanaged,
				)
			}(),
			target: transport.DNSEngineBIND, wantAction: `switch`,
		},
		{
			name: `panel-managed standby is an install retry, never a takeover`,
			snapshot: stagedStandaloneSnapshot(
				managedStandby, dnsEngineStateUnconfigured,
			),
			target: transport.DNSEngineBIND, wantAction: `install`,
		},
		{
			name: `stopped unmanaged PowerDNS is out of scope`,
			snapshot: stagedStandaloneSnapshot(
				unmanagedPDNS, dnsEngineStateUnconfigured,
			),
			target: transport.DNSEnginePowerDNS, wantAction: `switch`,
		},
		{
			name: `durable authority already exists`,
			snapshot: func() dnsEngineSnapshot {
				snapshot := stagedStandaloneSnapshot(
					stoppedUnmanagedBINDRuntimes(), dnsEngineStateReady,
				)
				snapshot.ActiveEngine = &pdns
				snapshot.EngineEpoch = 1
				return snapshot
			}(),
			target: transport.DNSEngineBIND, wantAction: `switch`,
		},
		{
			name: `readiness could not be proven`,
			snapshot: func() dnsEngineSnapshot {
				snapshot := stagedStandaloneSnapshot(
					stoppedUnmanagedBINDRuntimes(), dnsEngineStateUnconfigured,
				)
				snapshot.runtimeErr = errDNSIdentityStagingConflict
				return snapshot
			}(),
			target: transport.DNSEngineBIND, wantAction: `switch`,
		},
		{
			name: `port 53 is taken by something else`,
			snapshot: func() dnsEngineSnapshot {
				snapshot := stagedStandaloneSnapshot(
					stoppedUnmanagedBINDRuntimes(), dnsEngineStateUnconfigured,
				)
				snapshot.port53Conflict = true
				return snapshot
			}(),
			target: transport.DNSEngineBIND, wantAction: `switch`,
		},
		{
			name: `DNS identity was never staged`,
			snapshot: func() dnsEngineSnapshot {
				snapshot := stagedStandaloneSnapshot(
					stoppedUnmanagedBINDRuntimes(), dnsEngineStateUnconfigured,
				)
				snapshot.Revision = 0
				snapshot.Topology = dnsEngineStateUnconfigured
				return snapshot
			}(),
			target: transport.DNSEngineBIND, wantAction: `switch`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := dnsEngineAction(test.snapshot, test.target)
			if got != test.wantAction {
				t.Fatalf(`action=%s want=%s`, got, test.wantAction)
			}
			blockers := dnsEnginePreviewBlockers(
				test.snapshot, test.target, "", test.snapshot.Revision,
			)
			preview := dnsEngineSwitchPreview{Blockers: blockers}
			takeover := got == dnsEngineActionAdoptUnmanaged
			if takeover && len(blockers) != 0 {
				t.Fatalf(`takeover blockers=%+v, want none`, blockers)
			}
			// The out-of-scope half must stay refused, by name.
			if !takeover && test.snapshot.runtimeErr == nil &&
				test.snapshot.runtime[test.target].Installed &&
				!test.snapshot.runtime[test.target].Managed &&
				!hasDNSEngineBlocker(preview, "unmanaged_dns_detected") {
				t.Fatalf(`unmanaged shape lost its blocker: %+v`, blockers)
			}
		})
	}
}

func requireDNSEngineImpacts(
	t *testing.T,
	impacts []string,
	want, unwanted []string,
) {
	t.Helper()
	present := make(map[string]struct{}, len(impacts))
	for _, impact := range impacts {
		present[impact] = struct{}{}
	}
	for _, code := range want {
		if _, found := present[code]; !found {
			t.Errorf(`impacts=%v missing %s`, impacts, code)
		}
	}
	for _, code := range unwanted {
		if _, found := present[code]; found {
			t.Errorf(`impacts=%v must not promise %s`, impacts, code)
		}
	}
}

// Both halves keep the foreign zones, for the same reason: CelikPanel's BIND
// generation adds an options block and an include and deletes no zone
// declaration. The stopped half starts the server rather than reloading it,
// and that is the only difference in what it costs.
//
// İki yarı da yabancı bölgeleri korur, aynı nedenle: CelikPanel'in BIND nesli
// bir seçenek bloğu ile bir include ekler, hiçbir bölge bildirimini silmez.
// Durmuş yarı sunucuyu yeniden yüklemek yerine başlatır; bedel farkı bundan
// ibarettir.
func TestDNSEngineTakeoverImpactsNameTheReplacementAndWhatSurvives(t *testing.T) {
	requireDNSEngineImpacts(t,
		dnsEngineImpacts(dnsEngineActionAdoptUnmanaged, false, false),
		[]string{
			"replace_foreign_config", "validate_target", "publish_zones",
			"start_target", "keep_unknown_zones",
		},
		[]string{
			"install_target", "stop_source", "brief_dns_interruption",
			"reload_target", "drop_unknown_zones",
		},
	)
}

// The running half must not borrow the stopped half's costs. It starts
// nothing - the server is already up and is reloaded in place - and, like the
// stopped half, it drops nothing (register R-039).
//
// Çalışan yarı, durmuş yarının bedellerini ödünç almamalıdır. Hiçbir şey
// başlatmaz - sunucu zaten ayakta ve yerinde yeniden yükleniyor - ve hiçbir
// şeyi düşürmez, çünkü CelikPanel'in BIND nesli bir include ve bir seçenek
// bloğu ekler, hiçbir bölge bildirimini silmez (defter R-039).
func TestDNSEngineRunningTakeoverImpactsReloadAndKeepTheForeignZones(t *testing.T) {
	requireDNSEngineImpacts(t,
		dnsEngineImpacts(dnsEngineActionAdoptUnmanaged, false, true),
		[]string{
			"replace_foreign_config", "validate_target", "publish_zones",
			"reload_target", "keep_unknown_zones",
		},
		[]string{
			"install_target", "stop_source", "brief_dns_interruption",
			"start_target", "drop_unknown_zones",
		},
	)
}

func commitDNSEngineTakeover(
	t *testing.T,
	panel *Panel,
	requestID string,
	revision int64,
	token string,
	adoptionAcknowledged bool,
) *httptest.ResponseRecorder {
	t.Helper()
	if _, err := panel.db.GetDB().Exec(`
		INSERT OR IGNORE INTO users (
		  id, username, password_hash, email, role
		) VALUES (1, 'dns-engine-admin', 'hash',
		          'dns-engine-admin@example.test', 'admin')
	`); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{
		"request_id":            requestID,
		"target_engine":         transport.DNSEngineBIND,
		"expected_source":       nil,
		"expected_revision":     revision,
		"preview_token":         token,
		"downtime_acknowledged": false,
		"adoption_acknowledged": adoptionAcknowledged,
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/dns/engine/switch",
		strings.NewReader(string(body)),
	)
	request.RemoteAddr = "198.51.100.44:54321"
	request.Header.Set("User-Agent", "dns-engine-test-client")
	request = request.WithContext(context.WithValue(
		request.Context(), callerKey, &Caller{ID: 1, Role: roleAdmin},
	))
	panel.handleDNSEngineSwitch(recorder, request)
	return recorder
}

func newStoppedUnmanagedBINDPanel(t *testing.T) (*Panel, *dnsEngineTestAgent) {
	t.Helper()
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "standalone")
	agent := newDNSEngineTestAgent()
	agent.runtimes = stoppedUnmanagedBINDRuntimes()
	attachDNSEngineTestAgent(t, panel, agent)
	return panel, agent
}

func TestDNSEngineTakeoverRefusesWithoutItsOwnAcknowledgement(t *testing.T) {
	panel, _ := newStoppedUnmanagedBINDPanel(t)
	preview, recorder := requestDNSEnginePreview(
		t, panel, transport.DNSEngineBIND, nil, 0,
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf(`preview status=%d body=%s`, recorder.Code, recorder.Body.String())
	}
	if preview.Action != dnsEngineActionAdoptUnmanaged ||
		len(preview.Blockers) != 0 || preview.PreviewToken == "" {
		t.Fatalf(`preview=%+v`, preview)
	}
	if !preview.RequiresAdoptionAcknowledgement ||
		preview.RequiresDowntimeAcknowledgement ||
		preview.EstimatedDowntimeSeconds != 0 {
		t.Fatalf(`takeover acknowledgement contract=%+v`, preview)
	}
	commit := commitDNSEngineTakeover(
		t, panel, strings.Repeat("7", 32), 0, preview.PreviewToken, false,
	)
	if commit.Code != http.StatusBadRequest ||
		!strings.Contains(commit.Body.String(), "adoption acknowledgement") {
		t.Fatalf(`unacknowledged commit status=%d body=%s`,
			commit.Code, commit.Body.String())
	}
	state, err := readDNSEngineDBState(context.Background(), panel.db.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	if state.ActiveEngine != "" || state.EngineEpoch != 0 ||
		state.CurrentSwitchID != "" {
		t.Fatalf(`refused commit changed state=%+v`, state)
	}
}

func TestDNSEngineTakeoverActivatesPreinstalledBIND(t *testing.T) {
	panel, agent := newStoppedUnmanagedBINDPanel(t)
	preview, recorder := requestDNSEnginePreview(
		t, panel, transport.DNSEngineBIND, nil, 0,
	)
	if recorder.Code != http.StatusOK || len(preview.Blockers) != 0 {
		t.Fatalf(`preview status=%d body=%s`, recorder.Code, recorder.Body.String())
	}
	commit := commitDNSEngineTakeover(
		t, panel, strings.Repeat("8", 32), 0, preview.PreviewToken, true,
	)
	if commit.Code != http.StatusOK {
		t.Fatalf(`takeover commit status=%d body=%s`,
			commit.Code, commit.Body.String())
	}
	state, err := readDNSEngineDBState(context.Background(), panel.db.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	if state.ActiveEngine != transport.DNSEngineBIND ||
		state.EngineEpoch != 1 || state.CurrentSwitchID != "" {
		t.Fatalf(`takeover state=%+v`, state)
	}
	agent.mu.Lock()
	requests := append(
		[]transport.SwitchDNSEngineV1Request(nil), agent.switchRequests...,
	)
	agent.mu.Unlock()
	if len(requests) != 1 ||
		requests[0].Mode != transport.DNSEngineSwitchModeSwitch ||
		requests[0].SourceEngine != "" ||
		requests[0].TargetEngine != transport.DNSEngineBIND ||
		requests[0].TargetEpoch != 1 {
		t.Fatalf(`takeover dispatched %+v`, requests)
	}
	// The takeover is audited under its own name so an operator reading the
	// log can tell it apart from an ordinary first install.
	for _, outcome := range []string{`accepted`, `succeeded`} {
		want := `dns.engine.switch.` + outcome +
			` request=` + strings.Repeat("8", 32) + `%` +
			` source=none target=bind action=` +
			dnsEngineActionAdoptUnmanaged + ` mode=switch`
		var count int
		if err := panel.db.GetDB().QueryRow(
			`SELECT count(*) FROM audit_logs WHERE action LIKE ?`, want,
		).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Errorf(`audit %s count=%d want=1`, outcome, count)
		}
	}
}

func newRunningUnmanagedBINDPanel(t *testing.T) (*Panel, *dnsEngineTestAgent) {
	t.Helper()
	panel, agent := newStoppedUnmanagedBINDPanel(t)
	agent.mu.Lock()
	running := agent.runtimes[transport.DNSEngineBIND]
	running.Running = true
	agent.runtimes[transport.DNSEngineBIND] = running
	agent.mu.Unlock()
	return panel, agent
}

// The running half of the takeover: the same action, the same acknowledgement,
// honestly different impacts, and it is still refused without the
// acknowledgement (register R-039).
//
// Devralmanin calisan yarisi: ayni eylem, ayni onay, durustce farkli etkiler ve
// onay olmadan yine reddedilir (defter R-039).
func TestDNSEngineTakeoverAdoptsARunningUnmanagedBIND(t *testing.T) {
	panel, agent := newRunningUnmanagedBINDPanel(t)
	preview, recorder := requestDNSEnginePreview(
		t, panel, transport.DNSEngineBIND, nil, 0,
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf(`preview status=%d body=%s`, recorder.Code, recorder.Body.String())
	}
	if preview.Action != dnsEngineActionAdoptUnmanaged ||
		len(preview.Blockers) != 0 || preview.PreviewToken == "" {
		t.Fatalf(`running-shape preview=%+v`, preview)
	}
	if !preview.RequiresAdoptionAcknowledgement ||
		preview.RequiresDowntimeAcknowledgement ||
		preview.EstimatedDowntimeSeconds != 0 {
		t.Fatalf(`running takeover acknowledgement contract=%+v`, preview)
	}
	requireDNSEngineImpacts(t, preview.Impacts,
		[]string{"replace_foreign_config", "reload_target", "keep_unknown_zones"},
		[]string{"start_target", "brief_dns_interruption"},
	)

	refused := commitDNSEngineTakeover(
		t, panel, strings.Repeat("9", 32), 0, preview.PreviewToken, false,
	)
	if refused.Code != http.StatusBadRequest ||
		!strings.Contains(refused.Body.String(), "adoption acknowledgement") {
		t.Fatalf(`unacknowledged running commit status=%d body=%s`,
			refused.Code, refused.Body.String())
	}
	agent.mu.Lock()
	calls := agent.switchCalls
	agent.mu.Unlock()
	if calls != 0 {
		t.Fatalf(`refused running takeover dispatched %d switches`, calls)
	}

	preview, recorder = requestDNSEnginePreview(
		t, panel, transport.DNSEngineBIND, nil, 0,
	)
	if recorder.Code != http.StatusOK || len(preview.Blockers) != 0 {
		t.Fatalf(`second preview status=%d body=%s`, recorder.Code, recorder.Body.String())
	}
	commit := commitDNSEngineTakeover(
		t, panel, strings.Repeat("b", 32), 0, preview.PreviewToken, true,
	)
	if commit.Code != http.StatusOK {
		t.Fatalf(`running takeover commit status=%d body=%s`,
			commit.Code, commit.Body.String())
	}
	state, err := readDNSEngineDBState(context.Background(), panel.db.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	if state.ActiveEngine != transport.DNSEngineBIND ||
		state.EngineEpoch != 1 || state.CurrentSwitchID != "" {
		t.Fatalf(`running takeover state=%+v`, state)
	}
	agent.mu.Lock()
	requests := append(
		[]transport.SwitchDNSEngineV1Request(nil), agent.switchRequests...,
	)
	agent.mu.Unlock()
	if len(requests) != 1 ||
		requests[0].Mode != transport.DNSEngineSwitchModeSwitch ||
		requests[0].SourceEngine != "" ||
		requests[0].TargetEngine != transport.DNSEngineBIND ||
		requests[0].TargetEpoch != 1 {
		t.Fatalf(`running takeover dispatched %+v`, requests)
	}
}

// A DNS engine CelikPanel already manages is never offered a takeover, running
// or stopped: there is nothing foreign to take over and no consent to collect.
// This is the half of the gate that must not move.
//
// CelikPanel'in zaten yonettigi bir DNS motoruna, calissin ya da dursun, asla
// devralma sunulmaz: devralinacak yabanci bir sey ve toplanacak bir riza
// yoktur. Kapinin kimildamamasi gereken yarisi budur.
func TestDNSEngineManagedBINDIsNeverOfferedATakeover(t *testing.T) {
	for _, running := range []bool{false, true} {
		runtimes := stoppedUnmanagedBINDRuntimes()
		bind := runtimes[transport.DNSEngineBIND]
		bind.Managed, bind.Running = true, running
		runtimes[transport.DNSEngineBIND] = bind
		state := dnsEngineStateUnconfigured
		if running {
			state = dnsEngineStateUnmanaged
		}
		snapshot := stagedStandaloneSnapshot(runtimes, state)
		if adoptableUnmanagedDNSEngine(snapshot, transport.DNSEngineBIND) {
			t.Fatalf(`managed BIND (running=%v) was adoptable`, running)
		}
		if action := dnsEngineAction(
			snapshot, transport.DNSEngineBIND,
		); action == dnsEngineActionAdoptUnmanaged {
			t.Fatalf(`managed BIND (running=%v) was offered a takeover`, running)
		}
	}
}

// The takeover's own acknowledgement is not optional in either half, and a
// commit that arrives without it changes nothing.
//
// Devralmanin kendi onayi iki yarida da istege bagli degildir ve onsuz gelen
// bir commit hicbir seyi degistirmez.
func TestDNSEngineTakeoverAcknowledgementIsRequiredInBothShapes(t *testing.T) {
	shapes := map[string]func(*testing.T) (*Panel, *dnsEngineTestAgent){
		"stopped": newStoppedUnmanagedBINDPanel,
		"running": newRunningUnmanagedBINDPanel,
	}
	for name, newPanel := range shapes {
		t.Run(name, func(t *testing.T) {
			panel, _ := newPanel(t)
			preview, recorder := requestDNSEnginePreview(
				t, panel, transport.DNSEngineBIND, nil, 0,
			)
			if recorder.Code != http.StatusOK ||
				preview.Action != dnsEngineActionAdoptUnmanaged ||
				!preview.RequiresAdoptionAcknowledgement {
				t.Fatalf(`preview=%+v body=%s`, preview, recorder.Body.String())
			}
			commit := commitDNSEngineTakeover(
				t, panel, strings.Repeat("c", 32), 0, preview.PreviewToken, false,
			)
			if commit.Code != http.StatusBadRequest ||
				!strings.Contains(commit.Body.String(), "adoption acknowledgement") {
				t.Fatalf(`status=%d body=%s`, commit.Code, commit.Body.String())
			}
			state, err := readDNSEngineDBState(
				context.Background(), panel.db.GetDB(),
			)
			if err != nil {
				t.Fatal(err)
			}
			if state.ActiveEngine != "" || state.EngineEpoch != 0 ||
				state.CurrentSwitchID != "" {
				t.Fatalf(`refused commit changed state=%+v`, state)
			}
		})
	}
}

// Identity staging is the first door of the takeover and it opens for both
// halves: staging writes settings and nothing else - no package, no unit, no
// configuration - so a serving unmanaged engine is no reason to refuse it
// (register R-038, R-039). What it still refuses is a host that is not a
// takeover host at all: an engine CelikPanel already manages, or a contested
// port 53.
//
// Kimlik hazirlama devralmanin ilk kapisidir ve iki yariya da acilir: hazirlama
// ayarlari yazar, baska bir sey yapmaz - ne paket, ne birim, ne yapilandirma -
// dolayisiyla hizmet veren panel disi bir motor onu reddetmek icin sebep
// degildir (defter R-038, R-039). Hala reddettigi sey, hic devralma sunucusu
// olmayan bir sunucudur: CelikPanel'in zaten yonettigi bir motor ya da
// cekismeli bir 53 numarali baglanti noktasi.
func TestDNSIdentityStagingOpensForBothTakeoverShapes(t *testing.T) {
	stopped := stoppedUnmanagedBINDRuntimes()
	if kind := dnsIdentityStagingKind(
		stopped, false, transport.DNSTopologyStandalone, "", 0,
	); kind != dnsIdentityStagingFresh {
		t.Fatalf(`stopped unmanaged BIND staging kind=%q`, kind)
	}
	running := stoppedUnmanagedBINDRuntimes()
	state := running[transport.DNSEngineBIND]
	state.Running = true
	running[transport.DNSEngineBIND] = state
	if kind := dnsIdentityStagingKind(
		running, false, transport.DNSTopologyStandalone, "", 0,
	); kind != dnsIdentityStagingBINDTakeover {
		t.Fatalf(`serving unmanaged BIND staging kind=%q`, kind)
	}
	// A serving engine CelikPanel manages is still not a takeover host, and a
	// contested port 53 is still refused outright.
	//
	// CelikPanel'in yonettigi hizmet veren bir motor yine devralma sunucusu
	// degildir ve cekismeli bir 53 numarali baglanti noktasi yine reddedilir.
	managed := stoppedUnmanagedBINDRuntimes()
	bind := managed[transport.DNSEngineBIND]
	bind.Running, bind.Managed = true, true
	managed[transport.DNSEngineBIND] = bind
	if kind := dnsIdentityStagingKind(
		managed, false, transport.DNSTopologyStandalone, "", 0,
	); kind != "" {
		t.Fatalf(`managed serving BIND staging kind=%q, want refusal`, kind)
	}
	if kind := dnsIdentityStagingKind(
		running, true, transport.DNSTopologyStandalone, "", 0,
	); kind != "" {
		t.Fatalf(`contested port 53 staging kind=%q, want refusal`, kind)
	}
}
