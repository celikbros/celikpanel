package main

import (
	"context"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func pdnsPeerV3Fixture() (scenario, identityReceipt, pdnsPeerV3StateEnvelope) {
	source := scenario{
		Schema: scenarioSchema, Driver: "bind", SourceFixture: "uninitialized", Mode: "switch",
		TargetEngine: transport.DNSEngineBIND, TargetEpoch: 1, Topology: transport.DNSTopologyPaired,
		PairRole: "primary", LocalIP: "192.0.2.10", PeerIP: "192.0.2.11",
		Zones: []transport.DNSEngineSwitchZoneSnapshot{{Domain: pdnsPeerV3Zone,
			DesiredGeneration: 1, ZoneType: "NATIVE", Records: []transport.ZoneRecord{
				{Name: pdnsPeerV3Zone, Type: "SOA", Content: "ns1.s1-kill.test hostmaster.s1-kill.test 2026083101 10800 3600 604800 3600", TTL: 3600},
				{Name: pdnsPeerV3Zone, Type: "NS", Content: "ns1.s1-kill.test", TTL: 3600},
				{Name: "ns1.s1-kill.test", Type: "A", Content: "192.0.2.10", TTL: 300},
				{Name: "www.s1-kill.test", Type: "A", Content: "192.0.2.10", TTL: 300},
				{Name: pdnsPeerV3Zone, Type: "NS", Content: "ns2.s1-kill.test", TTL: 3600},
				{Name: "ns2.s1-kill.test", Type: "A", Content: "192.0.2.11", TTL: 300},
			},
		}},
	}
	receipt := identityReceipt{Schema: identityReceiptSchema, CellID: pdnsPeerV3Cell,
		Driver: "bind", SourceFixture: "uninitialized", RequestID: strings.Repeat("a", 32),
		OwnerID: strings.Repeat("b", 32), ManifestQualifier: "dns-engine-switch/v1:sha256:" + strings.Repeat("c", 64)}
	state := pdnsPeerV3StateEnvelope{Schema: "celikpanel-dns-engine-state/v2"}
	state.Acquisition.Schema = "celikpanel-dns-engine-acquisition/v1"
	state.Acquisition.Mode = "switch"
	state.Acquisition.Engine = "bind"
	state.Acquisition.EngineEpoch = 1
	state.Acquisition.PairRole = "primary"
	state.Acquisition.LocalIP = "192.0.2.10"
	state.Acquisition.PeerIP = "192.0.2.11"
	state.Acquisition.ManifestQualifier = receipt.ManifestQualifier
	state.Acquisition.RequestID = receipt.RequestID
	state.Acquisition.OwnerID = receipt.OwnerID
	state.Publication.Schema = "celikpanel-dns-engine-publication/v1"
	state.Publication.CatalogSerial = 1
	return source, receipt, state
}

func TestPDNSPeerV3ExactZoneCycleRequests(t *testing.T) {
	source, receipt, state := pdnsPeerV3Fixture()
	for _, test := range []struct {
		step       string
		generation int64
		deleted    bool
		address    string
	}{
		{"edit", 2, false, "192.0.2.12"}, {"delete", 3, true, ""}, {"add", 4, false, "192.0.2.13"},
	} {
		request, begin, err := pdnsPeerV3Request(test.step, source, receipt, state)
		if err != nil {
			t.Fatalf("%s: %v", test.step, err)
		}
		if request.DesiredGeneration != test.generation || request.Delete != test.deleted ||
			request.MutationRequestID != begin.RequestID || request.MutationOwnerID != begin.OwnerID ||
			begin.PackageName == "" || begin.Kind != mutationKindDNSZoneSync {
			t.Fatalf("%s identity differs: %+v %+v", test.step, request, begin)
		}
		if test.deleted && len(request.Records) != 0 {
			t.Fatal("delete carries records")
		}
		if !test.deleted {
			found := false
			for _, record := range request.Records {
				if record.Name == "www.s1-kill.test" && record.Type == "A" {
					found = record.Content == test.address
				}
			}
			if !found {
				t.Fatalf("%s has wrong A record", test.step)
			}
		}
	}
	state.Acquisition.PeerIP = "192.0.2.99"
	if _, _, err := pdnsPeerV3Request("edit", source, receipt, state); err == nil {
		t.Fatal("foreign peer identity accepted")
	}
}

func TestPDNSPeerV3MissingPredecessorCannotBeginDelete(t *testing.T) {
	source, receipt, state := pdnsPeerV3Fixture()
	request, begin, err := pdnsPeerV3Request("delete", source, receipt, state)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	call := func(context.Context, string, any, any) error { called = true; return nil }
	_, _, _, err = runRPCPDNSPeerV3(context.Background(), "delete", request, begin, nil, call)
	if err == nil || called {
		t.Fatal("delete began without exact predecessor")
	}
}
