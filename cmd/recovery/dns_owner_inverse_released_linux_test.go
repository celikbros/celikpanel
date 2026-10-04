//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/bindconfig"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
	"github.com/alicelik/celikpanel/internal/transport"
)

// These component tests start each owner inverse command from the state the
// restarted Agent leaves behind: its deliberate lease release in the ledger and
// the retained rolling-back journal. They use the commands' own durable-effect
// wiring on a private state root; native DNS is stubbed, so they are not
// native evidence.

const releasedInverseInstalledStatePath = "/var/lib/celikpanel-agent-private/dns-engine-state.json"

type releasedInverseHost struct {
	root   string
	owner  servicemutationledger.FileOwner
	policy dnsengineartifact.JournalPolicy
}

func newReleasedInverseHost(t *testing.T) releasedInverseHost {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	owner := servicemutationledger.FileOwner{UID: uint32(os.Getuid()), GID: uint32(os.Getgid())}
	return releasedInverseHost{root: root, owner: owner, policy: releasedInversePolicy(filepath.Join(root, "dns-engine-state.json"), owner)}
}

func releasedInversePolicy(statePath string, owner servicemutationledger.FileOwner) dnsengineartifact.JournalPolicy {
	return dnsengineartifact.JournalPolicy{
		StatePath: statePath, StateUID: owner.UID, StateGID: owner.GID, RequireOwner: true,
		PDNSMainPath:     "/etc/powerdns/pdns.conf",
		PDNSManagedPath:  "/etc/powerdns/pdns.d/celikpanel.conf",
		PDNSClusterPath:  "/etc/powerdns/pdns.d/celikpanel-cluster.conf",
		PDNSDatabasePath: "/var/lib/powerdns/pdns.sqlite3",
	}
}

