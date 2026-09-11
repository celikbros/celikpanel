package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type setupNameserverResolver map[string][]string

func (r setupNameserverResolver) LookupHost(_ context.Context, name string) ([]string, error) {
	if addresses, ok := r[name]; ok {
		return addresses, nil
	}
	return nil, errors.New("public resolution unavailable")
}

func TestSetupDNSPublicNameserversMustMatchExactDirectionalAddresses(t *testing.T) {
	p := newDNSPanelForTest(t)
	seedSetupDNSOwner(t, p)
	agent := newDNSEngineTestAgent()
	attachDNSEngineTestAgent(t, p, agent)
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.1")
	draft := setupDNSTestDraft()
	if err := p.startServerSetupDNS(context.Background(), draft, strings.Repeat("f", 32), serviceOperationActor{UserID: 1}); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name               string
		local, peer        []string
		ready              bool
		peerIPv6Unverified bool
	}{
		{"exact", []string{"192.0.2.1"}, []string{"192.0.2.2"}, true, false},
		{"unpublished", nil, nil, false, false},
		{"wrong_local", []string{"192.0.2.3"}, []string{"192.0.2.2"}, false, false},
		{"wrong_peer", []string{"192.0.2.1"}, []string{"192.0.2.3"}, false, false},
		{"mixed_local", []string{"192.0.2.1", "192.0.2.3"}, []string{"192.0.2.2"}, false, false},
		{"same_server", []string{"192.0.2.1"}, []string{"192.0.2.1"}, false, false},
		{"dual_stack_peer_unverified", []string{"192.0.2.1"}, []string{"192.0.2.2", "2001:db8::2"}, false, true},
		{"wrong_peer_ipv4_with_aaaa", []string{"192.0.2.1"}, []string{"2001:db8::2", "192.0.2.3"}, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			resolver := setupNameserverResolver{draft.NS1: test.local, draft.NS2: test.peer}
			ready, err := p.setupDNSNameserverReadinessWithResolvers(context.Background(), []hostResolver{resolver})
			if errors.Is(err, errSetupDNSPeerIPv6Unverified) != test.peerIPv6Unverified {
				t.Fatalf("peer IPv6 limitation=%v error=%v", test.peerIPv6Unverified, err)
			}
			if ready != test.ready || (test.ready && err != nil) || (!test.ready && err == nil) {
				t.Fatalf("ready=%v error=%v", ready, err)
			}
		})
	}
}
