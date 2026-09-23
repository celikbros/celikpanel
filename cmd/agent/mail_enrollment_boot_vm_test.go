//go:build linux

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/recoveryruntime"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
	"golang.org/x/sys/unix"
)

// This test-only producer cuts after a real native reload, before its receipt.
// Following reboot, only the installed production helper/native boot unit runs;
// neither this test executable nor its fault hook is part of that continuation.
func TestMailEnrollmentBootDisposableVM(t *testing.T) {
	if os.Getenv("CELIKPANEL_DISPOSABLE_MAIL_VM") != "arch-20260923-boot" {
		t.Skip("guarded Arch fixture only")
	}
	if os.Geteuid() != 0 {
		t.Fatal("root fixture required")
	}
	requireMailEnrollmentVMIdentity(t)
	if _, err := os.Lstat("/opt/celikpanel/bin/panel"); !os.IsNotExist(err) {
		t.Fatal("panel must be absent")
	}
	request, owner, target := os.Getenv("CP_MAIL_BOOT_REQUEST"), os.Getenv("CP_MAIL_BOOT_OWNER"), os.Getenv("CP_MAIL_BOOT_TARGET")
	if !validMutationIdentity(request) || !validMutationIdentity(owner) || !recoveryruntime.ValidDigest(target) {
		t.Fatal("exact fixture authority required")
	}
	intent := []byte("operation=" + request + "\nowner=" + owner + "\ntarget=" + target + "\n")
	intentPath := "/root/celikpanel-release-recovery-lab/mail-boot-automatic.intent"
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
	binding := recoveryruntime.MailEnrollmentBinding{LedgerPath: filepath.Join(state, serviceMutationLedgerFileName), Owner: servicemutationledger.FileOwner{GID: uint32(gid)}, OwnerID: owner, HostLock: host, HostOwner: serviceMutationLockOwner(), VerifyAuthority: verify, Native: mailBootCutNative{t: t}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err = recoveryruntime.PrepareMailEnrollmentJournal(9); err != nil {
		t.Fatal(err)
	}
	execution, _, err := recoveryruntime.PrepareMailEnrollment(ctx, request, mailEnrollmentJournalRoot, proof, binding)
	if err != nil {
		t.Fatal(err)
	}
	if err = execution.ArmBoot(ctx); err != nil {
		t.Fatal(err)
	}
	authority := mailEnrollmentAuthority{identity: execution.Identity(), agent: proof, verifyIntent: func() error { return execution.RevalidateAuthority(ctx) }}
	if err = executePreparedMailEnrollment(ctx, state, host, authority, execution, "forward"); err != nil {
		t.Fatal(err)
	}
	t.Fatal("native cut was not reached")
}

type mailBootCutNative struct {
	mailEnrollmentNativeHost
	t *testing.T
}

func (n mailBootCutNative) Reload(ctx context.Context) error {
	if err := n.mailEnrollmentNativeHost.Reload(ctx); err != nil {
		return err
	}
	n.t.Log("boot_enrollment_cut=first_native_reload_returned")
	_ = unix.Kill(os.Getpid(), unix.SIGKILL)
	panic("kill returned")
}
