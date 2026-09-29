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
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
	"github.com/alicelik/celikpanel/internal/transport"
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
	// localListeners adds loopback or link-local port-53 rows to the
	// inventory; cgroups maps a PID to its cgroup-v2 path.
	localListeners []string
	cgroups        map[uint64]string
	// generation is the staged target tree: "none", "staged" (exactly what
	// the journal staged) or "retained" (owner-modified).
	generation   string
	runtimeFiles []string
	systemctl    []string
	effects      []string
	nextPID      uint64
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
		pointerTarget: pointerTarget, config: config, nextPID: 5000, generation: "none",
		cgroups: map[uint64]string{370: "/system.slice/systemd-resolved.service"},
	}
}

func (f *neverStartedBINDHost) cgroup(_ context.Context, pid uint64) (string, error) {
	path, ok := f.cgroups[pid]
	if !ok {
		return "", fmt.Errorf("process %d cgroup unknown", pid)
	}
	return path, nil
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
	rows = append(rows, f.localListeners...)
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
	case "mask":
		if args[1] == "--runtime" {
			return nil, errors.New("runtime mask is not the guard's seal")
		}
		if u.file == "enabled" || u.active == "active" {
			// systemctl refuses to mask over an enabled alias link; the
			// inverse must stop and disable first.
			return nil, errors.New("unit is enabled or running; refusing to mask")
		}
		u.load, u.file = "masked", "masked"
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
				cgroup:    f.cgroup,
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
		generation: func(_ context.Context, j dnsengineartifact.SwitchJournalV1, _ bindroot.Layout, _ uint32) (dnsenginerecovery.BINDGenerationResidue, string, error) {
			if !dnsenginerecovery.BINDSwitchNeverStartedTargetJournal(j) || f.pointerTarget {
				return dnsenginerecovery.BINDGenerationNone, "", nil
			}
			switch f.generation {
			case "staged":
				return dnsenginerecovery.BINDGenerationStaged, "", nil
			case "retained":
				return dnsenginerecovery.BINDGenerationRetained, "BIND generation contains unexpected top-level entries", nil
			}
			return dnsenginerecovery.BINDGenerationNone, "", nil
		},
		removeGeneration: func(ctx context.Context, j dnsengineartifact.SwitchJournalV1, layout bindroot.Layout, gid uint32) (bool, error) {
			if !dnsenginerecovery.BINDSwitchNeverStartedTargetJournal(j) || f.pointerTarget || f.generation != "staged" {
				return false, nil
			}
			f.effects = append(f.effects, "remove staged BIND generation")
			f.generation = "none"
			return true, nil
		},
		runtimeFiles: func(bindroot.Layout) (string, []string, error) {
			return "/var/cache/bind", f.runtimeFiles, nil
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
		{
			// The verified source PowerDNS also holds loopback and link-local
			// sockets beside the resolver stub; both are accepted.
			name: "source-owned-local-listeners",
			host: func() *neverStartedBINDHost {
				f := newNeverStartedBINDHost(false, "before", false)
				f.localListeners = []string{
					`udp UNCONN 0 0 127.0.0.1:53 0.0.0.0:* users:(("pdns_server",pid=3066,fd=9))`,
					`tcp LISTEN 0 128 [fe80::5054:ff:fe13:10]%mgmt0:53 [::]:* users:(("pdns_server",pid=3066,fd=10))`,
				}
				return f
			},
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
		// Loopback and link-local sockets are no longer skipped.
		{name: "named-on-loopback", change: func(f *neverStartedBINDHost) {
			f.localListeners = []string{`udp UNCONN 0 0 127.0.0.1:53 0.0.0.0:* users:(("named",pid=4242,fd=18))`}
		}, want: "named process (PID 4242) holds local port-53 listener 127.0.0.1"},
		{name: "named-on-ipv6-loopback", change: func(f *neverStartedBINDHost) {
			f.localListeners = []string{`tcp LISTEN 0 10 [::1]:53 [::]:* users:(("named",pid=4242,fd=34))`}
		}, want: "holds local port-53 listener ::1 outside the verified DNS source"},
		{name: "named-on-link-local", change: func(f *neverStartedBINDHost) {
			f.localListeners = []string{`tcp LISTEN 0 10 [fe80::5054:ff:fe13:10]%mgmt0:53 [::]:* users:(("named",pid=4242,fd=42))`}
		}, want: "outside the verified DNS source"},
		{name: "other-pdns-on-loopback", change: func(f *neverStartedBINDHost) {
			f.localListeners = []string{`udp UNCONN 0 0 127.0.0.1:53 0.0.0.0:* users:(("pdns_server",pid=9999,fd=9))`}
		}, want: "pdns_server process (PID 9999)"},
		{name: "unknown-on-loopback", change: func(f *neverStartedBINDHost) {
			f.localListeners = []string{`udp UNCONN 0 0 127.0.1.1:53 0.0.0.0:* users:(("dnsmasq",pid=777,fd=4))`}
		}, want: "unrecognized process"},
		{name: "resolver-name-outside-its-cgroup", change: func(f *neverStartedBINDHost) {
			f.localListeners = []string{`udp UNCONN 0 0 127.0.0.53%lo:53 0.0.0.0:* users:(("systemd-resolve",pid=999,fd=4))`}
			f.cgroups[999] = "/system.slice/impostor.service"
		}, want: "is not in systemd-resolved.service"},
		{name: "resolver-name-on-other-address", change: func(f *neverStartedBINDHost) {
			f.localListeners = []string{`udp UNCONN 0 0 127.0.0.1:53 0.0.0.0:* users:(("systemd-resolve",pid=370,fd=30))`}
		}, want: "unrecognized process"},
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
// bind9 alias absent) keeps the loaded-unit proof and dnsunitrestore's
// absent-preimage compensation, and then ends under the guard's persistent
// mask like every created-BIND rollback. named is never started.
func TestOwnerBINDSwitchInverseSealsLiftedTarget(t *testing.T) {
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
		if strings.Contains(call, "named.service") && strings.HasPrefix(call, "start") {
			t.Fatalf("rollback started the target: %q", call)
		}
	}
	requireGuardSealed(t, f)
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
		func() error { return nil }, false)
	if err != nil || strings.Contains(line, "incomplete DNS unit identity") ||
		!strings.Contains(line, "persistent mask") || !reflect.DeepEqual(proved, []string{"named.service", "bind9.service"}) {
		t.Fatalf("masked target status = %q, %v (mask proofs %q)", line, err, proved)
	}
	if _, err := observeNeverStartedBINDTarget(context.Background(), masked, func(context.Context) error { return nil },
		func(string) error { return errors.New("mask link is not root-owned") }, func() error { return nil }, false); err == nil {
		t.Fatal("unproved mask link accepted")
	}
	mixed := func(ctx context.Context, name string) ([]byte, error) {
		if name == "bind9.service" {
			return []byte("Id=bind9.service\nNames=bind9.service\nLoadState=not-found\nUnitFileState=\nFragmentPath=\n"), nil
		}
		return masked(ctx, name)
	}
	if _, err := observeNeverStartedBINDTarget(context.Background(), mixed, func(context.Context) error { return nil },
		func(string) error { return nil }, func() error { return nil }, false); err == nil {
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
		func(string) error { t.Fatal("mask proof used for a loaded target"); return nil }, func() error { return nil }, false); err != nil || line != "" || loadedCalls != 1 {
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

// stageBeforeDecisionBINDSwitch is the state when the Agent was cut at a
// pre-start phase and has not restarted: the V2 journal at that forward phase
// beside the still-leased ledger job, the PowerDNS source receipt in place.
func stageBeforeDecisionBINDSwitch(t *testing.T, phase string) (releasedInverseHost, dnsengineartifact.SwitchJournalV1) {
	t.Helper()
	h := newReleasedInverseHost(t)
	j := releasedBINDSwitchJournal(t, h)
	j.Phase = phase
	h.write(t, "dns-engine-state.json", j.StateBefore.Data)
	raw, err := h.policy.EncodeSwitchJournal(j)
	if err != nil {
		t.Fatal(err)
	}
	h.write(t, "dns-engine-switch-journal.json", raw)
	now := time.Now().UTC().Truncate(time.Second)
	job := &transport.ServiceMutationJob{
		RequestID: j.MutationRequestID, OwnerID: j.MutationOwnerID,
		Kind: "dns_engine_switch", Target: string(j.TargetEngine), PackageName: j.ManifestQualifier,
		Status: servicemutationledger.StatusRunning, Phase: "leased", Attempt: 1,
		StartedAt: now.Add(-time.Minute), UpdatedAt: now.Add(-time.Minute),
		LeaseExpiresAt: now.Add(time.Hour), DeadlineAt: now.Add(2 * time.Hour),
	}
	ledger := servicemutationledger.Ledger{Version: servicemutationledger.Version, ActiveRequestID: job.RequestID,
		Jobs: map[string]*transport.ServiceMutationJob{job.RequestID: job}}
	ledgerRaw, err := servicemutationledger.Encode(&ledger)
	if err != nil {
		t.Fatal(err)
	}
	h.write(t, "service-mutations.json", ledgerRaw)
	return h, j
}

// Before the rollback decision, status reads a guard-masked target with the
// typed never-started observation, names the Agent restart and the status
// re-run, and never names an owner inverse command, which is not admitted.
func TestDNSSwitchStatusBeforeRollbackDecisionObservesNeverStartedTarget(t *testing.T) {
	for _, phase := range []string{dnsengineartifact.SwitchPhaseIntent, dnsengineartifact.SwitchPhaseTargetStaged, dnsengineartifact.SwitchPhaseSourceStopped} {
		t.Run(phase, func(t *testing.T) {
			h, j := stageBeforeDecisionBINDSwitch(t, phase)
			before := h.files(t)
			evidence, present, err := dnsenginerecovery.ReadSwitchEvidence(h.root, h.owner, h.policy, time.Now().UTC())
			if err != nil || !present || evidence.Observation.Status != dnsenginerecovery.EvidenceActive {
				t.Fatalf("fixture is not an active pre-decision journal: present=%v status=%q err=%v", present, evidence.Observation.Status, err)
			}
			if evidence.Observation.TargetReceipt == dnsenginerecovery.TargetReceiptExact ||
				!dnsenginerecovery.BINDSwitchNeverStartedBeforeDecisionJournal(evidence.Journal) ||
				dnsenginerecovery.BINDSwitchNeverStartedTargetJournal(evidence.Journal) {
				t.Fatal("status would not select the before-decision never-started observation")
			}
			if got := ownerDNSRecoveryCommand(evidence); got != "" {
				t.Fatalf("status named %q before the rollback decision", got)
			}
			for _, quiesced := range []bool{false, true} {
				guidance := ownerDNSRecoveryGuidance(evidence, quiesced)
				if strings.Contains(guidance, "recover-dns-") || !strings.Contains(guidance, "no rollback decision yet (journal phase "+phase+")") ||
					!strings.Contains(guidance, "systemctl restart celikpanel-agent") ||
					!strings.Contains(guidance, "--quiesced --request-id "+j.MutationRequestID) ||
					strings.Contains(guidance, "contact support") {
					t.Fatalf("before-decision guidance is wrong:\n%s", guidance)
				}
			}
			if !reflect.DeepEqual(h.files(t), before) {
				t.Fatal("status observation changed the evidence")
			}
		})
	}
	// A journal whose phase records a started BIND keeps the strict check.
	h, _ := stageBeforeDecisionBINDSwitch(t, dnsengineartifact.SwitchPhaseTargetStarted)
	evidence, _, err := dnsenginerecovery.ReadSwitchEvidence(h.root, h.owner, h.policy, time.Now().UTC())
	if err != nil || dnsenginerecovery.BINDSwitchNeverStartedBeforeDecisionJournal(evidence.Journal) ||
		!dnsenginerecovery.BINDSwitchBeforeRollbackDecisionJournal(evidence.Journal) {
		t.Fatalf("target-started journal entered the never-started class: %v", err)
	}
	// A released pre-decision journal keeps the existing no-command guidance.
	released := ownerGuidanceReleased(ownerGuidanceBINDSwitchEvidence(), dnsengineartifact.ReleasedHostWindowCode)
	released.Journal.Phase, released.Observation.Phase = dnsengineartifact.SwitchPhaseTargetStaged, dnsengineartifact.SwitchPhaseTargetStaged
	if got := ownerDNSRecoveryGuidance(released, true); strings.Contains(got, "systemctl restart celikpanel-agent") ||
		!strings.Contains(got, "No owner recovery command applies") {
		t.Fatalf("released pre-decision journal got before-decision guidance:\n%s", got)
	}
}

func TestNeverStartedBINDTargetBeforeDecisionTextNamesNoOwnerCommand(t *testing.T) {
	masked := func(_ context.Context, name string) ([]byte, error) {
		return []byte("Id=" + name + "\nNames=" + name + "\nLoadState=masked\nUnitFileState=masked\nFragmentPath=/etc/systemd/system/" + name + "\nDropInPaths=\nSourcePath=\nTransient=no\n"), nil
	}
	absent := func(_ context.Context, name string) ([]byte, error) {
		return []byte("Id=" + name + "\nNames=" + name + "\nLoadState=not-found\nUnitFileState=\nFragmentPath=\n"), nil
	}
	for name, runner := range map[string]dnsenginerecovery.BINDIdentityRunner{"masked": masked, "absent": absent} {
		line, err := observeNeverStartedBINDTarget(context.Background(), runner,
			func(context.Context) error { t.Fatal("loaded identity proof used"); return nil },
			func(string) error { return nil }, func() error { return nil }, true)
		if err != nil || !strings.Contains(line, "BIND never started for this operation") ||
			strings.Contains(line, "owner recovery command") || strings.Contains(line, "rollback standby") ||
			!strings.Contains(line, "not proved by this observation") {
			t.Fatalf("%s: before-decision line = %q, %v", name, line, err)
		}
	}

	request := strings.Repeat("a", 32)
	inactive := &dnsenginerecovery.NativeUnitObservation{Name: "pdns.service", LoadState: "loaded", ActiveState: "inactive", UnitFileState: "disabled"}
	active := &dnsenginerecovery.NativeUnitObservation{Name: "pdns.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"}
	for _, tc := range []struct {
		name       string
		workerGone bool
		pdns       *dnsenginerecovery.NativeUnitObservation
		want       []string
		unwanted   []string
	}{
		{"interrupted-source-stopped", true, inactive,
			[]string{"was interrupted before BIND started", "phase source-stopped", "recorded Agent worker is gone", "load=loaded active=inactive unit-file=disabled", "does not answer DNS", "Next step: the server owner restarts the CelikPanel Agent"},
			[]string{"cannot tell whether the Agent"}},
		{"worker-not-excluded", false, active,
			[]string{"BIND has not started", "cannot tell whether the Agent is still running", "load=loaded active=active unit-file=enabled", "If CelikPanel shows no progress"},
			[]string{"interrupted", "does not answer DNS"}},
		{"units-unknown", true, nil, []string{"PowerDNS unit state could not be read"}, nil},
	} {
		text := beforeRollbackDecisionText(dnsengineartifact.SwitchPhaseSourceStopped, request, tc.workerGone, tc.pdns)
		for _, want := range append(tc.want, "systemctl restart celikpanel-agent", "records the rollback decision for this same request",
			"dns-switch-status --quiesced --request-id "+request, "No owner recovery command applies before that decision", "started nothing") {
			if !strings.Contains(text, want) {
				t.Fatalf("%s: text lacks %q:\n%s", tc.name, want, text)
			}
		}
		for _, unwanted := range append(tc.unwanted, "recover-dns-") {
			if strings.Contains(text, unwanted) {
				t.Fatalf("%s: text contains %q:\n%s", tc.name, unwanted, text)
			}
		}
	}
}
