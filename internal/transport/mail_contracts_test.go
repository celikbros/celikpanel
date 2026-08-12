package transport

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestSyncMailTLSV2RequestWireRoundTripExcludesManagedRoot(t *testing.T) {
	request := SyncMailTLSV2Request{
		ServiceMutationBinding: ServiceMutationBinding{
			MutationRequestID: "11111111111111111111111111111111",
			MutationOwnerID:   "22222222222222222222222222222222",
		},
		ExpectedBuildCommit: "paired-build",
		Myhostname:          "mx.example.test",
		SNI: []MailSNIEntry{{
			Names: []string{"example.test", "mail.example.test"},
			CertPath: "/etc/ssl/celikpanel/example.test/sha256-" +
				strings.Repeat("a", 64) + "/fullchain.pem",
			KeyPath: "/etc/ssl/celikpanel/example.test/sha256-" +
				strings.Repeat("a", 64) + "/privkey.pem",
		}},
	}
	wire, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(wire), "managed_root") {
		t.Fatalf("client-controlled managed root leaked onto Mail TLS V2 wire: %s", wire)
	}
	for _, field := range []string{
		`"mutation_request_id"`, `"mutation_owner_id"`,
		`"expected_build_commit"`, `"myhostname"`, `"sni"`,
	} {
		if !strings.Contains(string(wire), field) {
			t.Fatalf("Mail TLS V2 wire is missing %s: %s", field, wire)
		}
	}
	var decoded SyncMailTLSV2Request
	if err := json.Unmarshal(wire, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, request) {
		t.Fatalf("Mail TLS V2 roundtrip = %+v, want %+v", decoded, request)
	}
}
