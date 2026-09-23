//go:build linux

package recoveryruntime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMailEnrollmentJournalProvisioning(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root fixture")
	}
	root, err := os.MkdirTemp("/run", "cp-mail-journal-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	path := filepath.Join(root, "private", "enrollment")
	if err = prepareMailEnrollmentJournalAt(path); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0700 {
		t.Fatal(info.Mode())
	}
	if err = os.WriteFile(filepath.Join(path, "retained"), []byte("evidence"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = prepareMailEnrollmentJournalAt(path); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(path)
	if !os.SameFile(info, after) {
		t.Fatal("replaced journal")
	}
	os.Chmod(path, 0755)
	if err = prepareMailEnrollmentJournalAt(path); err == nil {
		t.Fatal("normalized owner directory")
	}
	info, _ = os.Stat(path)
	if info.Mode().Perm() != 0755 {
		t.Fatal("changed owner mode")
	}
	link := filepath.Join(root, "link")
	os.Symlink(path, link)
	if err = prepareMailEnrollmentJournalAt(filepath.Join(link, "child")); err == nil {
		t.Fatal("followed owner symlink")
	}
	if _, err = os.Stat(filepath.Join(path, "child")); !os.IsNotExist(err) {
		t.Fatal("wrote through symlink")
	}
	raw, _ := os.ReadFile(filepath.Join(path, "retained"))
	if string(raw) != "evidence" {
		t.Fatal("lost evidence")
	}
}
