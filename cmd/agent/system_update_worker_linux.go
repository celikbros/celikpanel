//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

const systemUpdateWorkerLimit = 45 * time.Minute

var (
	systemUpdateExecutableResolver = resolveFixedRootExecutable
	systemUpdateCommandRunner      = runFixedSystemUpdateCommand
)

func runFixedSystemUpdateCommand(ctx context.Context, name string, args []string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C"}
	command.Dir = "/"
	var output boundedSystemUpdateBuffer
	command.Stdout, command.Stderr = &output, &output
	configureServiceMutationProcessGroup(command)
	err := command.Run()
	return append([]byte(nil), output.raw...), err
}

func systemUpdateUnitName(requestID string) (string, error) {
	if !systemUpdateRequestRE.MatchString(requestID) {
		return "", errors.New("invalid system-update worker identity")
	}
	return "celikpanel-self-update-" + requestID + ".service", nil
}

func resolveFixedRootExecutable(candidates ...string) (string, error) {
	for _, candidate := range candidates {
		clean := filepath.Clean(candidate)
		if clean != candidate || !filepath.IsAbs(clean) {
			continue
		}
		info, err := os.Lstat(clean)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o022 != 0 || info.Mode().Perm()&0o111 == 0 {
			continue
		}
		if err := validateRootOwnedDirectoryChain(filepath.Dir(clean)); err != nil {
			continue
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != 0 || stat.Gid != 0 || stat.Nlink != 1 {
			continue
		}
		return clean, nil
	}
	return "", errors.New("fixed executable failed security validation")
}

func (backend *linuxSystemUpdateBackend) launchWorker(ctx context.Context, requestID string) error {
	unit, err := systemUpdateUnitName(requestID)
	if err != nil {
		return err
	}
	systemdRun, err := systemUpdateExecutableResolver("/usr/bin/systemd-run", "/bin/systemd-run")
	if err != nil {
		return err
	}
	agent, err := systemUpdateExecutableResolver(systemUpdateAgentPath)
	if err != nil {
		return err
	}
	args := []string{
		"--unit=" + unit,
		"--property=Type=exec",
		"--property=KillMode=mixed",
		"--property=TimeoutStartSec=50min",
		"--no-block",
		agent, "--self-update-worker", requestID,
	}
	output, err := systemUpdateCommandRunner(ctx, systemdRun, args)
	if err != nil {
		return fmt.Errorf("systemd-run failed: %w: %s", err, sanitizedSystemUpdateError(errors.New(string(output))))
	}
	return nil
}

func (backend *linuxSystemUpdateBackend) probeWorkerUnit(ctx context.Context, requestID string) (systemUpdateUnitState, error) {
	unit, err := systemUpdateUnitName(requestID)
	if err != nil {
		return systemUpdateUnitAmbiguous, err
	}
	systemctl, err := systemUpdateExecutableResolver("/usr/bin/systemctl", "/bin/systemctl")
	if err != nil {
		return systemUpdateUnitAmbiguous, err
	}
	probeCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	output, err := systemUpdateCommandRunner(probeCtx, systemctl, []string{"show", unit, "--property=LoadState,ActiveState,SubState,Result", "--no-pager"})
	if err != nil {
		return systemUpdateUnitAmbiguous, err
	}
	fields := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return systemUpdateUnitAmbiguous, errors.New("systemd unit state is not canonical")
		}
		fields[parts[0]] = parts[1]
	}
	if len(fields) != 4 {
		return systemUpdateUnitAmbiguous, errors.New("systemd unit state is incomplete")
	}
	if fields["ActiveState"] == "active" || fields["ActiveState"] == "activating" {
		return systemUpdateUnitActive, nil
	}
	if (fields["LoadState"] == "loaded" || fields["LoadState"] == "not-found") && (fields["ActiveState"] == "inactive" || fields["ActiveState"] == "failed") {
		return systemUpdateUnitInactive, nil
	}
	return systemUpdateUnitAmbiguous, nil
}

