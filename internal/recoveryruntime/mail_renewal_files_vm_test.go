//go:build linux

package recoveryruntime

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/alicelik/celikpanel/internal/mailrenewalkit"
	"golang.org/x/sys/unix"
)

// This entry is test-only, explicitly guarded and absent from production code.
// The external controller additionally pins the QEMU process, SSH host key,
// machine identity and root nonce before granting this fixture operation.
func TestMailFilesDisposableVMTransition(t *testing.T) {
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
	target := os.Getenv("CP_MAIL_FILES_NATIVE_TARGET")
	if !ValidDigest(target) {
		t.Fatal("explicit target required")
	}
	phase := os.Getenv("CP_MAIL_FILES_NATIVE_PHASE")
	if phase != "forward-cut" && phase != "rollback-cut" && phase != "recover" {
		t.Fatal("explicit native phase required")
	}
	const operation = "a4c0f42e45db4cc7af843fd2ab338907"
	paths := mailCapturePaths{MailRenewalHookPath, "/etc/systemd/system", mailrenewalkit.InstalledRoot, "/root/celikpanel-release-recovery-lab/mail-native-files-journal", transactionPath}
	if e = verifyEnrollmentLock(paths.transaction, 9); e != nil {
		t.Fatal(e)
	}
	// Preparation/activation of loaded units is outside this file-only trial.
	for _, args := range [][]string{{"is-enabled", mailrenewalkit.TimerName}, {"is-active", mailrenewalkit.TimerName}} {
		raw, e := exec.Command("systemctl", args...).Output()
		if e != nil {
			t.Fatal(e)
		}
		expected := "enabled"
		if args[0] == "is-active" {
			expected = "active"
		}
		if strings.TrimSpace(string(raw)) != expected {
			t.Fatal("unexpected native timer", string(raw))
		}
	}
	capturePath := filepath.Join(paths.journals, operation+".json")
	captured, e := os.ReadFile(capturePath)
	if os.IsNotExist(e) && phase == "forward-cut" {
		captured, e = captureMailRenewalBeforeImageAt(operation, target, mailrenewalkit.TimerState{Enablement: "enabled", Activity: "active"}, 9, paths, nil)
	}
	if e != nil {
		t.Fatal(e)
	}
	captureSHA := Digest(captured)
	var before mailCaptureRecord
	if e = decodePromotion(captured, &before); e != nil {
		t.Fatal(e)
	}
	if before.Contract.Previous == "" || before.Contract.Target != target {
		t.Fatal("independent predecessor/target required")
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
		if phase == "forward-cut" && point == "forward_"+mailrenewalkit.HookName || phase == "rollback-cut" && point == "rollback_"+mailrenewalkit.HookName {
			t.Logf("native_cut=%s operation=%s previous=%s target=%s capture=%s", point, operation, before.Contract.Previous, target, captureSHA)
			unix.Kill(os.Getpid(), syscall.SIGKILL)
			panic("kill returned")
		}
	}
	direction := "rollback"
	if phase == "forward-cut" {
		direction = "forward"
	}
	if e = applyMailFilesAt(operation, captureSHA, direction, 9, paths, checkpoint); e != nil {
		t.Fatal(e)
	}
	if phase != "recover" {
		t.Fatal("expected native cut not reached")
	}
	assertMailNativeSide(t, paths, before, plan, false)
	if e = applyMailFilesAt(operation, captureSHA, "forward", 9, paths, nil); e == nil {
		t.Fatal("rollback direction reversed")
	}
	saved, e := os.ReadFile(capturePath)
	if e != nil || !bytes.Equal(saved, captured) {
		t.Fatal("capture evidence changed")
	}
	t.Logf("native_inverse=verified original_inodes=restored capture=preserved direction=rollback operation=%s previous=%s target=%s", operation, before.Contract.Previous, target)
}
