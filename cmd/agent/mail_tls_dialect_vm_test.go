//go:build linux

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/mailtlsconfig"
)

// This opt-in native fixture has no installed Panel or Agent. The controller
// additionally proves QEMU/SSH/nonce/DMI identity before invoking the test.
func TestMailHostCertificateDisposableVMNativeDialectReadback(t *testing.T) {
	requireDisposableMailVM(t)
	held, err := acquireExistingServiceMutationFileLock(serviceMutationLockFile())
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	const domain = "mail.setup.celikpanel.test"
	currentDomain, leaf, err := currentMailHostCertificateIdentity()
	if err != nil || currentDomain != domain {
		t.Fatalf("retained native certificate: %q %v", currentDomain, err)
	}
	assertMailVMListeners(t, domain, leaf)
	paths := []string{"/etc/postfix/main.cf", dovecotTLSConf, mailHostRenewalPendingPath(), filepath.Join(serviceMutationStateDirectory(), serviceMutationLedgerFileName)}
	before := make(map[string][]byte)
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		before[path] = raw
	}
	current, err := os.Readlink(filepath.Join(managedMailHostTLSDir, "current"))
	if err != nil {
		t.Fatal(err)
	}
	// A real executable fails the version observation; no daemon configuration,
	// postmap, reload or restart may be reached after that result.
	wrapper := filepath.Join(t.TempDir(), "dovecot")
	if err = os.WriteFile(wrapper, []byte("#!/bin/sh\nexit 75\n"), 0700); err != nil {
		t.Fatal(err)
	}
	previous := lookupMailTLSCommand
	t.Cleanup(func() { lookupMailTLSCommand = previous })
	lookupMailTLSCommand = func(name string) (string, error) {
		if name == "dovecot" {
			return wrapper, nil
		}
		return exec.LookPath(name)
	}
	calls := 0
	failedVersion := func(name string, args ...string) ([]byte, error) {
		calls++
		if name != wrapper || strings.Join(args, " ") != "--version" {
			t.Fatalf("unverified dialect reached another action: %s %v", name, args)
		}
		return exec.Command(name, args...).CombinedOutput()
	}
	var response SecureMailTLSResponse
	outcome, err := reconcileMailTLSHost(&SecureMailTLSRequest{Myhostname: domain}, &response, failedVersion)
	if err != nil || outcome != mailTLSHostUntouched || response.Configured || response.Error != mailtlsconfig.ErrDovecotVersionUnknown.Error() || calls != 1 {
		t.Fatalf("unavailable native version was not an untouched refusal: %v %+v calls=%d %v", outcome, response, calls, err)
	}
	lookupMailTLSCommand = previous
	runner := func(name string, args ...string) ([]byte, error) {
		switch filepath.Base(name) {
		case "postconf", "dovecot", "doveconf", "postfix":
		default:
			t.Fatalf("unexpected native verification command: %s", name)
		}
		return exec.Command(name, args...).CombinedOutput()
	}
	observed, err := dovecotIs24WithRunner(runner)
	if err != nil || !observed {
		t.Fatalf("native Dovecot 2.4 observation: %v %v", observed, err)
	}
	plan, err := loadMailHostCertificatePlan()
	if err != nil {
		t.Fatal(err)
	}
	if err = verifyMailTLSSyncPlan(plan, runner); err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before[path], after) {
			t.Fatalf("native observation changed %s: %v", path, err)
		}
	}
	after, err := os.Readlink(filepath.Join(managedMailHostTLSDir, "current"))
	if err != nil || after != current {
		t.Fatalf("native observation changed selection: %v", err)
	}
	assertMailVMListeners(t, domain, leaf)
	t.Logf("native version failure refused before configuration; native 2.4 retained-plan readback passed; configuration, ledger, pending renewal and trusted SMTP/IMAP leaf unchanged: %s", leaf)
}
