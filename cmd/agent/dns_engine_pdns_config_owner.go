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
	"strconv"
	"strings"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

type pdnsConfigOwnerPolicy struct {
	pdnsGID uint32
}

type pdnsGroupGIDResolver func(context.Context) (uint32, error)

type pdnsGroupLookupRunner func(context.Context, string, ...string) ([]byte, error)

func pdnsConfigPaths() []string {
	paths := []string{
		filepath.Clean(dnsMainConf),
		filepath.Clean(dnsManagedConf),
		filepath.Clean(dnsClusterConf),
	}
	sort.Strings(paths)
	return paths
}

func resolvePDNSConfigOwnerPolicy(
	ctx context.Context,
) (pdnsConfigOwnerPolicy, error) {
	return resolvePDNSConfigOwnerPolicyWithResolver(ctx, resolvePDNSGroupGID)
}

func resolvePDNSConfigOwnerPolicyWithResolver(
	ctx context.Context,
	resolveGID pdnsGroupGIDResolver,
) (pdnsConfigOwnerPolicy, error) {
	if ctx == nil || resolveGID == nil {
		return pdnsConfigOwnerPolicy{},
			errors.New("PowerDNS config owner proof requires a context and resolver")
	}
	gid, err := resolveGID(ctx)
	if err != nil {
		return pdnsConfigOwnerPolicy{}, err
	}
	if gid == 0 || gid > uint32(1<<31-1) {
		return pdnsConfigOwnerPolicy{},
			errors.New("PowerDNS service group proof returned an unsafe identity")
	}
	return pdnsConfigOwnerPolicy{pdnsGID: gid}, nil
}

func resolvePDNSGroupGIDWithRunner(
	ctx context.Context,
	getent string,
	runner pdnsGroupLookupRunner,
) (uint32, error) {
	if ctx == nil || getent == "" || runner == nil {
		return 0, errors.New("invalid PowerDNS group proof")
	}
	lookup := func() (uint32, error) {
		output, err := runner(ctx, getent, "group", "pdns")
		if err != nil {
			return 0, fmt.Errorf("resolve PowerDNS service group: %w", err)
		}
		line := string(output)
		if !strings.HasSuffix(line, "\n") || strings.Count(line, "\n") != 1 {
			return 0, errors.New("getent returned a non-canonical PowerDNS group record")
		}
		fields := strings.Split(strings.TrimSuffix(line, "\n"), ":")
		if len(fields) != 4 || fields[0] != "pdns" || fields[1] != "x" ||
			fields[2] == "" || fields[3] != "" {
			return 0, errors.New("getent returned an unsafe PowerDNS group record")
		}
		parsed, err := strconv.ParseUint(fields[2], 10, 32)
		if err != nil || parsed == 0 || parsed > uint64(1<<31-1) ||
			strconv.FormatUint(parsed, 10) != fields[2] {
			return 0, errors.New("getent returned an unsafe PowerDNS group identity")
		}
		return uint32(parsed), nil
	}
	first, err := lookup()
	if err != nil {
		return 0, err
	}
	second, err := lookup()
	if err != nil {
		return 0, err
	}
	if second != first {
		return 0, errors.New("PowerDNS service group changed during exact verification")
	}
	return second, nil
}

func validatePDNSConfigSnapshotSetStructure(
	snapshots []dnsFileSnapshot,
) error {
	paths := pdnsConfigPaths()
	if len(snapshots) != len(paths) {
		return errors.New("PowerDNS config snapshot set is incomplete")
	}
	for index, snapshot := range snapshots {
		if err := validateDNSFileSnapshotIntegrity(snapshot); err != nil {
			return err
		}
		if snapshot.Path != paths[index] {
			return errors.New("PowerDNS config snapshot set contains an unexpected path")
		}
		if err := validatePDNSConfigSnapshotStructure(snapshot); err != nil {
			return err
		}
	}
	return nil
}

