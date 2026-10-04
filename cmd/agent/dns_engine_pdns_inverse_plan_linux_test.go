//go:build linux

package main

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/bindconfig"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/hostplatform"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

// Rejected inputs must stop before the helper reads or mutates any native file.
func TestPrepareStandalonePDNSTargetInverseIntentV4RejectsUnsupportedScope(t *testing.T) {
	profile := hostplatform.Profile{
		DistroFamily:   hostplatform.DistroFamilyDebian,
		PackageManager: hostplatform.PackageManagerAPT,
		ServiceManager: hostplatform.ServiceManagerSystemd,
	}
	base := dnsEngineSwitchJournal{
		Schema:            dnsengineartifact.SwitchJournalSchemaV1,
		Phase:             dnsSwitchPhaseIntent,
		Mode:              transport.DNSEngineSwitchModeSwitch,
		Topology:          transport.DNSTopologyStandalone,
		SourceEngine:      transport.DNSEngineBIND,
		TargetEngine:      transport.DNSEnginePowerDNS,
		StateBefore:       dnsFileSnapshot{Exists: true},
		TargetUnitsBefore: []dnsUnitSnapshot{{Name: "pdns.service", LoadState: "loaded", ActiveState: "inactive", UnitFileState: "disabled"}},
		SourceUnitsBefore: []dnsUnitSnapshot{
			{Name: "bind9.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"},
			{Name: "named.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"},
		},
	}
	cases := []struct {
		name   string
		change func(*hostplatform.Profile, *dnsEngineSwitchJournal)
		want   string
	}{
		{"pacman", func(p *hostplatform.Profile, _ *dnsEngineSwitchJournal) {
			p.PackageManager = hostplatform.PackageManagerPacman
		}, "Debian-family"},
		{"paired", func(_ *hostplatform.Profile, j *dnsEngineSwitchJournal) { j.Topology = transport.DNSTopologyPaired }, "standalone"},
		{"reinstall", func(_ *hostplatform.Profile, j *dnsEngineSwitchJournal) {
			j.Mode = transport.DNSEngineSwitchModeReinstall
		}, "standalone"},
		{"unmanaged source", func(_ *hostplatform.Profile, j *dnsEngineSwitchJournal) { j.StateBefore.Exists = false }, "managed BIND"},
		{"prior database", func(_ *hostplatform.Profile, j *dnsEngineSwitchJournal) {
			j.PDNSBackupSHA256 = strings.Repeat("a", 64)
			j.PDNSBackupSize = 1
		}, "absent prior"},
		{"active target", func(_ *hostplatform.Profile, j *dnsEngineSwitchJournal) {
			j.TargetUnitsBefore[0].ActiveState = "active"
		}, "inactive PowerDNS"},
		{"uninstalled target", func(_ *hostplatform.Profile, j *dnsEngineSwitchJournal) {
			j.TargetUnitsBefore[0].LoadState = "not-found"
		}, "inactive PowerDNS"},
		{"bind alias only", func(_ *hostplatform.Profile, j *dnsEngineSwitchJournal) {
			j.SourceUnitsBefore[1].ActiveState = "inactive"
		}, "running BIND"},
		{"autostart target", func(_ *hostplatform.Profile, j *dnsEngineSwitchJournal) {
			j.TargetUnitsBefore[0].UnitFileState = "enabled"
		}, "inactive PowerDNS"},
		{"stopped source", func(_ *hostplatform.Profile, j *dnsEngineSwitchJournal) {
			j.SourceUnitsBefore[0].ActiveState = "inactive"
			j.SourceUnitsBefore[1].ActiveState = "inactive"
		}, "running BIND"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, j := profile, base
			j.TargetUnitsBefore = append([]dnsUnitSnapshot(nil), base.TargetUnitsBefore...)
			j.SourceUnitsBefore = append([]dnsUnitSnapshot(nil), base.SourceUnitsBefore...)
			tc.change(&p, &j)
			_, err := prepareStandalonePDNSTargetInverseIntentV4(
				context.Background(), p, j, pdnsConfigMutation{},
			)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("unexpected admission: %v", err)
			}
		})
	}
}

func TestStageStandalonePDNSTargetCandidateV4RefusesUnsealedIntent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "candidate.sqlite3")
	intent := dnsEngineSwitchJournal{
		Schema:            dnsengineartifact.SwitchJournalSchemaV4,
		Phase:             dnsSwitchPhaseIntent,
		PDNSCandidatePath: path,
		PDNSTargetPlan:    &dnsengineartifact.PDNSTargetInversePlanV4{},
	}
	if _, err := stageStandalonePDNSTargetCandidateWithSourceCheckV4(context.Background(), intent, func(context.Context, dnsengineartifact.ManagedBINDSourceProofV4) error { return nil }); err == nil {
		t.Fatal("unsealed V4 intent was accepted as staged")
	}
}

