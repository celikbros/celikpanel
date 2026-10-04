//go:build linux

package dnsenginerecovery

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIndependentStoppedUnitProbeUsesFixedNativeEvidence(t *testing.T) {
	for _, name := range []string{"named.service", "pdns.service"} {
		t.Run(name, func(t *testing.T) {
			unitCalls, processCalls, cgroupCalls := 0, 0, 0
			unitRunner := func(_ context.Context, actual string) ([]byte, error) {
				unitCalls++
				if actual != name {
					t.Fatalf("unexpected native unit %s", actual)
				}
				return []byte("Id=" + name + "\nNames=" + name + "\nLoadState=loaded\nActiveState=inactive\nUnitFileState=disabled\n"), nil
			}
			runtimeRunner := func(_ context.Context, actual string) ([]byte, error) {
				processCalls++
				if actual != name {
					t.Fatalf("unexpected native process unit %s", actual)
				}
				return []byte("MainPID=0\nControlPID=0\nSubState=dead\nNeedDaemonReload=no\n"), nil
			}
			if err := probeStoppedUnitWithCgroup(context.Background(), name, unitRunner, runtimeRunner,
				func(_ context.Context, actual string) error {
					cgroupCalls++
					if actual != name {
						t.Fatalf("wrong cgroup unit %s", actual)
					}
					return nil
				}); err != nil {
				t.Fatal(err)
			}
			if unitCalls != 2 || processCalls != 2 || cgroupCalls != 2 {
				t.Fatalf("native reads = %d/%d/%d, want 2/2/2", unitCalls, processCalls, cgroupCalls)
			}
			cgroupCalls = 0
			if err := probeStoppedUnitWithCgroup(context.Background(), name, unitRunner, runtimeRunner,
				func(context.Context, string) error {
					cgroupCalls++
					if cgroupCalls == 2 {
						return errors.New("cgroup became populated")
					}
					return nil
				}); err == nil {
				t.Fatal("cgroup drift was accepted")
			}
			processCalls = 0
			if err := probeStoppedUnitWithCgroup(context.Background(), name, unitRunner,
				func(_ context.Context, _ string) ([]byte, error) {
					processCalls++
					if processCalls == 2 {
						return []byte("MainPID=77\nControlPID=0\nSubState=dead\nNeedDaemonReload=no\n"), nil
					}
					return []byte("MainPID=0\nControlPID=0\nSubState=dead\nNeedDaemonReload=no\n"), nil
				},
				func(context.Context, string) error { return nil },
			); err == nil {
				t.Fatal("process appearing on second read was accepted")
			}
			if err := probeStoppedUnitWithCgroup(context.Background(), name, unitRunner,
				func(context.Context, string) ([]byte, error) {
					return []byte("MainPID=0\nControlPID=0\nSubState=dead\nNeedDaemonReload=yes\n"), nil
				},
				func(context.Context, string) error { return nil },
			); err == nil {
				t.Fatal("pending daemon reload was accepted")
			}
			if err := probeStoppedUnitWithCgroup(context.Background(), name,
				func(_ context.Context, actual string) ([]byte, error) {
					return []byte("Id=" + actual + "\nNames=" + actual + "\nLoadState=not-found\nActiveState=inactive\nUnitFileState=\n"), nil
				}, runtimeRunner,
				func(context.Context, string) error { return nil },
			); err == nil {
				t.Fatal("missing native unit was accepted as stopped")
			}
		})
	}
	called := false
	if err := probeStoppedUnitWithCgroup(context.Background(), "ssh.service",
		func(context.Context, string) ([]byte, error) { called = true; return nil, nil },
		func(context.Context, string) ([]byte, error) { called = true; return nil, nil },
		func(context.Context, string) error { called = true; return nil },
	); err == nil || called {
		t.Fatalf("foreign unit reached systemd: %v, %v", err, called)
	}
	unavailable := errors.New("systemd unavailable")
	if err := probeStoppedUnitWithCgroup(context.Background(), "named.service",
		func(context.Context, string) ([]byte, error) { return nil, unavailable },
		func(context.Context, string) ([]byte, error) {
			t.Fatal("runtime ran after failed unit read")
			return nil, nil
		},
		func(context.Context, string) error { t.Fatal("cgroup ran after failed unit read"); return nil },
	); !errors.Is(err, unavailable) {
		t.Fatalf("unknown unit state became success: %v", err)
	}
	if err := probeStoppedUnitWithCgroup(context.Background(), "pdns.service",
		func(context.Context, string) ([]byte, error) {
			return []byte("Id=pdns.service\nNames=pdns.service\nLoadState=loaded\nActiveState=inactive\nUnitFileState=disabled\n"), nil
		},
		func(context.Context, string) ([]byte, error) { return []byte(strings.Repeat("a", 4097)), nil },
		func(context.Context, string) error { t.Fatal("cgroup ran after malformed runtime"); return nil },
	); err == nil {
		t.Fatal("oversized process observation was accepted")
	}
}

