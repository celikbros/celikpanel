package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/mailtlsconfig"
)

// A value read from postconf is one line, never the buffer (2026-10-09). The
// bytes are those the real postconf wrote with a main.cf that makes it warn:
// deploy/e2e/release-recovery/evidence/set4c-20261009 (Postfix 3.10.13,
// Debian 13, a private configuration directory) and set4b-20261009 (Postfix
// 3.8.6, Ubuntu 24.04). Both streams are in the one buffer the Agent reads.
const (
	// `# comment after other text`: the warning comes first.
	measuredCommentWarning = "/usr/sbin/postconf: warning: /etc/postfix/main.cf: #comment after other text is not allowed: # raised for the campa...\n"
	// An unused parameter: the warning comes after the value.
	measuredUnusedWarning = "/usr/sbin/postconf: warning: /etc/postfix/main.cf: unused parameter: campaign_note=raised for the campaign\n"
	// The same, with postconf started by its bare name.
	measuredUnusedWarningBare = "postconf: warning: /etc/postfix/main.cf: unused parameter: campaign_note=raised for the campaign\n"
	measuredMasterWarning     = "/usr/sbin/postconf: warning: open /etc/postfix/master.cf: No such file or directory\n"
	measuredCertValue         = "/etc/ssl/celikpanel-mail/default.crt"
)

func TestPostconfValueIsOneLineOfTheAnswer(t *testing.T) {
	for _, c := range []struct {
		name, out, want string
		known           bool
	}{
		{"no warning", measuredCertValue + "\n", measuredCertValue, true},
		{"measured: the warning first", measuredCommentWarning + measuredCertValue + "\n", measuredCertValue, true},
		{"measured: the warning after the value", measuredCertValue + "\n" + measuredUnusedWarning, measuredCertValue, true},
		{"measured: three lines, the value between two warnings", measuredMasterWarning + measuredCertValue + "\n" + measuredUnusedWarning, measuredCertValue, true},
		{"measured: a setting that is not set", "\n", "", true},
		{"measured: not set, the warning after the empty line", "\n" + measuredUnusedWarningBare, "", true},
		{"not set, the warning before the empty line", measuredCommentWarning + "\n", "", true},
		{"an expanded value", "hash:/etc/aliases\n" + measuredUnusedWarning, "hash:/etc/aliases", true},
		{"a carriage return before the line break", measuredCertValue + "\r\n", measuredCertValue, true},
		// Not one value: nothing is taken from these.
		{"nothing at all", "", "", false},
		{"only a warning: no value line was printed", measuredUnusedWarning, "", false},
		{"only a fatal message", "/usr/sbin/postconf: fatal: open /etc/postfix/main.cf: No such file or directory\n", "", false},
		{"two value lines", measuredCertValue + "\n/etc/ssl/another.crt\n", "", false},
		{"a value that was cut before its line break", measuredCertValue, "", false},
		{"a message that was cut", measuredCertValue + "\n" + strings.TrimSuffix(measuredUnusedWarning, "\n"), "", false},
		{"a line of another program", "ld.so: object 'libx.so' cannot be preloaded\n" + measuredCertValue + "\n", "", false},
		{"a message of another program of Postfix", "postfix: warning: something\n" + measuredCertValue + "\n", "", false},
	} {
		got, known := postconfOneValue([]byte(c.out))
		if got != c.want || known != c.known {
			t.Errorf("%s: got %q, %v; want %q, %v", c.name, got, known, c.want, c.known)
		}
	}
	// What the readers did before: the trimmed buffer. For a setting that is
	// not set it is the warning line alone, one line, which `postconf -e`
	// accepts (measured: it was written into a private main.cf).
	whole := strings.TrimSpace("\n" + measuredUnusedWarning)
	if strings.Contains(whole, "\n") || !postconfOwnMessage.MatchString(whole) || postconfRestorable(whole) {
		t.Fatalf("the old reading of an unset setting is not the one line that was measured: %q", whole)
	}
	if !postconfRestorable(measuredCertValue) || !postconfRestorable("") || postconfRestorable(measuredCertValue+"\n/x") {
		t.Fatal("postconfRestorable does not tell a value from text that is not one")
	}
}

// postconfHost answers `postconf -h <name>` as the measured Postfix does:
// settings that are set print their line, the others an empty line, and the
// warnings stand where they were measured.
type postconfHost struct {
	values map[string]string
	// before / after are written to the error stream before / after the value.
	before, after string
	// extra is appended as a second value line for this setting.
	extraLineFor string
	calls        []string
}