func validatePDNSConfigSnapshotStructure(snapshot dnsFileSnapshot) error {
	if err := validateDNSFileSnapshotIntegrity(snapshot); err != nil {
		return err
	}
	switch snapshot.Path {
	case filepath.Clean(dnsMainConf):
		if !snapshot.Exists || snapshot.Mode != 0o640 ||
			!snapshot.OwnerKnown || snapshot.UID != 0 ||
			snapshot.GID > uint32(1<<31-1) {
			return errors.New("PowerDNS main config snapshot differs from its installed-file contract")
		}
	case filepath.Clean(dnsManagedConf), filepath.Clean(dnsClusterConf):
		if snapshot.Exists && (snapshot.Mode != 0o644 ||
			!snapshot.OwnerKnown || snapshot.UID != 0 || snapshot.GID != 0) {
			return errors.New("PowerDNS managed config snapshot differs from its root-owned contract")
		}
	default:
		return errors.New("PowerDNS config snapshot path is unsupported")
	}
	return nil
}

func (policy pdnsConfigOwnerPolicy) validateSnapshot(
	snapshot dnsFileSnapshot,
) error {
	if policy.pdnsGID == 0 || policy.pdnsGID > uint32(1<<31-1) {
		return errors.New("PowerDNS config owner policy has an unsafe service group")
	}
	if err := validatePDNSConfigSnapshotStructure(snapshot); err != nil {
		return err
	}
	if snapshot.Path == filepath.Clean(dnsMainConf) &&
		snapshot.GID != 0 && snapshot.GID != policy.pdnsGID {
		return errors.New("PowerDNS main config group is neither root nor the resolved pdns group")
	}
	return nil
}

func (policy pdnsConfigOwnerPolicy) validateSnapshots(
	snapshots []dnsFileSnapshot,
) error {
	if policy.pdnsGID == 0 || policy.pdnsGID > uint32(1<<31-1) {
		return errors.New("PowerDNS config owner policy has an unsafe service group")
	}
	if err := validatePDNSConfigSnapshotSetStructure(snapshots); err != nil {
		return err
	}
	main := snapshots[sort.Search(len(snapshots), func(index int) bool {
		return snapshots[index].Path >= filepath.Clean(dnsMainConf)
	})]
	if main.Path != filepath.Clean(dnsMainConf) ||
		(main.GID != 0 && main.GID != policy.pdnsGID) {
		return errors.New("PowerDNS main config group is neither root nor the resolved pdns group")
	}
	return nil
}

type pdnsConfigFileIdentity struct {
	Exists    bool
	Device    uint64
	Inode     uint64
	Mode      uint32
	UID       uint32
	GID       uint32
	Links     uint64
	Size      int64
	MTimeSec  int64
	MTimeNsec int64
	CTimeSec  int64
	CTimeNsec int64
}

type pdnsConfigObservation struct {
	Snapshot dnsFileSnapshot
	Identity pdnsConfigFileIdentity
}

func validatePDNSConfigObservations(
	policy pdnsConfigOwnerPolicy,
	observations []pdnsConfigObservation,
) error {
	snapshots := make([]dnsFileSnapshot, len(observations))
	for index, observation := range observations {
		snapshots[index] = observation.Snapshot
		if observation.Snapshot.Exists != observation.Identity.Exists {
			return errors.New("PowerDNS config observation has inconsistent existence state")
		}
		if observation.Snapshot.Exists {
			if observation.Identity.Links != 1 ||
				observation.Identity.Mode != observation.Snapshot.Mode ||
				observation.Identity.UID != observation.Snapshot.UID ||
				observation.Identity.GID != observation.Snapshot.GID ||
				observation.Identity.Size != int64(len(observation.Snapshot.Data)) {
				return errors.New("PowerDNS config observation identity differs from its snapshot")
			}
		} else if observation.Identity != (pdnsConfigFileIdentity{}) {
			return errors.New("absent PowerDNS config observation contains hidden identity")
		}
	}
	return policy.validateSnapshots(snapshots)
}

