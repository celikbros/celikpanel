package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/bindrndckey"
	"github.com/alicelik/celikpanel/internal/transport"
)

// A removed top-level zone has no parent authority on this BIND server. BIND
// correctly replies REFUSED, so an authoritative parent-negative response
// alone cannot prove its removal. Bind the DNS response to the same running
// daemon's authenticated "no matching zone in any view" control result.
type bindZoneStatusProbe func(context.Context, string) ([]byte, error)

func probeBINDZoneStatus(ctx context.Context, domain string) ([]byte, error) {
	control, err := firstTrustedExecutable(
		[]string{"/usr/sbin/rndc", "/usr/bin/rndc"}, "rndc",
	)
	if err != nil {
		return nil, errors.New("trusted BIND control executable is unavailable")
	}
	return serviceMutationCommand(ctx, control, "zonestatus", domain).CombinedOutputLimited(4 << 10)
}

func exactBINDDeletedZoneStatus(domain string, output []byte, commandErr error) bool {
	if commandErr == nil {
		return false
	}
	return strings.TrimSpace(string(output)) ==
		"rndc: 'zonestatus' failed: not found\nno matching zone '"+domain+"' in any view"
}

func verifyBINDDeletedZoneAt(
	ctx context.Context, address, domain string,
	probe dnsZoneSOAProbe, zonestatus bindZoneStatusProbe,
) error {
	if probe == nil || zonestatus == nil {
		return errors.New("BIND deletion proof is unavailable")
	}
	proofCtx, stop := context.WithTimeout(ctx, dnsPairProofLimit)
	defer stop()
	var last error
	for {
		last = verifyBINDDeletedZoneOnce(proofCtx, address, domain, probe, zonestatus)
		if last == nil {
			return nil
		}
		// A control channel without a usable key does not heal by waiting:
		// return its typed reason now instead of a generic timeout.
		if _, unavailable := bindrndckey.ReasonOf(last); unavailable {
			return fmt.Errorf("BIND zone %s deletion cannot be proved: %w", domain, last)
		}
		select {
		case <-proofCtx.Done():
			return fmt.Errorf("BIND zone %s deletion did not converge: %w: %v", domain, proofCtx.Err(), last)
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func verifyBINDDeletedZoneOnce(
	ctx context.Context, address, domain string,
	probe dnsZoneSOAProbe, zonestatus bindZoneStatusProbe,
) error {
	statusCtx, cancel := context.WithTimeout(ctx, dnsProbeTimeout)
	output, statusErr := zonestatus(statusCtx, domain)
	cancel()
	if !exactBINDDeletedZoneStatus(domain, output, statusErr) {
		if unavailable := bindrndckey.ClassifyControlFailure(output, statusErr); unavailable != nil {
			return unavailable
		}
		return fmt.Errorf("BIND did not prove zone %s absent in every view", domain)
	}
	for _, network := range []string{"udp", "tcp"} {
		probeCtx, cancel := context.WithTimeout(ctx, dnsProbeTimeout)
		result, err := probe(probeCtx, network, address, domain)
		cancel()
		if err != nil {
			return fmt.Errorf("verify removed BIND zone %s over %s: %w", domain, network, err)
		}
		if validDeletedDNSZoneProof(domain, result) {
			continue
		}
		if result.RCode != dnsRCodeRefused || result.Authoritative ||
			result.AnswerCount != 0 || len(result.SOASerials) != 0 ||
			len(result.AnswerSOAOwners) != 0 || len(result.AuthoritySOAOwners) != 0 {
			return fmt.Errorf("removed BIND zone %s did not return an empty REFUSED response over %s", domain, network)
		}
	}
	return nil
}

func verifyBINDV3AuthoritiesAt(
	ctx context.Context, address string, expected []expectedDNSZoneAuthority,
	probe dnsZoneSOAProbe, zonestatus bindZoneStatusProbe,
) error {
	if ip := net.ParseIP(address); ip == nil || ip.IsUnspecified() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return errors.New("BIND authority verification selected an unsafe address")
	}
	for _, zone := range expected {
		if zone.Delete {
			if err := verifyBINDDeletedZoneAt(ctx, address, zone.Domain, probe, zonestatus); err != nil {
				return err
			}
			continue
		}
		if err := verifyDNSZoneAuthoritiesAt(ctx, address, []expectedDNSZoneAuthority{zone}, probe); err != nil {
			return err
		}
	}
	return nil
}

func verifyBINDV3Authorities(ctx context.Context, expected []expectedDNSZoneAuthority) error {
	addresses, err := publicListenAddresses(ctx)
	if err != nil || len(addresses) == 0 {
		if err == nil {
			err = errors.New("no BIND verification address")
		}
		return fmt.Errorf("discover BIND authority verification address: %w", err)
	}
	return verifyBINDV3AuthoritiesAt(ctx, strings.TrimSpace(addresses[0]), expected, probeDNSZoneSOA, probeBINDZoneStatus)
}

func verifyBINDV3ZoneManifestAuthority(
	ctx context.Context, zones []transport.DNSEngineSwitchZoneSnapshot,
) error {
	expected, err := expectedDNSZoneAuthorities(zones)
	if err != nil {
		return err
	}
	return verifyBINDV3Authorities(ctx, expected)
}
