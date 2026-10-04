package pdnspeerproof

import (
	"github.com/alicelik/celikpanel/internal/dnspeerproof"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T) (RequestV1, ResponseV1, PeerAuthentication) {
	t.Helper()
	members, err := CatalogMembersSHA256("192.0.2.10", 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := RequestV1{Schema: RequestSchemaV1, MutationRequestID: strings.Repeat("a", 32), MutationOwnerID: strings.Repeat("b", 32), DeletionGeneration: 3, DeletionQualifier: "dns-zone-sync/v3:sha256:" + strings.Repeat("c", 64), PrimaryIP: "192.0.2.10", PeerIP: "192.0.2.11", PeerIdentitySHA256: strings.Repeat("d", 64), CatalogName: "catalog-c000020a.celikpanel.invalid", CatalogSerial: 2, CatalogMembersSHA256: members, DeletedZone: "s1-kill.test", View: dnspeerproof.DefaultView, Nonce: strings.Repeat("e", 64), Attempt: 1, IssuedAtUnix: 1800000000, ExpiresAtUnix: 1800000060}
	digest, err := RequestSHA256(r)
	if err != nil {
		t.Fatal(err)
	}
	s := ResponseV1{Schema: ResponseSchemaV1, RequestSHA256: digest, Nonce: r.Nonce, Attempt: r.Attempt, PrimaryIP: r.PrimaryIP, PeerIP: r.PeerIP, CatalogName: r.CatalogName, CatalogSerial: r.CatalogSerial, CatalogMembersSHA256: r.CatalogMembersSHA256, DeletedZone: r.DeletedZone, View: r.View, CatalogState: "transferred", MemberState: "absent", NativeState: "unloaded", ObservedAtUnix: 1800000002}
	return r, s, PeerAuthentication{Established: true, IdentitySHA256: r.PeerIdentitySHA256, PeerIP: r.PeerIP}
}
func TestDomainSeparationAndReplay(t *testing.T) {
	r, s, peer := fixture(t)
	raw, err := EncodeRequest(r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dnspeerproof.DecodeRequest(raw); err == nil {
		t.Fatal("PDNS challenge accepted as BIND")
	}
	if _, err := DecodeRequest(raw); err != nil {
		t.Fatal(err)
	}
	used := map[string]bool{}
	consume := func(d string) bool {
		if used[d] {
			return false
		}
		used[d] = true
		return true
	}
	result, err := Verify(r, s, peer, time.Unix(1800000003, 0), consume)
	if err != nil || result.RequestID != r.MutationRequestID || !used[result.RequestSHA256] {
		t.Fatalf("valid proof rejected: %+v %v", result, err)
	}
	if _, err := Verify(r, s, peer, time.Unix(1800000003, 0), consume); err == nil {
		t.Fatal("replay accepted")
	}
	base := dnspeerproof.RequestV1(r)
	base.Schema = dnspeerproof.RequestSchemaV1
	bindDigest, _ := dnspeerproof.RequestSHA256(base)
	if bindDigest == result.RequestSHA256 {
		t.Fatal("engine digest collision")
	}
}
func TestMismatchesNeverConsume(t *testing.T) {
	r, s, peer := fixture(t)
	cases := []struct {
		name string
		edit func(*RequestV1, *ResponseV1, *PeerAuthentication)
	}{
		{"unbound peer", func(_ *RequestV1, _ *ResponseV1, p *PeerAuthentication) { p.IdentitySHA256 = strings.Repeat("1", 64) }},
		{"wrong generation", func(r *RequestV1, _ *ResponseV1, _ *PeerAuthentication) { r.DeletionGeneration++ }},
		{"wrong owner", func(r *RequestV1, _ *ResponseV1, _ *PeerAuthentication) { r.MutationOwnerID = strings.Repeat("1", 32) }},
		{"loaded", func(_ *RequestV1, s *ResponseV1, _ *PeerAuthentication) { s.NativeState = "loaded" }},
		{"stale", func(_ *RequestV1, s *ResponseV1, _ *PeerAuthentication) { s.CatalogState = "stale" }},
		{"not absent", func(_ *RequestV1, s *ResponseV1, _ *PeerAuthentication) { s.MemberState = "present" }},
		{"BIND response schema", func(_ *RequestV1, s *ResponseV1, _ *PeerAuthentication) { s.Schema = dnspeerproof.ResponseSchemaV1 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr, ss, pp := r, s, peer
			tc.edit(&rr, &ss, &pp)
			called := false
			if _, err := Verify(rr, ss, pp, time.Unix(1800000003, 0), func(string) bool { called = true; return true }); err == nil || called {
				t.Fatalf("accepted %s: %v", tc.name, err)
			}
		})
	}
}
