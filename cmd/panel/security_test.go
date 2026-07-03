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
