package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
)

var errServerSetupDNSPublisherRequired = errors.New("the reviewed primary DNS publisher must be authorized")
var errServerSetupDNSReadinessRequired = errors.New("the reviewed DNS pair must become ready")

func serverSetupSecondaryHosting(draft serverSetupDraft) bool {
	return draft.DNSMode == setupDNSModeLocal && draft.DNSRole == "secondary" && serverSetupNeedsDNSPublisher(draft) && draft.DNSHostingManagement != "manual"
}

// Empty retains the Alpha71 reviewed-plan behavior. Only an explicit manual
// selection uses external record instructions for newly hosted domains.
// Bos deger Alpha71 planlarini korur; yalniz acik manuel secim yeni alan
// adlarinda harici DNS kayit yonlendirmesini kullanir.
func serverSetupManualSecondaryHosting(draft serverSetupDraft) bool {
	return draft.DNSMode == setupDNSModeLocal && draft.DNSRole == "secondary" && serverSetupNeedsDNSPublisher(draft) && draft.DNSHostingManagement == "manual"
}

func serverSetupDomainDNSMode(draft serverSetupDraft) string {
	if serverSetupManualSecondaryHosting(draft) {
		return setupDNSModeExternal
	}
	return draft.DNSMode
}

// DNS, the selected nginx challenge route and trusted management access
// bootstrap before the pair is ready. No tenant domain is created. The
// hosting steps remain in the same immutable execution, behind the relevant
// publishing proof. Existing saved plans retain their original step identities.
func serverSetupDNSBootstrapSteps(draft serverSetupDraft, steps []serverSetupPlanStep) []serverSetupPlanStep {
	if draft.DNSMode != setupDNSModeLocal || !serverSetupNeedsDNSPublisher(draft) {
		return steps
	}
	bootstrap, remaining := []serverSetupPlanStep{}, []serverSetupPlanStep{}
	for _, step := range steps {
		if step.Kind == "dns" || step.Kind == "firewall" || step.Kind == "panel_certificate" || (step.Kind == "service" && (step.Target == "nginx" || step.Target == "nftables" || step.Target == "certbot")) {
			bootstrap = append(bootstrap, step)
		} else {
			remaining = append(remaining, step)
		}
	}
	kind, target := "dns_readiness", "local"
	if serverSetupManualSecondaryHosting(draft) {
		target = "secondary"
	}
	if serverSetupSecondaryHosting(draft) {
		kind, target = "dns_publisher", draft.DNSPublisherEndpoint
	}
	steps = append(bootstrap, serverSetupPlanStep{Kind: kind, Target: target})
	steps = append(steps, remaining...)
	for index := range steps {
		steps[index].ID = fmt.Sprintf("%02d-%s", index+1, steps[index].Kind)
	}
	return steps
}

// This immutable, execution-scoped selection is separate from execution_json:
// a runner that read the execution before confirmation cannot overwrite it.
// Neither a credential nor an enrollment secret is stored in this receipt.
type serverSetupDNSPublisherBinding struct {
	ExecutionID string                         `json:"execution_id"`
	PlanID      string                         `json:"plan_id"`
	Connection  serverSetupRemoteDNSConnection `json:"connection"`
	PrimaryIP   string                         `json:"primary_ip"`
	SecondaryIP string                         `json:"secondary_ip"`
}

func serverSetupDNSPublisherKey(executionID string) string {
	return "server_setup_dns_publisher:" + executionID
}

func (p *Panel) readServerSetupDNSPublisher(ctx context.Context, plan serverSetupPlan, executionID string) (*serverSetupDNSPublisherBinding, error) {
	var raw string
	err := p.db.GetDB().QueryRowContext(ctx, `SELECT value FROM panel_settings WHERE key=?`, serverSetupDNSPublisherKey(executionID)).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var binding serverSetupDNSPublisherBinding
	if json.Unmarshal([]byte(raw), &binding) != nil || binding.ExecutionID != executionID || binding.PlanID != plan.ID || !validServiceOperationID(binding.Connection.ID) || binding.Connection.Endpoint != plan.Draft.DNSPublisherEndpoint {
		return nil, errors.New("saved DNS publisher binding does not match its reviewed execution")
	}
	names := []string{plan.Draft.NS1, plan.Draft.NS2}
	slices.Sort(names)
	if !slices.Equal(binding.Connection.Nameservers, names) || binding.PrimaryIP != plan.Draft.PeerIP || binding.SecondaryIP != plan.Draft.LocalIP {
		return nil, errors.New("saved DNS publisher topology differs from review")
	}
	return &binding, nil
}

