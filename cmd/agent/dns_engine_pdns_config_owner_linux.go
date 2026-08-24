//go:build linux

package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func resolvePDNSGroupGID(ctx context.Context) (uint32, error) {
	if ctx == nil {
		return 0, errors.New("PowerDNS group proof requires a context")
	}
	getent, err := firstTrustedExecutable(
		[]string{"/usr/bin/getent", "/bin/getent"}, "getent",
	)
	if err != nil {
		return 0, err
	}
	return resolvePDNSGroupGIDWithRunner(
		ctx, getent,
		func(commandCtx context.Context, name string, args ...string) ([]byte, error) {
			command := serviceMutationCommand(commandCtx, name, args...)
			command.Env = aptBINDStatOverrideCommandEnvironment()
			return command.CombinedOutputLimited(4 << 10)
		},
	)
}

type pdnsConfigParentHandle struct {
	rootFD    int
	chainFDs  []int
	chainPath []string
	chainStat []unix.Stat_t
}

func (handle *pdnsConfigParentHandle) close() {
	for index := len(handle.chainFDs) - 1; index >= 0; index-- {
		_ = unix.Close(handle.chainFDs[index])
	}
	if handle.rootFD >= 0 {
		_ = unix.Close(handle.rootFD)
	}
	handle.chainFDs = nil
	handle.rootFD = -1
}

func (handle *pdnsConfigParentHandle) parentFD() int {
	if len(handle.chainFDs) == 0 {
		return -1
	}
	return handle.chainFDs[len(handle.chainFDs)-1]
}

func pdnsConfigParentComponents(path string) ([]string, error) {
	clean := filepath.Clean(path)
	switch clean {
	case filepath.Clean(dnsMainConf):
		return []string{"etc", "powerdns"}, nil
	case filepath.Clean(dnsManagedConf), filepath.Clean(dnsClusterConf):
		return []string{"etc", "powerdns", "pdns.d"}, nil
	default:
		return nil, errors.New("PowerDNS config path is outside the exact managed layout")
	}
}

func inspectPDNSConfigParentFD(fd int, label string) (unix.Stat_t, error) {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return unix.Stat_t{}, fmt.Errorf("stat %s: %w", label, err)
	}
	permissions := stat.Mode & 0o777
	special := stat.Mode & (unix.S_ISUID | unix.S_ISGID | unix.S_ISVTX)
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Uid != 0 || stat.Gid != 0 ||
		permissions != 0o755 || special != 0 {
		return unix.Stat_t{},
			fmt.Errorf("%s is not an exact root:root 0755 directory", label)
	}
	if err := rejectBINDDirectoryACL(fd, label); err != nil {
		return unix.Stat_t{}, err
	}
	return stat, nil
}

func openPDNSConfigParent(path string) (*pdnsConfigParentHandle, error) {
	components, err := pdnsConfigParentComponents(path)
	if err != nil {
		return nil, err
	}
	rootFD, err := openSecureConfigRoot()
	if err != nil {
		return nil, err
	}
	return openPDNSConfigParentAt(rootFD, components)
}

