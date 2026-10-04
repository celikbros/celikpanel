package mailhostartifact

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"time"
)

// VerifyRetainedPair verifies historical chain evidence at a point within the
// leaf lifetime. A valid retained artifact is NOT proof of current service health.
func VerifyRetainedPair(cert, key []byte, domain string, roots *x509.CertPool, now time.Time) ([]byte, time.Time, error) {
	return verifyPair(cert, key, domain, roots, now, true)
}

// VerifyCurrentPair is for a newly renewed source: an expired or not-yet-valid
// pair must never be published merely because its historical chain verifies.
func VerifyCurrentPair(cert, key []byte, domain string, roots *x509.CertPool, now time.Time) ([]byte, time.Time, error) {
	return verifyPair(cert, key, domain, roots, now, false)
}
func verifyPair(cert, key []byte, domain string, roots *x509.CertPool, now time.Time, retained bool) ([]byte, time.Time, error) {
	pair, err := tls.X509KeyPair(cert, key)
	if err != nil {
		return nil, time.Time{}, err
	}
	if len(pair.Certificate) == 0 {
		return nil, time.Time{}, errors.New("empty host certificate")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, time.Time{}, err
	}
	if roots == nil {
		return nil, time.Time{}, errors.New("system trust roots unavailable")
	}
	intermediates := x509.NewCertPool()
	for _, der := range pair.Certificate[1:] {
		c, err := x509.ParseCertificate(der)
		if err != nil {
			return nil, time.Time{}, err
		}
		intermediates.AddCert(c)
	}
	if retained {
		if now.Before(leaf.NotBefore) {
			now = leaf.NotBefore
		}
		if !now.Before(leaf.NotAfter) {
			now = leaf.NotAfter.Add(-time.Second)
		}
	}
	if _, err = leaf.Verify(x509.VerifyOptions{DNSName: domain, Roots: roots, Intermediates: intermediates, CurrentTime: now, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
		return nil, time.Time{}, err
	}
	return leaf.Raw, leaf.NotAfter, nil
}
