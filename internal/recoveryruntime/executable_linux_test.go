//go:build linux

package recoveryruntime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecutingRecoveryBinaryRequiresExactSelectedPayload(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("native root metadata")
	}
	root := t.TempDir()
	path := filepath.Join(root, "recovery")
	raw := []byte("selected recovery executable")
	if err := os.WriteFile(path, raw, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0755); err != nil {
		t.Fatal(err)
	}
	check := func(want string) error {
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		return verifyExecutableDescriptor(f, want)
	}
	if err := check(Digest(raw)); err != nil {
		t.Fatal(err)
	}
	if err := check(strings.Repeat("0", 64)); err == nil {
		t.Fatal("candidate executable accepted")
	}
	if err := os.Link(path, filepath.Join(root, "extra-link")); err != nil {
		t.Fatal(err)
	}
	if err := check(Digest(raw)); err == nil {
		t.Fatal("extra link accepted")
	}
	if err := os.Remove(filepath.Join(root, "extra-link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if err := check(Digest(raw)); err == nil {
		t.Fatal("unreviewed executable mode accepted")
	}
}
