//go:build linux

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
)

func prepareNginxReadyTestConfig(t *testing.T) (string, []byte, os.FileInfo) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "nginx.conf")
	original := []byte("events {}\nhttp {\n}\n")
	if err := os.WriteFile(path, original, 0o640); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return path, original, info
}

func assertNginxReadyFileIdentity(
	t *testing.T,
	path string,
	wantMode os.FileMode,
	wantOwner *syscall.Stat_t,
) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != wantMode.Perm() {
		t.Fatalf("mode = %o, want %o", info.Mode().Perm(), wantMode.Perm())
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatal("nginx.conf stat does not expose Linux ownership")
	}
	if owner.Uid != wantOwner.Uid || owner.Gid != wantOwner.Gid {
		t.Fatalf(
			"owner = %d:%d, want %d:%d",
			owner.Uid, owner.Gid, wantOwner.Uid, wantOwner.Gid,
		)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".celikpanel-") ||
			strings.Contains(entry.Name(), ".bak") {
			t.Fatalf("temporary or backup artifact remained: %s", entry.Name())
		}
	}
}

func TestEnsureNginxMainConfigPublishesAtomicallyAndIsIdempotent(t *testing.T) {
	path, _, before := prepareNginxReadyTestConfig(t)
	beforeOwner := before.Sys().(*syscall.Stat_t)
	var commands [][]string
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		commands = append(commands, append([]string{name}, args...))
		return nil, nil
	}

	changed, err := ensureNginxMainConfig(context.Background(), path, run)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("first call did not report a change")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, include := range []string{
		"include /etc/nginx/conf.d/*.conf;",
		"include /etc/nginx/sites-enabled/*.conf;",
	} {
		if strings.Count(string(content), include) != 1 {
			t.Fatalf("updated config does not contain exactly one %q", include)
		}
	}
	wantCommands := [][]string{
		{"nginx", "-t"},
		{"systemctl", "reload", "nginx"},
	}
	if !reflect.DeepEqual(commands, wantCommands) {
		t.Fatalf("commands = %#v, want %#v", commands, wantCommands)
	}

	changed, err = ensureNginxMainConfig(
		context.Background(),
		path,
		func(context.Context, string, ...string) ([]byte, error) {
			return nil, errors.New("idempotent call must not run commands")
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("idempotent call reported a change")
	}
	assertNginxReadyFileIdentity(t, path, before.Mode(), beforeOwner)
}

func TestEnsureNginxMainConfigRestoresOriginalOnValidationFailure(t *testing.T) {
	path, original, before := prepareNginxReadyTestConfig(t)
	beforeOwner := before.Sys().(*syscall.Stat_t)
	var commands [][]string
	validationCalls := 0
	changed, err := ensureNginxMainConfig(
		context.Background(),
		path,
		func(_ context.Context, name string, args ...string) ([]byte, error) {
			commands = append(commands, append([]string{name}, args...))
			if name == "nginx" {
				validationCalls++
				if validationCalls == 1 {
					return []byte("invalid include\nmore detail\n"), errors.New("exit 1")
				}
			}
			return nil, nil
		},
	)
	if err == nil || !strings.Contains(err.Error(), "original nginx.conf restored") {
		t.Fatalf("error = %v", err)
	}
	if changed {
		t.Fatal("failed validation reported a change")
	}
	restored, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !reflect.DeepEqual(restored, original) {
		t.Fatalf("restored bytes = %q, want %q", restored, original)
	}
	wantCommands := [][]string{
		{"nginx", "-t"},
		{"nginx", "-t"},
		{"systemctl", "reload", "nginx"},
	}
	if !reflect.DeepEqual(commands, wantCommands) {
		t.Fatalf("commands = %#v, want %#v", commands, wantCommands)
	}
	assertNginxReadyFileIdentity(t, path, before.Mode(), beforeOwner)
}

