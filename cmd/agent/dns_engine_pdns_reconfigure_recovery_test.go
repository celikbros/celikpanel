package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func testPDNSReconfigureRecoveryJournal(
	t *testing.T,
	mainGID uint32,
) (dnsEngineSwitchJournal, *fakePDNSConfigFS) {
	t.Helper()
	installPublicListenAddressOutput(
		t, "2: eth0 inet 192.0.2.10/24 scope global eth0\n",
	)
	useTestPDNSConfigPaths(t)
	useTestServiceMutationOwner(t)
	root := t.TempDir()
	t.Setenv("CELIKPANEL_AGENT_STATE_DIR", filepath.Join(root, "state"))
	t.Setenv("CELIKPANEL_PDNS_DB", filepath.Join(root, "pdns.sqlite3"))
	manifest := testPDNSPairSecondaryReconfigureManifest(t)
	fake := newFakePDNSConfigFS(mainGID)
	main := testPDNSConfigSnapshot(
		dnsMainConf,
		"include-dir="+filepath.Clean(filepath.Dir(dnsManagedConf))+"\n",
		0o640, 0, mainGID,
	)
	fake.state[filepath.Clean(dnsMainConf)] =
		testPDNSConfigObservation(main, 190)
	managed := testPDNSConfigSnapshot(
		dnsManagedConf, string(testManagedPowerDNSStandaloneConfig(t)),
		0o644, 0, 0,
	)
	fake.state[filepath.Clean(dnsManagedConf)] =
		testPDNSConfigObservation(managed, 191)
	backup := []byte("exact legacy PowerDNS database preimage")
	requestID := strings.Repeat("a", 32)
	journal := dnsEngineSwitchJournal{
		Schema: dnsEngineSwitchJournalSchema, Phase: dnsSwitchPhaseRollingBack,
		Mode:              manifest.Mode,
		MutationRequestID: requestID, MutationOwnerID: strings.Repeat("b", 32),
		ManifestQualifier: manifest.Qualifier,
		SourceEngine:      manifest.SourceEngine, TargetEngine: manifest.TargetEngine,
		SourceEpoch: manifest.SourceEpoch, TargetEpoch: manifest.TargetEpoch,
		SourceRevision: manifest.SourceRevision, Topology: manifest.Topology,
		PairRole: manifest.PairRole, LocalIP: manifest.LocalIP, LocalNS: manifest.LocalNS,
		PeerIP: manifest.PeerIP, PeerNS: manifest.PeerNS,
		SnapshotBytes: manifest.SnapshotBytes, Zones: manifest.Zones,
		StateBefore:  dnsFileSnapshot{Path: filepath.Clean(dnsEngineStatePath())},
		ConfigBefore: fake.snapshots(),
		TargetUnitsBefore: []dnsUnitSnapshot{{
			Name: "pdns.service", LoadState: "loaded",
			ActiveState: "active", UnitFileState: "enabled",
		}},
		SourceUnitsBefore: []dnsUnitSnapshot{},
		PDNSCandidatePath: pdnsSwitchCandidatePath(requestID),
		PDNSBackupPath:    pdnsSwitchBackupPath(requestID),
		PDNSBackupSHA256:  digestDNSBytes(backup),
		PDNSBackupSize:    int64(len(backup)),
	}
	if err := validateDNSEngineSwitchJournal(journal); err != nil {
		t.Fatalf("invalid exact reconfigure recovery fixture: %v", err)
	}
	return journal, fake
}

func clonePDNSReconfigureRecoveryJournal(
	journal dnsEngineSwitchJournal,
) dnsEngineSwitchJournal {
	journal.ConfigBefore = clonePDNSConfigSnapshots(journal.ConfigBefore)
	journal.TargetUnitsBefore = append(
		[]dnsUnitSnapshot(nil), journal.TargetUnitsBefore...,
	)
	journal.SourceUnitsBefore = append(
		[]dnsUnitSnapshot(nil), journal.SourceUnitsBefore...,
	)
	journal.Zones = append(journal.Zones[:0:0], journal.Zones...)
	return journal
}

func replacePDNSReconfigureConfigSnapshot(
	journal *dnsEngineSwitchJournal,
	fake *fakePDNSConfigFS,
	snapshot dnsFileSnapshot,
) {
	path := filepath.Clean(snapshot.Path)
	for index := range journal.ConfigBefore {
		if journal.ConfigBefore[index].Path == path {
			journal.ConfigBefore[index] = snapshot
			break
		}
	}
	fake.state[path] = fake.nextObservation(snapshot)
}