func TestNeverStartedBINDTargetProbeReadsMaskedUnitNatively(t *testing.T) {
	unit := "Id=named.service\nNames=named.service\nLoadState=masked\nActiveState=inactive\nUnitFileState=masked\n"
	runtime := "MainPID=0\nControlPID=0\nSubState=dead\nNeedDaemonReload=no\n"
	unitRunner := func(_ context.Context, name string) ([]byte, error) {
		if name != "named.service" {
			t.Fatalf("unexpected unit %s", name)
		}
		return []byte(unit), nil
	}
	runtimeRunner := func(context.Context, string) ([]byte, error) { return []byte(runtime), nil }
	cgroup := func(context.Context, string) error { return nil }
	sourceOnly := 0
	seen, err := ProbeStoppedNeverStartedBINDTarget(context.Background(), unitRunner, runtimeRunner, cgroup,
		func(context.Context) error { sourceOnly++; return nil })
	if err != nil || seen.LoadState != "masked" || sourceOnly != 2 {
		t.Fatalf("guard-masked target refused: %+v %v (source-only reads %d)", seen, err, sourceOnly)
	}
	// The unchanged loaded-unit proof still refuses the same native reading.
	if err := ProbeStoppedUnitWithCgroup(context.Background(), "named.service", unitRunner, runtimeRunner, cgroup); err == nil ||
		!strings.Contains(err.Error(), "not a loaded unit") {
		t.Fatalf("default proof accepted a masked unit: %v", err)
	}
	if _, err := ProbeStoppedNeverStartedBINDTarget(context.Background(), unitRunner, runtimeRunner,
		func(context.Context, string) error { return errors.New("DNS service cgroup still contains processes") },
		func(context.Context) error { return nil }); err == nil {
		t.Fatal("populated cgroup accepted")
	}
	if _, err := ProbeStoppedNeverStartedBINDTarget(context.Background(), unitRunner, runtimeRunner, cgroup, nil); err == nil {
		t.Fatal("missing source-only proof accepted")
	}
}

func TestNamedProcessInventoryRefusesAnyNamedProcess(t *testing.T) {
	root := t.TempDir()
	write := func(pid, comm string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(root, pid), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, pid, "comm"), []byte(comm+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("1", "systemd")
	write("3066", "pdns_server")
	write("370", "systemd-resolve")
	if err := os.MkdirAll(filepath.Join(root, "self"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "999"), 0o755); err != nil { // exited during the scan
		t.Fatal(err)
	}
	procfs := func(string) error { return nil }
	if err := probeNoProcessComm(context.Background(), root, "named", procfs); err != nil {
		t.Fatalf("clean inventory refused: %v", err)
	}
	write("4242", "named")
	if err := probeNoProcessComm(context.Background(), root, "named", procfs); err == nil || !strings.Contains(err.Error(), "4242") {
		t.Fatalf("named process accepted: %v", err)
	}
	if err := probeNoProcessComm(context.Background(), root, "named", func(string) error { return errors.New("not procfs") }); err == nil {
		t.Fatal("unverified procfs accepted")
	}
}

func TestBINDSwitchTargetObservationsRequireTwoIdenticalTypedReads(t *testing.T) {
	masked := func(name string) []byte {
		return []byte("Id=" + name + "\nNames=" + name + "\nLoadState=masked\nUnitFileState=masked\nFragmentPath=/etc/systemd/system/" + name + "\nDropInPaths=\nSourcePath=\nTransient=no\n")
	}
	got, err := ProbeBINDSwitchTargetObservations(context.Background(), func(_ context.Context, name string) ([]byte, error) {
		return masked(name), nil
	})
	if err != nil || len(got) != 2 || got[0].ID != "named.service" || got[1].ID != "bind9.service" {
		t.Fatalf("masked target observations: %+v %v", got, err)
	}
	reads := 0
	if _, err := ProbeBINDSwitchTargetObservations(context.Background(), func(_ context.Context, name string) ([]byte, error) {
		reads++
		if reads > 2 && name == "named.service" {
			return []byte("Id=named.service\nNames=named.service\nLoadState=not-found\nUnitFileState=\nFragmentPath=\n"), nil
		}
		return masked(name), nil
	}); err == nil {
		t.Fatal("target class changed between reads was accepted")
	}
	if _, err := ProbeBINDSwitchTargetObservations(context.Background(), func(_ context.Context, name string) ([]byte, error) {
		return masked("pdns.service"), nil
	}); err == nil {
		t.Fatal("observation naming another unit was accepted")
	}
}
