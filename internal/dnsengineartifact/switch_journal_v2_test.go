package dnsengineartifact

import (
	"bytes"
	"encoding/json"
	"github.com/alicelik/celikpanel/internal/bindconfig"
	"os"
	"testing"
)

func v2BINDJournalFixture(t *testing.T, layout string) (JournalPolicy, SwitchJournalV1) {
	t.Helper()
	policy := journalTestPolicy()
	base, err := policy.DecodeSwitchJournal(journalFixture(t, "alpha81-bind"))
	if err != nil {
		t.Fatal(err)
	}
	base.Phase = SwitchPhaseIntent
	if layout == "apt" {
		base.ConfigBefore = []FileSnapshot{
			v2BINDConfigSnapshot("/etc/bind/named.conf.local", 0o644, 42, []byte("// local before\n")),
			v2BINDConfigSnapshot("/etc/bind/named.conf.options", 0o644, 42, []byte("// options before\n")),
		}
	}
	after := make([]FileSnapshot, len(base.ConfigBefore))
	for i, snapshot := range base.ConfigBefore {
		after[i] = snapshot
		after[i].Data = []byte("// managed after\n")
		after[i].SHA256 = DigestBytes(after[i].Data)
	}
	journal, err := policy.BuildBINDSwitchInverseJournalV2(base, layout, after)
	if err != nil {
		t.Fatal(err)
	}
	return policy, journal
}

func v2BINDConfigSnapshot(path string, mode, gid uint32, data []byte) FileSnapshot {
	return FileSnapshot{
		Path: path, Exists: true, Mode: mode,
		OwnerKnown: true, UID: 0, GID: gid,
		Data: data, SHA256: DigestBytes(data),
	}
}

func TestBINDSwitchJournalV2CodecAndImmutablePlanAcrossPhases(t *testing.T) {
	for _, layout := range []string{"apt", "pacman"} {
		t.Run(layout, func(t *testing.T) {
			policy, journal := v2BINDJournalFixture(t, layout)
			initial := journal
			raw, err := policy.EncodeSwitchJournal(journal)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := policy.DecodeSwitchJournal(raw)
			if err != nil || !bytes.Equal(raw, mustEncodeJournal(t, policy, decoded)) {
				t.Fatalf("v2 journal did not round trip: %v", err)
			}
			for _, phase := range []string{SwitchPhaseTargetStaged, SwitchPhaseRollingBack, SwitchPhaseRolledBack} {
				journal.Phase = phase
				encoded, err := policy.EncodeSwitchJournal(journal)
				if err != nil {
					t.Fatal(err)
				}
				read, err := policy.DecodeSwitchJournal(encoded)
				if err != nil || !SameImmutableBINDSwitchInversePlanV2(initial, read) ||
					read.InversePlan.Digest != initial.InversePlan.Digest {
					t.Fatalf("phase %s changed immutable plan: %v", phase, err)
				}
			}
		})
	}
}

