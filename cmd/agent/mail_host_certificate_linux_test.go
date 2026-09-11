//go:build linux

package main

import (
	"context"
	"crypto/x509"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"golang.org/x/sys/unix"
)

func TestMailHostCertificatePublishedButActivationFailedRetainsRecoveryOwnership(t *testing.T) {
	manager, _ := newMutationTestManager(t)
	qualifier := testMailHostCertificateQualifier(t)
	beginMutationTestJobWithIdentity(t, manager, "mail_host_certificate", "panel.example.test", qualifier)
	ctx, finish, err := manager.acquireStep(ServiceMutationBinding{MutationRequestID: testMutationRequestID, MutationOwnerID: testMutationOwnerID}, newServiceMutationStepClaim(serviceMutationStepIssueMailHostCertificate, "panel.example.test", qualifier, "issue"))
	if err != nil {
		t.Fatal(err)
	}
	defer finish()
	oldVerify, oldStabilize := mailHostCertificateVerifyPublished, mailHostCertificateStabilizePublished
	mailHostCertificateVerifyPublished = func(string, string, string) (bool, error) { return true, nil }
	mailHostCertificateStabilizePublished = func() error { return nil }
	t.Cleanup(func() {
		mailHostCertificateVerifyPublished = oldVerify
		mailHostCertificateStabilizePublished = oldStabilize
		releasePoisonedVPNPeerSyncTestManager(manager)
	})
	published, err := commitStandaloneMailHostCertificateStep(ctx, func() error { return errors.New("Dovecot reload failed") })
	if !published || err == nil {
		t.Fatalf("ambiguous activation lost: %v %v", published, err)
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.poisoned == nil || manager.active == nil || manager.active.lock == nil || manager.active.job.Status == serviceMutationStatusSucceeded || manager.active.mailHostCertificatePublishedPhase != "" {
		t.Fatal("publication without activation became terminal success")
	}
}

func TestMailHostCertificateCannotUsePanelCertificateLease(t *testing.T) {
	host, err := mutationpayload.CanonicalMailHostCertificate("panel.example.test", "admin@example.test", "unknown")
	if err != nil {
		t.Fatal(err)
	}
	panel, err := mutationpayload.CanonicalPanelCertificateIssue(host.Domain, host.Email, managedPanelTLSDir, "unknown")
	if err != nil {
		t.Fatal(err)
	}
	if host.Qualifier == panel.Qualifier {
		t.Fatal("different certificate purposes share commitment")
	}
	manager, _ := newMutationTestManager(t)
	beginMutationTestJobWithIdentity(t, manager, "panel_certificate_issue", host.Domain, panel.Qualifier)
	_, _, err = manager.acquireStep(ServiceMutationBinding{MutationRequestID: testMutationRequestID, MutationOwnerID: testMutationOwnerID}, newServiceMutationStepClaim(serviceMutationStepIssueMailHostCertificate, host.Domain, host.Qualifier, "issue"))
	if err == nil {
		t.Fatal("panel lease authorized mail host publication")
	}
}

func TestMailHostCertificateCurrentRequiresTrustedExactPairAndReceipt(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-owned material contract")
	}
	fixture := createTestPanelCertificateSource(t)
	cert, key, leaf, _, err := readPanelCertificateSource(fixture.domain)
	if err != nil {
		t.Fatal(err)
	}
	commitment, err := mutationpayload.CanonicalMailHostCertificate(fixture.domain, "admin@example.test", "unknown")
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := newMailHostCertificateReceipt(testMutationRequestID, commitment.Qualifier, fixture.domain, leaf)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := canonicalMailHostCertificateReceipt(receipt)
	root := t.TempDir()
	version := managedPanelCertVersionPrefix + strings.Repeat("3", 32)
	if err = os.Mkdir(filepath.Join(root, version), 0750); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"fullchain.pem": cert, "privkey.pem": key, "mail.domain": []byte(fixture.domain + "\n"), mailHostCertificateReceiptName: raw} {
		if err = os.WriteFile(filepath.Join(root, version, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err = os.Symlink(version, filepath.Join(root, "current")); err != nil {
		t.Fatal(err)
	}
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	_, got, _, _, found, err := readCurrentMailHostCertificateVersionAt(fd)
	if err != nil || !found || got != receipt {
		t.Fatalf("trusted generation: %v %v %+v", found, err, got)
	}
	keyPath := filepath.Join(root, version, "privkey.pem")
	if err = os.Chmod(keyPath, 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, _, err = readCurrentMailHostCertificateVersionAt(fd); err == nil {
		t.Fatal("public private key accepted")
	}
	if err = os.Chmod(keyPath, 0600); err != nil {
		t.Fatal(err)
	}
	old := panelCertificateSourceSystemRoots
	panelCertificateSourceSystemRoots = func() (*x509.CertPool, error) { return x509.NewCertPool(), nil }
	if _, _, _, _, _, err = readCurrentMailHostCertificateVersionAt(fd); err == nil {
		t.Fatal("untrusted certificate accepted")
	}
	panelCertificateSourceSystemRoots = old
	if err = os.Remove(filepath.Join(root, version, mailHostCertificateReceiptName)); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, _, err = readCurrentMailHostCertificateVersionAt(fd); err == nil {
		t.Fatal("missing receipt silently became bootstrap fallback")
	}
}

func TestMailHostCertificateCommandDoesNotAcceptArbitraryPrograms(t *testing.T) {
	if _, err := runMailHostCertificateCommand(t.Context(), "sh", "-c", "false"); err == nil {
		t.Fatal("shell execution admitted")
	}
}

func TestMailHostCertificateConvergenceRetainsLeaseAndUsesSupervisor(t *testing.T) {
	manager, _ := newMutationTestManager(t)
	qualifier := testMailHostCertificateQualifier(t)
	beginMutationTestJobWithIdentity(t, manager, "mail_host_certificate", "panel.example.test", qualifier)
	ctx, finish, err := manager.acquireStep(ServiceMutationBinding{MutationRequestID: testMutationRequestID, MutationOwnerID: testMutationOwnerID}, newServiceMutationStepClaim(serviceMutationStepIssueMailHostCertificate, "panel.example.test", qualifier, "issue"))
	if err != nil {
		t.Fatal(err)
	}
	defer finish()
	old := mailHostCertificateVerifyPublished
	mailHostCertificateVerifyPublished = func(string, string, string) (bool, error) { return true, nil }
	t.Cleanup(func() { mailHostCertificateVerifyPublished = old })
	published, err := commitStandaloneMailHostCertificateStep(ctx, func() error { return nil }, func(convergence context.Context) error {
		// These take the manager mutex: convergence must have released it while
		// retaining the global host lease and the immutable intent.
		cancelled, err := manager.cancelJob(&ServiceMutationCancelRequest{RequestID: testMutationRequestID, ExpectedOwner: testMutationOwnerID})
		if err != nil || cancelled.Status != serviceMutationStatusRunning {
			t.Fatalf("cancel interrupted committed host publication: %+v %v", cancelled, err)
		}
		finished, err := manager.finish(&ServiceMutationFinishRequest{RequestID: testMutationRequestID, OwnerID: testMutationOwnerID, Success: false})
		if err == nil || finished.Status != serviceMutationStatusRunning {
			t.Fatalf("Finish(false) broke committed publication: %+v %v", finished, err)
		}
		_, err = runServiceMutationCombinedOutput(convergence, "true")
		return err
	})
	if err != nil || !published {
		t.Fatalf("supervised convergence: %v %v", published, err)
	}
	job := manager.status(testMutationRequestID)
	if job.Status != serviceMutationStatusSucceeded || job.WorkerPID != 0 {
		t.Fatalf("terminal receipt: %+v", job)
	}
}

func TestMailHostCertificateSourceUsesDistinctConfinedLineage(t *testing.T) {
	fixture := createTestPanelCertificateSource(t)
	lineage := mailHostCertLineageName(fixture.domain)
	archive := filepath.Join(fixture.root, "archive", lineage)
	live := filepath.Join(fixture.root, "live", lineage)
	if err := os.Mkdir(archive, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(live, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"fullchain1.pem", "privkey1.pem"} {
		raw, err := os.ReadFile(filepath.Join(fixture.archive, name))
		if err != nil {
			t.Fatal(err)
		}
		mode := os.FileMode(0600)
		if name == "fullchain1.pem" {
			mode = 0644
		}
		if err = os.WriteFile(filepath.Join(archive, name), raw, mode); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"fullchain", "privkey"} {
		if err := os.Symlink("../../archive/"+lineage+"/"+name+"1.pem", filepath.Join(live, name+".pem")); err != nil {
			t.Fatal(err)
		}
	}
	_, _, leaf, _, err := readMailHostCertificateSource(fixture.domain)
	if err != nil || panelCertificateLeafSHA256(leaf) != panelCertificateLeafSHA256(fixture.leafDER) {
		t.Fatalf("mail lineage not readable: %v", err)
	}
	if err = os.Remove(filepath.Join(live, "privkey.pem")); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink("../../archive/"+fixture.lineage+"/privkey1.pem", filepath.Join(live, "privkey.pem")); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err = readMailHostCertificateSource(fixture.domain); err == nil {
		t.Fatal("mail certificate followed another managed purpose's lineage")
	}
}
