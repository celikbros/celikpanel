//go:build linux

package dnsenginerecovery

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestDNSCgroupProofRequiresFixedEmptyNativeGroup(t *testing.T) {
	for _, name := range []string{"named.service", "pdns.service"} {
		t.Run(name, func(t *testing.T) {
			show := func(_ context.Context, unit string) ([]byte, error) {
				if unit != name {
					t.Fatalf("wrong unit %s", unit)
				}
				return []byte("Id=" + name + "\nSlice=system.slice\nControlGroup=/system.slice/" + name + "\n"), nil
			}
			empty := func(_ context.Context, unit string) ([]byte, bool, error) {
				if unit != name {
					t.Fatalf("wrong cgroup %s", unit)
				}
				return []byte("populated 0\nfrozen 0\n"), true, nil
			}
			if err := ProbeEmptyUnitCgroup(context.Background(), name, show, empty); err != nil {
				t.Fatal(err)
			}
			if err := ProbeEmptyUnitCgroup(context.Background(), name, show,
				func(context.Context, string) ([]byte, bool, error) { return nil, false, nil }); err == nil {
				t.Fatal("disappeared reported cgroup accepted")
			}
			for _, raw := range []string{
				"populated 1\nfrozen 0\n", "frozen 0\n", "populated 0\npopulated 0\n",
				"populated unknown\n", "populated 0\nforeign 0\n", strings.Repeat("a", 4097),
			} {
				if err := ProbeEmptyUnitCgroup(context.Background(), name, show,
					func(context.Context, string) ([]byte, bool, error) { return []byte(raw), true, nil }); err == nil {
					t.Fatalf("unsafe cgroup events accepted: %q", raw[:min(len(raw), 64)])
				}
			}
			for _, raw := range []string{
				"Id=" + name + "\nSlice=other.slice\nControlGroup=\n",
				"Id=" + name + "\nSlice=system.slice\nControlGroup=/system.slice/ssh.service\n",
				"Id=ssh.service\nSlice=system.slice\nControlGroup=\n",
				"Id=" + name + "\nSlice=system.slice\nControlGroup=\nControlGroup=\n",
			} {
				if err := ProbeEmptyUnitCgroup(context.Background(), name,
					func(context.Context, string) ([]byte, error) { return []byte(raw), nil }, empty); err == nil {
					t.Fatalf("foreign cgroup identity accepted: %q", raw)
				}
			}
			absent := func(context.Context, string) ([]byte, error) {
				return []byte("Id=" + name + "\nSlice=system.slice\nControlGroup=\n"), nil
			}
			if err := ProbeEmptyUnitCgroup(context.Background(), name, absent,
				func(context.Context, string) ([]byte, bool, error) { return nil, false, nil }); err != nil {
				t.Fatal(err)
			}
		})
	}
	called := false
	if err := ProbeEmptyUnitCgroup(context.Background(), "ssh.service",
		func(context.Context, string) ([]byte, error) { called = true; return nil, nil },
		func(context.Context, string) ([]byte, bool, error) { called = true; return nil, false, nil }); err == nil || called {
		t.Fatalf("foreign unit reached native readers: %v %v", err, called)
	}
	unavailable := errors.New("systemd down")
	if err := ProbeEmptyUnitCgroup(context.Background(), "named.service",
		func(context.Context, string) ([]byte, error) { return nil, unavailable },
		func(context.Context, string) ([]byte, bool, error) {
			t.Fatal("events read after systemd failure")
			return nil, false, nil
		}); !errors.Is(err, unavailable) {
		t.Fatalf("unavailable became success: %v", err)
	}
}
