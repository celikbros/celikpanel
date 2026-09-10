package licensing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAuthenticatedRefreshCoalescesWithoutStaleAdmissionOrIdlePolling(t *testing.T) {
	pub, priv, c, e := fixture(t)
	var now atomic.Int64
	now.Store(c.IssuedAt)
	m, _ := New(filepath.Join(t.TempDir(), "license.json"), pub, c.ServerID)
	m.clock = func() time.Time { return time.Unix(now.Load(), 0) }
	if err := m.save(e); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int64
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		entered <- struct{}{}
		<-release
		next := c
		next.IssuedAt = now.Load()
		next.RefreshAfter = next.IssuedAt + 45
		next.OfflineUntil = next.IssuedAt + 60
		_ = json.NewEncoder(w).Encode(sign(t, next, priv))
	}))
	defer server.Close()
	m.endpoint = server.URL
	if !m.CanProvision(context.Background()) || calls.Load() != 0 {
		t.Fatal("fresh receipt contacted center")
	}
	now.Add(60)
	if m.Status().CanProvision || calls.Load() != 0 {
		t.Fatal("idle check polled or stale receipt allowed access")
	}
	done := make(chan bool, 100)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); done <- m.CanProvision(context.Background()) }()
	}
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("refresh did not begin")
	}
	select {
	case <-done:
		t.Fatal("request admitted before synchronous verification")
	default:
	}
	close(release)
	wg.Wait()
	for i := 0; i < 100; i++ {
		if !<-done {
			t.Fatal("valid shared result was not accepted")
		}
	}
	if calls.Load() != 1 {
		t.Fatal("refresh duplicated", calls.Load())
	}
	now.Add(60)
	_ = m.Status()
	if calls.Load() != 1 {
		t.Fatal("idle server polled")
	}
}

func TestOutageHasBoundedGraceBackoffAndRecoversOnUse(t *testing.T) {
	pub, priv, c, e := fixture(t)
	now := time.Unix(c.IssuedAt, 0)
	m, _ := New(filepath.Join(t.TempDir(), "license.json"), pub, c.ServerID)
	m.clock = func() time.Time { return now }
	if err := m.save(e); err != nil {
		t.Fatal(err)
	}
	calls := 0
	fail := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if fail {
			w.WriteHeader(503)
			return
		}
		next := c
		next.IssuedAt = now.Unix()
		next.RefreshAfter = next.IssuedAt + 45
		next.OfflineUntil = min(next.IssuedAt+60, next.ExpiresAt)
		_ = json.NewEncoder(w).Encode(sign(t, next, priv))
	}))
	defer server.Close()
	m.endpoint = server.URL
	now = now.Add(45 * time.Second)
	if !m.CanProvision(context.Background()) || calls != 1 {
		t.Fatal("early renewal should retain the unexpired short receipt")
	}
	now = now.Add(15 * time.Second)
	for i := 0; i < 100; i++ {
		if m.CanProvision(context.Background()) {
			t.Fatal("outage authorized stale access")
		}
	}
	if calls != 1 {
		t.Fatal("outage retried on every request", calls)
	}
	now = now.Add(15 * time.Second)
	fail = false
	if !m.CanProvision(context.Background()) || calls != 2 {
		t.Fatal("active request failed to recover")
	}
	now = time.Unix(c.ExpiresAt, 0)
	if m.CanProvision(context.Background()) {
		t.Fatal("refresh extended annual term")
	}
}

func TestExplicitRefreshRejectionPersistsAndOnlyActivationCanReplaceIt(t *testing.T) {
	for _, reason := range []string{"invalid_activation", "invalid_license", "license_in_use", "license_expired"} {
		t.Run(reason, func(t *testing.T) {
			pub, priv, c, e := fixture(t)
			now := time.Unix(c.IssuedAt, 0)
			file := filepath.Join(t.TempDir(), "license.json")
			m, _ := New(file, pub, c.ServerID)
			m.clock = func() time.Time { return now }
			if err := m.save(e); err != nil {
				t.Fatal(err)
			}
			reject := true
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if reject {
					w.WriteHeader(400)
					_ = json.NewEncoder(w).Encode(map[string]string{"error": reason})
					return
				}
				_ = json.NewEncoder(w).Encode(sign(t, c, priv))
			}))
			defer server.Close()
			m.endpoint = server.URL
			if err := m.Refresh(context.Background(), true); err == nil {
				t.Fatal("rejection hidden")
			}
			if m.Status().CanProvision || m.CanProvision(context.Background()) {
				t.Fatal("rejected receipt still grants access")
			}
			restarted, _ := New(file, pub, c.ServerID)
			restarted.clock = m.clock
			restarted.endpoint = server.URL
			if restarted.Status().CanProvision {
				t.Fatal("restart forgot revocation")
			}
			if err := restarted.Activate(context.Background(), "CPK-"+strings.Repeat("d", 64), "fixture"); err == nil || restarted.Status().CanProvision {
				t.Fatal("bad activation removed revocation")
			}
			reject = false
			if err := restarted.Activate(context.Background(), "CPK-"+strings.Repeat("a", 64), "fixture"); err != nil || !restarted.Status().CanProvision {
				t.Fatal("valid activation failed", err)
			}
		})
	}
}

func TestWrongReplacementKeyDoesNotRevokeCurrentLicense(t *testing.T) {
	pub, _, c, e := fixture(t)
	m, _ := New(filepath.Join(t.TempDir(), "license.json"), pub, c.ServerID)
	m.clock = func() time.Time { return time.Unix(c.IssuedAt, 0) }
	if err := m.save(e); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"error":"invalid_license"}`))
	}))
	defer server.Close()
	m.endpoint = server.URL
	if err := m.Activate(context.Background(), "CPK-"+strings.Repeat("d", 64), "fixture"); err == nil {
		t.Fatal("bad key accepted")
	}
	if !m.Status().CanProvision {
		t.Fatal("mistyped replacement revoked current license")
	}
}
