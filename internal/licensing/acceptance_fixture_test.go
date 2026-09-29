//go:build acceptance_license && linux

package licensing

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const acceptanceTestCell = "pair-accept__bind-arch__bind-debian13__t1"

type acceptanceTestGuest struct {
	dir   string
	guest AcceptanceGuest
}

func newAcceptanceTestGuest(t *testing.T, cell, node string) *acceptanceTestGuest {
	t.Helper()
	dir := t.TempDir()
	g := &acceptanceTestGuest{dir: dir, guest: AcceptanceGuest{MarkerPath: filepath.Join(dir, "marker"), MarkerOwner: uint32(os.Getuid())}}
	g.write(t, "schema="+AcceptanceGuestSchema+"\ncell_id="+cell+"\nnode="+node+"\n", 0444)
	return g
}

func (g *acceptanceTestGuest) write(t *testing.T, content string, mode os.FileMode) {
	t.Helper()
	_ = os.Remove(g.guest.MarkerPath)
	if err := os.WriteFile(g.guest.MarkerPath, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(g.guest.MarkerPath, mode); err != nil {
		t.Fatal(err)
	}
}

type acceptanceClock struct{ now time.Time }

func (c *acceptanceClock) get() time.Time { return c.now }

// neverDial fails the test if anything tries to reach a license service: the
// HTTP client dials nothing, and the endpoint is a local server that records hits.
func neverDial(t *testing.T, m *Manager) *int {
	t.Helper()
	hits := 0
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		t.Errorf("acceptance mode reached a license service: %s %s", r.Method, r.URL)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(service.Close)
	m.endpoint = service.URL + "/"
	m.client = &http.Client{Transport: &http.Transport{DialContext: func(_ context.Context, network, address string) (net.Conn, error) {
		t.Errorf("acceptance mode dialed %s %s", network, address)
		return nil, errors.New("dial refused by test")
	}}}
	return &hits
}

func newAcceptanceTestManager(t *testing.T, dir, server string, guest AcceptanceGuest, clock *acceptanceClock) *Manager {
	t.Helper()
	pub, _, _, _ := fixture(t)
	m, err := NewAcceptanceFixtureManager(filepath.Join(dir, "license.json"), pub, server, guest)
	if err != nil {
		t.Fatal(err)
	}
	m.clock = clock.get
	return m
}

func TestAcceptanceBuildInstallsTheSeam(t *testing.T) {
	if !AcceptanceFixtureBuild {
		t.Fatal("tagged build does not report the acceptance fixture build")
	}
	pub, _, c, _ := fixture(t)
	m, err := NewServer(filepath.Join(t.TempDir(), "license.json"), pub, c.ServerID)
	if err != nil {
		t.Fatal(err)
	}
	if m.seam == nil || m.endpoint != "" {
		t.Fatalf("tagged NewServer did not install the seam: endpoint=%q", m.endpoint)
	}
	if _, err := m.client.Transport.RoundTrip(httptest.NewRequest(http.MethodPost, "https://celikpanel.net/account/", nil)); err == nil {
		t.Fatal("acceptance transport did not refuse the license service")
	}
	if len(AcceptanceFixtureKey) != 68 || !keyPattern.MatchString(AcceptanceFixtureKey) {
		t.Fatal("fixture key does not have the License screen format")
	}
	if got := acceptanceGuestUUID(acceptanceTestCell, "arch"); got != "c70a9a20-0625-56b4-b06e-22bf733f62f9" {
		t.Fatalf("SMBIOS UUID differs from fixture.py uuid5: %s", got)
	}
}

func TestAcceptanceFixtureLifecycleIsLocalAndLabelled(t *testing.T) {
	guest := newAcceptanceTestGuest(t, acceptanceTestCell, "arch")
	clock := &acceptanceClock{now: time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)}
	_, _, c, _ := fixture(t)
	dataDir := t.TempDir()
	m := newAcceptanceTestManager(t, dataDir, c.ServerID, guest.guest, clock)
	hits := neverDial(t, m)
	dialsBefore := AcceptanceLicenseServiceDialAttempts()
	ctx := context.Background()

	s := m.Status()
	if s.State != "missing" || s.CanProvision || s.LicenseKind != AcceptanceFixtureKind || s.LicenseLabel != AcceptanceFixtureHolder ||
		s.AcceptanceGuest != "verified" || s.AcceptanceCell != acceptanceTestCell || s.AcceptanceNode != "arch" || s.LicenseService != AcceptanceLicenseService {
		t.Fatalf("before activation: %+v", s)
	}
	if m.AccessStatus(ctx).CanProvision || m.CanProvision(ctx) {
		t.Fatal("access before activation")
	}
	customer := "CPK-" + strings.Repeat("1", 64)
	if err := m.Activate(ctx, customer, "guest"); err == nil || !strings.Contains(err.Error(), "acceptance test build") {
		t.Fatalf("customer key accepted or unexplained: %v", err)
	}
	if err := m.Activate(ctx, AcceptanceFixtureKey, "guest"); err != nil {
		t.Fatal(err)
	}
	s = m.Status()
	if s.State != "active" || !s.CanProvision || s.Product != AcceptanceFixtureProduct || s.LicenseID != AcceptanceFixtureLicenseID ||
		s.OfflineUntil != clock.now.Unix()+60 || s.ExpiresAt != clock.now.Add(7*24*time.Hour).Unix() {
		t.Fatalf("after activation: %+v", s)
	}
	info, err := os.Stat(filepath.Join(dataDir, AcceptanceFixtureReceiptName))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("receipt: %v %v", info, err)
	}
	if _, err = os.Stat(filepath.Join(dataDir, "license.json")); !os.IsNotExist(err) {
		t.Fatal("the customer license receipt was touched")
	}
	encoded, _ := json.Marshal(s)
	for _, want := range []string{`"license_kind":"acceptance_fixture"`, `"license_label":"ACCEPTANCE FIXTURE — NOT FOR PRODUCTION"`, `"license_service":"not contacted: acceptance test build"`, `"acceptance_cell":"` + acceptanceTestCell + `"`} {
		if !strings.Contains(string(encoded), want) {
			t.Fatalf("status JSON lacks %s: %s", want, encoded)
		}
	}

	// The periodic re-verification is the customer schedule, done locally.
	clock.now = clock.now.Add(61 * time.Second)
	if s = m.Status(); s.State != "verification_unavailable" || s.CanProvision {
		t.Fatalf("stale verification granted access: %+v", s)
	}
	for i := 0; i < 5; i++ {
		clock.now = clock.now.Add(50 * time.Second)
		if s = m.AccessStatus(ctx); s.State != "active" || !s.CanProvision || s.OfflineUntil != clock.now.Unix()+60 {
			t.Fatalf("local re-verification %d: %+v", i, s)
		}
	}
	if err = m.Refresh(ctx, true); err != nil {
		t.Fatal(err)
	}

	// A restarted panel keeps the fixture and re-verifies it locally.
	restarted := newAcceptanceTestManager(t, dataDir, c.ServerID, guest.guest, clock)
	restartedHits := neverDial(t, restarted)
	if s = restarted.Status(); s.State != "verification_unavailable" {
		t.Fatalf("restart before verification: %+v", s)
	}
	if !restarted.CanProvision(ctx) {
		t.Fatal("restart did not re-verify locally")
	}
	clock.now = clock.now.Add(8 * 24 * time.Hour)
	if s = restarted.AccessStatus(ctx); s.State != "expired" || s.CanProvision {
		t.Fatalf("expired fixture: %+v", s)
	}
	if *hits != 0 || *restartedHits != 0 || AcceptanceLicenseServiceDialAttempts() != dialsBefore {
		t.Fatalf("license service contacted: hits=%d/%d dials=%d", *hits, *restartedHits, AcceptanceLicenseServiceDialAttempts()-dialsBefore)
	}
}

