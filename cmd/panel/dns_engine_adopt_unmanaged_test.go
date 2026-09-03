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

func TestDNSEngineTakeoverImpactsNameTheReplacementAndTheLoss(t *testing.T) {
	impacts := dnsEngineImpacts(dnsEngineActionAdoptUnmanaged, false)
	for _, want := range []string{
		"replace_foreign_config", "drop_unknown_zones",
	} {
		found := false
		for _, impact := range impacts {
			if impact == want {
				found = true
			}
		}
		if !found {
			t.Fatalf(`impacts=%v missing %s`, impacts, want)
		}
	}
	for _, unwanted := range []string{
		"install_target", "stop_source", "brief_dns_interruption",
	} {
		for _, impact := range impacts {
			if impact == unwanted {
				t.Fatalf(`impacts=%v must not promise %s`, impacts, unwanted)
			}
		}
	}
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

func TestDNSEngineRunningUnmanagedBINDStaysRefused(t *testing.T) {
	panel, agent := newStoppedUnmanagedBINDPanel(t)
	agent.mu.Lock()
	running := agent.runtimes[transport.DNSEngineBIND]
	running.Running = true
	agent.runtimes[transport.DNSEngineBIND] = running
	agent.mu.Unlock()

	preview, recorder := requestDNSEnginePreview(
		t, panel, transport.DNSEngineBIND, nil, 0,
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf(`preview status=%d body=%s`, recorder.Code, recorder.Body.String())
	}
	if preview.Action == dnsEngineActionAdoptUnmanaged {
		t.Fatalf(`a serving unmanaged BIND was offered a takeover: %+v`, preview)
	}
	if !hasDNSEngineBlocker(preview, "unmanaged_dns_detected") ||
		preview.PreviewToken != "" || preview.RequiresAdoptionAcknowledgement {
		t.Fatalf(`running-shape preview=%+v`, preview)
	}
	commit := commitDNSEngineTakeover(
		t, panel, strings.Repeat("9", 32), 0, strings.Repeat("a", 32), true,
	)
	if commit.Code == http.StatusOK {
		t.Fatalf(`running-shape commit succeeded: %s`, commit.Body.String())
	}
	state, err := readDNSEngineDBState(context.Background(), panel.db.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	if state.ActiveEngine != "" || state.EngineEpoch != 0 {
		t.Fatalf(`running-shape state=%+v`, state)
	}
	agent.mu.Lock()
	calls := agent.switchCalls
	agent.mu.Unlock()
	if calls != 0 {
		t.Fatalf(`running shape dispatched %d switches`, calls)
	}
}

// Identity staging is the first door of the takeover and the last door of the
// running shape: a stopped engine only needs settings written, and a serving
// one must still be refused there (register R-038, out-of-scope half).
//
// Kimlik hazırlama, devralmanın ilk kapısı ve çalışan biçimin son kapısıdır:
// durmuş bir motor için yalnız ayar yazılır, hizmet veren biri orada yine
// reddedilmelidir (defter R-038, kapsam dışı yarı).
func TestDNSIdentityStagingSeparatesTheStoppedShapeFromTheRunningOne(t *testing.T) {
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
	); kind != "" {
		t.Fatalf(`serving unmanaged BIND staging kind=%q, want refusal`, kind)
	}
}
