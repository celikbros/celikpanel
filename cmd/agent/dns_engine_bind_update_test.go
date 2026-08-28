package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/hostplatform"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

func TestSignedUpdateBINDPackageStatusIsExactAndDeadlineBound(t *testing.T) {
	wantArgs := []string{"-W", "-f", "${Status}", "--", "bind9"}
	installed, err := exactBINDPackageInstalledForSignedUpdateWithRunner(
		context.Background(), "/usr/bin/dpkg-query", "bind9",
		func(_ context.Context, name string, args ...string) ([]byte, error) {
			if name != "/usr/bin/dpkg-query" || !reflect.DeepEqual(args, wantArgs) {
				t.Fatalf("command = %s %#v", name, args)
			}
			return []byte("install ok installed"), nil
		},
	)
	if err != nil || !installed {
		t.Fatalf("installed=%v err=%v", installed, err)
	}
	for _, output := range []string{
		"install ok installed\n", "deinstall ok config-files",
		"prefix install ok installed",
	} {
		if _, err := exactBINDPackageInstalledForSignedUpdateWithRunner(
			context.Background(), "/usr/bin/dpkg-query", "bind9",
			func(context.Context, string, ...string) ([]byte, error) {
				return []byte(output), nil
			},
		); err == nil {
			t.Fatalf("non-canonical package status accepted: %q", output)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err = exactBINDPackageInstalledForSignedUpdateWithRunner(
		ctx, "/usr/bin/dpkg-query", "bind9",
		func(commandCtx context.Context, _ string, _ ...string) ([]byte, error) {
			<-commandCtx.Done()
			return nil, commandCtx.Err()
		},
	)
	if !errors.Is(err, context.DeadlineExceeded) ||
		time.Since(started) > time.Second {
		t.Fatalf("package proof ignored caller deadline: %v", err)
	}
}

func signedUpdateBINDInstallReceipt(t *testing.T) dnsEngineInstallOwnershipReceipt {
	t.Helper()
	manifest, err := mutationpayload.CanonicalDNSEngineSwitchManifest(
		transport.DNSEngineSwitchModeSwitch,
		transport.DNSEnginePowerDNS, transport.DNSEngineBIND,
		1, 2, 0, transport.DNSTopologyStandalone, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := newDNSEngineInstallOwnership(
		transport.DNSEngineBIND, hostplatform.PackageManagerAPT,
		[]string{"bind9"}, []string{"bind9"}, manifest,
		transport.ServiceMutationBinding{
			MutationRequestID: strings.Repeat("1", 32),
			MutationOwnerID:   strings.Repeat("2", 32),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return receipt
}

func signedUpdateBINDPreparationOps(
	t *testing.T,
) (bindSignedUpdatePreparationOps, *[]string) {
	t.Helper()
	events := []string{}
	receipt := signedUpdateBINDInstallReceipt(t)
	ops := bindSignedUpdatePreparationOps{
		checkIdle: func() error {
			events = append(events, "idle")
			return nil
		},
		detectProfile: func() (hostplatform.Profile, error) {
			events = append(events, "profile")
			return testUbuntuBINDProfile(), nil
		},
		readJournal: func() (dnsEngineSwitchJournal, bool, error) {
			events = append(events, "journal")
			return dnsEngineSwitchJournal{}, false, nil
		},
		readLedger: func() (serviceMutationLedger, error) {
			events = append(events, "ledger")
			return serviceMutationLedger{
				Version: serviceMutationLedgerVersion,
				Jobs:    map[string]*ServiceMutationJob{},
			}, nil
		},
		rollbackJournal: func(context.Context, dnsEngineSwitchJournal) error {
			events = append(events, "rollback")
			return nil
		},
		verifyRestored: func(context.Context, dnsEngineSwitchJournal) error {
			events = append(events, "restored")
			return nil
		},
		verifyTarget: func(context.Context, dnsEngineSwitchJournal) error {
			events = append(events, "target")
			return nil
		},
		writeJournal: func(dnsEngineSwitchJournal) error {
			events = append(events, "write")
			return nil
		},
		removeJournal: func() error {
			events = append(events, "remove")
			return nil
		},
		readInstall: func() (dnsEngineInstallOwnershipReceipt, bool, error) {
			events = append(events, "install")
			return receipt, true, nil
		},
		readState: func() (dnsEngineStateReceipt, bool, error) {
			events = append(events, "state")
			return dnsEngineStateReceipt{}, false, nil
		},
		readOwnership: func() (dnsEngineStateReceipt, bool, error) {
			events = append(events, "ownership")
			return dnsEngineStateReceipt{}, false, nil
		},
		writeOwnership: func(dnsEngineStateReceipt) error {
			events = append(events, "write-ownership")
			return nil
		},
		finalizeArtifacts: func(dnsEngineSwitchJournal) error {
			events = append(events, `finalize-artifacts`)
			return nil
		},
		retireInstall: func(dnsEngineSwitchJournal) error {
			events = append(events, "retire-install")
			return nil
		},
		packageInstalled: func(context.Context, hostplatform.Profile, string) (bool, error) {
			events = append(events, "package")
			return true, nil
		},
		parentExists: func() (bool, error) {
			events = append(events, "parent")
			return true, nil
		},
		prepare: func(context.Context) error {
			events = append(events, "prepare")
			return nil
		},
		hardenExisting: func(context.Context) error {
			events = append(events, "harden-existing")
			return nil
		},
		verifyExisting: func(_ context.Context, _ dnsEngineStateReceipt) error {
			events = append(events, "verify-existing")
			return nil
		},
	}
	return ops, &events
}

func signedUpdateBINDRecoveryFixture(
	t *testing.T,
) (dnsEngineSwitchJournal, serviceMutationLedger) {
	t.Helper()
	journal := testBINDSwitchJournal(t)
	manifest, err := mutationpayload.CanonicalDNSEngineSwitchManifestWithPairIdentity(
		transport.DNSEngineSwitchModeSwitch,
		"", transport.DNSEngineBIND, 0, 1, 1,
		transport.DNSTopologyPaired,
		transport.DNSPairRolePrimary,
		"72.62.38.15", "ns1.celikhost.com",
		"2.25.80.4", "ns2.celikhost.com", nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	journal.Phase = dnsSwitchPhaseRollingBack
	journal.ManifestQualifier = manifest.Qualifier
	journal.SourceRevision = manifest.SourceRevision
	journal.Topology = manifest.Topology
	journal.PairRole = manifest.PairRole
	journal.LocalIP = manifest.LocalIP
	journal.LocalNS = manifest.LocalNS
	journal.PeerIP = manifest.PeerIP
	journal.PeerNS = manifest.PeerNS
	journal.PrimaryCatalogSerial = 1
	journal.SnapshotBytes = manifest.SnapshotBytes
	journal.Zones = manifest.Zones
	request := signedUpdateRollbackEvidenceRequest(journal, manifest)
	now := time.Now().UTC()
	failedJob := &ServiceMutationJob{
		RequestID:    request.MutationRequestID,
		OwnerID:      request.MutationOwnerID,
		Kind:         "dns_engine_switch",
		Target:       string(manifest.TargetEngine),
		PackageName:  manifest.Qualifier,
		Status:       serviceMutationStatusFailed,
		Phase:        "failed",
		ErrorCode:    "service_operation_failed",
		ErrorMessage: "bounded failure",
		Attempt:      1,
		StartedAt:    now.Add(-2 * time.Minute),
		UpdatedAt:    now,
		DeadlineAt:   now.Add(time.Hour),
		FinishedAt:   now,
	}
	unrelatedID := strings.Repeat("c", 32)
	unrelated := &ServiceMutationJob{
		RequestID:    unrelatedID,
		OwnerID:      strings.Repeat("d", 32),
		Kind:         "mail_stack",
		Target:       "postfix",
		Status:       serviceMutationStatusFailed,
		Phase:        "failed",
		ErrorCode:    "service_operation_failed",
		ErrorMessage: "unrelated terminal history",
		Attempt:      1,
		StartedAt:    now.Add(-4 * time.Minute),
		UpdatedAt:    now.Add(-3 * time.Minute),
		DeadlineAt:   now.Add(time.Hour),
		FinishedAt:   now.Add(-3 * time.Minute),
	}
	ledger := serviceMutationLedger{
		Version: serviceMutationLedgerVersion,
		Jobs: map[string]*ServiceMutationJob{
			failedJob.RequestID: failedJob,
			unrelatedID:         unrelated,
		},
	}
	if err := validateServiceMutationLedger(&ledger); err != nil {
		t.Fatalf("invalid signed-update recovery fixture: %v", err)
	}
	return journal, ledger
}

func signedUpdateBINDCommittedRecoveryFixture(
	t *testing.T,
) (dnsEngineSwitchJournal, serviceMutationLedger, dnsEngineStateReceipt, dnsEngineInstallOwnershipReceipt) {
	t.Helper()
	journal, ledger := signedUpdateBINDRecoveryFixture(t)
	journal.Phase = dnsSwitchPhaseCommitted
	manifest, err := switchJournalManifest(journal)
	if err != nil {
		t.Fatal(err)
	}
	job := ledger.Jobs[journal.MutationRequestID]
	job.Status = serviceMutationStatusSucceeded
	job.Phase, err = formatDNSEngineSwitchPublishedPhase(
		journal.MutationRequestID, journal.ManifestQualifier,
	)
	if err != nil {
		t.Fatal(err)
	}
	job.ErrorCode = ""
	job.ErrorMessage = ""
	if err := validateServiceMutationLedger(&ledger); err != nil {
		t.Fatalf("invalid committed recovery ledger: %v", err)
	}
	state := dnsEngineStateReceipt{
		Schema: dnsEngineStateSchema, Mode: journal.Mode,
		Engine: journal.TargetEngine, EngineEpoch: journal.TargetEpoch,
		Generation: journal.TargetGeneration,
		PairRole:   journal.PairRole, PairLocalIP: journal.LocalIP,
		PairPeerIP:           journal.PeerIP,
		PrimaryCatalogSerial: journal.PrimaryCatalogSerial,
		SourceRevision:       journal.SourceRevision,
		ManifestQualifier:    journal.ManifestQualifier,
		MutationRequestID:    journal.MutationRequestID,
		MutationOwnerID:      journal.MutationOwnerID,
	}
	if err := validateDNSEngineState(state); err != nil ||
		!exactDNSEngineStateForJournal(state, journal) {
		t.Fatalf("invalid committed recovery state: %+v err=%v", state, err)
	}
	install, err := newDNSEngineInstallOwnership(
		transport.DNSEngineBIND, hostplatform.PackageManagerAPT,
		[]string{"bind9"}, []string{"bind9"}, manifest,
		switchJournalBinding(journal),
	)
	if err != nil {
		t.Fatal(err)
	}
	return journal, ledger, state, install
}

func signedUpdatePDNSBostonCommittedRecoveryFixture(
	t *testing.T,
) (dnsEngineSwitchJournal, serviceMutationLedger, dnsEngineStateReceipt, dnsEngineInstallOwnershipReceipt) {
	t.Helper()
	journal, _ := testPDNSReconfigureRecoveryJournal(t, 109)
	journal.Phase = dnsSwitchPhaseCommitted
	manifest, err := switchJournalManifest(journal)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	phase, err := formatDNSEngineSwitchPublishedPhase(
		journal.MutationRequestID, journal.ManifestQualifier,
	)
	if err != nil {
		t.Fatal(err)
	}
	job := &ServiceMutationJob{
		RequestID:   journal.MutationRequestID,
		OwnerID:     journal.MutationOwnerID,
		Kind:        "dns_engine_switch",
		Target:      string(transport.DNSEnginePowerDNS),
		PackageName: journal.ManifestQualifier,
		Status:      serviceMutationStatusSucceeded,
		Phase:       phase,
		Attempt:     1,
		StartedAt:   now.Add(-2 * time.Minute),
		UpdatedAt:   now,
		DeadlineAt:  now.Add(time.Hour),
		FinishedAt:  now,
	}
	ledger := serviceMutationLedger{
		Version: serviceMutationLedgerVersion,
		Jobs:    map[string]*ServiceMutationJob{job.RequestID: job},
	}
	if err := validateServiceMutationLedger(&ledger); err != nil {
		t.Fatalf("invalid Boston terminal ledger fixture: %v", err)
	}
	state := dnsEngineStateReceipt{
		Schema:               dnsEngineStateSchema,
		Mode:                 journal.Mode,
		Engine:               journal.TargetEngine,
		EngineEpoch:          journal.TargetEpoch,
		Generation:           journal.TargetGeneration,
		PairRole:             journal.PairRole,
		PairLocalIP:          journal.LocalIP,
		PairPeerIP:           journal.PeerIP,
		PrimaryCatalogSerial: journal.PrimaryCatalogSerial,
		SourceRevision:       journal.SourceRevision,
		ManifestQualifier:    journal.ManifestQualifier,
		MutationRequestID:    journal.MutationRequestID,
		MutationOwnerID:      journal.MutationOwnerID,
	}
	if err := validateDNSEngineState(state); err != nil {
		t.Fatalf("invalid Boston PowerDNS state fixture: %v", err)
	}
	if !exactDNSEngineStateForJournal(state, journal) {
		t.Fatal("Boston PowerDNS state does not bind its committed journal")
	}
	profile := testUbuntuBINDProfile()
	packages, err := managedDNSEnginePackagesForProfile(
		profile, transport.DNSEnginePowerDNS,
	)
	if err != nil {
		t.Fatal(err)
	}
	install, err := newDNSEngineInstallOwnership(
		transport.DNSEnginePowerDNS,
		profile.PackageManager,
		packages,
		packages,
		manifest,
		switchJournalBinding(journal),
	)
	if err != nil {
		t.Fatal(err)
	}
	return journal, ledger, state, install
}

func signedUpdatePDNSAdoptCommittedRecoveryFixture(
	t *testing.T,
) (dnsEngineSwitchJournal, serviceMutationLedger, dnsEngineStateReceipt) {
	t.Helper()
	useTestPDNSConfigPaths(t)
	stateRoot := t.TempDir()
	t.Setenv("CELIKPANEL_AGENT_STATE_DIR", stateRoot)
	manifest := testPDNSAdoptionManifest(
		t,
		transport.DNSTopologyPaired,
		"adopt.example.test",
		[]transport.ZoneRecord{{
			Name:    "adopt.example.test",
			Type:    "A",
			Content: "192.0.2.44",
			TTL:     300,
		}},
	)
	configs := []dnsFileSnapshot{
		testPDNSConfigSnapshot(
			dnsMainConf, "main", 0o640, 0, 109,
		),
		testPDNSConfigSnapshot(
			dnsManagedConf, "managed", 0o644, 0, 0,
		),
		testPDNSConfigSnapshot(
			dnsClusterConf, "paired", 0o644, 0, 0,
		),
	}
	sort.Slice(configs, func(left, right int) bool {
		return configs[left].Path < configs[right].Path
	})
	journal := dnsEngineSwitchJournal{
		Schema:            dnsEngineSwitchJournalSchema,
		Phase:             dnsSwitchPhaseCommitted,
		Mode:              manifest.Mode,
		MutationRequestID: strings.Repeat("d", 32),
		MutationOwnerID:   strings.Repeat("e", 32),
		ManifestQualifier: manifest.Qualifier,
		SourceEngine:      manifest.SourceEngine,
		TargetEngine:      manifest.TargetEngine,
		SourceEpoch:       manifest.SourceEpoch,
		TargetEpoch:       manifest.TargetEpoch,
		SourceRevision:    manifest.SourceRevision,
		Topology:          manifest.Topology,
		PeerIP:            manifest.PeerIP,
		PeerNS:            manifest.PeerNS,
		SnapshotBytes:     manifest.SnapshotBytes,
		Zones:             manifest.Zones,
		StateBefore:       dnsFileSnapshot{Path: filepath.Clean(dnsEngineStatePath())},
		ConfigBefore:      configs,
		TargetUnitsBefore: []dnsUnitSnapshot{
			{Name: "bind9.service", LoadState: "loaded", ActiveState: "inactive", UnitFileState: "disabled"},
			{Name: "named.service", LoadState: "loaded", ActiveState: "inactive", UnitFileState: "disabled"},
			{Name: "pdns.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"},
		},
		SourceUnitsBefore: []dnsUnitSnapshot{},
		PDNSLiveSHA256:    strings.Repeat("f", 64),
		PDNSLiveSize:      4096,
	}
	if err := validateDNSEngineSwitchJournal(journal); err != nil {
		t.Fatalf("invalid exact PowerDNS adoption journal: %v", err)
	}
	state := dnsEngineStateReceipt{
		Schema:            dnsEngineStateSchema,
		Mode:              journal.Mode,
		Engine:            journal.TargetEngine,
		EngineEpoch:       journal.TargetEpoch,
		SourceRevision:    journal.SourceRevision,
		ManifestQualifier: journal.ManifestQualifier,
		MutationRequestID: journal.MutationRequestID,
		MutationOwnerID:   journal.MutationOwnerID,
	}
	if err := validateDNSEngineState(state); err != nil {
		t.Fatalf("invalid exact PowerDNS adoption state: %v", err)
	}
	if !exactDNSEngineStateForJournal(state, journal) {
		t.Fatal("PowerDNS adoption state does not bind its committed journal")
	}
	now := time.Now().UTC()
	phase, err := formatDNSEngineSwitchPublishedPhase(
		journal.MutationRequestID, journal.ManifestQualifier,
	)
	if err != nil {
		t.Fatal(err)
	}
	job := &ServiceMutationJob{
		RequestID:   journal.MutationRequestID,
		OwnerID:     journal.MutationOwnerID,
		Kind:        "dns_engine_switch",
		Target:      string(transport.DNSEnginePowerDNS),
		PackageName: journal.ManifestQualifier,
		Status:      serviceMutationStatusSucceeded,
		Phase:       phase,
		Attempt:     1,
		StartedAt:   now.Add(-2 * time.Minute),
		UpdatedAt:   now,
		DeadlineAt:  now.Add(time.Hour),
		FinishedAt:  now,
	}
	ledger := serviceMutationLedger{
		Version: serviceMutationLedgerVersion,
		Jobs:    map[string]*ServiceMutationJob{job.RequestID: job},
	}
	if err := validateServiceMutationLedger(&ledger); err != nil {
		t.Fatalf("invalid PowerDNS adoption terminal ledger: %v", err)
	}
	return journal, ledger, state
}

func cloneSignedUpdateLedger(ledger serviceMutationLedger) serviceMutationLedger {
	clone := serviceMutationLedger{
		Version:         ledger.Version,
		ActiveRequestID: ledger.ActiveRequestID,
		Jobs:            make(map[string]*ServiceMutationJob, len(ledger.Jobs)),
	}
	for requestID, job := range ledger.Jobs {
		if job == nil {
			clone.Jobs[requestID] = nil
			continue
		}
		copied := *job
		clone.Jobs[requestID] = &copied
	}
	return clone
}

func TestSignedUpdateBINDPreparationExactTransitionalOwnership(t *testing.T) {
	ops, events := signedUpdateBINDPreparationOps(t)
	if err := prepareBINDGenerationRootForSignedUpdateWithOps(
		context.Background(), ops,
	); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"idle", "profile", "journal", "install", "state", "ownership",
		"package", "parent", "prepare",
	}
	if !reflect.DeepEqual(*events, want) {
		t.Fatalf("events = %#v, want %#v", *events, want)
	}
}

func TestSignedUpdateBINDPreparationRecoversRollbackPhasesRemoveLast(t *testing.T) {
	for _, phase := range []string{dnsSwitchPhaseRollingBack, dnsSwitchPhaseRolledBack} {
		t.Run(phase, func(t *testing.T) {
			ops, events := signedUpdateBINDPreparationOps(t)
			journal, ledger := signedUpdateBINDRecoveryFixture(t)
			journal.Phase = phase
			journalExists := true
			postTarget := map[string]bindInstallUnitState{}
			if phase == dnsSwitchPhaseRolledBack {
				postTarget = map[string]bindInstallUnitState{
					"bind9.service": {name: "bind9.service", loadState: "not-found", activeState: "inactive"},
					"named.service": {name: "named.service", loadState: "loaded", activeState: "inactive", unitFileState: "disabled"},
				}
			}
			ops.readJournal = func() (dnsEngineSwitchJournal, bool, error) {
				*events = append(*events, "journal")
				return journal, journalExists, nil
			}
			ops.readLedger = func() (serviceMutationLedger, error) {
				*events = append(*events, "ledger")
				return cloneSignedUpdateLedger(ledger), nil
			}
			ops.rollbackJournal = func(_ context.Context, candidate dnsEngineSwitchJournal) error {
				*events = append(*events, "rollback")
				if phase != dnsSwitchPhaseRollingBack || !reflect.DeepEqual(candidate, journal) {
					return errors.New("rollback received another journal")
				}
				postTarget = map[string]bindInstallUnitState{
					"bind9.service": {name: "bind9.service", loadState: "not-found", activeState: "inactive"},
					"named.service": {name: "named.service", loadState: "loaded", activeState: "inactive", unitFileState: "disabled"},
				}
				return nil
			}
			ops.verifyRestored = func(_ context.Context, candidate dnsEngineSwitchJournal) error {
				*events = append(*events, "restored")
				wantPreimage := []dnsUnitSnapshot{
					{Name: "bind9.service", LoadState: "not-found", ActiveState: "inactive"},
					{Name: "named.service", LoadState: "not-found", ActiveState: "inactive"},
				}
				if !reflect.DeepEqual(candidate, journal) ||
					!reflect.DeepEqual(candidate.TargetUnitsBefore, wantPreimage) {
					return errors.New("restored proof received another frozen journal")
				}
				wantNamed := bindInstallUnitState{
					name: "named.service", loadState: "loaded",
					activeState: "inactive", unitFileState: "disabled",
				}
				wantAlias := bindInstallUnitState{
					name: "bind9.service", loadState: "not-found", activeState: "inactive",
				}
				if postTarget["named.service"] != wantNamed ||
					postTarget["bind9.service"] != wantAlias {
					return errors.New("Frankfurt rollback compensation is not exact")
				}
				return nil
			}
			ops.writeJournal = func(candidate dnsEngineSwitchJournal) error {
				*events = append(*events, "write")
				if phase != dnsSwitchPhaseRollingBack ||
					candidate.Phase != dnsSwitchPhaseRolledBack {
					return errors.New("unexpected durable rollback phase write")
				}
				want := journal
				want.Phase = dnsSwitchPhaseRolledBack
				if !reflect.DeepEqual(candidate, want) {
					return errors.New("durable rollback phase changed the frozen journal")
				}
				journal = candidate
				return nil
			}
			ops.removeJournal = func() error {
				*events = append(*events, "remove")
				journalExists = false
				return nil
			}

			if err := prepareBINDGenerationRootForSignedUpdateWithOps(
				context.Background(), ops,
			); err != nil {
				t.Fatal(err)
			}
			want := []string{
				"idle", "profile", "journal", "ledger", "journal",
			}
			if phase == dnsSwitchPhaseRollingBack {
				want = append(want, "rollback", "restored", "write")
			} else {
				want = append(want, "restored")
			}
			want = append(want,
				"journal", "ledger", "idle", "journal", "remove",
				"install", "state", "ownership", "package", "parent", "prepare",
			)
			if !reflect.DeepEqual(*events, want) {
				t.Fatalf("recovery events = %#v, want %#v", *events, want)
			}
			if journalExists {
				t.Fatal("recovered journal was not removed last")
			}
		})
	}
}

func TestSignedUpdateBINDCommittedRecoveryFinalizesEveryCrashBoundary(t *testing.T) {
	tests := []struct {
		name            string
		installExists   bool
		ownershipExists bool
	}{
		{name: "published-before-cleanup", installExists: true},
		{name: "ownership-published", installExists: true, ownershipExists: true},
		{name: "install-retired", ownershipExists: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ops, events := signedUpdateBINDPreparationOps(t)
			journal, ledger, state, install := signedUpdateBINDCommittedRecoveryFixture(t)
			journalExists := true
			installExists := test.installExists
			ownershipExists := test.ownershipExists
			ownership := dnsEngineStateReceipt{}
			if ownershipExists {
				ownership = state
			}
			ops.readJournal = func() (dnsEngineSwitchJournal, bool, error) {
				*events = append(*events, "journal")
				return journal, journalExists, nil
			}
			ops.readLedger = func() (serviceMutationLedger, error) {
				*events = append(*events, "ledger")
				return cloneSignedUpdateLedger(ledger), nil
			}
			ops.readState = func() (dnsEngineStateReceipt, bool, error) {
				*events = append(*events, "state")
				return state, true, nil
			}
			ops.readInstall = func() (dnsEngineInstallOwnershipReceipt, bool, error) {
				*events = append(*events, "install")
				return install, installExists, nil
			}
			ops.readOwnership = func() (dnsEngineStateReceipt, bool, error) {
				*events = append(*events, "ownership")
				return ownership, ownershipExists, nil
			}
			ops.writeOwnership = func(candidate dnsEngineStateReceipt) error {
				*events = append(*events, "write-ownership")
				if candidate != state {
					return errors.New("ownership publication changed active state")
				}
				ownership, ownershipExists = candidate, true
				return nil
			}
			ops.retireInstall = func(candidate dnsEngineSwitchJournal) error {
				*events = append(*events, "retire-install")
				if !ownershipExists || ownership != state ||
					!reflect.DeepEqual(candidate, journal) {
					return errors.New("install retirement preceded exact ownership")
				}
				installExists = false
				return nil
			}
			ops.removeJournal = func() error {
				*events = append(*events, "remove")
				if installExists || !ownershipExists || ownership != state {
					return errors.New("journal removal preceded exact provenance finalization")
				}
				journalExists = false
				return nil
			}

			recovered, err := recoverDNSEngineSwitchJournalForSignedUpdate(
				context.Background(), ops,
			)
			if err != nil || !recovered {
				t.Fatalf("recovered=%v err=%v events=%#v", recovered, err, *events)
			}
			if journalExists || installExists || !ownershipExists || ownership != state {
				t.Fatalf(
					"incomplete final state journal=%v install=%v ownership=%v state=%+v",
					journalExists, installExists, ownershipExists, ownership,
				)
			}
			index := func(event string) int {
				for i, candidate := range *events {
					if candidate == event {
						return i
					}
				}
				return -1
			}
			writeIndex := index("write-ownership")
			retireIndex := index("retire-install")
			removeIndex := index("remove")
			if (!test.ownershipExists && writeIndex < 0) ||
				(test.ownershipExists && writeIndex >= 0) ||
				retireIndex < 0 || removeIndex <= retireIndex ||
				(writeIndex >= 0 && retireIndex <= writeIndex) ||
				removeIndex != len(*events)-1 {
				t.Fatalf("unsafe finalization order: %#v", *events)
			}
		})
	}
}

func TestSignedUpdateBINDPreparationFinalizesBostonCommittedPowerDNS(t *testing.T) {
	ops, events := signedUpdateBINDPreparationOps(t)
	journal, ledger, state, install :=
		signedUpdatePDNSBostonCommittedRecoveryFixture(t)
	for _, artifact := range []string{journal.PDNSCandidatePath, journal.PDNSBackupPath} {
		if err := os.MkdirAll(filepath.Dir(artifact), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(artifact, []byte(`signed-update committed artifact`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	journalExists := true
	installExists := true
	ownershipExists := false
	ownership := dnsEngineStateReceipt{}
	targetProved := false
	ops.readJournal = func() (dnsEngineSwitchJournal, bool, error) {
		*events = append(*events, "journal")
		return journal, journalExists, nil
	}
	ops.readLedger = func() (serviceMutationLedger, error) {
		*events = append(*events, "ledger")
		return cloneSignedUpdateLedger(ledger), nil
	}
	ops.verifyTarget = func(_ context.Context, candidate dnsEngineSwitchJournal) error {
		*events = append(*events, "target-pdns-secondary")
		if candidate.TargetEngine != transport.DNSEnginePowerDNS {
			return errors.New("Boston recovery tried another DNS engine")
		}
		if candidate.PairRole != transport.DNSPairRoleSecondary {
			return errors.New("Boston recovery lost the secondary role")
		}
		targetProved = true
		return nil
	}
	ops.readState = func() (dnsEngineStateReceipt, bool, error) {
		*events = append(*events, "state-pdns")
		return state, true, nil
	}
	ops.readEngineInstall = func(
		engine transport.DNSEngine,
	) (dnsEngineInstallOwnershipReceipt, bool, error) {
		*events = append(*events, "install-pdns")
		if engine != transport.DNSEnginePowerDNS {
			return dnsEngineInstallOwnershipReceipt{}, false,
				errors.New("Boston recovery read another install owner")
		}
		return install, installExists, nil
	}
	ops.readEngineOwnership = func(
		engine transport.DNSEngine,
	) (dnsEngineStateReceipt, bool, error) {
		*events = append(*events, "ownership-pdns")
		if engine != transport.DNSEnginePowerDNS {
			return dnsEngineStateReceipt{}, false,
				errors.New("Boston recovery read another active owner")
		}
		return ownership, ownershipExists, nil
	}
	ops.writeOwnership = func(candidate dnsEngineStateReceipt) error {
		*events = append(*events, "write-ownership-pdns")
		if candidate != state {
			return errors.New("Boston ownership publication changed state")
		}
		ownership = candidate
		ownershipExists = true
		return nil
	}
	ops.finalizeArtifacts = func(candidate dnsEngineSwitchJournal) error {
		*events = append(*events, `finalize-pdns-artifacts`)
		if !reflect.DeepEqual(candidate, journal) {
			return errors.New(`Boston artifact finalization changed journal`)
		}
		if err := finalizeCommittedDNSEngineSwitchArtifacts(candidate); err != nil {
			return err
		}
		for _, artifact := range []string{journal.PDNSCandidatePath, journal.PDNSBackupPath} {
			if _, err := os.Lstat(artifact); !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf(`PowerDNS artifact remains after signed-update finalization at %s: %v`, artifact, err)
			}
		}
		return nil
	}
	ops.retireInstall = func(candidate dnsEngineSwitchJournal) error {
		*events = append(*events, "retire-install-pdns")
		if !reflect.DeepEqual(candidate, journal) {
			return errors.New("Boston install retirement changed journal")
		}
		if !ownershipExists {
			return errors.New("Boston install retired before active ownership")
		}
		if ownership != state {
			return errors.New("Boston install retired before exact ownership")
		}
		installExists = false
		return nil
	}
	ops.removeJournal = func() error {
		*events = append(*events, "remove-pdns-journal")
		if installExists {
			return errors.New("Boston journal removed before install handoff")
		}
		if !ownershipExists {
			return errors.New("Boston journal removed without active ownership")
		}
		journalExists = false
		return nil
	}

	recovered, err := recoverDNSEngineSwitchJournalForSignedUpdate(
		context.Background(), ops,
	)
	if err != nil {
		t.Fatalf("Boston committed PowerDNS recovery failed: %v events=%#v", err, *events)
	}
	if !recovered {
		t.Fatalf("Boston committed PowerDNS recovery was not handled: %#v", *events)
	}
	if !targetProved {
		t.Fatal("Boston live PowerDNS target was not proved")
	}
	if journalExists {
		t.Fatal("Boston committed PowerDNS journal remains")
	}
	if installExists {
		t.Fatal("Boston install ownership was not retired")
	}
	if !ownershipExists {
		t.Fatal("Boston active PowerDNS ownership was not published")
	}
	if ownership != state {
		t.Fatalf("Boston active ownership=%+v want=%+v", ownership, state)
	}
}

func TestSignedUpdateCommittedPowerDNSArtifactTOCTOUFailsClosedWithJournalRetained(t *testing.T) {
	ops, events := signedUpdateBINDPreparationOps(t)
	journal, ledger, state, install :=
		signedUpdatePDNSBostonCommittedRecoveryFixture(t)
	journalExists := true
	installExists := true
	ownershipExists := false
	ownership := dnsEngineStateReceipt{}
	retireCalled := false
	removeCalled := false
	ops.readJournal = func() (dnsEngineSwitchJournal, bool, error) {
		*events = append(*events, `journal-pdns-toctou`)
		return journal, journalExists, nil
	}
	ops.readLedger = func() (serviceMutationLedger, error) {
		*events = append(*events, `ledger-pdns-toctou`)
		return cloneSignedUpdateLedger(ledger), nil
	}
	ops.readState = func() (dnsEngineStateReceipt, bool, error) {
		return state, true, nil
	}
	ops.readEngineInstall = func(
		transport.DNSEngine,
	) (dnsEngineInstallOwnershipReceipt, bool, error) {
		return install, installExists, nil
	}
	ops.readEngineOwnership = func(
		transport.DNSEngine,
	) (dnsEngineStateReceipt, bool, error) {
		return ownership, ownershipExists, nil
	}
	ops.writeOwnership = func(candidate dnsEngineStateReceipt) error {
		ownership = candidate
		ownershipExists = true
		return nil
	}
	ops.finalizeArtifacts = func(dnsEngineSwitchJournal) error {
		return errors.New(`PowerDNS switch artifact changed before quarantine`)
	}
	ops.retireInstall = func(dnsEngineSwitchJournal) error {
		retireCalled = true
		installExists = false
		return nil
	}
	ops.removeJournal = func() error {
		removeCalled = true
		journalExists = false
		return nil
	}

	recovered, err := recoverDNSEngineSwitchJournalForSignedUpdate(
		context.Background(), ops,
	)
	if recovered || err == nil ||
		!strings.Contains(err.Error(), `changed before quarantine`) {
		t.Fatalf(`recovered=%v err=%v events=%#v`, recovered, err, *events)
	}
	if !journalExists {
		t.Fatal(`TOCTOU failure removed the committed journal`)
	}
	if retireCalled || removeCalled || !installExists {
		t.Fatalf(
			`TOCTOU failure crossed finalization boundary: retire=%v remove=%v install=%v events=%#v`,
			retireCalled, removeCalled, installExists, *events,
		)
	}
	if !ownershipExists || ownership != state {
		t.Fatal(`TOCTOU failure lost the exact published active ownership`)
	}
}

func TestSignedUpdateCommittedSwitchRejectsMissingInstallAndActiveOwnership(t *testing.T) {
	ops, events := signedUpdateBINDPreparationOps(t)
	journal, ledger, state, _ := signedUpdatePDNSBostonCommittedRecoveryFixture(t)
	ops.readJournal = func() (dnsEngineSwitchJournal, bool, error) {
		*events = append(*events, "journal")
		return journal, true, nil
	}
	ops.readLedger = func() (serviceMutationLedger, error) {
		*events = append(*events, "ledger")
		return cloneSignedUpdateLedger(ledger), nil
	}
	ops.readState = func() (dnsEngineStateReceipt, bool, error) {
		*events = append(*events, "state-pdns")
		return state, true, nil
	}
	ops.readEngineInstall = func(
		transport.DNSEngine,
	) (dnsEngineInstallOwnershipReceipt, bool, error) {
		*events = append(*events, "install-missing")
		return dnsEngineInstallOwnershipReceipt{}, false, nil
	}
	ops.readEngineOwnership = func(
		transport.DNSEngine,
	) (dnsEngineStateReceipt, bool, error) {
		*events = append(*events, "ownership-missing")
		return dnsEngineStateReceipt{}, false, nil
	}
	mutated := false
	ops.writeOwnership = func(dnsEngineStateReceipt) error {
		mutated = true
		return nil
	}
	ops.retireInstall = func(dnsEngineSwitchJournal) error {
		mutated = true
		return nil
	}
	ops.removeJournal = func() error {
		mutated = true
		return nil
	}

	recovered, err := recoverDNSEngineSwitchJournalForSignedUpdate(
		context.Background(), ops,
	)
	if recovered {
		t.Fatalf("missing switch provenance recovered: %#v", *events)
	}
	if err == nil {
		t.Fatal("missing switch provenance was accepted")
	}
	if !strings.Contains(err.Error(), "no exact install or active ownership") {
		t.Fatalf("unexpected missing provenance error: %v", err)
	}
	if mutated {
		t.Fatalf("missing provenance reached mutation: %#v", *events)
	}
}

func TestSignedUpdatePreparePathPacmanCommittedPowerDNSAdoptUsesExactRuntimeProvenanceWithoutInstallReceipt(
	t *testing.T,
) {
	ops, events := signedUpdateBINDPreparationOps(t)
	ops.detectProfile = func() (hostplatform.Profile, error) {
		*events = append(*events, "profile-pacman-adopt")
		return hostplatform.Profile{PackageManager: hostplatform.PackageManagerPacman}, nil
	}
	journal, ledger, state := signedUpdatePDNSAdoptCommittedRecoveryFixture(t)
	journalExists := true
	ownershipExists := false
	ownership := dnsEngineStateReceipt{}
	targetProved := false
	ops.readJournal = func() (dnsEngineSwitchJournal, bool, error) {
		*events = append(*events, "journal-adopt")
		return journal, journalExists, nil
	}
	ops.readLedger = func() (serviceMutationLedger, error) {
		*events = append(*events, "ledger-adopt")
		return cloneSignedUpdateLedger(ledger), nil
	}
	ops.verifyTarget = func(_ context.Context, candidate dnsEngineSwitchJournal) error {
		*events = append(*events, "target-adopt-pdns")
		if candidate.Mode != transport.DNSEngineSwitchModeAdopt {
			return errors.New("adopt runtime proof received switch mode")
		}
		if candidate.TargetEngine != transport.DNSEnginePowerDNS {
			return errors.New("adopt runtime proof received another target")
		}
		targetProved = true
		return nil
	}
	ops.readState = func() (dnsEngineStateReceipt, bool, error) {
		*events = append(*events, "state-adopt")
		return state, true, nil
	}
	ops.readEngineInstall = func(
		transport.DNSEngine,
	) (dnsEngineInstallOwnershipReceipt, bool, error) {
		*events = append(*events, "install-absent-adopt")
		return dnsEngineInstallOwnershipReceipt{}, false, nil
	}
	ops.readEngineOwnership = func(
		transport.DNSEngine,
	) (dnsEngineStateReceipt, bool, error) {
		*events = append(*events, "ownership-adopt")
		return ownership, ownershipExists, nil
	}
	ops.writeOwnership = func(candidate dnsEngineStateReceipt) error {
		*events = append(*events, "write-ownership-adopt")
		if candidate != state {
			return errors.New("adopt ownership differs from exact state")
		}
		ownership = candidate
		ownershipExists = true
		return nil
	}
	ops.retireInstall = func(candidate dnsEngineSwitchJournal) error {
		*events = append(*events, "confirm-no-install-adopt")
		if !reflect.DeepEqual(candidate, journal) {
			return errors.New("adopt finalization changed journal")
		}
		if !ownershipExists {
			return errors.New("adopt finalization lacks active ownership")
		}
		return nil
	}
	ops.removeJournal = func() error {
		*events = append(*events, "remove-adopt-journal")
		if !ownershipExists {
			return errors.New("adopt journal removed before ownership publication")
		}
		journalExists = false
		return nil
	}

	err := prepareBINDGenerationRootForSignedUpdateWithOps(context.Background(), ops)
	if err != nil {
		t.Fatalf("exact PowerDNS adoption recovery failed: %v events=%#v", err, *events)
	}
	if !targetProved {
		t.Fatal("PowerDNS adoption live runtime was not proved")
	}
	if journalExists {
		t.Fatal("PowerDNS adoption journal remains")
	}
	if !ownershipExists {
		t.Fatal("PowerDNS adoption active ownership was not published")
	}
}

func TestSignedUpdateBostonPowerDNSRecoveryEvidenceMismatchFailsClosed(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(
			*serviceMutationLedger,
			*dnsEngineInstallOwnershipReceipt,
			*dnsEngineStateReceipt,
			*bool,
		)
	}{
		{
			name: "terminal-ledger-owner",
			mutate: func(
				ledger *serviceMutationLedger,
				_ *dnsEngineInstallOwnershipReceipt,
				_ *dnsEngineStateReceipt,
				_ *bool,
			) {
				for _, job := range ledger.Jobs {
					job.OwnerID = strings.Repeat("c", 32)
				}
			},
		},
		{
			name: "install-receipt-owner",
			mutate: func(
				_ *serviceMutationLedger,
				install *dnsEngineInstallOwnershipReceipt,
				_ *dnsEngineStateReceipt,
				_ *bool,
			) {
				install.MutationOwnerID = strings.Repeat("c", 32)
			},
		},
		{
			name: "foreign-active-ownership",
			mutate: func(
				_ *serviceMutationLedger,
				_ *dnsEngineInstallOwnershipReceipt,
				ownership *dnsEngineStateReceipt,
				ownershipExists *bool,
			) {
				ownership.MutationOwnerID = strings.Repeat("c", 32)
				*ownershipExists = true
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ops, events := signedUpdateBINDPreparationOps(t)
			journal, ledger, state, install :=
				signedUpdatePDNSBostonCommittedRecoveryFixture(t)
			ownership := state
			ownershipExists := false
			test.mutate(&ledger, &install, &ownership, &ownershipExists)
			ops.readJournal = func() (dnsEngineSwitchJournal, bool, error) {
				*events = append(*events, "journal")
				return journal, true, nil
			}
			ops.readLedger = func() (serviceMutationLedger, error) {
				*events = append(*events, "ledger")
				return cloneSignedUpdateLedger(ledger), nil
			}
			ops.readState = func() (dnsEngineStateReceipt, bool, error) {
				*events = append(*events, "state-pdns")
				return state, true, nil
			}
			ops.readEngineInstall = func(
				transport.DNSEngine,
			) (dnsEngineInstallOwnershipReceipt, bool, error) {
				*events = append(*events, "install-pdns")
				return install, true, nil
			}
			ops.readEngineOwnership = func(
				transport.DNSEngine,
			) (dnsEngineStateReceipt, bool, error) {
				*events = append(*events, "ownership-pdns")
				return ownership, ownershipExists, nil
			}
			mutated := false
			ops.writeOwnership = func(dnsEngineStateReceipt) error {
				mutated = true
				return nil
			}
			ops.retireInstall = func(dnsEngineSwitchJournal) error {
				mutated = true
				return nil
			}
			ops.removeJournal = func() error {
				mutated = true
				return nil
			}

			recovered, err := recoverDNSEngineSwitchJournalForSignedUpdate(
				context.Background(), ops,
			)
			if recovered {
				t.Fatalf("mismatched Boston evidence recovered: %#v", *events)
			}
			if err == nil {
				t.Fatal("mismatched Boston evidence was accepted")
			}
			if mutated {
				t.Fatalf("mismatched Boston evidence reached mutation: %#v", *events)
			}
		})
	}
}

func TestSignedUpdateBINDCommittedRecoveryFailsClosedBeforeProvenanceMutation(t *testing.T) {
	ops, events := signedUpdateBINDPreparationOps(t)
	journal, ledger, state, install := signedUpdateBINDCommittedRecoveryFixture(t)
	ops.readJournal = func() (dnsEngineSwitchJournal, bool, error) {
		*events = append(*events, "journal")
		return journal, true, nil
	}
	ops.readLedger = func() (serviceMutationLedger, error) {
		*events = append(*events, "ledger")
		return cloneSignedUpdateLedger(ledger), nil
	}
	ops.readState = func() (dnsEngineStateReceipt, bool, error) {
		*events = append(*events, "state")
		return state, true, nil
	}
	ops.readInstall = func() (dnsEngineInstallOwnershipReceipt, bool, error) {
		*events = append(*events, "install")
		return install, true, nil
	}
	ops.verifyTarget = func(context.Context, dnsEngineSwitchJournal) error {
		*events = append(*events, "target")
		return errors.New("named no longer serves the committed generation")
	}
	if recovered, err := recoverDNSEngineSwitchJournalForSignedUpdate(
		context.Background(), ops,
	); recovered || err == nil || !strings.Contains(err.Error(), "named no longer") {
		t.Fatalf("recovered=%v err=%v", recovered, err)
	}
	for _, forbidden := range []string{"write-ownership", "retire-install", "remove"} {
		if containsString(*events, forbidden) {
			t.Fatalf("target proof failure reached %s: %#v", forbidden, *events)
		}
	}
}

func TestSignedUpdateBINDRecoveryCrashAfterDurablePhaseDoesNotReplayRollback(t *testing.T) {
	journal, ledger := signedUpdateBINDRecoveryFixture(t)
	journalExists := true
	rollbackCalls := 0
	ledgerReads := 0
	failAfterWrite := true
	events := []string{}
	ops := bindSignedUpdatePreparationOps{
		readJournal: func() (dnsEngineSwitchJournal, bool, error) {
			events = append(events, "journal")
			return journal, journalExists, nil
		},
		readLedger: func() (serviceMutationLedger, error) {
			events = append(events, "ledger")
			ledgerReads++
			if failAfterWrite && ledgerReads == 2 {
				return serviceMutationLedger{}, errors.New("simulated crash boundary")
			}
			return cloneSignedUpdateLedger(ledger), nil
		},
		rollbackJournal: func(context.Context, dnsEngineSwitchJournal) error {
			events = append(events, "rollback")
			rollbackCalls++
			return nil
		},
		verifyRestored: func(context.Context, dnsEngineSwitchJournal) error {
			events = append(events, "restored")
			return nil
		},
		writeJournal: func(candidate dnsEngineSwitchJournal) error {
			events = append(events, "write")
			want := journal
			want.Phase = dnsSwitchPhaseRolledBack
			if !reflect.DeepEqual(candidate, want) {
				return errors.New("unexpected durable journal")
			}
			journal = candidate
			return nil
		},
		checkIdle: func() error {
			events = append(events, "idle")
			return nil
		},
		removeJournal: func() error {
			events = append(events, "remove")
			journalExists = false
			return nil
		},
	}

	if recovered, err := recoverDNSEngineSwitchJournalForSignedUpdate(
		context.Background(), ops,
	); recovered || err == nil || !strings.Contains(err.Error(), "after signed-update DNS rollback") {
		t.Fatalf("first recovery recovered=%v err=%v", recovered, err)
	}
	wantFirst := []string{
		"journal", "ledger", "journal", "rollback", "restored",
		"write", "journal", "ledger",
	}
	if !reflect.DeepEqual(events, wantFirst) {
		t.Fatalf("first recovery events = %#v, want %#v", events, wantFirst)
	}
	if !journalExists || journal.Phase != dnsSwitchPhaseRolledBack || rollbackCalls != 1 {
		t.Fatalf(
			"durable crash boundary exists=%v phase=%s rollback calls=%d",
			journalExists, journal.Phase, rollbackCalls,
		)
	}

	failAfterWrite = false
	ledgerReads = 0
	events = nil
	if recovered, err := recoverDNSEngineSwitchJournalForSignedUpdate(
		context.Background(), ops,
	); err != nil || !recovered {
		t.Fatalf("retry recovered=%v err=%v", recovered, err)
	}
	wantRetry := []string{
		"journal", "ledger", "journal", "restored", "journal",
		"ledger", "idle", "journal", "remove",
	}
	if !reflect.DeepEqual(events, wantRetry) {
		t.Fatalf("retry events = %#v, want %#v", events, wantRetry)
	}
	if journalExists || rollbackCalls != 1 {
		t.Fatalf("retry replayed rollback or retained journal: exists=%v calls=%d", journalExists, rollbackCalls)
	}
}

func TestSignedUpdateBINDPreparationNoProvenanceIsReadOnlyNoop(t *testing.T) {
	ops, events := signedUpdateBINDPreparationOps(t)
	ops.readInstall = func() (dnsEngineInstallOwnershipReceipt, bool, error) {
		*events = append(*events, "install")
		return dnsEngineInstallOwnershipReceipt{}, false, nil
	}
	if err := prepareBINDGenerationRootForSignedUpdateWithOps(
		context.Background(), ops,
	); err != nil {
		t.Fatal(err)
	}
	for _, event := range *events {
		if event == "package" || event == "parent" || event == "prepare" {
			t.Fatalf("unmanaged host reached mutation path: %#v", *events)
		}
	}
}

func TestSignedUpdateBINDPreparationAcceptsExactManagedStateOrOwnership(t *testing.T) {
	base := legacyDurableDNSState(transport.DNSEngineBIND)
	for _, source := range []string{"state", "ownership"} {
		t.Run(source, func(t *testing.T) {
			ops, events := signedUpdateBINDPreparationOps(t)
			ops.readInstall = func() (dnsEngineInstallOwnershipReceipt, bool, error) {
				*events = append(*events, "install")
				return dnsEngineInstallOwnershipReceipt{}, false, nil
			}
			if source == "state" {
				ops.readState = func() (dnsEngineStateReceipt, bool, error) {
					*events = append(*events, "state")
					return base, true, nil
				}
			} else {
				ops.readOwnership = func() (dnsEngineStateReceipt, bool, error) {
					*events = append(*events, "ownership")
					return base, true, nil
				}
			}
			if err := prepareBINDGenerationRootForSignedUpdateWithOps(
				context.Background(), ops,
			); err != nil {
				t.Fatal(err)
			}
			wantTail := []string{"harden-existing", "verify-existing"}
			if len(*events) < len(wantTail) ||
				!reflect.DeepEqual((*events)[len(*events)-len(wantTail):], wantTail) {
				t.Fatalf("managed provenance did not prove existing tree: %#v", *events)
			}
			for _, event := range *events {
				if event == "prepare" {
					t.Fatalf("state/ownership path created a BIND child: %#v", *events)
				}
			}
		})
	}
}

func TestSignedUpdateBINDManagedReceiptRejectsMissingOrDriftedExistingTree(t *testing.T) {
	for _, failure := range []string{"missing-child", "current-drift", "config-drift"} {
		t.Run(failure, func(t *testing.T) {
			ops, events := signedUpdateBINDPreparationOps(t)
			ops.readInstall = func() (dnsEngineInstallOwnershipReceipt, bool, error) {
				*events = append(*events, "install")
				return dnsEngineInstallOwnershipReceipt{}, false, nil
			}
			state := legacyDurableDNSState(transport.DNSEngineBIND)
			ops.readState = func() (dnsEngineStateReceipt, bool, error) {
				*events = append(*events, "state")
				return state, true, nil
			}
			ops.verifyExisting = func(
				context.Context, dnsEngineStateReceipt,
			) error {
				*events = append(*events, "verify-existing")
				return errors.New(failure)
			}
			if err := prepareBINDGenerationRootForSignedUpdateWithOps(
				context.Background(), ops,
			); err == nil || !strings.Contains(err.Error(), failure) {
				t.Fatalf("error = %v, want %s", err, failure)
			}
			if !containsString(*events, "harden-existing") {
				t.Fatalf("monotonic parent hardening did not run: %#v", *events)
			}
			for _, event := range *events {
				if event == "prepare" {
					t.Fatalf("existing-tree failure created a child: %#v", *events)
				}
			}
		})
	}
}

func TestSignedUpdateBINDPreparationRejectsTransitionalAndManagedCoexistence(t *testing.T) {
	for _, source := range []string{"state", "ownership", "matching-state-and-ownership"} {
		t.Run(source, func(t *testing.T) {
			ops, events := signedUpdateBINDPreparationOps(t)
			managed := legacyDurableDNSState(transport.DNSEngineBIND)
			if source == "state" || source == "matching-state-and-ownership" {
				ops.readState = func() (dnsEngineStateReceipt, bool, error) {
					*events = append(*events, "state")
					return managed, true, nil
				}
			}
			if source == "ownership" || source == "matching-state-and-ownership" {
				ops.readOwnership = func() (dnsEngineStateReceipt, bool, error) {
					*events = append(*events, "ownership")
					return managed, true, nil
				}
			}
			if err := prepareBINDGenerationRootForSignedUpdateWithOps(
				context.Background(), ops,
			); err == nil || !strings.Contains(err.Error(), "coexists") {
				t.Fatalf("error = %v, want mixed-provenance rejection", err)
			}
			for _, event := range *events {
				if event == "package" || event == "parent" || event == "prepare" ||
					event == "harden-existing" || event == "verify-existing" {
					t.Fatalf("mixed provenance reached host mutation/proof: %#v", *events)
				}
			}
		})
	}
}

func TestSignedUpdateBINDPreparationJournalRecoveryFailuresStopBeforeProvenance(t *testing.T) {
	tests := []struct {
		name      string
		want      []string
		wantError string
	}{
		{"target-staged", []string{"idle", "profile", "journal"}, "rollback phase"},
		{"target-verified", []string{"idle", "profile", "journal"}, "rollback phase"},
		{"committed", []string{"idle", "profile", "journal", "profile", "ledger"}, "exact published success ledger job"},
		{"active-ledger", []string{"idle", "profile", "journal", "ledger"}, "ledger"},
		{"failed-job-phase-mismatch", []string{"idle", "profile", "journal", "ledger"}, "exact terminal failed ledger job"},
		{"failed-job-binding-mismatch", []string{"idle", "profile", "journal", "ledger"}, "exact terminal failed ledger job"},
		{"journal-drift-before-rollback", []string{"idle", "profile", "journal", "ledger", "journal"}, "journal changed"},
		{"rollback-failed", []string{"idle", "profile", "journal", "ledger", "journal", "rollback"}, "resume signed-update"},
		{"restored-proof-failed", []string{"idle", "profile", "journal", "ledger", "journal", "rollback", "restored"}, "rollback host state"},
		{"phase-write-failed", []string{"idle", "profile", "journal", "ledger", "journal", "rollback", "restored", "write"}, "persist signed-update"},
		{"journal-drift-after-proof", []string{"idle", "profile", "journal", "ledger", "journal", "rollback", "restored", "write", "journal"}, "changed during"},
		{"journal-unreadable-after-proof", []string{"idle", "profile", "journal", "ledger", "journal", "rollback", "restored", "write", "journal"}, "after rollback proof"},
		{"ledger-drift-after-proof", []string{"idle", "profile", "journal", "ledger", "journal", "rollback", "restored", "write", "journal", "ledger"}, "ledger changed"},
		{"ledger-unreadable-after-proof", []string{"idle", "profile", "journal", "ledger", "journal", "rollback", "restored", "write", "journal", "ledger"}, "read service mutation ledger after"},
		{"ledger-not-idle-after-recovery", []string{"idle", "profile", "journal", "ledger", "journal", "rollback", "restored", "write", "journal", "ledger", "idle"}, "idle ledger after DNS recovery"},
		{"journal-drift-before-removal", []string{"idle", "profile", "journal", "ledger", "journal", "rollback", "restored", "write", "journal", "ledger", "idle", "journal"}, "changed before signed-update removal"},
		{"journal-unreadable-before-removal", []string{"idle", "profile", "journal", "ledger", "journal", "rollback", "restored", "write", "journal", "ledger", "idle", "journal"}, "before removal"},
		{"remove-failed", []string{"idle", "profile", "journal", "ledger", "journal", "rollback", "restored", "write", "journal", "ledger", "idle", "journal", "remove"}, "remove recovered"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ops, events := signedUpdateBINDPreparationOps(t)
			journal, ledger := signedUpdateBINDRecoveryFixture(t)
			journalReads := 0
			ledgerReads := 0
			switch test.name {
			case "target-staged":
				journal.Phase = dnsSwitchPhaseTargetStaged
			case "target-verified":
				journal.Phase = dnsSwitchPhaseTargetVerified
			case "committed":
				journal.Phase = dnsSwitchPhaseCommitted
			case "active-ledger":
				ledger.ActiveRequestID = journal.MutationRequestID
			case "failed-job-phase-mismatch":
				ledger.Jobs[journal.MutationRequestID].Phase = "rollback"
			case "failed-job-binding-mismatch":
				ledger.Jobs[journal.MutationRequestID].OwnerID = strings.Repeat("e", 32)
			}
			ops.readJournal = func() (dnsEngineSwitchJournal, bool, error) {
				*events = append(*events, "journal")
				journalReads++
				if test.name == "journal-unreadable-after-proof" && journalReads == 3 {
					return dnsEngineSwitchJournal{}, false, errors.New("journal unreadable")
				}
				if test.name == "journal-unreadable-before-removal" && journalReads == 4 {
					return dnsEngineSwitchJournal{}, false, errors.New("journal unreadable")
				}
				drift := (test.name == "journal-drift-before-rollback" && journalReads == 2) ||
					(test.name == "journal-drift-after-proof" && journalReads == 3) ||
					(test.name == "journal-drift-before-removal" && journalReads == 4)
				if drift {
					drifted := journal
					if journalReads == 2 {
						drifted.Phase = dnsSwitchPhaseRolledBack
					} else {
						drifted.Phase = dnsSwitchPhaseRollingBack
					}
					return drifted, true, nil
				}
				return journal, true, nil
			}
			ops.readLedger = func() (serviceMutationLedger, error) {
				*events = append(*events, "ledger")
				ledgerReads++
				if test.name == "ledger-unreadable-after-proof" && ledgerReads == 2 {
					return serviceMutationLedger{}, errors.New("ledger unreadable")
				}
				current := cloneSignedUpdateLedger(ledger)
				if test.name == "ledger-drift-after-proof" && ledgerReads == 2 {
					current.Jobs[strings.Repeat("c", 32)].ErrorMessage = "changed unrelated history"
				}
				return current, nil
			}
			ops.rollbackJournal = func(context.Context, dnsEngineSwitchJournal) error {
				*events = append(*events, "rollback")
				if test.name == "rollback-failed" {
					return errors.New("rollback failed")
				}
				return nil
			}
			ops.verifyRestored = func(context.Context, dnsEngineSwitchJournal) error {
				*events = append(*events, "restored")
				if test.name == "restored-proof-failed" {
					return errors.New("restored state is not exact")
				}
				return nil
			}
			ops.writeJournal = func(candidate dnsEngineSwitchJournal) error {
				*events = append(*events, "write")
				if test.name == "phase-write-failed" {
					return errors.New("phase write failed")
				}
				want := journal
				want.Phase = dnsSwitchPhaseRolledBack
				if !reflect.DeepEqual(candidate, want) {
					return errors.New("phase write changed the journal")
				}
				journal = candidate
				return nil
			}
			if test.name == "ledger-not-idle-after-recovery" {
				idleChecks := 0
				ops.checkIdle = func() error {
					*events = append(*events, "idle")
					idleChecks++
					if idleChecks == 2 {
						return errors.New("ledger became active")
					}
					return nil
				}
			}
			ops.removeJournal = func() error {
				*events = append(*events, "remove")
				if test.name == "remove-failed" {
					return errors.New("journal remained")
				}
				return nil
			}
			err := prepareBINDGenerationRootForSignedUpdateWithOps(
				context.Background(), ops,
			)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("error = %v, want %q", err, test.wantError)
			}
			if !reflect.DeepEqual(*events, test.want) {
				t.Fatalf("events = %#v, want %#v", *events, test.want)
			}
			if containsString(*events, "remove") && test.name != "remove-failed" {
				t.Fatal("unsafe recovery reached journal removal")
			}
		})
	}
}

func TestSignedUpdateBINDPreparationFailsClosedBeforeMutation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*bindSignedUpdatePreparationOps, *[]string)
	}{
		{name: "idle", mutate: func(ops *bindSignedUpdatePreparationOps, _ *[]string) {
			ops.checkIdle = func() error { return errors.New("lock lost") }
		}},
		{name: "profile", mutate: func(ops *bindSignedUpdatePreparationOps, _ *[]string) {
			ops.detectProfile = func() (hostplatform.Profile, error) {
				return hostplatform.Profile{}, errors.New("profile")
			}
		}},
		{name: "journal-present", mutate: func(ops *bindSignedUpdatePreparationOps, _ *[]string) {
			ops.readJournal = func() (dnsEngineSwitchJournal, bool, error) {
				return dnsEngineSwitchJournal{}, true, nil
			}
		}},
		{name: "journal-unreadable", mutate: func(ops *bindSignedUpdatePreparationOps, _ *[]string) {
			ops.readJournal = func() (dnsEngineSwitchJournal, bool, error) {
				return dnsEngineSwitchJournal{}, false, errors.New("journal")
			}
		}},
		{name: "install-mismatch", mutate: func(ops *bindSignedUpdatePreparationOps, _ *[]string) {
			original := ops.readInstall
			ops.readInstall = func() (dnsEngineInstallOwnershipReceipt, bool, error) {
				receipt, exists, err := original()
				receipt.Packages = []string{"bind9", "bind9-utils"}
				return receipt, exists, err
			}
		}},
		{name: "install-corrupt", mutate: func(ops *bindSignedUpdatePreparationOps, _ *[]string) {
			original := ops.readInstall
			ops.readInstall = func() (dnsEngineInstallOwnershipReceipt, bool, error) {
				receipt, exists, err := original()
				receipt.MissingBefore = nil
				return receipt, exists, err
			}
		}},
		{name: "install-unreadable", mutate: func(ops *bindSignedUpdatePreparationOps, _ *[]string) {
			ops.readInstall = func() (dnsEngineInstallOwnershipReceipt, bool, error) {
				return dnsEngineInstallOwnershipReceipt{}, false, errors.New("install")
			}
		}},
		{name: "state-corrupt", mutate: func(ops *bindSignedUpdatePreparationOps, _ *[]string) {
			ops.readState = func() (dnsEngineStateReceipt, bool, error) {
				return dnsEngineStateReceipt{Engine: transport.DNSEngineBIND}, true, nil
			}
		}},
		{name: "ownership-wrong-engine", mutate: func(ops *bindSignedUpdatePreparationOps, _ *[]string) {
			ops.readOwnership = func() (dnsEngineStateReceipt, bool, error) {
				return legacyDurableDNSState(transport.DNSEnginePowerDNS), true, nil
			}
		}},
		{name: "unsupported-profile", mutate: func(ops *bindSignedUpdatePreparationOps, _ *[]string) {
			ops.detectProfile = func() (hostplatform.Profile, error) {
				return hostplatform.Profile{
					DistroFamily:   hostplatform.DistroFamilyDebian,
					PackageManager: hostplatform.PackageManagerAPT,
					ServiceManager: "openrc",
					ID:             "operator-linux",
				}, nil
			}
		}},
		{name: "package-absent", mutate: func(ops *bindSignedUpdatePreparationOps, _ *[]string) {
			ops.packageInstalled = func(context.Context, hostplatform.Profile, string) (bool, error) {
				return false, nil
			}
		}},
		{name: "parent-absent", mutate: func(ops *bindSignedUpdatePreparationOps, _ *[]string) {
			ops.parentExists = func() (bool, error) { return false, nil }
		}},
		{name: "parent-unreadable", mutate: func(ops *bindSignedUpdatePreparationOps, _ *[]string) {
			ops.parentExists = func() (bool, error) { return false, errors.New("parent") }
		}},
		{name: "prepare", mutate: func(ops *bindSignedUpdatePreparationOps, _ *[]string) {
			ops.prepare = func(context.Context) error { return errors.New("prepare") }
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ops, events := signedUpdateBINDPreparationOps(t)
			test.mutate(&ops, events)
			err := prepareBINDGenerationRootForSignedUpdateWithOps(
				context.Background(), ops,
			)
			if err == nil {
				t.Fatal("unsafe signed-update state was accepted")
			}
			if test.name != "prepare" {
				for _, event := range *events {
					if event == "prepare" {
						t.Fatalf("mutation ran after %s failure: %#v", test.name, *events)
					}
				}
			}
		})
	}
}

func TestSignedUpdateBINDPreparationAcceptsDebian13Capabilities(t *testing.T) {
	ops, events := signedUpdateBINDPreparationOps(t)
	ops.detectProfile = func() (hostplatform.Profile, error) {
		*events = append(*events, "profile")
		return testDebian13BINDProfile(), nil
	}
	if err := prepareBINDGenerationRootForSignedUpdateWithOps(
		context.Background(), ops,
	); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"idle", "profile", "journal", "install", "state", "ownership",
		"package", "parent", "prepare",
	}
	if !reflect.DeepEqual(*events, want) {
		t.Fatalf("Debian BIND preparation events = %#v, want %#v", *events, want)
	}
}

