//go:build linux

// bundle prepares an offline artifact. It does not install a helper or unit.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/alicelik/celikpanel/internal/firewallruntime"
	"golang.org/x/sys/unix"
)

func main() {
	binary := flag.String("binary", "bin/firewall-restore", "matching reviewed consumer binary")
	output := flag.String("output", "bin/firewall-runtime", "offline artifact directory")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "unexpected bundle arguments")
		os.Exit(2)
	}
	id, err := assemble(*binary, *output)
	if err != nil {
		fmt.Fprintln(os.Stderr, "firewall bundle:", err)
		os.Exit(1)
	}
	fmt.Println(id)
}

func realParents(path string) error {
	for {
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("build parent is not a real directory")
		}
		parent := filepath.Dir(path)
		if parent == path {
			return nil
		}
		path = parent
	}
}
func readFile(path string, limit int) ([]byte, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	before, err := file.Stat()
	if err != nil {
		return nil, err
	}
	st, ok := before.Sys().(*syscall.Stat_t)
	if !ok || !before.Mode().IsRegular() || st.Nlink != 1 || before.Size() < 1 || before.Size() > int64(limit) {
		return nil, errors.New("unsafe build input")
	}
	raw, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	after, err := file.Stat()
	if err != nil {
		return nil, err
	}
	afterStat, ok := after.Sys().(*syscall.Stat_t)
	if !ok || !os.SameFile(before, after) || before.Size() != after.Size() || before.ModTime() != after.ModTime() || st.Ctim != afterStat.Ctim || st.Mode != afterStat.Mode || st.Nlink != afterStat.Nlink || int64(len(raw)) != before.Size() {
		return nil, errors.New("build input changed")
	}
	return raw, nil
}
func verify(root string) (string, error) {
	if err := realParents(root); err != nil {
		return "", err
	}
	info, err := os.Lstat(root)
	if err != nil || info.Mode().Perm() != 0755 || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return "", errors.New("unsafe artifact root mode")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	want := map[string]os.FileMode{"restore": 0755, "celikpanel-firewall-restore.service": 0644, firewallruntime.ManifestName: 0644}
	if len(entries) != len(want) {
		return "", errors.New("unrecognized artifact entries")
	}
	for _, entry := range entries {
		mode, ok := want[entry.Name()]
		info, err := os.Lstat(filepath.Join(root, entry.Name()))
		if !ok || err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != mode || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
			return "", errors.New("unsafe artifact entry")
		}
	}
	manifest, err := readFile(filepath.Join(root, firewallruntime.ManifestName), 4096)
	if err != nil {
		return "", err
	}
	binary, err := readFile(filepath.Join(root, "restore"), firewallruntime.MaxBinarySize)
	if err != nil {
		return "", err
	}
	unit, err := readFile(filepath.Join(root, "celikpanel-firewall-restore.service"), 16384)
	if err != nil {
		return "", err
	}
	m, err := firewallruntime.Verify(manifest, binary, unit)
	return m.Generation, err
}

func assemble(binary, output string) (string, error) {
	var err error
	binary, err = filepath.Abs(binary)
	if err != nil {
		return "", err
	}
	output, err = filepath.Abs(output)
	if err != nil {
		return "", err
	}
	if filepath.Base(output) != "firewall-runtime" || binary == output || filepath.Dir(binary) == output {
		return "", errors.New("output must be a separate firewall-runtime build directory")
	}
	for _, dir := range []string{filepath.Dir(binary), filepath.Dir(output)} {
		if err = realParents(dir); err != nil {
			return "", err
		}
	}
	payload, err := readFile(binary, firewallruntime.MaxBinarySize)
	if err != nil {
		return "", err
	}
	m, unit, err := firewallruntime.Build(payload)
	if err != nil {
		return "", err
	}
	manifest, err := firewallruntime.Encode(m)
	if err != nil {
		return "", err
	}
	existing := false
	if _, err = os.Lstat(output); err == nil {
		existing = true
		id, e := verify(output)
		if e != nil {
			return "", fmt.Errorf("preserve unrecognized previous build: %w", e)
		}
		if id == m.Generation {
			return id, nil
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	stage, err := os.MkdirTemp(filepath.Dir(output), ".firewall-runtime.build-")
	if err != nil {
		return "", err
	}
	cleanupStage := true
	defer func() {
		if cleanupStage {
			os.RemoveAll(stage)
		}
	}()
	for name, raw := range map[string][]byte{"restore": payload, "celikpanel-firewall-restore.service": unit, firewallruntime.ManifestName: manifest} {
		mode := os.FileMode(0644)
		if name == "restore" {
			mode = 0755
		}
		p := filepath.Join(stage, name)
		if err = os.WriteFile(p, raw, mode); err != nil {
			return "", err
		}
		if err = os.Chmod(p, mode); err != nil {
			return "", err
		}
	}
	if err = os.Chmod(stage, 0755); err != nil {
		return "", err
	}
	if id, e := verify(stage); e != nil {
		return "", fmt.Errorf("verify staged firewall artifact: %w", e)
	} else if id != m.Generation {
		return "", errors.New("staged firewall artifact differs")
	}
	if existing {
		// A build replacement atomically exchanges two fully verified directories.
		// No installed path is used. Unknown entries are never recursively deleted.
		if _, err = verify(output); err != nil {
			return "", err
		}
		if err = unix.Renameat2(unix.AT_FDCWD, stage, unix.AT_FDCWD, output, unix.RENAME_EXCHANGE); err != nil {
			return "", err
		}
		cleanupStage = false // Retain the exact previous build; never recursively delete a swapped-in tree.
	} else if err = unix.Renameat2(unix.AT_FDCWD, stage, unix.AT_FDCWD, output, unix.RENAME_NOREPLACE); err != nil {
		return "", err
	}
	id, err := verify(output)
	if err != nil {
		return "", fmt.Errorf("published build verification failed; preserve output: %w", err)
	}
	if id != m.Generation {
		return "", errors.New("published build differs; preserve output")
	}
	return id, nil
}
