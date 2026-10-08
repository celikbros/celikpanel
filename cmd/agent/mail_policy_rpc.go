package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"github.com/alicelik/celikpanel/internal/hostcmd"
	"github.com/alicelik/celikpanel/internal/transport"
)

// Server-wide mail policy — the Plesk "server-wide mail settings" core:
// message size limit, DNSBL protection for incoming mail and an outgoing rate
// limit. These are the server owner's Postfix values, so (8 Oct 2026; D-022,
// D-024, D-025 invariants 1 and 2):
//
//   - a read that fails is an error, never zeros. Before this a failed
//     `postconf` read answered "0 MB, no DNSBL, no limit" with success, the
//     form filled itself with defaults and one Save wrote them to main.cf;
//   - a read returns a version of the exact values it saw, and a write must
//     carry it back. A write without it, or after the values changed on the
//     server, changes nothing;
//   - a write touches only the values that differ from the server's, in one
//     `postconf -e`, and never rebuilds the recipient restrictions (see
//     mail_policy_restrictions.go).
//
// Sunucu geneli posta politikası: mesaj boyutu sınırı, gelen posta için DNSBL
// koruması ve giden hız sınırı. Bunlar sunucu sahibinin Postfix değerleridir:
// başarısız okuma sıfır değil hatadır; okuma gördüğü değerlerin sürümünü
// döndürür ve yazma onu geri taşır; yazma yalnız sunucudakinden farklı olan
// değerlere dokunur ve alıcı kısıtlarını baştan kurmaz.

type MailPolicy = transport.MailPolicy

type MailPolicyResponse = transport.MailPolicyResponse

const (
	mailPolicyMebibyte     = 1024 * 1024
	mailPolicyMaxSizeMB    = 200
	mailPolicyMaxRate      = 10000
	mailPolicyRateUnit     = "60s"
	mailPolicyNotInstalled = "postfix is not installed"
)

// The Postfix values the policy is read from, in the order the version is
// computed over.
// Politikanın okunduğu Postfix değerleri; sürüm bu sırayla hesaplanır.
var mailPolicyParameters = []string{
	"message_size_limit",
	"smtpd_recipient_restrictions",
	"smtpd_client_message_rate_limit",
	"anvil_rate_time_unit",
}

// Swapped by tests. Production reads and writes through postconf and makes the
// running Postfix take the change through mail_service_verify.go: its own
// check, its own reload command, and a master that is still running afterwards.
// A stopped Postfix is left stopped. `systemctl reload-or-restart postfix` was
// the reload until 10 Oct 2026; on Ubuntu that unit is a wrapper whose job
// succeeds whatever happened to the daemon.
// Testlerde değiştirilir. Üretimde çalışan Postfix değişikliği kendi denetimi,
// kendi yeniden yükleme komutu ve sonrasında hâlâ çalışan ana süreçle alır.
var (
	mailPolicyLookPath = exec.LookPath
	mailPolicyPostconf = func(args ...string) ([]byte, error) {
		return exec.Command("postconf", args...).Output()
	}
	mailPolicyReload = func() (string, error) {
		return applyPostfixVerified(runMailTLSCommand, mailServiceReload)
	}
)

// mailPolicyMu makes read, compare and write one step for the Panel's own
// requests. An owner editing main.cf by hand at the same instant is outside it.
// mailPolicyMu, Panel'in kendi istekleri için oku-karşılaştır-yaz adımını tek
// adım yapar.
var mailPolicyMu sync.Mutex

var errMailPolicyUnreadable = errors.New("the current Postfix mail policy could not be read")

type mailPolicyNative struct {
	values    map[string]string
	sizeBytes int
	rate      int
	version   string
}

