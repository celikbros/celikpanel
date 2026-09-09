// Package licensing verifies server entitlements independently of release trust.
package licensing

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

const API = "https://celikpanel.net/account/"

// Production public key is provisioned separately from the signed-release key.
const PublicKeyHex = "aa7768257096ed17ae78077d1269b0b38f57aae9c544616ee10708d22b656f5e"

var hex64 = regexp.MustCompile(`^[a-f0-9]{64}$`)
var hex32 = regexp.MustCompile(`^[a-f0-9]{32}$`)
var keyPattern = regexp.MustCompile(`^CPK-[a-f0-9]{64}$`)

type Envelope struct {
	Payload         string `json:"payload"`
	Signature       string `json:"signature"`
	ActivationToken string `json:"activation_token"`
}
type Claims struct {
	Format       string `json:"format"`
	Product      string `json:"product"`
	LicenseID    string `json:"license_id"`
	ServerID     string `json:"server_id"`
	IssuedAt     int64  `json:"issued_at"`
	ActivatedAt  int64  `json:"activated_at"`
	ExpiresAt    int64  `json:"expires_at"`
	RefreshAfter int64  `json:"refresh_after"`
	OfflineUntil int64  `json:"offline_until"`
}
type Status struct {
	State        string `json:"state"`
	Product      string `json:"product,omitempty"`
	LicenseID    string `json:"license_id,omitempty"`
	ExpiresAt    int64  `json:"expires_at,omitempty"`
	OfflineUntil int64  `json:"offline_until,omitempty"`
	CanProvision bool   `json:"can_provision"`
}

func ServerID(machineID []byte) (string, error) {
	b := bytes.TrimSpace(machineID)
	if len(b) != 32 {
		return "", errors.New("invalid machine identity")
	}
	raw, err := hex.DecodeString(string(b))
	if err != nil || bytes.Equal(raw, make([]byte, 16)) {
		return "", errors.New("invalid machine identity")
	}
	sum := sha256.Sum256(append([]byte("celikpanel-license-server-v1:"), raw...))
	return hex.EncodeToString(sum[:]), nil
}

func Verify(e Envelope, key ed25519.PublicKey, server string, now time.Time) (Claims, error) {
	var c Claims
	payload, err := base64.StdEncoding.Strict().DecodeString(e.Payload)
	if err != nil || len(payload) > 4096 {
		return c, errors.New("invalid license payload")
	}
	sig, err := base64.StdEncoding.Strict().DecodeString(e.Signature)
	if err != nil || len(key) != ed25519.PublicKeySize || !ed25519.Verify(key, payload, sig) {
		return c, errors.New("invalid license signature")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&c); err != nil {
		return Claims{}, errors.New("invalid license claims")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return Claims{}, errors.New("trailing license data")
	}
	if c.Format != "celikpanel-license-v1" || c.Product != "celikpanel" || !hex32.MatchString(c.LicenseID) ||
		!hex64.MatchString(server) || c.ServerID != server || !hex64.MatchString(e.ActivationToken) ||
		c.ActivatedAt <= 0 || c.IssuedAt < c.ActivatedAt || c.ExpiresAt <= c.ActivatedAt ||
		c.IssuedAt > now.Add(5*time.Minute).Unix() || c.RefreshAfter <= c.IssuedAt ||
		c.RefreshAfter > c.IssuedAt+86400 || c.OfflineUntil > c.IssuedAt+7*86400 ||
		c.OfflineUntil > c.ExpiresAt || c.OfflineUntil <= 0 {
		return Claims{}, errors.New("invalid license binding or dates")
	}
	return c, nil
}

type Manager struct {
	mu       sync.Mutex
	file     string
	key      ed25519.PublicKey
	server   string
	endpoint string
	client   *http.Client
	clock    func() time.Time
}

func New(file string, key ed25519.PublicKey, server string) (*Manager, error) {
	if !filepath.IsAbs(file) || len(key) != 32 || !hex64.MatchString(server) {
		return nil, errors.New("invalid licensing configuration")
	}
	return &Manager{file: file, key: append(ed25519.PublicKey(nil), key...), server: server, endpoint: API,
		client: &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, clock: time.Now}, nil
}
func (m *Manager) read() (Envelope, Claims, error) {
	var e Envelope
	f, err := openState(m.file)
	if err != nil {
		return e, Claims{}, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() > 8192 {
		return e, Claims{}, errors.New("invalid license state file")
	}
	b, err := io.ReadAll(io.LimitReader(f, 8193))
	if err != nil {
		return e, Claims{}, err
	}
	if err = json.Unmarshal(b, &e); err != nil {
		return e, Claims{}, err
	}
	c, err := Verify(e, m.key, m.server, m.clock())
	return e, c, err
}
func (m *Manager) Status() Status {
	_, c, err := m.read()
	if err != nil {
		if os.IsNotExist(err) {
			return Status{State: "missing"}
		}
		return Status{State: "invalid"}
	}
	s := Status{State: "active", Product: c.Product, LicenseID: c.LicenseID, ExpiresAt: c.ExpiresAt, OfflineUntil: c.OfflineUntil, CanProvision: true}
	now := m.clock().Unix()
	if now >= c.ExpiresAt {
		s.State = "expired"
		s.CanProvision = false
	} else if now >= c.OfflineUntil {
		s.State = "verification_unavailable"
		s.CanProvision = false
	}
	return s
}
func (m *Manager) save(e Envelope) error {
	if err := os.MkdirAll(filepath.Dir(m.file), 0700); err != nil {
		return err
	}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(m.file), ".license-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(name, m.file); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(m.file))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
func (m *Manager) request(ctx context.Context, action string, input map[string]string) error {
	body, err := json.Marshal(input)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.endpoint+"?action="+action, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.client.Do(req)
	if err != nil {
		return errors.New("license service unavailable; existing license retained")
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8193))
	if err != nil || len(b) > 8192 {
		return errors.New("invalid license service response")
	}
	if resp.StatusCode != http.StatusOK {
		var problem struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(b, &problem)
		switch problem.Error {
		case "invalid_license", "license_in_use", "license_expired", "invalid_activation", "rate_limited":
			return fmt.Errorf("license: %s", problem.Error)
		}
		return errors.New("license service unavailable; existing license retained")
	}
	var e Envelope
	if err = json.Unmarshal(b, &e); err != nil {
		return errors.New("invalid license service response")
	}
	if _, err = Verify(e, m.key, m.server, m.clock()); err != nil {
		return err
	}
	return m.save(e)
}
func (m *Manager) Activate(ctx context.Context, key, hostname string) error {
	if !keyPattern.MatchString(key) {
		return errors.New("invalid license key")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.request(ctx, "activate", map[string]string{"key": key, "server_id": m.server, "hostname": hostname})
}
func (m *Manager) Refresh(ctx context.Context, force bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, c, err := m.read()
	if err != nil {
		return err
	}
	if !force && m.clock().Unix() < c.RefreshAfter {
		return nil
	}
	return m.request(ctx, "refresh", map[string]string{"license_id": c.LicenseID, "server_id": m.server, "activation_token": e.ActivationToken})
}

// Refresh never tears down running services and keeps the last verified receipt
// when the central service is unavailable. No network access in request gates.
func (m *Manager) Run(ctx context.Context) {
	_ = m.Refresh(ctx, false)
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = m.Refresh(ctx, false)
		}
	}
}
