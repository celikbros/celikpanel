package dnspeerproof

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

const fixtureTime int64 = 1_800_000_000

func fixture(t *testing.T) (RequestV1, ResponseV1, PeerAuthentication) {
	t.Helper()
	memberDigest, err := CatalogMembersSHA256("192.0.2.10", 12, []string{"other.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	r := RequestV1{
		Schema:            RequestSchemaV1,
		MutationRequestID: strings.Repeat("a", 32), MutationOwnerID: strings.Repeat("b", 32),
		DeletionGeneration: 17, DeletionQualifier: "dns-zone-sync/v3:sha256:" + strings.Repeat("c", 64),
		PrimaryIP: "192.0.2.10", PeerIP: "192.0.2.11", PeerIdentitySHA256: strings.Repeat("d", 64),
		CatalogName: "catalog-c000020a.celikpanel.invalid", CatalogSerial: 12,
		CatalogMembersSHA256: memberDigest, DeletedZone: "gone.example.test",
		View: DefaultView, Nonce: strings.Repeat("e", 64), Attempt: 3,
		IssuedAtUnix: fixtureTime, ExpiresAtUnix: fixtureTime + 60,
	}
	digest, err := RequestSHA256(r)
	if err != nil {
		t.Fatal(err)
	}
	return r, ResponseV1{
		Schema: ResponseSchemaV1, RequestSHA256: digest, Nonce: r.Nonce, Attempt: r.Attempt,
		PrimaryIP: r.PrimaryIP, PeerIP: r.PeerIP, CatalogName: r.CatalogName,
		CatalogSerial: r.CatalogSerial, CatalogMembersSHA256: r.CatalogMembersSHA256,
		DeletedZone: r.DeletedZone, View: r.View, CatalogState: "transferred",
		MemberState: "absent", NativeState: "unloaded", ObservedAtUnix: fixtureTime + 2,
	}, PeerAuthentication{Established: true, IdentitySHA256: r.PeerIdentitySHA256, PeerIP: r.PeerIP}
}

func TestExactAuthenticatedNativeProofIsSingleUse(t *testing.T) {
	r, response, peer := fixture(t)
	used := map[string]bool{}
	consume := func(digest string) bool {
		if used[digest] {
			return false
		}
		used[digest] = true
		return true
	}
	verified, err := Verify(r, response, peer, time.Unix(fixtureTime+3, 0), consume)
	if err != nil || verified.RequestID != r.MutationRequestID || verified.DeletedZone != r.DeletedZone || len(used) != 1 {
		t.Fatalf("exact proof rejected: %+v %v", verified, err)
	}
	if _, err := Verify(r, response, peer, time.Unix(fixtureTime+3, 0), consume); !IsCode(err, CodeReplay) {
		t.Fatalf("replay accepted: %v", err)
	}
}

func TestContextAndAuthenticationChangesCannotConsumeProof(t *testing.T) {
	r, response, peer := fixture(t)
	count := 0
	consume := func(string) bool { count++; return true }
	tests := []struct {
		name string
		edit func(*RequestV1, *ResponseV1, *PeerAuthentication)
		code Code
	}{
		{"different request", func(r *RequestV1, _ *ResponseV1, _ *PeerAuthentication) {
			r.MutationRequestID = strings.Repeat("1", 32)
		}, CodeMismatch},
		{"different owner", func(r *RequestV1, _ *ResponseV1, _ *PeerAuthentication) { r.MutationOwnerID = strings.Repeat("1", 32) }, CodeMismatch},
		{"different deletion revision", func(r *RequestV1, _ *ResponseV1, _ *PeerAuthentication) { r.DeletionGeneration++ }, CodeMismatch},
		{"different deletion digest", func(r *RequestV1, _ *ResponseV1, _ *PeerAuthentication) {
			r.DeletionQualifier = "dns-zone-sync/v3:sha256:" + strings.Repeat("1", 64)
		}, CodeMismatch},
		{"different nonce", func(r *RequestV1, _ *ResponseV1, _ *PeerAuthentication) { r.Nonce = strings.Repeat("1", 64) }, CodeMismatch},
		{"different attempt", func(r *RequestV1, _ *ResponseV1, _ *PeerAuthentication) { r.Attempt++ }, CodeMismatch},
		{"different catalog serial", func(_ *RequestV1, s *ResponseV1, _ *PeerAuthentication) { s.CatalogSerial++ }, CodeMismatch},
		{"different catalog digest", func(_ *RequestV1, s *ResponseV1, _ *PeerAuthentication) {
			s.CatalogMembersSHA256 = strings.Repeat("1", 64)
		}, CodeMismatch},
		{"different source", func(_ *RequestV1, s *ResponseV1, _ *PeerAuthentication) { s.PrimaryIP = "192.0.2.12" }, CodeMismatch},
		{"different peer", func(_ *RequestV1, s *ResponseV1, _ *PeerAuthentication) { s.PeerIP = "192.0.2.12" }, CodeMismatch},
		{"different zone", func(_ *RequestV1, s *ResponseV1, _ *PeerAuthentication) { s.DeletedZone = "other.example.test" }, CodeMismatch},
		{"different view", func(_ *RequestV1, s *ResponseV1, _ *PeerAuthentication) { s.View = "elsewhere" }, CodeInvalid},
		{"untrusted transport", func(_ *RequestV1, _ *ResponseV1, p *PeerAuthentication) { p.Established = false }, CodeUnauthenticated},
		{"wrong peer identity", func(_ *RequestV1, _ *ResponseV1, p *PeerAuthentication) { p.IdentitySHA256 = strings.Repeat("1", 64) }, CodeUnauthenticated},
		{"wrong peer endpoint", func(_ *RequestV1, _ *ResponseV1, p *PeerAuthentication) { p.PeerIP = "192.0.2.12" }, CodeUnauthenticated},
		{"expired", func(r *RequestV1, _ *ResponseV1, _ *PeerAuthentication) { r.ExpiresAtUnix = fixtureTime + 1 }, CodeExpired},
		{"future observation", func(_ *RequestV1, s *ResponseV1, _ *PeerAuthentication) { s.ObservedAtUnix = fixtureTime + 30 }, CodeExpired},
		{"stale catalog", func(_ *RequestV1, s *ResponseV1, _ *PeerAuthentication) { s.CatalogState = "stale" }, CodeUnverified},
		{"member still present", func(_ *RequestV1, s *ResponseV1, _ *PeerAuthentication) { s.MemberState = "present" }, CodeUnverified},
		{"loaded zone", func(_ *RequestV1, s *ResponseV1, _ *PeerAuthentication) { s.NativeState = "loaded" }, CodeUnverified},
		{"unknown native result", func(_ *RequestV1, s *ResponseV1, _ *PeerAuthentication) { s.NativeState = "unknown" }, CodeUnverified},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req, res, auth := r, response, peer
			tc.edit(&req, &res, &auth)
			if _, err := Verify(req, res, auth, time.Unix(fixtureTime+3, 0), consume); !IsCode(err, tc.code) {
				t.Fatalf("wanted %s, got %v", tc.code, err)
			}
		})
	}
	if count != 0 {
		t.Fatalf("invalid response consumed replay state %d times", count)
	}
}

