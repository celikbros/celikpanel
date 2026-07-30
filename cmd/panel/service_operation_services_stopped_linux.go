//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const (
	serviceStopProofTimeout          = 15 * time.Second
	serviceStopProofMaxCgroupBytes   = 1 << 20
	serviceStopProofMaxCgroupDepth   = 64
	serviceStopProofMaxCgroupFolders = 4096
)

var celikPanelServiceUnits = []string{
	"celikpanel-panel.service",
	"celikpanel-agent.service",
}

type systemdUnitStopState struct {
	loadState    string
	activeState  string
	mainPID      uint64
	controlGroup string
}

type serviceStopProofInspector interface {
	readUnitState(context.Context, string) (systemdUnitStopState, error)
	proveControlGroupEmpty(string) error
}

type linuxServiceStopProofInspector struct {
	systemctlPath       string
	cgroupRoot          string
	requireKernelCgroup bool
}

func verifyCelikPanelServicesStoppedPlatform() error {
	systemctlPath, err := findTrustedSystemctl()
	if err != nil {
		return err
	}
	inspector := linuxServiceStopProofInspector{
		systemctlPath:       systemctlPath,
		cgroupRoot:          "/sys/fs/cgroup",
		requireKernelCgroup: true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), serviceStopProofTimeout)
	defer cancel()
	return verifyCelikPanelServicesStoppedWithInspector(ctx, inspector)
}

func verifyCelikPanelServicesStoppedWithInspector(
	ctx context.Context,
	inspector serviceStopProofInspector,
) error {
	if inspector == nil {
		return fmt.Errorf("CelikPanel service stop proof inspector is required")
	}
	for _, unit := range celikPanelServiceUnits {
		first, err := inspector.readUnitState(ctx, unit)
		if err != nil {
			return fmt.Errorf("prove %s is stopped: %w", unit, err)
		}
		if err := validateSystemdUnitStopState(unit, first); err != nil {
			return err
		}
		if first.controlGroup != "" {
			if err := inspector.proveControlGroupEmpty(first.controlGroup); err != nil {
				return fmt.Errorf("prove %s cgroup is empty: %w", unit, err)
			}
		}

		second, err := inspector.readUnitState(ctx, unit)
		if err != nil {
			return fmt.Errorf("re-prove %s is stopped: %w", unit, err)
		}
		if err := validateSystemdUnitStopState(unit, second); err != nil {
			return err
		}
		if first != second {
			return fmt.Errorf("%s stop state changed while it was being proven", unit)
		}
		if second.controlGroup != "" {
			if err := inspector.proveControlGroupEmpty(second.controlGroup); err != nil {
				return fmt.Errorf("re-prove %s cgroup is empty: %w", unit, err)
			}
		}
	}
	return nil
}

func validateSystemdUnitStopState(unit string, state systemdUnitStopState) error {
	if state.loadState != "loaded" {
		return fmt.Errorf("%s must be loaded before its stopped state can be proven; load state is %q", unit, state.loadState)
	}
	switch state.activeState {
	case "inactive", "failed":
	default:
		return fmt.Errorf(
			"%s must be stopped before database snapshot or restore; active state is %q",
			unit,
			state.activeState,
		)
	}
	if state.mainPID != 0 {
		return fmt.Errorf(
			"%s must have MainPID=0 before database snapshot or restore; MainPID=%d",
			unit,
			state.mainPID,
		)
	}
	if state.controlGroup == "" {
		return nil
	}
	if _, err := cleanSystemdControlGroup(state.controlGroup); err != nil {
		return fmt.Errorf("%s returned an unsafe ControlGroup: %w", unit, err)
	}
	if path.Base(state.controlGroup) != unit {
		return fmt.Errorf(
			"%s ControlGroup must end in the exact unit name; got %q",
			unit,
			state.controlGroup,
		)
	}
	return nil
}

func findTrustedSystemctl() (string, error) {
	for _, candidate := range []string{"/usr/bin/systemctl", "/bin/systemctl"} {
		var stat unix.Stat_t
		if err := unix.Lstat(candidate, &stat); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return "", fmt.Errorf("inspect systemctl trust metadata: %w", err)
		}
		if stat.Mode&unix.S_IFMT != unix.S_IFREG ||
			stat.Uid != 0 ||
			stat.Nlink != 1 ||
			stat.Mode&0022 != 0 {
			continue
		}
		return candidate, nil
	}
	return "", fmt.Errorf("a root-owned, single-link, non-writable systemctl is required to prove CelikPanel services are stopped")
}

