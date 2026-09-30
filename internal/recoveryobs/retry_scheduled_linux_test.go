//go:build linux

package recoveryobs

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func shellObservationFixture(t *testing.T, name string) (func(string), func() Status, string, string, Record) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("real shell metadata fixture requires root")
	}
	anchor, err := os.MkdirTemp("/var/lib", "celikpanel-"+name+"-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(anchor) })
	root := filepath.Join(anchor, "public")
	r := testRecord()
	_, here, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	shell := func(extra string) {
		t.Helper()
		script := `set -euo pipefail
TRUSTED_RELEASE_ROOT=$1
source "$1/deploy/release-transaction-guard.sh"
source "$1/deploy/release-recovery-foundation.sh"
source "$1/deploy/release-recovery-observation.sh"
RELEASE_OBSERVATION_ROOT=$2
_release_observation_gid() { printf '0\n'; }
` + extra
		if out, err := exec.Command("bash", "-c", script, name, repo, root, r.RequestID, r.TargetCommit).CombinedOutput(); err != nil {
			t.Fatalf("shell failed: %v: %s", err, out)
		}
	}
	read := func() Status { return readAt(root, r.RequestID, 0, 0, anchor) }
	return shell, read, root, anchor, r
}

// F3 (upd3): between automatic attempts the failure carries a scheduled-retry
// hint and the update's first typed cause stays visible through the next
// attempt until the pause. Every hint is read only and bound to its record.
func TestScheduledRetryKeepsTheFirstCauseBetweenAttempts(t *testing.T) {
	shell, read, root, anchor, r := shellObservationFixture(t, "retry-scheduled")
	shell(`release_observation_publish "$3" "$4" running none update_running`)
	shell(`release_observation_publish "$3" "$4" failed none update_failed`)
	shell(`release_observation_publish_failure "$3" "$4" panel_start_unverified`)
	shell(`release_observation_publish "$3" "$4" recovering none recovery_running`)
	if got := read(); got.FailureCode != "panel_start_unverified" || got.FirstFailureCode != "" {
		t.Fatalf("first attempt lost the update cause: %#v", got)
	}
	shell(`release_observation_publish "$3" "$4" recovery_required none recovery_failed "" retry_scheduled`)
	got := read()
	if got.Phase != "recovery_required" || got.Reason != "recovery_failed" || got.AutomaticRecovery != "retry_scheduled" ||
		got.FailureCode != "" || got.FirstFailureCode != "panel_start_unverified" || got.PreviousFailure != "recovery_failed" {
		t.Fatalf("scheduled retry: %#v", got)
	}
	encoded, _ := json.Marshal(got)
	if !strings.HasPrefix(string(encoded), `{"automatic_recovery":"retry_scheduled","schema":`) ||
		!strings.HasSuffix(string(encoded), `,"first_failure_code":"panel_start_unverified"}`) {
		t.Fatalf("JSON: %s", encoded)
	}
	raw, _ := os.ReadFile(filepath.Join(root, r.RequestID+".status"))
	if legacy, err := Decode(raw, r.RequestID); err != nil || legacy.Status().AutomaticRecovery != "" || len(strings.Split(string(raw), "\n")) != 9 {
		t.Fatalf("v1 record changed: %q %v", raw, err)
	}
	// The next attempt starts: the hint no longer binds, the first cause stays.
	shell(`release_observation_publish "$3" "$4" recovering none recovery_running`)
	if got := read(); got.AutomaticRecovery != "" || got.Phase != "recovering" || got.PreviousFailure != "recovery_failed" ||
		got.FirstFailureCode != "panel_start_unverified" || got.FailureCode != "" {
		t.Fatalf("next attempt: %#v", got)
	}
	shell(`release_observation_publish "$3" "$4" recovery_required none recovery_incomplete "" paused_retry_limit`)
	if got := read(); got.AutomaticRecovery != "paused_retry_limit" || got.FirstFailureCode != "panel_start_unverified" {
		t.Fatalf("pause: %#v", got)
	}

	// The producer refuses the hint on any other record; nothing is written.
	before, _ := os.ReadFile(filepath.Join(root, r.RequestID+".status"))
	for _, bad := range []string{
		`release_observation_publish "$3" "$4" recovery_required none recovery_incomplete "" retry_scheduled`,
		`release_observation_publish "$3" "$4" recovering none recovery_running "" retry_scheduled`,
	} {
		shell(`if ` + bad + `; then exit 9; fi`)
	}
	if after, _ := os.ReadFile(filepath.Join(root, r.RequestID+".status")); string(after) != string(before) {
		t.Fatal("refused hint changed the record")
	}
	// The reader refuses a scheduled-retry hint bound to an incomplete record.
	identity := ""
	if raw, id, err := readObservationFileForTest(root, anchor, r.RequestID); err == nil {
		identity = id
		hint := fmt.Sprintf("schema=celikpanel-recovery-automatic/v1\nrequest_id=%s\nobservation_identity=%s\nobservation_sha256=%x\nautomatic_recovery=retry_scheduled\n", r.RequestID, identity, sha256.Sum256(raw))
		if err := os.WriteFile(filepath.Join(root, r.RequestID+".automatic"), []byte(hint), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	if identity == "" {
		t.Fatal("cannot identify the status record")
	}
	if got := read(); got.AutomaticRecovery != "" || got.FirstFailureCode != "" || got.Reason != "recovery_incomplete" {
		t.Fatalf("retry hint on an incomplete record was used: %#v", got)
	}
}

func readObservationFileForTest(root, anchor, id string) ([]byte, string, error) {
	fd, err := openRoot(root, 0, 0, anchor, false)
	if err != nil {
		return nil, "", err
	}
	defer unix.Close(fd)
	return readObservationFile(fd, id+".status", 0, 0)
}

// F1 (upd3): a preflight stop is a failed record whose typed cause says that
// nothing was changed; the update check's previous attempt carries the same
// code. The v1 record is unchanged.
func TestPreflightStopIsATypedTerminalFailure(t *testing.T) {
	shell, read, root, anchor, r := shellObservationFixture(t, "preflight-stop")
	shell(`release_observation_publish "$3" "$4" running none update_running`)
	shell(`release_observation_publish_failure "$3" "$4" recovery_runtime_preflight_failed`)
	shell(`release_observation_publish "$3" "$4" failed none update_failed`)
	got := read()
	if got.Phase != "failed" || got.TerminalProof != "none" || got.PreviousFailure != "update_failed" ||
		got.FailureCode != "recovery_runtime_preflight_failed" || got.FirstFailureCode != "" || got.AutomaticRecovery != "" {
		t.Fatalf("preflight stop: %#v", got)
	}
	attempt, ok := lastAttemptAt(root, r.TargetCommit, 0, 0, anchor)
	if !ok || attempt.Phase != "failed" || attempt.FailureCode != "recovery_runtime_preflight_failed" || attempt.RequestID != r.RequestID {
		t.Fatalf("previous attempt: %#v %v", attempt, ok)
	}
	// Another code is refused by the producer; the first recorded cause wins.
	shell(`if release_observation_publish_failure "$3" "$4" package_manager_busy; then exit 9; fi`)
	shell(`release_observation_publish_failure "$3" "$4" panel_start_unverified`)
	if got := read(); got.FailureCode != "recovery_runtime_preflight_failed" {
		t.Fatalf("first cause rewritten: %#v", got)
	}
}
