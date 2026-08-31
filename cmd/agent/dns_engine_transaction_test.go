package main

import (
	"bytes"
	"errors"
	"fmt"
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
	tamperedPeer := journal
	tamperedPeer.PeerIP = "192.0.2.54"
	if _, err := encodeDNSEngineSwitchJournal(tamperedPeer); err == nil {
		t.Fatal("adoption journal accepted a peer tuple outside its manifest qualifier")
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

func TestPairedPrimarySwitchJournalBindsFreshCatalogSerial(t *testing.T) {
	manifest, err := mutationpayload.CanonicalDNSEngineSwitchManifestWithPairIdentity(
		transport.DNSEngineSwitchModeSwitch,
		"", transport.DNSEngineBIND, 0, 1, 0,
		transport.DNSTopologyPaired,
		transport.DNSPairRolePrimary,
		"192.0.2.10", "ns1.example.test",
		"192.0.2.20", "ns2.example.test", nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	journal := testBINDSwitchJournal(t)
	journal.ManifestQualifier = manifest.Qualifier
	journal.Topology = manifest.Topology
	journal.PairRole = manifest.PairRole
	journal.LocalIP = manifest.LocalIP
	journal.LocalNS = manifest.LocalNS
	journal.PeerIP = manifest.PeerIP
	journal.PeerNS = manifest.PeerNS
	journal.SnapshotBytes = manifest.SnapshotBytes
	journal.Zones = manifest.Zones
	journal.PrimaryCatalogSerial = 1
	encoded, err := encodeDNSEngineSwitchJournal(journal)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeDNSEngineSwitchJournal(encoded)
	if err != nil || decoded.PrimaryCatalogSerial != 1 {
		t.Fatalf("decoded serial=%d err=%v", decoded.PrimaryCatalogSerial, err)
	}
	journal.PrimaryCatalogSerial = 0
	if _, err := encodeDNSEngineSwitchJournal(journal); err == nil {
		t.Fatal("paired primary journal accepted a missing catalog serial")
	}
	journal.PrimaryCatalogSerial = 2
	if _, err := encodeDNSEngineSwitchJournal(journal); err == nil {
		t.Fatal("fresh primary journal accepted a non-initial catalog serial")
	}
}

func TestPersistExactDNSEngineStateAcceptsOnlyExactAfterRenameReadback(t *testing.T) {
	want := legacyDurableDNSState(transport.DNSEngineBIND)
	writeFailure := errors.New("directory fsync failed after rename")
	var durable dnsEngineStateReceipt
	exists := false
	writeAfterRename := func(state dnsEngineStateReceipt) error {
		durable, exists = state, true
		return writeFailure
	}
	read := func() (dnsEngineStateReceipt, bool, error) {
		return durable, exists, nil
	}
	if err := persistExactDNSEngineStateAt(want, writeAfterRename, read); err != nil {
		t.Fatalf("exact durable rename was treated as failure: %v", err)
	}
	durable = dnsEngineStateReceipt{}
	exists = false
	writeBeforeRename := func(dnsEngineStateReceipt) error { return writeFailure }
	if err := persistExactDNSEngineStateAt(want, writeBeforeRename, read); !errors.Is(err, writeFailure) {
		t.Fatalf("pre-rename failure was accepted: %v", err)
	}
	durable, exists = want, true
	durable.Generation = strings.Repeat("9", 64)
	if err := persistExactDNSEngineStateAt(want, writeBeforeRename, read); err == nil {
		t.Fatal("mismatched durable receipt was accepted")
	}
}

func TestPairedPrimarySwitchJournalAcceptsVerifiedLegacySourceReceipt(t *testing.T) {
	manifest, err := mutationpayload.CanonicalDNSEngineSwitchManifestWithPairIdentity(
		transport.DNSEngineSwitchModeSwitch,
		transport.DNSEnginePowerDNS, transport.DNSEngineBIND, 3, 4, 5,
		transport.DNSTopologyPaired,
		transport.DNSPairRolePrimary,
		"192.0.2.10", "ns1.example.test",
		"192.0.2.20", "ns2.example.test", nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	journal := testBINDSwitchJournal(t)
	legacySource := dnsEngineStateReceipt{
		Schema: dnsEngineStateSchema, Mode: transport.DNSEngineSwitchModeSwitch,
		Engine: transport.DNSEnginePowerDNS, EngineEpoch: manifest.SourceEpoch,
		SourceRevision:    4,
		ManifestQualifier: "dns-engine-switch/v1:sha256:" + strings.Repeat("d", 64),
		MutationRequestID: strings.Repeat("e", 32),
		MutationOwnerID:   strings.Repeat("f", 32),
	}
	legacyBytes, err := encodeDNSEngineState(legacySource)
	if err != nil {
		t.Fatal(err)
	}
	journal.StateBefore = dnsFileSnapshot{
		Path: dnsEngineStatePath(), Exists: true, Mode: 0o600,
		SHA256: digestDNSBytes(legacyBytes), Data: legacyBytes,
	}
	if dnsSnapshotOwnerRequired() {
		journal.StateBefore.OwnerKnown = true
	}
	journal.ManifestQualifier = manifest.Qualifier
	journal.SourceEngine = manifest.SourceEngine
	journal.TargetEngine = manifest.TargetEngine
	journal.SourceEpoch = manifest.SourceEpoch
	journal.TargetEpoch = manifest.TargetEpoch
	journal.SourceRevision = manifest.SourceRevision
	journal.Topology = manifest.Topology
	journal.PairRole = manifest.PairRole
	journal.LocalIP = manifest.LocalIP
	journal.LocalNS = manifest.LocalNS
	journal.PeerIP = manifest.PeerIP
	journal.PeerNS = manifest.PeerNS
	journal.PrimaryCatalogSerial = 41
	journal.SnapshotBytes = manifest.SnapshotBytes
	journal.Zones = manifest.Zones
	journal.SourceUnitsBefore = []dnsUnitSnapshot{{
		Name: "pdns.service", LoadState: "loaded",
		ActiveState: "active", UnitFileState: "enabled",
	}}
	if _, err := encodeDNSEngineSwitchJournal(journal); err != nil {
		t.Fatal(err)
	}
	journal.PrimaryCatalogSerial = 0
	if _, err := encodeDNSEngineSwitchJournal(journal); err == nil {
		t.Fatal("legacy primary source journal accepted a missing target handoff serial")
	}

	boundSource := legacySource
	boundSource.PairRole = transport.DNSPairRolePrimary
	boundSource.PairLocalIP = manifest.LocalIP
	boundSource.PairPeerIP = manifest.PeerIP
	boundSource.PrimaryCatalogSerial = 40
	boundBytes, err := encodeDNSEngineState(boundSource)
	if err != nil {
		t.Fatal(err)
	}
	journal.StateBefore = dnsFileSnapshot{
		Path: dnsEngineStatePath(), Exists: true, Mode: 0o600,
		SHA256: digestDNSBytes(boundBytes), Data: boundBytes,
	}
	if dnsSnapshotOwnerRequired() {
		journal.StateBefore.OwnerKnown = true
	}
	journal.PrimaryCatalogSerial = 41
	if _, err := encodeDNSEngineSwitchJournal(journal); err != nil {
		t.Fatalf("advanced durable serial was not accepted above its source anchor: %v", err)
	}
	boundSource.PrimaryCatalogSerial = 42
	boundBytes, err = encodeDNSEngineState(boundSource)
	if err != nil {
		t.Fatal(err)
	}
	journal.StateBefore.SHA256 = digestDNSBytes(boundBytes)
	journal.StateBefore.Data = boundBytes
	if _, err := encodeDNSEngineSwitchJournal(journal); err == nil {
		t.Fatal("journal accepted a catalog serial below its source anchor")
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
		{
			Path: filepath.Clean(dnsMainConf), Exists: true, Mode: 0o640,
			OwnerKnown: true, SHA256: digestDNSBytes([]byte("main\n")), Data: []byte("main\n"),
		},
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
	manifest, err := mutationpayload.CanonicalDNSEngineSwitchManifestWithPeer(
		transport.DNSEngineSwitchModeAdopt,
		"", transport.DNSEnginePowerDNS, 0, 1, 12,
		transport.DNSTopologyPaired,
		testPDNSAdoptionPeerIP, testPDNSAdoptionPeerNS, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	configs := []dnsFileSnapshot{
		{
			Path: filepath.Clean(dnsMainConf), Exists: true, Mode: 0o640,
			OwnerKnown: true, UID: 0, GID: 109,
			SHA256: digestDNSBytes([]byte("main")), Data: []byte("main"),
		},
		{
			Path: filepath.Clean(dnsManagedConf), Exists: true, Mode: 0o644,
			OwnerKnown: true, UID: 0, GID: 0,
			SHA256: digestDNSBytes([]byte("managed")), Data: []byte("managed"),
		},
		{
			Path: filepath.Clean(dnsClusterConf), Exists: true, Mode: 0o644,
			OwnerKnown: true, UID: 0, GID: 0,
			SHA256: digestDNSBytes([]byte("paired")), Data: []byte("paired"),
		},
	}
	sort.Slice(configs, func(left, right int) bool { return configs[left].Path < configs[right].Path })
	journal := dnsEngineSwitchJournal{
		Schema: dnsEngineSwitchJournalSchema, Phase: dnsSwitchPhaseIntent,
		Mode:              transport.DNSEngineSwitchModeAdopt,
		MutationRequestID: strings.Repeat("a", 32),
		MutationOwnerID:   strings.Repeat("b", 32),
		ManifestQualifier: manifest.Qualifier,
		TargetEngine:      manifest.TargetEngine, TargetEpoch: manifest.TargetEpoch,
		SourceRevision: manifest.SourceRevision, Topology: manifest.Topology,
		PeerIP: manifest.PeerIP, PeerNS: manifest.PeerNS,
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
	legacyRoot := journal
	legacyRoot.ConfigBefore = clonePDNSConfigSnapshots(journal.ConfigBefore)
	for index := range legacyRoot.ConfigBefore {
		if legacyRoot.ConfigBefore[index].Path == filepath.Clean(dnsMainConf) {
			legacyRoot.ConfigBefore[index].GID = 0
		}
	}
	if _, err := encodeDNSEngineSwitchJournal(legacyRoot); err != nil {
		t.Fatalf("root-owned 0640 main config rejected: %v", err)
	}
	unsafeManaged := journal
	unsafeManaged.ConfigBefore = clonePDNSConfigSnapshots(journal.ConfigBefore)
	for index := range unsafeManaged.ConfigBefore {
		if unsafeManaged.ConfigBefore[index].Path == filepath.Clean(dnsManagedConf) {
			unsafeManaged.ConfigBefore[index].GID = 109
		}
	}
	if _, err := encodeDNSEngineSwitchJournal(unsafeManaged); err == nil {
		t.Fatal("adoption journal accepted a non-root managed drop-in")
	}
	journal.PrimaryCatalogSerial = 1
	if _, err := encodeDNSEngineSwitchJournal(journal); err == nil {
		t.Fatal("legacy PowerDNS adoption accepted a primary catalog serial")
	}
	journal.PrimaryCatalogSerial = 0
	journal.TargetUnitsBefore[1].ActiveState = "active"
	if _, err := encodeDNSEngineSwitchJournal(journal); err == nil {
		t.Fatal("adoption journal accepted another active DNS engine")
	}
}

func TestDNSEngineSwitchJournalFaultHookBracketsDurableWrite(t *testing.T) {
	journal := testBINDSwitchJournal(t)
	var events []string
	var durable dnsEngineSwitchJournal
	exists := false
	err := writeDNSEngineSwitchJournalWithOps(
		journal,
		func(encoded []byte) error {
			events = append(events, "persist")
			decoded, err := decodeDNSEngineSwitchJournal(encoded)
			if err != nil {
				return err
			}
			durable, exists = decoded, true
			return nil
		},
		func() (dnsEngineSwitchJournal, bool, error) {
			events = append(events, "read")
			return durable, exists, nil
		},
		func(point string, got dnsEngineSwitchJournal) error {
			events = append(events, point)
			if got.Phase != journal.Phase ||
				got.MutationRequestID != journal.MutationRequestID ||
				got.TargetEngine != journal.TargetEngine {
				t.Fatalf("fault-hook context = %+v, want phase/request/target from %+v", got, journal)
			}
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(events, ",")
	want := strings.Join([]string{
		dnsEngineSwitchJournalFaultBeforeWrite,
		"persist",
		dnsEngineSwitchJournalFaultAfterWrite,
		"read",
	}, ",")
	if got != want {
		t.Fatalf("journal fault-hook events = %q, want %q", got, want)
	}
}

func TestDNSEngineSwitchJournalFaultHookBracketsAcceptedPersistError(t *testing.T) {
	journal := testBINDSwitchJournal(t)
	persistErr := errors.New("directory fsync failed after journal rename")
	var events []string
	var durable dnsEngineSwitchJournal
	readCalls := 0
	err := writeDNSEngineSwitchJournalWithOps(
		journal,
		func(encoded []byte) error {
			events = append(events, "persist")
			decoded, err := decodeDNSEngineSwitchJournal(encoded)
			if err != nil {
				return err
			}
			durable = decoded
			return persistErr
		},
		func() (dnsEngineSwitchJournal, bool, error) {
			readCalls++
			events = append(events, "read")
			return durable, true, nil
		},
		func(point string, _ dnsEngineSwitchJournal) error {
			events = append(events, point)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("verified persist error was not accepted: %v", err)
	}
	got := strings.Join(events, ",")
	want := strings.Join([]string{
		dnsEngineSwitchJournalFaultBeforeWrite,
		"persist",
		"read",
		dnsEngineSwitchJournalFaultAfterWrite,
	}, ",")
	if got != want || readCalls != 1 {
		t.Fatalf("accepted persist-error events = %q reads=%d, want %q/1", got, readCalls, want)
	}
}

func TestDNSEngineSwitchJournalFaultHookRejectsPersistErrorMismatch(t *testing.T) {
	journal := testBINDSwitchJournal(t)
	mismatch := journal
	mismatch.Phase = dnsSwitchPhaseSourceStopped
	persistErr := errors.New("journal persist failed before publication")
	var events []string
	err := writeDNSEngineSwitchJournalWithOps(
		journal,
		func([]byte) error {
			events = append(events, "persist")
			return persistErr
		},
		func() (dnsEngineSwitchJournal, bool, error) {
			events = append(events, "read")
			return mismatch, true, nil
		},
		func(point string, _ dnsEngineSwitchJournal) error {
			events = append(events, point)
			return nil
		},
	)
	if !errors.Is(err, persistErr) {
		t.Fatalf("mismatched persist error = %v, want persist sentinel", err)
	}
	got := strings.Join(events, ",")
	want := strings.Join([]string{
		dnsEngineSwitchJournalFaultBeforeWrite,
		"persist",
		"read",
	}, ",")
	if got != want {
		t.Fatalf("mismatched persist-error events = %q, want %q", got, want)
	}
}

func TestDNSEngineSwitchJournalFaultHookFailureOrdering(t *testing.T) {
	for _, point := range []string{
		dnsEngineSwitchJournalFaultBeforeWrite,
		dnsEngineSwitchJournalFaultAfterWrite,
	} {
		t.Run(point, func(t *testing.T) {
			journal := testBINDSwitchJournal(t)
			injected := errors.New("injected journal boundary failure")
			persistCalls, readCalls := 0, 0
			var durable dnsEngineSwitchJournal
			exists := false
			err := writeDNSEngineSwitchJournalWithOps(
				journal,
				func(encoded []byte) error {
					persistCalls++
					decoded, err := decodeDNSEngineSwitchJournal(encoded)
					if err != nil {
						return err
					}
					durable, exists = decoded, true
					return nil
				},
				func() (dnsEngineSwitchJournal, bool, error) {
					readCalls++
					return durable, exists, nil
				},
				func(actual string, _ dnsEngineSwitchJournal) error {
					if actual == point {
						return injected
					}
					return nil
				},
			)
			if !errors.Is(err, injected) {
				t.Fatalf("journal boundary error = %v, want injected sentinel", err)
			}
			wantPersist, wantExists := 0, false
			if point == dnsEngineSwitchJournalFaultAfterWrite {
				wantPersist, wantExists = 1, true
			}
			if persistCalls != wantPersist || readCalls != 0 || exists != wantExists {
				t.Fatalf(
					"boundary %s left persist=%d read=%d exists=%v, want %d/0/%v",
					point, persistCalls, readCalls, exists, wantPersist, wantExists,
				)
			}
		})
	}
}

func TestDNSEngineSwitchJournalAfterWritePreservesRollbackPrecursorSentinel(
	t *testing.T,
) {
	journal := testBINDSwitchJournal(t)
	persisted := false
	err := writeDNSEngineSwitchJournalWithOps(
		journal,
		func([]byte) error {
			persisted = true
			return nil
		},
		func() (dnsEngineSwitchJournal, bool, error) {
			t.Fatal("after-write injection must return before journal readback")
			return dnsEngineSwitchJournal{}, false, nil
		},
		func(point string, _ dnsEngineSwitchJournal) error {
			if point == dnsEngineSwitchJournalFaultAfterWrite {
				return fmt.Errorf(
					"tagged rollback precursor: %w",
					dnsEngineSwitchRollbackPrecursorError,
				)
			}
			return nil
		},
	)
	if !persisted || !errors.Is(err, dnsEngineSwitchRollbackPrecursorError) {
		t.Fatalf("persisted=%v error=%v, want durable sentinel", persisted, err)
	}
}

func TestDNSEngineSwitchJournalProductionWrapperUsesFaultHook(t *testing.T) {
	journal := testBINDSwitchJournal(t)
	injected := errors.New("production wrapper boundary")
	previous := dnsEngineSwitchJournalFaultHook
	t.Cleanup(func() { dnsEngineSwitchJournalFaultHook = previous })
	var seen string
	dnsEngineSwitchJournalFaultHook = func(
		driver string,
		point string,
		got dnsEngineSwitchJournal,
	) error {
		if driver != dnsEngineSwitchFaultDriverBIND {
			t.Fatalf("production fault-hook driver = %q", driver)
		}
		seen = point
		if got.Phase != journal.Phase ||
			got.MutationRequestID != journal.MutationRequestID {
			t.Fatalf("production fault-hook context = %+v", got)
		}
		return injected
	}
	err := writeDNSEngineSwitchJournalForFaultDriver(
		dnsEngineSwitchFaultDriverBIND, journal,
	)
	if !errors.Is(err, injected) ||
		seen != dnsEngineSwitchJournalFaultBeforeWrite {
		t.Fatalf("production wrapper error=%v point=%q", err, seen)
	}
}

func TestDNSEngineSwitchPreIntentFaultHookCarriesExactIdentity(t *testing.T) {
	journal := testBINDSwitchJournal(t)
	manifest, err := switchJournalManifest(journal)
	if err != nil {
		t.Fatal(err)
	}
	binding := switchJournalBinding(journal)
	injected := errors.New("pre-intent boundary")
	previous := dnsEngineSwitchJournalFaultHook
	t.Cleanup(func() { dnsEngineSwitchJournalFaultHook = previous })
	var seen dnsEngineSwitchJournal
	dnsEngineSwitchJournalFaultHook = func(
		driver string,
		point string,
		got dnsEngineSwitchJournal,
	) error {
		if driver != dnsEngineSwitchFaultDriverBIND {
			t.Fatalf("pre-intent driver = %q", driver)
		}
		if point != dnsEngineSwitchJournalFaultPreIntent {
			t.Fatalf("pre-intent point = %q", point)
		}
		seen = got
		return injected
	}
	err = runDNSEngineSwitchPreIntentFaultHook(
		dnsEngineSwitchFaultDriverBIND, manifest, binding,
	)
	if !errors.Is(err, injected) {
		t.Fatalf("pre-intent error = %v, want injected sentinel", err)
	}
	if seen.Phase != "pre-intent" ||
		seen.MutationRequestID != binding.MutationRequestID ||
		seen.MutationOwnerID != binding.MutationOwnerID ||
		seen.ManifestQualifier != manifest.Qualifier ||
		seen.SourceEngine != manifest.SourceEngine ||
		seen.TargetEngine != manifest.TargetEngine ||
		seen.Topology != manifest.Topology ||
		seen.PairRole != manifest.PairRole {
		t.Fatalf("pre-intent identity = %+v", seen)
	}
}
