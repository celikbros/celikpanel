package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/hostplatform"
)

func testUbuntuBINDProfile() hostplatform.Profile {
	return hostplatform.Profile{
		DistroFamily:   hostplatform.DistroFamilyDebian,
		PackageManager: hostplatform.PackageManagerAPT,
		ServiceManager: hostplatform.ServiceManagerSystemd,
		ID:             "ubuntu", Version: "24.04", Codename: "noble",
	}
}

func testDebian13BINDProfile() hostplatform.Profile {
	profile := testUbuntuBINDProfile()
	profile.ID, profile.Version, profile.Codename = "debian", "13", "trixie"
	return profile
}

func testPacmanBINDProfile() hostplatform.Profile {
	return hostplatform.Profile{
		DistroFamily:   hostplatform.DistroFamilyArch,
		PackageManager: hostplatform.PackageManagerPacman,
		ServiceManager: hostplatform.ServiceManagerSystemd,
		ID:             "arch", Version: "rolling",
	}
}

func TestRollbackBINDActivationPreservesCallerDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := rollbackBINDActivationWithOps(ctx, bindRollbackActivationOps{
		restoreTarget: func(commandCtx context.Context) error {
			<-commandCtx.Done()
			return commandCtx.Err()
		},
		restoreConfigs: func() error { return nil },
		restoreState:   func() error { return nil },
		restoreSource: func(commandCtx context.Context) error {
			return commandCtx.Err()
		},
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("BIND rollback did not return caller deadline: %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("BIND rollback escaped caller deadline: %s", elapsed)
	}
}

func TestAPTBindLayoutPreservesReleasedGenerationRoot(t *testing.T) {
	layout, err := bindLayout(testUbuntuBINDProfile())
	if err != nil {
		t.Fatal(err)
	}
	if layout.GenerationRoot != aptBINDGenerationRoot {
		t.Fatalf("generation root = %q, want %q", layout.GenerationRoot, aptBINDGenerationRoot)
	}
	if layout.GenerationRoot != "/var/cache/bind/celikpanel" {
		t.Fatal("APT layout changed the released BIND generation root")
	}
	if layout.GenerationRoot == abandonedAPTBindGenerationRoot {
		t.Fatal("APT layout silently moved an existing generation tree")
	}
}

func TestAPTBindLayoutUsesCapabilitiesInsteadOfDistributionIdentity(t *testing.T) {
	for _, profile := range []hostplatform.Profile{
		testUbuntuBINDProfile(),
		testDebian13BINDProfile(),
		{
			DistroFamily:   hostplatform.DistroFamilyDebian,
			PackageManager: hostplatform.PackageManagerAPT,
			ServiceManager: hostplatform.ServiceManagerSystemd,
			ID:             "operator-linux", Version: "2031.7", Codename: "custom",
		},
	} {
		if _, err := bindLayout(profile); err != nil {
			t.Fatalf("capable APT BIND profile rejected: %#v: %v", profile, err)
		}
	}
	for _, profile := range []hostplatform.Profile{
		{DistroFamily: hostplatform.DistroFamilyDebian, PackageManager: hostplatform.PackageManagerAPT},
		{DistroFamily: hostplatform.DistroFamilyRHEL, PackageManager: hostplatform.PackageManagerAPT, ServiceManager: hostplatform.ServiceManagerSystemd},
		{DistroFamily: hostplatform.DistroFamilyDebian, PackageManager: hostplatform.PackageManagerAPT, ServiceManager: "openrc"},
	} {
		if _, err := bindLayout(profile); err == nil {
			t.Fatalf("incomplete APT BIND capability profile accepted: %#v", profile)
		}
	}
}

func TestPacmanBindLayoutRequiresCompleteCapabilities(t *testing.T) {
	if _, err := bindLayout(testPacmanBINDProfile()); err != nil {
		t.Fatalf("capable pacman BIND profile rejected: %v", err)
	}
	for _, profile := range []hostplatform.Profile{
		{PackageManager: hostplatform.PackageManagerPacman},
		{DistroFamily: hostplatform.DistroFamilyDebian, PackageManager: hostplatform.PackageManagerPacman, ServiceManager: hostplatform.ServiceManagerSystemd},
		{DistroFamily: hostplatform.DistroFamilyArch, PackageManager: hostplatform.PackageManagerPacman, ServiceManager: "openrc"},
	} {
		if _, err := bindLayout(profile); err == nil {
			t.Fatalf("incomplete pacman BIND capability profile accepted: %#v", profile)
		}
	}
}

func TestParseDNSUnitProcessesCanonical(t *testing.T) {
	for _, test := range []struct {
		name   string
		output string
		want   dnsUnitProcesses
		ok     bool
	}{
		{name: "stopped", output: "MainPID=0\nControlPID=0\nSubState=dead\n", want: dnsUnitProcesses{SubState: "dead"}, ok: true},
		{name: "running-main", output: "ControlPID=0\nSubState=running\nMainPID=451\n", want: dnsUnitProcesses{MainPID: 451, SubState: "running"}, ok: true},
		{name: "running-control", output: "MainPID=0\nControlPID=452\nSubState=stop-sigterm\n", want: dnsUnitProcesses{ControlPID: 452, SubState: "stop-sigterm"}, ok: true},
		{name: "missing-both", output: "ActiveState=inactive\n"},
		{name: "missing-control", output: "MainPID=0\nSubState=dead\n"},
		{name: "missing-main", output: "ControlPID=0\nSubState=dead\n"},
		{name: "missing-substate", output: "MainPID=0\nControlPID=0\n"},
		{name: "empty-main", output: "MainPID=\nControlPID=0\nSubState=dead\n"},
		{name: "empty-control", output: "MainPID=0\nControlPID=\nSubState=dead\n"},
		{name: "empty-substate", output: "MainPID=0\nControlPID=0\nSubState=\n"},
		{name: "duplicate-main", output: "MainPID=0\nMainPID=0\nControlPID=0\nSubState=dead\n"},
		{name: "duplicate-control", output: "MainPID=0\nControlPID=0\nControlPID=0\nSubState=dead\n"},
		{name: "duplicate-substate", output: "MainPID=0\nControlPID=0\nSubState=dead\nSubState=dead\n"},
		{name: "leading-zero-main", output: "MainPID=00\nControlPID=0\nSubState=dead\n"},
		{name: "leading-zero-control", output: "MainPID=0\nControlPID=00\nSubState=dead\n"},
		{name: "signed", output: "MainPID=+1\nControlPID=0\nSubState=dead\n"},
		{name: "negative", output: "MainPID=0\nControlPID=-1\nSubState=dead\n"},
		{name: "word", output: "MainPID=none\nControlPID=0\nSubState=dead\n"},
		{name: "unknown-row", output: "MainPID=0\nControlPID=0\nSubState=dead\nEvil=value\n"},
		{name: "warning-row", output: "warning\nMainPID=0\nControlPID=0\nSubState=dead\n"},
		{name: "leading-space", output: " MainPID=0\nControlPID=0\nSubState=dead\n"},
		{name: "trailing-space", output: "MainPID=0 \nControlPID=0\nSubState=dead\n"},
		{name: "unicode-space", output: "\u00a0MainPID=0\nControlPID=0\nSubState=dead\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseDNSUnitProcesses(test.output)
			if (err == nil) != test.ok {
				t.Fatalf("parse error = %v, want ok=%v", err, test.ok)
			}
			if err == nil && got != test.want {
				t.Fatalf("processes = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestVerifyDNSUnitProcessesStoppedRejectsMainAndControlProcesses(t *testing.T) {
	if err := verifyDNSUnitProcessesStopped(dnsUnitProcesses{SubState: "dead"}); err != nil {
		t.Fatalf("stopped unit rejected: %v", err)
	}
	for _, processes := range []dnsUnitProcesses{
		{MainPID: 1, SubState: "running"},
		{ControlPID: 2, SubState: "stop-sigterm"},
		{MainPID: 1, ControlPID: 2, SubState: "running"},
		{SubState: "running"},
		{SubState: "failed"},
		{},
	} {
		if err := verifyDNSUnitProcessesStopped(processes); err == nil {
			t.Fatalf("live unit processes accepted: %+v", processes)
		}
	}
}

func TestInspectDNSUnitProcessesRespectsCallerDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := inspectDNSUnitProcessesWithRunner(
		ctx, "/usr/bin/systemctl", "named.service",
		func(commandCtx context.Context, _ string, _ ...string) ([]byte, error) {
			<-commandCtx.Done()
			return nil, commandCtx.Err()
		},
	)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("process inspection ignored caller deadline for %v", elapsed)
	}
}

func canonicalAPTNamedIdentity() dnsUnitIdentity {
	return dnsUnitIdentity{
		ID:            "named.service",
		Names:         []string{"bind9.service", "named.service"},
		FragmentPath:  "/usr/lib/systemd/system/named.service",
		Transient:     "no",
		ExecStartPath: "/usr/sbin/named",
		ExecStartArgv: "/usr/sbin/named -f $OPTIONS",
	}
}

func TestParseDNSUnitIdentityCanonical(t *testing.T) {
	valid := "Id=named.service\n" +
		"Names=named.service bind9.service\n" +
		"FragmentPath=/usr/lib/systemd/system/named.service\n" +
		"SourcePath=\nDropInPaths=\nTransient=no\n" +
		"ExecStart={ path=/usr/sbin/named ; argv[]=/usr/sbin/named -f $OPTIONS ; ignore_errors=no ; start_time=[n/a] ; stop_time=[n/a] ; pid=0 ; code=(null) ; status=0/0 }\n"
	identity, err := parseDNSUnitIdentity(valid)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(identity, canonicalAPTNamedIdentity()) {
		t.Fatalf("identity = %#v", identity)
	}
	for _, test := range []struct {
		name   string
		output string
	}{
		{name: "missing-property", output: strings.Replace(valid, "Transient=no\n", "", 1)},
		{name: "duplicate-property", output: valid + "DropInPaths=\n"},
		{name: "unexpected-property", output: valid + "Environment=OPTIONS=-c /evil\n"},
		{name: "noncanonical-names", output: strings.Replace(valid, "Names=named.service bind9.service", "Names=named.service  bind9.service", 1)},
		{name: "duplicate-name", output: strings.Replace(valid, "Names=named.service bind9.service", "Names=named.service named.service", 1)},
		{name: "multiple-exec", output: strings.Replace(valid,
			"ExecStart={ path=/usr/sbin/named ; argv[]=/usr/sbin/named -f $OPTIONS ; ignore_errors=no ; start_time=[n/a] ; stop_time=[n/a] ; pid=0 ; code=(null) ; status=0/0 }",
			"ExecStart={ path=/evil ; argv[]=/evil ; ignore_errors=no } { path=/usr/sbin/named ; argv[]=/usr/sbin/named -f $OPTIONS ; ignore_errors=no }", 1)},
		{name: "ignored-error", output: strings.Replace(valid, "ignore_errors=no", "ignore_errors=yes", 1)},
		{name: "path-argv-mismatch", output: strings.Replace(valid, "argv[]=/usr/sbin/named", "argv[]=/evil", 1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parseDNSUnitIdentity(test.output); err == nil {
				t.Fatal("unsafe identity output was accepted")
			}
		})
	}
}

func TestInspectDNSUnitIdentityRespectsCallerDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := inspectDNSUnitIdentityWithRunner(
		ctx, "/usr/bin/systemctl", "named.service",
		func(commandCtx context.Context, _ string, _ ...string) ([]byte, error) {
			<-commandCtx.Done()
			return nil, commandCtx.Err()
		},
	)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("identity inspection ignored caller deadline for %v", elapsed)
	}
}

func TestClassifyBINDTargetNotServingStates(t *testing.T) {
	sealed := bindInstallUnitState{
		loadState: "masked", activeState: "inactive", unitFileState: "masked",
	}
	unmasked := bindInstallUnitState{
		loadState: "loaded", activeState: "inactive", unitFileState: "disabled",
	}
	absent := bindInstallUnitState{
		loadState: "not-found", activeState: "inactive",
	}
	for _, test := range []struct {
		name       string
		named      bindInstallUnitState
		alias      bindInstallUnitState
		wantSealed bool
		wantErr    bool
	}{
		{name: "exact-double-persistent-mask", named: sealed, alias: sealed, wantSealed: true},
		{name: "clean-host-both-absent", named: absent, alias: absent},
		{name: "stock-disabled-alias-absent", named: unmasked, alias: absent},
		{name: "unmasked-inactive", named: unmasked, alias: unmasked},
		{name: "mixed-mask", named: sealed, alias: unmasked, wantErr: true},
		{name: "runtime-named", named: bindInstallUnitState{loadState: "masked", activeState: "inactive", unitFileState: "masked-runtime"}, alias: sealed, wantErr: true},
		{name: "runtime-alias", named: sealed, alias: bindInstallUnitState{loadState: "masked", activeState: "inactive", unitFileState: "masked-runtime"}, wantErr: true},
		{name: "active", named: bindInstallUnitState{loadState: "loaded", activeState: "active", unitFileState: "enabled"}, alias: unmasked, wantErr: true},
		{name: "failed", named: bindInstallUnitState{loadState: "loaded", activeState: "failed", unitFileState: "disabled"}, alias: unmasked, wantErr: true},
		{name: "masked-active", named: bindInstallUnitState{loadState: "masked", activeState: "active", unitFileState: "masked"}, alias: sealed, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := classifyBINDTargetNotServingStates(test.named, test.alias)
			if (err != nil) != test.wantErr || got != test.wantSealed {
				t.Fatalf("sealed=%v err=%v, want sealed=%v err=%v", got, err, test.wantSealed, test.wantErr)
			}
		})
	}
}

