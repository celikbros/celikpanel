package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

// Corrections after the set4 native measurement (2026-10-09).

// A part whose step ended without an error and imported nothing is neither
// imported nor failed. Measured in set4 on three platforms: an import that
// asked for no DNS on a server in external DNS mode answered
// `imported: [domain, files, dns, ...]`.
func TestImportDoesNotListAPartItDidNotImportAsImported(t *testing.T) {
	// The answer of set4's import (Debian 13 and Ubuntu 24.04), with the
	// steps as the handler records them now.
	steps := []importStep{
		{Step: "domain", OK: true, Detail: "set4-import.test (id 6, site 6) → /var/www/celikpanel/subscriptions/2/sites/6/public_html"},
		{Step: "files", OK: true, Detail: "2 files, 59 bytes"},
		{Step: "mail", OK: true, Detail: "1 accounts imported with original passwords (mailbox CONTENTS are not migrated in v1)"},
		{Step: "forwarders", OK: true, State: importStateNoneInArchive, Detail: "0 forwarders"},
		{Step: "dns", OK: true, State: importStateLeftToOwner, Detail: "external DNS ownership preserved; verify provider records before publishing the site"},
		{Step: "database:s4imp_app", OK: true, Detail: "created exclusively and dump imported (db USERS are not migrated; repoint app configs)"},
	}
	answer := importApplyAnswerFor("set4-import.test", 6, 6, steps)
	if got := strings.Join(answer.Imported, ","); got != "domain,files,mail,database:s4imp_app" {
		t.Fatalf("imported = %s", got)
	}
	if got := strings.Join(answer.LeftOut, ","); got != "forwarders,dns" {
		t.Fatalf("left out = %s", got)
	}
	// A part left to the owner on purpose does not make the import partial.
	if answer.Status != importStatusComplete || answer.DomainStatus != "active" || answer.Code != "" || answer.Message != "" ||
		len(answer.NotImported) != 0 {
		t.Fatalf("answer = %+v", answer)
	}
	encoded, err := json.Marshal(answer)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		`"imported":["domain","files","mail","database:s4imp_app"]`, `"not_imported":[]`, `"left_out":["forwarders","dns"]`,
		`{"step":"dns","ok":true,"detail":"external DNS ownership preserved; verify provider records before publishing the site","state":"left_to_owner"}`,
		`{"step":"files","ok":true,"detail":"2 files, 59 bytes"}`,
	} {
		if !strings.Contains(string(encoded), fragment) {
			t.Fatalf("answer lacks %s: %s", fragment, encoded)
		}
	}

	// A failed step is not imported whatever its state, and only a failed
	// step makes the import partial; the message lists what was imported
	// without the parts that were left out.
	partial := importApplyAnswerFor("set4-import.test", 6, 6, append(append([]importStep{}, steps...),
		importStep{Step: "database:s4imp_shop", OK: false, State: importStateNoneImported, Detail: "engine import failed"}))
	if partial.Status != importStatusPartial || partial.Code != errCodeImportPartial ||
		strings.Join(partial.NotImported, ",") != "database:s4imp_shop" || strings.Join(partial.LeftOut, ",") != "forwarders,dns" {
		t.Fatalf("partial answer = %+v", partial)
	}
	if !strings.Contains(partial.Message, "Imported: domain, files, mail, database:s4imp_app. Not imported: database:s4imp_shop.") {
		t.Fatalf("message = %s", partial.Message)
	}

	// Every list is a list, and a part is in exactly one of them.
	for _, answer := range []importApplyAnswer{answer, partial, importApplyAnswerFor("x.test", 1, 1, nil)} {
		seen := map[string]int{}
		for _, list := range [][]string{answer.Imported, answer.NotImported, answer.LeftOut} {
			if list == nil {
				t.Fatalf("a list is null: %+v", answer)
			}
			for _, part := range list {
				seen[part]++
			}
		}
		for _, step := range answer.Steps {
			if step.Step != "finalize" && seen[step.Step] != 1 {
				t.Fatalf("%s is in %d lists: %+v", step.Step, seen[step.Step], answer)
			}
		}
	}

	if importNothingState(0) != importStateNoneInArchive || importNothingState(3) != importStateNoneImported {
		t.Fatal("importNothingState does not tell an empty archive part from one of which nothing came in")
	}
}

// The handler records every step that imports nothing with its reason, and no
// such step with the plain `ok` that lists a part as imported.
func TestImportHandlerMarksEveryStepThatImportedNothing(t *testing.T) {
	source := readPanelSource(t, "import_handlers.go")
	for _, needed := range []string{
		// DNS of a server in external DNS mode: left to the owner's provider.
		`nothing("dns", importStateLeftToOwner, "external DNS ownership preserved; verify provider records before publishing the site")`,
		// The archive's DNS records are imported only when chosen and present.
		"} else if req.DoDNS && hasImportedZone {\n\t\t\tok(\"dns\", \"DNS records published through the domain's connected authority\")\n\t\t} else {\n\t\t\tnothing(\"dns\", dnsNotImported, \"DNS records published through the domain's connected authority\")",
		"} else if req.DoDNS && hasImportedZone {\n\t\t\tok(\"dns\", dnsDetail)\n\t\t} else {\n\t\t\tnothing(\"dns\", dnsNotImported, dnsDetail)",
		"dnsNotImported := importStateNotChosen\n\tif req.DoDNS {\n\t\tdnsNotImported = importStateNoneInArchive\n\t}",
		// Chosen parts of which nothing came in.
		`nothing("forwarders", importNothingState(len(preview.Forwarders)), "0 forwarders")`,
		"if imported > 0 {\n\t\t\tok(\"mail\", mailDetail)\n\t\t} else {\n\t\t\tnothing(\"mail\", importNothingState(mailboxesInArchive), mailDetail)",
		"if ext.Files > 0 {\n\t\t\t\tok(\"files\", filesDetail)\n\t\t\t} else {\n\t\t\t\tnothing(\"files\", importNothingState(ext.RefusedCount), filesDetail)",
	} {
		if !strings.Contains(source, needed) {
			t.Fatalf("import_handlers.go no longer contains %s", needed)
		}
	}
	for _, never := range []string{
		`ok("dns", "external DNS ownership preserved`, `ok("forwarders", "0 forwarders")`,
		`ok("mail", fmt.Sprintf(`, `ok("files", fmt.Sprintf(`,
	} {
		if strings.Contains(source, never) {
			t.Fatalf("import_handlers.go lists a part that imported nothing as imported: %s", never)
		}
	}
	// A step with a state never changes whether the import is complete: only
	// fail and failCoded do.
	body := source[strings.Index(source, "nothing := func(step, state, detail string) {"):]
	body = body[:strings.Index(body, "}\n")+1]
	if strings.Contains(body, "complete") || !strings.Contains(body, "OK: true") {
		t.Fatalf("a step that imported nothing changes the import's result: %s", body)
	}
}