func TestPDNSReconfigureRollbackUsesRestoredSolePDNSProof(t *testing.T) {
	for _, mainGID := range []uint32{109, 0} {
		t.Run(fmt.Sprintf("main-gid-%d", mainGID), func(t *testing.T) {
			journal, fake := testPDNSReconfigureRecoveryJournal(t, mainGID)
			proved, err := provePDNSPairSecondaryReconfigureRollbackWithOps(
				context.Background(), journal, fake.ops(),
			)
			if err != nil || !proved {
				t.Fatalf("exact reconfiguration proof=%v err=%v", proved, err)
			}
			pdnsCalls, noneCalls := 0, 0
			err = verifyRestoredEmptySourceAuthorityWithOps(
				context.Background(), journal,
				restoredEmptySourceAuthorityProofOps{
					pdnsConfig: fake.ops(),
					verifyOnlyPDNS: func() error {
						pdnsCalls++
						return nil
					},
					verifyNoAuthority: func() error {
						noneCalls++
						return errors.New("strict no-authority proof rejected restored PowerDNS")
					},
				},
			)
			if err != nil || pdnsCalls != 1 || noneCalls != 0 {
				t.Fatalf(
					"restored proof err=%v pdns=%d none=%d",
					err, pdnsCalls, noneCalls,
				)
			}
		})
	}
}

func TestPDNSPairSecondaryReconfigureAndRollbackNeverEnableAutoSecondary(
	t *testing.T,
) {
	manifest := testPDNSPairSecondaryReconfigureManifest(t)
	target, err := dnsClusterConfigForSwitchManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if !validManagedDNSClusterPowerDNSConfig(target) {
		t.Fatal("reconfigure target is not an exact managed pair config")
	}
	for _, token := range []string{"autosecondary", "autoprimary"} {
		if strings.Contains(strings.ToLower(target), token) {
			t.Fatalf("reconfigure target contains forbidden %s", token)
		}
	}

	journal, fake := testPDNSReconfigureRecoveryJournal(t, 109)
	for _, snapshot := range journal.ConfigBefore {
		if strings.Contains(
			strings.ToLower(string(snapshot.Data)), "autosecondary",
		) || strings.Contains(
			strings.ToLower(string(snapshot.Data)), "autoprimary",
		) {
			t.Fatal("rollback preimage contains automatic secondary discovery")
		}
	}
	proved, err := provePDNSPairSecondaryReconfigureRollbackWithOps(
		context.Background(), journal, fake.ops(),
	)
	if err != nil || !proved {
		t.Fatalf("safe rollback preimage proof=%v err=%v", proved, err)
	}
}

func TestFreshPDNSPairSecondaryRollbackKeepsNoAuthorityInvariant(t *testing.T) {
	journal, _ := testPDNSReconfigureRecoveryJournal(t, 109)
	journal.TargetUnitsBefore[0].ActiveState = "inactive"
	journal.TargetUnitsBefore[0].UnitFileState = "disabled"
	journal.PDNSBackupSHA256 = ""
	journal.PDNSBackupSize = 0
	pdnsCalls, noneCalls := 0, 0
	err := verifyRestoredEmptySourceAuthorityWithOps(
		context.Background(), journal,
		restoredEmptySourceAuthorityProofOps{
			verifyOnlyPDNS: func() error {
				pdnsCalls++
				return nil
			},
			verifyNoAuthority: func() error {
				noneCalls++
				return nil
			},
		},
	)
	if err != nil || pdnsCalls != 0 || noneCalls != 1 {
		t.Fatalf("fresh rollback err=%v pdns=%d none=%d", err, pdnsCalls, noneCalls)
	}
}

