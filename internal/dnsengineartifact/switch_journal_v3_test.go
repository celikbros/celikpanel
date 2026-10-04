package dnsengineartifact

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/pdnsnative"
	"github.com/alicelik/celikpanel/internal/transport"
)

func freshPrimaryV3Fixture(t *testing.T) (JournalPolicy, SwitchJournalV1) {
	t.Helper()
	policy := journalTestPolicy()
	base, err := policy.DecodeSwitchJournal(journalFixture(t, "alpha81-pdns-switch"))
	if err != nil {
		t.Fatal(err)
	}
	commitment, err := mutationpayload.CanonicalDNSEngineSwitchManifestWithPairIdentity(
		transport.DNSEngineSwitchModeSwitch, "", transport.DNSEnginePowerDNS,
		0, 1, 0, transport.DNSTopologyPaired, transport.DNSPairRolePrimary,
		"192.0.2.10", "ns1.example.test", "192.0.2.11", "ns2.example.test", nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	base.SourceEngine = ""
	base.SourceEpoch, base.TargetEpoch, base.SourceRevision = 0, 1, 0
	base.Topology, base.PairRole = commitment.Topology, commitment.PairRole
	base.LocalIP, base.LocalNS, base.PeerIP, base.PeerNS = commitment.LocalIP, commitment.LocalNS, commitment.PeerIP, commitment.PeerNS
	base.ManifestQualifier, base.SnapshotBytes, base.Zones = commitment.Qualifier, commitment.SnapshotBytes, commitment.Zones
	base.PrimaryCatalogSerial = 1
	base.StateBefore = FileSnapshot{Path: policy.StatePath}
	base.SourceUnitsBefore = nil
	base.TargetUnitsBefore = []UnitSnapshot{{Name: "pdns.service", LoadState: "loaded", ActiveState: "inactive", UnitFileState: "disabled"}}
	if err := policy.ValidateSwitchJournal(base); err != nil {
		t.Fatal(err)
	}
	return policy, base
}

func TestFreshPrimaryV3EmptyIntentThenFrozenStage(t *testing.T) {
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
	if intent.PDNSFreshPlan.Candidate != nil || intent.PDNSCandidatePath == base.PDNSCandidatePath {
		t.Fatal("fresh intent created candidate evidence or used daemon-writable staging parent")
	}
	raw, err := policy.EncodeSwitchJournal(intent)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := policy.DecodeSwitchJournal(raw); err != nil {
		t.Fatal(err)
	}
	staged := pdnsnative.Snapshot{Schema: [][4]string{{"table", "domains", "domains", "CREATE TABLE domains(id INT)"}}, Tables: map[string][][]any{"domains": {}}}
	stage, err := policy.StagePDNSFreshPrimaryCandidateV3(intent, v4Candidate(policy, base.MutationRequestID), staged)
	if err != nil {
		t.Fatal(err)
	}
	if !SameImmutablePDNSFreshPrimaryPlanV3(intent, stage) || !ValidPDNSFreshPrimaryForwardPhaseTransitionV3(intent, stage) {
		t.Fatal("valid intent to stage transition rejected")
	}
	if _, err := policy.DecodeSwitchJournal(mustEncodeJournal(t, policy, stage)); err != nil {
		t.Fatal(err)
	}
	modified := stage
	plan := *stage.PDNSFreshPlan
	candidate := *plan.Candidate
	candidate.Inode++
	plan.Candidate = &candidate
	modified.PDNSFreshPlan = &plan
	if SameImmutablePDNSFreshPrimaryPlanV3(stage, modified) {
		t.Fatal("candidate inode changed after staged checkpoint")
	}
	if _, err := policy.EncodeSwitchJournal(modified); err == nil {
		t.Fatal("candidate identity changed without a matching digest")
	}
	preStart := stage
	preStart.Phase = SwitchPhaseTargetEnableIntent
	preStartPlan := *stage.PDNSFreshPlan
	preStartPlan.Native = &pdnsnative.RecordedTransition{Observed: pdnsnative.CatalogTransition{SourceSerial: 1, NativeSerial: 2, CatalogHash: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="}, LogicalSHA256: strings.Repeat("a", 64)}
	preStart.PDNSFreshPlan = &preStartPlan
	preStartPlan.NativeDigest, err = pdnsFreshNativeDigestV3(preStart)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := policy.EncodeSwitchJournal(preStart); err == nil {
		t.Fatal("native observation admitted before target start")
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	decoded["schema"] = "celikpanel-dns-engine-switch-journal/v5"
	unknown, err := json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := policy.DecodeSwitchJournal(unknown); err == nil {
		t.Fatal("unknown journal version accepted")
	}
}

func TestFreshPrimaryV3TargetUnitPreimageShapes(t *testing.T) {
	policy, base := freshPrimaryV3Fixture(t)
	after := cloneFileSnapshotsV2(base.ConfigBefore)
	for _, i := range []int{1, 2} {
		after[i] = FileSnapshot{Path: base.ConfigBefore[i].Path, Exists: true, Mode: 0o644, OwnerKnown: true, Data: []byte("managed=1\\n")}
		after[i].SHA256 = DigestBytes(after[i].Data)
	}
	cases := []struct {
		name string
		unit UnitSnapshot
		want bool
	}{
		{"installed-disabled", UnitSnapshot{Name: "pdns.service", LoadState: "loaded", ActiveState: "inactive", UnitFileState: "disabled"}, true},
		{"guard-owned-mask", UnitSnapshot{Name: "pdns.service", LoadState: "masked", ActiveState: "inactive", UnitFileState: "masked"}, true},
		{"runtime-mask", UnitSnapshot{Name: "pdns.service", LoadState: "masked", ActiveState: "inactive", UnitFileState: "masked-runtime"}, false},
		{"active-mask", UnitSnapshot{Name: "pdns.service", LoadState: "masked", ActiveState: "active", UnitFileState: "masked"}, false},
		{"mixed-mask", UnitSnapshot{Name: "pdns.service", LoadState: "loaded", ActiveState: "inactive", UnitFileState: "masked"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			candidate := base
			candidate.TargetUnitsBefore = []UnitSnapshot{tc.unit}
			journal, err := policy.BuildPDNSFreshPrimaryJournalV3(candidate, after)
			if (err == nil) != tc.want {
				t.Fatalf("build accepted=%t, want %t: %v", err == nil, tc.want, err)
			}
			if tc.want {
				raw, err := policy.EncodeSwitchJournal(journal)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := policy.DecodeSwitchJournal(raw); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
