package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/rpc"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/transport"
)

// Corrections from the first native measurement of settings writes
// (set1, 2026-10-08; evidence deploy/e2e/release-recovery/evidence/set1-20261010).

type mailPolicyWrittenAnswer struct {
	apiErrorBody
	Policy *transport.MailPolicy `json:"policy"`
}

func putMailPolicyForTest(t *testing.T, agentAnswer transport.MailPolicyResponse) (*httptest.ResponseRecorder, mailPolicyWrittenAnswer) {
	t.Helper()
	panel := newMailPolicyTestPanel(t, &mailPolicyTestAgent{set: agentAnswer})
	recorder := httptest.NewRecorder()
	panel.handleMailPolicy(recorder, mailPolicyRequest(http.MethodPut, `{"message_size_mb":30,"outbound_rate_limit":46,"version":"mp1-abc"}`))
	var body mailPolicyWrittenAnswer
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %q: %v", recorder.Body.String(), err)
	}
	return recorder, body
}

// Measured on Ubuntu 24.04: the save answered `200 success` although Postfix did
// not reload. The Agent now reports which step it verified; the Panel says that
// step, carries Postfix's own line, and carries the policy that is written, so
// "the saved values are the ones shown" is true of this very answer.
func TestMailPolicyWrittenNotReloadedSaysTheVerifiedStageAndCarriesThePolicy(t *testing.T) {
	written := transport.MailPolicy{MessageSizeMB: 30, OutboundRateLimit: 46, Version: "mp1-next"}
	cases := []struct {
		stage    string
		said     string
		contains []string
	}{
		{transport.MailPolicyStageCheck, "postfix: fatal: bad numerical configuration: default_process_limit = 200 # raised for the campaign",
			[]string{"saved to /etc/postfix/main.cf", "Postfix's own check refuses its configuration", "A running Postfix keeps the settings it had before", "not in effect",
				"a line this page did not write", "Nothing was rolled back", "server owner", "sudo postfix check", "sudo postfix reload", "Reload this page"}},
		{transport.MailPolicyStageReload, "postfix/postfix-script: fatal: the Postfix mail system is not running",
			[]string{"saved to /etc/postfix/main.cf", "the reload failed", "has not taken the saved values", "Nothing was rolled back", "sudo postfix reload"}},
		{transport.MailPolicyStageVerify, "the Postfix master process stopped while it was reloading",
			[]string{"saved to /etc/postfix/main.cf", "no longer running after the reload", "Nothing was rolled back", "sudo postfix status"}},
		{"a stage from a newer Agent", "x", []string{"saved to /etc/postfix/main.cf", "did not take the saved values", "sudo postfix reload"}},
	}
	for _, tc := range cases {
		t.Run(tc.stage, func(t *testing.T) {
			recorder, body := putMailPolicyForTest(t, transport.MailPolicyResponse{
				Code: transport.MailPolicyNotReloaded, Error: "x", Stage: tc.stage, Reason: tc.said, Policy: written,
			})
			if recorder.Code != http.StatusBadGateway || body.Code != errCodeMailPolicyNotReloaded || !body.MutationApplied || !body.PartialSuccess {
				t.Fatalf("answer = %d %+v", recorder.Code, body)
			}
			for _, part := range tc.contains {
				if !strings.Contains(body.Error, part) {
					t.Errorf("guidance lacks %q: %q", part, body.Error)
				}
			}
			// The command that reports success whatever happened on a wrapper
			// unit is not the recovery command.
			if strings.Contains(body.Error, "systemctl reload postfix") {
				t.Errorf("the guidance names the wrapper unit's reload: %q", body.Error)
			}
			if body.Vars["detail"] != tc.said {
				t.Fatalf("vars = %v", body.Vars)
			}
			wantReason := tc.stage
			if strings.Contains(tc.stage, " ") {
				wantReason = ""
			}
			if body.Reason != wantReason {
				t.Fatalf("reason = %q, want %q", body.Reason, wantReason)
			}
			if body.Policy == nil || body.Policy.Version != "mp1-next" || body.Policy.OutboundRateLimit != 46 || body.Policy.DNSBLZones == nil {
				t.Fatalf("the answer does not carry the written policy and its version: %+v", body.Policy)
			}
		})
	}
}

