//go:build linux

// bundle assembles an offline build artifact. It does not enroll, select or
// execute a recovery runtime on an installed server.
// bundle çevrimdışı derleme paketi hazırlar; kurulu sunucuda kurtarma çalışma
// ortamını kaydetmez, seçmez veya çalıştırmaz.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/alicelik/celikpanel/internal/recoveryruntime"
	"golang.org/x/sys/unix"
)

func main() {
	source := flag.String("source-root", ".", "reviewed source root")
	binaries := flag.String("binary-root", "bin", "matching compiled binaries")
	output := flag.String("output", "bin/recovery-runtime", "offline artifact directory")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "unexpected bundle arguments")
		os.Exit(2)
	}
	digest, err := assemble(*source, *binaries, *output)
	if err != nil {
		fmt.Fprintln(os.Stderr, "recovery bundle: "+err.Error())
		os.Exit(1)
	}
	fmt.Println(digest)
}

func assemble(source, binaries, output string) (string, error) {
	var err error
	for _, path := range []*string{&source, &binaries, &output} {
		*path, err = filepath.Abs(*path)
		if err != nil {
			return "", err
		}
	}
	if filepath.Base(output) != "recovery-runtime" || output == source || output == binaries {
		return "", errors.New("output must be a separate recovery-runtime build directory")
	}
	for _, directory := range []string{source, binaries, filepath.Dir(output)} {
		if err := realDirectoryChain(directory); err != nil {
			return "", err
		}
	}
	contents := map[string][]byte{}
	manifest := recoveryruntime.Manifest{Files: map[string]string{}}
	total := 0
	for _, spec := range recoveryruntime.ExpectedFiles() {
		input := filepath.Join(source, spec.Path)
		if spec.Path == "deploy/recovery/runtime-entry.sh" {
			input = filepath.Join(source, "deploy/release-recovery-runner.sh")
		}
		if strings.HasPrefix(spec.Path, "bin/") {
			input = filepath.Join(binaries, strings.TrimPrefix(spec.Path, "bin/"))
		}
		if err := realDirectoryChain(filepath.Dir(input)); err != nil {
			return "", err
		}
		raw, err := readRegular(input, payloadLimit(spec.Path))
		if err != nil {
			return "", fmt.Errorf("read %s: %w", spec.Path, err)
		}
		total += len(raw)
		if total > recoveryruntime.MaxRuntimeSize {
			return "", errors.New("runtime exceeds size limit")
		}
		contents[spec.Path] = raw
		manifest.Files[spec.Path] = recoveryruntime.Digest(raw)
	}
	rawManifest, err := recoveryruntime.EncodeManifest(manifest)
	if err != nil {
		return "", err
	}
	if _, err := recoveryruntime.ParseManifest(rawManifest); err != nil {
		return "", err
	}
	digest := recoveryruntime.Digest(rawManifest)
	existing := false
	if _, err := os.Lstat(output); err == nil {
		existing = true
		oldDigest, err := verifyArtifact(output)
		if err != nil {
			return "", fmt.Errorf("refuse replacing unrecognized build output: %w", err)
		}
		if oldDigest == digest {
			return digest, nil
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	stage, err := os.MkdirTemp(filepath.Dir(output), ".recovery-runtime.build-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)
	for _, directory := range []string{"bin", "deploy", "deploy/recovery"} {
		if err := os.Mkdir(filepath.Join(stage, directory), 0755); err != nil {
			return "", err
		}
		if err := os.Chmod(filepath.Join(stage, directory), 0755); err != nil {
			return "", err
		}
	}
	for _, spec := range recoveryruntime.ExpectedFiles() {
		path := filepath.Join(stage, spec.Path)
		if err := os.WriteFile(path, contents[spec.Path], spec.Mode); err != nil {
			return "", err
		}
		if err := os.Chmod(path, spec.Mode); err != nil {
			return "", err
		}
	}
	if err := os.WriteFile(filepath.Join(stage, recoveryruntime.ManifestName), rawManifest, 0644); err != nil {
		return "", err
	}
	if err := os.Chmod(filepath.Join(stage, recoveryruntime.ManifestName), 0644); err != nil {
		return "", err
	}
	if err := os.Chmod(stage, 0755); err != nil {
		return "", err
	}
	if got, err := verifyArtifact(stage); err != nil || got != digest {
		return "", errors.New("assembled artifact verification failed")
	}
	backup := ""
	if existing {
		// The old build tree was verified above. Preserve it until the completed
		// replacement is in place; unknown files are never recursively removed.
		backup, err = os.MkdirTemp(filepath.Dir(output), ".recovery-runtime.previous-")
		if err != nil {
			return "", err
		}
		if err := os.Remove(backup); err != nil {
			return "", err
		}
		if err := os.Rename(output, backup); err != nil {
			return "", err
		}
	}
	if err := os.Rename(stage, output); err != nil {
		if backup != "" {
			if restoreErr := os.Rename(backup, output); restoreErr != nil {
				return "", errors.New("build replacement failed; previous artifact retained in staging directory")
			}
		}
		return "", err
	}
	if backup != "" {
		if err := os.RemoveAll(backup); err != nil {
			return "", err
		}
	}
	return digest, nil
}

func payloadLimit(path string) int {
	if strings.HasPrefix(path, "bin/") {
		return recoveryruntime.MaxBinarySize
	}
	return recoveryruntime.MaxScriptSize
}

func realDirectoryChain(path string) error {
	for {
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("input or output parent is not a real directory")
		}
		parent := filepath.Dir(path)
		if parent == path {
			return nil
		}
		path = parent
	}
}

func readRegular(path string, limit int) ([]byte, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	before, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > int64(limit) {
		return nil, errors.New("expected a nonempty bounded regular file")
	}
	raw, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	after, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if len(raw) > limit || int64(len(raw)) != before.Size() || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return nil, errors.New("build input changed during read")
	}
	return raw, nil
}

// Artifact modes differ intentionally from root-private installed kit modes.
// Paket kipleri, kurulu root'a özel çalışma ortamının kiplerinden ayrıdır.
func verifyArtifact(root string) (string, error) {
	if err := realDirectoryChain(root); err != nil {
		return "", err
	}
	raw, err := readRegular(filepath.Join(root, recoveryruntime.ManifestName), recoveryruntime.MaxManifestSize)
	if err != nil {
		return "", err
	}
	manifest, err := recoveryruntime.ParseManifest(raw)
	if err != nil {
		return "", err
	}
	want := map[string]os.FileMode{".": 0755, "bin": 0755, "deploy": 0755, "deploy/recovery": 0755, recoveryruntime.ManifestName: 0644}
	for _, spec := range recoveryruntime.ExpectedFiles() {
		want[spec.Path] = spec.Mode
	}
	seen := map[string]bool{}
	total := 0
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		mode, ok := want[rel]
		if !ok || seen[rel] || info.Mode().Perm() != mode || info.Mode()&(os.ModeSymlink|os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
			return errors.New("unexpected artifact entry or mode")
		}
		seen[rel] = true
		isDirectory := rel == "." || rel == "bin" || rel == "deploy" || rel == "deploy/recovery"
		if info.IsDir() != isDirectory {
			return errors.New("artifact entry type mismatch")
		}
		if !isDirectory && rel != recoveryruntime.ManifestName {
			content, err := readRegular(path, payloadLimit(rel))
			if err != nil {
				return err
			}
			total += len(content)
			if total > recoveryruntime.MaxRuntimeSize || recoveryruntime.Digest(content) != manifest.Files[rel] {
				return errors.New("artifact payload differs from manifest")
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(seen) != len(want) {
		return "", errors.New("artifact inventory incomplete")
	}
	return recoveryruntime.Digest(raw), nil
}
