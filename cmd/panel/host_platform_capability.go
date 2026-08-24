package main

import (
	"context"
	"regexp"
	"strings"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/transport"
)

// managedServiceHostProfile returns the verified server identity used by the
// catalogue. An older agent may expose only PkgFamily; that compatibility
// fallback is sufficient for catalogue display but deliberately lacks the
// complete family/manager/systemd capability required by mutation policy.
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
	if !validHostPlatformCapability(response) {
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

var (
	hostCapabilityIDToken = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	hostCapabilityVersion = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,127}$`)
)

func validHostPlatformCapability(response transport.HostPlatformResponse) bool {
	expectedManager := ""
	switch response.DistroFamily {
	case "debian":
		expectedManager = "apt"
	case "rhel":
		expectedManager = "dnf"
	case "arch":
		expectedManager = "pacman"
	default:
		return false
	}
	if response.PackageManager != expectedManager || response.ServiceManager != "systemd" {
		return false
	}
	if response.Architecture != "amd64" && response.Architecture != "arm64" {
		return false
	}
	if !hostCapabilityIDToken.MatchString(response.DistroID) ||
		!hostCapabilityIDToken.MatchString(response.Architecture) {
		return false
	}
	return response.VersionID == "" || hostCapabilityVersion.MatchString(response.VersionID)
}
