//go:build linux

package dnsenginerecovery

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/pdnsnative"
	"github.com/alicelik/celikpanel/internal/transport"
	"golang.org/x/sys/unix"
)

type freshChangeFixtureV3 struct {
	policy  dnsengineartifact.JournalPolicy
	intent  dnsengineartifact.SwitchJournalV1
	staged  dnsengineartifact.SwitchJournalV1
	started dnsengineartifact.SwitchJournalV1
	native  dnsengineartifact.SwitchJournalV1
	running pdnsnative.Snapshot
	desired dnsengineartifact.StateV1
}

var freshChangeFrozenUnitV3 = dnsengineartifact.UnitSnapshot{Name: "pdns.service", LoadState: "loaded", ActiveState: "inactive", UnitFileState: "disabled"}

func newFreshChangeFixtureV3(t *testing.T) freshChangeFixtureV3 {
	t.Helper()
	policy, _, _ := switchFixture(t)
	raw, err := os.ReadFile(filepath.Join("..", "dnsengineartifact", "testdata", "switch-journal", "alpha81-pdns-switch.json"))
	if err != nil {
		t.Fatal(err)
	}
	base, err := policy.DecodeSwitchJournal(raw)
	if err != nil {
		t.Fatal(err)
	}
	commitment, err := mutationpayload.CanonicalDNSEngineSwitchManifestWithPairIdentity(
		transport.DNSEngineSwitchModeSwitch, "", transport.DNSEnginePowerDNS, 0, 1, 0,
		transport.DNSTopologyPaired, transport.DNSPairRolePrimary,
		"192.0.2.10", "ns1.example.test", "192.0.2.11", "ns2.example.test", nil)
	if err != nil {
		t.Fatal(err)
	}
	base.SourceEngine, base.SourceEpoch, base.TargetEpoch, base.SourceRevision = "", 0, 1, 0
	base.Topology, base.PairRole = commitment.Topology, commitment.PairRole
	base.LocalIP, base.LocalNS, base.PeerIP, base.PeerNS = commitment.LocalIP, commitment.LocalNS, commitment.PeerIP, commitment.PeerNS
	base.ManifestQualifier, base.SnapshotBytes, base.Zones = commitment.Qualifier, commitment.SnapshotBytes, commitment.Zones
	base.PrimaryCatalogSerial = 1
	base.StateBefore = dnsengineartifact.FileSnapshot{Path: policy.StatePath}
	base.SourceUnitsBefore = nil
	base.TargetUnitsBefore = []dnsengineartifact.UnitSnapshot{freshChangeFrozenUnitV3}
	after := append([]dnsengineartifact.FileSnapshot(nil), base.ConfigBefore...)
	for _, i := range []int{1, 2} {
		after[i] = dnsengineartifact.FileSnapshot{Path: base.ConfigBefore[i].Path, Exists: true, Mode: 0o644, OwnerKnown: true, Data: []byte("managed=1\n")}
		after[i].SHA256 = dnsengineartifact.DigestBytes(after[i].Data)
	}
	intent, err := policy.BuildPDNSFreshPrimaryJournalV3(base, after)
	if err != nil {
		t.Fatal(err)
	}
	measuredRaw, err := os.ReadFile(filepath.Join("..", "..", "deploy", "e2e", "dns-kill-matrix", "evidence", "pdns-master-bind-20260928", "debian-primary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var measured struct {
		Staged struct {
			SQLite pdnsnative.Snapshot `json:"sqlite"`
		} `json:"staged"`
		Running struct {
			SQLite pdnsnative.Snapshot `json:"sqlite"`
		} `json:"running"`
	}
	if err := json.Unmarshal(measuredRaw, &measured); err != nil {
		t.Fatal(err)
	}
	candidate := dnsengineartifact.PDNSTargetCandidateProofV4{Path: intent.PDNSCandidatePath, Device: 9, Inode: 11, Mode: 0o640, UID: 0, GID: 42, Size: 4096, SHA256: strings.Repeat("f", 64), NoSidecars: true}
	staged, err := policy.StagePDNSFreshPrimaryCandidateV3(intent, candidate, measured.Staged.SQLite)
	if err != nil {
		t.Fatal(err)
	}
	started := staged
	started.Phase = dnsengineartifact.SwitchPhaseTargetStarted
	if err := policy.ValidateSwitchJournal(started); err != nil {
		t.Fatal(err)
	}
	catalog, err := binddns.CatalogDomain(staged.LocalIP)
	if err != nil {
		t.Fatal(err)
	}
	native, err := policy.AttachPDNSFreshNativeObservationV3(started, catalog, measured.Running.SQLite)
	if err != nil {
		t.Fatal(err)
	}
	desired, err := dnsengineartifact.FreshPrimaryTargetStateV3(native)
	if err != nil {
		t.Fatal(err)
	}
	return freshChangeFixtureV3{policy: policy, intent: intent, staged: staged, started: started, native: native, running: measured.Running.SQLite, desired: desired}
}

// freshChangeHostV3 is an in-memory host as the install wrote it.
type freshChangeHostV3 struct {
	configs    []PDNSFreshConfigFindingV3
	configErr  error
	present    map[string]bool
	existsErr  map[string]error
	candidate  *dnsengineartifact.PDNSTargetCandidateProofV4
	candErr    error
	liveDiffer bool
	liveErr    error
	native     pdnsnative.Snapshot
	nativeErr  error
	state      *dnsengineartifact.StateV1
	stateErr   error
	ownership  *dnsengineartifact.StateV1
}

func freshChangeConfigs(j dnsengineartifact.SwitchJournalV1, state PDNSTargetConfigStateV4) []PDNSFreshConfigFindingV3 {
	configs := make([]PDNSFreshConfigFindingV3, len(j.ConfigBefore))
	for i := range configs {
		configs[i] = PDNSFreshConfigFindingV3{Path: j.ConfigBefore[i].Path, State: state}
	}
	return configs
}

func (h *freshChangeHostV3) observers(policy dnsengineartifact.JournalPolicy, j dnsengineartifact.SwitchJournalV1) FreshPrimaryChangeObserversV3 {
	return FreshPrimaryChangeObserversV3{
		Policy: policy,
		Configs: func(context.Context, dnsengineartifact.SwitchJournalV1) ([]PDNSFreshConfigFindingV3, error) {
			return h.configs, h.configErr
		},
		Exists: func(path string) (bool, error) {
			if err := h.existsErr[path]; err != nil {
				return false, err
			}
			return h.present[path], nil
		},
		Candidate: func(string) (dnsengineartifact.PDNSTargetCandidateProofV4, error) {
			if h.candidate != nil {
				return *h.candidate, h.candErr
			}
			return *j.PDNSFreshPlan.Candidate, h.candErr
		},
		Live: func(dnsengineartifact.PDNSTargetCandidateProofV4, string) (bool, error) {
			return h.liveDiffer, h.liveErr
		},
		Native: func(context.Context, dnsengineartifact.SwitchJournalV1) (pdnsnative.Snapshot, error) {
			return h.native, h.nativeErr
		},
		State:     func() (*dnsengineartifact.StateV1, error) { return h.state, h.stateErr },
		Ownership: func() (*dnsengineartifact.StateV1, error) { return h.ownership, nil },
	}
}

func cloneSnapshotWithExtraRecord(t *testing.T, s pdnsnative.Snapshot) pdnsnative.Snapshot {
	t.Helper()
	out := pdnsnative.Snapshot{Schema: s.Schema, Tables: map[string][][]any{}}
	for name, rows := range s.Tables {
		out.Tables[name] = append([][]any(nil), rows...)
	}
	rows := out.Tables["records"]
	if len(rows) == 0 {
		t.Fatal("measured snapshot has no records table rows")
	}
	extra := append([]any(nil), rows[len(rows)-1]...)
	out.Tables["records"] = append(rows, extra)
	return out
}

func TestRecordedFreshPrimaryOwnerChangeV3ReadsTheAgentWords(t *testing.T) {
	for _, what := range []string{FreshPrimaryChangedConfigV3, FreshPrimaryChangedDatabaseV3, FreshPrimaryChangedStateV3} {
		message := FreshPrimaryOwnerChangePrefixV3 + what + FreshPrimaryOwnerChangeSuffixV3 + " (an administrator edit). CelikPanel kept that change."
		if got, ok := RecordedFreshPrimaryOwnerChangeV3(message); !ok || got != what {
			t.Fatalf("recorded %q = %q %v", what, got, ok)
		}
	}
	for _, message := range []string{
		"", "The first install of PowerDNS as the paired primary was interrupted before PowerDNS started.",
		"The interrupted DNS switch could not be verified after the Agent restarted.",
	} {
		if got, ok := RecordedFreshPrimaryOwnerChangeV3(message); ok {
			t.Fatalf("non-change message classified as %q", got)
		}
	}
}

func TestClassifyFreshPrimaryChangeV3Prestart(t *testing.T) {
	f := newFreshChangeFixtureV3(t)
	db := f.policy.PDNSDatabasePath
	candidate := f.staged.PDNSFreshPlan.Candidate.Path
	mainPath := f.staged.ConfigBefore[0].Path
	clean := func() *freshChangeHostV3 {
		return &freshChangeHostV3{configs: freshChangeConfigs(f.staged, PDNSTargetConfigAfterV4), present: map[string]bool{candidate: true}}
	}
	for _, tc := range []struct {
		name  string
		j     dnsengineartifact.SwitchJournalV1
		unit  dnsengineartifact.UnitSnapshot
		host  func() *freshChangeHostV3
		want  FreshPrimaryChangeV3
		what  string // unknown observation, when set
		noCmp bool
	}{
		{name: "as the install wrote it", j: f.staged, host: clean, want: FreshPrimaryChangeV3{Prestart: true, Compared: true}},
		{name: "configuration edited", j: f.staged, host: func() *freshChangeHostV3 {
			h := clean()
			h.configs[0] = PDNSFreshConfigFindingV3{Path: mainPath, Differs: true}
			return h
		}, want: FreshPrimaryChangeV3{Prestart: true, Compared: true, ConfigPaths: []string{mainPath}}},
		{name: "state record written", j: f.staged, host: func() *freshChangeHostV3 {
			h := clean()
			h.present[f.policy.StatePath] = true
			return h
		}, want: FreshPrimaryChangeV3{Prestart: true, Compared: true, StateRecord: true}},
		{name: "sidecar beside the never-started database", j: f.staged, host: func() *freshChangeHostV3 {
			h := clean()
			h.present[db+"-journal"] = true
			return h
		}, want: FreshPrimaryChangeV3{Prestart: true, Compared: true, Database: true}},
		{name: "staged copy edited", j: f.staged, host: func() *freshChangeHostV3 {
			h := clean()
			edited := *f.staged.PDNSFreshPlan.Candidate
			edited.SHA256 = strings.Repeat("a", 64)
			h.candidate = &edited
			return h
		}, want: FreshPrimaryChangeV3{Prestart: true, Compared: true, Database: true}},
		{name: "staged copy removed", j: f.staged, host: func() *freshChangeHostV3 {
			h := clean()
			h.present[candidate] = false
			return h
		}, want: FreshPrimaryChangeV3{Prestart: true, Compared: true, Database: true}},
		{name: "renamed live database edited", j: f.staged, host: func() *freshChangeHostV3 {
			h := clean()
			h.present = map[string]bool{db: true}
			h.liveDiffer = true
			return h
		}, want: FreshPrimaryChangeV3{Prestart: true, Compared: true, Database: true}},
		{name: "renamed live database unreadable", j: f.staged, host: func() *freshChangeHostV3 {
			h := clean()
			h.present = map[string]bool{db: true}
			h.liveErr = errors.New("permission denied")
			return h
		}, what: FreshPrimaryChangedDatabaseV3},
		{name: "configuration unreadable", j: f.staged, host: func() *freshChangeHostV3 {
			h := clean()
			h.configErr = errors.New("PowerDNS config /etc/powerdns/pdns.conf could not be read")
			return h
		}, what: FreshPrimaryChangedConfigV3},
		{name: "state record unreadable", j: f.staged, host: func() *freshChangeHostV3 {
			h := clean()
			h.existsErr = map[string]error{f.policy.StatePath: errors.New("input/output error")}
			return h
		}, what: FreshPrimaryChangedStateV3},
		{name: "intent: configuration already at the after-image", j: f.intent, host: func() *freshChangeHostV3 {
			return &freshChangeHostV3{configs: freshChangeConfigs(f.intent, PDNSTargetConfigAfterV4), present: map[string]bool{}}
		}, want: FreshPrimaryChangeV3{Prestart: true, Compared: true, ConfigPaths: []string{
			f.intent.ConfigBefore[0].Path, f.intent.ConfigBefore[1].Path, f.intent.ConfigBefore[2].Path,
		}}},
		{name: "unit is not a stopped never-started target", j: f.staged,
			unit: dnsengineartifact.UnitSnapshot{Name: "pdns.service", LoadState: "loaded", ActiveState: "failed", UnitFileState: "disabled"},
			host: clean, want: FreshPrimaryChangeV3{Prestart: true}, noCmp: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			unit := tc.unit
			if unit == (dnsengineartifact.UnitSnapshot{}) {
				unit = freshChangeFrozenUnitV3
			}
			host := tc.host()
			got, err := ClassifyFreshPrimaryChangeV3(context.Background(), tc.j, unit, host.observers(f.policy, tc.j))
			if tc.what != "" {
				var unknown *FreshPrimaryUnknownV3
				if !errors.As(err, &unknown) || unknown.What != tc.what || got.Changed() || got.Compared {
					t.Fatalf("want unknown %q, got %+v %v", tc.what, got, err)
				}
				return
			}
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v %v, want %+v", got, err, tc.want)
			}
			if tc.noCmp && got.Compared {
				t.Fatal("comparison ran although the Agent's route stops before it")
			}
		})
	}
}

