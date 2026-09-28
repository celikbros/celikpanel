package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func TestPeerCurrentPendingCodeKeepsFirstFailedBoundary(t *testing.T) {
	const privateDetail = "private peer credential or native output"
	for _, tc := range []struct {
		name  string
		fail  string
		want  string
		calls string
	}{
		{"current attempt changed", "attempt", transport.DNSPeerPendingOwnerEditUnknown, "attempt"},
		{"enrollment changed", "enrollment", transport.DNSPeerPendingEnrollmentChanged, "attempt,enrollment"},
		{"native DNS changed", "local", transport.DNSPeerPendingOwnerEditUnknown, "attempt,enrollment,local"},
		{"all current", "", "", "attempt,enrollment,local"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls []string
			check := func(boundary string) func() error {
				return func() error {
					calls = append(calls, boundary)
					if boundary == tc.fail {
						return errors.New(privateDetail)
					}
					return nil
				}
			}
			err := peerCurrentPendingCodeAt(check("attempt"), check("enrollment"), check("local"))
			if got := pendingDNSPeerCode(err); got != tc.want ||
				strings.Join(calls, ",") != tc.calls ||
				(err != nil && strings.Contains(err.Error(), privateDetail)) {
				t.Fatalf("code=%q calls=%v err=%v; want code=%q calls=%q",
					got, calls, err, tc.want, tc.calls)
			}
			if tc.want != "" && dnsZoneV3PendingLedgerCode(pendingDNSPeerCode(err)) != tc.want {
				t.Fatalf("reviewed reason was not ledger-safe: %v", err)
			}
		})
	}
	for _, checks := range [][3]func() error{
		{nil, func() error { t.Fatal("unsafe enrollment reached"); return nil }, func() error { return nil }},
		{func() error { return nil }, nil, func() error { t.Fatal("unsafe local check reached"); return nil }},
		{func() error { return nil }, func() error { return nil }, nil},
	} {
		err := peerCurrentPendingCodeAt(checks[0], checks[1], checks[2])
		if pendingDNSPeerCode(err) != transport.DNSPeerPendingOwnerEditUnknown {
			t.Fatalf("missing boundary accepted: %v", err)
		}
	}
}
