package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A VPN agent that answers exactly the two shapes R-058 was found on: a host
// whose kernel modules are gone, and a host that has no VPN server at all.
// Everything else is the ordinary service-operation agent, so the durable
// mutation lifecycle around the answer is the real one.
//
// R-058'in uzerinde bulundugu iki bicimi yanitlayan bir VPN agent'i: cekirdek
// modulleri kaybolmus bir makine ve hic VPN sunucusu olmayan bir makine.
type vpnRefusalTestAgent struct {
	*serviceOperationTestAgent

	setupError           string
	setupRestartRequired bool
	syncError            string
	syncNotConfigured    bool
}

func (a *vpnRefusalTestAgent) SetupVPN(
	_ *ServiceOperationVPNRequest,
	response *ServiceOperationVPNResponse,
) error {
	response.Error = a.setupError
	response.HostRestartRequired = a.setupRestartRequired
	return nil
}

func (a *vpnRefusalTestAgent) SyncVPNPeersV2(
	_ *ServiceOperationPeerRequest,
	response *ServiceOperationPeerResponse,
) error {
	response.Error = a.syncError
	response.NotConfigured = a.syncNotConfigured
	return nil
}

func adminVPNRequest(t *testing.T, path string) *http.Request {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}"))
	return request.WithContext(
		context.WithValue(request.Context(), callerKey, &Caller{ID: 1, Role: roleAdmin}),
	)
}

func terminalVPNMutationJob(
	t *testing.T, agent *vpnRefusalTestAgent, kind string,
) *ServiceOperationMutationJob {
	t.Helper()
	agent.mu.Lock()
	defer agent.mu.Unlock()
	for _, job := range agent.mutationJobs {
		if job.Kind == kind {
			return job
		}
	}
	t.Fatalf("no durable %s job was recorded", kind)
	return nil
}

