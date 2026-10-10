package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/alicelik/celikpanel/internal/services"
	"github.com/alicelik/celikpanel/internal/transport"
)

// D-031 at the Panel: the ledger, the typed refusals of every render path,
// the start line, the domain screen's read and the owner's three choices, and
// the answers of an Agent that predates them.

type siteFileTestAgent struct {
	verifiedAPTAgentRPCFixture

	mu        sync.Mutex
	commit    string
	batch     func(*transport.ApplyVhostsRequest, *transport.ApplyVhostsResponse)
	single    func(*transport.ApplyVhostRequest, *transport.ApplyVhostResponse)
	inspect   func(*transport.ApplyVhostRequest, *transport.InspectSiteFileResponse)
	requests  []transport.ApplyVhostRequest
	batchSent [][]transport.ApplyVhostRequest
}

func (a *siteFileTestAgent) Version(_ *struct{}, response *StartupVhostBatchVersionResponse) error {
	response.Version, response.Commit = "test", a.commit
	return nil
}

func (a *siteFileTestAgent) ApplyVhosts(request *transport.ApplyVhostsRequest, response *transport.ApplyVhostsResponse) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.batchSent = append(a.batchSent, append([]transport.ApplyVhostRequest(nil), request.Vhosts...))
	a.batch(request, response)
	return nil
}

func (a *siteFileTestAgent) ApplyVhost(request *transport.ApplyVhostRequest, response *transport.ApplyVhostResponse) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.requests = append(a.requests, *request)
	a.single(request, response)
	return nil
}

func (a *siteFileTestAgent) InspectSiteFile(request *transport.ApplyVhostRequest, response *transport.InspectSiteFileResponse) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.requests = append(a.requests, *request)
	a.inspect(request, response)
	return nil
}

func siteFilePath(domain string) string { return "/etc/nginx/sites-available/" + domain + ".conf" }

func fileResult(domain, state, outcome string) transport.SiteFileResult {
	return transport.SiteFileResult{
		Kind: transport.SiteFileKindNginxVhost, Path: siteFilePath(domain), State: state, Outcome: outcome,
		RenderSHA256: strings.Repeat("b", 64), FileSHA256: strings.Repeat("c", 64),
	}
}

func captureSiteFileLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buffer bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&buffer)
	t.Cleanup(func() { log.SetOutput(previous) })
	return &buffer
}

func ledgerRow(t *testing.T, panel *Panel, domainID int) services.SiteFileRecord {
	t.Helper()
	siteID, _, err := panel.siteIDForDomain(context.Background(), domainID)
	if err != nil {
		t.Fatal(err)
	}
	record, found, err := services.LoadSiteFileRecord(context.Background(), panel.db.GetDB(), siteID, transport.SiteFileKindNginxVhost)
	if err != nil || !found {
		t.Fatalf("ledger row of domain %d: found=%v err=%v", domainID, found, err)
	}
	return record
}

