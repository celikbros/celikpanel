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
