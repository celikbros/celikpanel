package dnsengineartifact

import (
	"github.com/alicelik/celikpanel/internal/bindconfig"
	"testing"
)

func adoptionV2Fixture(t *testing.T) (JournalPolicy, SwitchJournalV1) {
	t.Helper()
	policy, j := v2BINDJournalFixture(t, "apt")
	j.Schema = SwitchJournalSchemaV1
	j.InversePlan = nil
	j.TargetUnitsBefore = []UnitSnapshot{
		{Name: "bind9.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"},
		{Name: "named.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"},
	}
	local := []byte("zone \"owner.example\" { type master; file \"/etc/bind/db.owner\"; };\n")
	options := []byte("options { directory \"/var/cache/bind\"; dnssec-validation auto; };\n")
	j.ConfigBefore = []FileSnapshot{
		v2BINDConfigSnapshot("/etc/bind/named.conf.local", 0o644, 42, local),
		v2BINDConfigSnapshot("/etc/bind/named.conf.options", 0o644, 42, options),
	}
	main := []byte("include \"/etc/bind/named.conf.options\";\ninclude \"/etc/bind/named.conf.local\";\ninclude \"/etc/bind/named.conf.root-hints\";\n")
	leaf := []byte("zone \".\" { type hint; file \"/usr/share/dns/root.hints\"; };\n")
	unchanged := []FileSnapshot{v2BINDConfigSnapshot("/etc/bind/named.conf", 0o644, 42, main), v2BINDConfigSnapshot("/etc/bind/named.conf.root-hints", 0o644, 42, leaf)}
	managed, e := bindconfig.ManagedZoneInclude(string(local), "/var/cache/bind/celikpanel/current/zones.conf")
	if e != nil {
		t.Fatal(e)
	}
	after := []FileSnapshot{v2BINDConfigSnapshot("/etc/bind/named.conf.local", 0o644, 42, []byte(managed)), j.ConfigBefore[1]}
	proof := BINDAdoptionSourceProofV1{
		Kind: BINDAdoptionSourceProofKindV1,
		Files: []BINDAdoptionSourceFileV1{
			{Path: "/etc/bind/db.owner", SHA256: DigestBytes([]byte("owner")), Size: 5, Mode: 0o644, UID: 0, GID: 42, Device: 1, Inode: 2},
			{Path: "/usr/share/dns/root.hints", SHA256: DigestBytes([]byte("hints")), Size: 5, Mode: 0o644, UID: 0, GID: 0, Device: 1, Inode: 3},
		},
		Zones: []BINDAdoptionSourceZoneV1{
			{Name: "owner.example", Class: "IN", Type: "master", File: "/etc/bind/db.owner", SOASerial: 7},
			{Name: ".", Class: "IN", Type: "hint", File: "/usr/share/dns/root.hints"},
		},
	}
	result, e := policy.BuildBINDSwitchInverseJournalV2(j, "apt", after, BINDSwitchSourceProofV2{BINDUnchangedConfig: unchanged, SourceBIND: &proof})
	if e != nil {
		t.Fatal(e)
	}
	return policy, result
}
func TestBINDAdoptionSourceProofBoundAndCopied(t *testing.T) {
	policy, j := adoptionV2Fixture(t)
	raw, e := policy.EncodeSwitchJournal(j)
	if e != nil {
		t.Fatal(e)
	}
	decoded, e := policy.DecodeSwitchJournal(raw)
	if e != nil || !SameImmutableBINDSwitchInversePlanV2(j, decoded) {
		t.Fatalf("roundtrip: %v", e)
	}
	j.InversePlan.SourceBIND.Files[0].SHA256 = DigestBytes([]byte("edit"))
	if e = policy.ValidateSwitchJournal(j); e == nil {
		t.Fatal("tampered source proof retained digest")
	}
}
func TestBINDAdoptionSourceProofRejectsForeignEvidence(t *testing.T) {
	policy, original := adoptionV2Fixture(t)
	for name, edit := range map[string]func(*SwitchJournalV1){
		"both-sources":  func(j *SwitchJournalV1) { j.InversePlan.SourcePDNS = &PDNSSourceProofV2{Kind: PDNSSourceProofKindV1} },
		"foreign-zone":  func(j *SwitchJournalV1) { j.InversePlan.SourceBIND.Zones[0].Name = "foreign.example" },
		"foreign-file":  func(j *SwitchJournalV1) { j.InversePlan.SourceBIND.Files[0].Path = "/etc/passwd" },
		"owner-missing": func(j *SwitchJournalV1) { j.InversePlan.SourceBIND.Zones = j.InversePlan.SourceBIND.Zones[1:] },
		"stopped-owner": func(j *SwitchJournalV1) { j.TargetUnitsBefore[0].ActiveState = "inactive" },
	} {
		t.Run(name, func(t *testing.T) {
			j := original
			plan := *original.InversePlan
			j.InversePlan = &plan
			proof := *plan.SourceBIND
			proof.Files = append([]BINDAdoptionSourceFileV1(nil), proof.Files...)
			proof.Zones = append([]BINDAdoptionSourceZoneV1(nil), proof.Zones...)
			plan.SourceBIND = &proof
			j.TargetUnitsBefore = append([]UnitSnapshot(nil), j.TargetUnitsBefore...)
			edit(&j)
			digest, e := bindSwitchInversePlanDigestV2(j)
			if e != nil {
				t.Fatal(e)
			}
			j.InversePlan.Digest = digest
			if e := policy.ValidateSwitchJournal(j); e == nil {
				t.Fatal("foreign source evidence accepted")
			}
		})
	}
}

func TestBINDAdoptionSourceBuilderDeepCopiesInput(t *testing.T) {
	policy, j := adoptionV2Fixture(t)
	base := j
	base.Schema = SwitchJournalSchemaV1
	base.InversePlan = nil
	after := cloneFileSnapshotsV2(j.InversePlan.ConfigAfter)
	source := *j.InversePlan.SourceBIND
	source.Files = append([]BINDAdoptionSourceFileV1(nil), source.Files...)
	source.Zones = append([]BINDAdoptionSourceZoneV1(nil), source.Zones...)
	result, e := policy.BuildBINDSwitchInverseJournalV2(base, "apt", after, BINDSwitchSourceProofV2{SourceBIND: &source, BINDUnchangedConfig: cloneFileSnapshotsV2(j.InversePlan.BINDUnchangedConfig)})
	if e != nil {
		t.Fatal(e)
	}
	source.Files[0].SHA256 = DigestBytes([]byte("mutated"))
	source.Zones[0].Name = "mutated.example"
	if e = policy.ValidateSwitchJournal(result); e != nil || result.InversePlan.SourceBIND.Zones[0].Name != "owner.example" {
		t.Fatalf("source builder aliased caller input: %v", e)
	}
}
