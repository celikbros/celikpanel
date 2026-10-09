package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/services"
	"github.com/alicelik/celikpanel/internal/transport"
)

// Corrections from the final native round (12 Oct 2026; evidence
// deploy/e2e/release-recovery/evidence/set3-20261012).
// Son yerel turun düzeltmeleri.

const set3NginxLine = `nginx: [emerg] open() "/etc/nginx/snippets/fastcgi-php.conf" failed (2: No such file or directory) in /etc/nginx/sites-enabled/set3-php.test.conf:27`

func decodeErrorBody(t *testing.T, recorder *httptest.ResponseRecorder) apiErrorBody {
	t.Helper()
	var body apiErrorBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("answer is not JSON: %v: %s", err, recorder.Body.String())
	}
	return body
}

// P5b, the answer. A site nginx refused is `502 SITE_WEB_SERVER_REFUSED`, in
// plain words; it was `500 INTERNAL`. What it says was removed is what the
// site's deletion on the Agent confirmed, and nginx's own line is for an
// administrator only.
func TestRefusedSiteIsAnsweredTypedAndSaysOnlyWhatWasVerified(t *testing.T) {
	refusal := &services.WebServerRefusedConfigError{Detail: set3NginxLine}
	// The orchestrator joins its compensation's error to the cause: one error
	// in the join is a compensation that ended without one.
	cleaned := errors.Join(refusal, nil)
	notCleaned := errors.Join(refusal, errors.New("rollback agent site: site cleanup incomplete: PHP-FPM pool"))

	for _, c := range []struct {
		name         string
		err          error
		duringImport bool
		caller       *Caller
		reason       string
		says, never  []string
	}{
		{"created and removed again", cleaned, false, &Caller{Role: roleAdmin}, siteWebServerRefusedRemoved,
			[]string{"The site set3-php.test was not created", "the web server (nginx) refused the configuration CelikPanel generated",
				"was removed again and the removal was confirmed", "its web server configuration, its system account, its files and, for a PHP site, its PHP pool",
				"nginx was reloaded with the configuration it had before", "The server owner runs sudo nginx -t",
				"Then create the site again; nothing retries by itself."},
			[]string{"was not confirmed", "may remain", "import"}},
		{"removal not confirmed", notCleaned, false, &Caller{Role: roleAdmin}, siteWebServerRefusedUnconfirmed,
			[]string{"The site set3-php.test was not created", "Removing what had been created for the site was not confirmed",
				"parts of it may remain", "if set3-php.test is listed there, delete it", "The server owner runs sudo nginx -t",
				"Then create the site again; nothing retries by itself."},
			[]string{"the removal was confirmed", "was removed again"}},
		{"import, removed", cleaned, true, &Caller{Role: roleAdmin}, siteWebServerRefusedImportRemoved,
			[]string{"The import did not start", "no file, mailbox, DNS record or database of the archive was imported",
				"the removal was confirmed", "Then start the import again; nothing starts it again automatically."},
			[]string{"may remain", "create the site again"}},
		{"import, removal not confirmed", notCleaned, true, &Caller{Role: roleAdmin}, siteWebServerRefusedImportUnconfirmed,
			[]string{"The import did not start", "no file, mailbox, DNS record or database of the archive was imported",
				"was not confirmed", "Then start the import again; nothing starts it again automatically."},
			[]string{"the removal was confirmed"}},
	} {
		found, confirmed, ok := webServerRefusedConfig(c.err)
		if !ok || found != refusal {
			t.Fatalf("%s: the typed refusal was not found", c.name)
		}
		recorder := httptest.NewRecorder()
		writeSiteWebServerRefused(recorder, c.caller, "set3-php.test", found, confirmed, c.duringImport)
		body := decodeErrorBody(t, recorder)
		if recorder.Code != http.StatusBadGateway || body.Code != errCodeSiteWebServerRefused || body.Reason != c.reason {
			t.Fatalf("%s: status %d, body %+v", c.name, recorder.Code, body)
		}
		if body.Vars["domain"] != "set3-php.test" || body.Vars["command"] != "sudo nginx -t" {
			t.Fatalf("%s: vars = %v", c.name, body.Vars)
		}
		for _, fragment := range c.says {
			if !strings.Contains(body.Error, fragment) {
				t.Fatalf("%s: the sentence lacks %q: %s", c.name, fragment, body.Error)
			}
		}
		for _, fragment := range append(c.never, "internal server error", "/etc/nginx", "nothing was left behind", "Nothing was left behind") {
			if strings.Contains(body.Error, fragment) {
				t.Fatalf("%s: the sentence says %q: %s", c.name, fragment, body.Error)
			}
		}
		// nginx's own line: beside the sentence, for an administrator.
		if len(body.Details) != 1 || body.Details[0] != set3NginxLine {
			t.Fatalf("%s: details = %q", c.name, body.Details)
		}
		if body.MutationApplied || body.PartialSuccess {
			t.Fatalf("%s: a refusal was marked as applied: %+v", c.name, body)
		}
	}

	// Anyone who is not an administrator gets the sentence without the line.
	for _, caller := range []*Caller{nil, {Role: "customer"}} {
		recorder := httptest.NewRecorder()
		writeSiteWebServerRefused(recorder, caller, "set3-php.test", refusal, true, false)
		if body := decodeErrorBody(t, recorder); len(body.Details) != 0 || strings.Contains(recorder.Body.String(), "/etc/nginx") {
			t.Fatalf("a path of the server was answered to %+v: %s", caller, recorder.Body.String())
		}
	}

	// Not this answer: another failure, and the typed cause outside a join
	// (the compensation's result is then not known).
	if _, _, ok := webServerRefusedConfig(errors.Join(errors.New("site creation failed: site provisioning failed during PHP-FPM pool creation"), nil)); ok {
		t.Fatal("another failure was read as a refused configuration")
	}
	if _, confirmed, ok := webServerRefusedConfig(refusal); !ok || confirmed {
		t.Fatalf("a refusal without its compensation's result was read as confirmed (found %v)", ok)
	}

	// The create and the import handler both answer it instead of a bare 500.
	for _, name := range []string{"domain_handlers.go", "import_handlers.go"} {
		source := readPanelSource(t, name)
		if !strings.Contains(source, "webServerRefusedConfig(err); ok {\n\t\t\twriteSiteWebServerRefused(w, ") {
			t.Fatalf("%s does not answer a refused web server configuration", name)
		}
	}
}