func mustEncodeJournal(t *testing.T, policy JournalPolicy, journal SwitchJournalV1) []byte {
	t.Helper()
	raw, err := policy.EncodeSwitchJournal(journal)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestBINDSwitchJournalV2RejectsUnimplementedReinstallInverse(t *testing.T) {
	policy, journal := v2BINDJournalFixture(t, "apt")
	journal.Mode = "reinstall"
	if err := policy.ValidateSwitchJournal(journal); err == nil ||
		!bytes.Contains([]byte(err.Error()), []byte("no supported BIND inverse")) {
		t.Fatalf("reinstall v2 inverse admission = %v, want unsupported BIND inverse", err)
	}
}

func TestBINDSwitchJournalV2RejectsUnboundOrUnsafePlan(t *testing.T) {
	cases := []struct {
		name string
		edit func(*SwitchJournalV1)
	}{
		{"unknown-layout", func(j *SwitchJournalV1) { j.InversePlan.HostLayout = "unknown" }},
		{"layout-mismatch", func(j *SwitchJournalV1) { j.InversePlan.HostLayout = "pacman" }},
		{"unknown-kind", func(j *SwitchJournalV1) { j.InversePlan.Kind = "other" }},
		{"missing-after", func(j *SwitchJournalV1) { j.InversePlan.ConfigAfter = nil }},
		{"wrong-path", func(j *SwitchJournalV1) { j.InversePlan.ConfigAfter[0].Path = "/etc/passwd" }},
		{"duplicate-path", func(j *SwitchJournalV1) { j.InversePlan.ConfigAfter[1].Path = j.InversePlan.ConfigAfter[0].Path }},
		{"changed-after-bytes", func(j *SwitchJournalV1) { j.InversePlan.ConfigAfter[0].Data = []byte("owner edit") }},
		{"rehashed-after-bytes", func(j *SwitchJournalV1) {
			j.InversePlan.ConfigAfter[0].Data = []byte("owner edit")
			j.InversePlan.ConfigAfter[0].SHA256 = DigestBytes(j.InversePlan.ConfigAfter[0].Data)
		}},
		{"changed-after-owner", func(j *SwitchJournalV1) { j.InversePlan.ConfigAfter[0].GID++ }},
		{"missing-after-owner", func(j *SwitchJournalV1) { j.InversePlan.ConfigAfter[0].OwnerKnown = false }},
		{"changed-after-mode", func(j *SwitchJournalV1) { j.InversePlan.ConfigAfter[0].Mode = 0o600 }},
		{"absent-after", func(j *SwitchJournalV1) { j.InversePlan.ConfigAfter[0].Exists = false }},
		{"changed-before-bytes", func(j *SwitchJournalV1) {
			j.ConfigBefore[0].Data = []byte("edited before")
			j.ConfigBefore[0].SHA256 = DigestBytes(j.ConfigBefore[0].Data)
		}},
		{"foreign-request", func(j *SwitchJournalV1) { j.MutationRequestID = "dddddddddddddddddddddddddddddddd" }},
		{"foreign-generation", func(j *SwitchJournalV1) {
			j.TargetGeneration = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
		}},
		{"wrong-digest", func(j *SwitchJournalV1) {
			j.InversePlan.Digest = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
		}},
		{"unknown-schema", func(j *SwitchJournalV1) { j.Schema = "celikpanel-dns-engine-switch-journal/v3" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			policy, journal := v2BINDJournalFixture(t, "apt")
			tc.edit(&journal)
			if _, err := policy.EncodeSwitchJournal(journal); err == nil {
				t.Fatal("unsafe or unbound v2 journal accepted")
			}
		})
	}
}

func TestBINDSwitchJournalV2RejectsUnknownWireFieldsAndV1PlanSmuggling(t *testing.T) {
	policy, journal := v2BINDJournalFixture(t, "apt")
	raw := mustEncodeJournal(t, policy, journal)
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatal(err)
	}
	plan := object["inverse_plan"].(map[string]any)
	plan["unreviewed"] = true
	bad, err := json.Marshal(object)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := policy.DecodeSwitchJournal(append(bad, '\n')); err == nil {
		t.Fatal("unknown v2 plan field accepted")
	}
	adoption, err := policy.DecodeSwitchJournal(journalFixture(t, "alpha81-pdns-adopt"))
	if err != nil {
		t.Fatal(err)
	}
	adoption.Schema = SwitchJournalSchemaV2
	adoption.InversePlan = journal.InversePlan
	if _, err := policy.EncodeSwitchJournal(adoption); err == nil {
		t.Fatal("PowerDNS adoption accepted an unsupported v2 BIND inverse plan")
	}
	for _, fixture := range []string{"alpha81-bind", "alpha81-pdns-adopt"} {
		v1, err := policy.DecodeSwitchJournal(journalFixture(t, fixture))
		if err != nil {
			t.Fatal(err)
		}
		v1.InversePlan = journal.InversePlan
		if _, err := policy.EncodeSwitchJournal(v1); err == nil {
			t.Fatalf("v1 %s accepted a v2 plan", fixture)
		}
	}
}