func TestBINDTargetNotServingSelectsExactCleanStockAndEnabledProofs(t *testing.T) {
	absent := bindInstallUnitState{loadState: "not-found", activeState: "inactive"}
	stock := bindInstallUnitState{
		loadState: "loaded", activeState: "inactive", unitFileState: "disabled",
	}
	enabled := bindInstallUnitState{
		loadState: "loaded", activeState: "inactive", unitFileState: "enabled",
	}
	sealed := bindInstallUnitState{
		loadState: "masked", activeState: "inactive", unitFileState: "masked",
	}
	for _, test := range []struct {
		name      string
		named     bindInstallUnitState
		alias     bindInstallUnitState
		wantProof string
	}{
		{name: "clean-host-reaches-install", named: absent, alias: absent, wantProof: "absent"},
		{name: "stock-installed-disabled", named: stock, alias: absent, wantProof: "pre-enable"},
		{name: "already-enabled-inactive", named: enabled, alias: enabled, wantProof: "pre-start"},
		{name: "failed-install-double-mask", named: sealed, alias: sealed, wantProof: "sealed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			proofs := []string{}
			err := verifyBINDTargetNotServingWithOps(
				testUbuntuBINDProfile(),
				bindTargetNotServingOps{
					inspectStates: func() (bindInstallUnitState, bindInstallUnitState, error) {
						return test.named, test.alias, nil
					},
					verifySealed:    func() error { proofs = append(proofs, "sealed"); return nil },
					verifyAbsent:    func() error { proofs = append(proofs, "absent"); return nil },
					verifyPreEnable: func() error { proofs = append(proofs, "pre-enable"); return nil },
					verifyPreStart:  func() error { proofs = append(proofs, "pre-start"); return nil },
				},
			)
			if err != nil || !reflect.DeepEqual(proofs, []string{test.wantProof}) {
				t.Fatalf("err=%v proofs=%#v, want %q", err, proofs, test.wantProof)
			}
		})
	}
}

func TestBINDTargetNotServingRejectsMixedActiveAndFailedWithoutProof(t *testing.T) {
	absent := bindInstallUnitState{loadState: "not-found", activeState: "inactive"}
	loaded := bindInstallUnitState{
		loadState: "loaded", activeState: "inactive", unitFileState: "enabled",
	}
	sealed := bindInstallUnitState{
		loadState: "masked", activeState: "inactive", unitFileState: "masked",
	}
	for _, test := range []struct {
		name  string
		named bindInstallUnitState
		alias bindInstallUnitState
	}{
		{name: "named-absent-alias-loaded", named: absent, alias: loaded},
		{name: "named-enabled-alias-absent", named: loaded, alias: absent},
		{name: "mixed-mask", named: sealed, alias: loaded},
		{name: "active", named: bindInstallUnitState{loadState: "loaded", activeState: "active", unitFileState: "enabled"}, alias: loaded},
		{name: "failed", named: bindInstallUnitState{loadState: "loaded", activeState: "failed", unitFileState: "disabled"}, alias: loaded},
	} {
		t.Run(test.name, func(t *testing.T) {
			proofCalls := 0
			proof := func() error { proofCalls++; return nil }
			err := verifyBINDTargetNotServingWithOps(
				testUbuntuBINDProfile(),
				bindTargetNotServingOps{
					inspectStates: func() (bindInstallUnitState, bindInstallUnitState, error) {
						return test.named, test.alias, nil
					},
					verifySealed: proof, verifyAbsent: proof,
					verifyPreEnable: proof, verifyPreStart: proof,
				},
			)
			if err == nil || proofCalls != 0 {
				t.Fatalf("unsafe target err=%v proofCalls=%d", err, proofCalls)
			}
		})
	}
}

