//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/bindroot"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
)

// These component tests drive recover-dns-bind-switch from the state the
// 2026-09-29 owner-inverse native cells left: the Agent's deliberate release
// in the ledger, the V2 journal at rolling-back, the PowerDNS source receipt
// still in place and the BIND target under the package guard's persistent
// mask. The command's own executor, durable-effect wiring, assess/restore
// sequence, stopped-target class, source-only DNS proof and dnsunitrestore
// run for real; systemd, sockets, processes, config files and the BIND
// pointer are a simulated host. They are not native evidence.

type simulatedDNSUnit struct {
	load, active, file string
	pid                uint64
}

type neverStartedBINDHost struct {
	units           map[string]*simulatedDNSUnit
	pointerTarget   bool
	config          string // before, after or owner
	namedProcess    bool
	foreignListener bool
	systemctl       []string
	effects         []string
	nextPID         uint64
}

// newNeverStartedBINDHost is the native state at a pre-start cut: both BIND
// units under the guard's persistent mask, never started; PowerDNS serving
// with MainPID 3066 unless the source was already stopped.
func newNeverStartedBINDHost(pointerTarget bool, config string, sourceStopped bool) *neverStartedBINDHost {
	source := &simulatedDNSUnit{load: "loaded", active: "active", file: "enabled", pid: 3066}
	if sourceStopped {
		source = &simulatedDNSUnit{load: "loaded", active: "inactive", file: "disabled"}
	}
	return &neverStartedBINDHost{
		units: map[string]*simulatedDNSUnit{
			"named.service": {load: "masked", active: "inactive", file: "masked"},
			"bind9.service": {load: "masked", active: "inactive", file: "masked"},
			"pdns.service":  source,
		},
		pointerTarget: pointerTarget, config: config, nextPID: 5000,
	}
}

func (f *neverStartedBINDHost) unit(name string) (*simulatedDNSUnit, error) {
	u := f.units[name]
	if u == nil {
		return nil, fmt.Errorf("unexpected unit %s", name)
	}
	return u, nil
}

func (f *neverStartedBINDHost) listeners(context.Context) ([]byte, error) {
	rows := []string{
		`udp UNCONN 0 0 127.0.0.54:53 0.0.0.0:* users:(("systemd-resolve",pid=370,fd=20))`,
		`udp UNCONN 0 0 127.0.0.53%lo:53 0.0.0.0:* users:(("systemd-resolve",pid=370,fd=18))`,
		`tcp LISTEN 0 4096 127.0.0.53%lo:53 0.0.0.0:* users:(("systemd-resolve",pid=370,fd=19))`,
	}
	if pdns := f.units["pdns.service"]; pdns.active == "active" {
		rows = append(rows,
			fmt.Sprintf(`udp UNCONN 0 0 192.0.2.10:53 0.0.0.0:* users:(("pdns_server",pid=%d,fd=6))`, pdns.pid),
			fmt.Sprintf(`tcp LISTEN 0 128 192.0.2.10:53 0.0.0.0:* users:(("pdns_server",pid=%d,fd=8))`, pdns.pid))
	}
	if f.foreignListener {
		rows = append(rows, `udp UNCONN 0 0 10.0.2.15:53 0.0.0.0:* users:(("dnsmasq",pid=777,fd=4))`)
	}
	return []byte(strings.Join(rows, "\n") + "\n"), nil
}

// runSystemd is the simulated /usr/bin/systemctl behind dnsunitrestore.
func (f *neverStartedBINDHost) runSystemd(_ context.Context, path string, args ...string) ([]byte, error) {
	if path != "/usr/bin/systemctl" || len(args) < 2 {
		return nil, errors.New("unexpected systemctl invocation")
	}
	name := args[len(args)-1]
	if args[0] == "show" {
		u, err := f.unit(args[1])
		if err != nil {
			return nil, err
		}
		return []byte(fmt.Sprintf("LoadState=%s\nActiveState=%s\nUnitFileState=%s\n", u.load, u.active, u.file)), nil
	}
	f.systemctl = append(f.systemctl, strings.Join(args, " "))
	u, err := f.unit(name)
	if err != nil {
		return nil, err
	}
	switch args[0] {
	case "unmask":
		if args[1] != "--runtime" && u.load == "masked" {
			u.load, u.file = "loaded", "disabled"
		}
	case "enable":
		u.file = "enabled"
	case "disable":
		if u.load == "loaded" {
			u.file = "disabled"
		}
	case "start":
		if u.load != "loaded" {
			return nil, errors.New("unit is masked or absent")
		}
		if u.active != "active" {
			u.active, u.pid = "active", f.nextPID
			f.nextPID++
		}
	case "stop":
		u.active, u.pid = "inactive", 0
	default:
		return nil, fmt.Errorf("unsupported systemctl %s", args[0])
	}
	return nil, nil
}

