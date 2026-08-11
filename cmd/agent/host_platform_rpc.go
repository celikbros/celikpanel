package main

import (
	"fmt"

	"github.com/alicelik/celikpanel/internal/transport"
)

// HostPlatform exposes only the non-secret identity fields from the same
// verified profile used by privileged package operations. Executable paths are
// intentionally omitted. The panel needs distro ID and version because a
// package-manager family is compatibility evidence, not product authorization.
func (a *Agent) HostPlatform(_ *transport.Empty, reply *transport.HostPlatformResponse) error {
	if reply == nil {
		return fmt.Errorf("host platform response is required")
	}
	*reply = transport.HostPlatformResponse{}
	profile, err := verifiedHostProfileForAnyFamily()
	if err != nil {
		return err
	}
	*reply = transport.HostPlatformResponse{
		DistroFamily:   string(profile.DistroFamily),
		PackageManager: string(profile.PackageManager),
		ServiceManager: string(profile.ServiceManager),
		DistroID:       profile.ID,
		VersionID:      profile.Version,
		Architecture:   profile.Arch,
	}
	return nil
}
