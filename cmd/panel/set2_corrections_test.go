package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/services"
	"github.com/alicelik/celikpanel/internal/transport"
)

// Corrections from the second native measurement (11 Oct 2026).
// İkinci yerel ölçümün düzeltmeleri.

// cryptHashShaped matches a password hash in the modular crypt format that
// cPanel's shadow files, Dovecot and the engines use: `$id$...` with one of
// the known scheme identifiers. A test that finds one in an answer, a stored
// answer or a log line has found a leaked secret.
var cryptHashShaped = regexp.MustCompile(`\$(?:1|2[abxy]?|5|6|7|y|gy|sha1|apr1|argon2(?:id|i|d)?|pbkdf2(?:-sha(?:1|256|512))?|scram-sha-256)\$[^\s"']{4,}`)

const (
	set2MailboxHash  = "$6$rounds=5000$Xo3pQ1saltSALT$u9q0n3h8Jx2m4tT1wYb7s5kPzQ2vN8rL0cD6fG4hJ1aS3dF5gH7jK9lZ1xC3vB5nM7qW9eR2tY4uI6oP8aS0dF"
	set2BcryptHash   = "$2y$10$abcdefghijklmnopqrstuuABCDEFGHIJKLMNOPQRSTUVWXYZ0123456"
	set2ArgonHash    = "$argon2id$v=19$m=65536,t=3,p=4$c29tZXNhbHQ$RdescudvJCsgt3ub+b+dWRWJTmaaJObG"
	set2ImportedPath = "/var/lib/celikpanel-imports/cpmove-olduser.tar.gz"
)

func TestCryptHashShapedFindsTheSchemesAnArchiveCarries(t *testing.T) {
	for _, hash := range []string{set2MailboxHash, set2BcryptHash, set2ArgonHash, "$1$salt$abcdefghijklmnopqrstuv", "$5$salt$abcdefghijklmnopqrstuvwxyz"} {
		if !cryptHashShaped.MatchString(`{"x":"` + hash + `"}`) {
			t.Fatalf("a %s hash is not recognised", hash[:4])
		}
	}
	for _, harmless := range []string{`{"detail":"3 files, 120 bytes"}`, `{"price":"$5 a month"}`, `{"command":"echo $HOME"}`} {
		if cryptHashShaped.MatchString(harmless) {
			t.Fatalf("%s was taken for a hash", harmless)
		}
	}
}

func set2Preview() transport.CpmoveInspectResponse {
	return transport.CpmoveInspectResponse{
		Username: "olduser", MainDomain: "imported.example", Domains: []string{"imported.example"},
		PublicHTML: true, SiteBytes: 2048,
		MailAccounts: []transport.CpmoveMailAccount{
			{Domain: "imported.example", User: "info", QuotaMB: 250, HasPassword: true, CryptHash: set2MailboxHash},
			{Domain: "imported.example", User: "sales", QuotaMB: 100, HasPassword: true, CryptHash: set2BcryptHash},
			{Domain: "imported.example", User: "old", HasPassword: true, CryptHash: set2ArgonHash},
			{Domain: "imported.example", User: "suspended"},
		},
		Forwarders: []transport.CpmoveForwarder{{Source: "hello@imported.example", Destination: "info@imported.example"}},
		DNSZones:   map[string][]transport.CpmoveDNSRecord{"imported.example": {{Name: "imported.example", Type: "A", Content: "192.0.2.10", TTL: 3600}}},
		Databases:  []transport.CpmoveDatabase{{Name: "olduser_shop", DumpBytes: 4096}},
	}
}

func readPanelSource(t *testing.T, name string) string {
	t.Helper()
	source, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(source), "\r\n", "\n")
}

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var captured bytes.Buffer
	previous, flags := log.Writer(), log.Flags()
	log.SetOutput(&captured)
	t.Cleanup(func() { log.SetOutput(previous); log.SetFlags(flags) })
	return &captured
}

