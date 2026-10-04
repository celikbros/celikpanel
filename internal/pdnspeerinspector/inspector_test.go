package pdnspeerinspector

import (
	"context"
	"github.com/alicelik/celikpanel/internal/dnspeerproof"
	"github.com/alicelik/celikpanel/internal/pdnspeerproof"
	"strings"
	"testing"
	"time"
)

type testPolicy struct {
	p      OwnerPolicyV1
	hash   string
	reads  int
	change bool
}

func (p *testPolicy) Read(context.Context) (OwnerPolicyV1, string, error) {
	p.reads++
	if p.change && p.reads > 1 {
		return p.p, "changed", nil
	}
	return p.p, p.hash, nil
}

type testReader struct {
	first, second Snapshot
	reads         int
}

func (r *testReader) Read(context.Context, pdnspeerproof.RequestV1, OwnerPolicyV1) (Snapshot, error) {
	r.reads++
	if r.reads == 1 {
		return r.first, nil
	}
	return r.second, nil
}
func fixture(t *testing.T) (pdnspeerproof.RequestV1, OwnerPolicyV1, Snapshot) {
	t.Helper()
	digest, err := pdnspeerproof.CatalogMembersSHA256("192.0.2.10", 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	req := pdnspeerproof.RequestV1{Schema: pdnspeerproof.RequestSchemaV1, MutationRequestID: strings.Repeat("a", 32), MutationOwnerID: strings.Repeat("b", 32), DeletionGeneration: 3, DeletionQualifier: "dns-zone-sync/v3:sha256:" + strings.Repeat("c", 64), PrimaryIP: "192.0.2.10", PeerIP: "192.0.2.11", PeerIdentitySHA256: strings.Repeat("d", 64), CatalogName: "catalog-c000020a.celikpanel.invalid", CatalogSerial: 2, CatalogMembersSHA256: digest, DeletedZone: "s1-kill.test", View: dnspeerproof.DefaultView, Nonce: strings.Repeat("e", 64), Attempt: 1, IssuedAtUnix: 1800000000, ExpiresAtUnix: 1800000060}
	policy := OwnerPolicyV1{Schema: PolicySchemaV1, PrimaryIP: req.PrimaryIP, PeerIP: req.PeerIP, CatalogName: req.CatalogName, CatalogAccount: "fixture-pdns-peer"}
	snap := Snapshot{ProcessID: 123, ProcessStartTicks: 456, ConfigSHA256: strings.Repeat("f", 64), DatabaseDevice: 1, DatabaseInode: 2, ListenersVerified: true, ControlSocketVerified: true, CatalogSerial: 2, CatalogTransferred: true, ZoneState: "unloaded"}
	return req, policy, snap
}
func TestExactNativeUnloadedNeedsTwoStableObservations(t *testing.T) {
	r, p, s := fixture(t)
	reader := &testReader{first: s, second: s}
	policy := &testPolicy{p: p, hash: "hash"}
	response, err := Inspect(context.Background(), r, reader, policy, func() time.Time { return time.Unix(1800000003, 0) })
	if err != nil || response.CatalogState != "transferred" || response.MemberState != "absent" || response.NativeState != "unloaded" || reader.reads != 2 || policy.reads != 2 {
		t.Fatalf("exact observation: %+v %v", response, err)
	}
}
func TestChangedOrIncompleteNativeEvidenceNeverUnloads(t *testing.T) {
	r, p, s := fixture(t)
	tests := []struct {
		name string
		edit func(*Snapshot)
	}{{"control socket unknown", func(s *Snapshot) { s.ControlSocketVerified = false }}, {"DB inode changed", func(s *Snapshot) { s.DatabaseInode++ }}, {"process restarted", func(s *Snapshot) { s.ProcessStartTicks++ }}, {"catalog serial stale", func(s *Snapshot) { s.CatalogSerial++ }}, {"member still loaded", func(s *Snapshot) { s.ZoneState = "loaded" }}, {"member PTR present", func(s *Snapshot) { s.CatalogMembers = []string{"s1-kill.test"} }}}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			second := s
			tc.edit(&second)
			response, err := Inspect(context.Background(), r, &testReader{first: s, second: second}, &testPolicy{p: p, hash: "hash"}, func() time.Time { return time.Unix(1800000003, 0) })
			if err != nil {
				t.Fatal(err)
			}
			if response.CatalogState == "transferred" && response.MemberState == "absent" && response.NativeState == "unloaded" {
				t.Fatal("unstable native evidence asserted absence")
			}
		})
	}
}
func TestOwnerPolicyChangeAndIdentityFailClosed(t *testing.T) {
	r, p, s := fixture(t)
	if _, err := Inspect(context.Background(), r, &testReader{first: s, second: s}, &testPolicy{p: p, hash: "hash", change: true}, func() time.Time { return time.Unix(1800000003, 0) }); err == nil {
		t.Fatal("owner edit accepted")
	}
	p.PeerIP = "192.0.2.12"
	if _, err := Inspect(context.Background(), r, &testReader{first: s, second: s}, &testPolicy{p: p, hash: "hash"}, func() time.Time { return time.Unix(1800000003, 0) }); err == nil {
		t.Fatal("wrong peer accepted")
	}
}
