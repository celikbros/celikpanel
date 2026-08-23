package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
)

type bindConfigOwnerPolicy struct {
	paths   []string
	apt     bool
	bindGID uint32
}

type bindGroupGIDResolver func(context.Context) (uint32, error)

func bindConfigPaths(layout bindHostLayout) []string {
	paths := []string{filepath.Clean(layout.OptionsConfig), filepath.Clean(layout.AnchorConfig)}
	sort.Strings(paths)
	if len(paths) == 2 && paths[0] == paths[1] {
		paths = paths[:1]
	}
	return paths
}

func resolveBINDConfigOwnerPolicy(
	ctx context.Context,
	layout bindHostLayout,
) (bindConfigOwnerPolicy, error) {
	return resolveBINDConfigOwnerPolicyWithResolver(ctx, layout, resolveBINDGroupGID)
}

func resolveBINDConfigOwnerPolicyWithResolver(
	ctx context.Context,
	layout bindHostLayout,
	resolveGID bindGroupGIDResolver,
) (bindConfigOwnerPolicy, error) {
	if ctx == nil || resolveGID == nil {
		return bindConfigOwnerPolicy{}, errors.New("BIND config owner proof requires a context and resolver")
	}
	paths := bindConfigPaths(layout)
	policy := bindConfigOwnerPolicy{paths: paths}
	switch {
	case reflect.DeepEqual(paths, []string{
		"/etc/bind/named.conf.local", "/etc/bind/named.conf.options",
	}):
		gid, err := resolveGID(ctx)
		if err != nil {
			return bindConfigOwnerPolicy{}, err
		}
		if gid == 0 || gid > uint32(1<<31-1) {
			return bindConfigOwnerPolicy{}, errors.New("BIND service group proof returned an unsafe identity")
		}
		policy.apt = true
		policy.bindGID = gid
	case reflect.DeepEqual(paths, []string{"/etc/named.conf"}):
		// Pacman owns its single vendor configuration as exact root:root.
	default:
		return bindConfigOwnerPolicy{}, errors.New("BIND config owner proof received an unsupported layout")
	}
	return policy, nil
}

func (policy bindConfigOwnerPolicy) validateSnapshots(snapshots []dnsFileSnapshot) error {
	if len(snapshots) != len(policy.paths) {
		return errors.New("BIND config snapshot set is incomplete")
	}
	var commonGID uint32
	for index, snapshot := range snapshots {
		if err := validateDNSFileSnapshotIntegrity(snapshot); err != nil {
			return err
		}
		if snapshot.Path != policy.paths[index] || !snapshot.Exists || snapshot.Mode != 0o644 ||
			!snapshot.OwnerKnown || snapshot.UID != 0 {
			return errors.New("BIND config snapshot differs from its exact file contract")
		}
		if index == 0 {
			commonGID = snapshot.GID
		} else if snapshot.GID != commonGID {
			return errors.New("BIND config snapshots do not share one exact owner")
		}
	}
	if policy.apt {
		if commonGID != 0 && commonGID != policy.bindGID {
			return errors.New("APT BIND config owner is neither root nor the resolved bind group")
		}
	} else if commonGID != 0 {
		return errors.New("Pacman BIND config owner is not root:root")
	}
	return nil
}

func captureBINDConfigSnapshot(
	path string,
	mode os.FileMode,
) (dnsFileSnapshot, error) {
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) || mode.Perm() == 0 || mode.Perm() != mode {
		return dnsFileSnapshot{}, errors.New("invalid BIND config snapshot path or mode")
	}
	data, metadata, err := readDNSFileForSnapshot(path)
	if err != nil {
		return dnsFileSnapshot{}, err
	}
	if metadata.Mode.Perm() != mode.Perm() || !metadata.OwnerKnown {
		return dnsFileSnapshot{}, errors.New("BIND config metadata cannot be verified exactly")
	}
	return dnsFileSnapshot{
		Path: path, Exists: true, Mode: uint32(metadata.Mode.Perm()),
		OwnerKnown: true, UID: metadata.UID, GID: metadata.GID,
		SHA256: digestDNSBytes(data), Data: append([]byte(nil), data...),
	}, nil
}