// S1. The measured defect: `POST /api/v1/import/cpanel/inspect` answered each
// mailbox's password hash (`mail_accounts[].crypt_hash`). The fake Agent here
// hands the Panel the hashes whether asked or not, which is what an Agent with
// the old behaviour would do: the answer must not carry one even then.
func TestImportPreviewNeverAnswersAPasswordHash(t *testing.T) {
	panel, agent, userID, _, _ := newRequestIdentityHostingFixture(t)
	agent.preview = set2Preview()
	logged := captureLog(t)

	recorder := httptest.NewRecorder()
	request := requestIdentityTestRequest("/api/v1/import/cpanel/inspect", `{"path":"`+set2ImportedPath+`"}`, "", &Caller{ID: userID, Role: roleAdmin})
	panel.handleImportInspect(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	if found := cryptHashShaped.FindString(body); found != "" {
		t.Fatalf("the preview answers a password hash (%s...): %s", found[:6], body)
	}
	for _, forbidden := range []string{"crypt_hash", "CryptHash", "hash\"", set2MailboxHash[:20], "Xo3pQ1saltSALT"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("the preview contains %q: %s", forbidden, body)
		}
	}
	if found := cryptHashShaped.FindString(logged.String()); found != "" {
		t.Fatalf("a password hash was logged: %s", logged.String())
	}

	// What it does say about a mailbox: address, quota, and whether the
	// archive holds a password that the import will keep. Nothing else.
	var answer struct {
		MailAccounts []map[string]any `json:"mail_accounts"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	if len(answer.MailAccounts) != 4 {
		t.Fatalf("mail accounts = %v", answer.MailAccounts)
	}
	for index, mailbox := range answer.MailAccounts {
		if len(mailbox) != 4 {
			t.Fatalf("mailbox %d has other fields than domain, user, quota_mb, has_password: %v", index, mailbox)
		}
		for _, key := range []string{"domain", "user", "quota_mb", "has_password"} {
			if _, present := mailbox[key]; !present {
				t.Fatalf("mailbox %d lacks %s: %v", index, key, mailbox)
			}
		}
		if want := index < 3; mailbox["has_password"] != want {
			t.Fatalf("mailbox %v: has_password=%v, want %t", mailbox["user"], mailbox["has_password"], want)
		}
	}

	// A preview does not even ask the Agent for the hashes.
	agent.mu.Lock()
	asked := append([]transport.CpmoveInspectRequest(nil), agent.inspectRequests...)
	agent.mu.Unlock()
	if len(asked) != 1 || asked[0].IncludeMailHashes || asked[0].Path != set2ImportedPath {
		t.Fatalf("the preview asked the Agent: %+v", asked)
	}
}

// The type the Agent answers with cannot be encoded with a hash at all, so no
// other answer, stored answer or log line built from it can carry one.
func TestTheAgentsArchiveAnswerNeverEncodesAPasswordHash(t *testing.T) {
	preview := set2Preview()
	for name, value := range map[string]any{
		"the Agent's answer":  preview,
		"a pointer to it":     &preview,
		"one mailbox":         preview.MailAccounts[0],
		"the browser preview": importPreviewFor(&preview),
		"a request for it":    transport.CpmoveInspectRequest{Path: set2ImportedPath, IncludeMailHashes: true},
	} {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if found := cryptHashShaped.FindString(string(encoded)); found != "" || strings.Contains(string(encoded), "crypt_hash") {
			t.Fatalf("%s encodes a password hash: %s", name, encoded)
		}
	}
	// The preview keeps the one fact, also for an Agent that sent only a hash.
	onlyHash := transport.CpmoveInspectResponse{MailAccounts: []transport.CpmoveMailAccount{{Domain: "a.example", User: "x", CryptHash: set2MailboxHash}}}
	if got := importPreviewFor(&onlyHash); len(got.MailAccounts) != 1 || !got.MailAccounts[0].HasPassword {
		t.Fatalf("preview = %+v", got)
	}
}

// The apply reads the archive again on the server and asks for the hashes
// there, and only when mail is imported. The browser sends the archive's path
// and the owner's choices; a body that tries to hand hashes in is not read for
// them. Neither the apply's answer nor its stored answer nor a log line holds
// a hash.
func TestImportApplyTakesHashesFromTheServerSideArchiveOnly(t *testing.T) {
	for _, c := range []struct {
		name       string
		body       string
		wantHashes bool
	}{
		{"mail chosen", `"do_mail":true,"do_files":true`, true},
		{"mail not chosen", `"do_files":true,"do_databases":true`, false},
		{"hashes handed in by the browser", `"do_mail":false,"mail_accounts":[{"domain":"imported.example","user":"info","crypt_hash":"` + set2BcryptHash + `"}]`, false},
	} {
		panel, agent, userID, subscriptionID, _ := newRequestIdentityHostingFixture(t)
		agent.preview = set2Preview()
		logged := captureLog(t)
		guard := newRequestIdentityGuard(panel.db.GetDB())
		handler := guard.wrap(requestIdentityRoutesMux(panel))
		id := requestIdentityTestID(20480 + len(c.name))
		body := fmt.Sprintf(`{"path":"%s","subscription_id":%d,"domain":"imported.example",%s}`, set2ImportedPath, subscriptionID, c.body)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, requestIdentityTestRequest("/api/v1/import/cpanel/apply", body, id, &Caller{ID: userID, Role: roleAdmin}))

		agent.mu.Lock()
		asked := append([]transport.CpmoveInspectRequest(nil), agent.inspectRequests...)
		agent.mu.Unlock()
		if len(asked) != 1 || asked[0].IncludeMailHashes != c.wantHashes {
			t.Fatalf("%s: the apply asked the Agent %+v, want hashes=%t", c.name, asked, c.wantHashes)
		}
		row := readRequestIdentityRow(t, panel.db.GetDB(), id)
		for what, text := range map[string]string{
			"the answer": recorder.Body.String(), "the stored answer": string(row.body), "the log": logged.String(),
		} {
			if found := cryptHashShaped.FindString(text); found != "" {
				t.Fatalf("%s: %s holds a password hash: %s", c.name, what, text)
			}
		}
		var stored string
		if err := panel.db.GetDB().QueryRow(`SELECT COALESCE(GROUP_CONCAT(COALESCE(route,'') || COALESCE(response_content_type,'') || COALESCE(CAST(response_body AS TEXT),''), ' '), '') FROM request_identities`).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		if cryptHashShaped.MatchString(stored) {
			t.Fatalf("%s: a request identity row holds a password hash", c.name)
		}
	}
}

// P4. An import whose files step failed is a verified partial result. It is
// not "pending": nothing is still running and nothing completes it.
func TestImportAnswerNamesWhatWasImportedAndWhatWasNot(t *testing.T) {
	steps := []importStep{
		{Step: "domain", OK: true, Detail: "imported.example (id 7, site 3) → /var/www/celikpanel/subscriptions/1/sites/7/public_html"},
		{Step: "files", OK: false, Detail: "unsafe cpmove member path"},
		{Step: "mail", OK: true, Detail: "1 accounts imported with original passwords (mailbox CONTENTS are not migrated in v1)"},
		{Step: "mail:suspended@imported.example", OK: false, Detail: importMailboxWithoutPassword},
		{Step: "forwarders", OK: true, Detail: "1 forwarders"},
		{Step: "dns", OK: true, Detail: "external DNS ownership preserved; verify provider records before publishing the site"},
		{Step: "database:olduser_shop", OK: true, Detail: "created exclusively and dump imported (db USERS are not migrated; repoint app configs)"},
	}
	answer := importApplyAnswerFor("imported.example", 7, 3, steps)
	if answer.Status != "partial" || answer.Code != errCodeImportPartial || answer.DomainStatus != "pending" {
		t.Fatalf("answer = %+v", answer)
	}
	if got := strings.Join(answer.NotImported, ","); got != "files,mail:suspended@imported.example" {
		t.Fatalf("not imported = %s", got)
	}
	if got := strings.Join(answer.Imported, ","); got != "domain,mail,forwarders,dns,database:olduser_shop" {
		t.Fatalf("imported = %s", got)
	}
	// D-024: what happened, what is in place, who acts, the concrete next
	// actions, and that nothing resumes by itself.
	for _, fragment := range []string{
		"does not continue by itself", "Imported: domain, mail, forwarders, dns, database:olduser_shop.",
		"Not imported: files, mail:suspended@imported.example.", "imported.example was created and is kept",
		"server owner", "by hand", "removes imported.example on the Domains page", "imports the archive again", "nothing is imported twice",
	} {
		if !strings.Contains(answer.Message, fragment) {
			t.Fatalf("message lacks %q: %s", fragment, answer.Message)
		}
	}
	encoded, err := json.Marshal(answer)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"status":"pending"`) || cryptHashShaped.MatchString(string(encoded)) {
		t.Fatalf("answer = %s", encoded)
	}

	// Every part imported: the domain is in service and nothing is listed as
	// missing.
	complete := importApplyAnswerFor("imported.example", 7, 3, []importStep{
		{Step: "domain", OK: true}, {Step: "files", OK: true}, {Step: "dns", OK: true},
	})
	if complete.Status != "active" || complete.DomainStatus != "active" || complete.Code != "" || complete.Message != "" ||
		len(complete.NotImported) != 0 || len(complete.Imported) != 3 {
		t.Fatalf("complete answer = %+v", complete)
	}
	encoded, _ = json.Marshal(complete)
	if !strings.Contains(string(encoded), `"not_imported":[]`) {
		t.Fatalf("an empty list is not encoded as a list: %s", encoded)
	}

	// Every part imported but the domain could not be marked as finished: it
	// is partial, and the message says that rather than naming a missing part.
	unfinished := importApplyAnswerFor("imported.example", 7, 3, []importStep{
		{Step: "domain", OK: true}, {Step: "files", OK: true}, {Step: "finalize", OK: false, Detail: "update domain import status"},
	})
	if unfinished.Status != "partial" || len(unfinished.NotImported) != 0 ||
		!strings.Contains(unfinished.Message, "could not be marked as finished") {
		t.Fatalf("unfinished answer = %+v", unfinished)
	}
}

