package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/services"
	"github.com/alicelik/celikpanel/internal/transport"
)

// A site configuration file the owner changed is kept and named, never
// overwritten by a render (D-031, 10 Oct 2026; D-022; D-024; D-025 invariants
// 1, 2, 4 and 6).
//
// Every render of a site's vhost (start, setting, certificate, hosting change,
// PHP switch, creation, import) goes through the Agent's classifier. A file
// that is not CelikPanel's unchanged text is kept; the render's change is not
// applied to nginx and the operation says so with a typed refusal
// (SITE_CONFIG_OWNER_EDITED, SITE_CONFIG_MISSING, SITE_CONFIG_UNWRITABLE). The
// domain screen reads the file's state and the difference
// (GET /api/v1/domains/{id}/site-config) and offers the owner's three actions:
// keep mine, take CelikPanel's (a dated copy of the owner's file is kept
// beside it), or merge by hand in the file (the Panel's text is held in
// <file>.celikpanel-pending). A removed file is offered "recreate".
//
// Sahibin değiştirdiği site yapılandırma dosyası korunur ve adlandırılır;
// hiçbir üretim onun üzerine yazmaz. İşlem bunu tipli bir retle söyler; alan
// adı ekranı durumu ve farkı okur ve sahibin üç seçeneğini sunar.

const (
	errCodeSiteConfigOwnerEdited   = "SITE_CONFIG_OWNER_EDITED"
	errCodeSiteConfigMissing       = "SITE_CONFIG_MISSING"
	errCodeSiteConfigUnwritable    = "SITE_CONFIG_UNWRITABLE"
	errCodeSiteConfigExists        = "SITE_CONFIG_EXISTS"
	errCodeSiteConfigChanged       = "SITE_CONFIG_CHANGED"
	errCodeSiteConfigNotApplicable = "SITE_CONFIG_NOT_APPLICABLE"
	errCodeSiteConfigNotRead       = "SITE_CONFIG_NOT_READ"
	errCodeSiteConfigNginxRefused  = "SITE_CONFIG_NGINX_REFUSED"

	siteConfigReasonAgentDoesNotReport = "agent_does_not_report"
	siteConfigReasonRenderInput        = "render_input_unavailable"

	siteConfigActionTimeout = 5 * time.Minute
)

// The English originals of the refusals; the screens' sentences, in English
// and Turkish, are in web/src/i18n (err.<CODE>) and in
// docs/OPERATION-GUIDANCE.md.
const (
	siteConfigOwnerEditedMessage = "The site's nginx configuration file is not CelikPanel's unchanged text, so CelikPanel kept it and did not apply this change to it. " +
		"Nothing else was changed. Open the domain's Configuration file section: keep your file, take CelikPanel's text (your file is kept as a dated copy), or merge the two by hand."
	siteConfigMissingMessage = "The site's nginx configuration file is missing, so this change was not applied and the file was not recreated. " +
		"If the file was removed on purpose, nothing needs doing; otherwise open the domain's Configuration file section and choose Recreate."
	siteConfigUnwritableMessage = "CelikPanel could not read or replace the site's nginx configuration file (for example, it is a link or it is locked against changes), so it kept the file as it is and did not apply this change. " +
		"The server owner checks the file on the server; the reason is shown in the domain's Configuration file section."
	siteConfigExistsMessage = "A configuration file for this name already exists on the server and was not written by CelikPanel, so CelikPanel kept it and did not create the site. " +
		"Nothing was left behind. Move or rename that file on the server if it is no longer used, then create the site again."
	siteConfigChangedMessage = "The configuration file or CelikPanel's text changed after the page showed them, so nothing was done. " +
		"Read the file's state again, then choose again."
	siteConfigNotApplicableMessage = "This choice does not apply to the file as it is now, so nothing was done. Read the file's state again."
	siteConfigNotReadMessage       = "CelikPanel could not read the state of this site's configuration file just now. This does not mean anything is wrong with the file, and nothing was changed. Try again."
	siteConfigNginxRefusedMessage  = "nginx refused the configuration with CelikPanel's text in it, so your file was put back exactly as it was and nginx keeps running with it. Nothing else was changed."
)

// siteFileHeldError is a render the Agent did not apply because of the file's
// state. It is a refusal with a reason, never a bare failure.
type siteFileHeldError struct {
	Domain string
	File   transport.SiteFileResult
}

