package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/hostplatform"
	"github.com/alicelik/celikpanel/internal/transport"
)

func testDebian13PDNSProfile() hostplatform.Profile {
	return hostplatform.Profile{
		DistroFamily:   hostplatform.DistroFamilyDebian,
		PackageManager: hostplatform.PackageManagerAPT,
		ID:             "debian", Version: "13", Codename: "trixie",
	}
}

func TestPDNSTargetProfileCertificationPrecedesMutation(t *testing.T) {
	for _, profile := range []hostplatform.Profile{
		testUbuntuBINDProfile(),
		{DistroFamily: hostplatform.DistroFamilyArch, PackageManager: hostplatform.PackageManagerPacman, ID: "arch"},
		{DistroFamily: hostplatform.DistroFamilyDebian, PackageManager: hostplatform.PackageManagerAPT, ID: "debian", Version: "12", Codename: "bookworm"},
	} {
		mutations := 0
		_, err := runCertifiedPDNSTargetMutation(
			profile,
			func() (transport.SwitchDNSEngineV1Response, error) {
				mutations++
				return transport.SwitchDNSEngineV1Response{}, nil
			},
		)
		if err == nil {
			t.Fatalf("uncertified PowerDNS profile was accepted: %#v", profile)
		}
		if mutations != 0 {
			t.Fatalf("uncertified PowerDNS profile reached mutation: %#v", profile)
		}
	}
	mutations := 0
	if _, err := runCertifiedPDNSTargetMutation(
		testDebian13PDNSProfile(),
		func() (transport.SwitchDNSEngineV1Response, error) {
			mutations++
			return transport.SwitchDNSEngineV1Response{}, nil
		},
	); err != nil {
		t.Fatal(err)
	}
	if mutations != 1 {
		t.Fatalf("certified Debian 13 mutation count = %d", mutations)
	}
}

func TestPDNSSwitchAndAdoptionRollbackPreserveCallerDeadline(t *testing.T) {
	t.Run("switch-systemctl", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(
			context.Background(), 25*time.Millisecond,
		)
		defer cancel()
		started := time.Now()
		err := rollbackPDNSSwitchWithOps(ctx, pdnsSwitchRollbackOps{
			stopTarget: func(commandCtx context.Context) error {
				<-commandCtx.Done()
				return commandCtx.Err()
			},
			restorePDNSDatabaseSnapshot: func() error { return nil },
			restoreConfigs:              func() error { return nil },
			restoreState:                func() error { return nil },
			restoreTarget: func(commandCtx context.Context) error {
				return commandCtx.Err()
			},
			restoreSource: func(commandCtx context.Context) error {
				return commandCtx.Err()
			},
		})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("PowerDNS switch rollback lost deadline: %v", err)
		}
		if elapsed := time.Since(started); elapsed > time.Second {
			t.Fatalf("PowerDNS switch rollback escaped deadline: %s", elapsed)
		}
	})
	t.Run("adoption-evidence", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(
			context.Background(), 25*time.Millisecond,
		)
		defer cancel()
		started := time.Now()
		err := rollbackPDNSAdoptionWithOps(
			ctx,
			func() error { return nil },
			func(commandCtx context.Context) error {
				<-commandCtx.Done()
				return commandCtx.Err()
			},
		)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("PowerDNS adoption rollback lost deadline: %v", err)
		}
		if elapsed := time.Since(started); elapsed > time.Second {
			t.Fatalf("PowerDNS adoption rollback escaped deadline: %s", elapsed)
		}
	})
}

func TestDNSRollbackContextDetachesCancellationButRemainsBounded(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	cancelParent()
	recovery, cancel, err := newDNSEngineRollbackContext(parent)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	if recovery.Err() != nil {
		t.Fatalf("recovery context inherited cancellation: %v", recovery.Err())
	}
	deadline, ok := recovery.Deadline()
	if !ok || time.Until(deadline) <= 0 ||
		time.Until(deadline) > dnsEngineSwitchRecoveryLimit+time.Second {
		t.Fatalf("recovery context has no exact bound: %v %v", deadline, ok)
	}
}

func canonicalDebian13PDNSIdentity() dnsUnitIdentity {
	return dnsUnitIdentity{
		ID: "pdns.service", Names: []string{"pdns.service"},
		FragmentPath: certifiedPDNSUnitPath, Transient: "no",
		ExecStartPath: "/usr/sbin/pdns_server",
		ExecStartArgv: certifiedPDNSExecArgv,
	}
}

