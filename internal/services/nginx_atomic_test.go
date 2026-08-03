//go:build !windows

package services

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestWriteVhostFileAtomicallyReplacesConfigAndEnabledLink(t *testing.T) {
	previousDir := nginxDir
	nginxDir = t.TempDir()
	t.Cleanup(func() { nginxDir = previousDir })

	ng, err := NewNginxGenerator()
	if err != nil {
		t.Fatal(err)
	}
	const domain = "atomic.example.test"
	if err := ng.writeVhostFile(domain, "first\n"); err != nil {
		t.Fatalf("initial write: %v", err)
	}
	if err := ng.writeVhostFile(domain, "second\n"); err != nil {
		t.Fatalf("replacement write: %v", err)
	}

	available, enabled := vhostPaths(domain)
	content, err := os.ReadFile(available)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "second\n" {
		t.Fatalf("available content = %q", content)
	}
	info, err := os.Stat(available)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("available mode = %o, want 0644", info.Mode().Perm())
	}
	target, err := os.Readlink(enabled)
	if err != nil {
		t.Fatal(err)
	}
	if target != available {
		t.Fatalf("enabled target = %q, want %q", target, available)
	}

	for _, directory := range []string{filepath.Dir(available), filepath.Dir(enabled)} {
		entries, err := os.ReadDir(directory)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if strings.Contains(entry.Name(), ".tmp-") {
				t.Fatalf("temporary artifact remained in %s: %s", directory, entry.Name())
			}
		}
	}
}

func TestApplyVhostWithCommandRunnerOwnsValidationAndReload(t *testing.T) {
	previousDir := nginxDir
	nginxDir = t.TempDir()
	t.Cleanup(func() { nginxDir = previousDir })

	ng, err := NewNginxGenerator()
	if err != nil {
		t.Fatal(err)
	}
	type contextKey string
	const key contextKey = "mutation"
	ctx := context.WithValue(context.Background(), key, "owned")
	var commands [][]string
	run := func(
		commandCtx context.Context,
		name string,
		args ...string,
	) ([]byte, error) {
		if commandCtx.Value(key) != "owned" {
			t.Fatal("nginx subprocess escaped the caller-owned context")
		}
		commands = append(commands, append([]string{name}, args...))
		return nil, nil
	}

	if err := ng.ApplyVhostWithCommandRunner(
		ctx, "owned.example.test", "server {}\n", run,
	); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"nginx", "-t"},
		{"systemctl", "reload", "nginx"},
	}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands = %#v, want %#v", commands, want)
	}
}