// readMailPolicyNative reads every value or none. A value is known only when
// postconf succeeded and printed a line for it; the two numbers must also be
// numbers. Anything else is an unknown state, logged here and reported as one
// fixed error.
// readMailPolicyNative ya bütün değerleri okur ya hiçbirini. Değer, yalnız
// postconf başarılı olup onun için bir satır yazdıysa bilinir.
func readMailPolicyNative() (mailPolicyNative, error) {
	native := mailPolicyNative{values: map[string]string{}}
	digest := sha256.New()
	for _, name := range mailPolicyParameters {
		out, err := mailPolicyPostconf("-h", name)
		if err != nil {
			log.Printf("mail policy: postconf -h %s failed: %s", name, hostcmd.Diagnostic(out, err))
			return mailPolicyNative{}, errMailPolicyUnreadable
		}
		if !strings.HasSuffix(string(out), "\n") {
			log.Printf("mail policy: postconf -h %s printed no value line", name)
			return mailPolicyNative{}, errMailPolicyUnreadable
		}
		value := strings.TrimSpace(string(out))
		native.values[name] = value
		digest.Write([]byte(name))
		digest.Write([]byte{0})
		digest.Write([]byte(value))
		digest.Write([]byte{0})
	}
	var err error
	if native.sizeBytes, err = strconv.Atoi(native.values["message_size_limit"]); err != nil || native.sizeBytes < 0 {
		log.Printf("mail policy: message_size_limit is not a number")
		return mailPolicyNative{}, errMailPolicyUnreadable
	}
	if native.rate, err = strconv.Atoi(native.values["smtpd_client_message_rate_limit"]); err != nil || native.rate < 0 {
		log.Printf("mail policy: smtpd_client_message_rate_limit is not a number")
		return mailPolicyNative{}, errMailPolicyUnreadable
	}
	native.version = "mp1-" + hex.EncodeToString(digest.Sum(nil))
	return native, nil
}

func (n mailPolicyNative) policy() MailPolicy {
	restrictions := parseRecipientRestrictions(n.values["smtpd_recipient_restrictions"])
	zones := restrictions.zones
	if zones == nil {
		zones = []string{}
	}
	return MailPolicy{
		MessageSizeMB:     n.sizeBytes / mailPolicyMebibyte,
		DNSBLZones:        zones,
		OutboundRateLimit: n.rate,
		Version:           n.version,
		DNSBLLocked:       restrictions.lock,
	}
}

func refuseMailPolicy(resp *MailPolicyResponse, code, reason, text string) error {
	*resp = MailPolicyResponse{Error: text, Code: code, Reason: reason}
	return nil
}

func (a *Agent) GetMailPolicy(_ *transport.Empty, resp *MailPolicyResponse) error {
	if _, err := mailPolicyLookPath("postconf"); err != nil {
		resp.Error = mailPolicyNotInstalled
		return nil
	}
	native, err := readMailPolicyNative()
	if err != nil {
		return refuseMailPolicy(resp, transport.MailPolicyUnreadable, "", err.Error())
	}
	resp.Policy = native.policy()
	return nil
}

