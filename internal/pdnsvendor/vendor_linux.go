//go:build linux

package pdnsvendor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/alicelik/celikpanel/internal/bindroot"
	"github.com/alicelik/celikpanel/internal/hostplatform"
	"golang.org/x/sys/unix"
)

type boundedOwnerOutput struct{ data []byte }

func (output *boundedOwnerOutput) Write(data []byte) (int, error) {
	if len(output.data)+len(data) > 4<<10 {
		return 0, errors.New("PowerDNS package owner output exceeds its bound")
	}
	output.data = append(output.data, data...)
	return len(data), nil
}

func installedPackageOwner(ctx context.Context) ([]byte, error) {
	const path = "/usr/bin/dpkg-query"
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Mode().Perm()&0o022 != 0 {
		return nil, errors.New("native dpkg-query executable is unavailable or unsafe")
	}
	stat, ok := before.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 {
		return nil, errors.New("native dpkg-query executable is not root-owned")
	}
	queryCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(queryCtx, path, "-S", "--", UnitPath)
	cmd.Env = []string{"PATH=/usr/bin:/usr/sbin", "LC_ALL=C"}
	output := &boundedOwnerOutput{}
	cmd.Stdout = output
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("inspect PowerDNS package owner: %w", err)
	}
	after, err := os.Lstat(path)
	if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() {
		return nil, errors.New("native dpkg-query executable changed during observation")
	}
	afterStat, ok := after.Sys().(*syscall.Stat_t)
	if !ok || afterStat.Uid != 0 || after.Mode().Perm()&0o022 != 0 {
		return nil, errors.New("native dpkg-query ownership changed during observation")
	}
	return output.data, nil
}

func inspectUnitAt(rootFD int) (bindroot.FileIdentity, error) {
	read := func() (bindroot.FileIdentity, error) {
		data, identity, err := bindroot.ReadExactRootOwnedFileAt(rootFD, UnitPath, "PowerDNS vendor unit")
		if err != nil {
			return bindroot.FileIdentity{}, err
		}
		if err := VerifyBytes(data); err != nil {
			return bindroot.FileIdentity{}, err
		}
		return identity, nil
	}
	first, err := read()
	if err != nil {
		return bindroot.FileIdentity{}, err
	}
	second, err := read()
	if err != nil {
		return bindroot.FileIdentity{}, err
	}
	if first != second {
		return bindroot.FileIdentity{}, errors.New("PowerDNS vendor unit changed during observation")
	}
	return second, nil
}

// InspectInstalledUnit observes exact package ownership and reviewed unit
// bytes through secure no-follow reads. It never changes systemd or DNS.
func InspectInstalledUnit(ctx context.Context, profile hostplatform.Profile) (bindroot.FileIdentity, error) {
	if ctx == nil || profile.DistroFamily != hostplatform.DistroFamilyDebian ||
		profile.PackageManager != hostplatform.PackageManagerAPT ||
		profile.ServiceManager != hostplatform.ServiceManagerSystemd {
		return bindroot.FileIdentity{}, errors.New("PowerDNS vendor proof requires a verified APT/systemd host")
	}
	rootFD, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return bindroot.FileIdentity{}, fmt.Errorf("open PowerDNS vendor root: %w", err)
	}
	defer unix.Close(rootFD)
	before, err := installedPackageOwner(ctx)
	if err := VerifyPackageOwner(before, err); err != nil {
		return bindroot.FileIdentity{}, err
	}
	identity, err := inspectUnitAt(rootFD)
	if err != nil {
		return bindroot.FileIdentity{}, err
	}
	after, err := installedPackageOwner(ctx)
	if err := VerifyPackageOwner(after, err); err != nil {
		return bindroot.FileIdentity{}, err
	}
	return identity, nil
}