func (e *siteFileHeldError) Error() string {
	return fmt.Sprintf("site configuration of %s not applied: %s (%s %s)",
		e.Domain, e.File.Outcome, e.File.State, e.File.Reason)
}

func classifySiteFileError(err error) (agentRPCPlatformErrorClassification, bool) {
	var exists *services.SiteConfigExistsError
	if errors.As(err, &exists) {
		return agentRPCPlatformErrorClassification{
			Status: http.StatusConflict, Code: errCodeSiteConfigExists, Message: siteConfigExistsMessage,
		}, true
	}
	var held *siteFileHeldError
	if !errors.As(err, &held) {
		return agentRPCPlatformErrorClassification{}, false
	}
	switch held.File.Outcome {
	case transport.SiteFileOutcomeKept:
		return agentRPCPlatformErrorClassification{
			Status: http.StatusConflict, Code: errCodeSiteConfigOwnerEdited,
			Message: siteConfigOwnerEditedMessage, Reason: held.File.State,
		}, true
	case transport.SiteFileOutcomeMissing:
		return agentRPCPlatformErrorClassification{
			Status: http.StatusConflict, Code: errCodeSiteConfigMissing, Message: siteConfigMissingMessage,
		}, true
	default:
		reason := held.File.Reason
		if reason == "" {
			reason = held.File.State
		}
		return agentRPCPlatformErrorClassification{
			Status: http.StatusConflict, Code: errCodeSiteConfigUnwritable,
			Message: siteConfigUnwritableMessage, Reason: reason,
		}, true
	}
}

func siteFileReleaseLabel() string {
	commit := strings.TrimSpace(buildCommit)
	if len(commit) > 12 {
		commit = commit[:12]
	}
	return strings.TrimSpace(buildVersion + " " + commit)
}

func siteFileApplied(outcome string) bool {
	switch outcome {
	case transport.SiteFileOutcomeWritten, transport.SiteFileOutcomeUnchanged,
		transport.SiteFileOutcomeRecreated, transport.SiteFileOutcomeTaken:
		return true
	}
	return false
}

// describeSiteFile is the journal's words for one site's result.
func describeSiteFile(file transport.SiteFileResult) string {
	text := file.Outcome
	switch file.Outcome {
	case transport.SiteFileOutcomeKept:
		text = "kept (" + strings.ReplaceAll(file.State, "_", "-") + "); CelikPanel's text held in " + file.PendingPath
		if file.PendingPath == "" {
			text = "kept (" + strings.ReplaceAll(file.State, "_", "-") + ")"
		}
	case transport.SiteFileOutcomeRefused:
		text = "kept, not replaced (" + file.State + " " + file.Reason + ")"
	case transport.SiteFileOutcomeMissing:
		text = "missing, not recreated"
	}
	if file.AdoptedFrom != "" {
		text += "; adopted the text of " + file.AdoptedFrom
	}
	if file.BackupPath != "" {
		text += "; owner's file kept as " + file.BackupPath
	}
	if file.Detail != "" {
		text += "; " + boundedAgentDiagnostic(file.Detail)
	}
	return text
}

// siteIDForDomain reads a hosted site's identity.
func (p *Panel) siteIDForDomain(ctx context.Context, domainID int) (int, string, error) {
	var siteID int
	var name string
	err := p.db.GetDB().QueryRowContext(ctx, `
		SELECT s.id, d.name FROM sites s JOIN domains d ON d.id = s.domain_id
		WHERE s.domain_id = ? AND COALESCE(s.project_type, 'php') <> 'dnsonly'`, domainID).Scan(&siteID, &name)
	return siteID, name, err
}

// recordedVhostSHA256 is the body digest the ledger last recorded; "" when
// the Panel never wrote the file under the header (or the ledger was lost).
func (p *Panel) recordedVhostSHA256(ctx context.Context, siteID int) string {
	record, found, err := services.LoadSiteFileRecord(ctx, p.db.GetDB(), siteID, transport.SiteFileKindNginxVhost)
	if err != nil || !found {
		return ""
	}
	return record.BodySHA256
}