func TestCleanBINDTargetProofCompositionReachesInstallExactlyOnce(t *testing.T) {
	absent := bindInstallUnitState{
		loadState: "not-found", activeState: "inactive",
	}
	proofCalls, installCalls := 0, 0
	proof, err := proveBINDTargetNotServingWithOps(
		testUbuntuBINDProfile(),
		bindTargetNotServingOps{
			inspectStates: func() (
				bindInstallUnitState, bindInstallUnitState, error,
			) {
				return absent, absent, nil
			},
			verifyAbsent: func() error {
				proofCalls++
				return nil
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := runVerifiedBINDTargetInstall(proof, func() error {
		installCalls++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if proofCalls != 1 || installCalls != 1 {
		t.Fatalf(
			"clean-host proof=%d install=%d", proofCalls, installCalls,
		)
	}

	mixed := bindInstallUnitState{
		loadState: "loaded", activeState: "active",
		unitFileState: "enabled",
	}
	unsafeProof, err := proveBINDTargetNotServingWithOps(
		testUbuntuBINDProfile(),
		bindTargetNotServingOps{
			inspectStates: func() (
				bindInstallUnitState, bindInstallUnitState, error,
			) {
				return mixed, absent, nil
			},
			verifyAbsent: func() error {
				t.Fatal("unsafe target reached absent proof")
				return nil
			},
		},
	)
	if err == nil {
		t.Fatal("unsafe BIND target received an install proof")
	}
	if err := runVerifiedBINDTargetInstall(unsafeProof, func() error {
		installCalls++
		return nil
	}); err == nil {
		t.Fatal("zero BIND target proof authorized package mutation")
	}
	if installCalls != 1 {
		t.Fatalf("unsafe target reached install: %d", installCalls)
	}
}

func TestSourceNoneBINDPostInstallProofRejectsAppearingAuthorityBeforeStage(t *testing.T) {
	strictCalls, genericCalls, stageCalls := 0, 0, 0
	err := runBINDPostInstallContinuation(
		true,
		bindPostInstallProofOps{
			verifyGeneric: func() error {
				genericCalls++
				return nil
			},
			verifyNoAuthority: func() error {
				strictCalls++
				return errors.New("PowerDNS appeared after package install")
			},
		},
		func() error {
			stageCalls++
			return nil
		},
	)
	if err == nil || strictCalls != 1 || genericCalls != 0 || stageCalls != 0 {
		t.Fatalf(
			"source-none post-install err=%v strict=%d generic=%d stage=%d",
			err, strictCalls, genericCalls, stageCalls,
		)
	}
	if err := runBINDPostInstallContinuation(
		false,
		bindPostInstallProofOps{
			verifyGeneric: func() error {
				genericCalls++
				return nil
			},
			verifyNoAuthority: func() error {
				t.Fatal("managed source unexpectedly used strict no-authority proof")
				return nil
			},
		},
		func() error {
			stageCalls++
			return nil
		},
	); err != nil {
		t.Fatal(err)
	}
	if genericCalls != 1 || stageCalls != 1 {
		t.Fatalf("managed-source generic=%d stage=%d", genericCalls, stageCalls)
	}
}

func TestNoPublicDNSAuthorityProofIsStrictStableAndDoubleSampled(t *testing.T) {
	loopback := strings.Join([]string{
		`udp UNCONN 0 0 127.0.0.53%lo:53 0.0.0.0:* users:(("systemd-resolve",pid=55,fd=4))`,
		`tcp LISTEN 0 4096 [::1]:53 [::]:* users:(("local-dns",pid=56,fd=5))`,
	}, "\n")
	inactive := bindInstallUnitState{
		loadState: "loaded", activeState: "inactive", unitFileState: "disabled",
	}
	stateCalls, listenerCalls := 0, 0
	if err := verifyNoManagedDNSAuthorityWithOps(noManagedDNSAuthorityProofOps{
		inspectUnit: func(string) (bindInstallUnitState, error) {
			stateCalls++
			return inactive, nil
		},
		inspectListeners: func() (string, error) {
			listenerCalls++
			return loopback, nil
		},
	}); err != nil || stateCalls != 6 || listenerCalls != 2 {
		t.Fatalf(
			"strict no-authority proof err=%v states=%d listeners=%d",
			err, stateCalls, listenerCalls,
		)
	}
	for _, test := range []struct {
		name   string
		output string
	}{
		{name: "malformed", output: "garbage"},
		{name: "unknown-protocol", output: `raw LISTEN 0 0 127.0.0.1:53 0.0.0.0:* users:(("x",pid=1,fd=2))`},
		{name: "bad-endpoint", output: `udp UNCONN 0 0 broken:53 0.0.0.0:* users:(("x",pid=1,fd=2))`},
		{name: "extra-owner", output: `udp UNCONN 0 0 127.0.0.1:53 0.0.0.0:* users:(("x",pid=1,fd=2)) users:(("y",pid=2,fd=3))`},
		{name: "public-pdns", output: `udp UNCONN 0 0 192.0.2.8:53 0.0.0.0:* users:(("pdns_server",pid=10,fd=3))`},
		{name: "duplicate", output: loopback + "\n" + loopback},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := verifyNoManagedDNSAuthorityWithOps(noManagedDNSAuthorityProofOps{
				inspectUnit: func(string) (bindInstallUnitState, error) {
					return inactive, nil
				},
				inspectListeners: func() (string, error) {
					return test.output, nil
				},
			}); err == nil {
				t.Fatal("unsafe no-authority listener proof was accepted")
			}
		})
	}
	t.Run("snapshot-drift", func(t *testing.T) {
		calls := 0
		if err := verifyNoManagedDNSAuthorityWithOps(noManagedDNSAuthorityProofOps{
			inspectUnit: func(string) (bindInstallUnitState, error) {
				return inactive, nil
			},
			inspectListeners: func() (string, error) {
				calls++
				if calls == 1 {
					return loopback, nil
				}
				return strings.Replace(
					loopback, "pid=55", "pid=57", 1,
				), nil
			},
		}); err == nil {
			t.Fatal("no-authority listener snapshot drift was accepted")
		}
	})
	for _, activatingUnit := range []string{"named.service", "pdns.service"} {
		t.Run(activatingUnit+"-activates-without-listener", func(t *testing.T) {
			stateCalls := 0
			if err := verifyNoManagedDNSAuthorityWithOps(noManagedDNSAuthorityProofOps{
				inspectUnit: func(unit string) (bindInstallUnitState, error) {
					stateCalls++
					if stateCalls > 3 && unit == activatingUnit {
						return bindInstallUnitState{
							loadState: "loaded", activeState: "active",
							unitFileState: "enabled",
						}, nil
					}
					return inactive, nil
				},
				inspectListeners: func() (string, error) {
					return "", nil
				},
			}); err == nil {
				t.Fatal("managed authority activation without a listener was accepted")
			}
		})
	}
	for _, failedUnit := range []string{"named.service", "pdns.service"} {
		t.Run(failedUnit+"-stable-failed-without-listener", func(t *testing.T) {
			if err := verifyNoManagedDNSAuthorityWithOps(noManagedDNSAuthorityProofOps{
				inspectUnit: func(unit string) (bindInstallUnitState, error) {
					if unit == failedUnit {
						return bindInstallUnitState{
							loadState: "loaded", activeState: "failed",
							unitFileState: "enabled",
						}, nil
					}
					return inactive, nil
				},
				inspectListeners: func() (string, error) {
					return "", nil
				},
			}); err == nil {
				t.Fatal("failed managed authority without a listener was accepted")
			}
		})
	}
	t.Run("inactive-unit-state-drift", func(t *testing.T) {
		stateCalls := 0
		if err := verifyNoManagedDNSAuthorityWithOps(noManagedDNSAuthorityProofOps{
			inspectUnit: func(string) (bindInstallUnitState, error) {
				stateCalls++
				if stateCalls > 3 {
					return bindInstallUnitState{
						loadState: "loaded", activeState: "inactive",
						unitFileState: "enabled",
					}, nil
				}
				return inactive, nil
			},
			inspectListeners: func() (string, error) {
				return "", nil
			},
		}); err == nil {
			t.Fatal("inactive managed unit drift was accepted")
		}
	})
}

func TestBINDAbsentTargetProofDoubleSamplesStateAndListeners(t *testing.T) {
	absent := bindInstallUnitState{loadState: "not-found", activeState: "inactive"}
	stateCalls, listenerCalls := 0, 0
	err := verifyBINDAbsentTargetNotServingWithOps(bindAbsentTargetProofOps{
		inspectStates: func() (bindInstallUnitState, bindInstallUnitState, error) {
			stateCalls++
			return absent, absent, nil
		},
		port53Conflict: func() (bool, error) {
			listenerCalls++
			return false, nil
		},
	})
	if err != nil || stateCalls != 2 || listenerCalls != 2 {
		t.Fatalf("absent proof err=%v states=%d listeners=%d", err, stateCalls, listenerCalls)
	}

	for _, test := range []struct {
		name     string
		conflict bool
		drift    bool
	}{
		{name: "listener", conflict: true},
		{name: "state-drift", drift: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			err := verifyBINDAbsentTargetNotServingWithOps(bindAbsentTargetProofOps{
				inspectStates: func() (bindInstallUnitState, bindInstallUnitState, error) {
					calls++
					if test.drift && calls > 1 {
						return absent, bindInstallUnitState{
							loadState: "loaded", activeState: "inactive",
							unitFileState: "disabled",
						}, nil
					}
					return absent, absent, nil
				},
				port53Conflict: func() (bool, error) { return test.conflict, nil },
			})
			if err == nil {
				t.Fatal("unsafe absent target proof was accepted")
			}
		})
	}
}

func TestBINDReadinessLiveDoubleMaskUsesSealedProof(t *testing.T) {
	sealed := bindInstallUnitState{
		loadState: "masked", activeState: "inactive", unitFileState: "masked",
	}
	sealedCalls, topologyCalls := 0, 0
	stateCalls, maskCalls, portCalls := 0, 0, 0
	processUnits := []string{}
	err := verifyBINDReadinessUnitTopologyWithOps(true, bindReadinessUnitProofOps{
		packageManager: hostplatform.PackageManagerAPT,
		inspectStates: func() (bindInstallUnitState, bindInstallUnitState, error) {
			return sealed, sealed, nil
		},
		verifySealed: func() error {
			sealedCalls++
			// The live recovery fixture has no /etc/bind/celikpanel yet. Readiness
			// must not instantiate a publisher or require that managed child here.
			return verifyBINDSealedTargetNotServingWithOps(bindSealedTargetProofOps{
				inspectStates: func() (bindInstallUnitState, bindInstallUnitState, error) {
					stateCalls++
					return sealed, sealed, nil
				},
				verifyPersistentMasks: func() error {
					maskCalls++
					return nil
				},
				inspectProcesses: func(unit string) (dnsUnitProcesses, error) {
					processUnits = append(processUnits, unit)
					return dnsUnitProcesses{
						MainPID: 0, ControlPID: 0, SubState: "dead",
					}, nil
				},
				port53Conflict: func() (bool, error) {
					portCalls++
					return false, nil
				},
			})
		},
		verifyTopology: func() error {
			topologyCalls++
			return errors.New("masked aliases have no visible vendor identity")
		},
	})
	wantUnits := []string{
		"named.service", "bind9.service",
		"named.service", "bind9.service",
	}
	if err != nil || sealedCalls != 1 || topologyCalls != 0 ||
		stateCalls != 2 || maskCalls != 2 || portCalls != 2 ||
		!reflect.DeepEqual(processUnits, wantUnits) {
		t.Fatalf(
			"readiness err=%v sealed=%d topology=%d states=%d masks=%d port=%d processes=%v",
			err, sealedCalls, topologyCalls, stateCalls, maskCalls, portCalls, processUnits,
		)
	}
}

func TestBINDSealedTargetProofRejectsProcessMaskAndListenerFailures(t *testing.T) {
	sealed := bindInstallUnitState{
		loadState: "masked", activeState: "inactive", unitFileState: "masked",
	}
	tests := []struct {
		name      string
		processes dnsUnitProcesses
		maskErr   error
		conflict  bool
	}{
		{name: "main-pid", processes: dnsUnitProcesses{MainPID: 1, SubState: "dead"}},
		{name: "control-pid", processes: dnsUnitProcesses{ControlPID: 2, SubState: "dead"}},
		{name: "non-dead-substate", processes: dnsUnitProcesses{SubState: "stop-sigterm"}},
		{name: "persistent-mask-proof", processes: dnsUnitProcesses{SubState: "dead"}, maskErr: errors.New("unsafe mask")},
		{name: "public-listener", processes: dnsUnitProcesses{SubState: "dead"}, conflict: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := verifyBINDSealedTargetNotServingWithOps(bindSealedTargetProofOps{
				inspectStates: func() (bindInstallUnitState, bindInstallUnitState, error) {
					return sealed, sealed, nil
				},
				verifyPersistentMasks: func() error { return test.maskErr },
				inspectProcesses: func(string) (dnsUnitProcesses, error) {
					return test.processes, nil
				},
				port53Conflict: func() (bool, error) {
					return test.conflict, nil
				},
			})
			if err == nil {
				t.Fatal("unsafe sealed-target proof was accepted")
			}
		})
	}
}

func TestBINDSealedTargetProofRejectsStateProcessAndMaskTOCTOU(t *testing.T) {
	sealed := bindInstallUnitState{
		loadState: "masked", activeState: "inactive", unitFileState: "masked",
	}
	stopped := dnsUnitProcesses{SubState: "dead"}
	t.Run("unit-state-changed", func(t *testing.T) {
		stateCalls := 0
		err := verifyBINDSealedTargetNotServingWithOps(bindSealedTargetProofOps{
			inspectStates: func() (bindInstallUnitState, bindInstallUnitState, error) {
				stateCalls++
				if stateCalls == 1 {
					return sealed, sealed, nil
				}
				changed := sealed
				changed.activeState = "active"
				return sealed, changed, nil
			},
			verifyPersistentMasks: func() error { return nil },
			inspectProcesses:      func(string) (dnsUnitProcesses, error) { return stopped, nil },
			port53Conflict:        func() (bool, error) { return false, nil },
		})
		if err == nil {
			t.Fatal("unit-state TOCTOU was accepted")
		}
	})
	t.Run("process-changed", func(t *testing.T) {
		processCalls := 0
		err := verifyBINDSealedTargetNotServingWithOps(bindSealedTargetProofOps{
			inspectStates:         func() (bindInstallUnitState, bindInstallUnitState, error) { return sealed, sealed, nil },
			verifyPersistentMasks: func() error { return nil },
			inspectProcesses: func(string) (dnsUnitProcesses, error) {
				processCalls++
				if processCalls > 2 {
					return dnsUnitProcesses{ControlPID: 9, SubState: "stop-sigterm"}, nil
				}
				return stopped, nil
			},
			port53Conflict: func() (bool, error) { return false, nil },
		})
		if err == nil {
			t.Fatal("process TOCTOU was accepted")
		}
	})
	t.Run("mask-changed", func(t *testing.T) {
		maskCalls := 0
		err := verifyBINDSealedTargetNotServingWithOps(bindSealedTargetProofOps{
			inspectStates: func() (bindInstallUnitState, bindInstallUnitState, error) {
				return sealed, sealed, nil
			},
			verifyPersistentMasks: func() error {
				maskCalls++
				if maskCalls > 1 {
					return errors.New("mask changed")
				}
				return nil
			},
			inspectProcesses: func(string) (dnsUnitProcesses, error) { return stopped, nil },
			port53Conflict:   func() (bool, error) { return false, nil },
		})
		if err == nil {
			t.Fatal("persistent-mask TOCTOU was accepted")
		}
	})
	t.Run("listener-appeared", func(t *testing.T) {
		portCalls := 0
		err := verifyBINDSealedTargetNotServingWithOps(bindSealedTargetProofOps{
			inspectStates: func() (bindInstallUnitState, bindInstallUnitState, error) {
				return sealed, sealed, nil
			},
			verifyPersistentMasks: func() error { return nil },
			inspectProcesses:      func(string) (dnsUnitProcesses, error) { return stopped, nil },
			port53Conflict: func() (bool, error) {
				portCalls++
				return portCalls > 1, nil
			},
		})
		if err == nil {
			t.Fatal("listener TOCTOU was accepted")
		}
		if portCalls != 2 {
			t.Fatalf("port proof calls = %d, want 2", portCalls)
		}
	})
}

func TestBINDSealedTargetAuthorityPolicyIsExactAndDoubleSampled(t *testing.T) {
	sealed := bindInstallUnitState{
		loadState: "masked", activeState: "inactive", unitFileState: "masked",
	}
	for _, test := range []struct {
		name              string
		allowPowerDNS     bool
		wantAllowPowerDNS bool
	}{
		{name: "generic-standby", allowPowerDNS: true, wantAllowPowerDNS: true},
		{name: "source-none", allowPowerDNS: false, wantAllowPowerDNS: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			ops := bindSealedTargetProofOperations(
				context.Background(), "/bin/false", test.allowPowerDNS,
				func(_ context.Context, allowBIND, allowPowerDNS bool) (bool, error) {
					calls++
					if allowBIND {
						t.Fatal("sealed BIND proof unexpectedly allowed BIND")
					}
					if allowPowerDNS != test.wantAllowPowerDNS {
						t.Fatalf(
							"allowPowerDNS=%v, want %v",
							allowPowerDNS, test.wantAllowPowerDNS,
						)
					}
					return false, nil
				},
			)
			ops.inspectStates = func() (bindInstallUnitState, bindInstallUnitState, error) {
				return sealed, sealed, nil
			}
			ops.verifyPersistentMasks = func() error { return nil }
			ops.inspectProcesses = func(string) (dnsUnitProcesses, error) {
				return dnsUnitProcesses{SubState: "dead"}, nil
			}
			if err := verifyBINDSealedTargetNotServingWithOps(ops); err != nil {
				t.Fatal(err)
			}
			if calls != 2 {
				t.Fatalf("port proof calls = %d, want 2", calls)
			}
		})
	}
}

