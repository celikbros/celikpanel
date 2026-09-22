//go:build linux

package recoveryruntime

import (
	"context"
	"errors"
	"fmt"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
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

func enrollmentChild(root, target, scenario string, host, release *os.File) *exec.Cmd {
	c := exec.Command(os.Args[0], "-test.run=^TestMailEnrollmentChild$", "-test.v")
	c.Env = append(os.Environ(), "CP_MAIL_ENROLL_ROOT="+root, "CP_MAIL_ENROLL_TARGET="+target, "CP_MAIL_ENROLL_CASE="+scenario)
	c.ExtraFiles = []*os.File{nil, nil, nil, nil, nil, host, release}
	return c
}
func enrollmentFixture(t *testing.T, kind string) (string, string, *os.File, *os.File) {
	t.Helper()
	root, release := mailRenewalTestRoot(t)
	_, target, _ := mailCaptureFixture(t, root, kind)
	if e := os.Mkdir(filepath.Join(root, "run"), 0700); e != nil {
		t.Fatal(e)
	}
	host, e := os.OpenFile(filepath.Join(root, "run", "mutation.lock"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { host.Close() })
	for _, f := range []*os.File{release, host} {
		if e = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != nil {
			t.Fatal(e)
		}
	}
	return root, target, host, release
}
func TestMailEnrollmentProcessCuts(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root descriptor fixture")
	}
	forward := []string{
		"enrollment_acceptance_durable", "enrollment_acceptance_published", "enrollment_acceptance_parent_durable",
		"forward_" + mailrenewalkit.ServiceName, "forward_" + mailrenewalkit.TimerName, "forward_" + mailrenewalkit.HookName,
		"forward_receipt_published", "loaded_forward_intent_published", "enrollment_load-forward_attempt_published", "loaded_forward_reloaded", "loaded_forward_receipt_published",
		"enable_link_staged", "enable_plan_published", "enable_forward_moved", "enrollment_enable-forward_attempt_published", "enable_forward_reloaded", "enable_forward_receipt_published",
		"activity_forward_intent_published", "activity_forward_attempt_published", "activity_forward_acted", "activity_forward_receipt_published",
		"enrollment_forward_receipt_durable", "enrollment_forward_receipt_published", "enrollment_forward_receipt_parent_durable",
	}
	inverse := []string{"enrollment_rollback_intent_durable", "enrollment_rollback_intent_published",
		"activity_rollback_intent_published", "activity_rollback_acted", "activity_rollback_receipt_published",
		"enable_rollback_intent_published", "enable_rollback_moved", "enable_rollback_receipt_published",
		"rollback_intent_published", "rollback_" + mailrenewalkit.HookName, "rollback_" + mailrenewalkit.ServiceName,
		"rollback_receipt_published", "loaded_rollback_reloaded", "loaded_rollback_receipt_published", "enrollment_rollback_receipt_published"}
	for _, kind := range []string{"absent", "legacy"} {
		for _, side := range []string{"forward", "rollback"} {
			points := forward
			if side == "rollback" {
				points = inverse
			}
			for _, point := range points {
				t.Run(kind+"/"+point, func(t *testing.T) {
					root, target, host, release := enrollmentFixture(t, kind)
					c := enrollmentChild(root, target, "cut:"+side+":"+point, host, release)
					out, e := c.CombinedOutput()
					if e == nil {
						t.Fatal("missing kill", string(out))
					}
					status, ok := c.ProcessState.Sys().(syscall.WaitStatus)
					if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
						t.Fatalf("wrong cut %v %s", e, out)
					}
					if out, e = enrollmentChild(root, target, "resume:"+side, host, release).CombinedOutput(); e != nil {
						t.Fatalf("resume %v %s", e, out)
					}
				})
			}
		}
	}
}
func TestMailEnrollmentPartialForwardCompensation(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root descriptor fixture")
	}
	for _, point := range []string{"enrollment_acceptance_published", "forward_" + mailrenewalkit.ServiceName, "forward_" + mailrenewalkit.HookName, "loaded_forward_intent_published", "loaded_forward_reloaded", "enable_link_staged", "enable_plan_published", "enable_forward_moved", "activity_forward_attempt_published", "activity_forward_acted"} {
		t.Run(point, func(t *testing.T) {
			root, target, host, release := enrollmentFixture(t, "legacy")
			c := enrollmentChild(root, target, "cut:forward:"+point, host, release)
			out, e := c.CombinedOutput()
			if e == nil {
				t.Fatal("missing cut", string(out))
			}
			st, ok := c.ProcessState.Sys().(syscall.WaitStatus)
			if !ok || !st.Signaled() || st.Signal() != syscall.SIGKILL {
				t.Fatalf("wrong cut %v %s", e, out)
			}
			if out, e = enrollmentChild(root, target, "resume:rollback", host, release).CombinedOutput(); e != nil {
				t.Fatalf("partial inverse %v %s", e, out)
			}
		})
	}
}

