package main

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/alicelik/celikpanel/internal/hostname"
	"github.com/alicelik/celikpanel/internal/transport"
)

var issuedStagedLineageSuffixRE = regexp.MustCompile(`^[a-f0-9]{24}$`)

func validateIssuedCertificateLineage(
	domain string,
	domainID int,
	replacement bool,
	raw string,
) error {
	lineage := strings.TrimSpace(raw)
	if lineage == "" || lineage != raw || lineage != strings.ToLower(lineage) {
		return fmt.Errorf("the ACME agent returned an invalid certificate lineage identity")
	}
	canonicalDomain := strings.ToLower(
		strings.TrimSuffix(strings.TrimSpace(domain), "."),
	)
	if !replacement {
		if lineage != canonicalDomain {
			return fmt.Errorf(
				"the ACME agent returned a non-canonical initial certificate lineage",
			)
		}
		return nil
	}
	if domainID <= 0 {
		return fmt.Errorf("the certificate domain identity is invalid")
	}
	prefix := fmt.Sprintf("cp-site-%d-", domainID)
	if !strings.HasPrefix(lineage, prefix) ||
		!issuedStagedLineageSuffixRE.MatchString(
			strings.TrimPrefix(lineage, prefix),
		) {
		return fmt.Errorf(
			"the ACME agent returned an invalid staged certificate lineage",
		)
	}
	return nil
}

// managedSiteHostnames is the single source of truth for names served by a
// website and requested from its certificate authority. Root domains include
// their conventional www name because CelikPanel creates that DNS record by
// default; hosted subdomains do not grow an unexpected www.<subdomain> name.
// Explicit aliases are included in both the vhost and the certificate.
func (p *Panel) managedSiteHostnames(ctx context.Context, domainID int) ([]string, error) {
	var (
		primary  string
		parentID sql.NullInt64
	)
	if err := p.db.GetDB().QueryRowContext(ctx,
		`SELECT name, parent_domain_id FROM domains WHERE id = ?`, domainID,
	).Scan(&primary, &parentID); err != nil {
		return nil, err
	}

	raw := []string{primary}
	if !parentID.Valid {
		raw = append(raw, "www."+strings.TrimSuffix(strings.TrimSpace(primary), "."))
	}

	rows, err := p.db.GetDB().QueryContext(ctx,
		`SELECT alias FROM domain_aliases WHERE domain_id = ? ORDER BY alias`, domainID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var alias string
		if err := rows.Scan(&alias); err != nil {
			return nil, err
		}
		raw = append(raw, alias)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return normalizeManagedHostnames(raw)
}

func normalizeManagedHostnames(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("at least one hostname is required")
	}

	seen := make(map[string]bool, len(raw))
	names := make([]string, 0, len(raw))
	for _, value := range raw {
		name := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(value), "."))
		if err := validateManagedHostname(name); err != nil {
			return nil, err
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}

	// Keep the primary name first; deterministic ordering for every other name
	// makes generated vhosts and ACME requests stable and reviewable.
	if len(names) > 2 {
		sort.Strings(names[1:])
	}
	return names, nil
}

func validateManagedHostname(name string) error {
	return hostname.Validate(name)
}

func certificateCoversHostname(dnsNames []string, hostname string) bool {
	host := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(hostname), "."))
	for _, candidate := range dnsNames {
		name := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(candidate), "."))
		if name == host {
			return true
		}
		if !strings.HasPrefix(name, "*.") {
			continue
		}
		suffix := strings.TrimPrefix(name, "*.")
		if strings.HasSuffix(host, "."+suffix) &&
			strings.Count(host, ".") == strings.Count(suffix, ".")+1 {
			return true
		}
	}
	return false
}

// mailCertificateHostname derives the only mail hostname that certificate
// issuance may add. Callers never accept an arbitrary request-supplied name:
// the canonical primary domain is the sole input.
func mailCertificateHostname(domain string) (string, error) {
	return hostname.MailFQDN(domain)
}

type installedCertificateInfo = transport.ValidateCertResponse

func (p *Panel) installedCertificateDetails(certPath string) (installedCertificateInfo, error) {
	info, err := p.readInstalledCertificateInfo(certPath)
	if err != nil {
		return info, err
	}
	if !info.Valid {
		if info.Error == "" {
			info.Error = "certificate is not valid"
		}
		return info, fmt.Errorf("%s", info.Error)
	}
	return info, nil
}

func (p *Panel) readInstalledCertificateInfo(certPath string) (installedCertificateInfo, error) {
	var info installedCertificateInfo
	if strings.TrimSpace(certPath) == "" {
		return info, fmt.Errorf("certificate path is empty")
	}
	if err := p.callAgent("Agent.GetCertificateInfo", certPath, &info); err != nil {
		return info, err
	}
	info.DNSNames = normalizeCertificateDNSNames(info.DNSNames)
	return info, nil
}

func (p *Panel) inspectInstalledCertificate(
	ctx context.Context,
	certPath string,
	keyPath string,
) (installedCertificateInfo, error) {
	return p.inspectManagedCertificate(ctx, "", certPath, keyPath, "")
}

func (p *Panel) inspectManagedCertificate(
	ctx context.Context,
	domain string,
	certPath string,
	keyPath string,
	chainPath string,
) (installedCertificateInfo, error) {
	var info installedCertificateInfo
	if strings.TrimSpace(certPath) == "" {
		return info, fmt.Errorf("certificate path is empty")
	}
	if strings.TrimSpace(keyPath) == "" {
		return info, fmt.Errorf("private key path is empty")
	}
	request := transport.InspectCertificateRequest{
		Domain: domain, CertPath: certPath, KeyPath: keyPath, ChainPath: chainPath,
	}
	if err := p.callAgentContext(ctx, "Agent.InspectInstalledCertificate", request, &info); err != nil {
		return info, err
	}
	info.DNSNames = normalizeCertificateDNSNames(info.DNSNames)
	return info, nil
}

func (p *Panel) installedCertificateDNSNames(certPath string) ([]string, error) {
	info, err := p.installedCertificateDetails(certPath)
	if err != nil {
		return nil, err
	}
	return info.DNSNames, nil
}

func normalizeCertificateDNSNames(raw []string) []string {
	seen := make(map[string]bool, len(raw))
	out := make([]string, 0, len(raw))
	for _, value := range raw {
		name := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(value), "."))
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
