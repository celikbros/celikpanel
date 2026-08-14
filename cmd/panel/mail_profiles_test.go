package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/rpc"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/transport"
)

type mailProfileMutationCall struct {
	Name      string
	ServiceID string
	Binding   transport.ServiceMutationBinding
}

type mailProfileTestAgent struct {
	*serviceOperationTestAgent

	profileMu sync.Mutex
	calls     []mailProfileMutationCall

	agentCommit         string
	versionCapabilities *[]string
	tlsError            string
	tlsCountOffset      int
	submissionError     string
	failScanAt          int
	scanCalls           int
	phpInactive         bool
	firewallEnabled     bool
	firewallError       string
	firewallCalls       int
}

func (a *mailProfileTestAgent) record(name, serviceID string, binding transport.ServiceMutationBinding) {
	a.profileMu.Lock()
	defer a.profileMu.Unlock()
	a.calls = append(a.calls, mailProfileMutationCall{Name: name, ServiceID: serviceID, Binding: binding})
}

func (a *mailProfileTestAgent) callsSnapshot() []mailProfileMutationCall {
	a.profileMu.Lock()
	defer a.profileMu.Unlock()
	return append([]mailProfileMutationCall(nil), a.calls...)
}

func (a *mailProfileTestAgent) Version(_ *transport.Empty, response *transport.AgentVersionResponse) error {
	response.Commit = a.agentCommit
	capabilities := []string{
		transport.AgentCapabilityFirewallApplyV2,
		transport.AgentCapabilityDNSZoneSyncV2,
		transport.AgentCapabilityMailTLSSyncV2,
		transport.AgentCapabilityPanelCertificateIssueV2,
	}
	if a.versionCapabilities != nil {
		capabilities = append([]string(nil), (*a.versionCapabilities)...)
	}
	response.Capabilities = capabilities
	return nil
}

func (a *mailProfileTestAgent) InstallService(
	request *transport.InstallServiceRequest,
	response *transport.InstallServiceResponse,
) error {
	a.record("install", request.ID, request.ServiceMutationBinding)
	var inner ServiceOperationInstallResponse
	err := a.serviceOperationTestAgent.InstallService(&ServiceOperationInstallRequest{
		MutationRequestID: request.MutationRequestID,
		MutationOwnerID:   request.MutationOwnerID,
		ID:                request.ID,
		Package:           request.Package,
	}, &inner)
	response.Installed, response.Detail, response.Unit, response.Error =
		inner.Installed, inner.Detail, inner.Unit, inner.Error
	if err == nil && inner.Error == "" {
		a.serviceOperationTestAgent.mu.Lock()
		a.active[request.ID] = true
		a.serviceOperationTestAgent.mu.Unlock()
	}
	return err
}

func (a *mailProfileTestAgent) InstallRoundcube(
	request *transport.WebmailMutationRequest,
	response *transport.InstallRoundcubeResponse,
) error {
	a.record("install", "roundcube", request.ServiceMutationBinding)
	var inner ServiceOperationRoundcubeResponse
	if err := a.serviceOperationTestAgent.InstallRoundcube(&struct{}{}, &inner); err != nil {
		return err
	}
	response.Installed, response.Version, response.Error = inner.Installed, inner.Version, inner.Error
	return nil
}

func (a *mailProfileTestAgent) ConfigureWebmail(
	request *transport.WebmailMutationRequest,
	response *transport.ConfigureWebmailResponse,
) error {
	a.record("webmail", "roundcube", request.ServiceMutationBinding)
	var inner ServiceOperationWebmailResponse
	if err := a.serviceOperationTestAgent.ConfigureWebmail(&struct{}{}, &inner); err != nil {
		return err
	}
	response.Configured, response.Present, response.Error = inner.Configured, inner.Present, inner.Error
	return nil
}

func (a *mailProfileTestAgent) GetServices(_ *transport.Empty, response *[]core.Service) error {
	a.profileMu.Lock()
	a.scanCalls++
	call := a.scanCalls
	failAt := a.failScanAt
	a.profileMu.Unlock()
	if failAt > 0 && call == failAt {
		return errors.New("injected managed-service scan failure")
	}
	return a.serviceOperationTestAgent.GetServices(&transport.Empty{}, response)
}

func (a *mailProfileTestAgent) ListServiceInstances(
	request *transport.ServiceInstancesRequest,
	response *transport.ServiceInstancesResponse,
) error {
	if request.ID != "php-fpm" {
		var inner ServiceOperationInstancesResponse
		if err := a.serviceOperationTestAgent.ListServiceInstances(
			&ServiceOperationInstancesRequest{ID: request.ID}, &inner,
		); err != nil {
			return err
		}
		response.Instances, response.Error = inner.Instances, inner.Error
		return nil
	}
	a.serviceOperationTestAgent.mu.Lock()
	installed := a.installed["php-fpm"]
	a.serviceOperationTestAgent.mu.Unlock()
	if installed {
		status := "active (running)"
		if a.phpInactive {
			status = "inactive (dead)"
		}
		response.Instances = []core.ServiceInstance{{
			Version: "test", Unit: "php-fpm", Managed: true, Status: status,
		}}
	}
	return nil
}

func (a *mailProfileTestAgent) ConfigureMailStack(
	request *transport.ServiceMutationRequest,
	response *transport.ConfigureMailStackResponse,
) error {
	a.record("mail-stack", "", request.ServiceMutationBinding)
	response.Configured = true
	return nil
}

func (a *mailProfileTestAgent) ResetFailedUnitMutation(
	request *transport.ServiceMutationServiceRequest,
	response *bool,
) error {
	a.record("reset", request.ServiceName, request.ServiceMutationBinding)
	*response = true
	return nil
}

