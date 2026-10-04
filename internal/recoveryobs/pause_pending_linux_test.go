//go:build linux

package recoveryobs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Candidate review F1: a historical Agent's worker (v0.1.0-alpha.80) writes no
// record, so the updater writes the initial running record itself. Readers
// then follow the request as with the current Agent: running, the failed
// transition, the typed cause, the pending pause and the pause, and the Agent's
// own publisher continues the same record. An existing record is never touched.
func TestUpdaterInitialRecordForAHistoricalWorker(t *testing.T) {
	shell, read, root, anchor, r := shellObservationFixture(t, "initial-record")
	shell(`release_observation_publish_initial "$3" "$4"`)
	if got := read(); got.Observation != "known" || got.Phase != "running" || got.Reason != "update_running" ||
		got.TerminalProof != "none" || got.PreviousFailure != "" {
		t.Fatalf("initial record: %#v", got)
	}
	path := filepath.Join(root, r.RequestID+".status")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(raw, r.RequestID)
	if err != nil || decoded.TargetCommit != r.TargetCommit || decoded.PreviousFailure != "none" {
		t.Fatalf("initial record is not a v1 record of this request: %#v %v", decoded, err)
	}
	if encoded, err := decoded.Encode(); err != nil || string(encoded) != string(raw) {
		t.Fatalf("initial record differs from the Agent's encoding: %q", raw)
	}
	shell(`status=0; release_observation_publish_initial "$3" "$4" || status=$?; [[ $status == 3 ]]`)
	if after, _ := os.ReadFile(path); string(after) != string(raw) {
		t.Fatal("the initial producer rewrote an existing record")
	}
	shell(`release_observation_publish_failure "$3" "$4" panel_start_unverified`)
	shell(`release_observation_publish "$3" "$4" failed none update_failed`)
	if got := read(); got.Phase != "failed" || got.FailureCode != "panel_start_unverified" {
		t.Fatalf("failed transition: %#v", got)
	}
	shell(`release_observation_publish "$3" "$4" recovering none recovery_running`)
	shell(`release_observation_publish "$3" "$4" recovery_required none recovery_failed "" pause_pending`)
	if got := read(); got.AutomaticRecovery != "pause_pending" || got.FirstFailureCode != "panel_start_unverified" {
		t.Fatalf("pending pause: %#v", got)
	}
	shell(`release_observation_publish "$3" "$4" recovery_required none recovery_incomplete "" paused_retry_limit`)
	if got := read(); got.AutomaticRecovery != "paused_retry_limit" || got.FirstFailureCode != "panel_start_unverified" {
		t.Fatalf("pause: %#v", got)
	}
	r.Phase, r.TerminalProof, r.Reason = "succeeded", "update_verified", "update_verified"
	r.ObservedAt = time.Now().UTC().Format("2006-01-02T15:04:05Z")
	if err := publishAt(root, r, 0, 0, anchor); err != nil {
		t.Fatal(err)
	}
	if got := read(); got.Phase != "succeeded" || got.PreviousFailure != "recovery_failed" {
		t.Fatalf("Agent continuation: %#v", got)
	}
}

// F6 (upd4): after the last admitted attempt fails and before the next timer
// run records the pause, the failure carries pause_pending and the update's
// first typed cause stays visible; the pause then replaces the hint.
func TestPausePendingKeepsTheFirstCauseUntilThePause(t *testing.T) {
	shell, read, root, _, r := shellObservationFixture(t, "pause-pending")
	shell(`release_observation_publish "$3" "$4" running none update_running`)
	shell(`release_observation_publish "$3" "$4" failed none update_failed`)
	shell(`release_observation_publish_failure "$3" "$4" panel_start_unverified`)
	shell(`release_observation_publish "$3" "$4" recovering none recovery_running`)
	shell(`release_observation_publish "$3" "$4" recovery_required none recovery_failed "" pause_pending`)
	got := read()
	if got.Phase != "recovery_required" || got.Reason != "recovery_failed" || got.AutomaticRecovery != "pause_pending" ||
		got.FirstFailureCode != "panel_start_unverified" || got.FailureCode != "" || got.RenewalBeforeUpdate != "" {
		t.Fatalf("pause pending: %#v", got)
	}
	encoded, _ := json.Marshal(got)
	if !strings.HasPrefix(string(encoded), `{"automatic_recovery":"pause_pending","schema":`) ||
		!strings.HasSuffix(string(encoded), `,"first_failure_code":"panel_start_unverified"}`) {
		t.Fatalf("JSON: %s", encoded)
	}
	raw, _ := os.ReadFile(filepath.Join(root, r.RequestID+".status"))
	if len(strings.Split(string(raw), "\n")) != 9 {
		t.Fatalf("v1 record changed: %q", raw)
	}
	// The producer refuses the hint on any record other than a recovery failure.
	before := string(raw)
	for _, bad := range []string{
		`release_observation_publish "$3" "$4" recovery_required none recovery_incomplete "" pause_pending`,
		`release_observation_publish "$3" "$4" recovering none recovery_running "" pause_pending`,
	} {
		shell(`if ` + bad + `; then exit 9; fi`)
	}
	if after, _ := os.ReadFile(filepath.Join(root, r.RequestID+".status")); string(after) != before {
		t.Fatal("refused hint changed the record")
	}
	shell(`release_observation_publish "$3" "$4" recovery_required none recovery_incomplete "" paused_retry_limit`)
	if got := read(); got.AutomaticRecovery != "paused_retry_limit" || got.FirstFailureCode != "panel_start_unverified" {
		t.Fatalf("pause: %#v", got)
	}
	if !ValidAutomatic("pause_pending") || ValidAutomatic("pausing") {
		t.Fatal("closed automatic values changed")
	}
}

