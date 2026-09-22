package mailhostartifact

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"
)

func certificateFixture(t *testing.T) ([]byte, []byte, *x509.CertPool, time.Time) {
	t.Helper()
	now := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "fixture root"}, NotBefore: now.Add(-365 * 24 * time.Hour), NotAfter: now.Add(365 * 24 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	rootDER, err := x509.CreateCertificate(rand.Reader, ca, ca, pub, key)
	if err != nil {
		t.Fatal(err)
	}
	root, err := x509.ParseCertificate(rootDER)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(root)
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), DNSNames: []string{"mail.example.test"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, leaf, ca, pub, key)
	if err != nil {
		t.Fatal(err)
	}
	pk, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pk}), roots, now
}
func TestRetainedEvidenceDoesNotAuthorizeExpiredRenewal(t *testing.T) {
	cert, key, roots, now := certificateFixture(t)
	for _, at := range []time.Time{now.Add(-2 * time.Hour), now, now.Add(2 * time.Hour)} {
		leaf, _, err := VerifyRetainedPair(cert, key, "mail.example.test", roots, at)
		if err != nil || len(leaf) == 0 {
			t.Fatalf("retained chain: %v", err)
		}
		_, _, err = VerifyCurrentPair(cert, key, "mail.example.test", roots, at)
		if at.Equal(now) && err != nil {
			t.Fatal(err)
		}
		if !at.Equal(now) && err == nil {
			t.Fatal("historical validity became renewal authority")
		}
	}
}
func TestPairRequiresApprovedNameTrustAndMatchingKey(t *testing.T) {
	cert, key, roots, now := certificateFixture(t)
	_, wrongKey, _, _ := certificateFixture(t)
	for _, verify := range []func([]byte, []byte, string, *x509.CertPool, time.Time) ([]byte, time.Time, error){VerifyCurrentPair, VerifyRetainedPair} {
		for _, test := range []struct {
			cert, key []byte
			name      string
			roots     *x509.CertPool
		}{
			{cert, key, "other.example.test", roots}, {cert, key, "mail.example.test", nil},
			{cert, key, "mail.example.test", x509.NewCertPool()}, {cert, wrongKey, "mail.example.test", roots},
			{[]byte("invalid"), key, "mail.example.test", roots},
		} {
			if _, _, err := verify(test.cert, test.key, test.name, test.roots, now); err == nil {
				t.Fatal("unverified material accepted")
			}
		}
	}
}
