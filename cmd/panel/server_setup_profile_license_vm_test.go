//go:build linux

package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/licensing"
)

type setupProfileLicenseTransport struct {
	base     http.RoundTripper
	envelope func() []byte
}

func (f setupProfileLicenseTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Hostname() != "celikpanel.net" {
		return f.base.RoundTrip(r)
	}
	// No license request from this isolated fixture may contact production.
	if r.URL.Scheme != "https" || r.URL.Path != "/account/" || r.URL.Query().Get("action") != "refresh" || r.Method != http.MethodPost {
		return nil, errors.New("unexpected isolated fixture license request")
	}
	var request map[string]string
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&request); err != nil || request["license_id"] != strings.Repeat("a", 32) || request["server_id"] != strings.Repeat("b", 64) || request["activation_token"] != strings.Repeat("c", 64) {
		return nil, errors.New("fixture license refresh identity differs")
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(f.envelope())), Request: r}, nil
}

// A normal Manager still validates signed, expiring receipts on every activity
// boundary. Only its external issuer is simulated. Unlike a fixed test receipt,
// this supports a minutes-long real package install without a production call.
func serverSetupProfileFixtureLicense(t *testing.T, state string) *licensing.Manager {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	envelope := func() []byte {
		now := time.Now().Unix()
		claims := licensing.Claims{Format: "celikpanel-license-v1", Product: "celikpanel", LicenseID: strings.Repeat("a", 32), ServerID: strings.Repeat("b", 64), ActivatedAt: now - 86400, IssuedAt: now - 1, ExpiresAt: now + 86400, RefreshAfter: now + 45, OfflineUntil: now + 60}
		if state == "expired" {
			claims.IssuedAt = now - 10
			claims.ExpiresAt = now - 1
			claims.OfflineUntil = now - 1
		}
		payload, _ := json.Marshal(claims)
		value, _ := json.Marshal(licensing.Envelope{Payload: base64.StdEncoding.EncodeToString(payload), Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(priv, payload)), ActivationToken: strings.Repeat("c", 64)})
		return value
	}
	file := filepath.Join(t.TempDir(), "license.json")
	if err = os.WriteFile(file, envelope(), 0600); err != nil {
		t.Fatal(err)
	}
	old := http.DefaultTransport
	http.DefaultTransport = setupProfileLicenseTransport{base: old, envelope: envelope}
	t.Cleanup(func() { http.DefaultTransport = old })
	manager, err := licensing.New(file, pub, strings.Repeat("b", 64))
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

func TestServerSetupProfileFixtureLicenseUsesSignedRefreshAndExpiry(t *testing.T) {
	for _, state := range []string{"active", "expired"} {
		t.Run(state, func(t *testing.T) {
			manager := serverSetupProfileFixtureLicense(t, state)
			if err := manager.Refresh(context.Background(), true); err != nil {
				t.Fatal(err)
			}
			if manager.Status().CanProvision != (state == "active") {
				t.Fatalf("signed fixture lost production expiry enforcement: %+v", manager.Status())
			}
			request, err := http.NewRequest(http.MethodPost, "https://celikpanel.net/unexpected", nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = http.DefaultTransport.RoundTrip(request); err == nil {
				t.Fatal("fixture could contact a production license route")
			}
		})
	}
}