// openPDNSConfigParentAt takes ownership of rootFD. Keeping the directory walk
// separate from the fixed production layout makes the descriptor proof directly
// testable against an isolated filesystem tree.
func openPDNSConfigParentAt(
	rootFD int,
	components []string,
) (*pdnsConfigParentHandle, error) {
	if rootFD < 0 || len(components) == 0 {
		if rootFD >= 0 {
			_ = unix.Close(rootFD)
		}
		return nil, errors.New("PowerDNS config parent walk is incomplete")
	}
	handle := &pdnsConfigParentHandle{rootFD: rootFD}
	current := rootFD
	partial := ""
	for _, component := range components {
		if component == "" || component == "." || component == ".." ||
			filepath.Base(component) != component {
			handle.close()
			return nil, errors.New("PowerDNS config parent walk contains an unsafe component")
		}
		partial = filepath.Join(partial, component)
		fd, err := unix.Openat2(current, component, &unix.OpenHow{
			Flags: uint64(
				unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW,
			),
			Resolve: secureConfigResolve,
		})
		if err != nil {
			handle.close()
			return nil, secureConfigOpenError(
				"open PowerDNS config parent", filepath.Join("/", partial), err,
			)
		}
		stat, err := inspectPDNSConfigParentFD(
			fd, "PowerDNS config parent "+filepath.Join("/", partial),
		)
		if err != nil {
			_ = unix.Close(fd)
			handle.close()
			return nil, err
		}
		handle.chainFDs = append(handle.chainFDs, fd)
		handle.chainPath = append(handle.chainPath, partial)
		handle.chainStat = append(handle.chainStat, stat)
		current = fd
	}
	return handle, nil
}

func samePDNSParentStat(left, right unix.Stat_t) bool {
	return left.Dev == right.Dev && left.Ino == right.Ino &&
		left.Mode == right.Mode && left.Uid == right.Uid && left.Gid == right.Gid &&
		left.Nlink == right.Nlink
}

func (handle *pdnsConfigParentHandle) reprove() error {
	if handle == nil || handle.rootFD < 0 ||
		len(handle.chainFDs) == 0 ||
		len(handle.chainFDs) != len(handle.chainPath) ||
		len(handle.chainFDs) != len(handle.chainStat) {
		return errors.New("PowerDNS config parent proof is incomplete")
	}
	for index, fd := range handle.chainFDs {
		stat, err := inspectPDNSConfigParentFD(
			fd, "held PowerDNS config parent "+filepath.Join("/", handle.chainPath[index]),
		)
		if err != nil {
			return err
		}
		if !samePDNSParentStat(stat, handle.chainStat[index]) {
			return errors.New("PowerDNS config parent changed while its descriptor was held")
		}
		currentFD, err := unix.Openat2(handle.rootFD, handle.chainPath[index], &unix.OpenHow{
			Flags: uint64(
				unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW,
			),
			Resolve: secureConfigResolve,
		})
		if err != nil {
			return secureConfigOpenError(
				"reopen PowerDNS config parent",
				filepath.Join("/", handle.chainPath[index]), err,
			)
		}
		current, inspectErr := inspectPDNSConfigParentFD(
			currentFD,
			"current PowerDNS config parent "+filepath.Join("/", handle.chainPath[index]),
		)
		closeErr := unix.Close(currentFD)
		if inspectErr != nil || closeErr != nil {
			return errors.Join(inspectErr, closeErr)
		}
		if !samePDNSParentStat(current, handle.chainStat[index]) {
			return errors.New("PowerDNS config parent path changed during exact verification")
		}
	}
	return nil
}

func pdnsConfigIdentityFromStat(stat unix.Stat_t) pdnsConfigFileIdentity {
	return pdnsConfigFileIdentity{
		Exists: true, Device: uint64(stat.Dev), Inode: stat.Ino,
		Mode: stat.Mode & 0o777, UID: stat.Uid, GID: stat.Gid,
		Links: uint64(stat.Nlink), Size: stat.Size,
		MTimeSec: stat.Mtim.Sec, MTimeNsec: stat.Mtim.Nsec,
		CTimeSec: stat.Ctim.Sec, CTimeNsec: stat.Ctim.Nsec,
	}
}

