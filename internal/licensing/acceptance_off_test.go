//go:build !acceptance_license

package licensing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// In an ordinary build the panel constructor is exactly New: the hook resolves
// to the stub, no seam exists, the status JSON has no acceptance fields, and a
// fixture-shaped receipt or key is treated like anything else.
func TestOrdinaryBuildResolvesTheAcceptanceHookToTheStub(t *testing.T) {
	if AcceptanceFixtureBuild {
		t.Fatal("ordinary build reports the acceptance fixture build")
	}
	pub, _, c, _ := fixture(t)
	dir := t.TempDir()
	m, err := NewServer(filepath.Join(dir, "license.json"), pub, c.ServerID)
	if err != nil {
		t.Fatal(err)
	}
	if m.seam != nil || m.endpoint != API {
		t.Fatalf("ordinary NewServer differs from New: seam=%v endpoint=%q", m.seam, m.endpoint)
	}
	fixtureReceipt := `{"format":"celikpanel-acceptance-fixture-license/v1","holder":"x","product":"celikpanel-acceptance-fixture"}`
	if err = os.WriteFile(filepath.Join(dir, "acceptance-fixture-license.json"), []byte(fixtureReceipt), 0600); err != nil {
		t.Fatal(err)
	}
	status := m.Status()
	if status.State != "missing" || status.CanProvision {
		t.Fatalf("ordinary build accepted a fixture receipt: %+v", status)
	}
	encoded, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err = json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	for key := range fields {
		if key != "state" && key != "observation" && key != "can_provision" {
			t.Fatalf("ordinary status JSON has an extra field %q: %s", key, encoded)
		}
	}
	// A fixture-shaped key is an ordinary key here: it goes to the configured
	// license service (a local stub in this test) and is refused there.
	calls := 0
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_license"}`))
	}))
	defer service.Close()
	m.endpoint = service.URL + "/"
	if err = m.Activate(context.Background(), "CPK-acce57f1c7"+strings.Repeat("0", 54), "guest"); err == nil || calls != 1 {
		t.Fatalf("ordinary activation did not use the license service: err=%v calls=%d", err, calls)
	}
	if m.Status().CanProvision {
		t.Fatal("ordinary build granted access")
	}
}