func TestStartupRecordsEverySiteAndCountsThem(t *testing.T) {
	panel, subscriptionID := newStartupVhostBatchFixture(t)
	written := addStartupHostedDomain(t, panel, subscriptionID, "written.example", "static")
	kept := addStartupHostedDomain(t, panel, subscriptionID, "kept.example", "static")
	locked := addStartupHostedDomain(t, panel, subscriptionID, "locked.example", "static")
	unknown := addStartupHostedDomain(t, panel, subscriptionID, "legacy-edit.example", "static")
	// A site whose render input cannot be prepared (its certificate cannot be
	// inspected): its own failure, the others are still sent.
	broken := addStartupHostedDomain(t, panel, subscriptionID, "broken-input.example", "static")
	if _, err := panel.db.GetDB().Exec(`UPDATE sites SET ssl_enabled = 1, ssl_cert_path = '/x/fullchain.pem', ssl_key_path = '/x/privkey.pem' WHERE domain_id = ?`, broken); err != nil {
		t.Fatal(err)
	}
	agent := &siteFileTestAgent{batch: func(request *transport.ApplyVhostsRequest, response *transport.ApplyVhostsResponse) {
		for _, vhost := range request.Vhosts {
			if vhost.FileTrigger != transport.SiteFileTriggerStartup {
				panic("start item without the start trigger")
			}
			item := transport.SiteFileBatchItem{DomainID: vhost.DomainID, SiteID: vhost.SiteID}
			switch vhost.Domain {
			case "written.example":
				item.File = fileResult(vhost.Domain, transport.SiteFileManagedUnchanged, transport.SiteFileOutcomeWritten)
				item.File.AdoptedFrom = "v0.1.0-alpha.82"
				item.File.WrittenSHA256 = item.File.RenderSHA256
			case "kept.example":
				item.File = fileResult(vhost.Domain, transport.SiteFileOwnerEdited, transport.SiteFileOutcomeKept)
				item.File.PendingPath = siteFilePath(vhost.Domain) + ".celikpanel-pending"
			case "locked.example":
				item.File = fileResult(vhost.Domain, transport.SiteFileManagedUnchanged, transport.SiteFileOutcomeRefused)
				item.File.Reason = transport.SiteFileReasonWriteRefused
				item.File.Detail = "rename: operation not permitted"
			default:
				item.File = fileResult(vhost.Domain, transport.SiteFileUnknownOrigin, transport.SiteFileOutcomeKept)
			}
			response.Items = append(response.Items, item)
		}
		response.Counts = &transport.SiteFileCounts{Written: 1, Kept: 1, Unknown: 1, Unreadable: 1, Adopted: 1}
		response.Applied = 1
	}}
	attachStartupVhostBatchAgent(t, panel, agent)
	output := captureSiteFileLog(t)

	result, err := panel.reconcileHostedVhostsAtStartupDetailed(context.Background(), 10)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	logHostedVhostStartup(result)
	if len(agent.batchSent) != 1 || len(agent.batchSent[0]) != 4 {
		t.Fatalf("batch: %d items", len(agent.batchSent[0]))
	}
	if !result.notApplied[kept] || !result.notApplied[locked] || !result.notApplied[unknown] || !result.notApplied[broken] || result.notApplied[written] {
		t.Fatalf("not applied: %v", result.notApplied)
	}
	if row := ledgerRow(t, panel, written); row.State != transport.SiteFileManagedUnchanged || row.BodySHA256 != strings.Repeat("b", 64) ||
		row.AdoptedFrom != "v0.1.0-alpha.82" || row.FileFormat != services.ManagedRenderFormat || row.WrittenAt == 0 {
		t.Fatalf("written row: %+v", row)
	}
	if row := ledgerRow(t, panel, kept); row.State != transport.SiteFileOwnerEdited || row.PendingPath == "" || row.BodySHA256 != "" {
		t.Fatalf("kept row: %+v", row)
	}
	if row := ledgerRow(t, panel, locked); row.State != transport.SiteFileUnreadable || row.StateReason != transport.SiteFileReasonWriteRefused {
		t.Fatalf("locked row: %+v", row)
	}
	if row := ledgerRow(t, panel, broken); row.State != transport.SiteFileStateUnknown || row.StateReason != siteConfigReasonRenderInput {
		t.Fatalf("broken row: %+v", row)
	}
	text := output.String()
	for _, want := range []string{
		"site configuration written.example (startup): written; adopted the text of v0.1.0-alpha.82",
		"site configuration kept.example (startup): kept (owner-edited); CelikPanel's text held in /etc/nginx/sites-available/kept.example.conf.celikpanel-pending",
		"site configuration locked.example (startup): kept, not replaced (managed_unchanged write_refused); rename: operation not permitted",
		"site configuration broken-input.example (startup): not rendered",
		"site configuration files at start: 1 written, 0 unchanged, 1 kept (owner-edited), 0 kept (foreign), 1 kept (unknown origin), 1 unreadable or unwritable, 0 missing (not recreated), 1 failed; 1 adopted from an earlier release",
		"site configuration files at start: 1 adopted, 1 left alone because they differ from every known CelikPanel text",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("log lacks %q:\n%s", want, text)
		}
	}
}