func (h *postconfHost) run(name string, args ...string) ([]byte, error) {
	h.calls = append(h.calls, filepath.Base(name)+" "+strings.Join(args, " "))
	switch filepath.Base(name) {
	case "dovecot":
		return []byte("2.4.1\n"), nil
	case "postconf":
		if len(args) == 2 && args[0] == "-h" {
			out := h.before + h.values[args[1]] + "\n" + h.after
			if args[1] == h.extraLineFor {
				out += "an unexpected second line\n"
			}
			return []byte(out), nil
		}
	}
	// Everything else is a change or a service command: answered, and counted.
	return nil, nil
}

func (h *postconfHost) onlyRead(t *testing.T) {
	t.Helper()
	for _, call := range h.calls {
		if call != "dovecot --version" && !strings.HasPrefix(call, "postconf -h ") {
			t.Fatalf("something other than a reading was sent: %q (all: %v)", call, h.calls)
		}
	}
}

// The snapshot keeps what postconf printed as the value, with a warning
// before it, after it, or after the empty line of a setting that is not set.
func TestMailTLSSnapshotKeepsValuesNotWarnings(t *testing.T) {
	for _, c := range []struct{ name, before, after string }{
		{"the warning first", measuredCommentWarning, ""},
		{"the warning after", "", measuredUnusedWarning},
		{"both", measuredMasterWarning, measuredUnusedWarning},
	} {
		host := &postconfHost{before: c.before, after: c.after, values: map[string]string{
			"smtpd_tls_cert_file": measuredCertValue, "smtpd_tls_key_file": "/etc/ssl/celikpanel-mail/default.key",
			"smtpd_tls_security_level": "may", "myhostname": "mail.set4c.test",
		}}
		settings, err := snapshotPostfixTLSSettings(host.run)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if len(settings) != len(postfixTLSManagedSettings) {
			t.Fatalf("%s: %d settings", c.name, len(settings))
		}
		for _, setting := range settings {
			if setting.value != host.values[setting.name] || strings.Contains(setting.value, "warning") || !postconfRestorable(setting.value) {
				t.Fatalf("%s: %s was kept as %q", c.name, setting.name, setting.value)
			}
		}
		host.onlyRead(t)
	}
}

// A setting whose value cannot be read as one value ends the operation at the
// snapshot: before the default certificate, before any `postconf -e`, before
// any reload. The answer says what to run.
func TestMailTLSRefusesAtTheSnapshotBeforeAnyChange(t *testing.T) {
	previousLookup := lookupMailTLSCommand
	lookupMailTLSCommand = func(name string) (string, error) { return "/usr/sbin/" + name, nil }
	t.Cleanup(func() { lookupMailTLSCommand = previousLookup })
	t.Setenv("CELIKPANEL_MAIL_DIR", "")

	host := &postconfHost{after: measuredUnusedWarning, extraLineFor: "smtpd_tls_security_level", values: map[string]string{
		"smtpd_tls_cert_file": measuredCertValue, "smtpd_tls_key_file": "/etc/ssl/celikpanel-mail/default.key",
	}}
	var response SecureMailTLSResponse
	outcome, err := reconcileMailTLSHost(&SecureMailTLSRequest{Myhostname: "mail.set4c.test"}, &response, host.run)
	if err != nil {
		t.Fatal(err)
	}
	if outcome != mailTLSHostUntouched || response.Configured {
		t.Fatalf("outcome = %v, response = %+v", outcome, response)
	}
	for _, fragment := range []string{
		"mail TLS snapshot: nothing was changed", "the value of the Postfix setting smtpd_tls_security_level is not known",
		"`postconf -h smtpd_tls_security_level` did not print exactly one value line besides its own messages",
		"The server owner runs `postconf -n` and `postfix check`", "/etc/postfix/main.cf", "nothing retries by itself",
	} {
		if !strings.Contains(response.Error, fragment) {
			t.Fatalf("the answer lacks %q: %s", fragment, response.Error)
		}
	}
	// The warning's own text, which names what the owner wrote, is not repeated.
	if strings.Contains(response.Error, "campaign") {
		t.Fatalf("the answer repeats postconf's output: %s", response.Error)
	}
	host.onlyRead(t)
	// It stopped at the setting it could not read: the two before it were read.
	if got := strings.Join(host.calls, "; "); got != "dovecot --version; postconf -h smtpd_tls_cert_file; postconf -h smtpd_tls_key_file; postconf -h smtpd_tls_security_level" {
		t.Fatalf("calls = %s", got)
	}

	_, err = snapshotPostfixTLSSettings((&postconfHost{extraLineFor: "myhostname"}).run)
	var unread *postconfUnreadError
	if !errors.As(err, &unread) || unread.setting != "myhostname" || unread.command != "postconf -h myhostname" {
		t.Fatalf("error = %v", err)
	}
}

