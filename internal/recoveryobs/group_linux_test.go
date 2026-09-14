//go:build linux

package recoveryobs

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
)

func TestAgentGroupPublisherChild(t *testing.T) {
	anchor := os.Getenv("CELIKPANEL_OBSERVATION_GROUP_TEST")
	if anchor == "" {
		t.Skip("executed only by the guarded metadata fixture")
	}
	if os.Geteuid() != 0 || os.Getegid() != 12345 {
		t.Fatal("fixture has wrong process credentials")
	}
	r := testRecord()
	r.ObservedAt = "2000-01-01T00:00:00Z"
	if err := publishAt(filepath.Join(anchor, "public"), r, 0, 12345, anchor); err != nil {
		t.Fatal(err)
	}
}

func TestAgentNonzeroGroupAndNativeRootPublisherShareLock(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("native UID/GID fixture requires root")
	}
	anchor, err := os.MkdirTemp("/var/lib", "celikpanel-observation-group-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(anchor) })
	child := exec.Command(os.Args[0], "-test.run=^TestAgentGroupPublisherChild$")
	child.Env = append(os.Environ(), "CELIKPANEL_OBSERVATION_GROUP_TEST="+anchor)
	child.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 0, Gid: 12345}}
	if out, err := child.CombinedOutput(); err != nil {
		t.Fatalf("Agent-group publication: %v\n%s", err, out)
	}
	root := filepath.Join(anchor, "public")
	lock, err := os.Stat(filepath.Join(root, ".publish.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if st := lock.Sys().(*syscall.Stat_t); st.Uid != 0 || st.Gid != 0 {
		t.Fatalf("lock inherited Agent group: uid=%d gid=%d", st.Uid, st.Gid)
	}
	_, here, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	r := testRecord()
	script := `set -euo pipefail
TRUSTED_RELEASE_ROOT=$1
source "$1/deploy/release-transaction-guard.sh"
source "$1/deploy/release-recovery-foundation.sh"
source "$1/deploy/release-recovery-observation.sh"
RELEASE_OBSERVATION_ROOT=$2
_release_observation_gid() { printf '12345\n'; }
release_observation_publish "$3" "$4" recovering none recovery_running
release_observation_publish "$3" "$4" recovered rollback_verified rollback_verified
`
	cmd := exec.Command("bash", "-c", script, "group-contract-test", repo, root, r.RequestID, r.TargetCommit)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("native root publication: %v\n%s", err, out)
	}
	if got := readAt(root, r.RequestID, 0, 12345, anchor); got.Observation != "known" || got.TerminalProof != "rollback_verified" {
		t.Fatalf("mixed-group observation: %#v", got)
	}
	// An existing lock owned by the panel group is evidence to reject, not fix.
	if err := os.Chown(filepath.Join(root, ".publish.lock"), 0, 12345); err != nil {
		t.Fatal(err)
	}
	if err := publishAt(root, r, 0, 12345, anchor); err == nil {
		t.Fatal("wrong existing lock ownership was normalized")
	}
	lock, err = os.Stat(filepath.Join(root, ".publish.lock"))
	if err != nil || lock.Sys().(*syscall.Stat_t).Gid != 12345 {
		t.Fatal("existing lock metadata changed")
	}
}
