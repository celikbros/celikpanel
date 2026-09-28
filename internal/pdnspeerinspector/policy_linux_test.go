//go:build linux

package pdnspeerinspector

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestFixedOwnerPolicyReaderRejectsUnsafeFiles(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-only owner policy")
	}
	r, p, _ := fixture(t)
	root := t.TempDir()
	dir := filepath.Join(root, "etc", "pdns-peer-inspector")
	if err := os.MkdirAll(dir, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(root, "etc"), 0755); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "policy.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	got, hash, err := readOwnerPolicyAt(root)
	if err != nil || hash == "" || got.ValidateRequest(r) != nil {
		t.Fatalf("owner policy rejected: %+v %s %v", got, hash, err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readOwnerPolicyAt(root); err == nil {
		t.Fatal("world-readable policy accepted")
	}
	os.Chmod(path, 0600)
	if err := os.Rename(path, path+".real"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path+".real", path); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readOwnerPolicyAt(root); err == nil {
		t.Fatal("symlink policy accepted")
	}
}
