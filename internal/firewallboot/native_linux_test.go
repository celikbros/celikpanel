//go:build linux

package firewallboot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNativeSnapshotMetadataAndReplacement(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("native root ownership fixture requires root")
	}
	root, e := os.MkdirTemp("/root", "celikpanel-firewall-reader-test-")
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(root)
	p := filepath.Join(root, "policy")
	raw := []byte("{\"version\":2,\"tcp_ports\":null,\"udp_ports\":null,\"ssh_ports_at_save\":null}\n")
	if e = os.WriteFile(p, raw, 0600); e != nil {
		t.Fatal(e)
	}
	first, e := readSnapshot(p)
	if e != nil || !first.exists {
		t.Fatalf("read: %+v %v", first, e)
	}
	q := filepath.Join(root, "replacement")
	if e = os.WriteFile(q, raw, 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.Rename(q, p); e != nil {
		t.Fatal(e)
	}
	second, e := readSnapshot(p)
	if e != nil || first.identity == second.identity {
		t.Fatalf("replacement not distinguished: %v", e)
	}
	if e = os.Chmod(p, 0644); e != nil {
		t.Fatal(e)
	}
	if _, e = readSnapshot(p); e == nil {
		t.Fatal("unsafe mode accepted")
	}
	os.Chmod(p, 0600)
	link := filepath.Join(root, "link")
	os.Symlink(p, link)
	if _, e = readSnapshot(link); e == nil {
		t.Fatal("symlink accepted")
	}
	hard := filepath.Join(root, "hard")
	if e = os.Link(p, hard); e != nil {
		t.Fatal(e)
	}
	if _, e = readSnapshot(p); e == nil {
		t.Fatal("hardlink accepted")
	}
	missing, e := readSnapshot(filepath.Join(root, "missing"))
	if e != nil || missing.exists {
		t.Fatalf("absence: %+v %v", missing, e)
	}
}
func TestUnknownExecutableNameRefused(t *testing.T) {
	if _, e := trusted("sh"); e == nil {
		t.Fatal("unlisted command accepted")
	}
}

func TestNativeExclusionRejectsDuplicate(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-owned native lock fixture")
	}
	first, err := acquireLock()
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if duplicate, err := acquireLock(); err == nil {
		duplicate.Close()
		t.Fatal("duplicate native restore admitted")
	}
	first.Close()
	next, err := acquireLock()
	if err != nil {
		t.Fatal(err)
	}
	next.Close()
}

func TestNativeSnapshotAcceptsReadOnlyGroupParents(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("native ownership fixture requires root")
	}
	root, err := os.MkdirTemp("/root", "celikpanel-firewall-layout-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	parent := filepath.Join(root, "etc", "celikpanel")
	if err = os.MkdirAll(parent, 0750); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{filepath.Dir(parent), parent} {
		if err = os.Chown(dir, 0, 989); err != nil {
			t.Fatal(err)
		}
	}
	p := filepath.Join(parent, "policy")
	if err = os.WriteFile(p, []byte("saved policy"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Chown(p, 0, 989); err != nil {
		t.Fatal(err)
	}
	if got, err := readSnapshot(p); err != nil || !got.exists {
		t.Fatalf("installer layout rejected: %v", err)
	}
	for _, dir := range []string{filepath.Dir(parent), parent} {
		if err = os.Chmod(dir, 0770); err != nil {
			t.Fatal(err)
		}
		if _, err = readSnapshot(p); err == nil {
			t.Fatal("group writable directory accepted")
		}
		if err = os.Chmod(dir, 0750); err != nil {
			t.Fatal(err)
		}
		if err = os.Chown(dir, 989, 989); err != nil {
			t.Fatal(err)
		}
		if _, err = readSnapshot(p); err == nil {
			t.Fatal("non-root directory accepted")
		}
		if err = os.Chown(dir, 0, 989); err != nil {
			t.Fatal(err)
		}
	}
}