func TestBINDReadinessUnitTopologyRejectsUnsafeMasks(t *testing.T) {
	sealed := bindInstallUnitState{
		loadState: "masked", activeState: "inactive", unitFileState: "masked",
	}
	for _, test := range []struct {
		name  string
		named bindInstallUnitState
		alias bindInstallUnitState
	}{
		{name: "mixed", named: sealed, alias: bindInstallUnitState{loadState: "loaded", activeState: "inactive", unitFileState: "disabled"}},
		{name: "runtime", named: sealed, alias: bindInstallUnitState{loadState: "masked", activeState: "inactive", unitFileState: "masked-runtime"}},
		{name: "failed", named: bindInstallUnitState{loadState: "loaded", activeState: "failed", unitFileState: "disabled"}, alias: sealed},
		{name: "active", named: bindInstallUnitState{loadState: "loaded", activeState: "active", unitFileState: "enabled"}, alias: sealed},
	} {
		t.Run(test.name, func(t *testing.T) {
			proofCalls := 0
			err := verifyBINDReadinessUnitTopologyWithOps(true, bindReadinessUnitProofOps{
				packageManager: hostplatform.PackageManagerAPT,
				inspectStates:  func() (bindInstallUnitState, bindInstallUnitState, error) { return test.named, test.alias, nil },
				verifySealed:   func() error { proofCalls++; return nil },
				verifyTopology: func() error { proofCalls++; return nil },
			})
			if err == nil || proofCalls != 0 {
				t.Fatalf("unsafe readiness err=%v proofCalls=%d", err, proofCalls)
			}
		})
	}
}

func TestBINDReadinessUnmaskedTargetRetainsTopologyProof(t *testing.T) {
	unmasked := bindInstallUnitState{
		loadState: "loaded", activeState: "inactive", unitFileState: "disabled",
	}
	sealedCalls, topologyCalls := 0, 0
	err := verifyBINDReadinessUnitTopologyWithOps(true, bindReadinessUnitProofOps{
		packageManager: hostplatform.PackageManagerAPT,
		inspectStates:  func() (bindInstallUnitState, bindInstallUnitState, error) { return unmasked, unmasked, nil },
		verifySealed:   func() error { sealedCalls++; return nil },
		verifyTopology: func() error { topologyCalls++; return nil },
	})
	if err != nil || sealedCalls != 0 || topologyCalls != 1 {
		t.Fatalf("unmasked readiness err=%v sealed=%d topology=%d", err, sealedCalls, topologyCalls)
	}
}

func TestBINDReadinessStockDisabledUsesReadOnlyPreEnableProof(t *testing.T) {
	stock := bindInstallUnitState{
		loadState: "loaded", activeState: "inactive", unitFileState: "disabled",
	}
	absent := bindInstallUnitState{loadState: "not-found", activeState: "inactive"}
	preEnable, topology := 0, 0
	err := verifyBINDReadinessUnitTopologyWithOps(true, bindReadinessUnitProofOps{
		packageManager: hostplatform.PackageManagerAPT,
		inspectStates: func() (bindInstallUnitState, bindInstallUnitState, error) {
			return stock, absent, nil
		},
		verifyPreEnable: func() error { preEnable++; return nil },
		verifyTopology:  func() error { topology++; return nil },
	})
	if err != nil || preEnable != 1 || topology != 0 {
		t.Fatalf("stock readiness err=%v preEnable=%d topology=%d", err, preEnable, topology)
	}
}

