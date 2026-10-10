package main

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/transport"
)

// cronUnreadableAnswer reports whether err is the Agent's "crontab could not be
// read" answer: the fixed sentence, alone or with the evidence lines.
func cronUnreadableAnswer(err error) bool {
	if err == nil {
		return false
	}
	known, _, _ := transport.UnreadableEvidence(err.Error(), transport.CronStateUnreadable)
	return known
}

func installCronAccessFiles(t *testing.T, files map[string]string) {
	t.Helper()
	old := cronAccessFile
	t.Cleanup(func() { cronAccessFile = old })
	cronAccessFile = func(name string) ([]byte, error) {
		content, ok := files[name]
		if !ok {
			return nil, os.ErrNotExist
		}
		return []byte(content), nil
	}
}

// Measured on Debian 13 and Ubuntu 24.04 (set1, S1 f): the owner restricts cron
// with an /etc/cron.allow that holds only `root`. `crontab -u <user> -l` then
// refuses that user even for root: exit 1, "The user set1_owner_test cannot use
// this program (crontab)". The Panel told the owner to check that the service
// was running. The Agent now names the cause it verified and carries the line.
func TestUnreadableCrontabNamesCronAllowOnlyWhenTheAgentVerifiedIt(t *testing.T) {
	refused := "The user " + cronTestUser + " cannot use this program (crontab)\n"
	cases := []struct {
		name   string
		stderr string
		files  map[string]string
		cause  string
		detail string
	}{
		{
			name: "cron.allow exists without the user", stderr: refused,
			files: map[string]string{"/etc/cron.allow": "root\n"},
			cause: transport.UnreadableCronAllow, detail: "The user " + cronTestUser + " cannot use this program (crontab)",
		},
		{
			name: "cron.deny lists the user and there is no cron.allow", stderr: refused,
			files: map[string]string{"/etc/cron.deny": "guest\n" + cronTestUser + "\n"},
			cause: transport.UnreadableCronDeny, detail: "The user " + cronTestUser + " cannot use this program (crontab)",
		},
		{
			// crontab refuses the user, but neither file explains it: the line
			// is carried, no cause is asserted.
			name: "the user is listed in cron.allow", stderr: refused,
			files: map[string]string{"/etc/cron.allow": "root\n" + cronTestUser + "\n"},
			cause: "", detail: "The user " + cronTestUser + " cannot use this program (crontab)",
		},
		{
			// Measured on Arch (cronie): the spool behind a dangling symlink.
			// cron.allow exists and does not list the user, but crontab did
			// not say it refuses the user, so that is not the cause.
			name: "a relocated spool", stderr: "/var/spool/cron: No such file or directory\n/var/spool/cron: mkdir: File exists\n",
			files: map[string]string{"/etc/cron.allow": "root\n"},
			cause: "", detail: "/var/spool/cron: No such file or directory",
		},
		{name: "nothing was said", stderr: "", files: nil, cause: "", detail: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			installFakeCrontab(t, &fakeCrontab{stderr: tc.stderr, exitCode: 1})
			installCronAccessFiles(t, tc.files)
			content, err := readCrontab(cronTestUser)
			if err == nil || content != "" {
				t.Fatalf("readCrontab = %q, %v", content, err)
			}
			known, cause, detail := transport.UnreadableEvidence(err.Error(), transport.CronStateUnreadable)
			if !known || cause != tc.cause || detail != tc.detail {
				t.Fatalf("answer %q: known %t cause %q detail %q; want cause %q detail %q", err, known, cause, detail, tc.cause, tc.detail)
			}
		})
	}
}

