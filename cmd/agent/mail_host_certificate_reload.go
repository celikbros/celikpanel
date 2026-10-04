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
	if err := observeOrReloadMailHostTLS(ctx, journal, cert, key, commands.run, secureReadConfig, reload); err != nil {
		return &mailHostReloadUnverified{cause: err}
	}
	return nil
}

type mailHostReloadUnverified struct{ cause error }

func (e *mailHostReloadUnverified) Error() string {
	return "mail certificate activation paused: accepted native settings and running mail services could not be verified or reloaded; the server owner must review Postfix/Dovecot configuration and service status, then retry the same operation; settings and certificate evidence are preserved"
}
func (e *mailHostReloadUnverified) Unwrap() error { return e.cause }

// Observation runs before any reload, between reloads, and after both. Never
// start a stopped service, rewrite owner settings, regenerate fallback material,
// compile a customer SNI map, or call postfix check (which can create files).
func observeOrReloadMailHostTLS(ctx context.Context, journal *mailTLSSyncJournal, cert, key string, run mailTLSCommandRunner, read func(string) ([]byte, error), reload bool) error {
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
	for _, unit := range []string{"postfix.service", "dovecot.service"} {
		if _, err := run("systemctl", "reload", unit); err != nil {
			return err
		}
		if err := observe(); err != nil {
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
