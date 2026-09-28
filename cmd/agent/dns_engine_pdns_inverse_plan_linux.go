//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
	"github.com/alicelik/celikpanel/internal/hostplatform"
	"github.com/alicelik/celikpanel/internal/transport"
	"golang.org/x/sys/unix"
)

// prepareStandalonePDNSTargetInverseIntentV4 freezes the owner-aware PowerDNS
// config after-image before the first config write. This helper is deliberately
// not wired to the running switch: the independent inverse executor and
// runtime compatibility must be completed before v4 may be emitted in production.
func prepareStandalonePDNSTargetInverseIntentV4(
	ctx context.Context,
	profile hostplatform.Profile,
	base dnsEngineSwitchJournal,
	configs pdnsConfigMutation,
) (dnsEngineSwitchJournal, error) {
	if profile.DistroFamily != hostplatform.DistroFamilyDebian ||
		profile.PackageManager != hostplatform.PackageManagerAPT ||
		profile.ServiceManager != hostplatform.ServiceManagerSystemd {
		return dnsEngineSwitchJournal{}, errors.New("v4 PowerDNS inverse intent supports only Debian-family APT with systemd")
	}
	if base.Schema != dnsengineartifact.SwitchJournalSchemaV1 ||
		base.Phase != dnsSwitchPhaseIntent ||
		base.Mode != transport.DNSEngineSwitchModeSwitch ||
		base.Topology != transport.DNSTopologyStandalone ||
		base.SourceEngine != transport.DNSEngineBIND ||
		base.TargetEngine != transport.DNSEnginePowerDNS ||
		base.PairRole != "" || base.PDNSBackupSHA256 != "" ||
		base.PDNSBackupSize != 0 || base.PDNSLiveSHA256 != "" ||
		base.PDNSLiveSize != 0 || !base.StateBefore.Exists {
		return dnsEngineSwitchJournal{}, errors.New("v4 PowerDNS inverse intent requires a standalone managed BIND source and an absent prior PowerDNS database")
	}
	if len(base.TargetUnitsBefore) != 1 ||
		base.TargetUnitsBefore[0].Name != "pdns.service" ||
		base.TargetUnitsBefore[0].LoadState != "loaded" ||
		base.TargetUnitsBefore[0].ActiveState != "inactive" ||
		base.TargetUnitsBefore[0].UnitFileState != "disabled" {
		return dnsEngineSwitchJournal{}, errors.New("v4 PowerDNS inverse intent requires an initially inactive PowerDNS target")
	}
	if !dnsengineartifact.UnitSnapshotNamesEqual(
		base.SourceUnitsBefore, []string{"bind9.service", "named.service"},
	) || base.SourceUnitsBefore[0].LoadState != "loaded" ||
		base.SourceUnitsBefore[1].LoadState != "loaded" ||
		base.SourceUnitsBefore[1].ActiveState != "active" {
		return dnsEngineSwitchJournal{}, errors.New("v4 PowerDNS inverse intent requires a running BIND source")
	}
	if err := configs.validateOwnerAware(); err != nil {
		return dnsEngineSwitchJournal{}, fmt.Errorf("v4 PowerDNS owner-aware config: %w", err)
	}
	if len(configs.before) != len(base.ConfigBefore) {
		return dnsEngineSwitchJournal{}, errors.New("v4 PowerDNS config preimage differs from the journal")
	}
	for i := range configs.before {
		if !reflect.DeepEqual(configs.before[i], base.ConfigBefore[i]) {
			return dnsEngineSwitchJournal{}, errors.New("v4 PowerDNS config preimage differs from the journal")
		}
	}
	if err := dnsJournalPolicy().ValidateSwitchJournal(base); err != nil {
		return dnsEngineSwitchJournal{}, fmt.Errorf("v4 PowerDNS base intent: %w", err)
	}
	sourceState, exists, err := dnsengineartifact.SourceStateFromSwitchJournal(base)
	if err != nil || !exists || sourceState.Generation == "" {
		return dnsEngineSwitchJournal{}, errors.Join(errors.New("v4 PowerDNS inverse intent requires a frozen managed BIND generation"), err)
	}
	gid, err := resolveBINDGroupGID(ctx)
	if err != nil {
		return dnsEngineSwitchJournal{}, err
	}
	sourceProof, err := dnsenginerecovery.CaptureManagedBINDSourceProofV4(
		ctx, sourceState.Generation, sourceState.EngineEpoch, gid,
	)
	if err != nil {
		return dnsEngineSwitchJournal{}, fmt.Errorf("capture v4 managed BIND source: %w", err)
	}
	return buildStandalonePDNSTargetInverseIntentV4(base, configs.desiredSnapshots(), sourceProof)
}

// pdnsSwitchCandidatePathV4 lives under the agent's root-controlled private
// state parent, outside the PowerDNS-writable database directory.
func pdnsSwitchCandidatePathV4(requestID string) string {
	return filepath.Join(filepath.Dir(dnsEngineStatePath()), ".celikpanel-switch-"+requestID+".sqlite3")
}

