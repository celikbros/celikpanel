//go:build linux

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

func TestInspectLegacyPowerDNSDurableAuthorityUsesPersistedState(t *testing.T) {
	for _, test := range []struct {
		name      string
		engine    transport.DNSEngine
		wantError bool
	}{
		{name: "PowerDNS active", engine: transport.DNSEnginePowerDNS},
		{name: "BIND remains authoritative while stopped", engine: transport.DNSEngineBIND, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := mutationTestRoot(t)
			t.Setenv("CELIKPANEL_AGENT_STATE_DIR", root)
			encoded, err := encodeDNSEngineState(legacyDurableDNSState(test.engine))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(dnsEngineStatePath(), encoded, 0o600); err != nil {
				t.Fatal(err)
			}
			err = inspectLegacyPowerDNSDurableAuthorityOnHost(false)
			if test.wantError && err == nil {
				t.Fatal("persisted BIND authority unexpectedly allowed PowerDNS")
			}
			if !test.wantError && err != nil {
				t.Fatalf("persisted PowerDNS authority rejected: %v", err)
			}
		})
	}
}

func TestInspectLegacyPowerDNSDurableAuthorityJournalTakesPrecedence(t *testing.T) {
	journal := testBINDSwitchJournal(t)
	encodedJournal, err := encodeDNSEngineSwitchJournal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dnsEngineSwitchJournalPath(), encodedJournal, 0o600); err != nil {
		t.Fatal(err)
	}
	encodedState, err := encodeDNSEngineState(
		legacyDurableDNSState(transport.DNSEnginePowerDNS),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(serviceMutationStateDirectory(), "dns-engine-state.json"),
		encodedState, 0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := inspectLegacyPowerDNSDurableAuthorityOnHost(false); err == nil {
		t.Fatal("active persisted switch journal unexpectedly allowed PowerDNS")
	}
}

func TestTuplelessPowerDNSConsumerLegacyMutationGuardPrecedesRuntime(t *testing.T) {
	root := mutationTestRoot(t)
	t.Setenv("CELIKPANEL_AGENT_STATE_DIR", root)
	prepareManagedPDNSCatalogConfig(t)
	peerDomain, err := binddns.CatalogDomain("192.0.2.20")
	if err != nil {
		t.Fatal(err)
	}
	oldAXFR := probeDNSCatalogAXFR
	probeDNSCatalogAXFR = func(
		_ context.Context, address, domain string,
	) (dnsCatalogAXFRResult, error) {
		if address != "192.0.2.20" || domain != peerDomain {
			t.Fatalf("peer catalog tuple=%s/%s", address, domain)
		}
		return dnsCatalogAXFRResult{Serial: 7, Members: []string{}}, nil
	}
	t.Cleanup(func() { probeDNSCatalogAXFR = oldAXFR })
	consumerPath := filepath.Join(t.TempDir(), "tupleless-consumer.sqlite3")
	if err := buildPDNSSwitchCandidate(
		context.Background(), consumerPath,
		testPairedPDNSSwitchManifest(t, transport.DNSPairRoleSecondary, nil),
		testPDNSEngineBinding(),
	); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CELIKPANEL_PDNS_DB", consumerPath)
	encoded, err := encodeDNSEngineState(
		legacyDurableDNSState(transport.DNSEnginePowerDNS),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dnsEngineStatePath(), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	dbBefore, err := os.ReadFile(consumerPath)
	if err != nil {
		t.Fatal(err)
	}
	configBefore, err := os.ReadFile(dnsClusterConf)
	if err != nil {
		t.Fatal(err)
	}
	oldMutation := legacyPowerDNSMutationAuthorityCheck
	oldRuntime := legacyPowerDNSRuntimeSafetyCheck
	runtimeCalls := 0
	legacyPowerDNSMutationAuthorityCheck = inspectLegacyPowerDNSMutationAuthorityOnHost
	legacyPowerDNSRuntimeSafetyCheck = func(context.Context, bool) error {
		runtimeCalls++
		return nil
	}
	t.Cleanup(func() {
		legacyPowerDNSMutationAuthorityCheck = oldMutation
		legacyPowerDNSRuntimeSafetyCheck = oldRuntime
	})
	if err := requireLegacyPowerDNSMutationSafe(context.Background(), true); err == nil {
		t.Fatal("tuple-less consumer reached a legacy mutation")
	}
	if runtimeCalls != 0 {
		t.Fatalf("tuple-less consumer rejection performed %d runtime inspections", runtimeCalls)
	}
	dbAfter, err := os.ReadFile(consumerPath)
	if err != nil {
		t.Fatal(err)
	}
	configAfter, err := os.ReadFile(dnsClusterConf)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(dbBefore, dbAfter) || !bytes.Equal(configBefore, configAfter) {
		t.Fatal("tuple-less consumer rejection changed PowerDNS authority")
	}
}

func TestTuplelessPowerDNSProducerLegacyMutationGuardRequiresExactAuthority(t *testing.T) {
	prepareManagedPDNSCatalogConfig(t)
	domain := "legacy-guard.test"
	zone, err := mutationpayload.CanonicalDNSZoneSyncV3(
		transport.DNSEnginePowerDNS, 4, 1, domain, false, "MASTER",
		testPDNSEngineRecords(domain),
	)
	if err != nil {
		t.Fatal(err)
	}
	manifest := testPairedPDNSSwitchManifest(
		t, transport.DNSPairRolePrimary,
		[]transport.DNSEngineSwitchZoneSnapshot{{
			Domain: domain, DesiredGeneration: 1, ZoneType: "MASTER",
			Records: zone.Records, ZoneQualifier: zone.Qualifier,
		}},
	)
	path := filepath.Join(t.TempDir(), "tupleless-producer.sqlite3")
	if err := buildPDNSSwitchCandidateWithPrimaryCatalogSerial(
		context.Background(), path, manifest, testPDNSEngineBinding(), 41,
	); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CELIKPANEL_PDNS_DB", path)
	if err := validateTuplelessPowerDNSLegacyMutationAuthority(); err != nil {
		t.Fatalf("exact tuple-less producer rejected: %v", err)
	}
	db, err := openPDNSEngineDB(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO domains(name,type,account) VALUES('ambiguous.test','PRODUCER',?)`, pdnsBINDCatalogAccount); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := validateTuplelessPowerDNSLegacyMutationAuthority(); err == nil {
		t.Fatal("ambiguous tuple-less producer authority was accepted")
	}
}