// recordSiteFile stores and logs one site's result. A nil result is an Agent
// that did not report one: the state is unknown, never "unchanged".
func (p *Panel) recordSiteFile(ctx context.Context, siteID, domainID int, domain, trigger string, file *transport.SiteFileResult, unknownReason string) {
	if file == nil {
		path := services.SiteVhostPath(domain)
		log.Printf("site configuration %s (%s): state unknown (%s)", domain, trigger, unknownReason)
		if err := services.RecordSiteFileUnknown(ctx, p.db.GetDB(), siteID, domainID, path, unknownReason, 0); err != nil {
			log.Printf("site configuration %s: record the unknown state: %v", domain, err)
		}
		return
	}
	log.Printf("site configuration %s (%s): %s", domain, trigger, describeSiteFile(*file))
	if file.Path == "" {
		return
	}
	if err := services.RecordSiteFileResult(ctx, p.db.GetDB(), siteID, domainID, *file, siteFileReleaseLabel(), 0); err != nil {
		log.Printf("site configuration %s: record the result in the ledger: %v", domain, err)
	}
}

// applySiteFileRender is the one path of every single-site render.
func (p *Panel) applySiteFileRender(ctx context.Context, req applyVhostRPCRequest, trigger string) (*transport.SiteFileResult, error) {
	req.FileTrigger = trigger
	req.RecordedSHA256 = p.recordedVhostSHA256(ctx, req.SiteID)
	var resp transport.ApplyVhostResponse
	if err := p.callAgentContext(ctx, "Agent.ApplyVhost", &req, &resp); err != nil {
		return nil, err
	}
	p.recordSiteFile(ctx, req.SiteID, req.DomainID, req.Domain, trigger, resp.File, siteConfigReasonAgentDoesNotReport)
	if resp.File != nil {
		switch resp.File.Outcome {
		case transport.SiteFileOutcomeKept, transport.SiteFileOutcomeMissing, transport.SiteFileOutcomeRefused:
			return resp.File, &siteFileHeldError{Domain: req.Domain, File: *resp.File}
		}
	}
	if resp.Error != "" {
		return resp.File, errors.New(resp.Error)
	}
	return resp.File, nil
}

// siteConfigView is the domain screen's answer.
type siteConfigView struct {
	DomainID      int                   `json:"domain_id"`
	Domain        string                `json:"domain"`
	Kind          string                `json:"kind"`
	Path          string                `json:"path,omitempty"`
	State         string                `json:"state"`
	Reason        string                `json:"reason,omitempty"`
	Detail        string                `json:"detail,omitempty"`
	AdoptedFrom   string                `json:"adopted_from,omitempty"`
	IncludeDir    string                `json:"include_dir,omitempty"`
	Enabled       string                `json:"enabled,omitempty"`
	FileSHA256    string                `json:"file_sha256,omitempty"`
	RenderSHA256  string                `json:"render_sha256,omitempty"`
	PendingPath   string                `json:"pending_path,omitempty"`
	Diff          string                `json:"diff,omitempty"`
	DiffTruncated bool                  `json:"diff_truncated,omitempty"`
	Actions       []string              `json:"actions"`
	Decision      *siteConfigDecision   `json:"decision,omitempty"`
	Ledger        *siteConfigLedgerView `json:"ledger,omitempty"`
	Outcome       string                `json:"outcome,omitempty"`
	BackupPath    string                `json:"backup_path,omitempty"`
}

type siteConfigDecision struct {
	Kind      string `json:"kind"`
	DecidedAt string `json:"decided_at"`
	// Current: the file is still the one the decision was made on.
	Current bool `json:"current"`
}

type siteConfigLedgerView struct {
	WrittenRelease string `json:"written_release,omitempty"`
	WrittenAt      string `json:"written_at,omitempty"`
	ObservedAt     string `json:"observed_at,omitempty"`
	BackupPath     string `json:"backup_path,omitempty"`
}

func unixToAPI(value int64) string {
	if value <= 0 {
		return ""
	}
	return time.Unix(value, 0).UTC().Format(time.RFC3339)
}

func siteConfigActions(state string) []string {
	switch state {
	case transport.SiteFileOwnerEdited, transport.SiteFileForeign, transport.SiteFileUnknownOrigin:
		return []string{"keep", "take", "merge"}
	case transport.SiteFileAbsent:
		return []string{"recreate"}
	}
	return []string{}
}