func capturePDNSConfigObservationAtParent(
	handle *pdnsConfigParentHandle,
	path string,
	allowAbsent bool,
	policy pdnsConfigOwnerPolicy,
	afterFirstStat func(),
) (pdnsConfigObservation, error) {
	if handle == nil || handle.parentFD() < 0 {
		return pdnsConfigObservation{}, errors.New("PowerDNS config parent is unavailable")
	}
	base := filepath.Base(filepath.Clean(path))
	fd, err := unix.Openat2(handle.parentFD(), base, &unix.OpenHow{
		Flags: uint64(
			unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK,
		),
		Resolve: secureConfigResolve,
	})
	if errors.Is(err, unix.ENOENT) && allowAbsent {
		if err := handle.reprove(); err != nil {
			return pdnsConfigObservation{}, err
		}
		return pdnsConfigObservation{
			Snapshot: dnsFileSnapshot{Path: filepath.Clean(path)},
		}, nil
	}
	if err != nil {
		return pdnsConfigObservation{},
			secureConfigOpenError("snapshot PowerDNS configuration", path, err)
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return pdnsConfigObservation{}, errors.New("PowerDNS config snapshot has an invalid descriptor")
	}
	defer file.Close()
	var first unix.Stat_t
	if err := unix.Fstat(fd, &first); err != nil {
		return pdnsConfigObservation{}, err
	}
	if first.Mode&unix.S_IFMT != unix.S_IFREG || first.Nlink != 1 ||
		first.Mode&(unix.S_ISUID|unix.S_ISGID|unix.S_ISVTX) != 0 {
		return pdnsConfigObservation{}, errors.New("PowerDNS config is not a single-link regular file")
	}
	if first.Size < 0 || first.Size > dnsEngineSwitchJournalLimit {
		return pdnsConfigObservation{}, errors.New("PowerDNS config exceeds the exact snapshot limit")
	}
	if err := rejectBINDDirectoryACL(fd, "PowerDNS config file "+path); err != nil {
		return pdnsConfigObservation{}, err
	}
	if afterFirstStat != nil {
		afterFirstStat()
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return pdnsConfigObservation{}, err
	}
	var after unix.Stat_t
	if err := unix.Fstat(fd, &after); err != nil {
		return pdnsConfigObservation{}, err
	}
	if !sameSecureConfigStat(first, after) || int64(len(data)) != after.Size {
		return pdnsConfigObservation{}, errors.New("PowerDNS config changed while it was snapshotted")
	}
	currentFD, err := unix.Openat2(handle.parentFD(), base, &unix.OpenHow{
		Flags: uint64(
			unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK,
		),
		Resolve: secureConfigResolve,
	})
	if err != nil {
		return pdnsConfigObservation{},
			secureConfigOpenError("reopen PowerDNS configuration", path, err)
	}
	var current unix.Stat_t
	statErr := unix.Fstat(currentFD, &current)
	closeErr := unix.Close(currentFD)
	if statErr != nil || closeErr != nil {
		return pdnsConfigObservation{}, errors.Join(statErr, closeErr)
	}
	if !sameSecureConfigStat(first, current) {
		return pdnsConfigObservation{}, errors.New("PowerDNS config path changed while it was snapshotted")
	}
	snapshot := dnsFileSnapshot{
		Path: filepath.Clean(path), Exists: true,
		Mode: first.Mode & 0o777, OwnerKnown: true,
		UID: first.Uid, GID: first.Gid,
		SHA256: digestDNSBytes(data), Data: append([]byte(nil), data...),
	}
	if err := policy.validateSnapshot(snapshot); err != nil {
		return pdnsConfigObservation{}, err
	}
	if err := handle.reprove(); err != nil {
		return pdnsConfigObservation{}, err
	}
	return pdnsConfigObservation{
		Snapshot: snapshot, Identity: pdnsConfigIdentityFromStat(first),
	}, nil
}

func captureHostPDNSConfigObservationWithHooks(
	path string,
	allowAbsent bool,
	policy pdnsConfigOwnerPolicy,
	afterFirstStat func(),
	beforeFinalParentProof func(),
) (pdnsConfigObservation, error) {
	handle, err := openPDNSConfigParent(path)
	if err != nil {
		return pdnsConfigObservation{}, err
	}
	defer handle.close()
	observation, err := capturePDNSConfigObservationAtParent(
		handle, path, allowAbsent, policy, afterFirstStat,
	)
	if err != nil {
		return pdnsConfigObservation{}, err
	}
	if beforeFinalParentProof != nil {
		beforeFinalParentProof()
	}
	if err := handle.reprove(); err != nil {
		return pdnsConfigObservation{}, err
	}
	return observation, nil
}

