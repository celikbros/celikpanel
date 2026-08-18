package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

func assertDNSSetupRequired(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", recorder.Code, recorder.Body.String())
	}
	var body apiErrorBody
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("decode legacy DNS refusal: %v", err)
	}
	if body.Code != errCodeDNSSetupRequired {
		t.Fatalf("code = %q, want %q", body.Code, errCodeDNSSetupRequired)
	}
	if body.Action != "/settings?section=dns" {
		t.Fatalf("action = %q, want DNS settings", body.Action)
	}
	if !strings.Contains(body.Error, "/api/v1/settings/dns-setup") {
		t.Fatalf("migration endpoint missing from response: %q", body.Error)
	}
}

func dnsSetupAdminRequest(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPut, "/api/v1/settings/dns-setup", strings.NewReader(body))
	return strictDNSAdminRequest(req)
}

func dnsSetupSystemAdminRequest(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPut, "/api/v1/settings/dns-setup", strings.NewReader(body))
	return req.WithContext(context.WithValue(req.Context(), callerKey, &Caller{Role: roleAdmin}))
}

func TestDNSSetupRejectsUnknownFieldsAndTrailingJSONWithoutMutation(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "unknown field",
			body: `{"ns1":"ns3.example.net","ns2":"ns4.example.net","role":"paired","peer_ip":"198.51.100.20","peer_ns":"ns4.example.net","unexpected":true}`,
		},
		{
			name: "trailing JSON value",
			body: pairedDNSSetupBody + ` {"second":true}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
			p := newDNSPanelForTest(t)
			setDNSIdentityForTest(t, p, "standalone")
			before, err := readDNSClusterTopology(context.Background(), p)
			if err != nil {
				t.Fatal(err)
			}
			agent := &strictDNSRPCAgent{}
			attachStrictDNSRPCAgent(t, p, agent)

			recorder := httptest.NewRecorder()
			p.handleDNSSetup(recorder, dnsSetupAdminRequest(test.body))
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if pending, err := readPendingDNSClusterSaga(
				context.Background(), p,
			); err != nil {
				t.Fatal(err)
			} else if pending != nil {
				t.Fatalf("invalid JSON persisted DNS saga: %+v", pending)
			}
			after, err := readDNSClusterTopology(context.Background(), p)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(after, before) {
				t.Fatalf("invalid JSON changed topology: before=%+v after=%+v", before, after)
			}
			agent.mu.Lock()
			beginCalls, clusterCalls, versionCalls :=
				agent.beginCalls, agent.clusterCalls, agent.versionCalls
			agent.mu.Unlock()
			if beginCalls != 0 || clusterCalls != 0 || versionCalls != 0 {
				t.Fatalf(
					"invalid JSON reached agent: begin=%d cluster=%d version=%d",
					beginCalls, clusterCalls, versionCalls,
				)
			}
		})
	}
}

func assertDNSSetupSettings(t *testing.T, p *Panel, want map[string]string) {
	t.Helper()
	for key, value := range want {
		if got := p.setting(context.Background(), key); got != value {
			t.Fatalf("setting %s = %q, want %q", key, got, value)
		}
	}
}

func seedDNSSetupAuditUser(t *testing.T, p *Panel) {
	t.Helper()
	if _, err := p.db.GetDB().Exec(`
		INSERT OR IGNORE INTO users (
		  id, username, password_hash, email, role
		) VALUES (1, 'dns-setup-admin', 'hash',
		          'dns-setup-admin@example.test', 'admin')
	`); err != nil {
		t.Fatal(err)
	}
}

func stagePairedDNSIdentityForTest(
	t *testing.T,
	p *Panel,
) *httptest.ResponseRecorder {
	t.Helper()
	seedDNSSetupAuditUser(t, p)
	recorder := httptest.NewRecorder()
	p.handleDNSSetup(recorder, dnsSetupAdminRequest(
		`{"ns1":"ns1.example.net","ns2":"ns2.example.net","role":"paired","peer_ip":"192.0.2.20","peer_ns":"ns2.example.net"}`,
	))
	return recorder
}

func TestDNSSetupStagesFreshPairedIdentityWithoutHostMutation(t *testing.T) {
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
	p := newDNSPanelForTest(t)
	agent := newDNSEngineTestAgent()
	attachDNSEngineTestAgent(t, p, agent)

	recorder := stagePairedDNSIdentityForTest(t, p)
	if recorder.Code != http.StatusOK {
		t.Fatalf("stage status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Success bool   `json:"success"`
		Staged  bool   `json:"staged"`
		Result  string `json:"result"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil ||
		!response.Success || !response.Staged ||
		response.Result != dnsSetupResultIdentityStaged {
		t.Fatalf("staging response=%+v err=%v body=%s",
			response, err, recorder.Body.String())
	}
	assertDNSSetupSettings(t, p, map[string]string{
		settingNS1:       "ns1.example.net",
		settingNS2:       "ns2.example.net",
		settingDNSRole:   "paired",
		settingDNSPeerIP: "192.0.2.20",
		settingDNSPeerNS: "ns2.example.net",
	})
	state, err := readDNSEngineDBState(context.Background(), p.db.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	if state.ActiveEngine != "" || state.EngineEpoch != 0 ||
		state.Revision != 1 || state.Topology != transport.DNSTopologyStandalone ||
		state.CurrentSwitchID != "" {
		t.Fatalf("staged engine state=%+v", state)
	}
	snapshot, err := p.dnsEngineSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Topology != transport.DNSTopologyPaired ||
		snapshot.State != dnsEngineStateUnconfigured {
		t.Fatalf("staged snapshot=%+v", snapshot)
	}
	agent.mu.Lock()
	switchCalls, readinessCalls := agent.switchCalls, agent.readinessCalls
	agent.mu.Unlock()
	agent.durableMutationRPCFixture.mu.Lock()
	jobs := len(agent.durableMutationRPCFixture.jobs)
	agent.durableMutationRPCFixture.mu.Unlock()
	if switchCalls != 0 || jobs != 0 || readinessCalls != 3 {
		t.Fatalf("staging host observations switch=%d jobs=%d readiness=%d",
			switchCalls, jobs, readinessCalls)
	}
	var audits int
	if err := p.db.GetDB().QueryRow(`
		SELECT count(*) FROM audit_logs
		WHERE action = 'settings.dns_identity_staged:paired runtime=fresh'
	`).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 1 {
		t.Fatalf("staging audits=%d, want 1", audits)
	}
}

func TestStagedPairedIdentityBuildsExactFirstInstallManifest(t *testing.T) {
	for directionIndex, direction := range []struct {
		name, localIP, peerIP, peerNS, pairRole, localNS string
	}{
		{
			name: "primary", localIP: "192.0.2.10",
			peerIP: "192.0.2.20", peerNS: "ns2.example.net",
			pairRole: transport.DNSPairRolePrimary, localNS: "ns1.example.net",
		},
		{
			name: "secondary", localIP: "192.0.2.20",
			peerIP: "192.0.2.10", peerNS: "ns1.example.net",
			pairRole: transport.DNSPairRoleSecondary, localNS: "ns2.example.net",
		},
	} {
		for targetIndex, target := range []transport.DNSEngine{
			transport.DNSEngineBIND,
			transport.DNSEnginePowerDNS,
		} {
			t.Run(direction.name+"/"+string(target), func(t *testing.T) {
				t.Setenv("CELIKPANEL_SERVER_IP", direction.localIP)
				p := newDNSPanelForTest(t)
				agent := newDNSEngineTestAgent()
				attachDNSEngineTestAgent(t, p, agent)
				seedDNSSetupAuditUser(t, p)
				stage := httptest.NewRecorder()
				body := `{"ns1":"ns1.example.net","ns2":"ns2.example.net","role":"paired","peer_ip":"` +
					direction.peerIP + `","peer_ns":"` + direction.peerNS + `"}`
				p.handleDNSSetup(stage, dnsSetupAdminRequest(body))
				if stage.Code != http.StatusOK {
					t.Fatalf("stage status=%d body=%s", stage.Code, stage.Body.String())
				}
				preview, recorder := requestDNSEnginePreview(t, p, target, nil, 1)
				if recorder.Code != http.StatusOK || len(preview.Blockers) != 0 ||
					preview.Topology != transport.DNSTopologyPaired {
					t.Fatalf("preview=%+v status=%d body=%s",
						preview, recorder.Code, recorder.Body.String())
				}
				commit := commitDNSEngineSwitch(
					t, p,
					strings.Repeat(
						string(rune('1'+directionIndex*2+targetIndex)), 32,
					),
					target, nil, 1, preview.PreviewToken, false,
				)
				if commit.Code != http.StatusOK {
					t.Fatalf("commit status=%d body=%s", commit.Code, commit.Body.String())
				}
				agent.mu.Lock()
				requests := append(
					[]transport.SwitchDNSEngineV1Request(nil),
					agent.switchRequests...,
				)
				agent.mu.Unlock()
				if len(requests) != 1 ||
					requests[0].TargetEngine != target ||
					requests[0].Topology != transport.DNSTopologyPaired ||
					requests[0].PairRole != direction.pairRole ||
					requests[0].LocalIP != direction.localIP ||
					requests[0].LocalNS != direction.localNS ||
					requests[0].PeerIP != direction.peerIP ||
					requests[0].PeerNS != direction.peerNS {
					t.Fatalf(
						"exact staged %s/%s manifest=%+v",
						direction.name, target, requests,
					)
				}
			})
		}
	}
}

func TestDNSSetupStagesStandaloneIdentityForLegacyPowerDNSAdoption(t *testing.T) {
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
	p := newDNSPanelForTest(t)
	agent := newDNSEngineTestAgent()
	pdns := agent.runtimes[transport.DNSEnginePowerDNS]
	pdns.Installed, pdns.Running, pdns.Managed = true, true, true
	agent.runtimes[transport.DNSEnginePowerDNS] = pdns
	attachDNSEngineTestAgent(t, p, agent)
	seedDNSSetupAuditUser(t, p)

	recorder := httptest.NewRecorder()
	p.handleDNSSetup(recorder, dnsSetupAdminRequest(
		`{"ns1":"ns1.example.net","ns2":"ns2.example.net","role":"standalone","peer_ip":"","peer_ns":""}`,
	))
	if recorder.Code != http.StatusOK {
		t.Fatalf("legacy stage status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	preview, previewRecorder := requestDNSEnginePreview(
		t, p, transport.DNSEnginePowerDNS, nil, 1,
	)
	if previewRecorder.Code != http.StatusOK || len(preview.Blockers) != 0 ||
		preview.Action != "adopt" || preview.Topology != transport.DNSTopologyStandalone {
		t.Fatalf("legacy adoption preview=%+v status=%d body=%s",
			preview, previewRecorder.Code, previewRecorder.Body.String())
	}
	agent.mu.Lock()
	switchCalls := agent.switchCalls
	agent.mu.Unlock()
	agent.durableMutationRPCFixture.mu.Lock()
	jobs := len(agent.durableMutationRPCFixture.jobs)
	agent.durableMutationRPCFixture.mu.Unlock()
	if switchCalls != 0 || jobs != 0 {
		t.Fatalf("legacy staging mutated host switch=%d jobs=%d", switchCalls, jobs)
	}
}

func TestDNSSetupStagesEmptyLegacyPowerDNSAsPairedSecondary(t *testing.T) {
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.20")
	p := newDNSPanelForTest(t)
	agent := newDNSEngineTestAgent()
	pdns := agent.runtimes[transport.DNSEnginePowerDNS]
	pdns.Installed, pdns.Running, pdns.Managed = true, true, true
	agent.runtimes[transport.DNSEnginePowerDNS] = pdns
	attachDNSEngineTestAgent(t, p, agent)

	seedDNSSetupAuditUser(t, p)
	recorder := httptest.NewRecorder()
	p.handleDNSSetup(recorder, dnsSetupAdminRequest(
		`{"ns1":"ns1.example.net","ns2":"ns2.example.net","role":"paired","peer_ip":"192.0.2.10","peer_ns":"ns1.example.net"}`,
	))
	if recorder.Code != http.StatusOK {
		t.Fatalf("legacy secondary stage status=%d body=%s",
			recorder.Code, recorder.Body.String())
	}
	assertDNSSetupSettings(t, p, map[string]string{
		settingNS1:       "ns1.example.net",
		settingNS2:       "ns2.example.net",
		settingDNSRole:   "paired",
		settingDNSPeerIP: "192.0.2.10",
		settingDNSPeerNS: "ns1.example.net",
	})
	state, err := readDNSEngineDBState(context.Background(), p.db.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	if state.ActiveEngine != "" || state.EngineEpoch != 0 ||
		state.Revision != 1 {
		t.Fatalf("legacy secondary staged state=%+v", state)
	}
}

func TestDNSSetupRejectsLegacyPowerDNSPairedPrimaryOrNonemptyLedger(
	t *testing.T,
) {
	for _, test := range []struct {
		name      string
		localIP   string
		body      string
		seedZones bool
	}{
		{
			name: "primary", localIP: "192.0.2.10",
			body: `{"ns1":"ns1.example.net","ns2":"ns2.example.net","role":"paired","peer_ip":"192.0.2.20","peer_ns":"ns2.example.net"}`,
		},
		{
			name: "nonempty secondary", localIP: "192.0.2.20",
			body:      `{"ns1":"ns1.example.net","ns2":"ns2.example.net","role":"paired","peer_ip":"192.0.2.10","peer_ns":"ns1.example.net"}`,
			seedZones: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("CELIKPANEL_SERVER_IP", test.localIP)
			p := newDNSPanelForTest(t)
			if test.seedZones {
				if _, err := p.db.GetDB().Exec(`
					INSERT INTO dns_zone_sync_state (
					  zone_name, desired_generation, applied_generation,
					  desired_action, desired_zone_type, status
					) VALUES ('existing.example', 1, 1, 'delete', 'NATIVE', 'applied')
				`); err != nil {
					t.Fatal(err)
				}
			}
			agent := newDNSEngineTestAgent()
			pdns := agent.runtimes[transport.DNSEnginePowerDNS]
			pdns.Installed, pdns.Running, pdns.Managed = true, true, true
			agent.runtimes[transport.DNSEnginePowerDNS] = pdns
			attachDNSEngineTestAgent(t, p, agent)
			recorder := httptest.NewRecorder()
			p.handleDNSSetup(recorder, dnsSetupAdminRequest(test.body))
			if recorder.Code != http.StatusConflict ||
				!strings.Contains(recorder.Body.String(), errCodeDNSEngineWorkflowRequired) {
				t.Fatalf("unsafe legacy stage status=%d body=%s",
					recorder.Code, recorder.Body.String())
			}
			assertDNSSetupSettings(t, p, map[string]string{
				settingNS1: "", settingNS2: "", settingDNSRole: "",
			})
		})
	}
}

func TestDNSSetupStagedIdentityRetryAndEditAreRevisionExact(t *testing.T) {
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
	p := newDNSPanelForTest(t)
	agent := newDNSEngineTestAgent()
	attachDNSEngineTestAgent(t, p, agent)
	body := `{"ns1":"ns1.example.net","ns2":"ns2.example.net","role":"paired","peer_ip":"192.0.2.20","peer_ns":"ns2.example.net"}`
	seedDNSSetupAuditUser(t, p)
	for attempt := 0; attempt < 2; attempt++ {
		recorder := httptest.NewRecorder()
		p.handleDNSSetup(recorder, dnsSetupAdminRequest(body))
		if recorder.Code != http.StatusOK ||
			!strings.Contains(recorder.Body.String(), `"staged":true`) {
			t.Fatalf("stage attempt %d status=%d body=%s",
				attempt+1, recorder.Code, recorder.Body.String())
		}
	}
	state, err := readDNSEngineDBState(context.Background(), p.db.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	if state.Revision != 1 {
		t.Fatalf("exact retry advanced revision to %d", state.Revision)
	}
	var audits int
	if err := p.db.GetDB().QueryRow(`
		SELECT count(*) FROM audit_logs
		WHERE action = 'settings.dns_identity_staged:paired runtime=fresh'
	`).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 1 {
		t.Fatalf("exact retry audit count=%d, want 1", audits)
	}
	edit := httptest.NewRecorder()
	p.handleDNSSetup(edit, dnsSetupAdminRequest(
		`{"ns1":"ns3.example.net","ns2":"ns4.example.net","role":"standalone","peer_ip":"","peer_ns":""}`,
	))
	if edit.Code != http.StatusOK {
		t.Fatalf("staged edit status=%d body=%s", edit.Code, edit.Body.String())
	}
	state, err = readDNSEngineDBState(context.Background(), p.db.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	if state.Revision != 2 {
		t.Fatalf("staged edit revision=%d, want 2", state.Revision)
	}
	assertDNSSetupSettings(t, p, map[string]string{
		settingNS1: "ns3.example.net", settingNS2: "ns4.example.net",
		settingDNSRole: "standalone", settingDNSPeerIP: "",
		settingDNSPeerNS: "",
	})
}

func TestDNSSetupStagingRechecksDBAndRuntimeUnderLock(t *testing.T) {
	for _, test := range []struct {
		name string
		hook func(*Panel, *dnsEngineTestAgent, int)
	}{
		{
			name: "database revision",
			hook: func(p *Panel, _ *dnsEngineTestAgent, call int) {
				if call == 1 {
					if _, err := p.db.GetDB().Exec(`
						UPDATE dns_engine_state SET revision = revision + 1
						WHERE singleton_id = 1
					`); err != nil {
						panic(err)
					}
				}
			},
		},
		{
			name: "runtime identity",
			hook: func(_ *Panel, agent *dnsEngineTestAgent, call int) {
				if call == 1 {
					agent.mu.Lock()
					pdns := agent.runtimes[transport.DNSEnginePowerDNS]
					pdns.Installed, pdns.Running, pdns.Managed = true, true, true
					agent.runtimes[transport.DNSEnginePowerDNS] = pdns
					agent.mu.Unlock()
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
			p := newDNSPanelForTest(t)
			agent := newDNSEngineTestAgent()
			agent.onReadiness = func(call int) { test.hook(p, agent, call) }
			attachDNSEngineTestAgent(t, p, agent)
			seedDNSSetupAuditUser(t, p)
			recorder := httptest.NewRecorder()
			p.handleDNSSetup(recorder, dnsSetupAdminRequest(
				`{"ns1":"ns1.example.net","ns2":"ns2.example.net","role":"standalone","peer_ip":"","peer_ns":""}`,
			))
			if recorder.Code != http.StatusConflict {
				t.Fatalf("race status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			assertDNSSetupSettings(t, p, map[string]string{
				settingNS1: "", settingNS2: "", settingDNSRole: "",
			})
			agent.mu.Lock()
			switchCalls := agent.switchCalls
			agent.mu.Unlock()
			if switchCalls != 0 {
				t.Fatalf("race performed %d host switches", switchCalls)
			}
		})
	}
}

func TestDNSSetupStagingTransactionRollsBackRevisionOnLedgerFailure(t *testing.T) {
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
	p := newDNSPanelForTest(t)
	agent := newDNSEngineTestAgent()
	attachDNSEngineTestAgent(t, p, agent)
	seedDNSSetupAuditUser(t, p)
	if _, err := p.db.GetDB().Exec(`
		CREATE TRIGGER reject_staged_dns_role
		BEFORE INSERT ON panel_settings
		WHEN NEW.key = 'dns_role'
		BEGIN
		  SELECT RAISE(ABORT, 'injected DNS identity ledger failure');
		END
	`); err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	p.handleDNSSetup(recorder, dnsSetupAdminRequest(
		`{"ns1":"ns1.example.net","ns2":"ns2.example.net","role":"standalone","peer_ip":"","peer_ns":""}`,
	))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("failed stage status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	state, err := readDNSEngineDBState(context.Background(), p.db.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	if !exactUninitializedDNSEngineState(state) {
		t.Fatalf("failed staging advanced engine revision: %+v", state)
	}
	assertDNSSetupSettings(t, p, map[string]string{
		settingNS1: "", settingNS2: "", settingDNSRole: "",
	})
}

func TestDNSSetupStagingRejectsDurableDNSAmbiguity(t *testing.T) {
	for _, test := range []struct {
		name    string
		prepare func(*testing.T, *Panel)
	}{
		{
			name: "engine operation marker",
			prepare: func(t *testing.T, p *Panel) {
				raw, err := encodeDNSEngineOperationMarker(dnsEngineOperationMarker{
					Version:      dnsEngineOperationVersion,
					RequestID:    strings.Repeat("a", 32),
					SwitchID:     strings.Repeat("b", 32),
					TargetEngine: transport.DNSEngineBIND,
					Action:       "install", Phase: dnsEngineOperationAccepted,
				})
				if err != nil {
					t.Fatal(err)
				}
				if err := p.setSetting(
					context.Background(), dnsEngineOperationSetting, raw,
				); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "topology operation marker",
			prepare: func(t *testing.T, p *Panel) {
				if err := p.setSetting(
					context.Background(), dnsClusterSagaSetting, `{"pending":true}`,
				); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "pending publication",
			prepare: func(t *testing.T, p *Panel) {
				if _, err := p.db.GetDB().Exec(`
					INSERT INTO dns_zone_sync_state (
					  zone_name, desired_generation, applied_generation,
					  desired_action, desired_zone_type, status
					) VALUES ('pending.example', 1, 0, 'delete', 'NATIVE', 'pending')
				`); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
			p := newDNSPanelForTest(t)
			test.prepare(t, p)
			agent := newDNSEngineTestAgent()
			attachDNSEngineTestAgent(t, p, agent)
			seedDNSSetupAuditUser(t, p)
			recorder := httptest.NewRecorder()
			p.handleDNSSetup(recorder, dnsSetupAdminRequest(
				`{"ns1":"ns1.example.net","ns2":"ns2.example.net","role":"standalone","peer_ip":"","peer_ns":""}`,
			))
			if recorder.Code != http.StatusConflict {
				t.Fatalf("ambiguity status=%d body=%s",
					recorder.Code, recorder.Body.String())
			}
			state, err := readDNSEngineDBState(
				context.Background(), p.db.GetDB(),
			)
			if err != nil {
				t.Fatal(err)
			}
			if !exactUninitializedDNSEngineState(state) {
				t.Fatalf("ambiguity advanced engine state: %+v", state)
			}
			agent.mu.Lock()
			switchCalls := agent.switchCalls
			agent.mu.Unlock()
			if switchCalls != 0 {
				t.Fatalf("ambiguity performed %d host switches", switchCalls)
			}
		})
	}
}

func TestDNSSetupRequiresAdminAndPUT(t *testing.T) {
	p := newDNSPanelForTest(t)

	unauthorized := httptest.NewRecorder()
	p.handleDNSSetup(unauthorized, httptest.NewRequest(
		http.MethodPut,
		"/api/v1/settings/dns-setup",
		strings.NewReader(`{"role":"standalone"}`),
	))
	if unauthorized.Code != http.StatusForbidden {
		t.Fatalf("unauthorized status = %d, want 403; body=%s", unauthorized.Code, unauthorized.Body.String())
	}

	wrongMethod := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/settings/dns-setup", nil)
	request = request.WithContext(context.WithValue(request.Context(), callerKey, &Caller{Role: roleAdmin}))
	p.handleDNSSetup(wrongMethod, request)
	if wrongMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET status = %d, want 405; body=%s", wrongMethod.Code, wrongMethod.Body.String())
	}
}

func TestDNSSetupPairedRenameCommitsOneTopology(t *testing.T) {
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "paired")
	zoneID := seedReconcileZone(t, p, "biovision.health")
	agent := &strictDNSRPCAgent{}
	attachStrictDNSRPCAgent(t, p, agent)

	recorder := httptest.NewRecorder()
	p.handleDNSSetup(recorder, dnsSetupAdminRequest(
		`{"ns1":"ns3.biovision.health","ns2":"ns4.biovision.health","role":"paired","peer_ip":"198.51.100.20","peer_ns":"ns4.biovision.health"}`,
	))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	assertDNSSetupSettings(t, p, map[string]string{
		settingNS1:       "ns3.biovision.health",
		settingNS2:       "ns4.biovision.health",
		settingDNSRole:   "paired",
		settingDNSPeerIP: "198.51.100.20",
		settingDNSPeerNS: "ns4.biovision.health",
	})

	rows, err := p.db.GetDB().QueryContext(context.Background(), `
		SELECT content FROM pdns_records
		WHERE domain_id = ? AND LOWER(TRIM(name, '.')) = 'biovision.health' AND UPPER(type) = 'NS'
		ORDER BY content`, zoneID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var gotNS []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			t.Fatal(err)
		}
		gotNS = append(gotNS, value)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if want := []string{"ns3.biovision.health", "ns4.biovision.health"}; !reflect.DeepEqual(gotNS, want) {
		t.Fatalf("apex NS RRset = %v, want %v", gotNS, want)
	}
	if soa := recordContent(t, p, zoneID, "biovision.health", "SOA"); !strings.HasPrefix(soa, "ns3.biovision.health ") {
		t.Fatalf("SOA MNAME = %q, want local ns3", soa)
	}
	assertSingleReconciledA(t, p, zoneID, "ns3.biovision.health", "192.0.2.10")
	assertSingleReconciledA(t, p, zoneID, "ns4.biovision.health", "198.51.100.20")
}

func TestDNSSetupCommitsPowerDNSTopologyWithIdentity(t *testing.T) {
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "standalone")
	agent := &strictDNSRPCAgent{}
	attachStrictDNSRPCAgent(t, p, agent)
	before, err := readDNSEngineDBState(context.Background(), p.db.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	if before.Topology != transport.DNSTopologyStandalone {
		t.Fatalf("initial durable topology=%q", before.Topology)
	}

	paired := httptest.NewRecorder()
	p.handleDNSSetup(paired, dnsSetupAdminRequest(
		`{"ns1":"ns3.example.net","ns2":"ns4.example.net","role":"paired","peer_ip":"198.51.100.20","peer_ns":"ns4.example.net"}`,
	))
	if paired.Code != http.StatusOK {
		t.Fatalf("paired status=%d body=%s", paired.Code, paired.Body.String())
	}
	pairedState, err := readDNSEngineDBState(
		context.Background(), p.db.GetDB(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if pairedState.ActiveEngine != transport.DNSEnginePowerDNS ||
		pairedState.Topology != transport.DNSTopologyPaired ||
		pairedState.Revision != before.Revision+1 {
		t.Fatalf("paired durable state=%+v before=%+v", pairedState, before)
	}

	standalone := httptest.NewRecorder()
	p.handleDNSSetup(standalone, dnsSetupAdminRequest(
		`{"ns1":"ns3.example.net","ns2":"ns4.example.net","role":"standalone","peer_ip":"","peer_ns":""}`,
	))
	if standalone.Code != http.StatusOK {
		t.Fatalf("standalone status=%d body=%s",
			standalone.Code, standalone.Body.String())
	}
	standaloneState, err := readDNSEngineDBState(
		context.Background(), p.db.GetDB(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if standaloneState.Topology != transport.DNSTopologyStandalone ||
		standaloneState.Revision != pairedState.Revision+1 {
		t.Fatalf("standalone durable state=%+v paired=%+v",
			standaloneState, pairedState)
	}
}

func TestDNSSetupRejectsInvalidPairedTupleBeforeAgent(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{
			name: "self peer",
			body: `{"ns1":"ns1.example.net","ns2":"ns2.example.net","role":"paired","peer_ip":"192.0.2.10","peer_ns":"ns2.example.net"}`,
		},
		{
			name: "peer name outside pair",
			body: `{"ns1":"ns1.example.net","ns2":"ns2.example.net","role":"paired","peer_ip":"198.51.100.20","peer_ns":"ns9.example.net"}`,
		},
		{
			name: "unspecified peer",
			body: `{"ns1":"ns1.example.net","ns2":"ns2.example.net","role":"paired","peer_ip":"0.0.0.0","peer_ns":"ns2.example.net"}`,
		},
		{
			name: "nameserver label too long",
			body: `{"ns1":"` + strings.Repeat("a", 64) + `.example.net","ns2":"ns2.example.net","role":"paired","peer_ip":"198.51.100.20","peer_ns":"ns2.example.net"}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
			p := newDNSPanelForTest(t)
			agent := &strictDNSRPCAgent{}
			attachStrictDNSRPCAgent(t, p, agent)

			recorder := httptest.NewRecorder()
			p.handleDNSSetup(recorder, dnsSetupAdminRequest(tc.body))
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", recorder.Code, recorder.Body.String())
			}
			agent.mu.Lock()
			calls := agent.clusterCalls
			agent.mu.Unlock()
			if calls != 0 {
				t.Fatalf("invalid tuple reached the agent %d time(s)", calls)
			}
			assertDNSSetupSettings(t, p, map[string]string{
				settingNS1: "", settingNS2: "", settingDNSRole: "",
			})
		})
	}
}

func TestDNSSetupKnownTerminalFailureRetainsDesiredLedgerAndSaga(t *testing.T) {
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "paired")
	agent := &strictDNSRPCAgent{clusterError: "forced agent rejection"}
	attachStrictDNSRPCAgent(t, p, agent)

	recorder := httptest.NewRecorder()
	p.handleDNSSetup(recorder, dnsSetupAdminRequest(
		`{"ns1":"ns3.example.net","ns2":"ns4.example.net","role":"paired","peer_ip":"198.51.100.20","peer_ns":"ns4.example.net"}`,
	))
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", recorder.Code, recorder.Body.String())
	}
	assertDNSSetupSettings(t, p, map[string]string{
		settingNS1:       "ns1.celikhost.com",
		settingNS2:       "ns2.celikhost.com",
		settingDNSRole:   "paired",
		settingDNSPeerIP: "2.25.80.4",
		settingDNSPeerNS: "ns2.celikhost.com",
	})
	pending, err := readPendingDNSClusterSaga(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if pending == nil || pending.Desired.Role != "paired" ||
		pending.Desired.PeerIP != "198.51.100.20" ||
		pending.Desired.PeerNS != "ns4.example.net" {
		t.Fatalf("known terminal failure lost desired recovery saga=%+v", pending)
	}
	agent.mu.Lock()
	clusterCalls, syncCalls := agent.clusterCalls, len(agent.syncRequests)
	agent.mu.Unlock()
	if clusterCalls != 1 || syncCalls != 0 {
		t.Fatalf("terminal failure cluster/sync calls=%d/%d, want 1/0",
			clusterCalls, syncCalls)
	}
}

func TestDNSSetupV2PreflightRejectsBeforeHostOrLedgerMutation(t *testing.T) {
	for _, tc := range []struct {
		name         string
		capabilities []string
		rhel         bool
		wantStatus   int
	}{
		{name: "legacy agent", capabilities: []string{}, wantStatus: http.StatusInternalServerError},
		{
			name: "RHEL policy denial", capabilities: []string{transport.AgentCapabilityDNSZoneSyncV2},
			rhel: true, wantStatus: http.StatusConflict,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
			p := newDNSPanelForTest(t)
			setDNSIdentityForTest(t, p, "paired")
			zoneID := seedReconcileZone(t, p, "preflight.example")
			beforeState, err := readDNSZoneSyncState(context.Background(), p.db.GetDB(), "preflight.example")
			if err != nil {
				t.Fatal(err)
			}
			beforeSOA := recordContent(t, p, zoneID, "preflight.example", "SOA")
			capabilities := append([]string(nil), tc.capabilities...)
			agent := &strictDNSRPCAgent{versionCapabilities: &capabilities}
			attachStrictDNSRPCAgent(t, p, agent)
			if tc.rhel {
				p.pkgFamilyMu.Lock()
				p.pkgFamilyVal = "dnf"
				p.hostPlatformVal = rhelPolicyTestIdentity()
				p.hostPlatformKnown = true
				p.pkgFamilyMu.Unlock()
			}

			recorder := httptest.NewRecorder()
			p.handleDNSSetup(recorder, dnsSetupAdminRequest(
				`{"ns1":"ns3.example.net","ns2":"ns4.example.net","role":"paired","peer_ip":"198.51.100.20","peer_ns":"ns4.example.net"}`,
			))
			if recorder.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, tc.wantStatus, recorder.Body.String())
			}
			assertDNSSetupSettings(t, p, map[string]string{
				settingNS1:       "ns1.celikhost.com",
				settingNS2:       "ns2.celikhost.com",
				settingDNSRole:   "paired",
				settingDNSPeerIP: "2.25.80.4",
				settingDNSPeerNS: "ns2.celikhost.com",
			})
			if got := recordContent(t, p, zoneID, "preflight.example", "SOA"); got != beforeSOA {
				t.Fatalf("preflight rejection rewrote SOA: before=%q after=%q", beforeSOA, got)
			}
			afterState, err := readDNSZoneSyncState(context.Background(), p.db.GetDB(), "preflight.example")
			if err != nil {
				t.Fatal(err)
			}
			if afterState.hasLease() || afterState.DesiredGeneration != beforeState.DesiredGeneration ||
				afterState.AppliedGeneration != beforeState.AppliedGeneration || afterState.Status != beforeState.Status {
				t.Fatalf("preflight rejection mutated DNS ledger: before=%+v after=%+v", beforeState, afterState)
			}

			agent.mu.Lock()
			clusterCalls := agent.clusterCalls
			beginCalls := agent.beginCalls
			syncCalls := len(agent.syncCalls)
			agent.mu.Unlock()
			agent.durableMutationRPCFixture.mu.Lock()
			jobs := len(agent.durableMutationRPCFixture.jobs)
			agent.durableMutationRPCFixture.mu.Unlock()
			if clusterCalls != 0 || beginCalls != 0 || syncCalls != 0 || jobs != 0 {
				t.Fatalf("preflight rejection reached host: configure=%d begin=%d V2=%d jobs=%d",
					clusterCalls, beginCalls, syncCalls, jobs)
			}
		})
	}
}

func TestDNSSetupDesiredPersistenceFailurePrecedesAgentBegin(t *testing.T) {
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "paired")
	agent := &strictDNSRPCAgent{}
	attachStrictDNSRPCAgent(t, p, agent)
	rejectDNSClusterSettingWrites(t, p)

	recorder := httptest.NewRecorder()
	p.handleDNSSetup(recorder, dnsSetupAdminRequest(
		`{"ns1":"ns3.example.net","ns2":"ns4.example.net","role":"paired","peer_ip":"198.51.100.20","peer_ns":"ns4.example.net"}`,
	))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", recorder.Code, recorder.Body.String())
	}
	agent.mu.Lock()
	beginCalls, clusterCalls := agent.beginCalls, agent.clusterCalls
	agent.mu.Unlock()
	if beginCalls != 0 || clusterCalls != 0 {
		t.Fatalf("failed desired transaction reached host begin/cluster=%d/%d",
			beginCalls, clusterCalls)
	}
	assertDNSSetupSettings(t, p, map[string]string{
		settingNS1:       "ns1.celikhost.com",
		settingNS2:       "ns2.celikhost.com",
		settingDNSRole:   "paired",
		settingDNSPeerIP: "2.25.80.4",
		settingDNSPeerNS: "ns2.celikhost.com",
	})
	if pending, err := readPendingDNSClusterSaga(context.Background(), p); err != nil {
		t.Fatal(err)
	} else if pending != nil {
		t.Fatalf("rolled-back desired transaction retained saga=%+v", pending)
	}
}

func TestDNSSetupActiveBINDPublishesStandaloneIdentityWithoutPDNSRPC(t *testing.T) {
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
	p := newDNSPanelForTest(t)
	zoneID := seedReconcileZone(t, p, "bind-identity.example")
	agent := &strictDNSRPCAgent{}
	attachStrictDNSRPCAgentForEngine(
		t, p, agent, transport.DNSEngineBIND,
	)

	recorder := httptest.NewRecorder()
	p.handleDNSSetup(recorder, dnsSetupAdminRequest(
		`{"ns1":"ns3.example.net","ns2":"ns4.example.net","role":"standalone","peer_ip":"","peer_ns":""}`,
	))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	assertDNSSetupSettings(t, p, map[string]string{
		settingNS1:       "ns3.example.net",
		settingNS2:       "ns4.example.net",
		settingDNSRole:   "standalone",
		settingDNSPeerIP: "",
		settingDNSPeerNS: "",
	})
	if soa := recordContent(
		t, p, zoneID, "bind-identity.example", "SOA",
	); !strings.HasPrefix(soa, "ns3.example.net ") {
		t.Fatalf("BIND identity SOA=%q, want ns3 MNAME", soa)
	}
	agent.mu.Lock()
	clusterCalls := agent.clusterCalls
	beginCalls := agent.beginCalls
	readinessCalls := agent.readinessCalls
	powerDNSCalls := agent.powerDNSCalls
	requests := append(
		[]transport.SyncDNSZoneV3Request(nil),
		agent.syncV3Requests...,
	)
	agent.mu.Unlock()
	if clusterCalls != 0 || readinessCalls != 0 || powerDNSCalls != 0 ||
		beginCalls != 1 {
		t.Fatalf(
			"BIND identity RPCs cluster=%d begin=%d readiness=%d pdns=%d",
			clusterCalls, beginCalls, readinessCalls, powerDNSCalls,
		)
	}
	if len(requests) != 1 ||
		requests[0].Engine != transport.DNSEngineBIND ||
		requests[0].EngineEpoch != 1 ||
		requests[0].Domain != "bind-identity.example" {
		t.Fatalf("BIND V3 identity publication=%+v", requests)
	}
}

func TestDNSSetupPublicationFailureCanRetrySameMutation(t *testing.T) {
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "paired")
	seedReconcileZone(t, p, "biovision.health")
	agent := &strictDNSRPCAgent{failZone: "biovision.health"}
	attachStrictDNSRPCAgent(t, p, agent)
	body := `{"ns1":"ns3.example.net","ns2":"ns4.example.net","role":"paired","peer_ip":"198.51.100.20","peer_ns":"ns4.example.net"}`

	first := httptest.NewRecorder()
	p.handleDNSSetup(first, dnsSetupSystemAdminRequest(body))
	assertPublicationConflict(t, first)
	assertDNSSetupSettings(t, p, map[string]string{
		settingNS1:       "ns3.example.net",
		settingNS2:       "ns4.example.net",
		settingDNSRole:   "paired",
		settingDNSPeerIP: "198.51.100.20",
		settingDNSPeerNS: "ns4.example.net",
	})
	var savedAudits, publishedAudits int
	if err := p.db.GetDB().QueryRow(
		`SELECT COUNT(*) FROM audit_logs WHERE action LIKE 'settings.dns_setup_saved:%'`,
	).Scan(&savedAudits); err != nil {
		t.Fatal(err)
	}
	if err := p.db.GetDB().QueryRow(
		`SELECT COUNT(*) FROM audit_logs WHERE action LIKE 'settings.dns_setup_published:%'`,
	).Scan(&publishedAudits); err != nil {
		t.Fatal(err)
	}
	if savedAudits != 1 || publishedAudits != 0 {
		t.Fatalf("publication failure audits = saved:%d published:%d, want saved:1 published:0",
			savedAudits, publishedAudits)
	}

	agent.mu.Lock()
	agent.failZone = ""
	agent.mu.Unlock()
	second := httptest.NewRecorder()
	p.handleDNSSetup(second, dnsSetupSystemAdminRequest(body))
	if second.Code != http.StatusOK {
		t.Fatalf("retry status = %d, want 200; body=%s", second.Code, second.Body.String())
	}
	agent.mu.Lock()
	clusterCalls := agent.clusterCalls
	syncCalls := append([]string(nil), agent.syncCalls...)
	agent.mu.Unlock()
	if clusterCalls != 2 {
		t.Fatalf("same setup mutation reached the agent %d times, want 2", clusterCalls)
	}
	if !reflect.DeepEqual(syncCalls, []string{"biovision.health", "biovision.health"}) {
		t.Fatalf("zone publication calls = %v, want failed attempt plus retry", syncCalls)
	}
	if err := p.db.GetDB().QueryRow(
		`SELECT COUNT(*) FROM audit_logs WHERE action LIKE 'settings.dns_setup_saved:%'`,
	).Scan(&savedAudits); err != nil {
		t.Fatal(err)
	}
	if err := p.db.GetDB().QueryRow(
		`SELECT COUNT(*) FROM audit_logs WHERE action LIKE 'settings.dns_setup_published:%'`,
	).Scan(&publishedAudits); err != nil {
		t.Fatal(err)
	}
	if savedAudits != 2 || publishedAudits != 1 {
		t.Fatalf("retry audits = saved:%d published:%d, want saved:2 published:1",
			savedAudits, publishedAudits)
	}
}

func TestLegacyDNSSettingsPUTsRequireCompleteSetupWithoutMutation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		path    string
		body    string
		handler func(*Panel, http.ResponseWriter, *http.Request)
	}{
		{
			name:    "nameserver pair",
			path:    "/api/v1/settings/nameservers",
			body:    `{"ns1":"ns3.example.net","ns2":"ns4.example.net"}`,
			handler: (*Panel).handleNameserverSettings,
		},
		{
			name:    "cluster tuple",
			path:    "/api/v1/settings/dns-cluster",
			body:    `{"role":"standalone"}`,
			handler: (*Panel).handleDNSCluster,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
			p := newDNSPanelForTest(t)
			setDNSIdentityForTest(t, p, "paired")
			agent := &strictDNSRPCAgent{}
			attachStrictDNSRPCAgent(t, p, agent)

			recorder := httptest.NewRecorder()
			request := strictDNSAdminRequest(httptest.NewRequest(
				http.MethodPut, tc.path, strings.NewReader(tc.body),
			))
			tc.handler(p, recorder, request)
			assertDNSSetupRequired(t, recorder)

			assertDNSSetupSettings(t, p, map[string]string{
				settingNS1:       "ns1.celikhost.com",
				settingNS2:       "ns2.celikhost.com",
				settingDNSRole:   "paired",
				settingDNSPeerIP: "2.25.80.4",
				settingDNSPeerNS: "ns2.celikhost.com",
			})
			agent.mu.Lock()
			clusterCalls := agent.clusterCalls
			syncCalls := append([]string(nil), agent.syncCalls...)
			agent.mu.Unlock()
			if clusterCalls != 0 || len(syncCalls) != 0 {
				t.Fatalf("legacy PUT reached agent: cluster=%d sync=%v", clusterCalls, syncCalls)
			}
		})
	}
}

func TestLegacyDNSSettingsGETsRemainCompatible(t *testing.T) {
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "standalone")
	previousResolvers := nameserverResolvers
	nameserverResolvers = []hostResolver{fakeNameserverHostResolver{
		"ns1.celikhost.com": {"192.0.2.10"},
		"ns2.celikhost.com": {"192.0.2.10"},
	}}
	t.Cleanup(func() { nameserverResolvers = previousResolvers })

	namesRecorder := httptest.NewRecorder()
	p.handleNameserverSettings(namesRecorder, strictDNSAdminRequest(httptest.NewRequest(
		http.MethodGet, "/api/v1/settings/nameservers", nil,
	)))
	if namesRecorder.Code != http.StatusOK {
		t.Fatalf("nameserver GET status = %d, want 200; body=%s", namesRecorder.Code, namesRecorder.Body.String())
	}
	var names nameserverSettings
	if err := json.NewDecoder(namesRecorder.Body).Decode(&names); err != nil {
		t.Fatalf("decode nameserver GET: %v", err)
	}
	if names.NS1 != "ns1.celikhost.com" || names.NS2 != "ns2.celikhost.com" || names.Derived {
		t.Fatalf("nameserver GET changed contract: %+v", names)
	}

	clusterRecorder := httptest.NewRecorder()
	p.handleDNSCluster(clusterRecorder, strictDNSAdminRequest(httptest.NewRequest(
		http.MethodGet, "/api/v1/settings/dns-cluster", nil,
	)))
	if clusterRecorder.Code != http.StatusOK {
		t.Fatalf("cluster GET status = %d, want 200; body=%s", clusterRecorder.Code, clusterRecorder.Body.String())
	}
	var cluster dnsClusterView
	if err := json.NewDecoder(clusterRecorder.Body).Decode(&cluster); err != nil {
		t.Fatalf("decode cluster GET: %v", err)
	}
	if !cluster.Configured || cluster.Role != "standalone" ||
		cluster.NS1 != names.NS1 || cluster.NS2 != names.NS2 {
		t.Fatalf("cluster GET changed contract: %+v", cluster)
	}
}

func TestLegacyDNSTopologyPUTFailsClosedWhileAtomicSetupIsInFlight(t *testing.T) {
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
	p := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, p, "paired")
	seedReconcileZone(t, p, "biovision.health")
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	agent := &strictDNSRPCAgent{clusterEntered: entered, clusterRelease: release}
	attachStrictDNSRPCAgent(t, p, agent)

	firstDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		recorder := httptest.NewRecorder()
		p.handleDNSSetup(recorder, dnsSetupAdminRequest(
			`{"ns1":"ns1.celikhost.com","ns2":"ns2.celikhost.com","role":"paired","peer_ip":"2.25.80.4","peer_ns":"ns2.celikhost.com"}`,
		))
		firstDone <- recorder
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("first DNS topology mutation did not reach the blocked agent")
	}

	legacy := httptest.NewRecorder()
	p.handleNameserverSettings(legacy, strictDNSAdminRequest(httptest.NewRequest(
		http.MethodPut, "/api/v1/settings/nameservers",
		strings.NewReader(`{"ns1":"ns3.example.net","ns2":"ns4.example.net"}`),
	)))
	assertDNSSetupRequired(t, legacy)
	agent.mu.Lock()
	clusterCalls := agent.clusterCalls
	agent.mu.Unlock()
	if clusterCalls != 1 {
		t.Fatalf("legacy PUT reached the agent while setup was in flight: cluster calls=%d", clusterCalls)
	}
	close(release)

	select {
	case recorder := <-firstDone:
		if recorder.Code != http.StatusOK {
			t.Fatalf("setup status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("setup did not finish after releasing the topology lock")
	}
}