func TestSignedUpdateBINDPreparationUnmanagedDebian13IsReadOnlyNoop(t *testing.T) {
	ops, events := signedUpdateBINDPreparationOps(t)
	ops.detectProfile = func() (hostplatform.Profile, error) {
		*events = append(*events, "profile")
		return testDebian13BINDProfile(), nil
	}
	ops.readInstall = func() (dnsEngineInstallOwnershipReceipt, bool, error) {
		*events = append(*events, "install")
		return dnsEngineInstallOwnershipReceipt{}, false, nil
	}
	if err := prepareBINDGenerationRootForSignedUpdateWithOps(
		context.Background(), ops,
	); err != nil {
		t.Fatal(err)
	}
	for _, event := range *events {
		if event == "package" || event == "parent" || event == "prepare" ||
			event == "harden-existing" || event == "verify-existing" {
			t.Fatalf("unmanaged Debian reached host path: %#v", *events)
		}
	}
}

func TestSignedUpdateBINDPreparationNonAPTIsNoopAfterIdleProof(t *testing.T) {
	ops, events := signedUpdateBINDPreparationOps(t)
	ops.detectProfile = func() (hostplatform.Profile, error) {
		*events = append(*events, "profile")
		return hostplatform.Profile{PackageManager: hostplatform.PackageManagerPacman}, nil
	}
	if err := prepareBINDGenerationRootForSignedUpdateWithOps(
		context.Background(), ops,
	); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*events, []string{"idle", "profile", "journal"}) {
		t.Fatalf("non-APT hook events = %#v", *events)
	}
}
