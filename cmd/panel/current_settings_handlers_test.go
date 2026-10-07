package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/rpc"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

// mailPolicyTestAgent answers the two mail policy RPCs with a prepared
// response and records what a write carried.
type mailPolicyTestAgent struct {
	verifiedAPTAgentRPCFixture

	get, set transport.MailPolicyResponse
	sets     []transport.MailPolicy
}

func (a *mailPolicyTestAgent) GetMailPolicy(_ *transport.Empty, resp *transport.MailPolicyResponse) error {
	*resp = a.get
	return nil
}

func (a *mailPolicyTestAgent) SetMailPolicy(req *transport.MailPolicy, resp *transport.MailPolicyResponse) error {
	a.sets = append(a.sets, *req)
	*resp = a.set
	return nil
}

func newMailPolicyTestPanel(t *testing.T, agent *mailPolicyTestAgent) *Panel {
	t.Helper()
	panel := newPanelBackupFixture(t).panel
	server := rpc.NewServer()
	if err := server.RegisterName("Agent", agent); err != nil {
		t.Fatalf("register mail policy test agent: %v", err)
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
	t.Cleanup(func() { _ = client.Close() })
	panel.pkgFamilyVal = "apt"
	panel.agentClient = transport.NewReconnectingClientWithContextConnector(client, connector)
	return panel
}

func mailPolicyRequest(method, body string) *http.Request {
	var request *http.Request
	if body == "" {
		request = httptest.NewRequest(method, "/api/v1/mail/policy", nil)
	} else {
		request = httptest.NewRequest(method, "/api/v1/mail/policy", strings.NewReader(body))
	}
	return request.WithContext(context.WithValue(request.Context(), callerKey, &Caller{ID: 1, Role: roleAdmin}))
}

// The read: a policy the Agent could not read is an error, never a policy of
// zeros, and a read policy carries its version and the DNSBL lock.
func TestMailPolicyGetReportsAnUnreadablePolicyAndCarriesTheVersion(t *testing.T) {
	agent := &mailPolicyTestAgent{get: transport.MailPolicyResponse{
		Code: transport.MailPolicyUnreadable, Error: "the current Postfix mail policy could not be read",
	}}
	panel := newMailPolicyTestPanel(t, agent)
	recorder := httptest.NewRecorder()
	panel.handleMailPolicy(recorder, mailPolicyRequest(http.MethodGet, ""))
	body := decodeAPIError(t, recorder)
	if recorder.Code != http.StatusBadGateway || body.Code != errCodeCurrentSettingsUnreadable || body.Reason != settingsResourceMailPolicy {
		t.Fatalf("status/code/reason = %d/%q/%q", recorder.Code, body.Code, body.Reason)
	}
	if strings.Contains(recorder.Body.String(), "message_size_mb") {
		t.Fatalf("an unreadable policy still answered values: %q", recorder.Body.String())
	}

	agent.get = transport.MailPolicyResponse{Policy: transport.MailPolicy{
		MessageSizeMB: 9, OutboundRateLimit: 40, Version: "mp1-abc", DNSBLLocked: transport.MailPolicyLockNoBaseline,
	}}
	recorder = httptest.NewRecorder()
	panel.handleMailPolicy(recorder, mailPolicyRequest(http.MethodGet, ""))
	var policy map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &policy); err != nil {
		t.Fatal(err)
	}
	zones, isList := policy["dnsbl_zones"].([]any)
	if recorder.Code != http.StatusOK || policy["version"] != "mp1-abc" || policy["dnsbl_locked"] != transport.MailPolicyLockNoBaseline ||
		policy["message_size_mb"] != float64(9) || !isList || len(zones) != 0 {
		t.Fatalf("policy = %d %q", recorder.Code, recorder.Body.String())
	}
}

