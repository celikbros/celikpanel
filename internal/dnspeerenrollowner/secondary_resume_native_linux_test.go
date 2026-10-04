//go:build linux && celikpanel_dns_owner_native

package dnspeerenrollowner

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"

	"github.com/alicelik/celikpanel/internal/bindpeerinspector"
)

// This explicitly selected test stages an interrupted enrollment only on the
// exact disposable Debian/Arch fixture. It is never included in a shipped executable.
func TestNativeOwnerEnrollmentResume(t *testing.T) {
	const cell = "pdns-switch__intent__after-write__paired-primary__peer-reachable"
	if os.Getenv("CELIKPANEL_OWNER_RESUME_NATIVE") != cell {
		t.Skip("explicit disposable owner resume trial only")
	}
	node := os.Getenv("CELIKPANEL_OWNER_RESUME_NODE")
	if node == "" {
		node = "arch"
	}
	if node != "arch" && node != "debian13" {
		t.Fatal("unsupported disposable node")
	}
	marker, err := readManagedFile("/etc/celikpanel-dns-kill-matrix", 1024, 0444)
	if err != nil || string(marker) != "schema=celikpanel/dns-kill-fixture-plan/v1\ncell_id="+cell+"\nnode="+node+"\n" {
		t.Fatal("not the exact root-owned disposable fixture")
	}
	for _, path := range []string{PolicyPath, SSHDBackupPath, SSHDReceiptPath, AuthorizedKeysPath} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("fresh fixture required: %s", path)
		}
	}
	o := SecondaryOptions{PrimaryIP: "192.0.2.10", PeerIP: "192.0.2.11", CatalogName: "catalog-c000020a.celikpanel.invalid", PrimaryPublicKeyPath: "/root/primary.pub", InspectorBinaryPath: "/root/bind-peer-inspect"}
	if node == "debian13" {
		o.PrimaryIP, o.PeerIP = "192.0.2.11", "192.0.2.10"
		o.CatalogName = "catalog-c000020b.celikpanel.invalid"
	}
	key, bin, err := validateOptions(o)
	if err != nil {
		t.Fatal(err)
	}
	policy, wrapper, sudoers, sshd, _ := renderFiles(o, key)
	for _, dir := range []struct {
		path string
		mode os.FileMode
	}{{"/etc/bind-peer-inspector", 0750}, {"/usr/local/libexec", 0755}} {
		if err := ensureDir(dir.path, dir.mode); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []struct {
		path string
		data []byte
		mode os.FileMode
	}{{InspectorPath, bin, 0755}, {WrapperPath, wrapper, 0755}, {PolicyPath, policy, 0600}, {SudoersPath, sudoers, 0440}, {SSHDConfigPath, sshd, 0644}} {
		if err := writeExclusive(f.path, f.data, f.mode); err != nil {
			t.Fatal(err)
		}
	}
	original, err := readTrustedFile(SSHDMainPath, 1<<20, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeExclusive(SSHDBackupPath, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := installSSHDInclude(o.PrimaryIP, true); err != nil {
		t.Fatal(err)
	}
	t.Log("backup-only checkpoint resumed; SSH publication verified before account creation")
	if out, err := exec.Command("/usr/bin/getent", "passwd", Account).CombinedOutput(); err == nil || len(out) != 0 {
		t.Fatal("account existed before resume")
	}
	if err := ResumeSecondary(o); err != nil {
		t.Fatal(err)
	}
	status, err := SecondaryStatus()
	if err != nil || status.State != "configured" {
		t.Fatalf("resumed status=%+v err=%v", status, err)
	}
	firstKey, err := readManagedFile(AuthorizedKeysPath, 4096, 0644)
	if err != nil {
		t.Fatal(err)
	}
	if err := ResumeSecondary(o); err != nil {
		t.Fatalf("completed repeat: %v", err)
	}
	again, _ := readManagedFile(AuthorizedKeysPath, 4096, 0644)
	if !bytes.Equal(firstKey, again) {
		t.Fatal("repeat replaced key")
	}
	t.Log("published-SSH checkpoint completed with a new restricted account; completed repeat preserved key")
	if err := RevokeSecondary(); err != nil {
		t.Fatal(err)
	}
	if err := ResumeSecondary(o); err != nil {
		t.Fatalf("explicit reauthorization: %v", err)
	}
	t.Log("explicit resume reused the locked account after revocation")
	active, err := readTrustedFile(SSHDMainPath, 1<<20, false)
	if err != nil {
		t.Fatal(err)
	}
	edited := append(append([]byte(nil), active...), []byte("# disposable owner edit must survive\n")...)
	if err := os.WriteFile(SSHDMainPath, edited, 0600); err != nil {
		t.Fatal(err)
	}
	if err := ResumeSecondary(o); err == nil {
		t.Fatal("owner edit accepted")
	}
	preserved, _ := readTrustedFile(SSHDMainPath, 1<<20, false)
	if !bytes.Equal(preserved, edited) {
		t.Fatal("owner edit overwritten")
	}
	_, _, err = (bindpeerinspector.OwnerPolicyReader{}).Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := RevokeSecondary(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(AuthorizedKeysPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("trial key still authorized")
	}
	if out, err := run("systemctl", "is-active", "named.service"); err != nil || string(out) != "active\n" {
		t.Fatal("native BIND did not survive")
	}
	t.Log("owner edit refused and preserved; key revoked; native BIND remains active")
}
