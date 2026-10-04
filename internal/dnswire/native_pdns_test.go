//go:build linux

package dnswire

import (
	"context"
	"os"
	"testing"
	"time"
)

// Run in a disposable guest with a real authoritative PowerDNS service.
// The fixture must serve s1-kill.test and leave old.s1-kill.test absent.
func TestNativePowerDNSDeletedZoneSOA(t *testing.T) {
	endpoint := os.Getenv("CELIKPANEL_TEST_NATIVE_PDNS_ENDPOINT")
	if endpoint == "" {
		t.Skip("requires the disposable native PowerDNS fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	const active = "s1-kill.test"
	const deleted = "old.s1-kill.test"
	const serial = 2026083101
	for _, network := range []string{"udp", "tcp"} {
		t.Run(network, func(t *testing.T) {
			var got uint32
			var err error
			if network == "udp" {
				got, err = QueryAuthoritativeSOAUDP(ctx, endpoint, active)
			} else {
				got, err = QueryAuthoritativeSOA(ctx, endpoint, active)
			}
			if err != nil || got != serial {
				t.Fatalf("native parent SOA got=%d err=%v", got, err)
			}
			if err := QueryDeletedZoneSOA(ctx, network, endpoint, deleted); err != nil {
				t.Fatalf("native deleted child was not proved absent: %v", err)
			}
			if err := QueryDeletedZoneSOA(ctx, network, endpoint, active); err == nil {
				t.Fatal("active native zone passed deleted-zone proof")
			}
		})
	}
}