// An Agent that predates D-031 answers the old way: the start goes on, and
// every file's state is recorded as unknown, never as unchanged.
func TestStartupWithAnOlderAgentRecordsUnknownState(t *testing.T) {
	panel, subscriptionID := newStartupVhostBatchFixture(t)
	domainID := addStartupHostedDomain(t, panel, subscriptionID, "older-agent.example", "static")
	attachStartupVhostBatchAgent(t, panel, &startupVhostBatchAgent{})
	output := captureSiteFileLog(t)
	result, err := panel.reconcileHostedVhostsAtStartupDetailed(context.Background(), 10)
	if err != nil || !result.agentLegacy || result.applied != 1 {
		t.Fatalf("older agent: %+v %v", result, err)
	}
	logHostedVhostStartup(result)
	if row := ledgerRow(t, panel, domainID); row.State != transport.SiteFileStateUnknown || row.StateReason != siteConfigReasonAgentDoesNotReport {
		t.Fatalf("row: %+v", row)
	}
	if !strings.Contains(output.String(), "their state is unknown") {
		t.Fatalf("log: %s", output.String())
	}
}

// Every single-site render path (settings, certificate, hosting, PHP) goes
// through applyVhostForDomain. A kept file is a typed refusal with a code.
func TestASettingsRenderOnAKeptFileIsATypedRefusal(t *testing.T) {
	panel, subscriptionID := newStartupVhostBatchFixture(t)
	domainID := addStartupHostedDomain(t, panel, subscriptionID, "settings.example", "static")
	agent := &siteFileTestAgent{single: func(request *transport.ApplyVhostRequest, response *transport.ApplyVhostResponse) {
		file := fileResult(request.Domain, transport.SiteFileOwnerEdited, transport.SiteFileOutcomeKept)
		response.File = &file
		response.Error = "kept"
	}}
	attachStartupVhostBatchAgent(t, panel, agent)
	output := captureSiteFileLog(t)
	err := panel.applyVhostForDomain(context.Background(), domainID)
	if err == nil {
		t.Fatal("a kept file was reported as applied")
	}
	recorder := httptest.NewRecorder()
	writeServerError(recorder, err)
	var body apiErrorBody
	_ = json.Unmarshal(recorder.Body.Bytes(), &body)
	if recorder.Code != http.StatusConflict || body.Code != errCodeSiteConfigOwnerEdited || body.Reason != transport.SiteFileOwnerEdited {
		t.Fatalf("answer %d %+v", recorder.Code, body)
	}
	if agent.requests[0].FileTrigger != transport.SiteFileTriggerChange {
		t.Fatalf("trigger %q", agent.requests[0].FileTrigger)
	}
	if !strings.Contains(output.String(), "site configuration settings.example (change): kept (owner-edited)") {
		t.Fatalf("a save logged nothing: %s", output.String())
	}
	for outcome, code := range map[string]string{
		transport.SiteFileOutcomeMissing: errCodeSiteConfigMissing,
		transport.SiteFileOutcomeRefused: errCodeSiteConfigUnwritable,
	} {
		recorder := httptest.NewRecorder()
		writeServerError(recorder, &siteFileHeldError{Domain: "x", File: transport.SiteFileResult{Outcome: outcome}})
		if !strings.Contains(recorder.Body.String(), code) {
			t.Fatalf("%s: %s", outcome, recorder.Body.String())
		}
	}
}

// An older Agent's single render: success as before, state unknown.
func TestASingleRenderFromAnOlderAgentIsUnknownNotUnchanged(t *testing.T) {
	panel, subscriptionID := newStartupVhostBatchFixture(t)
	domainID := addStartupHostedDomain(t, panel, subscriptionID, "older-single.example", "static")
	agent := &siteFileTestAgent{single: func(*transport.ApplyVhostRequest, *transport.ApplyVhostResponse) {}}
	attachStartupVhostBatchAgent(t, panel, agent)
	if err := panel.applyVhostForDomain(context.Background(), domainID); err != nil {
		t.Fatal(err)
	}
	if row := ledgerRow(t, panel, domainID); row.State != transport.SiteFileStateUnknown {
		t.Fatalf("row: %+v", row)
	}
}

func siteConfigRequest(method, path string, body any, caller *Caller) *http.Request {
	var reader *bytes.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	request := httptest.NewRequest(method, path, reader)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	return request.WithContext(context.WithValue(request.Context(), callerKey, caller))
}

