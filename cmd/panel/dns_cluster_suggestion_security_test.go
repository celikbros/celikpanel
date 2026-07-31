package main

import "testing"

func TestSuggestDNSClusterPeerAcceptsVerifiedEffectiveDefaultPair(t *testing.T) {
	const (
		localIP = "2.25.80.4"
		peerIP  = "72.62.38.15"
		ns1     = "ns1.celikhost.com"
		ns2     = "ns2.celikhost.com"
	)

	// These are the valid defaults serverNameservers derives on a fresh
	// boston.celikhost.com install. No persisted nameserver settings are needed
	// for a safe, read-only suggestion once public DNS proves the assignment.
	localNS, suggestedPeerNS, suggestedPeerIP := suggestDNSClusterPeer(
		localIP,
		ns1,
		ns2,
		[]nameserverFact{
			{Host: ns1, IPs: []string{peerIP}},
			{Host: ns2, IPs: []string{localIP}},
		},
	)
	if localNS != ns2 || suggestedPeerNS != ns1 || suggestedPeerIP != peerIP {
		t.Fatalf("suggestion = (%q, %q, %q), want (%q, %q, %q)",
			localNS, suggestedPeerNS, suggestedPeerIP, ns2, ns1, peerIP)
	}
}

func TestSuggestDNSClusterPeerRequiresExclusiveLocalIPv4Set(t *testing.T) {
	const (
		localIP = "2.25.80.4"
		peerIP  = "72.62.38.15"
		ns1     = "ns1.celikhost.com"
		ns2     = "ns2.celikhost.com"
	)

	tests := []struct {
		name     string
		localIPs []string
		peerIPs  []string
		want     bool
	}{
		{
			name:     "one canonical local address",
			localIPs: []string{localIP},
			peerIPs:  []string{peerIP},
			want:     true,
		},
		{
			name:     "equivalent mapped local answer is not an extra address",
			localIPs: []string{localIP, "::ffff:2.25.80.4"},
			peerIPs:  []string{peerIP},
			want:     true,
		},
		{
			name:     "local name also contains peer",
			localIPs: []string{localIP, peerIP},
			peerIPs:  []string{peerIP},
		},
		{
			name:     "local name contains unrelated IPv4",
			localIPs: []string{localIP, "203.0.113.44"},
			peerIPs:  []string{peerIP},
		},
		{
			name:     "local marker without local IPv4",
			localIPs: []string{"2001:db8::4"},
			peerIPs:  []string{peerIP},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			localNS, suggestedPeerNS, suggestedPeerIP := suggestDNSClusterPeer(
				localIP,
				ns1,
				ns2,
				[]nameserverFact{
					{Host: ns1, IPs: tt.peerIPs},
					// PointsHere alone must never rescue an ambiguous A set.
					{Host: ns2, IPs: tt.localIPs, PointsHere: true},
				},
			)
			got := localNS != "" || suggestedPeerNS != "" || suggestedPeerIP != ""
			if got != tt.want {
				t.Fatalf("suggestion = (%q, %q, %q), present=%v want present=%v",
					localNS, suggestedPeerNS, suggestedPeerIP, got, tt.want)
			}
		})
	}
}

func TestSuggestDNSClusterPeerRequiresOneSafePeerIPv4(t *testing.T) {
	const (
		localIP = "2.25.80.4"
		ns1     = "ns1.celikhost.com"
		ns2     = "ns2.celikhost.com"
	)

	tests := []struct {
		name    string
		peerIPs []string
		wantIP  string
	}{
		{name: "public peer", peerIPs: []string{"72.62.38.15"}, wantIP: "72.62.38.15"},
		{
			name:    "duplicate canonical peer answer",
			peerIPs: []string{"72.62.38.15", "::ffff:72.62.38.15"},
			wantIP:  "72.62.38.15",
		},
		{
			// net.IP.IsGlobalUnicast includes private routed addresses, just as
			// the DNS setup validator does; a private replication link remains
			// a valid explicit topology.
			name:    "private routed peer follows setup validator",
			peerIPs: []string{"10.23.0.8"},
			wantIP:  "10.23.0.8",
		},
		{name: "multiple peer IPv4 answers", peerIPs: []string{"72.62.38.15", "203.0.113.44"}},
		{name: "peer overlaps local address", peerIPs: []string{localIP}},
		{name: "unspecified peer", peerIPs: []string{"0.0.0.0"}},
		{name: "loopback peer", peerIPs: []string{"127.0.0.2"}},
		{name: "link-local peer", peerIPs: []string{"169.254.10.20"}},
		{name: "multicast peer", peerIPs: []string{"224.0.0.1"}},
		{name: "broadcast peer", peerIPs: []string{"255.255.255.255"}},
		{name: "no IPv4 peer", peerIPs: []string{"2001:db8::8"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			localNS, peerNS, peerIP := suggestDNSClusterPeer(
				localIP,
				ns1,
				ns2,
				[]nameserverFact{
					{Host: ns1, IPs: tt.peerIPs},
					{Host: ns2, IPs: []string{localIP}, PointsHere: true},
				},
			)
			if tt.wantIP == "" {
				if localNS != "" || peerNS != "" || peerIP != "" {
					t.Fatalf("unsafe/ambiguous peer produced suggestion (%q, %q, %q)", localNS, peerNS, peerIP)
				}
				return
			}
			if localNS != ns2 || peerNS != ns1 || peerIP != tt.wantIP {
				t.Fatalf("suggestion = (%q, %q, %q), want (%q, %q, %q)",
					localNS, peerNS, peerIP, ns2, ns1, tt.wantIP)
			}
		})
	}
}

func TestSuggestDNSClusterPeerRejectsInvalidDerivedNames(t *testing.T) {
	for _, pair := range [][2]string{
		{"ns1", "ns2.celikhost.com"},
		{"-ns1.celikhost.com", "ns2.celikhost.com"},
		{"ns1.celikhost.com", "ns1.celikhost.com"},
	} {
		localNS, peerNS, peerIP := suggestDNSClusterPeer(
			"2.25.80.4",
			pair[0],
			pair[1],
			[]nameserverFact{
				{Host: pair[0], IPs: []string{"72.62.38.15"}},
				{Host: pair[1], IPs: []string{"2.25.80.4"}, PointsHere: true},
			},
		)
		if localNS != "" || peerNS != "" || peerIP != "" {
			t.Fatalf("invalid pair %q/%q produced suggestion (%q, %q, %q)",
				pair[0], pair[1], localNS, peerNS, peerIP)
		}
	}
}

func TestSuggestDNSClusterPeerRejectsNonUnicastLocalServerIPv4(t *testing.T) {
	localNS, peerNS, peerIP := suggestDNSClusterPeer(
		"127.0.0.1",
		"ns1.celikhost.com",
		"ns2.celikhost.com",
		[]nameserverFact{
			{Host: "ns1.celikhost.com", IPs: []string{"72.62.38.15"}},
			{Host: "ns2.celikhost.com", IPs: []string{"127.0.0.1"}, PointsHere: true},
		},
	)
	if localNS != "" || peerNS != "" || peerIP != "" {
		t.Fatalf("non-unicast local server produced suggestion (%q, %q, %q)", localNS, peerNS, peerIP)
	}
}
