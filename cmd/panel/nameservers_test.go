package main

import (
	"context"
	"errors"
	"reflect"
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
