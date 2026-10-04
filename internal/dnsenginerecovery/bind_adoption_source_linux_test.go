//go:build linux

package dnsenginerecovery

import (
	"context"
	"github.com/alicelik/celikpanel/internal/bindconfig"
	"os"
	"path/filepath"
	"testing"
)

func TestBINDAdoptionInventoryExact(t *testing.T) {
	zones := []bindconfig.StaticZone{{Name: "owner.example", Class: "IN", Type: "master", File: "/etc/bind/owner.db"}, {Name: ".", Class: "IN", Type: "hint", File: "/usr/share/dns/root.hints"}}
	got, e := ParseBINDAdoptionNativeInventory("owner.example IN _default master\n. IN _default hint\n")
	if e != nil || adoptionInventoryEqual(zones, got) != nil {
		t.Fatalf("inventory %v %v", got, e)
	}
	for _, raw := range []string{"owner.example IN private master\n. IN _default hint", "owner.example IN _default slave\n. IN _default hint", "owner.example IN _default master\nowner.example IN _default master"} {
		inv, e := ParseBINDAdoptionNativeInventory(raw)
		if e == nil && adoptionInventoryEqual(zones, inv) == nil {
			t.Fatalf("unsafe inventory accepted %q", raw)
		}
	}
}
func TestBINDAdoptionSourceFileRefusesMutableEvidence(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root-owned fixture files")
	}
	root := t.TempDir()
	name := filepath.Join(root, "owner.db")
	if e := os.WriteFile(name, []byte("owner zone\n"), 0o600); e != nil {
		t.Fatal(e)
	}
	if e := adoptionSafeParents(name, 0); e == nil {
		t.Fatal("world-writable /tmp parent accepted")
	}
	check := func(string, uint32) error { return nil }
	read := func() (string, error) {
		p, e := readAdoptionFileChecked(context.Background(), name, check)
		return p.SHA256, e
	}
	first, e := read()
	if e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(name, []byte("other zone\n"), 0o600); e != nil {
		t.Fatal(e)
	}
	second, e := read()
	if e != nil {
		t.Fatal(e)
	}
	if first == second {
		t.Fatal("same-size owner edit accepted")
	}
	if e := os.WriteFile(name+".jnl", []byte("journal"), 0o600); e != nil {
		t.Fatal(e)
	}
	if _, e := read(); e == nil {
		t.Fatal("journal sidecar accepted")
	}
	if e := os.Remove(name + ".jnl"); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(name, []byte("$INCLUDE hidden\n"), 0o600); e != nil {
		t.Fatal(e)
	}
	if _, e := read(); e == nil {
		t.Fatal("zone include accepted")
	}
	if e := os.WriteFile(name, []byte("safe\n"), 0o600); e != nil {
		t.Fatal(e)
	}
	link := name + ".link"
	if e := os.Link(name, link); e != nil {
		t.Fatal(e)
	}
	if _, e := read(); e == nil {
		t.Fatal("hardlink accepted")
	}
	if e := os.Remove(link); e != nil {
		t.Fatal(e)
	}
	if e := os.Chmod(name, 0o666); e != nil {
		t.Fatal(e)
	}
	if _, e := read(); e == nil {
		t.Fatal("world-write accepted")
	}
	if e := os.Remove(name); e != nil {
		t.Fatal(e)
	}
	target := filepath.Join(root, "other")
	if e := os.WriteFile(target, []byte("safe\n"), 0o600); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(target, name); e != nil {
		t.Fatal(e)
	}
	if _, e := read(); e == nil {
		t.Fatal("symlink accepted")
	}
}
