package main

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestRecoveryEntryCannotStartNewUpdateOrAcceptCallerEnvironment(t *testing.T) {
	cases := [][]string{{"update"}, {"recover", "--source", "/tmp/target"}, {"recover", "--request-id", "ab"}, {"recover", "--force"}, {"--verify-final-state"}, {"enroll-runtime", "--source", "/tmp/a/../b", "--transaction-fd", "9"}, {"enroll-runtime", "--source", "/tmp/b", "--transaction-fd", "8"}}
	for _, args := range cases {
		invoked := false
		code := dispatchEntry(args, 0, func() int { invoked = true; return 0 }, func([]string) error { invoked = true; return nil }, func(string) error { invoked = true; return nil }, func(string) {})
		if code != exitUsage || invoked {
			t.Fatalf("%q admitted: code=%d invoked=%v", args, code, invoked)
		}
	}
}
func TestRecoveryEntryRequiresNativeOwner(t *testing.T) {
	for _, args := range [][]string{{"recover"}, {"enroll-runtime", "--source", "/tmp/kit", "--transaction-fd", "9"}} {
		code := dispatchEntry(args, 1000, func() int { t.Fatal("observe"); return 0 }, func([]string) error { t.Fatal("execute"); return nil }, func(string) error { t.Fatal("enroll"); return nil }, func(string) {})
		if code != exitNotOwner {
			t.Fatal(code)
		}
	}
}
func TestRecoveryEntryPreservesFinalProofTuple(t *testing.T) {
	args := []string{"--verify-final-state", "--expected-version", "v0.1.0-alpha.80", "--expected-commit", strings.Repeat("a", 40), "--expected-sequence", "80"}
	called := false
	code := dispatchEntry(args, 0, nil, func(got []string) error {
		called = true
		if !reflect.DeepEqual(got, args) {
			t.Fatal(got)
		}
		return nil
	}, nil, func(string) {})
	if code != 0 || !called {
		t.Fatal(code, called)
	}
	code = dispatchEntry([]string{"recover"}, 0, nil, func(got []string) error {
		if len(got) != 0 {
			t.Fatal(got)
		}
		return errors.New("unavailable")
	}, nil, func(string) {})
	if code != exitUnavailable {
		t.Fatal(code)
	}
}

func TestRecoveryCompatibilityDispatch(t *testing.T) {
	for _, mode := range []string{"--normal", "--bootstrap-pre-ledger", "--bootstrap-schema17"} {
		called := false
		code := dispatchCompatibility([]string{"verify-compatibility", "--mode", mode}, 0, func(got string) error {
			called = true
			if got != mode {
				t.Fatal(got)
			}
			return nil
		}, func(string) {})
		if code != exitOK || !called {
			t.Fatal(code, called)
		}
	}
	for _, args := range [][]string{{"verify-compatibility"}, {"verify-compatibility", "--mode", "--force"}, {"verify-compatibility", "--mode", "--normal", "--db", "/tmp/db"}} {
		if got := dispatchCompatibility(args, 0, func(string) error { t.Fatal("invalid command reached checker"); return nil }, func(string) {}); got != exitUsage {
			t.Fatal(got)
		}
	}
	args := []string{"verify-compatibility", "--mode", "--normal"}
	if got := dispatchCompatibility(args, 1000, func(string) error { t.Fatal("unprivileged check"); return nil }, func(string) {}); got != exitNotOwner {
		t.Fatal(got)
	}
	if got := dispatchCompatibility(args, 0, func(string) error { return errors.New("incompatible") }, func(string) {}); got != exitUnavailable {
		t.Fatal(got)
	}
}