// A restore writes values. Text that is not one value (the old reading of a
// setting that was not set: the warning line; or two lines) is never handed
// to `postconf -e`, and the setting is reported as not restored.
func TestMailTLSRestoreNeverWritesAWarningLine(t *testing.T) {
	var written []string
	run := func(name string, args ...string) ([]byte, error) {
		if name != "postconf" || len(args) != 2 || args[0] != "-e" {
			t.Fatalf("unexpected command: %s %v", name, args)
		}
		written = append(written, args[1])
		return nil, nil
	}
	restoreErrors := restorePostfixTLSSettings([]postfixTLSSettingSnapshot{
		{name: "smtpd_tls_cert_file", value: measuredCertValue},
		{name: "tls_server_sni_maps", value: strings.TrimSpace("\n" + measuredUnusedWarning)},
		{name: "smtpd_tls_key_file", value: strings.TrimSpace(measuredCommentWarning + "/etc/ssl/celikpanel-mail/default.key\n")},
		{name: "smtp_tls_protocols", value: ""},
	}, run)
	if got := strings.Join(written, "; "); got != "smtpd_tls_cert_file="+measuredCertValue+"; smtp_tls_protocols=" {
		t.Fatalf("written = %s", got)
	}
	if len(restoreErrors) != 2 {
		t.Fatalf("errors = %v", restoreErrors)
	}
	for index, name := range []string{"tls_server_sni_maps", "smtpd_tls_key_file"} {
		text := restoreErrors[index].Error()
		if !strings.Contains(text, "restore postconf "+name) || !strings.Contains(text, "nothing was written for this setting") || strings.Contains(text, "campaign") {
			t.Fatalf("error = %s", text)
		}
	}
}

// The read-back compares the value postconf printed. A warning in the buffer
// does not make matching settings differ, does not make differing settings
// match, and a reading that is not one value is not verified.
func TestMailTLSReadBackComparesTheValue(t *testing.T) {
	raw, err := os.ReadFile("../../internal/mailtlsartifact/testdata/alpha81-empty.json")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := decodeMailTLSSyncJournal(raw)
	if err != nil {
		t.Fatal(err)
	}
	const cert, key = "/accepted/cert.pem", "/accepted/key.pem"
	read := func(string) ([]byte, error) { return []byte(mailtlsconfig.Dovecot(true, cert, key, nil)), nil }
	committed := func() *postconfHost {
		host := &postconfHost{values: map[string]string{}}
		for _, setting := range mailtlsconfig.PostfixSettings(plan.Myhostname, cert, key) {
			host.values[setting[0]] = setting[1]
		}
		return host
	}
	for _, c := range []struct{ name, before, after string }{
		{"no warning", "", ""}, {"the warning first", measuredCommentWarning, ""},
		{"the warning after", "", measuredUnusedWarning}, {"both", measuredMasterWarning, measuredUnusedWarningBare},
	} {
		host := committed()
		host.before, host.after = c.before, c.after
		if err := verifyMailTLSConfiguration(plan, cert, key, host.run, read); err != nil {
			t.Fatalf("%s: the committed settings were not verified: %v", c.name, err)
		}
		host.onlyRead(t)
	}

	// A setting that differs is a difference whatever the warning holds: here
	// the warning names the committed value.
	drifted := committed()
	drifted.values["smtpd_tls_cert_file"] = "/owner/other.pem"
	drifted.after = "postconf: warning: /etc/postfix/main.cf: unused parameter: note=" + cert + "\n"
	if err := verifyMailTLSConfiguration(plan, cert, key, drifted.run, read); err == nil {
		t.Fatal("a differing setting was accepted because a warning held the committed value")
	}
	// An SNI map the plan does not have, behind a warning.
	sni := committed()
	sni.values["tls_server_sni_maps"] = "hash:/owner/map"
	sni.before = measuredCommentWarning
	if err := verifyMailTLSConfiguration(plan, cert, key, sni.run, read); err == nil {
		t.Fatal("an SNI map outside the plan was accepted")
	}

	// A reading that is not one value: unknown, and said as that.
	unread := committed()
	unread.extraLineFor = "smtpd_tls_cert_file"
	err = verifyMailTLSConfiguration(plan, cert, key, unread.run, read)
	var unknown *postconfUnreadError
	if !errors.As(err, &unknown) || unknown.setting != "smtpd_tls_cert_file" || !strings.Contains(err.Error(), "the configuration was not verified") {
		t.Fatalf("error = %v", err)
	}
	unread.onlyRead(t)
	// On the certificate path the owner still gets that path's own sentence.
	public := &mailHostConfigurationUnverified{cause: err}
	if !strings.Contains(public.Error(), "server owner") || !errors.As(public, &unknown) {
		t.Fatalf("public = %v", public)
	}
}

