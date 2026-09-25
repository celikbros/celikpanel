//go:build linux

package dnsenginerecovery

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
)

func TestProbeBINDListenersRejectsAmbiguousSocketOwnership(t *testing.T) {
	const good = "tcp LISTEN 0 4096 72.62.38.15:53 0.0.0.0:* users:((\"named\",pid=123,fd=1))\n" +
		"udp UNCONN 0 0 72.62.38.15:53 0.0.0.0:* users:((\"named\",pid=123,fd=2))\n"
	tests := []struct {
		name, first, second, address string
		wantOK                       bool
	}{
		{"exact", good, good, "72.62.38.15", true},
		{"wildcard", strings.ReplaceAll(good, "72.62.38.15:53", "0.0.0.0:53"), strings.ReplaceAll(good, "72.62.38.15:53", "0.0.0.0:53"), "72.62.38.15", true},
		{"wrong-primary", good, good, "2.25.80.4", false},
		{"wrong-pid", strings.ReplaceAll(good, "pid=123", "pid=124"), good, "72.62.38.15", false},
		{"other-process", good + "tcp LISTEN 0 4096 2.25.80.4:53 0.0.0.0:* users:((\"pdns_server\",pid=90,fd=3))\n", good, "72.62.38.15", false},
		{"missing-udp", strings.Split(good, "\n")[0] + "\n", good, "72.62.38.15", false},
		{"changed-between-reads", good, strings.ReplaceAll(good, "72.62.38.15:53", "0.0.0.0:53"), "72.62.38.15", false},
		{"ipv6-only", strings.ReplaceAll(good, "72.62.38.15:53", "[::]:53"), strings.ReplaceAll(good, "72.62.38.15:53", "[::]:53"), "72.62.38.15", false},
		{"malformed", good + "tcp LISTEN 0 1 spoofed\n", good, "72.62.38.15", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			runner := func(context.Context) ([]byte, error) {
				calls++
				if calls == 1 {
					return []byte(test.first), nil
				}
				return []byte(test.second), nil
			}
			err := ProbeBINDListeners(context.Background(), 123, test.address, runner)
			if (err == nil) != test.wantOK {
				t.Fatalf("error = %v, wantOK = %t", err, test.wantOK)
			}
			if test.wantOK && calls != 2 {
				t.Fatalf("runner calls = %d, want 2", calls)
			}
		})
	}
}

func TestProbeBINDListenersRefusesUnknownAndOversizedResults(t *testing.T) {
	const good = "tcp LISTEN 0 1 0.0.0.0:53 0.0.0.0:* users:((\"named\",pid=123,fd=1))\n" +
		"udp UNCONN 0 0 0.0.0.0:53 0.0.0.0:* users:((\"named\",pid=123,fd=2))\n"
	if err := ProbeBINDListeners(context.Background(), 123, "", func(context.Context) ([]byte, error) {
		return nil, errors.New("ss failed")
	}); err == nil {
		t.Fatal("command error was accepted")
	}
	if err := ProbeBINDListeners(context.Background(), 123, "", func(context.Context) ([]byte, error) {
		return []byte(good + strings.Repeat("x", 64<<10)), nil
	}); err == nil {
		t.Fatal("oversized inventory was accepted")
	}
}

func TestProbeAuthorityListenersBindsPowerDNSProcess(t *testing.T) {
	const output = "tcp LISTEN 0 4096 0.0.0.0:53 0.0.0.0:* users:((\"pdns_server\",pid=77,fd=1))\n" +
		"udp UNCONN 0 0 0.0.0.0:53 0.0.0.0:* users:((\"pdns_server\",pid=77,fd=2))\n"
	runner := func(context.Context) ([]byte, error) { return []byte(output), nil }
	if err := ProbeAuthorityListeners(context.Background(), "pdns_server", 77, "", runner); err != nil {
		t.Fatalf("PowerDNS listener observation: %v", err)
	}
	if err := ProbeAuthorityListeners(context.Background(), "named", 77, "", runner); err == nil {
		t.Fatal("wrong process identity accepted")
	}
	if err := ProbeAuthorityListeners(context.Background(), "pdns_server", 78, "", runner); err == nil {
		t.Fatal("wrong systemd PID accepted")
	}
}

func TestSelectAuthorityIPv4AddressRequiresLocalSocketCoverage(t *testing.T) {
	address := func(value string) net.Addr {
		ip, subnet, err := net.ParseCIDR(value)
		if err != nil {
			t.Fatal(err)
		}
		subnet.IP = ip
		return subnet
	}
	candidates := []net.Addr{address("127.0.0.1/8"), address("192.0.2.11/24"), address("192.0.2.10/24")}
	ids := []string{"tcp|0.0.0.0|123", "udp|0.0.0.0|123"}
	selected, err := selectAuthorityIPv4Address(ids, 123, candidates)
	if err != nil || selected != "192.0.2.10" {
		t.Fatalf("wildcard listener selection = %q, %v", selected, err)
	}
	for _, test := range []struct {
		name string
		ids  []string
		pid  uint64
	}{
		{"wrong-pid", ids, 124},
		{"missing-udp", []string{"tcp|0.0.0.0|123"}, 123},
		{"different-address", []string{"tcp|192.0.2.12|123", "udp|192.0.2.12|123"}, 123},
	} {
		if chosen, err := selectAuthorityIPv4Address(test.ids, test.pid, candidates); err == nil {
			t.Fatalf("%s accepted %q", test.name, chosen)
		}
	}
}
