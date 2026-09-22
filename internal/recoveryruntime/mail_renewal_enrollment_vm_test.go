//go:build linux

package recoveryruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/mailrenewalkit"
	"golang.org/x/sys/unix"
)

// Test-only outer authority: a guarded disposable VM and protected owner intent.
// This does not stand in for production fence/dispatch enrollment acceptance.
func TestMailEnrollmentDisposableVM(t *testing.T) {
	if os.Getenv("CELIKPANEL_DISPOSABLE_MAIL_VM") != "arch-20260922-bootstrap" {
		t.Skip("guarded disposable Arch VM only")
	}
	if os.Geteuid() != 0 {
		t.Fatal("root fixture required")
	}
	for _, name := range []string{"agent", "panel"} {
		if _, e := os.Stat(filepath.Join("/opt/celikpanel/bin", name)); !os.IsNotExist(e) {
			t.Fatal("management must be absent")
		}
	}
	const markerPath = "/etc/celikpanel-release-recovery-lab"
	var markerStat unix.Stat_t
	if unix.Lstat(markerPath, &markerStat) != nil || markerStat.Mode != unix.S_IFREG|0444 || markerStat.Uid != 0 || markerStat.Gid != 0 || markerStat.Nlink != 1 || markerStat.Size > 2048 {
		t.Fatal("protected cloud-init lab marker required")
	}
	markerRaw, e := os.ReadFile(markerPath)
	if e != nil {
		t.Fatal(e)
	}
	var marker map[string]string
	if json.Unmarshal(markerRaw, &marker) != nil || marker["schema"] != "celikpanel-release-recovery-lab/v1" || marker["node"] != "arch" {
		t.Fatal("wrong disposable native fixture")
	}
	uuid, e := os.ReadFile("/sys/class/dmi/id/product_uuid")
	if e != nil || strings.ToLower(strings.TrimSpace(string(uuid))) != marker["vm_uuid"] {
		t.Fatal("cloud-init marker does not match this virtual machine")
	}
	virt, e := exec.Command("systemd-detect-virt", "--vm").Output()
	if e != nil || (strings.TrimSpace(string(virt)) != "qemu" && strings.TrimSpace(string(virt)) != "kvm") {
		t.Fatal("QEMU required", e)
	}

	target, operation, phase := os.Getenv("CP_MAIL_ENROLL_NATIVE_TARGET"), os.Getenv("CP_MAIL_ENROLL_NATIVE_OPERATION"), os.Getenv("CP_MAIL_ENROLL_NATIVE_PHASE")
	if !ValidDigest(target) || !validPromotionNonce(operation) {
		t.Fatal("exact native operation/target required")
	}
	direction, cut := "forward", ""
	switch phase {
	case "forward-cut":
		cut = "activity_forward_acted"
	case "forward-verify":
	case "rollback-cut":
		direction = "rollback"
		cut = "enable_rollback_moved"
	case "rollback-verify":
		direction = "rollback"
	default:
		t.Fatal("explicit native phase required")
	}
	const private = "/root/celikpanel-release-recovery-lab"
	paths := mailCapturePaths{MailRenewalHookPath, "/etc/systemd/system", mailrenewalkit.InstalledRoot, filepath.Join(private, "mail-native-enrollment-journal"), transactionPath}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	observe := func(ctx context.Context, unit string) ([]byte, error) {
		args := []string{"show"}
		for _, property := range mailrenewalkit.ScheduleProperties() {
			args = append(args, "--property="+property)
		}
		args = append(args, unit)
		raw, e := exec.CommandContext(ctx, "/usr/bin/systemctl", args...).Output()
		code := 0
		if e != nil {
			var exit *exec.ExitError
			if !errors.As(e, &exit) {
				return nil, e
			}
			code = exit.ExitCode()
		}
		if _, e = mailrenewalkit.ParseUnitObservationResult(unit, raw, code); e != nil {
			return nil, e
		}
		return raw, nil
	}
	commands := mailEnrollmentCommands{
		loaded: mailLoadedCommands{observe: observe, reload: func(ctx context.Context) error {
			return exec.CommandContext(ctx, "/usr/bin/systemctl", "daemon-reload").Run()
		}},
		activity: mailActivityCommands{observe: observe, startTimer: func(ctx context.Context) error {
			return exec.CommandContext(ctx, "/usr/bin/systemctl", "start", mailrenewalkit.TimerName).Run()
		}, stopTimer: func(ctx context.Context) error {
			return exec.CommandContext(ctx, "/usr/bin/systemctl", "stop", mailrenewalkit.TimerName).Run()
		}},
	}
	capture, e := os.ReadFile(filepath.Join(paths.journals, operation+".json"))
	if os.IsNotExist(e) && phase == "forward-cut" {
		if e = commands.loaded.observeTransition(ctx, mailrenewalkit.TimerState{Enablement: "absent", Activity: "inactive"}, false, true, false); e != nil {
			t.Fatal(e)
		}
		capture, e = captureMailRenewalBeforeImageAt(operation, target, mailrenewalkit.TimerState{Enablement: "absent", Activity: "inactive"}, 9, paths, nil)
	}
	if e != nil {
		t.Fatal(e)
	}
	plan, e := prepareMailFilesAt(operation, Digest(capture), 9, paths, nil)
	if e != nil {
		t.Fatal(e)
	}
	scope := mailEnrollmentScope{mailEnrollmentSchema, operation, Digest(capture), Digest(plan), target}
	guard := mailEnrollmentGuard{HostLock: "/run/celikpanel/service-mutation.lock", Verify: func(got mailEnrollmentScope) error {
		if got != scope {
			return fail(ReasonChanged)
		}
		var st unix.Stat_t
		name := filepath.Join(private, "mail-native-enrollment.intent")
		if unix.Lstat(name, &st) != nil || st.Mode != unix.S_IFREG|0600 || st.Uid != 0 || st.Gid != 0 || st.Nlink != 1 {
			return fail(ReasonUnsafeMetadata)
		}
		raw, e := os.ReadFile(name)
		if e != nil {
			return e
		}
		if !bytes.Equal(raw, []byte("operation="+operation+"\ntarget="+target+"\n")) {
			return fail(ReasonChanged)
		}
		return nil
	}}
	checkpoint := func(point string) {
		if point == cut {
			t.Logf("native_enrollment_cut=%s operation=%s target=%s capture=%s", point, operation, target, scope.CaptureSHA256)
			unix.Kill(os.Getpid(), syscall.SIGKILL)
			panic("kill returned")
		}
	}
	for {
		e = runMailEnrollmentAt(ctx, scope, direction, paths, guard, commands, checkpoint)
		if !errors.Is(e, mailrenewalkit.ErrScheduleBusy) {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
	if e != nil {
		t.Fatal(e)
	}
	if cut != "" {
		t.Fatal("native cut not reached")
	}
	t.Logf("native_enrollment_verified=%s operation=%s target=%s capture=%s files=%s", direction, operation, target, scope.CaptureSHA256, scope.FilesSHA256)
}