func TestAcceptanceFixtureGrantsNothingWithoutTheGuestMarker(t *testing.T) {
	clock := &acceptanceClock{now: time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)}
	_, _, c, _ := fixture(t)
	ctx := context.Background()
	cases := map[string]func(t *testing.T, g *acceptanceTestGuest){
		"absent":         func(t *testing.T, g *acceptanceTestGuest) { _ = os.Remove(g.guest.MarkerPath) },
		"symlink":        func(t *testing.T, g *acceptanceTestGuest) { acceptanceSymlinkMarker(t, g) },
		"group writable": func(t *testing.T, g *acceptanceTestGuest) { _ = os.Chmod(g.guest.MarkerPath, 0664) },
		"wrong owner":    func(t *testing.T, g *acceptanceTestGuest) { g.guest.MarkerOwner++ },
		"wrong schema": func(t *testing.T, g *acceptanceTestGuest) {
			g.write(t, "schema=other\ncell_id="+acceptanceTestCell+"\nnode=arch\n", 0444)
		},
		"no cell": func(t *testing.T, g *acceptanceTestGuest) {
			g.write(t, "schema="+AcceptanceGuestSchema+"\ncell_id=\nnode=arch\n", 0444)
		},
		"extra line": func(t *testing.T, g *acceptanceTestGuest) {
			g.write(t, "schema="+AcceptanceGuestSchema+"\ncell_id="+acceptanceTestCell+"\nnode=arch\nx=y\n", 0444)
		},
		"smbios mismatch": func(t *testing.T, g *acceptanceTestGuest) {
			g.guest.SMBIOSPath = filepath.Join(g.dir, "product_uuid")
			if err := os.WriteFile(g.guest.SMBIOSPath, []byte("00000000-0000-5000-8000-000000000000\n"), 0400); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			g := newAcceptanceTestGuest(t, acceptanceTestCell, "arch")
			dataDir := t.TempDir()
			// Activated while the marker was valid; the change must revoke access.
			m := newAcceptanceTestManager(t, dataDir, c.ServerID, g.guest, clock)
			hits := neverDial(t, m)
			if err := m.Activate(ctx, AcceptanceFixtureKey, "guest"); err != nil {
				t.Fatal(err)
			}
			change(t, g)
			m = newAcceptanceTestManager(t, dataDir, c.ServerID, g.guest, clock)
			hits2 := neverDial(t, m)
			s := m.AccessStatus(ctx)
			if s.State != "invalid" || s.CanProvision || !strings.HasPrefix(s.AcceptanceGuest, "refused: ") || s.LicenseLabel != AcceptanceFixtureHolder {
				t.Fatalf("marker %s: %+v", name, s)
			}
			if err := m.Activate(ctx, AcceptanceFixtureKey, "guest"); err == nil || !strings.Contains(err.Error(), "disposable acceptance guest") {
				t.Fatalf("activation without a valid marker: %v", err)
			}
			if err := m.Refresh(ctx, true); err == nil {
				t.Fatal("refresh succeeded without a valid marker")
			}
			if *hits != 0 || *hits2 != 0 {
				t.Fatal("license service contacted")
			}
		})
	}
	t.Run("never activated without marker", func(t *testing.T) {
		dataDir := t.TempDir()
		m := newAcceptanceTestManager(t, dataDir, c.ServerID, AcceptanceGuest{MarkerPath: filepath.Join(dataDir, "absent")}, clock)
		if err := m.Activate(ctx, AcceptanceFixtureKey, "guest"); err == nil {
			t.Fatal("activated without a marker")
		}
		if _, err := os.Stat(filepath.Join(dataDir, AcceptanceFixtureReceiptName)); !os.IsNotExist(err) {
			t.Fatal("receipt written without a marker")
		}
	})
}