func pdnsConfigObservationMap(
	observations []pdnsConfigObservation,
) map[string]pdnsConfigObservation {
	result := make(map[string]pdnsConfigObservation, len(observations))
	for _, observation := range observations {
		result[observation.Snapshot.Path] = observation
	}
	return result
}

func pdnsConfigSnapshotMap(
	snapshots []dnsFileSnapshot,
) map[string]dnsFileSnapshot {
	result := make(map[string]dnsFileSnapshot, len(snapshots))
	for _, snapshot := range snapshots {
		result[snapshot.Path] = snapshot
	}
	return result
}

func clonePDNSConfigSnapshots(snapshots []dnsFileSnapshot) []dnsFileSnapshot {
	cloned := make([]dnsFileSnapshot, len(snapshots))
	for index, snapshot := range snapshots {
		cloned[index] = snapshot
		cloned[index].Data = append([]byte(nil), snapshot.Data...)
	}
	return cloned
}

type pdnsConfigMutation struct {
	before             []dnsFileSnapshot
	desired            map[string][]byte
	policy             pdnsConfigOwnerPolicy
	originalIdentities map[string]pdnsConfigFileIdentity
	ownerAware         bool
}

func newPDNSConfigMutationFromSnapshots(
	policy pdnsConfigOwnerPolicy,
	before []dnsFileSnapshot,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	managedConfig []byte,
) (pdnsConfigMutation, error) {
	if len(managedConfig) == 0 {
		return pdnsConfigMutation{}, errors.New("managed PowerDNS configuration is required")
	}
	before = clonePDNSConfigSnapshots(before)
	sort.Slice(before, func(left, right int) bool {
		return before[left].Path < before[right].Path
	})
	if err := policy.validateSnapshots(before); err != nil {
		return pdnsConfigMutation{}, err
	}
	byPath := pdnsConfigSnapshotMap(before)
	mainBefore := byPath[filepath.Clean(dnsMainConf)]
	managedDir := filepath.Clean(filepath.Dir(dnsManagedConf))
	hasInclude, err := validateManagedPowerDNSMainConfig(
		string(mainBefore.Data), managedDir,
	)
	if err != nil {
		return pdnsConfigMutation{}, err
	}
	mainDesired := append([]byte(nil), mainBefore.Data...)
	if !hasInclude {
		mainDesired = append(
			mainDesired,
			[]byte("\n# Managed by CelikPanel.\ninclude-dir="+managedDir+"\n")...,
		)
	}
	desired := map[string][]byte{
		filepath.Clean(dnsMainConf):    mainDesired,
		filepath.Clean(dnsManagedConf): append([]byte(nil), managedConfig...),
	}
	if manifest.Topology == transport.DNSTopologyPaired {
		clusterConfig, err := dnsClusterConfigForSwitchManifest(manifest)
		if err != nil {
			return pdnsConfigMutation{}, err
		}
		desired[filepath.Clean(dnsClusterConf)] = []byte(clusterConfig)
	}
	return pdnsConfigMutation{
		before: before, desired: desired, policy: policy, ownerAware: true,
	}, nil
}

func (mutation pdnsConfigMutation) originalSnapshots() []dnsFileSnapshot {
	return clonePDNSConfigSnapshots(mutation.before)
}

func (mutation pdnsConfigMutation) desiredSnapshots() []dnsFileSnapshot {
	before := pdnsConfigSnapshotMap(mutation.before)
	paths := pdnsConfigPaths()
	result := make([]dnsFileSnapshot, 0, len(paths))
	for _, path := range paths {
		data, exists := mutation.desired[path]
		if !exists {
			result = append(result, dnsFileSnapshot{Path: path})
			continue
		}
		snapshot := dnsFileSnapshot{
			Path: path, Exists: true, Mode: 0o644,
			OwnerKnown: true, UID: 0, GID: 0,
			Data: append([]byte(nil), data...),
		}
		if path == filepath.Clean(dnsMainConf) {
			snapshot.Mode = before[path].Mode
			snapshot.UID = before[path].UID
			snapshot.GID = before[path].GID
		}
		snapshot.SHA256 = digestDNSBytes(snapshot.Data)
		result = append(result, snapshot)
	}
	return result
}