// The orchestrator turns the Agent's code into the typed cause, and joins its
// own compensation's result to it.
func TestOrchestratorCarriesTheWebServerRefusal(t *testing.T) {
	source := strings.ReplaceAll(func() string {
		raw, err := os.ReadFile("../../internal/services/site_orchestrator.go")
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}(), "\r\n", "\n")
	if !strings.Contains(source, "if agentReply.ErrorCode == transport.WebServerRefusedConfig {\n\t\t\tcause = &WebServerRefusedConfigError{Detail: agentReply.ErrorDetail}\n\t\t}\n\t\treturn nil, errors.Join(cause, so.rollbackCreatedSite(agentReq, domain.ID, siteID))") {
		t.Fatal("the orchestrator does not carry the Agent's refusal with its compensation's result")
	}
}

// O21. The PHP version of a new site is one this server runs, as the Agent
// reports it. Measured on Arch: the Panel read /etc/php/<version>/fpm on its
// own disk, found nothing, took the literal "8.3", and a site on PHP 8.5 got
// the socket php8.3-fpm-site2.sock.
func TestNewSitePHPVersionIsOneTheServerRuns(t *testing.T) {
	for _, c := range []struct {
		requested string
		installed []string
		version   string
		ok        bool
	}{
		{"", []string{"8.5"}, "8.5", true},        // Arch: the one unversioned PHP-FPM
		{"", []string{"8.4", "8.3"}, "8.4", true}, // the newest installed
		{"8.3", []string{"8.4", "8.3"}, "8.3", true},
		{"8.3", []string{"8.5"}, "8.3", false}, // named, and not installed
		{"", nil, "", false},
	} {
		version, ok := newSitePHPVersion(c.requested, c.installed)
		if version != c.version || ok != c.ok {
			t.Fatalf("requested %q of %v: %q, %t", c.requested, c.installed, version, ok)
		}
	}

	recorder := httptest.NewRecorder()
	writePHPVersionNotInstalled(recorder, "8.3", []string{"8.5"})
	body := decodeErrorBody(t, recorder)
	if recorder.Code != http.StatusConflict || body.Code != errCodePHPVersionNotInstalled || body.Action != "/services" ||
		body.Vars["version"] != "8.3" || body.Vars["installed"] != "8.5" ||
		body.Error != phpVersionNotInstalledMessage("8.3", "8.5") || !strings.HasPrefix(body.Error, "Nothing was created: PHP 8.3 is not installed on this server. Installed: 8.5.") {
		t.Fatalf("status %d, body %+v", recorder.Code, body)
	}

	// No version is assumed anywhere a site's version is chosen or shown.
	for _, name := range []string{"domain_handlers.go", "domain_php_handlers.go", "import_handlers.go"} {
		source := readPanelSource(t, name)
		for _, literal := range []string{`"8.3"`, `"8.4"`, "DetectInstalledPHPVersion", `"/var/run/php/php`} {
			if strings.Contains(source, literal) {
				t.Fatalf("%s still holds %s", name, literal)
			}
		}
	}
	if source := readPanelSource(t, "domain_handlers.go"); !strings.Contains(source, "newSitePHPVersion(req.PHPVersion, caps.PHPVersions)") {
		t.Fatal("the create handler does not take the version from the Agent's report")
	}
	// A version switch records the socket the Agent wrote the pool with.
	if source := readPanelSource(t, "domain_php_handlers.go"); !strings.Contains(source, "newSocket := services.PHPFPMSocketPath(req.PHPVersion, poolName)") {
		t.Fatal("the version switch builds its own socket path")
	}
}