func acceptanceSymlinkMarker(t *testing.T, g *acceptanceTestGuest) {
	t.Helper()
	target := g.guest.MarkerPath + ".target"
	if err := os.Rename(g.guest.MarkerPath, target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, g.guest.MarkerPath); err != nil {
		t.Fatal(err)
	}
}

func TestAcceptanceFixtureIsBoundToCellNodeAndServer(t *testing.T) {
	clock := &acceptanceClock{now: time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)}
	_, _, c, _ := fixture(t)
	ctx := context.Background()
	g := newAcceptanceTestGuest(t, acceptanceTestCell, "arch")
	g.guest.SMBIOSPath = filepath.Join(g.dir, "product_uuid")
	if err := os.WriteFile(g.guest.SMBIOSPath, []byte(strings.ToUpper(acceptanceGuestUUID(acceptanceTestCell, "arch"))+"\n"), 0400); err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	m := newAcceptanceTestManager(t, dataDir, c.ServerID, g.guest, clock)
	neverDial(t, m)
	if err := m.Activate(ctx, AcceptanceFixtureKey, "guest"); err != nil {
		t.Fatal(err)
	}
	if s := m.Status(); s.State != "active" || !strings.HasPrefix(s.AcceptanceSMBIOSUUID, "matched ") {
		t.Fatalf("matching SMBIOS: %+v", s)
	}
	g.guest.SMBIOSPath = filepath.Join(g.dir, "unreadable-or-absent")
	if s := newAcceptanceTestManager(t, dataDir, c.ServerID, g.guest, clock).AccessStatus(ctx); s.State != "active" ||
		s.AcceptanceSMBIOSUUID != "not readable by the panel service user" {
		t.Fatalf("unreadable SMBIOS: %+v", s)
	}
	receipt, err := os.ReadFile(filepath.Join(dataDir, AcceptanceFixtureReceiptName))
	if err != nil {
		t.Fatal(err)
	}
	other := newAcceptanceTestGuest(t, acceptanceTestCell+"-other", "arch")
	if s := newAcceptanceTestManager(t, dataDir, c.ServerID, other.guest, clock).AccessStatus(ctx); s.State != "invalid" || s.CanProvision {
		t.Fatalf("receipt of another cell accepted: %+v", s)
	}
	node := newAcceptanceTestGuest(t, acceptanceTestCell, "debian13")
	if s := newAcceptanceTestManager(t, dataDir, c.ServerID, node.guest, clock).AccessStatus(ctx); s.State != "invalid" {
		t.Fatalf("receipt of another node accepted: %+v", s)
	}
	if s := newAcceptanceTestManager(t, dataDir, strings.Repeat("d", 64), g.guest, clock).AccessStatus(ctx); s.State != "invalid" {
		t.Fatalf("receipt of another server accepted: %+v", s)
	}
	for _, tampered := range []string{
		strings.Replace(string(receipt), "NOT FOR PRODUCTION", "LICENSED", 1),
		strings.Replace(string(receipt), "}", `,"extra":true}`, 1),
		string(receipt) + "{}",
	} {
		if err = os.WriteFile(filepath.Join(dataDir, AcceptanceFixtureReceiptName), []byte(tampered), 0600); err != nil {
			t.Fatal(err)
		}
		if s := newAcceptanceTestManager(t, dataDir, c.ServerID, g.guest, clock).AccessStatus(ctx); s.State != "invalid" || s.CanProvision {
			t.Fatalf("tampered receipt accepted: %+v", s)
		}
	}
}
