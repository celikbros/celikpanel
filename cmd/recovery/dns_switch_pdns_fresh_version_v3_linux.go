//go:build linux

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"

	"github.com/alicelik/celikpanel/internal/hostplatform"
)

// verifyInstalledFreshPDNSNativeVersionV3 is independent, read-only recovery
// attestation. Unknown package or executable provenance leaves the serving
// target pending; it never grants an inverse or silently normalizes SQL.
func verifyInstalledFreshPDNSNativeVersionV3(ctx context.Context) error {
	if ctx == nil || ctx.Err() != nil {
		return errors.New("v3 PowerDNS version check requires a live context")
	}
	profile, err := hostplatform.Detect()
	if err != nil {
		return err
	}
	if profile.ID != "debian" || profile.Version != "13" || profile.Arch != "amd64" ||
		profile.PackageManager != hostplatform.PackageManagerAPT || profile.ServiceManager != hostplatform.ServiceManagerSystemd {
		return errors.New("v3 native PowerDNS transform is measured only on Debian 13 amd64")
	}
	const version = "4.9.17-0+deb13u1"
	for _, name := range []string{"pdns-server", "pdns-backend-sqlite3"} {
		cmd := exec.CommandContext(ctx, "/usr/bin/dpkg-query", "-W", "-f", "${Status}\t${Version}", "--", name)
		cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL=C"}
		raw, err := cmd.Output()
		if err != nil {
			return fmt.Errorf("v3 PowerDNS package %s could not be verified: %w", name, err)
		}
		if string(raw) != "install ok installed\t"+version {
			return fmt.Errorf("v3 measured PowerDNS package %s changed", name)
		}
	}
	if err := verifyFreshPDNSExecutableV3("/usr/sbin/pdns_server", "c11660ad7647fa3b42f0b0f1ace73cfde3765add380a4351889b6c6d86b46d84"); err != nil {
		return err
	}
	return ctx.Err()
}

// The caller supplies only the fixed measured vendor path and digest. Keeping
// the filesystem proof separate allows real-file regression checks without
// installing or changing a native DNS service.
func verifyFreshPDNSExecutableV3(binary, measuredSHA256 string) error {
	info, err := os.Lstat(binary)
	if err != nil {
		return err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Mode().Perm() != 0o755 || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 || st.Uid != 0 || st.Gid != 0 || st.Nlink != 1 {
		return errors.New("v3 PowerDNS executable identity changed")
	}
	file, err := os.Open(binary)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	if hex.EncodeToString(hash.Sum(nil)) != measuredSHA256 {
		return errors.New("v3 PowerDNS executable differs from measured native implementation")
	}
	again, err := file.Stat()
	if err != nil {
		return err
	}
	ast, aok := again.Sys().(*syscall.Stat_t)
	if !aok || ast.Dev != st.Dev || ast.Ino != st.Ino || ast.Size != st.Size || ast.Mtim != st.Mtim || ast.Ctim != st.Ctim {
		return errors.New("v3 PowerDNS executable changed while hashing")
	}
	return nil
}