func TestBINDSwitchJournalV2BuilderCopiesPreparedBytes(t *testing.T) {
	policy := journalTestPolicy()
	base, err := policy.DecodeSwitchJournal(journalFixture(t, "alpha81-bind"))
	if err != nil {
		t.Fatal(err)
	}
	base.Phase = SwitchPhaseIntent
	input := []FileSnapshot{base.ConfigBefore[0]}
	input[0].Data = []byte("prepared")
	input[0].SHA256 = DigestBytes(input[0].Data)
	journal, err := policy.BuildBINDSwitchInverseJournalV2(base, "pacman", input)
	if err != nil {
		t.Fatal(err)
	}
	input[0].Data[0] = 'X'
	if _, err := policy.EncodeSwitchJournal(journal); err != nil || string(journal.InversePlan.ConfigAfter[0].Data) != "prepared" {
		t.Fatalf("prepared plan alias changed after construction: %v", err)
	}
}

func TestBINDSwitchJournalV2GoldenAPTFixture(t *testing.T) {
	policy, journal := v2BINDJournalFixture(t, "apt")
	want, err := os.ReadFile("testdata/switch-journal/v2-bind-apt.json")
	if err != nil {
		t.Fatal(err)
	}
	encoded := mustEncodeJournal(t, policy, journal)
	if !bytes.Equal(want, encoded) {
		t.Fatal("v2 BIND APT journal fixture differs from canonical writer")
	}
	decoded, err := policy.DecodeSwitchJournal(want)
	if err != nil || !SameImmutableBINDSwitchInversePlanV2(journal, decoded) {
		t.Fatalf("v2 BIND APT fixture did not decode exactly: %v", err)
	}
}

func TestBINDSwitchSourcePDNSProofIsBoundAndValidated(t *testing.T) {
	policy, journal := v2BINDJournalFixture(t, "apt")
	journal.SourceEngine = "pdns"
	journal.InversePlan.SourcePDNS = &PDNSSourceProofV2{
		Kind: PDNSSourceProofKindV1,
		ConfigBefore: []FileSnapshot{
			{Path: policy.PDNSMainPath, Exists: true, Mode: 0o640, OwnerKnown: true, UID: 0, GID: 42, Data: []byte("main"), SHA256: DigestBytes([]byte("main"))},
			{Path: policy.PDNSClusterPath},
			{Path: policy.PDNSManagedPath, Exists: true, Mode: 0o644, OwnerKnown: true, UID: 0, GID: 0, Data: []byte("managed"), SHA256: DigestBytes([]byte("managed"))},
		},
		Database: PDNSSourceDatabaseProofV1{Path: policy.PDNSDatabasePath, LogicalSHA256: DigestBytes([]byte("logical")), Mode: 0o640, UID: 100, GID: 100, Device: 1, Inode: 2},
	}
	digest, err := bindSwitchInversePlanDigestV2(journal)
	if err != nil {
		t.Fatal(err)
	}
	journal.InversePlan.Digest = digest
	if err := policy.ValidateBINDSwitchInversePlanV2(journal); err != nil {
		t.Fatal(err)
	}
	tampered := journal
	plan := *journal.InversePlan
	tampered.InversePlan = &plan
	source := *plan.SourcePDNS
	plan.SourcePDNS = &source
	source.Database.LogicalSHA256 = DigestBytes([]byte("edited"))
	if err := policy.ValidateBINDSwitchInversePlanV2(tampered); err == nil {
		t.Fatal("database edit retained frozen digest")
	}
	source.Database.LogicalSHA256 = journal.InversePlan.SourcePDNS.Database.LogicalSHA256
	source.Database.Path = "/etc/passwd"
	digest, err = bindSwitchInversePlanDigestV2(tampered)
	if err != nil {
		t.Fatal(err)
	}
	plan.Digest = digest
	if err := policy.ValidateBINDSwitchInversePlanV2(tampered); err == nil {
		t.Fatal("foreign source database path accepted")
	}
}

