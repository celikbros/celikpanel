//go:build linux

package main

import (
	"context"
	"github.com/alicelik/celikpanel/internal/recoveryruntime"
	"os"
	"testing"
	"time"
)

func TestMailRenewalDisposableVMHookPreservation(t *testing.T) {
	requireDisposableMailVM(t)
	if _, err := os.Stat("/opt/celikpanel/bin/agent"); !os.IsNotExist(err) {
		t.Fatal("installed management must be absent")
	}
	h, err := recoveryruntime.InspectMailRenewalHook()
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if h.Mode != recoveryruntime.MailRenewalHookIndependent {
		t.Fatal("independent fixture required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if !independentMailRenewalScheduleReady(ctx) {
		t.Fatal("native schedule not ready")
	}
	if err = writeMailHostCertificateDeployHook(); err != nil {
		t.Fatal(err)
	}
	if err = h.Revalidate(); err != nil {
		t.Fatal("certificate writer changed enrolled hook", err)
	}
	t.Log("independent_hook_preserved generation=" + h.Generation + " loaded_schedule=verified")
}
func TestMailRenewalDisposableVMOwnerHookRefusal(t *testing.T) {
	requireDisposableMailVM(t)
	before, err := os.ReadFile(recoveryruntime.MailRenewalHookPath)
	if err != nil {
		t.Fatal(err)
	}
	const marker = "# disposable owner hook edit\n"
	if len(before) < len(marker) || string(before[len(before)-len(marker):]) != marker {
		t.Fatal("explicit fixture owner edit required")
	}
	info, err := os.Stat(recoveryruntime.MailRenewalHookPath)
	if err != nil {
		t.Fatal(err)
	}
	if err = writeMailHostCertificateDeployHook(); err == nil {
		t.Fatal("unknown owner hook accepted")
	}
	after, err := os.ReadFile(recoveryruntime.MailRenewalHookPath)
	if err != nil {
		t.Fatal(err)
	}
	current, err := os.Stat(recoveryruntime.MailRenewalHookPath)
	if err != nil || string(before) != string(after) || !os.SameFile(info, current) || !info.ModTime().Equal(current.ModTime()) {
		t.Fatal("owner hook was changed")
	}
	t.Log("owner_hook_refused bytes_inode_mtime=preserved")
}