func captureBINDConfigSnapshotSet(
	policy bindConfigOwnerPolicy,
) ([]dnsFileSnapshot, error) {
	snapshots := make([]dnsFileSnapshot, 0, len(policy.paths))
	for _, path := range policy.paths {
		if err := verifyBINDConfigParentPath(path, policy); err != nil {
			return nil, err
		}
		snapshot, err := captureBINDConfigSnapshot(path, 0o644)
		if err != nil {
			return nil, fmt.Errorf("capture BIND configuration %s: %w", path, err)
		}
		snapshots = append(snapshots, snapshot)
	}
	if err := policy.validateSnapshots(snapshots); err != nil {
		return nil, err
	}
	for _, path := range policy.paths {
		if err := verifyBINDConfigParentPath(path, policy); err != nil {
			return nil, err
		}
	}
	return snapshots, nil
}

func bindConfigSnapshotMap(snapshots []dnsFileSnapshot) map[string]dnsFileSnapshot {
	result := make(map[string]dnsFileSnapshot, len(snapshots))
	for _, snapshot := range snapshots {
		result[snapshot.Path] = snapshot
	}
	return result
}

func (mutation bindConfigMutation) originalSnapshots() []dnsFileSnapshot {
	snapshots := make([]dnsFileSnapshot, 0, len(mutation.paths))
	for _, path := range mutation.paths {
		snapshot := mutation.snapshots[path]
		snapshot.Data = append([]byte(nil), snapshot.Data...)
		snapshots = append(snapshots, snapshot)
	}
	return snapshots
}

func (mutation bindConfigMutation) desiredSnapshots() []dnsFileSnapshot {
	snapshots := mutation.originalSnapshots()
	for index := range snapshots {
		snapshots[index] = bindConfigSnapshotWithData(
			snapshots[index], mutation.desired[snapshots[index].Path],
		)
	}
	return snapshots
}

func bindConfigSnapshotWithData(
	metadata dnsFileSnapshot,
	data []byte,
) dnsFileSnapshot {
	metadata.Data = append([]byte(nil), data...)
	metadata.SHA256 = digestDNSBytes(metadata.Data)
	return metadata
}

func (mutation bindConfigMutation) captureOwnerAwareCurrent(
	ctx context.Context,
	requireOriginal bool,
) (bindConfigOwnerPolicy, map[string]dnsFileSnapshot, error) {
	if !mutation.ownerAware {
		return bindConfigOwnerPolicy{}, nil, errors.New("BIND config mutation lacks an owner-aware contract")
	}
	policy, err := resolveBINDConfigOwnerPolicy(ctx, mutation.layout)
	if err != nil {
		return bindConfigOwnerPolicy{}, nil, err
	}
	current, err := mutation.captureOwnerAwareCurrentWithPolicy(policy, requireOriginal)
	return policy, current, err
}

func (mutation bindConfigMutation) captureOwnerAwareCurrentWithPolicy(
	policy bindConfigOwnerPolicy,
	requireOriginal bool,
) (map[string]dnsFileSnapshot, error) {
	original := mutation.originalSnapshots()
	if err := policy.validateSnapshots(original); err != nil {
		return nil, err
	}
	current, err := captureBINDConfigSnapshotSet(policy)
	if err != nil {
		return nil, err
	}
	currentByPath := bindConfigSnapshotMap(current)
	for _, before := range original {
		actual := currentByPath[before.Path]
		if reflect.DeepEqual(actual, before) {
			continue
		}
		if !requireOriginal && bytes.Equal(actual.Data, mutation.desired[before.Path]) &&
			actual.Mode == before.Mode && actual.OwnerKnown == before.OwnerKnown &&
			actual.UID == before.UID && actual.GID == before.GID {
			continue
		}
		return nil,
			errors.New("BIND config changed outside the exact mutation preimage")
	}
	return currentByPath, nil
}

