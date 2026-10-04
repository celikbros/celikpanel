//go:build linux

package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

type fakePDNSLocalEvidence struct {
	state    dnsEngineStateReceipt
	binding  pdnsPrimaryNativeBinding
	receipt  transport.DNSEngineSwitchZoneSnapshot
	catalog  dnsPrimaryCatalogEvidence
	onlyPDNS error
	// onBracket runs once per bracket, between the two binding reads.
	onBracket func(read int)
	reads     int
}

func (f *fakePDNSLocalEvidence) readers() pdnsPeerLocalEvidenceReaders {
	bindingReads := 0
	return pdnsPeerLocalEvidenceReaders{
		binding: func() (pdnsPrimaryNativeBinding, error) {
			bindingReads++
			return f.binding, nil
		},
		state: func() (dnsEngineStateReceipt, bool, error) { return f.state, true, nil },
		onlyPDNS: func() error {
			f.reads++
			if f.onBracket != nil {
				f.onBracket(f.reads)
			}
			return f.onlyPDNS
		},
		receipt: func(dnsEngineStateReceipt) (transport.DNSEngineSwitchZoneSnapshot, bool, error) {
			return f.receipt, true, nil
		},
		catalog: func(dnsEngineStateReceipt) (dnsPrimaryCatalogEvidence, bool, error) {
			return cloneDNSPrimaryCatalogEvidence(f.catalog), true, nil
		},
	}
}

func newFakePDNSLocalEvidence() (*fakePDNSLocalEvidence, dnsV3PrimaryPropagationPlan) {
	state := dnsEngineStateReceipt{
		Engine: transport.DNSEnginePowerDNS, EngineEpoch: 3, Generation: "generation-9",
		Mode: transport.DNSEngineSwitchModeSwitch, PairRole: transport.DNSPairRolePrimary,
	}
	evidence := testRestampEvidence(1790745229, testCatalogHashBefore, nil, nil)
	plan := dnsV3PrimaryPropagationPlan{
		SourceState: state,
		Evidence:    evidence,
		Changed:     expectedDNSZoneAuthority{Domain: "s2.s1-kill.test", Delete: true},
		Operation:   dnsV3DeletionOperation{Generation: 4, Qualifier: "dns-zone-sync/v3:sha256:" + strings.Repeat("c", 64)},
	}
	return &fakePDNSLocalEvidence{
		state:   state,
		binding: pdnsPrimaryNativeBinding{PID: 101, Start: "312", Executable: "/usr/bin/pdns_server", DatabaseDevice: 2049, DatabaseInode: 55, DatabaseSize: 4096, DatabaseMtimeSec: 10},
		receipt: transport.DNSEngineSwitchZoneSnapshot{Domain: plan.Changed.Domain, Delete: true,
			DesiredGeneration: 4, ZoneQualifier: plan.Operation.Qualifier},
		catalog: evidence,
	}, plan
}

// Each local recheck names the check that differed; a daemon re-stamp,
// also one written while the recheck ran, is admitted and re-stamps the
// record once the whole bracket held.
func TestPDNSLocalRecheckNamesCheckAndAdmitsRestamp(t *testing.T) {
	for _, tc := range []struct {
		name    string
		edit    func(*fakePDNSLocalEvidence)
		want    string
		serial  uint32
		retries int
	}{
		{"unchanged", func(*fakePDNSLocalEvidence) {}, "", 1790745229, 1},
		{"daemon re-stamp", func(f *fakePDNSLocalEvidence) {
			f.catalog.Serial, f.catalog.CatalogHash = 1790745288, testCatalogHashAfter
		}, "", 1790745288, 1},
		{"re-stamp written during the recheck", func(f *fakePDNSLocalEvidence) {
			f.onBracket = func(read int) {
				if read == 1 {
					f.binding.DatabaseSize, f.binding.DatabaseMtimeSec = 8192, 70
					f.catalog.Serial, f.catalog.CatalogHash = 1790745288, testCatalogHashAfter
				}
			}
		}, "", 1790745288, 2},
		{"half-written re-stamp then complete", func(f *fakePDNSLocalEvidence) {
			f.catalog.Serial = 1790745288
			f.onBracket = func(read int) {
				if read == 2 {
					f.catalog.CatalogHash = testCatalogHashAfter
				}
			}
		}, "", 1790745288, 2},
		{"engine state", func(f *fakePDNSLocalEvidence) { f.state.Generation = "generation-10" }, "engine_state", 1790745229, 0},
		{"active engine", func(f *fakePDNSLocalEvidence) { f.onlyPDNS = errors.New("named.service is active") }, "active_engine", 1790745229, 1},
		{"deletion receipt", func(f *fakePDNSLocalEvidence) { f.receipt.DesiredGeneration = 5 }, "deletion_receipt", 1790745229, 1},
		{"producer catalog", func(f *fakePDNSLocalEvidence) {
			f.catalog.Serial, f.catalog.CatalogHash = 1790745288, testCatalogHashAfter
			f.catalog.Members, f.catalog.MemberSerials = []string{"owner.example.test"}, []uint32{1}
		}, "producer_catalog", 1790745229, 1},
		{"daemon restarted", func(f *fakePDNSLocalEvidence) {
			f.onBracket = func(int) { f.binding.PID++ }
		}, "native_binding", 1790745229, 1},
		{"database written on every read", func(f *fakePDNSLocalEvidence) {
			f.onBracket = func(read int) { f.binding.DatabaseMtimeSec = int64(100 + read) }
		}, "native_binding", 1790745229, pdnsPeerLocalRecheckReads},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := captureAgentLog(t)
			fake, plan := newFakePDNSLocalEvidence()
			tc.edit(fake)
			record := newDNSRecordedProducerCatalog(plan)
			err := recheckPDNSPeerLocalEvidenceAt(context.Background(), fake.readers(), plan, record)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("recheck: %v", err)
				}
			} else {
				var edit *dnsPeerOwnerEditError
				if !errors.As(err, &edit) || edit.check != tc.want {
					t.Fatalf("check=%v want %s", err, tc.want)
				}
				code := pendingDNSPeerCode(peerCurrentPendingCodeAt(
					func() error { return nil }, func() error { return nil }, func() error { return err }))
				if code != transport.DNSPeerPendingOwnerEditUnknown+":"+tc.want ||
					!strings.Contains(logs.String(), "check="+tc.want) {
					t.Fatalf("code=%q log=%q", code, logs.String())
				}
			}
			if got := record.Evidence().Serial; got != tc.serial || fake.reads != tc.retries {
				t.Fatalf("recorded serial=%d want %d; brackets=%d want %d", got, tc.serial, fake.reads, tc.retries)
			}
		})
	}
}
