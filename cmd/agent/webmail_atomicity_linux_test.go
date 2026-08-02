//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublishRoundcubeStageRefusesExistingDestination(t *testing.T) {
	parent := t.TempDir()
	stage := filepath.Join(parent, "stage")
	final := filepath.Join(parent, "final")
	mustWriteTestFile(t, filepath.Join(stage, "stage.txt"), []byte("stage"), 0o600)
	mustWriteTestFile(t, filepath.Join(final, "final.txt"), []byte("final"), 0o600)

	if err := publishRoundcubeStage(stage, final); err == nil {
		t.Fatal("publishRoundcubeStage unexpectedly replaced an existing destination")
	}
	assertTestFileContent(t, filepath.Join(stage, "stage.txt"), []byte("stage"))
	assertTestFileContent(t, filepath.Join(final, "final.txt"), []byte("final"))
}

func TestPublishAndRetireRoundcubeTree(t *testing.T) {
	parent := t.TempDir()
	stage := filepath.Join(parent, "stage")
	final := filepath.Join(parent, "final")
	mustWriteTestFile(t, filepath.Join(stage, "marker.txt"), []byte("ready"), 0o600)

	if err := publishRoundcubeStage(stage, final); err != nil {
		t.Fatalf("publishRoundcubeStage: %v", err)
	}
	if _, err := os.Lstat(stage); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staging tree still exists after publish: %v", err)
	}
	assertTestFileContent(t, filepath.Join(final, "marker.txt"), []byte("ready"))

	if err := retireRoundcubeTree(final); err != nil {
		t.Fatalf("retireRoundcubeTree: %v", err)
	}
	if _, err := os.Lstat(final); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("published tree still exists after retirement: %v", err)
	}
}

func TestRetireRoundcubeTreeRefusesSymlink(t *testing.T) {
	parent := t.TempDir()
	outside := filepath.Join(parent, "outside")
	link := filepath.Join(parent, "roundcube")
	mustWriteTestFile(t, filepath.Join(outside, "marker.txt"), []byte("keep"), 0o600)
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if err := retireRoundcubeTree(link); err == nil {
		t.Fatal("retireRoundcubeTree unexpectedly followed a symlink")
	}
	assertTestFileContent(t, filepath.Join(outside, "marker.txt"), []byte("keep"))
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlink was changed: info=%v err=%v", info, err)
	}
}

func TestRoundcubeInstallStateRequiresCompleteTree(t *testing.T) {
	original := webmailBaseDir
	webmailBaseDir = filepath.Join(t.TempDir(), "roundcube")
	t.Cleanup(func() { webmailBaseDir = original })

	mustWriteTestFile(t, filepath.Join(webmailBaseDir, "public_html", "index.php"), []byte("index"), 0o600)
	if installed, err := roundcubeInstallState(); err != nil || installed {
		t.Fatalf("entry point alone reported complete: installed=%v err=%v", installed, err)
	}
	mustWriteTestFile(t, filepath.Join(webmailBaseDir, "config", "config.inc.php"), []byte("config"), 0o600)
	if installed, err := roundcubeInstallState(); err != nil || installed {
		t.Fatalf("tree without database reported complete: installed=%v err=%v", installed, err)
	}
	mustWriteTestFile(t, filepath.Join(webmailBaseDir, "db", "roundcube.sqlite3"), []byte("db"), 0o600)
	if installed, err := roundcubeInstallState(); err != nil || !installed {
		t.Fatalf("complete tree not reported installed: installed=%v err=%v", installed, err)
	}
}

func TestApplyWebmailNginxMutationRestoresOnValidationFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "webmail.conf")
	old := []byte("old configuration\n")
	mustWriteTestFile(t, path, old, 0o640)

	var calls []string
	runner := func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(append([]string{name}, args...), " "))
		return []byte("invalid configuration"), errors.New("exit status 1")
	}
	err := applyWebmailNginxMutation(context.Background(), path, []byte("new\n"), true, runner)
	if err == nil {
		t.Fatal("validation failure unexpectedly succeeded")
	}
	assertTestFileContent(t, path, old)
	assertTestFileMode(t, path, 0o640)
	if len(calls) != 1 || calls[0] != "nginx -t" {
		t.Fatalf("unexpected commands: %#v", calls)
	}
}

