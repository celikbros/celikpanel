package licensing

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey, Claims, Envelope) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC).Unix()
	c := Claims{Format: "celikpanel-license-v1", Product: "celikpanel", LicenseID: strings.Repeat("a", 32), ServerID: strings.Repeat("b", 64),
		IssuedAt: now, ActivatedAt: now, ExpiresAt: now + 365*86400, RefreshAfter: now + 86400, OfflineUntil: now + 7*86400}
	return pub, priv, c, sign(t, c, priv)
}
func sign(t *testing.T, c Claims, priv ed25519.PrivateKey) Envelope {
	t.Helper()
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	return Envelope{Payload: base64.StdEncoding.EncodeToString(b), Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(priv, b)), ActivationToken: strings.Repeat("c", 64)}
}
func TestLicenseSignatureBindingAndDates(t *testing.T) {
	pub, priv, c, e := fixture(t)
	now := time.Unix(c.IssuedAt, 0)
	if _, err := Verify(e, pub, c.ServerID, now); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*Claims){"product": func(c *Claims) { c.Product = "other" }, "server": func(c *Claims) { c.ServerID = strings.Repeat("d", 64) },
		"future": func(c *Claims) { c.IssuedAt += 3600 }, "offline too long": func(c *Claims) { c.OfflineUntil = c.IssuedAt + 8*86400 },
		"activation": func(c *Claims) { c.ActivatedAt = c.ExpiresAt + 1 }, "format": func(c *Claims) { c.Format = "v2" }}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			bad := c
			change(&bad)
			if _, err := Verify(sign(t, bad, priv), pub, c.ServerID, now); err == nil {
				t.Fatal("accepted invalid entitlement")
			}
		})
	}
	changed := e
	changed.Payload = base64.StdEncoding.EncodeToString([]byte("{}"))
	if _, err := Verify(changed, pub, c.ServerID, now); err == nil {
		t.Fatal("accepted tampering")
	}
	wrong, _, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := Verify(e, wrong, c.ServerID, now); err == nil {
		t.Fatal("accepted wrong signer")
	}
}
func TestLicenseManagerOutageExpiryAndRefresh(t *testing.T) {
	pub, priv, c, e := fixture(t)
	now := time.Unix(c.IssuedAt, 0)
	m, err := New(filepath.Join(t.TempDir(), "license.json"), pub, c.ServerID)
	if err != nil {
		t.Fatal(err)
	}
	m.clock = func() time.Time { return now }
	if got := m.Status(); got.State != "missing" || got.CanProvision {
		t.Fatal(got)
	}
	fail := false
	tamper := false
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if fail {
			w.WriteHeader(503)
			return
		}
		v := e
		if tamper {
			v.Signature = "invalid"
		}
		_ = json.NewEncoder(w).Encode(v)
	}))
	defer server.Close()
	m.endpoint = server.URL
	if err := m.Activate(context.Background(), "CPK-"+strings.Repeat("a", 64), "frankfurt"); err != nil {
		t.Fatal(err)
	}
	if !m.Status().CanProvision {
		t.Fatal(m.Status())
	}
	info, _ := os.Stat(m.file)
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
	if err := m.Refresh(context.Background(), false); err != nil || calls != 1 {
		t.Fatal("premature refresh", err, calls)
	}
	original, _ := os.ReadFile(m.file)
	fail = true
	now = now.Add(2 * 24 * time.Hour)
	if err := m.Refresh(context.Background(), false); err == nil {
		t.Fatal("outage hidden")
	}
	after, _ := os.ReadFile(m.file)
	if string(after) != string(original) || m.Status().CanProvision {
		t.Fatal("outage must retain renewal credentials but deny stale access")
	}
	now = time.Unix(c.OfflineUntil, 0)
	if got := m.Status(); got.State != "verification_unavailable" || got.CanProvision {
		t.Fatal(got)
	}
	fail = false
	tamper = true
	if err := m.Refresh(context.Background(), true); err == nil {
		t.Fatal("accepted bad refresh")
	}
	after, _ = os.ReadFile(m.file)
	if string(after) != string(original) {
		t.Fatal("bad signature replaced good receipt")
	}
	tamper = false
	c.IssuedAt = now.Unix()
	c.RefreshAfter = c.IssuedAt + 86400
	c.OfflineUntil = c.IssuedAt + 7*86400
	e = sign(t, c, priv)
	if err := m.Refresh(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if !m.Status().CanProvision {
		t.Fatal(m.Status())
	}
	now = time.Unix(c.ExpiresAt, 0)
	if got := m.Status(); got.State != "expired" || got.CanProvision {
		t.Fatal(got)
	}
	c.IssuedAt = now.Unix()
	c.RefreshAfter = c.IssuedAt + 86400
	c.ExpiresAt += 365 * 86400
	c.OfflineUntil = c.IssuedAt + 7*86400
	e = sign(t, c, priv)
	if err := m.Refresh(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if !m.Status().CanProvision {
		t.Fatal("renewal not effective")
	}
}
func TestLicenseMachineIdentity(t *testing.T) {
	for _, bad := range []string{"", strings.Repeat("0", 32), strings.Repeat("z", 32)} {
		if _, err := ServerID([]byte(bad)); err == nil {
			t.Fatal("bad identity accepted")
		}
	}
	a, err := ServerID([]byte(strings.Repeat("a", 32)))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := ServerID([]byte(strings.Repeat("a", 32) + "\n"))
	if a != b || !hex64.MatchString(a) {
		t.Fatal("unstable identity")
	}
}