func captureHostPDNSConfigObservations(
	policy pdnsConfigOwnerPolicy,
) ([]pdnsConfigObservation, error) {
	paths := pdnsConfigPaths()
	observations := make([]pdnsConfigObservation, 0, len(paths))
	for _, path := range paths {
		observation, err := captureHostPDNSConfigObservationWithHooks(
			path, path != filepath.Clean(dnsMainConf), policy, nil, nil,
		)
		if err != nil {
			return nil, err
		}
		observations = append(observations, observation)
	}
	if err := validatePDNSConfigObservations(policy, observations); err != nil {
		return nil, err
	}
	return observations, nil
}

func samePDNSConfigObservation(
	left, right pdnsConfigObservation,
) bool {
	return samePDNSConfigData(left.Snapshot, right.Snapshot) &&
		left.Identity == right.Identity
}

func secureWritePDNSConfigReplacingObservation(
	policy pdnsConfigOwnerPolicy,
	before pdnsConfigObservation,
	desired dnsFileSnapshot,
) error {
	return secureWritePDNSConfigReplacingObservationWithHook(
		policy, before, desired, nil,
	)
}

func secureWritePDNSConfigReplacingObservationWithHook(
	policy pdnsConfigOwnerPolicy,
	before pdnsConfigObservation,
	desired dnsFileSnapshot,
	beforeFinalProof func(),
) error {
	if desired.Path != before.Snapshot.Path || !desired.Exists {
		return errors.New("PowerDNS config replacement has an invalid desired state")
	}
	if err := policy.validateSnapshot(before.Snapshot); err != nil {
		return err
	}
	if err := policy.validateSnapshot(desired); err != nil {
		return err
	}
	if before.Snapshot.Exists != before.Identity.Exists {
		return errors.New("PowerDNS config replacement preimage identity is incomplete")
	}
	handle, err := openPDNSConfigParent(desired.Path)
	if err != nil {
		return err
	}
	defer handle.close()
	current, err := capturePDNSConfigObservationAtParent(
		handle, desired.Path, !before.Snapshot.Exists, policy, nil,
	)
	if err != nil {
		return err
	}
	if !samePDNSConfigObservation(current, before) {
		return errors.New("PowerDNS config replacement preimage changed")
	}

	base := filepath.Base(desired.Path)
	var tempName string
	var tempFD int
	for attempt := 0; attempt < 16; attempt++ {
		random := make([]byte, 12)
		if _, err := rand.Read(random); err != nil {
			return err
		}
		tempName = "." + base + ".celikpanel-" + hex.EncodeToString(random)
		tempFD, err = unix.Openat(
			handle.parentFD(), tempName,
			unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW,
			desired.Mode,
		)
		if errors.Is(err, unix.EEXIST) {
			continue
		}
		break
	}
	if err != nil {
		return secureConfigOpenError("create PowerDNS config replacement", desired.Path, err)
	}
	if tempFD < 0 {
		return errors.New("PowerDNS config replacement could not allocate a stage file")
	}
	published := false
	defer func() {
		if !published {
			_ = unix.Unlinkat(handle.parentFD(), tempName, 0)
		}
	}()
	temp := os.NewFile(uintptr(tempFD), desired.Path+" (PowerDNS replacement)")
	if temp == nil {
		_ = unix.Close(tempFD)
		return errors.New("PowerDNS config replacement stage has an invalid descriptor")
	}
	closed := false
	defer func() {
		if !closed {
			_ = temp.Close()
		}
	}()
	if err := unix.Fchown(tempFD, int(desired.UID), int(desired.GID)); err != nil {
		return fmt.Errorf("set PowerDNS config replacement owner: %w", err)
	}
	if err := unix.Fchmod(tempFD, desired.Mode); err != nil {
		return fmt.Errorf("set PowerDNS config replacement mode: %w", err)
	}
	if _, err := io.Copy(temp, bytes.NewReader(desired.Data)); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	var tempStat unix.Stat_t
	if err := unix.Fstat(tempFD, &tempStat); err != nil {
		return err
	}
	if tempStat.Mode&unix.S_IFMT != unix.S_IFREG || tempStat.Nlink != 1 ||
		tempStat.Mode&0o777 != desired.Mode || tempStat.Uid != desired.UID ||
		tempStat.Gid != desired.GID || tempStat.Size != int64(len(desired.Data)) {
		return errors.New("PowerDNS config replacement stage metadata is unsafe")
	}
	if err := rejectBINDDirectoryACL(tempFD, "PowerDNS config replacement stage"); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	closed = true
	if beforeFinalProof != nil {
		beforeFinalProof()
	}
	current, err = capturePDNSConfigObservationAtParent(
		handle, desired.Path, !before.Snapshot.Exists, policy, nil,
	)
	if err != nil {
		return err
	}
	if !samePDNSConfigObservation(current, before) {
		return errors.New("PowerDNS config changed before atomic replacement")
	}
	if err := handle.reprove(); err != nil {
		return err
	}
	if before.Snapshot.Exists {
		err = unix.Renameat(handle.parentFD(), tempName, handle.parentFD(), base)
	} else {
		err = unix.Renameat2(
			handle.parentFD(), tempName, handle.parentFD(), base, unix.RENAME_NOREPLACE,
		)
	}
	if err != nil {
		return secureConfigOpenError("publish PowerDNS config replacement", desired.Path, err)
	}
	published = true
	if err := unix.Fsync(handle.parentFD()); err != nil {
		return fmt.Errorf("sync PowerDNS config directory: %w", err)
	}
	return nil
}

