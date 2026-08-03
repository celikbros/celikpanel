//go:build !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris

package main

import (
	"runtime"
	"strings"
	"testing"
)

func TestRequireRootOwnerFailsClosedOnUnsupportedPlatform(t *testing.T) {
	const label = "trusted firewall state"
	err := requireRootOwner(nil, label)
	if err == nil {
		t.Fatal("requireRootOwner accepted a platform without Unix ownership metadata")
	}
	if !strings.Contains(err.Error(), label) || !strings.Contains(err.Error(), "unsupported on "+runtime.GOOS) {
		t.Fatalf("requireRootOwner error = %q, want label and unsupported platform", err)
	}
}
