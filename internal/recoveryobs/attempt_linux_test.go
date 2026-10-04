//go:build linux

package recoveryobs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The update check reads the latest attempt to the offered commit from the
// native observations only. It is owner guidance, never an admission gate.
func TestLastAttemptForTargetReadsOnlyExactFailedOrRecoveredAttempts(t *testing.T) {
	root, anchor, uid, gid := observationFixture(t)
	target := strings.Repeat("b", 40)
	last := func() (Attempt, bool) { return lastAttemptAt(root, target, uid, gid, anchor) }
	publish := func(id, commit, phase, reason, proof, at, previous string) {
		t.Helper()
		r := Record{RequestID: id, TargetCommit: commit, Phase: phase, Reason: reason, TerminalProof: proof, ObservedAt: at, PreviousFailure: previous}
		raw, err := r.Encode()
		if err != nil {
			t.Fatal(err)
		}
		// Written directly so a terminal record can be replaced in the fixture.
		if err := os.WriteFile(filepath.Join(root, id+".status"), raw, 0o640); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(filepath.Join(root, id+".status"), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	sidecar := func(id, commit, code string) {
		t.Helper()
		path := filepath.Join(root, id+".failure")
		raw := "schema=" + FailureSchema + "\nrequest_id=" + id + "\ntarget_commit=" + commit + "\nfailure_code=" + code + "\n"
		if err := os.WriteFile(path, []byte(raw), 0o640); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o640); err != nil {
			t.Fatal(err)
		}
	}

	// No observation directory, then an empty one: absent.
	if _, ok := last(); ok {
		t.Fatal("missing directory produced an attempt")
	}
	seed := testRecord()
	seed.RequestID, seed.TargetCommit = strings.Repeat("0", 32), strings.Repeat("9", 40)
	if err := publishAt(root, seed, uid, gid, anchor); err != nil {
		t.Fatal(err)
	}
	if _, ok := last(); ok {
		t.Fatal("a foreign target produced an attempt")
	}

	// Recovered with a typed cause: present with the code and its time.
	first := strings.Repeat("1", 32)
	publish(first, target, "recovered", "rollback_verified", "rollback_verified", "2026-09-30T15:53:24Z", "update_failed")
	if got, ok := last(); !ok || got != (Attempt{RequestID: first, Phase: "recovered", FinishedAt: "2026-09-30T15:53:24Z"}) {
		t.Fatalf("recovered without code: %#v %v", got, ok)
	}
	sidecar(first, target, "candidate_panel_startup_check_failed")
	if got, ok := last(); !ok || got.FailureCode != "candidate_panel_startup_check_failed" || got.Phase != "recovered" {
		t.Fatalf("recovered with code: %#v %v", got, ok)
	}
	// A sidecar bound to another commit or an unknown code is ignored.
	sidecar(first, strings.Repeat("e", 40), "candidate_panel_startup_check_failed")
	if got, ok := last(); !ok || got.FailureCode != "" {
		t.Fatalf("foreign sidecar used: %#v %v", got, ok)
	}
	sidecar(first, target, "private_diagnostic")
	if got, ok := last(); !ok || got.FailureCode != "" {
		t.Fatalf("unknown code used: %#v %v", got, ok)
	}
	sidecar(first, target, "candidate_panel_startup_check_failed")

	// Malformed, unsafe, misnamed and foreign-identity records are ignored.
	bad := strings.Repeat("2", 32)
	if err := os.WriteFile(filepath.Join(root, bad+".status"), []byte("schema=other\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	misnamed := strings.Repeat("3", 32)
	publish(misnamed, target, "failed", "update_failed", "none", "2026-10-01T00:00:00Z", "update_failed")
	if err := os.Rename(filepath.Join(root, misnamed+".status"), filepath.Join(root, strings.Repeat("4", 32)+".status")); err != nil {
		t.Fatal(err)
	}
	unsafe := strings.Repeat("5", 32)
	publish(unsafe, target, "failed", "update_failed", "none", "2026-10-01T00:00:01Z", "update_failed")
	if err := os.Chmod(filepath.Join(root, unsafe+".status"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes.status"), []byte("x"), 0o640); err != nil {
		t.Fatal(err)
	}
	if got, ok := last(); !ok || got.RequestID != first {
		t.Fatalf("an invalid record replaced the verified attempt: %#v %v", got, ok)
	}
	if _, ok := lastAttemptAt(root, "unknown", uid, gid, anchor); ok {
		t.Fatal("a non-commit target was accepted")
	}

	// A later failed attempt to the same commit is the one described.
	second := strings.Repeat("6", 32)
	publish(second, target, "failed", "update_failed", "none", "2026-10-02T08:00:00Z", "update_failed")
	if got, ok := last(); !ok || got != (Attempt{RequestID: second, Phase: "failed", FinishedAt: "2026-10-02T08:00:00Z"}) {
		t.Fatalf("latest failed: %#v %v", got, ok)
	}
	// A newer attempt still in recovery hides older outcomes entirely.
	third := strings.Repeat("7", 32)
	publish(third, target, "recovering", "recovery_running", "none", "2026-10-03T08:00:00Z", "update_failed")
	if got, ok := last(); ok {
		t.Fatalf("an in-progress attempt was replaced by an older outcome: %#v", got)
	}
	publish(third, target, "recovery_required", "recovery_failed", "none", "2026-10-03T08:05:00Z", "recovery_failed")
	if _, ok := last(); ok {
		t.Fatal("recovery_required was described as a finished attempt")
	}
	publish(third, target, "recovered", "rollback_verified", "rollback_verified", "2026-10-03T08:10:00Z", "update_failed")
	if got, ok := last(); !ok || got.RequestID != third || got.FailureCode != "" {
		t.Fatalf("latest recovered: %#v %v", got, ok)
	}
}

func TestLastAttemptForTargetRefusesAnUnboundedDirectory(t *testing.T) {
	root, anchor, uid, gid := observationFixture(t)
	target := strings.Repeat("b", 40)
	r := testRecord()
	r.Phase, r.Reason, r.TerminalProof, r.PreviousFailure = "recovered", "rollback_verified", "rollback_verified", "update_failed"
	if err := publishAt(root, r, uid, gid, anchor); err != nil {
		t.Fatal(err)
	}
	if got, ok := lastAttemptAt(root, target, uid, gid, anchor); !ok || got.RequestID != r.RequestID {
		t.Fatalf("bounded directory: %#v %v", got, ok)
	}
	for i := 0; i < MaxAttemptEntries; i++ {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("filler-%04d", i)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if got, ok := lastAttemptAt(root, target, uid, gid, anchor); ok {
		t.Fatalf("an oversized directory produced a partial answer: %#v", got)
	}
}
