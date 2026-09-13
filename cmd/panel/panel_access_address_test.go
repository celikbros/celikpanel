package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func accessTestManagedPair(t *testing.T) (string, string, []byte) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CELIKPANEL_TLS", "1")
	t.Setenv("CELIKPANEL_TLS_DIR", dir)
	t.Setenv("CELIKPANEL_TLS_CERT", "")
	t.Setenv("CELIKPANEL_TLS_KEY", "")
	certPath, keyPath := filepath.Join(dir, "panel.crt"), filepath.Join(dir, "panel.key")
	if err := generateSelfSigned(certPath, keyPath); err != nil {
		t.Fatal(err)
	}
	bootstrap, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		t.Fatal(err)
	}
	version := filepath.Join(dir, managedPanelCertVersionPrefix+"00112233445566778899aabbccddeeff")
	if err := os.Mkdir(version, 0700); err != nil {
		t.Fatal(err)
	}
	certPath, keyPath = filepath.Join(version, "panel.crt"), filepath.Join(version, "panel.key")
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(99), DNSNames: []string{"panel.example.test"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, leaf, leaf, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0600); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath, bootstrap.Certificate[0]
}

func TestPanelTLSKeepsIPIdentityAndServesManagedSNI(t *testing.T) {
	certPath, keyPath, bootstrap := accessTestManagedPair(t)
	for _, explicit := range []bool{false, true} {
		t.Run(map[bool]string{false: "managed", true: "explicit"}[explicit], func(t *testing.T) {
			if explicit {
				t.Setenv("CELIKPANEL_TLS_CERT", certPath)
				t.Setenv("CELIKPANEL_TLS_KEY", keyPath)
			}
			server := newPanelHTTPServer("127.0.0.1:0", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
			running, err := startPanelHTTP(server, certPath, keyPath)
			if err != nil {
				t.Fatal(err)
			}
			defer server.Close()
			for _, name := range []string{"", "127.0.0.1", "panel.example.test"} {
				conn, err := tls.DialWithDialer(&net.Dialer{Timeout: time.Second * 5}, "tcp", running.addr.String(), &tls.Config{ServerName: name, InsecureSkipVerify: true}) // Inspect test certificates, intentionally self-signed.
				if err != nil {
					t.Fatal(err)
				}
				leaf := conn.ConnectionState().PeerCertificates[0]
				conn.Close()
				wantBootstrap := !explicit && name != "panel.example.test"
				if bytes.Equal(leaf.Raw, bootstrap) != wantBootstrap {
					t.Fatalf("explicit=%v SNI=%q selected wrong identity", explicit, name)
				}
				if !wantBootstrap && leaf.DNSNames[0] != "panel.example.test" {
					t.Fatal("managed hostname identity lost")
				}
			}
		})
	}
}

func TestPanelIPCertificateNeverCreatesOrRewritesMissingMaterial(t *testing.T) {
	certPath, keyPath, _ := accessTestManagedPair(t)
	original, err := os.ReadFile(filepath.Join(tlsDir(), "panel.crt"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(tlsDir(), "panel.key")); err != nil {
		t.Fatal(err)
	}
	if panelIPCertificate(certPath, keyPath) != nil {
		t.Fatal("incomplete bootstrap accepted")
	}
	remaining, _ := os.ReadFile(filepath.Join(tlsDir(), "panel.crt"))
	if !bytes.Equal(original, remaining) {
		t.Fatal("bootstrap was rewritten")
	}
	if _, err := os.Stat(filepath.Join(tlsDir(), "panel.key")); !os.IsNotExist(err) {
		t.Fatal("missing key was recreated")
	}
}

func TestPanelAccessAddressIsPublicReadOnlyAndIgnoresRequestDestinations(t *testing.T) {
	certPath, keyPath, _ := accessTestManagedPair(t)
	handler := (&Panel{}).requireAuth(panelAccessAddressHandler(certPath, keyPath))
	for _, method := range []string{"GET", "POST", "PUT", "DELETE"} {
		request := httptest.NewRequest(method, "https://attacker.example"+panelAccessAddressPath+"?hostname=evil.example", nil)
		request.Header.Set("X-Forwarded-Host", "evil.example")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if method != "GET" {
			if response.Code != 401 {
				t.Fatalf("%s bypassed authentication", method)
			}
			continue
		}
		var body map[string]string
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if response.Code != 200 || len(body) != 1 || body["hostname"] != "panel.example.test" || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("unexpected metadata %d %v", response.Code, body)
		}
	}
	for _, name := range []string{"evil.example/path", "evil.example:443", "*.example.test", "a..test", "127.0.0.1", "user@evil.example", "evil.example\\path"} {
		if validPanelAccessHostname(name) {
			t.Fatalf("unsafe hostname %q accepted", name)
		}
	}
	t.Setenv("CELIKPANEL_TLS_CERT", certPath)
	response := httptest.NewRecorder()
	panelAccessAddressHandler(certPath, keyPath)(response, httptest.NewRequest("GET", panelAccessAddressPath, nil))
	if response.Body.String() != "{\"hostname\":\"\"}\n" {
		t.Fatalf("explicit certificate suggested managed address: %s", response.Body.String())
	}
}
