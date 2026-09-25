//go:build linux

package dnsenginerecovery

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// NativeCgroupUnitRunner reads the fixed unit's systemd cgroup identity.
type NativeCgroupUnitRunner func(context.Context, string) ([]byte, error)

// NativeCgroupEventsReader reads cgroup v2's recursive populated bit. A
// missing group is distinct from a present empty group.
type NativeCgroupEventsReader func(context.Context, string) ([]byte, bool, error)

// SystemdCgroupUnitRunner never accepts a unit name from persisted evidence.
func SystemdCgroupUnitRunner(ctx context.Context, name string) ([]byte, error) {
	if ctx == nil || (name != "named.service" && name != "pdns.service") {
		return nil, errors.New("unsupported DNS cgroup unit")
	}
	bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(bounded, "/usr/bin/systemctl", "show", name,
		"--no-pager", "-p", "Id", "-p", "Slice", "-p", "ControlGroup")
	output := &boundedUnitOutput{}
	cmd.Stdout = output
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("read DNS unit cgroup identity: %w", err)
	}
	return output.Bytes(), nil
}

// NativeCgroupEvents reads only the canonical service cgroup on a cgroup-v2
// host. It does not follow symlinks or reinterpret a disappearing group as an
// empty one after systemd reported a nonempty ControlGroup.
func NativeCgroupEvents(ctx context.Context, name string) ([]byte, bool, error) {
	if ctx == nil || (name != "named.service" && name != "pdns.service") {
		return nil, false, errors.New("unsupported DNS cgroup events unit")
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	root := "/sys/fs/cgroup"
	if _, err := os.Lstat(filepath.Join(root, "cgroup.controllers")); err != nil {
		return nil, false, fmt.Errorf("cgroup v2 controllers unavailable: %w", err)
	}
	group := filepath.Join(root, "system.slice", name)
	info, err := os.Lstat(group)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, false, errors.New("DNS service cgroup is not a directory")
	}
	fd, err := unix.Open(filepath.Join(group, "cgroup.events"), unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, false, fmt.Errorf("open DNS cgroup events: %w", err)
	}
	file := os.NewFile(uintptr(fd), "cgroup.events")
	defer file.Close()
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return nil, false, errors.New("DNS cgroup events are not a regular kernel file")
	}
	raw, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil || len(raw) > 4096 {
		return nil, false, errors.New("DNS cgroup events could not be read within bound")
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	return raw, true, nil
}

func parseDNSCgroupIdentity(name string, raw []byte) (string, error) {
	if len(raw) > 4096 {
		return "", errors.New("DNS cgroup identity exceeds bound")
	}
	fields := make(map[string]string, 3)
	for _, line := range strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok || (key != "Id" && key != "Slice" && key != "ControlGroup") {
			return "", errors.New("unexpected DNS cgroup identity property")
		}
		if _, exists := fields[key]; exists {
			return "", errors.New("duplicate DNS cgroup identity property")
		}
		fields[key] = value
	}
	canonical := "/system.slice/" + name
	if len(fields) != 3 || fields["Id"] != name || fields["Slice"] != "system.slice" ||
		(fields["ControlGroup"] != "" && fields["ControlGroup"] != canonical) {
		return "", errors.New("DNS service cgroup identity differs from the fixed system slice")
	}
	return fields["ControlGroup"], nil
}

func parseDNSCgroupEvents(raw []byte) error {
	if len(raw) > 4096 {
		return errors.New("DNS cgroup events exceed bound")
	}
	seen := make(map[string]bool, 2)
	for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte{'\n'}) {
		fields := bytes.Fields(line)
		if len(fields) != 2 {
			return errors.New("malformed DNS cgroup event")
		}
		key, value := string(fields[0]), string(fields[1])
		if (key != "populated" && key != "frozen") || seen[key] || (value != "0" && value != "1") {
			return errors.New("unexpected DNS cgroup event")
		}
		seen[key] = true
		if key == "populated" && value != "0" {
			return errors.New("DNS service cgroup still contains processes")
		}
	}
	if !seen["populated"] {
		return errors.New("DNS cgroup population is unknown")
	}
	return nil
}

// ProbeEmptyUnitCgroup is a point-in-time extra condition for stopped DNS
// targets. The caller must repeat it as part of each stopped-unit observation;
// it does not exclude an independent owner restart after the final read.
func ProbeEmptyUnitCgroup(ctx context.Context, name string, show NativeCgroupUnitRunner, read NativeCgroupEventsReader) error {
	if ctx == nil || show == nil || read == nil || (name != "named.service" && name != "pdns.service") {
		return errors.New("invalid DNS cgroup proof")
	}
	raw, err := show(ctx, name)
	if err != nil {
		return err
	}
	controlGroup, err := parseDNSCgroupIdentity(name, raw)
	if err != nil {
		return err
	}
	events, exists, err := read(ctx, name)
	if err != nil {
		return err
	}
	if !exists {
		if controlGroup != "" {
			return errors.New("DNS cgroup disappeared while systemd still reports it")
		}
		return ctx.Err()
	}
	if err := parseDNSCgroupEvents(events); err != nil {
		return err
	}
	return ctx.Err()
}