func TestVerifyPDNSTargetStageFilesystemV4RefusesDifferentDevice(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-owned private staging requires root")
	}
	if _, err := os.Stat("/dev/shm"); err != nil {
		t.Skip("no independent tmpfs mount available")
	}
	privateDir := filepath.Join(t.TempDir(), "agent-private")
	if err := os.Mkdir(privateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CELIKPANEL_PDNS_DB", "/dev/shm/pdns-v4-test.sqlite3")
	err := verifyPDNSTargetStageFilesystemV4(filepath.Join(privateDir, "candidate.sqlite3"))
	if err == nil {
		t.Skip("private test directory and /dev/shm use the same filesystem")
	}
	if !strings.Contains(err.Error(), "different filesystems") {
		t.Fatalf("unexpected stage preflight failure: %v", err)
	}
}

// TestStageStandalonePDNSTargetCandidateV4BindsExactFile proves that a valid
// intent gains a candidate receipt before any service operation is invoked.
func TestStageStandalonePDNSTargetCandidateV4BindsExactFile(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-owned private staging requires root")
	}
	useTestServiceMutationOwner(t)
	root := t.TempDir()
	privateRoot := filepath.Join(root, "agent-private")
	pdnsRoot := filepath.Join(root, "powerdns")
	if err := os.Mkdir(privateRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(pdnsRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CELIKPANEL_AGENT_STATE_DIR", privateRoot)
	t.Setenv("CELIKPANEL_PDNS_DB", filepath.Join(pdnsRoot, "pdns.sqlite3"))
	priorMain, priorManaged, priorCluster := dnsMainConf, dnsManagedConf, dnsClusterConf
	dnsMainConf = filepath.Join(root, "pdns.conf")
	dnsManagedConf = filepath.Join(root, "pdns.d", "celikpanel.conf")
	dnsClusterConf = filepath.Join(root, "pdns.d", "celikpanel-cluster.conf")
	t.Cleanup(func() {
		dnsMainConf, dnsManagedConf, dnsClusterConf = priorMain, priorManaged, priorCluster
	})
	makeSnapshot := func(path string, data string) dnsFileSnapshot {
		return dnsFileSnapshot{
			Path: filepath.Clean(path), Exists: true, Mode: 0o644,
			OwnerKnown: true, UID: 0, GID: 0,
			Data: []byte(data), SHA256: digestDNSBytes([]byte(data)),
		}
	}
	manifest, err := mutationpayload.CanonicalDNSEngineSwitchManifest(
		transport.DNSEngineSwitchModeSwitch,
		transport.DNSEngineBIND, transport.DNSEnginePowerDNS,
		1, 2, 0, transport.DNSTopologyStandalone, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	state := legacyDurableDNSState(transport.DNSEngineBIND)
	stateBytes, err := encodeDNSEngineState(state)
	if err != nil {
		t.Fatal(err)
	}
	requestID := strings.Repeat("e", 32)
	mainBefore := makeSnapshot(dnsMainConf, "main before\n")
	mainBefore.Mode = 0o640
	mainAfter := makeSnapshot(dnsMainConf, "main after\n")
	mainAfter.Mode = 0o640
	managedAfter := makeSnapshot(dnsManagedConf, "managed\n")
	before := []dnsFileSnapshot{mainBefore, {Path: filepath.Clean(dnsManagedConf)}, {Path: filepath.Clean(dnsClusterConf)}}
	after := []dnsFileSnapshot{mainAfter, managedAfter, {Path: filepath.Clean(dnsClusterConf)}}
	sort.Slice(before, func(i, j int) bool { return before[i].Path < before[j].Path })
	sort.Slice(after, func(i, j int) bool { return after[i].Path < after[j].Path })
	base := dnsEngineSwitchJournal{
		Schema: dnsengineartifact.SwitchJournalSchemaV1,
		Phase:  dnsSwitchPhaseIntent, Mode: manifest.Mode,
		MutationRequestID: requestID, MutationOwnerID: strings.Repeat("f", 32),
		ManifestQualifier: manifest.Qualifier,
		SourceEngine:      manifest.SourceEngine, TargetEngine: manifest.TargetEngine,
		SourceEpoch: manifest.SourceEpoch, TargetEpoch: manifest.TargetEpoch,
		SourceRevision: manifest.SourceRevision, Topology: manifest.Topology,
		SnapshotBytes: manifest.SnapshotBytes, Zones: manifest.Zones,
		StateBefore:       testDNSEngineStateSnapshot(dnsEngineStatePath(), stateBytes),
		ConfigBefore:      before,
		TargetUnitsBefore: []dnsUnitSnapshot{{Name: "pdns.service", LoadState: "loaded", ActiveState: "inactive", UnitFileState: "disabled"}},
		SourceUnitsBefore: []dnsUnitSnapshot{
			{Name: "bind9.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"},
			{Name: "named.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"},
		},
		PDNSCandidatePath: pdnsSwitchCandidatePath(requestID),
		PDNSBackupPath:    pdnsSwitchBackupPath(requestID),
	}
	privateCandidate := pdnsSwitchCandidatePathV4(requestID)
	if filepath.Dir(privateCandidate) != privateRoot || filepath.Dir(privateCandidate) == pdnsRoot ||
		privateCandidate == base.PDNSCandidatePath {
		t.Fatal("V4 candidate must be under the private agent state parent")
	}
	managedLeaf, err := bindconfig.ManagedZoneInclude("// local\n", "/var/cache/bind/celikpanel/current/zones.conf")
	if err != nil {
		t.Fatal(err)
	}
	source := dnsengineartifact.ManagedBINDSourceProofV4{
		Kind:       dnsengineartifact.ManagedBINDSourceProofKindV4,
		HostLayout: "apt", Generation: state.Generation, EngineEpoch: state.EngineEpoch,
		ReceiptSHA256: strings.Repeat("a", 64),
		ConfigBefore: []dnsFileSnapshot{
			makeSnapshot("/etc/bind/named.conf", "include \"/etc/bind/named.conf.options\";\ninclude \"/etc/bind/named.conf.local\";\ninclude \"/etc/bind/named.conf.default-zones\";\n"),
			makeSnapshot("/etc/bind/named.conf.default-zones", "// defaults\n"),
			makeSnapshot("/etc/bind/named.conf.local", managedLeaf),
			makeSnapshot("/etc/bind/named.conf.options", "bind options\n"),
		},
	}
	if _, err := buildStandalonePDNSTargetInverseIntentV4(base, after, source); err == nil {
		t.Fatal("V4 intent was built before the candidate existed")
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(privateCandidate)+"?mode=rwc")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE proof (value TEXT NOT NULL); INSERT INTO proof(value) VALUES ('candidate');"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(privateCandidate, 0o600); err != nil {
		t.Fatal(err)
	}
	candidateBytes, err := os.ReadFile(privateCandidate)
	if err != nil {
		t.Fatal(err)
	}
	intent, err := buildStandalonePDNSTargetInverseIntentV4(base, after, source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(privateRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := stageStandalonePDNSTargetCandidateWithSourceCheckV4(context.Background(), intent, func(context.Context, dnsengineartifact.ManagedBINDSourceProofV4) error { return nil }); err == nil {
		t.Fatal("daemon-accessible staging parent accepted")
	}
	if err := os.Chmod(privateRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	drift := errors.New("owner changed BIND")
	if _, err := stageStandalonePDNSTargetCandidateWithSourceCheckV4(
		context.Background(), intent,
		func(context.Context, dnsengineartifact.ManagedBINDSourceProofV4) error { return drift },
	); !errors.Is(err, drift) {
		t.Fatalf("changed BIND source did not block stage: %v", err)
	}
	staged, err := stageStandalonePDNSTargetCandidateWithSourceCheckV4(context.Background(), intent, func(context.Context, dnsengineartifact.ManagedBINDSourceProofV4) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if staged.Phase != dnsSwitchPhaseTargetStaged || staged.PDNSTargetPlan.Candidate == nil ||
		staged.PDNSTargetPlan.Candidate.Path != intent.PDNSCandidatePath ||
		staged.PDNSTargetPlan.Candidate.SHA256 != digestDNSBytes(candidateBytes) {
		t.Fatal("candidate was not bound to exact staged file")
	}
	if _, err := encodeDNSEngineSwitchJournal(staged); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(intent.PDNSCandidatePath, []byte("owner changed the candidate"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := stageStandalonePDNSTargetCandidateWithSourceCheckV4(context.Background(), intent, func(context.Context, dnsengineartifact.ManagedBINDSourceProofV4) error { return nil }); err == nil {
		t.Fatal("changed candidate passed the frozen intent check")
	}
}