func (f *neverStartedBINDHost) native(h releasedInverseHost) bindSwitchNativeHost {
	return bindSwitchNativeHost{
		sourceProof: func(context.Context, dnsengineartifact.SwitchJournalV1) error { return nil },
		layout:      func() (bindroot.Layout, uint32, error) { return bindroot.APT, 42, nil },
		unchangedConfig: func(context.Context, dnsengineartifact.SwitchJournalV1, bindroot.Layout, uint32) error {
			return nil
		},
		pointer: func(context.Context, dnsengineartifact.SwitchJournalV1, bindroot.Layout, uint32) (bool, error) {
			return f.pointerTarget, nil
		},
		configs: func(context.Context, dnsengineartifact.SwitchJournalV1, bindroot.Layout, uint32) (bool, error) {
			if f.config == "owner" {
				return false, errors.New("named.conf.local differs from the frozen before and after images")
			}
			return f.config == "before", nil
		},
		unitRunner: func(_ context.Context, name string) ([]byte, error) {
			u, err := f.unit(name)
			if err != nil {
				return nil, err
			}
			return []byte(fmt.Sprintf("Id=%s\nNames=%s\nLoadState=%s\nActiveState=%s\nUnitFileState=%s\n", name, name, u.load, u.active, u.file)), nil
		},
		runtimeRunner: func(_ context.Context, name string) ([]byte, error) {
			u, err := f.unit(name)
			if err != nil {
				return nil, err
			}
			sub := "dead"
			if u.active == "active" {
				sub = "running"
			}
			return []byte(fmt.Sprintf("MainPID=%d\nControlPID=0\nSubState=%s\nNeedDaemonReload=no\n", u.pid, sub)), nil
		},
		cgroup: func(_ context.Context, name string) error {
			if f.units[name].active != "inactive" {
				return errors.New("DNS service cgroup still contains processes")
			}
			return nil
		},
		sourceOnly: func(ctx context.Context) error {
			return proveBINDTargetSourceOnlyDNS(ctx, bindTargetSourceOnlyDNSOps{
				noNamedProcess: func(context.Context) error {
					if f.namedProcess {
						return errors.New("a named process (PID 4242) exists beside the stopped DNS target")
					}
					return nil
				},
				sourceUnit: func(context.Context) (dnsenginerecovery.NativeUnitObservation, error) {
					u := f.units["pdns.service"]
					return dnsenginerecovery.NativeUnitObservation{Name: "pdns.service", LoadState: u.load, ActiveState: u.active, UnitFileState: u.file}, nil
				},
				sourcePID: f.pdnsPID,
				listeners: f.listeners,
			})
		},
		vendor: func(ctx context.Context, j dnsengineartifact.SwitchJournalV1, units []dnsenginerecovery.NativeUnitObservation) error {
			return bindInverseVendorProofForUnits(units,
				func(string) error { return nil }, func() error { return nil }, func() error { return nil })
		},
		pdnsRuntime: f.pdnsPID,
		answers: func(context.Context, dnsengineartifact.SwitchJournalV1, uint64) error {
			if f.units["pdns.service"].active != "active" {
				return errors.New("source PowerDNS does not answer")
			}
			return nil
		},
		activePredecessor: func(context.Context, dnsengineartifact.SwitchJournalV1, bindroot.Layout, uint32) error {
			return errors.New("no active BIND predecessor at a pre-start cut")
		},
		maskParent: func() error { return nil },
		runSystemd: f.runSystemd,
		restoreConfigs: func(context.Context, dnsengineartifact.SwitchJournalV1, bindroot.Layout, uint32) error {
			if f.config == "owner" {
				return errors.New("owner-modified BIND config")
			}
			if f.config == "after" {
				f.effects = append(f.effects, "restore BIND config before-images")
				f.config = "before"
			}
			return nil
		},
		restorePointer: func(dnsengineartifact.SwitchJournalV1, bindroot.Layout) error {
			if f.pointerTarget {
				f.effects = append(f.effects, "restore BIND predecessor pointer")
				f.pointerTarget = false
			}
			return nil
		},
		restoreReceipt: func(j dnsengineartifact.SwitchJournalV1) error {
			return dnsenginerecovery.RestoreExactBINDSwitchSourceReceipt(h.policy, h.owner, j)
		},
	}
}

