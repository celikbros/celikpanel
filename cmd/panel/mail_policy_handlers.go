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

// Written, not loaded: what state the configuration is in, who acts, the
// action and how work resumes (D-024). Nothing is rolled back: the values in
// main.cf are the ones the owner asked for, and Postfix takes them up at its
// next successful reload. One sentence per stage the Agent verified
// (10 Oct 2026), and the recovery command is Postfix's own `postfix reload`:
// it prints what Postfix objects to and is the same on every platform, while
// `systemctl reload postfix` on Ubuntu reloads a wrapper unit and reports
// success whatever happened.
// Yazıldı, yüklenmedi: yapılandırmanın hangi durumda olduğu, kimin işlem
// yapacağı, eylem ve işin nasıl süreceği. Agent'ın doğruladığı her adım için
// bir cümle; kurtarma komutu Postfix'in kendi `postfix reload` komutudur.
const mailPolicySavedValuesShown = " Reload this page afterwards; the saved values are the ones shown."

var mailPolicyNotReloadedMessages = map[string]string{
	transport.MailPolicyStageCheck: "The mail policy was saved to /etc/postfix/main.cf, but Postfix was not reloaded: Postfix's own check refuses its configuration as it is now. A running Postfix keeps the settings it had before, so the saved values are not in effect. " +
		"What Postfix said is shown with this message; it can be about a line this page did not write. Nothing was rolled back. " +
		"The server owner corrects that line, runs sudo postfix check until it prints no error, then runs sudo postfix reload." + mailPolicySavedValuesShown,
	transport.MailPolicyStageReload: "The mail policy was saved to /etc/postfix/main.cf and Postfix's own check accepts the file, but the reload failed, so Postfix has not taken the saved values. " +
		"What the reload said is shown with this message. Nothing was rolled back. " +
		"The server owner runs sudo postfix reload on the server and reads what it prints." + mailPolicySavedValuesShown,
	transport.MailPolicyStageVerify: "The mail policy was saved to /etc/postfix/main.cf, but Postfix was no longer running after the reload, so it has not taken the saved values and is not handling mail. " +
		"Nothing was rolled back. The server owner runs sudo postfix check, then starts Postfix (sudo systemctl start postfix) and confirms it with sudo postfix status." + mailPolicySavedValuesShown,
}

// The stage is not one this Panel knows: say only what is common to all.
const mailPolicyNotReloadedMessage = "The mail policy was saved to /etc/postfix/main.cf, but Postfix did not take the saved values. " +
	"Nothing was rolled back. The server owner runs sudo postfix check to see what Postfix objects to, corrects it, and then runs sudo postfix reload." + mailPolicySavedValuesShown

// Written, outcome unknown: not a verified failure and never a success.
// Yazıldı, sonuç bilinmiyor: doğrulanmış hata değildir, başarı hiç değildir.
const mailPolicyReloadUnknownMessage = "The mail policy was saved to /etc/postfix/main.cf, but CelikPanel could not establish whether Postfix took the saved values: " +
	"a command that checks or reloads Postfix could not be run, or did not answer in time. This is not a verified failure, and Postfix may already be running with them. " +
	"Nothing was rolled back. The server owner runs sudo postfix status and then sudo postfix reload on the server." + mailPolicySavedValuesShown

// mailPolicyWrittenBody is the answer after main.cf changed and Postfix was not
// seen to take the change. It carries the policy that is written now, with its
// version, so the screen shows the saved values and the next save is built
// from them without a second read (10 Oct 2026; before this the body carried
// no policy although the sentence says "the saved values are the ones shown").
// mailPolicyWrittenBody, main.cf değiştikten ve Postfix'in değişikliği aldığı
// görülmedikten sonraki yanıttır; şimdi yazılı olan politikayı sürümüyle taşır.
type mailPolicyWrittenBody struct {
	apiErrorBody
	Policy *transport.MailPolicy `json:"policy,omitempty"`
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
	case transport.MailPolicyNotReloaded, transport.MailPolicyReloadUnknown:
		// The answers here that follow a change: main.cf holds the new
		// values, and Postfix was not seen to take them.
		// Buradaki, bir değişikliği izleyen yanıtlar.
		code, message, stage := errCodeMailPolicyNotReloaded, mailPolicyNotReloadedMessage, ""
		if resp.Code == transport.MailPolicyReloadUnknown {
			code, message = errCodeMailPolicyReloadUnknown, mailPolicyReloadUnknownMessage
		} else if sentence, known := mailPolicyNotReloadedMessages[resp.Stage]; known {
			message, stage = sentence, resp.Stage
		}
		log.Printf("[502][mail policy] written, %s %s: %s", code, stage, boundedAgentDiagnostic(resp.Reason))
		var vars map[string]string
		if detail := boundedAgentDiagnostic(resp.Reason); detail != "" {
			vars = map[string]string{"detail": detail}
		}
		// mutation_applied is the proof that this request changed main.cf. A
		// save that wrote nothing (the values were already there, and only
		// the reload was asked for) does not carry it (10 Oct 2026).
		// mutation_applied, bu isteğin main.cf'i değiştirdiğinin kanıtıdır;
		// hiçbir şey yazmayan kayıt onu taşımaz.
		body := mailPolicyWrittenBody{apiErrorBody: apiErrorBody{
			Error: message, Code: code, Reason: stage,
			PartialSuccess: !resp.Unwritten, MutationApplied: !resp.Unwritten, Vars: vars,
		}}
		// A policy without a version is one the Agent could not read back.
		// Sürümsüz politika, Agent'ın geri okuyamadığı politikadır.
		if resp.Policy.Version != "" {
			policy := resp.Policy
			if policy.DNSBLZones == nil {
				policy.DNSBLZones = []string{}
			}
			body.Policy = &policy
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(body)
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
			switch {
			case resp.Unwritten:
				// Nothing in main.cf changed, so the ledger records no change.
				// main.cf'te hiçbir şey değişmedi; defter değişiklik yazmaz.
			case resp.Code == transport.MailPolicyNotReloaded:
				// main.cf was changed; the ledger says so.
				// main.cf değişti; defter bunu söyler.
				p.audit(r, "mail.policy.written-not-reloaded", "", 0)
			case resp.Code == transport.MailPolicyReloadUnknown:
				p.audit(r, "mail.policy.written-reload-unknown", "", 0)
			}
			writeMailPolicyAgentAnswer(w, resp)
			return
		}
		if resp.Policy.DNSBLZones == nil {
			resp.Policy.DNSBLZones = []string{}
		}
		p.audit(r, "mail.policy", "", 0)
		// `applied` says what the save came to: reloaded, not_running (Postfix
		// is stopped and was left stopped), unchanged (nothing to write and
		// Postfix is stopped) or unchanged_reloaded (nothing to write; the
		// running Postfix was reloaded and verified). Empty from an Agent that
		// does not say.
		// `applied`, kaydın neyle sonuçlandığını söyler.
		answer := map[string]any{"success": true, "policy": resp.Policy}
		switch resp.Applied {
		case transport.MailPolicyAppliedReloaded, transport.MailPolicyAppliedNotRunning,
			transport.MailPolicyAppliedUnchanged, transport.MailPolicyAppliedUnchangedReloaded:
			answer["applied"] = resp.Applied
		}
		json.NewEncoder(w).Encode(answer)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