// O8 (upd4): the pause names whether the update paused renewal, from the
// scheduler state the updater recorded; absent means not recorded.
func TestRenewalBeforeUpdateIsReadOnlyAtThePause(t *testing.T) {
	shell, read, root, _, r := shellObservationFixture(t, "renewal-state")
	shell(`release_observation_publish "$3" "$4" running none update_running`)
	shell(`release_observation_publish_renewal "$3" "$4" off`)
	// The first recorded value wins; unknown values are refused.
	shell(`release_observation_publish_renewal "$3" "$4" on`)
	shell(`if release_observation_publish_renewal "$3" "$4" maybe; then exit 9; fi`)
	raw, err := os.ReadFile(filepath.Join(root, r.RequestID+".renewal"))
	if err != nil || string(raw) != "schema=celikpanel-recovery-renewal/v1\nrequest_id="+r.RequestID+"\ntarget_commit="+r.TargetCommit+"\nrenewal_before_update=off\n" {
		t.Fatalf("renewal sidecar: %q %v", raw, err)
	}
	shell(`release_observation_publish "$3" "$4" failed none update_failed`)
	if got := read(); got.RenewalBeforeUpdate != "" {
		t.Fatalf("renewal shown outside the pause: %#v", got)
	}
	shell(`release_observation_publish "$3" "$4" recovery_required none recovery_failed "" pause_pending`)
	if got := read(); got.RenewalBeforeUpdate != "" {
		t.Fatalf("renewal shown before the pause: %#v", got)
	}
	shell(`release_observation_publish "$3" "$4" recovery_required none recovery_incomplete "" paused_retry_limit`)
	got := read()
	if got.AutomaticRecovery != "paused_retry_limit" || got.RenewalBeforeUpdate != "off" {
		t.Fatalf("pause: %#v", got)
	}
	encoded, _ := json.Marshal(got)
	if !strings.HasSuffix(string(encoded), `,"renewal_before_update":"off"}`) {
		t.Fatalf("JSON: %s", encoded)
	}
	for _, bad := range []string{
		"schema=celikpanel-recovery-renewal/v1\nrequest_id=" + r.RequestID + "\ntarget_commit=" + r.TargetCommit + "\nrenewal_before_update=unknown\n",
		"schema=celikpanel-recovery-renewal/v2\nrequest_id=" + r.RequestID + "\ntarget_commit=" + r.TargetCommit + "\nrenewal_before_update=off\n",
		"schema=celikpanel-recovery-renewal/v1\nrequest_id=" + r.RequestID + "\ntarget_commit=" + strings.Repeat("d", 40) + "\nrenewal_before_update=off\n",
	} {
		if DecodeRenewal([]byte(bad), r.RequestID, r.TargetCommit) != "" {
			t.Fatalf("accepted %q", bad)
		}
	}
}

// F4 (upd4): a read-only check that refused before the freeze is a typed,
// terminal failure like the recovery runtime preflight stop.
func TestRefusedUpdatePreflightIsATypedTerminalFailure(t *testing.T) {
	shell, read, root, anchor, r := shellObservationFixture(t, "preflight-refused")
	shell(`release_observation_publish "$3" "$4" running none update_running`)
	shell(`release_observation_publish_failure "$3" "$4" update_preflight_refused`)
	shell(`release_observation_publish "$3" "$4" failed none update_failed`)
	got := read()
	if got.Phase != "failed" || got.PreviousFailure != "update_failed" || got.FailureCode != "update_preflight_refused" {
		t.Fatalf("refused preflight: %#v", got)
	}
	attempt, ok := lastAttemptAt(root, r.TargetCommit, 0, 0, anchor)
	if !ok || attempt.FailureCode != "update_preflight_refused" {
		t.Fatalf("previous attempt: %#v %v", attempt, ok)
	}
	if !ValidFailureCode("update_preflight_refused") || ValidFailureCode("update_preflight") {
		t.Fatal("closed failure codes changed")
	}
}
