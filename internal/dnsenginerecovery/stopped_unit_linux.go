//go:build linux

package dnsenginerecovery

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// ProbeStoppedUnit binds the shared stop predicate to the independent,
// fixed-name systemd readers. It makes no native change and is not recovery
// admission without exact operation, worker, owner and lock proofs.
func ProbeStoppedUnit(ctx context.Context, name string, unitRunner NativeUnitRunner, runtimeRunner BINDRuntimeRunner) error {
	return probeStoppedUnitWithCgroup(ctx, name, unitRunner, runtimeRunner,
		func(proofCtx context.Context, unit string) error {
			return ProbeEmptyUnitCgroup(proofCtx, unit, SystemdCgroupUnitRunner, NativeCgroupEvents)
		})
}

// ProbeStoppedPDNSPersistentMask includes the same native process and cgroup
// proof as ProbeStoppedUnit, but admits only the V3 sealed persistent mask.
func ProbeStoppedPDNSPersistentMask(ctx context.Context) error {
	return probeStoppedUnitWithCgroupClass(ctx, "pdns.service", SystemdUnitRunner, SystemdPDNSRuntimeRunner,
		func(proofCtx context.Context, unit string) error {
			return ProbeEmptyUnitCgroup(proofCtx, unit, SystemdCgroupUnitRunner, NativeCgroupEvents)
		}, true)
}

func probeStoppedUnitWithCgroup(
	ctx context.Context,
	name string,
	unitRunner NativeUnitRunner,
	runtimeRunner BINDRuntimeRunner,
	cgroup func(context.Context, string) error,
) error {
	return probeStoppedUnitWithCgroupClass(ctx, name, unitRunner, runtimeRunner, cgroup, false)
}

// ProbeStoppedUnitWithCgroup is ProbeStoppedUnit with a caller-supplied cgroup
// proof. It applies the same loaded-unit class; the installed adapters pass
// ProbeEmptyUnitCgroup with the fixed systemd and kernel readers.
func ProbeStoppedUnitWithCgroup(
	ctx context.Context,
	name string,
	unitRunner NativeUnitRunner,
	runtimeRunner BINDRuntimeRunner,
	cgroup func(context.Context, string) error,
) error {
	return probeStoppedUnitWithCgroup(ctx, name, unitRunner, runtimeRunner, cgroup)
}

// ProbeStoppedNeverStartedBINDTarget binds VerifyStoppedNeverStartedTarget to
// the same fixed-name unit, process and cgroup readers as ProbeStoppedUnit
// for named.service. The caller selects this class only for a V2
// PowerDNS-to-BIND inverse journal admitted by
// BINDSwitchNeverStartedTargetJournal, and supplies sourceOnly. It makes no
// native change and is not recovery admission.
func ProbeStoppedNeverStartedBINDTarget(
	ctx context.Context,
	unitRunner NativeUnitRunner,
	runtimeRunner BINDRuntimeRunner,
	cgroup func(context.Context, string) error,
	sourceOnly func(context.Context) error,
) (StoppedUnitObservation, error) {
	if ctx == nil || unitRunner == nil || runtimeRunner == nil || cgroup == nil || sourceOnly == nil {
		return StoppedUnitObservation{}, errors.New("invalid never-started BIND target probe")
	}
	return VerifyStoppedNeverStartedTarget(ctx, "named.service",
		stoppedUnitObserver("named.service", unitRunner, runtimeRunner, cgroup), sourceOnly)
}

func probeStoppedUnitWithCgroupClass(
	ctx context.Context,
	name string,
	unitRunner NativeUnitRunner,
	runtimeRunner BINDRuntimeRunner,
	cgroup func(context.Context, string) error,
	persistentMask bool,
) error {
	if ctx == nil || unitRunner == nil || runtimeRunner == nil || cgroup == nil ||
		(name != "named.service" && name != "pdns.service") {
		return errors.New("invalid independent DNS stopped-unit probe")
	}
	observe := stoppedUnitObserver(name, unitRunner, runtimeRunner, cgroup)
	if persistentMask {
		return VerifyStoppedPDNSPersistentMask(ctx, observe)
	}
	return VerifyStoppedUnit(ctx, name, observe)
}

func stoppedUnitObserver(
	name string,
	unitRunner NativeUnitRunner,
	runtimeRunner BINDRuntimeRunner,
	cgroup func(context.Context, string) error,
) func(context.Context) (StoppedUnitObservation, error) {
	return func(proofCtx context.Context) (StoppedUnitObservation, error) {
		units, err := ProbeNativeUnits(proofCtx, []string{name}, unitRunner)
		if err != nil {
			return StoppedUnitObservation{}, fmt.Errorf("read DNS target unit: %w", err)
		}
		raw, err := runtimeRunner(proofCtx, name)
		if err != nil {
			return StoppedUnitObservation{}, fmt.Errorf("read DNS target process state: %w", err)
		}
		processes, err := parseUnitRuntime(raw)
		if err != nil {
			return StoppedUnitObservation{}, fmt.Errorf("decode DNS target process state: %w", err)
		}
		if err := cgroup(proofCtx, name); err != nil {
			return StoppedUnitObservation{}, fmt.Errorf("prove DNS target cgroup empty: %w", err)
		}
		unit := units[0]
		return StoppedUnitObservation{
			Name: unit.Name, LoadState: unit.LoadState,
			ActiveState: unit.ActiveState, UnitFileState: unit.UnitFileState,
			MainPID: processes.MainPID, ControlPID: processes.ControlPID,
			SubState: processes.SubState,
		}, nil
	}
}

// ProbeNoNamedProcess scans the kernel procfs once and refuses any process
// whose kernel command name is exactly "named", inside or outside a systemd
// unit. A process that exits during the scan is skipped. It is a point-in-time
// observation used beside a stopped never-started BIND target; it cannot see a
// renamed daemon, which the caller's public port-53 listener proof covers.
func ProbeNoNamedProcess(ctx context.Context) error {
	return probeNoProcessComm(ctx, "/proc", "named", verifyNamedScanProcFS)
}

func verifyNamedScanProcFS(root string) error {
	var fs unix.Statfs_t
	if err := unix.Statfs(root, &fs); err != nil {
		return fmt.Errorf("verify procfs before named process scan: %w", err)
	}
	if fs.Type != unix.PROC_SUPER_MAGIC {
		return errors.New("/proc is not the kernel procfs; named process absence is unknown")
	}
	return nil
}

func probeNoProcessComm(ctx context.Context, root, comm string, verify func(string) error) error {
	if ctx == nil || verify == nil || comm == "" || len(comm) > 15 {
		return errors.New("invalid process inventory")
	}
	if err := verify(root); err != nil {
		return err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return fmt.Errorf("read process inventory: %w", err)
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		pid, err := strconv.ParseUint(entry.Name(), 10, 64)
		if err != nil || pid == 0 || strconv.FormatUint(pid, 10) != entry.Name() {
			continue
		}
		file, err := os.Open(filepath.Join(root, entry.Name(), "comm"))
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, unix.ESRCH) {
			continue
		}
		if err != nil {
			return fmt.Errorf("read process %d command name: %w", pid, err)
		}
		raw, err := io.ReadAll(io.LimitReader(file, 64))
		file.Close()
		if errors.Is(err, unix.ESRCH) {
			continue
		}
		if err != nil {
			return fmt.Errorf("read process %d command name: %w", pid, err)
		}
		if strings.TrimSuffix(string(raw), "\n") == comm {
			return fmt.Errorf("a %s process (PID %d) exists beside the stopped DNS target", comm, pid)
		}
	}
	return verify(root)
}
