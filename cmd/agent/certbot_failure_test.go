package main

import (
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

// What certbot printed in the measured run (Debian 13, 2026-10-09: the lab's
// isolation made the certificate authority unreachable), and what it prints
// for the other kinds. Only what the output states is a kind of its own.
func TestCertbotFailureIsReadFromItsOwnOutput(t *testing.T) {
	measured := "Saving debug log to /var/log/celikpanel/certbot/letsencrypt.log\n" +
		"An unexpected error occurred:\n" +
		"requests.exceptions.SSLError: HTTPSConnectionPool(host='acme-v02.api.letsencrypt.org', port=443): Max retries exceeded with url: /directory (Caused by SSLError(SSLCertVerificationError(1, \"[SSL: CERTIFICATE_VERIFY_FAILED] certificate verify failed: Hostname mismatch, certificate is not valid for 'acme-v02.api.letsencrypt.org'. (_ssl.c:1029)\")))\n" +
		"Ask for help or search for solutions at https://community.letsencrypt.org. See the logfile /var/log/celikpanel/certbot/letsencrypt.log or re-run Certbot with -v for more details.\n"
	validation := "Saving debug log to /var/log/celikpanel/certbot/letsencrypt.log\n" +
		"Requesting a certificate for shop.example and www.shop.example\n\n" +
		"Certbot failed to authenticate some domains (authenticator: webroot). The Certificate Authority reported these problems:\n" +
		"  Domain: www.shop.example\n  Type:   dns\n  Detail: DNS problem: NXDOMAIN looking up A for www.shop.example - check that a DNS record exists for this domain\n\n" +
		"Hint: The Certificate Authority failed to download the temporary challenge files created by Certbot.\n\n" +
		"Some challenges have failed.\n"
	rateLimited := "Saving debug log to /var/log/celikpanel/certbot/letsencrypt.log\n" +
		"Requesting a certificate for shop.example\n" +
		"An unexpected error occurred:\n" +
		"Error creating new order :: too many certificates (5) already issued for this exact set of domains in the last 168h0m0s, retry after 2026-10-12 03:04:05 UTC: see https://letsencrypt.org/docs/rate-limits/\n"
	other := "Saving debug log to /var/log/celikpanel/certbot/letsencrypt.log\n" +
		"The requested webroot plugin does not appear to be installed\n"

	for name, c := range map[string]struct {
		output string
		kind   string
		line   string
	}{
		"the authority cannot be reached": {measured, transport.CertificateFailureAuthorityUnreachable, "Max retries exceeded"},
		"the authority refused a name":    {validation, transport.CertificateFailureValidation, "DNS problem: NXDOMAIN looking up A for www.shop.example"},
		"the authority's limit":           {rateLimited, transport.CertificateFailureRateLimited, "too many certificates"},
		"anything else":                   {other, transport.CertificateFailureTool, "webroot plugin does not appear to be installed"},
		"no output at all":                {"", transport.CertificateFailureTool, ""},
	} {
		if kind := certbotFailureKind(c.output); kind != c.kind {
			t.Fatalf("%s: kind %q, want %q", name, kind, c.kind)
		}
		line := certbotFailureLine(c.output)
		if !strings.Contains(line, c.line) || (c.line == "" && line != "") {
			t.Fatalf("%s: line %q, want it to carry %q", name, line, c.line)
		}
		// One bounded line, and never the line that only names the log file.
		if strings.ContainsAny(line, "\r\n") || len(line) > 310 || strings.HasPrefix(line, "Saving debug log") {
			t.Fatalf("%s: line %q", name, line)
		}
	}

	// A limit is a limit also when certbot goes on to say that a validation
	// failed because of it.
	if kind := certbotFailureKind("urn:ietf:params:acme:error:rateLimited :: too many failed authorizations recently\nSome challenges have failed."); kind != transport.CertificateFailureRateLimited {
		t.Fatalf("kind = %q", kind)
	}
	// A password-like assignment in the line is blanked.
	if line := certbotFailureLine("An unexpected error occurred: eab_hmac_secret=AbCdEf123456 was refused"); strings.Contains(line, "AbCdEf123456") {
		t.Fatalf("line = %q", line)
	}
}
