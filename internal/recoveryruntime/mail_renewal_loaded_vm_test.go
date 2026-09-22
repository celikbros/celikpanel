//go:build linux

package recoveryruntime

import (
	"context"
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

func TestMailLoadedDisposableVMTransition(t *testing.T) {
	if os.Getenv("CELIKPANEL_DISPOSABLE_MAIL_VM") != "debian13-20260911" {
		t.Skip("guarded disposable Debian VM only")
	}
	if os.Geteuid() != 0 {
		t.Fatal("root fixture required")
	}
	for _, name := range []string{"agent", "panel"} {
		if _, e := os.Stat(filepath.Join("/opt/celikpanel/bin", name)); !os.IsNotExist(e) {
			t.Fatal("management must be absent")
		}
	}
	if _, e := os.Stat("/var/lib/celikpanel-setup-vm"); e != nil {
		t.Fatal("fixture marker missing")
	}
	virt, e := exec.Command("systemd-detect-virt", "--vm").Output()
	if e != nil || (strings.TrimSpace(string(virt)) != "qemu" && strings.TrimSpace(string(virt)) != "kvm") {
		t.Fatal("QEMU required", e)
	}
	target, phase := os.Getenv("CP_MAIL_LOADED_NATIVE_TARGET"), os.Getenv("CP_MAIL_LOADED_NATIVE_PHASE")
	if !ValidDigest(target) || (phase != "forward-cut" && phase != "rollback-cut" && phase != "recover") {
		t.Fatal("explicit native target/phase required")
	}
	const operation = "2dbbb7485fe846de98e972cf2fa654c1"
	paths := mailCapturePaths{MailRenewalHookPath, "/etc/systemd/system", mailrenewalkit.InstalledRoot, "/root/celikpanel-release-recovery-lab/mail-native-loaded-journal", transactionPath}
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
			return exec.CommandContext(ctx, "/usr/bin/systemctl", args...).Output()
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
		state, err := mailrenewalkit.TransitionTimer(true, service, timer)
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
	if before.Contract.Target != target || before.Contract.Previous == "" {
		t.Fatal("independent target/predecessor required")
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
		if phase == "forward-cut" && point == "loaded_forward_reloaded" || phase == "rollback-cut" && point == "loaded_rollback_reloaded" {
			t.Logf("native_loaded_cut=%s operation=%s previous=%s target=%s capture=%s", point, operation, before.Contract.Previous, target, captureSHA)
			unix.Kill(os.Getpid(), syscall.SIGKILL)
			panic("kill returned")
		}
	}
	if phase != "recover" {
		if e = applyMailFilesAt(operation, captureSHA, "forward", 9, paths, nil); e != nil {
			t.Fatal(e)
		}
		if e = reloadMailFilesAt(ctx, operation, captureSHA, "forward", 9, paths, commands, checkpoint); e != nil {
			t.Fatal(e)
		}
		if phase == "forward-cut" {
			t.Fatal("forward cut not reached")
		}
		if e = applyMailFilesAt(operation, captureSHA, "rollback", 9, paths, nil); e != nil {
			t.Fatal(e)
		}
	}
	if e = reloadMailFilesAt(ctx, operation, captureSHA, "rollback", 9, paths, commands, checkpoint); e != nil {
		t.Fatal(e)
	}
	if phase != "recover" {
		t.Fatal("rollback cut not reached")
	}
	assertMailNativeSide(t, paths, before, plan, false)
	if e = commands.observePair(ctx, before.Contract.TimerBefore, false); e != nil {
		t.Fatal(e)
	}
	t.Logf("native_loaded_inverse=verified timer_preference=preserved original_inodes=restored operation=%s previous=%s target=%s", operation, before.Contract.Previous, target)
}
