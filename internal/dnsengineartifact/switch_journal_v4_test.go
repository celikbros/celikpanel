package dnsengineartifact

import (
	"bytes"
	"github.com/alicelik/celikpanel/internal/bindconfig"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
	"strings"
	"testing"
)

func v4PDNSTargetFixture(t *testing.T) (JournalPolicy, SwitchJournalV1) {
	t.Helper()
	policy := journalTestPolicy()
	base, err := policy.DecodeSwitchJournal(journalFixture(t, "alpha81-pdns-switch"))
	if err != nil {
		t.Fatal(err)
	}
	base.TargetUnitsBefore[0].LoadState = "loaded"
	base.TargetUnitsBefore[0].UnitFileState = "disabled"
	state, exists, err := SourceStateFromSwitchJournal(base)
	if err != nil || !exists {
		t.Fatal(err)
	}
	after := cloneFileSnapshotsV2(base.ConfigBefore)
	after[2] = FileSnapshot{
		Path: policy.PDNSManagedPath, Exists: true, Mode: 0o644, OwnerKnown: true,
		UID: 0, GID: 0, Data: []byte("launch=gsqlite3\n"),
	}
	after[2].SHA256 = DigestBytes(after[2].Data)
	source := ManagedBINDSourceProofV4{
		Kind: ManagedBINDSourceProofKindV4, HostLayout: "apt",
		Generation: state.Generation, EngineEpoch: state.EngineEpoch, ReceiptSHA256: strings.Repeat("e", 64),
		ConfigBefore: []FileSnapshot{
			v2BINDConfigSnapshot("/etc/bind/named.conf", 0o644, 42, []byte("include \"/etc/bind/named.conf.options\";\ninclude \"/etc/bind/named.conf.local\";\ninclude \"/etc/bind/named.conf.default-zones\";\n")),
			v2BINDConfigSnapshot("/etc/bind/named.conf.default-zones", 0o644, 42, []byte("// default\n")),
			v4ManagedLocal(t),
			v2BINDConfigSnapshot("/etc/bind/named.conf.options", 0o644, 42, []byte("// options\n")),
		},
	}
	journal, err := policy.BuildPDNSTargetInverseJournalV4(base, after, source, v4Candidate(policy, base.MutationRequestID))
	if err != nil {
		t.Fatal(err)
	}
	return policy, journal
}

func v4Candidate(policy JournalPolicy, request string) PDNSTargetCandidateProofV4 {
	return PDNSTargetCandidateProofV4{
		Path: policy.pdnsCandidatePathV4(request), Device: 9, Inode: 11,
		Mode: 0o640, UID: 0, GID: 42, Size: 4096,
		SHA256: strings.Repeat("f", 64), NoSidecars: true,
	}
}

func TestPDNSTargetV4IntentStageAndPhaseCodec(t *testing.T) {
	policy, intent := v4PDNSTargetFixture(t)
	if intent.PDNSCandidatePath != policy.pdnsCandidatePathV4(intent.MutationRequestID) ||
		intent.PDNSCandidatePath == policy.pdnsCandidatePath(intent.MutationRequestID) {
		t.Fatal("V4 candidate was not isolated in the agent's private state parent")
	}
	raw, err := policy.EncodeSwitchJournal(intent)
	if err != nil {
		t.Fatal(err)
	}
	read, err := policy.DecodeSwitchJournal(raw)
	if err != nil || !bytes.Equal(raw, mustEncodeJournal(t, policy, read)) {
		t.Fatalf("intent round trip: %v", err)
	}
	stage, err := policy.AttachPDNSTargetCandidateV4(intent, v4Candidate(policy, intent.MutationRequestID))
	if err != nil || !SameImmutablePDNSTargetInversePlanV4(intent, stage) {
		t.Fatalf("stage transition: %v", err)
	}
	for _, phase := range []string{SwitchPhaseTargetStaged, SwitchPhaseSourceStopped, SwitchPhaseTargetEnableIntent, SwitchPhaseTargetStarted, SwitchPhaseTargetVerified, SwitchPhaseCommitted, SwitchPhaseRollingBack, SwitchPhaseRollingBackTargetEnable, SwitchPhaseRolledBack} {
		next := stage
		next.Phase = phase
		encoded, e := policy.EncodeSwitchJournal(next)
		if e != nil {
			t.Fatalf("%s: %v", phase, e)
		}
		decoded, e := policy.DecodeSwitchJournal(encoded)
		if e != nil || !SameImmutablePDNSTargetInversePlanV4(stage, decoded) {
			t.Fatalf("%s round trip: %v", phase, e)
		}
	}
	premature := intent
	premature.Phase = SwitchPhaseSourceStopped
	if _, err := policy.EncodeSwitchJournal(premature); err == nil {
		t.Fatal("source stop without staged target accepted")
	}
}