func TestBINDReadinessPacmanRequiresNamedWithoutAlias(t *testing.T) {
	absent := bindInstallUnitState{
		loadState: "not-found", activeState: "inactive",
	}
	for _, activeState := range []string{"inactive", "active"} {
		t.Run(activeState, func(t *testing.T) {
			topologyCalls := 0
			err := verifyBINDReadinessUnitTopologyWithOps(
				true,
				bindReadinessUnitProofOps{
					packageManager: hostplatform.PackageManagerPacman,
					inspectStates: func() (bindInstallUnitState, bindInstallUnitState, error) {
						return bindInstallUnitState{
							loadState: "loaded", activeState: activeState,
							unitFileState: "enabled",
						}, absent, nil
					},
					verifyTopology: func() error { topologyCalls++; return nil },
				},
			)
			if err != nil || topologyCalls != 1 {
				t.Fatalf("Pacman readiness err=%v topologyCalls=%d", err, topologyCalls)
			}
		})
	}
	for _, alias := range []bindInstallUnitState{
		{loadState: "loaded", activeState: "inactive", unitFileState: "disabled"},
		{loadState: "masked", activeState: "inactive", unitFileState: "masked"},
	} {
		topologyCalls := 0
		err := verifyBINDReadinessUnitTopologyWithOps(
			true,
			bindReadinessUnitProofOps{
				packageManager: hostplatform.PackageManagerPacman,
				inspectStates: func() (bindInstallUnitState, bindInstallUnitState, error) {
					return bindInstallUnitState{
						loadState: "loaded", activeState: "inactive",
						unitFileState: "enabled",
					}, alias, nil
				},
				verifyTopology: func() error { topologyCalls++; return nil },
			},
		)
		if err == nil || topologyCalls != 0 {
			t.Fatalf("unexpected Pacman alias err=%v topologyCalls=%d", err, topologyCalls)
		}
	}
}

func TestBINDReadinessNotInstalledIsZeroTouch(t *testing.T) {
	inspectCalls := 0
	err := verifyBINDReadinessUnitTopologyWithOps(false, bindReadinessUnitProofOps{
		packageManager: hostplatform.PackageManagerAPT,
		inspectStates: func() (bindInstallUnitState, bindInstallUnitState, error) {
			inspectCalls++
			return bindInstallUnitState{}, bindInstallUnitState{}, nil
		},
	})
	if err != nil || inspectCalls != 0 {
		t.Fatalf("not-installed readiness err=%v inspectCalls=%d", err, inspectCalls)
	}
}

func TestValidateAPTBINDVendorAliasIdentity(t *testing.T) {
	valid := canonicalAPTNamedIdentity()
	if err := validateAPTBINDVendorAliasIdentity(valid, valid); err != nil {
		t.Fatalf("valid alias identity rejected: %v", err)
	}
	for _, test := range []struct {
		name  string
		alter func(*dnsUnitIdentity, *dnsUnitIdentity)
	}{
		{name: "named-id-spoof", alter: func(named, _ *dnsUnitIdentity) { named.ID = "evil.service" }},
		{name: "alias-id-spoof", alter: func(_, alias *dnsUnitIdentity) { alias.ID = "bind9.service" }},
		{name: "missing-alias", alter: func(named, _ *dnsUnitIdentity) { named.Names = []string{"named.service"} }},
		{name: "extra-name", alter: func(_, alias *dnsUnitIdentity) { alias.Names = append(alias.Names, "evil.service") }},
		{name: "path-mismatch", alter: func(_, alias *dnsUnitIdentity) { alias.FragmentPath = "/usr/lib/systemd/system/bind9.service" }},
		{name: "relative-path", alter: func(named, alias *dnsUnitIdentity) {
			named.FragmentPath = "usr/lib/systemd/system/named.service"
			alias.FragmentPath = named.FragmentPath
		}},
		{name: "non-canonical-path", alter: func(named, alias *dnsUnitIdentity) {
			named.FragmentPath = "/usr/lib/systemd/../system/named.service"
			alias.FragmentPath = named.FragmentPath
		}},
		{name: "fragment-basename-spoof", alter: func(named, alias *dnsUnitIdentity) {
			named.FragmentPath = "/usr/lib/systemd/system/evil.service"
			alias.FragmentPath = named.FragmentPath
		}},
		{name: "drop-in", alter: func(named, alias *dnsUnitIdentity) {
			named.DropInPaths = []string{"/etc/systemd/system/named.service.d/evil.conf"}
			alias.DropInPaths = append([]string(nil), named.DropInPaths...)
		}},
		{name: "source-path", alter: func(named, alias *dnsUnitIdentity) {
			named.SourcePath = "/etc/init.d/named"
			alias.SourcePath = named.SourcePath
		}},
		{name: "transient", alter: func(named, alias *dnsUnitIdentity) {
			named.Transient = "yes"
			alias.Transient = "yes"
		}},
		{name: "exec-path", alter: func(named, alias *dnsUnitIdentity) {
			named.ExecStartPath = "/tmp/named"
			alias.ExecStartPath = named.ExecStartPath
		}},
		{name: "exec-argv", alter: func(named, alias *dnsUnitIdentity) {
			named.ExecStartArgv = "/usr/sbin/named -f -c /tmp/evil.conf"
			alias.ExecStartArgv = named.ExecStartArgv
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			named := valid
			alias := valid
			named.Names = append([]string(nil), valid.Names...)
			alias.Names = append([]string(nil), valid.Names...)
			test.alter(&named, &alias)
			if err := validateAPTBINDVendorAliasIdentity(named, alias); err == nil {
				t.Fatal("spoofed BIND alias identity was accepted")
			}
		})
	}
}

func TestVerifyBINDPreStartIdentityRejectsFirstAndSecondSnapshotDrift(t *testing.T) {
	profile := testUbuntuBINDProfile()
	inactive := bindInstallUnitState{
		loadState: "loaded", activeState: "inactive", unitFileState: "enabled",
	}
	valid := canonicalAPTNamedIdentity()
	baseFiles := bindVendorFilesIdentity{
		Unit:        bindSecureFileIdentity{Device: 1, Inode: 2, Size: 376},
		Environment: bindSecureFileIdentity{Device: 1, Inode: 3, Size: 86},
	}
	newOps := func() bindPreStartIdentityOps {
		return bindPreStartIdentityOps{
			inspectStates: func() (bindInstallUnitState, bindInstallUnitState, error) {
				return inactive, inactive, nil
			},
			inspectIdentity: func(string) (dnsUnitIdentity, error) { return valid, nil },
			inspectVendorFiles: func() (bindVendorFilesIdentity, error) {
				return baseFiles, nil
			},
			inspectProcesses: func(string) (dnsUnitProcesses, error) {
				return dnsUnitProcesses{SubState: "dead"}, nil
			},
		}
	}
	if err := verifyBINDPreStartIdentityWithOps(profile, newOps()); err != nil {
		t.Fatalf("exact pre-start identity rejected: %v", err)
	}
	t.Run("unsafe-first-identity", func(t *testing.T) {
		ops := newOps()
		ops.inspectIdentity = func(string) (dnsUnitIdentity, error) {
			unsafe := valid
			unsafe.DropInPaths = []string{"/run/systemd/system/named.service.d/evil.conf"}
			return unsafe, nil
		}
		if err := verifyBINDPreStartIdentityWithOps(profile, ops); err == nil {
			t.Fatal("unsafe first identity was accepted")
		}
	})
	t.Run("second-identity-drift", func(t *testing.T) {
		ops := newOps()
		calls := 0
		ops.inspectIdentity = func(string) (dnsUnitIdentity, error) {
			calls++
			identity := valid
			if calls > 2 {
				identity.ExecStartArgv = "/usr/sbin/named -f -c /tmp/evil.conf"
			}
			return identity, nil
		}
		if err := verifyBINDPreStartIdentityWithOps(profile, ops); err == nil {
			t.Fatal("second systemd identity drift was accepted")
		}
	})
	t.Run("second-file-drift", func(t *testing.T) {
		ops := newOps()
		calls := 0
		ops.inspectVendorFiles = func() (bindVendorFilesIdentity, error) {
			calls++
			identity := baseFiles
			if calls > 1 {
				identity.Environment.Digest[0] = 1
			}
			return identity, nil
		}
		if err := verifyBINDPreStartIdentityWithOps(profile, ops); err == nil {
			t.Fatal("second vendor-file drift was accepted")
		}
	})
	t.Run("second-state-drift", func(t *testing.T) {
		ops := newOps()
		calls := 0
		ops.inspectStates = func() (bindInstallUnitState, bindInstallUnitState, error) {
			calls++
			if calls > 1 {
				changed := inactive
				changed.activeState = "active"
				return changed, inactive, nil
			}
			return inactive, inactive, nil
		}
		if err := verifyBINDPreStartIdentityWithOps(profile, ops); err == nil {
			t.Fatal("second unit-state drift was accepted")
		}
	})
}

