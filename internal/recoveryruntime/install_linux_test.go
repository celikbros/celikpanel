//go:build linux

package recoveryruntime

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func TestEnrollWithInheritedLock(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("native root fixture")
	}
	for _, scenario := range []string{"fresh", "active", "corrupt-source", "retained", "corrupt-selected", "interrupted-stage", "launcher-conflict", "no-lock", "umask077", "interrupted-launcher"} {
		t.Run(scenario, func(t *testing.T) {
			root, err := os.MkdirTemp("/run", "celikpanel-runtime-test-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(root)
			os.Chmod(root, 0700)
			txn := filepath.Join(root, "transaction")
			if err := os.Mkdir(txn, 0700); err != nil {
				t.Fatal(err)
			}
			lock, err := os.OpenFile(filepath.Join(txn, "transaction.lock"), os.O_CREATE|os.O_RDWR|os.O_EXCL, 0600)
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Close()
			if scenario != "no-lock" {
				if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command(os.Args[0], "-test.run=^TestEnrollmentChild$", "-test.v")
			cmd.Env = append(os.Environ(), "CP_ENROLL_TEST_ROOT="+root, "CP_ENROLL_TEST_CASE="+scenario)
			cmd.ExtraFiles = []*os.File{nil, nil, nil, nil, nil, nil, lock}
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
		})
	}
}
func TestEnrollmentChild(t *testing.T) {
	root := os.Getenv("CP_ENROLL_TEST_ROOT")
	if root == "" {
		t.Skip("only isolated inherited descriptor child")
	}
	paths := enrollmentPaths{filepath.Join(root, "libexec", "runtimes", "v1"), filepath.Join(root, "state", "selection"), filepath.Join(root, "libexec", "recovery"), filepath.Join(root, "transaction")}
	source := filepath.Join(root, "bundle")
	makePackagedKit(t, source)
	scenario := os.Getenv("CP_ENROLL_TEST_CASE")
	if scenario == "umask077" {
		old := unix.Umask(0077)
		defer unix.Umask(old)
	}
	switch scenario {
	case "interrupted-launcher":
		os.MkdirAll(filepath.Dir(paths.launcher), 0700)
		os.WriteFile(filepath.Join(filepath.Dir(paths.launcher), ".recovery-enroll-old-interruption"), []byte("partial"), 0600)

	case "active":
		os.WriteFile(filepath.Join(paths.transaction, "active"), []byte("durable operation"), 0600)
	case "corrupt-source":
		os.WriteFile(filepath.Join(source, "update.sh"), []byte("corrupt"), 0755)
	case "interrupted-stage":
		os.MkdirAll(filepath.Join(paths.runtimeRoot, ".enroll-interrupted"), 0700)
	case "launcher-conflict":
		os.MkdirAll(filepath.Dir(paths.launcher), 0700)
		os.WriteFile(paths.launcher, []byte("owner file"), 0755)
	}
	err := enrollAt(source, 9, paths)
	if scenario == "active" || scenario == "corrupt-source" || scenario == "no-lock" || scenario == "launcher-conflict" {
		if err == nil {
			t.Fatal("unsafe enrollment succeeded")
		}
		if _, err := os.Lstat(paths.selection); !os.IsNotExist(err) {
			t.Fatal("selection published on failure", err)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	config := resolveConfig{runtimeRoot: paths.runtimeRoot, selectionPath: paths.selection, anchor: "/", uid: 0, gid: 0}
	selected, err := resolveAt(config)
	if err != nil {
		t.Fatal(err)
	}
	defer selected.Close()
	if err := verifyLauncherAt(selected, paths.launcher); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(paths.selection)
	if err != nil {
		t.Fatal(err)
	}
	if scenario == "retained" {
		// A later candidate cannot replace the enrolled predecessor, even if its
		// candidate runtime is incompatible. No candidate code is executed here.
		os.WriteFile(filepath.Join(source, "update.sh"), []byte("new candidate"), 0755)
		if err := enrollAt(source, 9, paths); err != nil {
			t.Fatal(err)
		}
		after, _ := os.ReadFile(paths.selection)
		if !bytes.Equal(original, after) {
			t.Fatal("predecessor changed")
		}
	}
	if scenario == "corrupt-selected" {
		os.WriteFile(filepath.Join(selected.Root, "rollback.sh"), []byte("damaged"), 0755)
		if err := enrollAt(source, 9, paths); err == nil {
			t.Fatal("silently replaced selected kit")
		}
		after, _ := os.ReadFile(paths.selection)
		if !bytes.Equal(original, after) {
			t.Fatal("selection changed on failure")
		}
	}
}
func makePackagedKit(t *testing.T, root string) {
	t.Helper()
	for _, dir := range []string{"", "bin", "deploy", "deploy/recovery"} {
		path := filepath.Join(root, dir)
		if err := os.MkdirAll(path, 0755); err != nil {
			t.Fatal(err)
		}
		os.Chmod(path, 0755)
	}
	manifest := Manifest{Files: map[string]string{}}
	for _, spec := range ExpectedFiles() {
		raw := []byte("test kit " + spec.Path + "\n")
		path := filepath.Join(root, spec.Path)
		if err := os.WriteFile(path, raw, spec.Mode); err != nil {
			t.Fatal(err)
		}
		os.Chmod(path, spec.Mode)
		manifest.Files[spec.Path] = Digest(raw)
	}
	raw, err := EncodeManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ManifestName), raw, 0644); err != nil {
		t.Fatal(err)
	}
	os.Chmod(filepath.Join(root, ManifestName), 0644)
}

func TestEnrollAfterSIGKILLAtPublishedLauncher(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("native root fixture")
	}
	for _, scenario := range []string{"same-candidate", "different-candidate"} {
		t.Run(scenario, func(t *testing.T) {
			root, err := os.MkdirTemp("/run", "celikpanel-enrollment-crash-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(root)
			paths := enrollmentCrashTestPaths(root)
			if err := os.Mkdir(paths.transaction, 0700); err != nil {
				t.Fatal(err)
			}
			lock, err := os.OpenFile(filepath.Join(paths.transaction, "transaction.lock"), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Close()
			if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
				t.Fatal(err)
			}
			makePackagedKit(t, filepath.Join(root, "bundle"))
			child := func(phase string) *exec.Cmd {
				cmd := exec.Command(os.Args[0], "-test.run=^TestEnrollmentPublicationCrashChild$", "-test.v")
				cmd.Env = append(os.Environ(), "CP_ENROLL_CRASH_ROOT="+root, "CP_ENROLL_CRASH_PHASE="+phase)
				cmd.ExtraFiles = []*os.File{nil, nil, nil, nil, nil, nil, lock}
				return cmd
			}
			crash := child("kill-after-launcher")
			output, err := crash.CombinedOutput()
			exit, ok := err.(*exec.ExitError)
			if !ok {
				t.Fatalf("expected actual process SIGKILL, got %v: %s", err, output)
			}
			status, ok := exit.Sys().(syscall.WaitStatus)
			if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
				t.Fatalf("process did not die from SIGKILL: %v: %s", err, output)
			}
			if _, err := os.Lstat(paths.selection); !os.IsNotExist(err) {
				t.Fatal("selector exists at crash checkpoint", err)
			}
			raw, err := os.ReadFile(filepath.Join(root, "bundle", ManifestName))
			if err != nil {
				t.Fatal(err)
			}
			digest := Digest(raw)
			installedPath := filepath.Join(paths.runtimeRoot, digest)
			installed, err := VerifyBundle(installedPath, false)
			if err != nil {
				t.Fatal("crash did not leave a complete durable kit", err)
			}
			if err := verifyLauncherAt(installed, paths.launcher); err != nil {
				t.Fatal("launcher was not published before crash", err)
			}
			installed.Close()
			beforeKit := snapshotRuntimeFixture(t, installedPath)
			beforeLauncher := snapshotRuntimeFixture(t, paths.launcher)
			if scenario == "different-candidate" {
				newSource := filepath.Join(root, "different-bundle")
				makePackagedKit(t, newSource)
				newRecovery := []byte("different valid candidate recovery payload\n")
				if err := os.WriteFile(filepath.Join(newSource, "bin/recovery"), newRecovery, 0755); err != nil {
					t.Fatal(err)
				}
				manifest, err := ParseManifest(raw)
				if err != nil {
					t.Fatal(err)
				}
				manifest.Files["bin/recovery"] = Digest(newRecovery)
				newManifest, err := EncodeManifest(manifest)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(newSource, ManifestName), newManifest, 0644); err != nil {
					t.Fatal(err)
				}
			}
			if output, err := child(scenario).CombinedOutput(); err != nil {
				t.Fatalf("retry failed: %v\n%s", err, output)
			}
			if !reflect.DeepEqual(beforeKit, snapshotRuntimeFixture(t, installedPath)) || !reflect.DeepEqual(beforeLauncher, snapshotRuntimeFixture(t, paths.launcher)) {
				t.Fatal("retry changed the already published kit or launcher evidence")
			}
			if scenario == "different-candidate" {
				if _, err := os.Lstat(paths.selection); !os.IsNotExist(err) {
					t.Fatal("different candidate selected after refusal", err)
				}
			} else {
				selected, err := resolveAt(resolveConfig{runtimeRoot: paths.runtimeRoot, selectionPath: paths.selection, anchor: "/", uid: 0, gid: 0})
				if err != nil {
					t.Fatal(err)
				}
				defer selected.Close()
				if selected.Digest != digest {
					t.Fatal("retry selected a different kit")
				}
				if err := verifyLauncherAt(selected, paths.launcher); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func enrollmentCrashTestPaths(root string) enrollmentPaths {
	return enrollmentPaths{filepath.Join(root, "libexec/runtimes/v1"), filepath.Join(root, "state/selection"), filepath.Join(root, "libexec/recovery"), filepath.Join(root, "transaction")}
}

func TestEnrollmentPublicationCrashChild(t *testing.T) {
	root := os.Getenv("CP_ENROLL_CRASH_ROOT")
	if root == "" {
		t.Skip("only isolated inherited descriptor child")
	}
	paths := enrollmentCrashTestPaths(root)
	source := filepath.Join(root, "bundle")
	switch os.Getenv("CP_ENROLL_CRASH_PHASE") {
	case "kill-after-launcher":
		err := enrollWithCheckpoint(source, 9, paths, func() {
			if err := unix.Kill(os.Getpid(), unix.SIGKILL); err != nil {
				t.Fatal(err)
			}
			select {}
		})
		t.Fatalf("publication crash hook did not kill this process: %v", err)
	case "same-candidate":
		if err := enrollAt(source, 9, paths); err != nil {
			t.Fatal(err)
		}
	case "different-candidate":
		err := enrollAt(filepath.Join(root, "different-bundle"), 9, paths)
		requireRuntimeReason(t, err, ReasonContentMismatch)
	default:
		t.Fatal("unknown fixture phase")
	}
}
