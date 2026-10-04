package main

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestMailRenewalPreparationCLIBoundary(t *testing.T) {
	good := []string{"prepare-mail-renewal-runtime", "--source", "/reviewed/mail-renewal-runtime", "--transaction-fd", "9"}
	for _, args := range [][]string{nil, {"prepare-mail-renewal-runtime"}, {"prepare-mail-renewal-runtime", "--source", "/reviewed/mail-renewal-runtime", "--transaction-fd", "8"}, {"prepare-mail-renewal-runtime", "--source", "/reviewed/../mail-renewal-runtime", "--transaction-fd", "9"}, append(append([]string{}, good...), "--force")} {
		if got := dispatchMailRenewalPreparation(args, 0, func(string, int) (string, error) { t.Fatal("invalid input executed"); return "", nil }, &bytes.Buffer{}, func(string) {}); got != exitUsage {
			t.Fatalf("usage: %v %d", args, got)
		}
	}
	if got := dispatchMailRenewalPreparation(good, 1000, func(string, int) (string, error) { t.Fatal("nonowner executed"); return "", nil }, &bytes.Buffer{}, func(string) {}); got != exitNotOwner {
		t.Fatal(got)
	}
	for _, failed := range []bool{false, true} {
		var output bytes.Buffer
		reported := false
		got := dispatchMailRenewalPreparation(good, 0, func(source string, fd int) (string, error) {
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

func TestMailApplicationCompatibilityDispatch(t *testing.T) {
	called := false
	check := func(path string) error {
		called = true
		if path != "/snapshot/bin" {
			t.Fatal(path)
		}
		return nil
	}
	report := func(string) {}
	for _, args := range [][]string{nil, {"verify-mail-application"}, {"verify-mail-application", "--bin", "relative/bin"}, {"verify-mail-application", "--bin", "/snapshot/../bin"}, {"verify-mail-application", "--bin", "/snapshot/other"}} {
		if got := dispatchMailApplicationCompatibility(args, 0, check, report); got != exitUsage || called {
			t.Fatal(args, got)
		}
	}
	args := []string{"verify-mail-application", "--bin", "/snapshot/bin"}
	if got := dispatchMailApplicationCompatibility(args, 1000, check, report); got != exitNotOwner || called {
		t.Fatal(got)
	}
	if got := dispatchMailApplicationCompatibility(args, 0, check, report); got != exitOK || !called {
		t.Fatal(got)
	}
	message := ""
	if got := dispatchMailApplicationCompatibility(args, 0, func(string) error { return fmt.Errorf("unverified") }, func(s string) { message = s }); got != exitUnavailable || !strings.Contains(message, "preserve") && !strings.Contains(message, "keep") {
		t.Fatal(got, message)
	}
}

func TestCandidateAgentContractCheckIsNotSkippedWithNoNativeHook(t *testing.T) {
	calls := 0
	args := []string{"verify-agent-native-contract", "--bin", "/release/bin"}
	code := dispatchMailApplicationCompatibility(args, 0, func(path string) error {
		calls++
		if path != "/release/bin" {
			t.Fatal(path)
		}
		return errors.New("missing declaration")
	}, func(string) {})
	if code != exitUnavailable || calls != 1 {
		t.Fatal("missing candidate declaration accepted", code, calls)
	}
	if launcherDispatchCommand(args) {
		t.Fatal("candidate artifact check sent to historical selected reader")
	}
}
