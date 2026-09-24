//go:build linux

package dnsenginerecovery

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"reflect"
	"time"

	"github.com/alicelik/celikpanel/internal/bindroot"
	"github.com/alicelik/celikpanel/internal/dnsunitidentity"
	"github.com/alicelik/celikpanel/internal/hostplatform"
)

type BINDIdentityRunner func(context.Context, string) ([]byte, error)

// SystemdBINDIdentityRunner reads only fixed native BIND unit names and
// properties. It never reloads or changes a service.
func SystemdBINDIdentityRunner(ctx context.Context, name string) ([]byte, error) {
	if name != "named.service" && name != "bind9.service" {
		return nil, errors.New("unsupported BIND unit identity query")
	}
	return runSystemdDNSIdentity(ctx, name)
}

// SystemdPDNSIdentityRunner reads only pdns.service's native unit identity.
func SystemdPDNSIdentityRunner(ctx context.Context, name string) ([]byte, error) {
	if name != "pdns.service" {
		return nil, errors.New("unsupported PowerDNS unit identity query")
	}
	return runSystemdDNSIdentity(ctx, name)
}

func runSystemdDNSIdentity(ctx context.Context, name string) ([]byte, error) {
	if ctx == nil {
		return nil, errors.New("DNS unit identity observation requires a context")
	}
	queryCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(queryCtx, "/usr/bin/systemctl", "show", name,
		"--property=Id,Names,FragmentPath,DropInPaths,SourcePath,Transient,ExecStart",
		"--no-pager")
	output := &boundedUnitOutput{}
	cmd.Stdout = output
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("read DNS unit identity %s: %w", name, err)
	}
	return output.Bytes(), nil
}

func probeBINDVendorIdentityOnce(ctx context.Context, profile hostplatform.Profile, runner BINDIdentityRunner) (dnsunitidentity.Identity, error) {
	raw, err := runner(ctx, "named.service")
	if err != nil {
		return dnsunitidentity.Identity{}, err
	}
	named, err := dnsunitidentity.Parse(string(raw))
	if err != nil {
		return dnsunitidentity.Identity{}, fmt.Errorf("parse named.service identity: %w", err)
	}
	switch profile.PackageManager {
	case hostplatform.PackageManagerAPT:
		hasAlias := false
		for _, name := range named.Names {
			if name == "bind9.service" {
				hasAlias = true
			}
		}
		if err := dnsunitidentity.ValidateAPTBINDVendorNamedIdentity(named, hasAlias); err != nil {
			return dnsunitidentity.Identity{}, err
		}
		if hasAlias {
			rawAlias, err := runner(ctx, "bind9.service")
			if err != nil {
				return dnsunitidentity.Identity{}, err
			}
			alias, err := dnsunitidentity.Parse(string(rawAlias))
			if err != nil {
				return dnsunitidentity.Identity{}, fmt.Errorf("parse bind9.service identity: %w", err)
			}
			if err := dnsunitidentity.ValidateAPTBINDVendorAliasIdentity(named, alias); err != nil {
				return dnsunitidentity.Identity{}, err
			}
		}
	case hostplatform.PackageManagerPacman:
		if err := dnsunitidentity.ValidatePacmanBINDVendorIdentity(named); err != nil {
			return dnsunitidentity.Identity{}, err
		}
	default:
		return dnsunitidentity.Identity{}, errors.New("unsupported BIND unit host profile")
	}
	return named, nil
}

// ProbeBINDVendorIdentity compares two exact systemd identity observations.
// It does not prove process liveness, reloaded config or DNS answers.
func ProbeBINDVendorIdentity(ctx context.Context, profile hostplatform.Profile, runner BINDIdentityRunner) (dnsunitidentity.Identity, error) {
	if ctx == nil || runner == nil {
		return dnsunitidentity.Identity{}, errors.New("invalid BIND unit identity observation")
	}
	if _, err := bindroot.CertifiedVendorContract(profile); err != nil {
		return dnsunitidentity.Identity{}, err
	}
	first, err := probeBINDVendorIdentityOnce(ctx, profile, runner)
	if err != nil {
		return dnsunitidentity.Identity{}, err
	}
	second, err := probeBINDVendorIdentityOnce(ctx, profile, runner)
	if err != nil {
		return dnsunitidentity.Identity{}, err
	}
	if !reflect.DeepEqual(first, second) {
		return dnsunitidentity.Identity{}, errors.New("BIND unit identity changed during observation")
	}
	return second, nil
}

// ProbePDNSVendorIdentity compares two exact APT/systemd unit identity
// observations. It does not verify vendor file bytes or database content.
func ProbePDNSVendorIdentity(ctx context.Context, profile hostplatform.Profile, runner BINDIdentityRunner) (dnsunitidentity.Identity, error) {
	if ctx == nil || runner == nil ||
		profile.PackageManager != hostplatform.PackageManagerAPT ||
		profile.DistroFamily != hostplatform.DistroFamilyDebian ||
		profile.ServiceManager != hostplatform.ServiceManagerSystemd {
		return dnsunitidentity.Identity{}, errors.New("PowerDNS identity observation requires a verified APT/systemd host")
	}
	read := func() (dnsunitidentity.Identity, error) {
		raw, err := runner(ctx, "pdns.service")
		if err != nil {
			return dnsunitidentity.Identity{}, err
		}
		if len(raw) > 4096 {
			return dnsunitidentity.Identity{}, errors.New("PowerDNS unit identity exceeds its bound")
		}
		identity, err := dnsunitidentity.Parse(string(raw))
		if err != nil {
			return dnsunitidentity.Identity{}, err
		}
		if err := dnsunitidentity.ValidateAPTPDNSVendorIdentity(identity); err != nil {
			return dnsunitidentity.Identity{}, err
		}
		return identity, nil
	}
	first, err := read()
	if err != nil {
		return dnsunitidentity.Identity{}, err
	}
	second, err := read()
	if err != nil {
		return dnsunitidentity.Identity{}, err
	}
	if !reflect.DeepEqual(first, second) {
		return dnsunitidentity.Identity{}, errors.New("PowerDNS vendor unit identity changed during observation")
	}
	return second, nil
}
