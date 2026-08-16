package main

import (
	"context"
	"errors"
	"net"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

const legacyPowerDNSGuardTimeout = 15 * time.Second

var (
	legacyPowerDNSDurableAuthorityCheck = inspectLegacyPowerDNSDurableAuthorityOnHost
	legacyPowerDNSRuntimeSafetyCheck    = inspectLegacyPowerDNSRuntimeSafety
	dnsPort53ConflictCheck              = inspectDNSPort53Conflict
)

func validateLegacyPowerDNSDurableAuthority(
	state dnsEngineStateReceipt,
	stateExists, journalExists, requireResolved bool,
) error {
	if journalExists {
		return errors.New("a DNS engine switch transaction is active")
	}
	if stateExists {
		if err := validateDNSEngineState(state); err != nil {
			return errors.New("the durable DNS engine state is invalid")
		}
		if state.Engine != transport.DNSEnginePowerDNS {
			return errors.New("the durable DNS engine state does not authorize PowerDNS")
		}
		return nil
	}
	if requireResolved {
		return errors.New("the durable DNS engine state is unresolved")
	}
	return nil
}

func validateLegacyPowerDNSUnitStates(
	named, bindAlias, pdns dnsUnitState,
	requireActive bool,
) (bool, error) {
	if named.active() || bindAlias.active() {
		return false, errors.New("BIND is active")
	}
	if pdns.LoadState == "not-found" {
		return false, errors.New("PowerDNS is not installed")
	}
	if pdns.active() {
		return true, nil
	}
	if requireActive {
		return false, errors.New("PowerDNS is not active")
	}
	return false, nil
}

// requireLegacyPowerDNSMutationSafe prevents older configuration RPCs from
// bypassing the engine switch transaction. It is intentionally read-only.
func requireLegacyPowerDNSMutationSafe(
	parent context.Context,
	requireActive bool,
) error {
	return requireLegacyPowerDNSMutationSafeWithAuthority(
		parent, requireActive, false,
	)
}

func requireLegacyPowerDNSMutationSafeWithAuthority(
	parent context.Context,
	requireActive, requireResolved bool,
) error {
	if err := legacyPowerDNSDurableAuthorityCheck(requireResolved); err != nil {
		return err
	}
	return legacyPowerDNSRuntimeSafetyCheck(parent, requireActive)
}

func inspectLegacyPowerDNSRuntimeSafety(
	parent context.Context,
	requireActive bool,
) error {
	ctx, cancel := context.WithTimeout(parent, legacyPowerDNSGuardTimeout)
	defer cancel()
	profile, err := verifiedHostProfileForAnyFamily()
	if err != nil {
		return err
	}
	systemctl, err := executableForProfile(profile, string(profile.PackageManager), "systemctl")
	if err != nil {
		return err
	}
	named, err := captureDNSUnitState(ctx, systemctl, "named.service")
	if err != nil {
		return err
	}
	bindAlias, err := captureDNSUnitState(ctx, systemctl, "bind9.service")
	if err != nil {
		return err
	}
	pdns, err := captureDNSUnitState(ctx, systemctl, "pdns.service")
	if err != nil {
		return err
	}
	active, err := validateLegacyPowerDNSUnitStates(named, bindAlias, pdns, requireActive)
	if err != nil {
		return err
	}
	if active {
		return verifyOnlyPDNSActive(ctx, systemctl)
	}
	conflict, err := dnsPort53ConflictCheck(ctx, false, false)
	if err != nil {
		return err
	}
	if conflict {
		return errors.New("another public port-53 authority is active")
	}
	return nil
}

func rejectLegacyPublicDNSListeners(output string) error {
	conflict := hasUnrelatedPublicDNSListener(output, false, false)
	if conflict {
		return errors.New("another public port-53 authority is active")
	}
	return nil
}

func inspectDNSPort53Conflict(
	parent context.Context,
	allowBIND, allowPowerDNS bool,
) (bool, error) {
	ctx, cancel := context.WithTimeout(parent, legacyPowerDNSGuardTimeout)
	defer cancel()
	ss, err := firstTrustedExecutable([]string{"/usr/sbin/ss", "/usr/bin/ss"}, "ss")
	if err != nil {
		return false, err
	}
	output, err := serviceMutationCommand(
		ctx, ss, "-H", "-lntup", "sport = :53",
	).CombinedOutputLimited(64 << 10)
	if err != nil {
		return false, err
	}
	return hasUnrelatedPublicDNSListener(
		string(output), allowBIND, allowPowerDNS,
	), nil
}

func hasUnrelatedPublicDNSListener(
	output string,
	allowBIND, allowPowerDNS bool,
) bool {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			if strings.Contains(line, ":53") {
				return true
			}
			continue
		}
		protocol := strings.ToLower(fields[0])
		if protocol != "tcp" && protocol != "udp" {
			continue
		}
		host, port, splitErr := net.SplitHostPort(fields[4])
		if splitErr != nil {
			if strings.Contains(fields[4], ":53") {
				return true
			}
			continue
		}
		if port != "53" {
			continue
		}
		address := net.ParseIP(strings.Trim(host, "[]"))
		if address != nil && (address.IsLoopback() || address.IsLinkLocalUnicast()) {
			continue
		}
		lower := strings.ToLower(line)
		bindOwner := strings.Contains(lower, `("named",`)
		pdnsOwner := strings.Contains(lower, `("pdns_server",`)
		if bindOwner == pdnsOwner ||
			(bindOwner && !allowBIND) ||
			(pdnsOwner && !allowPowerDNS) {
			return true
		}
	}
	return false
}

func runDNSPort53PreMutationGuard(
	ctx context.Context,
	requireEmptyAuthority bool,
	mutation func() error,
) error {
	if mutation == nil {
		return errors.New("DNS engine mutation callback is required")
	}
	if requireEmptyAuthority {
		conflict, err := dnsPort53ConflictCheck(ctx, false, false)
		if err != nil {
			return err
		}
		if conflict {
			return errors.New("another public port-53 authority is active")
		}
	}
	return mutation()
}
