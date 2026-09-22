//go:build linux

package recoveryruntime

import (
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

func TestMailActivityDisposableVMTransition(t *testing.T) {
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
	target, phase := os.Getenv("CP_MAIL_ACTIVITY_NATIVE_TARGET"), os.Getenv("CP_MAIL_ACTIVITY_NATIVE_PHASE")
	if !ValidDigest(target) || (phase != "forward-cut" && phase != "rollback-cut" && phase != "recover") {
		t.Fatal("explicit native target/phase required")
	}
	const operation = "67ca3eb8c16d4131ac4782b13c0a3199"
	paths := mailCapturePaths{MailRenewalHookPath, "/etc/systemd/system", mailrenewalkit.InstalledRoot, "/root/celikpanel-release-recovery-lab/mail-native-activity-journal", transactionPath}
	if e = verifyEnrollmentLock(paths.transaction, 9); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	commands := mailLoadedCommands{
		observe: func(ctx context.Context, unit string) ([]byte, error) {
			args := []string{"show"}
			for _, property := range mailrenewalkit.ScheduleProperties() {
				args = append(args, "--property="+property)
			}
			args = append(args, unit)
			raw, err := exec.CommandContext(ctx, "/usr/bin/systemctl", args...).Output()
			code := 0
			if err != nil {
				var exit *exec.ExitError
				if !errors.As(err, &exit) {
					return nil, err
				}
				code = exit.ExitCode()
			}
			if _, err = mailrenewalkit.ParseUnitObservationResult(unit, raw, code); err != nil {
				return nil, err
			}
			return raw, nil
		},
		reload: func(ctx context.Context) error {
			return exec.CommandContext(ctx, "/usr/bin/systemctl", "daemon-reload").Run()
		},
	}
	capturePath := filepath.Join(paths.journals, operation+".json")
	captured, e := os.ReadFile(capturePath)
	if os.IsNotExist(e) && phase == "forward-cut" {
		raw, err := commands.observe(ctx, mailrenewalkit.ServiceName)
		if err != nil {
			t.Fatal(err)
		}
		service, err := mailrenewalkit.ParseUnitObservation(mailrenewalkit.ServiceName, raw)
		if err != nil {
			t.Fatal(err)
		}
		raw, err = commands.observe(ctx, mailrenewalkit.TimerName)
		if err != nil {
			t.Fatal(err)
		}
		timer, err := mailrenewalkit.ParseUnitObservation(mailrenewalkit.TimerName, raw)
		if err != nil {
			t.Fatal(err)
		}
		state, err := mailrenewalkit.TransitionTimer(false, service, timer)
		if err != nil {
			t.Fatal(err)
		}
		captured, e = captureMailRenewalBeforeImageAt(operation, target, state, 9, paths, nil)
	}
	if e != nil {
		t.Fatal(e)
	}
	captureSHA := Digest(captured)
	var before mailCaptureRecord
	if e = decodePromotion(captured, &before); e != nil {
		t.Fatal(e)
	}
	if before.Contract.Target != target || before.Contract.Previous != "" {
		t.Fatal("bootstrap target and original absence required")
	}
	planRaw, e := prepareMailFilesAt(operation, captureSHA, 9, paths, nil)
	if e != nil {
		t.Fatal(e)
	}
	var plan mailFilesRecord
	if e = decodePromotion(planRaw, &plan); e != nil {
		t.Fatal(e)
	}
	checkpoint := func(point string) {
		if phase == "forward-cut" && point == "activity_forward_acted" || phase == "rollback-cut" && point == "activity_rollback_acted" {
			t.Logf("native_activity_cut=%s operation=%s previous=%s target=%s capture=%s", point, operation, before.Contract.Previous, target, captureSHA)
			unix.Kill(os.Getpid(), syscall.SIGKILL)
			panic("kill returned")
		}
	}
	if e = applyMailFilesAt(operation, captureSHA, "forward", 9, paths, nil); e != nil {
		t.Fatal(e)
	}
	if _, err := os.Stat(filepath.Join(paths.journals, operation+".loaded-forward.json")); os.IsNotExist(err) {
		if e = reloadMailFilesAt(ctx, operation, captureSHA, "forward", 9, paths, commands, nil); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = prepareMailEnableAt(ctx, operation, captureSHA, 9, paths, commands, nil); e != nil {
		t.Fatal(e)
	}
	if _, err := os.Stat(filepath.Join(paths.journals, operation+".timer-enable-forward.json")); os.IsNotExist(err) {
		if e = applyMailEnableAt(ctx, operation, captureSHA, "forward", 9, paths, commands, nil); e != nil {
			t.Fatal(e)
		}
	}
	activity := mailActivityCommands{
		observe: commands.observe,
		startTimer: func(ctx context.Context) error {
			return exec.CommandContext(ctx, "/usr/bin/systemctl", "start", mailrenewalkit.TimerName).Run()
		},
		stopTimer: func(ctx context.Context) error {
			return exec.CommandContext(ctx, "/usr/bin/systemctl", "stop", mailrenewalkit.TimerName).Run()
		},
	}
	for {
		_, err := activity.state(ctx)
		if !errors.Is(err, mailrenewalkit.ErrScheduleBusy) {
			if err != nil {
				t.Fatal(err)
			}
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
	if phase != "recover" {
		if e = applyMailActivityAt(ctx, operation, captureSHA, "forward", 9, paths, activity, checkpoint); e != nil {
			t.Fatal(e)
		}
		if phase == "forward-cut" {
			t.Fatal("forward activity cut not reached")
		}
	}
	if e = applyMailActivityAt(ctx, operation, captureSHA, "rollback", 9, paths, activity, checkpoint); e != nil {
		t.Fatal(e)
	}
	if phase != "recover" {
		t.Fatal("rollback activity cut not reached")
	}
	if e = applyMailEnableAt(ctx, operation, captureSHA, "rollback", 9, paths, commands, nil); e != nil {
		t.Fatal(e)
	}
	if e = applyMailFilesAt(operation, captureSHA, "rollback", 9, paths, nil); e != nil {
		t.Fatal(e)
	}
	if e = reloadMailFilesAt(ctx, operation, captureSHA, "rollback", 9, paths, commands, nil); e != nil {
		t.Fatal(e)
	}
	assertMailNativeSide(t, paths, before, plan, false)
	if _, e = os.Lstat(filepath.Join(paths.units, mailTimerWants, mailrenewalkit.TimerName)); !os.IsNotExist(e) {
		t.Fatal("native enablement not restored", e)
	}
	if e = commands.observeTransition(ctx, before.Contract.TimerBefore, false, true, false); e != nil {
		t.Fatal(e)
	}
	t.Logf("native_activity_inverse=verified native_units=absent original_inodes=restored operation=%s previous=%s target=%s", operation, before.Contract.Previous, target)
}
