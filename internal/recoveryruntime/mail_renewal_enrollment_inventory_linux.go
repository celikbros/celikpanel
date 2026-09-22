//go:build linux

package recoveryruntime

import (
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

// Bound the journal read and refuse unknown records for this operation. Another
// operation's retained history and uncommitted random writer stages are not
// adopted or deleted. A future attempt number/schema cannot evade the budget.
func verifyMailEnrollmentInventory(c *mailFilesContext, operation string) error {
	allowed := map[string]bool{}
	for _, suffix := range []string{".json", ".files.json", ".files-rollback-intent.json", ".timer-enable.json", ".timer-enable-rollback-intent.json", ".timer-parent.json", ".timer-parent-ready.json", ".enrollment.json", ".enrollment-rollback-intent.json"} {
		allowed[operation+suffix] = true
	}
	for _, side := range []string{"forward", "rollback"} {
		for _, suffix := range []string{".files-" + side + ".json", ".loaded-" + side + "-intent.json", ".loaded-" + side + ".json", ".timer-enable-" + side + ".json", ".timer-activity-" + side + "-intent.json", ".timer-activity-" + side + ".json", ".enrollment-" + side + ".json"} {
			allowed[operation+suffix] = true
		}
		for n := 1; n <= mailActivityMaxAttempts; n++ {
			for _, suffix := range []string{"", "-failed"} {
				allowed[fmt.Sprintf("%s.timer-activity-%s-attempt-%d%s.json", operation, side, n, suffix)] = true
			}
		}
		for _, phase := range []string{"load", "enable"} {
			for n := 1; n <= mailEnrollmentReloadLimit; n++ {
				for _, suffix := range []string{"", "-failed"} {
					allowed[fmt.Sprintf("%s.enrollment-%s-%s-attempt-%d%s.json", operation, phase, side, n, suffix)] = true
				}
			}
		}
	}
	fd, err := unix.Openat(int(c.parent.file.Fd()), ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return fail(ReasonReadFailed)
	}
	listing := os.NewFile(uintptr(fd), "mail enrollment journal")
	defer listing.Close()
	names, err := listing.Readdirnames(4097)
	if err != nil && err != io.EOF || len(names) > 4096 {
		return fail(ReasonReadFailed)
	}
	for _, name := range names {
		if strings.HasPrefix(name, operation) && !allowed[name] {
			return fail(ReasonUnsupported)
		}
	}
	return c.revalidate()
}
