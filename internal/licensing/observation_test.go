package licensing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLicenseObservationReadFailureAndInvalidEvidenceRemainDistinct(t *testing.T) {
	pub, _, c, e := fixture(t)
	m, err := New(filepath.Join(t.TempDir(), "license.json"), pub, c.ServerID)
	if err != nil {
		t.Fatal(err)
	}
	m.clock = func() time.Time { return time.Unix(c.IssuedAt, 0) }
	assertState := func(want, observation string) {
		t.Helper()
		got := m.AccessStatus(context.Background())
		if got.State != want || got.Observation != observation || got.CanProvision {
			t.Fatalf("want %s/%s denied, got %+v", want, observation, got)
		}
	}
	assertState("missing", ObservationKnown)
	if err := os.WriteFile(m.file, []byte("malformed"), 0600); err != nil {
		t.Fatal(err)
	}
	assertState("invalid", ObservationKnown)
	if err := m.save(e); err != nil {
		t.Fatal(err)
	}
	if s := m.Status(); !s.CanProvision || s.Observation != ObservationKnown {
		t.Fatal(s)
	}
	// A regular file as an ancestor makes the syscall fail before any receipt
	// can be read. It is not evidence about the receipt's signature or expiry.
	original := m.file
	m.file = filepath.Join(original, "unreadable.json")
	assertState("status_unavailable", ObservationUnavailable)
	m.rejected.Store(true)
	assertState("invalid", ObservationKnown)
	m.rejected.Store(false)
	m.file = original
	bad := e
	bad.Signature = "bad"
	if err := m.save(bad); err != nil {
		t.Fatal(err)
	}
	assertState("invalid", ObservationKnown)
}

func TestLicenseObservationOutageKeepsExactDeadlineAndKnownExpiry(t *testing.T) {
	pub, _, c, e := fixture(t)
	m, _ := New(filepath.Join(t.TempDir(), "license.json"), pub, c.ServerID)
	now := time.Unix(c.IssuedAt, 0)
	m.clock = func() time.Time { return now }
	if err := m.save(e); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(m.file)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer server.Close()
	m.endpoint = server.URL
	now = now.Add(45 * time.Second)
	if s := m.AccessStatus(context.Background()); s.State != "active" || s.Observation != ObservationKnown || !s.CanProvision || s.OfflineUntil != c.IssuedAt+60 {
		t.Fatal(s)
	}
	now = now.Add(15 * time.Second)
	if s := m.AccessStatus(context.Background()); s.State != "verification_unavailable" || s.Observation != ObservationUnavailable || s.CanProvision || s.OfflineUntil != c.IssuedAt+60 {
		t.Fatal(s)
	}
	now = time.Unix(c.ExpiresAt, 0)
	if s := m.AccessStatus(context.Background()); s.State != "expired" || s.Observation != ObservationKnown || s.CanProvision {
		t.Fatal(s)
	}
	after, _ := os.ReadFile(m.file)
	if string(before) != string(after) {
		t.Fatal("outage rewrote receipt")
	}
}

func TestLicenseObservationPermissionFailureIsUnavailable(t *testing.T) {
	pub, _, c, e := fixture(t)
	m, _ := New(filepath.Join(t.TempDir(), "license.json"), pub, c.ServerID)
	m.clock = func() time.Time { return time.Unix(c.IssuedAt, 0) }
	if err := m.save(e); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(m.file, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(m.file, 0600) })
	if f, err := openState(m.file); err == nil {
		f.Close()
		t.Skip("current OS/user can read mode 0000")
	}
	if s := m.Status(); s.State != "status_unavailable" || s.Observation != ObservationUnavailable || s.CanProvision {
		t.Fatal(s)
	}
}