func TestClassifyFreshPrimaryChangeV3Poststart(t *testing.T) {
	f := newFreshChangeFixtureV3(t)
	running := dnsengineartifact.UnitSnapshot{Name: "pdns.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"}
	clean := func(j dnsengineartifact.SwitchJournalV1) *freshChangeHostV3 {
		return &freshChangeHostV3{configs: freshChangeConfigs(j, PDNSTargetConfigAfterV4), native: f.running}
	}
	desired := f.desired
	foreign := desired
	foreign.PrimaryCatalogSerial++
	for _, j := range []dnsengineartifact.SwitchJournalV1{f.started, f.native} {
		t.Run(j.Phase, func(t *testing.T) {
			host := clean(j)
			host.state, host.ownership = &desired, &desired
			got, err := ClassifyFreshPrimaryChangeV3(context.Background(), j, running, host.observers(f.policy, j))
			if err != nil || got.Prestart || !got.Compared || got.Changed() {
				t.Fatalf("clean after-start install: %+v %v", got, err)
			}
			host = clean(j)
			host.native = cloneSnapshotWithExtraRecord(t, f.running)
			got, err = ClassifyFreshPrimaryChangeV3(context.Background(), j, running, host.observers(f.policy, j))
			if err != nil || !got.Database || got.StateRecord || len(got.ConfigPaths) != 0 {
				t.Fatalf("added database row not classified: %+v %v", got, err)
			}
			host = clean(j)
			host.state = &foreign
			got, err = ClassifyFreshPrimaryChangeV3(context.Background(), j, running, host.observers(f.policy, j))
			if err != nil || !got.StateRecord || got.Database || got.OwnershipRecord {
				t.Fatalf("foreign state record not classified: %+v %v", got, err)
			}
			host = clean(j)
			host.ownership = &foreign
			got, err = ClassifyFreshPrimaryChangeV3(context.Background(), j, running, host.observers(f.policy, j))
			if err != nil || !got.OwnershipRecord || got.StateRecord {
				t.Fatalf("foreign ownership record not classified: %+v %v", got, err)
			}
			// A file left at its pre-install image differs after start
			// when the install wrote another one.
			host = clean(j)
			host.configs[1].State = PDNSTargetConfigBeforeV4
			got, err = ClassifyFreshPrimaryChangeV3(context.Background(), j, running, host.observers(f.policy, j))
			if err != nil || !reflect.DeepEqual(got.ConfigPaths, []string{j.ConfigBefore[1].Path}) {
				t.Fatalf("pre-install image after start not classified: %+v %v", got, err)
			}
			host = clean(j)
			host.nativeErr = errors.New("database is locked")
			got, err = ClassifyFreshPrimaryChangeV3(context.Background(), j, running, host.observers(f.policy, j))
			var unknown *FreshPrimaryUnknownV3
			if !errors.As(err, &unknown) || unknown.What != FreshPrimaryChangedDatabaseV3 || got.Changed() {
				t.Fatalf("busy database not unknown: %+v %v", got, err)
			}
			host = clean(j)
			host.stateErr = errors.New("state record owner differs")
			if _, err = ClassifyFreshPrimaryChangeV3(context.Background(), j, running, host.observers(f.policy, j)); !errors.As(err, &unknown) || unknown.What != FreshPrimaryChangedStateV3 {
				t.Fatalf("unreadable state record not unknown: %v", err)
			}
		})
	}
	// A target-enable-intent journal whose unit is running goes forward, as
	// the Agent routes it; stopped, it stays on the pre-start route.
	enable := f.staged
	enable.Phase = dnsengineartifact.SwitchPhaseTargetEnableIntent
	if err := f.policy.ValidateSwitchJournal(enable); err != nil {
		t.Fatal(err)
	}
	host := clean(enable)
	got, err := ClassifyFreshPrimaryChangeV3(context.Background(), enable, running, host.observers(f.policy, enable))
	if err != nil || got.Prestart || !got.Compared {
		t.Fatalf("running enable-intent not routed forward: %+v %v", got, err)
	}
	host = &freshChangeHostV3{configs: freshChangeConfigs(enable, PDNSTargetConfigAfterV4), present: map[string]bool{enable.PDNSFreshPlan.Candidate.Path: true}}
	got, err = ClassifyFreshPrimaryChangeV3(context.Background(), enable, freshChangeFrozenUnitV3, host.observers(f.policy, enable))
	if err != nil || !got.Prestart || !got.Compared || got.Changed() {
		t.Fatalf("stopped enable-intent not routed pre-start: %+v %v", got, err)
	}
}

