package main

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/alicelik/celikpanel/internal/transport"
)

// The answer to a certificate request that certbot ran and did not fulfil
// (11 Oct 2026; D-024).
//
// It used to be `500 INTERNAL` "internal server error" for every such run,
// with the cause only in the server's log. The Agent now says which of five
// kinds the run was (transport.CertificateFailure*), read from certbot's own
// output, and the answer is `502 CERTIFICATE_ISSUE_FAILED` with that kind as
// `reason`. Each sentence says only what that kind establishes, what is in
// place now, who acts and that nothing asks again by itself.
//
// `vars`: `domain`; `detail`, one bounded line of certbot's own words, for an
// administrator only (it can name paths and hosts of this server).
//
// What stays in place is the same for every kind: the request added no
// certificate, and a certificate the site already had keeps serving. The
// handler removes what certbot left of the unfinished request before it
// answers.
//
// certbot'un çalışıp yerine getirmediği bir sertifika isteğinin yanıtı. Eskiden
// her durumda `500` "internal server error" idi. Yanıt artık nedenin türünü,
// şu an neyin yerinde olduğunu, kimin ne yapacağını ve hiçbir şeyin
// kendiliğinden yeniden denemediğini söyler.
const errCodeCertificateIssueFailed = "CERTIFICATE_ISSUE_FAILED"

const (
	certificateIssueKept   = " The certificate this site already had is still in place and keeps serving."
	certificateIssueNone   = " The site has no certificate from this request and is served as before."
	certificateIssueResume = " Nothing asks again automatically."
)

var certificateIssueFailedMessages = map[string]string{
	transport.CertificateFailureAuthorityUnreachable: "No certificate was issued: this server could not reach the certificate authority, so no request was placed with it. " +
		"The server owner checks that this server can open HTTPS connections to the internet (DNS resolution, outbound port 443, the system clock), then requests the certificate here again.",
	transport.CertificateFailureValidation: "No certificate was issued: the certificate authority could not validate one of the names. " +
		"Each name of the site must resolve publicly to this server and answer on port 80 from the internet. " +
		"The domain's owner corrects the DNS records (or the firewall in front of this server), waits until public DNS shows them, then requests the certificate here again.",
	transport.CertificateFailureRateLimited: "No certificate was issued: the certificate authority refused the request because one of its limits was reached. " +
		"A new request before the limit resets is refused the same way and counts against it. " +
		"The line from certbot names the limit and, when the authority says so, when it resets; request the certificate here again after that time.",
	transport.CertificateFailureTimeout: "No certificate was issued: certbot did not finish within the time allowed and was stopped. " +
		"Why it took that long is not known from this answer. " +
		"The server owner reads /var/log/celikpanel/certbot/letsencrypt.log on the server, then requests the certificate here again.",
	transport.CertificateFailureTool: "No certificate was issued: certbot ended with an error. " +
		"Which step failed is not classified here; the line from certbot says what it reported. " +
		"The server owner reads /var/log/celikpanel/certbot/letsencrypt.log on the server, corrects what it names, then requests the certificate here again.",
}

// writeCertificateIssueFailure answers a certbot run the Agent classified. It
// reports whether it wrote the answer; anything the Agent did not classify
// (an older Agent, a failure before certbot ran) is left to the caller.
func writeCertificateIssueFailure(w http.ResponseWriter, caller *Caller, domain string, hadCertificate bool, reply *transport.IssueLetsEncryptResponse) bool {
	if reply == nil || reply.Success {
		return false
	}
	sentence, known := certificateIssueFailedMessages[reply.Failure]
	if !known {
		return false
	}
	if hadCertificate {
		sentence += certificateIssueKept
	} else {
		sentence += certificateIssueNone
	}
	sentence += certificateIssueResume
	vars := map[string]string{"domain": domain, "kept": "none"}
	if hadCertificate {
		vars["kept"] = "previous"
	}
	if caller != nil && caller.Role == roleAdmin {
		if detail := boundedAgentDiagnostic(reply.FailureDetail); detail != "" {
			vars["detail"] = detail
		}
	}
	log.Printf("[502][certificate] %s: %s %s: %s", domain, errCodeCertificateIssueFailed, reply.Failure, boundedAgentDiagnostic(reply.Error))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadGateway)
	_ = json.NewEncoder(w).Encode(apiErrorBody{Error: sentence, Code: errCodeCertificateIssueFailed, Reason: reply.Failure, Vars: vars})
	return true
}
