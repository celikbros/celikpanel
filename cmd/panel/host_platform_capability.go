package main

import (
	"context"
	"strings"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/transport"
)

// managedServiceHostProfile returns the verified server identity used by the
// catalogue. An older agent may expose only PkgFamily; that compatibility
// fallback is sufficient for established apt/pacman mappings but deliberately
// lacks the distro proof required by every dnf preview capability.
func (p *Panel) managedServiceHostProfile() core.ManagedServiceHostProfile {
	if p == nil {
		return core.ManagedServiceHostProfile{}
	}
	identity, err := p.agentRPCHostIdentity(context.Background())
	if err == nil {
		return identity.host
	}
	p.pkgFamilyMu.Lock()
	fallbackFamily := p.pkgFamilyVal
	p.pkgFamilyMu.Unlock()
	if fallbackFamily == "" {
		// A catalogue read may still display a family-only compatibility view,
		// but an invalid or timed-out HostPlatform response must not seed the
		// authorization cache and let a later mutation bypass full identity.
		var family string
		if callErr := p.callAgent("Agent.PkgFamily", &transport.Empty{}, &family); callErr == nil {
			fallbackFamily = strings.TrimSpace(family)
		}
	}
	return core.ManagedServiceHostProfile{PackageFamily: fallbackFamily}
}

func managedServiceHostProfileFromResponse(response transport.HostPlatformResponse) (core.ManagedServiceHostProfile, bool) {
	validIdentity := false
	switch {
	case response.DistroFamily == "debian" &&
		response.PackageManager == "apt" &&
		response.ServiceManager == "systemd":
		validIdentity = response.DistroID == "debian" || response.DistroID == "ubuntu"
	case response.DistroFamily == "rhel" &&
		response.PackageManager == "dnf" &&
		response.ServiceManager == "systemd":
		switch response.DistroID {
		case "rhel", "almalinux", "rocky", "centos", "fedora", "cloudlinux":
			validIdentity = true
		}
	case response.DistroFamily == "arch" &&
		response.PackageManager == "pacman" &&
		response.ServiceManager == "systemd":
		validIdentity = response.DistroID == "arch"
	}
	if !validIdentity || response.Architecture == "" {
		return core.ManagedServiceHostProfile{}, false
	}
	return core.ManagedServiceHostProfile{
		DistroFamily:   response.DistroFamily,
		PackageFamily:  response.PackageManager,
		ServiceManager: response.ServiceManager,
		DistroID:       response.DistroID,
		VersionID:      response.VersionID,
		Architecture:   response.Architecture,
	}, true
}
