package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/alicelik/celikpanel/internal/hostname"
	"github.com/alicelik/celikpanel/internal/transport"
)

type applyVhostRPCRequest = transport.ApplyVhostRequest

// applyVhostForDomain regenerates a domain's nginx vhost from the current
// database state via the agent (write → validate → reload). Every flow that
// changes what nginx should serve — SSL install, project type, forwarding —
// must end with this call, otherwise the change exists only in the ledger.
// applyVhostForDomain, bir domain'in nginx vhost'unu güncel veritabanı
// durumundan agent aracılığıyla yeniden üretir (yaz → doğrula → yeniden
// yükle). nginx'in sunacağı şeyi değiştiren her akış — SSL kurulumu, proje
// tipi, yönlendirme — bu çağrıyla bitmeli; yoksa değişiklik yalnız defterde
// kalır.
func (p *Panel) applyVhostForDomain(ctx context.Context, domainID int) error {
	return p.applyVhostForDomainWithACMEChallengeNames(ctx, domainID, nil)
}

// applyVhostForDomainWithACMEChallengeNames adds caller-authorized,
// validation-only hostnames to a dedicated port-80 server block. They never
// become website server names. This lets mail.<domain> and a not-yet-attached
// alias answer HTTP-01 without serving or redirecting the site.
func (p *Panel) applyVhostForDomainWithACMEChallengeNames(
	ctx context.Context,
	domainID int,
	explicitChallengeNames []string,
) error {
	return p.applyVhostForDomainRender(ctx, domainID, explicitChallengeNames, false)
}

// applyVhostForCertificateValidation is the render of a certificate
// operation: the same render, and for a site whose file the owner kept the
// Agent measures whether nginx serves the validation under every name
// (D-031 step 1b; ApplyVhostRequest.ProbeValidation).
func (p *Panel) applyVhostForCertificateValidation(ctx context.Context, domainID int, names []string) error {
	return p.applyVhostForDomainRender(ctx, domainID, names, true)
}

func (p *Panel) applyVhostForDomainRender(ctx context.Context, domainID int, explicitChallengeNames []string, probe bool) error {
	req, err := p.buildVhostRequest(
		ctx,
		domainID,
		explicitChallengeNames,
	)
	if err != nil {
		return err
	}
	req.ProbeValidation = probe
	// D-031: the Agent classifies the file first. A file the owner changed,
	// removed or locked is kept and this returns a typed refusal
	// (siteFileHeldError), which every caller reports as such.
	_, err = p.applySiteFileRender(ctx, req, transport.SiteFileTriggerChange)
	return err
}

// buildVhostRequest derives the complete agent input from the durable panel
// database without mutating nginx. Keeping derivation separate lets startup
// prepare every hosted vhost first and submit one all-or-nothing batch.
func (p *Panel) buildVhostRequest(
	ctx context.Context,
	domainID int,
	explicitChallengeNames []string,
) (applyVhostRPCRequest, error) {
	var (
		siteID                       int
		subscriptionID               int
		domainName, docroot          string
		phpSocket, certPath, keyPath *string
		sslEnabled                   bool
		redirectWWW                  bool
		forceHTTPS, hstsEnabled      bool
		hstsMaxAge                   int
		projectType                  string
		appPort, forwardCode         *int
		forwardTo                    *string
	)
	err := p.db.GetDB().QueryRowContext(ctx, `
		SELECT s.id, d.subscription_id, d.name, s.document_root, s.php_fpm_socket,
		       s.ssl_enabled, s.ssl_cert_path, s.ssl_key_path,
		       COALESCE(s.redirect_www, false),
		       COALESCE(s.force_https, false), COALESCE(s.hsts_enabled, false), COALESCE(s.hsts_max_age, 31536000),
		       COALESCE(s.project_type,'php'), s.app_port, s.forward_to, s.forward_code
		FROM sites s JOIN domains d ON d.id = s.domain_id
		WHERE s.domain_id = ?`, domainID).
		Scan(&siteID, &subscriptionID, &domainName, &docroot, &phpSocket,
			&sslEnabled, &certPath, &keyPath,
			&redirectWWW,
			&forceHTTPS, &hstsEnabled, &hstsMaxAge,
			&projectType, &appPort, &forwardTo, &forwardCode)
	if err != nil {
		return applyVhostRPCRequest{}, err
	}
	mailName, err := mailCertificateHostname(domainName)
	if err != nil {
		return applyVhostRPCRequest{}, fmt.Errorf(
			"derive mail certificate hostname: %w",
			err,
		)
	}
	acmeChallengeNames := make([]string, 0, len(explicitChallengeNames)+1)
	seenChallengeName := make(map[string]bool, len(explicitChallengeNames)+1)
	for _, rawName := range explicitChallengeNames {
		name, canonicalErr := hostname.CanonicalFQDN(rawName)
		if canonicalErr != nil {
			return applyVhostRPCRequest{}, fmt.Errorf(
				"invalid ACME challenge hostname %q: %w",
				rawName,
				canonicalErr,
			)
		}
		if seenChallengeName[name] {
			continue
		}
		seenChallengeName[name] = true
		acmeChallengeNames = append(acmeChallengeNames, name)
	}
	if sslEnabled && certPath != nil && keyPath != nil && *certPath != "" && *keyPath != "" {
		info, inspectErr := p.inspectInstalledCertificate(ctx, *certPath, *keyPath)
		if inspectErr != nil {
			return applyVhostRPCRequest{}, fmt.Errorf(
				"inspect active certificate challenge names: %w",
				inspectErr,
			)
		}
		if certificateCoversHostname(info.DNSNames, mailName) && !seenChallengeName[mailName] {
			acmeChallengeNames = append(acmeChallengeNames, mailName)
		}
	}
	serverNames, err := p.managedSiteHostnames(ctx, domainID)
	if err != nil {
		return applyVhostRPCRequest{}, err
	}

	req := applyVhostRPCRequest{
		ExpectedBuildCommit: strings.TrimSpace(buildCommit),
		SiteID:              siteID, SubscriptionID: subscriptionID, DomainID: domainID,
		Domain: domainName, DocumentRoot: docroot,
		SSLType: "none", ProjectType: projectType,
		ServerNames: serverNames, ACMEChallengeNames: acmeChallengeNames,
		RedirectWWW: redirectWWW,
	}
	if phpSocket != nil {
		req.PHPSocket = *phpSocket
	}
	if sslEnabled && certPath != nil && keyPath != nil && *certPath != "" && *keyPath != "" {
		req.SSLType = "custom"
		req.SSLCert = *certPath
		req.SSLKey = *keyPath
		req.ForceHTTPS = forceHTTPS
		req.HSTSEnabled = hstsEnabled
		req.HSTSMaxAge = hstsMaxAge
	}

	if appPort != nil {
		req.AppPort = *appPort
	}
	if forwardTo != nil {
		req.ForwardTo = *forwardTo
	}
	if forwardCode != nil {
		req.ForwardCode = *forwardCode
	}
	return req, nil
}