func (a *mailProfileTestAgent) StartServiceMutation(
	request *transport.ServiceMutationServiceRequest,
	response *bool,
) error {
	a.record("start", request.ServiceName, request.ServiceMutationBinding)
	a.serviceOperationTestAgent.mu.Lock()
	a.active[request.ServiceName] = true
	a.serviceOperationTestAgent.mu.Unlock()
	*response = true
	return nil
}

func (a *mailProfileTestAgent) EnsureNginxReady(
	request *transport.ServiceMutationRequest,
	response *transport.EnsureNginxReadyResponse,
) error {
	a.record("nginx-ready", "nginx", request.ServiceMutationBinding)
	response.Ready = true
	return nil
}

func (a *mailProfileTestAgent) WireMailFilters(
	request *transport.ServiceMutationRequest,
	response *transport.WireMailFiltersResponse,
) error {
	a.record("mail-filters", "rspamd", request.ServiceMutationBinding)
	response.Wired = true
	return nil
}

func (a *mailProfileTestAgent) SyncMailTLSV2(
	request *transport.SyncMailTLSV2Request,
	response *transport.SecureMailTLSResponse,
) error {
	a.record("mail-tls", "", request.ServiceMutationBinding)
	a.serviceOperationTestAgent.mu.Lock()
	a.serviceOperationTestAgent.mutationEvents = append(
		a.serviceOperationTestAgent.mutationEvents,
		"call:mail_tls_sync",
	)
	a.serviceOperationTestAgent.mu.Unlock()
	if a.tlsError != "" {
		response.Error = a.tlsError
		return nil
	}
	response.Configured = true
	response.SNICount = len(request.SNI) + a.tlsCountOffset
	response.DefaultCert = transport.DefaultMailTLSCertificatePath
	return nil
}

func (a *mailProfileTestAgent) ConfigureMailSubmission(
	request *transport.ServiceMutationRequest,
	response *transport.ConfigureMailSubmissionResponse,
) error {
	a.record("submission", "postfix", request.ServiceMutationBinding)
	if a.submissionError != "" {
		response.Error = a.submissionError
		return nil
	}
	response.Configured = true
	return nil
}

func (a *mailProfileTestAgent) FirewallStatus(_ *transport.Empty, response *FirewallStatusResp) error {
	a.profileMu.Lock()
	a.firewallCalls++
	a.profileMu.Unlock()
	response.Enabled = a.firewallEnabled
	return nil
}

func (a *mailProfileTestAgent) InstalledServiceIDsStrict(_ *transport.Empty, response *[]string) error {
	return a.serviceOperationTestAgent.InstalledServiceIDs(&transport.Empty{}, response)
}

func (a *mailProfileTestAgent) ApplyFirewallV2(
	request *transport.ApplyFirewallRequest,
	response *transport.FirewallStatusResponse,
) error {
	a.record("firewall", "", request.ServiceMutationBinding)
	a.serviceOperationTestAgent.mu.Lock()
	a.serviceOperationTestAgent.firewallCalls++
	a.serviceOperationTestAgent.firewallRequests = append(
		a.serviceOperationTestAgent.firewallRequests,
		transport.ApplyFirewallRequest{
			ServiceMutationBinding: request.ServiceMutationBinding,
			Enabled:                request.Enabled,
			Persist:                request.Persist,
			TCPPorts:               append([]int(nil), request.TCPPorts...),
			UDPPorts:               append([]int(nil), request.UDPPorts...),
		},
	)
	a.serviceOperationTestAgent.mutationEvents = append(
		a.serviceOperationTestAgent.mutationEvents,
		"call:firewall_sync",
	)
	a.serviceOperationTestAgent.mu.Unlock()
	if a.firewallError != "" {
		response.Error = a.firewallError
		return nil
	}
	response.Enabled = true
	return nil
}

func attachMailProfileTestAgent(t *testing.T, panel *Panel, agent *mailProfileTestAgent) {
	t.Helper()
	server := rpc.NewServer()
	if err := server.RegisterName("Agent", agent); err != nil {
		t.Fatalf("register mail profile agent: %v", err)
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
		t.Fatal(err)
	}
	panel.agentClient = transport.NewReconnectingClientWithContextConnector(client, connector)
	t.Cleanup(func() { _ = client.Close() })
}

func newMailProfileTestFixture(t *testing.T) (serviceOperationTestFixture, *mailProfileTestAgent) {
	t.Helper()
	fixture := newServiceOperationTestFixture(t)
	agent := &mailProfileTestAgent{
		serviceOperationTestAgent: fixture.agent,
		agentCommit:               buildCommit,
	}
	attachMailProfileTestAgent(t, fixture.panel, agent)
	fixture.panel.pkgFamilyVal = "apt"
	fixture.panel.webmailReadinessProbe = func(context.Context) bool { return true }
	previousHostname := readMailProfileHostname
	readMailProfileHostname = func() (string, error) { return "mail.profile.test", nil }
	previousTLSHostname := readMailTLSHostname
	readMailTLSHostname = func() (string, error) { return "MAIL.PROFILE.TEST.", nil }
	t.Cleanup(func() {
		readMailProfileHostname = previousHostname
		readMailTLSHostname = previousTLSHostname
	})
	return fixture, agent
}

