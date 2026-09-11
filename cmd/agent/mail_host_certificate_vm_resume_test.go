//go:build linux

package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

// Resume only the exact disposable fixture after initial host publication.
// This exercises an already queued renewal after the agent changes/restarts;
// it never issues a certificate or initializes an installed panel.
func TestMailHostCertificateDisposableVMRenewalResume(t *testing.T) {
	requireDisposableMailVM(t)
	const domain = "mail.setup.celikpanel.test"
	currentDomain, oldLeaf, err := currentMailHostCertificateIdentity()
	if err != nil || currentDomain != domain {
		t.Fatalf("initial fixture identity missing: %q %v", currentDomain, err)
	}
	_, _, source, _, err := readMailHostCertificateSource(domain)
	if err != nil {
		t.Fatal(err)
	}
	nextLeaf := panelCertificateLeafSHA256(source)
	if nextLeaf == oldLeaf {
		t.Fatal("fixture requires an unpublished second certificate generation")
	}
	assertMailVMListeners(t, domain, oldLeaf)
	fallbackBefore, err := os.ReadFile(defaultMailCert)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := agentServiceMutationManager()
	if err != nil {
		t.Fatal(err)
	}
	// Re-converge the unchanged real hostname/SNI snapshot through the durable
	// successful publication gate, which now keeps a separate committed copy.
	commitment, err := mutationpayload.CanonicalMailTLSSync("/etc/ssl/celikpanel", domain, nil)
	if err != nil {
		t.Fatal(err)
	}
	identity := make([]byte, 32)
	if _, err = rand.Read(identity); err != nil {
		t.Fatal(err)
	}
	requestID, ownerID := hex.EncodeToString(identity[:16]), hex.EncodeToString(identity[16:])
	if _, err = manager.begin(&ServiceMutationBeginRequest{RequestID: requestID, OwnerID: ownerID, Kind: "mail_tls_sync", Target: "mail-tls", PackageName: commitment.Qualifier}); err != nil {
		t.Fatal(err)
	}
	ctx, finish, err := manager.acquireStep(ServiceMutationBinding{MutationRequestID: requestID, MutationOwnerID: ownerID}, newServiceMutationStepClaim(serviceMutationStepSyncMailTLS, "mail-tls", commitment.Qualifier, "sync"))
	if err != nil {
		t.Fatal(err)
	}
	var response transport.SecureMailTLSResponse
	err = syncMailTLSV2(ctx, commitment, &response)
	finish()
	if err != nil || !response.Configured || response.Error != "" {
		t.Fatalf("committed baseline convergence failed: %+v %v", response, err)
	}
	if err = queueMailHostCertificateRenewal(mailHostCertLineageName(domain)); err != nil {
		t.Fatal(err)
	}
	// No license or panel service exists in this fixture.
	if err = deployPendingMailHostCertificate(); err != nil {
		t.Fatal(err)
	}
	assertMailVMListeners(t, domain, nextLeaf)
	if _, err = os.Stat(mailHostRenewalPendingPath()); !os.IsNotExist(err) {
		t.Fatalf("completed renewal queue remains: %v", err)
	}
	fallbackAfter, err := os.ReadFile(defaultMailCert)
	if err != nil || string(fallbackBefore) != string(fallbackAfter) {
		t.Fatalf("renewal changed fallback material: %v", err)
	}
	domainAfter, leafAfter, err := currentMailHostCertificateIdentity()
	if err != nil || domainAfter != domain || leafAfter != nextLeaf {
		t.Fatalf("published renewal identity differs: %q %q %v", domainAfter, leafAfter, err)
	}
	// Replay of the same deploy-hook source is a no-op with no new mutation.
	if err = queueMailHostCertificateRenewal(mailHostCertLineageName(domain)); err != nil {
		t.Fatal(err)
	}
	if err = deployPendingMailHostCertificate(); err != nil {
		t.Fatal(err)
	}
	assertMailVMListeners(t, domain, nextLeaf)
	assertMailVMRenewalReceipt(t, manager, domain, source)
	t.Logf("real Postfix/Dovecot renewal and exact-source replay passed without a panel/license; previous=%s renewed=%s", oldLeaf, nextLeaf)
}

func requireDisposableMailVM(t *testing.T) {
	t.Helper()
	if os.Getenv("CELIKPANEL_DISPOSABLE_MAIL_VM") != "debian13-20260911" {
		t.Skip("disposable Debian13 VM acceptance only")
	}
	if os.Geteuid() != 0 || os.Getenv("CELIKPANEL_MUTATION_LOCK") != "/run/celikpanel/service-mutation.lock" {
		t.Fatal("requires root and the disposable fixture's actual mutation lock")
	}
	if _, err := os.Stat("/opt/celikpanel/bin/panel"); !os.IsNotExist(err) {
		t.Fatal("refusing VM acceptance where a panel exists")
	}
	if _, err := os.Stat("/var/lib/celikpanel-setup-vm"); err != nil {
		t.Fatal("disposable setup fixture marker missing")
	}
	virt, err := exec.Command("systemd-detect-virt", "--vm").Output()
	if err != nil || (strings.TrimSpace(string(virt)) != "qemu" && strings.TrimSpace(string(virt)) != "kvm") {
		t.Fatalf("refusing non-QEMU environment: %q %v", virt, err)
	}
}

func assertMailVMRenewalReceipt(t *testing.T, manager *serviceMutationManager, domain string, source []byte) {
	t.Helper()
	commitment, err := mutationpayload.CanonicalMailHostCertificate(domain, "renewal@celikpanel.invalid", buildCommit)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(append([]byte("mail-host-renewal/v1/"+domain+"/"+buildCommit+"/"), source...))
	requestID := hex.EncodeToString(digest[:16])
	job := manager.status(requestID)
	phase, err := formatMailHostCertificateCommitPhase(mailHostCertificateCommitPublished, requestID, domain, commitment.Qualifier)
	if err != nil || job == nil || job.Status != serviceMutationStatusSucceeded || job.Phase != phase {
		t.Fatalf("exact durable renewal terminal receipt missing: %+v %v", job, err)
	}
	verified, err := mailHostCertificateVerifyPublished(requestID, commitment.Qualifier, domain)
	if err != nil || !verified {
		t.Fatalf("current certificate receipt differs from terminal job: %v %v", verified, err)
	}
	t.Logf("exact durable renewal receipt verified; request=%s phase=%s", requestID, phase)
}

func TestMailHostCertificateDisposableVMRenewalReceipt(t *testing.T) {
	requireDisposableMailVM(t)
	const domain = "mail.setup.celikpanel.test"
	_, _, source, _, err := readMailHostCertificateSource(domain)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := agentServiceMutationManager()
	if err != nil {
		t.Fatal(err)
	}
	assertMailVMRenewalReceipt(t, manager, domain, source)
	assertMailVMListeners(t, domain, panelCertificateLeafSHA256(source))
	if _, err = os.Stat(mailHostRenewalPendingPath()); !os.IsNotExist(err) {
		t.Fatalf("queue remains: %v", err)
	}
}
