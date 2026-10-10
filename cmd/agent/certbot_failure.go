package main

import (
	"strings"

	"github.com/alicelik/celikpanel/internal/hostcmd"
	"github.com/alicelik/celikpanel/internal/transport"
)

// What a certbot run that did not issue a certificate came to (11 Oct 2026;
// D-024).
//
// The issue route answered every such run `500 INTERNAL` "internal server
// error"; the cause was only in the Panel's log (measured on Debian 13, Ubuntu
// 24.04 and Arch, where certbot could not reach the certificate authority).
// certbot says what went wrong in its own output, and the ACME error types it
// prints are fixed by RFC 8555, so the output is read for exactly these:
//
//   - the authority's limit (`urn:ietf:params:acme:error:rateLimited`, "too
//     many certificates", "too many failed authorizations");
//   - a validation the authority refused ("Certbot failed to authenticate some
//     domains", "Some challenges have failed", the ACME types `unauthorized`,
//     `dns`, `connection`, `tls`, `caa` that certbot prints as "Type: ...");
//   - an authority that could not be reached at all (the Python connection
//     and TLS errors certbot prints before any order exists).
//
// Anything else is "certbot exited with an error" and nothing more is claimed.
// Beside the kind goes one bounded line of certbot's own words.
//
// Sertifika çıkarmayan bir certbot çalışmasının neye vardığı. certbot neyin
// ters gittiğini kendi çıktısında söyler; çıktı yalnızca şunlar için okunur:
// otoritenin sınırı, otoritenin reddettiği doğrulama, hiç ulaşılamayan otorite.
// Bunların dışındaki her şey "certbot hata ile çıktı"dır ve fazlası ileri
// sürülmez.

// certbotFailureKind classifies certbot's combined output. It answers one of
// the transport.CertificateFailure* kinds.
func certbotFailureKind(output string) string {
	// One space between words, so "Type:   dns" reads as "type: dns".
	lower := strings.ToLower(strings.Join(strings.Fields(output), " "))
	contains := func(needles ...string) bool {
		for _, needle := range needles {
			if strings.Contains(lower, needle) {
				return true
			}
		}
		return false
	}
	switch {
	case contains("urn:ietf:params:acme:error:ratelimited", "too many certificates", "too many failed authorizations",
		"too many new orders", "too many registrations", "rate limit"):
		return transport.CertificateFailureRateLimited
	case contains("certbot failed to authenticate some domains", "some challenges have failed", "challenge failed for domain",
		"urn:ietf:params:acme:error:unauthorized", "urn:ietf:params:acme:error:dns", "urn:ietf:params:acme:error:connection",
		"urn:ietf:params:acme:error:tls", "urn:ietf:params:acme:error:caa",
		"type: unauthorized", "type: dns", "type: connection", "type: tls", "type: caa"):
		return transport.CertificateFailureValidation
	case contains("requests.exceptions.", "max retries exceeded", "newconnectionerror", "connectionerror",
		"temporary failure in name resolution", "name or service not known", "certificate_verify_failed",
		"connection refused", "network is unreachable", "read timed out", "connect timeout"):
		return transport.CertificateFailureAuthorityUnreachable
	}
	return transport.CertificateFailureTool
}

// certbotFailureLine picks the one line of certbot's output worth showing:
// the first that states the failure, without the line that only names its log
// file. It is bounded, on one line, and password-like assignments are blanked.
func certbotFailureLine(output string) string {
	var lines []string
	for _, raw := range strings.Split(output, "\n") {
		line := strings.Join(strings.Fields(raw), " ")
		lower := strings.ToLower(line)
		if line == "" || strings.HasPrefix(lower, "saving debug log to") ||
			strings.HasPrefix(lower, "ask for help or search for solutions") ||
			strings.HasPrefix(lower, "see the logfile") {
			continue
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return ""
	}
	// The authority's own "Detail:" line says most; else the first line that
	// names a failure, with the line after it when it only introduces one
	// ("An unexpected error occurred:"); else the first line.
	chosen := lines[0]
	found := false
	for _, line := range lines {
		if strings.HasPrefix(strings.ToLower(line), "detail:") {
			chosen, found = line, true
			break
		}
	}
	for index := 0; !found && index < len(lines); index++ {
		lower := strings.ToLower(lines[index])
		if strings.Contains(lower, "error") || strings.Contains(lower, "failed") {
			chosen, found = lines[index], true
			if strings.HasSuffix(chosen, ":") && index+1 < len(lines) {
				chosen += " " + lines[index+1]
			}
		}
	}
	return hostcmd.Bounded(mailServiceSecret.ReplaceAllString(chosen, "${1}…"), 300)
}
