package main

import (
	"bytes"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

func testBINDSwitchJournal(t *testing.T) dnsEngineSwitchJournal {
	t.Helper()
	manifest, err := mutationpayload.CanonicalDNSEngineSwitchManifest(
		transport.DNSEngineSwitchModeSwitch,
		"", transport.DNSEngineBIND, 0, 1, 0,
		transport.DNSTopologyStandalone, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	t.Setenv("CELIKPANEL_AGENT_STATE_DIR", root)
	return dnsEngineSwitchJournal{
		Schema: dnsEngineSwitchJournalSchema, Phase: dnsSwitchPhaseTargetStaged,
		Mode:              transport.DNSEngineSwitchModeSwitch,
		MutationRequestID: strings.Repeat("a", 32), MutationOwnerID: strings.Repeat("b", 32),
		ManifestQualifier: manifest.Qualifier,
		SourceEngine:      manifest.SourceEngine, TargetEngine: manifest.TargetEngine,
		SourceEpoch: manifest.SourceEpoch, TargetEpoch: manifest.TargetEpoch,
		SourceRevision: manifest.SourceRevision, Topology: manifest.Topology,
		SnapshotBytes: manifest.SnapshotBytes, Zones: manifest.Zones,
		TargetGeneration: strings.Repeat("c", 64),
		StateBefore:      dnsFileSnapshot{Path: filepath.Join(root, "dns-engine-state.json")},
		ConfigBefore: []dnsFileSnapshot{{
			Path: "/etc/named.conf", Exists: true, Mode: 0o644,
			OwnerKnown: dnsSnapshotOwnerRequired(),
			SHA256:     digestDNSBytes([]byte("named")), Data: []byte("named"),
		}},
		TargetUnitsBefore: []dnsUnitSnapshot{
			{Name: "bind9.service", LoadState: "not-found", ActiveState: "inactive"},
			{Name: "named.service", LoadState: "not-found", ActiveState: "inactive"},
		},
		SourceUnitsBefore: []dnsUnitSnapshot{},
	}
}

func TestDNSEngineSwitchJournalCanonicalRoundTrip(t *testing.T) {
	journal := testBINDSwitchJournal(t)
	encoded, err := encodeDNSEngineSwitchJournal(journal)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeDNSEngineSwitchJournal(encoded)
	if err != nil {
		t.Fatal(err)
	}
	reencoded, err := encodeDNSEngineSwitchJournal(decoded)
	if err != nil || !bytes.Equal(encoded, reencoded) {
		t.Fatalf("canonical round trip err=%v", err)
	}
	if _, err := decodeDNSEngineSwitchJournal(append([]byte(" "), encoded...)); err == nil {
		t.Fatal("noncanonical journal whitespace was accepted")
	}
	journal.TargetUnitsBefore[0].UnitFileState = "alias"
	if _, err := encodeDNSEngineSwitchJournal(journal); err == nil {
		t.Fatal("unrestorable alias unit state was accepted")
	}
	journal = testBINDSwitchJournal(t)
	journal.ConfigBefore[0].Path = "/tmp/attacker.conf"
	if _, err := encodeDNSEngineSwitchJournal(journal); err == nil {
		t.Fatal("unmanaged BIND config snapshot was accepted")
	}
}

func TestPDNSSwitchJournalRejectsUnmanagedPaths(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CELIKPANEL_AGENT_STATE_DIR", root)
	t.Setenv("CELIKPANEL_PDNS_DB", filepath.Join(root, "pdns.sqlite3"))
	previousMain, previousManaged, previousCluster := dnsMainConf, dnsManagedConf, dnsClusterConf
	dnsMainConf = filepath.Join(root, "pdns.conf")
	dnsManagedConf = filepath.Join(root, "pdns.d", "celikpanel.conf")
	dnsClusterConf = filepath.Join(root, "pdns.d", "celikpanel-cluster.conf")
	t.Cleanup(func() {
		dnsMainConf, dnsManagedConf, dnsClusterConf = previousMain, previousManaged, previousCluster
	})
	manifest, err := mutationpayload.CanonicalDNSEngineSwitchManifest(
		transport.DNSEngineSwitchModeSwitch,
		transport.DNSEngineBIND, transport.DNSEnginePowerDNS, 2, 3, 8,
		transport.DNSTopologyStandalone, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	configs := []dnsFileSnapshot{
		{Path: filepath.Clean(dnsMainConf)},
		{Path: filepath.Clean(dnsManagedConf)},
		{Path: filepath.Clean(dnsClusterConf)},
	}
	sort.Slice(configs, func(left, right int) bool { return configs[left].Path < configs[right].Path })
	sourceState := legacyDurableDNSState(transport.DNSEngineBIND)
	sourceState.EngineEpoch = manifest.SourceEpoch
	sourceStateBytes, err := encodeDNSEngineState(sourceState)
	if err != nil {
		t.Fatal(err)
	}
	stateBefore := dnsFileSnapshot{
		Path:   filepath.Join(root, "dns-engine-state.json"),
		Exists: true, Mode: 0o600,
		SHA256: digestDNSBytes(sourceStateBytes), Data: sourceStateBytes,
	}
	if dnsSnapshotOwnerRequired() {
		stateBefore.OwnerKnown = true
	}
	journal := dnsEngineSwitchJournal{
		Schema: dnsEngineSwitchJournalSchema, Phase: dnsSwitchPhaseIntent,
		Mode:              transport.DNSEngineSwitchModeSwitch,
		MutationRequestID: strings.Repeat("a", 32), MutationOwnerID: strings.Repeat("b", 32),
		ManifestQualifier: manifest.Qualifier, SourceEngine: manifest.SourceEngine,
		TargetEngine: manifest.TargetEngine, SourceEpoch: manifest.SourceEpoch,
		TargetEpoch: manifest.TargetEpoch, SourceRevision: manifest.SourceRevision,
		Topology: manifest.Topology, SnapshotBytes: manifest.SnapshotBytes, Zones: manifest.Zones,
		StateBefore:       stateBefore,
		ConfigBefore:      configs,
		TargetUnitsBefore: []dnsUnitSnapshot{{Name: "pdns.service", LoadState: "not-found", ActiveState: "inactive"}},
		SourceUnitsBefore: []dnsUnitSnapshot{
			{Name: "bind9.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"},
			{Name: "named.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"},
		},
		PDNSCandidatePath: pdnsSwitchCandidatePath(strings.Repeat("a", 32)),
		PDNSBackupPath:    pdnsSwitchBackupPath(strings.Repeat("a", 32)),
	}
	if _, err := encodeDNSEngineSwitchJournal(journal); err != nil {
		t.Fatal(err)
	}
	journal.StateBefore = dnsFileSnapshot{Path: filepath.Join(root, "dns-engine-state.json")}
	if _, err := encodeDNSEngineSwitchJournal(journal); err == nil {
		t.Fatal("received source engine without exact state snapshot was accepted")
	}
	journal.StateBefore = stateBefore
	journal.PDNSCandidatePath = filepath.Join(root, "attacker.sqlite3")
	if _, err := encodeDNSEngineSwitchJournal(journal); err == nil {
		t.Fatal("unmanaged PowerDNS candidate path was accepted")
	}
}

func TestPDNSAdoptionJournalBindsReadOnlyRuntimeEvidence(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CELIKPANEL_AGENT_STATE_DIR", root)
	t.Setenv("CELIKPANEL_PDNS_DB", filepath.Join(root, "pdns.sqlite3"))
	previousMain, previousManaged, previousCluster := dnsMainConf, dnsManagedConf, dnsClusterConf
	dnsMainConf = filepath.Join(root, "pdns.conf")
	dnsManagedConf = filepath.Join(root, "pdns.d", "celikpanel.conf")
	dnsClusterConf = filepath.Join(root, "pdns.d", "celikpanel-cluster.conf")
	t.Cleanup(func() {
		dnsMainConf, dnsManagedConf, dnsClusterConf = previousMain, previousManaged, previousCluster
	})
	manifest, err := mutationpayload.CanonicalDNSEngineSwitchManifest(
		transport.DNSEngineSwitchModeAdopt,
		"", transport.DNSEnginePowerDNS, 0, 1, 12,
		transport.DNSTopologyPaired, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	configs := []dnsFileSnapshot{
		{Path: filepath.Clean(dnsMainConf), Exists: true, Mode: 0o644, SHA256: digestDNSBytes([]byte("main")), Data: []byte("main")},
		{Path: filepath.Clean(dnsManagedConf), Exists: true, Mode: 0o640, SHA256: digestDNSBytes([]byte("managed")), Data: []byte("managed")},
		{Path: filepath.Clean(dnsClusterConf), Exists: true, Mode: 0o600, SHA256: digestDNSBytes([]byte("paired")), Data: []byte("paired")},
	}
	sort.Slice(configs, func(left, right int) bool { return configs[left].Path < configs[right].Path })
	if dnsSnapshotOwnerRequired() {
		for index := range configs {
			configs[index].OwnerKnown = true
		}
	}
	journal := dnsEngineSwitchJournal{
		Schema: dnsEngineSwitchJournalSchema, Phase: dnsSwitchPhaseIntent,
		Mode:              transport.DNSEngineSwitchModeAdopt,
		MutationRequestID: strings.Repeat("a", 32),
		MutationOwnerID:   strings.Repeat("b", 32),
		ManifestQualifier: manifest.Qualifier,
		TargetEngine:      manifest.TargetEngine, TargetEpoch: manifest.TargetEpoch,
		SourceRevision: manifest.SourceRevision, Topology: manifest.Topology,
		SnapshotBytes: manifest.SnapshotBytes, Zones: manifest.Zones,
		StateBefore:  dnsFileSnapshot{Path: filepath.Join(root, "dns-engine-state.json")},
		ConfigBefore: configs,
		TargetUnitsBefore: []dnsUnitSnapshot{
			{Name: "bind9.service", LoadState: "loaded", ActiveState: "inactive", UnitFileState: "disabled"},
			{Name: "named.service", LoadState: "loaded", ActiveState: "inactive", UnitFileState: "disabled"},
			{Name: "pdns.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"},
		},
		SourceUnitsBefore: []dnsUnitSnapshot{},
		PDNSLiveSHA256:    strings.Repeat("c", 64), PDNSLiveSize: 4096,
	}
	encoded, err := encodeDNSEngineSwitchJournal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeDNSEngineSwitchJournal(encoded); err != nil {
		t.Fatal(err)
	}
	journal.TargetUnitsBefore[1].ActiveState = "active"
	if _, err := encodeDNSEngineSwitchJournal(journal); err == nil {
		t.Fatal("adoption journal accepted another active DNS engine")
	}
}
