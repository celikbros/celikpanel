package main

import (
	"context"
	"errors"
	"github.com/alicelik/celikpanel/internal/binddns"
	"testing"
)

func TestServerSetupSecondaryPrerequisiteUsesNativePrimaryCatalog(t *testing.T) {
	previous := probeServerSetupPrimaryCatalogSOA
	t.Cleanup(func() { probeServerSetupPrimaryCatalogSOA = previous })
	draft := serverSetupDraft{DNSMode: "local", DNSRole: "secondary", DNSEngine: "pdns", LocalIP: "192.0.2.20", PeerIP: "192.0.2.10", PanelDomain: "secondary.example.test"}
	catalog, err := binddns.CatalogDomain(draft.PeerIP)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	probeServerSetupPrimaryCatalogSOA = func(_ context.Context, endpoint, zone string) (uint32, error) {
		calls++
		if endpoint != "192.0.2.10:53" || zone != catalog {
			t.Fatalf("probe escaped reviewed native identity: %s %s", endpoint, zone)
		}
		return 12, nil
	}
	if err := serverSetupSecondaryPrerequisite(context.Background(), draft); err != nil || calls != 1 {
		t.Fatalf("native catalog rejected: calls=%d err=%v", calls, err)
	}
	for _, scenario := range []string{"unavailable", "missing-serial"} {
		probeServerSetupPrimaryCatalogSOA = func(context.Context, string, string) (uint32, error) {
			if scenario == "unavailable" {
				return 0, errors.New("connection refused")
			}
			return 0, nil
		}
		if err := serverSetupSecondaryPrerequisite(context.Background(), draft); !errors.Is(err, errServerSetupPrimaryDNSRequired) {
			t.Fatalf("%s must become primary prerequisite: %v", scenario, err)
		}
	}
}

func TestServerSetupSecondaryPrerequisiteDoesNotProbeOtherModesOrInvalidIdentity(t *testing.T) {
	previous := probeServerSetupPrimaryCatalogSOA
	t.Cleanup(func() { probeServerSetupPrimaryCatalogSOA = previous })
	probeServerSetupPrimaryCatalogSOA = func(context.Context, string, string) (uint32, error) {
		t.Fatal("unexpected network probe")
		return 0, nil
	}
	for _, draft := range []serverSetupDraft{
		{DNSMode: "local", DNSRole: "primary"},
		{DNSMode: "external", DNSRole: "secondary"},
		{DNSMode: "existing", DNSRole: "secondary"},
	} {
		if err := serverSetupSecondaryPrerequisite(context.Background(), draft); err != nil {
			t.Fatal(err)
		}
	}
	for _, draft := range []serverSetupDraft{
		{DNSMode: "local", DNSRole: "secondary", LocalIP: "192.0.2.20", PeerIP: "primary.example.test"},
		{DNSMode: "local", DNSRole: "secondary", LocalIP: "192.0.2.20", PeerIP: "192.0.2.20"},
		{DNSMode: "local", DNSRole: "secondary", LocalIP: "127.0.0.1", PeerIP: "192.0.2.10"},
	} {
		if err := serverSetupSecondaryPrerequisite(context.Background(), draft); err == nil || errors.Is(err, errServerSetupPrimaryDNSRequired) {
			t.Fatalf("invalid identity must not become waiting prerequisite: %v", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := serverSetupSecondaryPrerequisite(ctx, serverSetupDraft{DNSMode: "local", DNSRole: "secondary"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation became DNS unavailability: %v", err)
	}
}

func TestServerSetupSecondaryPrerequisitePreservesCancellationDuringProbe(t *testing.T) {
	previous := probeServerSetupPrimaryCatalogSOA
	t.Cleanup(func() { probeServerSetupPrimaryCatalogSOA = previous })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	probeServerSetupPrimaryCatalogSOA = func(context.Context, string, string) (uint32, error) {
		cancel()
		return 0, errors.New("connection closed")
	}
	draft := serverSetupDraft{DNSMode: "local", DNSRole: "secondary", LocalIP: "192.0.2.20", PeerIP: "192.0.2.10"}
	if err := serverSetupSecondaryPrerequisite(ctx, draft); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled operation became primary prerequisite: %v", err)
	}
}