// O19. A Stop that succeeded and left the unit marked as failed is answered as
// the success it is, with a note in the shape of every explained answer.
func TestSuccessfulStopCarriesTheNoteAboutTheFailedUnit(t *testing.T) {
	encode := func(unit, action string, reply transport.ServiceActionResult) (map[string]json.RawMessage, *apiErrorBody) {
		raw, err := json.Marshal(serviceActionSuccessAnswer(unit, action, reply))
		if err != nil {
			t.Fatal(err)
		}
		fields := map[string]json.RawMessage{}
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		for _, internal := range []string{"notice", "notice_unit", "notice_result", "notice_detail"} {
			if _, present := fields[internal]; present {
				t.Fatalf("the Agent's raw field %s was answered: %s", internal, raw)
			}
		}
		noteRaw, present := fields["note"]
		if !present {
			return fields, nil
		}
		var note apiErrorBody
		if err := json.Unmarshal(noteRaw, &note); err != nil {
			t.Fatal(err)
		}
		return fields, &note
	}
	stopped := transport.ServiceActionResult{
		Success: true, Outcome: transport.ServiceActionVerified, Applied: "stopped",
		Notice: transport.ServiceActionNoticeUnitFailed, NoticeUnit: "postfix.service", NoticeResult: "exit-code",
		NoticeDetail: "postfix: fatal: bad numerical configuration: default_process_limit = 200 # raised for the campaign",
	}
	fields, note := encode("postfix", "stop", stopped)
	if string(fields["success"]) != "true" || string(fields["applied"]) != `"stopped"` || note == nil {
		t.Fatalf("answer = %v", fields)
	}
	if note.Code != errCodeServiceActionNote || note.Reason != serviceActionNoteUnitFailedConfig ||
		note.Vars["unit"] != "postfix" || note.Vars["failed_unit"] != "postfix.service" || note.Vars["result"] != "exit-code" ||
		note.Vars["command"] != "sudo systemctl reset-failed postfix.service" || !strings.Contains(note.Vars["detail"], "default_process_limit") {
		t.Fatalf("note = %+v", note)
	}
	for _, fragment := range []string{
		"The service was stopped and is not running", "systemd now shows its unit as failed, which it was not before the stop",
		"systemd's own record", "CelikPanel leaves it as it is", "refuses its configuration at present",
		"will not start until that is corrected", "Start can be used from this state", "the server owner runs the command shown",
	} {
		if !strings.Contains(note.Error, fragment) {
			t.Fatalf("the note lacks %q: %s", fragment, note.Error)
		}
	}
	if strings.Contains(note.Error, "default_process_limit") {
		t.Fatalf("the service's line is in the sentence: %s", note.Error)
	}

	// Without a line of the service's own: the plain reason.
	plain := stopped
	plain.NoticeDetail, plain.NoticeUnit, plain.NoticeResult = "", "nginx.service", "timeout"
	if _, note = encode("nginx", "stop", plain); note == nil || note.Reason != serviceActionNoteUnitFailed ||
		note.Vars["result"] != "timeout" || strings.Contains(note.Error, "refuses its configuration") {
		t.Fatalf("note = %+v", note)
	}

	// No note: a clean stop, another action, a unit name or a result that is
	// not one.
	clean := transport.ServiceActionResult{Success: true, Outcome: transport.ServiceActionVerified, Applied: "stopped"}
	if fields, note = encode("postfix", "stop", clean); note != nil || len(fields) != 3 {
		t.Fatalf("a clean stop: %v", fields)
	}
	if _, note = encode("postfix", "restart", stopped); note != nil {
		t.Fatal("a restart carries the stop's note")
	}
	hostile := stopped
	hostile.NoticeUnit = "postfix.service; rm -rf /"
	if _, note = encode("postfix", "stop", hostile); note != nil {
		t.Fatal("a unit name that is not one was repeated")
	}
	odd := stopped
	odd.NoticeResult = "Exit Code 1; $(x)"
	if _, note = encode("postfix", "stop", odd); note == nil || note.Vars["result"] != "failed" {
		t.Fatalf("a result that is not systemd's was repeated: %+v", note)
	}
	failed := stopped
	failed.Success = false
	if _, note = encode("postfix", "stop", failed); note != nil {
		t.Fatal("a failure carries a note")
	}

	if source := readPanelSource(t, "main.go"); !strings.Contains(source, "json.NewEncoder(w).Encode(serviceActionSuccessAnswer(serviceName, req.Action, reply))") {
		t.Fatal("the service action handler does not answer the note")
	}
}