// A save from a form that never loaded the policy is refused before the Agent
// is asked; this is the request the old screen sent after a failed read.
func TestMailPolicyPutWithoutAVersionIsRefusedBeforeTheAgent(t *testing.T) {
	agent := &mailPolicyTestAgent{}
	panel := newMailPolicyTestPanel(t, agent)
	recorder := httptest.NewRecorder()
	panel.handleMailPolicy(recorder, mailPolicyRequest(http.MethodPut,
		`{"message_size_mb":25,"dnsbl_zones":[],"outbound_rate_limit":0}`))
	body := decodeAPIError(t, recorder)
	if recorder.Code != http.StatusConflict || body.Code != errCodeSettingsVersionRequired || body.Reason != settingsResourceMailPolicy {
		t.Fatalf("status/code/reason = %d/%q/%q", recorder.Code, body.Code, body.Reason)
	}
	if len(agent.sets) != 0 {
		t.Fatalf("the Agent was asked to write: %+v", agent.sets)
	}
}

func TestMailPolicyPutAnswersEachAgentRefusalWithItsTypedGuidance(t *testing.T) {
	cases := []struct {
		name   string
		agent  transport.MailPolicyResponse
		status int
		code   string
		reason string
		says   []string
	}{
		{"stale", transport.MailPolicyResponse{Code: transport.MailPolicyChanged, Error: "x"},
			http.StatusConflict, errCodeSettingsChanged, settingsResourceMailPolicy,
			[]string{"changed on the server since this page loaded", "nothing was changed", "Reload the page"}},
		{"no version at the Agent", transport.MailPolicyResponse{Code: transport.MailPolicyVersionRequired, Error: "x"},
			http.StatusConflict, errCodeSettingsVersionRequired, settingsResourceMailPolicy, []string{"nothing was changed"}},
		{"unreadable before the write", transport.MailPolicyResponse{Code: transport.MailPolicyUnreadable, Error: "x"},
			http.StatusBadGateway, errCodeCurrentSettingsUnreadable, settingsResourceMailPolicy, []string{"could not read", "nothing was changed"}},
		{"restrictions refer to another setting", transport.MailPolicyResponse{Code: transport.MailPolicyRestrictionsUnmanaged, Reason: transport.MailPolicyLockVariable, Error: "x"},
			http.StatusConflict, errCodeMailPolicyRestrictionsUnmanaged, transport.MailPolicyLockVariable,
			[]string{"was not changed", "$name", "server owner", "/etc/postfix/main.cf", "sudo systemctl reload postfix", "reload this page"}},
		{"restrictions malformed", transport.MailPolicyResponse{Code: transport.MailPolicyRestrictionsUnmanaged, Reason: transport.MailPolicyLockMalformed, Error: "x"},
			http.StatusConflict, errCodeMailPolicyRestrictionsUnmanaged, transport.MailPolicyLockMalformed,
			[]string{"was not changed", "server owner", "/etc/postfix/main.cf"}},
		{"restrictions hand-written", transport.MailPolicyResponse{Code: transport.MailPolicyRestrictionsUnmanaged, Reason: transport.MailPolicyLockNoBaseline, Error: "x"},
			http.StatusConflict, errCodeMailPolicyRestrictionsUnmanaged, transport.MailPolicyLockNoBaseline,
			[]string{"was not changed", "permit_mynetworks", "permit_sasl_authenticated", "server owner", "Message size and the outgoing rate limit can still be saved"}},
		{"restrictions end with a final action", transport.MailPolicyResponse{Code: transport.MailPolicyRestrictionsUnmanaged, Reason: transport.MailPolicyLockTerminal, Error: "x"},
			http.StatusConflict, errCodeMailPolicyRestrictionsUnmanaged, transport.MailPolicyLockTerminal,
			[]string{"was not changed", "permit, reject or defer", "server owner"}},
		{"an unknown lock from a newer Agent", transport.MailPolicyResponse{Code: transport.MailPolicyRestrictionsUnmanaged, Reason: "future", Error: "x"},
			http.StatusConflict, errCodeMailPolicyRestrictionsUnmanaged, "", []string{"was not changed", "server owner"}},
		{"invalid zone", transport.MailPolicyResponse{Code: transport.MailPolicyInvalid, Reason: transport.MailPolicyInvalidZone, Error: "x"},
			http.StatusBadRequest, errCodeMailPolicyInvalid, transport.MailPolicyInvalidZone, []string{"Nothing was changed", "zen.spamhaus.org"}},
		{"invalid size", transport.MailPolicyResponse{Code: transport.MailPolicyInvalid, Reason: transport.MailPolicyInvalidSize, Error: "x"},
			http.StatusBadRequest, errCodeMailPolicyInvalid, transport.MailPolicyInvalidSize, []string{"Nothing was changed", "between 1 and 200"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			agent := &mailPolicyTestAgent{set: tc.agent}
			panel := newMailPolicyTestPanel(t, agent)
			recorder := httptest.NewRecorder()
			panel.handleMailPolicy(recorder, mailPolicyRequest(http.MethodPut,
				`{"message_size_mb":25,"dnsbl_zones":["zen.spamhaus.org"],"outbound_rate_limit":0,"version":"mp1-abc"}`))
			body := decodeAPIError(t, recorder)
			if recorder.Code != tc.status || body.Code != tc.code || body.Reason != tc.reason {
				t.Fatalf("status/code/reason = %d/%q/%q, want %d/%q/%q", recorder.Code, body.Code, body.Reason, tc.status, tc.code, tc.reason)
			}
			for _, part := range tc.says {
				if !strings.Contains(body.Error, part) {
					t.Errorf("guidance lacks %q: %q", part, body.Error)
				}
			}
			if len(agent.sets) != 1 || agent.sets[0].Version != "mp1-abc" {
				t.Fatalf("the Agent received %+v", agent.sets)
			}
		})
	}
}