func TestCanonicalCodecRejectsUnknownAndAmbiguousWire(t *testing.T) {
	r, response, _ := fixture(t)
	requestRaw, err := EncodeRequest(r)
	if err != nil {
		t.Fatal(err)
	}
	if decoded, err := DecodeRequest(requestRaw); err != nil || decoded != r {
		t.Fatalf("request round trip: %+v %v", decoded, err)
	}
	responseRaw, err := EncodeResponse(response)
	if err != nil {
		t.Fatal(err)
	}
	if decoded, err := DecodeResponse(responseRaw); err != nil || decoded != response {
		t.Fatalf("response round trip: %+v %v", decoded, err)
	}
	for _, wire := range [][]byte{
		append([]byte(" "), requestRaw...),
		append(append([]byte(nil), requestRaw...), '\n'),
		append(append([]byte(nil), requestRaw...), []byte("{}")...),
		bytes.Replace(requestRaw, []byte(`"schema":`), []byte(`"unknown":0,"schema":`), 1),
		bytes.Replace(requestRaw, []byte(`"schema":`), []byte(`"schema":"bad","schema":`), 1),
	} {
		if _, err := DecodeRequest(wire); !IsCode(err, CodeInvalid) {
			t.Fatalf("noncanonical request accepted: %q (%v)", wire, err)
		}
	}
	var generic map[string]any
	if err := json.Unmarshal(responseRaw, &generic); err != nil {
		t.Fatal(err)
	}
	delete(generic, "native_state")
	incomplete, _ := json.Marshal(generic)
	if _, err := DecodeResponse(incomplete); !IsCode(err, CodeInvalid) {
		t.Fatalf("missing native result accepted: %v", err)
	}
}

