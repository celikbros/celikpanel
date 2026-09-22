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
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
	"golang.org/x/sys/unix"
)

// Test-only outer authority: a guarded disposable VM and protected owner intent.
// This does not stand in for production fence/dispatch enrollment acceptance.
func TestMailEnrollmentDisposableVM(t *testing.T) {
	fixture := os.Getenv("CELIKPANEL_DISPOSABLE_MAIL_VM")
	reserved := fixture == "arch-20260923-reserved"
	if fixture != "arch-20260922-bootstrap" && !reserved {
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
	intentName := "mail-native-enrollment.intent"
	if reserved {
		paths.journals = filepath.Join(private, "mail-reserved-enrollment-journal")
		intentName = "mail-reserved-enrollment.intent"
	}
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
		name := filepath.Join(private, intentName)
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
	reservation := mailEnrollmentReservation{Path: filepath.Join(private, "mail-reserved-ledger", "service-mutations.json"), OwnerID: strings.Repeat("b", 32)}
	var identity servicemutationledger.MailEnrollmentIdentity
	if reserved {
		// The protected disposable owner intent admits this fixture reservation.
		// Its producer is deliberately recorded separately from the production
		// common writer; this trial proves real native consumers of that schema.
		if e = guard.Verify(scope); e != nil {
			t.Fatal(e)
		}
		scopeRaw, _ := promotionJSON(scope)
		identity = servicemutationledger.MailEnrollmentIdentity{RequestID: operation, OwnerID: reservation.OwnerID, ScopeSHA256: Digest(scopeRaw)}
		raw, found, err := servicemutationledger.ReadFile(reservation.Path, servicemutationledger.MaxSize, reservation.Owner)
		if err != nil {
			t.Fatal(err)
		}
		var ledger servicemutationledger.Ledger
		if found {
			ledger, err = servicemutationledger.Decode(raw)
			if err != nil {
				t.Fatal(err)
			}
		} else {
			if phase != "forward-cut" {
				t.Fatal("missing initial reservation")
			}
			ledger = servicemutationledger.Ledger{Version: 1, Jobs: map[string]*servicemutationledger.ServiceMutationJob{}}
			ledger, err = servicemutationledger.AdmitMailEnrollment(&ledger, identity, time.Now().UTC())
			if err != nil {
				t.Fatal(err)
			}
			writeMailEnrollmentVMFixtureLedger(t, reservation.Path, ledger)
		}
		if phase == "rollback-cut" {
			ledger, err = servicemutationledger.AdvanceMailEnrollment(&ledger, identity, "rollback", time.Now().UTC())
			if err != nil {
				t.Fatal(err)
			}
			writeMailEnrollmentVMFixtureLedger(t, reservation.Path, ledger)
		}
	}
	checkpoint := func(point string) {
		if point == cut {
			t.Logf("native_enrollment_cut=%s operation=%s target=%s capture=%s", point, operation, target, scope.CaptureSHA256)
			unix.Kill(os.Getpid(), syscall.SIGKILL)
			panic("kill returned")
		}
	}
	for {
		if reserved {
			e = runReservedMailEnrollmentAt(ctx, scope, direction, paths, guard, reservation, commands, checkpoint)
		} else {
			e = runMailEnrollmentAt(ctx, scope, direction, paths, guard, commands, checkpoint)
		}
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
	if reserved {
		if e = verifyReservedMailEnrollmentAt(ctx, scope, direction, paths, guard, reservation, commands.loaded); e != nil {
			t.Fatal(e)
		}
		if phase == "rollback-verify" {
			raw, found, err := servicemutationledger.ReadFile(reservation.Path, servicemutationledger.MaxSize, reservation.Owner)
			if err != nil || !found {
				t.Fatal(err)
			}
			ledger, err := servicemutationledger.Decode(raw)
			if err != nil {
				t.Fatal(err)
			}
			ledger, err = servicemutationledger.AdvanceMailEnrollment(&ledger, identity, "restored", time.Now().UTC())
			if err != nil {
				t.Fatal(err)
			}
			writeMailEnrollmentVMFixtureLedger(t, reservation.Path, ledger)
			if e = verifyReservedMailEnrollmentAt(ctx, scope, direction, paths, guard, reservation, commands.loaded); e != nil {
				t.Fatal("terminal ledger read-only proof", e)
			}
			if e = runReservedMailEnrollmentAt(ctx, scope, direction, paths, guard, reservation, commands, nil); e == nil {
				t.Fatal("terminal fence reopened")
			}
		}
	}
	t.Logf("native_enrollment_verified=%s operation=%s target=%s capture=%s files=%s", direction, operation, target, scope.CaptureSHA256, scope.FilesSHA256)
}

// Test fixture only. Existing producer publication crashes are tested through
// the actual common writer in cmd/agent; this helper adds no production path.
func writeMailEnrollmentVMFixtureLedger(t *testing.T, path string, ledger servicemutationledger.Ledger) {
	t.Helper()
	raw, e := servicemutationledger.Encode(&ledger)
	if e != nil {
		t.Fatal(e)
	}
	stage, e := os.CreateTemp(filepath.Dir(path), ".fixture-enrollment-")
	if e != nil {
		t.Fatal(e)
	}
	if e = stage.Chmod(0600); e != nil {
		t.Fatal(e)
	}
	if _, e = stage.Write(raw); e != nil {
		t.Fatal(e)
	}
	if e = stage.Sync(); e != nil {
		t.Fatal(e)
	}
	if e = stage.Close(); e != nil {
		t.Fatal(e)
	}
	if e = os.Rename(stage.Name(), path); e != nil {
		t.Fatal(e)
	}
	dir, e := os.Open(filepath.Dir(path))
	if e != nil {
		t.Fatal(e)
	}
	defer dir.Close()
	if e = dir.Sync(); e != nil {
		t.Fatal(e)
	}
}
