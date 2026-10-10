package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/alicelik/celikpanel/internal/services"
)

// The answer to a site that could not be created because the web server
// refused the configuration CelikPanel generated for it (12 Oct 2026; D-024).
//
// It used to be `500 INTERNAL` "internal server error" (measured on Arch: the
// PHP virtual host named a file only Debian's nginx package ships, `nginx -t`
// refused it, the Agent put everything back and the owner was told nothing).
// The Agent now says that this is what happened
// (transport.WebServerRefusedConfig) with the line nginx printed, and the
// answer is `502 SITE_WEB_SERVER_REFUSED`.
//
// What the answer may say was removed is what was verified, and by whom:
//
//   - the Agent sets the code only after the vhost's own inverse ran to the
//     end: the vhost file and link are absent again, nginx accepted the
//     previous configuration and was reloaded with it;
//   - the Panel then asks the Agent to delete the site (the orchestrator's
//     compensation) and reads its answer. Agent.DeleteSite answers success
//     only when the vhost, the PHP pool, the system account and the site's
//     directory are gone; only then are the domain and site rows removed.
//
// `reason` is `removed` when that compensation was confirmed and
// `cleanup_unconfirmed` when it was not; an import has `import_removed` and
// `import_cleanup_unconfirmed`, whose sentences also say that nothing of the
// archive was imported. Not claimed by any of them: that nothing at all is
// left on the server. The directory the Agent prepares for certificate
// validation of the domain, under its own state directory, is not removed by
// a site's deletion.
//
// `details` holds nginx's own line for an administrator only: it can name
// paths of this server.
//
// Web sunucusu CelikPanel'in ürettiği yapılandırmayı reddettiği için
// oluşturulamayan bir sitenin yanıtı. Eskiden `500 INTERNAL` idi. Yanıt yalnızca
// doğrulananı söyler: Agent sanal konağı eski haline getirdiğini bildirir,
// Panel de sitenin silinmesini ister ve yanıtını okur. `reason`, bu temizliğin
// doğrulanıp doğrulanmadığını söyler. nginx'in kendi satırı yalnızca yöneticiye
// gösterilir.

const (
	siteWebServerRefusedRemoved             = "removed"
	siteWebServerRefusedUnconfirmed         = "cleanup_unconfirmed"
	siteWebServerRefusedImportRemoved       = "import_removed"
	siteWebServerRefusedImportUnconfirmed   = "import_cleanup_unconfirmed"
	siteWebServerRefusedWhat                = "the web server (nginx) refused the configuration CelikPanel generated for it, so the site was never put into service. "
	siteWebServerRefusedRemovedSentence     = "What had been created for it was removed again and the removal was confirmed: its web server configuration, its system account, its files and, for a PHP site, its PHP pool. nginx was reloaded with the configuration it had before. "
	siteWebServerRefusedUnconfirmedSentence = "nginx was reloaded with the configuration it had before. Removing what had been created for the site was not confirmed, so parts of it may remain on the server: open Domains and, if %s is listed there, delete it, which removes its parts. "
	siteWebServerRefusedOwner               = "The server owner runs sudo nginx -t on the server. If it reports an error now, a file of the server's own nginx configuration is refused and is corrected first. If it passes, what nginx refused was in the configuration CelikPanel generated for this server; the line nginx printed names it and is shown to administrators. "
)

func siteWebServerRefusedMessage(reason, domain string) string {
	unconfirmed := strings.Replace(siteWebServerRefusedUnconfirmedSentence, "%s", domain, 1)
	switch reason {
	case siteWebServerRefusedRemoved:
		return "The site " + domain + " was not created: " + siteWebServerRefusedWhat + siteWebServerRefusedRemovedSentence +
			siteWebServerRefusedOwner + "Then create the site again; nothing retries by itself."
	case siteWebServerRefusedUnconfirmed:
		return "The site " + domain + " was not created: " + siteWebServerRefusedWhat + unconfirmed +
			siteWebServerRefusedOwner + "Then create the site again; nothing retries by itself."
	case siteWebServerRefusedImportRemoved:
		return "The import did not start, and no file, mailbox, DNS record or database of the archive was imported. The site " + domain +
			" is the import's first step and it was not created: " + siteWebServerRefusedWhat + siteWebServerRefusedRemovedSentence +
			siteWebServerRefusedOwner + "Then start the import again; nothing starts it again automatically."
	default:
		return "The import did not start, and no file, mailbox, DNS record or database of the archive was imported. The site " + domain +
			" is the import's first step and it was not created: " + siteWebServerRefusedWhat + unconfirmed +
			siteWebServerRefusedOwner + "Then start the import again; nothing starts it again automatically."
	}
}

