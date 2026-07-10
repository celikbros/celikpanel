package main

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Server-wide mail policy — the Plesk "server-wide mail settings" core:
// message size limit and DNSBL protection for incoming mail. The recipient
// restrictions are written as one managed set (the standard safe baseline
// plus one reject_rbl_client per zone) rather than patched, so re-applying
// always converges and nothing manual can half-survive.
//
// Sunucu geneli posta politikası — Plesk'in "sunucu geneli posta ayarları"
// çekirdeği: mesaj boyutu sınırı ve gelen posta için DNSBL koruması. Alıcı
// kısıtları yamalanmak yerine tek yönetilen set olarak yazılır (standart
// güvenli taban artı zone başına bir reject_rbl_client); böylece yeniden
// uygulamak her zaman yakınsar.

type MailPolicy struct {
	MessageSizeMB int      `json:"message_size_mb"`
	DNSBLZones    []string `json:"dnsbl_zones"` // empty = protection off
	// OutboundRateLimit caps how many messages one sending client may inject
	// per minute (postfix smtpd_client_message_rate_limit). 0 = unlimited.
	// This is the guard against BEING a spam source: a compromised customer
	// account can no longer blast thousands of mails and get the server (and
	// its domains) suspended — it runs into the per-minute ceiling instead.
	// OutboundRateLimit, bir gönderen istemcinin dakikada kaç mesaj
	// enjekte edebileceğini sınırlar (postfix smtpd_client_message_rate_limit).
	// 0 = sınırsız. Bu, spam KAYNAĞI olmaya karşı korumadır: ele geçirilmiş
	// bir müşteri hesabı artık binlerce mail patlatıp sunucuyu (ve
	// domain'lerini) askıya aldıramaz — dakikalık tavana çarpar.
	OutboundRateLimit int `json:"outbound_rate_limit"`
}

type MailPolicyResponse struct {
	Policy MailPolicy `json:"policy"`
	Error  string     `json:"error,omitempty"`
}

func (a *Agent) GetMailPolicy(_ *struct{}, resp *MailPolicyResponse) error {
	if _, err := exec.LookPath("postconf"); err != nil {
		resp.Error = "postfix is not installed"
		return nil
	}
	if out, err := exec.Command("postconf", "-h", "message_size_limit").Output(); err == nil {
		if b, err := strconv.Atoi(strings.TrimSpace(string(out))); err == nil {
			resp.Policy.MessageSizeMB = b / (1024 * 1024)
		}
	}
	if out, err := exec.Command("postconf", "-h", "smtpd_recipient_restrictions").Output(); err == nil {
		for _, part := range strings.Split(string(out), ",") {
			part = strings.TrimSpace(part)
			if zone, ok := strings.CutPrefix(part, "reject_rbl_client "); ok {
				resp.Policy.DNSBLZones = append(resp.Policy.DNSBLZones, strings.TrimSpace(zone))
			}
		}
	}
	if out, err := exec.Command("postconf", "-h", "smtpd_client_message_rate_limit").Output(); err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(string(out))); err == nil {
			resp.Policy.OutboundRateLimit = n
		}
	}
	return nil
}

func (a *Agent) SetMailPolicy(req *MailPolicy, resp *MailPolicyResponse) error {
	if _, err := exec.LookPath("postconf"); err != nil {
		resp.Error = "postfix is not installed"
		return nil
	}
	if req.MessageSizeMB < 1 || req.MessageSizeMB > 200 {
		req.MessageSizeMB = 25
	}

	// Baseline: locals and authenticated users pass, open relay stays shut.
	// DNSBL rejections come after, so our own users are never DNSBL-blocked.
	// Taban: yereller ve kimlikli kullanıcılar geçer, açık aktarım kapalı
	// kalır. DNSBL retleri sonra gelir; kendi kullanıcımız asla takılmaz.
	restrictions := []string{"permit_mynetworks", "permit_sasl_authenticated", "reject_unauth_destination"}
	var zones []string
	for _, z := range req.DNSBLZones {
		z = strings.ToLower(strings.TrimSpace(z))
		if z == "" || strings.ContainsAny(z, " \t,;") || !strings.Contains(z, ".") {
			continue
		}
		zones = append(zones, z)
		restrictions = append(restrictions, "reject_rbl_client "+z)
	}

	// Outbound rate: 0 keeps postfix's own default (unlimited); a positive
	// value caps messages per minute per sending client. Clamped to a sane
	// range so a typo cannot set it to millions (useless) or negative.
	// Giden hız: 0, postfix'in kendi varsayılanını (sınırsız) korur; pozitif
	// değer, gönderen istemci başına dakikadaki mesajı sınırlar. Bir yazım
	// hatası milyonlara (işe yaramaz) ya da negatife çekemesin diye makul
	// aralığa sıkıştırılır.
	rate := req.OutboundRateLimit
	if rate < 0 {
		rate = 0
	}
	if rate > 10000 {
		rate = 10000
	}

	settings := [][2]string{
		{"message_size_limit", strconv.Itoa(req.MessageSizeMB * 1024 * 1024)},
		{"smtpd_recipient_restrictions", strings.Join(restrictions, ", ")},
		{"smtpd_client_message_rate_limit", strconv.Itoa(rate)},
		// Rate windows are counted per minute so the ceiling reads naturally.
		// Hız pencereleri dakika başına sayılır; tavan doğal okunur.
		{"anvil_rate_time_unit", "60s"},
	}
	for _, s := range settings {
		if out, err := exec.Command("postconf", "-e", s[0]+"="+s[1]).CombinedOutput(); err != nil {
			resp.Error = fmt.Sprintf("postconf %s: %s", s[0], strings.TrimSpace(string(out)))
			return nil
		}
	}
	_ = exec.Command("systemctl", "reload-or-restart", "postfix").Run()

	resp.Policy = MailPolicy{MessageSizeMB: req.MessageSizeMB, DNSBLZones: zones, OutboundRateLimit: rate}
	return nil
}