func TestCertifiedDebian13PDNSVendorFixtureMatchesLivePackage(t *testing.T) {
	bytes := []byte(certifiedDebian13PDNSVendorUnit)
	wantDigest := [32]byte{
		0x60, 0xde, 0x4d, 0x7e, 0x6c, 0xcc, 0x04, 0x52,
		0x03, 0x20, 0xe4, 0x73, 0x6f, 0xe3, 0xdf, 0x6e,
		0x03, 0xd1, 0xce, 0xeb, 0x23, 0x18, 0x40, 0x83,
		0xa4, 0xed, 0x9e, 0x60, 0x4e, 0x4a, 0xde, 0xa4,
	}
	if len(bytes) != 1579 || sha256.Sum256(bytes) != wantDigest {
		t.Fatalf("certified PowerDNS unit length/digest drifted: %d %x", len(bytes), sha256.Sum256(bytes))
	}
	if err := validatePDNSVendorUnitIdentity(canonicalDebian13PDNSIdentity()); err != nil {
		t.Fatal(err)
	}
}

func canonicalPDNSInactiveTarget() pdnsInactiveTargetSnapshot {
	return pdnsInactiveTargetSnapshot{
		state: bindInstallUnitState{
			name: "pdns.service", loadState: "loaded",
			activeState: "inactive", unitFileState: "disabled",
		},
		processes:  dnsUnitProcesses{SubState: "dead"},
		identity:   canonicalDebian13PDNSIdentity(),
		vendorUnit: bindSecureFileIdentity{Device: 1, Inode: 9, Size: 1579},
	}
}

