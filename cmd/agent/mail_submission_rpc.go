package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/alicelik/celikpanel/internal/transport"
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

var (
	dovecotSubmissionConf = "/etc/dovecot/conf.d/97-celikpanel-submission.conf"
	postfixLoginMapPath   = "/etc/postfix/celikpanel_login_map"
)

type ConfigureMailSubmissionResponse = transport.ConfigureMailSubmissionResponse

func (a *Agent) ConfigureMailSubmission(req *ServiceMutationRequest, resp *ConfigureMailSubmissionResponse) error {
	if resp == nil {
		return fmt.Errorf("mail submission configuration response is required")
	}
	*resp = ConfigureMailSubmissionResponse{}
	if req == nil {
		return fmt.Errorf("mail submission configuration request is required")
	}
	ctx, finishStep, err := a.requiredServiceMutationStep(
		req.ServiceMutationBinding,
		newServiceMutationStepClaim(serviceMutationStepConfigureMailSubmission, "postfix", "", "configure"),
	)
	if err != nil {
		*resp = ConfigureMailSubmissionResponse{Error: err.Error()}
		return nil
	}
	defer finishStep()
	if err := lockMailMutation(ctx); err != nil {
		resp.Error = fmt.Sprintf(
			"mail submission configuration did not start: service mutation lease ended before mail submission configuration completed: %v",
			err,
		)
		return nil
	}
	defer mailMutex.Unlock()

	if os.Getenv("CELIKPANEL_MAIL_DIR") != "" {
		resp.Error = "mail submission is a production action; not available with CELIKPANEL_MAIL_DIR set"
		return nil
	}
	if _, err := exec.LookPath("postconf"); err != nil {
		resp.Error = "postfix is not installed"
		return nil
	}
	if _, err := exec.LookPath("doveconf"); err != nil {
		resp.Error = "dovecot is not installed"
		return nil
	}
	if err := mailSubmissionLeaseError(ctx); err != nil {
		resp.Error = err.Error()
		return nil
	}

	paths := []string{dovecotSubmissionConf, postfixLoginMapPath}
	snapshots := make([]mailFileSnapshot, 0, len(paths))
	for _, path := range paths {
		snapshot, err := snapshotMailFile(path)
		if err != nil {
			resp.Error = fmt.Sprintf("snapshot mail submission configuration: %v", err)
			return nil
		}
		snapshots = append(snapshots, snapshot)
	}
	filesMayHaveChanged := false
	postfixMayHaveChanged := false
	rollback := func(cause error, recoverDovecot bool) error {
		var rollbackErrs []error
		if filesMayHaveChanged {
			for index := len(snapshots) - 1; index >= 0; index-- {
				if err := restoreMailFile(snapshots[index]); err != nil {
					rollbackErrs = append(rollbackErrs, err)
				}
			}
		}

		recoverySkipped := false
		if recoverDovecot {
			if ctx.Err() != nil {
				recoverySkipped = true
			} else if out, err := runMailTLSMutationCommand(ctx, "systemctl", "restart", "dovecot"); err != nil {
				rollbackErrs = append(rollbackErrs, mailSubmissionCommandError(
					ctx,
					"restore dovecot after failed mail submission configuration",
					out,
					err,
				))
			}
		}

		if len(rollbackErrs) != 0 {
			return fmt.Errorf("%w; mail submission rollback failed: %v", cause, errors.Join(rollbackErrs...))
		}
		detail := "managed files restored"
		if !filesMayHaveChanged {
			detail = "no managed files changed"
		}
		if postfixMayHaveChanged {
			detail += "; Postfix master.cf changes may remain until the same operation is retried"
		}
		if recoverySkipped {
			detail += "; Dovecot recovery restart was skipped because the mutation lease ended"
		}
		return fmt.Errorf("%w; %s", cause, detail)
	}
	fail := func(err error) error {
		resp.Configured = false
		resp.Detail = ""
		resp.Error = err.Error()
		return nil
	}

	// Dovecot: auth socket reachable from Postfix's chroot.
	// Dovecot: Postfix chroot'undan erişilebilir kimlik soketi.
	// service auth / unix_listener syntax is identical in Dovecot 2.3 and 2.4;
	// still validated by a lease-bound doveconf command so a broken combination
	// with the other drop-ins surfaces here rather than as a dead service.
	// service auth / unix_listener sözdizimi Dovecot 2.3 ve 2.4'te aynıdır;
	// yine de lease'e bağlı doveconf ile doğrulanır ki diğer eklerle bozuk bir
	// bileşim ölü servis yerine burada yüzeye çıksın.
	dovecotConf := `# Managed by CelikPanel — SASL for Postfix submission. Do not edit by hand.
service auth {
  unix_listener /var/spool/postfix/private/auth {
    mode = 0660
    user = postfix
    group = postfix
  }
}
`
	if err := mailSubmissionLeaseError(ctx); err != nil {
		return fail(err)
	}
	filesMayHaveChanged = true
	if err := secureWriteConfig(dovecotSubmissionConf, []byte(dovecotConf), 0o644); err != nil {
		return fail(rollback(fmt.Errorf("write dovecot submission configuration: %w", err), false))
	}
	if out, err := runMailTLSMutationCommand(ctx, "doveconf", "-n"); err != nil {
		return fail(rollback(mailSubmissionCommandError(
			ctx,
			"dovecot rejected the mail submission configuration",
			out,
			err,
		), false))
	}

	// Sender spoofing guard: the SASL login must equal the MAIL FROM address.
	// Deliberate v1 simplification: aliases cannot send-as yet.
	// Gönderici sahteciliği koruması: SASL girişi MAIL FROM adresine eşit
	// olmalı. Bilinçli v1 sadeleştirmesi: takma adlar henüz adına gönderemez.
	loginMap := "/^(.+)$/ ${1}\n"
	if err := mailSubmissionLeaseError(ctx); err != nil {
		return fail(rollback(err, false))
	}
	if err := secureWriteConfig(postfixLoginMapPath, []byte(loginMap), 0o644); err != nil {
		return fail(rollback(fmt.Errorf("write postfix sender login map: %w", err), false))
	}

	// Master.cf via postconf -M/-P: idempotent, no hand-editing.
	// Master.cf, postconf -M/-P ile: idempotent, elle düzenleme yok.
	postfixMayHaveChanged = true
	if out, err := runMailTLSMutationCommand(ctx, "postconf", "-M",
		"submission/inet=submission inet n - y - - smtpd"); err != nil {
		return fail(rollback(mailSubmissionCommandError(ctx, "postconf -M submission", out, err), false))
	}
	if out, err := runMailTLSMutationCommand(ctx, "postconf", "-M",
		"smtps/inet=smtps inet n - y - - smtpd"); err != nil {
		return fail(rollback(mailSubmissionCommandError(ctx, "postconf -M smtps", out, err), false))
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
			if out, err := runMailTLSMutationCommand(ctx, "postconf", "-P", arg); err != nil {
				return mailSubmissionCommandError(ctx, "postconf -P "+arg, out, err)
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
		return fail(rollback(err, false))
	}
	if err := apply("smtps", [][2]string{
		{"syslog_name", "postfix/smtps"},
		{"smtpd_tls_wrappermode", "yes"},
	}); err != nil {
		return fail(rollback(err, false))
	}

	if out, err := runMailTLSMutationCommand(ctx, "systemctl", "restart", "dovecot"); err != nil {
		return fail(rollback(mailSubmissionCommandError(ctx, "dovecot restart", out, err), true))
	}
	if out, err := runMailTLSMutationCommand(ctx, "systemctl", "restart", "postfix"); err != nil {
		return fail(rollback(mailSubmissionCommandError(ctx, "postfix restart", out, err), true))
	}
	if err := mailSubmissionLeaseError(ctx); err != nil {
		return fail(rollback(err, true))
	}

	resp.Configured = true
	resp.Detail = "submission on 587 (STARTTLS) and 465 (TLS), SASL via Dovecot, sender spoofing blocked"
	return nil
}

func mailSubmissionLeaseError(ctx context.Context) error {
	if ctx == nil {
		return errors.New("service mutation lease context is required")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("service mutation lease ended before mail submission configuration completed: %w", err)
	}
	return nil
}

func mailSubmissionCommandError(ctx context.Context, operation string, out []byte, commandErr error) error {
	if err := mailSubmissionLeaseError(ctx); err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	detail := strings.TrimSpace(firstLine(string(out)))
	if detail == "" {
		detail = commandErr.Error()
	}
	return fmt.Errorf("%s: %s", operation, detail)
}
