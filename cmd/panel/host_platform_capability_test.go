package main

import (
	"context"
	"net"
	"net/rpc"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/transport"
)

type hostPlatformCapabilityTestAgent struct {
	response    transport.HostPlatformResponse
	family      string
	hostCalls   int
	familyCalls int
}

func (a *hostPlatformCapabilityTestAgent) HostPlatform(
	_ *transport.Empty,
	reply *transport.HostPlatformResponse,
) error {
	a.hostCalls++
	*reply = a.response
	return nil
}

func (a *hostPlatformCapabilityTestAgent) PkgFamily(_ *transport.Empty, reply *string) error {
	a.familyCalls++
	*reply = a.family
	return nil
}

func newHostPlatformCapabilityTestPanel(t *testing.T, agent *hostPlatformCapabilityTestAgent) *Panel {
	t.Helper()
	server := rpc.NewServer()
	if err := server.RegisterName("Agent", agent); err != nil {
		t.Fatalf("register host platform test agent: %v", err)
	}
	connector := func(ctx context.Context) (*rpc.Client, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		serverConn, clientConn := net.Pipe()
		go server.ServeConn(serverConn)
		return rpc.NewClient(clientConn), nil
	}
	client, err := connector(context.Background())
	if err != nil {
		t.Fatalf("connect host platform test agent: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return &Panel{agentClient: transport.NewReconnectingClientWithContextConnector(client, connector)}
}

func TestPanelUsesAndCachesServerDerivedHostIdentity(t *testing.T) {
	agent := &hostPlatformCapabilityTestAgent{
		response: transport.HostPlatformResponse{
			DistroFamily:   "rhel",
			PackageManager: "dnf",
			ServiceManager: "systemd",
			DistroID:       "rocky",
			VersionID:      "9.6",
			Architecture:   "arm64",
		},
		family: "dnf",
	}
	panel := newHostPlatformCapabilityTestPanel(t, agent)

	first := panel.managedServiceHostProfile()
	second := panel.managedServiceHostProfile()
	if first != second {
		t.Fatalf("cached host identity changed: first=%+v second=%+v", first, second)
	}
	if !core.IsRHELPreviewNginxCandidate(first) {
		t.Fatalf("verified server identity was not recognized as the narrow candidate: %+v", first)
	}
	if agent.hostCalls != 1 || agent.familyCalls != 0 {
		t.Fatalf("RPC calls host=%d family=%d, want host=1 family=0", agent.hostCalls, agent.familyCalls)
	}
}

func TestPanelDoesNotAuthorizeDNFFromFamilyOnlyFallback(t *testing.T) {
	agent := &hostPlatformCapabilityTestAgent{
		response: transport.HostPlatformResponse{
			DistroFamily:   "debian",
			PackageManager: "dnf",
			ServiceManager: "systemd",
			DistroID:       "rocky",
			VersionID:      "9.6",
			Architecture:   "amd64",
		},
		family: "dnf",
	}
	panel := newHostPlatformCapabilityTestPanel(t, agent)

	host := panel.managedServiceHostProfile()
	if host.PackageFamily != "dnf" {
		t.Fatalf("compatibility fallback family = %q, want dnf", host.PackageFamily)
	}
	if host.DistroID != "" || host.VersionID != "" || core.IsRHELPreviewNginxCandidate(host) {
		t.Fatalf("family-only fallback gained distro authorization: %+v", host)
	}
	if agent.hostCalls != 1 || agent.familyCalls != 1 {
		t.Fatalf("RPC calls host=%d family=%d, want one of each", agent.hostCalls, agent.familyCalls)
	}
}

func TestQualifiedRHELPreviewPreflightStopsBeforeAnyMutationProbe(t *testing.T) {
	agent := &hostPlatformCapabilityTestAgent{
		response: transport.HostPlatformResponse{
			DistroFamily:   "rhel",
			PackageManager: "dnf",
			ServiceManager: "systemd",
			DistroID:       "almalinux",
			VersionID:      "9.6",
			Architecture:   "amd64",
		},
	}
	panel := newHostPlatformCapabilityTestPanel(t, agent)

	err := panel.preflightManagedServiceInstall(context.Background(), "nginx", "")
	if err == nil {
		t.Fatal("qualified but uncertified RHEL preview passed install preflight")
	}
	for _, required := range []string{"platform-capability firewall", "SELinux"} {
		if !strings.Contains(err.Error(), required) {
			t.Fatalf("preflight error %q does not name %q", err, required)
		}
	}
	// The fake agent implements no scan/mutation RPCs. Reaching one would turn
	// this into an RPC error instead of the explicit certification boundary.
	if agent.hostCalls != 1 {
		t.Fatalf("HostPlatform calls = %d, want 1", agent.hostCalls)
	}
}

func TestHostPlatformResponseValidationRejectsInconsistentOrIncompleteIdentity(t *testing.T) {
	valid := transport.HostPlatformResponse{
		DistroFamily:   "rhel",
		PackageManager: "dnf",
		ServiceManager: "systemd",
		DistroID:       "almalinux",
		VersionID:      "9.6",
		Architecture:   "amd64",
	}
	tests := []struct {
		name   string
		change func(*transport.HostPlatformResponse)
	}{
		{name: "unknown distro", change: func(r *transport.HostPlatformResponse) { r.DistroID = "oracle" }},
		{name: "family manager mismatch", change: func(r *transport.HostPlatformResponse) { r.PackageManager = "apt" }},
		{name: "service manager mismatch", change: func(r *transport.HostPlatformResponse) { r.ServiceManager = "openrc" }},
		{name: "missing architecture", change: func(r *transport.HostPlatformResponse) { r.Architecture = "" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := valid
			test.change(&response)
			if host, ok := managedServiceHostProfileFromResponse(response); ok {
				t.Fatalf("invalid response accepted as %+v", host)
			}
		})
	}
}