// Measured on Debian 13 and Ubuntu 24.04 (set1, S6): with the owner's
// unfinished line in main.cf, `postqueue -j` exits 69 and prints the line
// below. With Postfix stopped it reads the queue directly and succeeds, so
// "check that Postfix is running" was never the action.
func TestUnreadableMailQueueCarriesWhatPostqueueSaid(t *testing.T) {
	cases := []struct {
		name, said, cause, detail string
	}{
		{
			name:  "a value in main.cf Postfix refuses",
			said:  "postqueue: fatal: bad numerical configuration: default_process_limit = 200 # raised for the campaign\n",
			cause: transport.UnreadablePostfixConfig, detail: "bad numerical configuration: default_process_limit = 200 # raised for the campaign",
		},
		{
			name:  "a line of main.cf Postfix cannot parse",
			said:  "postqueue: warning: something first\npostqueue: fatal: /etc/postfix/main.cf, line 71: missing '=' after attribute name: \"oops\"\n",
			cause: transport.UnreadablePostfixConfig, detail: "/etc/postfix/main.cf, line 71: missing '=' after attribute name: \"oops\"",
		},
		{
			name:  "something else: the line, and no cause",
			said:  "postqueue: fatal: Queue report unavailable - mail system is down\n",
			cause: "", detail: "postqueue: fatal: Queue report unavailable - mail system is down",
		},
		{
			name:  "a password in the line is blanked",
			said:  "postqueue: fatal: bad string length 0 < 1: smtp_sasl_password = hunter2\n",
			cause: "", detail: "postqueue: fatal: bad string length 0 < 1: smtp_sasl_password = …",
		},
		{name: "nothing was said", said: "", cause: "", detail: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := postfixQueueUnreadable(tc.said)
			known, cause, detail := transport.UnreadableEvidence(err.Error(), transport.PostfixQueueUnreadable)
			if !known || cause != tc.cause || detail != tc.detail {
				t.Fatalf("answer %q: known %t cause %q detail %q; want cause %q detail %q", err, known, cause, detail, tc.cause, tc.detail)
			}
			if strings.Contains(err.Error(), "hunter2") {
				t.Fatalf("the answer carries a password: %q", err)
			}
		})
	}

	// Through the RPC: what postqueue wrote to standard error is what travels.
	installFakePostqueue(t, true, func() ([]byte, error) {
		return nil, &exec.ExitError{Stderr: []byte("postqueue: fatal: bad numerical configuration: default_process_limit = 200 # raised for the campaign\n")}
	})
	var result core.PostfixQueueResult
	err := (&Agent{}).PostfixQueue(&transport.Empty{}, &result)
	if err == nil {
		t.Fatal("a failed read was answered as a queue")
	}
	if known, cause, _ := transport.UnreadableEvidence(err.Error(), transport.PostfixQueueUnreadable); !known || cause != transport.UnreadablePostfixConfig {
		t.Fatalf("answer = %q", err)
	}
	if len(result.Items) != 0 {
		t.Fatalf("a failed read carried items: %+v", result)
	}
}

func TestUnreadableEvidenceContract(t *testing.T) {
	sentence := transport.CronStateUnreadable
	for _, tc := range []struct {
		answer        string
		known         bool
		cause, detail string
	}{
		{sentence, true, "", ""},
		{sentence + "\n", true, "", ""},
		{transport.UnreadableWithEvidence(sentence, transport.UnreadableCronAllow, "The user a cannot use this program (crontab)"), true, transport.UnreadableCronAllow, "The user a cannot use this program (crontab)"},
		{transport.UnreadableWithEvidence(sentence, "", "two\nlines"), true, "", "two lines"},
		{sentence + "; and something else", false, "", ""},
		{sentence + "\nan undefined line", false, "", ""},
		{"another sentence", false, "", ""},
	} {
		known, cause, detail := transport.UnreadableEvidence(tc.answer, sentence)
		if known != tc.known || cause != tc.cause || detail != tc.detail {
			t.Errorf("%q: known %t cause %q detail %q", tc.answer, known, cause, detail)
		}
	}
	if errors.New(transport.UnreadableWithEvidence(sentence, "", "")).Error() != sentence {
		t.Error("an answer without evidence is not the fixed sentence")
	}
}
