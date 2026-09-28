//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

type pdnsPrimaryNativeBinding struct {
	PID               uint64
	Start             string
	Executable        string
	DatabaseDevice    uint64
	DatabaseInode     uint64
	DatabaseSize      int64
	DatabaseMode      uint32
	DatabaseUID       uint32
	DatabaseGID       uint32
	DatabaseMtimeSec  int64
	DatabaseMtimeNSec int64
	DatabaseCtimeSec  int64
	DatabaseCtimeNSec int64
}

func recheckPDNSNativeBindingAt(read func() (pdnsPrimaryNativeBinding, error), check func() error) error {
	if read == nil || check == nil {
		return errors.New("PowerDNS native binding proof is unavailable")
	}
	before, err := read()
	if err != nil || before.PID == 0 || before.Start == "" || before.DatabaseInode == 0 {
		return errors.New("PowerDNS native process/database binding is unknown")
	}
	if err := check(); err != nil {
		return err
	}
	after, err := read()
	if err != nil || before != after {
		return errors.New("PowerDNS native process/database binding changed")
	}
	return nil
}

func readPDNSPrimaryNativeBinding(ctx context.Context, systemctl string) (pdnsPrimaryNativeBinding, error) {
	var result pdnsPrimaryNativeBinding
	processes, err := inspectDNSUnitProcesses(ctx, systemctl, "pdns.service")
	if err != nil || processes.MainPID == 0 || processes.ControlPID != 0 ||
		processes.SubState != "running" || processes.MainPID > uint64(^uint(0)>>1) {
		return result, errors.New("PowerDNS native service process is unknown")
	}
	pid := int(processes.MainPID)
	start, err := serviceMutationProcessStartIdentity(pid)
	if err != nil || start == "" {
		return result, errors.New("PowerDNS native process start is unknown")
	}
	executable, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil || (executable != "/usr/bin/pdns_server" && executable != "/usr/sbin/pdns_server") {
		return result, errors.New("PowerDNS native process executable is unknown")
	}
	path := pdnsDBPath()
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return result, errors.New("PowerDNS native database path is invalid")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 {
		return result, errors.New("PowerDNS native database file is untrusted")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Ino == 0 || stat.Nlink != 1 {
		return result, errors.New("PowerDNS native database inode is invalid")
	}
	fds, err := os.ReadDir(fmt.Sprintf("/proc/%d/fd", pid))
	if err != nil {
		return result, errors.New("PowerDNS native database descriptors are unavailable")
	}
	found := false
	for _, fd := range fds {
		if _, parseErr := strconv.ParseUint(fd.Name(), 10, 32); parseErr != nil {
			return result, errors.New("PowerDNS native descriptor list is invalid")
		}
		fdPath := fmt.Sprintf("/proc/%d/fd/%s", pid, fd.Name())
		target, linkErr := os.Readlink(fdPath)
		if linkErr != nil || (target != path && target != path+" (deleted)") {
			continue
		}
		fdInfo, fdErr := os.Stat(fdPath)
		if fdErr != nil {
			return result, errors.New("PowerDNS native database descriptor changed")
		}
		fdStat, ok := fdInfo.Sys().(*syscall.Stat_t)
		if !ok || fdStat.Ino != stat.Ino || fdStat.Dev != stat.Dev || strings.HasSuffix(target, " (deleted)") {
			return result, errors.New("PowerDNS native database descriptor differs from its path")
		}
		found = true
	}
	if !found {
		return result, errors.New("PowerDNS native daemon does not hold the managed database")
	}
	if current, err := serviceMutationProcessStartIdentity(pid); err != nil || current != start {
		return result, errors.New("PowerDNS native process changed during database inspection")
	}
	return pdnsPrimaryNativeBinding{PID: processes.MainPID, Start: start,
		Executable: executable, DatabaseDevice: uint64(stat.Dev), DatabaseInode: stat.Ino,
		DatabaseSize: stat.Size, DatabaseMode: stat.Mode, DatabaseUID: stat.Uid, DatabaseGID: stat.Gid,
		DatabaseMtimeSec: stat.Mtim.Sec, DatabaseMtimeNSec: stat.Mtim.Nsec,
		DatabaseCtimeSec: stat.Ctim.Sec, DatabaseCtimeNSec: stat.Ctim.Nsec}, nil
}
