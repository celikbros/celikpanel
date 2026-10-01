//go:build linux

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

// packageKitFixture is a fake /proc with PackageKit's daemon (PID 1721) as it
// looks on stock Ubuntu 24.04 after apt's DPkg::Post-Invoke hook started it:
// APT backend loaded, no children, no apt/dpkg lock (upd8 F1).
type packageKitFixture struct {
	root  string
	locks []string
	lines []string
}

const packageKitFixtureMaps = "7f0000000000-7f0000001000 r-xp 00000000 08:01 4242 " +
	"/usr/lib/x86_64-linux-gnu/packagekit-backend/libpk_backend_aptcc.so\n"

func newPackageKitFixture(t *testing.T) *packageKitFixture {
	t.Helper()
	fixture := &packageKitFixture{root: t.TempDir()}
	lockDir := t.TempDir()
	for _, name := range []string{"lock-frontend", "lock", "archives-lock", "lists-lock"} {
		path := filepath.Join(lockDir, name)
		if err := os.WriteFile(path, nil, 0o640); err != nil {
			t.Fatal(err)
		}
		fixture.locks = append(fixture.locks, path)
	}
	fixture.process(t, "1", "systemd", 0)
	fixture.process(t, "1721", "packagekitd", 1)
	fixture.file(t, "1721/maps", "55d000000000-55d000001000 r-xp 00000000 08:01 17 /usr/libexec/packagekitd\n"+packageKitFixtureMaps)
	fixture.process(t, "900", "sshd", 1)
	// Unrelated locks never count: another process's lock on an unwatched
	// file, and the daemon's own lock on its transaction database.
	fixture.lines = []string{
		"1: POSIX  ADVISORY  WRITE 900 08:01:1 0 EOF",
		"2: POSIX  ADVISORY  READ 1721 08:01:2 0 EOF",
	}
	return fixture
}

func (f *packageKitFixture) file(t *testing.T, name, content string) {
	t.Helper()
	path := filepath.Join(f.root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (f *packageKitFixture) process(t *testing.T, pid, comm string, parent int) {
	t.Helper()
	f.file(t, pid+"/comm", comm+"\n")
	f.file(t, pid+"/status", fmt.Sprintf("Name:\t%s\nState:\tS (sleeping)\nPid:\t%s\nPPid:\t%d\n", comm, pid, parent))
}

func (f *packageKitFixture) inode(t *testing.T, index int) uint64 {
	t.Helper()
	info, err := os.Stat(f.locks[index])
	if err != nil {
		t.Fatal(err)
	}
	return info.Sys().(*syscall.Stat_t).Ino
}

func (f *packageKitFixture) busy(t *testing.T) bool {
	t.Helper()
	f.file(t, "locks", strings.Join(f.lines, "\n")+"\n")
	busy, err := linuxPackageProcessBusyWith(f.root, f.locks)
	if err != nil {
		t.Fatalf("package process probe: %v", err)
	}
	return busy
}

func TestPackageKitIdleDaemonIsNotPackageActivity(t *testing.T) {
	fixture := newPackageKitFixture(t)
	if fixture.busy(t) {
		t.Fatal("an idle PackageKit daemon (APT backend, no child, no apt/dpkg lock) was counted as busy")
	}
}

func TestPackageKitTransactionIsPackageActivity(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*testing.T, *packageKitFixture)
	}{
		{"apt fetch method child", func(t *testing.T, f *packageKitFixture) { f.process(t, "1800", "http", 1721) }},
		{"dpkg child", func(t *testing.T, f *packageKitFixture) { f.process(t, "1801", "sh", 1721) }},
		{"holds the dpkg frontend lock", func(t *testing.T, f *packageKitFixture) {
			f.lines = append(f.lines, fmt.Sprintf("3: POSIX  ADVISORY  WRITE 1721 fd:00:%d 0 EOF", f.inode(t, 0)))
		}},
		{"holds the dpkg database lock", func(t *testing.T, f *packageKitFixture) {
			f.lines = append(f.lines, fmt.Sprintf("3: POSIX  ADVISORY  WRITE 1721 08:01:%d 0 EOF", f.inode(t, 1)))
		}},
		{"holds the archives lock while downloading", func(t *testing.T, f *packageKitFixture) {
			f.lines = append(f.lines, fmt.Sprintf("3: POSIX  ADVISORY  WRITE 1721 08:01:%d 0 EOF", f.inode(t, 2)))
		}},
		{"holds the lists lock while refreshing", func(t *testing.T, f *packageKitFixture) {
			f.lines = append(f.lines, fmt.Sprintf("3: POSIX  ADVISORY  WRITE 1721 08:01:%d 0 EOF", f.inode(t, 3)))
		}},
		{"waits for a lock", func(t *testing.T, f *packageKitFixture) {
			f.lines = append(f.lines,
				fmt.Sprintf("3: POSIX  ADVISORY  WRITE 900 08:01:%d 0 EOF", f.inode(t, 0)),
				fmt.Sprintf("3: -> POSIX  ADVISORY  WRITE 1721 08:01:%d 0 EOF", f.inode(t, 0)))
		}},
		{"unattributed lock on a watched file", func(t *testing.T, f *packageKitFixture) {
			f.lines = append(f.lines, fmt.Sprintf("3: OFDLCK ADVISORY  WRITE -1 08:01:%d 0 EOF", f.inode(t, 3)))
		}},
		{"another listed process beside an idle daemon", func(t *testing.T, f *packageKitFixture) { f.process(t, "2000", "dpkg", 1) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newPackageKitFixture(t)
			tc.setup(t, fixture)
			if !fixture.busy(t) {
				t.Fatal("PackageKit activity was not counted as busy")
			}
		})
	}
}