func TestSiteConfigReadAndTheOwnersChoices(t *testing.T) {
	panel, subscriptionID := newStartupVhostBatchFixture(t)
	domainID := addStartupHostedDomain(t, panel, subscriptionID, "choice.example", "static")
	fileSHA := strings.Repeat("c", 64)
	renderSHA := strings.Repeat("b", 64)
	agent := &siteFileTestAgent{}
	agent.inspect = func(request *transport.ApplyVhostRequest, response *transport.InspectSiteFileResponse) {
		response.File = fileResult(request.Domain, transport.SiteFileOwnerEdited, transport.SiteFileOutcomeInspected)
		response.File.IncludeDir = "/etc/nginx/celikpanel-sites.d/choice.example"
		response.File.PendingPath = siteFilePath(request.Domain) + ".celikpanel-pending"
		response.Diff = "--- a\n+++ b\n@@ -1 +1 @@\n-x\n+y\n"
	}
	agent.single = func(request *transport.ApplyVhostRequest, response *transport.ApplyVhostResponse) {
		switch {
		case request.FileTrigger == transport.SiteFileTriggerTake && request.ExpectedFileSHA256 == fileSHA && request.ExpectedRenderSHA256 == renderSHA:
			file := fileResult(request.Domain, transport.SiteFileOwnerEdited, transport.SiteFileOutcomeTaken)
			file.BackupPath = siteFilePath(request.Domain) + ".celikpanel-backup-20261010T120000Z"
			file.WrittenSHA256 = renderSHA
			response.File = &file
		case request.FileTrigger == transport.SiteFileTriggerTake:
			file := fileResult(request.Domain, transport.SiteFileOwnerEdited, transport.SiteFileOutcomeRefused)
			file.Reason = transport.SiteFileReasonChanged
			response.File = &file
		case request.FileTrigger == transport.SiteFileTriggerRecreate:
			file := fileResult(request.Domain, transport.SiteFileAbsent, transport.SiteFileOutcomeRecreated)
			response.File = &file
		}
	}
	attachStartupVhostBatchAgent(t, panel, agent)
	var adminID int
	if err := panel.db.GetDB().QueryRow(`SELECT id FROM users WHERE username = 'startup-vhost-owner'`).Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	admin := &Caller{ID: adminID, Role: roleAdmin}
	customer := &Caller{ID: 2, Role: roleCustomer}
	base := "/api/v1/domains/" + itoaForTest(domainID) + "/site-config"

	recorder := httptest.NewRecorder()
	panel.handleDomainSiteConfig(recorder, siteConfigRequest(http.MethodGet, base, nil, customer), domainID, "")
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("customer read: %d", recorder.Code)
	}
	recorder = httptest.NewRecorder()
	panel.handleDomainSiteConfig(recorder, siteConfigRequest(http.MethodGet, base, nil, admin), domainID, "")
	var view siteConfigView
	if err := json.Unmarshal(recorder.Body.Bytes(), &view); err != nil || recorder.Code != http.StatusOK {
		t.Fatalf("read: %d %s", recorder.Code, recorder.Body.String())
	}
	if view.State != transport.SiteFileOwnerEdited || view.Diff == "" || strings.Join(view.Actions, ",") != "keep,take,merge" ||
		view.PendingPath == "" || view.IncludeDir == "" {
		t.Fatalf("view: %+v", view)
	}
	var rows int
	_ = panel.db.GetDB().QueryRow(`SELECT COUNT(*) FROM managed_site_files`).Scan(&rows)
	if rows != 0 {
		t.Fatal("a read wrote the ledger")
	}

	// Keep mine: a digest the page was not shown is refused.
	recorder = httptest.NewRecorder()
	panel.handleDomainSiteConfig(recorder, siteConfigRequest(http.MethodPost, base+"/keep",
		siteConfigActionRequest{FileSHA256: strings.Repeat("d", 64)}, admin), domainID, "keep")
	if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), errCodeSiteConfigChanged) {
		t.Fatalf("stale keep: %d %s", recorder.Code, recorder.Body.String())
	}
	recorder = httptest.NewRecorder()
	panel.handleDomainSiteConfig(recorder, siteConfigRequest(http.MethodPost, base+"/keep",
		siteConfigActionRequest{FileSHA256: fileSHA}, admin), domainID, "keep")
	if recorder.Code != http.StatusOK {
		t.Fatalf("keep: %d %s", recorder.Code, recorder.Body.String())
	}
	row := ledgerRow(t, panel, domainID)
	if row.Decision != services.SiteFileDecisionKeepMine || row.DecisionFileSHA256 != fileSHA || row.DecidedAt == 0 {
		t.Fatalf("keep row: %+v", row)
	}
	_ = json.Unmarshal(recorder.Body.Bytes(), &view)
	if view.Decision == nil || !view.Decision.Current {
		t.Fatalf("keep view: %+v", view.Decision)
	}

	// Take CelikPanel's: bound to what the page showed.
	recorder = httptest.NewRecorder()
	panel.handleDomainSiteConfig(recorder, siteConfigRequest(http.MethodPost, base+"/take",
		siteConfigActionRequest{FileSHA256: strings.Repeat("e", 64), RenderSHA256: renderSHA}, admin), domainID, "take")
	if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), errCodeSiteConfigChanged) {
		t.Fatalf("stale take: %d %s", recorder.Code, recorder.Body.String())
	}
	recorder = httptest.NewRecorder()
	panel.handleDomainSiteConfig(recorder, siteConfigRequest(http.MethodPost, base+"/take",
		siteConfigActionRequest{FileSHA256: fileSHA, RenderSHA256: renderSHA}, admin), domainID, "take")
	if recorder.Code != http.StatusOK {
		t.Fatalf("take: %d %s", recorder.Code, recorder.Body.String())
	}
	row = ledgerRow(t, panel, domainID)
	if row.Decision != services.SiteFileDecisionTake || row.State != transport.SiteFileManagedUnchanged ||
		!strings.Contains(row.BackupPath, ".celikpanel-backup-") || row.BodySHA256 != renderSHA {
		t.Fatalf("take row: %+v", row)
	}

	// Recreate.
	recorder = httptest.NewRecorder()
	panel.handleDomainSiteConfig(recorder, siteConfigRequest(http.MethodPost, base+"/recreate", nil, admin), domainID, "recreate")
	if recorder.Code != http.StatusOK || ledgerRow(t, panel, domainID).Decision != services.SiteFileDecisionRecreate {
		t.Fatalf("recreate: %d %s", recorder.Code, recorder.Body.String())
	}
}

