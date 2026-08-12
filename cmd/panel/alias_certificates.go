package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	errCodeAliasCertificateReissueRequired = "ALIAS_CERTIFICATE_REISSUE_REQUIRED"
	errCodeAliasCertificatePending         = "ALIAS_CERTIFICATE_ACTIVATION_PENDING"
)

type activeAliasCertificate struct {
	ID             int64
	Type           string
	CertPath       string
	KeyPath        string
	ChainPath      string
	LineageName    string
	ACMEProviderID string
	Issuer         string
	Subject        string
	AutoRenew      bool
	SecureMail     bool
}

func (p *Panel) loadActiveAliasCertificate(
	ctx context.Context,
	domainID int,
) (*activeAliasCertificate, error) {
	var cert activeAliasCertificate
	err := p.db.GetDB().QueryRowContext(ctx, `
		SELECT id, type, cert_path, key_path, COALESCE(chain_path, ''),
		       COALESCE(lineage_name, ''), COALESCE(acme_provider_id, ''),
		       COALESCE(issuer, ''), COALESCE(subject, ''),
		       COALESCE(auto_renew, true), COALESCE(secure_mail, false)
		FROM ssl_certificates
		WHERE domain_id = ? AND status = 'active'
		ORDER BY id DESC LIMIT 1`, domainID).
		Scan(
			&cert.ID, &cert.Type, &cert.CertPath, &cert.KeyPath, &cert.ChainPath,
			&cert.LineageName, &cert.ACMEProviderID,
			&cert.Issuer, &cert.Subject, &cert.AutoRenew, &cert.SecureMail,
		)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &cert, nil
}

func desiredAliasCertificateNames(
	current []string,
	add string,
	remove string,
) ([]string, error) {
	names := append([]string(nil), current...)
	if add != "" {
		names = append(names, add)
	}
	if remove != "" {
		filtered := names[:0]
		for _, name := range names {
			if strings.EqualFold(name, remove) {
				continue
			}
			filtered = append(filtered, name)
		}
		names = filtered
	}
	return normalizeManagedHostnames(names)
}

func exactCertificateDNSNames(actual, expected []string) error {
	actualNames, err := normalizeManagedHostnames(actual)
	if err != nil {
		return fmt.Errorf("issued certificate contains an invalid DNS name: %w", err)
	}
	expectedNames, err := normalizeManagedHostnames(expected)
	if err != nil {
		return err
	}
	if len(actualNames) != len(expectedNames) {
		return fmt.Errorf(
			"issued certificate DNS name set differs from the requested set",
		)
	}
	actualSet := make(map[string]struct{}, len(actualNames))
	for _, name := range actualNames {
		actualSet[strings.ToLower(name)] = struct{}{}
	}
	for _, name := range expectedNames {
		if _, ok := actualSet[strings.ToLower(name)]; !ok {
			return fmt.Errorf("issued certificate does not contain exactly %s", name)
		}
	}
	return nil
}

type certificateCleanupTarget struct {
	Domain          string
	DeleteCanonical bool
	LineageName     string
	CertPath        string
	KeyPath         string
	ChainPath       string
}

func (target certificateCleanupTarget) snapshotPath() string {
	for _, candidate := range []string{
		target.CertPath, target.KeyPath, target.ChainPath,
	} {
		if candidate != strings.TrimSpace(candidate) {
			// Do not normalize an agent-supplied filesystem authority. The
			// exact immutable path must already be canonical.
			return ""
		}
	}
	for _, candidate := range []string{
		target.CertPath, target.KeyPath, target.ChainPath,
	} {
		if candidate != "" {
			return candidate
		}
	}
	return ""
}

