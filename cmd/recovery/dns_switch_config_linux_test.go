//go:build linux

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alicelik/celikpanel/internal/bindconfig"
	"github.com/alicelik/celikpanel/internal/bindroot"
	"golang.org/x/sys/unix"
)

func TestObserveBINDConfigAtRequiresExactActiveInclude(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-owned native config fixture requires root")
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "etc/bind"), 0o755); err != nil {
		t.Fatal(err)
	}
	options := filepath.Join(root, "etc/bind/named.conf.options")
	anchor := filepath.Join(root, "etc/bind/named.conf.local")
	if err := os.WriteFile(options, []byte("options {};\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	include, err := bindconfig.ManagedZoneInclude("// owner config\n", "/var/cache/bind/celikpanel/current/zones.conf")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(anchor, []byte(include), 0o644); err != nil {
		t.Fatal(err)
	}
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	if _, err := observeBINDConfigAt(fd, bindroot.APT, 12345); err != nil {
		t.Fatalf("exact managed include rejected: %v", err)
	}
	for _, changed := range []string{
		"// owner config\n",
		"/*\n" + include + "*/\n",
		"// BEGIN CELIKPANEL MANAGED BIND ZONES\ninclude \"/tmp/other\";\n// END CELIKPANEL MANAGED BIND ZONES\n",
	} {
		if err := os.WriteFile(anchor, []byte(changed), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := observeBINDConfigAt(fd, bindroot.APT, 12345); err == nil {
			t.Fatalf("missing or inert managed include accepted: %q", changed)
		}
	}
	if err := os.WriteFile(anchor, []byte(include), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(anchor, 0, 12345); err != nil {
		t.Fatal(err)
	}
	if _, err := observeBINDConfigAt(fd, bindroot.APT, 12345); err == nil {
		t.Fatal("different APT config owners accepted")
	}
}
