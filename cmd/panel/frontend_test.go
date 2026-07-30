package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFrontendHandlerCachePolicy(t *testing.T) {
	root := t.TempDir()
	assets := filepath.Join(root, "assets")
	if err := os.MkdirAll(assets, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("app shell"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assets, "index-Ab12Cd.js"), []byte("hashed asset"), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		path       string
		wantCache  string
		wantBody   string
		wantStatus int
	}{
		{
			name:       "entry document revalidates",
			path:       "/",
			wantCache:  "no-cache",
			wantBody:   "app shell",
			wantStatus: http.StatusOK,
		},
		{
			name:       "SPA fallback revalidates",
			path:       "/settings",
			wantCache:  "no-cache",
			wantBody:   "app shell",
			wantStatus: http.StatusOK,
		},
		{
			name:       "fingerprinted asset is immutable",
			path:       "/assets/index-Ab12Cd.js",
			wantCache:  "public, max-age=31536000, immutable",
			wantBody:   "hashed asset",
			wantStatus: http.StatusOK,
		},
		{
			name:       "API never falls through to SPA",
			path:       "/api/missing",
			wantCache:  "",
			wantBody:   "404 page not found",
			wantStatus: http.StatusNotFound,
		},
	}

	handler := frontendHandler(root)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, tt.path, nil)
			handler.ServeHTTP(recorder, request)

			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, tt.wantStatus)
			}
			if got := recorder.Header().Get("Cache-Control"); got != tt.wantCache {
				t.Fatalf("Cache-Control = %q, want %q", got, tt.wantCache)
			}
			if got := strings.TrimSpace(recorder.Body.String()); got != tt.wantBody {
				t.Fatalf("body = %q, want %q", got, tt.wantBody)
			}
		})
	}
}
