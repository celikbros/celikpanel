package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
)

const (
	errCodeMailProfileDNSIdentityNotReady = "mail_profile_dns_identity_not_ready"
	mailProfileDNSIdentityMessage         = "DNS identity is not ready. Install a supported DNS server and save the shared nameserver names and operating mode in Settings before installing a mail profile."
)

var errMailProfileDNSIdentityNotReady = errors.New("mail profile DNS identity is not ready")

var mailProfileDNSIdentitySettingKeys = [...]string{
	settingNS1,
	settingNS2,
	settingDNSRole,
	settingDNSPeerIP,
	settingDNSPeerNS,
}

// savedDNSIdentityConfiguredStrict reads the complete saved tuple in one SQL
// snapshot. The ordinary settings helper intentionally turns missing rows and
// database failures into empty strings; an install preflight must distinguish
// an incomplete identity from a state it could not verify.
func (p *Panel) savedDNSIdentityConfiguredStrict(ctx context.Context) (bool, error) {
	rows, err := p.db.GetDB().QueryContext(ctx, `
		SELECT key, value
		FROM panel_settings
		WHERE key IN (?, ?, ?, ?, ?)`,
		mailProfileDNSIdentitySettingKeys[0],
		mailProfileDNSIdentitySettingKeys[1],
		mailProfileDNSIdentitySettingKeys[2],
		mailProfileDNSIdentitySettingKeys[3],
		mailProfileDNSIdentitySettingKeys[4],
	)
	if err != nil {
		return false, fmt.Errorf("read saved DNS identity: %w", err)
	}
	defer rows.Close()

	values := make(map[string]string, len(mailProfileDNSIdentitySettingKeys))
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return false, fmt.Errorf("decode saved DNS identity: %w", err)
		}
		values[key] = strings.TrimSpace(value)
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("read saved DNS identity rows: %w", err)
	}

	ns1 := canonicalDNSName(values[settingNS1])
	ns2 := canonicalDNSName(values[settingNS2])
	if !validDNSHostname(ns1) || !validDNSHostname(ns2) || ns1 == ns2 {
		return false, nil
	}
	switch normalizeDNSRole(values[settingDNSRole]) {
	case "standalone":
		return true, nil
	case "paired":
		peerIP := net.ParseIP(values[settingDNSPeerIP])
		peerNS := canonicalDNSName(values[settingDNSPeerNS])
		return peerIP != nil && peerIP.To4() != nil && peerIP.IsGlobalUnicast() &&
			validDNSHostname(peerNS) && (peerNS == ns1 || peerNS == ns2), nil
	default:
		return false, nil
	}
}

// mailProfileDNSIdentityReady is the one strict prerequisite used by both the
// read-only Components contract and the mutation preflight: durable engine
// identity and strict runtime readiness must agree on exactly one managed
// publisher, and the complete identity must already be saved. Package presence
// alone is never publication authority. Public delegation is deliberately not
// required here; that is a later operational readiness check.
func (p *Panel) mailProfileDNSIdentityReady(ctx context.Context) (bool, error) {
	_, ready, err := p.activeDNSPublisher(ctx)
	if err != nil {
		return false, fmt.Errorf("verify active DNS publisher: %w", err)
	}
	if !ready {
		return false, nil
	}
	return p.savedDNSIdentityConfiguredStrict(ctx)
}

func (p *Panel) requireMailProfileDNSIdentity(ctx context.Context) error {
	ready, err := p.mailProfileDNSIdentityReady(ctx)
	if err != nil {
		return err
	}
	if !ready {
		return errMailProfileDNSIdentityNotReady
	}
	return nil
}

// managedServicesDNSIdentityReady fails closed for the read-only catalogue.
// A transient agent/database failure must never leave an enabled mail action;
// the mutation endpoint repeats the strict proof and retains the detailed
// server-only cause.
func (p *Panel) managedServicesDNSIdentityReady(ctx context.Context) bool {
	ready, err := p.mailProfileDNSIdentityReady(ctx)
	return err == nil && ready
}

func mailProfileCatalogBlockedReason(hostBlockedReason string, dnsIdentityReady bool) string {
	if hostBlockedReason != "" {
		return hostBlockedReason
	}
	if !dnsIdentityReady {
		return mailProfileDNSIdentityMessage
	}
	return ""
}