// The import handler no longer answers 202: every answer it gives is about
// work that has ended.
func TestImportHandlerNeverAnswersAccepted(t *testing.T) {
	source := readPanelSource(t, "import_handlers.go")
	if strings.Contains(source, "StatusAccepted") || strings.Contains(source, `status := "pending"`) {
		t.Fatal("the import handler still answers as if work were in progress")
	}
	for _, needed := range []string{"importApplyAnswerFor(req.Domain, domainID, siteID, steps)", "importPreviewFor(preview)", "p.inspectCpmove(r.Context(), req.Path, false)", "p.inspectCpmove(r.Context(), req.Path, req.DoMail)"} {
		if !strings.Contains(source, needed) {
			t.Fatalf("import_handlers.go no longer contains %s", needed)
		}
	}
}

// O9. A certbot run that did not issue is answered with what it came to,
// never "internal server error".
func TestCertificateIssueFailureIsTypedAndPlain(t *testing.T) {
	measured := "An unexpected error occurred: requests.exceptions.SSLError: HTTPSConnectionPool(host='acme-v02.api.letsencrypt.org', port=443): Max retries exceeded with url: /directory"
	for kind, fragments := range map[string][]string{
		transport.CertificateFailureAuthorityUnreachable: {"could not reach the certificate authority", "no request was placed", "outbound port 443"},
		transport.CertificateFailureValidation:           {"could not validate one of the names", "resolve publicly to this server", "port 80"},
		transport.CertificateFailureRateLimited:          {"one of its limits was reached", "counts against it", "after that time"},
		transport.CertificateFailureTimeout:              {"did not finish within the time allowed", "not known from this answer"},
		transport.CertificateFailureTool:                 {"certbot ended with an error", "not classified here"},
	} {
		for _, hadCertificate := range []bool{false, true} {
			recorder := httptest.NewRecorder()
			reply := &transport.IssueLetsEncryptResponse{Error: "certbot failed: exit status 1\nOutput: " + measured, Failure: kind, FailureDetail: measured}
			if !writeCertificateIssueFailure(recorder, &Caller{ID: 1, Role: roleAdmin}, "shop.example", hadCertificate, reply) {
				t.Fatalf("%s was not answered", kind)
			}
			var body apiErrorBody
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if recorder.Code != http.StatusBadGateway || body.Code != errCodeCertificateIssueFailed || body.Reason != kind {
				t.Fatalf("%s: status %d body %+v", kind, recorder.Code, body)
			}
			kept := "has no certificate from this request"
			if hadCertificate {
				kept = "already had is still in place"
			}
			for _, fragment := range append(fragments, "No certificate was issued", kept, "Nothing asks again automatically") {
				if !strings.Contains(body.Error, fragment) {
					t.Fatalf("%s: sentence lacks %q: %s", kind, fragment, body.Error)
				}
			}
			if strings.Contains(body.Error, "internal server error") || strings.Contains(body.Error, "HTTPSConnectionPool") {
				t.Fatalf("%s: %s", kind, body.Error)
			}
			if body.Vars["domain"] != "shop.example" || !strings.Contains(body.Vars["detail"], "Max retries exceeded") {
				t.Fatalf("%s: vars %v", kind, body.Vars)
			}
		}
	}

	// certbot's line can name this server's paths and hosts: it is for an
	// administrator only.
	recorder := httptest.NewRecorder()
	writeCertificateIssueFailure(recorder, &Caller{ID: 2, Role: roleCustomer}, "shop.example", false,
		&transport.IssueLetsEncryptResponse{Error: "x", Failure: transport.CertificateFailureValidation, FailureDetail: measured})
	if strings.Contains(recorder.Body.String(), "Max retries") || !strings.Contains(recorder.Body.String(), `"reason":"validation"`) {
		t.Fatalf("answer to a customer: %s", recorder.Body.String())
	}

	// What the Agent did not classify is left to the caller, as before.
	for name, reply := range map[string]*transport.IssueLetsEncryptResponse{
		"nil": nil, "success": {Success: true}, "an older Agent": {Error: "certbot failed: exit status 1"},
		"a kind this Panel does not know": {Error: "x", Failure: "something-new"},
	} {
		recorder := httptest.NewRecorder()
		if writeCertificateIssueFailure(recorder, nil, "shop.example", false, reply) || recorder.Body.Len() != 0 {
			t.Fatalf("%s was answered: %s", name, recorder.Body.String())
		}
	}
}

