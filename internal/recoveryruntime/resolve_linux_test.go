//go:build linux

package recoveryruntime

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

type runtimeFixture struct {
	config       resolveConfig
	root, digest string
}

func newRuntimeFixture(t *testing.T) runtimeFixture {
	t.Helper()
	anchor := t.TempDir()
	if err := os.Chmod(anchor, 0700); err != nil {
		t.Fatal(err)
	}
	config := resolveConfig{runtimeRoot: filepath.Join(anchor, "runtimes", "v1"), selectionPath: filepath.Join(anchor, "state", "recovery-runtime.v1"), anchor: anchor, uid: uint32(os.Geteuid()), gid: uint32(os.Getegid())}
	for _, path := range []string{config.runtimeRoot, filepath.Dir(config.selectionPath)} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	manifest := Manifest{Files: map[string]string{}}
	payloads := map[string][]byte{}
	for _, spec := range ExpectedFiles() {
		payloads[spec.Path] = []byte("immutable fixture " + spec.Path + "\n")
		manifest.Files[spec.Path] = Digest(payloads[spec.Path])
	}
	raw, err := EncodeManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	digest := Digest(raw)
	root := filepath.Join(config.runtimeRoot, digest)
	for _, path := range []string{root, filepath.Join(root, "bin"), filepath.Join(root, "deploy", "recovery")} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, spec := range ExpectedFiles() {
		if err := os.WriteFile(filepath.Join(root, spec.Path), payloads[spec.Path], spec.Mode); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, ManifestName), raw, 0600); err != nil {
		t.Fatal(err)
	}
	selection, _ := EncodeSelection(digest)
	if err := os.WriteFile(config.selectionPath, selection, 0600); err != nil {
		t.Fatal(err)
	}
	return runtimeFixture{config: config, root: root, digest: digest}
}
func requireRuntimeReason(t *testing.T, err error, want Reason) {
	t.Helper()
	var bounded *Error
	if !errors.As(err, &bounded) || bounded.Reason != want || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("reason=%v expected=%s", err, want)
	}
	if strings.Contains(err.Error(), "/") {
		t.Fatal("error exposes a file path")
	}
}
func snapshotRuntimeFixture(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		stat := info.Sys().(*syscall.Stat_t)
		key, _ := filepath.Rel(root, path)
		// The resolver uses O_NOATIME for both payload reads and inventories.
		result[key] = fmt.Sprintf("%d:%d:%d:%d:%d:%d:%d:%v:%v", stat.Dev, stat.Ino, stat.Mode, stat.Uid, stat.Gid, stat.Nlink, stat.Size, stat.Mtim, stat.Ctim)
		if info.Mode().IsRegular() {
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			result[key] += "|" + Digest(raw)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func TestResolvePinsExactKitWithoutChangingEvidence(t *testing.T) {
	fixture := newRuntimeFixture(t)
	before := snapshotRuntimeFixture(t, fixture.config.anchor)
	runtime, err := resolveAt(fixture.config)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Root != fixture.root || runtime.Digest != fixture.digest {
		t.Fatal("resolved wrong runtime identity")
	}
	if err := runtime.Revalidate(); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	requireRuntimeReason(t, runtime.Revalidate(), ReasonChanged)
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	after := snapshotRuntimeFixture(t, fixture.config.anchor)
	if len(before) != len(after) {
		t.Fatal("resolver altered inventory")
	}
	for path, value := range before {
		if value != after[path] {
			t.Fatalf("resolver changed evidence %s", path)
		}
	}
}
func TestResolveRejectsMissingUnsafeOrChangedKit(t *testing.T) {
	cases := []struct {
		name   string
		reason Reason
		change func(*testing.T, runtimeFixture)
	}{
		{"no selection", ReasonNotSelected, func(t *testing.T, f runtimeFixture) { mustRemoveRuntimeFile(t, f.config.selectionPath) }},
		{"selector mode", ReasonUnsafeMetadata, func(t *testing.T, f runtimeFixture) { mustRuntimeChmod(t, f.config.selectionPath, 0644) }},
		{"selector syntax", ReasonInvalidSelection, func(t *testing.T, f runtimeFixture) {
			mustRuntimeWrite(t, f.config.selectionPath, []byte("runtime=latest\n"), 0600)
		}},
		{"kit mode", ReasonUnsafeMetadata, func(t *testing.T, f runtimeFixture) { mustRuntimeChmod(t, f.root, 0755) }},
		{"subdirectory mode", ReasonUnsafeMetadata, func(t *testing.T, f runtimeFixture) { mustRuntimeChmod(t, filepath.Join(f.root, "bin"), 0755) }},
		{"writable parent", ReasonUnsafeMetadata, func(t *testing.T, f runtimeFixture) { mustRuntimeChmod(t, f.config.runtimeRoot, 0770) }},
		{"file mode", ReasonUnsafeMetadata, func(t *testing.T, f runtimeFixture) { mustRuntimeChmod(t, filepath.Join(f.root, "bin/recovery"), 0644) }},
		{"setuid", ReasonUnsafeMetadata, func(t *testing.T, f runtimeFixture) {
			mustRuntimeChmod(t, filepath.Join(f.root, "bin/recovery"), 0755|os.ModeSetuid)
		}},
		{"content", ReasonContentMismatch, func(t *testing.T, f runtimeFixture) {
			mustRuntimeWrite(t, filepath.Join(f.root, "update.sh"), []byte("changed source bytes\n"), 0755)
		}},
		{"manifest digest", ReasonDigestMismatch, func(t *testing.T, f runtimeFixture) {
			path := filepath.Join(f.root, ManifestName)
			raw, _ := os.ReadFile(path)
			raw[0] = 'X'
			mustRuntimeWrite(t, path, raw, 0600)
		}},
		{"extra file", ReasonInventoryMismatch, func(t *testing.T, f runtimeFixture) {
			mustRuntimeWrite(t, filepath.Join(f.root, "deploy/unreviewed.sh"), []byte("x"), 0600)
		}},
		{"extra directory", ReasonInventoryMismatch, func(t *testing.T, f runtimeFixture) {
			if err := os.Mkdir(filepath.Join(f.root, "extra"), 0700); err != nil {
				t.Fatal(err)
			}
		}},
		{"missing file", ReasonMissing, func(t *testing.T, f runtimeFixture) {
			mustRemoveRuntimeFile(t, filepath.Join(f.root, "bin/agent-checker"))
		}},
		{"hardlink", ReasonUnsafeMetadata, func(t *testing.T, f runtimeFixture) {
			if err := os.Link(filepath.Join(f.root, "bin/recovery"), filepath.Join(f.config.anchor, "extra-link")); err != nil {
				t.Fatal(err)
			}
		}},
		{"file symlink", ReasonUnsafeMetadata, func(t *testing.T, f runtimeFixture) {
			path := filepath.Join(f.root, "update.sh")
			mustRemoveRuntimeFile(t, path)
			if err := os.Symlink("rollback.sh", path); err != nil {
				t.Fatal(err)
			}
		}},
		{"directory symlink", ReasonUnsafeMetadata, func(t *testing.T, f runtimeFixture) {
			path := filepath.Join(f.root, "bin")
			if err := os.Rename(path, path+"-saved"); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("bin-saved", path); err != nil {
				t.Fatal(err)
			}
		}},
		{"fifo", ReasonUnsafeMetadata, func(t *testing.T, f runtimeFixture) {
			path := filepath.Join(f.root, "update.sh")
			mustRemoveRuntimeFile(t, path)
			if err := syscall.Mkfifo(path, 0755); err != nil {
				t.Fatal(err)
			}
		}},
		{"oversized", ReasonUnsafeMetadata, func(t *testing.T, f runtimeFixture) {
			file, err := os.OpenFile(filepath.Join(f.root, "update.sh"), os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			if err := file.Truncate(MaxScriptSize + 1); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRuntimeFixture(t)
			test.change(t, fixture)
			runtime, err := resolveAt(fixture.config)
			if runtime != nil {
				runtime.Close()
				t.Fatal("unsafe kit resolved")
			}
			requireRuntimeReason(t, err, test.reason)
		})
	}
}
func TestResolveChecksOwnerAndGroupIdentity(t *testing.T) {
	fixture := newRuntimeFixture(t)
	fixture.config.uid++
	_, err := resolveAt(fixture.config)
	requireRuntimeReason(t, err, ReasonUnsafeMetadata)
	fixture = newRuntimeFixture(t)
	fixture.config.gid++
	_, err = resolveAt(fixture.config)
	requireRuntimeReason(t, err, ReasonUnsafeMetadata)
}
func TestResolveRejectsChangesDuringProof(t *testing.T) {
	for _, name := range []string{"file swap", "kit swap", "selector swap", "extra file"} {
		t.Run(name, func(t *testing.T) {
			fixture := newRuntimeFixture(t)
			fixture.config.beforeFinalProof = func() {
				switch name {
				case "file swap":
					path := filepath.Join(fixture.root, "rollback.sh")
					raw, _ := os.ReadFile(path)
					mustRemoveRuntimeFile(t, path)
					mustRuntimeWrite(t, path, raw, 0755)
				case "kit swap":
					if err := os.Rename(fixture.root, fixture.root+"-moved"); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(fixture.root, 0700); err != nil {
						t.Fatal(err)
					}
				case "selector swap":
					raw, _ := os.ReadFile(fixture.config.selectionPath)
					mustRemoveRuntimeFile(t, fixture.config.selectionPath)
					mustRuntimeWrite(t, fixture.config.selectionPath, raw, 0600)
				case "extra file":
					mustRuntimeWrite(t, filepath.Join(fixture.root, "late"), []byte("late"), 0600)
				}
			}
			runtime, err := resolveAt(fixture.config)
			if runtime != nil {
				runtime.Close()
				t.Fatal("accepted changed proof")
			}
			if name == "extra file" {
				requireRuntimeReason(t, err, ReasonInventoryMismatch)
			} else {
				requireRuntimeReason(t, err, ReasonChanged)
			}
		})
	}
}
func TestRuntimeRevalidateNeverFollowsNewSelection(t *testing.T) {
	fixture := newRuntimeFixture(t)
	runtime, err := resolveAt(fixture.config)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	other, _ := EncodeSelection(strings.Repeat("a", 64))
	mustRuntimeWrite(t, fixture.config.selectionPath, other, 0600)
	requireRuntimeReason(t, runtime.Revalidate(), ReasonChanged)
	runtime.Root = filepath.Join(fixture.config.anchor, "arbitrary")
	requireRuntimeReason(t, runtime.Revalidate(), ReasonChanged)
}
func TestManifestSourceBytesMustMatchSelectedDigest(t *testing.T) {
	fixture := newRuntimeFixture(t)
	source := filepath.Join(fixture.root, "deploy/release-transaction-guard.sh")
	original, _ := os.ReadFile(source)
	modified := bytes.Replace(original, []byte("immutable"), []byte("MUTATED!!"), 1)
	mustRuntimeWrite(t, source, modified, 0644)
	_, err := resolveAt(fixture.config)
	requireRuntimeReason(t, err, ReasonContentMismatch)
}
func mustRuntimeChmod(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}
func mustRuntimeWrite(t *testing.T, path string, raw []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, raw, mode); err != nil {
		t.Fatal(err)
	}
}
func mustRemoveRuntimeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyBundleSharesProofWithoutRequiringSelection(t *testing.T) {
	fixture := newRuntimeFixture(t)
	mustRemoveRuntimeFile(t, fixture.config.selectionPath)
	bundle, err := verifyBundleAt(fixture.root, false, fixture.config)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Digest != fixture.digest {
		t.Fatal("bundle digest differs from its exact manifest")
	}
	if err := bundle.Revalidate(); err != nil {
		t.Fatal(err)
	}
	bundle.Close()
	_, err = verifyBundleAt(fixture.root, true, fixture.config)
	requireRuntimeReason(t, err, ReasonUnsafeMetadata)
	for _, path := range []string{fixture.root, filepath.Join(fixture.root, "bin"), filepath.Join(fixture.root, "deploy"), filepath.Join(fixture.root, "deploy/recovery")} {
		mustRuntimeChmod(t, path, 0755)
	}
	mustRuntimeChmod(t, filepath.Join(fixture.root, ManifestName), 0644)
	packagedPath := filepath.Join(fixture.config.anchor, "packaged-runtime")
	if err := os.Rename(fixture.root, packagedPath); err != nil {
		t.Fatal(err)
	}
	bundle, err = verifyBundleAt(packagedPath, true, fixture.config)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Root != packagedPath || bundle.Digest != fixture.digest {
		t.Fatal("packaged bundle lost manifest identity")
	}
	if err := bundle.Revalidate(); err != nil {
		t.Fatal(err)
	}
	mustRuntimeWrite(t, filepath.Join(packagedPath, "deploy/recovery/runtime-entry.sh"), []byte("modified"), 0755)
	requireRuntimeReason(t, bundle.Revalidate(), ReasonChanged)
	bundle.Close()
	_, err = verifyBundleAt(packagedPath, false, fixture.config)
	requireRuntimeReason(t, err, ReasonUnsafeMetadata)
}

func TestVerifyBundleRejectsUnsupportedManifestEvenWithMatchingIdentity(t *testing.T) {
	fixture := newRuntimeFixture(t)
	manifestPath := filepath.Join(fixture.root, ManifestName)
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	raw = bytes.Replace(raw, []byte("protocol=1"), []byte("protocol=2"), 1)
	mustRuntimeWrite(t, manifestPath, raw, 0600)
	newDigest := Digest(raw)
	newRoot := filepath.Join(fixture.config.runtimeRoot, newDigest)
	if err := os.Rename(fixture.root, newRoot); err != nil {
		t.Fatal(err)
	}
	selection, _ := EncodeSelection(newDigest)
	mustRuntimeWrite(t, fixture.config.selectionPath, selection, 0600)
	_, err = resolveAt(fixture.config)
	requireRuntimeReason(t, err, ReasonUnsupported)
	_, err = verifyBundleAt(newRoot, false, fixture.config)
	requireRuntimeReason(t, err, ReasonUnsupported)
}

func TestResolveRejectsPayloadOwnerChanges(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root only to construct a foreign-owned fixture file")
	}
	for _, target := range []string{"bin/recovery", "deploy/release-transaction-guard.sh", ManifestName} {
		t.Run(target, func(t *testing.T) {
			fixture := newRuntimeFixture(t)
			if err := os.Chown(filepath.Join(fixture.root, target), 0, 12345); err != nil {
				t.Fatal(err)
			}
			_, err := resolveAt(fixture.config)
			requireRuntimeReason(t, err, ReasonUnsafeMetadata)
		})
	}
}
