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
// looks on stock Ubuntu 24.04 after apt's hook started it: APT backend loaded,
// no children, no apt/dpkg lock (upd8 F1, measured by upd9).
type packageKitFixture struct {
	root  string
	locks []string
	lines []string
}

// packageKitUbuntu2404Maps is an excerpt of an idle packagekitd's maps on stock
// Ubuntu 24.04 (PackageKit 1.2.8-2ubuntu1.5). The pathnames are the ones the
// upd9 probe read from the running daemon (deploy/e2e/release-recovery/evidence/
// upd9-20261001/host/packagekit-probe-a.out.txt, PID 1719); the probe kept only
// the pathname column, so addresses, offsets, devices and inodes are
// illustrative. The backend is libpk_backend_apt.so: the rule of efcba145
// looked only for libpk_backend_aptcc.so and counted this daemon as busy.
const packageKitUbuntu2404Maps = `5a1c3e400000-5a1c3e40e000 r--p 00000000 fc:01 3473                       /usr/libexec/packagekitd
5a1c3e40e000-5a1c3e43a000 r-xp 0000e000 fc:01 3473                       /usr/libexec/packagekitd
5a1c3e44c000-5a1c3e44e000 rw-p 0004c000 fc:01 3473                       /usr/libexec/packagekitd
5a1c3f2a1000-5a1c3f4d6000 rw-p 00000000 00:00 0                          [heap]
7a0d8c000000-7a0d8c021000 rw-p 00000000 00:00 0
7a0d90a00000-7a0d90a62000 r--p 00000000 fc:01 2190                       /usr/lib/x86_64-linux-gnu/libapt-pkg.so.6.0.0
7a0d90a62000-7a0d90bc6000 r-xp 00062000 fc:01 2190                       /usr/lib/x86_64-linux-gnu/libapt-pkg.so.6.0.0
7a0d91200000-7a0d91212000 r--p 00000000 fc:01 41871                      /usr/lib/x86_64-linux-gnu/packagekit-backend/libpk_backend_apt.so
7a0d91212000-7a0d91249000 r-xp 00012000 fc:01 41871                      /usr/lib/x86_64-linux-gnu/packagekit-backend/libpk_backend_apt.so
7a0d91249000-7a0d9125a000 r--p 00049000 fc:01 41871                      /usr/lib/x86_64-linux-gnu/packagekit-backend/libpk_backend_apt.so
7a0d9125a000-7a0d9125c000 rw-p 0005a000 fc:01 41871                      /usr/lib/x86_64-linux-gnu/packagekit-backend/libpk_backend_apt.so
7a0d91400000-7a0d91428000 r--p 00000000 fc:01 2611                       /usr/lib/x86_64-linux-gnu/libpackagekit-glib2.so.18.1.3
7a0d91600000-7a0d9168f000 r--p 00000000 fc:01 2302                       /usr/lib/x86_64-linux-gnu/libglib-2.0.so.0.8000.0
7a0d91800000-7a0d91828000 r--p 00000000 fc:01 1843                       /usr/lib/x86_64-linux-gnu/libc.so.6
7a0d91a2f000-7a0d91a30000 r--p 00000000 fc:01 1838                       /usr/lib/x86_64-linux-gnu/ld-linux-x86-64.so.2
7ffd5e8a1000-7ffd5e8c2000 rw-p 00000000 00:00 0                          [stack]
7ffd5e9f3000-7ffd5e9f5000 r-xp 00000000 00:00 0                          [vdso]
`

// packageKitLegacyAptccMaps is the backend line of the APT backend under its
// name before upstream renamed it "apt" (not measured; efcba145's fixture).
const packageKitLegacyAptccMaps = "7f0000000000-7f0000001000 r-xp 00000000 08:01 4242 " +
	"/usr/lib/x86_64-linux-gnu/packagekit-backend/libpk_backend_aptcc.so\n"

const packageKitFixtureMaps = packageKitUbuntu2404Maps

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
	fixture.file(t, "1721/maps", packageKitFixtureMaps)
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

// upd9 F1: the maps of an idle daemon on stock Ubuntu 24.04 name the APT
// backend libpk_backend_apt.so. efcba145's test used only the aptcc name, so it
// passed while every Ubuntu 24.04 daemon stayed busy for its ~305 s life.
func TestPackageKitIdleUbuntu2404DaemonIsNotPackageActivity(t *testing.T) {
	fixture := newPackageKitFixture(t)
	fixture.file(t, "1721/maps", packageKitUbuntu2404Maps)
	if fixture.busy(t) {
		t.Fatal("an idle Ubuntu 24.04 PackageKit daemon (libpk_backend_apt.so) was counted as busy")
	}
	if !packageKitMapsShowOnlyAPTBackend(packageKitUbuntu2404Maps) {
		t.Fatal("the Ubuntu 24.04 maps excerpt was not recognised as the APT backend")
	}
}