func TestEnsureNginxMainConfigRestoresOriginalOnReloadFailure(t *testing.T) {
	path, original, before := prepareNginxReadyTestConfig(t)
	beforeOwner := before.Sys().(*syscall.Stat_t)
	var commands [][]string
	reloadCalls := 0
	changed, err := ensureNginxMainConfig(
		context.Background(),
		path,
		func(_ context.Context, name string, args ...string) ([]byte, error) {
			commands = append(commands, append([]string{name}, args...))
			if name == "systemctl" {
				reloadCalls++
				if reloadCalls == 1 {
					return []byte("reload refused"), errors.New("exit 1")
				}
			}
			return nil, nil
		},
	)
	if err == nil || !strings.Contains(err.Error(), "original nginx.conf restored") {
		t.Fatalf("error = %v", err)
	}
	if changed {
		t.Fatal("failed reload reported a change")
	}
	restored, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !reflect.DeepEqual(restored, original) {
		t.Fatalf("restored bytes = %q, want %q", restored, original)
	}
	wantCommands := [][]string{
		{"nginx", "-t"},
		{"systemctl", "reload", "nginx"},
		{"nginx", "-t"},
		{"systemctl", "reload", "nginx"},
	}
	if !reflect.DeepEqual(commands, wantCommands) {
		t.Fatalf("commands = %#v, want %#v", commands, wantCommands)
	}
	assertNginxReadyFileIdentity(t, path, before.Mode(), beforeOwner)
}

func TestEnsureNginxMainConfigRestoresAfterCommittedWriteError(t *testing.T) {
	path, original, before := prepareNginxReadyTestConfig(t)
	beforeOwner := before.Sys().(*syscall.Stat_t)
	writeCalls := 0
	write := func(path string, content []byte, mode os.FileMode) error {
		writeCalls++
		if err := secureWriteConfig(path, content, mode); err != nil {
			return err
		}
		if writeCalls == 1 {
			return errors.New("injected parent directory fsync failure after rename")
		}
		return nil
	}
	var commands [][]string
	changed, err := ensureNginxMainConfigWithWriter(
		context.Background(),
		path,
		func(_ context.Context, name string, args ...string) ([]byte, error) {
			commands = append(commands, append([]string{name}, args...))
			return nil, nil
		},
		write,
	)
	if err == nil || !strings.Contains(err.Error(), "original nginx.conf restored") {
		t.Fatalf("error = %v", err)
	}
	if changed {
		t.Fatal("committed write error reported a successful change")
	}
	if writeCalls != 2 {
		t.Fatalf("write calls = %d, want 2", writeCalls)
	}
	restored, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !reflect.DeepEqual(restored, original) {
		t.Fatalf("restored bytes = %q, want %q", restored, original)
	}
	wantCommands := [][]string{
		{"nginx", "-t"},
		{"systemctl", "reload", "nginx"},
	}
	if !reflect.DeepEqual(commands, wantCommands) {
		t.Fatalf("commands = %#v, want %#v", commands, wantCommands)
	}
	assertNginxReadyFileIdentity(t, path, before.Mode(), beforeOwner)
}

func TestEnsureNginxMainConfigDoesNotClaimRollbackReloadSuccess(t *testing.T) {
	path, original, _ := prepareNginxReadyTestConfig(t)
	changed, err := ensureNginxMainConfig(
		context.Background(),
		path,
		func(_ context.Context, name string, _ ...string) ([]byte, error) {
			if name == "systemctl" {
				return []byte("reload refused"), errors.New("exit 1")
			}
			return nil, nil
		},
	)
	if err == nil || !strings.Contains(err.Error(), "restored and validated but reload failed") {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(err.Error(), "original nginx.conf restored;") {
		t.Fatalf("ambiguous rollback reload was reported as successful: %v", err)
	}
	if changed {
		t.Fatal("failed rollback reload reported a change")
	}
	restored, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !reflect.DeepEqual(restored, original) {
		t.Fatalf("restored bytes = %q, want %q", restored, original)
	}
}

func TestEnsureNginxMainConfigSerializesConcurrentCallers(t *testing.T) {
	path, _, _ := prepareNginxReadyTestConfig(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	run := func(_ context.Context, name string, _ ...string) ([]byte, error) {
		if name == "nginx" {
			once.Do(func() { close(entered) })
			<-release
		}
		return nil, nil
	}

	type result struct {
		changed bool
		err     error
	}
	results := make(chan result, 2)
	go func() {
		changed, err := ensureNginxMainConfig(context.Background(), path, run)
		results <- result{changed: changed, err: err}
	}()
	<-entered
	go func() {
		changed, err := ensureNginxMainConfig(context.Background(), path, run)
		results <- result{changed: changed, err: err}
	}()
	close(release)

	var changedCount int
	for range 2 {
		got := <-results
		if got.err != nil {
			t.Fatal(got.err)
		}
		if got.changed {
			changedCount++
		}
	}
	if changedCount != 1 {
		t.Fatalf("changed callers = %d, want 1", changedCount)
	}
}
