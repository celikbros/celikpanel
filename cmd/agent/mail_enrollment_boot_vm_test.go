//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/mailrenewalkit"
	"github.com/alicelik/celikpanel/internal/recoveryruntime"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
	"golang.org/x/sys/unix"
)

// This test-only producer cuts after a real native timer start, before its receipt.
// Following reboot, only the installed production helper/native boot unit runs;
// neither this test executable nor its fault hook is part of that continuation.
func TestMailEnrollmentBootDisposableVM(t *testing.T) {
	runMailEnrollmentBootDisposableVM(t, false, false)
}
func TestMailEnrollmentRollbackBootDisposableVM(t *testing.T) {
	runMailEnrollmentBootDisposableVM(t, true, false)
}
func TestMailEnrollmentPreAdmissionBootDisposableVM(t *testing.T) {
	runMailEnrollmentBootDisposableVM(t, false, true)
}
func runMailEnrollmentBootDisposableVM(t *testing.T, rollback, beforeAdmission bool) {
	node := ""
	switch os.Getenv("CELIKPANEL_DISPOSABLE_MAIL_VM") {
	case "arch-20260923-boot":
		node = "arch"
	case "debian13-20260923-boot":
		node = "debian13"
	default:
		t.Skip("guarded Debian/Arch fixture only")
	}
	if os.Geteuid() != 0 {
		t.Fatal("root fixture required")
	}
	requireMailEnrollmentVMIdentityFor(t, node)
	if _, err := os.Lstat("/opt/celikpanel/bin/panel"); !os.IsNotExist(err) {
		t.Fatal("panel must be absent")
	}
	request, owner, target := os.Getenv("CP_MAIL_BOOT_REQUEST"), os.Getenv("CP_MAIL_BOOT_OWNER"), os.Getenv("CP_MAIL_BOOT_TARGET")
	if !validMutationIdentity(request) || !validMutationIdentity(owner) || !recoveryruntime.ValidDigest(target) {
		t.Fatal("exact fixture authority required")
	}
	intent := []byte("operation=" + request + "\nowner=" + owner + "\ntarget=" + target + "\n")
	intentPath := "/root/celikpanel-release-recovery-lab/mail-boot-automatic.intent"
	if rollback {
		intentPath = "/root/celikpanel-release-recovery-lab/mail-boot-rollback-automatic.intent"
	}
	if beforeAdmission {
		intentPath = "/root/celikpanel-release-recovery-lab/mail-boot-preadmission.intent"
	}
	legacy := os.Getenv("CP_MAIL_BOOT_LEGACY") == "1"
	if legacy {
		if beforeAdmission {
			t.Fatal("legacy fixture requires native publication")
		}
		intentPath = "/root/celikpanel-release-recovery-lab/mail-legacy-boot-automatic.intent"
		if rollback {
			intentPath = "/root/celikpanel-release-recovery-lab/mail-legacy-inverse-boot-automatic.intent"
		}
		raw, err := os.ReadFile(recoveryruntime.MailRenewalHookPath)
		if err != nil || !bytes.Equal(raw, mailrenewalkit.LegacyHook()) {
			t.Fatal("exact legacy producer hook required", err)
		}
	}
	gid, ok := lookupGroupID("celikpanel")
	if !ok || gid < 0 {
		t.Fatal("retained owner group missing")
	}
	serviceMutationRequiredOwnerUID, serviceMutationRequiredOwnerGID = 0, uint32(gid)
	proof, err := recoveryruntime.InspectCompatibleMailAgent("/opt/celikpanel/bin")
	if err != nil {
		t.Fatal(err)
	}
	defer proof.Close()
	if proof.Contract.MailRenewalGeneration != target {
		t.Fatal("wrong fixture generation")
	}
	verify := func() error {
		info, err := os.Lstat(intentPath)
		if err != nil {
			return err
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || st.Mode != unix.S_IFREG|0600 || st.Uid != 0 || st.Gid != 0 || st.Nlink != 1 {
			t.Fatal("unsafe fixture intent")
		}
		raw, err := os.ReadFile(intentPath)
		if err != nil {
			return err
		}
		if !bytes.Equal(raw, intent) {
			t.Fatal("fixture intent changed")
		}
		return proof.Revalidate()
	}
	const host = "/run/celikpanel/service-mutation.lock"
	state := "/var/lib/celikpanel-agent-private"
	native := &mailBootCutNative{t: t, rollback: rollback}
	binding := recoveryruntime.MailEnrollmentBinding{LedgerPath: filepath.Join(state, serviceMutationLedgerFileName), Owner: servicemutationledger.FileOwner{GID: uint32(gid)}, OwnerID: owner, HostLock: host, HostOwner: serviceMutationLockOwner(), VerifyAuthority: verify, Native: native}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	recordedInverse := os.Getenv("CP_MAIL_BOOT_RECORDED_INVERSE") == "1"
	if recordedInverse && !rollback {
		t.Fatal("recorded inverse requires inverse fixture")
	}
	var execution *recoveryruntime.PreparedMailEnrollment
	if recordedInverse {
		// Exact already accepted scope. The executor verifies its common-ledger hash
		// before persisting this explicit test-fixture inverse decision.
		var raw []byte
		raw, err = os.ReadFile(filepath.Join(mailEnrollmentJournalRoot, request+".enrollment.json"))
		if err == nil {
			execution, err = recoveryruntime.OpenPreparedMailEnrollment(ctx, raw, mailEnrollmentJournalRoot, binding)
		}
	} else {
		if err = recoveryruntime.PrepareMailEnrollmentJournal(9); err != nil {
			t.Fatal(err)
		}
		execution, _, err = recoveryruntime.PrepareMailEnrollment(ctx, request, mailEnrollmentJournalRoot, proof, binding)
	}
	if err != nil {
		t.Fatal(err)
	}
	if err = execution.ArmBoot(ctx); err != nil {
		t.Fatal(err)
	}
	if beforeAdmission {
		t.Log("boot_enrollment_cut=boot_armed_before_common_admission")
		_ = unix.Kill(os.Getpid(), unix.SIGKILL)
		panic("kill returned")
	}
	authority := mailEnrollmentAuthority{identity: execution.Identity(), agent: proof, verifyIntent: func() error { return execution.RevalidateAuthority(ctx) }}
	if !recordedInverse {
		err = executePreparedMailEnrollment(ctx, state, host, authority, execution, "forward")
	}
	if rollback {
		if !recordedInverse && (err == nil || !native.forwardInterrupted) {
			t.Fatal("expected fixture forward interruption", err)
		}
		// Explicit test-fixture inverse intent. Production boot must consume this
		// durable direction without initiating or selecting a new rollback itself.
		err = executePreparedMailEnrollment(ctx, state, host, authority, execution, "rollback")
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Fatal("native cut was not reached")
}

type mailBootCutNative struct {
	mailEnrollmentNativeHost
	t                  *testing.T
	rollback           bool
	forwardInterrupted bool
}

func (n *mailBootCutNative) StartTimer(ctx context.Context) error {
	if err := n.mailEnrollmentNativeHost.StartTimer(ctx); err != nil {
		return err
	}
	if n.rollback {
		n.forwardInterrupted = true
		n.t.Log("boot_enrollment_fixture=forward_reply_interrupted")
		return errMailBootFixtureForwardCut
	}
	n.t.Log("boot_enrollment_cut=native_timer_start_returned")
	_ = unix.Kill(os.Getpid(), unix.SIGKILL)
	panic("kill returned")
}

var errMailBootFixtureForwardCut = errors.New("test fixture interrupted after actual native timer start")

func (n *mailBootCutNative) StopTimer(ctx context.Context) error {
	if err := n.mailEnrollmentNativeHost.StopTimer(ctx); err != nil {
		return err
	}
	if n.rollback {
		n.t.Log("boot_enrollment_cut=native_timer_stop_returned")
		_ = unix.Kill(os.Getpid(), unix.SIGKILL)
		panic("kill returned")
	}
	return nil
}