func (p *Panel) certificateSnapshotReferenced(
	ctx context.Context,
	target certificateCleanupTarget,
) (bool, error) {
	var referenced bool
	err := p.db.GetDB().QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1
			FROM ssl_certificates
			WHERE (? <> '' AND cert_path = ?)
			   OR (? <> '' AND key_path = ?)
			   OR (? <> '' AND COALESCE(chain_path, '') = ?)
		)`,
		target.CertPath, target.CertPath,
		target.KeyPath, target.KeyPath,
		target.ChainPath, target.ChainPath,
	).Scan(&referenced)
	return referenced, err
}

func (p *Panel) cleanupUncommittedCertificate(
	ctx context.Context,
	target certificateCleanupTarget,
) {
	target.Domain = strings.TrimSpace(target.Domain)
	target.LineageName = strings.TrimSpace(target.LineageName)
	snapshotPath := target.snapshotPath()
	if snapshotPath != "" {
		referenced, err := p.certificateSnapshotReferenced(ctx, target)
		if err != nil {
			log.Printf(
				"certificate cleanup for %s: refuse snapshot deletion because ledger lookup failed: %v",
				target.Domain, err,
			)
			snapshotPath = ""
		} else if referenced {
			// An immutable version can be reused (for example, uploading the
			// same custom certificate twice). Never remove a version named by
			// any active, revoked, expired, or pending ledger row.
			snapshotPath = ""
		}
	}
	if !target.DeleteCanonical && target.LineageName == "" &&
		snapshotPath == "" {
		return
	}
	var lineageNames []string
	if target.LineageName != "" {
		lineageNames = []string{target.LineageName}
	}
	var resp transport.DeleteCertLineageResponse
	err := p.callAgentContext(ctx, "Agent.DeleteCertLineage", &transport.DeleteCertLineageRequest{
		ExpectedBuildCommit: strings.TrimSpace(buildCommit),
		Domain:              target.Domain,
		DeleteCanonical:     target.DeleteCanonical,
		LineageNames:        lineageNames,
		SnapshotPath:        snapshotPath,
	}, &resp)
	if err != nil || strings.TrimSpace(resp.Error) != "" {
		log.Printf(
			"uncommitted certificate cleanup for %s: %v %s",
			target.Domain, err, resp.Error,
		)
	}
}

// issueAliasCertificateSnapshot obtains and verifies an immutable ACME
// snapshot for the desired post-mutation hostname set. The alias is not yet in
// the database or website server_name list; it is exposed only in a temporary
// validation-only port-80 block. The normal vhost is restored before return.
func (p *Panel) issueAliasCertificateSnapshot(
	ctx context.Context,
	domainID int,
	current *activeAliasCertificate,
	desiredNames []string,
	extraChallengeNames []string,
) (certificateInstall, error) {
	var install certificateInstall
	if err := p.requireMatchingAgentBuild(ctx); err != nil {
		return install, err
	}
	var cleanupTarget certificateCleanupTarget
	keepIssuedMaterial := false
	defer func() {
		if keepIssuedMaterial {
			return
		}
		cleanupCtx, cancel := sslCompensationContext()
		defer cancel()
		p.cleanupUncommittedCertificate(cleanupCtx, cleanupTarget)
	}()
	if current == nil || current.Type != "letsencrypt" {
		return install, fmt.Errorf("an active panel-managed ACME certificate is required")
	}
	if len(desiredNames) == 0 {
		return install, fmt.Errorf("the desired certificate hostname set is empty")
	}

	var (
		domainName     string
		subscriptionID int
	)
	if err := p.db.GetDB().QueryRowContext(ctx, `
		SELECT d.name, d.subscription_id
		FROM domains d JOIN sites s ON s.domain_id = d.id
		WHERE d.id = ?`, domainID).
		Scan(&domainName, &subscriptionID); err != nil {
		return install, err
	}
	if !strings.EqualFold(desiredNames[0], domainName) {
		return install, fmt.Errorf("the primary certificate hostname cannot change")
	}
	cleanupTarget.Domain = domainName

	currentInfo, err := p.inspectInstalledCertificate(
		ctx, current.CertPath, current.KeyPath,
	)
	if err != nil {
		return install, fmt.Errorf("inspect active certificate before alias reissue: %w", err)
	}
	if !currentInfo.Valid || !currentInfo.TrustChecked || !currentInfo.Trusted {
		return install, fmt.Errorf("the active certificate is not a trusted reissue source")
	}
	mailName, err := mailCertificateHostname(domainName)
	if err != nil {
		return install, err
	}
	preserveMailSAN := certificateCoversHostname(currentInfo.DNSNames, mailName)
	if current.SecureMail && !preserveMailSAN {
		return install, fmt.Errorf("secure mail is enabled but the active certificate does not cover %s", mailName)
	}

	requestedNames := append([]string(nil), desiredNames...)
	challengeNames := append([]string(nil), extraChallengeNames...)
	if preserveMailSAN {
		requestedNames = append(requestedNames, mailName)
		challengeNames = append(challengeNames, mailName)
	}
	requestedNames, err = normalizeManagedHostnames(requestedNames)
	if err != nil {
		return install, err
	}

	providerID := strings.TrimSpace(current.ACMEProviderID)
	if providerID == "" {
		// Upgrade compatibility for a row that predates the durable provider
		// identity migration. Newly activated rows always persist the ID.
		providerID = acmeProviderIDForIssuer(current.Issuer)
	}
	provider := core.ACMEProviderByID(providerID)
	if provider == nil {
		return install, fmt.Errorf("the active ACME provider could not be identified")
	}

	if err := p.applyVhostForDomainWithACMEChallengeNames(
		ctx, domainID, challengeNames,
	); err != nil {
		return install, fmt.Errorf("prepare alias certificate validation vhost: %w", err)
	}
	validationVhostPrepared := true
	defer func() {
		if !validationVhostPrepared {
			return
		}
		restoreCtx, restoreCancel := sslCompensationContext()
		defer restoreCancel()
		if err := p.applyVhostForDomain(restoreCtx, domainID); err != nil {
			log.Printf("alias certificate domain %d: restore validation vhost: %v", domainID, err)
		}
	}()

	var agentResp transport.IssueLetsEncryptResponse
	agentReq := transport.IssueLetsEncryptRequest{
		ExpectedBuildCommit: strings.TrimSpace(buildCommit),
		Domain:              domainName,
		Aliases:             append([]string(nil), requestedNames[1:]...),
		SubscriptionID:      subscriptionID,
		DomainID:            domainID,
		AutoRenew:           current.AutoRenew,
		ForceRenewal:        true,
		StageLineage:        true,
		CurrentCertPath:     current.CertPath,
		CurrentLineageName:  current.LineageName,
		ACMEServer:          provider.Directory,
	}
	issueErr := p.callAgentContext(
		ctx, "Agent.IssueLetsEncryptCertificate", agentReq, &agentResp,
	)
	stagedLineage := strings.TrimSpace(agentResp.LineageName)
	cleanupTarget.LineageName = stagedLineage
	cleanupTarget.CertPath = agentResp.CertPath
	cleanupTarget.KeyPath = agentResp.KeyPath
	cleanupTarget.ChainPath = agentResp.ChainPath
	restoreCtx, restoreCancel := sslCompensationContext()
	restoreErr := p.applyVhostForDomain(restoreCtx, domainID)
	restoreCancel()
	if restoreErr == nil {
		validationVhostPrepared = false
	}
	if issueErr != nil || !agentResp.Success {
		if restoreErr != nil {
			return install, fmt.Errorf(
				"alias certificate issue failed: %v (%s); validation vhost restore failed: %w",
				issueErr, agentResp.Error, restoreErr,
			)
		}
		if issueErr != nil {
			return install, issueErr
		}
		return install, fmt.Errorf("%s", strings.TrimSpace(agentResp.Error))
	}
	if restoreErr != nil {
		return install, fmt.Errorf("restore vhost after alias certificate validation: %w", restoreErr)
	}
	if strings.TrimSpace(agentResp.CertPath) == "" ||
		strings.TrimSpace(agentResp.KeyPath) == "" {
		return install, fmt.Errorf("the ACME agent returned no immutable certificate paths")
	}
	if stagedLineage == "" {
		return install, fmt.Errorf("the ACME agent returned no staged lineage identity")
	}
	if err := validateIssuedCertificateLineage(
		domainName, domainID, true, stagedLineage,
	); err != nil {
		return install, err
	}

	info, err := p.inspectManagedCertificate(
		ctx,
		domainName,
		agentResp.CertPath,
		agentResp.KeyPath,
		agentResp.ChainPath,
	)
	if err != nil {
		return install, fmt.Errorf("inspect alias certificate snapshot: %w", err)
	}
	if !info.Valid || !info.TrustChecked || !info.Trusted {
		detail := strings.TrimSpace(info.TrustError)
		if detail == "" {
			detail = strings.TrimSpace(info.Error)
		}
		if detail == "" {
			detail = "certificate trust validation failed"
		}
		return install, fmt.Errorf("%s", detail)
	}
	if err := exactCertificateDNSNames(info.DNSNames, requestedNames); err != nil {
		return install, err
	}
	expiresAt := info.ExpiresAt
	if expiresAt.IsZero() {
		expiresAt = agentResp.ExpiresAt
	}
	if expiresAt.IsZero() {
		return install, fmt.Errorf("issued certificate has no expiry")
	}
	// Keep the chosen CA's stable display identity. The leaf's X.509 Issuer
	// commonly names a rotating intermediate (for example R13/E7).
	issuer := current.Issuer
	subject := strings.TrimSpace(info.Subject)
	if subject == "" {
		subject = domainName
	}

	install = certificateInstall{
		DomainName:     domainName,
		Type:           "letsencrypt",
		CertPath:       agentResp.CertPath,
		KeyPath:        agentResp.KeyPath,
		ChainPath:      agentResp.ChainPath,
		LineageName:    stagedLineage,
		ACMEProviderID: provider.ID,
		Issuer:         issuer,
		Subject:        subject,
		IssuedAt:       info.IssuedAt,
		ExpiresAt:      expiresAt,
		AutoRenew:      current.AutoRenew,
		SecureMail:     current.SecureMail,
	}
	keepIssuedMaterial = true
	return install, nil
}
