//go:build linux

package dnsenginerecovery

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestProbeNativeUnitsBindsAliasesAndRefusesPartialResults(t *testing.T) {
	ctx := context.Background()
	calls := 0
	runner := func(_ context.Context, name string) ([]byte, error) {
		calls++
		switch name {
		case "bind9.service":
			return []byte("Id=named.service\nNames=named.service bind9.service\nLoadState=loaded\nActiveState=active\nUnitFileState=enabled\n"), nil
		case "pdns.service":
			return []byte("Id=pdns.service\nNames=pdns.service\nLoadState=not-found\nActiveState=inactive\nUnitFileState=\n"), nil
		default:
			return nil, fmt.Errorf("unexpected unit %s", name)
		}
	}
	units, err := ProbeNativeUnits(ctx, []string{"bind9.service", "pdns.service"}, runner)
	if err != nil || len(units) != 2 || units[0].ActiveState != "active" || units[1].LoadState != "not-found" {
		t.Fatalf("native observations: %+v, %v", units, err)
	}
	for _, names := range [][]string{{"sshd.service"}, {"pdns.service", "pdns.service"}, nil} {
		calls = 0
		if _, err := ProbeNativeUnits(ctx, names, runner); err == nil || calls != 0 {
			t.Fatalf("untrusted names reached systemd: %v, %d", names, calls)
		}
	}
	badOutputs := []string{
		"Id=pdns.service\nNames=another.service\nLoadState=loaded\nActiveState=active\nUnitFileState=enabled\n",
		"Id=pdns.service\nNames=pdns.service\nLoadState=loaded\nActiveState=active\nActiveState=failed\nUnitFileState=enabled\n",
		"Id=pdns.service\nNames=pdns.service\nLoadState=loaded\nActiveState=active\nUnitFileState=enabled\nUnknown=secret\n",
		"Id=pdns.service\nNames=pdns.service\nLoadState=loaded\nActiveState=active\n",
		"Id=pdns.service\nNames=pdns.service\nLoadState=loaded\nActiveState=active;bad\nUnitFileState=enabled\n",
	}
	for _, output := range badOutputs {
		_, err := ProbeNativeUnits(ctx, []string{"pdns.service"}, func(context.Context, string) ([]byte, error) {
			return []byte(output), nil
		})
		if err == nil {
			t.Fatalf("malformed native property accepted: %q", output)
		}
	}
	_, err = ProbeNativeUnits(ctx, []string{"pdns.service"}, func(context.Context, string) ([]byte, error) {
		return nil, errors.New("systemd unavailable")
	})
	if err == nil || !strings.Contains(err.Error(), "systemd unavailable") {
		t.Fatalf("unknown native result became success: %v", err)
	}
}

func TestNativeUnitProbeBoundsOutputAndDoesNotRunAfterCancellation(t *testing.T) {
	output := &boundedUnitOutput{}
	if _, err := output.Write([]byte(strings.Repeat("a", 4096))); err != nil {
		t.Fatal(err)
	}
	if _, err := output.Write([]byte("b")); err == nil {
		t.Fatal("oversized systemd output accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	if _, err := ProbeNativeUnits(ctx, []string{"named.service"}, func(context.Context, string) ([]byte, error) {
		called = true
		return nil, nil
	}); !errors.Is(err, context.Canceled) || called {
		t.Fatalf("canceled observation ran native query: %v, %v", err, called)
	}
	if _, err := SystemdUnitRunner(nil, "named.service"); err == nil {
		t.Fatal("nil context accepted by native runner")
	}
}
