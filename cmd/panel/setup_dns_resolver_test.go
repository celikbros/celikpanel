package main

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"reflect"
	"sync"
	"testing"
	"time"
)

func setupDNSWireTestServer(t *testing.T, answer func(uint16) ([]string, uint16, time.Duration)) (string, *sync.Map) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	seen := &sync.Map{}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				var size [2]byte
				if _, err := io.ReadFull(conn, size[:]); err != nil {
					return
				}
				query := make([]byte, binary.BigEndian.Uint16(size[:]))
				if _, err := io.ReadFull(conn, query); err != nil || len(query) < 17 {
					return
				}
				kind := binary.BigEndian.Uint16(query[len(query)-4 : len(query)-2])
				seen.Store(kind, true)
				values, rcode, delay := answer(kind)
				if delay > 0 {
					time.Sleep(delay)
				}
				response := append([]byte(nil), query...)
				binary.BigEndian.PutUint16(response[2:4], 0x8180|rcode)
				binary.BigEndian.PutUint16(response[6:8], uint16(len(values)))
				for _, value := range values {
					data := net.ParseIP(value).To16()
					if kind == 1 {
						data = net.ParseIP(value).To4()
					}
					response = append(response, 0xc0, 0x0c, byte(kind>>8), byte(kind), 0, 1, 0, 0, 0, 60, 0, byte(len(data)))
					response = append(response, data...)
				}
				binary.BigEndian.PutUint16(size[:], uint16(len(response)))
				_, _ = conn.Write(append(size[:], response...))
			}()
		}
	}()
	return listener.Addr().String(), seen
}

func TestSetupDNSWireIgnoresLocalhostHostsEntryAndReadsBothFamilies(t *testing.T) {
	local, err := net.LookupHost("localhost")
	if err != nil || len(local) == 0 {
		t.Fatal("OS hosts prerequisite unavailable")
	}
	for _, value := range local {
		if !net.ParseIP(value).IsLoopback() {
			t.Fatalf("unexpected localhost %v", local)
		}
	}
	address, seen := setupDNSWireTestServer(t, func(kind uint16) ([]string, uint16, time.Duration) {
		if kind == 1 {
			return []string{"192.0.2.15"}, 0, 0
		}
		return []string{"2001:db8::15"}, 0, 0
	})
	got, err := setupDNSResolverAt(address).LookupHost(context.Background(), "localhost")
	if err != nil || !reflect.DeepEqual(got, []string{"192.0.2.15", "2001:db8::15"}) {
		t.Fatalf("DNS proof was replaced by hosts/NSS: %v %v", got, err)
	}
	for _, kind := range []uint16{1, 28} {
		if _, ok := seen.Load(kind); !ok {
			t.Fatal("address family did not reach the actual DNS listener")
		}
	}
}

func TestSetupDNSWireDoesNotTreatPartialAddressProofAsReady(t *testing.T) {
	for _, scenario := range []string{"ipv4-only", "aaaa-servfail", "aaaa-timeout", "nxdomain", "no-addresses"} {
		t.Run(scenario, func(t *testing.T) {
			address, _ := setupDNSWireTestServer(t, func(kind uint16) ([]string, uint16, time.Duration) {
				if scenario == "nxdomain" {
					return nil, 3, 0
				}
				if scenario == "no-addresses" {
					return nil, 0, 0
				}
				if kind == 1 {
					return []string{"192.0.2.15"}, 0, 0
				}
				if scenario == "aaaa-servfail" {
					return nil, 2, 0
				}
				if scenario == "aaaa-timeout" {
					return nil, 0, 250 * time.Millisecond
				}
				return nil, 0, 0
			})
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			started := time.Now()
			got, err := setupDNSResolverAt(address).LookupHost(ctx, "ns.example.test")
			if scenario == "ipv4-only" {
				if err != nil || !reflect.DeepEqual(got, []string{"192.0.2.15"}) {
					t.Fatalf("valid AAAA NODATA rejected: %v %v", got, err)
				}
				return
			}
			if err == nil || len(got) != 0 {
				t.Fatalf("partial/unavailable proof admitted: %v %v", got, err)
			}
			if scenario == "aaaa-timeout" && time.Since(started) > time.Second {
				t.Fatal("lookup exceeded the shared context bound")
			}
			if scenario == "nxdomain" || scenario == "no-addresses" {
				var dnsErr *net.DNSError
				if !errors.As(err, &dnsErr) || !dnsErr.IsNotFound {
					t.Fatalf("missing DNS identity misclassified: %v", err)
				}
			}
		})
	}
}