func (a *Agent) SetMailPolicy(req *MailPolicy, resp *MailPolicyResponse) error {
	if _, err := mailPolicyLookPath("postconf"); err != nil {
		resp.Error = mailPolicyNotInstalled
		return nil
	}
	mailPolicyMu.Lock()
	defer mailPolicyMu.Unlock()

	// The pre-image comes first: a write is never built from a failed read.
	// Önce ön görüntü: başarısız okumadan asla yazı kurulmaz.
	native, err := readMailPolicyNative()
	if err != nil {
		return refuseMailPolicy(resp, transport.MailPolicyUnreadable, "", err.Error())
	}
	if req.Version == "" {
		return refuseMailPolicy(resp, transport.MailPolicyVersionRequired, "",
			"the mail policy request carried no version of the settings it was built from")
	}
	if req.Version != native.version {
		return refuseMailPolicy(resp, transport.MailPolicyChanged, "",
			"the Postfix mail policy is not the one the request was built from")
	}

	// Only what differs from the server is written, so a value the form
	// cannot express exactly (a limit that is not a whole number of MiB, a
	// larger rate the owner set) survives a save that did not change it.
	// Yalnız sunucudakinden farklı olan yazılır.
	var assignments []string
	if req.MessageSizeMB != native.sizeBytes/mailPolicyMebibyte {
		if req.MessageSizeMB < 1 || req.MessageSizeMB > mailPolicyMaxSizeMB {
			return refuseMailPolicy(resp, transport.MailPolicyInvalid, transport.MailPolicyInvalidSize,
				"message_size_mb must be between 1 and 200")
		}
		assignments = append(assignments, "message_size_limit="+strconv.Itoa(req.MessageSizeMB*mailPolicyMebibyte))
	}

	var zones []string
	for _, zone := range req.DNSBLZones {
		zone = strings.ToLower(strings.TrimSpace(zone))
		if zone == "" {
			continue
		}
		if !validDNSBLZone(zone) {
			return refuseMailPolicy(resp, transport.MailPolicyInvalid, transport.MailPolicyInvalidZone,
				"a DNSBL zone is not a plain host name")
		}
		zones = append(zones, zone)
	}
	restrictions, changed, lock := planRecipientRestrictions(native.values["smtpd_recipient_restrictions"], zones)
	if lock != "" {
		return refuseMailPolicy(resp, transport.MailPolicyRestrictionsUnmanaged, lock,
			"the recipient restrictions on this server are not rewritten by the panel")
	}
	if changed {
		assignments = append(assignments, "smtpd_recipient_restrictions="+restrictions)
	}

	// Outbound rate: 0 leaves Postfix without a limit; a positive value caps
	// messages per minute per sending client, so the window is set with it.
	// Giden hız: 0 sınırsız bırakır; pozitif değer gönderen istemci başına
	// dakikadaki mesajı sınırlar, bu yüzden pencere onunla birlikte ayarlanır.
	if req.OutboundRateLimit != native.rate {
		if req.OutboundRateLimit < 0 || req.OutboundRateLimit > mailPolicyMaxRate {
			return refuseMailPolicy(resp, transport.MailPolicyInvalid, transport.MailPolicyInvalidRate,
				"outbound_rate_limit must be between 0 and 10000")
		}
		assignments = append(assignments, "smtpd_client_message_rate_limit="+strconv.Itoa(req.OutboundRateLimit))
		if req.OutboundRateLimit > 0 && native.values["anvil_rate_time_unit"] != mailPolicyRateUnit {
			assignments = append(assignments, "anvil_rate_time_unit="+mailPolicyRateUnit)
		}
	}

	if len(assignments) > 0 {
		// One postconf edits main.cf once, so the save is whole or absent.
		// Tek postconf main.cf'i bir kez düzenler; kayıt ya tamdır ya yoktur.
		if out, err := mailPolicyPostconf(append([]string{"-e"}, assignments...)...); err != nil {
			log.Printf("mail policy: postconf -e failed: %s", hostcmd.Diagnostic(out, err))
			return refuseMailPolicy(resp, transport.MailPolicyWriteFailed, "",
				"the Postfix mail policy could not be written")
		}
		applied, err := mailPolicyReload()
		if err != nil {
			// main.cf holds the new values and Postfix was not seen to take
			// them. That is a failure after a change, not something to log
			// and call saved (9 Oct 2026; D-024): the answer carries what is
			// written now, so the screen shows it. A refusal by Postfix's own
			// check, a reload that failed and a master that stopped are
			// verified failures; a command that could not be run or did not
			// answer leaves the outcome unknown, and is said as unknown.
			// main.cf yeni değerleri tutuyor ve Postfix'in onları aldığı
			// görülmedi. Bu, günlüğe yazıp "kaydedildi" denecek bir şey değil,
			// bir değişiklik sonrası hatadır; sonucu bilinmeyen durum da
			// bilinmeyen olarak söylenir.
			log.Printf("mail policy: postfix reload after a policy write failed: %v", err)
			answer := MailPolicyResponse{
				Error: "the mail policy was written to main.cf, but Postfix could not be reloaded",
				Code:  transport.MailPolicyNotReloaded,
			}
			var failure *mailServiceError
			if errors.As(err, &failure) {
				answer.Reason, answer.Stage = failure.detail, failure.stage
				if failure.unknown {
					answer.Code = transport.MailPolicyReloadUnknown
					answer.Error = "the mail policy was written to main.cf, but whether Postfix took it could not be established"
				}
			} else {
				answer.Code = transport.MailPolicyReloadUnknown
				answer.Error = "the mail policy was written to main.cf, but whether Postfix took it could not be established"
				answer.Reason = hostcmd.Bounded(strings.Join(strings.Fields(err.Error()), " "), 300)
			}
			if fresh, readErr := readMailPolicyNative(); readErr == nil {
				answer.Policy = fresh.policy()
			}
			*resp = answer
			return nil
		}
		resp.Applied = applied
	} else {
		resp.Applied = transport.MailPolicyAppliedUnchanged
	}

	// The answer is what the server holds now, with the version the next save
	// needs. If it cannot be read back the save still happened; the screen
	// reloads and reports the unreadable state itself.
	// Yanıt, sunucunun şimdi tuttuğu değerdir.
	if fresh, err := readMailPolicyNative(); err == nil {
		resp.Policy = fresh.policy()
	} else {
		resp.Policy = MailPolicy{
			MessageSizeMB: req.MessageSizeMB, DNSBLZones: zones, OutboundRateLimit: req.OutboundRateLimit,
		}
	}
	return nil
}
