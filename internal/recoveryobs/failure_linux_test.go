//go:build linux

package recoveryobs

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The typed update cause is an additive sidecar. The shell producer writes it
// once; the Go reader exposes it only while the update's own failure is the
// latest recorded failure, and never lets it replace phase or terminal proof.
func TestNativeFailureCodeSidecarIsAdditiveAndBoundToTheUpdateFailure(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("real shell metadata fixture requires root")
	}
	anchor, err := os.MkdirTemp("/var/lib", "celikpanel-failure-interop-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(anchor) })
	root := filepath.Join(anchor, "public")
	r := testRecord()
	_, here, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	shell := func(wantOK bool, extra string) {
		t.Helper()
		script := `set -euo pipefail
TRUSTED_RELEASE_ROOT=$1
source "$1/deploy/release-transaction-guard.sh"
source "$1/deploy/release-recovery-foundation.sh"
source "$1/deploy/release-recovery-observation.sh"
RELEASE_OBSERVATION_ROOT=$2
_release_observation_gid() { printf '0\n'; }
` + extra
		out, err := exec.Command("bash", "-c", script, "failure-test", repo, root, r.RequestID, r.TargetCommit).CombinedOutput()
		if (err == nil) != wantOK {
			t.Fatalf("shell result %v, want ok=%v: %s", err, wantOK, out)
		}
	}
	read := func() Status { return readAt(root, r.RequestID, 0, 0, anchor) }

	// Without a status record for this exact request, nothing is written.
	shell(false, `release_observation_publish_failure "$3" "$4" candidate_panel_startup_check_failed`)
	shell(true, `release_observation_publish "$3" "$4" running none update_running`)
	shell(false, `release_observation_publish_failure "$3" "$4" private_diagnostic`)
	shell(false, `release_observation_publish_failure "$3" "`+strings.Repeat("c", 40)+`" candidate_panel_startup_check_failed`)
	path := filepath.Join(root, r.RequestID+".failure")
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("rejected producer wrote a sidecar: %v", err)
	}
	shell(true, `release_observation_publish_failure "$3" "$4" candidate_panel_startup_check_failed`)
	// The first cause wins; a later code does not rewrite it.
	shell(true, `release_observation_publish_failure "$3" "$4" panel_start_unverified`)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "schema=celikpanel-recovery-failure/v1\nrequest_id=" + r.RequestID + "\ntarget_commit=" + r.TargetCommit + "\nfailure_code=candidate_panel_startup_check_failed\n"
	if string(raw) != want {
		t.Fatalf("sidecar = %q", raw)
	}
	// Running is not a failure yet: no code is exposed.
	if got := read(); got.FailureCode != "" || got.Phase != "running" {
		t.Fatalf("running exposed a failure code: %#v", got)
	}
	shell(true, `release_observation_publish "$3" "$4" failed none update_failed`)
	if got := read(); got.FailureCode != "candidate_panel_startup_check_failed" || got.Reason != "update_failed" {
		t.Fatalf("failed: %#v", got)
	}
	// The v1 record bytes and the legacy decoder are unchanged.
	status, _ := os.ReadFile(filepath.Join(root, r.RequestID+".status"))
	legacy, err := Decode(status, r.RequestID)
	if err != nil || len(strings.Split(string(status), "\n")) != 9 || legacy.Status().FailureCode != "" {
		t.Fatal("legacy v1 changed", err)
	}
	shell(true, `release_observation_publish "$3" "$4" recovering none recovery_running`)
	if got := read(); got.FailureCode != "candidate_panel_startup_check_failed" || got.PreviousFailure != "update_failed" {
		t.Fatalf("recovering: %#v", got)
	}
	encoded, _ := json.Marshal(read())
	if !strings.Contains(string(encoded), `"failure_code":"candidate_panel_startup_check_failed"`) {
		t.Fatalf("status JSON lacks the additive field: %s", encoded)
	}
	// Malformed, foreign or unsafe sidecars are ignored; the base status stays.
	for _, bad := range []string{
		strings.Replace(want, "failure_code=candidate_panel_startup_check_failed", "failure_code=private-diagnostic", 1),
		strings.Replace(want, "request_id="+r.RequestID, "request_id="+strings.Repeat("d", 32), 1),
		strings.Replace(want, "target_commit="+r.TargetCommit, "target_commit="+strings.Repeat("e", 40), 1),
		strings.Replace(want, "schema=celikpanel-recovery-failure/v1", "schema=celikpanel-recovery-failure/v2", 1),
		want + "extra=private\n", strings.Repeat("x", MaxRecordSize+1),
	} {
		if err := os.WriteFile(path, []byte(bad), 0640); err != nil {
			t.Fatal(err)
		}
		if got := read(); got.FailureCode != "" || got.Observation != "known" || got.PreviousFailure != "update_failed" {
			t.Fatalf("bad sidecar changed status: %#v", got)
		}
	}
	if err := os.WriteFile(path, []byte(want), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0660); err != nil {
		t.Fatal(err)
	}
	if got := read(); got.FailureCode != "" {
		t.Fatalf("unsafe mode accepted: %#v", got)
	}
	if err := os.Chmod(path, 0640); err != nil {
		t.Fatal(err)
	}
	// A later recovery failure is the newer recorded failure; the update cause hides.
	shell(true, `release_observation_publish "$3" "$4" recovery_required none recovery_failed`)
	if got := read(); got.FailureCode != "" || got.PreviousFailure != "recovery_failed" {
		t.Fatalf("recovery failure kept the update cause: %#v", got)
	}
	// A terminal rollback that follows the update failure keeps the cause.
	base, _ := os.ReadFile(filepath.Join(root, r.RequestID+".status"))
	record, err := Decode(base, r.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	record.Phase, record.Reason, record.TerminalProof, record.PreviousFailure = "recovered", "rollback_verified", "rollback_verified", "update_failed"
	record.ObservedAt = time.Now().UTC().Format("2006-01-02T15:04:05Z")
	raw, err = record.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, r.RequestID+".status"), raw, 0640); err != nil {
		t.Fatal(err)
	}
	if got := read(); got.FailureCode != "candidate_panel_startup_check_failed" || got.TerminalProof != "rollback_verified" || got.Phase != "recovered" {
		t.Fatalf("recovered: %#v", got)
	}
}

func TestDecodeFailureIsClosed(t *testing.T) {
	r := testRecord()
	valid := "schema=" + FailureSchema + "\nrequest_id=" + r.RequestID + "\ntarget_commit=" + r.TargetCommit + "\nfailure_code=panel_start_unverified\n"
	if got := DecodeFailure([]byte(valid), r.RequestID, r.TargetCommit); got != "panel_start_unverified" {
		t.Fatalf("valid sidecar = %q", got)
	}
	for _, bad := range []string{"", strings.TrimSuffix(valid, "\n"), valid + "\n", strings.Replace(valid, "panel_start_unverified", "update_failed", 1)} {
		if got := DecodeFailure([]byte(bad), r.RequestID, r.TargetCommit); got != "" {
			t.Fatalf("%q decoded as %q", bad, got)
		}
	}
	if DecodeFailure([]byte(valid), r.RequestID, strings.Repeat("f", 40)) != "" || DecodeFailure([]byte(valid), "bad", r.TargetCommit) != "" {
		t.Fatal("foreign identity accepted")
	}
}
