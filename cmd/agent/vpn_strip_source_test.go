package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func newVPNStripSourceTestConfigDir(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	previous := wgConfDir
	wgConfDir = directory
	t.Cleanup(func() { wgConfDir = previous })
	return directory
}

// wg-quick derives the interface name from the config basename, so it must
// only ever be handed "wg0.conf". Handing it the staging name
// ".wg0.conf.tmp-1234567890.conf" made every apply fail with "the config file
// must be a valid interface name, followed by .conf".
func TestApplyWireGuardConfigStripsCanonicalInterfaceBasename(t *testing.T) {
	configDirectory := newVPNStripSourceTestConfigDir(t)
	content := []byte("[Interface]\nAddress = 10.8.0.1/24\nListenPort = 51820\n")
	staged, err := stageAtomicFile(wgConfPath(), content, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(staged)
	if filepath.Base(staged) == wgIface+".conf" {
		t.Fatalf("staged basename %q is already canonical; the regression cannot be observed", staged)
	}

	capturedPath := ""
	var capturedContent []byte
	previousStrip := runWireGuardStrip
	runWireGuardStrip = func(_ context.Context, source string) ([]byte, error) {
		capturedPath = source
		capturedContent, _ = os.ReadFile(source)
		// Stop before "wg syncconf": this test only inspects the strip argv.
		return nil, errors.New("strip stopped by the test runner")
	}
	t.Cleanup(func() { runWireGuardStrip = previousStrip })

	if err := applyWireGuardConfig(context.Background(), staged); err == nil ||
		!strings.Contains(err.Error(), "wg-quick strip failed") {
		t.Fatalf("applyWireGuardConfig error=%v, want the strip failure", err)
	}
	if capturedPath == "" {
		t.Fatal("wg-quick strip was never invoked")
	}
	if got := filepath.Base(capturedPath); got != wgIface+".conf" {
		t.Fatalf("wg-quick strip received %q (basename %q), want basename %q",
			capturedPath, got, wgIface+".conf")
	}
	if !bytes.Equal(capturedContent, content) {
		t.Fatalf("wg-quick strip read %q, want the staged bytes %q", capturedContent, content)
	}
	stripDirectory := filepath.Dir(capturedPath)
	if stripDirectory == configDirectory {
		t.Fatalf("strip source %q sits in the config directory; the stage scanner would see it", capturedPath)
	}
	if _, err := os.Stat(stripDirectory); !os.IsNotExist(err) {
		t.Fatalf("private strip directory %q survived the apply: %v", stripDirectory, err)
	}
	if _, err := os.Stat(staged); err != nil {
		t.Fatalf("the durable stage did not survive the apply: %v", err)
	}
}

func TestStageWireGuardStripSourceUsesPrivateCanonicalCopy(t *testing.T) {
	newVPNStripSourceTestConfigDir(t)
	content := []byte("[Interface]\nPrivateKey = AAAA\n")
	staged, err := stageAtomicFile(wgConfPath(), content, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(staged)

	directory, source, err := stageWireGuardStripSource(staged)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(directory)
	if filepath.Dir(source) != directory {
		t.Fatalf("strip source %q is not inside the private directory %q", source, directory)
	}
	if got := filepath.Base(source); got != wgIface+".conf" {
		t.Fatalf("strip source basename=%q, want %q", got, wgIface+".conf")
	}
	copied, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(copied, content) {
		t.Fatalf("strip source content=%q, want %q", copied, content)
	}
	if runtime.GOOS == "windows" {
		return
	}
	directoryInfo, err := os.Stat(directory)
	if err != nil {
		t.Fatal(err)
	}
	if directoryInfo.Mode().Perm() != 0o700 {
		t.Fatalf("private strip directory mode=%o, want 700", directoryInfo.Mode().Perm())
	}
	sourceInfo, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	if sourceInfo.Mode().Perm() != 0o600 {
		t.Fatalf("strip source mode=%o, want 600", sourceInfo.Mode().Perm())
	}
}

// The staging name is load-bearing for crash recovery: findVPNPeerSyncRecoveryStage
// only recognises ".wg0.conf.tmp-*.conf" in the config directory. The strip fix
// must not have changed it, nor what the commit publishes.
func TestStageAtomicFileKeepsDurableWireGuardOutput(t *testing.T) {
	configDirectory := newVPNStripSourceTestConfigDir(t)
	previousSync := syncAtomicParentDirectory
	syncedPath := ""
	syncAtomicParentDirectory = func(path string) error {
		syncedPath = path
		return nil
	}
	t.Cleanup(func() { syncAtomicParentDirectory = previousSync })

	content := []byte("[Interface]\nListenPort = 51820\n")
	staged, err := stageAtomicFile(wgConfPath(), content, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if directory := filepath.Dir(staged); directory != configDirectory {
		t.Fatalf("stage directory=%q, want the config directory %q", directory, configDirectory)
	}
	name := filepath.Base(staged)
	prefix := "." + wgIface + ".conf.tmp-"
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".conf") {
		t.Fatalf("stage name=%q, want %q...%q so recovery still finds it", name, prefix, ".conf")
	}
	stagedContent, err := os.ReadFile(staged)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stagedContent, content) {
		t.Fatalf("stage content=%q, want %q", stagedContent, content)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(staged)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("stage mode=%o, want 600", info.Mode().Perm())
		}
	}

	if err := commitAtomicFile(staged, wgConfPath()); err != nil {
		t.Fatal(err)
	}
	if syncedPath != configDirectory {
		t.Fatalf("committed fsync path=%q, want %q", syncedPath, configDirectory)
	}
	published, err := os.ReadFile(wgConfPath())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(published, content) {
		t.Fatalf("published content=%q, want %q", published, content)
	}
	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Fatalf("the stage survived the commit rename: %v", err)
	}
}