func (f *neverStartedBINDHost) pdnsPID(context.Context) (uint64, error) {
	if u := f.units["pdns.service"]; u.active == "active" && u.pid != 0 {
		return u.pid, nil
	}
	return 0, errors.New("PowerDNS has no running MainPID")
}

func (f *neverStartedBINDHost) run(ctx context.Context, h releasedInverseHost, request string) error {
	native := f.native(h)
	return dnsenginerecovery.CompleteInactiveBINDSwitchInverse(ctx, bindInverseOps(h.root, h.owner, h.policy, request, false, noLocks,
		bindInverseNative{
			assess: func(ctx context.Context, j dnsengineartifact.SwitchJournalV1) (dnsenginerecovery.BINDSwitchNativeState, error) {
				return assessBINDSwitchNative(ctx, native, j)
			},
			restore: func(ctx context.Context, j dnsengineartifact.SwitchJournalV1) error {
				return restoreBINDSwitchNative(ctx, native, j)
			},
		}))
}

// stageNeverStartedBINDSwitch is the released-undecided start: the rolling-back
// V2 journal, the Agent's release, and the PowerDNS source receipt, which a
// pre-start cut never replaced.
func stageNeverStartedBINDSwitch(t *testing.T, targets []dnsengineartifact.UnitSnapshot) (releasedInverseHost, dnsengineartifact.SwitchJournalV1) {
	t.Helper()
	h := newReleasedInverseHost(t)
	j := releasedBINDSwitchJournalWithTargets(t, h, targets)
	h.write(t, "dns-engine-state.json", j.StateBefore.Data)
	h.stage(t, j, dnsengineartifact.ReleasedNativeUnknownCode)
	return h, j
}

func TestOwnerBINDSwitchInverseCompletesNeverStartedGuardMaskedTarget(t *testing.T) {
	for _, tc := range []struct {
		name          string
		host          func() *neverStartedBINDHost
		wantSystemctl []string
		wantEffects   []string
	}{
		{
			// Config written and the generation selected; PowerDNS never stopped.
			name:        "precursor-target-staged",
			host:        func() *neverStartedBINDHost { return newNeverStartedBINDHost(true, "after", false) },
			wantEffects: []string{"restore BIND config before-images", "restore BIND predecessor pointer"},
		},
		{
			// Only the package install and the staged generation preceded intent.
			name: "precursor-intent",
			host: func() *neverStartedBINDHost { return newNeverStartedBINDHost(false, "before", false) },
		},
		{
			// PowerDNS stopped and disabled; the guard mask was not lifted yet.
			name:          "precursor-source-stopped",
			host:          func() *neverStartedBINDHost { return newNeverStartedBINDHost(true, "after", true) },
			wantSystemctl: []string{"unmask pdns.service", "unmask --runtime pdns.service", "enable pdns.service", "start pdns.service"},
			wantEffects:   []string{"restore BIND config before-images", "restore BIND predecessor pointer"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, j := stageNeverStartedBINDSwitch(t, nil)
			if !dnsenginerecovery.BINDSwitchNeverStartedTargetJournal(j) {
				t.Fatal("fixture is not the never-started target journal class")
			}
			ledgerBefore := h.files(t)["service-mutations.json"]
			f := tc.host()
			sourcePID := f.units["pdns.service"].pid
			if err := f.run(context.Background(), h, j.MutationRequestID); err != nil {
				t.Fatalf("owner inverse refused the never-started target: %v", err)
			}
			after := h.files(t)
			if _, present := after["dns-engine-switch-journal.json"]; present {
				t.Fatal("completed rollback did not retire its journal")
			}
			if !bytes.Equal(after["dns-engine-state.json"], j.StateBefore.Data) {
				t.Fatal("PowerDNS source receipt changed")
			}
			if !bytes.Equal(after["service-mutations.json"], ledgerBefore) {
				t.Fatal("owner command rewrote the Agent's terminal ledger verdict")
			}
			if !reflect.DeepEqual(f.systemctl, tc.wantSystemctl) {
				t.Fatalf("systemctl mutations = %q, want %q", f.systemctl, tc.wantSystemctl)
			}
			if !reflect.DeepEqual(f.effects, tc.wantEffects) {
				t.Fatalf("native effects = %q, want %q", f.effects, tc.wantEffects)
			}
			for _, name := range []string{"named.service", "bind9.service"} {
				if u := f.units[name]; u.load != "masked" || u.file != "masked" || u.active != "inactive" {
					t.Fatalf("%s left the guard's persistent mask: %+v", name, u)
				}
			}
			pdns := f.units["pdns.service"]
			if pdns.load != "loaded" || pdns.active != "active" || pdns.file != "enabled" {
				t.Fatalf("PowerDNS source is not serving at its frozen preimage: %+v", pdns)
			}
			if sourcePID != 0 && pdns.pid != sourcePID {
				t.Fatalf("running unchanged PowerDNS was restarted: pid %d, want %d", pdns.pid, sourcePID)
			}
			if f.config != "before" || f.pointerTarget {
				t.Fatalf("BIND config or pointer not restored: config=%s pointer-target=%v", f.config, f.pointerTarget)
			}

			// Re-running the completed request changes nothing.
			systemctl, effects, files := len(f.systemctl), len(f.effects), h.files(t)
			err := f.run(context.Background(), h, j.MutationRequestID)
			if !errors.Is(err, errDNSInverseReleasedReconciled) {
				t.Fatalf("re-run did not report the already-reconciled release: %v", err)
			}
			if len(f.systemctl) != systemctl || len(f.effects) != effects || !reflect.DeepEqual(h.files(t), files) {
				t.Fatal("re-run of a completed request mutated the host")
			}
		})
	}
}

