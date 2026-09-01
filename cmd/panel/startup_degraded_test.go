package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A failed startup repair must degrade exactly one subsystem, keep the panel
// answering, and refuse only that subsystem's mutations — with the cause
// attached, so the screen can say what is wrong instead of spinning.
func TestDegradedSubsystemIsolatesOneSubsystem(t *testing.T) {
	panel := &Panel{}
	cause := errors.New("committed DNS engine ownership differs from its exact active state")
	panel.markSubsystemDegraded(
		degradedSubsystemServiceOperations,
		errCodeStartupRecoveryFailed,
		cause,
	)

	entry, degraded := panel.subsystemDegraded(degradedSubsystemServiceOperations)
	if !degraded {
		t.Fatal("marked subsystem must read back as degraded")
	}
	if entry.Code != errCodeStartupRecoveryFailed {
		t.Fatalf("stored code = %q, want %q", entry.Code, errCodeStartupRecoveryFailed)
	}
	if entry.Detail != cause.Error() {
		t.Fatalf("stored detail = %q, want the underlying cause", entry.Detail)
	}
	if entry.At.IsZero() {
		t.Fatal("a degraded entry must carry when it happened")
	}

	// Every other subsystem stays operational: one failed repair is not an outage.
	for _, other := range []string{
		degradedSubsystemAppInstalls,
		degradedSubsystemCertificates,
		degradedSubsystemVPN,
		degradedSubsystemMailFilters,
	} {
		if _, bad := panel.subsystemDegraded(other); bad {
			t.Fatalf("subsystem %q must not be degraded by an unrelated failure", other)
		}
	}
}

// The first failure is the diagnosis; a later one is usually its echo and must
// not overwrite the cause the operator needs.
func TestDegradedSubsystemKeepsFirstCause(t *testing.T) {
	panel := &Panel{}
	panel.markSubsystemDegraded(
		degradedSubsystemVPN,
		errCodeStartupRecoveryFailed,
		errors.New("first cause"),
	)
	panel.markSubsystemDegraded(
		degradedSubsystemVPN,
		errCodeInternal,
		errors.New("later echo"),
	)

	entry, _ := panel.subsystemDegraded(degradedSubsystemVPN)
	if entry.Detail != "first cause" || entry.Code != errCodeStartupRecoveryFailed {
		t.Fatalf("later failure overwrote the first diagnosis: %+v", entry)
	}
}

// A subsystem recovers without a process restart — that is the whole point of
// not exiting in the first place.
func TestDegradedSubsystemClears(t *testing.T) {
	panel := &Panel{}
	panel.markSubsystemDegraded(
		degradedSubsystemCertificates,
		errCodeStartupRecoveryFailed,
		errors.New("no startup mutation lease"),
	)
	panel.clearSubsystemDegraded(degradedSubsystemCertificates)
	if _, degraded := panel.subsystemDegraded(degradedSubsystemCertificates); degraded {
		t.Fatal("a cleared subsystem must read back as operational")
	}
	if entries := panel.degradedSubsystems(); len(entries) != 0 {
		t.Fatalf("snapshot must be empty after clearing, got %d", len(entries))
	}
}

// A mutation against a healthy subsystem is untouched.
func TestRequireSubsystemOperationalAllowsHealthy(t *testing.T) {
	panel := &Panel{}
	recorder := httptest.NewRecorder()
	if !panel.requireSubsystemOperational(recorder, degradedSubsystemServiceOperations) {
		t.Fatal("a healthy subsystem must be allowed to mutate")
	}
	if recorder.Code != http.StatusOK || recorder.Body.Len() != 0 {
		t.Fatalf("guard wrote a response for a healthy subsystem: %d %q",
			recorder.Code, recorder.Body.String())
	}
}

// The refusal is a coded 409 carrying the subsystem, the code and the cause.
// A spinner is not an acceptable answer to "this cannot proceed".
func TestRequireSubsystemOperationalRefusesDegraded(t *testing.T) {
	panel := &Panel{}
	cause := errors.New("agent RPC unavailable during startup")
	panel.markSubsystemDegraded(
		degradedSubsystemServiceOperations,
		errCodeStartupRecoveryFailed,
		cause,
	)

	recorder := httptest.NewRecorder()
	if panel.requireSubsystemOperational(recorder, degradedSubsystemServiceOperations) {
		t.Fatal("a degraded subsystem must refuse mutations")
	}
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusConflict)
	}

	var body apiErrorBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("refusal body is not the coded envelope: %v", err)
	}
	if body.Code != errCodeSubsystemDegraded {
		t.Fatalf("code = %q, want %q", body.Code, errCodeSubsystemDegraded)
	}
	if body.Error == "" {
		t.Fatal("refusal must carry a human-readable reason")
	}
	if body.Action == "" {
		t.Fatal("refusal must point somewhere the operator can act")
	}
	// The client gets identity, never the internal cause. httperr.go states the
	// contract in its own header: handlers must never hand internal error text
	// to the client. A startup recovery failure quotes agent state, filesystem
	// paths and request identities, so leaking it here would turn a coded
	// refusal into a reconnaissance surface. The operator reads the cause from
	// the DEGRADED journal line instead.
	for _, detail := range body.Details {
		if strings.Contains(detail, cause.Error()) {
			t.Fatalf("refusal leaked the internal cause to the client: %q", detail)
		}
	}
	if len(body.Details) != 2 ||
		body.Details[0] != degradedSubsystemServiceOperations ||
		body.Details[1] != errCodeStartupRecoveryFailed {
		t.Fatalf("refusal must carry exactly the subsystem and its code, got %v", body.Details)
	}
}

