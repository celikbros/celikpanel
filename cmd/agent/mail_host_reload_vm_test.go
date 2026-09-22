//go:build linux

package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
)

const mailReloadOwnerFixturePath = "/root/celikpanel-release-recovery-lab/mail-reload-owner-fixture.json"

type mailReloadOwnerFixture struct {
	Request, Owner, Qualifier, Domain, Leaf, PreviousLeaf string
	Before, Edited                                        []byte
	Mode                                                  uint32
}

func readMailReloadOwnerFixture(t *testing.T) mailReloadOwnerFixture {
	t.Helper()
	requireDisposableMailVM(t)
	if _, err := os.Stat("/opt/celikpanel/bin/agent"); !os.IsNotExist(err) {
		t.Fatal("installed Agent must be absent")
	}
	raw, err := os.ReadFile(mailReloadOwnerFixturePath)
	if err != nil {
		t.Fatal(err)
	}
	var value mailReloadOwnerFixture
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	if !validMutationIdentity(value.Request) || !validMutationIdentity(value.Owner) ||
		!mutationpayload.ValidMailHostCertificateQualifier(value.Qualifier) || value.Domain != "mail.setup.celikpanel.test" ||
		len(value.Before) == 0 || !bytes.Equal(value.Edited, append(append([]byte{}, value.Before...), []byte("# owner edit after certificate publication\n")...)) || value.Mode != 0644 {
		t.Fatal("unexpected owner fixture evidence")
	}
	return value
}

// A controlled callback fault after the actual durable selection. This is not
// a SIGKILL/power-loss claim. Subsequent tests run as independent native units.
func TestMailHostCertificateDisposableVMReloadOwnerFault(t *testing.T) {
	requireDisposableMailVM(t)
	if _, err := os.Stat("/opt/celikpanel/bin/agent"); !os.IsNotExist(err) {
		t.Fatal("installed Agent must be absent")
	}
	domain, selected, err := currentMailHostCertificateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if domain != "mail.setup.celikpanel.test" {
		t.Fatal("wrong fixture hostname")
	}
	cert, key, leaf, _, err := readMailHostCertificateSource(domain)
	if err != nil {
		t.Fatal(err)
	}
	if panelCertificateLeafSHA256(leaf) == selected {
		t.Fatal("run after the owner-selection drift trial with a newer queued source")
	}
	c, err := mutationpayload.CanonicalMailHostCertificate(domain, "renewal@celikpanel.invalid", buildCommit)
	if err != nil {
		t.Fatal(err)
	}
	identity := make([]byte, 32)
	if _, err := rand.Read(identity); err != nil {
		t.Fatal(err)
	}
	value := mailReloadOwnerFixture{Request: hex.EncodeToString(identity[:16]), Owner: hex.EncodeToString(identity[16:]), Qualifier: c.Qualifier, Domain: domain, Leaf: panelCertificateLeafSHA256(leaf), PreviousLeaf: selected, Mode: 0644}
	value.Before, err = os.ReadFile(dovecotTLSConf)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dovecotTLSConf)
	if err != nil || info.Mode().Perm() != 0644 {
		t.Fatalf("unexpected original mode: %v", err)
	}
	value.Edited = append(append([]byte{}, value.Before...), []byte("# owner edit after certificate publication\n")...)
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := os.OpenFile(mailReloadOwnerFixturePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = evidence.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err = evidence.Sync(); err != nil {
		t.Fatal(err)
	}
	if err = evidence.Close(); err != nil {
		t.Fatal(err)
	}
	request := &ServiceMutationBeginRequest{RequestID: value.Request, OwnerID: value.Owner, Kind: "mail_host_certificate", Target: domain, PackageName: c.Qualifier}
	manager, err := newMailRenewalMutationManager("", "", request)
	if err != nil {
		t.Fatal(err)
	}
	mailRenewalExecution.retained = manager // Retain any poison/lease until this test process exits.
	if _, err = manager.begin(request); err != nil {
		t.Fatal(err)
	}
	ctx, finish, err := manager.acquireStep(ServiceMutationBinding{MutationRequestID: value.Request, MutationOwnerID: value.Owner}, newServiceMutationStepClaim(serviceMutationStepIssueMailHostCertificate, domain, c.Qualifier, "issue"))
	if err != nil {
		t.Fatal(err)
	}
	defer finish()
	if err = preflightMailHostCertificateReload(ctx, domain); err != nil {
		t.Fatal(err)
	}
	receipt, err := newMailHostCertificateReceipt(value.Request, c.Qualifier, domain, leaf)
	if err != nil {
		t.Fatal(err)
	}
	injected := errors.New("fixture owner edit after durable publication")
	err = panelCertWithPublishLock(func() error {
		stage, err := stageMailHostCertificateMaterial(domain, managedMailHostTLSDir, cert, key, receipt)
		if err != nil {
			return err
		}
		defer stage.close()
		published, err := commitStandaloneMailHostCertificateStep(ctx, stage.publish, func(_ context.Context) error {
			if err := os.WriteFile(dovecotTLSConf, value.Edited, 0644); err != nil {
				return err
			}
			return injected
		})
		if !published || !errors.Is(err, injected) {
			t.Fatalf("controlled publication fault not reached: %v %v", published, err)
		}
		return err
	})
	manager.mu.Lock()
	retained := manager.poisoned != nil && manager.active != nil
	manager.mu.Unlock()
	if !errors.Is(err, injected) || !retained {
		t.Fatal("uncertain activation lost its operation lease")
	}
	actual, err := os.ReadFile(dovecotTLSConf)
	if err != nil || !bytes.Equal(actual, value.Edited) {
		t.Fatal("owner edit lost")
	}
	assertMailVMListeners(t, domain, selected, value.Leaf)
	t.Logf("controlled post-publication fault retained exact active intent and owner edit; request=%s previous=%s selected=%s", value.Request, selected, value.Leaf)
}

