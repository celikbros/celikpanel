//go:build linux

package dnsenginerecovery

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/alicelik/celikpanel/internal/bindroot"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"golang.org/x/sys/unix"
)

func bindSwitchConfigProbeFixture(t *testing.T) (string, int, dnsengineartifact.JournalPolicy, dnsengineartifact.SwitchJournalV1) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("secure BIND config fixture requires root-owned paths")
	}
	policy, journal := bindConfigClassifierFixture(t)
	root := t.TempDir()
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "etc", "bind")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, snapshot := range journal.ConfigBefore {
		path := filepath.Join(root, snapshot.Path)
		if err := os.WriteFile(path, snapshot.Data, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chown(path, 0, int(snapshot.GID)); err != nil {
			t.Fatal(err)
		}
	}
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { unix.Close(fd) })
	return root, fd, policy, journal
}

func TestProbeBINDSwitchConfigCheckpointAtV2ClassifiesExactMixedFiles(t *testing.T) {
	root, fd, policy, journal := bindSwitchConfigProbeFixture(t)
	states, err := ProbeBINDSwitchConfigCheckpointAtV2(context.Background(), fd, policy, journal, bindroot.APT, 42)
	if err != nil || !reflect.DeepEqual(states, []BINDConfigFileState{BINDConfigFileBefore, BINDConfigFileBefore}) {
		t.Fatalf("before states=%v error=%v", states, err)
	}
	// The trusted root is reached through the same descriptor, not a journal
	// controlled path. Replace only one complete file with its frozen after bytes.
	after := journal.InversePlan.ConfigAfter[0]
	if err := os.WriteFile(filepath.Join(root, after.Path), after.Data, 0o644); err != nil {
		t.Fatal(err)
	}
	states, err = ProbeBINDSwitchConfigCheckpointAtV2(context.Background(), fd, policy, journal, bindroot.APT, 42)
	if err != nil || !reflect.DeepEqual(states, []BINDConfigFileState{BINDConfigFileAfter, BINDConfigFileBefore}) {
		t.Fatalf("mixed states=%v error=%v", states, err)
	}
}

func TestProbeBINDSwitchConfigCheckpointAtV2RefusesOwnerEditAndUnknownLayout(t *testing.T) {
	root, fd, policy, journal := bindSwitchConfigProbeFixture(t)
	if _, err := ProbeBINDSwitchConfigCheckpointAtV2(context.Background(), fd, policy, journal, bindroot.Pacman, 42); err == nil {
		t.Fatal("wrong host layout accepted")
	}
	path := filepath.Join(root, journal.ConfigBefore[0].Path)
	if err := os.WriteFile(path, []byte("owner edit"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ProbeBINDSwitchConfigCheckpointAtV2(context.Background(), fd, policy, journal, bindroot.APT, 42); err == nil {
		t.Fatal("owner edit accepted as an exact checkpoint")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", path); err != nil {
		t.Fatal(err)
	}
	if _, err := ProbeBINDSwitchConfigCheckpointAtV2(context.Background(), fd, policy, journal, bindroot.APT, 42); err == nil {
		t.Fatal("symlinked config accepted")
	}
}