func TestMailEnrollmentRefusesLostAuthorityAndOwnerDrift(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root descriptor fixture")
	}
	for _, scenario := range []string{"normal", "reload-budget", "deny", "no-host-lock", "wrong-scope", "unknown-native", "owner-stop", "owner-hook", "bad-history", "future-attempt", "orphan-history", "late-deny", "late-host-replace", "late-capture-edit", "busy", "rollback-after-partial"} {
		t.Run(scenario, func(t *testing.T) {
			root, target, host, release := enrollmentFixture(t, "legacy")
			if out, e := enrollmentChild(root, target, scenario, host, release).CombinedOutput(); e != nil {
				t.Fatalf("%v %s", e, out)
			}
		})
	}
}
func TestMailEnrollmentChild(t *testing.T) {
	root := os.Getenv("CP_MAIL_ENROLL_ROOT")
	if root == "" {
		t.Skip("descriptor subprocess")
	}
	target, scenario := os.Getenv("CP_MAIL_ENROLL_TARGET"), os.Getenv("CP_MAIL_ENROLL_CASE")
	reserved := strings.HasPrefix(scenario, "reserved:")
	scenario = strings.TrimPrefix(scenario, "reserved:")
	paths := mailCapturePaths{filepath.Join(root, "hooks", mailrenewalkit.HookName), filepath.Join(root, "units"), filepath.Join(root, "runtime"), filepath.Join(root, "journals"), filepath.Join(root, "transaction")}
	capture, e := os.ReadFile(filepath.Join(paths.journals, mailCaptureTestOperation+".json"))
	if os.IsNotExist(e) {
		capture, e = captureMailRenewalBeforeImageAt(mailCaptureTestOperation, target, mailrenewalkit.TimerState{Enablement: "absent", Activity: "inactive"}, 9, paths, nil)
	}
	if e != nil {
		t.Fatal(e)
	}
	plan, e := prepareMailFilesAt(mailCaptureTestOperation, Digest(capture), 9, paths, nil)
	if e != nil {
		t.Fatal(e)
	}
	scope := mailEnrollmentScope{mailEnrollmentSchema, mailCaptureTestOperation, Digest(capture), Digest(plan), target}
	cache, activity := filepath.Join(root, "native-cache"), filepath.Join(root, "native-activity")
	for path, value := range map[string]string{cache: "absent", activity: "inactive"} {
		if _, e = os.Stat(path); os.IsNotExist(e) {
			capturePut(t, path, []byte(value), 0600)
		}
	}
	denied, unknown, busy := false, false, false
	failReload := false
	calls := 0
	guard := mailEnrollmentGuard{HostLock: filepath.Join(root, "run", "mutation.lock"), Verify: func(s mailEnrollmentScope) error {
		if denied || s != scope {
			return errors.New("outer accepted fence unavailable")
		}
		return nil
	}}
	observe := func(_ context.Context, unit string) ([]byte, error) {
		if unknown {
			return nil, errors.New("native unavailable")
		}
		saved, e := os.ReadFile(cache)
		if e != nil {
			return nil, e
		}
		value, e := os.ReadFile(activity)
		if e != nil {
			return nil, e
		}
		loaded := string(saved) != "absent"
		enable, fragment, load, active := "", "", "not-found", "inactive"
		if loaded {
			load = "loaded"
			fragment = "/etc/systemd/system/" + unit
			enable = "static"
			if unit == mailrenewalkit.TimerName {
				enable = "disabled"
				if string(saved) == "enabled" {
					enable = "enabled"
				}
				active = string(value)
			}
		}
		if busy && unit == mailrenewalkit.ServiceName {
			active = "activating"
		}
		_, diskErr := os.Stat(filepath.Join(paths.units, unit))
		onDisk := diskErr == nil
		_, linkErr := os.Lstat(filepath.Join(paths.units, mailTimerWants, mailrenewalkit.TimerName))
		link := linkErr == nil
		pending := "no"
		if onDisk != loaded || loaded && link != (string(saved) == "enabled") {
			pending = "yes"
		}
		return []byte(fmt.Sprintf("LoadState=%s\nFragmentPath=%s\nDropInPaths=\nNeedDaemonReload=%s\nActiveState=%s\nUnitFileState=%s\n", load, fragment, pending, active, enable)), nil
	}
	commands := mailEnrollmentCommands{
		loaded: mailLoadedCommands{observe: observe, reload: func(context.Context) error {
			calls++
			if failReload {
				return errors.New("native reload failed")
			}
			next := "absent"
			if _, e := os.Stat(filepath.Join(paths.units, mailrenewalkit.ServiceName)); e == nil {
				next = "disabled"
				if _, e = os.Lstat(filepath.Join(paths.units, mailTimerWants, mailrenewalkit.TimerName)); e == nil {
					next = "enabled"
				}
			}
			capturePut(t, cache, []byte(next), 0600)
			return nil
		}},
		activity: mailActivityCommands{observe: observe, startTimer: func(context.Context) error { calls++; capturePut(t, activity, []byte("active"), 0600); return nil }, stopTimer: func(context.Context) error { calls++; capturePut(t, activity, []byte("inactive"), 0600); return nil }},
	}
	direction := "forward"
	cut := ""
	if strings.HasPrefix(scenario, "cut:") {
		parts := strings.SplitN(scenario, ":", 3)
		direction, cut = parts[1], parts[2]
	}
	if scenario == "resume:rollback" {
		direction = "rollback"
	}
	reservation := mailEnrollmentReservation{Path: filepath.Join(root, "ledger", "service-mutations.json"), OwnerID: strings.Repeat("b", 32)}
	identity := servicemutationledger.MailEnrollmentIdentity{RequestID: scope.Operation, OwnerID: reservation.OwnerID}
	scopeRaw, _ := promotionJSON(scope)
	identity.ScopeSHA256 = Digest(scopeRaw)
	if reserved {
		if _, e = os.Stat(filepath.Dir(reservation.Path)); os.IsNotExist(e) {
			if e = os.Mkdir(filepath.Dir(reservation.Path), 0700); e != nil {
				t.Fatal(e)
			}
		}
		if _, e = os.Stat(reservation.Path); os.IsNotExist(e) {
			empty := servicemutationledger.Ledger{Version: 1, Jobs: map[string]*servicemutationledger.ServiceMutationJob{}}
			ledger, err := servicemutationledger.AdmitMailEnrollment(&empty, identity, time.Now().UTC())
			if err != nil {
				t.Fatal(err)
			}
			raw, err := servicemutationledger.Encode(&ledger)
			if err != nil {
				t.Fatal(err)
			}
			capturePut(t, reservation.Path, raw, 0600)
		}
	}
	run := func(side string, checkpoint func(string)) error {
		if !reserved {
			return runMailEnrollmentAt(context.Background(), scope, side, paths, guard, commands, checkpoint)
		}
		// Test owner explicitly selects inverse in the common ledger first.
		// The production writer's fsync/SIGKILL behavior is tested in cmd/agent.
		if side == "rollback" {
			raw, err := os.ReadFile(reservation.Path)
			if err != nil {
				return err
			}
			ledger, err := servicemutationledger.Decode(raw)
			if err != nil {
				return err
			}
			state, err := servicemutationledger.MailEnrollmentState(&ledger, identity)
			if err != nil {
				return err
			}
			if state == "forward" {
				ledger, err = servicemutationledger.AdvanceMailEnrollment(&ledger, identity, "rollback", time.Now().UTC())
				if err != nil {
					return err
				}
				raw, err = servicemutationledger.Encode(&ledger)
				if err != nil {
					return err
				}
				capturePut(t, reservation.Path, raw, 0600)
			}
		}
		if scenario == "adapter" || scenario == "recorded" {
			binding := MailEnrollmentBinding{LedgerPath: reservation.Path, Owner: reservation.Owner, OwnerID: reservation.OwnerID, HostLock: guard.HostLock, HostOwner: guard.HostOwner, VerifyAuthority: func() error { return guard.Verify(scope) }, Native: enrollmentNativeFixture{commands}}
			var prepared *PreparedMailEnrollment
			var err error
			if scenario == "recorded" {
				agent := enrollmentRecordedAgent(t, root, target)
				defer agent.Close()
				var recordedSide string
				prepared, recordedSide, err = openRecordedMailEnrollmentAt(context.Background(), scope.Operation, paths, agent, binding)
				if err == nil && recordedSide != side {
					return errors.New("recorded direction cannot be reversed")
				}
			} else {
				prepared, err = openPreparedMailEnrollmentAt(context.Background(), scopeRaw, paths, binding)
			}
			if err != nil {
				return err
			}
			if prepared.Identity() != identity {
				return errors.New("prepared scope identity changed")
			}
			if err = prepared.Resume(context.Background(), side); err != nil {
				return err
			}
			before := calls
			if err = prepared.Verify(context.Background(), side); err != nil {
				return err
			}
			if calls != before {
				return errors.New("adapter verification changed native state")
			}
			return nil
		}
		return runReservedMailEnrollmentAt(context.Background(), scope, side, paths, guard, reservation, commands, checkpoint)
	}
	if strings.HasPrefix(scenario, "cut:rollback:") {
		if e = run("forward", nil); e != nil {
			t.Fatal("forward before inverse", e)
		}
	}
	checkpoint := func(point string) {
		if point == cut {
			unix.Kill(os.Getpid(), syscall.SIGKILL)
			panic("kill returned")
		}
		if point == "enrollment_acceptance_parent_durable" {
			if scenario == "reservation-late-replace" {
				raw, err := os.ReadFile(reservation.Path)
				if err != nil {
					t.Fatal(err)
				}
				if err = os.Rename(reservation.Path, reservation.Path+".retained"); err != nil {
					t.Fatal(err)
				}
				capturePut(t, reservation.Path, raw, 0600)
			}
			if scenario == "reservation-late-clear" {
				capturePut(t, reservation.Path, []byte(`{"version":1,"jobs":{}}`), 0600)
			}
			if scenario == "late-deny" {
				denied = true
			}
			if scenario == "late-host-replace" {
				if e := os.Rename(guard.HostLock, guard.HostLock+".retained"); e != nil {
					t.Fatal(e)
				}
				capturePut(t, guard.HostLock, nil, 0600)
			}
			if scenario == "late-capture-edit" {
				capturePut(t, filepath.Join(paths.journals, scope.Operation+".json"), []byte("{}\n"), 0600)
			}
		}
	}
	if scenario == "reload-budget" {
		failReload = true
		for n := 1; n <= mailEnrollmentReloadLimit; n++ {
			if e = run("forward", nil); e == nil || calls != n {
				t.Fatal("reload budget count", n, e, calls)
			}
		}
		var budget *mailEnrollmentReloadBudget
		if e = run("forward", nil); !errors.As(e, &budget) || calls != mailEnrollmentReloadLimit {
			t.Fatal("budget not retained", e, calls)
		}
		failReload = false
		// Exact owner-completed native reload is observed; no budget reset or
		// fourth command at the exhausted load phase is necessary.
		capturePut(t, cache, []byte("disabled"), 0600)
		if e = run("forward", nil); e != nil {
			t.Fatal("owner-completed reload not verified", e)
		}
		if _, e = os.Stat(filepath.Join(paths.journals, scope.Operation+".enrollment-load-forward-attempt-1-failed.json")); e != nil {
			t.Fatal("known failure lost", e)
		}
		if calls != mailEnrollmentReloadLimit+2 {
			t.Fatal("unexpected commands after owner continuation", calls)
		}
	}
	expectRefusal := false
	switch scenario {
	case "reservation-missing":
		os.Remove(reservation.Path)
		expectRefusal = true
	case "reservation-owner":
		reservation.OwnerID = strings.Repeat("c", 32)
		expectRefusal = true
	case "reservation-group":
		reservation.Owner.GID = 65534
		expectRefusal = true
	case "reservation-closed":
		raw, _ := os.ReadFile(reservation.Path)
		ledger, err := servicemutationledger.Decode(raw)
		if err != nil {
			t.Fatal(err)
		}
		ledger, err = servicemutationledger.AdvanceMailEnrollment(&ledger, identity, "published", time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
		raw, err = servicemutationledger.Encode(&ledger)
		if err != nil {
			t.Fatal(err)
		}
		capturePut(t, reservation.Path, raw, 0600)
		expectRefusal = true
	case "reservation-late-clear", "reservation-late-replace":
		expectRefusal = true
	case "deny":
		denied = true
		expectRefusal = true
	case "no-host-lock":
		unix.Close(8)
		expectRefusal = true
	case "wrong-scope":
		scope.Target = strings.Repeat("f", 64)
		expectRefusal = true
	case "unknown-native":
		unknown = true
		expectRefusal = true
	case "busy":
		busy = true
		expectRefusal = true
	case "late-deny", "late-host-replace", "late-capture-edit":
		expectRefusal = true
	case "future-attempt":
		capturePut(t, filepath.Join(paths.journals, scope.Operation+".enrollment-load-forward-attempt-4.json"), []byte("{}\n"), 0600)
		expectRefusal = true
	case "orphan-history":
		if e = applyMailFilesAt(scope.Operation, scope.CaptureSHA256, "forward", 9, paths, nil); e != nil {
			t.Fatal(e)
		}
		expectRefusal = true
	case "owner-stop", "owner-hook", "bad-history":
		if e = run("forward", nil); e != nil {
			t.Fatal("initial", e)
		}
		calls = 0
		if scenario == "owner-stop" {
			capturePut(t, activity, []byte("inactive"), 0600)
		}
		if scenario == "owner-hook" {
			capturePut(t, paths.hook, []byte("owner change"), 0755)
		}
		if scenario == "bad-history" {
			capturePut(t, filepath.Join(paths.journals, scope.Operation+".loaded-forward.json"), []byte("{}\n"), 0600)
		}
		expectRefusal = true
	case "rollback-after-partial":
		unknownAfter := func(point string) {
			if point == "forward_receipt_parent_durable" {
				unknown = true
			}
		}
		if e = run("forward", unknownAfter); e == nil {
			t.Fatal("missing unknown load")
		}
		unknown = false
		direction = "rollback"
	}
	e = run(direction, checkpoint)
	if expectRefusal {
		if e == nil || calls != 0 {
			t.Fatal("unsafe admission", e, calls)
		}
		return
	}
	if e != nil {
		t.Fatal("enrollment", direction, e)
	}
	if cut != "" {
		t.Fatal("checkpoint not reached", cut)
	}
	if reserved {
		before := calls
		if e = verifyReservedMailEnrollmentAt(context.Background(), scope, direction, paths, guard, reservation, commands.loaded); e != nil {
			t.Fatal("terminal native proof", e)
		}
		if calls != before {
			t.Fatal("terminal proof ran native mutation")
		}
		if scenario == "terminal-ledger-observer" {
			raw, _ := os.ReadFile(reservation.Path)
			ledger, err := servicemutationledger.Decode(raw)
			if err != nil {
				t.Fatal(err)
			}
			ledger, err = servicemutationledger.AdvanceMailEnrollment(&ledger, identity, "published", time.Now().UTC())
			if err != nil {
				t.Fatal(err)
			}
			raw, err = servicemutationledger.Encode(&ledger)
			if err != nil {
				t.Fatal(err)
			}
			capturePut(t, reservation.Path, raw, 0600)
			if e = verifyReservedMailEnrollmentAt(context.Background(), scope, "forward", paths, guard, reservation, commands.loaded); e != nil {
				t.Fatal("released terminal retry", e)
			}
			if e = run("forward", nil); e == nil {
				t.Fatal("released reservation reopened mutation")
			}
			capturePut(t, activity, []byte("inactive"), 0600)
			if e = verifyReservedMailEnrollmentAt(context.Background(), scope, "forward", paths, guard, reservation, commands.loaded); e == nil {
				t.Fatal("old receipt hid owner stop")
			}
			if calls != before {
				t.Fatal("observer repaired owner state")
			}
			return
		}
	}
	beforeCalls := calls
	if e = run(direction, nil); e != nil || calls != beforeCalls {
		t.Fatal("terminal repeat mutated", e, calls, beforeCalls)
	}
	if direction == "forward" {
		if e = run("rollback", nil); e != nil {
			t.Fatal("inverse", e)
		}
	}
	if e = run("forward", nil); e == nil {
		t.Fatal("forward resurrected after inverse")
	}
	beforeCalls = calls
	if e = run("rollback", nil); e != nil || calls != beforeCalls {
		t.Fatal("inverse repeat", e, calls, beforeCalls)
	}
	var original mailCaptureRecord
	if e = decodePromotion(capture, &original); e != nil {
		t.Fatal(e)
	}
	var files mailFilesRecord
	if e = decodePromotion(plan, &files); e != nil {
		t.Fatal(e)
	}
	assertMailNativeSide(t, paths, original, files, false)
}

func TestMailReservedEnrollmentComposition(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root descriptor fixture")
	}
	for _, scenario := range []string{"adapter", "recorded", "terminal-ledger-observer", "resume:rollback", "reservation-missing", "reservation-owner", "reservation-group", "reservation-closed", "reservation-late-clear", "reservation-late-replace"} {
		t.Run(scenario, func(t *testing.T) {
			root, target, host, release := enrollmentFixture(t, "legacy")
			out, e := enrollmentChild(root, target, "reserved:"+scenario, host, release).CombinedOutput()
			if e != nil {
				t.Fatal(e, string(out))
			}
		})
	}
	for _, side := range []string{"forward", "rollback"} {
		points := []string{"enrollment_acceptance_published", "enable_forward_moved", "activity_forward_acted"}
		if side == "rollback" {
			points = []string{"enrollment_rollback_intent_published", "enable_rollback_moved", "enrollment_rollback_receipt_published"}
		}
		for _, point := range points {
			t.Run(side+"/"+point, func(t *testing.T) {
				root, target, host, release := enrollmentFixture(t, "legacy")
				cmd := enrollmentChild(root, target, "reserved:cut:"+side+":"+point, host, release)
				out, e := cmd.CombinedOutput()
				if e == nil {
					t.Fatal("missing kill", string(out))
				}
				st, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
				if !ok || !st.Signaled() || st.Signal() != syscall.SIGKILL {
					t.Fatal(e, string(out))
				}
				out, e = enrollmentChild(root, target, "reserved:resume:"+side, host, release).CombinedOutput()
				if e != nil {
					t.Fatal("resume", e, string(out))
				}
			})
		}
	}
}

// A native adapter for the existing subprocess fixture; no second executor.
type enrollmentNativeFixture struct{ commands mailEnrollmentCommands }

func (n enrollmentNativeFixture) ObserveUnit(ctx context.Context, unit string) ([]byte, error) {
	return n.commands.loaded.observe(ctx, unit)
}
func (n enrollmentNativeFixture) Reload(ctx context.Context) error {
	return n.commands.loaded.reload(ctx)
}
func (n enrollmentNativeFixture) StartTimer(ctx context.Context) error {
	return n.commands.activity.startTimer(ctx)
}
func (n enrollmentNativeFixture) StopTimer(ctx context.Context) error {
	return n.commands.activity.stopTimer(ctx)
}
