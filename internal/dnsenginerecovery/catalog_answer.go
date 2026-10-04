package dnsenginerecovery

import (
	"context"
	"errors"
	"fmt"
	"net"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/dnswire"
)

type CatalogSOAQuery func(context.Context, string, string) (uint32, error)
type LocalAddressCheck func(context.Context, string) (bool, error)

// HostOwnsIPAddress observes the local interface list. It is not a lease on
// the address and does not establish which process holds port 53.
func HostOwnsIPAddress(ctx context.Context, address string) (bool, error) {
	if ctx == nil {
		return false, errors.New("local DNS address observation requires a context")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	expected := net.ParseIP(address)
	if expected == nil || expected.To4() == nil || expected.To4().String() != address || !expected.IsGlobalUnicast() {
		return false, errors.New("local DNS address is not a canonical public IPv4 address")
	}
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		return false, err
	}
	for _, candidate := range addresses {
		ipnet, ok := candidate.(*net.IPNet)
		if ok && ipnet.IP.Equal(expected) {
			return true, nil
		}
	}
	return false, nil
}

// ProbePrimaryCatalogAnswer observes one exact authoritative SOA serial at
// the verified primary's literal local IP twice. False means no primary
// catalog applies. It does not prove zone members, AXFR, loaded config or PID.
func ProbePrimaryCatalogAnswer(ctx context.Context, receipt binddns.Receipt, owns LocalAddressCheck, query CatalogSOAQuery) (bool, error) {
	if ctx == nil || owns == nil || query == nil {
		return false, errors.New("invalid primary catalog observation")
	}
	if receipt.Pairing == nil || receipt.Pairing.Role == binddns.PairRoleSecondary {
		return false, nil
	}
	if receipt.Pairing.Role != binddns.PairRolePrimary {
		return false, errors.New("unknown BIND catalog role")
	}
	pairing := receipt.Pairing
	expectedCatalog, err := binddns.CatalogDomain(pairing.LocalIP)
	if err != nil || pairing.LocalCatalog != expectedCatalog || pairing.CatalogSerial == 0 {
		return false, errors.New("primary catalog identity differs from the verified generation")
	}
	endpoint := net.JoinHostPort(pairing.LocalIP, "53")
	for range 2 {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		owned, err := owns(ctx, pairing.LocalIP)
		if err != nil {
			return false, fmt.Errorf("inspect primary catalog address: %w", err)
		}
		if !owned {
			return false, errors.New("primary catalog address is not confirmed on this host")
		}
		serial, err := query(ctx, endpoint, pairing.LocalCatalog)
		if err != nil {
			return false, fmt.Errorf("query local authoritative primary catalog: %w", err)
		}
		if serial != pairing.CatalogSerial {
			return false, errors.New("local authoritative catalog serial differs from the selected generation")
		}
	}
	return true, nil
}

func ProbeInstalledPrimaryCatalogAnswer(ctx context.Context, receipt binddns.Receipt) (bool, error) {
	return ProbePrimaryCatalogAnswer(ctx, receipt, HostOwnsIPAddress, dnswire.QueryAuthoritativeSOA)
}
