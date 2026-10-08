package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

// A configuration file is read with the version of its bytes and written only
// with that version (9 Oct 2026). Before this the three database editors opened
// on nothing after a failed read with Save enabled.

type recordingConfigAgent struct {
	configHandlerRPCAgent
	updates []transport.UpdateConfigArgs
}

func (a *recordingConfigAgent) UpdateConfig(args *transport.UpdateConfigArgs, reply *transport.UpdateConfigResponse) error {
	a.updates = append(a.updates, *args)
	*reply = a.updateResponse
	return a.updateError
}

func newRecordingConfigPanel(t *testing.T, agent *recordingConfigAgent) *Panel {
	t.Helper()
	// The handler tests of config_rpc_error_test.go build the panel around the
	// embedded agent; register this one under the same name instead.
	return newConfigHandlerPanelFor(t, agent)
}

func configPost(body string) *http.Request {
	return httptest.NewRequest(http.MethodPost, "/api/v1/config", bytes.NewReader([]byte(body)))
}

func TestConfigReadCarriesTheVersionAndAnUnreadableFileIsAnError(t *testing.T) {
	agent := &recordingConfigAgent{}
	agent.getResponse = transport.ConfigResponse{Content: "max_connections = 100\n", Version: "cf1-abc"}
	panel := newRecordingConfigPanel(t, agent)
	recorder := httptest.NewRecorder()
	panel.handleConfig(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/config?path=%2Fetc%2Fpostgresql%2F17%2Fmain%2Fpostgresql.conf", nil))
	var read struct{ Content, Version string }
	if err := json.Unmarshal(recorder.Body.Bytes(), &read); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusOK || read.Content != "max_connections = 100\n" || read.Version != "cf1-abc" {
		t.Fatalf("read = %d %q", recorder.Code, recorder.Body.String())
	}

	for name, response := range map[string]transport.ConfigResponse{
		"the Agent could not read the file": {Error: &transport.ConfigRPCError{Code: transport.ConfigErrorUnreadable, Message: "x"}},
		// An older Agent answers content without a version; its writes are not
		// protected, so its answer is not something a save may be built from.
		"an Agent that answers no version": {Content: "max_connections = 100\n"},
	} {
		t.Run(name, func(t *testing.T) {
			agent := &recordingConfigAgent{}
			agent.getResponse = response
			panel := newRecordingConfigPanel(t, agent)
			recorder := httptest.NewRecorder()
			panel.handleConfig(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/config?path=%2Fetc%2Fmy.cnf", nil))
			body := decodeAPIError(t, recorder)
			if recorder.Code != http.StatusBadGateway || body.Code != errCodeCurrentSettingsUnreadable || body.Reason != settingsResourceConfigFile {
				t.Fatalf("answer = %d %+v, want 502 %s", recorder.Code, body, errCodeCurrentSettingsUnreadable)
			}
			if strings.Contains(recorder.Body.String(), "max_connections") {
				t.Fatalf("an unusable read still carried content: %q", recorder.Body.String())
			}
		})
	}
}

func TestConfigWriteWithoutAVersionIsRefusedBeforeTheAgent(t *testing.T) {
	agent := &recordingConfigAgent{}
	agent.updateResponse = transport.UpdateConfigResponse{Success: true, Version: "cf1-next"}
	panel := newRecordingConfigPanel(t, agent)
	for name, body := range map[string]string{
		"no version":       `{"path":"/etc/postgresql/17/main/pg_hba.conf","content":"local all all peer\n"}`,
		"an empty version": `{"path":"/etc/postgresql/17/main/pg_hba.conf","content":"local all all peer\n","version":""}`,
	} {
		t.Run(name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			panel.handleConfig(recorder, configPost(body))
			answer := decodeAPIError(t, recorder)
			if recorder.Code != http.StatusConflict || answer.Code != errCodeSettingsVersionRequired || answer.Reason != settingsResourceConfigFile {
				t.Fatalf("answer = %d %+v", recorder.Code, answer)
			}
		})
	}
	// What the old screens sent: the file as text/plain, no JSON at all.
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/config?path=%2Fetc%2Fmy.cnf", strings.NewReader("[mysqld]\n"))
	request.Header.Set("Content-Type", "text/plain")
	panel.handleConfig(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("a text/plain body = %d, want 400", recorder.Code)
	}
	if len(agent.updates) != 0 {
		t.Fatalf("a refused write reached the Agent: %+v", agent.updates)
	}
}

func TestConfigWriteAnswersEachAgentRefusalWithItsTypedGuidance(t *testing.T) {
	cases := []struct {
		name   string
		agent  transport.ConfigRPCError
		status int
		code   string
		reason string
		vars   map[string]string
		says   []string
	}{
		{"the file changed since it was read", transport.ConfigRPCError{Code: transport.ConfigErrorChanged},
			http.StatusConflict, errCodeSettingsChanged, settingsResourceConfigFile, nil, []string{"nothing was changed", "Reload the page"}},
		{"no version at the Agent", transport.ConfigRPCError{Code: transport.ConfigErrorVersionRequired},
			http.StatusConflict, errCodeSettingsVersionRequired, settingsResourceConfigFile, nil, []string{"nothing was changed"}},
		{"unreadable before the write", transport.ConfigRPCError{Code: transport.ConfigErrorUnreadable},
			http.StatusBadGateway, errCodeCurrentSettingsUnreadable, settingsResourceConfigFile, nil, []string{"could not read", "nothing was changed"}},
		{"empty content", transport.ConfigRPCError{Code: transport.ConfigErrorValidationFail, Reason: transport.ConfigInvalidEmpty},
			http.StatusUnprocessableEntity, errCodeConfigInvalid, transport.ConfigInvalidEmpty, nil, []string{"Nothing was saved", "empty", "Reload the page"}},
		{"refused by the service's own program", transport.ConfigRPCError{
			Code: transport.ConfigErrorValidationFail, Reason: transport.ConfigInvalidDaemon,
			Detail: `unrecognized configuration parameter "wrk_mem" in file "/etc/postgresql/17/main/postgresql.conf" line 12`, Line: 12, Name: "wrk_mem"},
			http.StatusUnprocessableEntity, errCodeConfigInvalid, transport.ConfigInvalidDaemon,
			map[string]string{"detail": `unrecognized configuration parameter "wrk_mem" in file "/etc/postgresql/17/main/postgresql.conf" line 12`, "line": "12", "name": "wrk_mem"},
			[]string{"Nothing is changed", "refused", "save again"}},
		{"a changed pg_hba.conf line", transport.ConfigRPCError{
			Code: transport.ConfigErrorValidationFail, Reason: transport.ConfigInvalidSyntax, Detail: "end-of-line before authentication method", Line: 19},
			http.StatusUnprocessableEntity, errCodeConfigInvalid, transport.ConfigInvalidSyntax,
			map[string]string{"detail": "end-of-line before authentication method", "line": "19"}, []string{"Nothing was saved", "Correct the line"}},
		{"the administrator access would be lost", transport.ConfigRPCError{
			Code: transport.ConfigErrorValidationFail, Reason: transport.ConfigInvalidLockout, Name: "denied", Line: 7},
			http.StatusUnprocessableEntity, errCodeConfigInvalid, transport.ConfigInvalidLockout,
			map[string]string{"name": "denied", "line": "7"}, []string{"Nothing was saved", "local administrator access", "local all postgres peer"}},
		{"no validating program", transport.ConfigRPCError{Code: transport.ConfigErrorValidationFail, Reason: transport.ConfigInvalidNoValidator, Name: "postgres"},
			http.StatusUnprocessableEntity, errCodeConfigInvalid, transport.ConfigInvalidNoValidator,
			map[string]string{"name": "postgres"}, []string{"Nothing was saved", "could not be run", "server owner"}},
		{"a reason from a newer Agent", transport.ConfigRPCError{Code: transport.ConfigErrorValidationFail, Reason: "future"},
			http.StatusUnprocessableEntity, errCodeConfigInvalid, "", nil, []string{"Nothing is changed"}},
		{"reload failed, previous file restored", transport.ConfigRPCError{
			Code: transport.ConfigErrorReloadFailed, Reason: transport.ConfigReloadRestored, Detail: "Error: invalid data directory"},
			http.StatusBadGateway, errCodeConfigReloadFailed, transport.ConfigReloadRestored,
			map[string]string{"detail": "Error: invalid data directory"}, []string{"was not kept", "put the previous file back", "running with it"}},
		{"reload failed, not restored", transport.ConfigRPCError{
			Code: transport.ConfigErrorReloadFailed, Reason: transport.ConfigReloadNotRestored, Detail: "Job failed",
			Name: "/etc/postgresql/17/main/postgresql.conf.celikpanel-backup-20261009T120000Z"},
			http.StatusBadGateway, errCodeConfigReloadFailed, transport.ConfigReloadNotRestored,
			map[string]string{"detail": "Job failed", "name": "/etc/postgresql/17/main/postgresql.conf.celikpanel-backup-20261009T120000Z"},
			[]string{"could not put the previous file back", "server owner", "sudo systemctl reload"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			agent := &recordingConfigAgent{}
			rpcErr := tc.agent
			rpcErr.Message = "the Agent's own sentence, which is not shown"
			agent.updateResponse = transport.UpdateConfigResponse{Error: &rpcErr}
			panel := newRecordingConfigPanel(t, agent)
			recorder := httptest.NewRecorder()
			panel.handleConfig(recorder, configPost(`{"path":"/etc/postgresql/17/main/postgresql.conf","content":"x = 1\n","version":"cf1-abc"}`))
			body := decodeAPIError(t, recorder)
			if recorder.Code != tc.status || body.Code != tc.code || body.Reason != tc.reason {
				t.Fatalf("status/code/reason = %d/%q/%q, want %d/%q/%q", recorder.Code, body.Code, body.Reason, tc.status, tc.code, tc.reason)
			}
			for _, part := range tc.says {
				if !strings.Contains(body.Error, part) {
					t.Errorf("guidance lacks %q: %q", part, body.Error)
				}
			}
			if strings.Contains(body.Error, "the Agent's own sentence") {
				t.Errorf("the Agent's sentence was shown: %q", body.Error)
			}
			if len(body.Vars) != len(tc.vars) {
				t.Fatalf("vars = %v, want %v", body.Vars, tc.vars)
			}
			for name, want := range tc.vars {
				if body.Vars[name] != want {
					t.Errorf("vars[%s] = %q, want %q", name, body.Vars[name], want)
				}
			}
			if len(agent.updates) != 1 || agent.updates[0].Version != "cf1-abc" {
				t.Fatalf("the Agent received %+v", agent.updates)
			}
		})
	}
}

func TestConfigWriteAnswersWhatHappenedToTheService(t *testing.T) {
	agent := &recordingConfigAgent{}
	agent.updateResponse = transport.UpdateConfigResponse{
		Success: true, Version: "cf1-next", Backup: "/etc/postgresql/17/main/postgresql.conf.celikpanel-backup-20261009T120000Z",
		Applied: transport.ConfigAppliedReloaded, DaemonCheck: transport.ConfigDaemonAccepted, RestartRequired: []string{"shared_buffers"},
	}
	panel := newRecordingConfigPanel(t, agent)
	recorder := httptest.NewRecorder()
	panel.handleConfig(recorder, configPost(`{"path":"/etc/postgresql/17/main/postgresql.conf","content":"shared_buffers = 256MB\n","version":"cf1-abc"}`))
	var answer struct {
		Success         bool     `json:"success"`
		Version         string   `json:"version"`
		Unchanged       bool     `json:"unchanged"`
		Backup          string   `json:"backup"`
		Applied         string   `json:"applied"`
		DaemonCheck     string   `json:"daemon_check"`
		RestartRequired []string `json:"restart_required"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusOK || !answer.Success || answer.Version != "cf1-next" || answer.Applied != "reloaded" ||
		answer.DaemonCheck != "accepted" || len(answer.RestartRequired) != 1 || !strings.HasSuffix(answer.Backup, "20261009T120000Z") {
		t.Fatalf("answer = %d %q", recorder.Code, recorder.Body.String())
	}

	// An Agent that confirms without the version of what is on disk now is an
	// older one that wrote without checking; that is not a confirmed save.
	agent.updateResponse = transport.UpdateConfigResponse{Success: true}
	recorder = httptest.NewRecorder()
	panel.handleConfig(recorder, configPost(`{"path":"/etc/my.cnf","content":"[mysqld]\n","version":"cf1-abc"}`))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("an unconfirmed write = %d, want 500", recorder.Code)
	}
}