func TestPDNSTargetV4IntentMustFreezeCandidate(t *testing.T) {
	policy, intent := v4PDNSTargetFixture(t)
	base, err := policy.DecodeSwitchJournal(journalFixture(t, "alpha81-pdns-switch"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := policy.BuildPDNSTargetInverseJournalV4(
		base, intent.PDNSTargetPlan.ConfigAfter, *intent.PDNSTargetPlan.SourceBIND,
		PDNSTargetCandidateProofV4{},
	); err == nil {
		t.Fatal("candidate-less V4 intent accepted")
	}

	raw, err := policy.EncodeSwitchJournal(intent)
	if err != nil {
		t.Fatal(err)
	}
	missing, err := policy.DecodeSwitchJournal(raw)
	if err != nil {
		t.Fatal(err)
	}
	missing.PDNSTargetPlan.Candidate = nil
	if _, err := policy.EncodeSwitchJournal(missing); err == nil {
		t.Fatal("persisted V4 intent without candidate accepted")
	}
	tampered, err := policy.DecodeSwitchJournal(raw)
	if err != nil {
		t.Fatal(err)
	}
	tampered.PDNSTargetPlan.Candidate.Inode++
	if _, err := policy.EncodeSwitchJournal(tampered); err == nil {
		t.Fatal("candidate change after intent accepted")
	}
	premature, err := policy.DecodeSwitchJournal(raw)
	if err != nil {
		t.Fatal(err)
	}
	premature.PDNSTargetPlan.StagedDigest = strings.Repeat("a", 64)
	if _, err := policy.EncodeSwitchJournal(premature); err == nil {
		t.Fatal("intent claimed a staged phase")
	}
	wrong := v4Candidate(policy, intent.MutationRequestID)
	wrong.Inode++
	if _, err := policy.AttachPDNSTargetCandidateV4(intent, wrong); err == nil {
		t.Fatal("stage transition replaced frozen candidate")
	}
}
func TestPDNSTargetV4RejectsPresentConfigMetadataChangeBeforeIntent(t *testing.T) {
	policy, intent := v4PDNSTargetFixture(t)
	base, err := policy.DecodeSwitchJournal(journalFixture(t, "alpha81-pdns-switch"))
	if err != nil {
		t.Fatal(err)
	}
	base.TargetUnitsBefore[0].LoadState = "loaded"
	base.TargetUnitsBefore[0].UnitFileState = "disabled"
	after := cloneFileSnapshotsV2(intent.PDNSTargetPlan.ConfigAfter)
	found := false
	for i := range after {
		if after[i].Path == policy.PDNSMainPath {
			if !base.ConfigBefore[i].Exists || !after[i].Exists {
				t.Fatal("test requires present PowerDNS main config")
			}
			after[i].GID = base.ConfigBefore[i].GID + 1
			found = true
		}
	}
	if !found {
		t.Fatal("PowerDNS main config was missing")
	}
	if _, err := policy.BuildPDNSTargetInverseJournalV4(
		base, after, *intent.PDNSTargetPlan.SourceBIND,
		v4Candidate(policy, base.MutationRequestID),
	); err == nil || !strings.Contains(err.Error(), "metadata transition") {
		t.Fatalf("present-file metadata change passed V4 admission: %v", err)
	}
}
func TestPDNSTargetV4RejectsUnsupportedScopeAndTampering(t *testing.T) {
	policy, intent := v4PDNSTargetFixture(t)
	stage, err := policy.AttachPDNSTargetCandidateV4(intent, v4Candidate(policy, intent.MutationRequestID))
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*SwitchJournalV1){
		"paired":               func(j *SwitchJournalV1) { j.Topology = "paired" },
		"target-active":        func(j *SwitchJournalV1) { j.TargetUnitsBefore[0].ActiveState = "active" },
		"target-not-installed": func(j *SwitchJournalV1) { j.TargetUnitsBefore[0].LoadState = "not-found" },
		"target-autostarts":    func(j *SwitchJournalV1) { j.TargetUnitsBefore[0].UnitFileState = "enabled" },
		"named-not-running":    func(j *SwitchJournalV1) { j.SourceUnitsBefore[1].ActiveState = "inactive" },
		"prior-database":       func(j *SwitchJournalV1) { j.PDNSBackupSHA256 = strings.Repeat("a", 64); j.PDNSBackupSize = 4096 },
		"source-missing":       func(j *SwitchJournalV1) { j.PDNSTargetPlan.SourceBIND = nil },
		"source-generation":    func(j *SwitchJournalV1) { j.PDNSTargetPlan.SourceBIND.Generation = strings.Repeat("a", 64) },
		"source-config-owner":  func(j *SwitchJournalV1) { j.PDNSTargetPlan.SourceBIND.ConfigBefore[0].UID = 33 },
		"target-config-bytes":  func(j *SwitchJournalV1) { j.PDNSTargetPlan.ConfigAfter[2].Data = []byte("owner edit") },
		"target-config-rehashed": func(j *SwitchJournalV1) {
			j.PDNSTargetPlan.ConfigAfter[2].Data = []byte("owner edit")
			j.PDNSTargetPlan.ConfigAfter[2].SHA256 = DigestBytes(j.PDNSTargetPlan.ConfigAfter[2].Data)
		},
		"candidate-path": func(j *SwitchJournalV1) { j.PDNSTargetPlan.Candidate.Path = "/tmp/foreign" },
		"legacy-candidate-path": func(j *SwitchJournalV1) {
			j.PDNSCandidatePath = policy.pdnsCandidatePath(j.MutationRequestID)
		},
		"candidate-inode":   func(j *SwitchJournalV1) { j.PDNSTargetPlan.Candidate.Inode++ },
		"candidate-sidecar": func(j *SwitchJournalV1) { j.PDNSTargetPlan.Candidate.NoSidecars = false },
		"stage-digest":      func(j *SwitchJournalV1) { j.PDNSTargetPlan.StagedDigest = strings.Repeat("b", 64) },
		"v3-reserved":       func(j *SwitchJournalV1) { j.Schema = "celikpanel-dns-engine-switch-journal/v3" },
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			raw, e := policy.EncodeSwitchJournal(stage)
			if e != nil {
				t.Fatal(e)
			}
			j, e := policy.DecodeSwitchJournal(raw)
			if e != nil {
				t.Fatal(e)
			}
			edit(&j)
			if _, e := policy.EncodeSwitchJournal(j); e == nil {
				t.Fatal("unsupported or tampered V4 journal accepted")
			}
		})
	}
	base, err := policy.DecodeSwitchJournal(journalFixture(t, "alpha81-pdns-switch"))
	if err != nil {
		t.Fatal(err)
	}
	adopt, err := policy.DecodeSwitchJournal(journalFixture(t, "alpha81-pdns-adopt"))
	if err != nil {
		t.Fatal(err)
	}
	adopt.Schema = SwitchJournalSchemaV4
	adopt.PDNSTargetPlan = intent.PDNSTargetPlan
	if _, err := policy.EncodeSwitchJournal(adopt); err == nil {
		t.Fatal("V4 adoption bypassed switch validation")
	}
	base.PDNSTargetPlan = intent.PDNSTargetPlan
	if _, err := policy.EncodeSwitchJournal(base); err == nil {
		t.Fatal("V1 accepted V4 evidence")
	}
}