func TestVerifyBINDPreEnableIdentityRequiresAbsentAPTAliasAndStoppedVendorUnit(t *testing.T) {
	profile := testUbuntuBINDProfile()
	namedState := bindInstallUnitState{
		loadState: "loaded", activeState: "inactive", unitFileState: "disabled",
	}
	aliasAbsent := bindInstallUnitState{
		loadState: "not-found", activeState: "inactive",
	}
	namedIdentity := canonicalAPTNamedIdentity()
	namedIdentity.Names = []string{"named.service"}
	files := bindVendorFilesIdentity{
		Unit:        bindSecureFileIdentity{Device: 1, Inode: 2, Size: 376},
		Environment: bindSecureFileIdentity{Device: 1, Inode: 3, Size: 86},
	}
	newOps := func() bindUnitIdentityProofOps {
		return bindUnitIdentityProofOps{
			inspectStates: func() (bindInstallUnitState, bindInstallUnitState, error) {
				return namedState, aliasAbsent, nil
			},
			inspectIdentity: func(unit string) (dnsUnitIdentity, error) {
				if unit != "named.service" {
					t.Fatalf("unexpected identity inspection for %s", unit)
				}
				return namedIdentity, nil
			},
			inspectVendorFiles: func() (bindVendorFilesIdentity, error) {
				return files, nil
			},
			inspectProcesses: func(unit string) (dnsUnitProcesses, error) {
				if unit != "named.service" {
					t.Fatalf("unexpected process inspection for %s", unit)
				}
				return dnsUnitProcesses{SubState: "dead"}, nil
			},
		}
	}
	mode := bindUnitIdentityProofMode{
		aptAliasEnabled: false, requireInactive: true, requireStopped: true,
	}
	if err := verifyBINDUnitIdentityWithOps(profile, mode, newOps()); err != nil {
		t.Fatalf("exact pre-enable proof rejected: %v", err)
	}
	t.Run("alias-already-materialized", func(t *testing.T) {
		ops := newOps()
		ops.inspectStates = func() (bindInstallUnitState, bindInstallUnitState, error) {
			return namedState, namedState, nil
		}
		if err := verifyBINDUnitIdentityWithOps(profile, mode, ops); err == nil {
			t.Fatal("unexpected pre-enable alias was accepted")
		}
	})
	t.Run("alias-name-visible-before-enable", func(t *testing.T) {
		ops := newOps()
		unsafe := namedIdentity
		unsafe.Names = []string{"bind9.service", "named.service"}
		ops.inspectIdentity = func(string) (dnsUnitIdentity, error) { return unsafe, nil }
		if err := verifyBINDUnitIdentityWithOps(profile, mode, ops); err == nil {
			t.Fatal("pre-existing alias identity was accepted before enable")
		}
	})
	t.Run("control-process", func(t *testing.T) {
		ops := newOps()
		ops.inspectProcesses = func(string) (dnsUnitProcesses, error) {
			return dnsUnitProcesses{ControlPID: 19, SubState: "stop-sigterm"}, nil
		}
		if err := verifyBINDUnitIdentityWithOps(profile, mode, ops); err == nil {
			t.Fatal("pre-enable control process was accepted")
		}
	})
}

func TestVerifyBINDRuntimeTopologyRejectsIdentityVendorAndSnapshotDrift(t *testing.T) {
	profile := testUbuntuBINDProfile()
	active := bindInstallUnitState{
		loadState: "loaded", activeState: "active", unitFileState: "enabled",
	}
	valid := canonicalAPTNamedIdentity()
	files := bindVendorFilesIdentity{
		Unit:        bindSecureFileIdentity{Device: 1, Inode: 2, Size: 376},
		Environment: bindSecureFileIdentity{Device: 1, Inode: 3, Size: 86},
	}
	newOps := func() bindUnitIdentityProofOps {
		return bindUnitIdentityProofOps{
			inspectStates: func() (bindInstallUnitState, bindInstallUnitState, error) {
				return active, active, nil
			},
			inspectIdentity: func(string) (dnsUnitIdentity, error) {
				return valid, nil
			},
			inspectVendorFiles: func() (bindVendorFilesIdentity, error) {
				return files, nil
			},
		}
	}
	mode := bindUnitIdentityProofMode{aptAliasEnabled: true}
	if err := verifyBINDUnitIdentityWithOps(profile, mode, newOps()); err != nil {
		t.Fatalf("exact runtime topology rejected: %v", err)
	}
	for _, test := range []struct {
		name string
		edit func(*dnsUnitIdentity)
	}{
		{name: "drop-in", edit: func(identity *dnsUnitIdentity) {
			identity.DropInPaths = []string{"/etc/systemd/system/named.service.d/evil.conf"}
		}},
		{name: "exec-override", edit: func(identity *dnsUnitIdentity) {
			identity.ExecStartArgv = "/usr/sbin/named -f -c /tmp/evil.conf"
		}},
		{name: "source-path", edit: func(identity *dnsUnitIdentity) {
			identity.SourcePath = "/etc/init.d/named"
		}},
		{name: "transient", edit: func(identity *dnsUnitIdentity) {
			identity.Transient = "yes"
		}},
	} {
		t.Run("first-"+test.name, func(t *testing.T) {
			ops := newOps()
			unsafe := valid
			test.edit(&unsafe)
			ops.inspectIdentity = func(string) (dnsUnitIdentity, error) {
				return unsafe, nil
			}
			if err := verifyBINDUnitIdentityWithOps(profile, mode, ops); err == nil {
				t.Fatalf("unsafe runtime %s was accepted", test.name)
			}
		})
	}
	t.Run("vendor-environment", func(t *testing.T) {
		ops := newOps()
		ops.inspectVendorFiles = func() (bindVendorFilesIdentity, error) {
			return bindVendorFilesIdentity{}, errors.New("unsafe /etc/default/named")
		}
		if err := verifyBINDUnitIdentityWithOps(profile, mode, ops); err == nil {
			t.Fatal("unsafe runtime vendor environment was accepted")
		}
	})
	t.Run("second-identity", func(t *testing.T) {
		ops := newOps()
		calls := 0
		ops.inspectIdentity = func(string) (dnsUnitIdentity, error) {
			calls++
			identity := valid
			if calls > 2 {
				identity.DropInPaths = []string{"/run/systemd/system/named.service.d/drift.conf"}
			}
			return identity, nil
		}
		if err := verifyBINDUnitIdentityWithOps(profile, mode, ops); err == nil {
			t.Fatal("second runtime identity drift was accepted")
		}
	})
	t.Run("second-vendor-file", func(t *testing.T) {
		ops := newOps()
		calls := 0
		ops.inspectVendorFiles = func() (bindVendorFilesIdentity, error) {
			calls++
			identity := files
			if calls > 1 {
				identity.Environment.Digest[0] = 1
			}
			return identity, nil
		}
		if err := verifyBINDUnitIdentityWithOps(profile, mode, ops); err == nil {
			t.Fatal("second runtime vendor-file drift was accepted")
		}
	})
	t.Run("second-state", func(t *testing.T) {
		ops := newOps()
		calls := 0
		ops.inspectStates = func() (bindInstallUnitState, bindInstallUnitState, error) {
			calls++
			if calls > 1 {
				changed := active
				changed.activeState = "inactive"
				return changed, active, nil
			}
			return active, active, nil
		}
		if err := verifyBINDUnitIdentityWithOps(profile, mode, ops); err == nil {
			t.Fatal("second runtime state drift was accepted")
		}
	})
}

