package main

import (
	"context"
	"errors"
	"os"
)

// A renewal changes the selected certificate, not the accepted native mail
// configuration. Recovery of an already-selected version has the same limit.
// An initial fallback-to-host configuration transition still needs an explicit
// setup operation; a historical certificate receipt does not authorize it.
func reloadMailHostCertificateSelection(ctx context.Context, domain string) error {
	return observeOrReloadMailHostCertificate(ctx, domain, true)
}

func preflightMailHostCertificateReload(ctx context.Context, domain string) error {
	return observeOrReloadMailHostCertificate(ctx, domain, false)
}

func observeOrReloadMailHostCertificate(ctx context.Context, domain string, reload bool) error {
	if ctx == nil || os.Getenv("CELIKPANEL_MAIL_DIR") != "" {
		return errors.New("mail host reload requires a production operation context")
	}
	journal, err := loadMailHostCertificatePlan()
	if err != nil {
		return err
	}
	if journal.Myhostname != domain {
		return errors.New("accepted mail hostname changed before certificate reload")
	}
	cert, key, err := selectedMailHostCertificate(domain)
	if err != nil {
		return err
	}
	if cert != managedMailHostTLSDir+"/current/fullchain.pem" || key != managedMailHostTLSDir+"/current/privkey.pem" {
		return &mailHostReloadUnverified{cause: errors.New("native host certificate enrollment is incomplete")}
	}
	if _, _, err := validateSecureMailTLSRequest(&SecureMailTLSRequest{Myhostname: domain, SNI: journal.SNI}); err != nil {
		return &mailHostReloadUnverified{cause: err}
	}
	run := func(name string, args ...string) ([]byte, error) {
		return runMailHostCertificateCommand(ctx, name, args...)
	}
	// Resolve fixed commands and observe the supported dialect. No SNI map
	// probe/compilation and no configuration writer participates in renewal.
	commands, err := preflightMailTLSCommands(false, run)
	if err != nil {
		return &mailHostReloadUnverified{cause: err}
	}
	// What each service must be seen presenting afterwards: the certificate
	// that is selected now. Read from the selection, not from the request.
	var served mailServedCheck
	if reload {
		selectedDomain, selectedLeaf, identityErr := currentMailHostCertificateIdentity()
		if identityErr != nil {
			return &mailHostReloadUnverified{cause: identityErr}
		}
		if selectedDomain != domain {
			return &mailHostReloadUnverified{cause: errors.New("selected mail certificate belongs to another mail identity")}
		}
		served = func(ctx context.Context, unit string) error {
			return verifyMailServiceServes(ctx, unit, selectedLeaf)
		}
	}
	if err := observeOrReloadMailHostTLS(ctx, journal, cert, key, commands.run, secureReadConfig, reload, served); err != nil {
		return &mailHostReloadUnverified{cause: err}
	}
	return nil
}

// mailServedCheck asks one mail service's own listeners which certificate they
// present; nil means the selected one was seen.
type mailServedCheck func(ctx context.Context, unit string) error

type mailHostReloadUnverified struct{ cause error }

func (e *mailHostReloadUnverified) Error() string {
	// The one cause that has its own sentence: what the service's listeners
	// presented. It carries no native output.
	var served *mailServedCertificateError
	if errors.As(e.cause, &served) {
		return served.Error()
	}
	return "mail certificate activation paused: accepted native settings and running mail services could not be verified or reloaded; the server owner must review Postfix/Dovecot configuration and service status, then retry the same operation; settings and certificate evidence are preserved"
}
func (e *mailHostReloadUnverified) Unwrap() error { return e.cause }

// Observation runs before any reload, between reloads, and after both. Never
// start a stopped service, rewrite owner settings, regenerate fallback material,
// compile a customer SNI map, or call postfix check (which can create files).
//
// `systemctl reload` stays the only thing sent to a service: it is the unit's
// own reload, with whatever its owner added to it, and it is all the
// independent helper's closed command scope allows. Its exit status and
// `is-active` are not the outcome, because on Ubuntu both answer for a wrapper
// unit (mail_served_certificate.go). The outcome is what the service's own
// listeners present after its reload: served must report the selected
// certificate, or the activation is not complete.
//
// `systemctl reload` bir hizmete gönderilen tek şey olarak kalır. Çıkış durumu
// ve `is-active` sonuç değildir; Ubuntu'da ikisi de sarmalayıcı unit adına
// yanıt verir. Sonuç, yeniden yüklemeden sonra hizmetin kendi dinleyicilerinin
// sunduğu sertifikadır.
func observeOrReloadMailHostTLS(ctx context.Context, journal *mailTLSSyncJournal, cert, key string, run mailTLSCommandRunner, read func(string) ([]byte, error), reload bool, served mailServedCheck) error {
	observe := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := verifyMailTLSConfiguration(journal, cert, key, run, read); err != nil {
			return err
		}
		if err := validateDovecotTLSConfig(run); err != nil {
			return err
		}
		for _, unit := range []string{"postfix.service", "dovecot.service"} {
			if _, err := run("systemctl", "is-active", "--quiet", unit); err != nil {
				return err
			}
		}
		return ctx.Err()
	}
	if err := observe(); err != nil {
		return err
	}
	if !reload {
		return nil
	}
	if served == nil {
		return errors.New("mail certificate reload requires the served certificate check")
	}
	for _, unit := range []string{"postfix.service", "dovecot.service"} {
		if _, err := run("systemctl", "reload", unit); err != nil {
			return err
		}
		if err := observe(); err != nil {
			return err
		}
		if err := served(ctx, unit); err != nil {
			return err
		}
	}
	return nil
}

func mailHostCertificateUsesScopedRenewal(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	tracker, _ := ctx.Value(serviceMutationExecutionTrackerKey{}).(*serviceMutationExecutionTracker)
	return tracker != nil && tracker.manager != nil && tracker.manager.mailRenewalScope != nil
}
