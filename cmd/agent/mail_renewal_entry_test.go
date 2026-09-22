package main

import (
	"strings"
	"testing"
)

func TestIndependentMailEntryScope(t *testing.T) {
	for _, args := range [][]string{{"--process-pending"}, {"--retry-selected", strings.Repeat("a", 32)}, {"--inspect-build-identity"}, {"--queue", "celikpanel-mail-" + strings.Repeat("a", 24)}} {
		if err := validateIndependentMailEntry(args, 0, []string{"PATH=/owner/bin", "INVOCATION_ID=fixture"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{nil, {}, {"--self-update-worker", strings.Repeat("a", 32)}, {"--initialize-service-mutation-ledger"}, {"--restart-panel-after-certificate-publish"}, {"--deploy-panel-certificate", "domain"}, {"--process-pending", "extra"}, {"--queue", "celikpanel-mail-" + strings.Repeat("A", 24)}, {"--queue", "../other"}, {"--retry-selected"}, {"--retry-selected", "bad"}, {"--retry-selected", strings.Repeat("a", 32), "extra"}} {
		if err := validateIndependentMailEntry(args, 0, nil); err == nil {
			t.Fatalf("broad entry accepted: %v", args)
		}
	}
	if err := validateIndependentMailEntry([]string{"--process-pending"}, 1000, nil); err == nil {
		t.Fatal("non-root accepted")
	}
	for _, env := range []string{"CELIKPANEL_MAIL_DIR=/tmp/untrusted", "CELIKPANEL_AGENT_STATE_DIR=/tmp/state", "CELIKPANEL_MUTATION_LOCK=", "CELIKPANEL_DNS_KILL_MATRIX=x", "LD_PRELOAD=/tmp/library", "DYLD_LIBRARY_PATH=/tmp/library"} {
		if err := validateIndependentMailEntry([]string{"--process-pending"}, 0, []string{env}); err == nil {
			t.Fatalf("override accepted: %s", env)
		}
	}
}

func TestIndependentMailSupervisorCommandScope(t *testing.T) {
	for _, args := range [][]string{
		{"/usr/sbin/dovecot", "--version"}, {"/usr/bin/doveconf", "-n"}, {"/usr/sbin/postconf", "-h", "myhostname"}, {"/usr/sbin/postconf", "-h", "tls_server_sni_maps"},
		{"/bin/systemctl", "reload", "postfix.service"}, {"/usr/bin/systemctl", "reload", "dovecot.service"}, {"/usr/bin/systemctl", "is-active", "--quiet", "postfix.service"},
	} {
		if err := validateIndependentMailSupervisor(append([]string{"/run/celikpanel/service-mutation.lock"}, args...), 0, nil); err != nil {
			t.Fatalf("supported command refused: %v %v", args, err)
		}
	}
	for _, args := range [][]string{
		{"/bin/sh", "-c", "true"}, {"/usr/sbin/postfix", "check"}, {"/usr/sbin/postmap", "hash:/etc/postfix/sni"}, {"/usr/sbin/postconf", "-e", "myhostname=other"},
		{"/usr/sbin/postconf", "-h", "-e"}, {"/usr/sbin/postconf", "-h", "config_directory"}, {"/usr/bin/doveconf", "-n", "-c", "/tmp/owner"}, {"/usr/bin/dovecot", "--version", "extra"},
		{"/bin/systemctl", "reload-or-restart", "postfix.service"}, {"/bin/systemctl", "reload", "celikpanel-agent.service"}, {"/bin/systemctl", "start", "postfix.service"},
		{"/tmp/systemctl", "reload", "postfix.service"}, {"/usr/bin/../bin/systemctl", "reload", "postfix.service"}, {"systemctl", "reload", "postfix.service"},
	} {
		if err := validateIndependentMailSupervisor(append([]string{"/run/celikpanel/service-mutation.lock"}, args...), 0, nil); err == nil {
			t.Fatalf("broad command accepted: %v", args)
		}
	}
	if err := validateIndependentMailSupervisor([]string{"/tmp/lock", "/bin/systemctl", "reload", "postfix.service"}, 0, nil); err == nil {
		t.Fatal("other lock accepted")
	}
}
