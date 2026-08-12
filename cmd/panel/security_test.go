package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestRequireAuthTerminatesAPIPreflightBeforeHandler(t *testing.T) {
	reached := false
	handler := (&Panel{}).requireAuth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodOptions, "/api/v1/system/stats", nil)
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusNoContent, recorder.Body.String())
	}
	if reached {
		t.Fatal("API preflight reached the protected handler")
	}
}

func TestNewPanelHTTPServer(t *testing.T) {
	server := newPanelHTTPServer(`127.0.0.1:0`, okHandler())
	if server.Addr != `127.0.0.1:0` {
		t.Fatalf(`Addr = %q, want %q`, server.Addr, `127.0.0.1:0`)
	}

	timeouts := []struct {
		name string
		got  time.Duration
		want time.Duration
	}{
		{name: `read header`, got: server.ReadHeaderTimeout, want: 10 * time.Second},
		{name: `read`, got: server.ReadTimeout, want: 5 * time.Minute},
		{name: `write`, got: server.WriteTimeout, want: 30 * time.Minute},
		{name: `idle`, got: server.IdleTimeout, want: 2 * time.Minute},
	}
	for _, tt := range timeouts {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf(`timeout = %s, want %s`, tt.got, tt.want)
			}
		})
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, `/`, nil)
	server.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf(`handler status = %d, want %d`, rec.Code, http.StatusOK)
	}
}

func TestSecurityHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	securityHeaders(true, okHandler()).ServeHTTP(rec, req)

	want := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
	}
	for h, v := range want {
		if got := rec.Header().Get(h); got != v {
			t.Errorf("header %s = %q, want %q", h, got, v)
		}
	}
	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Error("missing Content-Security-Policy")
	}
	if rec.Header().Get("Strict-Transport-Security") == "" {
		t.Error("HSTS missing when secure=true")
	}
}

func TestSecurityHeadersNoHSTSOverPlainHTTP(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	securityHeaders(false, okHandler()).ServeHTTP(rec, req)

	if rec.Header().Get("Strict-Transport-Security") != "" {
		t.Error("HSTS should not be sent over plain HTTP")
	}
}

func TestCSRFAllowsSafeMethods(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/domains", nil)
	// No Origin header at all; a GET must still pass.
	// Hiç Origin başlığı yok; bir GET yine de geçmeli.
	csrfProtect(okHandler()).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET blocked: got %d", rec.Code)
	}
}

func TestCSRFBlocksCrossOriginWrite(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/domains/create", nil)
	req.Host = "panel.example.com"
	req.Header.Set("Origin", "https://evil.example.net")

	csrfProtect(okHandler()).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-origin POST not blocked: got %d, want 403", rec.Code)
	}
}

func TestCSRFAllowsSameOriginWrite(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/domains/create", nil)
	req.Host = "panel.example.com"
	req.Header.Set("Origin", "https://panel.example.com")

	csrfProtect(okHandler()).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("same-origin POST blocked: got %d", rec.Code)
	}
}

func TestCSRFBlocksMissingOriginWrite(t *testing.T) {
	// A state-changing request with neither Origin nor Referer is treated
	// as cross-origin and rejected.
	// Ne Origin ne Referer taşıyan durum değiştiren istek köken-dışı sayılıp
	// reddedilir.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/domains/create", nil)
	req.Host = "panel.example.com"

	csrfProtect(okHandler()).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("origin-less POST not blocked: got %d, want 403", rec.Code)
	}
}

func TestRateLimiter(t *testing.T) {
	l := newRateLimiter(3, time.Minute)
	for i := 0; i < 3; i++ {
		if !l.allow("1.2.3.4") {
			t.Fatalf("attempt %d denied, want allowed", i+1)
		}
	}
	if l.allow("1.2.3.4") {
		t.Fatal("4th attempt allowed, want denied")
	}
	// A different key has its own budget.
	// Farklı bir anahtarın kendi bütçesi vardır.
	if !l.allow("5.6.7.8") {
		t.Fatal("different IP denied, want allowed")
	}
}

func TestRateLimiterWindowExpiry(t *testing.T) {
	l := newRateLimiter(1, 30*time.Millisecond)
	if !l.allow("k") {
		t.Fatal("first attempt denied")
	}
	if l.allow("k") {
		t.Fatal("second immediate attempt allowed")
	}
	time.Sleep(40 * time.Millisecond)
	if !l.allow("k") {
		t.Fatal("attempt after window still denied")
	}
}

func TestRateLimiterSweepsExpiredOneOffKeys(t *testing.T) {
	l := newRateLimiter(2, time.Minute)
	old := time.Now().Add(-2 * time.Minute)
	l.hits["expired-a"] = []time.Time{old}
	l.hits["expired-b"] = []time.Time{old}

	if !l.allow("current") {
		t.Fatal("current key denied")
	}
	if _, ok := l.hits["expired-a"]; ok {
		t.Fatal("first expired key was not removed")
	}
	if _, ok := l.hits["expired-b"]; ok {
		t.Fatal("second expired key was not removed")
	}
	if len(l.hits) != 1 {
		t.Fatalf("limiter retained %d keys, want only the current key", len(l.hits))
	}
}