func TestPDNSReconfigureRollbackRejectsIncompleteJournalEvidence(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*dnsEngineSwitchJournal, *fakePDNSConfigFS)
	}{
		{
			name: "not-durably-rolling-back",
			mutate: func(journal *dnsEngineSwitchJournal, _ *fakePDNSConfigFS) {
				journal.Phase = dnsSwitchPhaseIntent
			},
		},
		{
			name: "target-was-not-enabled",
			mutate: func(journal *dnsEngineSwitchJournal, _ *fakePDNSConfigFS) {
				journal.TargetUnitsBefore[0].UnitFileState = "disabled"
			},
		},
		{
			name: "database-preimage-absent",
			mutate: func(journal *dnsEngineSwitchJournal, _ *fakePDNSConfigFS) {
				journal.PDNSBackupSHA256 = ""
				journal.PDNSBackupSize = 0
			},
		},
		{
			name: "database-path-differs",
			mutate: func(journal *dnsEngineSwitchJournal, _ *fakePDNSConfigFS) {
				journal.PDNSBackupPath += ".foreign"
			},
		},
		{
			name: "foreign-main-group",
			mutate: func(journal *dnsEngineSwitchJournal, fake *fakePDNSConfigFS) {
				main := testPDNSConfigSnapshot(
					dnsMainConf,
					"include-dir="+filepath.Clean(filepath.Dir(dnsManagedConf))+"\n",
					0o640, 0, 110,
				)
				replacePDNSReconfigureConfigSnapshot(journal, fake, main)
			},
		},
		{
			name: "main-does-not-load-managed-directory",
			mutate: func(journal *dnsEngineSwitchJournal, fake *fakePDNSConfigFS) {
				main := testPDNSConfigSnapshot(
					dnsMainConf, "# stock without include\n", 0o640, 0, 109,
				)
				replacePDNSReconfigureConfigSnapshot(journal, fake, main)
			},
		},
		{
			name: "managed-config-not-standalone",
			mutate: func(journal *dnsEngineSwitchJournal, fake *fakePDNSConfigFS) {
				managed := testPDNSConfigSnapshot(
					dnsManagedConf, "launch=bind\n", 0o644, 0, 0,
				)
				replacePDNSReconfigureConfigSnapshot(journal, fake, managed)
			},
		},
		{
			name: "cluster-config-was-present",
			mutate: func(journal *dnsEngineSwitchJournal, fake *fakePDNSConfigFS) {
				cluster := testPDNSConfigSnapshot(
					dnsClusterConf, "role=paired\n", 0o644, 0, 0,
				)
				replacePDNSReconfigureConfigSnapshot(journal, fake, cluster)
			},
		},
		{
			name: "restored-config-drifted-after-readback",
			mutate: func(_ *dnsEngineSwitchJournal, fake *fakePDNSConfigFS) {
				managed := testPDNSConfigSnapshot(
					dnsManagedConf, "# changed after restore\n", 0o644, 0, 0,
				)
				fake.state[filepath.Clean(dnsManagedConf)] =
					fake.nextObservation(managed)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			journal, fake := testPDNSReconfigureRecoveryJournal(t, 109)
			journal = clonePDNSReconfigureRecoveryJournal(journal)
			test.mutate(&journal, fake)
			proved, err := provePDNSPairSecondaryReconfigureRollbackWithOps(
				context.Background(), journal, fake.ops(),
			)
			if proved || err == nil {
				t.Fatalf("incomplete evidence proof=%v err=%v", proved, err)
			}
		})
	}
}

func TestBINDRecoveryMutationRejectsUnsafeMaskParentBeforeMutation(t *testing.T) {
	proofErr := errors.New("unsafe BIND mask parent")
	mutated := false
	err := runBINDMutationWithMaskParentProof(
		func() error { return proofErr },
		func() error {
			mutated = true
			return nil
		},
	)
	if !errors.Is(err, proofErr) || mutated {
		t.Fatalf("recovery proof error=%v mutated=%v", err, mutated)
	}
}

func TestBINDRecoveryMutationRunsOnlyAfterExactMaskParentProof(t *testing.T) {
	var order []string
	err := runBINDMutationWithMaskParentProof(
		func() error {
			order = append(order, "proof")
			return nil
		},
		func() error {
			order = append(order, "mutation")
			return nil
		},
	)
	if err != nil || !reflect.DeepEqual(order, []string{"proof", "mutation"}) {
		t.Fatalf("recovery order=%v err=%v", order, err)
	}
}

func TestDNSSwitchRollbackRequiresMaskParentProofForEitherBINDSide(t *testing.T) {
	for _, test := range []struct {
		name    string
		journal dnsEngineSwitchJournal
		proofs  int
	}{
		{
			name: "BIND target",
			journal: dnsEngineSwitchJournal{
				TargetEngine: transport.DNSEngineBIND,
			},
			proofs: 1,
		},
		{
			name: "BIND source",
			journal: dnsEngineSwitchJournal{
				SourceEngine: transport.DNSEngineBIND,
				TargetEngine: transport.DNSEnginePowerDNS,
			},
			proofs: 1,
		},
		{
			name: "standalone to PowerDNS switch",
			journal: dnsEngineSwitchJournal{
				Mode:         transport.DNSEngineSwitchModeSwitch,
				TargetEngine: transport.DNSEnginePowerDNS,
			},
			proofs: 1,
		},
		{
			name: "PowerDNS adoption",
			journal: dnsEngineSwitchJournal{
				Mode:         transport.DNSEngineSwitchModeAdopt,
				TargetEngine: transport.DNSEnginePowerDNS,
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			proofs := 0
			rollbacks := 0
			err := runDNSSwitchRollbackWithMaskParentProof(
				test.journal,
				func() error {
					proofs++
					return nil
				},
				func() error {
					rollbacks++
					return nil
				},
			)
			if err != nil || proofs != test.proofs || rollbacks != 1 {
				t.Fatalf(
					"proofs=%d rollbacks=%d err=%v, want proofs=%d rollback=1",
					proofs, rollbacks, err, test.proofs,
				)
			}
		})
	}
}

