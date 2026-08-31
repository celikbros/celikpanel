package main

import (
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func TestValidateDNSBackendReadinessRejectsPairReadyWithoutManagedRuntime(t *testing.T) {
	response := transport.DNSBackendReadinessResponse{Engines: []transport.DNSBackendRuntimeState{
		{
			Engine: transport.DNSEnginePowerDNS, Installed: true,
			Running: true, Managed: true, Unit: "pdns.service",
		},
		{
			Engine: transport.DNSEngineBIND, Installed: true,
			Running: true, PairReady: true, Unit: "named.service",
		},
	}}
	if _, _, _, err := validateDNSBackendReadiness(response); err == nil {
		t.Fatal("PairReady without exact managed runtime was accepted")
	}
	response.Engines[1].Managed = true
	if _, _, _, err := validateDNSBackendReadiness(response); err != nil {
		t.Fatalf("exact managed PairReady runtime rejected: %v", err)
	}
}