// webServerRefusedConfig finds the Agent's typed answer in a site creation
// error. confirmed reports whether the Panel's own compensation (the site's
// deletion on the Agent, then the rows) ended without an error: the
// orchestrator joins that compensation's error to the cause, so a join of
// exactly one error is a confirmed removal.
func webServerRefusedConfig(err error) (refusal *services.WebServerRefusedConfigError, confirmed, found bool) {
	if !errors.As(err, &refusal) || refusal == nil {
		return nil, false, false
	}
	joined, ok := err.(interface{ Unwrap() []error })
	return refusal, ok && len(joined.Unwrap()) == 1, true
}

func writeSiteWebServerRefused(w http.ResponseWriter, caller *Caller, domain string, refusal *services.WebServerRefusedConfigError, confirmed, duringImport bool) {
	reason := siteWebServerRefusedUnconfirmed
	switch {
	case duringImport && confirmed:
		reason = siteWebServerRefusedImportRemoved
	case duringImport:
		reason = siteWebServerRefusedImportUnconfirmed
	case confirmed:
		reason = siteWebServerRefusedRemoved
	}
	detail := boundedAgentDiagnostic(refusal.Detail)
	body := apiErrorBody{
		Error:  siteWebServerRefusedMessage(reason, domain),
		Code:   errCodeSiteWebServerRefused,
		Reason: reason,
		Vars:   map[string]string{"domain": domain, "command": "sudo nginx -t"},
	}
	if caller != nil && caller.Role == roleAdmin && detail != "" {
		body.Details = []string{detail}
	}
	log.Printf("[502][domain] %s: %s %s: %s", domain, errCodeSiteWebServerRefused, reason, detail)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadGateway)
	_ = json.NewEncoder(w).Encode(body)
}

// writePHPVersionNotInstalled refuses, before anything is created, a PHP
// version the request names and this server does not run (12 Oct 2026).
// writePHPVersionNotInstalled, isteğin adlandırdığı ve bu sunucunun
// çalıştırmadığı bir PHP sürümünü hiçbir şey oluşturulmadan reddeder.
func writePHPVersionNotInstalled(w http.ResponseWriter, requested string, installed []string) {
	requested = boundedAgentDiagnostic(requested)
	list := strings.Join(installed, ", ")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusConflict)
	_ = json.NewEncoder(w).Encode(apiErrorBody{
		Error:  phpVersionNotInstalledMessage(requested, list),
		Code:   errCodePHPVersionNotInstalled,
		Action: "/services",
		Vars:   map[string]string{"version": requested, "installed": list},
	})
}

func phpVersionNotInstalledMessage(requested, installed string) string {
	return "Nothing was created: PHP " + requested + " is not installed on this server. Installed: " + installed +
		". Choose one of these versions and create the site again; another version can be used only after it is installed on this server (Services)."
}

// newSitePHPVersion is the PHP version a new site is created with: the one
// the request names when this server runs it, the newest this server runs
// when the request names none. installed is what the Agent reports
// (hostingCaps), newest first. Never a version nobody observed.
// newSitePHPVersion, yeni bir sitenin oluşturulduğu PHP sürümüdür: istek bir
// sürüm adlandırıyorsa ve sunucu onu çalıştırıyorsa o, adlandırmıyorsa
// sunucunun çalıştırdığı en yeni sürüm. Kimsenin gözlemlemediği bir sürüm asla.
func newSitePHPVersion(requested string, installed []string) (string, bool) {
	if requested == "" {
		if len(installed) == 0 {
			return "", false
		}
		return installed[0], true
	}
	return requested, containsDomainPHPVersion(installed, requested)
}
