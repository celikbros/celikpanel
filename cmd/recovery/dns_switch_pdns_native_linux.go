//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"reflect"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
	"github.com/alicelik/celikpanel/internal/transport"
)

// pdnsAdoptionNativeProof describes only a read-only, point-in-time owner
// PowerDNS source observation. It is shared by status and a future narrowly
// admitted inverse; it never grants mutation authority by itself.
type pdnsAdoptionNativeProof struct {
	ActiveSOA          int
	DeletedSOA         int
	DeletedSOAVerified int
}

func proveOnlyPDNSActiveUnits(units []dnsenginerecovery.NativeUnitObservation) error {
	if len(units) != 3 ||
		units[0].Name != "named.service" ||
		units[1].Name != "bind9.service" ||
		units[2].Name != "pdns.service" ||
		units[0].ActiveState != "inactive" ||
		units[1].ActiveState != "inactive" ||
		units[2].LoadState != "loaded" ||
		units[2].ActiveState != "active" {
		return errors.New("native DNS units do not show a sole active PowerDNS authority")
	}
	return nil
}

func proveInstalledPDNSAdoptionNative(
	ctx context.Context,
	policy dnsengineartifact.JournalPolicy,
	journal dnsengineartifact.SwitchJournalV1,
) (pdnsAdoptionNativeProof, error) {
	if ctx == nil {
		return pdnsAdoptionNativeProof{}, errors.New("PowerDNS adoption native proof requires a context")
	}
	if journal.Mode != transport.DNSEngineSwitchModeAdopt || journal.SourceEngine != "" || journal.TargetEngine != transport.DNSEnginePowerDNS {
		return pdnsAdoptionNativeProof{}, errors.New("native proof requires an exact PowerDNS adoption journal")
	}
	manifest, err := dnsengineartifact.SwitchJournalManifest(journal)
	if err != nil {
		return pdnsAdoptionNativeProof{}, fmt.Errorf("reconstruct frozen PowerDNS adoption manifest: %w", err)
	}
	names := []string{"named.service", "bind9.service", "pdns.service"}
	units, err := dnsenginerecovery.ProbeNativeUnits(ctx, names, dnsenginerecovery.SystemdUnitRunner)
	if err != nil {
		return pdnsAdoptionNativeProof{}, fmt.Errorf("inspect native DNS units: %w", err)
	}
	if err := proveOnlyPDNSActiveUnits(units); err != nil {
		return pdnsAdoptionNativeProof{}, err
	}
	pdnsGID, err := localServiceGroupID("/etc/group", "pdns")
	if err != nil {
		return pdnsAdoptionNativeProof{}, fmt.Errorf("verify local PowerDNS service group: %w", err)
	}
	if err := dnsenginerecovery.ProbeInstalledPDNSAdoptionConfigs(ctx, policy, journal.ConfigBefore, pdnsGID); err != nil {
		return pdnsAdoptionNativeProof{}, fmt.Errorf("verify frozen PowerDNS config: %w", err)
	}
	if err := dnsenginerecovery.ProbePDNSAdoptionDatabase(ctx, policy.PDNSDatabasePath, journal.PDNSLiveSize, journal.PDNSLiveSHA256, manifest); err != nil {
		return pdnsAdoptionNativeProof{}, fmt.Errorf("verify frozen PowerDNS database and zones: %w", err)
	}
	mainPID, err := verifyInstalledPDNSRuntime(ctx)
	if err != nil {
		return pdnsAdoptionNativeProof{}, fmt.Errorf("verify native PowerDNS process: %w", err)
	}
	if err := dnsenginerecovery.ProbeAuthorityListeners(ctx, "pdns_server", mainPID, "", dnsenginerecovery.SSListenerRunner); err != nil {
		return pdnsAdoptionNativeProof{}, fmt.Errorf("verify sole PowerDNS TCP/UDP listeners: %w", err)
	}
	address, err := dnsenginerecovery.ProbeAuthorityIPv4Address(ctx, "pdns_server", mainPID, dnsenginerecovery.SSListenerRunner)
	if err != nil {
		return pdnsAdoptionNativeProof{}, fmt.Errorf("select verified local PowerDNS answer endpoint: %w", err)
	}
	activeSOA, deletedSOA, err := dnsenginerecovery.ProbeInstalledPDNSAdoptionSOA(ctx, net.JoinHostPort(address, "53"), manifest)
	if err != nil {
		return pdnsAdoptionNativeProof{}, fmt.Errorf("verify frozen authoritative PowerDNS SOA answers: %w", err)
	}
	deletedVerified, err := dnsenginerecovery.ProbeInstalledPDNSAdoptionDeletedSOA(ctx, net.JoinHostPort(address, "53"), manifest)
	if err != nil || deletedVerified != deletedSOA {
		return pdnsAdoptionNativeProof{}, errors.Join(errors.New("verify frozen deleted-zone absence over UDP and TCP"), err)
	}
	if again, err := verifyInstalledPDNSRuntime(ctx); err != nil || again != mainPID {
		return pdnsAdoptionNativeProof{}, errors.Join(errors.New("native PowerDNS process changed around SOA observation"), err)
	}
	if err := dnsenginerecovery.ProbeAuthorityListeners(ctx, "pdns_server", mainPID, "", dnsenginerecovery.SSListenerRunner); err != nil {
		return pdnsAdoptionNativeProof{}, fmt.Errorf("recheck PowerDNS listeners after SOA observation: %w", err)
	}
	if againGID, err := localServiceGroupID("/etc/group", "pdns"); err != nil || againGID != pdnsGID {
		return pdnsAdoptionNativeProof{}, errors.Join(errors.New("PowerDNS service group changed around SOA observation"), err)
	}
	if err := dnsenginerecovery.ProbeInstalledPDNSAdoptionConfigs(ctx, policy, journal.ConfigBefore, pdnsGID); err != nil {
		return pdnsAdoptionNativeProof{}, fmt.Errorf("recheck frozen PowerDNS config after SOA observation: %w", err)
	}
	if err := dnsenginerecovery.ProbePDNSDatabasePreimage(ctx, policy.PDNSDatabasePath, journal.PDNSLiveSize, journal.PDNSLiveSHA256); err != nil {
		return pdnsAdoptionNativeProof{}, fmt.Errorf("recheck frozen PowerDNS database after SOA observation: %w", err)
	}
	againUnits, err := dnsenginerecovery.ProbeNativeUnits(ctx, names, dnsenginerecovery.SystemdUnitRunner)
	if err != nil || !reflect.DeepEqual(units, againUnits) {
		return pdnsAdoptionNativeProof{}, errors.Join(errors.New("native DNS unit state changed around SOA observation"), err)
	}
	if err := ctx.Err(); err != nil {
		return pdnsAdoptionNativeProof{}, err
	}
	return pdnsAdoptionNativeProof{ActiveSOA: activeSOA, DeletedSOA: deletedSOA, DeletedSOAVerified: deletedVerified}, nil
}