func (p *Panel) serverSetupDNSPublisherProof(ctx context.Context, draft serverSetupDraft, connectionID string) (serverSetupDNSPublisherBinding, error) {
	var binding serverSetupDNSPublisherBinding
	if !serverSetupSecondaryHosting(draft) || !validServiceOperationID(connectionID) {
		return binding, errServerSetupDNSPublisherRequired
	}
	connection, err := p.readRemoteDNSConnection(ctx, connectionID)
	if err != nil || connection.Endpoint != draft.DNSPublisherEndpoint {
		return binding, errServerSetupDNSPublisherRequired
	}
	proof, err := p.remoteDNSConnectionPairReadiness(ctx, connectionID)
	if err != nil || proof.PrimaryIP != draft.PeerIP || proof.SecondaryIP != draft.LocalIP || len(proof.Nameservers) != 2 || proof.Nameservers[0] != draft.NS1 || proof.Nameservers[1] != draft.NS2 {
		return binding, errServerSetupDNSPublisherRequired
	}
	names := append([]string(nil), proof.Nameservers...)
	slices.Sort(names)
	expected := []string{draft.NS1, draft.NS2}
	slices.Sort(expected)
	if !slices.Equal(names, expected) {
		return binding, errServerSetupDNSPublisherRequired
	}
	binding.Connection = serverSetupRemoteDNSConnection{ID: connection.ID, Endpoint: connection.Endpoint, Nameservers: names}
	binding.PrimaryIP, binding.SecondaryIP = proof.PrimaryIP, proof.SecondaryIP
	return binding, nil
}

func (p *Panel) runServerSetupDNSPublisher(ctx context.Context, plan serverSetupPlan, executionID string) (bool, error) {
	if err := p.requireServerSetupAdmission(); err != nil {
		return false, err
	}
	binding, err := p.readServerSetupDNSPublisher(ctx, plan, executionID)
	if err != nil {
		return false, err
	}
	if binding == nil {
		return false, errServerSetupDNSPublisherRequired
	}
	if _, err := p.serverSetupDNSPublisherProof(ctx, plan.Draft, binding.Connection.ID); err != nil {
		return false, errServerSetupDNSPublisherRequired
	}
	ready, err := p.serverSetupLocalSecondaryReadiness(ctx, plan.Draft)
	if err != nil || !ready {
		return false, errServerSetupDNSPublisherRequired
	}
	if err := p.commitServerSetupDNSPublisherDefault(ctx, plan, executionID, binding.Connection.ID); err != nil {
		return false, err
	}
	return true, nil
}

func (p *Panel) serverSetupManualSecondaryHostingReadiness(ctx context.Context, draft serverSetupDraft) (bool, error) {
	if !serverSetupManualSecondaryHosting(draft) {
		return false, nil
	}
	mode, err := p.setupDNSManagementMode(ctx)
	if err != nil || mode != setupDNSModeExternal {
		return false, err
	}
	return p.serverSetupLocalSecondaryReadiness(ctx, draft)
}

func (p *Panel) serverSetupLocalSecondaryReadiness(ctx context.Context, draft serverSetupDraft) (bool, error) {
	request, local, err := setupDNSIdentity(draft)
	if err != nil || draft.DNSRole != "secondary" {
		return false, err
	}
	state, err := readDNSEngineDBState(ctx, p.db.GetDB())
	if err != nil || !setupDNSDraftMatchesState(draft, request, local, state) {
		return false, err
	}
	if ready, err := p.setupDNSModeReadiness(ctx, setupDNSModeLocal); err != nil || !ready {
		return false, err
	}
	return p.setupDNSNameserverReadiness(ctx)
}

func (p *Panel) serverSetupSecondaryHostingReadiness(ctx context.Context, draft serverSetupDraft) (bool, error) {
	execution, err := p.latestServerSetupExecution(ctx)
	if err != nil || execution == nil {
		return false, err
	}
	plan, err := p.loadServerSetupPlan(ctx, execution.PlanID)
	if err != nil {
		return false, err
	}
	want, _ := json.Marshal(draft)
	actual, _ := json.Marshal(plan.Draft)
	if string(want) != string(actual) || validateServerSetupExecution(plan, *execution) != nil {
		return false, nil
	}
	binding, err := p.readServerSetupDNSPublisher(ctx, plan, execution.ID)
	if err != nil || binding == nil {
		return false, err
	}
	mode, err := p.setupDNSManagementMode(ctx)
	if err != nil || mode != setupDNSModeExisting {
		return false, err
	}
	id, err := p.defaultRemoteDNSConnectionID(ctx)
	if err != nil || id != binding.Connection.ID {
		return false, err
	}
	if _, err := p.serverSetupDNSPublisherProof(ctx, draft, id); err != nil {
		return false, err
	}
	return p.serverSetupLocalSecondaryReadiness(ctx, draft)
}

