//go:build linux

package pdnspeertransport

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/dnspeerproof"
	"github.com/alicelik/celikpanel/internal/dnspeertransport"
	"github.com/alicelik/celikpanel/internal/pdnspeerproof"
)

type exchangeProbe struct {
	raw     []byte
	auth    pdnspeerproof.PeerAuthentication
	calls   int
	request []byte
}

func (f *exchangeProbe) Exchange(_ context.Context, _ Enrollment, request []byte) ([]byte, pdnspeerproof.PeerAuthentication, error) {
	f.calls++
	f.request = request
	return f.raw, f.auth, nil
}

func transportFixture(t *testing.T) (Enrollment, pdnspeerproof.RequestV1, pdnspeerproof.ResponseV1, pdnspeerproof.PeerAuthentication) {
	t.Helper()
	members, err := pdnspeerproof.CatalogMembersSHA256("192.0.2.10", 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := pdnspeerproof.RequestV1{
		Schema:            pdnspeerproof.RequestSchemaV1,
		MutationRequestID: strings.Repeat("a", 32), MutationOwnerID: strings.Repeat("b", 32),
		DeletionGeneration: 3, DeletionQualifier: "dns-zone-sync/v3:sha256:" + strings.Repeat("c", 64),
		PrimaryIP: "192.0.2.10", PeerIP: "192.0.2.11", PeerIdentitySHA256: strings.Repeat("d", 64),
		CatalogName: "catalog-c000020a.celikpanel.invalid", CatalogSerial: 2,
		CatalogMembersSHA256: members, DeletedZone: "gone.example.test", View: dnspeerproof.DefaultView,
		Nonce: strings.Repeat("e", 64), Attempt: 1, IssuedAtUnix: 1800000000, ExpiresAtUnix: 1800000060,
	}
	digest, err := pdnspeerproof.RequestSHA256(r)
	if err != nil {
		t.Fatal(err)
	}
	s := pdnspeerproof.ResponseV1{
		Schema: pdnspeerproof.ResponseSchemaV1, RequestSHA256: digest, Nonce: r.Nonce, Attempt: r.Attempt,
		PrimaryIP: r.PrimaryIP, PeerIP: r.PeerIP, CatalogName: r.CatalogName, CatalogSerial: r.CatalogSerial,
		CatalogMembersSHA256: r.CatalogMembersSHA256, DeletedZone: r.DeletedZone, View: r.View,
		CatalogState: "transferred", MemberState: "absent", NativeState: "unloaded", ObservedAtUnix: 1800000002,
	}
	e := Enrollment{PeerIP: r.PeerIP, Username: "dnsobserver", HostKeySHA256: r.PeerIdentitySHA256,
		PrivateKeyPath: "/root/.ssh/celikpanel-pdns-observer", Timeout: time.Second}
	a := pdnspeerproof.PeerAuthentication{Established: true, IdentitySHA256: e.HostKeySHA256, PeerIP: e.PeerIP}
	return e, r, s, a
}

func TestInspectPowerDNSWireAndIdentity(t *testing.T) {
	if dnspeertransport.FixedPowerDNSCommand == dnspeertransport.FixedCommand {
		t.Fatal("engines share forced command")
	}
	e, r, s, a := transportFixture(t)
	raw, err := pdnspeerproof.EncodeResponse(s)
	if err != nil {
		t.Fatal(err)
	}
	f := &exchangeProbe{raw: append(raw, '\n'), auth: a}
	got, auth, err := Inspect(context.Background(), e, r, f)
	if err != nil || got != s || auth != a || f.calls != 1 {
		t.Fatalf("valid exchange failed: %v calls=%d", err, f.calls)
	}
	if _, err := pdnspeerproof.DecodeRequest(f.request); err != nil {
		t.Fatal(err)
	}
	if _, err := dnspeerproof.DecodeRequest(f.request); err == nil {
		t.Fatal("PDNS request accepted as BIND")
	}
}

func TestInspectRejectsMismatchedEngineAndPeerBeforeAccepting(t *testing.T) {
	e, r, s, a := transportFixture(t)
	raw, _ := pdnspeerproof.EncodeResponse(s)
	cases := []struct {
		name  string
		edit  func(*Enrollment, *pdnspeerproof.RequestV1, *exchangeProbe)
		code  Code
		calls int
	}{
		{"wrong peer", func(e *Enrollment, _ *pdnspeerproof.RequestV1, _ *exchangeProbe) { e.PeerIP = "192.0.2.12" }, CodeEnrollment, 0},
		{"wrong identity", func(_ *Enrollment, r *pdnspeerproof.RequestV1, _ *exchangeProbe) {
			r.PeerIdentitySHA256 = strings.Repeat("1", 64)
		}, CodeEnrollment, 0},
		{"root user", func(e *Enrollment, _ *pdnspeerproof.RequestV1, _ *exchangeProbe) { e.Username = "root" }, CodeEnrollment, 0},
		{"BIND request", func(_ *Enrollment, r *pdnspeerproof.RequestV1, _ *exchangeProbe) {
			r.Schema = dnspeerproof.RequestSchemaV1
		}, CodeEnrollment, 0},
		{"unverified host", func(_ *Enrollment, _ *pdnspeerproof.RequestV1, f *exchangeProbe) { f.auth.Established = false }, CodeUnavailable, 1},
		{"BIND response", func(_ *Enrollment, _ *pdnspeerproof.RequestV1, f *exchangeProbe) {
			f.raw = []byte(`{"schema":"celikpanel-dns-peer-deletion-observation/v1"}`)
		}, CodeMalformed, 1},
		{"noncanonical response", func(_ *Enrollment, _ *pdnspeerproof.RequestV1, f *exchangeProbe) { f.raw = append([]byte(" "), raw...) }, CodeMalformed, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ee, rr := e, r
			f := &exchangeProbe{raw: raw, auth: a}
			tc.edit(&ee, &rr, f)
			_, _, err := Inspect(context.Background(), ee, rr, f)
			if !IsCode(err, tc.code) || f.calls != tc.calls {
				t.Fatalf("err=%v calls=%d", err, f.calls)
			}
		})
	}
}