func secureRemovePDNSConfigReplacingObservation(
	policy pdnsConfigOwnerPolicy,
	before pdnsConfigObservation,
) error {
	return secureRemovePDNSConfigReplacingObservationWithHook(policy, before, nil)
}

func secureRemovePDNSConfigReplacingObservationWithHook(
	policy pdnsConfigOwnerPolicy,
	before pdnsConfigObservation,
	beforeFinalProof func(),
) error {
	if !before.Snapshot.Exists || !before.Identity.Exists {
		return errors.New("PowerDNS config removal requires an existing exact preimage")
	}
	if err := policy.validateSnapshot(before.Snapshot); err != nil {
		return err
	}
	handle, err := openPDNSConfigParent(before.Snapshot.Path)
	if err != nil {
		return err
	}
	defer handle.close()
	current, err := capturePDNSConfigObservationAtParent(
		handle, before.Snapshot.Path, false, policy, nil,
	)
	if err != nil {
		return err
	}
	if !samePDNSConfigObservation(current, before) {
		return errors.New("PowerDNS config removal preimage changed")
	}
	if beforeFinalProof != nil {
		beforeFinalProof()
	}
	current, err = capturePDNSConfigObservationAtParent(
		handle, before.Snapshot.Path, false, policy, nil,
	)
	if err != nil {
		return err
	}
	if !samePDNSConfigObservation(current, before) {
		return errors.New("PowerDNS config changed before atomic removal")
	}
	if err := handle.reprove(); err != nil {
		return err
	}
	if err := unix.Unlinkat(
		handle.parentFD(), filepath.Base(before.Snapshot.Path), 0,
	); err != nil {
		return secureConfigOpenError("remove PowerDNS configuration", before.Snapshot.Path, err)
	}
	if err := unix.Fsync(handle.parentFD()); err != nil {
		return fmt.Errorf("sync PowerDNS config directory after removal: %w", err)
	}
	return nil
}