// An Agent without InspectSiteFile: the screen says the state is unknown.
func TestSiteConfigReadFromAnOlderAgentIsUnknown(t *testing.T) {
	panel, subscriptionID := newStartupVhostBatchFixture(t)
	domainID := addStartupHostedDomain(t, panel, subscriptionID, "older-read.example", "static")
	attachStartupVhostBatchAgent(t, panel, &startupVhostBatchAgent{})
	recorder := httptest.NewRecorder()
	panel.handleDomainSiteConfig(recorder, siteConfigRequest(http.MethodGet, "/x", nil, &Caller{ID: 1, Role: roleAdmin}), domainID, "")
	var view siteConfigView
	_ = json.Unmarshal(recorder.Body.Bytes(), &view)
	if recorder.Code != http.StatusOK || view.State != transport.SiteFileStateUnknown || view.Reason != siteConfigReasonAgentDoesNotReport {
		t.Fatalf("older read: %d %s", recorder.Code, recorder.Body.String())
	}
}

// The three choices are state-changing routes under the request identity
// guard (D-029); the read is not. Team members are never admitted.
func TestSiteConfigRoutesAreGuardedAndAdministratorOnly(t *testing.T) {
	for _, action := range []string{"keep", "take", "recreate"} {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/domains/5/site-config/"+action, nil)
		route, ok := requestIdentityRouteFor(request)
		if !ok || route.pattern != "/api/v1/domains/{id}/site-config/"+action {
			t.Fatalf("%s: guarded=%v %+v", action, ok, route)
		}
		if _, ok := teamMemberDomainRequirementFor("site-config-"+action, http.MethodPost); ok {
			t.Fatalf("%s: a team member may reach it", action)
		}
	}
	if _, ok := requestIdentityRouteFor(httptest.NewRequest(http.MethodGet, "/api/v1/domains/5/site-config", nil)); ok {
		t.Fatal("the read is guarded")
	}
	match, ok := matchDomainSubroute(httptest.NewRequest(http.MethodGet, "/api/v1/domains/5/site-config", nil))
	if !ok || match.kind != "site-config" || match.domainID != 5 {
		t.Fatalf("route: %+v", match)
	}
}

func itoaForTest(value int) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}
