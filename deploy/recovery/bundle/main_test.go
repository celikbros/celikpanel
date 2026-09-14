//go:build linux

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/recoveryruntime"
	"golang.org/x/sys/unix"
)

func bundleFixture(t *testing.T) (string, string, string) {
	t.Helper()
	root := t.TempDir()
	source, binaries := filepath.Join(root, "source"), filepath.Join(root, "bin")
	for _, directory := range []string{source, binaries} {
		if err := os.Mkdir(directory, 0755); err != nil {
			t.Fatal(err)
		}
	}
	for _, spec := range recoveryruntime.ExpectedFiles() {
		path := filepath.Join(source, spec.Path)
		if spec.Path == "deploy/recovery/runtime-entry.sh" {
			path = filepath.Join(source, "deploy/release-recovery-runner.sh")
		}
		if filepath.Dir(spec.Path) == "bin" {
			path = filepath.Join(binaries, filepath.Base(spec.Path))
		}
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("fixture payload: "+spec.Path+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return source, binaries, filepath.Join(root, "recovery-runtime")
}

func TestBundleUsesSharedInventoryAndPreservesIdenticalOutput(t *testing.T) {
	source, binaries, output := bundleFixture(t)
	digest, err := assemble(source, binaries, output)
	if err != nil {
		t.Fatal(err)
	}
	if !recoveryruntime.ValidDigest(digest) {
		t.Fatal("invalid manifest digest")
	}
	got, err := verifyArtifact(output)
	if err != nil || got != digest {
		t.Fatalf("artifact proof: %q %v", got, err)
	}
	before := bundleInventory(t, output)
	second, err := assemble(source, binaries, output)
	if err != nil || second != digest {
		t.Fatalf("repeat: %q %v", second, err)
	}
	if after := bundleInventory(t, output); !reflect.DeepEqual(before, after) {
		t.Fatal("identical assembly changed artifact")
	}
	if err := os.WriteFile(filepath.Join(binaries, "agent-checker"), []byte("new matching checker\n"), 0600); err != nil {
		t.Fatal(err)
	}
	changed, err := assemble(source, binaries, output)
	if err != nil || changed == digest {
		t.Fatalf("replacement: %q %v", changed, err)
	}
	if _, err := verifyArtifact(output); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Dir(output))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("staging artifact leaked: %v", entries)
	}
}

func TestBundleRefusesUnknownOutputAndUnsafeInput(t *testing.T) {
	for _, kind := range []string{"extra-output", "changed-output", "symlink-input", "FIFO-input", "symlink-parent", "missing-input"} {
		t.Run(kind, func(t *testing.T) {
			source, binaries, output := bundleFixture(t)
			if _, err := assemble(source, binaries, output); err != nil {
				t.Fatal(err)
			}
			input := filepath.Join(binaries, "agent-checker")
			switch kind {
			case "extra-output":
				if err := os.WriteFile(filepath.Join(output, "owner-file"), []byte("owner state"), 0600); err != nil {
					t.Fatal(err)
				}
			case "changed-output":
				if err := os.WriteFile(filepath.Join(output, "bin", "agent-checker"), []byte("owner change"), 0755); err != nil {
					t.Fatal(err)
				}
			case "symlink-input", "FIFO-input", "missing-input":
				if err := os.Remove(input); err != nil {
					t.Fatal(err)
				}
				if kind == "symlink-input" {
					if err := os.Symlink(filepath.Join(binaries, "recovery"), input); err != nil {
						t.Fatal(err)
					}
				}
				if kind == "FIFO-input" {
					if err := unix.Mkfifo(input, 0600); err != nil {
						t.Fatal(err)
					}
				}
			case "symlink-parent":
				alias := filepath.Join(filepath.Dir(output), "alias")
				if err := os.Symlink(binaries, alias); err != nil {
					t.Fatal(err)
				}
				binaries = alias
			}
			before := bundleInventory(t, output)
			if _, err := assemble(source, binaries, output); err == nil {
				t.Fatal("unsafe input or output accepted")
			}
			if after := bundleInventory(t, output); !reflect.DeepEqual(before, after) {
				t.Fatal("failed assembly changed output")
			}
		})
	}
}

func bundleInventory(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		value := info.Mode().String() + "/" + info.ModTime().String()
		if info.Mode().IsRegular() {
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			value += "/" + recoveryruntime.Digest(raw)
		}
		result[path] = value
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return result
}

// Run only the candidate artifact validator, never either bootstrap entrypoint.
func TestCandidateBootstrapArtifactGuards(t *testing.T) {
	repository, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"bootstrap-update.sh", "bootstrap-prebuilt-update.sh"} {
		t.Run(name, func(t *testing.T) {
			script, err := os.ReadFile(filepath.Join(repository, name))
			if err != nil {
				t.Fatal(err)
			}
			start := strings.Index(string(script), "validate_recovery_runtime_artifact() {")
			if start < 0 {
				t.Fatal("candidate validator missing")
			}
			end := strings.Index(string(script[start:]), "\n}")
			if end < 0 {
				t.Fatal("candidate validator incomplete")
			}
			function := string(script[start : start+end+2])
			source, binaries, output := bundleFixture(t)
			if _, err := assemble(source, binaries, output); err != nil {
				t.Fatal(err)
			}
			run := func(want bool) {
				t.Helper()
				command := exec.Command("bash", "-c", "set -eu\ndie() { exit 1; }\n"+function+"\nvalidate_recovery_runtime_artifact \"$1\"", "fixture", filepath.Dir(output))
				raw, err := command.CombinedOutput()
				if (err == nil) != want {
					t.Fatalf("candidate guard success=%v expected=%v: %v %s", err == nil, want, err, raw)
				}
			}
			run(true)
			for _, spec := range recoveryruntime.ExpectedFiles() {
				path := filepath.Join(output, spec.Path)
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				run(false)
				if err := os.WriteFile(path, raw, spec.Mode); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(path, spec.Mode); err != nil {
					t.Fatal(err)
				}
				if spec.Mode == 0755 {
					if err := os.Chmod(path, 0644); err != nil {
						t.Fatal(err)
					}
					run(false)
					if err := os.Chmod(path, spec.Mode); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := os.Remove(filepath.Join(output, recoveryruntime.ManifestName)); err != nil {
				t.Fatal(err)
			}
			run(false)
		})
	}
}