// Unknown is neither the verified failure nor a success.
func TestMailPolicyReloadUnknownIsATypedUnknown(t *testing.T) {
	recorder, body := putMailPolicyForTest(t, transport.MailPolicyResponse{
		Code: transport.MailPolicyReloadUnknown, Error: "x", Stage: transport.MailPolicyStageVerify,
		Reason: "postfix timed out after 30s: context deadline exceeded",
		Policy: transport.MailPolicy{MessageSizeMB: 30, OutboundRateLimit: 46, Version: "mp1-next"},
	})
	if recorder.Code != http.StatusBadGateway || body.Code != errCodeMailPolicyReloadUnknown || !body.MutationApplied {
		t.Fatalf("answer = %d %+v", recorder.Code, body)
	}
	for _, part := range []string{"saved to /etc/postfix/main.cf", "could not establish whether Postfix took the saved values",
		"not a verified failure", "Nothing was rolled back", "server owner", "sudo postfix status", "sudo postfix reload"} {
		if !strings.Contains(body.Error, part) {
			t.Errorf("guidance lacks %q: %q", part, body.Error)
		}
	}
	if body.Policy == nil || body.Policy.Version != "mp1-next" {
		t.Fatalf("policy = %+v", body.Policy)
	}

	// A policy the Agent could not read back has no version and is not sent
	// as if it were the saved one.
	_, body = putMailPolicyForTest(t, transport.MailPolicyResponse{Code: transport.MailPolicyNotReloaded, Error: "x", Stage: transport.MailPolicyStageCheck})
	if body.Policy != nil {
		t.Fatalf("a policy without a version was sent: %+v", body.Policy)
	}
}

// A save that wrote nothing and could not make Postfix reload (the owner's
// line is still wrong) is the same refusal with the same sentence, but it
// carries no proof of a change: this request changed nothing in main.cf.
func TestMailPolicyUnchangedSaveThatDidNotReloadCarriesNoProofOfAChange(t *testing.T) {
	for _, code := range []string{transport.MailPolicyNotReloaded, transport.MailPolicyReloadUnknown} {
		recorder, body := putMailPolicyForTest(t, transport.MailPolicyResponse{
			Code: code, Error: "x", Stage: transport.MailPolicyStageCheck, Reason: "postfix: fatal: bad line", Unwritten: true,
			Policy: transport.MailPolicy{MessageSizeMB: 30, Version: "mp1-same"},
		})
		if recorder.Code != http.StatusBadGateway || body.MutationApplied || body.PartialSuccess {
			t.Fatalf("%s: answer = %d %+v", code, recorder.Code, body)
		}
		if body.Policy == nil || body.Policy.Version != "mp1-same" || body.Vars["detail"] != "postfix: fatal: bad line" {
			t.Fatalf("%s: body = %+v", code, body)
		}
	}
}

func TestMailPolicySuccessSaysWhatTheSaveCameTo(t *testing.T) {
	for _, applied := range []string{transport.MailPolicyAppliedReloaded, transport.MailPolicyAppliedNotRunning, transport.MailPolicyAppliedUnchanged, transport.MailPolicyAppliedUnchangedReloaded, ""} {
		panel := newMailPolicyTestPanel(t, &mailPolicyTestAgent{set: transport.MailPolicyResponse{
			Applied: applied, Policy: transport.MailPolicy{MessageSizeMB: 30, Version: "mp1-next"},
		}})
		recorder := httptest.NewRecorder()
		panel.handleMailPolicy(recorder, mailPolicyRequest(http.MethodPut, `{"message_size_mb":30,"version":"mp1-abc"}`))
		var body map[string]any
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil || recorder.Code != http.StatusOK {
			t.Fatalf("answer = %d %q", recorder.Code, recorder.Body.String())
		}
		got, present := body["applied"]
		if applied == "" && present {
			t.Fatalf("an Agent that does not say was answered as %v", got)
		}
		if applied != "" && got != applied {
			t.Fatalf("applied = %v, want %q", got, applied)
		}
	}
}

