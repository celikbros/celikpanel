//go:build linux

package main

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

	"github.com/alicelik/celikpanel/internal/recoveryruntime"
	"golang.org/x/sys/unix"
)

// Only this guarded native fixture can pause the real adapter after an actual
// systemd action. The production writer, executor and native adapter are used;
// owner admission and the initial empty test ledger remain fixture authority.
func TestMailEnrollmentExecuteDisposableVM(t *testing.T) {
	if os.Getenv("CELIKPANEL_DISPOSABLE_MAIL_VM") != "arch-20260923-joined" {
		t.Skip("guarded Arch fixture only")
	}
	if os.Geteuid() != 0 {
		t.Fatal("root fixture required")
	}
	for _, name := range []string{"agent", "panel"} {
		if _, e := os.Lstat(filepath.Join("/opt/celikpanel/bin", name)); !os.IsNotExist(e) {
			t.Fatal("management must be absent")
		}
	}
	requireMailEnrollmentVMIdentity(t)
	operation, target, phase := os.Getenv("CP_MAIL_ENROLL_NATIVE_OPERATION"), os.Getenv("CP_MAIL_ENROLL_NATIVE_TARGET"), os.Getenv("CP_MAIL_ENROLL_NATIVE_PHASE")
	if !validMutationIdentity(operation) || !recoveryruntime.ValidDigest(target) {
		t.Fatal("exact operation and target required")
	}
	direction := "rollback"
	switch phase {
	case "forward-cut":
		direction = "forward"
	case "rollback-cut", "rollback-verify", "terminal-verify":
	default:
		t.Fatal("explicit native phase required")
	}
	serviceMutationRequiredOwnerUID, serviceMutationRequiredOwnerGID = 0, 0
	const root = "/root/celikpanel-release-recovery-lab"
	journal := filepath.Join(root, "mail-joined-enrollment-journal")
	scopePath := filepath.Join(root, "mail-joined.scope.json")
	scopeRaw, found, e := readSecureServiceMutationLedger(scopePath, 2048)
	if e != nil || !found {
		t.Fatal("prepared scope missing", e)
	}
	proof, e := recoveryruntime.InspectCompatibleMailAgent(filepath.Join(root, "mail-joined-agent-bin"))
	if e != nil {
		t.Fatal(e)
	}
	defer proof.Close()
	verify := func() error {
		raw, found, err := readSecureServiceMutationLedger(filepath.Join(root, "mail-joined-enrollment.intent"), 512)
		if err != nil || !found || !bytes.Equal(raw, []byte("operation="+operation+"\ntarget="+target+"\n")) {
			return errors.New("fixture owner intent changed")
		}
		now, found, err := readSecureServiceMutationLedger(scopePath, 2048)
		if err != nil || !found || !bytes.Equal(now, scopeRaw) {
			return errors.New("prepared scope changed")
		}
		return proof.Revalidate()
	}
	native := &mailEnrollmentCutNative{t: t, phase: phase}
	state := filepath.Join(root, "mail-joined-ledger")
	const host = "/run/celikpanel/service-mutation.lock"
	binding := recoveryruntime.MailEnrollmentBinding{LedgerPath: filepath.Join(state, "service-mutations.json"), OwnerID: strings.Repeat("b", 32), HostLock: host, VerifyAuthority: verify, Native: native}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	execution, e := recoveryruntime.OpenPreparedMailEnrollment(ctx, scopeRaw, journal, binding)
	if e != nil {
		t.Fatal(e)
	}
	authority := mailEnrollmentAuthority{identity: execution.Identity(), agent: proof, verifyIntent: verify}
	if e = executePreparedMailEnrollment(ctx, state, host, authority, execution, direction); e != nil {
		t.Fatal(e)
	}
	if strings.HasSuffix(phase, "-cut") {
		t.Fatal("actual native cut not reached")
	}
	if phase == "terminal-verify" && native.mutations != 0 {
		t.Fatal("terminal retry mutated native state")
	}
	t.Logf("joined_native_enrollment_verified=%s operation=%s scope=%s mutations=%d", phase, operation, execution.Identity().ScopeSHA256, native.mutations)
}

type mailEnrollmentCutNative struct {
	mailEnrollmentNativeHost
	t         *testing.T
	phase     string
	mutations int
}

func (n *mailEnrollmentCutNative) Reload(ctx context.Context) error {
	n.mutations++
	return n.mailEnrollmentNativeHost.Reload(ctx)
}
func (n *mailEnrollmentCutNative) StartTimer(ctx context.Context) error {
	n.mutations++
	if e := n.mailEnrollmentNativeHost.StartTimer(ctx); e != nil {
		return e
	}
	if n.phase == "forward-cut" {
		n.t.Log("joined_native_enrollment_cut=start-returned")
		unix.Kill(os.Getpid(), syscall.SIGKILL)
		panic("kill returned")
	}
	return nil
}
func (n *mailEnrollmentCutNative) StopTimer(ctx context.Context) error {
	n.mutations++
	if e := n.mailEnrollmentNativeHost.StopTimer(ctx); e != nil {
		return e
	}
	if n.phase == "rollback-cut" {
		n.t.Log("joined_native_enrollment_cut=stop-returned")
		unix.Kill(os.Getpid(), syscall.SIGKILL)
		panic("kill returned")
	}
	return nil
}

func requireMailEnrollmentVMIdentity(t *testing.T) {
	t.Helper()
	requireMailEnrollmentVMIdentityFor(t, "arch")
}
func requireMailEnrollmentVMIdentityFor(t *testing.T, node string) {
	t.Helper()
	if node != "arch" && node != "debian13" {
		t.Fatal("unsupported fixture node")
	}
	const markerPath = "/etc/celikpanel-release-recovery-lab"
	var st unix.Stat_t
	if unix.Lstat(markerPath, &st) != nil || st.Mode != unix.S_IFREG|0444 || st.Uid != 0 || st.Gid != 0 || st.Nlink != 1 || st.Size > 2048 {
		t.Fatal("protected marker required")
	}
	raw, e := os.ReadFile(markerPath)
	if e != nil {
		t.Fatal(e)
	}
	var marker map[string]string
	if json.Unmarshal(raw, &marker) != nil || marker["schema"] != "celikpanel-release-recovery-lab/v1" || marker["node"] != node {
		t.Fatal("wrong fixture")
	}
	uuid, e := os.ReadFile("/sys/class/dmi/id/product_uuid")
	if e != nil || strings.ToLower(strings.TrimSpace(string(uuid))) != marker["vm_uuid"] {
		t.Fatal("wrong VM identity")
	}
	virt, e := exec.Command("systemd-detect-virt", "--vm").Output()
	if e != nil || (strings.TrimSpace(string(virt)) != "qemu" && strings.TrimSpace(string(virt)) != "kvm") {
		t.Fatal("QEMU required")
	}
}
