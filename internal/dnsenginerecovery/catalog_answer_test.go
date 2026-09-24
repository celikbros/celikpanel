package dnsenginerecovery

import (
	"context"
	"errors"
	"testing"

	"github.com/alicelik/celikpanel/internal/binddns"
)

func primaryCatalogFixture(t *testing.T) binddns.Receipt {
	t.Helper()
	catalog, err := binddns.CatalogDomain("192.0.2.10")
	if err != nil {
		t.Fatal(err)
	}
	return binddns.Receipt{Pairing: &binddns.PairingReceipt{
		Role: binddns.PairRolePrimary, LocalIP: "192.0.2.10",
		LocalCatalog: catalog, CatalogSerial: 17,
	}}
}

func TestProbePrimaryCatalogAnswerBindsLocalSerialTwice(t *testing.T) {
	receipt := primaryCatalogFixture(t)
	ownedCalls, queryCalls := 0, 0
	seen, err := ProbePrimaryCatalogAnswer(context.Background(), receipt,
		func(_ context.Context, ip string) (bool, error) {
			ownedCalls++
			if ip != "192.0.2.10" {
				t.Fatalf("unexpected address %s", ip)
			}
			return true, nil
		},
		func(_ context.Context, endpoint, zone string) (uint32, error) {
			queryCalls++
			if endpoint != "192.0.2.10:53" || zone != receipt.Pairing.LocalCatalog {
				t.Fatalf("unexpected query %s %s", endpoint, zone)
			}
			return 17, nil
		})
	if err != nil || !seen || ownedCalls != 2 || queryCalls != 2 {
		t.Fatalf("catalog answer: seen=%v err=%v owns=%d queries=%d", seen, err, ownedCalls, queryCalls)
	}
}

func TestProbePrimaryCatalogAnswerRejectsUnknownAndWrongEvidence(t *testing.T) {
	receipt := primaryCatalogFixture(t)
	goodOwner := func(context.Context, string) (bool, error) { return true, nil }
	goodQuery := func(context.Context, string, string) (uint32, error) { return 17, nil }
	for _, mutate := range []func(*binddns.Receipt){
		func(r *binddns.Receipt) { r.Pairing.LocalIP = "127.0.0.1" },
		func(r *binddns.Receipt) { r.Pairing.LocalCatalog = "other.invalid" },
		func(r *binddns.Receipt) { r.Pairing.CatalogSerial = 0 },
	} {
		copy := primaryCatalogFixture(t)
		mutate(&copy)
		if seen, err := ProbePrimaryCatalogAnswer(context.Background(), copy, goodOwner, goodQuery); err == nil || seen {
			t.Fatalf("bad receipt accepted: %+v", copy.Pairing)
		}
	}
	if seen, err := ProbePrimaryCatalogAnswer(context.Background(), receipt,
		func(context.Context, string) (bool, error) { return false, nil }, goodQuery); err == nil || seen {
		t.Fatal("foreign local address accepted")
	}
	if seen, err := ProbePrimaryCatalogAnswer(context.Background(), receipt, goodOwner,
		func(context.Context, string, string) (uint32, error) { return 18, nil }); err == nil || seen {
		t.Fatal("wrong catalog serial accepted")
	}
	if seen, err := ProbePrimaryCatalogAnswer(context.Background(), receipt, goodOwner,
		func(context.Context, string, string) (uint32, error) { return 0, errors.New("offline") }); err == nil || seen {
		t.Fatal("unknown network result accepted")
	}
	queries := 0
	if seen, err := ProbePrimaryCatalogAnswer(context.Background(), receipt, goodOwner,
		func(context.Context, string, string) (uint32, error) {
			queries++
			if queries == 2 {
				return 18, nil
			}
			return 17, nil
		}); err == nil || seen || queries != 2 {
		t.Fatal("changed catalog answer accepted")
	}
	unknownRole := primaryCatalogFixture(t)
	unknownRole.Pairing.Role = "unknown"
	if seen, err := ProbePrimaryCatalogAnswer(context.Background(), unknownRole, goodOwner, goodQuery); err == nil || seen {
		t.Fatal("unknown catalog role accepted")
	}
	if seen, err := ProbePrimaryCatalogAnswer(context.Background(), receipt,
		func(context.Context, string) (bool, error) { return false, errors.New("interface unavailable") }, goodQuery); err == nil || seen {
		t.Fatal("failed local address inspection accepted")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if seen, err := ProbePrimaryCatalogAnswer(cancelled, receipt, goodOwner, goodQuery); err == nil || seen {
		t.Fatal("cancelled catalog probe accepted")
	}
	standalone := binddns.Receipt{}
	if seen, err := ProbePrimaryCatalogAnswer(context.Background(), standalone, goodOwner, goodQuery); err != nil || seen {
		t.Fatalf("standalone should be non-applicable: %v %v", seen, err)
	}
}