func TestVerifyOnlyBINDActiveRequiresExactAliasAuthorityAndStableSnapshots(t *testing.T) {
	profile := testUbuntuBINDProfile()
	active := bindInstallUnitState{
		loadState: "loaded", activeState: "active", unitFileState: "enabled",
	}
	pdnsInactive := bindInstallUnitState{
		loadState: "loaded", activeState: "inactive", unitFileState: "disabled",
	}
	baseTopology := bindRuntimeTopologySnapshot{
		namedState: active,
		aliasState: active,
		namedIdentity: dnsUnitIdentity{
			ID: "named.service", FragmentPath: "/usr/lib/systemd/system/named.service",
		},
		aliasIdentity: dnsUnitIdentity{
			ID: "named.service", FragmentPath: "/usr/lib/systemd/system/named.service",
		},
		namedProcesses: dnsUnitProcesses{MainPID: 10, SubState: "running"},
		aliasProcesses: dnsUnitProcesses{MainPID: 10, SubState: "running"},
	}
	validListeners := strings.Join([]string{
		`udp UNCONN 0 0 0.0.0.0:53 0.0.0.0:* users:(("named",pid=10,fd=1))`,
		`tcp LISTEN 0 4096 [::]:53 [::]:* users:(("named",pid=10,fd=2))`,
	}, "\n")
	newOps := func() bindActiveProofOps {
		return bindActiveProofOps{
			inspectTopology: func() (bindRuntimeTopologySnapshot, error) {
				return baseTopology, nil
			},
			inspectPowerDNS: func() (bindInstallUnitState, error) {
				return pdnsInactive, nil
			},
			inspectListeners: func() (string, error) {
				return validListeners, nil
			},
		}
	}
	if err := verifyOnlyBINDActiveWithOps(profile, newOps()); err != nil {
		t.Fatalf("exact active BIND proof rejected: %v", err)
	}
	t.Run("queue-fd-order-variation", func(t *testing.T) {
		ops := newOps()
		calls := 0
		ops.inspectListeners = func() (string, error) {
			calls++
			if calls == 1 {
				return validListeners, nil
			}
			return strings.Join([]string{
				`tcp LISTEN 7 8192 [::]:53 [::]:* users:(("named",pid=10,fd=99))`,
				`udp UNCONN 12 0 0.0.0.0:53 0.0.0.0:* users:(("named",pid=10,fd=88))`,
			}, "\n"), nil
		}
		if err := verifyOnlyBINDActiveWithOps(profile, ops); err != nil {
			t.Fatalf("stable listener identity rejected queue/fd/order drift: %v", err)
		}
	})
	for _, test := range []struct {
		name  string
		alias bindInstallUnitState
	}{
		{name: "alias-inactive", alias: bindInstallUnitState{
			loadState: "loaded", activeState: "inactive", unitFileState: "enabled",
		}},
		{name: "alias-absent", alias: bindInstallUnitState{
			loadState: "not-found", activeState: "inactive",
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			ops := newOps()
			topology := baseTopology
			topology.aliasState = test.alias
			ops.inspectTopology = func() (bindRuntimeTopologySnapshot, error) {
				return topology, nil
			}
			if err := verifyOnlyBINDActiveWithOps(profile, ops); err == nil {
				t.Fatal("unsafe APT alias state was accepted")
			}
		})
	}
	t.Run("PowerDNS-active", func(t *testing.T) {
		ops := newOps()
		ops.inspectPowerDNS = func() (bindInstallUnitState, error) {
			return bindInstallUnitState{
				loadState: "loaded", activeState: "active", unitFileState: "enabled",
			}, nil
		}
		if err := verifyOnlyBINDActiveWithOps(profile, ops); err == nil {
			t.Fatal("active PowerDNS was accepted alongside BIND")
		}
	})
	t.Run("topology-identity-drift", func(t *testing.T) {
		ops := newOps()
		calls := 0
		ops.inspectTopology = func() (bindRuntimeTopologySnapshot, error) {
			calls++
			topology := baseTopology
			if calls > 1 {
				topology.namedIdentity.ExecStartArgv =
					"/usr/sbin/named -f -c /tmp/drift.conf"
			}
			return topology, nil
		}
		if err := verifyOnlyBINDActiveWithOps(profile, ops); err == nil {
			t.Fatal("runtime identity drift was accepted")
		}
	})
	t.Run("PowerDNS-state-drift", func(t *testing.T) {
		ops := newOps()
		calls := 0
		ops.inspectPowerDNS = func() (bindInstallUnitState, error) {
			calls++
			if calls > 1 {
				return bindInstallUnitState{
					loadState: "not-found", activeState: "inactive",
				}, nil
			}
			return pdnsInactive, nil
		}
		if err := verifyOnlyBINDActiveWithOps(profile, ops); err == nil {
			t.Fatal("PowerDNS state drift was accepted")
		}
	})
	t.Run("listener-drift", func(t *testing.T) {
		ops := newOps()
		calls := 0
		ops.inspectListeners = func() (string, error) {
			calls++
			if calls > 1 {
				return strings.Replace(validListeners, "pid=10", "pid=11", 1), nil
			}
			return validListeners, nil
		}
		if err := verifyOnlyBINDActiveWithOps(profile, ops); err == nil {
			t.Fatal("listener snapshot drift was accepted")
		}
	})
	t.Run("listener-process-spoof", func(t *testing.T) {
		ops := newOps()
		ops.inspectListeners = func() (string, error) {
			return strings.Replace(validListeners, `"named"`, `"renamed"`, 1), nil
		}
		if err := verifyOnlyBINDActiveWithOps(profile, ops); err == nil {
			t.Fatal("listener process spoof was accepted")
		}
	})
	t.Run("listener-address-drift", func(t *testing.T) {
		ops := newOps()
		calls := 0
		ops.inspectListeners = func() (string, error) {
			calls++
			if calls > 1 {
				return strings.Replace(validListeners, "0.0.0.0:53", "192.0.2.1:53", 1), nil
			}
			return validListeners, nil
		}
		if err := verifyOnlyBINDActiveWithOps(profile, ops); err == nil {
			t.Fatal("listener address drift was accepted")
		}
	})
}

func TestCanonicalBINDPublicListenersRejectsMalformedAndSpoofedRows(t *testing.T) {
	valid := strings.Join([]string{
		`udp UNCONN 0 0 0.0.0.0:53 0.0.0.0:* users:(("named",pid=10,fd=1))`,
		`tcp LISTEN 0 4096 [::]:53 [::]:* users:(("named",pid=10,fd=2))`,
	}, "\n")
	if _, err := canonicalBINDPublicListeners(valid, 10); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		row  string
	}{
		{name: "malformed-extra", row: "garbage"},
		{name: "unknown-protocol", row: `sctp LISTEN 0 0 0.0.0.0:53 0.0.0.0:* users:(("named",pid=10,fd=3))`},
		{name: "wrong-state", row: `tcp EVIL 0 0 192.0.2.1:53 0.0.0.0:* users:(("named",pid=10,fd=3))`},
		{name: "bad-queue", row: `udp UNCONN x 0 192.0.2.1:53 0.0.0.0:* users:(("named",pid=10,fd=3))`},
		{name: "bad-endpoint", row: `udp UNCONN 0 0 bad:53 0.0.0.0:* users:(("named",pid=10,fd=3))`},
		{name: "bad-peer", row: `udp UNCONN 0 0 192.0.2.1:53 junk users:(("named",pid=10,fd=3))`},
		{name: "normal-zoned-peer", row: `udp UNCONN 0 0 192.0.2.1:53 [fe80::1%eth0]:* users:(("named",pid=10,fd=3))`},
		{name: "iproute2-zoned-peer", row: `udp UNCONN 0 0 192.0.2.1:53 [fe80::1]%eth0:* users:(("named",pid=10,fd=3))`},
		{name: "zoned-ipv4-peer", row: `udp UNCONN 0 0 192.0.2.1:53 127.0.0.1%lo:* users:(("named",pid=10,fd=3))`},
		{name: "ambiguous-wildcard", row: `udp UNCONN 0 0 *:53 *:* users:(("named",pid=10,fd=3))`},
		{name: "normal-scoped-ipv4", row: `udp UNCONN 0 0 192.0.2.1%eth0:53 0.0.0.0:* users:(("named",pid=10,fd=3))`},
		{name: "scoped-ipv4", row: `udp UNCONN 0 0 [192.0.2.1]%eth0:53 0.0.0.0:* users:(("named",pid=10,fd=3))`},
		{name: "empty-ipv6-scope", row: `udp UNCONN 0 0 [fe80::1]%:53 [::]:* users:(("named",pid=10,fd=3))`},
		{name: "double-ipv6-scope", row: `udp UNCONN 0 0 [fe80::1]%eth0%evil:53 [::]:* users:(("named",pid=10,fd=3))`},
		{name: "invalid-ipv6-interface", row: `udp UNCONN 0 0 [fe80::1]%eth0/evil:53 [::]:* users:(("named",pid=10,fd=3))`},
		{name: "extra-ipv6-bracket", row: `udp UNCONN 0 0 [fe80::1]]%eth0:53 [::]:* users:(("named",pid=10,fd=3))`},
		{name: "suffixed-ipv6-port", row: `udp UNCONN 0 0 [fe80::1]%eth0:53:evil [::]:* users:(("named",pid=10,fd=3))`},
		{name: "leading-zero-pid", row: `udp UNCONN 0 0 192.0.2.1:53 0.0.0.0:* users:(("named",pid=010,fd=3))`},
		{name: "multiple-pids", row: `udp UNCONN 0 0 192.0.2.1:53 0.0.0.0:* users:(("named",pid=10,fd=3),("named",pid=11,fd=4))`},
		{name: "extra-owner-field", row: `udp UNCONN 0 0 192.0.2.1:53 0.0.0.0:* users:(("evil",pid=11,fd=3)) users:(("named",pid=10,fd=4))`},
		{name: "extra-field", row: `udp UNCONN 0 0 192.0.2.1:53 0.0.0.0:* extra users:(("named",pid=10,fd=4))`},
		{name: "trailing-spoof", row: `udp UNCONN 0 0 192.0.2.1:53 0.0.0.0:* users:(("named",pid=10,fd=3))evil`},
		{name: "wrong-pid", row: `udp UNCONN 0 0 192.0.2.1:53 0.0.0.0:* users:(("named",pid=11,fd=3))`},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := canonicalBINDPublicListeners(valid+"\n"+test.row, 10); err == nil {
				t.Fatal("malformed or spoofed public listener row was accepted")
			}
		})
	}
}

func TestCanonicalBINDPublicListenersAcceptsIPRoute2ScopedIPv6Rows(t *testing.T) {
	output := strings.Join([]string{
		`udp UNCONN 0 0 72.62.38.15:53 0.0.0.0:* users:(("named",pid=10,fd=1))`,
		`tcp LISTEN 0 4096 72.62.38.15:53 0.0.0.0:* users:(("named",pid=10,fd=2))`,
		`udp UNCONN 0 0 [2a02:4780:41:c2df::1]:53 [::]:* users:(("named",pid=10,fd=3))`,
		`tcp LISTEN 0 4096 [2a02:4780:41:c2df::1]:53 [::]:* users:(("named",pid=10,fd=4))`,
		`udp UNCONN 0 0 [fe80::62e8:d4ff:feb6:6a61]%eth0:53 [::]:* users:(("named",pid=10,fd=5))`,
		`tcp LISTEN 0 4096 [fe80::62e8:d4ff:feb6:6a61]%eth0:53 [::]:* users:(("named",pid=10,fd=6))`,
	}, "\n")
	listeners, err := canonicalBINDPublicListeners(output, 10)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"tcp|2a02:4780:41:c2df::1|10",
		"tcp|72.62.38.15|10",
		"udp|2a02:4780:41:c2df::1|10",
		"udp|72.62.38.15|10",
	}
	if !reflect.DeepEqual(listeners, want) {
		t.Fatalf("listeners = %#v, want %#v", listeners, want)
	}
}