// Diagnostics list every open entry, oldest first, so the first failure of a
// boot reads as the first line.
func TestDegradedSubsystemsSnapshotIsOrdered(t *testing.T) {
	panel := &Panel{}
	panel.markSubsystemDegraded(degradedSubsystemServiceOperations, errCodeStartupRecoveryFailed, errors.New("a"))
	panel.markSubsystemDegraded(degradedSubsystemVPN, errCodeStartupRecoveryFailed, errors.New("b"))
	panel.markSubsystemDegraded(degradedSubsystemMailFilters, errCodeStartupRecoveryFailed, errors.New("c"))

	entries := panel.degradedSubsystems()
	if len(entries) != 3 {
		t.Fatalf("snapshot length = %d, want 3", len(entries))
	}
	for i := 1; i < len(entries); i++ {
		if entries[i].At.Before(entries[i-1].At) {
			t.Fatalf("snapshot is not oldest-first at index %d", i)
		}
	}
}

// A nil panel must not panic: the guard sits on request paths that run before
// and after startup, and a crash there would reintroduce the outage it exists
// to prevent.
func TestDegradedHelpersAreNilSafe(t *testing.T) {
	var panel *Panel
	panel.markSubsystemDegraded(degradedSubsystemVPN, errCodeInternal, errors.New("x"))
	panel.clearSubsystemDegraded(degradedSubsystemVPN)
	if _, degraded := panel.subsystemDegraded(degradedSubsystemVPN); degraded {
		t.Fatal("nil panel must report no degraded subsystem")
	}
	if entries := panel.degradedSubsystems(); entries != nil {
		t.Fatal("nil panel must report an empty snapshot")
	}
	recorder := httptest.NewRecorder()
	if !panel.requireSubsystemOperational(recorder, degradedSubsystemVPN) {
		t.Fatal("nil panel must not refuse on an unknown subsystem")
	}
}

// The migration must tell two failures apart, because startup answers them
// differently: a value that will not open says "this key does not belong to
// this database" (degrade, keep serving so the operator can re-enter it), while
// a value that opens fine but is structurally wrong says "this row is bad"
// (still fatal — the existing contract). Getting this boundary wrong either
// re-bricks a restored panel or boots one with silently broken 2FA.
func TestSealedSecretUnreadableIsDistinguishedFromDataCorruption(t *testing.T) {
	tests := []struct {
		name       string
		stored     func(*Panel) any
		unreadable bool
	}{
		{
			name:       "ciphertext that will not open",
			stored:     func(*Panel) any { return `enc:v1:not-base64` },
			unreadable: true,
		},
		{
			name: "opens cleanly but is not a valid secret",
			stored: func(panel *Panel) any {
				sealed, err := panel.secrets.Encrypt(`AAAA`)
				if err != nil {
					t.Fatalf("seal fixture secret: %v", err)
				}
				return sealed
			},
			unreadable: false,
		},
		{
			name:       "enabled account with no secret at all",
			stored:     func(*Panel) any { return nil },
			unreadable: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			panel, database, _ := newTOTPSecurityPanel(t)
			if _, err := database.GetDB().Exec(`
				INSERT INTO users (username, password_hash, email, role, status, totp_secret, totp_enabled)
				VALUES ('classify-user', 'hash', 'classify@example.test', 'customer', 'active', ?, 1)`,
				test.stored(panel)); err != nil {
				t.Fatal(err)
			}

			err := panel.encryptLegacyTOTPSecrets(t.Context())
			if err == nil {
				t.Fatal("migration must still refuse to commit on a bad row")
			}
			if got := errors.Is(err, errSealedSecretUnreadable); got != test.unreadable {
				t.Fatalf("errors.Is(err, errSealedSecretUnreadable) = %v, want %v (err: %v)",
					got, test.unreadable, err)
			}
		})
	}
}
