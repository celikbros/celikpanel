package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/licensing"
)

// Background setup license admission (pair2 finding P-C). The runner judges
// the license through the same refresh the HTTP access gate performs; policy
// (verification, one-minute check, refresh interval, failure semantics) is
// unchanged. Component tests only.

// staleSetupLicense returns a license whose last verification is older than
// licensing.CheckInterval but which the license service still renews. The
// service is replaced by a transport that answers with fresh (or fails).
func staleSetupLicense(t *testing.T, serviceUp *atomic.Bool, calls *atomic.Int64) *licensing.Manager {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Now().Unix()
	c := licensing.Claims{Format: "celikpanel-license-v1", Product: "celikpanel", LicenseID: strings.Repeat("a", 32), ServerID: strings.Repeat("b", 64),
		ActivatedAt: now - 3*86400, IssuedAt: now - 2*int64(licensing.CheckInterval/time.Second), ExpiresAt: now + 365*86400,
		RefreshAfter: now - 60, OfflineUntil: now + 5*86400}
	encode := func(c licensing.Claims) []byte {
		payload, _ := json.Marshal(c)
		body, _ := json.Marshal(licensing.Envelope{Payload: base64.StdEncoding.EncodeToString(payload),
			Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(priv, payload)), ActivationToken: strings.Repeat("c", 64)})
		return body
	}
	stale := encode(c)
	c.IssuedAt, c.RefreshAfter = now, now+3600
	fresh := encode(c)
	previous := http.DefaultTransport
	http.DefaultTransport = licenseTestTransport(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		if !serviceUp.Load() {
			return nil, errors.New("license service unreachable")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(fresh))}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = previous })
	file := filepath.Join(t.TempDir(), "license.json")
	if err := os.WriteFile(file, stale, 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := licensing.New(file, pub, c.ServerID)
	if err != nil {
		t.Fatal(err)
	}
	if status := m.Status(); status.CanProvision || status.State != "verification_unavailable" {
		t.Fatalf("fixture is not a stale check: %+v", status)
	}
	return m
}

func externalDNSSetupExecutionForLicense(t *testing.T) (*setupDNSOutcomeFixture, serverSetupPlan) {
	t.Helper()
	f, state := setupOperationFixture(t)
	plan := serverSetupPlan{Version: serverSetupPlanVersion, Revision: state.Revision, Draft: state.Draft, Purpose: state.Draft.Purpose,
		Actor: serviceOperationActor{UserID: f.userID},
		Steps: []serverSetupPlanStep{{ID: "01-dns", Kind: "dns", Target: "external"}, {ID: "02-verify", Kind: "verify", Target: state.Draft.Purpose}}}
	plan.ID = serverSetupPlanIdentity(plan)
	raw, _ := json.Marshal(plan)
	if _, err := f.database.GetDB().Exec(`INSERT INTO server_setup_plans(id,revision,plan_json,requested_by,created_at) VALUES(?,?,?,?,?)`, plan.ID, plan.Revision, string(raw), f.userID, "now"); err != nil {
		t.Fatal(err)
	}
	execution := serverSetupExecution{ID: strings.Repeat("7", 32), RequestID: strings.Repeat("7", 32), PlanID: plan.ID, Status: "running"}
	for _, step := range plan.Steps {
		execution.Steps = append(execution.Steps, serverSetupExecutionStep{serverSetupPlanStep: step, Status: "pending",
			RequestID: serverSetupID(execution.ID, step.ID, "request"), OwnerID: serverSetupID(execution.ID, step.ID, "owner")})
	}
	raw, _ = json.Marshal(execution)
	if _, err := f.database.GetDB().Exec(`INSERT INTO server_setup_executions(id,request_id,plan_id,status,execution_json,created_at,updated_at) VALUES(?,?,?,?,?,'now','now')`,
		execution.ID, execution.RequestID, plan.ID, execution.Status, string(raw)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.database.GetDB().Exec(`UPDATE server_setup_state SET status='running' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	return &setupDNSOutcomeFixture{f: f, plan: plan, execution: execution}, plan
}

// Stale but valid: no browser request refreshed the check, yet the runner
// refreshes it itself and proceeds without ever showing license_required.
func TestServerSetupRunnerRefreshesStaleValidLicense(t *testing.T) {
	var up atomic.Bool
	var calls atomic.Int64
	up.Store(true)
	x, _ := externalDNSSetupExecutionForLicense(t)
	x.f.panel.license = staleSetupLicense(t, &up, &calls)
	x.poll(t)
	if x.execution.Steps[0].Status != "succeeded" || x.execution.Error != nil || x.execution.Phase == "license" {
		t.Fatalf("stale valid license blocked the runner: %+v", x.execution)
	}
	if calls.Load() != 1 {
		t.Fatalf("license refresh calls=%d, want exactly the one refresh a handler would make", calls.Load())
	}
	if status := x.f.panel.license.Status(); !status.CanProvision || status.State != "active" {
		t.Fatalf("refresh did not renew the check: %+v", status)
	}
}

// Verification unavailable: the runner shows the unknown state with the
// access gate's code, never license_required, and continues once the check
// succeeds.
func TestServerSetupRunnerUnverifiedLicenseIsUnknownNotRequired(t *testing.T) {
	var up atomic.Bool
	var calls atomic.Int64
	x, _ := externalDNSSetupExecutionForLicense(t)
	x.f.panel.license = staleSetupLicense(t, &up, &calls)
	x.poll(t)
	if x.execution.Status != "waiting" || x.execution.Phase == "license" || x.execution.Error == nil ||
		x.execution.Error.Code != errCodeLicenseVerificationUnavailable ||
		!strings.Contains(x.execution.Error.Message, "does not mean the license is missing or invalid") {
		t.Fatalf("unverifiable license shown as: %+v", x.execution)
	}
	if calls.Load() == 0 {
		t.Fatal("the runner did not attempt the refresh")
	}
	// Same failure semantics: the refresh is deferred for RetryInterval after
	// a failure, exactly as for a handler; the next successful check resumes.
	up.Store(true)
	if err := x.f.panel.license.Refresh(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	x.poll(t)
	if x.execution.Steps[0].Status != "succeeded" || x.execution.Error != nil {
		t.Fatalf("setup did not continue after verification: %+v", x.execution)
	}
}

// A license that is known not to be active still waits with license_required.
func TestServerSetupRunnerMissingLicenseStillRequired(t *testing.T) {
	for _, state := range []string{"missing", "expired", "invalid"} {
		t.Run(state, func(t *testing.T) {
			x, _ := externalDNSSetupExecutionForLicense(t)
			x.f.panel.license = testPanelLicense(t, state)
			x.poll(t)
			if x.execution.Status != "waiting" || x.execution.Phase != "license" || x.execution.Error == nil ||
				x.execution.Error.Code != "license_required" || x.execution.Steps[0].Status == "succeeded" {
				t.Fatalf("%s license shown as: %+v", state, x.execution)
			}
		})
	}
}

// An unreadable license status (no license manager) is unknown, not invalid.
func TestServerSetupAdmissionStatusUnavailableIsUnknown(t *testing.T) {
	p := &Panel{}
	err := p.requireServerSetupAdmission(context.Background())
	var unverified *serverSetupLicenseUnverifiedError
	if !errors.As(err, &unverified) || errors.Is(err, errServerSetupLicenseRequired) {
		t.Fatalf("status unavailable admission=%v", err)
	}
	if failure := serverSetupLicenseUnverifiedFailure(err); failure.Code != errCodeLicenseStatusUnavailable {
		t.Fatalf("failure=%+v", failure)
	}
}
