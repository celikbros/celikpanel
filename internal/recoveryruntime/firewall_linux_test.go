//go:build linux

package recoveryruntime

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/alicelik/celikpanel/internal/firewallruntime"
	"golang.org/x/sys/unix"
)

func firewallFixture(t *testing.T, source string, binary []byte) string {
	t.Helper()
	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(source, 0755); err != nil {
		t.Fatal(err)
	}
	manifest, unit, err := firewallruntime.Build(binary)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := firewallruntime.Encode(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for name, payload := range map[string][]byte{"restore": binary, firewallUnitName: unit, firewallruntime.ManifestName: raw} {
		mode := os.FileMode(0644)
		if name == "restore" {
			mode = 0755
		}
		p := filepath.Join(source, name)
		if err = os.WriteFile(p, payload, mode); err != nil {
			t.Fatal(err)
		}
		if err = os.Chmod(p, mode); err != nil {
			t.Fatal(err)
		}
	}
	return manifest.Generation
}

func TestFirewallPreparationWithInheritedLock(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("native root fixture")
	}
	for _, scenario := range []string{"fresh", "umask077", "no-lock", "active", "source-corrupt", "source-link", "source-extra", "source-mode", "existing-edited", "late-source-edit", "late-destination-move", "destination-collision"} {
		t.Run(scenario, func(t *testing.T) {
			root, lock := firewallTestRoot(t)
			if scenario != "no-lock" {
				if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
					t.Fatal(err)
				}
			}
			cmd := firewallChild(root, scenario, lock)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
		})
	}
}

func firewallTestRoot(t *testing.T) (string, *os.File) {
	t.Helper()
	root, err := os.MkdirTemp("/run", "celikpanel-firewall-publication-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	if err = os.Mkdir(filepath.Join(root, "transaction"), 0700); err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(filepath.Join(root, "transaction", "transaction.lock"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lock.Close() })
	return root, lock
}
func firewallChild(root, scenario string, lock *os.File) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^TestFirewallPreparationChild$", "-test.v")
	cmd.Env = append(os.Environ(), "CP_FIREWALL_PREP_ROOT="+root, "CP_FIREWALL_PREP_CASE="+scenario)
	cmd.ExtraFiles = []*os.File{nil, nil, nil, nil, nil, nil, lock}
	return cmd
}

func TestFirewallPreparationChild(t *testing.T) {
	root := os.Getenv("CP_FIREWALL_PREP_ROOT")
	if root == "" {
		t.Skip("isolated descriptor child")
	}
	scenario := os.Getenv("CP_FIREWALL_PREP_CASE")
	source := filepath.Join(root, "source", "firewall-runtime")
	destination := filepath.Join(root, "libexec", "firewall")
	transaction := filepath.Join(root, "transaction")
	generation := firewallFixture(t, source, []byte("first reviewed helper"))
	final := filepath.Join(destination, generation)
	if scenario == "umask077" {
		old := unix.Umask(077)
		defer unix.Umask(old)
	}
	switch scenario {
	case "active":
		os.WriteFile(filepath.Join(transaction, "active"), []byte("owner transaction"), 0600)
	case "source-corrupt":
		os.WriteFile(filepath.Join(source, "restore"), []byte("foreign binary"), 0755)
	case "source-link":
		os.Link(filepath.Join(source, "restore"), filepath.Join(root, "linked-helper"))
	case "source-extra":
		os.WriteFile(filepath.Join(source, "owner-data"), []byte("preserve"), 0600)
	case "source-mode":
		os.Chmod(filepath.Join(source, "restore"), 0775)
	case "existing-edited":
		if _, err := prepareFirewallAt(source, 9, destination, transaction, nil); err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(final, "restore"), []byte("owner edit"), 0755)
	}
	checkpoint := func(phase string) {
		if scenario == "cut-"+phase {
			unix.Kill(os.Getpid(), syscall.SIGKILL)
			panic("SIGKILL returned")
		}
		if phase != "stage_durable" {
			return
		}
		switch scenario {
		case "late-source-edit":
			os.WriteFile(filepath.Join(source, "restore"), []byte("later owner edit"), 0755)
		case "late-destination-move":
			if err := os.Rename(destination, destination+".owner"); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(destination, 0700); err != nil {
				t.Fatal(err)
			}
			os.WriteFile(filepath.Join(destination, "owner-data"), []byte("preserve"), 0600)
		case "destination-collision":
			if err := os.Mkdir(final, 0755); err != nil {
				t.Fatal(err)
			}
			os.WriteFile(filepath.Join(final, "owner-data"), []byte("preserve"), 0600)
		}
	}
	got, err := prepareFirewallAt(source, 9, destination, transaction, checkpoint)
	if scenario != "fresh" && scenario != "umask077" && scenario != "resume" {
		if err == nil {
			t.Fatal("unsafe publication succeeded")
		}
		if scenario == "existing-edited" {
			if raw, e := os.ReadFile(filepath.Join(final, "restore")); e != nil || string(raw) != "owner edit" {
				t.Fatal("owner edit lost")
			}
		} else if scenario == "destination-collision" || scenario == "late-destination-move" {
			parent := final
			if scenario == "late-destination-move" {
				parent = destination
			}
			if raw, e := os.ReadFile(filepath.Join(parent, "owner-data")); e != nil || string(raw) != "preserve" {
				t.Fatal("owner evidence lost")
			}
		} else if _, e := os.Lstat(final); !os.IsNotExist(e) {
			t.Fatal("final generation visible after refusal", e)
		}
		return
	}
	if err != nil || got != generation {
		t.Fatalf("prepare: %s %v", got, err)
	}
	first, err := os.Stat(filepath.Join(final, "restore"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = prepareFirewallAt(source, 9, destination, transaction, nil); err != nil {
		t.Fatal(err)
	}
	again, err := os.Stat(filepath.Join(final, "restore"))
	if err != nil || !os.SameFile(first, again) {
		t.Fatal("idempotent preparation replaced helper")
	}
	next := firewallFixture(t, source, []byte("next reviewed helper"))
	if got, err = prepareFirewallAt(source, 9, destination, transaction, nil); err != nil || got != next {
		t.Fatal("next generation failed", err)
	}
	for id, content := range map[string][]byte{generation: []byte("first reviewed helper"), next: []byte("next reviewed helper")} {
		bundle, e := readFirewallBundle(filepath.Join(destination, id))
		if e != nil {
			t.Fatal(e)
		}
		if !bytes.Equal(bundle.payload["restore"], content) {
			t.Fatal("retained generation changed")
		}
		bundle.state.close()
	}
}

func TestFirewallPreparationResumesAfterSIGKILL(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("native root fixture")
	}
	for _, phase := range []string{"file_restore", "stage_durable", "published", "parent_durable"} {
		t.Run(phase, func(t *testing.T) {
			root, lock := firewallTestRoot(t)
			if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
				t.Fatal(err)
			}
			cmd := firewallChild(root, "cut-"+phase, lock)
			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatal("cut not reached", string(out))
			}
			status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
			if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
				t.Fatalf("wrong cut: %v %s", err, out)
			}
			if out, err = firewallChild(root, "resume", lock).CombinedOutput(); err != nil {
				t.Fatalf("resume: %v %s", err, out)
			}
			stages, _ := filepath.Glob(filepath.Join(root, "libexec", "firewall", ".prepare-firewall-*"))
			if (phase == "file_restore" || phase == "stage_durable") && len(stages) == 0 {
				t.Fatal("interrupted evidence disappeared")
			}
		})
	}
}
