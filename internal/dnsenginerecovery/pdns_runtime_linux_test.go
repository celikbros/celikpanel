//go:build linux

package dnsenginerecovery

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/hostplatform"
)

func pdnsProfile() hostplatform.Profile {
	return hostplatform.Profile{
		DistroFamily:   hostplatform.DistroFamilyDebian,
		PackageManager: hostplatform.PackageManagerAPT,
		ServiceManager: hostplatform.ServiceManagerSystemd,
	}
}

func pdnsUnitFixture(name, load, active, file string) []byte {
	return []byte("Id=" + name + "\nNames=" + name + "\nLoadState=" + load +
		"\nActiveState=" + active + "\nUnitFileState=" + file + "\n")
}

func pdnsProcessFixture(pid, substate, reload string) []byte {
	return []byte("MainPID=" + pid + "\nControlPID=0\nSubState=" + substate +
		"\nNeedDaemonReload=" + reload + "\n")
}

func TestProbePDNSRuntimeRequiresStableNativeTopology(t *testing.T) {
	unitCalls, processCalls := 0, 0
	units := func(_ context.Context, name string) ([]byte, error) {
		unitCalls++
		switch name {
		case "named.service", "bind9.service":
			return pdnsUnitFixture(name, "masked", "inactive", "masked"), nil
		case "pdns.service":
			return pdnsUnitFixture(name, "loaded", "active", "enabled"), nil
		default:
			t.Fatalf("unexpected unit %s", name)
			return nil, nil
		}
	}
	processes := func(_ context.Context, name string) ([]byte, error) {
		processCalls++
		if name == "pdns.service" {
			return pdnsProcessFixture("1234", "running", "no"), nil
		}
		return pdnsProcessFixture("0", "dead", "no"), nil
	}
	pid, err := ProbePDNSRuntime(context.Background(), pdnsProfile(), units, processes)
	if err != nil || pid != 1234 || unitCalls != 6 || processCalls != 6 {
		t.Fatalf("topology: pid=%d err=%v unitCalls=%d processCalls=%d", pid, err, unitCalls, processCalls)
	}
}

func TestProbePDNSRuntimeRejectsAmbiguousNativeState(t *testing.T) {
	calls := 0
	goodUnits := func(_ context.Context, name string) ([]byte, error) {
		if name == "pdns.service" {
			return pdnsUnitFixture(name, "loaded", "active", "enabled"), nil
		}
		return pdnsUnitFixture(name, "loaded", "inactive", "disabled"), nil
	}
	goodProcesses := func(_ context.Context, name string) ([]byte, error) {
		if name == "pdns.service" {
			return pdnsProcessFixture("1234", "running", "no"), nil
		}
		return pdnsProcessFixture("0", "dead", "no"), nil
	}
	cases := []struct {
		name      string
		units     NativeUnitRunner
		processes BINDRuntimeRunner
	}{
		{"foreign-active-bind", func(ctx context.Context, name string) ([]byte, error) {
			if name == "named.service" {
				return pdnsUnitFixture(name, "loaded", "active", "enabled"), nil
			}
			return goodUnits(ctx, name)
		}, goodProcesses},
		{"changed-pdns-pid", goodUnits, func(ctx context.Context, name string) ([]byte, error) {
			if name != "pdns.service" {
				return goodProcesses(ctx, name)
			}
			calls++
			if calls == 2 {
				return pdnsProcessFixture("5678", "running", "no"), nil
			}
			return goodProcesses(ctx, name)
		}},
		{"pending-reload", goodUnits, func(ctx context.Context, name string) ([]byte, error) {
			if name == "pdns.service" {
				return pdnsProcessFixture("1234", "running", "yes"), nil
			}
			return goodProcesses(ctx, name)
		}},
		{"stopped-pdns", goodUnits, func(ctx context.Context, name string) ([]byte, error) {
			if name == "pdns.service" {
				return pdnsProcessFixture("0", "dead", "no"), nil
			}
			return goodProcesses(ctx, name)
		}},
		{"unknown-unit", func(context.Context, string) ([]byte, error) {
			return nil, errors.New("systemd unavailable")
		}, goodProcesses},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ProbePDNSRuntime(context.Background(), pdnsProfile(), test.units, test.processes); err == nil {
				t.Fatal("ambiguous native topology accepted")
			}
		})
	}
	badProfile := pdnsProfile()
	badProfile.PackageManager = hostplatform.PackageManagerPacman
	calls = 0
	if _, err := ProbePDNSRuntime(context.Background(), badProfile,
		func(context.Context, string) ([]byte, error) { calls++; return nil, nil }, goodProcesses); err == nil || calls != 0 {
		t.Fatalf("unsupported profile accepted: err=%v calls=%d", err, calls)
	}
	if _, err := SystemdPDNSRuntimeRunner(nil, "pdns.service"); err == nil {
		t.Fatal("nil context accepted")
	}
	if _, err := SystemdPDNSRuntimeRunner(context.Background(), "sshd.service"); err == nil {
		t.Fatal("arbitrary unit accepted")
	}
	if got := stoppedBINDUnit(NativeUnitObservation{LoadState: "loaded", ActiveState: "inactive", UnitFileState: "masked-runtime"}); !got {
		t.Fatal("runtime mask should count as stopped")
	}
	if got := stoppedBINDUnit(NativeUnitObservation{LoadState: "masked", ActiveState: "active", UnitFileState: "masked"}); got {
		t.Fatal("active mask should not count as stopped")
	}
	if !strings.Contains(string(pdnsProcessFixture("1", "running", "no")), "NeedDaemonReload=no") {
		t.Fatal("fixture broken")
	}
}
