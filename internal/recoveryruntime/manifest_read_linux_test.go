//go:build linux

package recoveryruntime

import (
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"testing"
)

func TestPinnedManifestReadRejectsPathReplacement(t *testing.T) {
	fixture := newRuntimeFixture(t)
	runtime, err := resolveAt(fixture.config)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	raw, err := runtime.ManifestBytes()
	if err != nil || Digest(raw) != runtime.Digest {
		t.Fatal(err)
	}
	path := filepath.Join(runtime.Root, ManifestName)
	if err := os.Rename(path, path+".old"); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.ManifestBytes(); err == nil {
		t.Fatal("replacement accepted")
	}
}