// O10. A database created with a password the owner sent: the answer does not
// repeat the password, so it is an ordinary answer, kept and replayed like any
// other. A minted password is still answered once and not kept.
func TestSentDatabasePasswordIsNotEchoedAndItsAnswerIsReplayed(t *testing.T) {
	fixture := newDatabaseV2SecurityFixture(t)
	driver := useRecordingDatabaseDriver(t)
	guard := newRequestIdentityGuard(fixture.sql)
	handler := guard.wrap(requestIdentityRoutesMux(fixture.panel))
	path := fmt.Sprintf("/api/v1/database-servers/%d/databases", dbSecurityServer)
	caller := &Caller{ID: dbSecurityOwnerID, Role: roleCustomer}

	sent := `{"database_name":"shop","new_username":"shopper","new_password":"the-owner-chose-this-one"}`
	id := requestIdentityTestID(24577)
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, requestIdentityTestRequest(path, sent, id, caller))
	if first.Code != http.StatusOK || strings.Contains(first.Body.String(), "the-owner-chose-this-one") ||
		!strings.Contains(first.Body.String(), `"password_set":true`) || strings.Contains(first.Body.String(), `"password":`) {
		t.Fatalf("first answer: %d %s", first.Code, first.Body.String())
	}
	if driver.createdPassword != "the-owner-chose-this-one" {
		t.Fatalf("the engine was not given the owner's password: %+v", driver)
	}
	row := readRequestIdentityRow(t, fixture.sql, id)
	if row.retained != 1 || strings.Contains(string(row.body), "the-owner-chose-this-one") {
		t.Fatalf("stored row: %+v", row)
	}
	replay := httptest.NewRecorder()
	handler.ServeHTTP(replay, requestIdentityTestRequest(path, sent, id, caller))
	if replay.Code != http.StatusOK || replay.Header().Get(requestIdentityReplayedHeader) != "1" || replay.Body.String() != first.Body.String() {
		t.Fatalf("replay: %d %s", replay.Code, replay.Body.String())
	}
	if driver.createDatabaseCalls != 1 {
		t.Fatalf("the replay created the database again (%d calls)", driver.createDatabaseCalls)
	}

	// A password this request minted is answered once and its answer is not kept.
	minted := `{"database_name":"blog","new_username":"blogger"}`
	mintedID := requestIdentityTestID(24578)
	once := httptest.NewRecorder()
	handler.ServeHTTP(once, requestIdentityTestRequest(path, minted, mintedID, caller))
	var answer map[string]any
	if err := json.Unmarshal(once.Body.Bytes(), &answer); err != nil || once.Code != http.StatusOK {
		t.Fatalf("minted answer: %d %s", once.Code, once.Body.String())
	}
	password, _ := answer["password"].(string)
	if len(password) < 12 || answer["password_set"] != true {
		t.Fatalf("minted answer: %v", answer)
	}
	if row := readRequestIdentityRow(t, fixture.sql, mintedID); row.retained != 0 || strings.Contains(string(row.body), password) {
		t.Fatalf("the minted password's answer was kept: %+v", row)
	}
}

