//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestOwnerPDNSTargetInverseV4DispatchRejectsInvalidOrUnauthorized(t *testing.T) {
	id := strings.Repeat("a", 32)
	for _, args := range [][]string{
		{"recover-dns-pdns-target-staged"},
		{"recover-dns-pdns-target-staged", "--request-id", "invalid"},
		{"recover-dns-pdns-target-staged", "--request-id", id, "--request-id", id},
		{"recover-dns-pdns-target-staged", "--request-id", id, "--root", "/tmp/other"},
		{"recover-dns-pdns-target-staged", "--request-id", id, "--lang", "fr"},
	} {
		called := false
		var out, diagnostic bytes.Buffer
		got := dispatchOwnerPDNSTargetInverseV4(args, 0, func(context.Context, string) error { called = true; return nil }, &out, &diagnostic)
		if got != exitUsage || called {
			t.Fatalf("invalid command %v reached inverse: exit=%d", args, got)
		}
	}
	called := false
	var out, diagnostic bytes.Buffer
	args := []string{"recover-dns-pdns-target-staged", "--request-id", id}
	if got := dispatchOwnerPDNSTargetInverseV4(args, 1000, func(context.Context, string) error { called = true; return nil }, &out, &diagnostic); got != exitNotOwner || called {
		t.Fatalf("non-owner reached inverse: exit=%d", got)
	}
}

func TestOwnerPDNSTargetInverseV4DispatchReportsSameRequestRecovery(t *testing.T) {
	id := strings.Repeat("b", 32)
	args := []string{"recover-dns-pdns-target-staged", "--request-id", id, "--lang", "tr"}
	var out, diagnostic bytes.Buffer
	if got := dispatchOwnerPDNSTargetInverseV4(args, 0, func(_ context.Context, got string) error {
		if got != id {
			t.Fatalf("request changed: %s", got)
		}
		return errors.New("unknown native proof")
	}, &out, &diagnostic); got != exitUnavailable || !strings.Contains(diagnostic.String(), "dns-switch-status --quiesced --request-id "+id) {
		t.Fatalf("missing actionable same-request error: %d %q", got, diagnostic.String())
	}
	out.Reset()
	diagnostic.Reset()
	if got := dispatchOwnerPDNSTargetInverseV4(args, 0, func(context.Context, string) error { return nil }, &out, &diagnostic); got != exitOK || !strings.Contains(out.String(), id) {
		t.Fatalf("missing successful exact-request verdict: %d %q", got, out.String())
	}
}
