package dnsenginerecovery

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanNativeInverseUsesFrozenJournalShape(t *testing.T) {
	policy, _, _, _ := inspectionFixture(t)
	for _, tc := range []struct {
		fixture string
		want    NativeInverseKind
	}{
		{"alpha81-bind.json", NativeInverseBINDSwitch},
		{"alpha81-pdns-adopt.json", NativeInversePDNSAdoption},
		{"alpha81-pdns-switch.json", NativeInversePDNSSwitch},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("..", "dnsengineartifact", "testdata", "switch-journal", tc.fixture))
			if err != nil {
				t.Fatal(err)
			}
			journal, err := policy.DecodeSwitchJournal(raw)
			if err != nil {
				t.Fatal(err)
			}
			got, err := PlanNativeInverse(journal)
			if err != nil || got != tc.want {
				t.Fatalf("inverse = %q, %v; want %q", got, err, tc.want)
			}
			if tc.fixture != "alpha81-bind.json" {
				return
			}
			for i := range journal.TargetUnitsBefore {
				journal.TargetUnitsBefore[i].LoadState = "loaded"
				journal.TargetUnitsBefore[i].ActiveState = "active"
				journal.TargetUnitsBefore[i].UnitFileState = "enabled"
			}
			if err := policy.ValidateSwitchJournal(journal); err != nil {
				t.Fatal(err)
			}
			got, err = PlanNativeInverse(journal)
			if err != nil || got != NativeInverseBINDRunningAdoption {
				t.Fatalf("running takeover = %q, %v", got, err)
			}
			journal.TargetUnitsBefore[0].ActiveState = "inactive"
			if _, err = PlanNativeInverse(journal); err == nil {
				t.Fatal("contradictory loaded BIND aliases selected an inverse")
			}
			journal.TargetUnitsBefore[0].ActiveState = "active"
			journal.ManifestQualifier = strings.Repeat("f", len(journal.ManifestQualifier))
			if _, err = PlanNativeInverse(journal); err == nil {
				t.Fatal("manifest mismatch selected an inverse")
			}
		})
	}
}
