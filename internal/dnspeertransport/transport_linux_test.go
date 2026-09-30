package dnspeertransport

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/dnspeerproof"
)

type fakeExchange struct {
	raw    []byte
	auth   dnspeerproof.PeerAuthentication
	err    error
	called int
}

func (f *fakeExchange) Exchange(_ context.Context, _ Enrollment, _ []byte) ([]byte, dnspeerproof.PeerAuthentication, error) {
	f.called++
	return f.raw, f.auth, f.err
}

func proofFixture(t *testing.T) (Enrollment, dnspeerproof.RequestV1, dnspeerproof.ResponseV1, dnspeerproof.PeerAuthentication) {
	t.Helper()
	members, err := dnspeerproof.CatalogMembersSHA256("192.0.2.10", 12, []string{"other.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	r := dnspeerproof.RequestV1{
		Schema:            dnspeerproof.RequestSchemaV1,
		MutationRequestID: strings.Repeat("a", 32), MutationOwnerID: strings.Repeat("b", 32),
		DeletionGeneration: 17, DeletionQualifier: "dns-zone-sync/v3:sha256:" + strings.Repeat("c", 64),
		PrimaryIP: "192.0.2.10", PeerIP: "192.0.2.11", PeerIdentitySHA256: strings.Repeat("d", 64),
		CatalogName: "catalog-c000020a.celikpanel.invalid", CatalogSerial: 12,
		CatalogMembersSHA256: members, DeletedZone: "gone.example.test", View: dnspeerproof.DefaultView,
		Nonce: strings.Repeat("e", 64), Attempt: 3, IssuedAtUnix: 1800000000, ExpiresAtUnix: 1800000060,
	}
	digest, err := dnspeerproof.RequestSHA256(r)
	if err != nil {
		t.Fatal(err)
	}
	response := dnspeerproof.ResponseV1{
		Schema: dnspeerproof.ResponseSchemaV1, RequestSHA256: digest, Nonce: r.Nonce, Attempt: r.Attempt,
		PrimaryIP: r.PrimaryIP, PeerIP: r.PeerIP, CatalogName: r.CatalogName, CatalogSerial: r.CatalogSerial,
		CatalogMembersSHA256: r.CatalogMembersSHA256, DeletedZone: r.DeletedZone, View: r.View,
		CatalogState: "transferred", MemberState: "absent", NativeState: "unloaded", ObservedAtUnix: 1800000002,
	}
	enrollment := Enrollment{PeerIP: r.PeerIP, Username: "dnsobserver", HostKeySHA256: r.PeerIdentitySHA256,
		PrivateKeyPath: "/root/.ssh/celikpanel-dns-observer", Timeout: 3 * time.Second}
	auth := dnspeerproof.PeerAuthentication{Established: true, IdentitySHA256: enrollment.HostKeySHA256, PeerIP: enrollment.PeerIP}
	return enrollment, r, response, auth
}

func TestInspectOneBoundedAuthenticatedDocument(t *testing.T) {
	enrollment, request, response, auth := proofFixture(t)
	raw, err := dnspeerproof.EncodeResponse(response)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeExchange{raw: append(raw, '\n'), auth: auth}
	got, gotAuth, err := Inspect(context.Background(), enrollment, request, f)
	if err != nil || got != response || gotAuth != auth || f.called != 1 {
		t.Fatalf("unexpected proof: %v %+v %+v %d", err, got, gotAuth, f.called)
	}
}

func TestInspectRejectsIdentityAndBadWire(t *testing.T) {
	enrollment, request, response, auth := proofFixture(t)
	raw, _ := dnspeerproof.EncodeResponse(response)
	tests := []struct {
		name  string
		edit  func(*Enrollment, *dnspeerproof.RequestV1, *fakeExchange)
		code  Code
		calls int
	}{
		{"wrong enrolled peer", func(e *Enrollment, _ *dnspeerproof.RequestV1, _ *fakeExchange) { e.PeerIP = "192.0.2.12" }, CodeEnrollment, 0},
		{"wrong request identity", func(_ *Enrollment, r *dnspeerproof.RequestV1, _ *fakeExchange) {
			r.PeerIdentitySHA256 = strings.Repeat("1", 64)
		}, CodeEnrollment, 0},
		{"root login", func(e *Enrollment, _ *dnspeerproof.RequestV1, _ *fakeExchange) { e.Username = "root" }, CodeEnrollment, 0},
		{"unauthenticated", func(_ *Enrollment, _ *dnspeerproof.RequestV1, f *fakeExchange) { f.auth.Established = false }, CodeUnavailable, 1},
		{"wrong SSH host key", func(_ *Enrollment, _ *dnspeerproof.RequestV1, f *fakeExchange) {
			f.auth.IdentitySHA256 = strings.Repeat("1", 64)
		}, CodeUnavailable, 1},
		{"network failure", func(_ *Enrollment, _ *dnspeerproof.RequestV1, f *fakeExchange) { f.err = context.DeadlineExceeded }, CodeUnavailable, 1},
		{"overlong response", func(_ *Enrollment, _ *dnspeerproof.RequestV1, f *fakeExchange) {
			f.raw = bytes.Repeat([]byte{'x'}, maxWireSize+2)
		}, CodeMalformed, 1},
		{"noncanonical response", func(_ *Enrollment, _ *dnspeerproof.RequestV1, f *fakeExchange) { f.raw = append([]byte(" "), raw...) }, CodeMalformed, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, r := enrollment, request
			f := &fakeExchange{raw: raw, auth: auth}
			tt.edit(&e, &r, f)
			_, _, err := Inspect(context.Background(), e, r, f)
			if !IsCode(err, tt.code) || f.called != tt.calls {
				t.Fatalf("got %v calls %d", err, f.called)
			}
		})
	}
}

