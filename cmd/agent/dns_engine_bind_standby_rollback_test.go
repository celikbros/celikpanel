package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
)

// Component tests for the Agent's in-process V2 PowerDNS-to-BIND rollback end
// state; systemd is the install-guard fake. They are not native evidence.

func standbyTestGuard(systemd *fakeBINDInstallSystemd) *bindPackageInstallGuard {
	recoveries := 0
	return &bindPackageInstallGuard{
		systemctl: "/usr/bin/systemctl",
		ops:       fakeBINDInstallGuardOps(systemd, &recoveries),
		ownedMask: map[string]bool{},
	}
}

func standbyTestOps(guard *bindPackageInstallGuard, maskProofs *int) bindStandbyTargetOps {
	ops := hostBINDStandbyTargetOps(guard)
	ops.verifyMasks = func() error { *maskProofs++; return nil }
	return ops
}

func TestAgentBINDStandbyRollbackSealsStartedTargetLikeOwnerCommand(t *testing.T) {
	systemd := newFakeBINDInstallSystemd(map[string]*fakeBINDInstallUnit{
		"bind9.service": {loadState: "loaded", unitFileState: "enabled", active: true},
		"named.service": {loadState: "loaded", unitFileState: "enabled", active: true},
	})
	guard := standbyTestGuard(systemd)
	proofs := 0
	if err := restoreBINDTargetToStandbyWithOps(context.Background(), standbyTestOps(guard, &proofs)); err != nil {
		t.Fatalf("started target not sealed: %v", err)
	}
	for name, unit := range systemd.units {
		if !unit.masked || unit.runtimeMasked || unit.active {
			t.Fatalf("%s is not under the guard's persistent mask: %+v", name, unit)
		}
	}
	disable, mask := commandIndex(systemd.commands, "disable named.service"), commandIndex(systemd.commands, "mask named.service")
	if disable < 0 || mask < 0 || disable > mask || commandIndex(systemd.commands, "stop named.service") > disable {
		t.Fatalf("target was not stopped and disabled before masking: %v", systemd.commands)
	}
	for _, command := range systemd.commands {
		if strings.HasPrefix(command, "start ") || strings.HasPrefix(command, "enable ") {
			t.Fatalf("rollback started or enabled BIND: %v", systemd.commands)
		}
	}
	if proofs != 1 {
		t.Fatalf("mask files proved %d times", proofs)
	}

	// Same switch again: the sealed end state is exactly the pair the
	// forward admission accepts as sealed, and the package guard adds no mask
	// of its own over it.
	named, _ := guard.inspect(context.Background(), "named.service")
	alias, _ := guard.inspect(context.Background(), "bind9.service")
	if sealed, err := classifyBINDTargetNotServingStates(named, alias); err != nil || !sealed {
		t.Fatalf("forward admission refused the rollback standby: sealed=%v err=%v", sealed, err)
	}
	retry, err := beginBINDPackageInstallGuard(context.Background(), "/usr/bin/systemctl", guard.ops)
	if err != nil || len(retry.ownedMask) != 0 {
		t.Fatalf("retry guard did not accept the existing masks: owned=%v err=%v", retry.ownedMask, err)
	}

	// Rolling back that retry (the journal froze masked/masked) from a
	// started target ends in the same state again.
	for _, unit := range systemd.units {
		unit.masked, unit.active, unit.unitFileState = false, true, "enabled"
	}
	if err := restoreBINDTargetToStandbyWithOps(context.Background(), standbyTestOps(guard, &proofs)); err != nil {
		t.Fatalf("retry rollback not sealed: %v", err)
	}
	for name, unit := range systemd.units {
		if !unit.masked || unit.active {
			t.Fatalf("%s not sealed after the retry rollback: %+v", name, unit)
		}
	}
}