func (mutation pdnsConfigMutation) validateOwnerAware() error {
	if !mutation.ownerAware {
		return errors.New("PowerDNS config mutation lacks an owner-aware contract")
	}
	if err := mutation.policy.validateSnapshots(mutation.before); err != nil {
		return err
	}
	desired := mutation.desiredSnapshots()
	if err := mutation.policy.validateSnapshots(desired); err != nil {
		return err
	}
	if _, ok := mutation.desired[filepath.Clean(dnsMainConf)]; !ok {
		return errors.New("PowerDNS config mutation is missing the main configuration")
	}
	if _, ok := mutation.desired[filepath.Clean(dnsManagedConf)]; !ok {
		return errors.New("PowerDNS config mutation is missing the managed configuration")
	}
	for path := range mutation.desired {
		if path != filepath.Clean(dnsMainConf) &&
			path != filepath.Clean(dnsManagedConf) &&
			path != filepath.Clean(dnsClusterConf) {
			return errors.New("PowerDNS config mutation contains an unsupported path")
		}
	}
	return nil
}

type pdnsConfigAccessOps struct {
	resolve     func(context.Context) (pdnsConfigOwnerPolicy, error)
	capture     func(pdnsConfigOwnerPolicy) ([]pdnsConfigObservation, error)
	replace     func(pdnsConfigOwnerPolicy, pdnsConfigObservation, dnsFileSnapshot) error
	remove      func(pdnsConfigOwnerPolicy, pdnsConfigObservation) error
	beforeFinal func()
}

func hostPDNSConfigAccessOps() pdnsConfigAccessOps {
	return pdnsConfigAccessOps{
		resolve: resolvePDNSConfigOwnerPolicy,
		capture: captureHostPDNSConfigObservations,
		replace: secureWritePDNSConfigReplacingObservation,
		remove:  secureRemovePDNSConfigReplacingObservation,
	}
}

func validatePDNSConfigAccessOps(ops pdnsConfigAccessOps) error {
	if ops.resolve == nil || ops.capture == nil || ops.replace == nil ||
		ops.remove == nil {
		return errors.New("PowerDNS config access operations are incomplete")
	}
	return nil
}

func (mutation pdnsConfigMutation) captureOwnerAwareCurrentWithOps(
	ctx context.Context,
	requireOriginalIdentity bool,
	ops pdnsConfigAccessOps,
) (map[string]pdnsConfigObservation, error) {
	if ctx == nil {
		return nil, errors.New("PowerDNS config proof requires a context")
	}
	if err := mutation.validateOwnerAware(); err != nil {
		return nil, err
	}
	if err := validatePDNSConfigAccessOps(ops); err != nil {
		return nil, err
	}
	policy, err := ops.resolve(ctx)
	if err != nil {
		return nil, err
	}
	if policy != mutation.policy {
		return nil, errors.New("PowerDNS service group changed after the config plan was prepared")
	}
	observations, err := ops.capture(policy)
	if err != nil {
		return nil, err
	}
	if err := validatePDNSConfigObservations(policy, observations); err != nil {
		return nil, err
	}
	current := pdnsConfigObservationMap(observations)
	before := pdnsConfigSnapshotMap(mutation.before)
	desired := pdnsConfigSnapshotMap(mutation.desiredSnapshots())
	for _, path := range pdnsConfigPaths() {
		actual := current[path]
		originalSnapshot := reflect.DeepEqual(actual.Snapshot, before[path])
		originalIdentity := true
		if expected, ok := mutation.originalIdentities[path]; ok {
			originalIdentity = actual.Identity == expected
		}
		original := originalSnapshot && originalIdentity
		if requireOriginalIdentity {
			if !original {
				return nil, errors.New("PowerDNS config changed outside the exact mutation preimage")
			}
			continue
		}
		if originalSnapshot || reflect.DeepEqual(actual.Snapshot, desired[path]) {
			continue
		}
		return nil, errors.New("PowerDNS config changed outside the exact mutation states")
	}
	return current, nil
}