type boundedSystemUpdateBuffer struct{ raw []byte }

func (buffer *boundedSystemUpdateBuffer) Write(value []byte) (int, error) {
	const limit = 4096
	written := len(value)
	if len(value) >= limit {
		buffer.raw = append(buffer.raw[:0], value[len(value)-limit:]...)
		return written, nil
	}
	if overflow := len(buffer.raw) + len(value) - limit; overflow > 0 {
		copy(buffer.raw, buffer.raw[overflow:])
		buffer.raw = buffer.raw[:len(buffer.raw)-overflow]
	}
	buffer.raw = append(buffer.raw, value...)
	return written, nil
}

func (buffer *boundedSystemUpdateBuffer) String() string {
	return sanitizedSystemUpdateError(errors.New(string(bytes.TrimSpace(buffer.raw))))
}

func runSystemUpdateInstaller(ctx context.Context, state *systemUpdateState, trustedMinimum string) error {
	if _, err := parseCanonicalPositiveDecimal(trustedMinimum, math.MaxInt64); err != nil {
		return errors.New("trusted minimum release sequence is invalid")
	}
	installer, err := systemUpdateExecutableResolver(systemUpdateInstallerPath)
	if err != nil {
		return err
	}
	args := []string{
		"--update",
		"--version", state.TargetVersion,
		"--require-signed-manifest",
		"--expected-sequence", state.TargetSequence,
		"--minimum-sequence", trustedMinimum,
		"--expected-commit", state.TargetCommit,
		"--expected-archive-sha256", state.TargetArchiveSHA256,
		"--expected-archive-size", state.TargetArchiveSize,
	}
	output, err := systemUpdateCommandRunner(ctx, installer, args)
	if err != nil {
		return fmt.Errorf("reviewed updater failed: %w: %s", err, sanitizedSystemUpdateError(errors.New(string(output))))
	}
	return nil
}

func inspectInstalledSystemUpdateBuild(ctx context.Context) (string, string, error) {
	agent, err := systemUpdateExecutableResolver(systemUpdateAgentPath)
	if err != nil {
		return "", "", err
	}
	probeCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	output, err := systemUpdateCommandRunner(probeCtx, agent, []string{"--inspect-build-identity"})
	if err != nil {
		return "", "", err
	}
	if len(output) > 256 {
		return "", "", errors.New("installed build identity output is too large")
	}
	lines := strings.Split(string(output), "\n")
	if len(lines) != 3 || lines[2] != "" || !strings.HasPrefix(lines[0], "version=") || !strings.HasPrefix(lines[1], "commit=") {
		return "", "", errors.New("installed build identity is not canonical")
	}
	version, commit := strings.TrimPrefix(lines[0], "version="), strings.TrimPrefix(lines[1], "commit=")
	if _, err := parseSystemUpdateSemver(version); err != nil || !systemUpdateCommitRE.MatchString(commit) {
		return "", "", errors.New("installed build identity is invalid")
	}
	return version, commit, nil
}

