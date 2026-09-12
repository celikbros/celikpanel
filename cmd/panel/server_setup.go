package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/hostname"
)

const serverSetupPath = "/api/v1/setup"

var errServerSetupConflict = errors.New("server setup changed; reload the current plan")

type serverSetupDraft struct {
	Purpose               string                    `json:"purpose"`
	DNSHostingManagement  string                    `json:"dns_hosting_management,omitempty"`
	DNSPublisherEndpoint  string                    `json:"dns_publisher_endpoint,omitempty"`
	RemoteDNSConnectionID string                    `json:"remote_dns_connection_id"`
	PanelDomain           string                    `json:"panel_domain"`
	MailHostname          string                    `json:"mail_hostname"`
	DNSMode               string                    `json:"dns_mode"`
	DNSEngine             string                    `json:"dns_engine"`
	DNSRole               string                    `json:"dns_role"`
	NS1                   string                    `json:"ns1"`
	NS2                   string                    `json:"ns2"`
	LocalIP               string                    `json:"local_ip"`
	PeerIP                string                    `json:"peer_ip"`
	PeerNS                string                    `json:"peer_ns"`
	NodeVersion           string                    `json:"node_version"`
	Database              string                    `json:"database"`
	Customization         *serverSetupCustomization `json:"customization,omitempty"`
}

type serverSetupCheck struct {
	ID    string `json:"id"`
	State string `json:"state"`
	Code  string `json:"code"`
}

type serverSetupState struct {
	Version     int                `json:"version"`
	Revision    int                `json:"revision"`
	Origin      string             `json:"origin"`
	Status      string             `json:"status"`
	Draft       serverSetupDraft   `json:"draft"`
	CompletedAt string             `json:"completed_at,omitempty"`
	Required    bool               `json:"required"`
	Guidance    string             `json:"guidance"`
	Checks      []serverSetupCheck `json:"checks"`
	ServerIP    string             `json:"server_ip,omitempty"`
}

func defaultServerSetupDraft() serverSetupDraft {
	return serverSetupDraft{Purpose: "web", DNSMode: "local", DNSEngine: "pdns", DNSRole: "primary", Database: "mariadb"}
}

// Drafts may be incomplete; review validates the complete executable plan.
// Taslak eksik olabilir; inceleme calistirilacak tam plani dogrular.
func canonicalServerSetupDraft(d serverSetupDraft) (serverSetupDraft, error) {
	for _, item := range []*string{&d.Purpose, &d.PanelDomain, &d.MailHostname, &d.DNSMode, &d.DNSEngine, &d.DNSRole, &d.NS1, &d.NS2, &d.LocalIP, &d.PeerIP, &d.PeerNS, &d.NodeVersion, &d.Database, &d.RemoteDNSConnectionID, &d.DNSPublisherEndpoint, &d.DNSHostingManagement} {
		*item = strings.TrimSpace(*item)
		if len(*item) > 253 {
			return d, errors.New("setup input is too long")
		}
	}
	if d.DNSEngine == "" {
		d.DNSEngine = "pdns"
	}
	if d.DNSRole == "" {
		d.DNSRole = "primary"
	}
	if !stringIn(d.Purpose, "web", "web_mail", "application", "dns", "custom") || !stringIn(d.DNSMode, "local", "existing", "external") || !stringIn(d.DNSEngine, "", "pdns", "bind") || !stringIn(d.DNSRole, "", "primary", "secondary") || !stringIn(d.Database, "", "mariadb", "postgresql") {
		return d, errors.New("invalid setup choice")
	}
	for _, value := range []*string{&d.PanelDomain, &d.MailHostname, &d.NS1, &d.NS2, &d.PeerNS} {
		if *value == "" {
			continue
		}
		name, err := hostname.CanonicalFQDN(*value)
		if err != nil {
			return d, errors.New("setup names must be fully qualified domain names")
		}
		*value = name
	}
	for _, value := range []*string{&d.LocalIP, &d.PeerIP} {
		if *value == "" {
			continue
		}
		ip := net.ParseIP(*value)
		if ip == nil || ip.IsUnspecified() || ip.IsMulticast() {
			return d, errors.New("invalid setup address")
		}
		*value = ip.String()
	}
	if !stringIn(d.DNSHostingManagement, "", "manual", "panel") {
		return d, errors.New("invalid hosting DNS management choice")
	}
	if d.DNSPublisherEndpoint != "" {
		endpoint, err := canonicalRemoteDNSEndpoint(d.DNSPublisherEndpoint)
		if err != nil {
			return d, errors.New("invalid DNS publisher HTTPS endpoint")
		}
		d.DNSPublisherEndpoint = endpoint
	}
	if d.RemoteDNSConnectionID != "" && !validServiceOperationID(d.RemoteDNSConnectionID) {
		return d, errors.New("invalid remote DNS connection")
	}
	for _, c := range d.NodeVersion {
		if !(c >= '0' && c <= '9') && c != '.' && c != 'v' {
			return d, errors.New("invalid runtime version")
		}
	}
	var err error
	d.Customization, err = canonicalServerSetupCustomization(d.Customization)
	if err != nil {
		return d, err
	}
	if d.Purpose == "custom" && d.Customization == nil {
		d.Customization = &serverSetupCustomization{Components: []string{}}
	}
	return d, nil
}

