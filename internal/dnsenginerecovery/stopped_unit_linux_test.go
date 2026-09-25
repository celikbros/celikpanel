//go:build linux

package dnsenginerecovery

import (
	"context"
	"errors"
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
