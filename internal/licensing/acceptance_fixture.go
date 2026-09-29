//go:build acceptance_license

// This file compiles only with the acceptance_license test tag. That build is
// produced solely by deploy/e2e/dns-pair-acceptance/scripts/build-dist.sh
// --acceptance-license for disposable QEMU acceptance guests. It is never a
// CelikPanel release: make dist and the signed-release writer refuse any
// artifact built with this tag or carrying AcceptanceFixtureHolder
// (deploy/release-acceptance-license-guard.sh). Ordinary builds compile
// acceptance_off.go instead, so the license policy of every customer build is
// unchanged.
//
// In this build the panel never contacts the license service. It accepts
// exactly one fixture license, only on a guest that carries the fixture's
// root-owned cloud-init marker naming its cell. Anywhere else it grants
// nothing, so a tagged binary copied to a real server cannot manage it.

package licensing

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
)

// AcceptanceFixtureBuild is true only in the acceptance_license test build.
const AcceptanceFixtureBuild = true

const (
	// AcceptanceFixtureHolder is shown in the license status and scanned for by
	// release packaging; it must never appear in a release binary.
	AcceptanceFixtureHolder    = "ACCEPTANCE FIXTURE — NOT FOR PRODUCTION"
	AcceptanceFixtureKind      = "acceptance_fixture"
	AcceptanceFixtureProduct   = "celikpanel-acceptance-fixture"
	AcceptanceFixtureLicenseID = "acceptance-fixture"
	// AcceptanceFixtureKey has the License screen's key format so activation
	// runs through the owner-visible flow. It is not secret and not a customer key.
	AcceptanceFixtureKey           = "CPK-acce57f1c7000000000000000000000000000000000000000000000000000000"
	AcceptanceFixtureReceiptFormat = "celikpanel-acceptance-fixture-license/v1"
	AcceptanceFixtureReceiptName   = "acceptance-fixture-license.json"
	AcceptanceLicenseService       = "not contacted: acceptance test build"

	// The kill-matrix fixture (deploy/e2e/dns-kill-matrix/fixture.py
	// cloud_init_files) writes this root-owned 0444 marker on each disposable
	// guest and starts QEMU with -uuid uuid5(namespace, cell "\x00" node).
	AcceptanceGuestMarker = "/etc/celikpanel-dns-kill-matrix"
	AcceptanceGuestSchema = "celikpanel/dns-kill-fixture-plan/v1"
	AcceptanceGuestSMBIOS = "/sys/class/dmi/id/product_uuid"

	acceptanceGuestNamespace  = "718f806f-5f2f-4a43-8fa0-88b642d2d432"
	acceptanceFixtureLifetime = 7 * 24 * time.Hour
)

