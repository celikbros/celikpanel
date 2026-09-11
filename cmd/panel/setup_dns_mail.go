package main

import (
	"context"
	"net"
	"strings"
	"time"
)

// These are instructions for the domain's existing provider, never records in
// a local shadow zone. The per-domain mail hostname matches mail TLS/SNI setup.
func externalMailDNSRecords(domain, ipv4, ipv6 string, auth mailAuthStatus) []DNSRecord {
	mailName := "mail." + domain
	records := []DNSRecord{{Name: domain, Type: "MX", Content: mailName, TTL: 3600, Prio: 10}}
	for _, address := range []struct{ kind, value string }{{"A", ipv4}, {"AAAA", ipv6}} {
		if address.value != "" {
			records = append(records, DNSRecord{Name: mailName, Type: address.kind, Content: address.value, TTL: 3600})
		}
	}
	for _, record := range []mailAuthRecord{auth.SPF, auth.DKIM, auth.DMARC} {
		if record.Recommended != "" {
			records = append(records, DNSRecord{Name: record.Name, Type: "TXT", Content: record.Recommended, TTL: 3600})
		}
	}
	return records
}

type mailDNSResolver interface {
	LookupMX(context.Context, string) ([]*net.MX, error)
	LookupHost(context.Context, string) ([]string, error)
}

// Required-record instructions alone cannot establish readiness. An external
// domain needs a matching primary MX and mail addresses resolving to this host.
func externalMailDNSHealth(ctx context.Context, domain, ipv4, ipv6 string, resolver mailDNSResolver) healthCheck {
	result := healthCheck{ID: "mx", Status: "unknown", Detail: "mail." + domain}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	mx, err := resolver.LookupMX(ctx, domain)
	if err != nil {
		if failure, ok := err.(*net.DNSError); ok && failure.IsNotFound {
			result.Status = "fail"
		}
		return result
	}
	result.Status = "fail"
	if len(mx) == 0 {
		return result
	}
	preference := uint16(65535)
	for _, entry := range mx {
		if entry != nil && entry.Pref < preference {
			preference = entry.Pref
		}
	}
	matched := false
	for _, entry := range mx {
		if entry == nil || entry.Pref != preference {
			continue
		}
		if !strings.EqualFold(strings.TrimSuffix(entry.Host, "."), result.Detail) {
			return result
		}
		matched = true
	}
	if !matched {
		return result
	}
	addresses, err := resolver.LookupHost(ctx, result.Detail)
	if err != nil {
		result.Status = "unknown"
		if failure, ok := err.(*net.DNSError); ok && failure.IsNotFound {
			result.Status = "fail"
		}
		return result
	}
	if len(addresses) == 0 {
		return result
	}
	for _, raw := range addresses {
		ip := net.ParseIP(raw)
		if ip == nil || !(ip.Equal(net.ParseIP(ipv4)) || ip.Equal(net.ParseIP(ipv6))) {
			return result
		}
	}
	result.Status = "ok"
	return result
}
