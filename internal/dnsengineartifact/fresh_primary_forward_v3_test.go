package dnsengineartifact

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/alicelik/celikpanel/internal/pdnsnative"
)

func freshPrimaryForwardMeasuredFixtureV3(t *testing.T) (JournalPolicy, SwitchJournalV1, pdnsnative.Snapshot, pdnsnative.Snapshot, SwitchIdentity) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "deploy", "e2e", "dns-kill-matrix", "evidence", "pdns-master-bind-20260928", "debian-primary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var observed struct {
		Staged struct {
			SQLite pdnsnative.Snapshot `json:"sqlite"`
		} `json:"staged"`
		Running struct {
			SQLite pdnsnative.Snapshot `json:"sqlite"`
		} `json:"running"`
	}
	if err := json.Unmarshal(raw, &observed); err != nil {
		t.Fatal(err)
	}
	policy, base := freshPrimaryV3Fixture(t)
	after := cloneFileSnapshotsV2(base.ConfigBefore)
	for _, i := range []int{1, 2} {
		after[i] = FileSnapshot{Path: base.ConfigBefore[i].Path, Exists: true, Mode: 0o644, OwnerKnown: true, Data: []byte("managed=1\n")}
		after[i].SHA256 = DigestBytes(after[i].Data)
	}
	intent, err := policy.BuildPDNSFreshPrimaryJournalV3(base, after)
	if err != nil {
		t.Fatal(err)
	}
	staged, err := policy.StagePDNSFreshPrimaryCandidateV3(intent, v4Candidate(policy, base.MutationRequestID), observed.Staged.SQLite)
	if err != nil {
		t.Fatal(err)
	}
	staged.Phase = SwitchPhaseTargetEnableIntent
	if err := policy.ValidateSwitchJournal(staged); err != nil {
		t.Fatal(err)
	}
	id := SwitchIdentity{RequestID: staged.MutationRequestID, OwnerID: staged.MutationOwnerID, Target: staged.TargetEngine, Qualifier: staged.ManifestQualifier}
	return policy, staged, observed.Staged.SQLite, observed.Running.SQLite, id
}

func TestFreshPrimaryForwardV3PureMeasuredSequence(t *testing.T) {
	policy, j, _, live, id := freshPrimaryForwardMeasuredFixtureV3(t)
	plan, err := PlanObservedFreshPrimaryForwardV3(policy, j, id, live, nil, true, false)
	if err != nil || plan.Action != FreshPrimaryCheckpointStartedV3 {
		t.Fatalf("start: %v %+v", err, plan)
	}
	j = plan.NextJournal
	plan, err = PlanObservedFreshPrimaryForwardV3(policy, j, id, live, nil, true, false)
	if err != nil || plan.Action != FreshPrimaryCheckpointNativeV3 || plan.NextJournal.PDNSFreshPlan.Native == nil {
		t.Fatalf("native: %v %+v", err, plan)
	}
	j = plan.NextJournal
	if _, err := PlanObservedFreshPrimaryForwardV3(policy, j, id, live, nil, true, false); err == nil {
		t.Fatal("state publication without local/peer proof")
	}
	plan, err = PlanObservedFreshPrimaryForwardV3(policy, j, id, live, nil, true, true)
	if err != nil || plan.Action != FreshPrimaryPublishStateV3 || plan.DesiredState == nil {
		t.Fatalf("state: %v %+v", err, plan)
	}
	state := *plan.DesiredState
	if state.PrimaryCatalogSerial != 1790542951 || state.NativeCatalogV3 != NativeCatalogDebian49V3 {
		t.Fatal("native target state differs")
	}
	plan, err = PlanObservedFreshPrimaryForwardV3(policy, j, id, live, &state, true, true)
	if err != nil || plan.Action != FreshPrimaryCheckpointVerifiedV3 {
		t.Fatalf("verified: %v %+v", err, plan)
	}
	j = plan.NextJournal
	plan, err = PlanObservedFreshPrimaryForwardV3(policy, j, id, live, &state, true, true)
	if err != nil || plan.Action != FreshPrimaryCheckpointCommittedV3 {
		t.Fatalf("committed: %v %+v", err, plan)
	}
	j = plan.NextJournal
	plan, err = PlanObservedFreshPrimaryForwardV3(policy, j, id, live, &state, true, true)
	if err != nil || plan.Action != FreshPrimaryRetainCommittedV3 || plan.NextJournal.Phase != SwitchPhaseCommitted {
		t.Fatalf("retained: %v %+v", err, plan)
	}
}

func TestFreshPrimaryForwardV3RefusesForeignObservation(t *testing.T) {
	policy, j, _, live, id := freshPrimaryForwardMeasuredFixtureV3(t)
	wrongPhase := j
	wrongPhase.Phase = SwitchPhaseTargetStaged
	if _, err := PlanObservedFreshPrimaryForwardV3(policy, wrongPhase, id, live, nil, true, true); err == nil {
		t.Fatal("prestart phase accepted as native forward")
	}
	badID := id
	badID.OwnerID = "cccccccccccccccccccccccccccccccc"
	if _, err := PlanObservedFreshPrimaryForwardV3(policy, j, badID, live, nil, true, true); err == nil {
		t.Fatal("foreign owner accepted")
	}
	if _, err := PlanObservedFreshPrimaryForwardV3(policy, j, id, live, nil, false, true); err == nil {
		t.Fatal("unverified service accepted")
	}
	var bad pdnsnative.Snapshot
	raw, err := json.Marshal(live)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &bad); err != nil {
		t.Fatal(err)
	}
	bad.Tables["records"][len(bad.Tables["records"])-1][4] = "foreign invalid 1790542951 60 30 3600 30"
	if _, err := PlanObservedFreshPrimaryForwardV3(policy, j, id, bad, nil, true, true); err == nil {
		t.Fatal("foreign SQL accepted")
	}
	plan, err := PlanObservedFreshPrimaryForwardV3(policy, j, id, live, nil, true, true)
	if err != nil {
		t.Fatal(err)
	}
	j = plan.NextJournal
	plan, err = PlanObservedFreshPrimaryForwardV3(policy, j, id, live, nil, true, true)
	if err != nil {
		t.Fatal(err)
	}
	j = plan.NextJournal
	desired, err := FreshPrimaryTargetStateV3(j)
	if err != nil {
		t.Fatal(err)
	}
	desired.PrimaryCatalogSerial++
	if _, err := PlanObservedFreshPrimaryForwardV3(policy, j, id, live, &desired, true, true); err == nil {
		t.Fatal("foreign state serial accepted")
	}
}
