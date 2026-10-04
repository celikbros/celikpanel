package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/dnspeerproof"
)

func TestMintBINDPeerDeletionRequestBindsAcceptedOperationAndPair(t *testing.T) {
	evidence := testPDNSPrimaryPropagationEvidence(8, nil, nil)
	plan := dnsV3PrimaryPropagationPlan{
		Evidence: evidence,
		Changed:  expectedDNSZoneAuthority{Domain: "gone.example.test", Delete: true},
		Operation: dnsV3DeletionOperation{
			RequestID:  strings.Repeat("a", 32),
			OwnerID:    strings.Repeat("b", 32),
			Generation: 3,
			Qualifier:  "dns-zone-sync/v3:sha256:" + strings.Repeat("c", 64),
		},
	}
	authority := dnsPeerAXFRAuthority{
		sourceIP: evidence.LocalIP, peerIP: evidence.PeerIP,
		catalog: evidence.Domain, catalogSerial: evidence.Serial,
	}
	now := time.Unix(100000, 0)
	mint := func(p dnsV3PrimaryPropagationPlan, a dnsPeerAXFRAuthority, random []byte) (dnspeerproof.RequestV1, error) {
		return mintBINDPeerDeletionRequest(
			p, a, strings.Repeat("d", 64), 2, now, bytes.NewReader(random),
		)
	}
	request, err := mint(plan, authority, bytes.Repeat([]byte{7}, 32))
	if err != nil || request.Validate() != nil {
		t.Fatalf("valid request: %+v err=%v", request, err)
	}
	if request.MutationRequestID != plan.Operation.RequestID ||
		request.MutationOwnerID != plan.Operation.OwnerID ||
		request.DeletionGeneration != plan.Operation.Generation ||
		request.DeletionQualifier != plan.Operation.Qualifier ||
		request.DeletedZone != plan.Changed.Domain ||
		request.CatalogSerial != evidence.Serial ||
		request.Nonce != strings.Repeat("07", 32) ||
		request.ExpiresAtUnix-request.IssuedAtUnix != 30 {
		t.Fatalf("request lost accepted operation or freshness binding: %+v", request)
	}
	if other, err := mint(plan, authority, bytes.Repeat([]byte{8}, 32)); err != nil || other.Nonce == request.Nonce {
		t.Fatalf("fresh attempt reused nonce: %+v err=%v", other, err)
	}
	zeroGeneration := plan
	zeroGeneration.Operation.Generation = 0
	if zero, err := mint(zeroGeneration, authority, bytes.Repeat([]byte{9}, 32)); err != nil || zero.DeletionGeneration != 0 {
		t.Fatalf("valid zero-generation V3 deletion rejected: %+v err=%v", zero, err)
	}
	cases := []struct {
		name   string
		change func(*dnsV3PrimaryPropagationPlan, *dnsPeerAXFRAuthority)
	}{
		{"wrong owner", func(p *dnsV3PrimaryPropagationPlan, _ *dnsPeerAXFRAuthority) { p.Operation.OwnerID = "bad" }},
		{"wrong generation", func(p *dnsV3PrimaryPropagationPlan, _ *dnsPeerAXFRAuthority) { p.Operation.Generation = -1 }},
		{"wrong qualifier", func(p *dnsV3PrimaryPropagationPlan, _ *dnsPeerAXFRAuthority) { p.Operation.Qualifier = "old" }},
		{"wrong peer", func(_ *dnsV3PrimaryPropagationPlan, a *dnsPeerAXFRAuthority) { a.peerIP = "192.0.2.90" }},
		{"wrong catalog", func(_ *dnsV3PrimaryPropagationPlan, a *dnsPeerAXFRAuthority) { a.catalogSerial++ }},
		{"not deletion", func(p *dnsV3PrimaryPropagationPlan, _ *dnsPeerAXFRAuthority) {
			p.Changed.Delete = false
			p.Changed.Serial = 1
		}},
		{"legacy pair", func(p *dnsV3PrimaryPropagationPlan, _ *dnsPeerAXFRAuthority) { p.Legacy = true }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			changedPlan, changedAuthority := plan, authority
			tc.change(&changedPlan, &changedAuthority)
			if _, err := mint(changedPlan, changedAuthority, bytes.Repeat([]byte{7}, 32)); err == nil {
				t.Fatal("unbound native challenge accepted")
			}
		})
	}
	if _, err := mint(plan, authority, []byte{7}); err == nil {
		t.Fatal("short random source accepted")
	}
}
