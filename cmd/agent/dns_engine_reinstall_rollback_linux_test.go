//go:build linux

package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

// An interrupted reinstall's rollback returns the host to exactly the state the
// reinstall started from: the recorded authority still names BIND at epoch N,
// and BIND is not serving. Before this, the terminal proof was
// verifyOnlyBINDActive - it required the very BIND that the reinstall exists
// to bring back - so every reinstall rollback, at any cut before the target
// was verified, ended with its journal retained at rolling-back and every
// later DNS change blocked.
//
// Yarıda kalan bir yeniden kurulumun geri alınması sunucuyu tam başladığı
// duruma döndürür: kayıtlı yetki hâlâ N çağında BIND'i adlandırır ve BIND
// hizmet vermez. Önceden son kanıt, yeniden kurulumun geri getirmek için var
// olduğu BIND'in çalışmasını istiyordu.
func TestReinstallRollbackProofIsThePreOperationState(t *testing.T) {
	for _, phase := range []string{
		dnsSwitchPhaseIntent,        // cut before the target started
		dnsSwitchPhaseTargetStaged,  // cut before the target started
		dnsSwitchPhaseSourceStopped, // cut before the target started
		dnsSwitchPhaseTargetStarted, // cut after the target started; the inverse stopped it
		dnsSwitchPhaseRollingBack,
	} {
		t.Run(phase, func(t *testing.T) {
			prepareDNSEngineOwnershipTest(t)
			source := reinstallSourceStateForTest(t, strings.Repeat("d", 64))
			journal := reinstallJournalForTest(t, source)
			journal.Phase = phase
			if !dnsSwitchJournalReinstallsAbsentEngine(journal) {
				t.Fatal("the reinstall journal fixture is not recognised")
			}
			authorityProofs := 0
			ops := restoredReinstallSourceProofOps{
				readState: readDNSEngineState,
				verifyNoAuthority: func() error {
					authorityProofs++
					return nil
				},
			}
			if err := verifyRestoredReinstallSourceWithOps(journal, ops); err != nil {
				t.Fatalf("the exact pre-operation state was refused: %v", err)
			}
			if authorityProofs != 1 {
				t.Fatalf("no-authority proofs = %d, want 1", authorityProofs)
			}

			// The target started and the inverse could not stop it: BIND is
			// still serving, which is not the state the reinstall began in.
			ops.verifyNoAuthority = func() error {
				return errors.New("DNS rollback did not prove every managed authority inactive")
			}
			if err := verifyRestoredReinstallSourceWithOps(journal, ops); err == nil {
				t.Fatal("a still-serving target was accepted as the restored state")
			}
			ops.verifyNoAuthority = func() error { return nil }

			// The reinstall's own receipt (it reached its state write) is not
			// the pre-operation authority.
			advanced := source
			advanced.MutationRequestID = strings.Repeat("7", 32)
			advanced.Generation = strings.Repeat("8", 64)
			if err := writeDNSEngineState(advanced); err != nil {
				t.Fatal(err)
			}
			if err := verifyRestoredReinstallSourceWithOps(journal, ops); err == nil ||
				!strings.Contains(err.Error(), "differs from the recorded pre-operation authority") {
				t.Fatalf("an advanced state receipt was accepted: %v", err)
			}
			if err := os.Remove(dnsEngineStatePath()); err != nil {
				t.Fatal(err)
			}
			if err := verifyRestoredReinstallSourceWithOps(journal, ops); err == nil {
				t.Fatal("a missing state receipt was accepted")
			}
		})
	}
}

