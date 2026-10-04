//go:build linux

package dnsenginerecovery

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/alicelik/celikpanel/internal/processidentity"
)

// ProbeLocalDNSListeners extends a public port-53 authority proof to the
// loopback and link-local sockets that proof skips. Every such socket must
// belong to the verified source daemon (sourceProcess with sourcePID; both
// empty when no source daemon may hold one) or to the systemd-resolved stub
// on 127.0.0.53/127.0.0.54, proved by its PID's cgroup, not by its name.
// Accepted stub listeners are returned and added to a context record made by
// WithLocalDNSListenerRecord. It is a point-in-time observation.
func ProbeLocalDNSListeners(
	ctx context.Context, sourceProcess string, sourcePID uint64,
	runner BINDListenerRunner, cgroup ProcessCgroupReader,
) ([]string, error) {
	return verifyLocalDNSListeners(ctx, sourceProcess, sourcePID, runner, cgroup)
}

// NativeProcessUnifiedCgroup reads /proc/PID/cgroup from the kernel procfs and
// returns the cgroup-v2 ("0::") path, bound to one process start identity: the
// start token must match before and after the read.
func NativeProcessUnifiedCgroup(ctx context.Context, pid uint64) (string, error) {
	return processUnifiedCgroup(ctx, "/proc", pid, verifyNamedScanProcFS, func(pid uint64) (string, error) {
		return processidentity.StartToken(int(pid))
	})
}

func processUnifiedCgroup(
	ctx context.Context, root string, pid uint64,
	verify func(string) error, start func(uint64) (string, error),
) (string, error) {
	if ctx == nil || verify == nil || start == nil || pid == 0 || uint64(int(pid)) != pid {
		return "", errors.New("invalid process cgroup observation")
	}
	if err := verify(root); err != nil {
		return "", err
	}
	before, err := start(pid)
	if err != nil {
		return "", fmt.Errorf("read process %d start identity: %w", pid, err)
	}
	file, err := os.Open(root + "/" + strconv.FormatUint(pid, 10) + "/cgroup")
	if err != nil {
		return "", fmt.Errorf("read process %d cgroup: %w", pid, err)
	}
	raw, err := io.ReadAll(io.LimitReader(file, 4097))
	file.Close()
	if err != nil || len(raw) > 4096 {
		return "", errors.Join(fmt.Errorf("read process %d cgroup within its bound", pid), err)
	}
	after, err := start(pid)
	if err != nil || after != before {
		return "", errors.Join(fmt.Errorf("process %d changed while its cgroup was read", pid), err)
	}
	if err := verify(root); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return parseUnifiedCgroup(raw)
}

// parseUnifiedCgroup returns the single cgroup-v2 path of /proc/PID/cgroup.
// On a hybrid host the v1 lines are ignored; the unified line is what systemd
// places each unit's processes in.
func parseUnifiedCgroup(raw []byte) (string, error) {
	path := ""
	found := false
	for _, line := range strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n") {
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 {
			return "", errors.New("malformed process cgroup record")
		}
		if parts[0] != "0" || parts[1] != "" {
			continue
		}
		if found || !strings.HasPrefix(parts[2], "/") || strings.ContainsAny(parts[2], "\x00\r\t ") {
			return "", errors.New("process cgroup-v2 record is ambiguous")
		}
		path, found = parts[2], true
	}
	if !found {
		return "", errors.New("process has no cgroup-v2 record")
	}
	return path, nil
}