func (p *Panel) siteConfigViewFor(ctx context.Context, siteID int, view siteConfigView) siteConfigView {
	record, found, err := services.LoadSiteFileRecord(ctx, p.db.GetDB(), siteID, transport.SiteFileKindNginxVhost)
	if err != nil || !found {
		return view
	}
	view.Ledger = &siteConfigLedgerView{
		WrittenRelease: record.WrittenRelease, WrittenAt: unixToAPI(record.WrittenAt),
		ObservedAt: unixToAPI(record.ObservedAt), BackupPath: record.BackupPath,
	}
	if record.Decision != "" {
		view.Decision = &siteConfigDecision{
			Kind: record.Decision, DecidedAt: unixToAPI(record.DecidedAt),
			Current: record.DecisionFileSHA256 != "" && record.DecisionFileSHA256 == view.FileSHA256,
		}
	}
	return view
}

// inspectSiteConfig asks the Agent for the file's state and the difference.
// It writes nothing, on the server or in the Panel's database.
func (p *Panel) inspectSiteConfig(ctx context.Context, domainID int) (siteConfigView, int, *transport.SiteFileResult, error) {
	siteID, domain, err := p.siteIDForDomain(ctx, domainID)
	if err != nil {
		return siteConfigView{}, 0, nil, err
	}
	view := siteConfigView{DomainID: domainID, Domain: domain, Kind: transport.SiteFileKindNginxVhost, Actions: []string{}}
	req, err := p.buildVhostRequest(ctx, domainID, nil)
	if err != nil {
		return view, siteID, nil, fmt.Errorf("%s: %w", siteConfigReasonRenderInput, err)
	}
	req.FileTrigger = transport.SiteFileTriggerChange
	req.RecordedSHA256 = p.recordedVhostSHA256(ctx, siteID)
	var resp transport.InspectSiteFileResponse
	if err := p.callAgentContext(ctx, "Agent.InspectSiteFile", &req, &resp); err != nil {
		if agentRPCMethodUnavailable(err, "Agent.InspectSiteFile") {
			view.State = transport.SiteFileStateUnknown
			view.Reason = siteConfigReasonAgentDoesNotReport
			return p.siteConfigViewFor(ctx, siteID, view), siteID, nil, nil
		}
		return view, siteID, nil, err
	}
	if resp.Error != "" {
		return view, siteID, nil, errors.New(resp.Error)
	}
	file := resp.File
	view.Path, view.State, view.Reason, view.Detail = file.Path, file.State, file.Reason, file.Detail
	view.AdoptedFrom, view.IncludeDir, view.Enabled = file.AdoptedFrom, file.IncludeDir, file.Enabled
	view.FileSHA256, view.RenderSHA256, view.PendingPath = file.FileSHA256, file.RenderSHA256, file.PendingPath
	view.Diff, view.DiffTruncated = resp.Diff, resp.DiffTruncated
	view.Actions = siteConfigActions(file.State)
	return p.siteConfigViewFor(ctx, siteID, view), siteID, &file, nil
}

type siteConfigActionRequest struct {
	FileSHA256   string `json:"file_sha256"`
	RenderSHA256 string `json:"render_sha256"`
}

func validSiteConfigDigest(value string) bool {
	return len(value) == 64 && strings.Trim(value, "0123456789abcdef") == ""
}

// handleDomainSiteConfig serves GET site-config and the three POST actions.
// The file's path and the difference name this server's files, so every one
// of them is for an administrator.
func (p *Panel) handleDomainSiteConfig(w http.ResponseWriter, r *http.Request, domainID int, action string) {
	caller := currentCaller(r)
	if caller == nil || !caller.hasAccountRole(roleAdmin) {
		writeClientError(w, http.StatusForbidden, "administrator access is required")
		return
	}
	switch action {
	case "":
		p.handleSiteConfigRead(w, r, domainID)
	case "keep":
		p.handleSiteConfigKeep(w, r, domainID)
	case "take":
		p.handleSiteConfigRender(w, r, domainID, transport.SiteFileTriggerTake)
	case "recreate":
		p.handleSiteConfigRender(w, r, domainID, transport.SiteFileTriggerRecreate)
	default:
		http.NotFound(w, r)
	}
}

func writeSiteConfigNotRead(w http.ResponseWriter, domain string, err error) {
	log.Printf("[502] site configuration %s: could not read its state: %v", domain, err)
	writeCodedError(w, http.StatusBadGateway, errCodeSiteConfigNotRead, siteConfigNotReadMessage, "")
}

