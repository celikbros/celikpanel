package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

var errSetupDNSPeerIPv6Unverified = errors.New("peer nameserver IPv6 cannot yet be verified; initial setup currently requires IPv4-only peer nameserver publication")

// Initial public setup must be verifiable through public resolvers. A private
// split-horizon OS answer is useful diagnostically but cannot complete this gate.
// The production resolver set is fixed. Tests may supply controlled DNS
// endpoints; no environment or administrator input can change this policy.
var setupDNSPublicResolvers = func() []hostResolver {
	return []hostResolver{setupDNSResolverAt("1.1.1.1:53"), setupDNSResolverAt("8.8.8.8:53")}
}

func (p *Panel) setupDNSNameserverReadiness(ctx context.Context) (bool, error) {
	return p.setupDNSNameserverReadinessWithResolvers(ctx, setupDNSPublicResolvers())
}

func (p *Panel) setupDNSNameserverReadinessWithResolvers(ctx context.Context, resolvers []hostResolver) (bool, error) {
	state, err := readDNSEngineDBState(ctx, p.db.GetDB())
	if err != nil {
		return false, err
	}
	if state.ActiveEngine == "" || state.Topology != transport.DNSTopologyPaired || state.CurrentSwitchID != "" ||
		state.LocalNS == "" || state.PeerNS == "" || state.LocalIP == "" || state.PeerIP == "" {
		return false, errors.New("configure the authoritative DNS pair before publishing its nameserver addresses")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	facts := make([]nameserverFact, 0, 2)
	for _, expected := range []struct{ host, ipv4, ipv6 string }{
		{state.LocalNS, state.LocalIP, serverPrimaryIPv6()}, {state.PeerNS, state.PeerIP, ""},
	} {
		addresses, err := lookupNameserverHost(ctx, expected.host, resolvers)
		if err != nil || len(addresses) == 0 {
			return false, fmt.Errorf("waiting for public DNS: publish %s pointing to %s, then check again", expected.host, expected.ipv4)
		}
		fact := nameserverFact{Host: expected.host, IPs: addresses}
		peerIPv6Unverified := false
		for _, raw := range addresses {
			ip := net.ParseIP(raw)
			if expected.host == state.PeerNS && ip != nil && ip.To4() == nil {
				// The reviewed peer identity has no IPv6 field. Its AAAA answer
				// is unverified, not proof that the address belongs elsewhere.
				peerIPv6Unverified = true
				continue
			}
			if ip == nil || !(ip.Equal(net.ParseIP(expected.ipv4)) || (expected.ipv6 != "" && ip.Equal(net.ParseIP(expected.ipv6)))) {
				return false, fmt.Errorf("nameserver %s resolves outside its configured DNS server; correct its public A/AAAA records before completing setup", expected.host)
			}
			if ip.Equal(net.ParseIP(state.LocalIP)) || (expected.host == state.LocalNS && expected.ipv6 != "" && ip.Equal(net.ParseIP(expected.ipv6))) {
				fact.PointsHere = true
			}
		}
		// This directional topology has a reviewed IPv4 address for each peer.
		// An IPv6 answer cannot substitute for a missing reviewed IPv4 mapping.
		if !containsStr(addresses, expected.ipv4) {
			return false, fmt.Errorf("nameserver %s must publish the configured IPv4 address %s", expected.host, expected.ipv4)
		}
		if peerIPv6Unverified {
			return false, fmt.Errorf("%w: %s", errSetupDNSPeerIPv6Unverified, expected.host)
		}
		facts = append(facts, fact)
	}
	if strings.EqualFold(state.LocalNS, state.PeerNS) || !nameserverPairUsable(transport.DNSTopologyPaired, state.PeerIP, facts) {
		return false, errors.New("publish a separate nameserver address for each configured DNS server")
	}
	return true, nil
}
