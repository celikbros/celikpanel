package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/alicelik/celikpanel/internal/transport"
)

// The answer for each reason the Panel will not rewrite the owner's
// smtpd_recipient_restrictions: what was found, that nothing changed, who
// acts, the action and how work resumes (D-024). The Agent's own line and the
// restrictions themselves are never shown.
// Panel'in sahibin smtpd_recipient_restrictions değerini yeniden yazmadığı her
// gerekçenin yanıtı: ne bulundu, hiçbir şeyin değişmediği, kim işlem yapar,
// eylem ve işin nasıl sürdüğü.
const mailPolicyRestrictionsOwnerAction = " The server owner changes the reject_rbl_client entries of smtpd_recipient_restrictions " +
	"in /etc/postfix/main.cf and runs sudo systemctl reload postfix. Then reload this page. " +
	"Message size and the outgoing rate limit can still be saved here."

var mailPolicyRestrictionsMessages = map[string]string{
	transport.MailPolicyLockVariable: "The DNSBL setting was not changed: the recipient restrictions on this server refer to another Postfix setting ($name), " +
		"so CelikPanel cannot tell which checks they contain and will not rewrite them." + mailPolicyRestrictionsOwnerAction,
	transport.MailPolicyLockMalformed: "The DNSBL setting was not changed: CelikPanel could not read the recipient restrictions on this server with certainty " +
		"(an unclosed brace, or reject_rbl_client without a zone) and will not rewrite them." + mailPolicyRestrictionsOwnerAction,
	transport.MailPolicyLockNoBaseline: "The DNSBL setting was not changed: the recipient restrictions on this server were written by hand without both " +
		"permit_mynetworks and permit_sasl_authenticated, so a DNSBL check placed by CelikPanel could reject this server's own users." + mailPolicyRestrictionsOwnerAction,
	transport.MailPolicyLockTerminal: "The DNSBL setting was not changed: the recipient restrictions on this server end with permit, reject or defer, " +
		"so a DNSBL check added after them would never run, and where it belongs is the owner's decision." + mailPolicyRestrictionsOwnerAction,
}

const mailPolicyRestrictionsGenericMessage = "The DNSBL setting was not changed: CelikPanel will not rewrite the recipient restrictions found on this server." +
	mailPolicyRestrictionsOwnerAction

var mailPolicyInvalidMessages = map[string]string{
	transport.MailPolicyInvalidSize: "Nothing was changed: the maximum message size must be between 1 and 200 MB.",
	transport.MailPolicyInvalidZone: "Nothing was changed: a DNSBL zone must be a plain host name such as zen.spamhaus.org, with zones separated by commas.",
	transport.MailPolicyInvalidRate: "Nothing was changed: the outgoing rate limit must be between 0 and 10000 messages per minute.",
}

// writeMailPolicyAgentAnswer turns the Agent's classified refusal into the
// typed HTTP answer. The one unclassified answer still shown is the fixed
// "postfix is not installed"; every other text stays in the Panel log.
// writeMailPolicyAgentAnswer, Agent'ın sınıflandırılmış reddini tipli HTTP
// yanıtına çevirir.
func writeMailPolicyAgentAnswer(w http.ResponseWriter, resp transport.MailPolicyResponse) {
	switch resp.Code {
	case transport.MailPolicyUnreadable:
		writeCurrentSettingsUnreadable(w, settingsResourceMailPolicy)
	case transport.MailPolicyVersionRequired:
		writeSettingsVersionRequired(w, settingsResourceMailPolicy)
	case transport.MailPolicyChanged:
		writeSettingsChanged(w, settingsResourceMailPolicy)
	case transport.MailPolicyRestrictionsUnmanaged:
		message, known := mailPolicyRestrictionsMessages[resp.Reason]
		reason := resp.Reason
		if !known {
			message, reason = mailPolicyRestrictionsGenericMessage, ""
		}
		writeSettingsRefusalWithReason(w, http.StatusConflict, errCodeMailPolicyRestrictionsUnmanaged, message, reason)
	case transport.MailPolicyInvalid:
		message, known := mailPolicyInvalidMessages[resp.Reason]
		reason := resp.Reason
		if !known {
			message, reason = "Nothing was changed: a mail policy value is outside what CelikPanel sets.", ""
		}
		writeSettingsRefusalWithReason(w, http.StatusBadRequest, errCodeMailPolicyInvalid, message, reason)
	default:
		if resp.Code == "" && resp.Error == "postfix is not installed" {
			writeClientError(w, http.StatusConflict, resp.Error)
			return
		}
		writeAgentError(w, errors.New("mail policy refused by the agent"), resp.Code+": "+resp.Error)
	}
}

func writeSettingsRefusalWithReason(w http.ResponseWriter, status int, code, message, reason string) {
	log.Printf("[%d][mail policy] refused: %s %s", status, code, reason)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(apiErrorBody{Error: message, Code: code, Reason: reason})
}

// Admin-only server mail policy: message size, inbound DNSBL protection and
// the outgoing rate limit. A read returns the version of the Postfix values it
// saw; a write must carry it back (8 Oct 2026).
// Yalnız yönetici sunucu posta politikası. Okuma, gördüğü Postfix değerlerinin
// sürümünü döndürür; yazma onu geri taşımak zorundadır.
func (p *Panel) handleMailPolicy(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if c := currentCaller(r); c == nil || c.Role != roleAdmin {
		writeClientError(w, http.StatusForbidden, "admin only")
		return
	}
	switch r.Method {
	case http.MethodGet:
		var resp transport.MailPolicyResponse
		if err := p.callAgent("Agent.GetMailPolicy", &transport.Empty{}, &resp); err != nil {
			writeAgentError(w, err, "mail policy")
			return
		}
		if resp.Error != "" || resp.Code != "" {
			writeMailPolicyAgentAnswer(w, resp)
			return
		}
		if resp.Policy.DNSBLZones == nil {
			resp.Policy.DNSBLZones = []string{}
		}
		json.NewEncoder(w).Encode(resp.Policy)

	case http.MethodPut:
		var req transport.MailPolicy
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeClientError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		// A page that never loaded the current policy cannot save one. The
		// Agent refuses it too.
		// Geçerli politikayı hiç yüklememiş bir sayfa kaydedemez.
		if req.Version == "" {
			writeSettingsVersionRequired(w, settingsResourceMailPolicy)
			return
		}
		req.DNSBLLocked = ""
		var resp transport.MailPolicyResponse
		if err := p.callAgent("Agent.SetMailPolicy", &req, &resp); err != nil {
			writeAgentError(w, err, "mail policy")
			return
		}
		if resp.Error != "" || resp.Code != "" {
			writeMailPolicyAgentAnswer(w, resp)
			return
		}
		if resp.Policy.DNSBLZones == nil {
			resp.Policy.DNSBLZones = []string{}
		}
		p.audit(r, "mail.policy", "", 0)
		json.NewEncoder(w).Encode(map[string]any{"success": true, "policy": resp.Policy})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
