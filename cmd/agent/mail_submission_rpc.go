package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Authenticated mail submission — the half of the mail stack that lets
// customers actually SEND. Dovecot exposes its auth as a socket inside the
// Postfix chroot; Postfix opens submission (587, STARTTLS required) and
// smtps (465, implicit TLS), both restricted to authenticated users, with
// the SASL login forced to equal the envelope sender so one mailbox cannot
// spoof another. Port 25 stays exactly as it is: receiving from the world.
//
// Kimlik doğrulamalı gönderim — posta yığınının müşterinin gerçekten
// GÖNDERMESİNİ sağlayan yarısı. Dovecot, kimlik doğrulamasını Postfix
// chroot'u içinde bir soket olarak açar; Postfix, submission (587, STARTTLS
// zorunlu) ve smtps (465, örtük TLS) açar; ikisi de kimlikli kullanıcıyla
// sınırlıdır ve SASL girişi zarf göndericisine eşit olmaya zorlanır — bir
// posta kutusu diğerinin adına gönderemez. 25 portu olduğu gibi kalır:
// dünyadan alma.

const (
	dovecotSubmissionConf = "/etc/dovecot/conf.d/97-celikpanel-submission.conf"
	postfixLoginMapPath   = "/etc/postfix/celikpanel_login_map"
)

type ConfigureMailSubmissionResponse struct {
	Configured bool   `json:"configured"`
	Detail     string `json:"detail,omitempty"`
	Error      string `json:"error,omitempty"`
}

func (a *Agent) ConfigureMailSubmission(_ *struct{}, resp *ConfigureMailSubmissionResponse) error {
	if os.Getenv("CELIKPANEL_MAIL_DIR") != "" {
		resp.Error = "mail submission is a production action; not available with CELIKPANEL_MAIL_DIR set"
		return nil
	}
	if _, err := exec.LookPath("postconf"); err != nil {
		resp.Error = "postfix is not installed"
		return nil
	}

	// Dovecot: auth socket reachable from Postfix's chroot.
	// Dovecot: Postfix chroot'undan erişilebilir kimlik soketi.
	dovecotConf := `# Managed by CelikPanel — SASL for Postfix submission. Do not edit by hand.
service auth {
  unix_listener /var/spool/postfix/private/auth {
    mode = 0660
    user = postfix
    group = postfix
  }
}
`
	if err := os.WriteFile(dovecotSubmissionConf, []byte(dovecotConf), 0o644); err != nil {
		resp.Error = err.Error()
		return nil
	}

	// Sender spoofing guard: the SASL login must equal the MAIL FROM address.
	// Deliberate v1 simplification: aliases cannot send-as yet.
	// Gönderici sahteciliği koruması: SASL girişi MAIL FROM adresine eşit
	// olmalı. Bilinçli v1 sadeleştirmesi: takma adlar henüz adına gönderemez.
	loginMap := "/^(.+)$/ ${1}\n"
	if err := os.WriteFile(postfixLoginMapPath, []byte(loginMap), 0o644); err != nil {
		resp.Error = err.Error()
		return nil
	}

	// Master.cf via postconf -M/-P: idempotent, no hand-editing.
	// Master.cf, postconf -M/-P ile: idempotent, elle düzenleme yok.
	if out, err := exec.Command("postconf", "-M",
		"submission/inet=submission inet n - y - - smtpd").CombinedOutput(); err != nil {
		resp.Error = fmt.Sprintf("postconf -M submission: %s", strings.TrimSpace(string(out)))
		return nil
	}
	if out, err := exec.Command("postconf", "-M",
		"smtps/inet=smtps inet n - y - - smtpd").CombinedOutput(); err != nil {
		resp.Error = fmt.Sprintf("postconf -M smtps: %s", strings.TrimSpace(string(out)))
		return nil
	}

	shared := [][2]string{
		{"smtpd_sasl_auth_enable", "yes"},
		{"smtpd_sasl_type", "dovecot"},
		{"smtpd_sasl_path", "private/auth"},
		{"smtpd_client_restrictions", "permit_sasl_authenticated,reject"},
		{"smtpd_recipient_restrictions", "reject_sender_login_mismatch,permit_sasl_authenticated,reject"},
		{"smtpd_sender_login_maps", "regexp:" + postfixLoginMapPath},
		{"cleanup_service_name", "cleanup"},
	}
	apply := func(service string, extra [][2]string) error {
		for _, kv := range append(append([][2]string{}, shared...), extra...) {
			arg := fmt.Sprintf("%s/inet/%s=%s", service, kv[0], kv[1])
			if out, err := exec.Command("postconf", "-P", arg).CombinedOutput(); err != nil {
				return fmt.Errorf("postconf -P %s: %s", arg, strings.TrimSpace(string(out)))
			}
		}
		return nil
	}
	// 587: STARTTLS required before auth. 465: TLS from the first byte.
	// 587: kimlikten önce STARTTLS zorunlu. 465: ilk bayttan TLS.
	if err := apply("submission", [][2]string{
		{"syslog_name", "postfix/submission"},
		{"smtpd_tls_security_level", "encrypt"},
	}); err != nil {
		resp.Error = err.Error()
		return nil
	}
	if err := apply("smtps", [][2]string{
		{"syslog_name", "postfix/smtps"},
		{"smtpd_tls_wrappermode", "yes"},
	}); err != nil {
		resp.Error = err.Error()
		return nil
	}

	_ = exec.Command("systemctl", "restart", "dovecot").Run()
	if out, err := exec.Command("systemctl", "restart", "postfix").CombinedOutput(); err != nil {
		resp.Error = fmt.Sprintf("postfix restart: %s", firstLine(string(out)))
		return nil
	}

	resp.Configured = true
	resp.Detail = "submission on 587 (STARTTLS) and 465 (TLS), SASL via Dovecot, sender spoofing blocked"
	return nil
}