func (inspector linuxServiceStopProofInspector) readUnitState(
	ctx context.Context,
	unit string,
) (systemdUnitStopState, error) {
	command := exec.CommandContext(
		ctx,
		inspector.systemctlPath,
		"show",
		"--property=LoadState",
		"--property=ActiveState",
		"--property=MainPID",
		"--property=ControlGroup",
		"--",
		unit,
	)
	command.Env = []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"LC_ALL=C",
		"SYSTEMD_COLORS=0",
		"SYSTEMD_PAGER=cat",
	}
	output, err := command.Output()
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return systemdUnitStopState{}, fmt.Errorf("systemctl timed out")
		}
		return systemdUnitStopState{}, fmt.Errorf("systemctl show failed: %w", err)
	}
	return parseSystemdUnitStopState(output)
}

func parseSystemdUnitStopState(output []byte) (systemdUnitStopState, error) {
	values := make(map[string]string, 4)
	for _, line := range strings.Split(strings.TrimSuffix(string(output), "\n"), "\n") {
		line = strings.TrimSuffix(line, "\r")
		key, value, found := strings.Cut(line, "=")
		if !found {
			return systemdUnitStopState{}, fmt.Errorf("systemd returned a malformed unit property")
		}
		switch key {
		case "LoadState", "ActiveState", "MainPID", "ControlGroup":
		default:
			return systemdUnitStopState{}, fmt.Errorf("systemd returned unexpected unit property %q", key)
		}
		if _, duplicate := values[key]; duplicate {
			return systemdUnitStopState{}, fmt.Errorf("systemd returned duplicate unit property %q", key)
		}
		values[key] = value
	}
	for _, key := range []string{"LoadState", "ActiveState", "MainPID", "ControlGroup"} {
		if _, ok := values[key]; !ok {
			return systemdUnitStopState{}, fmt.Errorf("systemd omitted unit property %q", key)
		}
	}
	if values["ActiveState"] == "" || strings.TrimSpace(values["ActiveState"]) != values["ActiveState"] {
		return systemdUnitStopState{}, fmt.Errorf("systemd returned an invalid ActiveState")
	}
	mainPID, err := strconv.ParseUint(values["MainPID"], 10, 64)
	if err != nil || strconv.FormatUint(mainPID, 10) != values["MainPID"] {
		return systemdUnitStopState{}, fmt.Errorf("systemd returned an invalid MainPID")
	}
	return systemdUnitStopState{
		loadState:    values["LoadState"],
		activeState:  values["ActiveState"],
		mainPID:      mainPID,
		controlGroup: values["ControlGroup"],
	}, nil
}

func cleanSystemdControlGroup(controlGroup string) ([]string, error) {
	if controlGroup == "" ||
		len(controlGroup) > 4096 ||
		!strings.HasPrefix(controlGroup, "/") ||
		controlGroup == "/" ||
		path.Clean(controlGroup) != controlGroup {
		return nil, fmt.Errorf("ControlGroup must be a clean, non-root absolute cgroup path")
	}
	components := strings.Split(strings.TrimPrefix(controlGroup, "/"), "/")
	for _, component := range components {
		if component == "" || component == "." || component == ".." || len(component) > 255 {
			return nil, fmt.Errorf("ControlGroup contains an unsafe path component")
		}
		for _, character := range component {
			if character < 0x20 || character == 0x7f || character == '\\' {
				return nil, fmt.Errorf("ControlGroup contains an unsafe path character")
			}
		}
	}
	return components, nil
}

func (inspector linuxServiceStopProofInspector) proveControlGroupEmpty(controlGroup string) error {
	components, err := cleanSystemdControlGroup(controlGroup)
	if err != nil {
		return err
	}
	rootFD, err := unix.Open(
		inspector.cgroupRoot,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		return fmt.Errorf("open trusted cgroup root: %w", err)
	}
	defer unix.Close(rootFD)
	if err := validateCgroupRoot(rootFD, inspector.requireKernelCgroup); err != nil {
		return err
	}

	currentFD := rootFD
	for _, component := range components {
		nextFD, openErr := unix.Openat(
			currentFD,
			component,
			unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
			0,
		)
		if currentFD != rootFD {
			unix.Close(currentFD)
		}
		if openErr != nil {
			return fmt.Errorf("open ControlGroup component %q: %w", component, openErr)
		}
		currentFD = nextFD
	}

	folderCount := 1
	return proveCgroupTreeEmpty(currentFD, controlGroup, 0, &folderCount, inspector.requireKernelCgroup)
}

