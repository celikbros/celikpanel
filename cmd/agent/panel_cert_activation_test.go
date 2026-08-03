package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"
)

func TestPanelCertificateActivationStateLifecycle(t *testing.T) {
	state, err := newPanelCertificateActivationState("Panel.Example.Test ")
	if err != nil {
		t.Fatalf("new state: %v", err)
	}
	if state.Domain != "panel.example.test" ||
		state.LineageName != panelCertLineageName(state.Domain) ||
		state.Phase != panelCertificateActivationPendingSource {
		t.Fatalf("unexpected pending source state: %#v", state)
	}

	expiry := time.Date(2030, 1, 2, 3, 4, 5, 987654321, time.FixedZone("test", 3600))
	leaf := []byte("certificate DER")
	state, err = bindPanelCertificateActivationMaterial(state, leaf, expiry)
	if err != nil {
		t.Fatalf("bind certificate material: %v", err)
	}
	if state.Phase != panelCertificateActivationPendingPublish ||
		state.LeafSHA256 != panelCertificateLeafSHA256(leaf) ||
		state.NotAfter == nil ||
		!state.NotAfter.Equal(expiry.UTC().Truncate(time.Second)) ||
		state.NotAfter.Location() != time.UTC {
		t.Fatalf("unexpected bound state: %#v", state)
	}

	state, err = panelCertificateActivationWithPhase(
		state,
		panelCertificateActivationPendingRestart,
	)
	if err != nil {
		t.Fatalf("advance phase: %v", err)
	}
	failedAt := time.Date(2030, 1, 2, 4, 5, 6, 999, time.UTC)
	state, err = panelCertificateActivationFailure(
		state,
		failedAt,
		errors.New(" restart\nfailed\t "),
	)
	if err != nil {
		t.Fatalf("record failure: %v", err)
	}
	if state.Attempts != 1 || state.LastError != "restart failed" ||
		state.LastAttemptAt == nil ||
		!state.LastAttemptAt.Equal(failedAt.Truncate(time.Second)) {
		t.Fatalf("unexpected failed state: %#v", state)
	}
}

func TestPanelCertificateActivationStateValidation(t *testing.T) {
	valid, err := newPanelCertificateActivationState("panel.example.test")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*panelCertificateActivationState)
	}{
		{"version", func(s *panelCertificateActivationState) { s.Version++ }},
		{"domain", func(s *panelCertificateActivationState) { s.Domain = "UPPER.example.test" }},
		{"lineage", func(s *panelCertificateActivationState) { s.LineageName = "other" }},
		{"phase", func(s *panelCertificateActivationState) { s.Phase = "unknown" }},
		{"source material", func(s *panelCertificateActivationState) { s.LeafSHA256 = strings.Repeat("0", 64) }},
		{"orphan attempt time", func(s *panelCertificateActivationState) {
			now := time.Now().UTC().Truncate(time.Second)
			s.LastAttemptAt = &now
		}},
		{"orphan error", func(s *panelCertificateActivationState) { s.LastError = "failed" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := valid
			test.mutate(&state)
			if err := validatePanelCertificateActivationState(state); err == nil {
				t.Fatal("expected validation failure")
			}
		})
	}

	materialState := valid
	materialState.Phase = panelCertificateActivationPendingVerify
	if err := validatePanelCertificateActivationState(materialState); err == nil {
		t.Fatal("expected material phase without material to fail")
	}
}

func TestPanelCertificateActivationStateCanonicalJSON(t *testing.T) {
	state, err := newPanelCertificateActivationState("panel.example.test")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := canonicalPanelCertificateActivationState(state)
	if err != nil {
		t.Fatalf("marshal canonical state: %v", err)
	}
	decoded, err := decodePanelCertificateActivationState(raw)
	if err != nil {
		t.Fatalf("decode canonical state: %v", err)
	}
	if decoded != state {
		t.Fatalf("round trip mismatch: got %#v want %#v", decoded, state)
	}

	nonCanonical := append([]byte{' '}, raw...)
	if _, err := decodePanelCertificateActivationState(nonCanonical); err == nil {
		t.Fatal("expected non-canonical JSON to fail")
	}
	unknown := bytes.Replace(raw, []byte("}\n"), []byte(",\"unknown\":true}\n"), 1)
	if _, err := decodePanelCertificateActivationState(unknown); err == nil {
		t.Fatal("expected unknown field to fail")
	}
	trailer := append(append([]byte{}, raw...), []byte("{}\n")...)
	if _, err := decodePanelCertificateActivationState(trailer); err == nil {
		t.Fatal("expected multiple JSON values to fail")
	}
}

