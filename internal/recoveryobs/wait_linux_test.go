//go:build linux

package recoveryobs

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestNativeWaitingKeepsLegacyRecordAndInvalidatesOnRepublication(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("real shell metadata fixture requires root")
	}
	anchor, err := os.MkdirTemp("/var/lib", "celikpanel-wait-interop-")
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
		out, err := exec.Command("bash", "-c", script, "wait-test", repo, root, r.RequestID, r.TargetCommit).CombinedOutput()
		if err != nil {
			t.Fatalf("shell failed: %v: %s", err, out)
		}
	}
	shell(`release_observation_publish "$3" "$4" failed none update_failed`)
	path := filepath.Join(root, r.RequestID+".wait")
	for _, reason := range []string{"initializing", "starting", "stopping"} {
		shell(`release_observation_publish "$3" "$4" recovering none recovery_running ` + reason)
		got := readAt(root, r.RequestID, 0, 0, anchor)
		if got.WaitingFor != reason || got.Phase != "recovering" || got.TerminalProof != "none" || got.PreviousFailure != "update_failed" {
			t.Fatalf("wait: %#v", got)
		}
		raw, _ := os.ReadFile(filepath.Join(root, r.RequestID+".status"))
		legacy, err := Decode(raw, r.RequestID)
		if err != nil || len(strings.Split(string(raw), "\n")) != 9 || legacy.Status().WaitingFor != "" {
			t.Fatal("legacy v1 changed", err)
		}
	}
	valid, _ := os.ReadFile(path)
	assertNoHint := func() {
		t.Helper()
		got := readAt(root, r.RequestID, 0, 0, anchor)
		if got.Observation != "known" || got.WaitingFor != "" || got.PreviousFailure != "update_failed" {
			t.Fatalf("hint changed base: %#v", got)
		}
	}
	for _, bad := range []string{
		strings.Replace(string(valid), "waiting_for=stopping", "waiting_for=private-diagnostic", 1),
		strings.Replace(string(valid), "request_id="+r.RequestID, "request_id="+strings.Repeat("c", 32), 1),
		strings.Replace(string(valid), "observation_sha256=", "observation_sha256=0", 1),
		strings.Replace(string(valid), "observation_identity=", "observation_identity=0", 1),
		string(valid) + "extra=private\n", strings.Repeat("x", MaxRecordSize+1),
	} {
		if err := os.WriteFile(path, []byte(bad), 0640); err != nil {
			t.Fatal(err)
		}
		assertNoHint()
	}
	if err := os.WriteFile(path, valid, 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0660); err != nil {
		t.Fatal(err)
	}
	assertNoHint()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, r.RequestID+".status"), path); err != nil {
		t.Fatal(err)
	}
	assertNoHint()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(path, 0640); err != nil {
		t.Fatal(err)
	}
	assertNoHint()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, valid, 0640); err != nil {
		t.Fatal(err)
	}
	// A v1-only producer may publish identical bytes in the same second. The
	// file identity, not just its hash or second-resolution timestamp, must bind.
	base, _ := os.ReadFile(filepath.Join(root, r.RequestID+".status"))
	record, err := Decode(base, r.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	if err := publishAt(root, record, 0, 0, anchor); err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(filepath.Join(root, r.RequestID+".status"))
	if string(base) != string(again) {
		t.Fatal("test did not republish identical v1 bytes")
	}
	assertNoHint()
	shell(`release_observation_publish "$3" "$4" recovering none recovery_running starting`)
	record.Phase, record.Reason, record.TerminalProof = "recovered", "rollback_verified", "rollback_verified"
	record.ObservedAt = time.Now().UTC().Format("2006-01-02T15:04:05Z")
	if err := publishAt(root, record, 0, 0, anchor); err != nil {
		t.Fatal(err)
	}
	got := readAt(root, r.RequestID, 0, 0, anchor)
	if got.WaitingFor != "" || got.TerminalProof != "rollback_verified" {
		t.Fatalf("terminal wait: %#v", got)
	}
	shell(`release_observation_publish "$3" "$4" recovering none recovery_running starting`)
	if after := readAt(root, r.RequestID, 0, 0, anchor); after != got {
		t.Fatalf("late wait erased terminal: %#v", after)
	}
}
