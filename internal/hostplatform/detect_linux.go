//go:build linux

package hostplatform

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

var trustedExecutableDirs = []string{"/usr/bin", "/usr/sbin", "/bin", "/sbin"}

func Detect() (Profile, error) {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return Profile{}, fmt.Errorf("read /etc/os-release: %w", err)
	}
	return DetectWith(data, Probe{
		LookPath: trustedLookPath,
		ValidateExecutable: func(_ string, path string) error {
			return validateTrustedExecutable(path)
		},
		SystemdReady: func(systemctl string) error {
			if err := validateRootOwnedPathChain("/run/systemd"); err != nil {
				return err
			}
			info, err := os.Lstat("/run/systemd/private")
			if err != nil {
				return err
			}
			if info.Mode()&os.ModeSocket == 0 {
				return fmt.Errorf("/run/systemd/private is not a socket")
			}
			stat, ok := info.Sys().(*syscall.Stat_t)
			if !ok || stat.Uid != 0 || info.Mode().Perm()&0o022 != 0 {
				return fmt.Errorf("/run/systemd/private is not root-owned and private")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()
			out, err := exec.CommandContext(ctx, systemctl, "is-system-running").CombinedOutput()
			if ctx.Err() != nil {
				return fmt.Errorf("systemd readiness probe timed out: %w", ctx.Err())
			}
			state := strings.TrimSpace(string(out))
			if state == "running" || state == "degraded" {
				return nil
			}
			if err != nil {
				return fmt.Errorf("systemd state %q: %w", state, err)
			}
			return fmt.Errorf("systemd state %q", state)
		},
		Architecture: func() (string, error) {
			return runtime.GOARCH, nil
		},
	})
}

func trustedLookPath(name string) (string, error) {
	if filepath.Base(name) != name || name == "." || name == ".." {
		return "", fmt.Errorf("invalid executable name %q", name)
	}
	for _, dir := range trustedExecutableDirs {
		path := filepath.Join(dir, name)
		canonical, err := filepath.EvalSymlinks(path)
		if err != nil {
			continue
		}
		if err := validateTrustedExecutable(canonical); err == nil {
			return canonical, nil
		}
	}
	return "", fmt.Errorf("not found in trusted system directories")
}

func validateTrustedExecutable(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("executable path %q is not canonical and absolute", path)
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	if canonical != path {
		return fmt.Errorf("executable path %q is not canonical", path)
	}
	trustedDir := false
	for _, dir := range trustedExecutableDirs {
		canonicalDir, err := filepath.EvalSymlinks(dir)
		if err != nil || filepath.Dir(canonical) != canonicalDir {
			continue
		}
		if err := validateRootOwnedPathChain(canonicalDir); err != nil {
			return err
		}
		trustedDir = true
		break
	}
	if !trustedDir {
		return fmt.Errorf("path %q is outside trusted system directories", canonical)
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("path %q is not a regular executable", canonical)
	}
	return validateRootOwnedNotWritable(canonical, info)
}

func validateRootOwnedPathChain(path string) error {
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) {
		return fmt.Errorf("path %q is not absolute", path)
	}
	var chain []string
	for current := path; ; current = filepath.Dir(current) {
		chain = append(chain, current)
		if parent := filepath.Dir(current); parent == current {
			break
		}
	}
	for i := len(chain) - 1; i >= 0; i-- {
		info, err := os.Stat(chain[i])
		if err != nil {
			return err
		}
		if err := validateRootOwnedNotWritable(chain[i], info); err != nil {
			return err
		}
	}
	return nil
}

func validateRootOwnedNotWritable(path string, info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 {
		return fmt.Errorf("path %q is not root-owned", path)
	}
	if info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("path %q is group/world-writable", path)
	}
	return nil
}