func (h releasedInverseHost) write(t *testing.T, name string, raw []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(h.root, name), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

// files returns every durable document of the private root, so a refused or
// repeated command can be checked for the absence of any write.
func (h releasedInverseHost) files(t *testing.T) map[string][]byte {
	t.Helper()
	entries, err := os.ReadDir(h.root)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for _, entry := range entries {
		raw, err := os.ReadFile(filepath.Join(h.root, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		files[entry.Name()] = raw
	}
	return files
}

// stage writes the restarted Agent's release: an idle ledger whose exact job
// is a terminal failure with the given reason, beside the rolling-back journal.
func (h releasedInverseHost) stage(t *testing.T, journal dnsengineartifact.SwitchJournalV1, code string) {
	t.Helper()
	raw, err := h.policy.EncodeSwitchJournal(journal)
	if err != nil {
		t.Fatal(err)
	}
	h.write(t, "dns-engine-switch-journal.json", raw)
	now := time.Now().UTC().Truncate(time.Second)
	job := &transport.ServiceMutationJob{
		RequestID: journal.MutationRequestID, OwnerID: journal.MutationOwnerID,
		Kind: "dns_engine_switch", Target: string(journal.TargetEngine), PackageName: journal.ManifestQualifier,
		Status: servicemutationledger.StatusFailed, Phase: "interrupted", Attempt: 1,
		StartedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Minute), FinishedAt: now.Add(-time.Minute),
		DeadlineAt: now.Add(time.Hour), ErrorCode: code,
		ErrorMessage: "The interrupted DNS switch could not be verified after the Agent restarted.",
	}
	ledger := servicemutationledger.Ledger{Version: servicemutationledger.Version, Jobs: map[string]*transport.ServiceMutationJob{job.RequestID: job}}
	id := dnsengineartifact.SwitchIdentity{RequestID: job.RequestID, OwnerID: job.OwnerID, Target: journal.TargetEngine, Qualifier: journal.ManifestQualifier}
	if code == dnsengineartifact.ReleasedNativeUnknownCode && !id.ReleasedUndecidedJob(ledger) {
		t.Fatal("fixture ledger is not the Agent's exact release")
	}
	ledgerRaw, err := servicemutationledger.Encode(&ledger)
	if err != nil {
		t.Fatal(err)
	}
	h.write(t, "service-mutations.json", ledgerRaw)
	evidence, present, err := dnsenginerecovery.ReadSwitchEvidence(h.root, h.owner, h.policy, time.Now().UTC())
	if err != nil || !present || evidence.Observation.Status != dnsenginerecovery.EvidenceReleasedUndecided {
		t.Fatalf("fixture is not a released-undecided start: present=%v status=%q err=%v", present, evidence.Observation.Status, err)
	}
}

func (h releasedInverseHost) targetState(t *testing.T, j dnsengineartifact.SwitchJournalV1) []byte {
	t.Helper()
	raw, err := dnsengineartifact.CanonicalV1(dnsengineartifact.StateV1{
		Schema: dnsengineartifact.StateSchemaV1, Mode: j.Mode, Engine: j.TargetEngine,
		EngineEpoch: j.TargetEpoch, Generation: j.TargetGeneration, SourceRevision: j.SourceRevision,
		ManifestQualifier: j.ManifestQualifier, MutationRequestID: j.MutationRequestID, MutationOwnerID: j.MutationOwnerID,
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func readSwitchJournalFixture(t *testing.T, name string) dnsengineartifact.SwitchJournalV1 {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "internal", "dnsengineartifact", "testdata", "switch-journal", name))
	if err != nil {
		t.Fatal(err)
	}
	installed := releasedInversePolicy(releasedInverseInstalledStatePath, servicemutationledger.FileOwner{})
	installed.RequireOwner = false
	journal, err := installed.DecodeSwitchJournal(raw)
	if err != nil {
		t.Fatal(err)
	}
	return journal
}

func releasedConfigSnapshot(path string, gid uint32, data []byte) dnsengineartifact.FileSnapshot {
	return dnsengineartifact.FileSnapshot{Path: path, Exists: true, Mode: 0o644, OwnerKnown: true, GID: gid, Data: data, SHA256: dnsengineartifact.DigestBytes(data)}
}

// releasedPDNSAdoptionJournal is the retained adoption journal after the
// Agent's rollback decision; the PowerDNS target receipt is still exact.
func releasedPDNSAdoptionJournal(t *testing.T, h releasedInverseHost) dnsengineartifact.SwitchJournalV1 {
	t.Helper()
	j := readSwitchJournalFixture(t, "alpha81-pdns-adopt.json")
	j.StateBefore.Path = h.policy.StatePath
	j.Phase = dnsengineartifact.SwitchPhaseRollingBack
	if err := h.policy.ValidateSwitchJournal(j); err != nil {
		t.Fatal(err)
	}
	h.write(t, "dns-engine-state.json", h.targetState(t, j))
	return j
}

// releasedBINDSwitchJournal is a managed PowerDNS-to-BIND switch whose V2
// inverse the Agent refuses by design; the BIND target receipt is exact and
// the PowerDNS ownership receipt still matches the frozen source.
func releasedBINDSwitchJournal(t *testing.T, h releasedInverseHost) dnsengineartifact.SwitchJournalV1 {
	t.Helper()
	return releasedBINDSwitchJournalWithTargets(t, h, nil)
}

// releasedBINDSwitchJournalWithTargets freezes targets as the BIND unit
// preimage when it is non-nil; nil keeps the fixture's absent units.
func releasedBINDSwitchJournalWithTargets(t *testing.T, h releasedInverseHost, targets []dnsengineartifact.UnitSnapshot) dnsengineartifact.SwitchJournalV1 {
	t.Helper()
	base := readSwitchJournalFixture(t, "alpha81-bind.json")
	base.Phase = dnsengineartifact.SwitchPhaseIntent
	if targets != nil {
		base.TargetUnitsBefore = targets
	}
	local := []byte("// local before\n")
	base.ConfigBefore = []dnsengineartifact.FileSnapshot{
		releasedConfigSnapshot("/etc/bind/named.conf.local", 42, local),
		releasedConfigSnapshot("/etc/bind/named.conf.options", 42, []byte("// options before\n")),
	}
	managed, err := bindconfig.ManagedZoneInclude(string(local), "/var/cache/bind/celikpanel/current/zones.conf")
	if err != nil {
		t.Fatal(err)
	}
	after := []dnsengineartifact.FileSnapshot{
		releasedConfigSnapshot("/etc/bind/named.conf.local", 42, []byte(managed)),
		releasedConfigSnapshot("/etc/bind/named.conf.options", 42, []byte("// managed after\n")),
	}
	base.SourceEngine, base.SourceEpoch, base.TargetEpoch = transport.DNSEnginePowerDNS, 1, 2
	manifest, err := mutationpayload.CanonicalDNSEngineSwitchManifestWithPairIdentity(base.Mode, base.SourceEngine, base.TargetEngine, base.SourceEpoch, base.TargetEpoch, base.SourceRevision, base.Topology, "", "", "", "", "", base.Zones)
	if err != nil {
		t.Fatal(err)
	}
	base.ManifestQualifier, base.SnapshotBytes, base.Zones = manifest.Qualifier, manifest.SnapshotBytes, manifest.Zones
	source, err := dnsengineartifact.CanonicalV1(dnsengineartifact.StateV1{
		Schema: dnsengineartifact.StateSchemaV1, Mode: transport.DNSEngineSwitchModeSwitch, Engine: transport.DNSEnginePowerDNS,
		EngineEpoch: 1, ManifestQualifier: manifest.Qualifier,
		MutationRequestID: strings.Repeat("d", 32), MutationOwnerID: strings.Repeat("e", 32),
	})
	if err != nil {
		t.Fatal(err)
	}
	base.StateBefore = dnsengineartifact.FileSnapshot{Path: h.policy.StatePath, Exists: true, Mode: 0o600, OwnerKnown: true, UID: h.owner.UID, GID: h.owner.GID, Data: source, SHA256: dnsengineartifact.DigestBytes(source)}
	base.SourceUnitsBefore = []dnsengineartifact.UnitSnapshot{{Name: "pdns.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"}}
	main := []byte("include \"/etc/bind/named.conf.options\";\ninclude \"/etc/bind/named.conf.local\";\ninclude \"/etc/bind/named.conf.default-zones\";\n")
	defaults := []byte("zone \"localhost\" { type master; file \"/etc/bind/db.local\"; };\n")
	proof := dnsengineartifact.BINDSwitchSourceProofV2{
		BINDUnchangedConfig: []dnsengineartifact.FileSnapshot{
			releasedConfigSnapshot("/etc/bind/named.conf", 42, main),
			releasedConfigSnapshot("/etc/bind/named.conf.default-zones", 42, defaults),
		},
		SourcePDNS: &dnsengineartifact.PDNSSourceProofV2{
			Kind: dnsengineartifact.PDNSSourceProofKindV1,
			ConfigBefore: []dnsengineartifact.FileSnapshot{
				{Path: h.policy.PDNSMainPath, Exists: true, Mode: 0o640, OwnerKnown: true, GID: 42, Data: []byte("main"), SHA256: dnsengineartifact.DigestBytes([]byte("main"))},
				{Path: h.policy.PDNSClusterPath},
				{Path: h.policy.PDNSManagedPath, Exists: true, Mode: 0o644, OwnerKnown: true, Data: []byte("managed"), SHA256: dnsengineartifact.DigestBytes([]byte("managed"))},
			},
			Database: dnsengineartifact.PDNSSourceDatabaseProofV1{Path: h.policy.PDNSDatabasePath, LogicalSHA256: dnsengineartifact.DigestBytes([]byte("logical")), Mode: 0o640, UID: 100, GID: 100, Device: 1, Inode: 2},
		},
	}
	j, err := h.policy.BuildBINDSwitchInverseJournalV2(base, "apt", after, proof)
	if err != nil {
		t.Fatal(err)
	}
	j.Phase = dnsengineartifact.SwitchPhaseRollingBack
	if dnsenginerecovery.InactiveBINDSwitchInverseJournal(j) != nil {
		t.Fatal("fixture is not a recover-dns-bind-switch journal")
	}
	h.write(t, "dns-engine-state.json", h.targetState(t, j))
	h.write(t, "dns-engine-ownership-pdns.json", source)
	return j
}

// releasedBINDAdoptionJournal is a running owner BIND adoption whose no-stop
// inverse the Agent refuses; the BIND adoption target receipt is exact.
func releasedBINDAdoptionJournal(t *testing.T, h releasedInverseHost) dnsengineartifact.SwitchJournalV1 {
	t.Helper()
	base := readSwitchJournalFixture(t, "alpha81-bind.json")
	base.Phase = dnsengineartifact.SwitchPhaseIntent
	base.StateBefore = dnsengineartifact.FileSnapshot{Path: h.policy.StatePath}
	base.TargetUnitsBefore = []dnsengineartifact.UnitSnapshot{
		{Name: "bind9.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"},
		{Name: "named.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"},
	}
	local := []byte("zone \"owner.example\" { type master; file \"/etc/bind/db.owner\"; };\n")
	options := []byte("options { directory \"/var/cache/bind\"; dnssec-validation auto; };\n")
	base.ConfigBefore = []dnsengineartifact.FileSnapshot{
		releasedConfigSnapshot("/etc/bind/named.conf.local", 42, local),
		releasedConfigSnapshot("/etc/bind/named.conf.options", 42, options),
	}
	main := []byte("include \"/etc/bind/named.conf.options\";\ninclude \"/etc/bind/named.conf.local\";\ninclude \"/etc/bind/named.conf.root-hints\";\n")
	hints := []byte("zone \".\" { type hint; file \"/usr/share/dns/root.hints\"; };\n")
	managed, err := bindconfig.ManagedZoneInclude(string(local), "/var/cache/bind/celikpanel/current/zones.conf")
	if err != nil {
		t.Fatal(err)
	}
	after := []dnsengineartifact.FileSnapshot{releasedConfigSnapshot("/etc/bind/named.conf.local", 42, []byte(managed)), base.ConfigBefore[1]}
	proof := dnsengineartifact.BINDSwitchSourceProofV2{
		BINDUnchangedConfig: []dnsengineartifact.FileSnapshot{
			releasedConfigSnapshot("/etc/bind/named.conf", 42, main),
			releasedConfigSnapshot("/etc/bind/named.conf.root-hints", 42, hints),
		},
		SourceBIND: &dnsengineartifact.BINDAdoptionSourceProofV1{
			Kind: dnsengineartifact.BINDAdoptionSourceProofKindV1,
			Files: []dnsengineartifact.BINDAdoptionSourceFileV1{
				{Path: "/etc/bind/db.owner", SHA256: dnsengineartifact.DigestBytes([]byte("owner")), Size: 5, Mode: 0o644, GID: 42, Device: 1, Inode: 2},
				{Path: "/usr/share/dns/root.hints", SHA256: dnsengineartifact.DigestBytes([]byte("hints")), Size: 5, Mode: 0o644, Device: 1, Inode: 3},
			},
			Zones: []dnsengineartifact.BINDAdoptionSourceZoneV1{
				{Name: "owner.example", Class: "IN", Type: "master", File: "/etc/bind/db.owner", SOASerial: 7},
				{Name: ".", Class: "IN", Type: "hint", File: "/usr/share/dns/root.hints"},
			},
		},
	}
	j, err := h.policy.BuildBINDSwitchInverseJournalV2(base, "apt", after, proof)
	if err != nil {
		t.Fatal(err)
	}
	j.Phase = dnsengineartifact.SwitchPhaseRollingBack
	if kind, err := dnsenginerecovery.PlanNativeInverse(j); err != nil || kind != dnsenginerecovery.NativeInverseBINDRunningAdoption {
		t.Fatalf("fixture is not a running BIND adoption: %q %v", kind, err)
	}
	h.write(t, "dns-engine-state.json", h.targetState(t, j))
	return j
}

// releasedBINDNative stands in for the installed BIND adapters: the frozen
// native preimage needs restoring until restore runs, and restore publishes
// the exact source receipt through the command's own receipt primitive.
type releasedBINDNative struct {
	restored    bool
	ownerEdited bool
	restores    int
}

func (n *releasedBINDNative) adapter(restoreReceipt func(dnsengineartifact.SwitchJournalV1) error) bindInverseNative {
	return bindInverseNative{
		assess: func(context.Context, dnsengineartifact.SwitchJournalV1) (dnsenginerecovery.BINDSwitchNativeState, error) {
			if n.ownerEdited {
				return dnsenginerecovery.BINDSwitchNativeUnknown, errors.New("owner changed the native BIND configuration")
			}
			if n.restored {
				return dnsenginerecovery.BINDSwitchNativeRestored, nil
			}
			return dnsenginerecovery.BINDSwitchNativeNeedsRestore, nil
		},
		restore: func(_ context.Context, j dnsengineartifact.SwitchJournalV1) error {
			n.restores++
			if err := restoreReceipt(j); err != nil {
				return err
			}
			n.restored = true
			return nil
		},
	}
}

type releasedOwnerInverseCase struct {
	name    string
	journal func(*testing.T, releasedInverseHost) dnsengineartifact.SwitchJournalV1
	// run executes the command's own inverse wiring once from the private root.
	run func(context.Context, releasedInverseHost, string, *releasedBINDNative) error
	// restored reports whether the source state matches the frozen preimage.
	restored func(*testing.T, releasedInverseHost, dnsengineartifact.SwitchJournalV1)
}

func noLocks(context.Context) error { return nil }

func releasedOwnerInverseCases() []releasedOwnerInverseCase {
	stateAbsent := func(t *testing.T, h releasedInverseHost, _ dnsengineartifact.SwitchJournalV1) {
		t.Helper()
		if _, err := os.Lstat(h.policy.StatePath); !os.IsNotExist(err) {
			t.Fatalf("adoption target receipt was not removed: %v", err)
		}
	}
	return []releasedOwnerInverseCase{
		{
			name:    ownerBINDSwitchInverseCommand,
			journal: releasedBINDSwitchJournal,
			run: func(ctx context.Context, h releasedInverseHost, request string, native *releasedBINDNative) error {
				return dnsenginerecovery.CompleteInactiveBINDSwitchInverse(ctx, bindInverseOps(h.root, h.owner, h.policy, request, false, noLocks,
					native.adapter(func(j dnsengineartifact.SwitchJournalV1) error {
						return dnsenginerecovery.RestoreExactBINDSwitchSourceReceipt(h.policy, h.owner, j)
					})))
			},
			restored: func(t *testing.T, h releasedInverseHost, j dnsengineartifact.SwitchJournalV1) {
				t.Helper()
				raw, err := os.ReadFile(h.policy.StatePath)
				if err != nil || !bytes.Equal(raw, j.StateBefore.Data) {
					t.Fatalf("frozen PowerDNS source receipt was not restored: %v", err)
				}
			},
		},
		{
			name:    ownerBINDAdoptionInverseCommand,
			journal: releasedBINDAdoptionJournal,
			run: func(ctx context.Context, h releasedInverseHost, request string, native *releasedBINDNative) error {
				return dnsenginerecovery.CompleteRunningBINDAdoptionInverse(ctx, bindInverseOps(h.root, h.owner, h.policy, request, true, noLocks,
					native.adapter(func(j dnsengineartifact.SwitchJournalV1) error {
						return dnsenginerecovery.RemoveExactBINDAdoptionTargetReceipt(h.policy, h.owner, j)
					})))
			},
			restored: stateAbsent,
		},
		{
			name:    ownerPDNSAdoptionInverseCommand,
			journal: releasedPDNSAdoptionJournal,
			run: func(ctx context.Context, h releasedInverseHost, request string, native *releasedBINDNative) error {
				return dnsenginerecovery.CompletePDNSAdoptionInverse(ctx, pdnsAdoptionInverseOps(h.root, h.owner, h.policy, request,
					func() error { return nil },
					func(context.Context, dnsengineartifact.SwitchJournalV1) (pdnsAdoptionNativeProof, error) {
						if native.ownerEdited {
							return pdnsAdoptionNativeProof{}, errors.New("owner changed the PowerDNS configuration")
						}
						return pdnsAdoptionNativeProof{ActiveSOA: 1}, nil
					}, nil))
			},
			restored: stateAbsent,
		},
	}
}

func TestOwnerInverseCompletesAgentReleasedRollbackAndRerunIsIdempotent(t *testing.T) {
	for _, tc := range releasedOwnerInverseCases() {
		t.Run(tc.name, func(t *testing.T) {
			h := newReleasedInverseHost(t)
			j := tc.journal(t, h)
			h.stage(t, j, dnsengineartifact.ReleasedNativeUnknownCode)
			ledgerBefore := h.files(t)["service-mutations.json"]
			native := &releasedBINDNative{}
			if err := tc.run(context.Background(), h, j.MutationRequestID, native); err != nil {
				t.Fatalf("owner command refused the Agent's deliberate release: %v", err)
			}
			after := h.files(t)
			if _, present := after["dns-engine-switch-journal.json"]; present {
				t.Fatal("completed rollback did not retire its journal")
			}
			tc.restored(t, h, j)
			// The Agent's release is already this job's terminal verdict. The
			// command writes no ledger entry over it.
			if !bytes.Equal(after["service-mutations.json"], ledgerBefore) {
				t.Fatal("owner command rewrote the Agent's terminal ledger verdict")
			}
			ledger, err := servicemutationledger.Decode(after["service-mutations.json"])
			if err != nil {
				t.Fatal(err)
			}
			id := dnsengineartifact.SwitchIdentity{RequestID: j.MutationRequestID, OwnerID: j.MutationOwnerID, Target: j.TargetEngine, Qualifier: j.ManifestQualifier}
			if !id.TerminalRolledBackJob(ledger) || ledger.ActiveRequestID != "" {
				t.Fatalf("terminal job lost after owner recovery: %+v", ledger)
			}
			if tc.name != ownerPDNSAdoptionInverseCommand && native.restores != 1 {
				t.Fatalf("native restore ran %d times", native.restores)
			}

			// Re-running the completed request changes nothing and does not
			// claim a verdict it cannot re-prove from a retired journal.
			restores := native.restores
			err = tc.run(context.Background(), h, j.MutationRequestID, native)
			if err == nil {
				t.Fatal("re-run of a retired journal reported a new terminal verdict")
			}
			if errors.Is(err, errBINDInverseTerminalLedgerObserved) || errors.Is(err, errPDNSInverseTerminalLedgerObserved) {
				t.Fatalf("re-run claimed an owner-recovery ledger verdict the Agent's release does not record: %v", err)
			}
			if !errors.Is(err, errDNSInverseReleasedReconciled) || !strings.Contains(err.Error(), j.MutationRequestID) {
				t.Fatalf("re-run did not report the already-reconciled release: %v", err)
			}
			if native.restores != restores || !reflect.DeepEqual(h.files(t), after) {
				t.Fatalf("re-run of a completed request mutated the host: %v", err)
			}
		})
	}
}

func TestOwnerInverseFromAgentReleasePreservesOwnerEditAndOtherReasons(t *testing.T) {
	for _, tc := range releasedOwnerInverseCases() {
		t.Run(tc.name+"/owner-edit", func(t *testing.T) {
			h := newReleasedInverseHost(t)
			j := tc.journal(t, h)
			h.stage(t, j, dnsengineartifact.ReleasedNativeUnknownCode)
			before := h.files(t)
			native := &releasedBINDNative{ownerEdited: true}
			if err := tc.run(context.Background(), h, j.MutationRequestID, native); err == nil {
				t.Fatal("owner-modified native DNS was accepted")
			}
			if native.restores != 0 || !reflect.DeepEqual(h.files(t), before) {
				t.Fatal("owner edit reached a durable or native effect")
			}
		})
		for _, code := range []string{dnsengineartifact.ReleasedUnsupportedHostCode, dnsengineartifact.ReleasedHostWindowCode} {
			t.Run(tc.name+"/"+code, func(t *testing.T) {
				h := newReleasedInverseHost(t)
				j := tc.journal(t, h)
				h.stage(t, j, code)
				before := h.files(t)
				native := &releasedBINDNative{}
				if err := tc.run(context.Background(), h, j.MutationRequestID, native); err == nil {
					t.Fatal("release with another reason was admitted")
				}
				if native.restores != 0 || !reflect.DeepEqual(h.files(t), before) {
					t.Fatal("refused release reached a durable or native effect")
				}
			})
		}
		t.Run(tc.name+"/other-request", func(t *testing.T) {
			h := newReleasedInverseHost(t)
			j := tc.journal(t, h)
			h.stage(t, j, dnsengineartifact.ReleasedNativeUnknownCode)
			before := h.files(t)
			native := &releasedBINDNative{}
			if err := tc.run(context.Background(), h, strings.Repeat("d", 32), native); err == nil {
				t.Fatal("another request's command was admitted")
			}
			if native.restores != 0 || !reflect.DeepEqual(h.files(t), before) {
				t.Fatal("foreign request reached a durable or native effect")
			}
		})
	}
}

func TestReleasedDNSInverseWorkerExclusionAdmitsOnlyDeliberateRelease(t *testing.T) {
	evidence := adoptionWorkerEvidence()
	evidence.Observation.Status = dnsenginerecovery.EvidenceReleasedUndecided
	evidence.Observation.Phase = evidence.Journal.Phase
	evidence.Observation.ReleaseReason = dnsengineartifact.ReleasedNativeUnknownCode
	job := &evidence.AcceptedJob
	job.Status, job.Phase = servicemutationledger.StatusFailed, "interrupted"
	job.ErrorCode = dnsengineartifact.ReleasedNativeUnknownCode
	job.ErrorMessage = "The interrupted DNS switch could not be verified after the Agent restarted."
	job.LeaseExpiresAt = time.Time{}
	job.FinishedAt = job.UpdatedAt
	if err := excludeInstalledReleasedDNSInverseWorker(context.Background(), evidence); err != nil {
		t.Fatalf("deliberate release refused: %v", err)
	}
	// Commands outside this rule keep refusing every released job.
	if err := excludeInstalledDNSInverseWorker(context.Background(), evidence); err == nil {
		t.Fatal("shared worker exclusion admitted a released job")
	}
	other := evidence
	other.Observation.ReleaseReason = dnsengineartifact.ReleasedHostWindowCode
	other.AcceptedJob.ErrorCode = dnsengineartifact.ReleasedHostWindowCode
	if err := excludeInstalledReleasedDNSInverseWorker(context.Background(), other); err == nil {
		t.Fatal("host-window release admitted")
	}
	worker := evidence
	worker.AcceptedJob.WorkerPID, worker.AcceptedJob.WorkerStarted, worker.AcceptedJob.WorkerCommand = 1, "1", "agent"
	if err := excludeInstalledReleasedDNSInverseWorker(context.Background(), worker); err == nil {
		t.Fatal("released job recording a worker admitted")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := excludeInstalledReleasedDNSInverseWorker(cancelled, evidence); err == nil {
		t.Fatal("cancelled exclusion admitted")
	}
}

// A journal-free ledger that holds a release for another reason is not the
// reconciled deliberate release: the re-run keeps its previous answer.
func TestOwnerInverseRerunReportsReconciledOnlyForDeliberateRelease(t *testing.T) {
	for _, tc := range releasedOwnerInverseCases() {
		t.Run(tc.name, func(t *testing.T) {
			h := newReleasedInverseHost(t)
			j := tc.journal(t, h)
			h.stage(t, j, dnsengineartifact.ReleasedHostWindowCode)
			if err := os.Remove(filepath.Join(h.root, "dns-engine-switch-journal.json")); err != nil {
				t.Fatal(err)
			}
			before := h.files(t)
			err := tc.run(context.Background(), h, j.MutationRequestID, &releasedBINDNative{})
			if err == nil || errors.Is(err, errDNSInverseReleasedReconciled) {
				t.Fatalf("host-window release was reported as reconciled: %v", err)
			}
			if !reflect.DeepEqual(h.files(t), before) {
				t.Fatal("journal-free re-run mutated the host")
			}
		})
	}
}

type ownerInverseDispatcher struct {
	name     string
	dispatch func([]string, int, func(context.Context, string) error, io.Writer, io.Writer) int
}

func ownerInverseDispatchers() []ownerInverseDispatcher {
	return []ownerInverseDispatcher{
		{ownerBINDSwitchInverseCommand, dispatchOwnerBINDSwitchInverse},
		{ownerBINDAdoptionInverseCommand, dispatchOwnerBINDAdoptionInverse},
		{ownerPDNSAdoptionInverseCommand, dispatchOwnerPDNSAdoptionInverse},
	}
}

// A request that is already complete exits 0 with its explanation on stdout;
// the text still says the command does not check current DNS health.
func TestOwnerInverseDispatchersReportReconciledReleaseInBothLanguages(t *testing.T) {
	id := strings.Repeat("a", 32)
	reconciled := func(context.Context, string) error { return releasedDNSInverseReconciledOutcome(id) }
	for _, tc := range ownerInverseDispatchers() {
		for _, lang := range []string{"en", "tr"} {
			var out, diagnostic bytes.Buffer
			code := tc.dispatch([]string{tc.name, "--request-id", id, "--lang", lang}, 0, reconciled, &out, &diagnostic)
			text := out.String()
			if code != exitOK || diagnostic.Len() != 0 || !strings.Contains(text, id) {
				t.Fatalf("%s %s: code=%d out=%q diagnostic=%q", tc.name, lang, code, text, diagnostic.String())
			}
			want, health, unwanted := "already reconciled", "does not check current DNS health", "retry this same request"
			if lang == "tr" {
				want, health, unwanted = "zaten sonuçlanmış", "şu anki sağlığını kontrol etmez", "yeniden deneyin"
			}
			if !strings.Contains(text, want) || !strings.Contains(text, health) || strings.Contains(text, unwanted) {
				t.Fatalf("%s %s: reconciled release text is wrong: %q", tc.name, lang, text)
			}
		}
	}
}

func TestOwnerInverseDispatchersReportEarlierOwnerVerdictAsComplete(t *testing.T) {
	id := strings.Repeat("a", 32)
	for _, tc := range ownerInverseDispatchers() {
		for _, terminal := range []error{errBINDInverseTerminalLedgerObserved, errPDNSInverseTerminalLedgerObserved} {
			for _, lang := range []string{"en", "tr"} {
				var out, diagnostic bytes.Buffer
				code := tc.dispatch([]string{tc.name, "--request-id", id, "--lang", lang}, 0, func(context.Context, string) error {
					return fmt.Errorf("%w: request %s", terminal, id)
				}, &out, &diagnostic)
				text := out.String()
				if code != exitOK || diagnostic.Len() != 0 || !strings.Contains(text, id) {
					t.Fatalf("%s %s: code=%d out=%q diagnostic=%q", tc.name, lang, code, text, diagnostic.String())
				}
				want, health, unwanted := "already complete", "does not check current DNS health", "retry this same request"
				if lang == "tr" {
					want, health, unwanted = "zaten tamamlanmış", "şu anki sağlığını kontrol etmez", "yeniden deneyin"
				}
				if !strings.Contains(text, want) || !strings.Contains(text, health) || strings.Contains(text, unwanted) ||
					strings.Contains(text, "terminal verdict for request") {
					t.Fatalf("%s %s: earlier verdict text is wrong: %q", tc.name, lang, text)
				}
			}
		}
	}
}

// Refusals and unknown results keep exit 3 with the retry guidance.
func TestOwnerInverseDispatchersKeepUnavailableForRefusalAndUnknown(t *testing.T) {
	id := strings.Repeat("a", 32)
	for _, tc := range ownerInverseDispatchers() {
		for _, refusal := range []error{
			errors.New("another DNS operation owns retained journal"),
			fmt.Errorf("journal-absent DNS result is unknown: %w", errors.New("ledger changed")),
			context.DeadlineExceeded,
		} {
			var out, diagnostic bytes.Buffer
			code := tc.dispatch([]string{tc.name, "--request-id", id}, 0, func(context.Context, string) error { return refusal }, &out, &diagnostic)
			if code != exitUnavailable || out.Len() != 0 || !strings.Contains(diagnostic.String(), "same request") ||
				strings.Contains(diagnostic.String(), "already") {
				t.Fatalf("%s: refusal %v: code=%d out=%q diagnostic=%q", tc.name, refusal, code, out.String(), diagnostic.String())
			}
		}
	}
}

// After an owner run that recorded its own terminal verdict and retired the
// journal (no Agent release), each command's re-run reports the earlier
// verdict and changes nothing.
func TestOwnerInverseRerunRecognisesEarlierOwnerVerdict(t *testing.T) {
	for _, tc := range releasedOwnerInverseCases() {
		t.Run(tc.name, func(t *testing.T) {
			h := newReleasedInverseHost(t)
			j := tc.journal(t, h)
			h.stage(t, j, dnsengineartifact.ReleasedNativeUnknownCode)
			ledger, err := servicemutationledger.Decode(h.files(t)["service-mutations.json"])
			if err != nil {
				t.Fatal(err)
			}
			job := ledger.Jobs[j.MutationRequestID]
			job.ErrorCode = "dns_engine_switch_rolled_back_by_owner_recovery"
			job.ErrorMessage = "The interrupted DNS engine switch was rolled back to the verified previous state."
			raw, err := servicemutationledger.Encode(&ledger)
			if err != nil {
				t.Fatal(err)
			}
			h.write(t, "service-mutations.json", raw)
			if err := os.Remove(filepath.Join(h.root, "dns-engine-switch-journal.json")); err != nil {
				t.Fatal(err)
			}
			before := h.files(t)
			native := &releasedBINDNative{}
			err = tc.run(context.Background(), h, j.MutationRequestID, native)
			if !errors.Is(err, errBINDInverseTerminalLedgerObserved) && !errors.Is(err, errPDNSInverseTerminalLedgerObserved) {
				t.Fatalf("earlier owner verdict was not recognised: %v", err)
			}
			if native.restores != 0 || !reflect.DeepEqual(h.files(t), before) {
				t.Fatal("re-run after an owner verdict mutated the host")
			}
		})
	}
}

func TestDNSSwitchStatusReportsReconciledReleaseOnlyWithoutJournal(t *testing.T) {
	h := newReleasedInverseHost(t)
	j := releasedBINDSwitchJournal(t, h)
	h.stage(t, j, dnsengineartifact.ReleasedNativeUnknownCode)
	request := j.MutationRequestID
	want := releasedDNSSwitchReconciledStatus(request)
	render := func(fn func(context.Context, string, servicemutationledger.FileOwner, string, io.Writer, io.Writer) int, requestID string) (int, string, string) {
		t.Helper()
		var out, diagnostic bytes.Buffer
		code := fn(context.Background(), h.root, h.owner, requestID, &out, &diagnostic)
		return code, out.String(), diagnostic.String()
	}

	// Journal still retained: the journal-free paths refuse, and the
	// retained-journal status text still says the journal blocks new switches.
	for name, fn := range map[string]func(context.Context, string, servicemutationledger.FileOwner, string, io.Writer, io.Writer) int{
		"recorded": renderRecordedDNSSwitchStatus, "journal-free": renderJournalFreeDNSSwitchStatus,
	} {
		if code, out, _ := render(fn, request); code == exitOK || strings.Contains(out, "no longer blocks") {
			t.Fatalf("%s: retained journal reported as reconciled: %d %q", name, code, out)
		}
	}
	evidence, present, err := dnsenginerecovery.ReadSwitchEvidence(h.root, h.owner, h.policy, time.Now().UTC())
	if err != nil || !present {
		t.Fatal(err)
	}
	if text, known := releasedDNSSwitchGuidance(evidence); !known || !strings.Contains(text, "blocks a new DNS switch") || strings.Contains(text, "no longer blocks") {
		t.Fatalf("retained journal lost its blocking text: %q", text)
	}

	if err := os.Remove(filepath.Join(h.root, "dns-engine-switch-journal.json")); err != nil {
		t.Fatal(err)
	}
	for name, run := range map[string]func() (int, string, string){
		"recorded":                 func() (int, string, string) { return render(renderRecordedDNSSwitchStatus, request) },
		"quiesced with request":    func() (int, string, string) { return render(renderJournalFreeDNSSwitchStatus, request) },
		"quiesced without request": func() (int, string, string) { return render(renderJournalFreeDNSSwitchStatus, "") },
	} {
		code, out, diagnostic := run()
		if code != exitOK || !strings.Contains(out, want) || diagnostic != "" ||
			!strings.Contains(out, "request "+request) || strings.Contains(out, "records status") {
			t.Fatalf("%s: code=%d out=%q diagnostic=%q", name, code, out, diagnostic)
		}
	}

	// Another release reason beside a retired journal keeps the generic,
	// non-inferring text.
	other := newReleasedInverseHost(t)
	oj := releasedBINDSwitchJournal(t, other)
	other.stage(t, oj, dnsengineartifact.ReleasedHostWindowCode)
	if err := os.Remove(filepath.Join(other.root, "dns-engine-switch-journal.json")); err != nil {
		t.Fatal(err)
	}
	var out, diagnostic bytes.Buffer
	if code := renderJournalFreeDNSSwitchStatus(context.Background(), other.root, other.owner, oj.MutationRequestID, &out, &diagnostic); code != exitOK ||
		strings.Contains(out.String(), "no longer blocks") || !strings.Contains(out.String(), "records status failed") {
		t.Fatalf("host-window release was reported as reconciled: %d %q %q", code, out.String(), diagnostic.String())
	}
}