func serverSetupAtPublisherGate(plan serverSetupPlan, execution serverSetupExecution) bool {
	if !serverSetupSecondaryHosting(plan.Draft) || execution.Status != "waiting" || execution.Phase != "dns_publisher" || validateServerSetupExecution(plan, execution) != nil {
		return false
	}
	found := false
	for _, step := range execution.Steps {
		if step.Kind == "dns_publisher" {
			if found || step.Status != "running" || step.Target != plan.Draft.DNSPublisherEndpoint {
				return false
			}
			found = true
		} else if (!found && step.Status != "succeeded") || (found && step.Status != "pending") {
			return false
		}
	}
	return found
}

func (p *Panel) handleServerSetupPublisher(w http.ResponseWriter, r *http.Request) {
	if !serverSetupAdmin(w, r, http.MethodPost) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	var request struct {
		ExecutionID  string `json:"execution_id"`
		ConnectionID string `json:"connection_id"`
	}
	if decodeServiceOperationJSON(w, r, &request) != nil || !validServiceOperationID(request.ExecutionID) || !validServiceOperationID(request.ConnectionID) {
		writeClientError(w, http.StatusBadRequest, "confirm the setup execution and authorized DNS connection")
		return
	}
	if err := p.requireServerSetupAdmission(); err != nil {
		writeCodedError(w, http.StatusForbidden, "license_required", "Activate the license to continue setup.", "")
		return
	}
	p.serverSetupMu.Lock()
	defer p.serverSetupMu.Unlock()
	conflict := func() {
		writeCodedError(w, http.StatusConflict, "server_setup_dns_publisher_required", "Authorize the reviewed primary DNS server before continuing this setup.", "/setup")
	}
	var raw string
	if err := p.db.GetDB().QueryRowContext(r.Context(), `SELECT execution_json FROM server_setup_executions WHERE id=?`, request.ExecutionID).Scan(&raw); err != nil {
		conflict()
		return
	}
	var execution serverSetupExecution
	if json.Unmarshal([]byte(raw), &execution) != nil {
		conflict()
		return
	}
	plan, err := p.loadServerSetupPlan(r.Context(), execution.PlanID)
	if err != nil || validateServerSetupExecution(plan, execution) != nil || !serverSetupSecondaryHosting(plan.Draft) {
		conflict()
		return
	}
	previous, err := p.readServerSetupDNSPublisher(r.Context(), plan, execution.ID)
	if err != nil {
		conflict()
		return
	}
	if previous != nil {
		// Lost-response replay returns the same accepted selection even when
		// its execution has advanced. It can never replace that selection.
		if previous.Connection.ID != request.ConnectionID {
			conflict()
			return
		}
		p.launchServerSetupExecution()
		writeServerSetupExecution(w, execution, http.StatusAccepted)
		return
	}
	state, err := p.loadServerSetup(r.Context())
	if err != nil || state.Revision != plan.Revision || !serverSetupAtPublisherGate(plan, execution) {
		conflict()
		return
	}
	binding, err := p.serverSetupDNSPublisherProof(r.Context(), plan.Draft, request.ConnectionID)
	if err != nil {
		conflict()
		return
	}
	if ready, err := p.serverSetupLocalSecondaryReadiness(r.Context(), plan.Draft); err != nil || !ready {
		conflict()
		return
	}
	if active, err := p.activeServiceOperation(r.Context()); err != nil || active != nil {
		conflict()
		return
	}
	if job, err := p.statusAgentMutation(r.Context(), ""); err != nil || (job != nil && agentMutationActive(job.Status)) {
		conflict()
		return
	}
	binding.ExecutionID, binding.PlanID = execution.ID, plan.ID
	encoded, _ := json.Marshal(binding)
	// The conditional insertion binds only the exact still-waiting execution.
	// A concurrent identical confirmation is reconciled below without replacing
	// either its plan or the already completed DNS/certificate operations.
	_, err = p.db.GetDB().ExecContext(r.Context(), `INSERT OR IGNORE INTO panel_settings(key,value)
		SELECT ?,? WHERE EXISTS (SELECT 1 FROM server_setup_executions WHERE id=? AND execution_json=? AND status='waiting')
		AND EXISTS (SELECT 1 FROM server_setup_state WHERE id=1 AND revision=?)
		AND NOT EXISTS (SELECT 1 FROM service_operations WHERE status IN ('queued','running'))`,
		serverSetupDNSPublisherKey(execution.ID), string(encoded), execution.ID, raw, plan.Revision)
	if err != nil {
		writeServerError(w, err)
		return
	}
	saved, err := p.readServerSetupDNSPublisher(r.Context(), plan, execution.ID)
	if err != nil || saved == nil || saved.Connection.ID != request.ConnectionID {
		conflict()
		return
	}
	p.audit(r, "server.setup.dns_publisher", "", 0)
	p.launchServerSetupExecution()
	writeServerSetupExecution(w, execution, http.StatusAccepted)
}
