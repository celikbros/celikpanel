//go:build linux

package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
)

// Explicit owner edit/resolution only in the guarded disposable native fixture.
// A normal Agent renewal must not use historical intent to erase that edit.
func TestMailHostCertificateDisposableVMOwnerConfigurationBarrier(t *testing.T) {
	requireDisposableMailVM(t)
	if _, err := os.Stat("/opt/celikpanel/bin/agent"); !os.IsNotExist(err) {
		t.Fatal("installed agent must be absent")
	}
	const domain = "mail.setup.celikpanel.test"
	gotDomain, leaf, err := currentMailHostCertificateIdentity()
	if err != nil || gotDomain != domain {
		t.Fatalf("retained identity: %s %v", gotDomain, err)
	}
	assertMailVMListeners(t, domain, leaf)
	manager, err := agentServiceMutationManager()
	if err != nil {
		t.Fatal(err)
	}
	commitment, err := mutationpayload.CanonicalMailHostCertificate(domain, "test@example.test", buildCommit)
	if err != nil {
		t.Fatal(err)
	}
	identity := make([]byte, 32)
	if _, err = rand.Read(identity); err != nil {
		t.Fatal(err)
	}
	requestID, ownerID := hex.EncodeToString(identity[:16]), hex.EncodeToString(identity[16:])
	if _, err = manager.begin(&ServiceMutationBeginRequest{RequestID: requestID, OwnerID: ownerID, Kind: "mail_host_certificate", Target: domain, PackageName: commitment.Qualifier}); err != nil {
		t.Fatal(err)
	}
	ctx, finish, err := manager.acquireStep(ServiceMutationBinding{MutationRequestID: requestID, MutationOwnerID: ownerID}, newServiceMutationStepClaim(serviceMutationStepIssueMailHostCertificate, domain, commitment.Qualifier, "issue"))
	if err != nil {
		t.Fatal(err)
	}
	defer finish()
	if err = preflightMailHostCertificate(ctx, domain); err != nil {
		t.Fatalf("native unchanged preflight: %v", err)
	}
	paths := []string{"/etc/postfix/main.cf", dovecotTLSConf, mailHostRenewalPendingPath()}
	before := make(map[string][]byte)
	for _, path := range paths {
		raw, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		before[path] = raw
	}
	current, err := os.Readlink(filepath.Join(managedMailHostTLSDir, "current"))
	if err != nil {
		t.Fatal(err)
	}
	list := func() []string {
		entries, e := os.ReadDir(managedMailHostTLSDir)
		if e != nil {
			t.Fatal(e)
		}
		names := []string{}
		for _, e := range entries {
			names = append(names, e.Name())
		}
		return names
	}
	stages := list()
	edited := append(append([]byte{}, before[dovecotTLSConf]...), []byte("# owner fixture change; publication must preserve this\n")...)
	if err = os.WriteFile(dovecotTLSConf, edited, 0600); err != nil {
		t.Fatal(err)
	}
	resolved := false
	t.Cleanup(func() {
		if resolved {
			return
		}
		actual, e := os.ReadFile(dovecotTLSConf)
		if e != nil || !bytes.Equal(actual, edited) {
			t.Error("fixture changed again; owner edit retained for review")
			return
		}
		if e = os.WriteFile(dovecotTLSConf, before[dovecotTLSConf], 0600); e != nil {
			t.Error(e)
		}
	})
	_, err = publishMailHostCertificateSource(ctx, domain, requestID, commitment.Qualifier, "")
	var refused *mailHostConfigurationUnverified
	if !errors.As(err, &refused) {
		t.Fatalf("publication did not stop at configuration observation: %v", err)
	}
	for _, path := range paths {
		want := before[path]
		if path == dovecotTLSConf {
			want = edited
		}
		actual, e := os.ReadFile(path)
		if e != nil || !bytes.Equal(actual, want) {
			t.Fatalf("publication changed %s: %v", path, e)
		}
	}
	after, err := os.Readlink(filepath.Join(managedMailHostTLSDir, "current"))
	if err != nil || after != current || !reflect.DeepEqual(stages, list()) {
		t.Fatalf("publication staged or selected a certificate: %v", err)
	}
	assertMailVMListeners(t, domain, leaf)
	// Explicit fixture-owner resolution, never automatic product repair.
	if err = os.WriteFile(dovecotTLSConf, before[dovecotTLSConf], 0600); err != nil {
		t.Fatal(err)
	}
	resolved = true
	if err = preflightMailHostCertificate(ctx, domain); err != nil {
		t.Fatalf("owner-resolved observation: %v", err)
	}
	finish()
	if _, err = manager.finish(&ServiceMutationFinishRequest{RequestID: requestID, OwnerID: ownerID, Success: false, FailureCode: "fixture_owner_configuration_refused", Message: "Native owner configuration barrier verified; no certificate was published."}); err != nil {
		t.Fatal(err)
	}
	job := manager.status(requestID)
	if job == nil || job.Status != serviceMutationStatusFailed {
		t.Fatalf("refusal became success: %+v", job)
	}
	t.Logf("owner edit preserved before certificate staging; pending renewal and selected trusted SMTP/IMAP leaf unchanged; explicit fixture owner resolution reverified; failed request=%s leaf=%s", requestID, leaf)
}
