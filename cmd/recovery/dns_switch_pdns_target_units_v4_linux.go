//go:build linux

package main

import (
	"context"
	"errors"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsunitrestore"
)

// The native inverse may need several systemctl effects. Re-prove the same
// frozen journal, excluded worker and native candidate/source state before
// every effect; status reads remain independent to avoid recursive probes.
func guardedPDNSTargetUnitRunnerV4(
	guard func(context.Context) error,
	run func(context.Context, string, ...string) ([]byte, error),
) func(context.Context, string, ...string) ([]byte, error) {
	var refused error
	return func(ctx context.Context, path string, args ...string) ([]byte, error) {
		if guard == nil || run == nil || path != "/usr/bin/systemctl" || len(args) == 0 {
			return nil, errors.New("PowerDNS target unit restore lacks a protected systemctl runner")
		}
		// dnsunitrestore may regard a post-command readback as success even if
		// the command returned an error. After a guard failure, deny readbacks
		// too so an owner change cannot be silently converted into success.
		if refused != nil {
			return nil, refused
		}
		if args[0] != "show" {
			if err := guard(ctx); err != nil {
				refused = err
				return nil, err
			}
		}
		return run(ctx, path, args...)
	}
}

func restorePDNSTargetBINDUnitsV4(ctx context.Context, snapshots []dnsengineartifact.UnitSnapshot, guard func(context.Context) error) error {
	if ctx == nil || guard == nil || len(snapshots) != 2 ||
		snapshots[0].Name != "bind9.service" || snapshots[1].Name != "named.service" {
		return errors.New("PowerDNS target unit restore requires frozen BIND source units")
	}
	owned := map[string]bool{}
	for _, snapshot := range snapshots {
		if snapshot.LoadState != "masked" {
			owned[snapshot.Name] = true
		}
	}
	return dnsunitrestore.Restore(ctx, snapshots, owned, dnsunitrestore.Ops{
		Systemctl: "/usr/bin/systemctl", VerifyMaskParent: bindInverseMaskParent,
		RunSystemd: guardedPDNSTargetUnitRunnerV4(guard, bindInverseSystemd),
	})
}

// A persisted V4 enable intent, not mere observed enablement, authorizes
// returning the stopped target unit to its originally disabled state.
func restorePDNSTargetUnitV4(ctx context.Context, snapshot dnsengineartifact.UnitSnapshot, guard func(context.Context) error) error {
	if ctx == nil || guard == nil || snapshot.Name != "pdns.service" || snapshot.LoadState != "loaded" ||
		snapshot.ActiveState != "inactive" || snapshot.UnitFileState != "disabled" {
		return errors.New("PowerDNS target unit restore requires the frozen disabled preimage")
	}
	return dnsunitrestore.Restore(ctx, []dnsengineartifact.UnitSnapshot{snapshot}, nil, dnsunitrestore.Ops{
		Systemctl: "/usr/bin/systemctl", VerifyMaskParent: bindInverseMaskParent,
		RunSystemd: guardedPDNSTargetUnitRunnerV4(guard, bindInverseSystemd),
	})
}

// The fresh V3 package guard can freeze its own persistent mask. Restoring it
// after an enable-intent returns the installed package to a sealed, stopped
// state. All other preimages keep the V4 disabled-unit admission.
func restorePDNSTargetUnitV3(ctx context.Context, snapshot dnsengineartifact.UnitSnapshot, guard func(context.Context) error) error {
	if snapshot == (dnsengineartifact.UnitSnapshot{Name: "pdns.service", LoadState: "loaded", ActiveState: "inactive", UnitFileState: "disabled"}) {
		return restorePDNSTargetUnitV4(ctx, snapshot, guard)
	}
	if ctx == nil || guard == nil ||
		snapshot != (dnsengineartifact.UnitSnapshot{Name: "pdns.service", LoadState: "masked", ActiveState: "inactive", UnitFileState: "masked"}) {
		return errors.New("v3 PowerDNS target restore requires the frozen guarded mask")
	}
	return dnsunitrestore.Restore(ctx, []dnsengineartifact.UnitSnapshot{snapshot}, nil, dnsunitrestore.Ops{
		Systemctl: "/usr/bin/systemctl", VerifyMaskParent: bindInverseMaskParent,
		RunSystemd: guardedPDNSTargetUnitRunnerV4(guard, bindInverseSystemd),
	})
}
