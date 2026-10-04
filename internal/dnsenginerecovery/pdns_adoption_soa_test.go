package dnsenginerecovery

import (
	"context"
	"errors"
	"testing"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

func TestProbePDNSAdoptionSOABindsBothTransportsAndReportsDeletionGap(t *testing.T) {
	manifest := mutationpayload.DNSEngineSwitchManifestCommitment{
		Mode:         transport.DNSEngineSwitchModeAdopt,
		TargetEngine: transport.DNSEnginePowerDNS,
		Zones: []transport.DNSEngineSwitchZoneSnapshot{
			{Domain: "example.test", Records: []transport.ZoneRecord{{Name: "example.test", Type: "SOA", Content: "ns1.example.test hostmaster.example.test 73 3600 600 604800 300"}}},
			{Domain: "deleted.test", Delete: true},
		},
	}
	var transports []string
	query := func(_ context.Context, network, endpoint, zone string) (uint32, error) {
		if endpoint != "192.0.2.10:53" || zone != "example.test" {
			t.Fatalf("unexpected query %s %s", endpoint, zone)
		}
		transports = append(transports, network)
		return 73, nil
	}
	active, deleted, err := ProbePDNSAdoptionSOA(context.Background(), "192.0.2.10:53", manifest, query)
	if err != nil || active != 1 || deleted != 1 || len(transports) != 2 || transports[0] != "udp" || transports[1] != "tcp" {
		t.Fatalf("unexpected authority proof: active=%d deleted=%d calls=%v err=%v", active, deleted, transports, err)
	}
	for _, network := range []string{"udp", "tcp"} {
		active, _, err = ProbePDNSAdoptionSOA(context.Background(), "192.0.2.10:53", manifest,
			func(_ context.Context, observed, _, _ string) (uint32, error) {
				if observed == network {
					return 72, nil
				}
				return 73, nil
			})
		if err == nil || active != 0 {
			t.Fatalf("%s wrong serial passed: active=%d err=%v", network, active, err)
		}
	}
	if _, _, err := ProbePDNSAdoptionSOA(context.Background(), "127.0.0.1:53", manifest, query); err == nil {
		t.Fatal("loopback endpoint accepted without public listener proof")
	}
	if _, _, err := ProbePDNSAdoptionSOA(context.Background(), "192.0.2.10:53", manifest,
		func(context.Context, string, string, string) (uint32, error) { return 0, errors.New("unavailable") }); err == nil {
		t.Fatal("DNS query failure accepted as proof")
	}
	manifest.Zones[0].Records[0].Content = "ns1.example.test hostmaster.example.test invalid 3600 600 604800 300"
	if _, _, err := ProbePDNSAdoptionSOA(context.Background(), "192.0.2.10:53", manifest, query); err == nil {
		t.Fatal("malformed frozen SOA admitted")
	}
}

func TestProbePDNSAdoptionDeletedSOARequiresBothTransportProofs(t *testing.T) {
	manifest := mutationpayload.DNSEngineSwitchManifestCommitment{
		Mode: transport.DNSEngineSwitchModeAdopt, TargetEngine: transport.DNSEnginePowerDNS,
		Zones: []transport.DNSEngineSwitchZoneSnapshot{{Domain: "gone.example.test", Delete: true}},
	}
	var calls []string
	query := func(_ context.Context, network, endpoint, zone string) error {
		if endpoint != "192.0.2.10:53" || zone != "gone.example.test" {
			t.Fatalf("unexpected negative query: %s %s", endpoint, zone)
		}
		calls = append(calls, network)
		return nil
	}
	verified, err := ProbePDNSAdoptionDeletedSOA(context.Background(), "192.0.2.10:53", manifest, query)
	if err != nil || verified != 1 || len(calls) != 2 || calls[0] != "udp" || calls[1] != "tcp" {
		t.Fatalf("deleted proof=%d calls=%v err=%v", verified, calls, err)
	}
	for _, failed := range []string{"udp", "tcp"} {
		verified, err := ProbePDNSAdoptionDeletedSOA(context.Background(), "192.0.2.10:53", manifest,
			func(_ context.Context, network, _, _ string) error {
				if network == failed {
					return errors.New("not proved")
				}
				return nil
			})
		if err == nil || verified != 0 {
			t.Fatalf("%s unavailable negative proof admitted: verified=%d err=%v", failed, verified, err)
		}
	}
	if _, err := ProbePDNSAdoptionDeletedSOA(context.Background(), "127.0.0.1:53", manifest, query); err == nil {
		t.Fatal("loopback negative endpoint was accepted")
	}
}
