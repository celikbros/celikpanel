//go:build linux

package dnsenginerecovery

import (
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/pdnsnative"
)

func TestFreshPrimaryRollbackPhaseV3RejectsPoststartAndEditedEvidence(t *testing.T) {
	base := dnsengineartifact.SwitchJournalV1{
		Schema: dnsengineartifact.SwitchJournalSchemaV3,
		PDNSFreshPlan: &dnsengineartifact.PDNSFreshPrimaryPlanV3{
			Kind: dnsengineartifact.PDNSFreshPrimaryPlanKindV3,
		},
	}
	for _, pair := range [][2]string{
		{dnsengineartifact.SwitchPhaseIntent, dnsengineartifact.SwitchPhaseRollingBack},
		{dnsengineartifact.SwitchPhaseTargetStaged, dnsengineartifact.SwitchPhaseRollingBack},
		{dnsengineartifact.SwitchPhaseTargetEnableIntent, dnsengineartifact.SwitchPhaseRollingBackTargetEnable},
		{dnsengineartifact.SwitchPhaseRollingBack, dnsengineartifact.SwitchPhaseRolledBack},
		{dnsengineartifact.SwitchPhaseRollingBackTargetEnable, dnsengineartifact.SwitchPhaseRolledBack},
	} {
		before, after := base, base
		before.Phase, after.Phase = pair[0], pair[1]
		if !freshPrimaryRollbackPhaseV3(before, after) {
			t.Fatalf("safe prestart phase denied: %v", pair)
		}
		edited := after
		plan := *after.PDNSFreshPlan
		plan.IntentDigest = "owner-edit"
		edited.PDNSFreshPlan = &plan
		if freshPrimaryRollbackPhaseV3(before, edited) {
			t.Fatal("edited V3 plan authorized rollback")
		}
	}
	for _, phase := range []string{dnsengineartifact.SwitchPhaseTargetStarted, dnsengineartifact.SwitchPhaseTargetVerified, dnsengineartifact.SwitchPhaseCommitted} {
		before, after := base, base
		before.Phase, after.Phase = phase, dnsengineartifact.SwitchPhaseRollingBack
		if freshPrimaryRollbackPhaseV3(before, after) {
			t.Fatalf("poststart phase %s entered destructive inverse", phase)
		}
	}
	before, after := base, base
	before.Phase, after.Phase = dnsengineartifact.SwitchPhaseTargetEnableIntent, dnsengineartifact.SwitchPhaseRollingBackTargetEnable
	plan := *before.PDNSFreshPlan
	plan.Native = &pdnsnative.RecordedTransition{LogicalSHA256: "observed"}
	before.PDNSFreshPlan, after.PDNSFreshPlan = &plan, &plan
	if freshPrimaryRollbackPhaseV3(before, after) {
		t.Fatal("native-observed target entered inverse")
	}
}
