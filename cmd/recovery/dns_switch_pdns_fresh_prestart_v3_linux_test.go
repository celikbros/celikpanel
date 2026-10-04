//go:build linux

package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestFreshPDNSPrestartOwnerCommandRejectsUnboundOrNonOwner(t *testing.T) {
	id := strings.Repeat("a", 32)
	for _, args := range [][]string{
		{ownerPDNSFreshPrestartV3Command},
		{ownerPDNSFreshPrestartV3Command, "--request-id", "bad"},
		{ownerPDNSFreshPrestartV3Command, "--request-id", id, "--root", "/tmp"},
	} {
		var out, diagnostic bytes.Buffer
		if got := runOwnerPDNSFreshPrestartV3(args, 0, &out, &diagnostic); got != exitUsage || out.Len() != 0 || !strings.Contains(diagnostic.String(), "Usage:") {
			t.Fatalf("args %v: %d %q %q", args, got, out.String(), diagnostic.String())
		}
	}
	var out, diagnostic bytes.Buffer
	if got := runOwnerPDNSFreshPrestartV3([]string{ownerPDNSFreshPrestartV3Command, "--request-id", id}, 1000, &out, &diagnostic); got != exitNotOwner || out.Len() != 0 || !strings.Contains(diagnostic.String(), "root") {
		t.Fatalf("nonowner: %d %q %q", got, out.String(), diagnostic.String())
	}
}
