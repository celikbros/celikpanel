//go:build linux

package dnsenginerecovery

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/hostplatform"
)

func bindRuntimeFixture(pid, reload string) []byte {
	return []byte("MainPID=" + pid + "\nControlPID=0\nSubState=running\nNeedDaemonReload=" + reload + "\n")
}

func TestProbeBINDVendorRuntimeRequiresStableAliasAndReload(t *testing.T) {
	profile := hostplatform.Profile{DistroFamily: hostplatform.DistroFamilyDebian,
		PackageManager: hostplatform.PackageManagerAPT, ServiceManager: hostplatform.ServiceManagerSystemd}
	calls := 0
	good := bindRuntimeFixture("1234", "no")
	got, err := ProbeBINDVendorRuntime(context.Background(), profile, func(_ context.Context, name string) ([]byte, error) {
		calls++
		if name != "named.service" && name != "bind9.service" {
			t.Fatalf("unexpected unit %s", name)
		}
		return good, nil
	})
	if err != nil || got.MainPID != 1234 || calls != 4 {
		t.Fatalf("runtime observation: %+v %v calls=%d", got, err, calls)
	}
	if _, err := ProbeBINDVendorRuntime(context.Background(), profile, func(_ context.Context, name string) ([]byte, error) {
		if name == "bind9.service" {
			return bindRuntimeFixture("5678", "no"), nil
		}
		return good, nil
	}); err == nil || !strings.Contains(err.Error(), "alias") {
		t.Fatalf("divergent alias accepted: %v", err)
	}
	runtimeCalls := 0
	if _, err := ProbeBINDVendorRuntime(context.Background(), profile, func(_ context.Context, name string) ([]byte, error) {
		runtimeCalls++
		if runtimeCalls >= 3 {
			return bindRuntimeFixture("5678", "no"), nil
		}
		return good, nil
	}); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("runtime drift accepted: %v", err)
	}
}

func TestProbeBINDVendorRuntimeRejectsUnknowns(t *testing.T) {
	profile := hostplatform.Profile{DistroFamily: hostplatform.DistroFamilyArch,
		PackageManager: hostplatform.PackageManagerPacman, ServiceManager: hostplatform.ServiceManagerSystemd}
	for _, raw := range [][]byte{
		bindRuntimeFixture("0", "no"), bindRuntimeFixture("1", "yes"),
		[]byte("MainPID=1\nControlPID=2\nSubState=running\nNeedDaemonReload=no\n"),
		[]byte("MainPID=01\nControlPID=0\nSubState=running\nNeedDaemonReload=no\n"),
		[]byte("MainPID=1\nControlPID=0\nSubState=dead\nNeedDaemonReload=no\n"),
		[]byte("MainPID=1\nControlPID=0\nSubState=running\n"),
		[]byte("MainPID=1\nControlPID=0\nSubState=running\nNeedDaemonReload=no\nNeedDaemonReload=no\n"),
		[]byte("MainPID=1\nControlPID=0\nSubState=running\nNeedDaemonReload=no\nActiveState=active\n"),
	} {
		if _, err := ProbeBINDVendorRuntime(context.Background(), profile, func(_ context.Context, name string) ([]byte, error) {
			if name != "named.service" {
				t.Fatalf("unexpected pacman alias %s", name)
			}
			return raw, nil
		}); err == nil {
			t.Fatalf("unsafe runtime accepted: %q", raw)
		}
	}
	if _, err := ProbeBINDVendorRuntime(context.Background(), profile, func(context.Context, string) ([]byte, error) {
		return nil, errors.New("systemd unavailable")
	}); err == nil {
		t.Fatal("failed systemd query accepted")
	}
	if _, err := SystemdBINDRuntimeRunner(context.Background(), "sshd.service"); err == nil {
		t.Fatal("arbitrary service accepted")
	}
	if _, err := SystemdBINDRuntimeRunner(nil, "named.service"); err == nil {
		t.Fatal("nil context accepted")
	}
}
