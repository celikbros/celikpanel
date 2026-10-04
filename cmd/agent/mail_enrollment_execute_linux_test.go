//go:build linux

package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/alicelik/celikpanel/internal/hostmutationlock"
	"github.com/alicelik/celikpanel/internal/recoveryruntime"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
	"golang.org/x/sys/unix"
)

type enrollmentExecutionFixture struct {
	t              *testing.T
	root, scenario string
	id             servicemutationledger.MailEnrollmentIdentity
	resumes        int
}

func (e *enrollmentExecutionFixture) Identity() servicemutationledger.MailEnrollmentIdentity {
	return e.id
}
func (e *enrollmentExecutionFixture) cut(point string) {
	if e.scenario == "kill:"+point {
		unix.Kill(os.Getpid(), syscall.SIGKILL)
		panic("kill returned")
	}
}
func (e *enrollmentExecutionFixture) Resume(ctx context.Context, direction string) error {
	e.resumes++
	ledger := readEnrollmentLedger(e.t, e.root)
	state, err := servicemutationledger.MailEnrollmentState(&ledger, e.id)
	if err != nil || state != direction || ledger.ActiveRequestID != e.id.RequestID {
		return errors.New("native work lacked durable reservation")
	}
	e.cut("native-before")
	if e.scenario == "native-failure" {
		return errors.New("native result unknown")
	}
	if err = os.WriteFile(filepath.Join(e.root, "native-result"), []byte(direction), 0600); err != nil {
		return err
	}
	e.cut("native-after")
	return nil
}
func (e *enrollmentExecutionFixture) Verify(ctx context.Context, direction string) error {
	raw, err := os.ReadFile(filepath.Join(e.root, "native-result"))
	if err != nil || string(raw) != direction || e.scenario == "proof-unknown" {
		return errors.New("native proof unavailable")
	}
	e.cut("proof-after")
	return nil
}
func enrollmentExecuteChild(root, direction, scenario string, host, release *os.File) *exec.Cmd {
	c := exec.Command(os.Args[0], "-test.run=^TestMailEnrollmentExecuteChild$", "-test.v")
	c.Env = append(os.Environ(), "CP_ENROLL_EXEC_ROOT="+root, "CP_ENROLL_EXEC_DIRECTION="+direction, "CP_ENROLL_EXEC_CASE="+scenario)
	c.ExtraFiles = []*os.File{nil, nil, nil, nil, nil, host, release}
	return c
}
func runEnrollmentExecuteChild(t *testing.T, root, direction, scenario string, host, release *os.File) {
	t.Helper()
	if out, err := enrollmentExecuteChild(root, direction, scenario, host, release).CombinedOutput(); err != nil {
		t.Fatalf("%s/%s: %v %s", direction, scenario, err, out)
	}
}
func TestMailEnrollmentExecuteProcessCuts(t *testing.T) {
	for _, direction := range []string{"forward", "rollback"} {
		for _, point := range []string{"native-before", "native-after", "proof-after", "terminal-before_rename", "terminal-after_rename_before_directory_sync", "terminal-after_directory_sync"} {
			t.Run(direction+"/"+point, func(t *testing.T) {
				root, host, release := enrollmentLedgerFixture(t)
				if direction == "rollback" {
					runEnrollmentExecuteChild(t, root, "forward", "proof-unknown", host, release)
				}
				out, err := enrollmentExecuteChild(root, direction, "kill:"+point, host, release).CombinedOutput()
				var exit *exec.ExitError
				if !errors.As(err, &exit) || !exit.Sys().(syscall.WaitStatus).Signaled() || exit.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
					t.Fatalf("missing actual process cut: %v %s", err, out)
				}
				ledger := readEnrollmentLedger(t, root)
				state, e := servicemutationledger.MailEnrollmentState(&ledger, enrollmentLedgerID())
				if e != nil {
					t.Fatal(e)
				}
				terminal := servicemutationledger.MailEnrollmentPublished
				if direction == "rollback" {
					terminal = servicemutationledger.MailEnrollmentRestored
				}
				if point == "terminal-after_rename_before_directory_sync" || point == "terminal-after_directory_sync" {
					if state != terminal || ledger.ActiveRequestID != "" {
						t.Fatal("terminal publication lost")
					}
				} else if state != direction || ledger.ActiveRequestID != enrollmentLedgerID().RequestID {
					t.Fatal("early reservation release")
				}
				runEnrollmentExecuteChild(t, root, direction, "", host, release)
				runEnrollmentExecuteChild(t, root, direction, "terminal-retry", host, release)
			})
		}
	}
}
func TestMailEnrollmentExecutePreservesUnknownAndTerminalOwnership(t *testing.T) {
	for _, scenario := range []string{"native-failure", "proof-unknown", "wrong-identity", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			root, host, release := enrollmentLedgerFixture(t)
			runEnrollmentExecuteChild(t, root, "forward", scenario, host, release)
		})
	}
	root, host, release := enrollmentLedgerFixture(t)
	runEnrollmentExecuteChild(t, root, "forward", "", host, release)
	runEnrollmentExecuteChild(t, root, "rollback", "terminal-opposite", host, release)
	runEnrollmentExecuteChild(t, root, "forward", "proof-unknown", host, release)
}
func TestMailEnrollmentExecuteChild(t *testing.T) {
	root := os.Getenv("CP_ENROLL_EXEC_ROOT")
	if root == "" {
		t.Skip("subprocess only")
	}
	serviceMutationRequiredOwnerUID, serviceMutationRequiredOwnerGID = 0, 0
	direction, scenario := os.Getenv("CP_ENROLL_EXEC_DIRECTION"), os.Getenv("CP_ENROLL_EXEC_CASE")
	proof, err := recoveryruntime.InspectCompatibleMailAgent(filepath.Join(root, "bin"))
	if err != nil {
		t.Fatal(err)
	}
	defer proof.Close()
	id := enrollmentLedgerID()
	authority := mailEnrollmentAuthority{identity: id, agent: proof, verifyIntent: func() error {
		raw, e := os.ReadFile(filepath.Join(root, "accepted"))
		if e != nil || string(raw) != id.ScopeSHA256 {
			return errors.New("owner intent changed")
		}
		return nil
	}}
	fixture := &enrollmentExecutionFixture{t: t, root: root, scenario: scenario, id: id}
	if scenario == "wrong-identity" {
		fixture.id.OwnerID = strings.Repeat("c", 32)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if scenario == "cancelled" {
		cancel()
	}
	verifyRelease := func() error {
		return hostmutationlock.VerifyInherited(filepath.Join(root, "transaction.lock"), 9, hostmutationlock.Owner{})
	}
	fault := func(point string) error {
		if raw, e := os.ReadFile(filepath.Join(root, "native-result")); e == nil && string(raw) == direction {
			fixture.cut("terminal-" + point)
		}
		return nil
	}
	before := readEnrollmentLedger(t, root)
	err = executeMailEnrollmentAt(ctx, filepath.Join(root, "state"), filepath.Join(root, "mutation.lock"), authority, fixture, direction, verifyRelease, fault)
	after := readEnrollmentLedger(t, root)
	switch scenario {
	case "native-failure", "proof-unknown":
		if err == nil {
			t.Fatal("unknown result acknowledged")
		}
		if before.ActiveRequestID == "" && before.Jobs[id.RequestID] != nil {
			if fixture.resumes != 0 || after.ActiveRequestID != "" {
				t.Fatal("terminal observer resumed native work")
			}
		} else if after.ActiveRequestID != id.RequestID {
			t.Fatal("unknown result released reservation")
		}
	case "wrong-identity", "cancelled", "terminal-opposite":
		if err == nil || fixture.resumes != 0 {
			t.Fatal("unauthorized execution")
		}
		old, _ := servicemutationledger.Encode(&before)
		now, _ := servicemutationledger.Encode(&after)
		if string(old) != string(now) {
			t.Fatal("refusal changed ledger")
		}
	default:
		if err != nil {
			t.Fatal(err)
		}
		if after.ActiveRequestID != "" {
			t.Fatal("verified operation not acknowledged")
		}
		if scenario == "terminal-retry" && fixture.resumes != 0 {
			t.Fatal("terminal retry replayed mutation")
		}
	}
}