func (mutation pdnsConfigMutation) verifyOwnerAwarePreimage(
	ctx context.Context,
) error {
	_, err := mutation.captureOwnerAwareCurrentWithOps(
		ctx, true, hostPDNSConfigAccessOps(),
	)
	return err
}

func capturePDNSConfigSnapshotsExactWithOps(
	policy pdnsConfigOwnerPolicy,
	expected []dnsFileSnapshot,
	ops pdnsConfigAccessOps,
) ([]pdnsConfigObservation, error) {
	observations, err := ops.capture(policy)
	if err != nil {
		return nil, err
	}
	if err := validatePDNSConfigObservations(policy, observations); err != nil {
		return nil, err
	}
	actual := make([]dnsFileSnapshot, len(observations))
	for index, observation := range observations {
		actual[index] = observation.Snapshot
	}
	if !reflect.DeepEqual(actual, expected) {
		return nil, errors.New("PowerDNS config set changed during exact readback")
	}
	return observations, nil
}

type pdnsConfigWriteReceipt struct {
	before pdnsConfigObservation
	after  pdnsConfigObservation
}

func pdnsConfigObservationForPath(
	observations []pdnsConfigObservation,
	path string,
) (pdnsConfigObservation, bool) {
	for _, observation := range observations {
		if observation.Snapshot.Path == path {
			return observation, true
		}
	}
	return pdnsConfigObservation{}, false
}

func applyPDNSConfigReplacement(
	policy pdnsConfigOwnerPolicy,
	before pdnsConfigObservation,
	desired dnsFileSnapshot,
	ops pdnsConfigAccessOps,
) error {
	if desired.Exists {
		return ops.replace(policy, before, desired)
	}
	if !before.Snapshot.Exists {
		return nil
	}
	return ops.remove(policy, before)
}

func rollbackPDNSConfigWrites(
	policy pdnsConfigOwnerPolicy,
	written []pdnsConfigWriteReceipt,
	expectedFull []dnsFileSnapshot,
	ops pdnsConfigAccessOps,
) error {
	var rollbackErr error
	for index := len(written) - 1; index >= 0; index-- {
		receipt := written[index]
		if err := applyPDNSConfigReplacement(
			policy, receipt.after, receipt.before.Snapshot, ops,
		); err != nil {
			observations, captureErr := ops.capture(policy)
			actual, found := pdnsConfigObservationForPath(
				observations, receipt.before.Snapshot.Path,
			)
			if captureErr == nil && found &&
				reflect.DeepEqual(actual.Snapshot, receipt.before.Snapshot) {
				continue
			}
			rollbackErr = errors.Join(rollbackErr, err, captureErr)
			continue
		}
		observations, err := ops.capture(policy)
		actual, found := pdnsConfigObservationForPath(
			observations, receipt.before.Snapshot.Path,
		)
		if err != nil || !found ||
			!reflect.DeepEqual(actual.Snapshot, receipt.before.Snapshot) {
			if err == nil {
				err = errors.New("PowerDNS config rollback readback mismatch")
			}
			rollbackErr = errors.Join(rollbackErr, err)
		}
	}
	_, finalErr := capturePDNSConfigSnapshotsExactWithOps(
		policy, expectedFull, ops,
	)
	return errors.Join(rollbackErr, finalErr)
}

