package transport

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSystemUpdateSecurityIntegersRemainJSONStrings(t *testing.T) {
	response := SystemUpdateCheckResponse{TargetSequence: "9223372036854775807", TargetArchiveSize: "2147483648"}
	raw, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(raw)
	for _, field := range []string{`"target_sequence":"9223372036854775807"`, `"target_archive_size":"2147483648"`} {
		if !strings.Contains(encoded, field) {
			t.Fatalf("JSON %s lacks exact string field %s", encoded, field)
		}
	}
	if AgentCapabilitySystemUpdateV1 != "system_update_v1" {
		t.Fatalf("capability = %q", AgentCapabilitySystemUpdateV1)
	}
}