func verifyBINDConfigMutationPreimage(
	ctx context.Context,
	mutation bindConfigMutation,
) error {
	_, _, err := mutation.captureOwnerAwareCurrent(ctx, true)
	return err
}

func readBackBINDConfigReplacement(
	policy bindConfigOwnerPolicy,
	expected dnsFileSnapshot,
) (dnsFileSnapshot, error) {
	after, err := captureBINDConfigSnapshot(expected.Path, os.FileMode(expected.Mode))
	if err != nil {
		return dnsFileSnapshot{}, err
	}
	allowedPath := false
	for _, path := range policy.paths {
		allowedPath = allowedPath || path == after.Path
	}
	allowedGID := after.GID == 0 || (policy.apt && after.GID == policy.bindGID)
	if !allowedPath || !after.Exists || after.Mode != 0o644 || !after.OwnerKnown ||
		after.UID != 0 || !allowedGID {
		return dnsFileSnapshot{}, errors.New("BIND config replacement readback has unsafe metadata")
	}
	if !reflect.DeepEqual(after, expected) {
		return dnsFileSnapshot{}, errors.New("BIND config replacement readback mismatch")
	}
	return after, nil
}

func verifyBINDConfigSnapshotSetExact(
	policy bindConfigOwnerPolicy,
	expected []dnsFileSnapshot,
) error {
	if err := policy.validateSnapshots(expected); err != nil {
		return err
	}
	actual, err := captureBINDConfigSnapshotSet(policy)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(actual, expected) {
		return errors.New("BIND config set changed during final readback")
	}
	return nil
}

type bindConfigWriteReceipt struct {
	before dnsFileSnapshot
	after  dnsFileSnapshot
}

type bindConfigApplyOps struct {
	write       func(string, []byte, os.FileMode, *dnsFileSnapshot) error
	beforeFinal func()
}

type bindConfigRestoreOps struct {
	write       func(string, []byte, os.FileMode, *dnsFileSnapshot) error
	beforeFinal func()
}

func bindConfigWriterForPolicy(
	policy bindConfigOwnerPolicy,
) func(string, []byte, os.FileMode, *dnsFileSnapshot) error {
	return func(
		path string,
		data []byte,
		mode os.FileMode,
		before *dnsFileSnapshot,
	) error {
		return secureWriteBINDConfigReplacingSnapshot(
			path, data, mode, before, policy,
		)
	}
}

func rollbackBINDConfigWrites(
	policy bindConfigOwnerPolicy,
	written []bindConfigWriteReceipt,
	expectedFull []dnsFileSnapshot,
) error {
	var rollbackErr error
	for index := len(written) - 1; index >= 0; index-- {
		receipt := written[index]
		if err := secureWriteBINDConfigReplacingSnapshot(
			receipt.after.Path,
			receipt.before.Data,
			os.FileMode(receipt.before.Mode),
			&receipt.after,
			policy,
		); err != nil {
			rollbackErr = errors.Join(rollbackErr, err)
			continue
		}
		if _, err := readBackBINDConfigReplacement(policy, receipt.before); err != nil {
			rollbackErr = errors.Join(rollbackErr, err)
		}
	}
	rollbackErr = errors.Join(
		rollbackErr,
		verifyBINDConfigSnapshotSetExact(policy, expectedFull),
	)
	return rollbackErr
}

func (mutation bindConfigMutation) applyOwnerAware(ctx context.Context) error {
	policy, err := resolveBINDConfigOwnerPolicy(ctx, mutation.layout)
	if err != nil {
		return err
	}
	return mutation.applyOwnerAwareWithPolicy(policy)
}

func (mutation bindConfigMutation) applyOwnerAwareWithPolicy(
	policy bindConfigOwnerPolicy,
) error {
	return mutation.applyOwnerAwareWithPolicyAndOps(policy, bindConfigApplyOps{
		write: bindConfigWriterForPolicy(policy),
	})
}

