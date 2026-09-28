//go:build linux

package dnspeerenrollowner

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/binddns"
	"golang.org/x/crypto/ssh"
)

func fixtureOptions(t *testing.T) SecondaryOptions {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("trusted source fixture requires root")
	}
	root, err := os.MkdirTemp("/root", "bind-peer-enroll-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	publicPath := filepath.Join(root, "primary.pub")
	if err := os.WriteFile(publicPath, ssh.MarshalAuthorizedKey(pub), 0600); err != nil {
		t.Fatal(err)
	}
	binaryPath := filepath.Join(root, "inspector")
	if err := os.WriteFile(binaryPath, append([]byte{0x7f, 'E', 'L', 'F'}, bytes.Repeat([]byte{'x'}, 32)...), 0700); err != nil {
		t.Fatal(err)
	}
	catalog, err := binddns.CatalogDomain("192.0.2.10")
	if err != nil {
		t.Fatal(err)
	}
	return SecondaryOptions{PrimaryIP: "192.0.2.10", PeerIP: "192.0.2.11", CatalogName: catalog, PrimaryPublicKeyPath: publicPath, InspectorBinaryPath: binaryPath}
}

func TestValidateOptionsRejectsChangedAuthorityAndUnsafeSources(t *testing.T) {
	o := fixtureOptions(t)
	if _, _, err := validateOptions(o); err != nil {
		t.Fatalf("valid fixture: %v", err)
	}
	bad := o
	bad.CatalogName = "catalog-deadbeef.celikpanel.invalid"
	if _, _, err := validateOptions(bad); err == nil {
		t.Fatal("wrong catalog accepted")
	}
	bad = o
	bad.PeerIP = o.PrimaryIP
	if _, _, err := validateOptions(bad); err == nil {
		t.Fatal("same peer and primary accepted")
	}
	bad = o
	bad.PeerIP = net.ParseIP("2001:db8::1").String()
	if _, _, err := validateOptions(bad); err == nil {
		t.Fatal("IPv6 peer accepted")
	}
	if err := os.Chmod(o.PrimaryPublicKeyPath, 0666); err != nil {
		t.Fatal(err)
	}
	if _, _, err := validateOptions(o); err == nil {
		t.Fatal("writable public key accepted")
	}
	if err := os.Chmod(o.PrimaryPublicKeyPath, 0600); err != nil {
		t.Fatal(err)
	}
	bad = o
	bad.PrimaryPublicKeyPath = o.PrimaryPublicKeyPath + ".link"
	if err := os.Symlink(o.PrimaryPublicKeyPath, bad.PrimaryPublicKeyPath); err != nil {
		t.Fatal(err)
	}
	if _, _, err := validateOptions(bad); err == nil {
		t.Fatal("symlinked key accepted")
	}
}

func TestValidateOptionsRejectsKeyOptionsAndNonELF(t *testing.T) {
	o := fixtureOptions(t)
	raw, err := os.ReadFile(o.PrimaryPublicKeyPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(o.PrimaryPublicKeyPath, append([]byte("command=\"/bin/sh\" "), raw...), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := validateOptions(o); err == nil {
		t.Fatal("prequalified authorized key accepted")
	}
	if err := os.WriteFile(o.PrimaryPublicKeyPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(o.InspectorBinaryPath, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := validateOptions(o); err == nil {
		t.Fatal("script accepted as inspector binary")
	}
}

func TestRenderedChannelIsBoundedAndReadOnly(t *testing.T) {
	o := fixtureOptions(t)
	key, _, err := validateOptions(o)
	if err != nil {
		t.Fatal(err)
	}
	policy, wrapper, sudoers, sshd, authorized := renderFiles(o, key)
	if !bytes.Contains(policy, []byte(o.CatalogName)) || !bytes.Contains(policy, []byte(o.PrimaryIP)) {
		t.Fatal("policy lost exact authority")
	}
	if !bytes.Contains(wrapper, []byte("SSH_ORIGINAL_COMMAND")) || !bytes.Contains(wrapper, []byte("sudo -n -- "+InspectorPath)) {
		t.Fatal("wrapper lost exact command guard")
	}
	if !bytes.Contains(sudoers, []byte(InspectorPath+" \"\"")) {
		t.Fatal("sudoers permits arguments")
	}
	for _, want := range []string{"Match User " + Account, "PasswordAuthentication no", "AuthorizedKeysCommand /usr/bin/false", "AuthorizedKeysCommandUser nobody", "DisableForwarding yes", "PermitTTY no", "ForceCommand " + WrapperPath} {
		if !strings.Contains(string(sshd), want) {
			t.Fatalf("missing sshd restriction: %s", want)
		}
	}
	if !bytes.HasPrefix(authorized, []byte("restrict,from=\""+o.PrimaryIP+"\",command=\""+WrapperPath+"\" ")) {
		t.Fatal("authorized key is not address and command restricted")
	}
	if _, err := enrolledPrimaryKey(authorized, o.PrimaryIP); err != nil {
		t.Fatalf("rendered key rejected: %v", err)
	}
	if _, err := enrolledPrimaryKey(authorized, "192.0.2.12"); err == nil {
		t.Fatal("wrong primary accepted")
	}
	if _, err := enrolledPrimaryKey(bytes.Replace(authorized, []byte("restrict,"), nil, 1), o.PrimaryIP); err == nil {
		t.Fatal("unrestricted key accepted")
	}
}

func TestExactStagingResumeRejectsDrift(t *testing.T) {
	o := fixtureOptions(t)
	name := filepath.Join(filepath.Dir(o.PrimaryPublicKeyPath), "managed.conf")
	want := []byte("exact staged policy")
	if err := writeExactOrCreate(name, want, 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeExactOrCreate(name, want, 0600); err != nil {
		t.Fatalf("same-input retry: %v", err)
	}
	if err := writeExactOrCreate(name, []byte("different"), 0600); err == nil {
		t.Fatal("changed bytes accepted")
	}
	if err := os.Chmod(name, 0644); err != nil {
		t.Fatal(err)
	}
	if err := writeExactOrCreate(name, want, 0600); err == nil {
		t.Fatal("changed mode accepted")
	}
	if err := os.Remove(name); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(o.PrimaryPublicKeyPath, name); err != nil {
		t.Fatal(err)
	}
	if err := writeExactOrCreate(name, want, 0600); err == nil {
		t.Fatal("symlink accepted")
	}
}

func TestPublicAuthorizedKeyModeIsExact(t *testing.T) {
	o := fixtureOptions(t)
	name := filepath.Join(filepath.Dir(o.PrimaryPublicKeyPath), "authorized_keys")
	if err := writeExclusive(name, []byte("public key fixture"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := readManagedFile(name, 4096, 0644); err != nil {
		t.Fatalf("safe public mode rejected: %v", err)
	}
	if err := os.Chmod(name, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readManagedFile(name, 4096, 0644); err == nil {
		t.Fatal("unreadable key mode accepted")
	}
	if err := os.Chmod(name, 0666); err != nil {
		t.Fatal(err)
	}
	if _, err := readManagedFile(name, 4096, 0644); err == nil {
		t.Fatal("writable key mode accepted")
	}
}
