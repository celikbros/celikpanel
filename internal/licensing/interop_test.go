package licensing

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestLicensePHPInterop(t *testing.T) {
	path := os.Getenv("CELIKPANEL_LICENSE_INTEROP_FIXTURE")
	if path == "" {
		t.Skip("run deploy/test-membership.sh for fresh PHP signature interoperability")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		PublicKey string   `json:"public_key"`
		ServerID  string   `json:"server_id"`
		Envelope  Envelope `json:"envelope"`
	}
	if err = json.Unmarshal(b, &fixture); err != nil {
		t.Fatal(err)
	}
	pub, err := hex.DecodeString(fixture.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := base64.StdEncoding.DecodeString(fixture.Envelope.Payload)
	var dates Claims
	if err = json.Unmarshal(raw, &dates); err != nil {
		t.Fatal(err)
	}
	c, err := Verify(fixture.Envelope, pub, fixture.ServerID, time.Unix(dates.IssuedAt, 0))
	if err != nil {
		t.Fatal(err)
	}
	if c.Product != "celikpanel" || c.ServerID != fixture.ServerID {
		t.Fatal("wrong PHP entitlement")
	}
}