// O17. A member the files step refused by its name is a part of the archive
// that was not imported: the import is `partial`, the member is listed under
// `not_imported`, and the count is never lost. Every chosen part is there, so
// the domain is marked as finished. What is merely outside the site folder is
// said on the files step's own line and does not make an import partial.
func TestImportListsEveryMemberTheFilesStepLeftOut(t *testing.T) {
	ext := &transport.CpmoveExtractResponse{
		Files: 2, Bytes: 59, Complete: true,
		Refused:      []transport.CpmoveRefusedMember{{Name: "/etc/set3-escape-absolute.txt", Reason: transport.CpmoveRefusedAbsolutePath}},
		RefusedCount: 1,
		OutsideCount: 5,
		OutsideGroups: []transport.CpmoveMemberGroup{
			{Name: "homedir/mail", Count: 2}, {Name: "homedir/etc", Count: 1}, {Name: "mysql", Count: 1}, {Name: "homedir", Count: 1},
		},
	}
	steps := []importStep{
		{Step: "domain", OK: true, Detail: "set3-hostile-absolute.test (id 9, site 4)"},
		{Step: "files", OK: true, Detail: "2 files, 59 bytes" + importOutsideSiteFolder(ext)},
	}
	steps = append(steps, importRefusedMemberSteps(ext)...)
	steps = append(steps, importStep{Step: "dns", OK: true, Detail: "panel DNS template created"})
	answer := importApplyAnswerFor("set3-hostile-absolute.test", 9, 4, steps)
	if answer.Status != importStatusPartial || answer.Code != errCodeImportPartial || answer.DomainStatus != "active" {
		t.Fatalf("answer = %+v", answer)
	}
	if got := strings.Join(answer.NotImported, ","); got != "member:/etc/set3-escape-absolute.txt" {
		t.Fatalf("not imported = %s", got)
	}
	if got := strings.Join(answer.Imported, ","); got != "domain,files,dns" {
		t.Fatalf("imported = %s", got)
	}
	for _, fragment := range []string{
		"every part that was chosen was imported; set3-hostile-absolute.test is in service",
		"Imported: domain, files, dns.", "Not imported: member:/etc/set3-escape-absolute.txt.",
		"refused by their names, and nothing was written for them", "file manager of set3-hostile-absolute.test",
		"Importing the archive again refuses the same entries; nothing continues by itself.",
	} {
		if !strings.Contains(answer.Message, fragment) {
			t.Fatalf("message lacks %q: %s", fragment, answer.Message)
		}
	}
	for _, never := range []string{"not finished", "removes set3-hostile-absolute.test"} {
		if strings.Contains(answer.Message, never) {
			t.Fatalf("message says %q: %s", never, answer.Message)
		}
	}
	// A chosen part that is missing as well: the domain is left not finished,
	// in the words of every other partial import.
	both := importApplyAnswerFor("set3-hostile-absolute.test", 9, 4, append(append([]importStep{}, steps...),
		importStep{Step: "database:shop", OK: false, Detail: "engine import failed"}))
	if both.Status != importStatusPartial || both.DomainStatus != "pending" || !strings.Contains(both.Message, "left marked as not finished") ||
		strings.Join(both.NotImported, ",") != "member:/etc/set3-escape-absolute.txt,database:shop" {
		t.Fatalf("answer = %+v", both)
	}
	if steps[2].Detail != importRefusedMemberAbsolute || steps[2].OK {
		t.Fatalf("step = %+v", steps[2])
	}
	for _, fragment := range []string{
		"2 files, 59 bytes. 5 other entries of the archive are outside the site folder (homedir/public_html) and are not copied by this step: homedir/mail (2), homedir/etc (1), mysql (1), homedir (1).",
		"read from their own entries by their own steps", "mailbox contents and the other folders of the home directory are not imported",
	} {
		if !strings.Contains(steps[1].Detail, fragment) {
			t.Fatalf("the files line lacks %q: %s", fragment, steps[1].Detail)
		}
	}

	// Members outside the site folder alone: said, and the import is complete.
	onlyOutside := &transport.CpmoveExtractResponse{Files: 2, Complete: true, OutsideCount: 3, OutsideGroups: []transport.CpmoveMemberGroup{{Name: "homedir/mail", Count: 3}}}
	if got := importRefusedMemberSteps(onlyOutside); len(got) != 0 {
		t.Fatalf("steps = %+v", got)
	}
	complete := importApplyAnswerFor("x.test", 1, 1, []importStep{{Step: "domain", OK: true}, {Step: "files", OK: true, Detail: "2 files, 0 bytes" + importOutsideSiteFolder(onlyOutside)}})
	if complete.Status != importStatusComplete || len(complete.NotImported) != 0 {
		t.Fatalf("answer = %+v", complete)
	}
	if got := importOutsideSiteFolder(&transport.CpmoveExtractResponse{Files: 2, Complete: true}); got != "" {
		t.Fatalf("nothing outside: %q", got)
	}

	// More refused members than are listed: the list is bounded, the count is
	// kept, and nothing the Agent sent beyond the bound is repeated.
	many := &transport.CpmoveExtractResponse{Complete: true, RefusedCount: 64}
	for i := 0; i < 40; i++ {
		many.Refused = append(many.Refused, transport.CpmoveRefusedMember{Name: fmt.Sprintf("/var/tmp/x-%02d", i), Reason: transport.CpmoveRefusedAbsolutePath})
	}
	listed := importRefusedMemberSteps(many)
	if len(listed) != transport.CpmoveRefusedMemberLimit+1 {
		t.Fatalf("%d steps", len(listed))
	}
	last := listed[len(listed)-1]
	if last.Step != "members:44" || last.OK || !strings.Contains(last.Detail, "44 more entries") || !strings.Contains(last.Detail, "64 in all") {
		t.Fatalf("last = %+v", last)
	}
	// A reason this Panel has no words for is still a member that was not imported.
	unknown := importRefusedMemberSteps(&transport.CpmoveExtractResponse{RefusedCount: 1, Refused: []transport.CpmoveRefusedMember{{Name: "x\x1b[0m", Reason: "later_reason"}}})
	if len(unknown) != 1 || unknown[0].OK || unknown[0].Step != "member:x [0m" || !strings.Contains(unknown[0].Detail, "refused this entry of the archive by its name") {
		t.Fatalf("unknown = %+v", unknown)
	}

	if source := readPanelSource(t, "import_handlers.go"); !strings.Contains(source, "steps = append(steps, importRefusedMemberSteps(&ext)...)") {
		t.Fatal("the import handler does not list the refused members as parts that were not imported")
	}
}