func stringIn(value string, choices ...string) bool {
	for _, choice := range choices {
		if value == choice {
			return true
		}
	}
	return false
}

func (p *Panel) loadServerSetup(ctx context.Context) (serverSetupState, error) {
	var s serverSetupState
	var raw string
	err := p.db.GetDB().QueryRowContext(ctx, `SELECT version,revision,origin,status,draft_json,completed_at,COALESCE((SELECT value FROM panel_settings WHERE key='server_setup_guidance'),'') FROM server_setup_state WHERE id=1`).Scan(&s.Version, &s.Revision, &s.Origin, &s.Status, &raw, &s.CompletedAt, &s.Guidance)
	if err != nil {
		return s, fmt.Errorf("read server setup: %w", err)
	}
	if s.Version != 1 || s.Revision < 0 || !stringIn(s.Origin, "fresh", "legacy") || !stringIn(s.Status, "new", "legacy", "draft", "running", "waiting", "failed", "ready") {
		return s, errors.New("invalid persisted setup state")
	}
	s.Draft = defaultServerSetupDraft()
	if err := json.Unmarshal([]byte(raw), &s.Draft); err != nil {
		return s, errors.New("invalid persisted setup draft")
	}
	s.Draft, err = canonicalServerSetupDraft(s.Draft)
	if err != nil {
		return s, err
	}
	s.Required = s.Status != "legacy" && s.Status != "ready"
	if s.Guidance == "" {
		s.Guidance = "guided"
		if stringIn(s.Status, "new", "legacy") {
			s.Guidance = "undecided"
		}
	}
	if !stringIn(s.Guidance, "undecided", "guided", "manual") {
		return s, errors.New("invalid persisted setup guidance")
	}
	s.Checks = []serverSetupCheck{}
	return s, nil
}

func (p *Panel) saveServerSetupDraft(ctx context.Context, revision int, draft serverSetupDraft) (serverSetupState, error) {
	p.serverSetupMu.Lock()
	defer p.serverSetupMu.Unlock()
	canonical, err := canonicalServerSetupDraft(draft)
	if err != nil {
		return serverSetupState{}, err
	}
	active, err := p.serverSetupExecutionActive(ctx)
	if err != nil {
		return serverSetupState{}, err
	}
	if active {
		return serverSetupState{}, errServerSetupConflict
	}
	raw, err := json.Marshal(canonical)
	if err != nil {
		return serverSetupState{}, err
	}
	// The state CAS also protects against another panel process starting the
	// reviewed plan. / CAS baska panel isleminin ayni plani baslatmasini da korur.
	result, err := p.db.GetDB().ExecContext(ctx, `UPDATE server_setup_state SET draft_json=?,revision=revision+1,status='draft',updated_at=datetime('now') WHERE id=1 AND revision=? AND status IN ('new','legacy','draft','failed','ready')`, string(raw), revision)
	if err != nil {
		return serverSetupState{}, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return serverSetupState{}, err
	}
	if count != 1 {
		return serverSetupState{}, errServerSetupConflict
	}
	return p.loadServerSetup(ctx)
}

func serverSetupChecksReady(checks []serverSetupCheck) bool {
	if len(checks) == 0 {
		return false
	}
	for _, check := range checks {
		if check.State != "ready" {
			return false
		}
	}
	return true
}

func (p *Panel) completeServerSetup(ctx context.Context, revision int) error {
	return p.completeServerSetupOwned(ctx, revision, "")
}

func (p *Panel) completeServerSetupExecution(ctx context.Context, revision int, executionID string) error {
	if !validServiceOperationID(executionID) {
		return errServerSetupConflict
	}
	return p.completeServerSetupOwned(ctx, revision, executionID)
}

