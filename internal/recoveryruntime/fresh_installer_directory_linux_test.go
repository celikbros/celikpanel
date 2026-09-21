//go:build linux

package recoveryruntime

import (
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

// This crosses the real fresh-installer, packaged-runtime enrollment and shell
// start-helper contracts with initially absent shared directories. Only the
// unrelated foundation manifest and systemd publication are omitted.
func TestFreshInstallerEnrollmentSharedDirectory(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("native root fixture")
	}
	directory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repository := filepath.Clean(filepath.Join(directory, "../.."))
	for _, scenario := range []string{"absent", "existing-755", "owner-700"} {
		t.Run(scenario, func(t *testing.T) {
			root, err := os.MkdirTemp("/run", "celikpanel-fresh-install-test-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(root)
			source := filepath.Join(root, "source")
			for _, dir := range []string{source, filepath.Join(source, "deploy"), filepath.Join(root, "usr")} {
				if err := os.Mkdir(dir, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(dir, 0755); err != nil {
					t.Fatal(err)
				}
			}
			for _, name := range []string{"release-transaction-guard.sh", "release-transaction-start-guard.sh"} {
				raw, err := os.ReadFile(filepath.Join(repository, "deploy", name))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(source, "deploy", name), raw, 0644); err != nil {
					t.Fatal(err)
				}
			}
			bundle := filepath.Join(source, "recovery-runtime")
			makePackagedKit(t, bundle)
			shim := []byte("#!/bin/sh\nexec \"$CP_FRESH_INSTALL_TEST_BINARY\" -test.run=^TestFreshInstallerEnrollmentChild$ -- \"$@\"\n")
			if err := os.WriteFile(filepath.Join(bundle, "bin/recovery"), shim, 0755); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(filepath.Join(bundle, ManifestName))
			if err != nil {
				t.Fatal(err)
			}
			manifest, err := ParseManifest(raw)
			if err != nil {
				t.Fatal(err)
			}
			manifest.Files["bin/recovery"] = Digest(shim)
			raw, err = EncodeManifest(manifest)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(bundle, ManifestName), raw, 0644); err != nil {
				t.Fatal(err)
			}
			shared := filepath.Join(root, "usr/libexec/celikpanel")
			var before map[string]string
			if scenario != "absent" {
				if err := os.MkdirAll(shared, 0755); err != nil {
					t.Fatal(err)
				}
				mode := os.FileMode(0755)
				if scenario == "owner-700" {
					mode = 0700
				}
				if err := os.Chmod(shared, mode); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(shared, "owner-note"), []byte("owner content\n"), 0600); err != nil {
					t.Fatal(err)
				}
				before = snapshotRuntimeFixture(t, shared)
			}
			script := `set -euo pipefail
umask 077
root=$1
repository=$2
SRC=$root/source
TRUSTED_RELEASE_ROOT=$SRC
RELEASE_TRANSACTION_ROOT=$root/transaction
RELEASE_TRANSACTION_RUNTIME_ROOT=$root/run-transaction
UNIT_DIR=$root/units
LIBEXEC_DIR=$root/usr/libexec/celikpanel
RELEASE_TRANSACTION_HELPER=$LIBEXEC_DIR/release-transaction-start-guard
APPLY_ONLY=0
INSTALL_RELEASE_TRANSACTION_FD=
source "$SRC/deploy/release-transaction-guard.sh"
eval "$(sed -n '/^prepare_fresh_release_transaction_foundation() {$/,/^}$/p' "$repository/install.sh")"
die() { printf 'fixture installer stopped: %s\n' "$1" >&2; exit 91; }
preflight_reviewed_release_recovery_foundation() { :; }
publish_reviewed_release_recovery_intent() { :; }
install_release_transaction_guards_with_label_barrier() { _release_txn_install_start_helper "$4"; }
prepare_fresh_release_transaction_foundation
`
			command := exec.Command("/bin/bash", "-c", script, "fresh-installer-fixture", root, repository)
			command.Env = append(os.Environ(), "CP_FRESH_INSTALL_TEST_ROOT="+root, "CP_FRESH_INSTALL_TEST_BINARY="+os.Args[0])
			output, err := command.CombinedOutput()
			if scenario == "owner-700" {
				if err == nil {
					t.Fatal("existing conflicting owner directory was accepted")
				}
				if !reflect.DeepEqual(before, snapshotRuntimeFixture(t, shared)) {
					t.Fatal("existing owner directory was changed")
				}
				if _, err := os.Lstat(filepath.Join(root, "enrollment-entered")); !os.IsNotExist(err) {
					t.Fatal("conflicting shared directory was discovered only after enrollment")
				}
				return
			}
			if err != nil {
				t.Fatalf("fresh installer/enrollment boundary: %v\n%s", err, output)
			}
			for _, item := range []struct {
				path string
				mode os.FileMode
			}{
				{shared, 0755}, {filepath.Join(shared, "recovery-runtimes"), 0700}, {filepath.Join(shared, "recovery-runtimes/v1"), 0700},
			} {
				info, err := os.Stat(item.path)
				if err != nil {
					t.Fatal(err)
				}
				if info.Mode().Perm() != item.mode {
					t.Fatalf("%s: got %04o, want %04o", item.path, info.Mode().Perm(), item.mode)
				}
			}
			if scenario == "existing-755" {
				raw, err := os.ReadFile(filepath.Join(shared, "owner-note"))
				if err != nil || string(raw) != "owner content\n" {
					t.Fatal("owner file changed", err)
				}
			}
			selected, err := resolveAt(resolveConfig{runtimeRoot: filepath.Join(shared, "recovery-runtimes/v1"), selectionPath: filepath.Join(root, "state/selection"), anchor: "/", uid: 0, gid: 0})
			if err != nil {
				t.Fatal(err)
			}
			defer selected.Close()
			if err := verifyLauncherAt(selected, filepath.Join(shared, "recovery")); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestFreshInstallerEnrollmentChild(t *testing.T) {
	root := os.Getenv("CP_FRESH_INSTALL_TEST_ROOT")
	if root == "" {
		t.Skip("only inherited native fresh-installer child")
	}
	source := filepath.Join(root, "source/recovery-runtime")
	if !reflect.DeepEqual(flag.Args(), []string{"enroll-runtime", "--source", source, "--transaction-fd", "9"}) {
		t.Fatal("fresh installer enrollment arguments changed")
	}
	if err := os.WriteFile(filepath.Join(root, "enrollment-entered"), []byte("entered\n"), 0600); err != nil {
		t.Fatal(err)
	}
	shared := filepath.Join(root, "usr/libexec/celikpanel")
	paths := enrollmentPaths{filepath.Join(shared, "recovery-runtimes/v1"), filepath.Join(root, "state/selection"), filepath.Join(shared, "recovery"), filepath.Join(root, "transaction")}
	if err := enrollAt(source, 9, paths); err != nil {
		t.Fatal(err)
	}
}