func TestPackageKitUnanswerableQuestionStaysBusy(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*testing.T, *packageKitFixture)
	}{
		{"backend map unreadable", func(t *testing.T, f *packageKitFixture) {
			if err := os.Remove(filepath.Join(f.root, "1721", "maps")); err != nil {
				t.Fatal(err)
			}
		}},
		{"backend other than APT", func(t *testing.T, f *packageKitFixture) {
			f.file(t, "1721/maps", "7f0000000000-7f0000001000 r-xp 00000000 08:01 4242 /usr/lib64/packagekit-backend/libpk_backend_dnf.so\n")
		}},
		{"lock table unparseable", func(t *testing.T, f *packageKitFixture) { f.lines = append(f.lines, "3: POSIX ADVISORY") }},
		{"lock owner unparseable", func(t *testing.T, f *packageKitFixture) {
			f.lines = append(f.lines, "3: POSIX  ADVISORY  WRITE pid 08:01:9 0 EOF")
		}},
		{"process parent unreadable", func(t *testing.T, f *packageKitFixture) { f.file(t, "900/status", "Name:\tsshd\n") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newPackageKitFixture(t)
			tc.setup(t, fixture)
			if !fixture.busy(t) {
				t.Fatal("an unanswered PackageKit question did not keep the daemon busy")
			}
		})
	}
	t.Run("lock table missing", func(t *testing.T) {
		fixture := newPackageKitFixture(t)
		busy, err := linuxPackageProcessBusyWith(fixture.root, fixture.locks)
		if err != nil || !busy {
			t.Fatalf("missing lock table: busy=%v err=%v, want busy", busy, err)
		}
	})
	t.Run("lock path uninspectable", func(t *testing.T) {
		fixture := newPackageKitFixture(t)
		// A path through a regular file cannot be inspected (ENOTDIR).
		fixture.locks = append(fixture.locks, filepath.Join(fixture.locks[0], "lock"))
		if !fixture.busy(t) {
			t.Fatal("an uninspectable lock path did not keep the daemon busy")
		}
	})
	t.Run("absent lock files hide nothing", func(t *testing.T) {
		fixture := newPackageKitFixture(t)
		fixture.locks = []string{filepath.Join(t.TempDir(), "absent")}
		if fixture.busy(t) {
			t.Fatal("a lock file that does not exist was treated as held")
		}
	})
}

// The admission refusal for real package activity keeps its sentinel and now
// names the package manager, so the Panel shows that sentence (D-024), and the
// independent mail renewal still treats it as an exclusion wait.
func TestBeginRefusalForPackageActivityNamesThePackageManager(t *testing.T) {
	manager, _ := newMutationTestManager(t)
	original := packageManagerMutationBusyProbe
	packageManagerMutationBusyProbe = func() (bool, error) { return true, nil }
	t.Cleanup(func() { packageManagerMutationBusyProbe = original })
	job, err := manager.begin(&ServiceMutationBeginRequest{
		RequestID: testMutationRequestID,
		OwnerID:   testMutationOwnerID,
		Kind:      "service_install",
		Target:    "nginx",
	})
	if job != nil || !errors.Is(err, errServiceMutationHostBusy) {
		t.Fatalf("package-busy begin job=%+v err=%v", job, err)
	}
	if err.Error() != errServiceMutationHostBusy.Error() {
		t.Fatalf("refusal text changed: %q", err.Error())
	}
	var response ServiceMutationResponse
	if !setHostMutationBusyResponse(&response, err) ||
		response.ErrorCode != transport.HostMutationBusy ||
		response.Reason != transport.HostMutationReasonPackageManager {
		t.Fatalf("package-busy response = %+v", response)
	}
	if !independentMailRenewalWait(err) {
		t.Fatal("mail renewal no longer treats a package-busy refusal as an exclusion wait")
	}
}