func TestAgentBINDStandbyRollbackLeavesSealedOrAbsentTargetUntouched(t *testing.T) {
	for name, units := range map[string]map[string]*fakeBINDInstallUnit{
		"sealed": {
			"bind9.service": {loadState: "loaded", unitFileState: "disabled", masked: true},
			"named.service": {loadState: "loaded", unitFileState: "disabled", masked: true},
		},
		"absent": {
			"bind9.service": {loadState: "not-found"},
			"named.service": {loadState: "not-found"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			systemd := newFakeBINDInstallSystemd(units)
			proofs := 0
			if err := restoreBINDTargetToStandbyWithOps(context.Background(), standbyTestOps(standbyTestGuard(systemd), &proofs)); err != nil {
				t.Fatal(err)
			}
			for _, command := range systemd.commands {
				if !strings.HasPrefix(command, "show ") {
					t.Fatalf("%s target received %q", name, command)
				}
			}
			if (name == "sealed") != (proofs == 1) {
				t.Fatalf("mask proof count %d for %s", proofs, name)
			}
		})
	}
	// A runtime-only mask is not the standby state and a failed mask proof
	// fails the rollback before any config is touched.
	systemd := newFakeBINDInstallSystemd(map[string]*fakeBINDInstallUnit{
		"bind9.service": {loadState: "loaded", unitFileState: "disabled", masked: true},
		"named.service": {loadState: "loaded", unitFileState: "disabled", masked: true},
	})
	ops := hostBINDStandbyTargetOps(standbyTestGuard(systemd))
	ops.verifyMasks = func() error { return errors.New("mask link is not root-owned") }
	if err := restoreBINDTargetToStandbyWithOps(context.Background(), ops); err == nil {
		t.Fatal("unproved mask accepted")
	}
}

func TestAgentBINDStandbySourceOnlyProof(t *testing.T) {
	unit := bindInstallUnitState{name: "pdns.service", loadState: "loaded", activeState: "active", unitFileState: "enabled"}
	var authorityPID uint64
	ops := bindStandbySourceOnlyOps{
		noNamedProcess: func(context.Context) error { return nil },
		sourceUnit:     func(context.Context) (bindInstallUnitState, error) { return unit, nil },
		sourcePID:      func(context.Context) (uint64, error) { return 3066, nil },
		authority:      func(_ context.Context, pid uint64) error { authorityPID = pid; return nil },
		noListener:     func(context.Context) error { t.Fatal("no-listener proof used for an active source"); return nil },
	}
	if err := proveBINDStandbySourceOnlyDNSWithOps(context.Background(), ops); err != nil || authorityPID != 3066 {
		t.Fatalf("active source: %v pid=%d", err, authorityPID)
	}
	ops.authority = func(context.Context, uint64) error {
		return errors.New("a named process (PID 7461) holds local port-53 listener 127.0.0.1 outside the verified DNS source")
	}
	if err := proveBINDStandbySourceOnlyDNSWithOps(context.Background(), ops); err == nil {
		t.Fatal("named loopback listener beside an active source accepted")
	}
	unit.activeState = "inactive"
	listenerCalls := 0
	ops.noListener = func(context.Context) error { listenerCalls++; return nil }
	if err := proveBINDStandbySourceOnlyDNSWithOps(context.Background(), ops); err != nil || listenerCalls != 1 {
		t.Fatalf("stopped source: %v calls=%d", err, listenerCalls)
	}
	ops.noNamedProcess = func(context.Context) error { return errors.New("a named process (PID 4242) exists") }
	if err := proveBINDStandbySourceOnlyDNSWithOps(context.Background(), ops); err == nil {
		t.Fatal("named process accepted")
	}
}

func TestAgentBINDStandbyResidueRemovesOnlyExactGeneration(t *testing.T) {
	journal := dnsEngineSwitchJournal{MutationRequestID: strings.Repeat("a", 32), TargetGeneration: strings.Repeat("c", 64)}
	for _, tc := range []struct {
		state      dnsenginerecovery.BINDGenerationResidue
		classErr   error
		wantRemove bool
	}{
		{dnsenginerecovery.BINDGenerationStaged, nil, true},
		{dnsenginerecovery.BINDGenerationRetained, nil, false},
		{dnsenginerecovery.BINDGenerationNone, nil, false},
		{dnsenginerecovery.BINDGenerationStaged, errors.New("catalog changed"), false},
	} {
		removed := false
		logBINDStandbyResidue(journal,
			func() (dnsenginerecovery.BINDGenerationResidue, string, error) {
				return tc.state, "owner file", tc.classErr
			},
			func() (bool, error) { removed = true; return true, nil },
			func() ([]string, error) { return nil, nil })
		if removed != tc.wantRemove {
			t.Fatalf("state %v err %v: removed=%v", tc.state, tc.classErr, removed)
		}
	}
}