// Guidance must not name a cause nobody verified. Measured: with
// /etc/cron.allow not listing the site user, and with a relocated spool, the
// answer told the owner to check that "the service behind this page is
// running". Cron was running.
func TestUnreadableScheduledTasksNameOnlyAVerifiedCause(t *testing.T) {
	said := "The user set1_owner_test cannot use this program (crontab)"
	cases := []struct {
		name     string
		answer   string
		cause    string
		detail   string
		contains []string
	}{
		{"cron.allow, verified by the Agent", transport.UnreadableWithEvidence(transport.CronStateUnreadable, transport.UnreadableCronAllow, said),
			transport.UnreadableCronAllow, said,
			[]string{"cannot read or change this site user's scheduled tasks", "/etc/cron.allow", "not listed in it", "Nothing was changed",
				"cannot manage this user's tasks", "server owner adds the site user's name", "reloads this page"}},
		{"cron.deny, verified by the Agent", transport.UnreadableWithEvidence(transport.CronStateUnreadable, transport.UnreadableCronDeny, said),
			transport.UnreadableCronDeny, said,
			[]string{"/etc/cron.deny", "lists the user", "Nothing was changed", "server owner removes"}},
		{"a line and no cause", transport.UnreadableWithEvidence(transport.CronStateUnreadable, "", "/var/spool/cron: No such file or directory"),
			"", "/var/spool/cron: No such file or directory",
			[]string{"could not read", "nothing was changed", "does not mean the user has no tasks", "has not established why", "sudo crontab -u"}},
		{"a cause this Panel does not know", transport.UnreadableWithEvidence(transport.CronStateUnreadable, "from_a_newer_agent", "x"),
			"", "x", []string{"has not established why"}},
		{"nothing known", transport.CronStateUnreadable, "", "", []string{"could not read", "has not established why"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			writeCronAgentError(recorder, rpc.ServerError(tc.answer), cronReasonRead)
			body := decodeAPIError(t, recorder)
			if recorder.Code != http.StatusBadGateway || body.Code != errCodeCurrentSettingsUnreadable || body.Reason != settingsResourceScheduledTasks {
				t.Fatalf("answer = %d %+v", recorder.Code, body)
			}
			if body.Detail != tc.cause || body.Vars["detail"] != tc.detail {
				t.Fatalf("cause %q detail %q, want %q %q", body.Detail, body.Vars["detail"], tc.cause, tc.detail)
			}
			for _, part := range tc.contains {
				if !strings.Contains(body.Error, part) {
					t.Errorf("guidance lacks %q: %q", part, body.Error)
				}
			}
			if strings.Contains(body.Error, "is running") {
				t.Errorf("guidance names a cause nobody verified: %q", body.Error)
			}
		})
	}
	// The shared sentence of the other settings screens asserts no cause either.
	if strings.Contains(currentSettingsUnreadableMessage, "is running") {
		t.Errorf("the shared sentence names a cause: %q", currentSettingsUnreadableMessage)
	}
}

