package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/mail"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/hostname"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

const serverSetupPlanVersion = 1

type serverSetupPlanStep struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Target    string `json:"target"`
	Qualifier string `json:"qualifier,omitempty"`
}

type serverSetupRemoteDNSConnection struct {
	ID          string   `json:"id"`
	Endpoint    string   `json:"endpoint"`
	Nameservers []string `json:"nameservers"`
}

type serverSetupPlan struct {
	ID                  string                          `json:"id"`
	Version             int                             `json:"version"`
	Revision            int                             `json:"revision"`
	Purpose             string                          `json:"purpose"`
	Steps               []serverSetupPlanStep           `json:"steps"`
	Blockers            []string                        `json:"blockers"`
	CanStart            bool                            `json:"can_start"`
	TCPPorts            []int                           `json:"tcp_ports"`
	UDPPorts            []int                           `json:"udp_ports"`
	PreserveSSH         bool                            `json:"preserve_ssh"`
	PersistFirewall     bool                            `json:"persist_firewall"`
	HostnameChange      string                          `json:"hostname_change,omitempty"`
	RemoteDNSConnection *serverSetupRemoteDNSConnection `json:"remote_dns_connection,omitempty"`
	Draft               serverSetupDraft                `json:"draft"`
	BuildCommit         string                          `json:"build_commit"`
	ContactEmail        string                          `json:"contact_email"`
	Actor               serviceOperationActor           `json:"actor"`
	Components          []serverSetupPlanComponent      `json:"components,omitempty"`
}

type serverSetupExecutionStep struct {
	serverSetupPlanStep
	Status      string `json:"status"`
	RequestID   string `json:"request_id"`
	OwnerID     string `json:"owner_id"`
	OperationID string `json:"operation_id,omitempty"`
}

type serverSetupExecution struct {
	ID        string                     `json:"id"`
	RequestID string                     `json:"request_id"`
	PlanID    string                     `json:"plan_id"`
	Status    string                     `json:"status"`
	Phase     string                     `json:"phase"`
	Steps     []serverSetupExecutionStep `json:"steps"`
	Error     *serviceOperationError     `json:"error,omitempty"`
	Checks    []serverSetupCheck         `json:"checks,omitempty"`
	PanelURL  string                     `json:"panel_url,omitempty"`
}

var serverSetupRunners sync.Map

func serverSetupID(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:16])
}

func (p *Panel) serverSetupExecutionActive(ctx context.Context) (bool, error) {
	var count int
	err := p.db.GetDB().QueryRowContext(ctx, `SELECT COUNT(*) FROM server_setup_executions WHERE status IN ('running','waiting')`).Scan(&count)
	return count != 0, err
}

func (p *Panel) serverSetupExecutionMutating(ctx context.Context) (bool, error) {
	var count int
	err := p.db.GetDB().QueryRowContext(ctx, `SELECT COUNT(*) FROM server_setup_executions WHERE status='running'`).Scan(&count)
	return count != 0, err
}

