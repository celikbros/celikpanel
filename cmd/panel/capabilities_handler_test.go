package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/rpc"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/transport"
)

type hostingCapabilitiesTestAgent struct {
	installed    []string
	installedErr error
	instances    []core.ServiceInstance
	instancesErr error
}

func (a *hostingCapabilitiesTestAgent) InstalledServiceIDsStrict(
	_ *transport.Empty,
	reply *[]string,
) error {
	if a.installedErr != nil {
		return a.installedErr
	}
	*reply = append([]string(nil), a.installed...)
	return nil
}

func (a *hostingCapabilitiesTestAgent) ListServiceInstances(
	_ *transport.ServiceInstancesRequest,
	reply *transport.ServiceInstancesResponse,
) error {
	if a.instancesErr != nil {
		return a.instancesErr
	}
	reply.Instances = append([]core.ServiceInstance(nil), a.instances...)
	return nil
}

func newHostingCapabilitiesTestPanel(t *testing.T, agent *hostingCapabilitiesTestAgent) *Panel {
	t.Helper()
	server := rpc.NewServer()
	if err := server.RegisterName("Agent", agent); err != nil {
		t.Fatalf("register hosting capabilities test agent: %v", err)
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
		t.Fatalf("connect hosting capabilities test agent: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return &Panel{agentClient: transport.NewReconnectingClientWithContextConnector(client, connector)}
}

func TestHostingCapabilitiesDiscoveryFailureIsNotReportedAsEmpty(t *testing.T) {
	panel := newHostingCapabilitiesTestPanel(t, &hostingCapabilitiesTestAgent{
		installedErr: errors.New("probe failed"),
	})

	recorder := httptest.NewRecorder()
	panel.handleHostingCapabilities(
		recorder,
		httptest.NewRequest(http.MethodGet, "/api/v1/hosting/capabilities", nil),
	)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusInternalServerError, recorder.Body.String())
	}

	createRecorder := httptest.NewRecorder()
	panel.handleCreateDomain(
		createRecorder,
		httptest.NewRequest(
			http.MethodPost,
			"/api/v1/domains",
			strings.NewReader(`{"domain":"example.test","project_type":"static"}`),
		),
	)
	if createRecorder.Code != http.StatusInternalServerError {
		t.Fatalf("create status = %d, want %d; body=%s", createRecorder.Code, http.StatusInternalServerError, createRecorder.Body.String())
	}
}

func TestHostingCapabilitiesPHPDiscoveryFailureIsNotReportedAsMissing(t *testing.T) {
	panel := newHostingCapabilitiesTestPanel(t, &hostingCapabilitiesTestAgent{
		installed:    []string{"nginx", "pdns", "php-fpm"},
		instancesErr: errors.New("instance probe failed"),
	})

	recorder := httptest.NewRecorder()
	panel.handleHostingCapabilities(
		recorder,
		httptest.NewRequest(http.MethodGet, "/api/v1/hosting/capabilities", nil),
	)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusInternalServerError, recorder.Body.String())
	}
}

func TestHostingCapabilitiesRejectsInstalledPHPWithoutManagedVersion(t *testing.T) {
	panel := newHostingCapabilitiesTestPanel(t, &hostingCapabilitiesTestAgent{
		installed: []string{"nginx", "pdns", "php-fpm"},
	})

	recorder := httptest.NewRecorder()
	panel.handleHostingCapabilities(
		recorder,
		httptest.NewRequest(http.MethodGet, "/api/v1/hosting/capabilities", nil),
	)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusInternalServerError, recorder.Body.String())
	}
}

func TestHostingCapabilitiesReturnsStableEmptyArrays(t *testing.T) {
	panel := newHostingCapabilitiesTestPanel(t, &hostingCapabilitiesTestAgent{})

	caps, err := panel.hostingCaps(context.Background())
	if err != nil {
		t.Fatalf("hosting capabilities: %v", err)
	}
	if caps.PHPVersions == nil || caps.DatabaseServers == nil || caps.DBTools == nil {
		t.Fatalf("slice contract contains nil: %+v", caps)
	}
}

func TestHostingCapabilitiesReturnsManagedPHPVersions(t *testing.T) {
	panel := newHostingCapabilitiesTestPanel(t, &hostingCapabilitiesTestAgent{
		installed: []string{"nginx", "pdns", "php-fpm"},
		instances: []core.ServiceInstance{
			{Version: "8.4", Managed: true},
			{Version: "8.3", Managed: true},
			{Version: "system", Managed: false},
		},
	})

	caps, err := panel.hostingCaps(context.Background())
	if err != nil {
		t.Fatalf("hosting capabilities: %v", err)
	}
	if got := strings.Join(caps.PHPVersions, ","); got != "8.4,8.3" {
		t.Fatalf("PHP versions = %q, want %q", got, "8.4,8.3")
	}
}
