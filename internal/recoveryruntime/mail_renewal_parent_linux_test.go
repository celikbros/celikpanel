//go:build linux

package recoveryruntime

import (
	"golang.org/x/sys/unix"
	"os"
	"strings"
	"syscall"
	"testing"
)

func TestMailParentPublicationAndInterruption(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root fixture")
	}
	cases := []string{"parent-created", "parent-owner-before-publish", "parent-owner-after-ready", "parent-corrupt-ready", "parent-existing-owner-content", "parent-stage-edit", "parent-unsafe-symlink"}
	for _, point := range []string{"staged", "plan_durable", "plan_published", "plan_parent_durable", "moved", "directory_durable", "ready_durable", "ready_published", "ready_parent_durable"} {
		cases = append(cases, "cut-parent_"+point)
	}
	for _, scenario := range cases {
		t.Run(scenario, func(t *testing.T) {
			root, lock := mailRenewalTestRoot(t)
			_, target, _ := mailCaptureFixture(t, root, "absent")
			if e := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != nil {
				t.Fatal(e)
			}
			child := mailEnableChild(root, target, scenario, lock)
			out, e := child.CombinedOutput()
			if !strings.HasPrefix(scenario, "cut-") {
				if e != nil {
					t.Fatalf("%v %s", e, out)
				}
				return
			}
			status, ok := child.ProcessState.Sys().(syscall.WaitStatus)
			if e == nil || !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
				t.Fatalf("wrong cut %v %s", e, out)
			}
			if out, e = mailEnableChild(root, target, "parent-resume", lock).CombinedOutput(); e != nil {
				t.Fatalf("resume %v %s", e, out)
			}
		})
	}
}