func decodeVPNRefusal(t *testing.T, recorder *httptest.ResponseRecorder) (string, string) {
	t.Helper()
	var body struct {
		Code  string `json:"code"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode refusal body %q: %v", recorder.Body.String(), err)
	}
	return body.Code, body.Error
}

// R-058. On a host whose running kernel's modules are gone, VPN setup answered
// with the sentence naming the restart while its durable job kept "The
// privileged host operation did not complete." - the framework's own words,
// true of every failed host mutation and therefore silent about this one. The
// reason the operator is given must be the reason the record keeps, and this
// asserts they are the same string.
//
// R-058. Cekirdek modulleri kaybolmus bir makinede VPN kurulumu, yeniden
// baslatmayi adlandiran cumleyle yanit veriyor; kalici isi ise hicbir seyi
// adlandirmayan cumleyi tutuyordu. Ikisinin ayni dize oldugu dogrulanir.
func TestVPNSetupRestartRefusalIsTheReasonTheLedgerKeeps(t *testing.T) {
	fixture, _, _, _ := newVPNSecurityFixture(t)
	agent := &vpnRefusalTestAgent{
		serviceOperationTestAgent: newServiceOperationTestAgent(),
		setupError: vpnEngineRebootTestSentence +
			" (wg-quick failed to start the VPN server)",
		setupRestartRequired: true,
	}
	attachVPNTestAgent(t, fixture.panel, agent)

	recorder := httptest.NewRecorder()
	fixture.panel.handleVPNSetup(recorder, adminVPNRequest(t, "/api/v1/vpn/setup"))

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusConflict, recorder.Body.String())
	}
	code, message := decodeVPNRefusal(t, recorder)
	if code != errCodeVPNHostRestartRequired {
		t.Fatalf("code = %q, want %q", code, errCodeVPNHostRestartRequired)
	}
	if message != vpnHostRestartRequiredMessage {
		t.Fatalf("message = %q, want the restart sentence", message)
	}

	job := terminalVPNMutationJob(t, agent, "vpn_setup")
	if job.Status != agentMutationFailed {
		t.Fatalf("job status = %q, want %q", job.Status, agentMutationFailed)
	}
	if job.ErrorMessage == "The privileged host operation did not complete." {
		t.Fatal("the ledger still keeps the sentence that names nothing")
	}
	if job.ErrorMessage != message {
		t.Fatalf(
			"the record keeps %q while the operator was given %q",
			job.ErrorMessage, message,
		)
	}
	// The agent's own words are never forwarded: the sentence is the panel's.
	// Agent'in kendi sozleri asla iletilmez; cumle panelindir.
	if strings.Contains(job.ErrorMessage, "wg-quick") {
		t.Fatalf("the record forwarded the agent's own text: %q", job.ErrorMessage)
	}
}

// R-056's rule is not widened by R-058: when the lease itself is what failed,
// the panel does not know whether the work ran, so the generic words stay.
//
// R-056'nin kurali genisletilmez: kiranin kendisi basarisiz olduysa panel isin
// calisip calismadigini bilmez ve genel cumle kalir.
func TestVPNRestartSentenceYieldsToAnUnprovenLease(t *testing.T) {
	named := namedVPNHostRestartFailure(errors.New("VPN setup: wg-quick failed"))

	if failure := standaloneAgentMutationFailure(named, nil); failure == nil ||
		failure.Message != vpnHostRestartRequiredMessage {
		t.Fatalf("a proven host failure did not carry the sentence: %+v", failure)
	}
	for name, heartbeatErr := range map[string]error{
		"lost lease":              errAgentMutationLeaseLost,
		"terminal lease failure":  errAgentMutationTerminalFailed,
		"unverifiable heartbeat":  errors.New("heartbeat transport failed"),
		"identity mismatch lease": errAgentMutationIdentityMismatch,
	} {
		failure := standaloneAgentMutationFailure(named, heartbeatErr)
		if failure == nil {
			t.Fatalf("%s produced no failure", name)
		}
		if failure.Message == vpnHostRestartRequiredMessage {
			t.Fatalf("%s claimed a reason the panel cannot prove", name)
		}
	}
}

// R-058, the other half. `POST /api/v1/vpn/sync` answered 500 INTERNAL with
// the agent's "VPN server is not set up" on a host where the VPN was never set
// up - the opaque shape R-054 named as a defect in its own right. It now says
// what is wrong and what to do first.
//
// R-058'in diger yarisi. VPN hic kurulmamis bir makinede `/vpn/sync` opak bir
// 500 donuyordu; artik neyin yanlis oldugunu ve once ne yapilacagini soyler.
func TestVPNSyncNamesAHostWithNoVPNServer(t *testing.T) {
	fixture, _, _, _ := newVPNSecurityFixture(t)
	agent := &vpnRefusalTestAgent{
		serviceOperationTestAgent: newServiceOperationTestAgent(),
		syncError:                 "VPN server is not set up",
		syncNotConfigured:         true,
	}
	attachVPNTestAgent(t, fixture.panel, agent)

	recorder := httptest.NewRecorder()
	fixture.panel.handleVPNSync(recorder, adminVPNRequest(t, "/api/v1/vpn/sync"))

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusConflict, recorder.Body.String())
	}
	code, message := decodeVPNRefusal(t, recorder)
	if code != errCodeVPNNotSetUp {
		t.Fatalf("code = %q, want %q", code, errCodeVPNNotSetUp)
	}
	if message != vpnNotSetUpMessage {
		t.Fatalf("message = %q, want the panel's own sentence", message)
	}
	if !strings.Contains(message, "Set the VPN up first") {
		t.Fatalf("the answer does not say what to do: %q", message)
	}
	// The host stays mutable: the job is terminal and its lease released.
	// Makine degistirilebilir kalir: is uctur ve kirasi birakilmistir.
	job := terminalVPNMutationJob(t, agent, "vpn_peer_sync")
	if job.Status != agentMutationFailed {
		t.Fatalf("job status = %q, want %q", job.Status, agentMutationFailed)
	}
}

// A failure with no name is still a failure with no name. This is an addition,
// not a reclassification of everything the VPN can answer.
//
// Adi olmayan bir ariza hala adi olmayan bir arizadir. Bu bir ekleme, VPN'in
// yanitlayabildigi her seyin yeniden siniflandirilmasi degil.
func TestVPNRefusalWriterOnlyAnswersTheRefusalsItCanName(t *testing.T) {
	if writeVPNRefusal(httptest.NewRecorder(), errors.New("peer sync: backend exploded")) {
		t.Fatal("an unnamed failure was answered as a named refusal")
	}
	for name, err := range map[string]error{
		"restart required": errVPNHostRestartRequired,
		"not set up":       errVPNNotSetUp,
	} {
		recorder := httptest.NewRecorder()
		if !writeVPNRefusal(recorder, err) {
			t.Fatalf("%s was not answered", name)
		}
		if recorder.Code != http.StatusConflict {
			t.Fatalf("%s answered %d, want %d", name, recorder.Code, http.StatusConflict)
		}
	}
}

// The sentence the agent would produce for the restart, kept here rather than
// reached for across the module boundary: this test is about what the panel
// does with an agent that says so, not about the agent's own wording.
//
// Agent'in yeniden baslatma icin uretecegi cumle; bu test, boyle diyen bir
// agent karsisinda panelin ne yaptigina dairdir.
const vpnEngineRebootTestSentence = "This server is running a kernel whose " +
	"modules are no longer on disk, so WireGuard cannot be loaded."
