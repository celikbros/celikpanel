package main

import (
	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/transport"
)

// managedServiceHostProfile returns the verified server identity used by the
// catalogue. An older agent may expose only PkgFamily; that compatibility
// fallback is sufficient for established apt/pacman mappings but deliberately
// lacks the distro proof required by every dnf preview capability.
func (p *Panel) managedServiceHostProfile() core.ManagedServiceHostProfile {
	p.pkgFamilyMu.Lock()
	if p.hostPlatformKnown {
		response := p.hostPlatformVal
		p.pkgFamilyMu.Unlock()
		host, _ := managedServiceHostProfileFromResponse(response)
		return host
	}
	hasAgent := p.agentClient != nil
	fallbackFamily := p.pkgFamilyVal
	p.pkgFamilyMu.Unlock()

	if hasAgent {
		var response transport.HostPlatformResponse
		if err := p.callAgent("Agent.HostPlatform", &transport.Empty{}, &response); err == nil {
			if host, ok := managedServiceHostProfileFromResponse(response); ok {
				p.pkgFamilyMu.Lock()
				p.hostPlatformVal = response
				p.hostPlatformKnown = true
				p.pkgFamilyVal = response.PackageManager
				p.pkgFamilyMu.Unlock()
				return host
			}
		}
	}

	if !hasAgent {
		return core.ManagedServiceHostProfile{PackageFamily: fallbackFamily}
	}
	if fallbackFamily == "" {
		fallbackFamily = p.packageFamily()
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