func TestUnreadableMailQueueNamesOnlyAVerifiedCause(t *testing.T) {
	said := "bad numerical configuration: default_process_limit = 200 # raised for the campaign"
	panel := newAgentPanel(t, &mailQueueTestAgent{err: errors.New(
		transport.UnreadableWithEvidence(transport.PostfixQueueUnreadable, transport.UnreadablePostfixConfig, said))})
	recorder := httptest.NewRecorder()
	panel.handlePostfixQueue(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/postfix/queue", nil))
	body := decodeAPIError(t, recorder)
	if recorder.Code != http.StatusBadGateway || body.Code != errCodeMailQueueUnreadable || body.Reason != transport.UnreadablePostfixConfig || body.Vars["detail"] != said {
		t.Fatalf("answer = %d %+v", recorder.Code, body)
	}
	for _, part := range []string{"could not be read", "Postfix refuses its own configuration", "main.cf", "Nothing was changed",
		"does not mean the queue is empty", "server owner corrects", "sudo postfix check", "reloads this page"} {
		if !strings.Contains(body.Error, part) {
			t.Errorf("guidance lacks %q: %q", part, body.Error)
		}
	}

	panel = newAgentPanel(t, &mailQueueTestAgent{err: errors.New(
		transport.UnreadableWithEvidence(transport.PostfixQueueUnreadable, "", "postqueue: fatal: Queue report unavailable"))})
	recorder = httptest.NewRecorder()
	panel.handlePostfixQueue(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/postfix/queue", nil))
	body = decodeAPIError(t, recorder)
	if body.Code != errCodeMailQueueUnreadable || body.Reason != "" || body.Vars["detail"] != "postqueue: fatal: Queue report unavailable" ||
		!strings.Contains(body.Error, "has not established why") {
		t.Fatalf("answer = %+v", body)
	}
	_ = core.PostfixQueueResult{}
}

// Measured on three platforms: the previous file was back in place, and the
// answer said it could not be put back and named a copy of that same file as
// "the other version". Each reason now says only what the Agent verified.
func TestConfigReloadFailedSaysOnlyWhatWasVerified(t *testing.T) {
	said := "Failed to reload set1-owner-pooler.service: Unit set1-owner-pooler.service not found."
	cases := []struct {
		reason   string
		name     string
		contains []string
		never    []string
	}{
		{transport.ConfigReloadRestoredUnitFailed, "",
			[]string{"was not kept", "previous file is back in place", "asked the server directly", "running with the settings it had before your change",
				"failed with the previous file too", "server owner", "sudo systemctl reload postgresql@17-main", "saves the change here again"},
			[]string{"could not put the previous file back", "other version", "copy named below"}},
		{transport.ConfigReloadRestoredUnknown, "",
			[]string{"was not kept", "previous file is back in place", "could not establish which settings the service is running with",
				"may already have made it read the new file", "server owner", "sudo systemctl reload postgresql@17-main", "reloads this page"},
			[]string{"could not put the previous file back", "other version", "copy named below", "running with the settings it had before"}},
		{transport.ConfigReloadNotRestored, "/etc/postgresql/17/main/postgresql.conf.celikpanel-backup-20261009T120000Z",
			[]string{"could not put the previous file back", "copy named below holds the previous file", "sudo systemctl reload postgresql@17-main"},
			[]string{"other version"}},
	}
	for _, tc := range cases {
		t.Run(tc.reason, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			writeConfigRPCError(recorder, &transport.ConfigRPCError{
				Code: transport.ConfigErrorReloadFailed, Reason: tc.reason, Detail: said, Name: tc.name, Unit: "postgresql@17-main",
			})
			body := decodeAPIError(t, recorder)
			if recorder.Code != http.StatusBadGateway || body.Code != errCodeConfigReloadFailed || body.Reason != tc.reason {
				t.Fatalf("answer = %d %+v", recorder.Code, body)
			}
			for _, part := range tc.contains {
				if !strings.Contains(body.Error, part) {
					t.Errorf("guidance lacks %q: %q", part, body.Error)
				}
			}
			for _, part := range tc.never {
				if strings.Contains(body.Error, part) {
					t.Errorf("guidance says %q although that was not verified: %q", part, body.Error)
				}
			}
			if body.Vars["detail"] != said || body.Vars["unit"] != "postgresql@17-main" || body.Vars["name"] != tc.name {
				t.Fatalf("vars = %v", body.Vars)
			}
		})
	}

	// A unit name that is not a unit name is not put into a sentence.
	recorder := httptest.NewRecorder()
	writeConfigRPCError(recorder, &transport.ConfigRPCError{
		Code: transport.ConfigErrorReloadFailed, Reason: transport.ConfigReloadRestoredUnknown, Unit: "x; rm -rf /",
	})
	body := decodeAPIError(t, recorder)
	if strings.Contains(body.Error, "rm -rf") || !strings.Contains(body.Error, "sudo systemctl reload <service>") || body.Vars["unit"] != "" {
		t.Fatalf("answer = %+v", body)
	}
}

// Measured on Debian 13 and Ubuntu 24.04: postgresql.conf and pg_hba.conf were
// each listed twice, because the component has two units (the wrapper
// postgresql.service and postgresql@17-main.service) and each scan returns the
// component's files.
func TestComponentConfigurationFilesAreListedOnceByResolvedPath(t *testing.T) {
	old := configFileResolve
	t.Cleanup(func() { configFileResolve = old })
	links := map[string]string{"/etc/mysql/my.cnf": "/etc/mysql/mariadb.cnf"}
	configFileResolve = func(path string) (string, error) {
		if target, ok := links[path]; ok {
			return target, nil
		}
		if strings.HasPrefix(path, "/var/lib/postgres/") {
			return "", errors.New("permission denied")
		}
		return path, nil
	}
	paths := func(files []core.ConfigFile) string {
		var out []string
		for _, file := range files {
			out = append(out, file.Path)
		}
		return strings.Join(out, " ")
	}
	list := func(names ...string) []core.ConfigFile {
		var files []core.ConfigFile
		for _, name := range names {
			files = append(files, core.ConfigFile{Path: name, IsManaged: true})
		}
		return files
	}

	// The measured list.
	got := uniqueConfigFiles(list(
		"/etc/postgresql/17/main/postgresql.conf", "/etc/postgresql/17/main/pg_hba.conf",
		"/etc/postgresql/17/main/postgresql.conf", "/etc/postgresql/17/main/pg_hba.conf"))
	if paths(got) != "/etc/postgresql/17/main/postgresql.conf /etc/postgresql/17/main/pg_hba.conf" {
		t.Fatalf("listed %q", paths(got))
	}
	// Two names for one file: the file itself is kept, in the first one's place.
	got = uniqueConfigFiles(list("/etc/mysql/my.cnf", "/etc/mysql/mariadb.cnf", "/etc/mysql/mariadb.conf.d/50-server.cnf"))
	if paths(got) != "/etc/mysql/mariadb.cnf /etc/mysql/mariadb.conf.d/50-server.cnf" {
		t.Fatalf("listed %q", paths(got))
	}
	// A path that cannot be resolved is compared as written, and kept.
	got = uniqueConfigFiles(list("/var/lib/postgres/data/postgresql.conf", "/var/lib/postgres/data/pg_hba.conf", "/var/lib/postgres/data/postgresql.conf"))
	if paths(got) != "/var/lib/postgres/data/postgresql.conf /var/lib/postgres/data/pg_hba.conf" {
		t.Fatalf("listed %q", paths(got))
	}
	if got := uniqueConfigFiles(nil); got != nil {
		t.Fatalf("nothing listed became %v", got)
	}
}