func (mutation pdnsConfigMutation) applyOwnerAwareWithOps(
	ctx context.Context,
	ops pdnsConfigAccessOps,
) error {
	current, err := mutation.captureOwnerAwareCurrentWithOps(ctx, true, ops)
	if err != nil {
		return err
	}
	desired := pdnsConfigSnapshotMap(mutation.desiredSnapshots())
	written := make([]pdnsConfigWriteReceipt, 0, len(desired))
	for _, path := range pdnsConfigPaths() {
		before := current[path]
		want := desired[path]
		if reflect.DeepEqual(before.Snapshot, want) {
			continue
		}
		if err := applyPDNSConfigReplacement(
			mutation.policy, before, want, ops,
		); err != nil {
			observations, captureErr := ops.capture(mutation.policy)
			actual, found := pdnsConfigObservationForPath(observations, path)
			switch {
			case captureErr == nil && found && reflect.DeepEqual(actual.Snapshot, want):
				written = append(written, pdnsConfigWriteReceipt{before: before, after: actual})
			case captureErr == nil && found && reflect.DeepEqual(actual, before):
				// This replacement did not commit; only earlier writes need rollback.
			default:
				captureErr = errors.Join(
					captureErr,
					errors.New("PowerDNS config replacement outcome is ambiguous"),
				)
			}
			return errors.Join(
				fmt.Errorf("write managed PowerDNS configuration %s: %w", path, err),
				captureErr,
				rollbackPDNSConfigWrites(
					mutation.policy, written, mutation.originalSnapshots(), ops,
				),
			)
		}
		observations, err := ops.capture(mutation.policy)
		after, found := pdnsConfigObservationForPath(observations, path)
		if err != nil || !found || !reflect.DeepEqual(after.Snapshot, want) {
			if err == nil {
				err = errors.New("PowerDNS config replacement readback mismatch")
			}
			if found {
				written = append(written, pdnsConfigWriteReceipt{before: before, after: after})
			}
			return errors.Join(
				err,
				rollbackPDNSConfigWrites(
					mutation.policy, written, mutation.originalSnapshots(), ops,
				),
			)
		}
		written = append(written, pdnsConfigWriteReceipt{before: before, after: after})
		current[path] = after
	}
	if ops.beforeFinal != nil {
		ops.beforeFinal()
	}
	if _, err := capturePDNSConfigSnapshotsExactWithOps(
		mutation.policy, mutation.desiredSnapshots(), ops,
	); err != nil {
		return errors.Join(
			err,
			rollbackPDNSConfigWrites(
				mutation.policy, written, mutation.originalSnapshots(), ops,
			),
		)
	}
	return nil
}

func (mutation pdnsConfigMutation) applyOwnerAware(ctx context.Context) error {
	return mutation.applyOwnerAwareWithOps(ctx, hostPDNSConfigAccessOps())
}

func (mutation pdnsConfigMutation) restoreOwnerAwareWithOps(
	ctx context.Context,
	ops pdnsConfigAccessOps,
) error {
	current, err := mutation.captureOwnerAwareCurrentWithOps(ctx, false, ops)
	if err != nil {
		return err
	}
	before := pdnsConfigSnapshotMap(mutation.before)
	desired := pdnsConfigSnapshotMap(mutation.desiredSnapshots())
	var restoreErr error
	paths := pdnsConfigPaths()
	for index := len(paths) - 1; index >= 0; index-- {
		path := paths[index]
		actual := current[path]
		if reflect.DeepEqual(actual.Snapshot, before[path]) {
			continue
		}
		if !reflect.DeepEqual(actual.Snapshot, desired[path]) {
			restoreErr = errors.Join(
				restoreErr,
				errors.New("PowerDNS config changed before exact rollback"),
			)
			continue
		}
		if err := applyPDNSConfigReplacement(
			mutation.policy, actual, before[path], ops,
		); err != nil {
			observations, captureErr := ops.capture(mutation.policy)
			readback, found := pdnsConfigObservationForPath(observations, path)
			if captureErr == nil && found &&
				reflect.DeepEqual(readback.Snapshot, before[path]) {
				current[path] = readback
				continue
			}
			restoreErr = errors.Join(restoreErr, err, captureErr)
			continue
		}
		observations, captureErr := ops.capture(mutation.policy)
		readback, found := pdnsConfigObservationForPath(observations, path)
		if captureErr != nil || !found ||
			!reflect.DeepEqual(readback.Snapshot, before[path]) {
			if captureErr == nil {
				captureErr = errors.New("PowerDNS config restore readback mismatch")
			}
			restoreErr = errors.Join(restoreErr, captureErr)
			continue
		}
		current[path] = readback
	}
	if ops.beforeFinal != nil {
		ops.beforeFinal()
	}
	_, finalErr := capturePDNSConfigSnapshotsExactWithOps(
		mutation.policy, mutation.originalSnapshots(), ops,
	)
	return errors.Join(restoreErr, finalErr)
}

