package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type fakeHostResolver struct {
	addrs []string
	err   error
	calls int
}

func (r *fakeHostResolver) LookupHost(context.Context, string) ([]string, error) {
	r.calls++
	return r.addrs, r.err
}

func TestLookupNameserverHostFallsBackAfterResolverFailure(t *testing.T) {
	first := &fakeHostResolver{err: errors.New(`resolver unavailable`)}
	public := &fakeHostResolver{addrs: []string{`2.25.80.4`}}
	system := &fakeHostResolver{addrs: []string{`72.62.38.15`}}
	got, err := lookupNameserverHost(context.Background(), `ns2.celikhost.com`, []hostResolver{first, public, system})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{`2.25.80.4`}) {
		t.Fatal(got)
	}
	if system.calls != 0 {
		t.Fatal(`machine resolver was used before a successful public answer`)
	}
}

func TestLookupNameserverHostUsesMachineResolverAsLastFallback(t *testing.T) {
	first := &fakeHostResolver{err: errors.New(`blocked`)}
	second := &fakeHostResolver{err: errors.New(`blocked`)}
	system := &fakeHostResolver{addrs: []string{`10.0.0.53`}}
	got, err := lookupNameserverHost(context.Background(), `ns1.internal.example`, []hostResolver{first, second, system})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{`10.0.0.53`}) {
		t.Fatal(got)
	}
}

func TestVerifyNameserversKeepsUnresolvedIPsAsEmptySlice(t *testing.T) {
	// An empty hostname follows the unresolved path without a network lookup,
	// which keeps this JSON-contract prerequisite deterministic.
	facts := verifyNameservers(context.Background(), []string{``}, `192.0.2.10`, ``)
	if len(facts) != 1 {
		t.Fatalf(`got %d facts, want 1`, len(facts))
	}
	if facts[0].IPs == nil {
		t.Fatal(`unresolved nameserver IPs must be an empty slice, not nil`)
	}
}

func TestValidDNSHostnameEnforcesWireLengthLimits(t *testing.T) {
	valid253 := strings.Repeat("a", 63) + "." +
		strings.Repeat("b", 63) + "." +
		strings.Repeat("c", 63) + "." +
		strings.Repeat("d", 61)
	for _, tc := range []struct {
		name  string
		value string
		valid bool
	}{
		{name: "ordinary", value: "ns1.example.com", valid: true},
		{name: "canonical trailing dot", value: "NS1.EXAMPLE.COM.", valid: true},
		{name: "maximum total length", value: valid253, valid: true},
		{name: "label too long", value: strings.Repeat("a", 64) + ".example.com"},
		{name: "name too long", value: valid253 + "x"},
		{name: "single label", value: "localhost"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := validDNSHostname(tc.value); got != tc.valid {
				t.Fatalf("validDNSHostname(%q) = %v, want %v", tc.value, got, tc.valid)
			}
		})
	}
}
