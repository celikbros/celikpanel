//go:build linux

package main

import (
	"context"
	"reflect"
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
	"github.com/alicelik/celikpanel/internal/transport"
)

// The exact state pair2 t3 reached before the Agent's same-request recovery:
// a zero-zone fresh paired PowerDNS primary whose candidate build failed its
// catalog verification. The V3 journal is at intent (unsealed, no candidate
// proof), the unit is under the guard's mask with the packages installed, the
// configuration is still the preimage, and the only native effect is this
// operation's own temporary build file. Recovery is pre-start, so the Agent
// rolls it back by itself: durable decision, the build removed, rolled-back
// only after the pre-install state is proved again. Component test with
// injected native observations; not native evidence.
func TestFreshPrimaryV3FailedCatalogVerificationLeftoverRollsBackByItself(t *testing.T) {
	policy, intent, _ := freshPrimaryV3AgentFixture(t, freshPDNSGuardMaskV3)
	if len(intent.Zones) != 0 || intent.Phase != dnsSwitchPhaseIntent || intent.PDNSFreshPlan == nil || intent.PDNSFreshPlan.Candidate != nil {
		t.Fatalf("fixture is not the zero-zone unsealed intent: phase=%s zones=%d", intent.Phase, len(intent.Zones))
	}
	if !freshPrimaryPrestartJournalShapeV3(intent) {
		t.Fatal("the leftover intent is not routed to the pre-start inverse")
	}
	build := freshPrimaryV3BuildPathForTest(t, intent)
	fixture := &prestartObserverFixture{
		policy: policy, unit: freshPDNSGuardMaskV3,
		configs: allConfigs(dnsenginerecovery.PDNSTargetConfigBeforeV4),
		present: map[string]bool{build: true},
	}
	shape, err := assessFreshPrimaryPrestartV3(context.Background(), intent, fixture.observers())
	if err != nil || shape != freshPrimaryPrestartIntentPartialV3 {
		t.Fatalf("leftover assessed as shape=%d err=%v, want its own interrupted build", shape, err)
	}
	stored := intent
	var effects []string
	unexpected := func(name string) { t.Fatalf("pre-start inverse of an unsealed intent ran %s", name) }
	ops := freshPrimaryPrestartEffectsV3{
		read: func() (dnsEngineSwitchJournal, bool, error) { return stored, true, nil },
		assess: func(ctx context.Context, j dnsEngineSwitchJournal) (freshPrimaryPrestartShapeV3, error) {
			return assessFreshPrimaryPrestartV3(ctx, j, fixture.observers())
		},
		checkpoint: func(before, after dnsEngineSwitchJournal) error {
			if !reflect.DeepEqual(before, stored) {
				t.Fatal("checkpoint ignored the exact journal preimage")
			}
			if err := policy.ValidateSwitchJournal(after); err != nil {
				t.Fatalf("checkpoint %s is not a valid journal: %v", after.Phase, err)
			}
			stored = after
			effects = append(effects, "checkpoint:"+after.Phase)
			return nil
		},
		restoreUnit: func(context.Context, dnsengineartifact.UnitSnapshot, func(context.Context) error) error {
			unexpected("restoreUnit")
			return nil
		},
		restoreRenamed: func(dnsEngineSwitchJournal, func() error) error { unexpected("restoreRenamed"); return nil },
		restoreConfigs: func(context.Context, dnsEngineSwitchJournal, func(context.Context) error) error {
			unexpected("restoreConfigs")
			return nil
		},
		removeStaged: func(dnsEngineSwitchJournal, func() error) error { unexpected("removeStaged"); return nil },
		removePartial: func(j dnsEngineSwitchJournal, guard func() error) error {
			if j.Phase != dnsengineartifact.SwitchPhaseRollingBack {
				t.Fatalf("build removed before a durable rollback decision (phase %s)", j.Phase)
			}
			if err := guard(); err != nil {
				return err
			}
			delete(fixture.present, build)
			effects = append(effects, "remove-partial")
			return nil
		},
	}
	outcome, err := runFreshPrimaryPrestartInverseV3(context.Background(), intent, ops)
	if err != nil || outcome != dnsenginerecovery.OutcomeRolledBack {
		t.Fatalf("outcome=%s err=%v", outcome, err)
	}
	want := []string{"checkpoint:" + dnsengineartifact.SwitchPhaseRollingBack, "remove-partial", "checkpoint:" + dnsengineartifact.SwitchPhaseRolledBack}
	if !reflect.DeepEqual(effects, want) || stored.Phase != dnsengineartifact.SwitchPhaseRolledBack {
		t.Fatalf("effects=%v phase=%s, want %v", effects, stored.Phase, want)
	}
}

// Gate closed (as shipped) the fresh paired PowerDNS primary stays outside
// the Agent's rollback-evidence scope; open, only that exact first install
// enters.
func TestFreshPairedPDNSPrimaryRollbackEvidenceScopeFollowsTheGate(t *testing.T) {
	manifest := freshPairedPDNSPrimaryManifest(t)
	if initialDNSEngineInstallRollbackEvidenceScope(manifest) {
		t.Fatal("closed gate admitted the fresh paired PowerDNS primary rollback evidence")
	}
	openFreshPairedPDNSPrimaryGate(t)
	if !initialDNSEngineInstallRollbackEvidenceScope(manifest) {
		t.Fatal("open gate refused the fresh paired PowerDNS primary rollback evidence")
	}
	bindSource := testPairedPDNSSwitchManifest(t, transport.DNSPairRolePrimary, nil)
	if initialDNSEngineInstallRollbackEvidenceScope(bindSource) {
		t.Fatal("open gate admitted a BIND-source switch into first-install rollback evidence")
	}
	partial := manifest
	partial.PeerNS = ""
	if initialDNSEngineInstallRollbackEvidenceScope(partial) {
		t.Fatal("open gate admitted a partial pair identity")
	}
}
