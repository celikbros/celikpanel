//go:build linux

package main

import (
	"errors"
	"testing"
)

func TestPDNSNativeBindingMustRemainStableAcrossReceiptAndCatalogRead(t *testing.T) {
	base := pdnsPrimaryNativeBinding{PID: 101, Start: "312", Executable: "/usr/bin/pdns_server", DatabaseDevice: 2049, DatabaseInode: 55}
	for name, mutate := range map[string]func(*pdnsPrimaryNativeBinding){
		"pid changed":             func(b *pdnsPrimaryNativeBinding) { b.PID++ },
		"process restarted":       func(b *pdnsPrimaryNativeBinding) { b.Start = "313" },
		"executable changed":      func(b *pdnsPrimaryNativeBinding) { b.Executable = "/usr/sbin/pdns_server" },
		"database inode changed":  func(b *pdnsPrimaryNativeBinding) { b.DatabaseInode++ },
		"database device changed": func(b *pdnsPrimaryNativeBinding) { b.DatabaseDevice++ },
	} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			read := func() (pdnsPrimaryNativeBinding, error) {
				calls++
				if calls == 1 {
					return base, nil
				}
				changed := base
				mutate(&changed)
				return changed, nil
			}
			if err := recheckPDNSNativeBindingAt(read, func() error { return nil }); err == nil || calls != 2 {
				t.Fatalf("changed binding passed or skipped recheck: calls=%d err=%v", calls, err)
			}
		})
	}
	checks := 0
	if err := recheckPDNSNativeBindingAt(func() (pdnsPrimaryNativeBinding, error) { return base, nil },
		func() error { checks++; return nil }); err != nil || checks != 1 {
		t.Fatalf("stable binding failed: checks=%d err=%v", checks, err)
	}
	if err := recheckPDNSNativeBindingAt(func() (pdnsPrimaryNativeBinding, error) { return base, nil },
		func() error { return errors.New("owner edit") }); err == nil || err.Error() != "owner edit" {
		t.Fatalf("known owner edit lost: %v", err)
	}
}