// verifyPDNSTargetStageFilesystemV4 is a preflight. The later native rename
// must still fail closed if the mount changes after this check.
func verifyPDNSTargetStageFilesystemV4(candidatePath string) error {
	privateDir := filepath.Dir(candidatePath)
	liveDir := filepath.Dir(pdnsDBPath())
	if privateDir == liveDir {
		return errors.New("v4 PowerDNS candidate is inside the daemon-writable database parent")
	}
	openDir := func(path string) (unix.Stat_t, error) {
		var stat unix.Stat_t
		fd, err := unix.Openat2(unix.AT_FDCWD, path, &unix.OpenHow{
			Flags:   unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW,
			Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
		})
		if err != nil {
			return stat, err
		}
		err = unix.Fstat(fd, &stat)
		return stat, errors.Join(err, unix.Close(fd))
	}
	private, err := openDir(privateDir)
	if err != nil {
		return fmt.Errorf("inspect private PowerDNS staging directory: %w", err)
	}
	if private.Uid != 0 || private.Mode&0o7777 != 0o700 {
		return errors.New("v4 PowerDNS staging directory is not root-controlled")
	}
	live, err := openDir(liveDir)
	if err != nil {
		return fmt.Errorf("inspect PowerDNS database directory: %w", err)
	}
	if private.Dev == 0 || private.Dev != live.Dev {
		return errors.New("v4 PowerDNS staging and live database directories are on different filesystems")
	}
	return nil
}

// buildStandalonePDNSTargetInverseIntentV4 refuses to create the first durable
// V4 intent until an already-built candidate has a stable native file proof.
// Building the candidate must precede this call and may not change DNS config.
func buildStandalonePDNSTargetInverseIntentV4(
	base dnsEngineSwitchJournal,
	configAfter []dnsFileSnapshot,
	sourceProof dnsengineartifact.ManagedBINDSourceProofV4,
) (dnsEngineSwitchJournal, error) {
	if err := dnsJournalPolicy().ValidateSwitchJournal(base); err != nil {
		return dnsEngineSwitchJournal{}, fmt.Errorf("v4 PowerDNS base intent: %w", err)
	}
	candidatePath := pdnsSwitchCandidatePathV4(base.MutationRequestID)
	if err := verifyPDNSTargetStageFilesystemV4(candidatePath); err != nil {
		return dnsEngineSwitchJournal{}, err
	}
	candidateProof, err := dnsenginerecovery.CapturePDNSTargetCandidateV4(candidatePath)
	if err != nil {
		return dnsEngineSwitchJournal{}, fmt.Errorf("capture v4 PowerDNS candidate before intent: %w", err)
	}
	return dnsJournalPolicy().BuildPDNSTargetInverseJournalV4(
		base, configAfter, sourceProof, candidateProof,
	)
}

// stageStandalonePDNSTargetCandidateV4 rechecks the exact SQLite inode already
// frozen in the durable intent before the BIND source can be stopped.
// Capture is read-only and refuses symlinks, hardlinks, changes and sidecars.
func stageStandalonePDNSTargetCandidateV4(
	ctx context.Context,
	intent dnsEngineSwitchJournal,
) (dnsEngineSwitchJournal, error) {
	return stageStandalonePDNSTargetCandidateWithSourceCheckV4(ctx, intent,
		func(ctx context.Context, source dnsengineartifact.ManagedBINDSourceProofV4) error {
			gid, err := resolveBINDGroupGID(ctx)
			if err != nil {
				return err
			}
			return dnsenginerecovery.VerifyManagedBINDSourceProofV4(ctx, source, gid)
		},
	)
}

func stageStandalonePDNSTargetCandidateWithSourceCheckV4(
	ctx context.Context,
	intent dnsEngineSwitchJournal,
	verifySource func(context.Context, dnsengineartifact.ManagedBINDSourceProofV4) error,
) (dnsEngineSwitchJournal, error) {
	if ctx == nil || verifySource == nil {
		return dnsEngineSwitchJournal{}, errors.New("v4 PowerDNS source verification is required")
	}
	if intent.Schema != dnsengineartifact.SwitchJournalSchemaV4 ||
		intent.Phase != dnsSwitchPhaseIntent || intent.PDNSTargetPlan == nil {
		return dnsEngineSwitchJournal{}, errors.New("v4 PowerDNS candidate requires a sealed intent")
	}
	if err := dnsJournalPolicy().ValidateSwitchJournal(intent); err != nil {
		return dnsEngineSwitchJournal{}, fmt.Errorf("v4 PowerDNS frozen intent: %w", err)
	}
	if err := verifyPDNSTargetStageFilesystemV4(intent.PDNSCandidatePath); err != nil {
		return dnsEngineSwitchJournal{}, err
	}
	proof, err := dnsenginerecovery.CapturePDNSTargetCandidateV4(intent.PDNSCandidatePath)
	if err != nil {
		return dnsEngineSwitchJournal{}, fmt.Errorf("capture v4 PowerDNS candidate: %w", err)
	}
	if err := verifySource(ctx, *intent.PDNSTargetPlan.SourceBIND); err != nil {
		return dnsEngineSwitchJournal{}, fmt.Errorf("verify v4 managed BIND source before stopping it: %w", err)
	}
	return dnsJournalPolicy().AttachPDNSTargetCandidateV4(intent, proof)
}