func TestPDNSTargetV4AcceptsCanonicalHostedZoneManifest(t *testing.T) {
	policy := journalTestPolicy()
	base, err := policy.DecodeSwitchJournal(journalFixture(t, "alpha81-pdns-switch"))
	if err != nil {
		t.Fatal(err)
	}
	requested := []transport.DNSEngineSwitchZoneSnapshot{{
		Domain: "example.test", DesiredGeneration: 1, ZoneType: "NATIVE",
		Records: []transport.ZoneRecord{{Name: "example.test", Type: "A", Content: "192.0.2.4"}},
	}}
	commitment, err := mutationpayload.CanonicalDNSEngineSwitchManifestWithPairIdentity(
		base.Mode, base.SourceEngine, base.TargetEngine, base.SourceEpoch, base.TargetEpoch,
		base.SourceRevision, base.Topology, base.PairRole, base.LocalIP, base.LocalNS,
		base.PeerIP, base.PeerNS, requested)
	if err != nil {
		t.Fatal(err)
	}
	base.ManifestQualifier = commitment.Qualifier
	base.SnapshotBytes = commitment.SnapshotBytes
	base.Zones = commitment.Zones
	after := cloneFileSnapshotsV2(base.ConfigBefore)
	for i := range after {
		if after[i].Path == policy.PDNSManagedPath {
			after[i] = FileSnapshot{Path: policy.PDNSManagedPath, Exists: true, Mode: 0o644, OwnerKnown: true, UID: 0, GID: 0,
				Data: []byte("launch=gsqlite3\n")}
			after[i].SHA256 = DigestBytes(after[i].Data)
		}
	}
	base.TargetUnitsBefore[0].LoadState = "loaded"
	base.TargetUnitsBefore[0].UnitFileState = "disabled"
	state, exists, err := SourceStateFromSwitchJournal(base)
	if err != nil || !exists {
		t.Fatal(err)
	}
	source := ManagedBINDSourceProofV4{Kind: ManagedBINDSourceProofKindV4, HostLayout: "apt",
		Generation: state.Generation, EngineEpoch: state.EngineEpoch, ReceiptSHA256: strings.Repeat("e", 64),
		ConfigBefore: []FileSnapshot{
			v2BINDConfigSnapshot("/etc/bind/named.conf", 0o644, 42, []byte("include \"/etc/bind/named.conf.options\";\ninclude \"/etc/bind/named.conf.local\";\ninclude \"/etc/bind/named.conf.default-zones\";\n")),
			v2BINDConfigSnapshot("/etc/bind/named.conf.default-zones", 0o644, 42, []byte("// default\n")),
			v4ManagedLocal(t),
			v2BINDConfigSnapshot("/etc/bind/named.conf.options", 0o644, 42, []byte("// options\n")),
		},
	}
	journal, err := policy.BuildPDNSTargetInverseJournalV4(base, after, source, v4Candidate(policy, base.MutationRequestID))
	if err != nil || len(journal.Zones) != 1 {
		t.Fatalf("canonical hosted zone refused: %v", err)
	}
}
func v4ManagedLocal(t *testing.T) FileSnapshot {
	t.Helper()
	data, err := bindconfig.ManagedZoneInclude("// local before\n", "/var/cache/bind/celikpanel/current/zones.conf")
	if err != nil {
		t.Fatal(err)
	}
	return v2BINDConfigSnapshot("/etc/bind/named.conf.local", 0o644, 42, []byte(data))
}