// The expanded reading follows the same rule; its source is pinned because it
// runs postconf itself.
func TestPostconfExpandedReadFollowsTheRule(t *testing.T) {
	source, err := os.ReadFile("mail_stack_rpc.go")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(source), "\r\n", "\n")
	body := text[strings.Index(text, "func postconfExpandedContext("):]
	body = body[:strings.Index(body, "\n}\n")]
	for _, needed := range []string{
		"value, known := postconfOneValue(out)",
		"return \"\", &postconfUnreadError{setting: key, command: \"postconf -x -h \" + key}",
		"return value, nil",
	} {
		if !strings.Contains(body, needed) {
			t.Fatalf("postconfExpandedContext no longer contains %s", needed)
		}
	}
	if strings.Contains(body, "TrimSpace(string(out))") {
		t.Fatal("postconfExpandedContext takes the buffer as the value")
	}
	// The alias repair's reading with a warning in the buffer, as measured.
	if value, known := postconfOneValue([]byte("hash:/etc/aliases\n" + measuredUnusedWarning)); !known || value != "hash:/etc/aliases" {
		t.Fatalf("value = %q, %v", value, known)
	}
	// No reader of a postconf value in this package takes the trimmed buffer.
	for _, file := range []string{"mail_tls_rpc.go", "mail_tls_sync_commit.go", "mail_stack_rpc.go", "mail_service_verify.go"} {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
		for index, line := range lines {
			if !strings.Contains(line, `"postconf", "-h"`) && !strings.Contains(line, `"postconf", "-x", "-h"`) {
				continue
			}
			if strings.Contains(line, "exec.Command(") {
				continue // standard output alone (Output); not a buffer of both streams
			}
			window := strings.Join(lines[index:min(index+22, len(lines))], "\n")
			if !strings.Contains(window, "postconfOneValue(out)") && !strings.Contains(window, "postconfOnePath(out)") {
				t.Fatalf("%s:%d reads a postconf value without the rule", file, index+1)
			}
		}
	}
}

// The rule against the real program, where there is one: a private
// configuration directory, nothing of the system read or changed.
func TestPostconfValueRuleAgainstTheRealPostconf(t *testing.T) {
	program, err := exec.LookPath("postconf")
	if err != nil {
		t.Skip("no postconf on this host")
	}
	directory := t.TempDir()
	main := "compatibility_level = 3.6\nmyhostname = mail.set4c.test\nqueue_directory = /var/spool/postfix\n" +
		"smtpd_tls_cert_file = " + measuredCertValue + "\n" +
		"default_process_limit = 200 # raised for the campaign\ncampaign_note = raised for the campaign\n"
	if err := os.WriteFile(filepath.Join(directory, "main.cf"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "master.cf"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"smtpd_tls_cert_file": measuredCertValue, "tls_server_sni_maps": "", "myhostname": "mail.set4c.test"} {
		out, err := exec.Command(program, "-c", directory, "-h", name).CombinedOutput()
		if err != nil {
			t.Fatalf("postconf -h %s: %v: %s", name, err, out)
		}
		if !strings.Contains(string(out), "postconf: warning: ") {
			t.Skipf("this postconf printed no warning for the two lines: %q", out)
		}
		value, known := postconfOneValue(out)
		if !known || value != want {
			t.Fatalf("%s: read %q, %v from %q", name, value, known, out)
		}
		if old := strings.TrimSpace(string(out)); old == want {
			t.Fatalf("%s: the buffer holds no warning to leave out: %q", name, out)
		}
	}
}

// The operation guidance records the Agent's reason word for word, in its
// English and its Turkish edition.
func TestTheGuidanceRecordsThePostconfReason(t *testing.T) {
	sentence := (&postconfUnreadError{setting: "{setting}", command: "{command}"}).Error()
	for _, name := range []string{"OPERATION-GUIDANCE.md", "OPERATION-GUIDANCE.tr.md"} {
		raw, err := os.ReadFile("../../docs/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if document := strings.Join(strings.Fields(string(raw)), " "); !strings.Contains(document, `"`+sentence+`"`) {
			t.Fatalf("%s does not record the sentence: %s", name, sentence)
		}
	}
}