func TestVerifiedPDNSInactiveTargetRejectsTamperAndTOCTOU(t *testing.T) {
	profile := testDebian13PDNSProfile()
	canonical := canonicalPDNSInactiveTarget()
	newOps := func() pdnsInactiveTargetOps {
		return pdnsInactiveTargetOps{
			inspectState: func() (bindInstallUnitState, error) {
				return canonical.state, nil
			},
			inspectIdentity: func() (dnsUnitIdentity, error) {
				return canonical.identity, nil
			},
			inspectVendor: func() (bindSecureFileIdentity, error) {
				return canonical.vendorUnit, nil
			},
			inspectProcesses: func() (dnsUnitProcesses, error) {
				return canonical.processes, nil
			},
		}
	}
	if got, err := inspectVerifiedPDNSInactiveTargetWithOps(
		profile, []string{"disabled"}, newOps(),
	); err != nil || !reflect.DeepEqual(got, canonical) {
		t.Fatalf("inactive target got=%+v err=%v", got, err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*pdnsInactiveTargetOps)
	}{
		{name: "active", mutate: func(ops *pdnsInactiveTargetOps) {
			ops.inspectState = func() (bindInstallUnitState, error) {
				state := canonical.state
				state.activeState = "active"
				return state, nil
			}
		}},
		{name: "drop-in", mutate: func(ops *pdnsInactiveTargetOps) {
			ops.inspectIdentity = func() (dnsUnitIdentity, error) {
				identity := canonical.identity
				identity.DropInPaths = []string{
					"/etc/systemd/system/pdns.service.d/evil.conf",
				}
				return identity, nil
			}
		}},
		{name: "control-pid", mutate: func(ops *pdnsInactiveTargetOps) {
			ops.inspectProcesses = func() (dnsUnitProcesses, error) {
				return dnsUnitProcesses{
					ControlPID: 19, SubState: "stop-sigterm",
				}, nil
			}
		}},
		{name: "vendor-toctou", mutate: func(ops *pdnsInactiveTargetOps) {
			calls := 0
			ops.inspectVendor = func() (bindSecureFileIdentity, error) {
				calls++
				identity := canonical.vendorUnit
				if calls > 1 {
					identity.Inode++
				}
				return identity, nil
			}
		}},
		{name: "state-toctou", mutate: func(ops *pdnsInactiveTargetOps) {
			calls := 0
			ops.inspectState = func() (bindInstallUnitState, error) {
				calls++
				state := canonical.state
				if calls > 1 {
					state.unitFileState = "enabled"
				}
				return state, nil
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			ops := newOps()
			test.mutate(&ops)
			if _, err := inspectVerifiedPDNSInactiveTargetWithOps(
				profile, []string{"disabled", "enabled"}, ops,
			); err == nil {
				t.Fatal("unsafe inactive PowerDNS target was accepted")
			}
		})
	}
}

func TestPDNSTargetActivationNeverStartsBeforeExactInactiveReproof(t *testing.T) {
	run := func(initial string, failProof int) ([]string, error) {
		var commands []string
		unitFileState := initial
		proofs := 0
		err := startPDNSTargetWithOps(
			context.Background(),
			pdnsTargetActivationOps{
				verifySealed: func(context.Context) error {
					commands = append(commands, "sealed")
					return nil
				},
				unmask: func(context.Context) error {
					commands = append(commands, "unmask")
					return nil
				},
				daemonReload: func(context.Context) error {
					commands = append(commands, "reload")
					return nil
				},
				inspectStopped: func(
					_ context.Context, allowed ...string,
				) (pdnsInactiveTargetSnapshot, error) {
					proofs++
					commands = append(commands, "proof-"+unitFileState)
					if proofs == failProof {
						return pdnsInactiveTargetSnapshot{},
							errors.New("identity drift")
					}
					if !containsExactString(allowed, unitFileState) {
						return pdnsInactiveTargetSnapshot{},
							errors.New("unexpected state contract")
					}
					snapshot := canonicalPDNSInactiveTarget()
					snapshot.state.unitFileState = unitFileState
					return snapshot, nil
				},
				enable: func(context.Context) error {
					commands = append(commands, "enable")
					unitFileState = "enabled"
					return nil
				},
				start: func(context.Context) error {
					commands = append(commands, "start")
					return nil
				},
			},
		)
		return commands, err
	}
	disabledCommands, err := run("disabled", 0)
	if err != nil {
		t.Fatal(err)
	}
	wantDisabled := []string{
		"sealed", "unmask", "reload", "proof-disabled",
		"enable", "reload", "proof-enabled", "start",
	}
	if !reflect.DeepEqual(disabledCommands, wantDisabled) {
		t.Fatalf("disabled activation order=%v want=%v", disabledCommands, wantDisabled)
	}
	enabledCommands, err := run("enabled", 0)
	if err != nil {
		t.Fatal(err)
	}
	wantEnabled := []string{
		"sealed", "unmask", "reload", "proof-enabled", "proof-enabled", "start",
	}
	if !reflect.DeepEqual(enabledCommands, wantEnabled) {
		t.Fatalf("enabled activation order=%v want=%v", enabledCommands, wantEnabled)
	}
	for _, failProof := range []int{1, 2} {
		commands, err := run("disabled", failProof)
		if err == nil {
			t.Fatal("PowerDNS activation accepted a failed vendor proof")
		}
		if containsExactString(commands, "start") {
			t.Fatalf("PowerDNS started after failed proof: %v", commands)
		}
	}
}

func containsExactString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func canonicalPDNSRuntimeTopology() pdnsRuntimeTopologySnapshot {
	stopped := dnsUnitProcesses{SubState: "dead"}
	return pdnsRuntimeTopologySnapshot{
		namedState: bindInstallUnitState{
			loadState: "not-found", activeState: "inactive",
		},
		aliasState: bindInstallUnitState{
			loadState: "not-found", activeState: "inactive",
		},
		pdnsState: bindInstallUnitState{
			loadState: "loaded", activeState: "active", unitFileState: "enabled",
		},
		namedProcesses: stopped, aliasProcesses: stopped,
		pdnsProcesses: dnsUnitProcesses{MainPID: 1003, SubState: "running"},
		pdnsIdentity:  canonicalDebian13PDNSIdentity(),
		vendorUnit:    bindSecureFileIdentity{Device: 1, Inode: 2, Size: 1579},
	}
}

func TestVerifyOnlyPDNSActiveUsesStableMainPIDBoundListenerProof(t *testing.T) {
	profile := testDebian13PDNSProfile()
	topology := canonicalPDNSRuntimeTopology()
	first := strings.Join([]string{
		`udp UNCONN 0 0 2.25.80.4:53 0.0.0.0:* users:(("pdns_server",pid=1003,fd=4))`,
		`tcp LISTEN 0 4096 [2001:db8::10]:53 [::]:* users:(("pdns_server",pid=1003,fd=5))`,
	}, "\n")
	second := strings.Join([]string{
		`tcp LISTEN 12 4096 [2001:db8::10]:53 [::]:* users:(("pdns_server",pid=1003,fd=9))`,
		`udp UNCONN 7 0 2.25.80.4:53 0.0.0.0:* users:(("pdns_server",pid=1003,fd=0))`,
	}, "\n")
	newOps := func() pdnsActiveProofOps {
		listenerCalls := 0
		return pdnsActiveProofOps{
			inspectTopology: func() (pdnsRuntimeTopologySnapshot, error) {
				return topology, nil
			},
			inspectListeners: func() (string, error) {
				listenerCalls++
				if listenerCalls == 1 {
					return first, nil
				}
				return second, nil
			},
		}
	}
	if err := verifyOnlyPDNSActiveWithOps(profile, newOps()); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*pdnsActiveProofOps)
	}{
		{name: "wrong-listener-pid", mutate: func(ops *pdnsActiveProofOps) {
			ops.inspectListeners = func() (string, error) {
				return strings.ReplaceAll(first, "pid=1003", "pid=1004"), nil
			}
		}},
		{name: "process-spoof", mutate: func(ops *pdnsActiveProofOps) {
			ops.inspectListeners = func() (string, error) {
				return strings.ReplaceAll(first, "pdns_server", "named"), nil
			}
		}},
		{name: "malformed-extra-owner", mutate: func(ops *pdnsActiveProofOps) {
			ops.inspectListeners = func() (string, error) {
				return first + "\n" + `udp UNCONN 0 0 192.0.2.9:53 0.0.0.0:* users:(("evil",pid=1,fd=2)) users:(("pdns_server",pid=1003,fd=4))`, nil
			}
		}},
		{name: "listener-drift", mutate: func(ops *pdnsActiveProofOps) {
			calls := 0
			ops.inspectListeners = func() (string, error) {
				calls++
				if calls == 1 {
					return first, nil
				}
				return strings.Replace(second, "2.25.80.4", "192.0.2.44", 1), nil
			}
		}},
		{name: "topology-drift", mutate: func(ops *pdnsActiveProofOps) {
			calls := 0
			ops.inspectTopology = func() (pdnsRuntimeTopologySnapshot, error) {
				calls++
				candidate := topology
				if calls > 1 {
					candidate.pdnsProcesses.MainPID++
				}
				return candidate, nil
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			ops := newOps()
			test.mutate(&ops)
			if err := verifyOnlyPDNSActiveWithOps(profile, ops); err == nil {
				t.Fatal("unsafe PowerDNS active proof was accepted")
			}
		})
	}
}

