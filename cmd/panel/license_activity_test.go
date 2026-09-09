package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/licensing"
)

type licenseTestTransport func(*http.Request) (*http.Response, error)

func (f licenseTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestLicenseActivityAcrossAuthenticatedRoles(t *testing.T) {
	fixture := newAuthzMatrixFixture(t)
	seedAdditionalUserSession(t, &fixture)
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Now().Unix()
	c := licensing.Claims{Format: "celikpanel-license-v1", Product: "celikpanel", LicenseID: strings.Repeat("a", 32), ServerID: strings.Repeat("b", 64),
		ActivatedAt: now - 3*86400, IssuedAt: now - 2*86400, ExpiresAt: now + 365*86400, RefreshAfter: now - 86400, OfflineUntil: now + 5*86400}
	encode := func(c licensing.Claims) []byte {
		payload, _ := json.Marshal(c)
		body, _ := json.Marshal(licensing.Envelope{Payload: base64.StdEncoding.EncodeToString(payload), Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(priv, payload)), ActivationToken: strings.Repeat("c", 64)})
		return body
	}
	stale := encode(c)
	c.IssuedAt = now
	c.RefreshAfter = now + 86400
	c.OfflineUntil = now + 7*86400
	fresh := encode(c)
	var calls atomic.Int64
	previous := http.DefaultTransport
	http.DefaultTransport = licenseTestTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(fresh))}, nil
	})
	defer func() { http.DefaultTransport = previous }()
	for _, id := range []int{authzMatrixAdminID, authzMatrixResellerID, authzMatrixCustomerID, authzMatrixAdditionalUserID} {
		file := filepath.Join(t.TempDir(), "license.json")
		if err := os.WriteFile(file, stale, 0600); err != nil {
			t.Fatal(err)
		}
		m, err := licensing.New(file, pub, c.ServerID)
		if err != nil {
			t.Fatal(err)
		}
		fixture.panel.license = m
		before := calls.Load()
		// Stored valid sessions alone, public requests, and rejected sessions
		// cannot trigger the licensing service.
		for _, request := range []struct{ path, token string }{
			{"/api/v1/auth/me", ""}, {"/api/v1/auth/me", "invalid"},
			{"/api/v1/auth/me", fixture.tokens[authzMatrixSuspendedID]},
			{"/api/v1/auth/login", fixture.tokens[id]},
		} {
			requestWithToken(fixture.handler, http.MethodGet, request.path, request.token)
		}
		if calls.Load() != before {
			t.Fatal("unauthenticated/public activity refreshed")
		}
		w := requestWithToken(fixture.handler, http.MethodGet, "/api/v1/auth/me", fixture.tokens[id])
		if w.Code != 204 {
			t.Fatal(id, w.Code, w.Body.String())
		}
		if err := m.Refresh(context.Background(), false); err != nil {
			t.Fatal(err)
		}
		if calls.Load() != before+1 {
			t.Fatal("role did not trigger shared refresh", id, calls.Load())
		}
		for _, token := range fixture.tokens {
			requestWithToken(fixture.handler, http.MethodGet, "/api/v1/auth/me", token)
		}
		if calls.Load() != before+1 {
			t.Fatal("each user refreshed separately")
		}
	}
}
