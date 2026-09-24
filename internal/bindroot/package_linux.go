//go:build linux

package bindroot

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// ProveInstalledPackage checks the package-owned BIND working directory using
// fixed, root-owned executables and exact bounded output. It never installs or
// changes a package or a dpkg statoverride.
func ProveInstalledPackage(ctx context.Context, layout Layout) error {
	if ctx == nil {
		return errors.New("BIND package proof requires a context")
	}
	proofCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	switch layout {
	case APT:
		owner, err := runTrusted(proofCtx, []string{"/usr/bin/dpkg-query", "/usr/sbin/dpkg-query"}, "-S", "--", "/var/cache/bind")
		if err := VerifyAPTPackageOwner(owner, err); err != nil {
			return err
		}
		override, err := runTrusted(proofCtx, []string{"/usr/sbin/dpkg-statoverride", "/usr/bin/dpkg-statoverride"}, "--list", "/var/cache/bind")
		state, err := ClassifyAPTStatOverride(override, err)
		if err != nil {
			return err
		}
		if state != APTStatOverrideExact {
			return errors.New("/var/cache/bind lacks the exact durable dpkg-statoverride")
		}
		return nil
	case Pacman:
		owner, err := runTrusted(proofCtx, []string{"/usr/bin/pacman", "/usr/sbin/pacman"}, "-Qo", "--", "/var/named")
		return VerifyPacmanPackageOwner(owner, err)
	default:
		return errors.New("unsupported managed BIND generation root")
	}
}

func runTrusted(ctx context.Context, candidates []string, args ...string) ([]byte, error) {
	var selected string
	for _, path := range candidates {
		if err := trustedExecutable(path); err == nil {
			selected = path
			break
		}
	}
	if selected == "" {
		return nil, errors.New("no trusted BIND package executable was found")
	}
	command := exec.CommandContext(ctx, selected, args...)
	command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL=C", "LANG=C"}
	output := &boundedOutput{limit: 4 << 10}
	command.Stdout, command.Stderr = output, output
	err := command.Run()
	if output.exceeded {
		return nil, errors.New("BIND package proof output exceeds limit")
	}
	return output.data, err
}

func trustedExecutable(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("non-canonical package executable path")
	}
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		if err := trustedPath(dir, true); err != nil {
			return err
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	return trustedPath(path, false)
}

func trustedPath(path string, directory bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || info.Mode().Perm()&0o022 != 0 {
		return errors.New("BIND package executable path has unsafe ownership or permissions")
	}
	if directory && !info.IsDir() || !directory && (!info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0) {
		return errors.New("BIND package executable path has unexpected type or mode")
	}
	return nil
}

type boundedOutput struct {
	data     []byte
	limit    int
	exceeded bool
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	n := len(p)
	space := b.limit - len(b.data)
	if n > space {
		b.exceeded = true
		p = p[:space]
	}
	b.data = append(b.data, p...)
	return n, nil
}