// The same for a database user created on its own.
func TestSentDatabaseUserPasswordIsNotEchoed(t *testing.T) {
	source := readPanelSource(t, "database_v2_handlers.go")
	for _, needed := range []string{`"password_set": true,`, "if minted {\n\t\tanswer[\"password\"] = password", "if newUserSecretMinted {\n\t\tresponse[\"password\"] = newUserSecret"} {
		if !strings.Contains(source, needed) {
			t.Fatalf("database_v2_handlers.go no longer contains %q", needed)
		}
	}
	if strings.Contains(source, "\"password\":   password,") {
		t.Fatal("the database user answer carries the password unconditionally")
	}
}

// O14. The Databases page showed "15.1" for a MariaDB 10.11.14 (Ubuntu 24.04:
// the client's version, from the service scan) and "VERSION()" on Arch. The
// running engine is asked whenever the Panel has an account on it, whatever
// the scan recorded: one method on every platform.
func TestTheEngineIsAskedForItsVersionWhateverTheScanRecorded(t *testing.T) {
	for _, scanned := range []string{"15.1", "VERSION()", "unknown", "", "10.11.14"} {
		agent := &databaseAdminAccountAgent{services: []core.Service{
			{Name: "mariadb.service", Version: scanned, Status: "running"},
		}}
		panel, _ := newDatabaseAdminAccountFixture(t, agent)
		previous := newDatabaseDriver
		newDatabaseDriver = func(services.DriverConfig) (services.DatabaseDriver, error) {
			return &versionAnsweringDriver{version: "10.11.14-MariaDB-0ubuntu0.24.04.1"}, nil
		}
		t.Cleanup(func() { newDatabaseDriver = previous })
		if err := panel.ensureInstalledDBServers(context.Background(), adminAccountSubscriptionID); err != nil {
			t.Fatal(err)
		}
		if got := onlyDatabaseServer(t, panel).Version; got != "10.11.14-MariaDB-0ubuntu0.24.04.1" {
			t.Fatalf("scan said %q: recorded version %q, want what the engine answered", scanned, got)
		}
	}
}

// The operation guidance records, word for word, every sentence this entry
// added to an answer, in its English and its Turkish edition.
func TestTheGuidanceRecordsTheSentencesOfTheSecondMeasurement(t *testing.T) {
	sentences := map[string]string{
		"reload":            serviceActionFailedMessages["reload"],
		"reload_reread":     serviceActionFailedMessages[transport.ServiceActionStageReloadReread],
		"reload_not_reread": serviceActionFailedMessages[transport.ServiceActionStageReloadNotReread],
		"not_running":       serviceActionFailedMessages[transport.ServiceActionStageNotRunning],
		"partial import":    importPartialMessage("{domain}", []string{"{imported}"}, []string{"{not imported}"}),
		"site not created":  importSiteNotCreatedMessage,
		"no password":       importMailboxWithoutPassword,
		"kept":              strings.TrimSpace(certificateIssueKept),
		"none":              strings.TrimSpace(certificateIssueNone),
		"resume":            strings.TrimSpace(certificateIssueResume),
	}
	for kind, sentence := range certificateIssueFailedMessages {
		sentences["certificate "+kind] = sentence
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