func TestBINDSwitchUnchangedDebianEnvelopeIsFrozen(t *testing.T) {
	policy, journal := v2BINDJournalFixture(t, "apt")
	journal.SourceEngine = "pdns"
	main := []byte("include \"/etc/bind/named.conf.options\";\ninclude \"/etc/bind/named.conf.local\";\ninclude \"/etc/bind/named.conf.default-zones\";\n")
	defaults := []byte("zone \"localhost\" { type master; file \"/etc/bind/db.local\"; };\n")
	journal.InversePlan.BINDUnchangedConfig = []FileSnapshot{
		v2BINDConfigSnapshot("/etc/bind/named.conf", 0o644, 42, main),
		v2BINDConfigSnapshot("/etc/bind/named.conf.default-zones", 0o644, 42, defaults),
	}
	managed, err := bindconfig.ManagedZoneInclude("// local before\n", "/var/cache/bind/celikpanel/current/zones.conf")
	if err != nil {
		t.Fatal(err)
	}
	journal.InversePlan.ConfigAfter[0].Data = []byte(managed)
	journal.InversePlan.ConfigAfter[0].SHA256 = DigestBytes([]byte(managed))
	digest, err := bindSwitchInversePlanDigestV2(journal)
	if err != nil {
		t.Fatal(err)
	}
	journal.InversePlan.Digest = digest
	if err := policy.ValidateBINDSwitchInversePlanV2(journal); err != nil {
		t.Fatal(err)
	}
	bad := journal
	plan := *journal.InversePlan
	bad.InversePlan = &plan
	plan.BINDUnchangedConfig = cloneFileSnapshotsV2(plan.BINDUnchangedConfig)
	plan.BINDUnchangedConfig[1].Data = []byte("include \"/etc/bind/owner.conf\";\n")
	plan.BINDUnchangedConfig[1].SHA256 = DigestBytes(plan.BINDUnchangedConfig[1].Data)
	if err := policy.ValidateBINDSwitchInversePlanV2(bad); err == nil {
		t.Fatal("rehashed default-zones edit retained frozen digest")
	}
	digest, err = bindSwitchInversePlanDigestV2(bad)
	if err != nil {
		t.Fatal(err)
	}
	plan.Digest = digest
	if err := policy.ValidateBINDSwitchInversePlanV2(bad); err == nil {
		t.Fatal("active default-zones include accepted")
	}
	plan.BINDUnchangedConfig[1] = journal.InversePlan.BINDUnchangedConfig[1]
	plan.BINDUnchangedConfig[0].Path = "/etc/passwd"
	digest, err = bindSwitchInversePlanDigestV2(bad)
	if err != nil {
		t.Fatal(err)
	}
	plan.Digest = digest
	if err := policy.ValidateBINDSwitchInversePlanV2(bad); err == nil {
		t.Fatal("foreign unchanged path accepted")
	}
}

func TestBINDSwitchRootHintsEnvelopeSelectsFrozenLeaf(t *testing.T) {
	policy, journal := v2BINDJournalFixture(t, "apt")
	journal.SourceEngine = "pdns"
	main := []byte("include \"/etc/bind/named.conf.options\";\ninclude \"/etc/bind/named.conf.local\";\ninclude \"/etc/bind/named.conf.root-hints\";\n")
	leaf := []byte("zone \".\" { type hint; file \"/usr/share/dns/root.hints\"; };\n")
	journal.InversePlan.BINDUnchangedConfig = []FileSnapshot{
		v2BINDConfigSnapshot("/etc/bind/named.conf", 0o644, 42, main),
		v2BINDConfigSnapshot("/etc/bind/named.conf.root-hints", 0o644, 42, leaf),
	}
	managed, err := bindconfig.ManagedZoneInclude("// local before\n", "/var/cache/bind/celikpanel/current/zones.conf")
	if err != nil {
		t.Fatal(err)
	}
	journal.InversePlan.ConfigAfter[0].Data = []byte(managed)
	journal.InversePlan.ConfigAfter[0].SHA256 = DigestBytes([]byte(managed))
	digest, err := bindSwitchInversePlanDigestV2(journal)
	if err != nil {
		t.Fatal(err)
	}
	journal.InversePlan.Digest = digest
	if err := policy.ValidateBINDSwitchInversePlanV2(journal); err != nil {
		t.Fatal(err)
	}
	wrong := journal
	plan := *journal.InversePlan
	wrong.InversePlan = &plan
	plan.BINDUnchangedConfig = cloneFileSnapshotsV2(plan.BINDUnchangedConfig)
	plan.BINDUnchangedConfig[1].Path = "/etc/bind/named.conf.default-zones"
	digest, err = bindSwitchInversePlanDigestV2(wrong)
	if err != nil {
		t.Fatal(err)
	}
	plan.Digest = digest
	if err := policy.ValidateBINDSwitchInversePlanV2(wrong); err == nil {
		t.Fatal("frozen leaf path disagrees with main include")
	}
}
