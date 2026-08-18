package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/transport"
)

func TestDNSPrimaryPairReadyRequiresPeerCatalogSOAEvenWithoutMembers(t *testing.T) {
	evidence := dnsPrimaryCatalogEvidence{
		LocalIP: "192.0.2.10", PeerIP: "192.0.2.20",
		Domain: "catalog-c000020a.celikpanel.invalid", Serial: 7,
	}
	axfr := func(_ context.Context, address, domain string) (dnsCatalogAXFRResult, error) {
		if address != evidence.LocalIP || domain != evidence.Domain {
			return dnsCatalogAXFRResult{}, errors.New("unexpected catalog query")
		}
		return dnsCatalogAXFRResult{Serial: evidence.Serial, Members: []string{}}, nil
	}
	for _, test := range []struct {
		name       string
		peerSerial uint32
		peerErr    error
		wantReady  bool
	}{
		{name: "exact peer catalog", peerSerial: 7, wantReady: true},
		{name: "peer absent", peerErr: errors.New("unreachable")},
		{name: "stale peer catalog", peerSerial: 6},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := map[string]int{}
			soa := func(_ context.Context, network, address, domain string) (dnsSOAProbeResult, error) {
				calls[network]++
				if address != evidence.PeerIP || domain != evidence.Domain {
					return dnsSOAProbeResult{}, errors.New("unexpected SOA query")
				}
				if test.peerErr != nil {
					return dnsSOAProbeResult{}, test.peerErr
				}
				return dnsSOAProbeResult{
					Authoritative: true, RCode: dnsRCodeNoError,
					SOASerials: []uint32{test.peerSerial},
				}, nil
			}
			err := verifyDNSPrimaryPairReadyAt(
				context.Background(), evidence, soa, axfr,
			)
			if (err == nil) != test.wantReady {
				t.Fatalf("ready=%v err=%v", err == nil, err)
			}
			if test.wantReady && (calls["udp"] != 1 || calls["tcp"] != 1) {
				t.Fatalf("peer catalog SOA calls=%v", calls)
			}
		})
	}
}

func TestDNSPrimaryPairReadyProvesEveryMemberOnBothAuthorities(t *testing.T) {
	evidence := dnsPrimaryCatalogEvidence{
		LocalIP: "192.0.2.10", PeerIP: "192.0.2.20",
		Domain: "catalog-c000020a.celikpanel.invalid", Serial: 9,
		Members: []string{"example.test"},
	}
	axfr := func(_ context.Context, address, domain string) (dnsCatalogAXFRResult, error) {
		if address != evidence.LocalIP || domain != evidence.Domain {
			return dnsCatalogAXFRResult{}, errors.New("unexpected catalog query")
		}
		return dnsCatalogAXFRResult{Serial: 9, Members: []string{"example.test"}}, nil
	}
	for _, test := range []struct {
		name       string
		peerSerial uint32
		wantReady  bool
	}{
		{name: "exact member", peerSerial: 41, wantReady: true},
		{name: "stale member", peerSerial: 40},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := map[string]int{}
			soa := func(_ context.Context, network, address, domain string) (dnsSOAProbeResult, error) {
				calls[address+"/"+network+"/"+domain]++
				serial := uint32(9)
				if domain == "example.test" {
					serial = 41
					if address == evidence.PeerIP {
						serial = test.peerSerial
					}
				}
				return dnsSOAProbeResult{
					Authoritative: true, RCode: dnsRCodeNoError,
					SOASerials: []uint32{serial},
				}, nil
			}
			err := verifyDNSPrimaryPairReadyAt(
				context.Background(), evidence, soa, axfr,
			)
			if (err == nil) != test.wantReady {
				t.Fatalf("ready=%v err=%v calls=%v", err == nil, err, calls)
			}
			if test.wantReady {
				for _, address := range []string{evidence.LocalIP, evidence.PeerIP} {
					for _, network := range []string{"udp", "tcp"} {
						key := address + "/" + network + "/example.test"
						if calls[key] != 1 {
							t.Fatalf("member proof calls=%v; want one %s", calls, key)
						}
					}
				}
			}
		})
	}
}

func TestBINDPrimaryCatalogEvidenceRejectsSecondaryRole(t *testing.T) {
	receipt := binddns.Receipt{Pairing: &binddns.PairingReceipt{
		Role: binddns.PairRoleSecondary, LocalIP: "192.0.2.20",
		PeerIP: "192.0.2.10", CatalogSerial: 1,
	}}
	if _, primary, err := bindPrimaryCatalogEvidence(receipt); err != nil || primary {
		t.Fatalf("secondary evidence primary=%v err=%v", primary, err)
	}
}

func TestManagedPDNSPrimaryCatalogEvidenceRequiresExactProducer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pdns.sqlite3")
	t.Setenv("CELIKPANEL_PDNS_DB", path)
	db, err := initializePDNSEngineDB(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	applyPDNSTestZone(t, path, "example.test", 1)
	db, err = openPDNSEngineDB(path, false)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reconcilePDNSBINDCatalogTx(
		context.Background(), tx, true, "192.0.2.10",
	); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	oldConf := dnsClusterConf
	oldProof := dnsPairLocalProofAddress
	t.Cleanup(func() {
		dnsClusterConf = oldConf
		dnsPairLocalProofAddress = oldProof
	})
	dnsClusterConf = filepath.Join(t.TempDir(), "celikpanel-cluster.conf")
	dnsPairLocalProofAddress = func() (string, error) { return "192.0.2.10", nil }
	config := dnsClusterConfig(&transport.DNSClusterRequest{
		Role: dnsRolePaired, PeerIP: "192.0.2.20",
	})
	if err := os.WriteFile(dnsClusterConf, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	evidence, primary, err := managedPDNSPrimaryCatalogEvidence(context.Background())
	if err != nil || !primary {
		t.Fatalf("primary=%v evidence=%+v err=%v", primary, evidence, err)
	}
	if evidence.LocalIP != "192.0.2.10" || evidence.PeerIP != "192.0.2.20" ||
		evidence.Serial != 1 || len(evidence.Members) != 1 ||
		evidence.Members[0] != "example.test" {
		t.Fatalf("unexpected producer evidence=%+v", evidence)
	}

	db, err = openPDNSEngineDB(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM domains`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	peerCatalog, err := binddns.CatalogDomain("192.0.2.20")
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO domains(name,type,master,account) VALUES(?, 'CONSUMER', ?, ?)`,
		peerCatalog, "192.0.2.20", pdnsPeerCatalogAccount,
	); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, primary, err := managedPDNSPrimaryCatalogEvidence(context.Background()); err != nil || primary {
		t.Fatalf("PowerDNS consumer primary=%v err=%v", primary, err)
	}
}