func TestPDNSTargetEnableIntentPhasesAreV4Only(t *testing.T) {
	policy, _ := v4PDNSTargetFixture(t)
	legacy, err := policy.DecodeSwitchJournal(journalFixture(t, "alpha81-pdns-switch"))
	if err != nil {
		t.Fatal(err)
	}
	for _, phase := range []string{SwitchPhaseTargetEnableIntent, SwitchPhaseRollingBackTargetEnable} {
		changed := legacy
		changed.Phase = phase
		if _, err := policy.EncodeSwitchJournal(changed); err == nil {
			t.Fatalf("legacy journal admitted V4 enable-intent phase %s", phase)
		}
	}
}

func TestPDNSTargetV4ForwardPhaseRequiresEnableIntent(t *testing.T) {
	for _, tc := range []struct {
		before, after string
		want          bool
	}{
		{SwitchPhaseIntent, SwitchPhaseTargetStaged, true},
		{SwitchPhaseTargetStaged, SwitchPhaseSourceStopped, true},
		{SwitchPhaseSourceStopped, SwitchPhaseTargetEnableIntent, true},
		{SwitchPhaseTargetEnableIntent, SwitchPhaseTargetStarted, true},
		{SwitchPhaseTargetStarted, SwitchPhaseTargetVerified, true},
		{SwitchPhaseTargetVerified, SwitchPhaseCommitted, true},
		{SwitchPhaseSourceStopped, SwitchPhaseTargetStarted, false},
		{SwitchPhaseTargetEnableIntent, SwitchPhaseRollingBack, false},
		{SwitchPhaseRollingBackTargetEnable, SwitchPhaseTargetStarted, false},
		{SwitchPhaseCommitted, SwitchPhaseTargetStarted, false},
		{SwitchPhaseSourceStopped, SwitchPhaseSourceStopped, true},
		{SwitchPhaseRollingBackTargetEnable, SwitchPhaseRollingBackTargetEnable, false},
	} {
		before := SwitchJournalV1{Schema: SwitchJournalSchemaV4, Phase: tc.before}
		after := before
		after.Phase = tc.after
		if got := ValidPDNSTargetForwardPhaseTransitionV4(before, after); got != tc.want {
			t.Fatalf("%s -> %s valid=%t, want %t", tc.before, tc.after, got, tc.want)
		}
	}
	legacy := SwitchJournalV1{Schema: SwitchJournalSchemaV1, Phase: SwitchPhaseSourceStopped}
	next := SwitchJournalV1{Schema: SwitchJournalSchemaV4, Phase: SwitchPhaseTargetEnableIntent}
	if ValidPDNSTargetForwardPhaseTransitionV4(legacy, next) {
		t.Fatal("legacy journal was silently upgraded to V4")
	}
}
