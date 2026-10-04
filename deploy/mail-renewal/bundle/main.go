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

	"github.com/alicelik/celikpanel/internal/mailrenewalkit"
	"golang.org/x/sys/unix"
)

func main() {
	binary := flag.String("binary", "bin/mail-renewal", "matching reviewed consumer binary")
	output := flag.String("output", "bin/mail-renewal-runtime", "offline artifact directory")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "unexpected bundle arguments")
		os.Exit(2)
	}
	id, err := assemble(*binary, *output)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mail renewal bundle:", err)
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
	want := map[string]os.FileMode{mailrenewalkit.BinaryName: 0755, mailrenewalkit.ServiceName: 0644, mailrenewalkit.TimerName: 0644, mailrenewalkit.HookName: 0755, mailrenewalkit.ManifestName: 0644}
	if len(entries) != len(want) {
		return "", errors.New("unknown mail runtime entries; preserve output")
	}
	files := map[string][]byte{}
	var manifest []byte
	for _, entry := range entries {
		mode, ok := want[entry.Name()]
		path := filepath.Join(root, entry.Name())
		info, err := os.Lstat(path)
		if !ok || err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != mode || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
			return "", errors.New("unsafe mail runtime file")
		}
		limit := 16384
		if entry.Name() == mailrenewalkit.BinaryName {
			limit = mailrenewalkit.MaxBinarySize
		}
		raw, err := readFile(path, limit)
		if err != nil {
			return "", err
		}
		if entry.Name() == mailrenewalkit.ManifestName {
			manifest = raw
		} else {
			files[entry.Name()] = raw
		}
	}
	m, err := mailrenewalkit.Verify(manifest, files)
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
	if filepath.Base(output) != "mail-renewal-runtime" || binary == output || filepath.Dir(binary) == output {
		return "", errors.New("output must be a separate mail-renewal-runtime build directory")
	}
	for _, dir := range []string{filepath.Dir(binary), filepath.Dir(output)} {
		if err = realParents(dir); err != nil {
			return "", err
		}
	}
	payload, err := readFile(binary, mailrenewalkit.MaxBinarySize)
	if err != nil {
		return "", err
	}
	m, files, err := mailrenewalkit.Payload(payload)
	if err != nil {
		return "", err
	}
	manifest, err := mailrenewalkit.Encode(m)
	if err != nil {
		return "", err
	}
	files[mailrenewalkit.ManifestName] = manifest
	existing := false
	if _, err = os.Lstat(output); err == nil {
		existing = true
		id, e := verify(output)
		if e != nil {
			return "", fmt.Errorf("preserve previous build: %w", e)
		}
		if id == m.Generation {
			return id, nil
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	stage, err := os.MkdirTemp(filepath.Dir(output), ".mail-renewal-runtime.build-")
	if err != nil {
		return "", err
	}
	// Failed or exchanged stages are retained. Never recursively delete a path
	// which may have acquired owner files or a different generation.
	for name, raw := range files {
		mode := os.FileMode(0644)
		if name == mailrenewalkit.BinaryName || name == mailrenewalkit.HookName {
			mode = 0755
		}
		f, e := os.OpenFile(filepath.Join(stage, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
		if e != nil {
			return "", e
		}
		_, e = f.Write(raw)
		if e == nil {
			e = f.Chmod(mode)
		}
		if e == nil {
			e = f.Sync()
		}
		e = errors.Join(e, f.Close())
		if e != nil {
			return "", e
		}
	}
	if err = os.Chmod(stage, 0755); err != nil {
		return "", err
	}
	if id, e := verify(stage); e != nil || id != m.Generation {
		return "", errors.Join(errors.New("staged mail runtime differs"), e)
	}
	dir, err := os.Open(stage)
	if err != nil {
		return "", err
	}
	err = errors.Join(dir.Sync(), dir.Close())
	if err != nil {
		return "", err
	}
	if existing {
		if _, err = verify(output); err != nil {
			return "", err
		}
		err = unix.Renameat2(unix.AT_FDCWD, stage, unix.AT_FDCWD, output, unix.RENAME_EXCHANGE)
	} else {
		err = unix.Renameat2(unix.AT_FDCWD, stage, unix.AT_FDCWD, output, unix.RENAME_NOREPLACE)
	}
	if err != nil {
		return "", err
	}
	parent, err := os.Open(filepath.Dir(output))
	if err != nil {
		return "", err
	}
	err = errors.Join(parent.Sync(), parent.Close())
	if err != nil {
		return "", err
	}
	id, err := verify(output)
	if err != nil || id != m.Generation {
		return "", errors.Join(errors.New("published mail runtime differs; preserve output"), err)
	}
	return id, nil
}