// The per-path configuration comparison tells a file that differs from one
// that could not be read, with the same secure observer the Agent uses.
func TestClassifyPDNSFreshConfigPassV3DiffersVersusUnreadable(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-owned native config fixture requires root")
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"etc", "etc/powerdns", "etc/powerdns/pdns.d"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	const gid = 12345
	mainFile := filepath.Join(root, "etc/powerdns/pdns.conf")
	if err := os.WriteFile(mainFile, []byte("before-main\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(mainFile, 0, gid); err != nil {
		t.Fatal(err)
	}
	before := []dnsengineartifact.FileSnapshot{
		{Path: "/etc/powerdns/pdns.conf", Exists: true, Mode: 0o640, OwnerKnown: true, UID: 0, GID: gid, Data: []byte("before-main\n")},
		{Path: "/etc/powerdns/pdns.d/celikpanel-cluster.conf"},
		{Path: "/etc/powerdns/pdns.d/celikpanel.conf"},
	}
	after := []dnsengineartifact.FileSnapshot{
		{Path: before[0].Path, Exists: true, Mode: 0o640, OwnerKnown: true, UID: 0, GID: gid, Data: []byte("after-main\n")},
		{Path: before[1].Path},
		{Path: before[2].Path, Exists: true, Mode: 0o644, OwnerKnown: true, UID: 0, GID: 0, Data: []byte("after-managed\n")},
	}
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	got, err := classifyPDNSFreshConfigPassV3(context.Background(), fd, before, after)
	if err != nil || got[0].State != PDNSTargetConfigBeforeV4 || got[0].Differs || got[2].State != PDNSTargetConfigBeforeV4 {
		t.Fatalf("unchanged files: %+v %v", got, err)
	}
	// The owner appends a line: same owner and mode, other bytes.
	if err := os.WriteFile(mainFile, []byte("before-main\n# owner note\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	got, err = classifyPDNSFreshConfigPassV3(context.Background(), fd, before, after)
	if err != nil || !got[0].Differs || got[1].Differs || got[2].Differs {
		t.Fatalf("owner edit not a difference: %+v %v", got, err)
	}
	// Reverted: back to the before-image.
	if err := os.WriteFile(mainFile, []byte("before-main\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if got, err = classifyPDNSFreshConfigPassV3(context.Background(), fd, before, after); err != nil || got[0].Differs {
		t.Fatalf("reverted edit still differs: %+v %v", got, err)
	}
	// A symbolic link at a path the install left absent is a difference.
	if err := os.Symlink("/etc/passwd", filepath.Join(root, "etc/powerdns/pdns.d/celikpanel.conf")); err != nil {
		t.Fatal(err)
	}
	if got, err = classifyPDNSFreshConfigPassV3(context.Background(), fd, before, after); err != nil || !got[2].Differs {
		t.Fatalf("symlink not a difference: %+v %v", got, err)
	}
	if err := os.Remove(filepath.Join(root, "etc/powerdns/pdns.d/celikpanel.conf")); err != nil {
		t.Fatal(err)
	}
	// An unsafe parent directory cannot be read securely: unknown, never a
	// difference.
	if err := os.Chmod(filepath.Join(root, "etc/powerdns"), 0o775); err != nil {
		t.Fatal(err)
	}
	if got, err = classifyPDNSFreshConfigPassV3(context.Background(), fd, before, after); err == nil || !strings.Contains(err.Error(), "could not be read") {
		t.Fatalf("unsafe parent classified: %+v %v", got, err)
	}
}
