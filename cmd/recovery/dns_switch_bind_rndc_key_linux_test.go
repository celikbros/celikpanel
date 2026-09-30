//go:build linux

package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/bindrndckey"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
)

func TestOwnerBINDInverseRemovesOnlyThisSwitchsUnchangedRNDCKey(t *testing.T) {
	j := dnsengineartifact.SwitchJournalV1{
		ManifestQualifier: "dns-engine-switch/v1:sha256:" + strings.Repeat("a", 64),
		MutationRequestID: strings.Repeat("1", 32), MutationOwnerID: strings.Repeat("2", 32),
	}
	product := bindrndckey.Record{
		Schema: bindrndckey.RecordSchema, Path: bindrndckey.APTKeyPath,
		Provenance: bindrndckey.ProvenanceProductCreated, SHA256: strings.Repeat("c", 64),
		ManifestQualifier: j.ManifestQualifier, MutationRequestID: j.MutationRequestID, MutationOwnerID: j.MutationOwnerID,
	}
	owner := product
	owner.Provenance, owner.SHA256, owner.Basis = bindrndckey.ProvenanceOwnerOrPackage, "", bindrndckey.BasisKeyPresent
	for _, tc := range []struct {
		name    string
		record  *bindrndckey.Record
		outcome bindrndckey.RemovalOutcome
		removes bool
		kind    string
	}{
		{"product-unchanged", &product, bindrndckey.RemovalRemoved, true, bindRNDCKeyRemoved},
		{"product-changed", &product, bindrndckey.RemovalKeptChanged, true, bindRNDCKeyKeptChanged},
		{"package-key", &owner, "", false, bindRNDCKeyNotOurs},
		{"no-record", nil, "", false, bindRNDCKeyNotOurs},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			got := retireBINDRNDCKeyForOwnerInverseWithOps(j, bindRNDCKeyCLIOps{
				read: func() (bindrndckey.Record, bool, error) {
					if tc.record == nil {
						return bindrndckey.Record{}, false, nil
					}
					return *tc.record, true, nil
				},
				remove: func(string, string) (bindrndckey.RemovalOutcome, error) {
					calls++
					return tc.outcome, nil
				},
			})
			if (calls == 1) != tc.removes || got.Kind != tc.kind {
				t.Fatalf("calls=%d got=%+v", calls, got)
			}
		})
	}
	if got := retireBINDRNDCKeyForOwnerInverseWithOps(j, bindRNDCKeyCLIOps{
		read:   func() (bindrndckey.Record, bool, error) { return product, true, nil },
		remove: func(string, string) (bindrndckey.RemovalOutcome, error) { return "", errors.New("busy") },
	}); got.Kind != bindRNDCKeyKeptUnknown {
		t.Fatalf("failed removal: %+v", got)
	}
}

func TestOwnerBINDInverseSummaryStatesTheRNDCKeyOutcome(t *testing.T) {
	record := &bindRollbackRecord{createdBIND: true, target: bindTargetGuardMask}
	record.observeRNDCKey(bindRNDCKeyCLIOutcome{Kind: bindRNDCKeyRemoved, Path: "/etc/bind/rndc.key"})
	out := bindRollbackSummaryText("en", record)
	if !strings.Contains(out, "Removed: the rndc key /etc/bind/rndc.key that this switch created; it was unchanged.") ||
		strings.Contains(out, "the rndc key and the BIND install-ownership record") {
		t.Fatalf("removed summary:\n%s", out)
	}
	record.observeRNDCKey(bindRNDCKeyCLIOutcome{Kind: bindRNDCKeyKeptChanged, Path: "/etc/bind/rndc.key"})
	out = bindRollbackSummaryText("tr", record)
	if !strings.Contains(out, "sizin anahtarınız sayılır") || !strings.Contains(out, "rndc anahtarı ve BIND kurulum sahipliği kaydı") {
		t.Fatalf("kept summary:\n%s", out)
	}
}