// The operation guidance records, word for word, every sentence this entry
// added to an answer, in its English and its Turkish edition.
func TestTheGuidanceRecordsTheSentencesOfTheFinalRound(t *testing.T) {
	sentences := map[string]string{
		"site refused, removed":               siteWebServerRefusedMessage(siteWebServerRefusedRemoved, "{domain}"),
		"site refused, removal not confirmed": siteWebServerRefusedMessage(siteWebServerRefusedUnconfirmed, "{domain}"),
		"import, site refused, removed":       siteWebServerRefusedMessage(siteWebServerRefusedImportRemoved, "{domain}"),
		"import, removal not confirmed":       siteWebServerRefusedMessage(siteWebServerRefusedImportUnconfirmed, "{domain}"),
		"php version":                         phpVersionNotInstalledMessage("{version}", "{installed}"),
		"stop left the unit failed":           serviceActionNoteMessage(serviceActionNoteUnitFailed),
		"stop left the unit failed, config":   serviceActionNoteMessage(serviceActionNoteUnitFailedConfig),
		"absolute member":                     importRefusedMemberAbsolute,
		"only refused entries":                importRefusedMembersMessage("{domain}", []string{"{imported}"}, []string{"{not imported}"}),
	}
	for _, name := range []string{"OPERATION-GUIDANCE.md", "OPERATION-GUIDANCE.tr.md"} {
		raw, err := os.ReadFile("../../docs/" + name)
		if err != nil {
			t.Fatal(err)
		}
		document := strings.Join(strings.Fields(string(raw)), " ")
		for what, sentence := range sentences {
			if !strings.Contains(document, `"`+sentence+`"`) {
				t.Fatalf("%s does not record the sentence of %s: %s", name, what, sentence)
			}
		}
	}
}