func TestOwnerBINDSwitchInverseRefusesUnprovenNeverStartedTarget(t *testing.T) {
	for _, tc := range []struct {
		name    string
		targets []dnsengineartifact.UnitSnapshot
		change  func(*neverStartedBINDHost)
		want    string
	}{
		{name: "owner-edited-staged-config", change: func(f *neverStartedBINDHost) { f.config = "owner" }, want: "differs from the frozen"},
		{name: "named-process", change: func(f *neverStartedBINDHost) { f.namedProcess = true }, want: "named process"},
		{name: "listener-not-owned-by-source", change: func(f *neverStartedBINDHost) { f.foreignListener = true }, want: "not only the source PowerDNS"},
		{name: "runtime-mask", change: func(f *neverStartedBINDHost) { f.units["named.service"].file = "masked-runtime" }, want: "differs from allowed operation states"},
		{name: "target-running", change: func(f *neverStartedBINDHost) {
			f.units["named.service"] = &simulatedDNSUnit{load: "loaded", active: "active", file: "enabled", pid: 4242}
			f.units["bind9.service"] = &simulatedDNSUnit{load: "loaded", active: "active", file: "enabled", pid: 4242}
		}, want: ""},
		{
			// BIND preexisted (loaded/disabled) so this operation did not create
			// it: the never-started class does not apply and the unchanged
			// loaded-unit proof refuses the mask.
			name: "journal-outside-class-keeps-loaded-proof",
			targets: []dnsengineartifact.UnitSnapshot{
				{Name: "bind9.service", LoadState: "not-found", ActiveState: "inactive"},
				{Name: "named.service", LoadState: "loaded", ActiveState: "inactive", UnitFileState: "disabled"},
			},
			want: "not a loaded unit",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, j := stageNeverStartedBINDSwitch(t, tc.targets)
			if tc.targets != nil && dnsenginerecovery.BINDSwitchNeverStartedTargetJournal(j) {
				t.Fatal("preexisting BIND journal admitted to the never-started class")
			}
			f := newNeverStartedBINDHost(false, "after", false)
			if tc.change != nil {
				tc.change(f)
			}
			before := h.files(t)
			err := f.run(context.Background(), h, j.MutationRequestID)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("unproven target not refused as %q: %v", tc.want, err)
			}
			if len(f.systemctl) != 0 || len(f.effects) != 0 || !reflect.DeepEqual(h.files(t), before) {
				t.Fatalf("refusal reached an effect: systemctl=%q effects=%q", f.systemctl, f.effects)
			}
			if _, present := h.files(t)["dns-engine-switch-journal.json"]; !present {
				t.Fatal("refusal did not preserve the journal")
			}
		})
	}
}

// A target already lifted from the guard but never started (loaded/disabled,
// bind9 alias absent) keeps the existing inverse: the loaded-unit proof and
// dnsunitrestore's absent-preimage compensation, unchanged by this rule.
func TestOwnerBINDSwitchInverseKeepsLoadedTargetPath(t *testing.T) {
	h, j := stageNeverStartedBINDSwitch(t, nil)
	f := newNeverStartedBINDHost(true, "after", true)
	f.units["named.service"] = &simulatedDNSUnit{load: "loaded", active: "inactive", file: "disabled"}
	f.units["bind9.service"] = &simulatedDNSUnit{load: "not-found", active: "inactive"}
	if err := f.run(context.Background(), h, j.MutationRequestID); err != nil {
		t.Fatalf("loaded never-started target refused: %v", err)
	}
	if _, present := h.files(t)["dns-engine-switch-journal.json"]; present {
		t.Fatal("journal not retired")
	}
	for _, call := range f.systemctl {
		if strings.Contains(call, "named.service") && !strings.HasPrefix(call, "unmask") && !strings.HasPrefix(call, "disable") {
			t.Fatalf("unexpected target mutation %q", call)
		}
	}
}