func (mutation pdnsConfigMutation) restoreOwnerAware(ctx context.Context) error {
	return mutation.restoreOwnerAwareWithOps(ctx, hostPDNSConfigAccessOps())
}

func prepareOwnerAwarePDNSConfigMutation(
	ctx context.Context,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	managedConfig []byte,
) (pdnsConfigMutation, error) {
	if ctx == nil {
		return pdnsConfigMutation{}, errors.New("PowerDNS config preparation requires a context")
	}
	if len(managedConfig) == 0 {
		return pdnsConfigMutation{}, errors.New("managed PowerDNS configuration is required")
	}
	policy, err := resolvePDNSConfigOwnerPolicy(ctx)
	if err != nil {
		return pdnsConfigMutation{}, err
	}
	observations, err := captureHostPDNSConfigObservations(policy)
	if err != nil {
		return pdnsConfigMutation{}, err
	}
	if err := validatePDNSConfigObservations(policy, observations); err != nil {
		return pdnsConfigMutation{}, err
	}
	before := make([]dnsFileSnapshot, len(observations))
	identities := make(map[string]pdnsConfigFileIdentity, len(observations))
	for index, observation := range observations {
		before[index] = observation.Snapshot
		identities[observation.Snapshot.Path] = observation.Identity
	}
	mutation, err := newPDNSConfigMutationFromSnapshots(
		policy, before, manifest, managedConfig,
	)
	if err != nil {
		return pdnsConfigMutation{}, err
	}
	mutation.originalIdentities = identities
	return mutation, nil
}

func pdnsConfigMutationFromJournal(
	ctx context.Context,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	journal dnsEngineSwitchJournal,
) (pdnsConfigMutation, error) {
	if ctx == nil {
		return pdnsConfigMutation{}, errors.New("PowerDNS config recovery requires a context")
	}
	managedConfig, err := managedPowerDNSStandaloneConfig(ctx)
	if err != nil {
		return pdnsConfigMutation{},
			fmt.Errorf("discover managed PowerDNS recovery listen addresses: %w", err)
	}
	policy, err := resolvePDNSConfigOwnerPolicy(ctx)
	if err != nil {
		return pdnsConfigMutation{}, err
	}
	mutation, err := newPDNSConfigMutationFromSnapshots(
		policy, journal.ConfigBefore, manifest, managedConfig,
	)
	if err != nil {
		return pdnsConfigMutation{}, err
	}
	if _, err := mutation.captureOwnerAwareCurrentWithOps(
		ctx, false, hostPDNSConfigAccessOps(),
	); err != nil {
		return pdnsConfigMutation{}, err
	}
	return mutation, nil
}

func pdnsConfigMode(snapshot dnsFileSnapshot) os.FileMode {
	return os.FileMode(snapshot.Mode)
}

func samePDNSConfigData(left, right dnsFileSnapshot) bool {
	return left.Path == right.Path && left.Exists == right.Exists &&
		left.Mode == right.Mode && left.OwnerKnown == right.OwnerKnown &&
		left.UID == right.UID && left.GID == right.GID &&
		left.SHA256 == right.SHA256 && bytes.Equal(left.Data, right.Data)
}