func TestDNSSwitchRollbackRejectsUnsafeSystemdParentForStandalonePDNSSwitch(t *testing.T) {
	proofErr := errors.New("unsafe systemd parent")
	rolledBack := false
	err := runDNSSwitchRollbackWithMaskParentProof(
		dnsEngineSwitchJournal{
			Mode:         transport.DNSEngineSwitchModeSwitch,
			TargetEngine: transport.DNSEnginePowerDNS,
		},
		func() error { return proofErr },
		func() error {
			rolledBack = true
			return nil
		},
	)
	if !errors.Is(err, proofErr) || rolledBack {
		t.Fatalf("error=%v rolledBack=%v", err, rolledBack)
	}
}

func TestDNSSwitchRollbackRejectsUnsafeMaskParentForBINDSource(t *testing.T) {
	proofErr := errors.New("unsafe mask parent")
	rolledBack := false
	err := runDNSSwitchRollbackWithMaskParentProof(
		dnsEngineSwitchJournal{
			SourceEngine: transport.DNSEngineBIND,
			TargetEngine: transport.DNSEnginePowerDNS,
		},
		func() error { return proofErr },
		func() error {
			rolledBack = true
			return nil
		},
	)
	if !errors.Is(err, proofErr) || rolledBack {
		t.Fatalf("error=%v rolledBack=%v", err, rolledBack)
	}
}

func TestDNSSwitchRecoveryRollbackRetainsJournalUntilVerified(t *testing.T) {
	proofErr := errors.New("restored authority proof failed")
	journal := dnsEngineSwitchJournal{Phase: dnsSwitchPhaseIntent}
	var order []string
	removed := false
	err := runDNSSwitchRecoveryRollbackWithJournal(
		&journal,
		dnsSwitchRecoveryRollbackOps{
			write: func(current dnsEngineSwitchJournal) error {
				order = append(order, "write:"+current.Phase)
				return nil
			},
			rollback: func(current dnsEngineSwitchJournal) error {
				order = append(order, "rollback:"+current.Phase)
				return proofErr
			},
			remove: func() error {
				removed = true
				order = append(order, "remove")
				return nil
			},
		},
	)
	want := []string{
		"write:" + dnsSwitchPhaseRollingBack,
		"rollback:" + dnsSwitchPhaseRollingBack,
	}
	if !errors.Is(err, proofErr) || !reflect.DeepEqual(order, want) ||
		removed || journal.Phase != dnsSwitchPhaseRollingBack {
		t.Fatalf(
			"failed proof did not retain rolling journal: phase=%q order=%v removed=%v err=%v",
			journal.Phase, order, removed, err,
		)
	}
}

func TestDNSSwitchRecoveryRollbackRemovesOnlyAfterFinalPhase(t *testing.T) {
	journal := dnsEngineSwitchJournal{Phase: dnsSwitchPhaseIntent}
	var order []string
	err := runDNSSwitchRecoveryRollbackWithJournal(
		&journal,
		dnsSwitchRecoveryRollbackOps{
			write: func(current dnsEngineSwitchJournal) error {
				order = append(order, "write:"+current.Phase)
				return nil
			},
			rollback: func(current dnsEngineSwitchJournal) error {
				order = append(order, "rollback:"+current.Phase)
				return nil
			},
			remove: func() error {
				order = append(order, "remove")
				return nil
			},
		},
	)
	want := []string{
		"write:" + dnsSwitchPhaseRollingBack,
		"rollback:" + dnsSwitchPhaseRollingBack,
		"write:" + dnsSwitchPhaseRolledBack,
		"remove",
	}
	if err != nil || !reflect.DeepEqual(order, want) ||
		journal.Phase != dnsSwitchPhaseRolledBack {
		t.Fatalf("successful recovery phase=%q order=%v err=%v", journal.Phase, order, err)
	}
}