func TestDNSSwitchStatusObservesGuardMaskedTargetWithoutIdentityFailure(t *testing.T) {
	h, j := stageNeverStartedBINDSwitch(t, nil)
	evidence, present, err := dnsenginerecovery.ReadSwitchEvidence(h.root, h.owner, h.policy, time.Now().UTC())
	if err != nil || !present {
		t.Fatal(err)
	}
	if evidence.Observation.TargetReceipt == dnsenginerecovery.TargetReceiptExact ||
		!dnsenginerecovery.BINDSwitchNeverStartedTargetJournal(evidence.Journal) {
		t.Fatal("status would not select the never-started target observation")
	}
	text, known := releasedDNSSwitchGuidance(evidence)
	named := ownerDNSRecoveryGuidance(evidence, true)
	if !known || !strings.Contains(text, "owner recovery command named above") ||
		!strings.Contains(named, "/usr/libexec/celikpanel/recovery recover-dns-bind-switch --request-id "+j.MutationRequestID) {
		t.Fatalf("status does not name the owner command: %q / %q", named, text)
	}
	masked := func(_ context.Context, name string) ([]byte, error) {
		return []byte("Id=" + name + "\nNames=" + name + "\nLoadState=masked\nUnitFileState=masked\nFragmentPath=/etc/systemd/system/" + name + "\nDropInPaths=\nSourcePath=\nTransient=no\n"), nil
	}
	var proved []string
	line, err := observeNeverStartedBINDTarget(context.Background(), masked,
		func(context.Context) error { t.Fatal("loaded identity proof used for a masked target"); return nil },
		func(unit string) error { proved = append(proved, unit); return nil },
		func() error { return nil })
	if err != nil || strings.Contains(line, "incomplete DNS unit identity") ||
		!strings.Contains(line, "persistent mask") || !reflect.DeepEqual(proved, []string{"named.service", "bind9.service"}) {
		t.Fatalf("masked target status = %q, %v (mask proofs %q)", line, err, proved)
	}
	if _, err := observeNeverStartedBINDTarget(context.Background(), masked, func(context.Context) error { return nil },
		func(string) error { return errors.New("mask link is not root-owned") }, func() error { return nil }); err == nil {
		t.Fatal("unproved mask link accepted")
	}
	mixed := func(ctx context.Context, name string) ([]byte, error) {
		if name == "bind9.service" {
			return []byte("Id=bind9.service\nNames=bind9.service\nLoadState=not-found\nUnitFileState=\nFragmentPath=\n"), nil
		}
		return masked(ctx, name)
	}
	if _, err := observeNeverStartedBINDTarget(context.Background(), mixed, func(context.Context) error { return nil },
		func(string) error { return nil }, func() error { return nil }); err == nil {
		t.Fatal("named masked without the alias mask accepted")
	}
	loadedCalls := 0
	loaded := func(_ context.Context, name string) ([]byte, error) {
		if name == "bind9.service" {
			return []byte("Id=bind9.service\nNames=bind9.service\nLoadState=not-found\nUnitFileState=\nFragmentPath=\n"), nil
		}
		return []byte("Id=named.service\nNames=named.service\nLoadState=loaded\nUnitFileState=disabled\nFragmentPath=/usr/lib/systemd/system/named.service\nDropInPaths=\nSourcePath=\nTransient=no\nExecStart={ path=/usr/sbin/named ; argv[]=/usr/sbin/named -f $OPTIONS ; ignore_errors=no }\n"), nil
	}
	if line, err := observeNeverStartedBINDTarget(context.Background(), loaded,
		func(context.Context) error { loadedCalls++; return nil },
		func(string) error { t.Fatal("mask proof used for a loaded target"); return nil }, func() error { return nil }); err != nil || line != "" || loadedCalls != 1 {
		t.Fatalf("loaded target did not keep the unchanged identity proof: %q %v %d", line, err, loadedCalls)
	}
	// The retained journal is unchanged by the observation.
	raw, err := os.ReadFile(h.root + "/dns-engine-switch-journal.json")
	if err != nil {
		t.Fatal(err)
	}
	if encoded, _ := h.policy.EncodeSwitchJournal(j); !bytes.Equal(raw, encoded) {
		t.Fatal("status observation changed the journal")
	}
}