func TestPanelCertificateActivationBackoff(t *testing.T) {
	want := []time.Duration{
		0,
		15 * time.Second,
		30 * time.Second,
		60 * time.Second,
		120 * time.Second,
		240 * time.Second,
		300 * time.Second,
		300 * time.Second,
	}
	for attempts, expected := range want {
		if got := panelCertificateActivationBackoff(uint32(attempts)); got != expected {
			t.Fatalf("attempt %d: got %s want %s", attempts, got, expected)
		}
	}

	state, err := newPanelCertificateActivationState("panel.example.test")
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	state, err = panelCertificateActivationFailure(state, base, errors.New("failed"))
	if err != nil {
		t.Fatal(err)
	}
	if panelCertificateActivationRetryReady(state, base.Add(14*time.Second)) {
		t.Fatal("retry became ready before backoff elapsed")
	}
	if !panelCertificateActivationRetryReady(state, base.Add(15*time.Second)) {
		t.Fatal("retry did not become ready at backoff boundary")
	}
}

func TestSanitizePanelCertificateActivationError(t *testing.T) {
	input := errors.New("\x00  " + strings.Repeat("é", 700) + "\n")
	got := sanitizePanelCertificateActivationError(input)
	if len(got) > panelCertificateActivationErrorMaxSize {
		t.Fatalf("sanitized error is too large: %d", len(got))
	}
	if err := validatePanelCertificateActivationError(got); err != nil {
		t.Fatalf("sanitized error is invalid: %v", err)
	}
	if !strings.HasPrefix(got, "é") {
		t.Fatalf("unexpected sanitized error prefix: %q", got[:min(len(got), 16)])
	}
}

func TestVerifyServedPanelCertificate(t *testing.T) {
	serverName := "panel.example.test"
	certificate, leafDER := testPanelCertificate(t, serverName)

	address, done := startPanelCertificateTLSServer(t, certificate)
	if err := verifyServedPanelCertificate(
		context.Background(),
		address,
		serverName,
		panelCertificateLeafSHA256(leafDER),
	); err != nil {
		t.Fatalf("verify served certificate: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("TLS server: %v", err)
	}

	address, done = startPanelCertificateTLSServer(t, certificate)
	if err := verifyServedPanelCertificate(
		context.Background(),
		address,
		serverName,
		strings.Repeat("0", 64),
	); err == nil {
		t.Fatal("expected mismatched leaf fingerprint to fail")
	}
	if err := <-done; err != nil {
		t.Fatalf("TLS server: %v", err)
	}

	if err := verifyServedPanelCertificate(
		context.Background(),
		"192.0.2.1:2083",
		serverName,
		panelCertificateLeafSHA256(leafDER),
	); err == nil {
		t.Fatal("expected non-loopback verification target to fail")
	}
}

func TestPanelCertificateActivationVerifierSeam(t *testing.T) {
	original := panelCertificateActivationVerifyServed
	t.Cleanup(func() { panelCertificateActivationVerifyServed = original })
	called := false
	panelCertificateActivationVerifyServed = func(
		context.Context,
		string,
		string,
		string,
	) error {
		called = true
		return nil
	}
	if err := panelCertificateActivationVerifyServed(
		context.Background(),
		"127.0.0.1:2083",
		"panel.example.test",
		strings.Repeat("0", 64),
	); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("verification seam was not called")
	}
}

func testPanelCertificate(t *testing.T, serverName string) (tls.Certificate, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate test key: %v", err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: serverName},
		DNSNames:     []string{serverName},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create test certificate: %v", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal test key: %v", err)
	}
	certificate, err := tls.X509KeyPair(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}),
	)
	if err != nil {
		t.Fatalf("load test certificate: %v", err)
	}
	return certificate, leafDER
}

func startPanelCertificateTLSServer(
	t *testing.T,
	certificate tls.Certificate,
) (string, <-chan error) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for TLS test: %v", err)
	}
	tlsListener := tls.NewListener(listener, &tls.Config{
		Certificates: []tls.Certificate{certificate},
		MinVersion:   tls.VersionTLS12,
	})
	done := make(chan error, 1)
	go func() {
		defer tlsListener.Close()
		connection, err := tlsListener.Accept()
		if err == nil {
			err = connection.(*tls.Conn).Handshake()
			_ = connection.Close()
		}
		done <- err
	}()
	return tlsListener.Addr().String(), done
}