func (p *Panel) handleSiteConfigRead(w http.ResponseWriter, r *http.Request, domainID int) {
	ctx, cancel := context.WithTimeout(r.Context(), agentRPCStandardReadTimeout)
	defer cancel()
	view, _, _, err := p.inspectSiteConfig(ctx, domainID)
	if errors.Is(err, sql.ErrNoRows) {
		writeClientError(w, http.StatusNotFound, "this domain has no hosted site")
		return
	}
	if err != nil {
		writeSiteConfigNotRead(w, view.Domain, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(view)
}

func decodeSiteConfigAction(w http.ResponseWriter, r *http.Request) (siteConfigActionRequest, bool) {
	var body siteConfigActionRequest
	if r.ContentLength != 0 {
		if err := decodeServiceConfigJSON(w, r, &body); err != nil {
			writeClientError(w, http.StatusBadRequest, "invalid request body")
			return body, false
		}
	}
	if (body.FileSHA256 != "" && !validSiteConfigDigest(body.FileSHA256)) ||
		(body.RenderSHA256 != "" && !validSiteConfigDigest(body.RenderSHA256)) {
		writeClientError(w, http.StatusBadRequest, "file_sha256 and render_sha256 are 64 lowercase hexadecimal characters")
		return body, false
	}
	return body, true
}

// handleSiteConfigKeep records "keep mine". The file is not touched, not even
// its header: rewriting the header would make the owner's text look like the
// Panel's unchanged text, and the next render would replace it.
func (p *Panel) handleSiteConfigKeep(w http.ResponseWriter, r *http.Request, domainID int) {
	body, ok := decodeSiteConfigAction(w, r)
	if !ok {
		return
	}
	if body.FileSHA256 == "" {
		writeClientError(w, http.StatusBadRequest, "file_sha256 is required")
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), siteConfigActionTimeout)
	defer cancel()
	view, siteID, file, err := p.inspectSiteConfig(ctx, domainID)
	if errors.Is(err, sql.ErrNoRows) {
		writeClientError(w, http.StatusNotFound, "this domain has no hosted site")
		return
	}
	if err != nil {
		writeSiteConfigNotRead(w, view.Domain, err)
		return
	}
	if file == nil || !containsString(view.Actions, "keep") {
		writeCodedError(w, http.StatusConflict, errCodeSiteConfigNotApplicable, siteConfigNotApplicableMessage, "")
		return
	}
	if file.FileSHA256 != body.FileSHA256 {
		writeCodedError(w, http.StatusConflict, errCodeSiteConfigChanged, siteConfigChangedMessage, "")
		return
	}
	observed := *file
	observed.Outcome = transport.SiteFileOutcomeKept
	if err := services.RecordSiteFileResult(ctx, p.db.GetDB(), siteID, domainID, observed, siteFileReleaseLabel(), 0); err != nil {
		writeServerError(w, err)
		return
	}
	if err := services.RecordSiteFileDecision(ctx, p.db.GetDB(), siteID, transport.SiteFileKindNginxVhost,
		file.Path, services.SiteFileDecisionKeepMine, file.FileSHA256, currentCaller(r).ID, 0); err != nil {
		writeServerError(w, err)
		return
	}
	log.Printf("site configuration %s: the owner chose to keep the file (sha256 %s)", view.Domain, file.FileSHA256)
	p.audit(r, "domain.site_config.keep", "domain", domainID)
	view = p.siteConfigViewFor(ctx, siteID, view)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(view)
}

// handleSiteConfigRender is "take CelikPanel's" and "recreate".
func (p *Panel) handleSiteConfigRender(w http.ResponseWriter, r *http.Request, domainID int, trigger string) {
	body, ok := decodeSiteConfigAction(w, r)
	if !ok {
		return
	}
	if trigger == transport.SiteFileTriggerTake && (body.FileSHA256 == "" || body.RenderSHA256 == "") {
		writeClientError(w, http.StatusBadRequest, "file_sha256 and render_sha256 are required")
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), siteConfigActionTimeout)
	defer cancel()
	siteID, domain, err := p.siteIDForDomain(ctx, domainID)
	if errors.Is(err, sql.ErrNoRows) {
		writeClientError(w, http.StatusNotFound, "this domain has no hosted site")
		return
	}
	if err != nil {
		writeServerError(w, err)
		return
	}
	req, err := p.buildVhostRequest(ctx, domainID, nil)
	if err != nil {
		writeSiteConfigNotRead(w, domain, err)
		return
	}
	req.ExpectedFileSHA256 = body.FileSHA256
	req.ExpectedRenderSHA256 = body.RenderSHA256
	file, err := p.applySiteFileRender(ctx, req, trigger)
	if file == nil {
		if err == nil {
			err = errors.New("the Agent did not report the file's state")
		}
		writeSiteConfigNotRead(w, domain, err)
		return
	}
	switch {
	case file.Outcome == transport.SiteFileOutcomeRefused && file.Reason == transport.SiteFileReasonChanged:
		writeCodedError(w, http.StatusConflict, errCodeSiteConfigChanged, siteConfigChangedMessage, "")
		return
	case file.Outcome == transport.SiteFileOutcomeFailed &&
		(file.Reason == transport.SiteFileReasonNginxRefused || file.Reason == transport.SiteFileReasonReloadFailed):
		body := apiErrorBody{Error: siteConfigNginxRefusedMessage, Code: errCodeSiteConfigNginxRefused, Reason: file.Reason}
		if file.Detail != "" {
			body.Details = []string{boundedAgentDiagnostic(file.Detail)}
		}
		log.Printf("[502] site configuration %s: %s: %v", domain, trigger, err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(body)
		return
	case !siteFileApplied(file.Outcome):
		if file.Outcome == transport.SiteFileOutcomeKept && trigger == transport.SiteFileTriggerRecreate {
			writeCodedError(w, http.StatusConflict, errCodeSiteConfigNotApplicable, siteConfigNotApplicableMessage, "")
			return
		}
		writeServerError(w, err)
		return
	}
	decision := services.SiteFileDecisionTake
	audit := "domain.site_config.take"
	if trigger == transport.SiteFileTriggerRecreate {
		decision = services.SiteFileDecisionRecreate
		audit = "domain.site_config.recreate"
	}
	if err := services.RecordSiteFileDecision(ctx, p.db.GetDB(), siteID, transport.SiteFileKindNginxVhost,
		file.Path, decision, file.FileSHA256, currentCaller(r).ID, 0); err != nil {
		log.Printf("site configuration %s: record the decision: %v", domain, err)
	}
	p.audit(r, audit, "domain", domainID)
	view := siteConfigView{
		DomainID: domainID, Domain: domain, Kind: transport.SiteFileKindNginxVhost,
		Path: file.Path, State: transport.SiteFileManagedUnchanged, IncludeDir: file.IncludeDir,
		Enabled: file.Enabled, FileSHA256: file.FileSHA256, RenderSHA256: file.RenderSHA256,
		Actions: []string{}, Outcome: file.Outcome, BackupPath: file.BackupPath,
	}
	view = p.siteConfigViewFor(ctx, siteID, view)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(view)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// domainSiteConfigSummary is the domains list's field for an administrator.
type domainSiteConfigSummary struct {
	State       string `json:"state"`
	AdoptedFrom string `json:"adopted_from,omitempty"`
	// KeptByChoice: the owner chose "keep mine" for the file as it is now.
	KeptByChoice bool `json:"kept_by_choice,omitempty"`
}

func (p *Panel) domainSiteConfigSummaries(ctx context.Context) (map[int]domainSiteConfigSummary, error) {
	rows, err := p.db.GetDB().QueryContext(ctx, `
		SELECT domain_id, state, adopted_from, decision, decision_file_sha256, file_sha256
		FROM managed_site_files WHERE kind = ? ORDER BY id`, transport.SiteFileKindNginxVhost)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	summaries := make(map[int]domainSiteConfigSummary)
	for rows.Next() {
		var domainID int
		var state, adopted, decision, decisionSHA, fileSHA string
		if err := rows.Scan(&domainID, &state, &adopted, &decision, &decisionSHA, &fileSHA); err != nil {
			return nil, err
		}
		summaries[domainID] = domainSiteConfigSummary{
			State: state, AdoptedFrom: adopted,
			KeptByChoice: decision == services.SiteFileDecisionKeepMine && decisionSHA != "" && decisionSHA == fileSHA,
		}
	}
	return summaries, rows.Err()
}

func siteConfigFor(summaries map[int]domainSiteConfigSummary, domainID int) *domainSiteConfigSummary {
	summary, ok := summaries[domainID]
	if !ok {
		return nil
	}
	return &summary
}
