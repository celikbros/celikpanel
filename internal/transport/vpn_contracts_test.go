package transport

import (
	"bytes"
	"encoding/gob"
	"reflect"
	"testing"
)

func TestVPNPeerSyncContractPreservesGeneration(t *testing.T) {
	wantRequest := SyncVPNPeersRequest{
		ServiceMutationBinding: ServiceMutationBinding{
			MutationRequestID: "request",
			MutationOwnerID:   "owner",
		},
		DesiredGeneration: 17,
		Peers: []VPNPeerSpec{{
			PublicKey: "public", PresharedKey: "preshared", IP: "10.8.0.2",
		}},
	}
	var wire bytes.Buffer
	if err := gob.NewEncoder(&wire).Encode(wantRequest); err != nil {
		t.Fatal(err)
	}
	var gotRequest SyncVPNPeersRequest
	if err := gob.NewDecoder(&wire).Decode(&gotRequest); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotRequest, wantRequest) {
		t.Fatalf("request round trip got=%#v want=%#v", gotRequest, wantRequest)
	}

	wantResponse := SyncVPNPeersResponse{Applied: true, AppliedGeneration: 17}
	wire.Reset()
	if err := gob.NewEncoder(&wire).Encode(wantResponse); err != nil {
		t.Fatal(err)
	}
	var gotResponse SyncVPNPeersResponse
	if err := gob.NewDecoder(&wire).Decode(&gotResponse); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotResponse, wantResponse) {
		t.Fatalf("response round trip got=%#v want=%#v", gotResponse, wantResponse)
	}
}
