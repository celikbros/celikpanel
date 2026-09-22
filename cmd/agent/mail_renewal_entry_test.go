package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
	"strings"
	"testing"
)

func TestIndependentMailEntryScope(t *testing.T) {
	for _, args := range [][]string{{"--resume-enrollment", strings.Repeat("a", 32)}, {"--retry-failed", strings.Repeat("a", 32)}, {"--process-pending"}, {"--retry-selected", strings.Repeat("a", 32)}, {"--inspect-build-identity"}, {"--queue", "celikpanel-mail-" + strings.Repeat("a", 24)}} {
		if err := validateIndependentMailEntry(args, 0, []string{"PATH=/owner/bin", "INVOCATION_ID=fixture"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{nil, {}, {"--resume-enrollment"}, {"--resume-enrollment", "../active"}, {"--resume-enrollment", strings.Repeat("a", 32), "rollback"}, {"--retry-failed"}, {"--retry-failed", "bad"}, {"--retry-failed", strings.Repeat("a", 32), "extra"}, {"--self-update-worker", strings.Repeat("a", 32)}, {"--initialize-service-mutation-ledger"}, {"--restart-panel-after-certificate-publish"}, {"--deploy-panel-certificate", "domain"}, {"--process-pending", "extra"}, {"--queue", "celikpanel-mail-" + strings.Repeat("A", 24)}, {"--queue", "../other"}, {"--retry-selected"}, {"--retry-selected", "bad"}, {"--retry-selected", strings.Repeat("a", 32), "extra"}} {
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

func TestIndependentMailRenewalWaitClassification(t *testing.T) {
	unknown := errors.New("native state unreadable")
	for _, e := range []error{errServiceMutationBusy, errServiceMutationHostBusy, fmt.Errorf("host exclusion: %w", errServiceMutationHostBusy), errors.Join(errServiceMutationBusy, errServiceMutationHostBusy)} {
		if !independentMailRenewalWait(e) {
			t.Fatalf("known wait rejected: %v", e)
		}
	}
	for _, e := range []error{nil, unknown, errMailRenewalCompletionUnverified, errMailRenewalRecoveryRequired, errors.Join(errServiceMutationHostBusy, unknown), fmt.Errorf("wrapped: %w", errors.Join(errServiceMutationBusy, errMailRenewalRecoveryRequired))} {
		if independentMailRenewalWait(e) {
			t.Fatalf("unknown/failure hidden by busy cause: %v", e)
		}
	}
}

func TestEnrollmentEntryCannotUseGenericManagementOrOverrides(t *testing.T) {
	args := []string{"--resume-enrollment", strings.Repeat("a", 32)}
	if err := validateIndependentMailEntry(args, 1000, nil); err == nil {
		t.Fatal("unprivileged enrollment consumer")
	}
	for _, env := range []string{"CELIKPANEL_AGENT_STATE_DIR=/tmp/fake", "LD_PRELOAD=override", "CELIKPANEL_MUTATION_LOCK=/tmp/lock"} {
		if err := validateIndependentMailEntry(args, 0, []string{env}); err == nil {
			t.Fatal("overridden recorded source")
		}
	}
}

func TestEnrollmentGuidancePreservesUnknownAndRedactsRawErrors(t *testing.T) {
	for _, tt := range []struct {
		err  error
		want string
	}{
		{errors.New("SECRET key material /private/path"), "could not be verified"},
		{context.DeadlineExceeded, "native result is unknown"},
		{servicemutationledger.ErrMailEnrollment, "do not match"},
	} {
		got := mailEnrollmentResumeGuidance(tt.err)
		if !strings.Contains(got, tt.want) || !strings.Contains(got, "same request") || strings.Contains(got, "SECRET") || strings.Contains(got, "/private/path") {
			t.Fatal(got)
		}
	}
}
