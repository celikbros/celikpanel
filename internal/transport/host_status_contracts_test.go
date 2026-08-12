package transport

import (
	"bytes"
	"encoding/gob"
	"reflect"
	"testing"
)

func TestApplyFirewallContractRoundTripPreservesPayloadAndBinding(t *testing.T) {
	want := ApplyFirewallRequest{
		ServiceMutationBinding: ServiceMutationBinding{
			MutationRequestID: "request",
			MutationOwnerID:   "owner",
		},
		Enabled:  true,
		Persist:  true,
		TCPPorts: []int{80, 443},
		UDPPorts: []int{53},
	}
	var wire bytes.Buffer
	if err := gob.NewEncoder(&wire).Encode(want); err != nil {
		t.Fatal(err)
	}
	var got ApplyFirewallRequest
	if err := gob.NewDecoder(&wire).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("request round trip got=%#v want=%#v", got, want)
	}
}

func TestAgentVersionCapabilityExtensionIsGobBackwardCompatible(t *testing.T) {
	type legacyAgentVersionResponse struct {
		Version string
		Commit  string
	}
	newResponse := AgentVersionResponse{
		Version: "v1", Commit: "commit",
		Capabilities: []string{
			AgentCapabilityFirewallApplyV2,
			AgentCapabilityPanelCertificateIssueV2,
		},
	}
	var wire bytes.Buffer
	if err := gob.NewEncoder(&wire).Encode(newResponse); err != nil {
		t.Fatal(err)
	}
	var legacy legacyAgentVersionResponse
	if err := gob.NewDecoder(&wire).Decode(&legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.Version != newResponse.Version || legacy.Commit != newResponse.Commit {
		t.Fatalf("legacy decoder = %#v", legacy)
	}

	wire.Reset()
	if err := gob.NewEncoder(&wire).Encode(legacy); err != nil {
		t.Fatal(err)
	}
	var upgraded AgentVersionResponse
	if err := gob.NewDecoder(&wire).Decode(&upgraded); err != nil {
		t.Fatal(err)
	}
	if upgraded.Version != legacy.Version || upgraded.Commit != legacy.Commit ||
		upgraded.Capabilities != nil {
		t.Fatalf("upgraded decoder = %#v", upgraded)
	}
}
