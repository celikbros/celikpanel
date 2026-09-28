//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestOwnerPDNSAdoptionInverseCommandExactIdentityAndRoot(t *testing.T) {
	id := strings.Repeat("a", 32)
	valid := []string{ownerPDNSAdoptionInverseCommand, "--request-id", id}
	gotID, gotLang, ok := parseOwnerPDNSAdoptionInverseArgs(valid)
	if !ok || gotID != id || gotLang != "en" {
		t.Fatalf("exact command rejected: %q %q %t", gotID, gotLang, ok)
	}
	gotID, gotLang, ok = parseOwnerPDNSAdoptionInverseArgs([]string{ownerPDNSAdoptionInverseCommand, "--lang", "tr", "--request-id", id})
	if !ok || gotID != id || gotLang != "tr" {
		t.Fatalf("Turkish exact command rejected: %q %q %t", gotID, gotLang, ok)
	}
	var out, diagnostic bytes.Buffer
	called := false
	inverse := func(_ context.Context, got string) error {
		called = true
		if got != id {
			t.Fatalf("foreign request reached inverse: %q", got)
		}
		return nil
	}
	if code := dispatchOwnerPDNSAdoptionInverse(valid, 1000, inverse, &out, &diagnostic); code != exitNotOwner || called {
		t.Fatalf("nonowner reached inverse: code=%d called=%t", code, called)
	}
	for _, args := range [][]string{
		{ownerPDNSAdoptionInverseCommand},
		{ownerPDNSAdoptionInverseCommand, "--request-id", strings.Repeat("A", 32)},
		{ownerPDNSAdoptionInverseCommand, "--request-id", "../state"},
		{ownerPDNSAdoptionInverseCommand, "--request-id", id, "--request-id", id},
		{ownerPDNSAdoptionInverseCommand, "--request-id", id, "--lang", "tr", "--lang", "en"},
		{ownerPDNSAdoptionInverseCommand, "--request-id", id, "--force"},
		{ownerPDNSAdoptionInverseCommand, "--request-id", id, "--state-root", "/tmp"},
		{ownerPDNSAdoptionInverseCommand, "--request-id", id, "--lang", "de"},
		{"recover", "--request-id", id},
	} {
		called = false
		out.Reset()
		diagnostic.Reset()
		if code := dispatchOwnerPDNSAdoptionInverse(args, 0, inverse, &out, &diagnostic); code != exitUsage || called {
			t.Fatalf("unsafe command admitted: %q code=%d called=%t", args, code, called)
		}
	}
	called = false
	out.Reset()
	diagnostic.Reset()
	if code := dispatchOwnerPDNSAdoptionInverse(valid, 0, inverse, &out, &diagnostic); code != exitOK || !called || !strings.Contains(out.String(), id) {
		t.Fatalf("exact owner request failed: code=%d called=%t out=%q", code, called, out.String())
	}
}

func TestOwnerPDNSAdoptionInverseTurkishGuidanceIsUTF8(t *testing.T) {
	id := strings.Repeat("c", 32)
	var out, diagnostic bytes.Buffer
	args := []string{ownerPDNSAdoptionInverseCommand, "--lang", "tr", "--request-id", id, "--force"}
	if code := dispatchOwnerPDNSAdoptionInverse(args, 0, func(context.Context, string) error {
		t.Fatal("invalid Turkish command reached inverse")
		return nil
	}, &out, &diagnostic); code != exitUsage || !strings.Contains(diagnostic.String(), "Kullan\u0131m:") {
		t.Fatalf("Turkish usage was corrupted or unavailable: code=%d diagnostic=%q", code, diagnostic.String())
	}
	out.Reset()
	diagnostic.Reset()
	args = []string{ownerPDNSAdoptionInverseCommand, "--lang", "tr", "--request-id", id}
	if code := dispatchOwnerPDNSAdoptionInverse(args, 0, func(context.Context, string) error {
		return errors.New("native proof unknown")
	}, &out, &diagnostic); code != exitUnavailable ||
		!strings.Contains(diagnostic.String(), "Sunucu sahibi") ||
		!strings.Contains(diagnostic.String(), "ayn\u0131 i\u015flemi") {
		t.Fatalf("Turkish recovery guidance was corrupted or missing: code=%d diagnostic=%q", code, diagnostic.String())
	}
}
func TestOwnerPDNSAdoptionInverseDistinguishesTerminalHistoryAndUnknown(t *testing.T) {
	id := strings.Repeat("b", 32)
	args := []string{ownerPDNSAdoptionInverseCommand, "--request-id", id}
	var out, diagnostic bytes.Buffer
	terminal := func(context.Context, string) error { return errPDNSInverseTerminalLedgerObserved }
	if code := dispatchOwnerPDNSAdoptionInverse(args, 0, terminal, &out, &diagnostic); code != exitUnavailable ||
		strings.Contains(out.String(), "terminal verdict") || !strings.Contains(diagnostic.String(), "historical rollback verdict") ||
		!strings.Contains(diagnostic.String(), "current PowerDNS health") {
		t.Fatalf("retired journal was reported as current success: code=%d out=%q diagnostic=%q", code, out.String(), diagnostic.String())
	}
	out.Reset()
	diagnostic.Reset()
	unknown := func(context.Context, string) error { return errors.New("owner config changed") }
	if code := dispatchOwnerPDNSAdoptionInverse(args, 0, unknown, &out, &diagnostic); code != exitUnavailable ||
		!strings.Contains(diagnostic.String(), "same request") || !strings.Contains(diagnostic.String(), "owner config changed") ||
		!strings.Contains(diagnostic.String(), "dns-switch-status --quiesced --request-id "+id) {
		t.Fatalf("unknown result lost owner action: code=%d diagnostic=%q", code, diagnostic.String())
	}
}
