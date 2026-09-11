//go:build linux

package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"net"
	"net/smtp"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

// This opt-in acceptance test runs only in the named disposable QEMU fixture.
// It installs no panel and does not contact an ACME provider. A temporary CA
// proves the production file/commit/reload/renewal path against real daemons.
func TestMailHostCertificateDisposableVMConvergenceAndRenewal(t *testing.T) {
	if os.Getenv("CELIKPANEL_DISPOSABLE_MAIL_VM") != "debian13-20260911" {
		t.Skip("disposable Debian13 VM acceptance only")
	}
	if os.Geteuid() != 0 {
		t.Fatal("fixture requires root")
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
	if _, err := os.Stat(managedMailHostTLSDir); !os.IsNotExist(err) {
		t.Fatal("host certificate fixture destination must initially be absent")
	}
	const domain = "mail.setup.celikpanel.test"
	command := func(name string, args ...string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if out, err := exec.CommandContext(ctx, name, args...).CombinedOutput(); err != nil {
			t.Fatalf("%s: %v: %s", name, err, out)
		}
	}
	for _, name := range []string{"postfix", "postconf", "dovecot", "doveconf", "update-ca-certificates"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Fatalf("fixture dependency %s missing", name)
		}
	}

	_, caKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	caTemplate := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "CelikPanel disposable setup test CA"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(90 * 24 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, caKey.Public(), caKey)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	const caFile = "/usr/local/share/ca-certificates/celikpanel-disposable-setup.crt"
	if err = os.WriteFile(caFile, caPEM, 0644); err != nil {
		t.Fatal(err)
	}
	command("update-ca-certificates")

	lineage := mailHostCertLineageName(domain)
	live := filepath.Join("/etc/letsencrypt/live", lineage)
	archive := filepath.Join("/etc/letsencrypt/archive", lineage)
	for _, dir := range []string{live, archive} {
		if err = os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	issueFixtureSource := func(generation int64) string {
		t.Helper()
		_, key, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		leafTemplate := &x509.Certificate{SerialNumber: big.NewInt(100 + generation), Subject: pkix.Name{CommonName: domain}, DNSNames: []string{domain}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(60 * 24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
		leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, ca, key.Public(), caKey)
		if err != nil {
			t.Fatal(err)
		}
		keyDER, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			t.Fatal(err)
		}
		number := big.NewInt(generation).String()
		for name, data := range map[string][]byte{"fullchain": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}), "privkey": pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})} {
			mode := os.FileMode(0600)
			if name == "fullchain" {
				mode = 0644
			}
			if err = os.WriteFile(filepath.Join(archive, name+number+".pem"), data, mode); err != nil {
				t.Fatal(err)
			}
			temp := filepath.Join(live, "."+name+"-next")
			if err = os.Symlink("../../archive/"+lineage+"/"+name+number+".pem", temp); err != nil {
				t.Fatal(err)
			}
			if err = os.Rename(temp, filepath.Join(live, name+".pem")); err != nil {
				t.Fatal(err)
			}
		}
		return panelCertificateLeafSHA256(leafDER)
	}
	firstLeaf := issueFixtureSource(1)
	command("postconf", "-M", "submission/inet=submission inet n - y - - smtpd")
	command("postconf", "-P", "submission/inet/smtpd_tls_security_level=encrypt")
	command("systemctl", "start", "postfix", "dovecot")

	manager, err := agentServiceMutationManager()
	if err != nil {
		t.Fatal(err)
	}
	identity := func() (string, string) {
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			t.Fatal(err)
		}
		return hex.EncodeToString(raw[:16]), hex.EncodeToString(raw[16:])
	}
	tlsCommit, err := mutationpayload.CanonicalMailTLSSync("/etc/ssl/celikpanel", domain, nil)
	if err != nil {
		t.Fatal(err)
	}
	requestID, ownerID := identity()
	if _, err = manager.begin(&ServiceMutationBeginRequest{RequestID: requestID, OwnerID: ownerID, Kind: "mail_tls_sync", Target: "mail-tls", PackageName: tlsCommit.Qualifier}); err != nil {
		t.Fatal(err)
	}
	ctx, finish, err := manager.acquireStep(ServiceMutationBinding{MutationRequestID: requestID, MutationOwnerID: ownerID}, newServiceMutationStepClaim(serviceMutationStepSyncMailTLS, "mail-tls", tlsCommit.Qualifier, "sync"))
	if err != nil {
		t.Fatal(err)
	}
	var tlsResponse transport.SecureMailTLSResponse
	err = syncMailTLSV2(ctx, tlsCommit, &tlsResponse)
	finish()
	if err != nil || !tlsResponse.Configured || tlsResponse.Error != "" {
		t.Fatalf("real mail TLS bootstrap: %+v %v", tlsResponse, err)
	}
	fallbackBefore, err := os.ReadFile(defaultMailCert)
	if err != nil {
		t.Fatal(err)
	}

	hostCommit, err := mutationpayload.CanonicalMailHostCertificate(domain, "test@example.test", buildCommit)
	if err != nil {
		t.Fatal(err)
	}
	requestID, ownerID = identity()
	if _, err = manager.begin(&ServiceMutationBeginRequest{RequestID: requestID, OwnerID: ownerID, Kind: "mail_host_certificate", Target: domain, PackageName: hostCommit.Qualifier}); err != nil {
		t.Fatal(err)
	}
	ctx, finish, err = manager.acquireStep(ServiceMutationBinding{MutationRequestID: requestID, MutationOwnerID: ownerID}, newServiceMutationStepClaim(serviceMutationStepIssueMailHostCertificate, domain, hostCommit.Qualifier, "issue"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = publishMailHostCertificateSource(ctx, domain, requestID, hostCommit.Qualifier, firstLeaf)
	finish()
	if err != nil {
		t.Fatal(err)
	}
	job := manager.status(requestID)
	expectedPhase, _ := formatMailHostCertificateCommitPhase(mailHostCertificateCommitPublished, requestID, domain, hostCommit.Qualifier)
	if job == nil || job.Status != serviceMutationStatusSucceeded || job.Phase != expectedPhase {
		t.Fatalf("exact publication receipt missing: %+v", job)
	}
	assertMailVMListeners(t, domain, firstLeaf)
	t.Logf("initial host publication and SMTP/IMAP system-trusted TLS passed; request=%s leaf=%s", requestID, firstLeaf)

	if err = writeMailHostCertificateDeployHook(); err != nil {
		t.Fatal(err)
	}
	secondLeaf := issueFixtureSource(2)
	if err = queueMailHostCertificateRenewal(lineage); err != nil {
		t.Fatal(err)
	}
	// No panel or license manager participates in unattended renewal.
	if err = deployPendingMailHostCertificate(); err != nil {
		t.Fatal(err)
	}
	assertMailVMListeners(t, domain, secondLeaf)
	if _, err = os.Stat(mailHostRenewalPendingPath()); !os.IsNotExist(err) {
		t.Fatalf("completed renewal queue remains: %v", err)
	}
	fallbackAfter, err := os.ReadFile(defaultMailCert)
	if err != nil {
		t.Fatal(err)
	}
	if string(fallbackBefore) != string(fallbackAfter) {
		t.Fatal("trusted host lifecycle replaced fallback pair")
	}
	t.Logf("renewal exported/reloaded exact second generation without panel/license; leaf=%s", secondLeaf)
}

func assertMailVMListeners(t *testing.T, domain, leafSHA string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		conn, err := tls.DialWithDialer(&net.Dialer{Timeout: time.Second}, "tcp", "127.0.0.1:993", &tls.Config{ServerName: domain, MinVersion: tls.VersionTLS12})
		if err != nil {
			lastErr = err
			time.Sleep(200 * time.Millisecond)
			continue
		}
		actual := panelCertificateLeafSHA256(conn.ConnectionState().PeerCertificates[0].Raw)
		conn.Close()
		if actual != leafSHA {
			lastErr = errMailVMWrongLeaf{}
			time.Sleep(200 * time.Millisecond)
			continue
		}
		plain, err := net.DialTimeout("tcp", "127.0.0.1:587", time.Second)
		if err != nil {
			lastErr = err
			continue
		}
		_ = plain.SetDeadline(time.Now().Add(3 * time.Second))
		client, err := smtp.NewClient(plain, domain)
		if err != nil {
			plain.Close()
			lastErr = err
			continue
		}
		err = client.StartTLS(&tls.Config{ServerName: domain, MinVersion: tls.VersionTLS12})
		state, ok := client.TLSConnectionState()
		client.Close()
		if err == nil && ok && len(state.PeerCertificates) > 0 && panelCertificateLeafSHA256(state.PeerCertificates[0].Raw) == leafSHA {
			return
		}
		lastErr = err
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("real SMTP/IMAP listener did not serve exact trusted host generation: %v", lastErr)
}

type errMailVMWrongLeaf struct{}

func (errMailVMWrongLeaf) Error() string { return "listener serves previous certificate generation" }
