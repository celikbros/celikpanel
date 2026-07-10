package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// aptListsAge decides whether an install is preceded by `apt-get update`; a
// wrong answer either slows every install or lets a stale list 404 through.
func TestAptListsAge(t *testing.T) {
	orig := aptListsDir
	defer func() { aptListsDir = orig }()

	// Missing directory → unknown, callers must treat as stale.
	aptListsDir = filepath.Join(t.TempDir(), "does-not-exist")
	if _, ok := aptListsAge(); ok {
		t.Error("missing lists dir must report ok=false")
	}

	// Empty directory (only subdirs, like partial/) → still unknown.
	dir := t.TempDir()
	aptListsDir = dir
	if err := os.Mkdir(filepath.Join(dir, "partial"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, ok := aptListsAge(); ok {
		t.Error("dir with no list files must report ok=false")
	}

	// A fresh list file → small age, ok=true.
	f := filepath.Join(dir, "deb.debian.org_debian_dists_trixie_InRelease")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	age, ok := aptListsAge()
	if !ok || age > time.Minute {
		t.Errorf("fresh file: age=%v ok=%v, want recent + true", age, ok)
	}

	// Back-date it → age must reflect the newest file, and an even older
	// second file must not win.
	old := time.Now().Add(-3 * time.Hour)
	if err := os.Chtimes(f, old, old); err != nil {
		t.Fatal(err)
	}
	age, ok = aptListsAge()
	if !ok || age < 2*time.Hour {
		t.Errorf("back-dated file: age=%v ok=%v, want ≥2h + true", age, ok)
	}
}