func TestActivateBINDTargetEnablesAliasBeforeVerifiedStart(t *testing.T) {
	var events []string
	ops := bindActivationOps{
		unmask: func(_ context.Context, unit string, runtime bool) error {
			kind := "persistent"
			if runtime {
				kind = "runtime"
			}
			events = append(events, "unmask:"+kind+":"+unit)
			return nil
		},
		daemonReload: func(context.Context) error {
			events = append(events, "daemon-reload")
			return nil
		},
		verifyPreEnable: func(context.Context) (bindBeforeEnableDisposition, error) {
			events = append(events, "verify-pre-enable")
			return bindBeforeEnableNeedsEnable, nil
		},
		enable: func(_ context.Context, unit string) error {
			events = append(events, "enable-only:"+unit)
			return nil
		},
		verifyPreStart: func(context.Context) error {
			events = append(events, "verify-pre-start")
			return nil
		},
		start: func(_ context.Context, unit string) error {
			events = append(events, "start-only:"+unit)
			return nil
		},
		verifyStarted: func(context.Context) error {
			events = append(events, "verify-started")
			return nil
		},
	}
	if err := activateBINDTargetWithOps(context.Background(), "named.service", ops); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"unmask:persistent:named.service",
		"unmask:runtime:named.service",
		"unmask:persistent:bind9.service",
		"unmask:runtime:bind9.service",
		"daemon-reload",
		"verify-pre-enable",
		"enable-only:named.service",
		"daemon-reload",
		"verify-pre-start",
		"start-only:named.service",
		"verify-started",
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("activation events = %#v, want %#v", events, want)
	}
}

func TestActivateBINDTargetPreservesAlreadyEnabledVerifiedAlias(t *testing.T) {
	var events []string
	enables := 0
	ops := bindActivationOps{
		unmask: func(_ context.Context, unit string, runtime bool) error {
			kind := "persistent"
			if runtime {
				kind = "runtime"
			}
			events = append(events, "unmask:"+kind+":"+unit)
			return nil
		},
		daemonReload: func(context.Context) error {
			events = append(events, "daemon-reload")
			return nil
		},
		verifyPreEnable: func(context.Context) (bindBeforeEnableDisposition, error) {
			events = append(events, "verify-before-enable")
			return bindBeforeEnableAlreadyEnabled, nil
		},
		enable: func(context.Context, string) error {
			enables++
			return nil
		},
		verifyPreStart: func(context.Context) error {
			events = append(events, "verify-pre-start")
			return nil
		},
		start: func(_ context.Context, unit string) error {
			events = append(events, "start-only:"+unit)
			return nil
		},
		verifyStarted: func(context.Context) error {
			events = append(events, "verify-started")
			return nil
		},
	}
	if err := activateBINDTargetWithOps(context.Background(), "named.service", ops); err != nil {
		t.Fatal(err)
	}
	if enables != 0 {
		t.Fatalf("enable calls = %d, want 0 for an exact already-enabled alias", enables)
	}
	want := []string{
		"unmask:persistent:named.service",
		"unmask:runtime:named.service",
		"unmask:persistent:bind9.service",
		"unmask:runtime:bind9.service",
		"daemon-reload",
		"verify-before-enable",
		"daemon-reload",
		"verify-pre-start",
		"start-only:named.service",
		"verify-started",
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("activation events = %#v, want %#v", events, want)
	}
}

func TestVerifyBINDBeforeEnableIdentitySelectsOnlyExactDisposition(t *testing.T) {
	profile := testUbuntuBINDProfile()
	stockNamed := bindInstallUnitState{
		loadState: "loaded", activeState: "inactive", unitFileState: "disabled",
	}
	absentAlias := bindInstallUnitState{
		loadState: "not-found", activeState: "inactive",
	}
	enabled := bindInstallUnitState{
		loadState: "loaded", activeState: "inactive", unitFileState: "enabled",
	}

	t.Run("stock-disabled", func(t *testing.T) {
		disabledProofs, enabledProofs := 0, 0
		disposition, err := verifyBINDBeforeEnableIdentityWithOps(
			profile, stockNamed, absentAlias,
			func() error { disabledProofs++; return nil },
			func() error { enabledProofs++; return nil },
		)
		if err != nil || disposition != bindBeforeEnableNeedsEnable {
			t.Fatalf("disposition=%d err=%v, want needs-enable", disposition, err)
		}
		if disabledProofs != 1 || enabledProofs != 0 {
			t.Fatalf("proofs disabled=%d enabled=%d", disabledProofs, enabledProofs)
		}
	})
	t.Run("already-enabled", func(t *testing.T) {
		disabledProofs, enabledProofs := 0, 0
		disposition, err := verifyBINDBeforeEnableIdentityWithOps(
			profile, enabled, enabled,
			func() error { disabledProofs++; return nil },
			func() error { enabledProofs++; return nil },
		)
		if err != nil || disposition != bindBeforeEnableAlreadyEnabled {
			t.Fatalf("disposition=%d err=%v, want already-enabled", disposition, err)
		}
		if disabledProofs != 0 || enabledProofs != 1 {
			t.Fatalf("proofs disabled=%d enabled=%d", disabledProofs, enabledProofs)
		}
	})
	for _, test := range []struct {
		name         string
		named, alias bindInstallUnitState
	}{
		{name: "enabled-named-missing-alias", named: enabled, alias: absentAlias},
		{name: "disabled-named-enabled-alias", named: stockNamed, alias: enabled},
		{name: "active", named: bindInstallUnitState{
			loadState: "loaded", activeState: "active", unitFileState: "enabled",
		}, alias: enabled},
	} {
		t.Run(test.name, func(t *testing.T) {
			proofs := 0
			if _, err := verifyBINDBeforeEnableIdentityWithOps(
				profile, test.named, test.alias,
				func() error { proofs++; return nil },
				func() error { proofs++; return nil },
			); err == nil {
				t.Fatal("unsafe before-enable state was accepted")
			}
			if proofs != 0 {
				t.Fatalf("proof calls = %d, want 0 before exact state classification", proofs)
			}
		})
	}
	t.Run("enabled-proof-failure", func(t *testing.T) {
		if _, err := verifyBINDBeforeEnableIdentityWithOps(
			profile, enabled, enabled,
			func() error { return nil },
			func() error { return errors.New("alias drift") },
		); err == nil || !strings.Contains(err.Error(), "alias drift") {
			t.Fatalf("error = %v, want alias drift", err)
		}
	})
}

func TestActivateBINDTargetNeverEnablesOrStartsAfterPreEnableIdentityFailure(t *testing.T) {
	enables, starts := 0, 0
	err := activateBINDTargetWithOps(context.Background(), "named.service", bindActivationOps{
		unmask:       func(context.Context, string, bool) error { return nil },
		daemonReload: func(context.Context) error { return nil },
		verifyPreEnable: func(context.Context) (bindBeforeEnableDisposition, error) {
			return 0, errors.New("identity spoof")
		},
		enable: func(context.Context, string) error {
			enables++
			return nil
		},
		verifyPreStart: func(context.Context) error { return nil },
		start: func(context.Context, string) error {
			starts++
			return nil
		},
		verifyStarted: func(context.Context) error { return nil },
	})
	if err == nil || !strings.Contains(err.Error(), "identity spoof") {
		t.Fatalf("activation error = %v, want identity failure", err)
	}
	if enables != 0 || starts != 0 {
		t.Fatalf("enable calls=%d start calls=%d, want 0", enables, starts)
	}
}

func TestActivateBINDTargetNeverStartsBeforePostEnableAliasProof(t *testing.T) {
	starts := 0
	err := activateBINDTargetWithOps(context.Background(), "named.service", bindActivationOps{
		unmask:       func(context.Context, string, bool) error { return nil },
		daemonReload: func(context.Context) error { return nil },
		verifyPreEnable: func(context.Context) (bindBeforeEnableDisposition, error) {
			return bindBeforeEnableNeedsEnable, nil
		},
		enable:         func(context.Context, string) error { return nil },
		verifyPreStart: func(context.Context) error { return errors.New("alias spoof") },
		start: func(context.Context, string) error {
			starts++
			return nil
		},
		verifyStarted: func(context.Context) error { return nil },
	})
	if err == nil || !strings.Contains(err.Error(), "alias spoof") {
		t.Fatalf("activation error = %v, want alias failure", err)
	}
	if starts != 0 {
		t.Fatalf("start calls = %d, want 0", starts)
	}
}

func TestActivateBINDTargetNeverContinuesAfterDaemonReloadFailure(t *testing.T) {
	verifies, enables, starts := 0, 0, 0
	err := activateBINDTargetWithOps(context.Background(), "named.service", bindActivationOps{
		unmask:       func(context.Context, string, bool) error { return nil },
		daemonReload: func(context.Context) error { return errors.New("reload failed") },
		verifyPreEnable: func(context.Context) (bindBeforeEnableDisposition, error) {
			verifies++
			return bindBeforeEnableNeedsEnable, nil
		},
		enable: func(context.Context, string) error {
			enables++
			return nil
		},
		verifyPreStart: func(context.Context) error {
			verifies++
			return nil
		},
		start: func(context.Context, string) error {
			starts++
			return nil
		},
		verifyStarted: func(context.Context) error {
			verifies++
			return nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "reload failed") {
		t.Fatalf("activation error = %v, want reload failure", err)
	}
	if verifies != 0 || enables != 0 || starts != 0 {
		t.Fatalf(
			"continued after daemon-reload failure: verifies=%d enables=%d starts=%d",
			verifies, enables, starts,
		)
	}
}

func TestActivateBINDTargetRejectsInvalidOperations(t *testing.T) {
	if err := activateBINDTargetWithOps(context.Background(), "bind9.service", bindActivationOps{}); err == nil {
		t.Fatal("invalid BIND activation was accepted")
	}
}
