package main

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/hostplatform"
	"github.com/alicelik/celikpanel/internal/transport"
)

func TestHostPlatformReturnsVerifiedNonSecretIdentity(t *testing.T) {
	original := detectHostPlatform
	defer func() { detectHostPlatform = original }()
	detectHostPlatform = func() (hostplatform.Profile, error) {
		return hostplatform.Profile{
			DistroFamily:   hostplatform.DistroFamilyRHEL,
			PackageManager: hostplatform.PackageManagerDNF,
			ServiceManager: hostplatform.ServiceManagerSystemd,
			ID:             "rocky",
			Version:        "9.6",
			Arch:           "amd64",
			Executables: map[string]string{
				"dnf":       "/usr/bin/dnf",
				"rpm":       "/usr/bin/rpm",
				"systemctl": "/usr/bin/systemctl",
			},
		}, nil
	}

	var response transport.HostPlatformResponse
	if err := (&Agent{}).HostPlatform(&transport.Empty{}, &response); err != nil {
		t.Fatal(err)
	}
	want := transport.HostPlatformResponse{
		DistroFamily:   "rhel",
		PackageManager: "dnf",
		ServiceManager: "systemd",
		DistroID:       "rocky",
		VersionID:      "9.6",
		Architecture:   "amd64",
	}
	if !reflect.DeepEqual(response, want) {
		t.Fatalf("HostPlatform response = %+v, want %+v", response, want)
	}
}

func TestHostPlatformFailsClosedWhenDetectionFails(t *testing.T) {
	original := detectHostPlatform
	defer func() { detectHostPlatform = original }()
	detectHostPlatform = func() (hostplatform.Profile, error) {
		return hostplatform.Profile{}, errors.New("systemd offline")
	}

	response := transport.HostPlatformResponse{DistroID: "stale"}
	err := (&Agent{}).HostPlatform(&transport.Empty{}, &response)
	if err == nil || !strings.Contains(err.Error(), "systemd offline") {
		t.Fatalf("HostPlatform error = %v, want detection detail", err)
	}
	if response != (transport.HostPlatformResponse{}) {
		t.Fatalf("failed HostPlatform retained stale identity: %+v", response)
	}
}
