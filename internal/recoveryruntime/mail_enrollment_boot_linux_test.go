//go:build linux

package recoveryruntime

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/alicelik/celikpanel/internal/mailrenewalkit"
	"golang.org/x/sys/unix"
)

func bootEnrollmentChild(root, target, scenario string, host, release *os.File) *exec.Cmd {
	c := exec.Command(os.Args[0], "-test.run=^TestMailEnrollmentBootChild$", "-test.v")
	c.Env = append(os.Environ(), "CP_MAIL_BOOT_ROOT="+root, "CP_MAIL_BOOT_TARGET="+target, "CP_MAIL_BOOT_CASE="+scenario)
	c.ExtraFiles = []*os.File{nil, nil, nil, nil, nil, host, release}
	return c
}
func TestMailEnrollmentBootProcessCuts(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root descriptor fixture")
	}
	for _, point := range []string{"boot_plan_durable", "boot_plan_published", "boot_plan_parent_durable", "boot_unit_moved", "boot_enable_moved", "boot_armed_durable", "boot_armed_published", "boot_armed_parent_durable"} {
		t.Run(point, func(t *testing.T) {
			root, target, host, release := enrollmentFixture(t, "absent")
			out, err := bootEnrollmentChild(root, target, "cut:"+point, host, release).CombinedOutput()
			if err == nil {
				t.Fatal("missing cut", string(out))
			}
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
				t.Fatalf("%v %s", err, out)
			}
			if out, err = bootEnrollmentChild(root, target, "resume", host, release).CombinedOutput(); err != nil {
				t.Fatalf("%v %s", err, out)
			}
		})
	}
}
func TestMailEnrollmentBootAuthority(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root descriptor fixture")
	}
	for _, scenario := range []string{"budget", "no-arm", "owner-unit", "owner-link", "same-bytes-new-inode", "owner-mode", "owner-stage", "missing-unit", "missing-link", "future-attempt", "gap-attempt", "denied", "cancelled", "unowned-unit", "unowned-link"} {
		t.Run(scenario, func(t *testing.T) {
			root, target, host, release := enrollmentFixture(t, "legacy")
			if out, err := bootEnrollmentChild(root, target, scenario, host, release).CombinedOutput(); err != nil {
				t.Fatalf("%v %s", err, out)
			}
		})
	}
}
func TestMailEnrollmentBootChild(t *testing.T) {
	root := os.Getenv("CP_MAIL_BOOT_ROOT")
	if root == "" {
		t.Skip("descriptor subprocess")
	}
	target, scenario := os.Getenv("CP_MAIL_BOOT_TARGET"), os.Getenv("CP_MAIL_BOOT_CASE")
	paths := mailCapturePaths{filepath.Join(root, "hooks", mailrenewalkit.HookName), filepath.Join(root, "units"), filepath.Join(root, "runtime"), filepath.Join(root, "journals"), filepath.Join(root, "transaction")}
	capture, err := captureMailRenewalBeforeImageAt(mailCaptureTestOperation, target, mailrenewalkit.TimerState{Enablement: "absent", Activity: "inactive"}, 9, paths, nil)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := prepareMailFilesAt(mailCaptureTestOperation, Digest(capture), 9, paths, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	denied := false
	e := &PreparedMailEnrollment{scope: mailEnrollmentScope{mailEnrollmentSchema, mailCaptureTestOperation, Digest(capture), Digest(plan), target}, paths: paths, binding: MailEnrollmentBinding{OwnerID: strings.Repeat("b", 32), HostLock: filepath.Join(root, "run", "mutation.lock"), VerifyAuthority: func() error {
		if denied {
			return errors.New("owner denied")
		}
		return nil
	}}}
	wants := filepath.Join(paths.units, mailEnrollmentBootWants)
	if err = os.Mkdir(wants, 0755); err != nil && !os.IsExist(err) {
		t.Fatal(err)
	}
	unit := filepath.Join(paths.units, mailEnrollmentBootName(e.scope.Operation))
	link := filepath.Join(wants, filepath.Base(unit))
	if strings.HasPrefix(scenario, "unowned-") {
		p := unit
		if scenario == "unowned-link" {
			p = link
		}
		if err = os.WriteFile(p, []byte("owner content"), 0644); err != nil {
			t.Fatal(err)
		}
		if e.ArmBoot(ctx) == nil {
			t.Fatal("owner file adopted")
		}
		raw, _ := os.ReadFile(p)
		if string(raw) != "owner content" {
			t.Fatal("owner content changed")
		}
		return
	}
	if scenario == "no-arm" {
		if e.ClaimBootAttempt(ctx) == nil {
			t.Fatal("unarmed continuation")
		}
		return
	}
	err = e.armBoot(ctx, func(point string) {
		if scenario == "cut:"+point {
			_ = unix.Kill(os.Getpid(), unix.SIGKILL)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = e.ArmBoot(ctx); err != nil {
		t.Fatal("arm retry", err)
	}
	if scenario == "resume" {
		if err = e.ClaimBootAttempt(ctx); err != nil {
			t.Fatal(err)
		}
		return
	}
	switch scenario {
	case "budget":
		for n := 0; n < mailEnrollmentBootLimit; n++ {
			if err = e.ClaimBootAttempt(ctx); err != nil {
				t.Fatal(err)
			}
		}
		var budget *MailEnrollmentBootBudget
		if err = e.ClaimBootAttempt(ctx); !errors.As(err, &budget) {
			t.Fatal("budget not retained", err)
		}
		return
	case "owner-unit":
		capturePut(t, unit, []byte("owner replacement"), 0644)
	case "same-bytes-new-inode":
		raw, _ := os.ReadFile(unit)
		if err = os.Rename(unit, unit+".owner-before"); err != nil {
			t.Fatal(err)
		}
		capturePut(t, unit, raw, 0644)
	case "owner-link":
		if err = os.Remove(link); err != nil {
			t.Fatal(err)
		}
		if err = os.Symlink("/dev/null", link); err != nil {
			t.Fatal(err)
		}
	case "owner-mode":
		if err = os.Chmod(unit, 0600); err != nil {
			t.Fatal(err)
		}
	case "missing-unit":
		if err = os.Remove(unit); err != nil {
			t.Fatal(err)
		}
	case "missing-link":
		if err = os.Remove(link); err != nil {
			t.Fatal(err)
		}
	case "owner-stage":
		raw, _ := os.ReadFile(filepath.Join(paths.journals, e.scope.Operation+".boot-plan.json"))
		p, err := e.decodeBootPlan(raw)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.Chmod(filepath.Join(paths.units, ".celikpanel-mail-boot-"+p.Nonce), 0755); err != nil {
			t.Fatal(err)
		}
	case "future-attempt":
		capturePut(t, filepath.Join(paths.journals, e.scope.Operation+".boot-attempt-4.json"), []byte("{}\n"), 0600)
	case "gap-attempt":
		raw, _ := os.ReadFile(filepath.Join(paths.journals, e.scope.Operation+".boot-plan.json"))
		receipt, _ := promotionJSON(mailEnrollmentBootReceipt{mailEnrollmentBootSchema, Digest(raw), 2})
		capturePut(t, filepath.Join(paths.journals, e.scope.Operation+".boot-attempt-2.json"), receipt, 0600)
	case "denied":
		denied = true
	case "cancelled":
		cancel()
	default:
		t.Fatal("unknown scenario")
	}
	if e.ClaimBootAttempt(ctx) == nil {
		t.Fatal("changed proof accepted")
	}
	if _, err = os.Lstat(filepath.Join(paths.journals, e.scope.Operation+".boot-attempt-1.json")); !os.IsNotExist(err) {
		t.Fatal("denied attempt consumed or synthesized evidence", err)
	}
	if strings.HasPrefix(scenario, "missing-") || strings.HasPrefix(scenario, "owner-") || scenario == "same-bytes-new-inode" {
		if e.ArmBoot(ctx) == nil {
			t.Fatal("owner edit repaired")
		}
	}
}