func TestOwnerKeyFileIsRestricted(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-owned credential validation requires root")
	}
	dir, err := os.MkdirTemp("/root", "dns-peer-key-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "id")
	if err := os.WriteFile(path, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readOwnerKey(path); err != nil {
		t.Fatalf("safe key refused: %v", err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := readOwnerKey(path); err == nil {
		t.Fatal("group/world-readable key accepted")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "alias")
	if err := os.Symlink(path, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := readOwnerKey(alias); err == nil {
		t.Fatal("symlink key accepted")
	}
	if err := os.Chmod(dir, 0777); err != nil {
		t.Fatal(err)
	}
	if _, err := readOwnerKey(path); err == nil {
		t.Fatal("writable key directory accepted")
	}
}

// An authenticated forced command that exited non-zero may name one reviewed
// reason bound to this exact request; anything else stays a bare unavailable.
func TestInspectKeepsOnlyADigestBoundReviewedInspectorReason(t *testing.T) {
	enrollment, request, _, _ := proofFixture(t)
	digest, err := dnspeerproof.RequestSHA256(request)
	if err != nil {
		t.Fatal(err)
	}
	line, ok := dnspeerproof.FormatInspectorReason(digest, "catalog_transfer_refused")
	if !ok {
		t.Fatal("reviewed reason line was not formatted")
	}
	pdnsOnly, ok := dnspeerproof.FormatInspectorReason(digest, "config_unreviewed")
	if !ok {
		t.Fatal("PowerDNS reason line was not formatted")
	}
	other := strings.Repeat("f", 64)
	for stderr, want := range map[string]string{
		// A PowerDNS-only token from the BIND channel is not a BIND detail.
		pdnsOnly + "\n": "",
		line + "\n":     "catalog_transfer_refused",
		line:            "catalog_transfer_refused",
		dnspeerproof.InspectorReasonPrefixV1 + " " + other + " catalog_transfer_refused\n": "",
		dnspeerproof.InspectorReasonPrefixV1 + " " + digest + " made_up\n":                 "",
		"sudo: warning\n" + line + "\n":                                                    "",
		"native BIND peer observation unavailable\n":                                       "",
		"": "",
	} {
		f := &fakeExchange{err: commandFailure{stderr: []byte(stderr)}}
		_, _, err := Inspect(context.Background(), enrollment, request, f)
		if !IsCode(err, CodeUnavailable) || InspectorReason(err) != want {
			t.Fatalf("stderr %q: err=%v reason=%q want %q", stderr, err, InspectorReason(err), want)
		}
	}
	f := &fakeExchange{err: context.DeadlineExceeded}
	if _, _, err := Inspect(context.Background(), enrollment, request, f); InspectorReason(err) != "" {
		t.Fatal("a transport failure carried an inspector reason")
	}
}