func TestPackageKitIdleLegacyAptccDaemonIsNotPackageActivity(t *testing.T) {
	fixture := newPackageKitFixture(t)
	fixture.file(t, "1721/maps", "55d000000000-55d000001000 r-xp 00000000 08:01 17 /usr/libexec/packagekitd\n"+
		packageKitLegacyAptccMaps)
	if fixture.busy(t) {
		t.Fatal("an idle PackageKit daemon with the aptcc-named APT backend was counted as busy")
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
		{"zypp backend", func(t *testing.T, f *packageKitFixture) {
			f.file(t, "1721/maps", "7f0000000000-7f0000001000 r-xp 00000000 08:01 4242 /usr/lib64/packagekit-backend/libpk_backend_zypp.so\n")
		}},
		{"alpm backend", func(t *testing.T, f *packageKitFixture) {
			f.file(t, "1721/maps", "7f0000000000-7f0000001000 r-xp 00000000 08:01 4242 /usr/lib/packagekit-backend/libpk_backend_alpm.so\n")
		}},
		{"no backend mapped", func(t *testing.T, f *packageKitFixture) {
			f.file(t, "1721/maps", "55d000000000-55d000001000 r-xp 00000000 08:01 17 /usr/libexec/packagekitd\n")
		}},
		{"a name that only starts like the APT backend", func(t *testing.T, f *packageKitFixture) {
			f.file(t, "1721/maps", strings.ReplaceAll(packageKitUbuntu2404Maps, "libpk_backend_apt.so", "libpk_backend_apt-something.so"))
		}},
		{"the APT backend name outside a packagekit-backend directory", func(t *testing.T, f *packageKitFixture) {
			f.file(t, "1721/maps", strings.ReplaceAll(packageKitUbuntu2404Maps, "/packagekit-backend/libpk_backend_apt.so", "/libpk_backend_apt.so"))
		}},
		{"the APT backend name under a lookalike directory", func(t *testing.T, f *packageKitFixture) {
			f.file(t, "1721/maps", strings.ReplaceAll(packageKitUbuntu2404Maps, "/packagekit-backend/", "/packagekit-backend.old/"))
		}},
		{"the APT backend beside another backend", func(t *testing.T, f *packageKitFixture) {
			f.file(t, "1721/maps", packageKitUbuntu2404Maps+
				"7f0000000000-7f0000001000 r-xp 00000000 08:01 4243 /usr/lib/x86_64-linux-gnu/packagekit-backend/libpk_backend_test_spawn.so\n")
		}},
		{"replaced APT backend file", func(t *testing.T, f *packageKitFixture) {
			f.file(t, "1721/maps", strings.ReplaceAll(packageKitUbuntu2404Maps, "libpk_backend_apt.so\n", "libpk_backend_apt.so (deleted)\n"))
		}},
		{"unclean backend path", func(t *testing.T, f *packageKitFixture) {
			f.file(t, "1721/maps", strings.ReplaceAll(packageKitUbuntu2404Maps, "/usr/lib/x86_64-linux-gnu/packagekit-backend/", "/usr/lib/x86_64-linux-gnu/../packagekit-backend/"))
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

// upd9 F2: the update start's idle proof refuses package activity with the same
// sentence as before and now the typed reason, and the Agent's start reply
// carries that reason to the Panel. A refusal without a typed cause carries none.
func TestSystemUpdateStartRefusalForPackageActivityCarriesTheReason(t *testing.T) {
	root := mutationTestRoot(t)
	stateDir := filepath.Join(root, "state")
	lockPath := filepath.Join(root, "service-mutation.lock")
	if err := initializeServiceMutationLedger(stateDir, lockPath); err != nil {
		t.Fatalf("initialize service mutation ledger: %v", err)
	}
	t.Setenv("CELIKPANEL_AGENT_STATE_DIR", stateDir)
	t.Setenv("CELIKPANEL_MUTATION_LOCK", lockPath)
	original := packageManagerMutationBusyProbe
	packageManagerMutationBusyProbe = func() (bool, error) { return true, nil }
	t.Cleanup(func() { packageManagerMutationBusyProbe = original })

	lock, idleErr := acquireSystemUpdateServiceMutationIdleLock()
	if lock != nil || idleErr == nil {
		t.Fatalf("package-busy idle proof lock=%v err=%v", lock, idleErr)
	}
	if idleErr.Error() != "the host package manager is active" {
		t.Fatalf("idle refusal text changed: %q", idleErr.Error())
	}
	if reason, ok := serviceMutationReadinessReason(idleErr); !ok || reason != transport.HostMutationReasonPackageManager {
		t.Fatalf("idle refusal reason = %q, %v", reason, ok)
	}

	withSystemUpdateBuild(t, "v1.2.3-alpha.9", strings.Repeat("c", 40))
	manifest := testSystemUpdateManifest()
	backend := &fakeSystemUpdateBackend{
		floor: &systemUpdateFloor{Sequence: "41", Version: "v1.2.3-alpha.9"},
		// As linuxSystemUpdateBackend.QueueAndLaunch wraps it.
		queueErr: fmt.Errorf("global service mutation state is not idle: %w", idleErr),
	}
	service := newSystemUpdateService(&fakeSystemUpdateFetcher{version: manifest.Version, manifest: manifest}, backend, "linux", "amd64")
	globalSystemUpdateMu.Lock()
	previousService, previousErr := globalSystemUpdateService, globalSystemUpdateErr
	globalSystemUpdateService, globalSystemUpdateErr = service, nil
	globalSystemUpdateMu.Unlock()
	t.Cleanup(func() {
		globalSystemUpdateMu.Lock()
		globalSystemUpdateService, globalSystemUpdateErr = previousService, previousErr
		globalSystemUpdateMu.Unlock()
	})

	request := testSystemUpdateStartRequest(manifest, buildVersion, buildCommit)
	var reply transport.SystemUpdateStartResponse
	if err := new(Agent).StartSystemUpdate(&request, &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Accepted || reply.Reason != transport.HostMutationReasonPackageManager ||
		reply.Error != "global service mutation state is not idle: the host package manager is active" {
		t.Fatalf("package-busy start reply = %+v", reply)
	}

	backend.queueErr = errors.New("another system update request is active")
	reply = transport.SystemUpdateStartResponse{}
	if err := new(Agent).StartSystemUpdate(&request, &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Accepted || reply.Reason != "" || reply.Error == "" {
		t.Fatalf("untyped refusal reply = %+v", reply)
	}
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