func postMailProfile(
	t *testing.T,
	fixture serviceOperationTestFixture,
	profileID, requestID string,
) (*httptest.ResponseRecorder, *serviceOperation) {
	t.Helper()
	body, err := json.Marshal(mailProfileInstallRequest{
		ProfileID: profileID,
		RequestID: requestID,
		Confirmed: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	fixture.panel.handleMailProfileInstall(recorder, serviceOperationAdminRequest(
		t, http.MethodPost, mailProfileInstallPath, string(body), fixture.userID,
	))
	return recorder, decodeServiceOperationEnvelope(t, recorder)
}

func runMailProfileForTest(
	t *testing.T,
	fixture serviceOperationTestFixture,
	profileID string,
) (serviceOperationResult, *serviceOperationFailure) {
	t.Helper()
	return fixture.panel.runMailProfileInstall(
		serviceOperationBoundContext(), profileID, func(string) error { return nil },
	)
}

func TestMailProfileCatalogueIsClosedAndOrdered(t *testing.T) {
	want := map[string][]string{
		"core-mail":      {"postfix", "dovecot"},
		"webmail":        {"postfix", "dovecot", "nginx", "php-fpm", "roundcube"},
		"protected-mail": {"postfix", "dovecot", "rspamd"},
	}
	if len(mailProfileDefinitions) != len(want) {
		t.Fatalf("profile count = %d, want %d", len(mailProfileDefinitions), len(want))
	}
	for id, services := range want {
		profile, ok := mailProfileByID(id)
		if !ok || !reflect.DeepEqual(profile.Services, services) {
			t.Fatalf("profile %s = %+v, want services %v", id, profile, services)
		}
	}
	for _, forbidden := range []string{"clamav", "sogo", "exchange"} {
		if _, ok := mailProfileByID(forbidden); ok {
			t.Fatalf("forbidden profile %q is exposed", forbidden)
		}
	}
}

func TestMailProfileInstallRouteIsStrictAndAdminOnly(t *testing.T) {
	fixture, agent := newMailProfileTestFixture(t)
	requestID := mustServiceOperationRequestID(t)

	method := httptest.NewRecorder()
	fixture.panel.handleMailProfileInstall(method, serviceOperationAdminRequest(
		t, http.MethodGet, mailProfileInstallPath, "", fixture.userID,
	))
	if method.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET status = %d", method.Code)
	}

	unauthorized := httptest.NewRecorder()
	fixture.panel.handleMailProfileInstall(unauthorized, httptest.NewRequest(
		http.MethodPost, mailProfileInstallPath,
		strings.NewReader(`{"profile_id":"core-mail","request_id":"`+requestID+`"}`),
	))
	if unauthorized.Code != http.StatusForbidden {
		t.Fatalf("unauthorized status = %d body=%s", unauthorized.Code, unauthorized.Body.String())
	}

	for name, body := range map[string]string{
		"unknown field":   `{"profile_id":"core-mail","request_id":"` + requestID + `","services":["postfix"]}`,
		"unknown profile": `{"profile_id":"exchange","request_id":"` + requestID + `"}`,
		"invalid request": `{"profile_id":"core-mail","request_id":"bad"}`,
	} {
		t.Run(name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			fixture.panel.handleMailProfileInstall(recorder, serviceOperationAdminRequest(
				t, http.MethodPost, mailProfileInstallPath, body, fixture.userID,
			))
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
	unconfirmed := httptest.NewRecorder()
	fixture.panel.handleMailProfileInstall(unconfirmed, serviceOperationAdminRequest(
		t, http.MethodPost, mailProfileInstallPath,
		`{"profile_id":"core-mail","request_id":"`+requestID+`"}`, fixture.userID,
	))
	if unconfirmed.Code != http.StatusBadRequest ||
		!strings.Contains(unconfirmed.Body.String(), errCodeMailProfileConfirmationRequired) {
		t.Fatalf("unconfirmed status=%d body=%s", unconfirmed.Code, unconfirmed.Body.String())
	}
	if !isAdminOnlyPath(mailProfileInstallPath) {
		t.Fatal("mail profile route is not covered by the admin middleware")
	}
	if agent.installCalls.Load() != 0 {
		t.Fatal("invalid request reached InstallService")
	}
}

func TestMailProfileRequestReplayAndConflict(t *testing.T) {
	fixture, agent := newMailProfileTestFixture(t)
	requestID := mustServiceOperationRequestID(t)
	firstRecorder, first := postMailProfile(t, fixture, "core-mail", requestID)
	if firstRecorder.Code != http.StatusAccepted || first == nil {
		t.Fatalf("first status=%d operation=%+v body=%s", firstRecorder.Code, first, firstRecorder.Body.String())
	}
	waitForServiceOperation(t, fixture.panel, fixture.userID, first.ID, serviceOperationSucceeded)
	before := agent.installCalls.Load()

	replayRecorder, replay := postMailProfile(t, fixture, "core-mail", requestID)
	if replayRecorder.Code != http.StatusAccepted || replay == nil || replay.ID != first.ID {
		t.Fatalf("replay status=%d operation=%+v", replayRecorder.Code, replay)
	}
	if agent.installCalls.Load() != before {
		t.Fatal("request replay started a second install")
	}

	conflictRecorder, _ := postMailProfile(t, fixture, "protected-mail", requestID)
	if conflictRecorder.Code != http.StatusConflict || !strings.Contains(conflictRecorder.Body.String(), errCodeServiceOperationRequestConflict) {
		t.Fatalf("conflict status=%d body=%s", conflictRecorder.Code, conflictRecorder.Body.String())
	}
}

func TestMailProfileWholePlanPreflightBlocksBeforeMutation(t *testing.T) {
	tests := []struct {
		name    string
		profile mailProfileDefinition
		setup   func(*testing.T, serviceOperationTestFixture, *mailProfileTestAgent)
	}{
		{
			name:    "invalid FQDN",
			profile: mailProfileDefinition{ID: "core-mail", Services: []string{"postfix", "dovecot"}},
			setup: func(t *testing.T, _ serviceOperationTestFixture, _ *mailProfileTestAgent) {
				readMailProfileHostname = func() (string, error) { return "localhost", nil }
			},
		},
		{
			name:    "occupied SMTP seat",
			profile: mailProfileDefinition{ID: "core-mail", Services: []string{"postfix", "dovecot"}},
			setup: func(_ *testing.T, _ serviceOperationTestFixture, agent *mailProfileTestAgent) {
				seedInstalledServices(agent.serviceOperationTestAgent, "exim")
			},
		},
		{
			name:    "projected requirement missing",
			profile: mailProfileDefinition{ID: "invalid", Services: []string{"rspamd"}},
		},
		{
			name:    "unsupported package family",
			profile: mailProfileDefinition{ID: "core-mail", Services: []string{"postfix", "dovecot"}},
			setup: func(_ *testing.T, fixture serviceOperationTestFixture, _ *mailProfileTestAgent) {
				fixture.panel.pkgFamilyVal = "dnf"
			},
		},
		{
			name:    "fresh scan failure",
			profile: mailProfileDefinition{ID: "core-mail", Services: []string{"postfix", "dovecot"}},
			setup: func(_ *testing.T, _ serviceOperationTestFixture, agent *mailProfileTestAgent) {
				agent.failScanAt = 1
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture, agent := newMailProfileTestFixture(t)
			if test.setup != nil {
				test.setup(t, fixture, agent)
			}
			if _, err := fixture.panel.preflightMailProfileInstall(serviceOperationBoundContext(), test.profile); err == nil {
				t.Fatal("unsafe whole-plan preflight passed")
			}
			if agent.installCalls.Load() != 0 {
				t.Fatal("preflight failure reached InstallService")
			}
		})
	}
}

func TestMailProfileInvalidServerHostnameIsActionableAndSanitized(t *testing.T) {
	fixture, agent := newMailProfileTestFixture(t)
	const rejectedHostname = "private-boston-host"
	readMailProfileHostname = func() (string, error) { return rejectedHostname, nil }

	profile, _ := mailProfileByID(core.MailProfileCore)
	if _, err := fixture.panel.preflightMailProfileInstall(
		serviceOperationBoundContext(), profile,
	); !errors.Is(err, errMailProfileServerHostnameInvalid) {
		t.Fatalf("preflight error = %v, want invalid hostname category", err)
	}

	recorder, queued := postMailProfile(
		t, fixture, core.MailProfileCore, mustServiceOperationRequestID(t),
	)
	if recorder.Code != http.StatusAccepted || queued == nil {
		t.Fatalf("profile status=%d operation=%+v body=%s", recorder.Code, queued, recorder.Body.String())
	}
	failed, body := waitForServiceOperation(
		t, fixture.panel, fixture.userID, queued.ID, serviceOperationFailed,
	)
	if failed.Error == nil ||
		failed.Error.Code != errCodeMailProfileServerHostnameInvalid ||
		failed.Error.Message != mailProfileServerHostnameMessage {
		t.Fatalf("failed profile=%+v", failed)
	}
	for _, leaked := range []string{
		rejectedHostname,
		"invalid hostname",
		"mail profile server hostname is not a canonical FQDN",
	} {
		if strings.Contains(body, leaked) {
			t.Fatalf("server-only hostname detail %q leaked in response: %s", leaked, body)
		}
	}
	if agent.installCalls.Load() != 0 {
		t.Fatal("invalid hostname reached InstallService")
	}
}

func TestMailProfileBuildMismatchBlocksBeforeMutation(t *testing.T) {
	withPanelBuildCommit(t, "profile-panel-build")
	fixture, agent := newMailProfileTestFixture(t)
	agent.agentCommit = "different-agent-build"
	profile, _ := mailProfileByID("core-mail")
	if _, err := fixture.panel.preflightMailProfileInstall(serviceOperationBoundContext(), profile); err == nil {
		t.Fatal("build mismatch passed profile preflight")
	}
	if agent.installCalls.Load() != 0 {
		t.Fatal("build mismatch reached InstallService")
	}
}

func TestMailProfileAdmissionRejectsUnavailableMailTLSV2BeforeRowOrHostMutation(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*serviceOperationTestFixture, *mailProfileTestAgent)
	}{
		{
			name: "agent capability missing",
			setup: func(_ *serviceOperationTestFixture, agent *mailProfileTestAgent) {
				capabilities := []string{
					transport.AgentCapabilityFirewallApplyV2,
					transport.AgentCapabilityDNSZoneSyncV2,
					transport.AgentCapabilityPanelCertificateIssueV2,
				}
				agent.versionCapabilities = &capabilities
			},
		},
		{
			name: "RHEL preview remains closed",
			setup: func(fixture *serviceOperationTestFixture, _ *mailProfileTestAgent) {
				fixture.panel.pkgFamilyVal = "dnf"
				fixture.panel.hostPlatformKnown = true
				fixture.panel.hostPlatformVal = rhelPolicyTestIdentity()
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture, agent := newMailProfileTestFixture(t)
			test.setup(&fixture, agent)
			recorder, queued := postMailProfile(
				t, fixture, "core-mail", mustServiceOperationRequestID(t),
			)
			if recorder.Code == http.StatusAccepted || queued != nil {
				t.Fatalf("unsafe admission status=%d operation=%+v body=%s", recorder.Code, queued, recorder.Body.String())
			}
			var rows int
			if err := fixture.database.GetDB().QueryRow(
				`SELECT COUNT(*) FROM service_operations`,
			).Scan(&rows); err != nil {
				t.Fatal(err)
			}
			agent.serviceOperationTestAgent.mu.Lock()
			jobs := len(agent.mutationJobs)
			agent.serviceOperationTestAgent.mu.Unlock()
			if rows != 0 || jobs != 0 || agent.installCalls.Load() != 0 || len(agent.callsSnapshot()) != 0 {
				t.Fatalf("rejected admission touched state: rows=%d jobs=%d installs=%d calls=%v",
					rows, jobs, agent.installCalls.Load(), agent.callsSnapshot())
			}
		})
	}
}

func TestMailProfileRunnerUsesExactOrderOneBindingWithoutNestedChildren(t *testing.T) {
	for _, profileID := range []string{"core-mail", "webmail", "protected-mail"} {
		t.Run(profileID, func(t *testing.T) {
			fixture, agent := newMailProfileTestFixture(t)
			agent.firewallEnabled = true
			result, failure := runMailProfileForTest(t, fixture, profileID)
			if failure != nil || result["success"] != true {
				t.Fatalf("result=%v failure=%+v", result, failure)
			}
			profile, _ := mailProfileByID(profileID)
			var installed []string
			calls := agent.callsSnapshot()
			for _, call := range calls {
				if call.Name == "install" {
					installed = append(installed, call.ServiceID)
				}
				if call.Binding.MutationRequestID != "00112233445566778899aabbccddeeff" ||
					call.Binding.MutationOwnerID != "ffeeddccbbaa99887766554433221100" {
					t.Fatalf("call %+v escaped the profile binding", call)
				}
			}
			if !reflect.DeepEqual(installed, profile.Services) {
				t.Fatalf("install order = %v, want %v", installed, profile.Services)
			}
			agent.profileMu.Lock()
			firewallCalls := agent.firewallCalls
			agent.profileMu.Unlock()
			if firewallCalls != 0 {
				t.Fatalf("nested firewall status calls = %d, want 0", firewallCalls)
			}
			last := map[string]int{"mail-stack": -1, "submission": -1}
			for i, call := range calls {
				if call.Name == "firewall" || call.Name == "mail-tls" {
					t.Fatalf("mail profile runner reused outer binding for a direct child: %+v", call)
				}
				if _, ok := last[call.Name]; ok {
					last[call.Name] = i
				}
			}
			if last["mail-stack"] < 0 || last["mail-stack"] >= last["submission"] {
				t.Fatalf("final order is wrong: %v calls=%v", last, calls)
			}
		})
	}
}

func TestMailProfileFirewallFailureOccursInFreshPostTerminalChild(t *testing.T) {
	fixture, agent := newMailProfileTestFixture(t)
	agent.firewallEnabled = true
	agent.firewallError = "firewall failed"
	recorder, queued := postMailProfile(
		t, fixture, "core-mail", mustServiceOperationRequestID(t),
	)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("profile status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	failed, _ := waitForServiceOperation(
		t, fixture.panel, fixture.userID, queued.ID, serviceOperationFailed,
	)
	if failed.Phase != mailProfilePhase("core-mail", "firewall") ||
		failed.Error == nil || failed.Error.Code != "firewall_sync_failed" {
		t.Fatalf("failed profile=%+v", failed)
	}
	events := fixture.agent.capturedMutationEvents()
	outerFinish := mutationEventIndex(events, "finish:mail_profile_install:succeeded")
	childBegin := mutationEventIndex(events, "begin:firewall_sync")
	childCall := mutationEventIndex(events, "call:firewall_sync")
	childFinish := mutationEventIndex(events, "finish:firewall_sync:failed")
	if outerFinish < 0 || childBegin <= outerFinish || childCall <= childBegin ||
		childFinish <= childCall {
		t.Fatalf("mutation events=%v", events)
	}
	calls := agent.callsSnapshot()
	var firewallCall *mailProfileMutationCall
	for index := range calls {
		if calls[index].Name == "firewall" {
			firewallCall = &calls[index]
		}
	}
	if firewallCall == nil ||
		!validServiceOperationID(firewallCall.Binding.MutationRequestID) ||
		!validServiceOperationID(firewallCall.Binding.MutationOwnerID) ||
		firewallCall.Binding.MutationRequestID == queued.RequestID {
		t.Fatalf("fresh firewall child binding=%+v outer=%s", firewallCall, queued.RequestID)
	}
	agent.serviceOperationTestAgent.mu.Lock()
	legacyCalls := agent.serviceOperationTestAgent.legacyFirewallCalls
	agent.serviceOperationTestAgent.mu.Unlock()
	if legacyCalls != 0 {
		t.Fatalf("legacy firewall calls=%d", legacyCalls)
	}
}

func TestMailProfileTLSFailureOccursInFreshPostTerminalChild(t *testing.T) {
	for _, setup := range []func(*mailProfileTestAgent){
		func(agent *mailProfileTestAgent) { agent.tlsError = "TLS failed" },
		func(agent *mailProfileTestAgent) { agent.tlsCountOffset = 1 },
	} {
		fixture, agent := newMailProfileTestFixture(t)
		setup(agent)
		recorder, queued := postMailProfile(
			t, fixture, "core-mail", mustServiceOperationRequestID(t),
		)
		if recorder.Code != http.StatusAccepted {
			t.Fatalf("profile status=%d body=%s", recorder.Code, recorder.Body.String())
		}
		failed, _ := waitForServiceOperation(
			t, fixture.panel, fixture.userID, queued.ID, serviceOperationFailed,
		)
		if failed.Phase != mailProfilePhase("core-mail", "mail-tls") || failed.Error == nil {
			t.Fatalf("failed profile=%+v", failed)
		}
		events := fixture.agent.capturedMutationEvents()
		outerFinish := mutationEventIndex(events, "finish:mail_profile_install:succeeded")
		childBegin := mutationEventIndex(events, "begin:mail_tls_sync")
		childCall := mutationEventIndex(events, "call:mail_tls_sync")
		childFinish := mutationEventIndex(events, "finish:mail_tls_sync:failed")
		if outerFinish < 0 || childBegin <= outerFinish || childCall <= childBegin ||
			childFinish <= childCall {
			t.Fatalf("mutation events=%v", events)
		}
	}
}

func TestMailProfileFallbackWarningAndFinalGates(t *testing.T) {
	t.Run("zero SNI is explicit fallback success", func(t *testing.T) {
		fixture, _ := newMailProfileTestFixture(t)
		recorder, queued := postMailProfile(
			t, fixture, "core-mail", mustServiceOperationRequestID(t),
		)
		if recorder.Code != http.StatusAccepted {
			t.Fatalf("profile status=%d body=%s", recorder.Code, recorder.Body.String())
		}
		completed, _ := waitForServiceOperation(
			t, fixture.panel, fixture.userID, queued.ID, serviceOperationSucceeded,
		)
		var result struct {
			MailTLS  mailProfileTLSResult `json:"mail_tls"`
			Warnings []string             `json:"warnings"`
		}
		if err := json.Unmarshal(completed.Result, &result); err != nil {
			t.Fatal(err)
		}
		tlsResult := result.MailTLS
		if !tlsResult.Configured || tlsResult.SNICount != 0 || !tlsResult.FallbackOnly {
			t.Fatalf("mail_tls = %+v", tlsResult)
		}
		warnings := result.Warnings
		if len(warnings) != 1 || warnings[0] != mailProfileFallbackWarning {
			t.Fatalf("warnings = %v", warnings)
		}
	})

	t.Run("final scan failure", func(t *testing.T) {
		fixture, agent := newMailProfileTestFixture(t)
		agent.failScanAt = 6
		result, failure := runMailProfileForTest(t, fixture, "core-mail")
		if failure == nil || result["success"] != false {
			t.Fatalf("result=%v failure=%+v", result, failure)
		}
	})

	t.Run("runner defers firewall failure to direct child", func(t *testing.T) {
		fixture, agent := newMailProfileTestFixture(t)
		agent.firewallEnabled = true
		agent.firewallError = "firewall failed"
		result, failure := runMailProfileForTest(t, fixture, "core-mail")
		if failure != nil || result["success"] != true {
			t.Fatalf("result=%v failure=%+v", result, failure)
		}
		if calls := agent.callsSnapshot(); len(calls) == 0 || calls[len(calls)-1].Name == "firewall" {
			t.Fatalf("runner unexpectedly called nested firewall: %v", calls)
		}
	})

	t.Run("inactive PHP runtime", func(t *testing.T) {
		fixture, agent := newMailProfileTestFixture(t)
		agent.phpInactive = true
		result, failure := runMailProfileForTest(t, fixture, "webmail")
		if failure == nil || result["success"] != false {
			t.Fatalf("result=%v failure=%+v", result, failure)
		}
	})

	t.Run("dead webmail socket", func(t *testing.T) {
		fixture, _ := newMailProfileTestFixture(t)
		fixture.panel.webmailReadinessProbe = func(context.Context) bool { return false }
		result, failure := runMailProfileForTest(t, fixture, "webmail")
		if failure == nil || result["success"] != false {
			t.Fatalf("result=%v failure=%+v", result, failure)
		}
	})
}

func TestMailProfileUsesSingleFinalWebmailReadinessProof(t *testing.T) {
	fixture, _ := newMailProfileTestFixture(t)
	probeCalls := 0
	fixture.panel.webmailReadinessProbe = func(context.Context) bool {
		probeCalls++
		// The Roundcube component scan observes the first value. The final
		// profile scan must atomically replace it, and every terminal consumer
		// must reuse that stored proof instead of probing a third time.
		return probeCalls == 2
	}

	result, failure := runMailProfileForTest(t, fixture, "webmail")
	if failure != nil || result["success"] != true {
		t.Fatalf("result=%v failure=%+v", result, failure)
	}
	if probeCalls != 2 {
		t.Fatalf("webmail readiness probe calls = %d, want exactly component and final scans", probeCalls)
	}

	ready, proven, err := fixture.panel.cachedWebmailReadinessProof(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !proven || !ready {
		t.Fatalf("stored final webmail proof = ready:%v proven:%v", ready, proven)
	}

	recorder := httptest.NewRecorder()
	fixture.panel.handleManagedServices(
		recorder,
		httptest.NewRequest(http.MethodGet, "/api/v1/managed-services", nil),
	)
	var payload managedServicesPayload
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if profile := profileViewByID(t, payload.Profiles, "webmail"); profile.Status != mailProfileStatusComplete {
		t.Fatalf("cached webmail profile = %+v", profile)
	}
	if probeCalls != 2 {
		t.Fatalf("managed view performed a live readiness probe; calls = %d", probeCalls)
	}
}

func profileViewByID(t *testing.T, profiles []MailProfileResponse, id string) MailProfileResponse {
	t.Helper()
	for _, profile := range profiles {
		if profile.ID == id {
			return profile
		}
	}
	t.Fatalf("profile %s is missing", id)
	return MailProfileResponse{}
}

func TestMailProfileManagedViewStatusesFailClosed(t *testing.T) {
	if profile := profileViewByID(t, mailProfilesView(nil, false, "apt", "", false, false), "core-mail"); profile.Status != mailProfileStatusUnknown || profile.Available {
		t.Fatalf("unknown profile = %+v", profile)
	}

	availableServices := catalogView(nil, "apt")
	if profile := profileViewByID(t, mailProfilesView(availableServices, true, "apt", "", false, true), "core-mail"); profile.Status != mailProfileStatusAvailable || !profile.Available {
		t.Fatalf("available profile = %+v", profile)
	}
	if profile := profileViewByID(t, mailProfilesView(
		availableServices, true, "apt", mailProfileServerHostnameMessage, false, true,
	), "core-mail"); profile.Status != mailProfileStatusBlocked ||
		profile.Available || profile.BlockedReason != mailProfileServerHostnameMessage {
		t.Fatalf("hostname-blocked profile = %+v", profile)
	}

	partial := catalogView([]serviceObservation{{ID: "postfix", IsInstalled: true, Status: "active (running)"}}, "apt")
	if profile := profileViewByID(t, mailProfilesView(partial, true, "apt", "", false, true), "core-mail"); profile.Status != mailProfileStatusPartial || !profile.Available {
		t.Fatalf("partial profile = %+v", profile)
	}

	complete := catalogView([]serviceObservation{
		{ID: "postfix", IsInstalled: true, Status: "active (running)"},
		{ID: "dovecot", IsInstalled: true, Status: "active (running)"},
	}, "apt")
	if profile := profileViewByID(t, mailProfilesView(complete, true, "apt", "", false, true), "core-mail"); profile.Status != mailProfileStatusComplete || !profile.Available || profile.Warning != mailProfileReconciliationWarning {
		t.Fatalf("complete profile = %+v", profile)
	}

	blocked := catalogView([]serviceObservation{{ID: "exim", IsInstalled: true, Status: "active (running)"}}, "apt")
	if profile := profileViewByID(t, mailProfilesView(blocked, true, "apt", "", false, true), "core-mail"); profile.Status != mailProfileStatusBlocked || profile.Available || profile.BlockedReason == "" {
		t.Fatalf("blocked profile = %+v", profile)
	}

	webmailObservations := []serviceObservation{
		{ID: "postfix", IsInstalled: true, Status: "active (running)"},
		{ID: "dovecot", IsInstalled: true, Status: "active (running)"},
		{ID: "nginx", IsInstalled: true, Status: "active (running)"},
		{ID: "php-fpm", IsInstalled: true, Status: "active (running)"},
		{ID: "roundcube", IsInstalled: true, Status: "installed"},
	}
	webmailServices := catalogView(webmailObservations, "apt")
	if profile := profileViewByID(t, mailProfilesView(webmailServices, true, "apt", "", true, true), "webmail"); profile.Status != mailProfileStatusComplete || profile.Warning != mailProfileReconciliationWarning {
		t.Fatalf("ready webmail = %+v", profile)
	}
	for name, profiles := range map[string][]MailProfileResponse{
		"missing proof": mailProfilesView(webmailServices, true, "apt", "", false, false),
		"dead socket":   mailProfilesView(webmailServices, true, "apt", "", false, true),
	} {
		if profile := profileViewByID(t, profiles, "webmail"); profile.Status != mailProfileStatusPartial || profile.Warning == "" {
			t.Fatalf("%s webmail = %+v", name, profile)
		}
	}
}

func TestMailProfileHostBlockedReasonIsActionable(t *testing.T) {
	previous := readMailProfileHostname
	t.Cleanup(func() { readMailProfileHostname = previous })

	readMailProfileHostname = func() (string, error) { return "mail.example.test", nil }
	if reason := mailProfileHostBlockedReason(); reason != "" {
		t.Fatalf("valid FQDN blocked: %q", reason)
	}
	readMailProfileHostname = func() (string, error) { return "localhost", nil }
	if reason := mailProfileHostBlockedReason(); reason != mailProfileServerHostnameMessage {
		t.Fatalf("invalid FQDN reason = %q", reason)
	}
	readMailProfileHostname = func() (string, error) { return "", errors.New("unavailable") }
	if reason := mailProfileHostBlockedReason(); reason == "" {
		t.Fatal("hostname read failure did not block profiles")
	}
}

func TestManagedProfileCacheTimestampAndWebmailProof(t *testing.T) {
	panel := newManagedServiceCachePanel(t)
	ready := true
	raw, err := json.Marshal(scanCacheDoc{
		Observations: []serviceObservation{
			{ID: "postfix", IsInstalled: true, Status: "active (running)"},
			{ID: "dovecot", IsInstalled: true, Status: "active (running)"},
		},
		WebmailReady: &ready,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := panel.db.GetDB().Exec(
		`INSERT INTO service_scan_cache (id, data, scanned_at) VALUES (1, ?, 'not-a-time')`, string(raw),
	); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	panel.handleManagedServices(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/managed-services", nil))
	var payload managedServicesPayload
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	for _, profile := range payload.Profiles {
		if profile.Status != mailProfileStatusUnknown || profile.Available {
			t.Fatalf("malformed timestamp exposed verified profile %+v", profile)
		}
	}

	if _, err := panel.db.GetDB().Exec(
		"UPDATE service_scan_cache SET scanned_at=? WHERE id=1",
		time.Now().UTC().Add(-6*time.Minute).Format(time.RFC3339),
	); err != nil {
		t.Fatal(err)
	}
	recorder = httptest.NewRecorder()
	panel.handleManagedServices(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/managed-services", nil))
	payload = managedServicesPayload{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.ScannedAt == nil {
		t.Fatal("parseable stale timestamp was not returned")
	}
	for _, profile := range payload.Profiles {
		if profile.Status != mailProfileStatusUnknown || profile.Available {
			t.Fatalf("stale timestamp exposed verified profile %+v", profile)
		}
	}

	if _, err := panel.db.GetDB().Exec(
		"UPDATE service_scan_cache SET scanned_at=? WHERE id=1",
		time.Now().UTC().Add(time.Minute).Format(time.RFC3339),
	); err != nil {
		t.Fatal(err)
	}
	recorder = httptest.NewRecorder()
	panel.handleManagedServices(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/managed-services", nil))
	payload = managedServicesPayload{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	for _, profile := range payload.Profiles {
		if profile.Status != mailProfileStatusUnknown || profile.Available {
			t.Fatalf("future timestamp exposed verified profile %+v", profile)
		}
	}
}

func seedProfileOperation(
	t *testing.T,
	fixture serviceOperationTestFixture,
	profileID, phase string,
) serviceOperation {
	t.Helper()
	requestID := mustServiceOperationRequestID(t)
	op, err := fixture.panel.createServiceOperationRequest(
		context.Background(), serviceOperationKindMailProfileInstall, profileID, "", requestID, serviceOperationActor{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.database.GetDB().Exec(
		`UPDATE service_operations SET status=?, phase=? WHERE id=?`, serviceOperationRunning, phase, op.ID,
	); err != nil {
		t.Fatal(err)
	}
	op.Status, op.Phase = serviceOperationRunning, phase
	return op
}

func TestMailProfileRecoveryRerunsFromFirstService(t *testing.T) {
	fixture, agent := newMailProfileTestFixture(t)
	seedInstalledServices(agent.serviceOperationTestAgent, "postfix")
	agent.serviceOperationTestAgent.mu.Lock()
	agent.active["postfix"] = true
	agent.serviceOperationTestAgent.mu.Unlock()
	op := seedProfileOperation(t, fixture, "core-mail", mailProfilePhase("core-mail", "dovecot", "scanning"))
	agent.serviceOperationTestAgent.mu.Lock()
	agent.mutationJobs[op.RequestID] = &ServiceOperationMutationJob{
		RequestID: op.RequestID,
		OwnerID:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Kind:      serviceOperationKindMailProfileInstall,
		Target:    "core-mail",
		Status:    agentMutationFailed,
		ErrorCode: "panel_restarted_during_mutation",
	}
	agent.serviceOperationTestAgent.mu.Unlock()

	if recovered, err := fixture.panel.recoverInterruptedServiceOperations(context.Background()); err != nil || recovered != 0 {
		t.Fatalf("recover = %d, %v", recovered, err)
	}
	waitForServiceOperation(t, fixture.panel, fixture.userID, op.ID, serviceOperationSucceeded)
	var installed []string
	for _, call := range agent.callsSnapshot() {
		if call.Name == "install" {
			installed = append(installed, call.ServiceID)
		}
	}
	if !reflect.DeepEqual(installed, []string{"postfix", "dovecot"}) {
		t.Fatalf("recovery resumed from stored phase: installs=%v", installed)
	}
}

func TestSucceededMailProfileRecoveryReconstructsFullResult(t *testing.T) {
	fixture, agent := newMailProfileTestFixture(t)
	seedInstalledServices(agent.serviceOperationTestAgent, "postfix", "dovecot")
	agent.serviceOperationTestAgent.mu.Lock()
	agent.active["postfix"], agent.active["dovecot"] = true, true
	agent.serviceOperationTestAgent.mu.Unlock()
	op := seedProfileOperation(t, fixture, "core-mail", mailProfilePhase("core-mail", "firewall"))
	agent.serviceOperationTestAgent.mu.Lock()
	agent.mutationJobs[op.RequestID] = &ServiceOperationMutationJob{
		RequestID: op.RequestID,
		OwnerID:   "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Kind:      serviceOperationKindMailProfileInstall,
		Target:    "core-mail",
		Status:    agentMutationSucceeded,
	}
	agent.serviceOperationTestAgent.mu.Unlock()

	if recovered, err := fixture.panel.recoverInterruptedServiceOperations(context.Background()); err != nil || recovered != 1 {
		t.Fatalf("recover = %d, %v", recovered, err)
	}
	loaded, _ := getServiceOperation(t, fixture.panel, fixture.userID, op.ID)
	var result struct {
		Success              bool                 `json:"success"`
		ProfileID            string               `json:"profile_id"`
		Services             []string             `json:"services"`
		CompletedServices    []string             `json:"completed_services"`
		MailTLS              mailProfileTLSResult `json:"mail_tls"`
		SubmissionConfigured bool                 `json:"submission_configured"`
		Warnings             []string             `json:"warnings"`
	}
	if err := json.Unmarshal(loaded.Result, &result); err != nil {
		t.Fatal(err)
	}
	if !result.Success || result.ProfileID != "core-mail" ||
		!reflect.DeepEqual(result.Services, []string{"postfix", "dovecot"}) ||
		!reflect.DeepEqual(result.CompletedServices, result.Services) ||
		!result.MailTLS.Configured || !result.MailTLS.FallbackOnly ||
		!result.SubmissionConfigured || len(result.Warnings) != 1 {
		t.Fatalf("reconstructed result = %+v", result)
	}
	calls := agent.callsSnapshot()
	if len(calls) != 1 || calls[0].Name != "mail-tls" {
		t.Fatalf("succeeded recovery child calls=%v, want one fresh mail TLS child", calls)
	}
}

func TestMailProfileRecoveryRejectsUnknownOrPackageTarget(t *testing.T) {
	fixture, _ := newMailProfileTestFixture(t)
	for _, op := range []serviceOperation{
		{Kind: serviceOperationKindMailProfileInstall, ServiceID: "unknown"},
		{Kind: serviceOperationKindMailProfileInstall, ServiceID: "core-mail", PackageName: "postfix"},
	} {
		if err := fixture.panel.resumeInterruptedServiceOperation(op); err == nil {
			t.Fatalf("invalid recovery target passed: %+v", op)
		}
	}
}

func TestMailProfileFinalScanCountContract(t *testing.T) {
	fixture, agent := newMailProfileTestFixture(t)
	_, failure := runMailProfileForTest(t, fixture, "core-mail")
	if failure != nil {
		t.Fatal(failure.Cause)
	}
	agent.profileMu.Lock()
	defer agent.profileMu.Unlock()
	if agent.scanCalls != 6 {
		t.Fatalf("core profile GetServices calls = %d, want 6", agent.scanCalls)
	}
}
