package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPHPHandlersRejectUnsupportedMethods(t *testing.T) {
	panel := &Panel{}
	testCases := []struct {
		name    string
		method  string
		path    string
		allowed string
		handler http.HandlerFunc
	}{
		{
			name:    "pool collection cannot pretend to save",
			method:  http.MethodPost,
			path:    "/api/v1/php/pools?version=8.3",
			allowed: http.MethodGet,
			handler: panel.handlePHPPools,
		},
		{
			name:    "extensions reject put",
			method:  http.MethodPut,
			path:    "/api/v1/php/extensions?version=8.3",
			allowed: "GET, POST",
			handler: panel.handlePHPExtensions,
		},
		{
			name:    "configuration rejects delete",
			method:  http.MethodDelete,
			path:    "/api/v1/php/config?version=8.3",
			allowed: "GET, POST",
			handler: panel.handlePHPConfig,
		},
		{
			name:    "advanced pool rejects patch",
			method:  http.MethodPatch,
			path:    "/api/v1/php/pool-config?version=8.3&pool=site42",
			allowed: "GET, POST, DELETE",
			handler: panel.handlePHPPoolConfig,
		},
		{
			name:    "extended configuration rejects delete",
			method:  http.MethodDelete,
			path:    "/api/v1/php/extended-config?version=8.3",
			allowed: "GET, POST",
			handler: panel.handlePHPExtendedConfig,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(testCase.method, testCase.path, nil)
			recorder := httptest.NewRecorder()

			testCase.handler(recorder, request)

			if recorder.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
			}
			if got := recorder.Header().Get("Allow"); got != testCase.allowed {
				t.Fatalf("Allow = %q, want %q", got, testCase.allowed)
			}
		})
	}
}
