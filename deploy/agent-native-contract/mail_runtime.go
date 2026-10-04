package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/alicelik/celikpanel/internal/agentnativecontract"
	"github.com/alicelik/celikpanel/internal/mailrenewalkit"
)

// Offline build inputs only. Installed kit discovery uses the protected runtime
// reader; this producer never scans a host to infer enrollment authority.
func bindBuildMailRuntime(c agentnativecontract.Contract, root string) (agentnativecontract.Contract, error) {
	bad := errors.New("matching complete mail-renewal-runtime build required")
	before, err := os.Lstat(root)
	if err != nil || !before.IsDir() || filepath.Base(root) != "mail-renewal-runtime" {
		return c, bad
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 5 {
		return c, bad
	}
	limits := map[string]int{mailrenewalkit.BinaryName: mailrenewalkit.MaxBinarySize, mailrenewalkit.ManifestName: 4096, mailrenewalkit.ServiceName: 16384, mailrenewalkit.TimerName: 16384, mailrenewalkit.HookName: 16384}
	files := map[string][]byte{}
	for _, entry := range entries {
		limit, ok := limits[entry.Name()]
		if !ok {
			return c, bad
		}
		raw, err := readBuildRuntimeFile(filepath.Join(root, entry.Name()), limit)
		if err != nil {
			return c, err
		}
		files[entry.Name()] = raw
	}
	after, err := os.Lstat(root)
	if err != nil || !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		return c, bad
	}
	manifest := files[mailrenewalkit.ManifestName]
	delete(files, mailrenewalkit.ManifestName)
	return agentnativecontract.BindMailRenewal(c, manifest, files)
}
func readBuildRuntimeFile(path string, limit int) ([]byte, error) {
	bad := errors.New("unsafe or changed mail runtime build input")
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Size() < 1 || before.Size() > int64(limit) {
		return nil, bad
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(before, opened) {
		return nil, bad
	}
	raw, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	after, err := f.Stat()
	current, e := os.Lstat(path)
	if err != nil || e != nil || !os.SameFile(before, current) || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || int64(len(raw)) != before.Size() {
		return nil, bad
	}
	return raw, nil
}