func TestVerifiedPDNSRuntimeTopologyRejectsStateIdentityProcessAndTOCTOU(t *testing.T) {
	profile := testDebian13PDNSProfile()
	canonical := canonicalPDNSRuntimeTopology()
	newOps := func() pdnsRuntimeTopologyOps {
		return pdnsRuntimeTopologyOps{
			inspectStates: func() (bindInstallUnitState, bindInstallUnitState, bindInstallUnitState, error) {
				return canonical.namedState, canonical.aliasState, canonical.pdnsState, nil
			},
			inspectIdentity: func() (dnsUnitIdentity, error) {
				return canonical.pdnsIdentity, nil
			},
			inspectVendor: func() (bindSecureFileIdentity, error) {
				return canonical.vendorUnit, nil
			},
			inspectProcesses: func(unit string) (dnsUnitProcesses, error) {
				switch unit {
				case "named.service":
					return canonical.namedProcesses, nil
				case "bind9.service":
					return canonical.aliasProcesses, nil
				case "pdns.service":
					return canonical.pdnsProcesses, nil
				default:
					return dnsUnitProcesses{}, errors.New("unexpected unit")
				}
			},
		}
	}
	if got, err := inspectVerifiedPDNSRuntimeTopologyWithOps(profile, newOps()); err != nil ||
		!reflect.DeepEqual(got, canonical) {
		t.Fatalf("exact PowerDNS topology got=%+v err=%v", got, err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*pdnsRuntimeTopologyOps)
	}{
		{name: "pdns-inactive", mutate: func(ops *pdnsRuntimeTopologyOps) {
			ops.inspectStates = func() (bindInstallUnitState, bindInstallUnitState, bindInstallUnitState, error) {
				pdns := canonical.pdnsState
				pdns.activeState = "inactive"
				return canonical.namedState, canonical.aliasState, pdns, nil
			}
		}},
		{name: "bind-active", mutate: func(ops *pdnsRuntimeTopologyOps) {
			ops.inspectStates = func() (bindInstallUnitState, bindInstallUnitState, bindInstallUnitState, error) {
				named := canonical.namedState
				named.loadState, named.activeState, named.unitFileState = "loaded", "active", "enabled"
				return named, canonical.aliasState, canonical.pdnsState, nil
			}
		}},
		{name: "identity-spoof", mutate: func(ops *pdnsRuntimeTopologyOps) {
			ops.inspectIdentity = func() (dnsUnitIdentity, error) {
				identity := canonical.pdnsIdentity
				identity.DropInPaths = []string{"/etc/systemd/system/pdns.service.d/evil.conf"}
				return identity, nil
			}
		}},
		{name: "control-pid", mutate: func(ops *pdnsRuntimeTopologyOps) {
			base := ops.inspectProcesses
			ops.inspectProcesses = func(unit string) (dnsUnitProcesses, error) {
				processes, err := base(unit)
				if unit == "pdns.service" {
					processes.ControlPID = 1004
				}
				return processes, err
			}
		}},
		{name: "vendor-toctou", mutate: func(ops *pdnsRuntimeTopologyOps) {
			calls := 0
			ops.inspectVendor = func() (bindSecureFileIdentity, error) {
				calls++
				identity := canonical.vendorUnit
				if calls > 1 {
					identity.Inode++
				}
				return identity, nil
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			ops := newOps()
			test.mutate(&ops)
			if _, err := inspectVerifiedPDNSRuntimeTopologyWithOps(profile, ops); err == nil {
				t.Fatal("unsafe PowerDNS topology was accepted")
			}
		})
	}
}