// A Stop after which the unit's own stop was not read is not answered as if
// the unit had been looked at and found clean: it carries a note of its own.
func TestSuccessfulStopSaysWhenTheUnitsStopWasNotRead(t *testing.T) {
	note := func(unit, action string, reply transport.ServiceActionResult) (map[string]json.RawMessage, *apiErrorBody) {
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
		if _, present := fields["note"]; !present {
			return fields, nil
		}
		var body apiErrorBody
		if err := json.Unmarshal(fields["note"], &body); err != nil {
			t.Fatal(err)
		}
		return fields, &body
	}
	// systemd still showed the unit between two states when the wait ended.
	pending := transport.ServiceActionResult{
		Success: true, Outcome: transport.ServiceActionVerified, Applied: "stopped",
		Notice: transport.ServiceActionNoticeUnitNotSettled, NoticeUnit: "postfix@-.service", NoticeResult: "deactivating",
	}
	fields, got := note("postfix", "stop", pending)
	if string(fields["success"]) != "true" || string(fields["applied"]) != `"stopped"` || string(fields["outcome"]) != `"verified"` || got == nil {
		t.Fatalf("answer = %v", fields)
	}
	if got.Code != errCodeServiceActionNote || got.Reason != serviceActionNoteUnitNotSettled ||
		got.Vars["unit"] != "postfix" || got.Vars["pending_unit"] != "postfix@-.service" || got.Vars["state"] != "deactivating" ||
		got.Vars["command"] != "systemctl status postfix@-.service" || len(got.Vars) != 4 {
		t.Fatalf("note = %+v", got)
	}
	for _, fragment := range []string{
		"The service was stopped and is not running", "systemd had not finished stopping its unit when CelikPanel stopped waiting",
		"how its stop ended was not read", "it may end marked as failed", "The server owner runs the command shown",
		"sends nothing more to the unit", "does not look again by itself",
	} {
		if !strings.Contains(got.Error, fragment) {
			t.Fatalf("the note lacks %q: %s", fragment, got.Error)
		}
	}
	// It claims neither state of the unit, and offers nothing that changes it.
	for _, never := range []string{"is failed", "now shows its unit as failed", "is clean", "was not marked", "reset-failed"} {
		if strings.Contains(got.Error, never) || strings.Contains(got.Vars["command"], never) {
			t.Fatalf("the note says %q: %+v", never, got)
		}
	}

	// The unit could not be read after the stop: no state is repeated.
	unread := pending
	unread.NoticeResult = ""
	if _, got = note("postfix", "stop", unread); got == nil || got.Reason != serviceActionNoteUnitNotRead ||
		got.Vars["pending_unit"] != "postfix@-.service" || got.Vars["command"] != "systemctl status postfix@-.service" {
		t.Fatalf("note = %+v", got)
	} else if _, has := got.Vars["state"]; has || !strings.Contains(got.Error, "could not be read from systemd after the stop") ||
		!strings.Contains(got.Error, "is not known") {
		t.Fatalf("note = %+v", got)
	}
	// A state that is not one of systemd's words is not repeated either.
	odd := pending
	odd.NoticeResult = "deactivating; $(x)"
	if _, got = note("postfix", "stop", odd); got == nil || got.Reason != serviceActionNoteUnitNotRead {
		t.Fatalf("note = %+v", got)
	} else if _, has := got.Vars["state"]; has {
		t.Fatalf("a state that is not systemd's was repeated: %+v", got)
	}

	// No note: another action, a failure, a unit name that is not one.
	if _, got = note("postfix", "restart", pending); got != nil {
		t.Fatal("a restart carries the stop's note")
	}
	failed := pending
	failed.Success = false
	if _, got = note("postfix", "stop", failed); got != nil {
		t.Fatal("a failure carries a note")
	}
	hostile := pending
	hostile.NoticeUnit = "postfix@-.service; rm -rf /"
	if _, got = note("postfix", "stop", hostile); got != nil {
		t.Fatal("a unit name that is not one was repeated")
	}
}

// The operation guidance records, word for word, the sentences this entry
// added to an answer, in its English and its Turkish edition.
func TestTheGuidanceRecordsTheSentencesAfterSet4(t *testing.T) {
	sentences := map[string]string{
		"stop, the unit had not settled":   serviceActionNoteMessage(serviceActionNoteUnitNotSettled),
		"stop, the unit could not be read": serviceActionNoteMessage(serviceActionNoteUnitNotRead),
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