func TestMailHostCertificateDisposableVMReloadOwnerRecoveryRefuses(t *testing.T) {
	value := readMailReloadOwnerFixture(t)
	before, err := os.ReadFile(dovecotTLSConf)
	if err != nil || !bytes.Equal(before, value.Edited) {
		t.Fatal("owner edit fixture differs")
	}
	manager, err := newServiceMutationManager("", "")
	// Keep poison reachable until process exit; no handler is allowed to erase
	// the edit just because the selected certificate has this operation's receipt.
	mailRenewalExecution.retained = manager
	var refused *mailHostReloadUnverified
	if manager == nil || !errors.As(err, &refused) || manager.poisoned == nil {
		t.Fatalf("recovery did not refuse owner drift: %v", err)
	}
	actual, err := os.ReadFile(dovecotTLSConf)
	if err != nil || !bytes.Equal(actual, before) {
		t.Fatal("recovery overwrote owner configuration")
	}
	if manager.ledger.ActiveRequestID != value.Request {
		t.Fatal("unknown activation became terminal")
	}
	verified, err := mailHostCertificateVerifyPublished(value.Request, value.Qualifier, value.Domain)
	if err != nil || !verified {
		t.Fatal("exact publication receipt lost")
	}
	assertMailVMListeners(t, value.Domain, value.PreviousLeaf, value.Leaf)
	t.Logf("native startup recovery preserved owner edit and exact published receipt; active request=%s previous=%s selected=%s", value.Request, value.PreviousLeaf, value.Leaf)
}

func TestMailHostCertificateDisposableVMReloadOwnerResolution(t *testing.T) {
	value := readMailReloadOwnerFixture(t)
	current, err := os.ReadFile(dovecotTLSConf)
	if err != nil || !bytes.Equal(current, value.Edited) {
		t.Fatal("owner changed fixture again; preserve it for review")
	}
	// Explicit fixture-owner resolution, not automatic product recovery.
	if err = os.WriteFile(dovecotTLSConf, value.Before, os.FileMode(value.Mode)); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dovecotTLSConf)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := newServiceMutationManager("", "")
	if err != nil {
		t.Fatal(err)
	}
	job := manager.status(value.Request)
	phase, err := formatMailHostCertificateCommitPhase(mailHostCertificateCommitPublished, value.Request, value.Domain, value.Qualifier)
	if err != nil || job == nil || job.Status != serviceMutationStatusSucceeded || job.Phase != phase || manager.ledger.ActiveRequestID != "" {
		t.Fatalf("same-operation recovery failed: %+v %v", job, err)
	}
	actual, err := os.ReadFile(dovecotTLSConf)
	if err != nil || !bytes.Equal(actual, value.Before) {
		t.Fatal("recovery rewrote resolved configuration")
	}
	after, err := os.Stat(dovecotTLSConf)
	if err != nil || !after.ModTime().Equal(info.ModTime()) {
		t.Fatal("recovery rewrote resolved metadata")
	}
	assertMailVMListeners(t, value.Domain, value.Leaf)
	t.Logf("explicit owner resolution recovered same operation by reload only; request=%s leaf=%s", value.Request, value.Leaf)
}
