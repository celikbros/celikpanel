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
)

// A paused automatic recovery names the update's first typed cause even after a
// later recovery failure hid failure_code. The sidecar is only read; the phase,
// the pause hint and the v1 record stay exactly as the producers wrote them.
func TestPausedRecoveryExposesTheFirstTypedCauseReadOnly(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("real shell metadata fixture requires root")
	}
	for _, withSidecar := range []bool{true, false} {
		anchor, err := os.MkdirTemp("/var/lib", "celikpanel-paused-cause-")
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
			if out, err := exec.Command("bash", "-c", script, "paused-cause", repo, root, r.RequestID, r.TargetCommit).CombinedOutput(); err != nil {
				t.Fatalf("shell failed: %v: %s", err, out)
			}
		}
		read := func() Status { return readAt(root, r.RequestID, 0, 0, anchor) }
		// A forward completion that never came up: failed, the typed cause, then
		// recovery attempts that fail and finally pause on the retry limit.
		shell(`release_observation_publish "$3" "$4" running none update_running`)
		shell(`release_observation_publish "$3" "$4" failed none update_failed`)
		if withSidecar {
			shell(`release_observation_publish_failure "$3" "$4" panel_start_unverified`)
		}
		shell(`release_observation_publish "$3" "$4" recovering none recovery_running`)
		shell(`release_observation_publish "$3" "$4" recovery_required none recovery_failed`)
		if got := read(); got.FailureCode != "" || got.FirstFailureCode != "" || got.PreviousFailure != "recovery_failed" {
			t.Fatalf("an unpaused recovery failure exposed the update cause: %#v", got)
		}
		status, _ := os.ReadFile(filepath.Join(root, r.RequestID+".status"))
		shell(`release_observation_publish "$3" "$4" recovery_required none recovery_incomplete "" paused_retry_limit`)
		got := read()
		if got.AutomaticRecovery != "paused_retry_limit" || got.Phase != "recovery_required" || got.PreviousFailure != "recovery_failed" || got.FailureCode != "" {
			t.Fatalf("paused base changed: %#v", got)
		}
		want := ""
		if withSidecar {
			want = "panel_start_unverified"
		}
		if got.FirstFailureCode != want {
			t.Fatalf("paused first cause = %q, want %q", got.FirstFailureCode, want)
		}
		encoded, _ := json.Marshal(got)
		if strings.Contains(string(encoded), "first_failure_code") != withSidecar ||
			(withSidecar && !strings.HasSuffix(string(encoded), `,"first_failure_code":"panel_start_unverified"}`)) {
			t.Fatalf("JSON: %s", encoded)
		}
		after, _ := os.ReadFile(filepath.Join(root, r.RequestID+".status"))
		if len(strings.Split(string(after), "\n")) != 9 || strings.Contains(string(after), "failure_code") || len(status) == 0 {
			t.Fatalf("v1 record changed: %q", after)
		}
		if withSidecar {
			path := filepath.Join(root, r.RequestID+".failure")
			valid, _ := os.ReadFile(path)
			for _, bad := range []string{
				strings.Replace(string(valid), "panel_start_unverified", "private_detail", 1),
				strings.Replace(string(valid), "target_commit="+r.TargetCommit, "target_commit="+strings.Repeat("e", 40), 1),
			} {
				if err := os.WriteFile(path, []byte(bad), 0o640); err != nil {
					t.Fatal(err)
				}
				if got := read(); got.FirstFailureCode != "" || got.AutomaticRecovery != "paused_retry_limit" {
					t.Fatalf("bad sidecar used: %#v", got)
				}
			}
		}
	}
}