func (p *Panel) completeServerSetupOwned(ctx context.Context, revision int, executionID string) error {
	p.serverSetupMu.Lock()
	defer p.serverSetupMu.Unlock()
	s, err := p.loadServerSetup(ctx)
	if err != nil {
		return err
	}
	if s.Revision != revision {
		return errServerSetupConflict
	}
	if s.Status == "ready" {
		return nil
	}
	active, err := p.serverSetupExecutionActive(ctx)
	if err != nil {
		return err
	}
	if executionID != "" {
		var own int
		err := p.db.GetDB().QueryRowContext(ctx, `SELECT count(*) FROM server_setup_executions e JOIN server_setup_plans p ON p.id=e.plan_id WHERE e.id=? AND e.status IN ('running','waiting') AND p.revision=?`, executionID, revision).Scan(&own)
		if err != nil {
			return err
		}
		if own != 1 {
			return errServerSetupConflict
		}
		var children int
		if err := p.db.GetDB().QueryRowContext(ctx, `SELECT count(*) FROM service_operations WHERE status IN ('queued','running')`).Scan(&children); err != nil {
			return err
		}
		if children != 0 {
			return errServerSetupConflict
		}
	} else if active {
		return errServerSetupConflict
	}
	checks, err := p.serverSetupCompletionChecks(ctx, s.Draft)
	if err != nil {
		return err
	}
	if !serverSetupChecksReady(checks) {
		return errors.New("setup prerequisites are not ready")
	}
	// Recheck ownership in the committing statement: another panel process
	// may have admitted an execution while readiness was being read.
	result, err := p.db.GetDB().ExecContext(ctx, `UPDATE server_setup_state SET status='ready',completed_at=?,updated_at=datetime('now')
		WHERE id=1 AND revision=?
		AND NOT EXISTS (SELECT 1 FROM server_setup_executions WHERE status IN ('running','waiting') AND id != ?)
		AND NOT EXISTS (SELECT 1 FROM service_operations WHERE status IN ('queued','running'))`, time.Now().UTC().Format(time.RFC3339), revision, executionID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return errServerSetupConflict
	}
	return nil
}

func requireServerSetupAdmin(w http.ResponseWriter, r *http.Request) bool {
	c := currentCaller(r)
	if c == nil || !c.hasAccountRole(roleAdmin) {
		writeCodedError(w, http.StatusForbidden, errCodeAdminOnly, "administrator access required", "")
		return false
	}
	return true
}

func (p *Panel) handleServerSetup(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if !requireServerSetupAdmin(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		s, err := p.loadServerSetup(r.Context())
		if err != nil {
			writeServerError(w, err)
			return
		}
		if address := net.ParseIP(serverPrimaryIP()); address != nil && address.IsGlobalUnicast() && !address.IsPrivate() && !address.IsLoopback() {
			s.ServerIP = address.String()
		}
		if r.URL.Query().Get("check") == "1" {
			ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
			defer cancel()
			s.Checks, err = p.serverSetupCompletionChecks(ctx, s.Draft)
			if err != nil {
				writeServerError(w, err)
				return
			}
		}
		json.NewEncoder(w).Encode(s)
	case http.MethodPut:
		var req struct {
			Revision int              `json:"revision"`
			Draft    serverSetupDraft `json:"draft"`
		}
		if err := decodeServiceOperationJSON(w, r, &req); err != nil {
			writeClientError(w, http.StatusBadRequest, "invalid setup draft")
			return
		}
		if _, err := canonicalServerSetupDraft(req.Draft); err != nil {
			writeClientError(w, http.StatusBadRequest, err.Error())
			return
		}
		s, err := p.saveServerSetupDraft(r.Context(), req.Revision, req.Draft)
		if errors.Is(err, errServerSetupConflict) {
			writeCodedError(w, http.StatusConflict, "setup_conflict", err.Error(), "/setup")
			return
		}
		if err != nil {
			writeServerError(w, err)
			return
		}
		p.audit(r, "server.setup.draft", "", 0)
		json.NewEncoder(w).Encode(s)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (p *Panel) handleServerSetupComplete(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if !requireServerSetupAdmin(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Revision int `json:"revision"`
	}
	if err := decodeServiceOperationJSON(w, r, &req); err != nil {
		writeClientError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if err := p.completeServerSetup(r.Context(), req.Revision); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeServerError(w, err)
			return
		}
		writeCodedError(w, http.StatusConflict, "setup_not_ready", "complete the outstanding setup checks before continuing", "/setup")
		return
	}
	p.audit(r, "server.setup.complete", "", 0)
	state, err := p.loadServerSetup(r.Context())
	if err != nil {
		writeServerError(w, err)
		return
	}
	json.NewEncoder(w).Encode(state)
}