// A write failure keeps the masked answer: postconf's words never reach the
// client. The fixed "postfix is not installed" answer is unchanged.
func TestMailPolicyUnclassifiedAgentAnswersStayMasked(t *testing.T) {
	agent := &mailPolicyTestAgent{set: transport.MailPolicyResponse{
		Code: transport.MailPolicyWriteFailed, Error: "postconf: fatal: open /etc/postfix/main.cf.tmp: Permission denied",
	}}
	panel := newMailPolicyTestPanel(t, agent)
	recorder := httptest.NewRecorder()
	panel.handleMailPolicy(recorder, mailPolicyRequest(http.MethodPut, `{"message_size_mb":25,"version":"mp1-abc"}`))
	if recorder.Code != http.StatusInternalServerError || strings.Contains(recorder.Body.String(), "Permission denied") {
		t.Fatalf("answer = %d %q", recorder.Code, recorder.Body.String())
	}

	agent.get = transport.MailPolicyResponse{Error: "postfix is not installed"}
	recorder = httptest.NewRecorder()
	panel.handleMailPolicy(recorder, mailPolicyRequest(http.MethodGet, ""))
	if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), "postfix is not installed") {
		t.Fatalf("answer = %d %q", recorder.Code, recorder.Body.String())
	}
}

func TestMailPolicyPutAnswersTheSavedPolicyWithItsNewVersion(t *testing.T) {
	agent := &mailPolicyTestAgent{set: transport.MailPolicyResponse{Policy: transport.MailPolicy{
		MessageSizeMB: 50, DNSBLZones: []string{"zen.spamhaus.org"}, Version: "mp1-next",
	}}}
	panel := newMailPolicyTestPanel(t, agent)
	recorder := httptest.NewRecorder()
	panel.handleMailPolicy(recorder, mailPolicyRequest(http.MethodPut,
		`{"message_size_mb":50,"dnsbl_zones":["zen.spamhaus.org"],"outbound_rate_limit":0,"version":"mp1-abc","dnsbl_locked":"terminal"}`))
	var answer struct {
		Success bool                 `json:"success"`
		Policy  transport.MailPolicy `json:"policy"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusOK || !answer.Success || answer.Policy.Version != "mp1-next" {
		t.Fatalf("answer = %d %q", recorder.Code, recorder.Body.String())
	}
	if len(agent.sets) != 1 || agent.sets[0].Version != "mp1-abc" || agent.sets[0].DNSBLLocked != "" {
		t.Fatalf("the Agent received %+v", agent.sets)
	}
}

// --- Automatic backup schedule ---

func backupScheduleRequest(method, version, body string) *http.Request {
	target := "/api/v1/domains/1/backups/schedule"
	if method == http.MethodDelete && version != "" {
		target += "?version=" + version
	}
	if body == "" {
		return httptest.NewRequest(method, target, nil)
	}
	return httptest.NewRequest(method, target, strings.NewReader(body))
}

func readBackupScheduleForTest(t *testing.T, f panelBackupFixture) map[string]any {
	t.Helper()
	recorder := httptest.NewRecorder()
	f.panel.handleBackupSchedule(recorder, backupScheduleRequest(http.MethodGet, "", ""), f.domainID)
	if recorder.Code != http.StatusOK {
		t.Fatalf("schedule read = %d %q", recorder.Code, recorder.Body.String())
	}
	var schedule map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &schedule); err != nil {
		t.Fatal(err)
	}
	return schedule
}

func storedBackupSchedule(t *testing.T, f panelBackupFixture) (frequency, backupType string, retention int, exists bool) {
	t.Helper()
	rows, err := f.panel.db.GetDB().Query(
		`SELECT frequency, backup_type, retention FROM backup_schedules WHERE domain_id = ?`, f.domainID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		return "", "", 0, false
	}
	if err := rows.Scan(&frequency, &backupType, &retention); err != nil {
		t.Fatal(err)
	}
	return frequency, backupType, retention, true
}

// The defect: the schedule read failed, the form stayed on "daily / files / 7"
// and Turn on wrote those over a weekly full backup kept 30 times. That request
// carries no version and is refused; the schedule is untouched.
func TestBackupSchedulePutFromAFormThatNeverLoadedIsRefused(t *testing.T) {
	f := newPanelBackupFixture(t)
	if _, err := f.panel.db.GetDB().Exec(`
		INSERT INTO backup_schedules (domain_id, frequency, backup_type, retention, enabled)
		VALUES (?, 'weekly', 'full', 30, 1)`, f.domainID); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	f.panel.handleBackupSchedule(recorder, backupScheduleRequest(http.MethodPut, "",
		`{"frequency":"daily","backup_type":"files","retention":7}`), f.domainID)
	body := decodeAPIError(t, recorder)
	if recorder.Code != http.StatusConflict || body.Code != errCodeSettingsVersionRequired || body.Reason != settingsResourceBackupSchedule {
		t.Fatalf("status/code/reason = %d/%q/%q", recorder.Code, body.Code, body.Reason)
	}
	if frequency, backupType, retention, _ := storedBackupSchedule(t, f); frequency != "weekly" || backupType != "full" || retention != 30 {
		t.Fatalf("the schedule was overwritten: %s/%s/%d", frequency, backupType, retention)
	}

	recorder = httptest.NewRecorder()
	f.panel.handleBackupSchedule(recorder, backupScheduleRequest(http.MethodDelete, "", ""), f.domainID)
	if body := decodeAPIError(t, recorder); recorder.Code != http.StatusConflict || body.Code != errCodeSettingsVersionRequired {
		t.Fatalf("delete without a version = %d/%q", recorder.Code, body.Code)
	}
	if _, _, _, exists := storedBackupSchedule(t, f); !exists {
		t.Fatal("the schedule was turned off by a request without a version")
	}
}

func TestBackupScheduleWritesNeedTheVersionOfTheScheduleTheyReplace(t *testing.T) {
	f := newPanelBackupFixture(t)

	// No schedule yet is a known state with its own version.
	none := readBackupScheduleForTest(t, f)
	noneVersion, _ := none["version"].(string)
	if none["enabled"] != false || !strings.HasPrefix(noneVersion, "bs1-") {
		t.Fatalf("no schedule read as %v", none)
	}
	put := func(version, body string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		f.panel.handleBackupSchedule(recorder, backupScheduleRequest(http.MethodPut, "",
			strings.Replace(body, "}", `,"version":"`+version+`"}`, 1)), f.domainID)
		return recorder
	}

	first := put(noneVersion, `{"frequency":"weekly","backup_type":"full","retention":30}`)
	if first.Code != http.StatusOK {
		t.Fatalf("turn on = %d %q", first.Code, first.Body.String())
	}
	var saved struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	current := readBackupScheduleForTest(t, f)
	if current["version"] != saved.Version || saved.Version == noneVersion || current["backup_type"] != "full" {
		t.Fatalf("saved version %q, read %v", saved.Version, current)
	}

	// A second tab that still holds "no schedule" cannot replace it.
	stale := put(noneVersion, `{"frequency":"daily","backup_type":"files","retention":7}`)
	body := decodeAPIError(t, stale)
	if stale.Code != http.StatusConflict || body.Code != errCodeSettingsChanged || body.Reason != settingsResourceBackupSchedule {
		t.Fatalf("stale save = %d/%q/%q", stale.Code, body.Code, body.Reason)
	}
	if !strings.Contains(body.Error, "changed on the server since this page loaded") || !strings.Contains(body.Error, "Reload the page") {
		t.Fatalf("guidance = %q", body.Error)
	}
	if frequency, backupType, retention, _ := storedBackupSchedule(t, f); frequency != "weekly" || backupType != "full" || retention != 30 {
		t.Fatalf("a stale save overwrote the schedule: %s/%s/%d", frequency, backupType, retention)
	}

	// A background run changes the run status only, so the version holds.
	if _, err := f.panel.db.GetDB().Exec(`
		UPDATE backup_schedules SET last_run = '2026-10-08T03:00:00Z', last_status = 'success'
		WHERE domain_id = ?`, f.domainID); err != nil {
		t.Fatal(err)
	}
	if after := readBackupScheduleForTest(t, f); after["version"] != saved.Version {
		t.Fatal("a scheduled run changed the settings version")
	}
	if update := put(saved.Version, `{"frequency":"daily","backup_type":"full","retention":14}`); update.Code != http.StatusOK {
		t.Fatalf("update with the current version = %d %q", update.Code, update.Body.String())
	}

	// Turning off needs the current version too.
	recorder := httptest.NewRecorder()
	f.panel.handleBackupSchedule(recorder, backupScheduleRequest(http.MethodDelete, saved.Version, ""), f.domainID)
	if body := decodeAPIError(t, recorder); recorder.Code != http.StatusConflict || body.Code != errCodeSettingsChanged {
		t.Fatalf("stale turn-off = %d/%q", recorder.Code, body.Code)
	}
	latest, _ := readBackupScheduleForTest(t, f)["version"].(string)
	recorder = httptest.NewRecorder()
	f.panel.handleBackupSchedule(recorder, backupScheduleRequest(http.MethodDelete, latest, ""), f.domainID)
	if recorder.Code != http.StatusOK {
		t.Fatalf("turn off = %d %q", recorder.Code, recorder.Body.String())
	}
	if _, _, _, exists := storedBackupSchedule(t, f); exists {
		t.Fatal("the schedule is still stored after turning it off")
	}
	if after := readBackupScheduleForTest(t, f); after["version"] != noneVersion {
		t.Fatalf("no schedule reads %v, want version %q again", after, noneVersion)
	}
}

// A changed backup type still drops the claimed job key, and an unchanged one
// keeps it, exactly as the upsert did.
func TestBackupSchedulePutKeepsTheJobKeyRule(t *testing.T) {
	f := newPanelBackupFixture(t)
	seedBackupSchedule(t, f.panel, f.domainID, 7)
	if _, err := f.panel.db.GetDB().Exec(
		`UPDATE backup_schedules SET active_job_key = 'schedule:0123456789abcdef0123456789abcdef' WHERE domain_id = ?`,
		f.domainID); err != nil {
		t.Fatal(err)
	}
	jobKey := func() any {
		var key any
		if err := f.panel.db.GetDB().QueryRow(
			`SELECT active_job_key FROM backup_schedules WHERE domain_id = ?`, f.domainID).Scan(&key); err != nil {
			t.Fatal(err)
		}
		return key
	}
	save := func(body string) {
		version, _ := readBackupScheduleForTest(t, f)["version"].(string)
		recorder := httptest.NewRecorder()
		f.panel.handleBackupSchedule(recorder, backupScheduleRequest(http.MethodPut, "",
			strings.Replace(body, "}", `,"version":"`+version+`"}`, 1)), f.domainID)
		if recorder.Code != http.StatusOK {
			t.Fatalf("save = %d %q", recorder.Code, recorder.Body.String())
		}
	}
	save(`{"frequency":"weekly","backup_type":"files","retention":9}`)
	if jobKey() == nil {
		t.Fatal("the job key was dropped although the backup type did not change")
	}
	save(`{"frequency":"weekly","backup_type":"full","retention":9}`)
	if jobKey() != nil {
		t.Fatalf("the job key survived a backup type change: %v", jobKey())
	}
}
