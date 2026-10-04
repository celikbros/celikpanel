package main

import (
	"bytes"
	"errors"
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/transport"
)

func runningBINDAdoptionInstallFixture(t *testing.T) (dnsEngineSwitchJournal, dnsEngineInstallOwnershipReceipt, []byte) {
	t.Helper()
	binding := adoptionTestBinding()
	receipt := dnsEngineInstallOwnershipReceipt{
		Schema: dnsEngineInstallOwnershipSchema,
		Engine: transport.DNSEngineBIND, PackageManager: "apt",
		Packages: []string{"bind9"}, MissingBefore: []string{},
		ManifestQualifier: adoptionTestManifest(t).Qualifier,
		MutationRequestID: binding.MutationRequestID,
		MutationOwnerID:   binding.MutationOwnerID, AdoptedPresent: true,
	}
	encoded, err := encodeDNSEngineInstallOwnership(receipt)
	if err != nil {
		t.Fatal(err)
	}
	journal := dnsEngineSwitchJournal{
		Schema:            dnsengineartifact.SwitchJournalSchemaV2,
		Phase:             dnsSwitchPhaseRollingBack,
		TargetEngine:      transport.DNSEngineBIND,
		MutationRequestID: binding.MutationRequestID,
		MutationOwnerID:   binding.MutationOwnerID,
		ManifestQualifier: receipt.ManifestQualifier,
		InversePlan: &dnsengineartifact.BINDSwitchInversePlanV2{
			SourceBIND: &dnsengineartifact.BINDAdoptionSourceProofV1{},
		},
	}
	return journal, receipt, encoded
}

func TestRetireRunningBINDAdoptionInstallExactAndIdempotent(t *testing.T) {
	journal, receipt, encoded := runningBINDAdoptionInstallFixture(t)
	stored := append([]byte(nil), encoded...)
	removals := 0
	read := func() ([]byte, bool, error) { return stored, stored != nil, nil }
	remove := func(want []byte) error {
		removals++
		if !bytes.Equal(want, encoded) {
			t.Fatal("wrong exact bytes passed to removal")
		}
		stored = nil
		return nil
	}
	for i := 0; i < 2; i++ {
		if err := retireRunningBINDAdoptionInstallWithOps(journal, receipt, read, remove); err != nil {
			t.Fatal(err)
		}
	}
	if removals != 1 {
		t.Fatalf("removals=%d, want one", removals)
	}
}

func TestRetireRunningBINDAdoptionInstallPreservesForeignEvidence(t *testing.T) {
	original, receipt, encoded := runningBINDAdoptionInstallFixture(t)
	tests := []struct {
		name string
		edit func(*dnsEngineSwitchJournal, *dnsEngineInstallOwnershipReceipt, *[]byte)
	}{
		{"wrong request", func(j *dnsEngineSwitchJournal, _ *dnsEngineInstallOwnershipReceipt, _ *[]byte) {
			j.MutationRequestID = "different"
		}},
		{"wrong phase", func(j *dnsEngineSwitchJournal, _ *dnsEngineInstallOwnershipReceipt, _ *[]byte) {
			j.Phase = dnsSwitchPhaseRolledBack
		}},
		{"missing source proof", func(j *dnsEngineSwitchJournal, _ *dnsEngineInstallOwnershipReceipt, _ *[]byte) {
			j.InversePlan.SourceBIND = nil
		}},
		{"installed package", func(_ *dnsEngineSwitchJournal, r *dnsEngineInstallOwnershipReceipt, _ *[]byte) {
			r.MissingBefore = []string{"bind9"}
			r.AdoptedPresent = false
		}},
		{"owner byte edit", func(_ *dnsEngineSwitchJournal, _ *dnsEngineInstallOwnershipReceipt, raw *[]byte) {
			*raw = append(*raw, ' ')
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			journal := original
			plan := *original.InversePlan
			journal.InversePlan = &plan
			want := receipt
			raw := append([]byte(nil), encoded...)
			tt.edit(&journal, &want, &raw)
			removed := false
			err := retireRunningBINDAdoptionInstallWithOps(journal, want,
				func() ([]byte, bool, error) { return raw, true, nil },
				func([]byte) error { removed = true; return nil })
			if err == nil || removed {
				t.Fatalf("err=%v removed=%v", err, removed)
			}
		})
	}
	if err := retireRunningBINDAdoptionInstallWithOps(original, receipt,
		func() ([]byte, bool, error) { return nil, false, errors.New("read failed") },
		func([]byte) error { t.Fatal("removed after read failure"); return nil }); err == nil {
		t.Fatal("read failure was suppressed")
	}
}
