package licensing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestActivityRefreshCoalescesAndStopsWhenIdle(t *testing.T) {
	pub, priv, c, e := fixture(t)
	var now atomic.Int64
	now.Store(c.IssuedAt)
	m, _ := New(filepath.Join(t.TempDir(), "license.json"), pub, c.ServerID)
	m.clock = func() time.Time { return time.Unix(now.Load(), 0) }
	if err := m.save(e); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int64
	entered := make(chan struct{}, 4)
	release := make(chan struct{}, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		entered <- struct{}{}
		<-release
		next := c
		next.IssuedAt = now.Load()
		next.RefreshAfter = next.IssuedAt + 86400
		next.OfflineUntil = min(next.IssuedAt+7*86400, next.ExpiresAt)
		_ = json.NewEncoder(w).Encode(sign(t, next, priv))
	}))
	defer server.Close()
	m.endpoint = server.URL
	// Reading local status and recent authenticated use have no network cost.
	m.Activity()
	if !m.Status().CanProvision || calls.Load() != 0 {
		t.Fatal("fresh receipt contacted service")
	}
	now.Store(c.RefreshAfter)
	// Advancing time alone does not trigger network work, even past the deadline.
	if !m.Status().CanProvision || calls.Load() != 0 {
		t.Fatal("idle manager contacted service")
	}
	m.Activity()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("activity did not refresh")
	}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); m.Activity() }()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal("concurrent users duplicated refresh", calls.Load())
	}
	release <- struct{}{}
	// Wait for the shared refresh, then verify the persisted daily schedule.
	if err := m.Refresh(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	m.Activity()
	if calls.Load() != 1 {
		t.Fatal("fresh result was not shared")
	}
	now.Add(86400)
	_ = m.Status()
	if calls.Load() != 1 {
		t.Fatal("idle server polled")
	}
}

func TestActivityFailureBackoffAndProvisioningRecovery(t *testing.T) {
	pub, priv, c, e := fixture(t)
	now := time.Unix(c.OfflineUntil, 0)
	m, _ := New(filepath.Join(t.TempDir(), "license.json"), pub, c.ServerID)
	m.clock = func() time.Time { return now }
	if err := m.save(e); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int64
	var fail atomic.Bool
	fail.Store(true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if fail.Load() {
			w.WriteHeader(503)
			return
		}
		next := c
		next.IssuedAt = now.Unix()
		next.RefreshAfter = next.IssuedAt + 86400
		next.OfflineUntil = min(next.IssuedAt+7*86400, next.ExpiresAt)
		_ = json.NewEncoder(w).Encode(sign(t, next, priv))
	}))
	defer server.Close()
	m.endpoint = server.URL
	if m.CanProvision(context.Background()) {
		t.Fatal("stale offline receipt authorized provisioning")
	}
	for i := 0; i < 100; i++ {
		m.Activity()
		if m.CanProvision(context.Background()) {
			t.Fatal("backoff granted provisioning")
		}
	}
	if calls.Load() != 1 {
		t.Fatal("outage retried on every request", calls.Load())
	}
	// No browser session is needed: the next licensed operation can recover.
	now = now.Add(15 * time.Minute)
	fail.Store(false)
	if !m.CanProvision(context.Background()) || calls.Load() != 2 {
		t.Fatal("provisioning did not recover")
	}
	now = time.Unix(c.ExpiresAt, 0)
	if m.CanProvision(context.Background()) {
		t.Fatal("login/activity extended the license term")
	}
}
