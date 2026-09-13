package main

import (
	"context"
	"errors"
	"net"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/dnswire"
)

var errServerSetupPrimaryDNSRequired = errors.New("the configured primary DNS catalog is not yet reachable; prepare the primary DNS server and check DNS access")

var probeServerSetupPrimaryCatalogSOA = dnswire.QueryAuthoritativeSOA

// serverSetupSecondaryPrerequisite is a read-only check before starting a NEW
// secondary installation. Existing operations must be reconciled by identity,
// never replaced or reclassified by this preflight. An authoritative catalog SOA
// establishes only that a primary is reachable; the agent still verifies AXFR,
// ownership and the complete native transfer topology during installation.
func serverSetupSecondaryPrerequisite(ctx context.Context, draft serverSetupDraft) error {
	if draft.DNSMode != setupDNSModeLocal || draft.DNSRole != "secondary" {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	catalog, err := binddns.CatalogDomain(draft.PeerIP)
	if err != nil {
		return err
	}
	local := net.ParseIP(draft.LocalIP)
	if local == nil || local.To4() == nil || local.String() != draft.LocalIP || !local.IsGlobalUnicast() || draft.LocalIP == draft.PeerIP {
		return errors.New("secondary DNS requires distinct canonical local and primary IPv4 addresses")
	}
	serial, err := probeServerSetupPrimaryCatalogSOA(ctx, net.JoinHostPort(draft.PeerIP, "53"), catalog)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil || serial == 0 {
		return errServerSetupPrimaryDNSRequired
	}
	return nil
}
