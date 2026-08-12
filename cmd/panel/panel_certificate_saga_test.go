package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	paneldb "github.com/alicelik/celikpanel/internal/db"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

func newPanelCertificateSagaTestPanel(
	t *testing.T,
	agent *firewallSyncTestAgent,
) (*Panel, *paneldb.SQLiteDB) {
	t.Helper()
	database, err := paneldb.NewSQLiteDB(filepath.Join(t.TempDir(), "panel.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	panel := &Panel{db: database}
	attachFirewallSyncTestAgent(t, panel, agent)
	t.Setenv("CELIKPANEL_TLS_CERT", "")
	t.Setenv("CELIKPANEL_TLS_KEY", "")
	t.Setenv("CELIKPANEL_TLS_DIR", panelManagedTLSDirectory)
	return panel, database
}

func insertPanelCertificateSagaAdmin(t *testing.T, database *paneldb.SQLiteDB) int {
	t.Helper()
	result, err := database.GetDB().Exec(
		"INSERT INTO users (username,password_hash,email,role) VALUES (?,?,?,?)",
		"cert-saga-admin", "x", "cert-saga-private@example.test", roleAdmin,
	)
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return int(id)
}

func panelCertificateSagaRequestBody(t *testing.T, requestID string) *bytes.Reader {
	t.Helper()
	raw, err := json.Marshal(map[string]string{
		"domain": "panel.example.test", "request_id": requestID,
	})
	if err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(raw)
}

func createPanelCertificateSagaTestOperation(
	t *testing.T,
	panel *Panel,
	requestID string,
) (serviceOperation, panelCertificateSagaData) {
	t.Helper()
	commitment, err := mutationpayload.CanonicalPanelCertificateIssue(
		"panel.example.test",
		"cert-saga-private@example.test",
		panelManagedTLSDirectory,
		strings.TrimSpace(buildCommit),
	)
	if err != nil {
		t.Fatal(err)
	}
	data := newPanelCertificateSagaData(commitment)
	identity := serviceOperation{
		RequestID: requestID, Kind: serviceOperationKindPanelCertificate,
		ServiceID: commitment.Domain, PackageName: commitment.Qualifier,
		Status: serviceOperationQueued, Phase: panelCertificatePhaseQueued,
	}
	encoded, err := canonicalPanelCertificateSagaData(identity, data)
	if err != nil {
		t.Fatal(err)
	}
	op, err := panel.createServiceOperationRequestWithState(
		context.Background(),
		serviceOperationKindPanelCertificate,
		commitment.Domain,
		commitment.Qualifier,
		requestID,
		serviceOperationActor{},
		panelCertificatePhaseQueued,
		encoded,
	)
	if err != nil {
		t.Fatal(err)
	}
	return op, data
}

func transitionPanelCertificateSagaTestChild(
	t *testing.T,
	panel *Panel,
	op *serviceOperation,
	data *panelCertificateSagaData,
	phase, step, kind, target, qualifier string,
	firewall *panelCertificateSagaFirewall,
) *panelCertificateSagaChild {
	t.Helper()
	child, err := newPanelCertificateSagaChild(step, kind, target, qualifier)
	if err != nil {
		t.Fatal(err)
	}
	child.Firewall = firewall
	data.Child = child
	if err := panel.transitionPanelCertificateSaga(
		context.Background(), op, phase, *data,
	); err != nil {
		t.Fatal(err)
	}
	return child
}

func TestPanelCertificateOperationDataIsPrivateStrictAndMigrated(t *testing.T) {
	panel, database := newPanelCertificateSagaTestPanel(
		t,
		&firewallSyncTestAgent{status: FirewallStatusResp{EngineAvailable: true}},
	)
	op, _ := createPanelCertificateSagaTestOperation(
		t, panel, "41111111111111111111111111111111",
	)

	var columnCount int
	if err := database.GetDB().QueryRow(
		"SELECT COUNT(*) FROM pragma_table_info('service_operations') WHERE name='operation_data'",
	).Scan(&columnCount); err != nil {
		t.Fatal(err)
	}
	if columnCount != 1 {
		t.Fatalf("operation_data columns = %d, want 1", columnCount)
	}

	recorder := httptest.NewRecorder()
	panel.handleServiceOperation(
		recorder,
		httptest.NewRequest(
			http.MethodGet,
			"/api/v1/service/operation?request_id="+op.RequestID,
			nil,
		),
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("operation response = %d %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, privateValue := range []string{
		"operation_data",
		"cert-saga-private@example.test",
		"expected_build_commit",
		"certificate_qualifier",
		"owner_id",
	} {
		if strings.Contains(body, privateValue) {
			t.Fatalf("operation API leaked %q: %s", privateValue, body)
		}
	}

	tampered := op
	tampered.OperationData = strings.TrimSuffix(op.OperationData, "}") + `,"unknown":true}`
	if _, err := decodePanelCertificateSagaData(tampered); err == nil ||
		!strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown operation_data field error = %v", err)
	}
}

func TestPanelCertificateSagaCASRejectsStaleOperationData(t *testing.T) {
	panel, database := newPanelCertificateSagaTestPanel(
		t,
		&firewallSyncTestAgent{status: FirewallStatusResp{EngineAvailable: true}},
	)
	op, data := createPanelCertificateSagaTestOperation(
		t, panel, "42222222222222222222222222222222",
	)

	queuedReplacementJSON := `{"tampered":true}`
	if _, err := database.GetDB().Exec(
		"UPDATE service_operations SET operation_data=? WHERE id=?",
		queuedReplacementJSON, op.ID,
	); err != nil {
		t.Fatal(err)
	}
	if err := panel.startPanelCertificateSaga(&op); err == nil ||
		!strings.Contains(err.Error(), "lost its row") {
		t.Fatalf("stale queued start error = %v", err)
	}
	loaded, err := panel.serviceOperationByID(context.Background(), op.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != serviceOperationQueued ||
		loaded.OperationData != queuedReplacementJSON {
		t.Fatalf("stale start overwrote row: %+v", loaded)
	}

	if _, err := database.GetDB().Exec(
		"UPDATE service_operations SET operation_data=? WHERE id=?",
		op.OperationData, op.ID,
	); err != nil {
		t.Fatal(err)
	}
	loaded.OperationData = op.OperationData
	if err := panel.startPanelCertificateSaga(&loaded); err != nil {
		t.Fatal(err)
	}
	runningReplacementJSON := `{"tampered":true}`
	if _, err := database.GetDB().Exec(
		"UPDATE service_operations SET operation_data=? WHERE id=?",
		runningReplacementJSON, loaded.ID,
	); err != nil {
		t.Fatal(err)
	}
	if err := panel.transitionPanelCertificateSaga(
		context.Background(),
		&loaded,
		panelCertificatePhasePreflightSkipped,
		data,
	); err == nil || !strings.Contains(err.Error(), "lost its running row") {
		t.Fatalf("stale running transition error = %v", err)
	}
	after, err := panel.serviceOperationByID(context.Background(), loaded.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Phase != panelCertificatePhasePreflightPlanning ||
		after.OperationData != runningReplacementJSON {
		t.Fatalf("stale transition overwrote row: %+v", after)
	}
}

func TestPanelCertificateSagaFailureMetadataNeverPersistsAgentDetail(t *testing.T) {
	code, message := panelCertificateSagaFailure(&panelCertificateChildTerminalFailure{
		code:    "agent_raw_code",
		message: "secret command output /root/private/key",
		cause:   errors.New("sensitive host failure"),
	})
	if code != "panel_certificate_issue_failed" ||
		message != "The panel certificate could not be issued and verified." {
		t.Fatalf("durable failure classification = %q %q", code, message)
	}
}

func TestPanelCertificateOperationPersistsBeforeFirewallPreflight(t *testing.T) {
	statusStarted := make(chan struct{})
	releaseStatus := make(chan struct{})
	agent := &firewallSyncTestAgent{
		status:        FirewallStatusResp{Enabled: true, EngineAvailable: true},
		statusStarted: statusStarted,
		releaseStatus: releaseStatus,
		issueResponse: transport.IssuePanelCertificateResponse{Issued: true},
	}
	panel, database := newPanelCertificateSagaTestPanel(t, agent)
	userID := insertPanelCertificateSagaAdmin(t, database)
	requestID := "43333333333333333333333333333333"

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/settings/panel-certificate",
		panelCertificateSagaRequestBody(t, requestID),
	)
	request = request.WithContext(context.WithValue(
		request.Context(), callerKey, &Caller{ID: userID, Role: roleAdmin},
	))
	recorder := httptest.NewRecorder()
	panel.handlePanelCertificate(recorder, request)
	if recorder.Code != http.StatusAccepted {
		close(releaseStatus)
		t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
	}
	select {
	case <-statusStarted:
	case <-time.After(2 * time.Second):
		close(releaseStatus)
		t.Fatal("saga did not reach blocked firewall preflight")
	}
	op, err := panel.serviceOperationByRequestID(context.Background(), requestID)
	if err != nil {
		close(releaseStatus)
		t.Fatal(err)
	}
	if op.Status != serviceOperationRunning ||
		op.Phase != panelCertificatePhasePreflightPlanning ||
		op.OperationData == "" {
		close(releaseStatus)
		t.Fatalf("preflight began without durable operation row: %+v", op)
	}
	data, err := decodePanelCertificateSagaData(op)
	if err != nil || data.Email != "cert-saga-private@example.test" {
		close(releaseStatus)
		t.Fatalf("durable preflight payload = %+v err=%v", data, err)
	}
	close(releaseStatus)
	waitPanelCertificateOperation(t, panel, requestID, serviceOperationSucceeded)
}

func TestPanelCertificateAdmissionRejectsUnrelatedAgentLeaseButReplaysExactRow(t *testing.T) {
	tests := []struct {
		name       string
		seedReplay bool
		wantStatus int
		wantRows   int
	}{
		{name: "new request is busy", wantStatus: http.StatusConflict, wantRows: 0},
		{name: "exact replay precedes admission", seedReplay: true, wantStatus: http.StatusAccepted, wantRows: 1},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			agent := &firewallSyncTestAgent{
				status: FirewallStatusResp{Enabled: true, EngineAvailable: true},
			}
			panel, database := newPanelCertificateSagaTestPanel(t, agent)
			userID := insertPanelCertificateSagaAdmin(t, database)
			requestID := strings.Repeat(string(rune('5'+index)), 32)
			if test.seedReplay {
				createPanelCertificateSagaTestOperation(t, panel, requestID)
			}
			agent.durableMutationRPCFixture.mu.Lock()
			agent.durableMutationRPCFixture.jobs = map[string]*ServiceOperationMutationJob{
				"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa": {
					RequestID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
					OwnerID:   "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
					Kind:      "panel-certificate-activation",
					Target:    "unrelated.example.test",
					Status:    agentMutationRunning,
				},
			}
			agent.durableMutationRPCFixture.active = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			agent.durableMutationRPCFixture.mu.Unlock()

			request := httptest.NewRequest(
				http.MethodPost,
				"/api/v1/settings/panel-certificate",
				panelCertificateSagaRequestBody(t, requestID),
			)
			request = request.WithContext(context.WithValue(
				request.Context(), callerKey, &Caller{ID: userID, Role: roleAdmin},
			))
			recorder := httptest.NewRecorder()
			panel.handlePanelCertificate(recorder, request)
			if recorder.Code != test.wantStatus {
				t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
			}
			var rows int
			if err := database.GetDB().QueryRow(
				"SELECT COUNT(*) FROM service_operations",
			).Scan(&rows); err != nil {
				t.Fatal(err)
			}
			if rows != test.wantRows {
				t.Fatalf("operation rows = %d, want %d", rows, test.wantRows)
			}
		})
	}
}

func TestPanelCertificateCapabilityGateFailsBeforeOperationRowOrMutation(t *testing.T) {
	tests := []struct {
		name         string
		requestID    string
		capabilities []string
	}{
		{name: "legacy nil", requestID: strings.Repeat("9", 32)},
		{
			name: "duplicate", requestID: strings.Repeat("a", 32),
			capabilities: []string{
				transport.AgentCapabilityFirewallApplyV2,
				transport.AgentCapabilityFirewallApplyV2,
			},
		},
		{
			name: "unexpected extension", requestID: strings.Repeat("c", 32),
			capabilities: []string{
				transport.AgentCapabilityFirewallApplyV2,
				transport.AgentCapabilityPanelCertificateIssueV2,
				"unreviewed_capability",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			capabilities := append([]string(nil), test.capabilities...)
			agent := &firewallSyncTestAgent{
				status:              FirewallStatusResp{Enabled: true, EngineAvailable: true},
				versionCapabilities: &capabilities,
			}
			panel, database := newPanelCertificateSagaTestPanel(t, agent)
			userID := insertPanelCertificateSagaAdmin(t, database)
			request := httptest.NewRequest(
				http.MethodPost,
				"/api/v1/settings/panel-certificate",
				panelCertificateSagaRequestBody(t, test.requestID),
			)
			request = request.WithContext(context.WithValue(
				request.Context(), callerKey, &Caller{ID: userID, Role: roleAdmin},
			))
			recorder := httptest.NewRecorder()
			panel.handlePanelCertificate(recorder, request)
			if recorder.Code != http.StatusInternalServerError {
				t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
			}
			var rows int
			if err := database.GetDB().QueryRow(
				"SELECT COUNT(*) FROM service_operations",
			).Scan(&rows); err != nil {
				t.Fatal(err)
			}
			if rows != 0 {
				t.Fatalf("invalid capability list created %d operation rows", rows)
			}
			agent.mu.Lock()
			defer agent.mu.Unlock()
			if agent.beginCalls != 0 || agent.applyCalls != 0 || agent.issueCalls != 0 {
				t.Fatalf(
					"invalid capability list mutated host: begin=%d firewall=%d issue=%d",
					agent.beginCalls, agent.applyCalls, agent.issueCalls,
				)
			}
		})
	}
}

func TestPanelCertificatePostCommitFirewallFailureRemainsRunningUntilRetry(t *testing.T) {
	agent := &firewallSyncTestAgent{
		status:             FirewallStatusResp{Enabled: true, EngineAvailable: true},
		applyResponseError: "forced final firewall failure",
	}
	panel, _ := newPanelCertificateSagaTestPanel(t, agent)
	op, data := createPanelCertificateSagaTestOperation(
		t, panel, "47777777777777777777777777777777",
	)
	if err := panel.startPanelCertificateSaga(&op); err != nil {
		t.Fatal(err)
	}
	commitment, err := mutationpayload.CanonicalFirewallApply(
		true, false, []int{2083}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	data.CertificateCommitted = true
	child := transitionPanelCertificateSagaTestChild(
		t, panel, &op, &data,
		panelCertificatePhaseFinalChild,
		panelCertificateChildFinal,
		"firewall_sync",
		"nftables",
		commitment.Qualifier,
		&panelCertificateSagaFirewall{
			Enabled: true, Persist: false,
			TCPPorts: append([]int(nil), commitment.TCPPorts...),
			UDPPorts: append([]int(nil), commitment.UDPPorts...),
		},
	)
	data.Child = child
	encoded, err := canonicalPanelCertificateSagaData(op, data)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := panel.db.GetDB().Exec(
		"UPDATE service_operations SET operation_data=? WHERE id=?",
		encoded, op.ID,
	); err != nil {
		t.Fatal(err)
	}
	op.OperationData = encoded

	terminal, retry, stepErr := panel.stepPanelCertificateSaga(
		context.Background(), &op,
	)
	if terminal || !retry || stepErr == nil {
		t.Fatalf("failed final step terminal=%v retry=%v err=%v", terminal, retry, stepErr)
	}
	pending, err := panel.serviceOperationByID(context.Background(), op.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pending.Status != serviceOperationRunning ||
		pending.Phase != panelCertificatePhaseFinalPlanning {
		t.Fatalf("post-commit firewall failure became terminal: %+v", pending)
	}
	pendingData, err := decodePanelCertificateSagaData(pending)
	if err != nil || !pendingData.CertificateCommitted || pendingData.Child != nil {
		t.Fatalf("post-commit retry state = %+v err=%v", pendingData, err)
	}

	agent.mu.Lock()
	agent.applyResponseError = ""
	agent.mu.Unlock()
	if terminal, _, err = panel.stepPanelCertificateSaga(
		context.Background(), &pending,
	); terminal || err != nil {
		t.Fatalf("final retry planning terminal=%v err=%v", terminal, err)
	}
	if terminal, _, err = panel.stepPanelCertificateSaga(
		context.Background(), &pending,
	); !terminal || err != nil {
		t.Fatalf("final retry execution terminal=%v err=%v", terminal, err)
	}
	succeeded, err := panel.serviceOperationByID(context.Background(), op.ID)
	if err != nil {
		t.Fatal(err)
	}
	if succeeded.Status != serviceOperationSucceeded {
		t.Fatalf("retried final operation = %+v", succeeded)
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.applyRequests) != 2 ||
		containsInt(agent.applyRequests[1].TCPPorts, 80) {
		t.Fatalf("retried final firewall policies = %+v", agent.applyRequests)
	}
}

func TestPanelCertificateIdentityMismatchRetainsLockAndRetries(t *testing.T) {
	previousDelay := panelCertificateSagaRetryDelay
	panelCertificateSagaRetryDelay = 25 * time.Millisecond
	t.Cleanup(func() { panelCertificateSagaRetryDelay = previousDelay })
	agent := &firewallSyncTestAgent{}
	panel, _ := newPanelCertificateSagaTestPanel(t, agent)
	op, data := createPanelCertificateSagaTestOperation(
		t, panel, "4a4a4a4a4a4a4a4a4a4a4a4a4a4a4a4a",
	)
	if err := panel.startPanelCertificateSaga(&op); err != nil {
		t.Fatal(err)
	}
	child := transitionPanelCertificateSagaTestChild(
		t, panel, &op, &data,
		panelCertificatePhaseCertificateChild,
		panelCertificateChildCertificate,
		serviceOperationKindPanelCertificate,
		data.Domain,
		data.CertificateQualifier,
		nil,
	)
	agent.durableMutationRPCFixture.mu.Lock()
	agent.durableMutationRPCFixture.jobs = map[string]*ServiceOperationMutationJob{
		child.RequestID: {
			RequestID:   child.RequestID,
			OwnerID:     "ffffffffffffffffffffffffffffffff",
			Kind:        child.Kind,
			Target:      child.Target,
			PackageName: child.Qualifier,
			Status:      agentMutationRunning,
		},
	}
	agent.durableMutationRPCFixture.active = child.RequestID
	agent.durableMutationRPCFixture.mu.Unlock()

	panel.serviceMutationMu.Lock()
	panel.launchPanelCertificateSaga(op, serviceOperationActor{}, panel.serviceMutationMu.Unlock)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		agent.mu.Lock()
		calls := agent.mutationStatusCalls
		agent.mu.Unlock()
		if calls > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if panel.serviceMutationMu.TryLock() {
		panel.serviceMutationMu.Unlock()
		t.Fatal("identity mismatch released the process mutation lock")
	}
	retained, err := panel.serviceOperationByID(context.Background(), op.ID)
	if err != nil {
		t.Fatal(err)
	}
	if retained.Status != serviceOperationRunning ||
		retained.Phase != panelCertificatePhaseCertificateChild ||
		retained.OperationData != op.OperationData {
		t.Fatalf("identity mismatch terminalized or rewrote saga: %+v", retained)
	}

	if _, err := panel.db.GetDB().Exec(
		"UPDATE service_operations SET status=? WHERE id=?",
		serviceOperationFailed, op.ID,
	); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if panel.serviceMutationMu.TryLock() {
			panel.serviceMutationMu.Unlock()
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("test cleanup could not stop the identity-mismatch retry driver")
}

func TestRecoverablePanelCertificateActivationAcrossSagaGapsAndOldDomain(t *testing.T) {
	commitment, err := mutationpayload.CanonicalPanelCertificateIssue(
		"new-panel.example.test",
		"cert-saga-private@example.test",
		panelManagedTLSDirectory,
		strings.TrimSpace(buildCommit),
	)
	if err != nil {
		t.Fatal(err)
	}
	base := newPanelCertificateSagaData(commitment)
	activation := &agentMutationJob{
		RequestID: "abababababababababababababababab",
		OwnerID:   "cdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd",
		Kind:      "panel-certificate-activation",
		Target:    "old-panel.example.test",
		Status:    agentMutationRunning,
	}
	tests := []struct {
		name   string
		status string
		phase  string
		mutate func(*panelCertificateSagaData)
	}{
		{name: "queued", status: serviceOperationQueued, phase: panelCertificatePhaseQueued},
		{name: "preflight planning", status: serviceOperationRunning, phase: panelCertificatePhasePreflightPlanning},
		{name: "certificate planning", status: serviceOperationRunning, phase: panelCertificatePhaseCertificatePlanning},
		{
			name: "compensation planning", status: serviceOperationRunning,
			phase: panelCertificatePhaseCompensatePlanning,
			mutate: func(data *panelCertificateSagaData) {
				data.FailureCode = "panel_certificate_issue_failed"
				data.FailureMessage = "The panel certificate could not be issued."
			},
		},
		{
			name: "final planning", status: serviceOperationRunning,
			phase: panelCertificatePhaseFinalPlanning,
			mutate: func(data *panelCertificateSagaData) {
				data.CertificateCommitted = true
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := base
			if test.mutate != nil {
				test.mutate(&data)
			}
			op := serviceOperation{
				RequestID: "efefefefefefefefefefefefefefefef",
				Kind:      serviceOperationKindPanelCertificate, ServiceID: commitment.Domain,
				PackageName: commitment.Qualifier, Status: test.status, Phase: test.phase,
			}
			encoded, err := canonicalPanelCertificateSagaData(op, data)
			if err != nil {
				t.Fatal(err)
			}
			op.OperationData = encoded
			if !recoverablePanelCertificateActivation(op, activation) {
				t.Fatalf("old-domain activation rejected in %q", test.phase)
			}
		})
	}

	for _, test := range []struct {
		name   string
		mutate func(*agentMutationJob)
	}{
		{"noncanonical target", func(job *agentMutationJob) { job.Target = "Old-Panel.Example.Test." }},
		{"package", func(job *agentMutationJob) { job.PackageName = "unexpected" }},
		{"kind", func(job *agentMutationJob) { job.Kind = "firewall_sync" }},
		{"request", func(job *agentMutationJob) { job.RequestID = "short" }},
		{"owner", func(job *agentMutationJob) { job.OwnerID = "short" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			copy := *activation
			test.mutate(&copy)
			if validIndependentPanelCertificateActivation(&copy) {
				t.Fatalf("invalid independent activation accepted: %+v", copy)
			}
		})
	}
}

func TestStartupRecoveryWaitsForIndependentActivationWithoutOperation(t *testing.T) {
	agent := &firewallSyncTestAgent{}
	panel, _ := newPanelCertificateSagaTestPanel(t, agent)
	activation := &ServiceOperationMutationJob{
		RequestID: "12121212121212121212121212121212",
		OwnerID:   "34343434343434343434343434343434",
		Kind:      "panel-certificate-activation",
		Target:    "renewing-panel.example.test",
		Status:    agentMutationRunning,
		Phase:     "panel-certificate-activation",
	}
	agent.durableMutationRPCFixture.mu.Lock()
	agent.durableMutationRPCFixture.jobs = map[string]*ServiceOperationMutationJob{
		activation.RequestID: activation,
	}
	agent.durableMutationRPCFixture.active = activation.RequestID
	agent.durableMutationRPCFixture.mu.Unlock()

	type recoveryResult struct {
		recovered int64
		err       error
	}
	result := make(chan recoveryResult, 1)
	go func() {
		recovered, err := panel.recoverInterruptedServiceOperations(
			context.Background(),
		)
		result <- recoveryResult{recovered: recovered, err: err}
	}()
	select {
	case got := <-result:
		t.Fatalf("recovery returned before activation terminalized: %+v", got)
	case <-time.After(100 * time.Millisecond):
	}
	agent.durableMutationRPCFixture.mu.Lock()
	got := agent.durableMutationRPCFixture.jobs[activation.RequestID]
	active := agent.durableMutationRPCFixture.active
	agent.durableMutationRPCFixture.mu.Unlock()
	if got == nil || got.Status != agentMutationRunning || active != activation.RequestID {
		t.Fatalf("listener recovery cancelled activation: job=%+v active=%q", got, active)
	}
	agent.durableMutationRPCFixture.mu.Lock()
	got.Status = agentMutationSucceeded
	got.Phase = "completed"
	agent.durableMutationRPCFixture.active = ""
	agent.durableMutationRPCFixture.mu.Unlock()
	select {
	case recovery := <-result:
		if recovery.err != nil || recovery.recovered != 0 {
			t.Fatalf(
				"independent activation recovery=%d err=%v",
				recovery.recovered,
				recovery.err,
			)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("recovery did not finish after activation terminalized")
	}
	if !panel.serviceMutationMu.TryLock() {
		t.Fatal("activation recovery retained process lock without an operation row")
	}
	panel.serviceMutationMu.Unlock()
}

func TestStartupRecoveryAllowsOnlyExactCertificateActivationRestartWindow(t *testing.T) {
	useFastPanelCertificateSagaRetry(t)
	agent := &firewallSyncTestAgent{
		status: FirewallStatusResp{Enabled: true, EngineAvailable: true},
	}
	panel, _ := newPanelCertificateSagaTestPanel(t, agent)
	op, data := createPanelCertificateSagaTestOperation(
		t, panel, "48888888888888888888888888888888",
	)
	if err := panel.startPanelCertificateSaga(&op); err != nil {
		t.Fatal(err)
	}
	child := transitionPanelCertificateSagaTestChild(
		t, panel, &op, &data,
		panelCertificatePhaseCertificateChild,
		panelCertificateChildCertificate,
		serviceOperationKindPanelCertificate,
		data.Domain,
		data.CertificateQualifier,
		nil,
	)
	activation := &agentMutationJob{
		RequestID: "cccccccccccccccccccccccccccccccc",
		OwnerID:   "dddddddddddddddddddddddddddddddd",
		Kind:      "panel-certificate-activation",
		Target:    data.Domain,
		Status:    agentMutationRunning,
	}
	if !recoverablePanelCertificateActivation(op, activation) {
		t.Fatal("exact certificate activation restart window was rejected")
	}
	for _, test := range []struct {
		name   string
		mutate func(*agentMutationJob)
	}{
		{"domain", func(job *agentMutationJob) { job.Target = "Other.Example.Test." }},
		{"package", func(job *agentMutationJob) { job.PackageName = "unexpected" }},
		{"kind", func(job *agentMutationJob) { job.Kind = "firewall_sync" }},
		{"request", func(job *agentMutationJob) { job.RequestID = "short" }},
		{"owner", func(job *agentMutationJob) { job.OwnerID = "short" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			copy := *activation
			test.mutate(&copy)
			if recoverablePanelCertificateActivation(op, &copy) {
				t.Fatalf("mismatched activation accepted: %+v", copy)
			}
		})
	}

	agent.durableMutationRPCFixture.mu.Lock()
	activationJob := &ServiceOperationMutationJob{
		RequestID: activation.RequestID, OwnerID: activation.OwnerID,
		Kind: activation.Kind, Target: activation.Target,
		PackageName: activation.PackageName, Status: activation.Status,
	}
	agent.durableMutationRPCFixture.jobs = map[string]*ServiceOperationMutationJob{
		child.RequestID: {
			RequestID: child.RequestID, OwnerID: child.OwnerID,
			Kind: child.Kind, Target: child.Target, PackageName: child.Qualifier,
			Status: agentMutationSucceeded,
			Phase: "commit/panel-certificate-issue/v1/published/" +
				child.RequestID + "/" + child.Target + "/" + child.Qualifier,
		},
		activation.RequestID: activationJob,
	}
	agent.durableMutationRPCFixture.active = activation.RequestID
	agent.durableMutationRPCFixture.mu.Unlock()

	type recoveryResult struct {
		recovered int64
		err       error
	}
	result := make(chan recoveryResult, 1)
	go func() {
		recovered, err := panel.recoverInterruptedServiceOperations(
			context.Background(),
		)
		result <- recoveryResult{recovered: recovered, err: err}
	}()
	select {
	case got := <-result:
		t.Fatalf("restart recovery returned before activation terminalized: %+v", got)
	case <-time.After(100 * time.Millisecond):
	}
	agent.durableMutationRPCFixture.mu.Lock()
	activationJob.Status = agentMutationSucceeded
	activationJob.Phase = "completed"
	agent.durableMutationRPCFixture.active = ""
	agent.durableMutationRPCFixture.mu.Unlock()
	select {
	case recovery := <-result:
		if recovery.err != nil || recovery.recovered != 0 {
			t.Fatalf(
				"restart-window recovery=%d err=%v",
				recovery.recovered,
				recovery.err,
			)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("restart recovery did not finish after activation terminalized")
	}
	if panel.serviceMutationMu.TryLock() {
		panel.serviceMutationMu.Unlock()
		t.Fatal("resumed certificate saga did not retain the process mutation lock")
	}
	waitPanelCertificateOperation(t, panel, op.RequestID, serviceOperationSucceeded)
	if !panel.serviceMutationMu.TryLock() {
		t.Fatal("terminal resumed certificate saga retained the process mutation lock")
	}
	panel.serviceMutationMu.Unlock()
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.applyRequests) == 0 ||
		containsInt(agent.applyRequests[len(agent.applyRequests)-1].TCPPorts, 80) {
		t.Fatalf("startup final firewall policy = %+v", agent.applyRequests)
	}
}