func (mutation bindConfigMutation) applyOwnerAwareWithPolicyAndOps(
	policy bindConfigOwnerPolicy,
	ops bindConfigApplyOps,
) error {
	if ops.write == nil {
		return errors.New("BIND config apply writer is required")
	}
	current, err := mutation.captureOwnerAwareCurrentWithPolicy(policy, true)
	if err != nil {
		return err
	}
	written := make([]bindConfigWriteReceipt, 0, len(mutation.paths))
	for _, path := range mutation.paths {
		before := current[path]
		desired := bindConfigSnapshotWithData(before, mutation.desired[path])
		if reflect.DeepEqual(before, desired) {
			continue
		}
		if err := ops.write(
			path, desired.Data, os.FileMode(desired.Mode), &before,
		); err != nil {
			actual, captureErr := captureBINDConfigSnapshot(path, os.FileMode(before.Mode))
			switch {
			case captureErr == nil && reflect.DeepEqual(actual, desired):
				written = append(written, bindConfigWriteReceipt{before: before, after: actual})
			case captureErr == nil && reflect.DeepEqual(actual, before):
				// The replacement did not commit; only prior writes need rollback.
			default:
				captureErr = errors.Join(
					captureErr,
					errors.New("BIND config replacement outcome is ambiguous"),
				)
			}
			return errors.Join(
				fmt.Errorf("write managed BIND configuration %s: %w", path, err),
				captureErr,
				rollbackBINDConfigWrites(policy, written, mutation.originalSnapshots()),
			)
		}
		written = append(written, bindConfigWriteReceipt{before: before, after: desired})
		after, err := readBackBINDConfigReplacement(policy, desired)
		if err != nil {
			return errors.Join(
				err,
				rollbackBINDConfigWrites(policy, written, mutation.originalSnapshots()),
			)
		}
		written[len(written)-1].after = after
	}
	if ops.beforeFinal != nil {
		ops.beforeFinal()
	}
	if err := verifyBINDConfigSnapshotSetExact(policy, mutation.desiredSnapshots()); err != nil {
		return errors.Join(
			err,
			rollbackBINDConfigWrites(policy, written, mutation.originalSnapshots()),
		)
	}
	return nil
}

func (mutation bindConfigMutation) restoreOwnerAware(ctx context.Context) error {
	policy, err := resolveBINDConfigOwnerPolicy(ctx, mutation.layout)
	if err != nil {
		return err
	}
	return mutation.restoreOwnerAwareWithPolicy(policy)
}

func (mutation bindConfigMutation) restoreOwnerAwareWithPolicy(
	policy bindConfigOwnerPolicy,
) error {
	return mutation.restoreOwnerAwareWithPolicyAndOps(policy, bindConfigRestoreOps{
		write: bindConfigWriterForPolicy(policy),
	})
}

func (mutation bindConfigMutation) restoreOwnerAwareWithPolicyAndOps(
	policy bindConfigOwnerPolicy,
	ops bindConfigRestoreOps,
) error {
	if ops.write == nil {
		return errors.New("BIND config restore writer is required")
	}
	current, err := mutation.captureOwnerAwareCurrentWithPolicy(policy, false)
	if err != nil {
		return err
	}
	var restoreErr error
	for index := len(mutation.paths) - 1; index >= 0; index-- {
		path := mutation.paths[index]
		before := mutation.snapshots[path]
		actual := current[path]
		if reflect.DeepEqual(actual, before) {
			continue
		}
		if err := ops.write(
			path, before.Data, os.FileMode(before.Mode), &actual,
		); err != nil {
			restoreErr = errors.Join(restoreErr, err)
			continue
		}
		if _, err := readBackBINDConfigReplacement(policy, before); err != nil {
			restoreErr = errors.Join(restoreErr, err)
		}
	}
	if ops.beforeFinal != nil {
		ops.beforeFinal()
	}
	restoreErr = errors.Join(
		restoreErr,
		verifyBINDConfigSnapshotSetExact(policy, mutation.originalSnapshots()),
	)
	return restoreErr
}
