//go:build linux

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDNSSwitchStatusRequiresOwnerAndExactCommand(t *testing.T) {
	var out, diagnostic bytes.Buffer
	if got := runDNSSwitchStatus([]string{"dns-switch-status", "extra"}, 0, &out, &diagnostic); got != exitUsage {
		t.Fatalf("unexpected argument was accepted: %d", got)
	}
	if got := runDNSSwitchStatus([]string{"dns-switch-status"}, 1000, &out, &diagnostic); got != exitNotOwner {
		t.Fatalf("unprivileged observation was accepted: %d", got)
	}
	if !strings.Contains(diagnostic.String(), "Owner authentication") || out.Len() != 0 {
		t.Fatalf("unexpected guidance or data disclosure: %q / %q", diagnostic.String(), out.String())
	}
}
func TestLocalCelikPanelGroupIsBoundedAndUnambiguous(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "group")
	write := func(data string, mode os.FileMode) {
		t.Helper()
		if err := os.WriteFile(path, []byte(data), mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
	}
	write("root:x:0:\ncelikpanel:x:975:\n", 0o644)
	if got, err := localCelikPanelGroupID(path); err != nil || got != 975 {
		t.Fatalf("local identity: %d, %v", got, err)
	}
	for _, data := range []string{
		"root:x:0:\n",
		"celikpanel:x:0:\n",
		"celikpanel:x:not-a-number:\n",
		"celikpanel:x:975:\ncelikpanel:x:976:\n",
	} {
		write(data, 0o644)
		if _, err := localCelikPanelGroupID(path); err == nil {
			t.Fatalf("ambiguous or missing group accepted: %q", data)
		}
	}
	write("celikpanel:x:975:\n", 0o666)
	if _, err := localCelikPanelGroupID(path); err == nil {
		t.Fatal("writable group file accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("celikpanel:x:975:\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, err := localCelikPanelGroupID(path); err == nil {
		t.Fatal("symlink group file accepted")
	}
}
