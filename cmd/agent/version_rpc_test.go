package main

import (
	"reflect"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func TestAgentVersionPublishesClosedMutationCapabilities(t *testing.T) {
	var response AgentVersionResponse
	if err := (&Agent{}).Version(&transport.Empty{}, &response); err != nil {
		t.Fatal(err)
	}
	want := []string{
		transport.AgentCapabilityFirewallApplyV2,
		transport.AgentCapabilityDNSZoneSyncV2,
		transport.AgentCapabilityDNSSECSecureV2,
		transport.AgentCapabilityDNSClusterConfigureV2,
		transport.AgentCapabilityDNSZoneSyncV3,
		transport.AgentCapabilityDNSZoneRecoverV1,
		transport.AgentCapabilityDNSEngineSwitchV1,
		transport.AgentCapabilityMailTLSSyncV2,
		transport.AgentCapabilityPanelCertificateIssueV2,
		transport.AgentCapabilitySystemUpdateV1,
		transport.AgentCapabilitySecurityAuditV1,
	}
	if !reflect.DeepEqual(response.Capabilities, want) {
		t.Fatalf("agent capabilities = %#v, want %#v", response.Capabilities, want)
	}
}