func (p *Panel) latestServerSetupExecution(ctx context.Context) (*serverSetupExecution, error) {
	var raw string
	err := p.db.GetDB().QueryRowContext(ctx, `SELECT execution_json FROM server_setup_executions ORDER BY created_at DESC, id DESC LIMIT 1`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var execution serverSetupExecution
	if err := json.Unmarshal([]byte(raw), &execution); err != nil {
		return nil, err
	}
	return &execution, nil
}

func (p *Panel) loadServerSetupPlan(ctx context.Context, id string) (serverSetupPlan, error) {
	var raw string
	err := p.db.GetDB().QueryRowContext(ctx, `SELECT plan_json FROM server_setup_plans WHERE id=?`, id).Scan(&raw)
	if err != nil {
		return serverSetupPlan{}, err
	}
	var plan serverSetupPlan
	if err := json.Unmarshal([]byte(raw), &plan); err != nil {
		return plan, err
	}
	if plan.Version != serverSetupPlanVersion || plan.ID != id || serverSetupPlanIdentity(plan) != id {
		return plan, errors.New("saved setup plan identity is invalid")
	}
	return plan, nil
}

func serverSetupPlanIdentity(plan serverSetupPlan) string {
	plan.ID = ""
	encoded, _ := json.Marshal(plan)
	return serverSetupID(string(encoded))
}

func (p *Panel) handleServerSetupPlan(w http.ResponseWriter, r *http.Request) {
	if !serverSetupAdmin(w, r, http.MethodPost) {
		return
	}
	var request struct {
		Revision int `json:"revision"`
	}
	if err := decodeServiceOperationJSON(w, r, &request); err != nil {
		writeClientError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	p.serverSetupMu.Lock()
	defer p.serverSetupMu.Unlock()
	state, err := p.loadServerSetup(r.Context())
	if err != nil {
		writeServerError(w, err)
		return
	}
	if state.Revision != request.Revision {
		writeCodedError(w, http.StatusConflict, "server_setup_review_stale", "Setup changed. Review the current plan.", "")
		return
	}
	plan, err := p.buildServerSetupPlan(r.Context(), state, captureServiceOperationActor(r))
	if err != nil {
		writeServerError(w, err)
		return
	}
	encoded, err := json.Marshal(plan)
	if err != nil {
		writeServerError(w, err)
		return
	}
	_, err = p.db.GetDB().ExecContext(r.Context(), `INSERT OR IGNORE INTO server_setup_plans(id,revision,plan_json,requested_by,created_at) VALUES(?,?,?,?,?)`, plan.ID, plan.Revision, string(encoded), plan.Actor.UserID, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		writeServerError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(plan)
}

func serverSetupAdmin(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method != method {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	caller := currentCaller(r)
	if caller == nil || caller.Role != roleAdmin {
		writeClientError(w, http.StatusForbidden, "admin only")
		return false
	}
	return true
}

func (p *Panel) buildServerSetupPlan(ctx context.Context, state serverSetupState, actor serviceOperationActor) (serverSetupPlan, error) {
	draft := state.Draft
	plan := serverSetupPlan{Version: serverSetupPlanVersion, Revision: state.Revision, Purpose: draft.Purpose, Draft: draft, Actor: actor,
		BuildCommit: strings.TrimSpace(buildCommit), Steps: []serverSetupPlanStep{}, Blockers: []string{}, TCPPorts: []int{panelPort(), 80}, UDPPorts: []int{}, PreserveSSH: true, PersistFirewall: true}
	addBlocker := func(code string) {
		if !slices.Contains(plan.Blockers, code) {
			plan.Blockers = append(plan.Blockers, code)
		}
	}
	addStep := func(kind, target, qualifier string) {
		plan.Steps = append(plan.Steps, serverSetupPlanStep{ID: fmt.Sprintf("%02d-%s", len(plan.Steps)+1, kind), Kind: kind, Target: target, Qualifier: qualifier})
	}
	if canonical, err := hostname.CanonicalFQDN(draft.PanelDomain); err != nil || canonical != draft.PanelDomain {
		addBlocker("server_setup_panel_domain_invalid")
	}
	if err := p.db.GetDB().QueryRowContext(ctx, `SELECT email FROM users WHERE id=?`, actor.UserID).Scan(&plan.ContactEmail); err != nil {
		return plan, err
	}
	plan.ContactEmail = strings.TrimSpace(plan.ContactEmail)
	if parsed, err := mail.ParseAddress(plan.ContactEmail); err != nil || parsed.Name != "" || parsed.Address != plan.ContactEmail {
		addBlocker("panel_certificate_contact_email_invalid")
	}
	if _, _, blocked := panelCertificateManagementBlocker(); blocked {
		addBlocker("server_setup_panel_certificate_unmanaged")
	}
	if !setupDNSModeSupported(draft.DNSMode) {
		addBlocker("server_setup_dns_mode_unsupported")
	}

	if draft.DNSMode == setupDNSModeExisting {
		authority, err := p.remoteDNSConnectionReadiness(ctx, draft.RemoteDNSConnectionID)
		if err != nil || !authority.Ready {
			addBlocker("server_setup_remote_dns_unavailable")
		} else {
			var endpoint string
			err = p.db.GetDB().QueryRowContext(ctx, `SELECT endpoint FROM remote_dns_connections WHERE id=? AND status='ready'`, draft.RemoteDNSConnectionID).Scan(&endpoint)
			if err != nil {
				return plan, err
			}
			plan.RemoteDNSConnection = &serverSetupRemoteDNSConnection{ID: draft.RemoteDNSConnectionID, Endpoint: endpoint, Nameservers: append([]string(nil), authority.Nameservers...)}
			slices.Sort(plan.RemoteDNSConnection.Nameservers)
		}
	}
	if draft.Purpose == "dns" && draft.DNSMode != "local" {
		addBlocker("server_setup_dns_purpose_requires_local")
	}
	if draft.DNSMode == "local" {
		request, local, identityErr := setupDNSIdentity(draft)
		if identityErr != nil {
			addBlocker("server_setup_dns_identity_required")
		} else {
			state, stateErr := readDNSEngineDBState(ctx, p.db.GetDB())
			if stateErr != nil {
				return plan, stateErr
			}
			if state.ActiveEngine != "" && !setupDNSDraftMatchesState(draft, request, local, state) {
				addBlocker("server_setup_existing_dns_requires_migration")
			}
			if serverSetupSecondaryHosting(draft) {
				endpoint, err := canonicalRemoteDNSEndpoint(draft.DNSPublisherEndpoint)
				if err != nil || endpoint != draft.DNSPublisherEndpoint {
					addBlocker("server_setup_dns_publisher_endpoint_required")
				}
			}
		}
	}
	var installedIDs []string
	if err := p.callAgentContext(ctx, "Agent.InstalledServiceIDsStrict", &transport.Empty{}, &installedIDs); err != nil {
		return plan, err
	}
	var existingFirewall FirewallStatusResp
	if err := p.callAgentContext(ctx, "Agent.FirewallStatus", &transport.Empty{}, &existingFirewall); err != nil {
		return plan, err
	}
	if existingFirewall.Error != "" {
		return plan, errors.New("firewall status could not be verified")
	}
	plan.TCPPorts = append(plan.TCPPorts, existingFirewall.TCPPorts...)
	plan.TCPPorts = append(plan.TCPPorts, existingFirewall.SSHPorts...)
	plan.UDPPorts = append(plan.UDPPorts, existingFirewall.UDPPorts...)
	installed := make(map[string]bool, len(installedIDs))
	for _, id := range installedIDs {
		installed[id] = true
	}
	if draft.Customization != nil && serverSetupHasComponent(draft, "node") {
		var versions transport.NodeVersionsResponse
		if err := p.callAgentContext(ctx, "Agent.ListNodeVersions", &transport.Empty{}, &versions); err != nil {
			return plan, err
		}
		for _, version := range versions.Installed {
			if strings.TrimPrefix(version, "v") == strings.TrimPrefix(draft.NodeVersion, "v") {
				installed["node"] = true
			}
		}
	}
	planned := make(map[string]bool, len(installed))
	for id := range installed {
		planned[id] = true
	}
	host := p.managedServiceHostProfile()
	if draft.DNSMode == "local" {
		managed := core.GetManagedServiceByID(draft.DNSEngine)
		if managed == nil {
			addBlocker("server_setup_dns_engine_unsupported")
		} else if _, reason := core.ManagedServiceInstallBlockForHost(managed, host); reason != "" {
			addBlocker("server_setup_dns_engine_unsupported")
		}
	}
	addService := func(id string) {
		managed := core.GetManagedServiceByID(id)
		if managed == nil {
			addBlocker("server_setup_service_unknown")
			return
		}
		if _, reason := core.ManagedServiceInstallBlockForHost(managed, host); reason != "" {
			addBlocker("server_setup_service_unsupported:" + id)
		}
		if missing := core.RequirementsMissing(managed, planned); len(missing) > 0 {
			addBlocker("server_setup_dependency_missing:" + id)
		}
		if taken := core.SeatTakenBy(managed, planned); taken != "" {
			addBlocker("server_setup_service_conflict:" + id + ":" + taken)
		}
		if !installed[id] {
			addStep("service", id, "")
		}
		planned[id] = true
	}
	addStep("dns", draft.DNSMode, "")
	if draft.Customization != nil {
		resolved, err := serverSetupResolvedComponents(draft)
		if err != nil {
			addBlocker("server_setup_service_unknown")
		}
		if len(resolved) == 0 && !(draft.Purpose == "custom" && draft.DNSMode == "local") && draft.Purpose != "dns" {
			addBlocker("server_setup_components_required")
		}
		for _, id := range resolved {
			plan.Components = append(plan.Components, serverSetupPlanComponent{ID: id, Selected: slices.Contains(draft.Customization.Components, id), Required: !slices.Contains(draft.Customization.Components, id), Installed: installed[id]})
			switch id {
			case "postfix", "dovecot", "rspamd", "roundcube":
				// These components are installed only by the audited mail profile.
			case "node":
				if !nodeSemverRe.MatchString(draft.NodeVersion) {
					addBlocker("server_setup_node_version_required")
				}
				managed := core.GetManagedServiceByID(id)
				if _, reason := core.ManagedServiceInstallBlockForHost(managed, host); reason != "" {
					addBlocker("server_setup_service_unsupported:" + id)
				}
				if len(core.RequirementsMissing(managed, planned)) != 0 {
					addBlocker("server_setup_dependency_missing:" + id)
				}
				addStep("runtime", "node", draft.NodeVersion)
				planned[id] = true
			default:
				addService(id)
			}
		}
		for _, id := range serverSetupRequiredComponents {
			plan.Components = append(plan.Components, serverSetupPlanComponent{ID: id, Required: true, Installed: installed[id]})
		}
	} else {
		switch draft.Purpose {
		case "web", "web_mail":
			addService("nginx")
			addService("php-fpm")
			addService("mariadb")
		case "application":
			addService("nginx")
			if !nodeSemverRe.MatchString(draft.NodeVersion) {
				addBlocker("server_setup_node_version_required")
			}
			if managed := core.GetManagedServiceByID("node"); managed != nil {
				if _, reason := core.ManagedServiceInstallBlockForHost(managed, host); reason != "" {
					addBlocker("server_setup_service_unsupported:node")
				}
			}
			addStep("runtime", "node", draft.NodeVersion)
			if draft.Database != "" && draft.Database != "none" {
				if draft.Database != "mariadb" && draft.Database != "postgresql" {
					addBlocker("server_setup_database_invalid")
				} else {
					addService(draft.Database)
				}
			}
		case "dns":
		default:
			addBlocker("server_setup_purpose_invalid")
		}
	}
	mailProfiles := serverSetupMailProfileIDs(draft)
	if len(mailProfiles) > 0 {
		if canonical, err := hostname.CanonicalFQDN(draft.MailHostname); err != nil || canonical != draft.MailHostname {
			addBlocker("server_setup_mail_hostname_invalid")
		}
		for _, profileID := range mailProfiles {
			profile, _ := mailProfileByID(profileID)
			for _, id := range profile.Services {
				managed := core.GetManagedServiceByID(id)
				if _, reason := core.ManagedServiceInstallBlockForHost(managed, host); reason != "" {
					addBlocker("server_setup_service_unsupported:" + id)
				}
				if draft.Customization != nil && len(core.RequirementsMissing(managed, planned)) > 0 {
					addBlocker("server_setup_dependency_missing:" + id)
				}
				if taken := core.SeatTakenBy(managed, planned); taken != "" {
					addBlocker("server_setup_service_conflict:" + id + ":" + taken)
				}
				planned[id] = true
			}
			addStep("mail_profile", profileID, "")
		}
	}
	addService("nftables")
	addService("certbot")
	for id := range planned {
		if service := core.GetManagedServiceByID(id); service != nil {
			for _, port := range service.FirewallPorts {
				if port.Proto == "udp" {
					plan.UDPPorts = append(plan.UDPPorts, port.Port)
				} else {
					plan.TCPPorts = append(plan.TCPPorts, port.Port)
				}
			}
		}
	}
	if draft.DNSMode == "local" {
		plan.TCPPorts = append(plan.TCPPorts, 53)
		plan.UDPPorts = append(plan.UDPPorts, 53)
	}
	slices.Sort(plan.TCPPorts)
	plan.TCPPorts = slices.Compact(plan.TCPPorts)
	slices.Sort(plan.UDPPorts)
	plan.UDPPorts = slices.Compact(plan.UDPPorts)
	addStep("firewall", "enable_and_persist", "")
	cert := currentPanelCert()
	certificateRequired := serverSetupPanelCertificateChangesRequired(cert, draft.PanelDomain, plan.Steps)
	if !certificateRequired {
		var renewal transport.PanelRenewalReadinessResponse
		if err := p.callAgentContext(ctx, "Agent.PanelRenewalReadiness", &transport.PanelRenewalReadinessRequest{Domain: draft.PanelDomain}, &renewal); err != nil {
			addBlocker("server_setup_panel_renewal_unavailable")
		} else {
			certificateRequired = !renewal.Ready
		}
	}
	if certificateRequired {
		addStep("panel_certificate", draft.PanelDomain, "")
	}
	if len(mailProfiles) > 0 {
		var agent transport.AgentVersionResponse
		if err := p.callAgentContext(ctx, "Agent.Version", &transport.Empty{}, &agent); err != nil {
			return plan, err
		}
		if err := requireKnownAgentCapabilities(agent.Capabilities, transport.AgentCapabilityMailHostCertificateV1); err != nil {
			addBlocker("server_setup_mail_certificate_unavailable")
		}
		addStep("mail_certificate", draft.MailHostname, "")
	}
	addStep("verify", draft.Purpose, "")
	plan.Steps = serverSetupDNSBootstrapSteps(draft, plan.Steps)
	plan.CanStart = len(plan.Blockers) == 0
	plan.ID = serverSetupPlanIdentity(plan)
	return plan, nil
}

func (p *Panel) handleServerSetupStart(w http.ResponseWriter, r *http.Request) {
	if !serverSetupAdmin(w, r, http.MethodPost) {
		return
	}
	if !p.requireSubsystemOperational(w, degradedSubsystemServiceOperations) {
		return
	}
	var request struct {
		PlanID    string `json:"plan_id"`
		Confirmed bool   `json:"confirmed"`
		RequestID string `json:"request_id"`
	}
	if err := decodeServiceOperationJSON(w, r, &request); err != nil || !request.Confirmed || !validServiceOperationID(request.RequestID) || !validServiceOperationID(request.PlanID) {
		writeClientError(w, http.StatusBadRequest, "confirm the exact reviewed plan and provide a valid request_id")
		return
	}
	p.serverSetupMu.Lock()
	defer p.serverSetupMu.Unlock()
	var previous string
	err := p.db.GetDB().QueryRowContext(r.Context(), `SELECT execution_json FROM server_setup_executions WHERE request_id=?`, request.RequestID).Scan(&previous)
	if err == nil {
		var execution serverSetupExecution
		if json.Unmarshal([]byte(previous), &execution) != nil {
			writeServerError(w, errors.New("invalid saved setup operation"))
			return
		}
		if execution.PlanID != request.PlanID {
			writeCodedError(w, http.StatusConflict, "server_setup_request_conflict", "Request identity belongs to another setup plan.", "")
			return
		}
		if execution.Status == "running" || execution.Status == "waiting" {
			p.launchServerSetupExecution()
		}
		writeServerSetupExecution(w, execution, http.StatusAccepted)
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		writeServerError(w, err)
		return
	}
	if err := p.requireServerSetupAdmission(); err != nil {
		writeCodedError(w, http.StatusForbidden, "license_required", "An active license is required to start setup.", "")
		return
	}
	plan, err := p.loadServerSetupPlan(r.Context(), request.PlanID)
	if err != nil {
		writeCodedError(w, http.StatusConflict, "server_setup_review_required", "Review the setup plan first.", "")
		return
	}
	state, err := p.loadServerSetup(r.Context())
	if err != nil {
		writeServerError(w, err)
		return
	}
	if state.Revision != plan.Revision || plan.BuildCommit != strings.TrimSpace(buildCommit) || plan.Actor.UserID != currentCaller(r).ID {
		writeCodedError(w, http.StatusConflict, "server_setup_review_stale", "Setup changed. Review the current plan.", "")
		return
	}
	fresh, err := p.buildServerSetupPlan(r.Context(), state, plan.Actor)
	if err != nil {
		writeServerError(w, err)
		return
	}
	if fresh.ID != plan.ID {
		writeCodedError(w, http.StatusConflict, "server_setup_review_stale", "Server requirements changed. Review the current plan.", "")
		return
	}
	if !plan.CanStart {
		writeCodedError(w, http.StatusConflict, "server_setup_plan_blocked", "Resolve the plan requirements before starting.", "")
		return
	}
	active, err := p.serverSetupExecutionActive(r.Context())
	if err != nil {
		writeServerError(w, err)
		return
	}
	if active {
		writeCodedError(w, http.StatusConflict, "server_setup_busy", "A setup operation is already active.", "")
		return
	}
	execution := serverSetupExecution{ID: request.RequestID, RequestID: request.RequestID, PlanID: plan.ID, Status: "running", Phase: "queued", Steps: []serverSetupExecutionStep{}, PanelURL: fmt.Sprintf("https://%s:%d/", plan.Draft.PanelDomain, panelPort())}
	for _, step := range plan.Steps {
		execution.Steps = append(execution.Steps, serverSetupExecutionStep{serverSetupPlanStep: step, Status: "pending", RequestID: serverSetupID(execution.ID, step.ID, "request"), OwnerID: serverSetupID(execution.ID, step.ID, "owner")})
	}
	encoded, _ := json.Marshal(execution)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := p.db.GetDB().BeginTx(r.Context(), nil)
	if err != nil {
		writeServerError(w, err)
		return
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(r.Context(), `UPDATE server_setup_state SET status='running',updated_at=? WHERE id=1 AND revision=? AND status IN ('new','legacy','draft','ready')`, now, plan.Revision)
	if err != nil {
		writeServerError(w, err)
		return
	}
	changed, err := result.RowsAffected()
	if err != nil {
		writeServerError(w, err)
		return
	}
	if changed != 1 {
		writeCodedError(w, http.StatusConflict, "server_setup_review_stale", "Setup changed. Review the current plan.", "")
		return
	}
	_, err = tx.ExecContext(r.Context(), `INSERT INTO server_setup_executions(id,request_id,plan_id,status,execution_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, execution.ID, execution.RequestID, plan.ID, execution.Status, string(encoded), now, now)
	if err != nil {
		writeServerError(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeServerError(w, err)
		return
	}
	p.auditServiceOperation(r.Context(), captureServiceOperationActor(r), "server.setup.start:"+plan.Purpose)
	p.launchServerSetupExecution()
	writeServerSetupExecution(w, execution, http.StatusAccepted)
}

func writeServerSetupExecution(w http.ResponseWriter, execution serverSetupExecution, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(execution)
}

func (p *Panel) handleServerSetupOperation(w http.ResponseWriter, r *http.Request) {
	if !serverSetupAdmin(w, r, http.MethodGet) {
		return
	}
	var execution *serverSetupExecution
	var err error
	if requestID := r.URL.Query().Get("request_id"); requestID != "" {
		if !validServiceOperationID(requestID) {
			writeClientError(w, http.StatusBadRequest, "invalid request_id")
			return
		}
		var raw string
		err = p.db.GetDB().QueryRowContext(r.Context(), `SELECT execution_json FROM server_setup_executions WHERE request_id=?`, requestID).Scan(&raw)
		if errors.Is(err, sql.ErrNoRows) {
			err = nil
		} else if err == nil {
			execution = &serverSetupExecution{}
			err = json.Unmarshal([]byte(raw), execution)
		}
	} else {
		execution, err = p.latestServerSetupExecution(r.Context())
	}
	if err != nil {
		writeServerError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(p.serverSetupOperationResponse(r.Context(), execution))
}

func (p *Panel) persistServerSetupExecution(ctx context.Context, execution serverSetupExecution) error {
	encoded, err := json.Marshal(execution)
	if err != nil {
		return err
	}
	result, err := p.db.GetDB().ExecContext(ctx, `UPDATE server_setup_executions SET status=?,execution_json=?,updated_at=? WHERE id=? AND status IN ('running','waiting')`, execution.Status, string(encoded), time.Now().UTC().Format(time.RFC3339Nano), execution.ID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return errors.New("setup execution lost active state")
	}
	return nil
}

// Called once after the existing service/DNS startup reconciliation. Reads and
// browser refreshes never launch work; only a committed start or restart does.
func (p *Panel) resumeServerSetupExecutions() { p.launchServerSetupExecution() }

func (p *Panel) launchServerSetupExecution() {
	if _, loaded := serverSetupRunners.LoadOrStore(p, true); loaded {
		return
	}
	go func() {
		lastID := ""
		defer func() { p.finishServerSetupRunner(lastID) }()
		defer func() {
			if recovered := recover(); recovered != nil {
				log.Printf("server setup runner stopped: %v", recovered)
			}
		}()
		for {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			execution, err := p.latestServerSetupExecution(ctx)
			if err != nil || execution == nil || (execution.Status != "running" && execution.Status != "waiting") {
				cancel()
				return
			}
			lastID = execution.ID
			plan, err := p.loadServerSetupPlan(ctx, execution.PlanID)
			if err != nil {
				cancel()
				log.Printf("server setup saved plan: %v", err)
				return
			}
			cancel()
			progressed, err := p.advanceServerSetupExecution(plan, execution)
			if err != nil {
				log.Printf("server setup execution %s: %v", execution.ID, err)
				return
			}
			if execution.Status == "failed" || execution.Status == "succeeded" {
				return
			}
			if !progressed {
				if execution.Status == "waiting" {
					time.Sleep(20 * time.Second)
				} else {
					time.Sleep(2 * time.Second)
				}
			}
		}
	}()
}

func (p *Panel) advanceServerSetupExecution(plan serverSetupPlan, execution *serverSetupExecution) (bool, error) {
	if err := validateServerSetupExecution(plan, *execution); err != nil {
		return false, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
	defer cancel()
	for index := range execution.Steps {
		step := &execution.Steps[index]
		if step.Status == "succeeded" {
			continue
		}
		execution.Phase = step.ID
		if step.Status == "pending" {
			step.Status = "running"
			execution.Status = "running"
			if err := p.persistServerSetupExecution(ctx, *execution); err != nil {
				return false, err
			}
		}
		var done bool
		var err error
		if step.Kind == "dns_publisher" {
			done, err = p.runServerSetupDNSPublisher(ctx, plan, execution.ID)
		} else {
			done, err = p.runServerSetupStep(ctx, plan, step)
		}
		if errors.Is(err, errServerSetupDNSPublisherRequired) || errors.Is(err, errServerSetupDNSReadinessRequired) {
			execution.Status = "waiting"
			execution.Phase = step.Kind
			execution.Error = &serviceOperationError{Code: "server_setup_" + step.Kind + "_required", Message: "Finish the DNS connection shown in this setup flow to continue. Completed operations and existing services are preserved."}
			return false, p.persistServerSetupExecution(ctx, *execution)
		}
		if errors.Is(err, errServerSetupLicenseRequired) {
			execution.Status = "waiting"
			execution.Phase = "license"
			execution.Error = &serviceOperationError{Code: "license_required", Message: "Activate the license to continue the remaining setup steps. Existing services keep running."}
			return false, p.persistServerSetupExecution(ctx, *execution)
		}
		if errors.Is(err, errServerSetupDNSReconciliationRequired) {
			execution.Status = "running"
			execution.Error = &serviceOperationError{Code: "server_setup_reconciling", Message: "The previous DNS operation is being reconciled. Its exact receipt must be verified before the setup plan can change."}
			return false, p.persistServerSetupExecution(ctx, *execution)
		}
		if errors.Is(err, errServiceOperationBusy) {
			return false, nil
		}
		if err != nil {
			// A failed transport reply cannot prove that the admitted child did
			// not commit. Keep its exact identity and reconcile before a retry.
			active, observeErr := p.activeServiceOperation(ctx)
			job, agentErr := p.statusAgentMutation(ctx, "")
			if observeErr != nil || agentErr != nil || active != nil || (job != nil && agentMutationActive(job.Status)) {
				execution.Error = &serviceOperationError{Code: "server_setup_reconciling", Message: "The previous operation is being reconciled. Its saved identity will be checked before continuing."}
				return false, p.persistServerSetupExecution(ctx, *execution)
			}
			step.Status = "failed"
			execution.Status = "failed"
			execution.Error = serverSetupFailureForStep(*step, err)
			log.Printf("server setup step %s failed: %v", step.ID, err)
			if persistErr := p.persistServerSetupExecution(ctx, *execution); persistErr != nil {
				return false, persistErr
			}
			p.releaseFailedServerSetupDraft(ctx, plan)
			return false, nil
		}
		execution.Status = "running"
		execution.Error = nil
		if !done {
			return false, p.persistServerSetupExecution(ctx, *execution)
		}
		step.Status = "succeeded"
		execution.Error = nil
		if err := p.persistServerSetupExecution(ctx, *execution); err != nil {
			return false, err
		}
		return true, nil
	}
	checks, err := p.serverSetupCompletionChecks(ctx, plan.Draft)
	if err != nil {
		return false, err
	}
	execution.Checks = checks
	for _, check := range checks {
		if check.State != "ready" {
			execution.Status = "waiting"
			execution.Phase = "verification"
			return false, p.persistServerSetupExecution(ctx, *execution)
		}
	}
	if err := p.completeServerSetupExecution(ctx, plan.Revision, execution.ID); err != nil {
		return false, err
	}
	execution.Status = "succeeded"
	execution.Phase = "complete"
	return true, p.persistServerSetupExecution(ctx, *execution)
}

func (p *Panel) runServerSetupStep(ctx context.Context, plan serverSetupPlan, step *serverSetupExecutionStep) (bool, error) {
	switch step.Kind {
	case "verify":
		return true, nil
	case "dns_readiness":
		if err := p.requireServerSetupAdmission(); err != nil {
			return false, err
		}
		if serverSetupManualSecondaryHosting(plan.Draft) {
			ready, err := p.serverSetupManualSecondaryHostingReadiness(ctx, plan.Draft)
			if err != nil || !ready {
				return false, errServerSetupDNSReadinessRequired
			}
			return true, nil
		}
		if _, err := p.remoteDNSLocalAuthority(ctx); err != nil {
			return false, errServerSetupDNSReadinessRequired
		}
		return true, nil
	case "dns":
		if plan.Draft.DNSMode == "external" {
			if err := p.requireServerSetupAdmission(); err != nil {
				return false, err
			}
			return true, p.saveSetupDNSManagementMode(ctx, "external")
		}

		if plan.Draft.DNSMode == setupDNSModeExisting {
			if err := p.requireServerSetupAdmission(); err != nil {
				return false, err
			}
			if plan.RemoteDNSConnection == nil || plan.RemoteDNSConnection.ID != plan.Draft.RemoteDNSConnectionID {
				return false, errors.New("reviewed remote DNS identity is missing")
			}
			authority, err := p.remoteDNSConnectionReadiness(ctx, plan.Draft.RemoteDNSConnectionID)
			if err != nil || !authority.Ready {
				return false, errors.New("reviewed remote DNS authority is unavailable")
			}
			names := append([]string(nil), authority.Nameservers...)
			slices.Sort(names)
			var endpoint string
			if err := p.db.GetDB().QueryRowContext(ctx, `SELECT endpoint FROM remote_dns_connections WHERE id=? AND status='ready'`, plan.Draft.RemoteDNSConnectionID).Scan(&endpoint); err != nil {
				return false, err
			}
			if endpoint != plan.RemoteDNSConnection.Endpoint || !slices.Equal(names, plan.RemoteDNSConnection.Nameservers) {
				return false, errors.New("remote DNS identity changed after review")
			}
			return true, p.saveSetupRemoteDNSConnection(ctx, plan.Draft.RemoteDNSConnectionID)
		}
		state, err := p.serverSetupDNSOperationStatus(ctx, step.RequestID)
		if err != nil {
			return false, setupDNSReconciliationError(err)
		}
		if state == "missing" {
			if err := p.requireServerSetupAdmission(); err != nil {
				return false, err
			}
		}
		return true, p.startServerSetupDNS(ctx, plan.Draft, step.RequestID, plan.Actor)
	case "mail_certificate":
		return p.runServerSetupMailCertificate(ctx, plan, *step)
	case "firewall":
		return p.runServerSetupFirewall(ctx, plan, *step)
	case "service", "runtime", "mail_profile", "panel_certificate":
		op, err := p.ensureServerSetupChild(ctx, plan, *step)
		if err != nil {
			return false, err
		}
		step.OperationID = op.ID
		switch op.Status {
		case serviceOperationSucceeded:
			return true, nil
		case serviceOperationFailed:
			if op.Error != nil {
				return false, &serverSetupChildFailure{Code: op.Error.Code, Message: op.Error.Message}
			}
			return false, errors.New("setup child failed")
		default:
			return false, nil
		}
	default:
		return false, errors.New("unknown persisted setup step")
	}
}

func (p *Panel) ensureServerSetupChild(ctx context.Context, plan serverSetupPlan, step serverSetupExecutionStep) (serviceOperation, error) {
	kind := serviceOperationKindInstall
	switch step.Kind {
	case "runtime":
		kind = serviceOperationKindRuntimeInstall
	case "mail_profile":
		kind = serviceOperationKindMailProfileInstall
	case "panel_certificate":
		kind = serviceOperationKindPanelCertificate
	}
	qualifier := step.Qualifier
	var certificate mutationpayload.PanelCertificateIssueCommitment
	if step.Kind == "panel_certificate" {
		var err error
		certificate, err = mutationpayload.CanonicalPanelCertificateIssue(plan.Draft.PanelDomain, plan.ContactEmail, tlsDir(), plan.BuildCommit)
		if err != nil {
			return serviceOperation{}, err
		}
		qualifier = certificate.Qualifier
	}
	prior, found, err := p.idempotentServiceOperation(ctx, step.RequestID, kind, step.Target, qualifier)
	if err != nil || found {
		return prior, err
	}
	if err := p.requireServerSetupAdmission(); err != nil {
		return serviceOperation{}, err
	}
	if plan.BuildCommit != "" && plan.BuildCommit != strings.TrimSpace(buildCommit) {
		return serviceOperation{}, errServerSetupBuildChanged
	}
	if _, degraded := p.subsystemDegraded(degradedSubsystemServiceOperations); degraded {
		return serviceOperation{}, errors.New("service operation recovery is required")
	}
	if !p.serviceMutationMu.TryLock() {
		return serviceOperation{}, errServiceOperationBusy
	}
	release := p.serviceMutationMu.Unlock
	transferred := false
	defer func() {
		if !transferred {
			release()
		}
	}()
	if active, err := p.activeServiceOperation(ctx); err != nil {
		return serviceOperation{}, err
	} else if active != nil {
		return serviceOperation{}, errServiceOperationBusy
	}
	job, err := p.statusAgentMutation(ctx, "")
	if err != nil {
		return serviceOperation{}, err
	}
	if job != nil && agentMutationActive(job.Status) {
		return serviceOperation{}, errServiceOperationBusy
	}
	if step.Kind == "panel_certificate" {
		if _, _, blocked := panelCertificateManagementBlocker(); blocked {
			return serviceOperation{}, errors.New("panel certificate path is unmanaged")
		}
		if err := p.requireMatchingAgentBuild(ctx); err != nil {
			return serviceOperation{}, err
		}
		if err := p.requirePanelCertificateSagaAgentCapabilities(ctx); err != nil {
			return serviceOperation{}, err
		}
		for _, method := range []string{"Agent.ApplyFirewallV2", "Agent.IssuePanelCertificateV2"} {
			if err := p.authorizeAgentRPCContext(ctx, method); err != nil {
				return serviceOperation{}, err
			}
		}
		identity := serviceOperation{RequestID: step.RequestID, Kind: kind, ServiceID: step.Target, PackageName: qualifier, Status: serviceOperationQueued, Phase: panelCertificatePhaseQueued}
		data, err := canonicalPanelCertificateSagaData(identity, newPanelCertificateSagaData(certificate))
		if err != nil {
			return serviceOperation{}, err
		}
		op, err := p.createServiceOperationRequestWithState(ctx, kind, step.Target, qualifier, step.RequestID, plan.Actor, panelCertificatePhaseQueued, data)
		if errors.Is(err, errServiceOperationReplay) {
			return op, nil
		}
		if err != nil {
			return serviceOperation{}, err
		}
		p.launchPanelCertificateSaga(op, plan.Actor, release)
		transferred = true
		return op, nil
	}
	if step.Kind == "mail_profile" {
		if err := p.authorizeAgentRPCContext(ctx, "Agent.SyncMailTLSV2"); err != nil {
			return serviceOperation{}, err
		}
		if err := p.requireMailTLSSyncV2Agent(ctx); err != nil {
			return serviceOperation{}, err
		}
		if err := p.setSetting(ctx, settingMailHostname, plan.Draft.MailHostname); err != nil {
			return serviceOperation{}, err
		}
	}
	operationData := ""
	if step.Kind == "mail_profile" {
		raw, _ := json.Marshal(serverSetupMailChild{MailHostname: plan.Draft.MailHostname})
		operationData = string(raw)
	}
	op, err := p.createServiceOperationRequestWithState(ctx, kind, step.Target, qualifier, step.RequestID, plan.Actor, "queued", operationData)
	if errors.Is(err, errServiceOperationReplay) {
		return op, nil
	}
	if err != nil {
		return serviceOperation{}, err
	}
	var runner serviceOperationRunner
	switch step.Kind {
	case "service":
		runner = func(ctx context.Context, advance func(string) error) (serviceOperationResult, *serviceOperationFailure) {
			return p.runServiceInstall(ctx, serviceInstallRequest{ServiceID: step.Target, Package: step.Qualifier, RequestID: step.RequestID}, advance)
		}
	case "runtime":
		runner = func(ctx context.Context, advance func(string) error) (serviceOperationResult, *serviceOperationFailure) {
			return p.runNodeInstall(ctx, step.Qualifier, advance)
		}
	case "mail_profile":
		runner = func(ctx context.Context, advance func(string) error) (serviceOperationResult, *serviceOperationFailure) {
			return p.runMailProfileInstall(context.WithValue(ctx, serverSetupMailHostnameKey{}, plan.Draft.MailHostname), step.Target, advance)
		}
	default:
		return serviceOperation{}, errors.New("unknown service setup child")
	}
	p.launchServiceOperation(op, plan.Actor, "queued", "server.setup.child:"+step.Target, "server.setup.child.failed:"+step.Target, release, runner)
	transferred = true
	return op, nil
}

func (p *Panel) runServerSetupFirewall(ctx context.Context, plan serverSetupPlan, step serverSetupExecutionStep) (bool, error) {
	commitment, err := mutationpayload.CanonicalFirewallApply(true, true, plan.TCPPorts, plan.UDPPorts)
	if err != nil {
		return false, err
	}
	job, err := p.statusAgentMutation(ctx, step.RequestID)
	if err != nil {
		return false, err
	}
	if job != nil && agentMutationActive(job.Status) {
		return false, errServiceOperationBusy
	}
	if job != nil && job.Status == agentMutationFailed {
		return false, errors.New("interrupted firewall operation requires a new reviewed attempt")
	}
	if job != nil && job.Status == agentMutationSucceeded {
		identity := agentMutationIdentityForOperation(serviceOperation{RequestID: step.RequestID, Kind: "firewall_apply", ServiceID: "nftables", PackageName: commitment.Qualifier}, step.OwnerID)
		if err := validateAgentMutationSucceededReceipt(job, identity); err != nil {
			return false, err
		}
		return true, nil
	}
	if !p.serviceMutationMu.TryLock() {
		return false, errServiceOperationBusy
	}
	defer p.serviceMutationMu.Unlock()
	if active, err := p.activeServiceOperation(ctx); err != nil {
		return false, err
	} else if active != nil {
		return false, errServiceOperationBusy
	}
	panelFirewallMu.Lock()
	defer panelFirewallMu.Unlock()
	var current FirewallStatusResp
	if err := p.callAgentContext(ctx, "Agent.FirewallStatus", &transport.Empty{}, &current); err != nil {
		return false, err
	}
	if current.Error != "" {
		return false, errors.New(current.Error)
	}
	if !current.EngineAvailable {
		return false, errFirewallNoEngine
	}
	if err := firewallSSHDiscoveryError(current.SSHDiscoveryReason); err != nil {
		return false, err
	}
	if len(current.SSHPorts) == 0 {
		return false, errFirewallSSHUnprovable
	}
	currentTCP, currentUDP, err := p.desiredFirewallPorts(80)
	if err != nil {
		return false, err
	}
	for _, port := range append(currentTCP, current.TCPPorts...) {
		if !slices.Contains(plan.TCPPorts, port) && !slices.Contains(current.SSHPorts, port) {
			return false, errors.New("firewall requirements changed after plan review")
		}
	}
	for _, port := range append(currentUDP, current.UDPPorts...) {
		if !slices.Contains(plan.UDPPorts, port) {
			return false, errors.New("firewall requirements changed after plan review")
		}
	}

	if err := p.requireServerSetupAdmission(); err != nil {
		return false, err
	}
	response, err := p.applyCanonicalFirewallV2Identity(ctx, "firewall_apply", commitment, step.RequestID, step.OwnerID)
	if err != nil {
		return false, err
	}
	if response.Error != "" || !response.Enabled {
		return false, errors.New("firewall application was not verified")
	}
	return true, nil
}

// A new review is permitted after a proven terminal failure only. If the agent
// cannot be observed, preserve the running draft: uncertainty is not authority
// to start a second mutation or to change the plan beneath an interrupted one.
func (p *Panel) releaseFailedServerSetupDraft(ctx context.Context, plan serverSetupPlan) {
	p.serverSetupMu.Lock()
	defer p.serverSetupMu.Unlock()
	active, err := p.activeServiceOperation(ctx)
	if err != nil || active != nil {
		return
	}
	job, err := p.statusAgentMutation(ctx, "")
	if err != nil || (job != nil && agentMutationActive(job.Status)) {
		return
	}
	_, err = p.db.GetDB().ExecContext(ctx, `UPDATE server_setup_state SET status='draft',updated_at=? WHERE id=1 AND revision=? AND status='running'`, time.Now().UTC().Format(time.RFC3339Nano), plan.Revision)
	if err != nil {
		log.Printf("release failed setup draft: %v", err)
	}
}

type serverSetupMailHostnameKey struct{}
type serverSetupMailChild struct {
	MailHostname string `json:"setup_mail_hostname"`
}

func decodeServerSetupMailChild(op serviceOperation) (string, error) {
	if op.OperationData == "" {
		return "", nil
	}
	var child serverSetupMailChild
	if err := json.Unmarshal([]byte(op.OperationData), &child); err != nil {
		return "", err
	}
	canonical, err := hostname.CanonicalFQDN(child.MailHostname)
	if err != nil || canonical != child.MailHostname {
		return "", errors.New("saved setup mail hostname is invalid")
	}
	return canonical, nil
}

func validateServerSetupExecution(plan serverSetupPlan, execution serverSetupExecution) error {
	if execution.PlanID != plan.ID || execution.ID != execution.RequestID || !validServiceOperationID(execution.ID) || len(execution.Steps) != len(plan.Steps) {
		return errors.New("saved setup execution does not match its reviewed plan")
	}
	for index, step := range execution.Steps {
		if step.serverSetupPlanStep != plan.Steps[index] || step.RequestID != serverSetupID(execution.ID, step.ID, "request") || step.OwnerID != serverSetupID(execution.ID, step.ID, "owner") {
			return errors.New("saved setup child does not match its reviewed identity")
		}
		if !slices.Contains([]string{"pending", "running", "succeeded", "failed"}, step.Status) {
			return errors.New("saved setup child status is invalid")
		}
	}
	return nil
}

type serverSetupChildFailure struct{ Code, Message string }

func (e *serverSetupChildFailure) Error() string { return e.Code + ": " + e.Message }

func serverSetupFailureForStep(step serverSetupExecutionStep, cause error) *serviceOperationError {
	var child *serverSetupChildFailure
	if errors.As(cause, &child) {
		return &serviceOperationError{Code: child.Code, Message: child.Message}
	}
	switch {
	case errors.Is(cause, errServerSetupBuildChanged):
		return &serviceOperationError{Code: "server_setup_build_changed", Message: "The panel version changed after this setup plan was reviewed. Completed operations are preserved. Review and confirm a new plan before starting the remaining setup steps."}
	case errors.Is(cause, errFirewallNoSSHService):
		return &serviceOperationError{Code: "firewall_no_ssh_service", Message: "No SSH service is available. Configure a verified management access path before enabling the firewall."}
	case errors.Is(cause, errFirewallSSHNotListening):
		return &serviceOperationError{Code: "firewall_ssh_not_listening", Message: "SSH is installed but has no verified listening port. Restore SSH access before retrying."}
	case errors.Is(cause, errFirewallSSHUnprovable):
		return &serviceOperationError{Code: "firewall_ssh_unprovable", Message: "The SSH access port could not be verified. Review server access before retrying."}
	case errors.Is(cause, errFirewallNoEngine):
		return &serviceOperationError{Code: "firewall_no_engine", Message: "The firewall engine is unavailable. Review the component installation result."}
	}
	switch step.Kind {
	case "dns":
		return &serviceOperationError{Code: "server_setup_dns_failed", Message: "DNS setup requires attention. Verify the local address, nameserver identities, independent peer and the DNS operation result."}
	case "panel_certificate":
		return &serviceOperationError{Code: "server_setup_panel_certificate_failed", Message: "The panel certificate could not be verified. Check the panel domain DNS and HTTP validation access, then review the certificate operation."}
	case "firewall":
		return &serviceOperationError{Code: "server_setup_firewall_failed", Message: "Firewall requirements changed or application could not be verified. Review management access and the current service ports before retrying."}
	default:
		return &serviceOperationError{Code: "server_setup_step_failed", Message: "The setup step could not be completed. Review its component operation before retrying. Existing services are preserved."}
	}
}

var errServerSetupLicenseRequired = errors.New("setup requires an active license")
var errServerSetupBuildChanged = errors.New("setup build changed; review the remaining plan")

func (p *Panel) requireServerSetupAdmission() error {
	if p.license == nil || !p.license.Status().CanProvision {
		return errServerSetupLicenseRequired
	}
	return nil
}

// A new request may have committed between the old runner's terminal read and
// its defer. Recheck after releasing the per-panel runner token and hand off
// only to a different durable request, never spin on the same persistent error.
func (p *Panel) finishServerSetupRunner(lastID string) {
	serverSetupRunners.Delete(p)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	next, err := p.latestServerSetupExecution(ctx)
	if err == nil && next != nil && next.ID != lastID && (next.Status == "running" || next.Status == "waiting") {
		p.launchServerSetupExecution()
	}
}

func (p *Panel) runServerSetupMailCertificate(ctx context.Context, plan serverSetupPlan, step serverSetupExecutionStep) (bool, error) {
	commitment, err := mutationpayload.CanonicalMailHostCertificate(step.Target, plan.ContactEmail, plan.BuildCommit)
	if err != nil {
		return false, err
	}
	op := serviceOperation{RequestID: step.RequestID, Kind: "mail_host_certificate", ServiceID: step.Target, PackageName: commitment.Qualifier}
	identity := agentMutationIdentityForOperation(op, step.OwnerID)
	job, err := p.statusAgentMutation(ctx, step.RequestID)
	if err != nil {
		return false, err
	}
	if job != nil {
		if err := validateAgentMutationIdentity(job, identity); err != nil {
			return false, err
		}
		if job.Status == agentMutationSucceeded {
			return true, validateAgentMutationSucceededReceipt(job, identity)
		}
		if agentMutationActive(job.Status) {
			return false, nil
		}
		return false, errors.New("mail host certificate operation failed; review a new attempt")
	}
	if err := p.requireServerSetupAdmission(); err != nil {
		return false, err
	}
	if plan.BuildCommit != strings.TrimSpace(buildCommit) {
		return false, errServerSetupBuildChanged
	}
	if !p.serviceMutationMu.TryLock() {
		return false, errServiceOperationBusy
	}
	defer p.serviceMutationMu.Unlock()
	if active, err := p.activeServiceOperation(ctx); err != nil {
		return false, err
	} else if active != nil {
		return false, errServiceOperationBusy
	}
	if err := p.requireMatchingAgentBuild(ctx); err != nil {
		return false, err
	}
	var agent transport.AgentVersionResponse
	if err := p.callAgentContext(ctx, "Agent.Version", &transport.Empty{}, &agent); err != nil {
		return false, err
	}
	if err := requireKnownAgentCapabilities(agent.Capabilities, transport.AgentCapabilityMailHostCertificateV1); err != nil {
		return false, err
	}
	if err := p.authorizeAgentRPCContext(ctx, "Agent.IssueMailHostCertificateV1"); err != nil {
		return false, err
	}
	p.auditServiceOperation(ctx, plan.Actor, "server_setup_mail_certificate_requested")
	err = p.withStandaloneAgentMutationIdentity(ctx, op, step.OwnerID, func(bound context.Context, binding agentMutationBinding) error {
		var response transport.IssueMailHostCertificateResponse
		req := transport.IssueMailHostCertificateRequest{ServiceMutationBinding: binding, Domain: commitment.Domain, Email: commitment.Email, ExpectedBuildCommit: commitment.ExpectedBuildCommit}
		if err := p.callAgentContext(bound, "Agent.IssueMailHostCertificateV1", &req, &response); err != nil {
			return err
		}
		if response.Error != "" || !response.Issued {
			return errors.New("mail host certificate publication was not verified")
		}
		return nil
	})
	return err == nil, err
}
