package transport

import (
	"bytes"
	"encoding/gob"
	"reflect"
	"testing"
)

func TestDNSZoneSyncV2WireContractPreservesFullSnapshotAndGeneration(t *testing.T) {
	wantRequest := SyncDNSZoneV2Request{
		ServiceMutationBinding: ServiceMutationBinding{
			MutationRequestID: "request",
			MutationOwnerID:   "owner",
		},
		DesiredGeneration: 19,
		Domain:            "example.test",
		ZoneType:          "MASTER",
		Records: []ZoneRecord{
			{
				Name: "example.test", Type: "MX", Content: "mail.example.test",
				TTL: 3600, Prio: 10,
			},
			{
				Name: "old.example.test", Type: "A", Content: "192.0.2.10",
				TTL: 300, Disabled: true,
			},
		},
	}
	var wire bytes.Buffer
	if err := gob.NewEncoder(&wire).Encode(wantRequest); err != nil {
		t.Fatal(err)
	}
	var gotRequest SyncDNSZoneV2Request
	if err := gob.NewDecoder(&wire).Decode(&gotRequest); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotRequest, wantRequest) {
		t.Fatalf("request round trip got=%#v want=%#v", gotRequest, wantRequest)
	}

	wantResponse := SyncDNSZoneV2Response{Synced: true, AppliedGeneration: 19}
	wire.Reset()
	if err := gob.NewEncoder(&wire).Encode(wantResponse); err != nil {
		t.Fatal(err)
	}
	var gotResponse SyncDNSZoneV2Response
	if err := gob.NewDecoder(&wire).Decode(&gotResponse); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotResponse, wantResponse) {
		t.Fatalf("response round trip got=%#v want=%#v", gotResponse, wantResponse)
	}
}

func TestDNSZoneSyncV2DeleteWireContractKeepsExplicitZoneType(t *testing.T) {
	request := SyncDNSZoneV2Request{
		DesiredGeneration: 20,
		Domain:            "example.test",
		Delete:            true,
		ZoneType:          "NATIVE",
		Records:           []ZoneRecord{},
	}
	if request.DesiredGeneration != 20 || request.Domain == "" ||
		!request.Delete || request.ZoneType != "NATIVE" || request.Records == nil {
		t.Fatalf("delete request lost an effective field: %#v", request)
	}
}

func TestDNSZoneSyncV3WireContractBindsEngineAndEpoch(t *testing.T) {
	want := SyncDNSZoneV3Request{
		ServiceMutationBinding: ServiceMutationBinding{
			MutationRequestID: "request", MutationOwnerID: "owner",
		},
		Engine: DNSEngineBIND, EngineEpoch: 4, DesiredGeneration: 19,
		Domain: "example.test", ZoneType: "MASTER",
		Records: []ZoneRecord{{
			Name: "example.test", Type: "A", Content: "192.0.2.10", TTL: 300,
		}},
	}
	var wire bytes.Buffer
	if err := gob.NewEncoder(&wire).Encode(want); err != nil {
		t.Fatal(err)
	}
	var got SyncDNSZoneV3Request
	if err := gob.NewDecoder(&wire).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("request round trip got=%#v want=%#v", got, want)
	}
}

func TestDNSBackendReadinessWireContractIsBounded(t *testing.T) {
	want := DNSBackendReadinessResponse{Port53Conflict: true, Engines: []DNSBackendRuntimeState{
		{Engine: DNSEnginePowerDNS, Installed: true, Running: true, Managed: true, Unit: "pdns.service"},
		{Engine: DNSEngineBIND, Installed: true, Unit: "named.service"},
	}}
	var wire bytes.Buffer
	if err := gob.NewEncoder(&wire).Encode(want); err != nil {
		t.Fatal(err)
	}
	var got DNSBackendReadinessResponse
	if err := gob.NewDecoder(&wire).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("readiness round trip got=%#v want=%#v", got, want)
	}
	for _, value := range []DNSEngine{DNSEnginePowerDNS, DNSEngineBIND} {
		if !ValidDNSEngine(value) {
			t.Fatalf("valid engine rejected: %q", value)
		}
	}
	for _, value := range []DNSEngine{"", "powerdns", "BIND", " bind"} {
		if ValidDNSEngine(value) {
			t.Fatalf("invalid engine accepted: %q", value)
		}
	}
}

func TestDNSEngineSwitchWireContractPreservesManifest(t *testing.T) {
	want := SwitchDNSEngineV1Request{
		ServiceMutationBinding: ServiceMutationBinding{
			MutationRequestID: "request", MutationOwnerID: "owner",
		},
		Mode:         DNSEngineSwitchModeSwitch,
		SourceEngine: DNSEnginePowerDNS, TargetEngine: DNSEngineBIND,
		SourceEpoch: 2, TargetEpoch: 3, SourceRevision: 7,
		Topology:      DNSTopologyStandalone,
		SnapshotBytes: 123,
		Zones: []DNSEngineSwitchZoneSnapshot{{
			Ordinal: 0, Domain: "example.test", DesiredGeneration: 11,
			ZoneType: "NATIVE", ZoneQualifier: "dns-zone-sync/v3:sha256:digest",
			Records: []ZoneRecord{{Name: "example.test", Type: "A", Content: "192.0.2.2"}},
		}},
		ManifestQualifier: "dns-engine-switch/v1:sha256:digest",
	}
	var wire bytes.Buffer
	if err := gob.NewEncoder(&wire).Encode(want); err != nil {
		t.Fatal(err)
	}
	var got SwitchDNSEngineV1Request
	if err := gob.NewDecoder(&wire).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("switch round trip got=%#v want=%#v", got, want)
	}
}
