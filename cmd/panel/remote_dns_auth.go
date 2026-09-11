package main

import (
	"net/http"
	"strings"
)

// Only these exact machine endpoints use the dedicated DNS credential protocol.
// Administrative pairing and revocation retain session authentication and CSRF.
func remoteDNSMachinePath(path string) bool {
	switch path {
	case "/api/v1/dns/remote/accept", "/api/v1/dns/remote/receiver/status", "/api/v1/dns/remote/receiver/publish":
		return true
	default:
		return false
	}
}

func remoteDNSMachineRequest(r *http.Request) bool {
	if !remoteDNSMachinePath(r.URL.Path) || r.TLS == nil || r.URL.RawPath != "" || r.URL.RawQuery != "" || r.URL.ForceQuery {
		return false
	}
	for name := range r.Header {
		lower := strings.ToLower(name)
		if lower == "origin" || lower == "referer" || lower == "cookie" || strings.HasPrefix(lower, "sec-fetch-") {
			return false
		}
	}
	return true
}

// Authentication precedes both ordinary CSRF and session middleware. Only a
// private claim produced from a verified enrollment/client credential may take
// that narrow machine path. TLS/header shape alone never authenticates callers.
func (p *Panel) requireRemoteDNSMachineAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if remoteDNSMachinePath(r.URL.Path) && !p.authenticateRemoteDNSMachine(w, r) {
			return
		}
		next.ServeHTTP(w, r)
	})
}