func validateCgroupRoot(fd int, requireKernelCgroup bool) error {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return fmt.Errorf("inspect trusted cgroup root: %w", err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		return fmt.Errorf("trusted cgroup root is not a directory")
	}
	if !requireKernelCgroup {
		return nil
	}
	if stat.Uid != 0 || stat.Gid != 0 || stat.Mode&0022 != 0 {
		return fmt.Errorf("trusted cgroup root must be root-owned and not group/world writable")
	}
	var statfs unix.Statfs_t
	if err := unix.Fstatfs(fd, &statfs); err != nil {
		return fmt.Errorf("inspect trusted cgroup filesystem: %w", err)
	}
	if statfs.Type != unix.CGROUP2_SUPER_MAGIC {
		return fmt.Errorf("trusted cgroup root must be a cgroup v2 filesystem")
	}
	return nil
}

func proveCgroupTreeEmpty(
	directoryFD int,
	label string,
	depth int,
	folderCount *int,
	requireKernelCgroup bool,
) error {
	if depth > serviceStopProofMaxCgroupDepth {
		unix.Close(directoryFD)
		return fmt.Errorf("ControlGroup tree exceeds the maximum safe depth")
	}
	directory := os.NewFile(uintptr(directoryFD), label)
	if directory == nil {
		unix.Close(directoryFD)
		return fmt.Errorf("wrap ControlGroup directory descriptor")
	}
	defer directory.Close()

	if err := proveCgroupProcsEmpty(directoryFD, label, requireKernelCgroup); err != nil {
		return err
	}
	entries, err := directory.ReadDir(-1)
	if err != nil {
		return fmt.Errorf("list ControlGroup %q: %w", label, err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == "." || name == ".." || strings.ContainsRune(name, '/') || strings.ContainsRune(name, 0) {
			return fmt.Errorf("ControlGroup contains an unsafe child name")
		}
		if name == "cgroup.procs" {
			continue
		}
		var stat unix.Stat_t
		if err := unix.Fstatat(directoryFD, name, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return fmt.Errorf("inspect ControlGroup child %q: %w", name, err)
		}
		switch stat.Mode & unix.S_IFMT {
		case unix.S_IFREG:
			continue
		case unix.S_IFDIR:
			*folderCount++
			if *folderCount > serviceStopProofMaxCgroupFolders {
				return fmt.Errorf("ControlGroup tree exceeds the maximum safe directory count")
			}
			childFD, err := unix.Openat(
				directoryFD,
				name,
				unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
				0,
			)
			if err != nil {
				return fmt.Errorf("open ControlGroup child %q: %w", name, err)
			}
			if err := proveCgroupTreeEmpty(
				childFD,
				label+"/"+name,
				depth+1,
				folderCount,
				requireKernelCgroup,
			); err != nil {
				return err
			}
		case unix.S_IFLNK:
			return fmt.Errorf("ControlGroup contains symbolic link %q", name)
		default:
			return fmt.Errorf("ControlGroup contains unsupported child %q", name)
		}
	}
	return nil
}

func proveCgroupProcsEmpty(directoryFD int, label string, requireKernelCgroup bool) error {
	fd, err := unix.Openat(
		directoryFD,
		"cgroup.procs",
		unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		return fmt.Errorf("open %q/cgroup.procs: %w", label, err)
	}
	file := os.NewFile(uintptr(fd), label+"/cgroup.procs")
	if file == nil {
		unix.Close(fd)
		return fmt.Errorf("wrap %q/cgroup.procs descriptor", label)
	}
	defer file.Close()

	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return fmt.Errorf("inspect %q/cgroup.procs: %w", label, err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return fmt.Errorf("%q/cgroup.procs must be a regular file", label)
	}
	if requireKernelCgroup && (stat.Uid != 0 || stat.Gid != 0 || stat.Mode&0022 != 0) {
		return fmt.Errorf("%q/cgroup.procs must be root-owned and not group/world writable", label)
	}
	contents, err := io.ReadAll(io.LimitReader(file, serviceStopProofMaxCgroupBytes+1))
	if err != nil {
		return fmt.Errorf("read %q/cgroup.procs: %w", label, err)
	}
	if len(contents) > serviceStopProofMaxCgroupBytes {
		return fmt.Errorf("%q/cgroup.procs exceeds the maximum safe size", label)
	}
	if len(contents) != 0 {
		return fmt.Errorf("%q/cgroup.procs still contains process IDs", label)
	}
	return nil
}