func (backend *linuxSystemUpdateBackend) RunWorker(ctx context.Context, requestID string, fetcher systemUpdateManifestFetcher) error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if fetcher == nil {
		return errors.New("system update manifest fetcher is unavailable")
	}
	workerLock, err := acquireSystemUpdateNamedLock(ctx, backend.stateRoot, ".worker.lock")
	if err != nil {
		return err
	}
	defer workerLock.Close()
	stateLock, err := acquireSystemUpdateStateLock(ctx, backend.stateRoot)
	if err != nil {
		return err
	}
	state, err := readSystemUpdateState(backend.stateRoot, requestID)
	if err != nil {
		stateLock.Close()
		return err
	}
	if !state.active() {
		stateLock.Close()
		return errors.New("system update request is not active")
	}
	manifest, err := fetcher.Fetch(ctx, state.TargetVersion, state.TargetOS, state.TargetArch)
	if err != nil {
		result := backend.failStateLocked(state, err)
		stateLock.Close()
		return result
	}
	request := stateToSystemUpdateStartRequest(state)
	if !systemUpdateRequestMatchesManifest(&request, manifest) {
		result := backend.failStateLocked(state, errors.New("worker manifest no longer matches the durable request"))
		stateLock.Close()
		return result
	}
	floor, err := backend.ReadFloor()
	if err != nil {
		result := backend.failStateLocked(state, err)
		stateLock.Close()
		return result
	}
	if err := systemUpdateFloorAllows(floor, manifest); err != nil {
		result := backend.failStateLocked(state, err)
		stateLock.Close()
		return result
	}
	if state.Status == systemUpdateQueued {
		running := *state
		running.Status = systemUpdateRunning
		running.UpdatedAt = backend.now().UTC().Format(time.RFC3339Nano)
		if err := writeSystemUpdateState(backend.stateRoot, &running, backend.writeFault); err != nil {
			stateLock.Close()
			return err
		}
		state = &running
	}
	if err := stateLock.Close(); err != nil {
		return err
	}
	// QueueAndLaunch proves the global mutation ledger/package manager idle while
	// launching this worker. The reviewed updater performs the same proof again
	// immediately before its host-mutation boundary. Do not retain that flock
	// across this child: update.sh deliberately reacquires it with a separate
	// CLOEXEC-safe descriptor and would otherwise fail against our own lock.
	installerErr := backend.runInstaller(ctx, state, floor.Sequence)
	stateLock, err = acquireSystemUpdateStateLock(ctx, backend.stateRoot)
	if err != nil {
		return err
	}
	defer stateLock.Close()
	current, err := readSystemUpdateState(backend.stateRoot, requestID)
	if err != nil {
		return err
	}
	if !stateIdentityMatches(current, state) || !current.active() {
		return errors.New("system update state changed while the worker was running")
	}
	state = current
	if installerErr != nil {
		return backend.failStateLocked(state, installerErr)
	}
	floor, err = backend.ReadFloor()
	if err != nil || floor == nil || floor.Sequence != state.TargetSequence || floor.Version != state.TargetVersion {
		if err == nil {
			err = errors.New("reviewed updater did not publish the exact durable release floor")
		}
		return backend.failStateLocked(state, err)
	}
	version, commit, err := backend.installedBuild(ctx)
	if err != nil || version != state.TargetVersion || commit != state.TargetCommit {
		if err == nil {
			err = errors.New("installed agent identity does not match the signed target")
		}
		return backend.failStateLocked(state, err)
	}
	succeeded := *state
	succeeded.Status = systemUpdateSucceeded
	succeeded.Error = ""
	succeeded.UpdatedAt = backend.now().UTC().Format(time.RFC3339Nano)
	return writeSystemUpdateState(backend.stateRoot, &succeeded, backend.writeFault)
}

func stateToSystemUpdateStartRequest(state *systemUpdateState) transport.SystemUpdateStartRequest {
	return transport.SystemUpdateStartRequest{RequestID: state.RequestID, TargetVersion: state.TargetVersion, TargetCommit: state.TargetCommit, TargetSequence: state.TargetSequence, TargetOS: state.TargetOS, TargetArch: state.TargetArch, TargetArchiveSHA256: state.TargetArchiveSHA256, TargetArchiveSize: state.TargetArchiveSize, ExpectedCurrentVersion: state.ExpectedCurrentVersion, ExpectedCurrentCommit: state.ExpectedCurrentCommit}
}

func runSystemUpdateWorker(requestID string) error {
	if !systemUpdateRequestRE.MatchString(requestID) {
		return errors.New("invalid self-update worker request_id")
	}
	backend := newLinuxSystemUpdateBackend()
	ctx, cancel := context.WithTimeout(context.Background(), systemUpdateWorkerLimit)
	defer cancel()
	return backend.RunWorker(ctx, requestID, newHTTPSystemUpdateManifestFetcher())
}