var (
	acceptanceCellPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{1,239}$`)
	acceptanceNodePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)
	acceptanceServiceDial atomic.Int64
)

// acceptanceStatus labels the fixture license in the license status JSON so
// screenshots and evidence cannot be mistaken for a customer license.
type acceptanceStatus struct {
	LicenseKind          string `json:"license_kind,omitempty"`
	LicenseLabel         string `json:"license_label,omitempty"`
	LicenseService       string `json:"license_service,omitempty"`
	AcceptanceGuest      string `json:"acceptance_guest,omitempty"`
	AcceptanceCell       string `json:"acceptance_cell,omitempty"`
	AcceptanceNode       string `json:"acceptance_node,omitempty"`
	AcceptanceSMBIOSUUID string `json:"acceptance_smbios_uuid,omitempty"`
}

// AcceptanceGuest names where a disposable guest proves its identity.
// NewServer always uses DefaultAcceptanceGuest; tests pass temporary paths.
type AcceptanceGuest struct {
	MarkerPath  string
	MarkerOwner uint32
	// SMBIOSPath is compared with the cell's UUID when readable. The panel
	// service user normally cannot read it; the marker then binds alone.
	SMBIOSPath string
}

func DefaultAcceptanceGuest() AcceptanceGuest {
	return AcceptanceGuest{MarkerPath: AcceptanceGuestMarker, MarkerOwner: 0, SMBIOSPath: AcceptanceGuestSMBIOS}
}

// AcceptanceLicenseServiceDialAttempts counts HTTP attempts that reached the
// refusing transport of an acceptance manager. It stays zero in correct use.
func AcceptanceLicenseServiceDialAttempts() int64 { return acceptanceServiceDial.Load() }

type acceptanceNoServiceTransport struct{}

func (acceptanceNoServiceTransport) RoundTrip(*http.Request) (*http.Response, error) {
	acceptanceServiceDial.Add(1)
	return nil, errors.New("the acceptance test build never contacts the license service")
}

// NewServer is the panel's license constructor. In this test build it always
// installs the acceptance seam and never uses the production license service.
func NewServer(file string, key ed25519.PublicKey, server string) (*Manager, error) {
	m, err := NewAcceptanceFixtureManager(file, key, server, DefaultAcceptanceGuest())
	if err != nil {
		return nil, err
	}
	status := m.Status()
	log.Printf("%s: this panel was built with the acceptance_license test tag; it never contacts the license service and accepts only the acceptance fixture license on a disposable acceptance guest (guest: %s)",
		AcceptanceFixtureHolder, status.AcceptanceGuest)
	return m, nil
}

// NewAcceptanceFixtureManager exists only in this test build.
func NewAcceptanceFixtureManager(file string, key ed25519.PublicKey, server string, guest AcceptanceGuest) (*Manager, error) {
	m, err := New(file, key, server)
	if err != nil {
		return nil, err
	}
	m.endpoint = ""
	m.client = &http.Client{Transport: acceptanceNoServiceTransport{}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	m.seam = &acceptanceFixture{m: m, guest: guest, file: filepath.Join(filepath.Dir(file), AcceptanceFixtureReceiptName)}
	return m, nil
}

type acceptanceFixture struct {
	m     *Manager
	guest AcceptanceGuest
	file  string
	// verifiedAt is the Unix time of the last successful local verification.
	// The same one-minute window as a customer license applies to it.
	verifiedAt atomic.Int64
}

type acceptanceGuestIdentity struct {
	cell   string
	node   string
	smbios string
}

type acceptanceReceipt struct {
	Format      string `json:"format"`
	Holder      string `json:"holder"`
	Product     string `json:"product"`
	LicenseID   string `json:"license_id"`
	ServerID    string `json:"server_id"`
	CellID      string `json:"cell_id"`
	Node        string `json:"node"`
	ActivatedAt int64  `json:"activated_at"`
	ExpiresAt   int64  `json:"expires_at"`
}

func acceptanceGuestUUID(cell, node string) string {
	namespace, _ := hex.DecodeString(strings.ReplaceAll(acceptanceGuestNamespace, "-", ""))
	digest := sha1.New()
	digest.Write(namespace)
	digest.Write([]byte(cell + "\x00" + node))
	b := digest.Sum(nil)[:16]
	b[6] = b[6]&0x0f | 0x50
	b[8] = b[8]&0x3f | 0x80
	x := hex.EncodeToString(b)
	return x[0:8] + "-" + x[8:12] + "-" + x[12:16] + "-" + x[16:20] + "-" + x[20:32]
}

func (g AcceptanceGuest) verify() (acceptanceGuestIdentity, error) {
	var id acceptanceGuestIdentity
	if g.MarkerPath == "" {
		return id, errors.New("no guest marker is configured")
	}
	f, err := openState(g.MarkerPath)
	if err != nil {
		switch {
		case errors.Is(err, errInvalidState):
			return id, errors.New("guest marker is a symbolic link")
		case errors.Is(err, fs.ErrNotExist):
			return id, fmt.Errorf("guest marker %s is absent, so this is not a disposable acceptance guest", g.MarkerPath)
		}
		return id, errors.New("guest marker could not be read")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1024 {
		return id, errors.New("guest marker is not a small regular file")
	}
	if info.Mode().Perm()&0o022 != 0 {
		return id, errors.New("guest marker is writable by group or others")
	}
	if err = acceptanceMarkerOwner(info, g.MarkerOwner); err != nil {
		return id, err
	}
	b, err := io.ReadAll(io.LimitReader(f, 1025))
	if err != nil || len(b) > 1024 {
		return id, errors.New("guest marker could not be read")
	}
	lines := strings.Split(string(b), "\n")
	if len(lines) != 4 || lines[3] != "" || lines[0] != "schema="+AcceptanceGuestSchema ||
		!strings.HasPrefix(lines[1], "cell_id=") || !strings.HasPrefix(lines[2], "node=") {
		return id, errors.New("guest marker does not have the fixture schema, cell and node")
	}
	id.cell = strings.TrimPrefix(lines[1], "cell_id=")
	id.node = strings.TrimPrefix(lines[2], "node=")
	if !acceptanceCellPattern.MatchString(id.cell) || !acceptanceNodePattern.MatchString(id.node) {
		return id, errors.New("guest marker does not name a valid cell and node")
	}
	if g.SMBIOSPath != "" {
		raw, readErr := os.ReadFile(g.SMBIOSPath)
		switch {
		case readErr == nil:
			want := acceptanceGuestUUID(id.cell, id.node)
			if strings.ToLower(strings.TrimSpace(string(raw))) != want {
				return id, errors.New("SMBIOS UUID does not belong to the cell named by the guest marker")
			}
			id.smbios = "matched " + want
		case errors.Is(readErr, fs.ErrPermission), errors.Is(readErr, fs.ErrNotExist):
			id.smbios = "not readable by the panel service user"
		default:
			return id, errors.New("SMBIOS UUID could not be read")
		}
	}
	return id, nil
}

func (a *acceptanceFixture) expected(id acceptanceGuestIdentity, activatedAt int64) acceptanceReceipt {
	return acceptanceReceipt{
		Format: AcceptanceFixtureReceiptFormat, Holder: AcceptanceFixtureHolder, Product: AcceptanceFixtureProduct,
		LicenseID: AcceptanceFixtureLicenseID, ServerID: a.m.server, CellID: id.cell, Node: id.node,
		ActivatedAt: activatedAt, ExpiresAt: activatedAt + int64(acceptanceFixtureLifetime/time.Second),
	}
}

func (a *acceptanceFixture) readReceipt(id acceptanceGuestIdentity, now time.Time) (acceptanceReceipt, error) {
	var r acceptanceReceipt
	f, err := openState(a.file)
	if err != nil {
		return r, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return r, err
	}
	if !info.Mode().IsRegular() || info.Size() > 4096 {
		return r, errInvalidState
	}
	b, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil {
		return r, err
	}
	if len(b) > 4096 {
		return r, errInvalidState
	}
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&r) != nil || decoder.Decode(new(any)) != io.EOF {
		return acceptanceReceipt{}, errInvalidState
	}
	// Exactly one fixture license: every field is fixed except the activation
	// time, and it is bound to this server, cell and node.
	if r.ActivatedAt <= 0 || r.ActivatedAt > now.Add(5*time.Minute).Unix() || r != a.expected(id, r.ActivatedAt) {
		return acceptanceReceipt{}, errInvalidState
	}
	return r, nil
}

func acceptanceRefused(err error) error {
	return fmt.Errorf("this panel is an acceptance test build and works only on a disposable acceptance guest (%v); install a CelikPanel release on this server. No license was changed and the license service was not contacted", err)
}

func (a *acceptanceFixture) status() Status {
	label := acceptanceStatus{LicenseKind: AcceptanceFixtureKind, LicenseLabel: AcceptanceFixtureHolder, LicenseService: AcceptanceLicenseService}
	id, err := a.guest.verify()
	if err != nil {
		label.AcceptanceGuest = "refused: " + err.Error()
		return Status{State: "invalid", Observation: ObservationKnown, acceptanceStatus: label}
	}
	label.AcceptanceGuest = "verified"
	label.AcceptanceCell, label.AcceptanceNode, label.AcceptanceSMBIOSUUID = id.cell, id.node, id.smbios
	now := a.m.clock()
	r, err := a.readReceipt(id, now)
	if err != nil {
		if errors.Is(err, errInvalidState) {
			return Status{State: "invalid", Observation: ObservationKnown, acceptanceStatus: label}
		}
		if errors.Is(err, fs.ErrNotExist) {
			return Status{State: "missing", Observation: ObservationKnown, acceptanceStatus: label}
		}
		return Status{State: "status_unavailable", Observation: ObservationUnavailable, acceptanceStatus: label}
	}
	verified := a.verifiedAt.Load()
	s := Status{State: "active", Observation: ObservationKnown, Product: AcceptanceFixtureProduct, LicenseID: AcceptanceFixtureLicenseID,
		ExpiresAt: r.ExpiresAt, OfflineUntil: min(r.ExpiresAt, verified+int64(CheckInterval/time.Second)), CanProvision: true, acceptanceStatus: label}
	switch unix := now.Unix(); {
	case unix >= r.ExpiresAt:
		s.State, s.CanProvision = "expired", false
	case verified == 0 || unix >= s.OfflineUntil || unix < verified:
		s.State, s.Observation, s.CanProvision = "verification_unavailable", ObservationUnavailable, false
	}
	return s
}

// refresh re-verifies the guest marker and the fixture receipt locally, on the
// same schedule as a customer license. It never opens a network connection.
func (a *acceptanceFixture) refresh(_ context.Context, force bool) error {
	a.m.mu.Lock()
	defer a.m.mu.Unlock()
	now := a.m.clock().Unix()
	if verified := a.verifiedAt.Load(); !force && verified != 0 && now >= verified && now < verified+int64(RefreshInterval/time.Second) {
		return nil
	}
	id, err := a.guest.verify()
	if err != nil {
		a.verifiedAt.Store(0)
		return acceptanceRefused(err)
	}
	if _, err = a.readReceipt(id, a.m.clock()); err != nil {
		a.verifiedAt.Store(0)
		if errors.Is(err, fs.ErrNotExist) {
			return errors.New("the acceptance fixture license is not activated; activate it with the acceptance fixture key")
		}
		return errors.New("the acceptance fixture license receipt does not belong to this guest; activate it again with the acceptance fixture key")
	}
	a.verifiedAt.Store(now)
	return nil
}

func (a *acceptanceFixture) activate(_ context.Context, key, _ string) error {
	a.m.mu.Lock()
	defer a.m.mu.Unlock()
	id, err := a.guest.verify()
	if err != nil {
		return acceptanceRefused(err)
	}
	if key != AcceptanceFixtureKey {
		return errors.New("this panel is an acceptance test build: it accepts only the acceptance fixture key and never contacts the license service. The key was not sent anywhere and the license was not changed")
	}
	now := a.m.clock()
	if _, err = a.readReceipt(id, now); err != nil {
		b, marshalErr := json.Marshal(a.expected(id, now.Unix()))
		if marshalErr != nil {
			return marshalErr
		}
		if err = writeAcceptanceReceipt(a.file, b); err != nil {
			return err
		}
	}
	a.verifiedAt.Store(now.Unix())
	return nil
}

func writeAcceptanceReceipt(file string, b []byte) error {
	dir := filepath.Dir(file)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".acceptance-fixture-license-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0o600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(name, file); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
