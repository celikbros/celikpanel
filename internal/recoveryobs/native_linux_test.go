//go:build linux

package recoveryobs

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// Exercise the real shell writer and Go reader together. The fixture changes
// only the private destination and group lookup, never production state.
func TestNativeShellProducerAndGoReaderContract(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("native metadata proof requires root fixture")
	}
	anchor, err := os.MkdirTemp("/var/lib", "celikpanel-observation-interop-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(anchor) })
	root := filepath.Join(anchor, "public")
	r := testRecord()
	r.ObservedAt = "2000-01-01T00:00:00Z"
	if err := publishAt(root, r, 0, 0, anchor); err != nil {
		t.Fatal(err)
	}
	_, here, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	script := `set -euo pipefail
TRUSTED_RELEASE_ROOT=$1
source "$1/deploy/release-transaction-guard.sh"
source "$1/deploy/release-recovery-foundation.sh"
source "$1/deploy/release-recovery-observation.sh"
RELEASE_OBSERVATION_ROOT=$2
_release_observation_gid() { printf '0\n'; }
release_observation_publish "$3" "$4" failed none update_failed
release_observation_publish "$3" "$4" recovering none recovery_running
release_observation_publish "$3" "$4" recovered rollback_verified rollback_verified
`
	cmd := exec.Command("bash", "-c", script, "observation-contract-test", repo, root, r.RequestID, r.TargetCommit)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("native publisher: %v\n%s", err, out)
	}
	status := readAt(root, r.RequestID, 0, 0, anchor)
	if status.Observation != "known" || status.Phase != "recovered" || status.TerminalProof != "rollback_verified" || status.PreviousFailure != "update_failed" {
		t.Fatalf("native/Go contract mismatch: %#v", status)
	}
	late := testRecord()
	late.Phase, late.Reason = "failed", "update_failed"
	if err := publishAt(root, late, 0, 0, anchor); err != nil {
		t.Fatal(err)
	}
	if got := readAt(root, r.RequestID, 0, 0, anchor); got != status {
		t.Fatalf("late Go producer erased native proof: %#v", got)
	}
}