func TestApplyWebmailNginxMutationRestoresAndReloadsOnReloadFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "webmail.conf")
	old := []byte("old configuration\n")
	mustWriteTestFile(t, path, old, 0o640)

	var calls []string
	runner := func(_ context.Context, name string, args ...string) ([]byte, error) {
		call := strings.Join(append([]string{name}, args...), " ")
		calls = append(calls, call)
		if len(calls) == 2 {
			return []byte("reload failed"), errors.New("exit status 1")
		}
		return nil, nil
	}
	err := applyWebmailNginxMutation(context.Background(), path, []byte("new\n"), true, runner)
	if err == nil {
		t.Fatal("reload failure unexpectedly succeeded")
	}
	assertTestFileContent(t, path, old)
	assertTestFileMode(t, path, 0o640)
	want := []string{
		"nginx -t",
		"systemctl reload nginx",
		"nginx -t",
		"systemctl reload nginx",
	}
	if strings.Join(calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("unexpected commands:\n got: %#v\nwant: %#v", calls, want)
	}
}

func TestApplyWebmailNginxMutationRemoveReloadFailureRestoresConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "webmail.conf")
	old := []byte("old configuration\n")
	mustWriteTestFile(t, path, old, 0o600)

	callCount := 0
	runner := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		callCount++
		if callCount == 2 {
			return []byte("reload failed"), errors.New("exit status 1")
		}
		return nil, nil
	}
	err := applyWebmailNginxMutation(context.Background(), path, nil, false, runner)
	if err == nil {
		t.Fatal("remove reload failure unexpectedly succeeded")
	}
	assertTestFileContent(t, path, old)
	assertTestFileMode(t, path, 0o600)
	if callCount != 4 {
		t.Fatalf("unexpected command count: got %d want 4", callCount)
	}
}

func TestApplyRoundcubePermissionsReturnsChownFailure(t *testing.T) {
	base := filepath.Join(t.TempDir(), "roundcube")
	if err := os.MkdirAll(base, 0o700); err != nil {
		t.Fatalf("mkdir base: %v", err)
	}
	runner := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte("permission denied"), errors.New("exit status 1")
	}
	if err := applyRoundcubePermissions(context.Background(), base, filepath.Join(base, "db.sqlite3"), "root", runner); err == nil {
		t.Fatal("chown failure unexpectedly succeeded")
	}
}

func TestApplyRoundcubePermissionsRefusesSymlinkBeforeChown(t *testing.T) {
	parent := t.TempDir()
	outside := filepath.Join(parent, "outside")
	link := filepath.Join(parent, "roundcube")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatalf("mkdir outside: %v", err)
	}
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	called := false
	runner := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		called = true
		return nil, nil
	}
	if err := applyRoundcubePermissions(context.Background(), link, filepath.Join(link, "db.sqlite3"), "root", runner); err == nil {
		t.Fatal("symlinked Roundcube tree unexpectedly accepted")
	}
	if called {
		t.Fatal("chown runner was called before symlink rejection")
	}
}

func TestApplyRoundcubePermissionsAppliesExactModes(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("changing Roundcube ownership requires root")
	}
	base := filepath.Join(t.TempDir(), "roundcube")
	for _, dir := range []string{"db", "temp", "logs", "config"} {
		if err := os.MkdirAll(filepath.Join(base, dir), 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	config := filepath.Join(base, "config", "config.inc.php")
	database := filepath.Join(base, "db", "roundcube.sqlite3")
	mustWriteTestFile(t, config, []byte("config"), 0o600)
	mustWriteTestFile(t, database, []byte("db"), 0o600)
	runner := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return nil, nil
	}

	if err := applyRoundcubePermissions(context.Background(), base, database, "root", runner); err != nil {
		t.Fatalf("applyRoundcubePermissions: %v", err)
	}
	assertTestFileMode(t, base, 0o750)
	for _, dir := range []string{"db", "temp", "logs"} {
		assertTestFileMode(t, filepath.Join(base, dir), 0o770)
	}
	assertTestFileMode(t, config, 0o640)
	assertTestFileMode(t, database, 0o660)
}

func mustWriteTestFile(t *testing.T, path string, content []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, content, mode); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod %s: %v", path, err)
	}
}

func assertTestFileContent(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("content mismatch for %s: got %q want %q", path, got, want)
	}
}

func assertTestFileMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want.Perm() {
		t.Fatalf("mode mismatch for %s: got %04o want %04o", path, got, want.Perm())
	}
}
