package main

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/alicelik/celikpanel/internal/hostplatform"
)

// inspectVerifiedAdoptedBINDRuntimeTopology is restricted to a native Debian
// owner preimage: named is enabled and running, with either the exact vendor
// bind9 alias or a genuinely absent alias. Managed BIND continues to use the
// stricter shared topology checker.
func inspectVerifiedAdoptedBINDRuntimeTopology(ctx context.Context, profile hostplatform.Profile, systemctl string) (bindRuntimeTopologySnapshot, error) {
	if ctx == nil || profile.PackageManager != hostplatform.PackageManagerAPT {
		return bindRuntimeTopologySnapshot{}, errors.New("owner BIND runtime requires Debian APT")
	}
	ops, err := bindUnitIdentityProofOperations(ctx, profile, systemctl)
	if err != nil {
		return bindRuntimeTopologySnapshot{}, err
	}
	return inspectVerifiedAdoptedBINDRuntimeTopologyWithOps(ops)
}

func inspectVerifiedAdoptedBINDRuntimeTopologyWithOps(ops bindUnitIdentityProofOps) (bindRuntimeTopologySnapshot, error) {
	if ops.inspectStates == nil || ops.inspectIdentity == nil ||
		ops.inspectVendorFiles == nil || ops.inspectProcesses == nil {
		return bindRuntimeTopologySnapshot{}, errors.New("invalid owner BIND runtime inspection")
	}
	capture := func() (bindRuntimeTopologySnapshot, error) {
		named, alias, err := ops.inspectStates()
		if err != nil {
			return bindRuntimeTopologySnapshot{}, err
		}
		if named.loadState != "loaded" || named.activeState != "active" || named.unitFileState != "enabled" {
			return bindRuntimeTopologySnapshot{}, errors.New("owner BIND named unit is not active and enabled")
		}
		aliasAbsent := exactAbsentInactiveBINDUnit(alias)
		aliasPresent := alias.loadState == "loaded" && alias.activeState == "active" && alias.unitFileState == "enabled"
		if !aliasAbsent && !aliasPresent {
			return bindRuntimeTopologySnapshot{}, errors.New("owner BIND alias is neither exact vendor-active nor absent")
		}
		identity, err := ops.inspectIdentity("named.service")
		if err != nil {
			return bindRuntimeTopologySnapshot{}, err
		}
		aliasIdentity := dnsUnitIdentity{}
		if aliasAbsent {
			if err = validateAPTBINDVendorNamedIdentity(identity, false); err != nil {
				return bindRuntimeTopologySnapshot{}, err
			}
		} else {
			aliasIdentity, err = ops.inspectIdentity("bind9.service")
			if err != nil {
				return bindRuntimeTopologySnapshot{}, err
			}
			if err = validateAPTBINDVendorAliasIdentity(identity, aliasIdentity); err != nil {
				return bindRuntimeTopologySnapshot{}, err
			}
		}
		vendor, err := ops.inspectVendorFiles()
		if err != nil {
			return bindRuntimeTopologySnapshot{}, fmt.Errorf("owner BIND vendor files: %w", err)
		}
		processes, err := ops.inspectProcesses("named.service")
		if err != nil {
			return bindRuntimeTopologySnapshot{}, err
		}
		if processes.MainPID == 0 || processes.ControlPID != 0 || processes.SubState != "running" {
			return bindRuntimeTopologySnapshot{}, errors.New("owner BIND has no exact named main process")
		}
		aliasProcesses := dnsUnitProcesses{}
		if aliasPresent {
			aliasProcesses, err = ops.inspectProcesses("bind9.service")
			if err != nil {
				return bindRuntimeTopologySnapshot{}, err
			}
			if aliasProcesses != processes {
				return bindRuntimeTopologySnapshot{}, errors.New("owner BIND vendor alias process differs from named")
			}
		}
		return bindRuntimeTopologySnapshot{
			namedState: named, aliasState: alias, namedIdentity: identity,
			aliasIdentity: aliasIdentity, vendorFiles: vendor,
			namedProcesses: processes, aliasProcesses: aliasProcesses,
		}, nil
	}
	before, err := capture()
	if err != nil {
		return bindRuntimeTopologySnapshot{}, err
	}
	after, err := capture()
	if err != nil {
		return bindRuntimeTopologySnapshot{}, err
	}
	if !reflect.DeepEqual(before, after) {
		return bindRuntimeTopologySnapshot{}, errors.New("owner BIND native unit identity moved during proof")
	}
	return after, nil
}

func captureAdoptedBINDRuntimeTopology(ctx context.Context, profile hostplatform.Profile, systemctl string) (bindRuntimeTopologySnapshot, error) {
	topology, err := inspectVerifiedAdoptedBINDRuntimeTopology(ctx, profile, systemctl)
	if err != nil {
		return bindRuntimeTopologySnapshot{}, err
	}
	pdns, err := dnsSystemdStateGuard(systemctl).inspect(ctx, "pdns.service")
	if err != nil {
		return bindRuntimeTopologySnapshot{}, err
	}
	if !exactAbsentInactiveBINDUnit(pdns) {
		return bindRuntimeTopologySnapshot{}, errors.New("owner BIND has a competing PowerDNS unit")
	}
	return topology, nil
}

func verifyOnlyAdoptedBINDActive(ctx context.Context, profile hostplatform.Profile, systemctl string) error {
	_, err := captureBINDAdoptionRuntimeEvidence(ctx, profile, systemctl)
	return err
}