func TestCatalogDigestIsSemanticAndRejectsAmbiguousMembers(t *testing.T) {
	first, err := CatalogMembersSHA256("192.0.2.10", 12, []string{"z.example.test", "a.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	otherOrder, _ := CatalogMembersSHA256("192.0.2.10", 12, []string{"a.example.test", "z.example.test"})
	if first != otherOrder {
		t.Fatal("member order changed the semantic digest")
	}
	changed, _ := CatalogMembersSHA256("192.0.2.10", 13, []string{"a.example.test", "z.example.test"})
	if first == changed {
		t.Fatal("catalog serial did not bind digest")
	}
	for _, members := range [][]string{{"a.example.test", "a.example.test"}, {"A.example.test"}, {"bad..example.test"}} {
		if _, err := CatalogMembersSHA256("192.0.2.10", 12, members); !IsCode(err, CodeInvalid) {
			t.Fatalf("noncanonical members accepted: %v", members)
		}
	}
}

func TestInvalidRequestAndReplayStoreUnavailable(t *testing.T) {
	r, response, peer := fixture(t)
	r.CatalogName = "wrong.example.test"
	if r.Validate() == nil {
		t.Fatal("wrong catalog identity accepted")
	}
	r, response, peer = fixture(t)
	if _, err := Verify(r, response, peer, time.Unix(fixtureTime+3, 0), nil); !IsCode(err, CodeReplay) {
		t.Fatalf("missing durable replay store accepted: %v", err)
	}
	if _, err := Verify(r, response, peer, time.Unix(fixtureTime+3, 0), func(string) bool { return false }); !IsCode(err, CodeReplay) {
		t.Fatalf("unavailable replay store accepted: %v", err)
	}
}

func TestZeroGenerationV3DeletionRemainsProvable(t *testing.T) {
	commitment, err := mutationpayload.CanonicalDNSZoneSyncV3(
		transport.DNSEngineBIND, 1, 0, "gone.example.test", true, "MASTER", nil,
	)
	if err != nil {
		t.Fatalf("canonical V3 deletion rejected generation zero: %v", err)
	}
	request, response, peer := fixture(t)
	request.DeletionGeneration = commitment.DesiredGeneration
	request.DeletionQualifier = commitment.Qualifier
	response.RequestSHA256, err = RequestSHA256(request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(request, response, peer, time.Unix(fixtureTime+3, 0), func(string) bool { return true }); err != nil {
		t.Fatalf("valid zero-generation deletion refused: %v", err)
	}
	request.DeletionGeneration = -1
	if request.Validate() == nil {
		t.Fatal("negative generation accepted")
	}
}