func TestReinstallRollbackUsesTheNeverServedStoppedProof(t *testing.T) {
	prepareDNSEngineOwnershipTest(t)
	journal := reinstallJournalForTest(t, reinstallSourceStateForTest(t, strings.Repeat("d", 64)))
	if !dnsSwitchJournalTargetDidNotServeBefore(journal) {
		t.Fatal("a reinstall journal did not select the never-served stopped proof")
	}
	for name, other := range map[string]dnsEngineSwitchJournal{
		"pdns-to-bind-switch": {
			Mode: transport.DNSEngineSwitchModeSwitch, SourceEngine: transport.DNSEnginePowerDNS,
			TargetEngine: transport.DNSEngineBIND, SourceEpoch: 1, TargetEpoch: 2,
		},
		"same-engine-other-mode": {
			Mode: transport.DNSEngineSwitchModeSwitch, SourceEngine: transport.DNSEngineBIND,
			TargetEngine: transport.DNSEngineBIND, SourceEpoch: 1, TargetEpoch: 1,
		},
		"reinstall-without-epoch": {
			Mode: transport.DNSEngineSwitchModeReinstall, SourceEngine: transport.DNSEngineBIND,
			TargetEngine: transport.DNSEngineBIND,
		},
	} {
		if dnsSwitchJournalReinstallsAbsentEngine(other) {
			t.Fatalf("%s was classified as a reinstall", name)
		}
		if name != "reinstall-without-epoch" && dnsSwitchJournalTargetDidNotServeBefore(other) {
			t.Fatalf("%s selected the never-served stopped proof", name)
		}
	}
	if err := verifyRestoredReinstallSourceWithOps(
		dnsEngineSwitchJournal{Mode: transport.DNSEngineSwitchModeSwitch},
		restoredReinstallSourceProofOps{
			readState:         readDNSEngineState,
			verifyNoAuthority: func() error { return nil },
		},
	); err == nil {
		t.Fatal("the reinstall proof accepted a non-reinstall journal")
	}

	// A retry of a reinstall whose earlier attempt installed bind9 and aborted
	// before its intent froze both units under the package guard's persistent
	// mask. Cut before the target started: the restore re-masks, and the
	// stopped proof must accept it (the loaded-unit proof refused it with
	// "DNS target is not a loaded unit"). Cut after the target started: the
	// restore stops and re-masks; a unit still running is refused.
	dead := func(context.Context) (dnsUnitProcesses, error) { return dnsUnitProcesses{SubState: "dead"}, nil }
	emptyCgroup := func(context.Context) error { return nil }
	noListener := func(context.Context) error { return nil }
	for _, unit := range []bindInstallUnitState{
		{name: "named.service", loadState: "masked", activeState: "inactive", unitFileState: "masked"},
		{name: "named.service", loadState: "not-found", activeState: "inactive"},
		{name: "named.service", loadState: "loaded", activeState: "inactive", unitFileState: "disabled"},
	} {
		if err := verifyBINDTargetStoppedBeforeConfigRestoreForSourceWithOps(
			context.Background(), dnsSwitchJournalTargetDidNotServeBefore(journal),
			func(context.Context) (bindInstallUnitState, error) { return unit, nil },
			dead, emptyCgroup, noListener,
		); err != nil {
			t.Fatalf("reinstall stopped proof refused %s: %v", unit.loadState, err)
		}
	}
	running := bindInstallUnitState{name: "named.service", loadState: "loaded", activeState: "active", unitFileState: "enabled"}
	if err := verifyBINDTargetStoppedBeforeConfigRestoreForSourceWithOps(
		context.Background(), true,
		func(context.Context) (bindInstallUnitState, error) { return running, nil },
		func(context.Context) (dnsUnitProcesses, error) {
			return dnsUnitProcesses{MainPID: 42, SubState: "running"}, nil
		},
		emptyCgroup, noListener,
	); err == nil {
		t.Fatal("a running reinstall target passed the stopped proof")
	}
}

// Both terminal proofs route by journal before any engine-specific check: the
// reinstall is decided before verifyOnlyBINDActive can be reached.
func TestRestoredSwitchSourceRoutesReinstallFirst(t *testing.T) {
	raw, err := os.ReadFile("dns_engine_catalog_handoff.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	start := strings.Index(source, "func verifyRestoredDNSSwitchSource(")
	if start < 0 {
		t.Fatal("verifyRestoredDNSSwitchSource is missing")
	}
	body := source[start:]
	if end := strings.Index(body[1:], "\nfunc "); end > 0 {
		body = body[:end+1]
	}
	reinstall := strings.Index(body, "dnsSwitchJournalReinstallsAbsentEngine(journal)")
	active := strings.Index(body, "verifyOnlyBINDActive(")
	if reinstall < 0 || active < 0 || reinstall > active {
		t.Fatalf("reinstall routing=%d BIND active proof=%d", reinstall, active)
	}
	for _, file := range []string{"dns_engine_host.go", "dns_engine_recovery.go"} {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "dnsSwitchJournalHasEmptySource(journal),\n\t\t\t\t\tproveSource") ||
			!strings.Contains(string(raw), "dnsSwitchJournalTargetDidNotServeBefore(journal)") {
			t.Fatalf("%s does not select the stopped proof class through the reinstall-aware predicate", file)
		}
	}
}

// A retry that fails before writing its own install receipt meets the receipt
// an earlier attempt at the same reinstall left behind. That is residue, not
// ambiguity; a receipt for another manifest still fails closed.
func TestReinstallAbortAcceptsEarlierAttemptResidueOfTheSameManifest(t *testing.T) {
	state, _ := stageAbsentActiveDNSEngine(t)
	if err := writeDNSEngineOwnership(state); err != nil {
		t.Fatal(err)
	}
	if err := writeDNSEngineInstallOwnership(dnsEngineInstallOwnershipReceipt{
		Schema:            dnsEngineInstallOwnershipSchema,
		Engine:            transport.DNSEngineBIND,
		PackageManager:    "apt",
		Packages:          []string{"bind9"},
		MissingBefore:     []string{"bind9"},
		ManifestQualifier: residueQualifier,
		MutationRequestID: residueRequestID,
		MutationOwnerID:   residueOwnerID,
	}); err != nil {
		t.Fatal(err)
	}
	retry := transport.ServiceMutationBinding{
		MutationRequestID: strings.Repeat("5", 32),
		MutationOwnerID:   strings.Repeat("6", 32),
	}
	finalized, err := exactFinalizedDNSEngineSwitchProvenanceOnHost(
		transport.DNSEngineBIND, residueQualifier, retry,
	)
	if err != nil || finalized {
		t.Fatalf("an earlier attempt's receipt poisoned the retry's abort: finalized=%v err=%v", finalized, err)
	}
	if _, err := exactFinalizedDNSEngineSwitchProvenanceOnHost(
		transport.DNSEngineBIND,
		"dns-engine-switch/v1:sha256:"+strings.Repeat("a", 64), retry,
	); err == nil {
		t.Fatal("a receipt for another manifest was dismissed")
	}
}
