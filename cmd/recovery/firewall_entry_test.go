package main

import (
	"bytes"
	"errors"
	"testing"
)

func TestFirewallPreparationCLIBoundary(t *testing.T) {
	good := []string{"prepare-firewall-runtime", "--source", "/reviewed/firewall-runtime", "--transaction-fd", "9"}
	for _, args := range [][]string{nil, {"prepare-firewall-runtime"}, {"prepare-firewall-runtime", "--source", "/reviewed/firewall-runtime", "--transaction-fd", "8"}, {"prepare-firewall-runtime", "--source", "/reviewed/../firewall-runtime", "--transaction-fd", "9"}, append(append([]string{}, good...), "--force")} {
		if got := dispatchFirewallPreparation(args, 0, func(string, int) (string, error) { t.Fatal("invalid input executed"); return "", nil }, &bytes.Buffer{}, func(string) {}); got != exitUsage {
			t.Fatalf("usage: %v %d", args, got)
		}
	}
	if got := dispatchFirewallPreparation(good, 1000, func(string, int) (string, error) { t.Fatal("nonowner executed"); return "", nil }, &bytes.Buffer{}, func(string) {}); got != exitNotOwner {
		t.Fatal(got)
	}
	for _, failed := range []bool{false, true} {
		var output bytes.Buffer
		reported := false
		got := dispatchFirewallPreparation(good, 0, func(source string, fd int) (string, error) {
			if source != good[2] || fd != 9 {
				t.Fatal("scope changed")
			}
			if failed {
				return "", errors.New("fixture unavailable")
			}
			return "generation", nil
		}, &output, func(string) { reported = true })
		if failed {
			if got != exitUnavailable || !reported || output.Len() != 0 {
				t.Fatal("failure became success")
			}
		} else if got != exitOK || output.String() != "generation\n" || reported {
			t.Fatal("success differs")
		}
	}
	if launcherDispatchCommand(good) {
		t.Fatal("preparation dispatched to old selected runtime")
	}
}

func TestFirewallUnitVerificationCLI(t *testing.T) {
	args := []string{"verify-firewall-unit", "--unit", "/reviewed/celikpanel-firewall-restore.service"}
	if !launcherDispatchCommand(args) {
		t.Fatal("verification bypasses selected runtime proof")
	}
	for _, bad := range [][]string{nil, {"verify-firewall-unit"}, {"verify-firewall-unit", "--unit", "relative/celikpanel-firewall-restore.service"}, append(append([]string{}, args...), "--force")} {
		if got := dispatchFirewallUnitVerification(bad, 0, func(string) error { t.Fatal("invalid input executed"); return nil }, func(string) {}); got != exitUsage {
			t.Fatal(got)
		}
	}
	if got := dispatchFirewallUnitVerification(args, 1000, func(string) error { t.Fatal("nonowner executed"); return nil }, func(string) {}); got != exitNotOwner {
		t.Fatal(got)
	}
	for _, failed := range []bool{false, true} {
		reported := false
		got := dispatchFirewallUnitVerification(args, 0, func(path string) error {
			if path != args[2] {
				t.Fatal("scope changed")
			}
			if failed {
				return errors.New("missing")
			}
			return nil
		}, func(string) { reported = true })
		if failed {
			if got != exitUnavailable || !reported {
				t.Fatal("missing helper accepted")
			}
		} else if got != exitOK || reported {
			t.Fatal(got)
		}
	}
}
