package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/hostplatform"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

const bindSignedUpdatePreparationTimeout = 30 * time.Second

func bindSafeAPTCommandEnvironment() []string {
	return []string{
		"PATH=/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=/root",
		"LC_ALL=C",
	}
}

type bindSignedUpdatePreparationOps struct {
	checkIdle        func() error
	detectProfile    func() (hostplatform.Profile, error)
	readJournal      func() (dnsEngineSwitchJournal, bool, error)
	readLedger       func() (serviceMutationLedger, error)
	rollbackJournal  func(context.Context, dnsEngineSwitchJournal) error
	verifyRestored   func(context.Context, dnsEngineSwitchJournal) error
	writeJournal     func(dnsEngineSwitchJournal) error
	removeJournal    func() error
	readInstall      func() (dnsEngineInstallOwnershipReceipt, bool, error)
	readState        func() (dnsEngineStateReceipt, bool, error)
	readOwnership    func() (dnsEngineStateReceipt, bool, error)
	packageInstalled func(context.Context, hostplatform.Profile, string) (bool, error)
	parentExists     func() (bool, error)
	prepare          func(context.Context) error
	hardenExisting   func(context.Context) error
	verifyExisting   func(context.Context, dnsEngineStateReceipt) error
}

func prepareBINDGenerationRootForSignedUpdateUnderExternalLock(
	ctx context.Context,
	stateDir, lockPath string,
) error {
	return prepareBINDGenerationRootForSignedUpdateWithOps(
		ctx,
		bindSignedUpdatePreparationOps{
			checkIdle: func() error {
				return checkServiceMutationIdleUnderExternalLock(stateDir, lockPath)
			},
			detectProfile: verifiedHostProfileForAnyFamily,
			readJournal:   readDNSEngineSwitchJournal,
			readLedger: func() (serviceMutationLedger, error) {
				return readSignedUpdateServiceMutationLedgerUnderExternalLock(stateDir, lockPath)
			},
			rollbackJournal: rollbackInitialBINDJournalForSignedUpdate,
			verifyRestored:  verifyInitialBINDRollbackForSignedUpdate,
			writeJournal:    writeDNSEngineSwitchJournal,
			removeJournal:   removeDNSEngineSwitchJournal,
			readInstall: func() (dnsEngineInstallOwnershipReceipt, bool, error) {
				return readDNSEngineInstallOwnership(transport.DNSEngineBIND)
			},
			readState: readDNSEngineState,
			readOwnership: func() (dnsEngineStateReceipt, bool, error) {
				return readDNSEngineOwnership(transport.DNSEngineBIND)
			},
			packageInstalled: exactBINDPackageInstalledForSignedUpdate,
			parentExists: func() (bool, error) {
				_, err := os.Lstat(aptBINDCacheParentPath)
				if errors.Is(err, os.ErrNotExist) {
					return false, nil
				}
				return err == nil, err
			},
			prepare: func(prepareCtx context.Context) error {
				return prepareHostBINDGenerationRoot(prepareCtx, bindHostLayout{
					GenerationRoot: aptBINDGenerationRoot,
					Packages:       []string{"bind9"},
				})
			},
			hardenExisting: func(prepareCtx context.Context) error {
				return hardenExistingHostBINDGenerationRoot(
					prepareCtx,
					bindHostLayout{
						GenerationRoot: aptBINDGenerationRoot,
						Packages:       []string{"bind9"},
					},
				)
			},
			verifyExisting: verifyExistingManagedBINDGenerationForSignedUpdate,
		},
	)
}

func readSignedUpdateServiceMutationLedgerUnderExternalLock(
	stateDir, lockPath string,
) (serviceMutationLedger, error) {
	if strings.TrimSpace(stateDir) == "" {
		stateDir = serviceMutationStateDirectory()
	}
	if strings.TrimSpace(lockPath) == "" {
		lockPath = serviceMutationLockFile()
	}
	stateDir = filepath.Clean(stateDir)
	lockPath = filepath.Clean(lockPath)
	if !filepath.IsAbs(stateDir) || !filepath.IsAbs(lockPath) {
		return serviceMutationLedger{}, errors.New("signed-update service mutation paths must be absolute")
	}
	if err := verifyInheritedServiceMutationFileLock(lockPath); err != nil {
		return serviceMutationLedger{}, fmt.Errorf("verify inherited mutation lock before ledger read: %w", err)
	}
	manager := &serviceMutationManager{
		ledgerPath: filepath.Join(stateDir, serviceMutationLedgerFileName),
		lockPath:   lockPath,
	}
	return manager.loadLedgerFromDisk()
}

func signedUpdateRollbackEvidenceRequest(
	journal dnsEngineSwitchJournal,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) *transport.DNSEngineRollbackEvidenceRequest {
	return &transport.DNSEngineRollbackEvidenceRequest{
		ServiceMutationBinding: switchJournalBinding(journal),
		Mode:                   manifest.Mode,
		SourceEngine:           manifest.SourceEngine,
		TargetEngine:           manifest.TargetEngine,
		SourceEpoch:            manifest.SourceEpoch,
		TargetEpoch:            manifest.TargetEpoch,
		SourceRevision:         manifest.SourceRevision,
		Topology:               manifest.Topology,
		PairRole:               manifest.PairRole,
		LocalIP:                manifest.LocalIP,
		LocalNS:                manifest.LocalNS,
		PeerIP:                 manifest.PeerIP,
		PeerNS:                 manifest.PeerNS,
		Zones:                  append([]transport.DNSEngineSwitchZoneSnapshot(nil), manifest.Zones...),
		SnapshotBytes:          manifest.SnapshotBytes,
		ManifestQualifier:      manifest.Qualifier,
	}
}

func exactFailedSignedUpdateDNSEngineLedger(
	ledger serviceMutationLedger,
	request *transport.DNSEngineRollbackEvidenceRequest,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) (string, error) {
	if err := validateServiceMutationLedger(&ledger); err != nil {
		return "", fmt.Errorf("validate signed-update service mutation ledger: %w", err)
	}
	if ledger.ActiveRequestID != "" {
		return "", errors.New("signed-update service mutation ledger has an active request")
	}
	job := ledger.Jobs[request.MutationRequestID]
	if !exactFailedDNSEngineEvidenceJob(job, request, manifest) ||
		job.Phase != serviceMutationStatusFailed {
		return "", errors.New("signed-update DNS journal lacks its exact terminal failed ledger job")
	}
	commitment, err := failedDNSEngineReceiptCommitment(job)
	if err != nil {
		return "", fmt.Errorf("commit terminal failed DNS ledger job: %w", err)
	}
	return commitment, nil
}

func exactInitialBINDSignedUpdateRollbackJournal(
	journal dnsEngineSwitchJournal,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) bool {
	wantState := dnsFileSnapshot{Path: filepath.Clean(dnsEngineStatePath())}
	wantTarget := []dnsUnitSnapshot{
		{Name: "bind9.service", LoadState: "not-found", ActiveState: "inactive"},
		{Name: "named.service", LoadState: "not-found", ActiveState: "inactive"},
	}
	return initialBINDInstallRollbackEvidenceScope(manifest) &&
		!journal.HadPrevious && journal.PreviousGeneration == "" &&
		reflect.DeepEqual(journal.StateBefore, wantState) &&
		reflect.DeepEqual(journal.TargetUnitsBefore, wantTarget) &&
		len(journal.SourceUnitsBefore) == 0
}

func rollbackInitialBINDJournalForSignedUpdate(
	ctx context.Context,
	journal dnsEngineSwitchJournal,
) error {
	if ctx == nil {
		return errors.New("signed-update BIND rollback requires a context")
	}
	if err := validateDNSEngineSwitchJournal(journal); err != nil {
		return err
	}
	manifest, err := switchJournalManifest(journal)
	if err != nil {
		return err
	}
	if journal.Phase != dnsSwitchPhaseRollingBack ||
		!exactInitialBINDSignedUpdateRollbackJournal(journal, manifest) {
		return errors.New("signed-update recovery can mutate only an exact initial BIND rolling-back journal")
	}
	return rollbackDNSSwitchJournal(ctx, journal)
}

func verifyInitialBINDRollbackPreimageForSignedUpdate(
	ctx context.Context,
	journal dnsEngineSwitchJournal,
	profile hostplatform.Profile,
	layout bindHostLayout,
	systemctl string,
	request *transport.DNSEngineRollbackEvidenceRequest,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) error {
	configs, err := bindConfigMutationFromJournal(layout, "", journal)
	if err != nil {
		return err
	}
	if _, _, err := configs.captureOwnerAwareCurrent(ctx, true); err != nil {
		return fmt.Errorf("verify restored BIND config preimage: %w", err)
	}
	publisher, _, err := newHostBINDPublisher(ctx, layout)
	if err != nil {
		return err
	}
	current, exists, err := publisher.Current()
	if err != nil {
		return err
	}
	if exists != journal.HadPrevious ||
		(journal.HadPrevious && current != journal.PreviousGeneration) {
		return errors.New("restored BIND generation pointer differs from the journal preimage")
	}
	if err := verifyDNSFileSnapshotsExact([]dnsFileSnapshot{journal.StateBefore}); err != nil {
		return fmt.Errorf("verify restored DNS state preimage: %w", err)
	}
	guard := dnsSystemdStateGuard(systemctl)
	for _, snapshot := range journal.TargetUnitsBefore {
		before := bindInstallUnitState{
			name: snapshot.Name, loadState: snapshot.LoadState,
			activeState: snapshot.ActiveState, unitFileState: snapshot.UnitFileState,
		}
		if err := guard.verifyRestoredState(ctx, before); err != nil {
			return fmt.Errorf("verify restored BIND unit preimage: %w", err)
		}
	}
	if _, exists, err := readDNSEngineOwnership(transport.DNSEngineBIND); err != nil {
		return err
	} else if exists {
		return errors.New("rolled-back initial BIND target has a managed ownership receipt")
	}
	install, installExists, err := readDNSEngineInstallOwnership(transport.DNSEngineBIND)
	if err != nil {
		return err
	}
	wantMissing := append([]string(nil), layout.Packages...)
	sort.Strings(wantMissing)
	if validateDNSEngineInstallOwnership(install) != nil ||
		!exactDNSEngineInstallEvidence(install, request, manifest) ||
		!exactDNSEngineInstallOwnership(
			install, installExists, transport.DNSEngineBIND,
			profile.PackageManager, layout.Packages,
		) || !reflect.DeepEqual(install.MissingBefore, wantMissing) {
		return errors.New("rolled-back initial BIND install ownership is absent or different")
	}
	return nil
}

func verifyInitialBINDRollbackForSignedUpdate(
	ctx context.Context,
	journal dnsEngineSwitchJournal,
) error {
	if ctx == nil {
		return errors.New("signed-update BIND rollback proof requires a context")
	}
	if err := validateDNSEngineSwitchJournal(journal); err != nil {
		return err
	}
	manifest, err := switchJournalManifest(journal)
	if err != nil {
		return err
	}
	if (journal.Phase != dnsSwitchPhaseRollingBack &&
		journal.Phase != dnsSwitchPhaseRolledBack) ||
		!exactInitialBINDSignedUpdateRollbackJournal(journal, manifest) {
		return errors.New("signed-update BIND restored proof received another journal scope")
	}
	profile, err := verifiedHostProfileForAnyFamily()
	if err != nil {
		return err
	}
	if profile.PackageManager != hostplatform.PackageManagerAPT {
		return errors.New("signed-update BIND rollback proof requires an APT host")
	}
	layout, err := bindLayout(profile)
	if err != nil {
		return err
	}
	systemctl, err := executableForProfile(
		profile, string(profile.PackageManager), "systemctl",
	)
	if err != nil {
		return err
	}
	request := signedUpdateRollbackEvidenceRequest(journal, manifest)
	verifyPreimage := func() error {
		return verifyInitialBINDRollbackPreimageForSignedUpdate(
			ctx, journal, profile, layout, systemctl, request, manifest,
		)
	}
	if err := verifyPreimage(); err != nil {
		return err
	}
	if err := verifyRestoredDNSSwitchSource(
		ctx, profile, systemctl, manifest, journal,
	); err != nil {
		return err
	}
	return verifyPreimage()
}

func recoverDNSEngineSwitchJournalForSignedUpdate(
	ctx context.Context,
	ops bindSignedUpdatePreparationOps,
) (bool, error) {
	firstJournal, exists, err := ops.readJournal()
	if err != nil {
		return false, fmt.Errorf("inspect DNS engine switch journal before signed-update recovery: %w", err)
	}
	if !exists {
		return false, nil
	}
	if err := validateDNSEngineSwitchJournal(firstJournal); err != nil {
		return false, fmt.Errorf("validate signed-update DNS engine switch journal: %w", err)
	}
	if firstJournal.Phase != dnsSwitchPhaseRollingBack &&
		firstJournal.Phase != dnsSwitchPhaseRolledBack {
		return false, errors.New("signed-update recovery accepts only a DNS journal in a rollback phase")
	}
	manifest, err := switchJournalManifest(firstJournal)
	if err != nil {
		return false, err
	}
	request := signedUpdateRollbackEvidenceRequest(firstJournal, manifest)
	canonicalManifest, err := canonicalDNSEngineRollbackEvidence(request)
	if err != nil || !reflect.DeepEqual(canonicalManifest, manifest) {
		if err == nil {
			err = errors.New("signed-update rollback request differs from its journal manifest")
		}
		return false, err
	}
	if !exactInitialBINDSignedUpdateRollbackJournal(firstJournal, manifest) {
		return false, errors.New("signed-update DNS journal is outside the exact initial BIND rollback scope")
	}

	firstLedger, err := ops.readLedger()
	if err != nil {
		return false, fmt.Errorf("read service mutation ledger before signed-update DNS rollback: %w", err)
	}
	firstCommitment, err := exactFailedSignedUpdateDNSEngineLedger(
		firstLedger, request, manifest,
	)
	if err != nil {
		return false, err
	}
	secondJournal, exists, err := ops.readJournal()
	if err != nil {
		return false, fmt.Errorf("reinspect DNS engine switch journal before rollback: %w", err)
	}
	if !exists || !reflect.DeepEqual(firstJournal, secondJournal) {
		return false, errors.New("DNS engine switch journal changed before signed-update rollback")
	}
	recoveredJournal := secondJournal
	if secondJournal.Phase == dnsSwitchPhaseRollingBack {
		if err := ops.rollbackJournal(ctx, secondJournal); err != nil {
			return false, fmt.Errorf("resume signed-update DNS journal rollback: %w", err)
		}
		if err := ops.verifyRestored(ctx, secondJournal); err != nil {
			return false, fmt.Errorf("verify signed-update DNS rollback host state: %w", err)
		}
		recoveredJournal.Phase = dnsSwitchPhaseRolledBack
		if err := ops.writeJournal(recoveredJournal); err != nil {
			return false, fmt.Errorf("persist signed-update DNS rolled-back phase: %w", err)
		}
	} else if err := ops.verifyRestored(ctx, secondJournal); err != nil {
		return false, fmt.Errorf("verify signed-update DNS rollback host state: %w", err)
	}
	thirdJournal, exists, err := ops.readJournal()
	if err != nil {
		return false, fmt.Errorf("reinspect DNS engine switch journal after rollback proof: %w", err)
	}
	if !exists || !reflect.DeepEqual(recoveredJournal, thirdJournal) {
		return false, errors.New("DNS engine switch journal changed during signed-update rollback proof")
	}
	secondLedger, err := ops.readLedger()
	if err != nil {
		return false, fmt.Errorf("read service mutation ledger after signed-update DNS rollback: %w", err)
	}
	secondCommitment, err := exactFailedSignedUpdateDNSEngineLedger(
		secondLedger, request, manifest,
	)
	if err != nil {
		return false, err
	}
	if firstCommitment != secondCommitment || !reflect.DeepEqual(firstLedger, secondLedger) {
		return false, errors.New("service mutation ledger changed during signed-update DNS rollback")
	}
	if err := ops.checkIdle(); err != nil {
		return false, fmt.Errorf("reverify external mutation lock and idle ledger after DNS recovery: %w", err)
	}
	finalJournal, exists, err := ops.readJournal()
	if err != nil {
		return false, fmt.Errorf("reinspect DNS engine switch journal before removal: %w", err)
	}
	if !exists || !reflect.DeepEqual(recoveredJournal, finalJournal) {
		return false, errors.New("DNS engine switch journal changed before signed-update removal")
	}
	if err := ops.removeJournal(); err != nil {
		return false, fmt.Errorf("remove recovered DNS engine switch journal: %w", err)
	}
	return true, nil
}

func prepareBINDGenerationRootForSignedUpdateWithOps(
	ctx context.Context,
	ops bindSignedUpdatePreparationOps,
) error {
	if ctx == nil || ops.checkIdle == nil || ops.detectProfile == nil ||
		ops.readJournal == nil || ops.readLedger == nil ||
		ops.rollbackJournal == nil || ops.verifyRestored == nil ||
		ops.writeJournal == nil || ops.removeJournal == nil ||
		ops.readInstall == nil ||
		ops.readState == nil || ops.readOwnership == nil ||
		ops.packageInstalled == nil || ops.parentExists == nil ||
		ops.prepare == nil || ops.hardenExisting == nil ||
		ops.verifyExisting == nil {
		return errors.New("invalid signed-update BIND root preparation")
	}
	if err := ops.checkIdle(); err != nil {
		return fmt.Errorf("reverify external mutation lock and idle ledger: %w", err)
	}
	profile, err := ops.detectProfile()
	if err != nil {
		return err
	}
	if profile.PackageManager != hostplatform.PackageManagerAPT {
		return nil
	}
	if _, err := recoverDNSEngineSwitchJournalForSignedUpdate(ctx, ops); err != nil {
		return err
	}
	install, installExists, err := ops.readInstall()
	if err != nil {
		return fmt.Errorf("inspect BIND install ownership: %w", err)
	}
	if installExists {
		if err := validateDNSEngineInstallOwnership(install); err != nil {
			return err
		}
		if !exactDNSEngineInstallOwnership(
			install, true, transport.DNSEngineBIND,
			hostplatform.PackageManagerAPT, []string{"bind9"},
		) {
			return errors.New("BIND install ownership differs from the supported APT package set")
		}
	}
	state, stateExists, err := ops.readState()
	if err != nil {
		return fmt.Errorf("inspect DNS engine state: %w", err)
	}
	if stateExists {
		if err := validateDNSEngineState(state); err != nil {
			return err
		}
	}
	ownership, ownershipExists, err := ops.readOwnership()
	if err != nil {
		return fmt.Errorf("inspect BIND engine ownership: %w", err)
	}
	if ownershipExists {
		if err := validateDNSEngineState(ownership); err != nil {
			return err
		}
		if ownership.Engine != transport.DNSEngineBIND {
			return errors.New("BIND ownership receipt names another DNS engine")
		}
	}
	managedState := dnsEngineStateReceipt{}
	managedStateExists := false
	if stateExists && state.Engine == transport.DNSEngineBIND {
		managedState = state
		managedStateExists = true
	}
	if ownershipExists {
		if managedStateExists && ownership != managedState {
			return errors.New("BIND state and ownership receipts disagree")
		}
		managedState = ownership
		managedStateExists = true
	}
	if installExists && managedStateExists {
		return errors.New(
			"BIND transitional install ownership coexists with managed engine state",
		)
	}
	provenance := installExists ||
		managedStateExists
	if !provenance {
		return nil
	}
	if !supportedAPTBindLegacyRootProfile(profile) {
		return errors.New("existing managed BIND root lacks the required verified APT and systemd capabilities")
	}
	installed, err := ops.packageInstalled(ctx, profile, "bind9")
	if err != nil {
		return fmt.Errorf("verify exact bind9 package status: %w", err)
	}
	if !installed {
		return errors.New("managed BIND provenance exists but the exact bind9 package is absent")
	}
	exists, err := ops.parentExists()
	if err != nil {
		return fmt.Errorf("inspect legacy BIND cache parent: %w", err)
	}
	if !exists {
		return errors.New("managed BIND provenance exists but /var/cache/bind is absent")
	}
	if installExists {
		if err := ops.prepare(ctx); err != nil {
			return err
		}
		return nil
	}
	if err := ops.hardenExisting(ctx); err != nil {
		return fmt.Errorf("harden existing managed BIND root: %w", err)
	}
	if err := ops.verifyExisting(ctx, managedState); err != nil {
		return fmt.Errorf("verify existing managed BIND generation: %w", err)
	}
	return nil
}

func exactBINDPackageInstalledForSignedUpdate(
	ctx context.Context,
	profile hostplatform.Profile,
	packageName string,
) (bool, error) {
	if ctx == nil || packageName != "bind9" ||
		profile.PackageManager != hostplatform.PackageManagerAPT {
		return false, errors.New("invalid signed-update BIND package proof")
	}
	dpkgQuery, err := executableForProfile(
		profile, string(profile.PackageManager), "dpkg-query",
	)
	if err != nil {
		return false, err
	}
	return exactBINDPackageInstalledForSignedUpdateWithRunner(
		ctx, dpkgQuery, packageName,
		func(commandCtx context.Context, name string, args ...string) ([]byte, error) {
			command := serviceMutationCommand(commandCtx, name, args...)
			command.Env = bindSafeAPTCommandEnvironment()
			return command.CombinedOutputLimited(4 << 10)
		},
	)
}

type bindPackageStatusRunner func(context.Context, string, ...string) ([]byte, error)

func exactBINDPackageInstalledForSignedUpdateWithRunner(
	ctx context.Context,
	dpkgQuery, packageName string,
	runner bindPackageStatusRunner,
) (bool, error) {
	if ctx == nil || dpkgQuery == "" || packageName != "bind9" || runner == nil {
		return false, errors.New("invalid signed-update BIND package command proof")
	}
	output, err := runner(
		ctx, dpkgQuery, "-W", "-f", "${Status}", "--", packageName,
	)
	if err != nil {
		return false, err
	}
	if string(output) != "install ok installed" {
		return false, errors.New("dpkg-query returned a non-canonical bind9 package status")
	}
	return true, nil
}

func verifyExistingManagedBINDGenerationForSignedUpdate(
	ctx context.Context,
	state dnsEngineStateReceipt,
) error {
	if ctx == nil {
		return errors.New("existing managed BIND proof requires a context")
	}
	if err := validateDNSEngineState(state); err != nil {
		return err
	}
	if state.Engine != transport.DNSEngineBIND {
		return errors.New("existing managed BIND proof names another engine")
	}
	profile, err := verifiedHostProfileForAnyFamily()
	if err != nil {
		return err
	}
	layout, err := bindLayout(profile)
	if err != nil {
		return err
	}
	publisher, _, err := newHostBINDPublisher(ctx, layout)
	if err != nil {
		return err
	}
	tree, err := publisher.LoadCurrent()
	if err != nil {
		return fmt.Errorf("load existing managed BIND generation: %w", err)
	}
	return verifyExistingManagedBINDTreeForSignedUpdateOwnerAware(ctx, layout, state, tree)
}

func verifyExistingManagedBINDTreeForSignedUpdateOwnerAware(
	ctx context.Context,
	layout bindHostLayout,
	state dnsEngineStateReceipt,
	tree binddns.VerifiedTree,
) error {
	receipt := tree.CurrentReceipt()
	if receipt.EngineEpoch != state.EngineEpoch || receipt.Generation != state.Generation {
		return errors.New("existing BIND current receipt differs from managed state")
	}
	legacyOptions, err := bindStateTreePairContract(
		layout.GenerationRoot, state, tree, false, true, false,
	)
	if err != nil {
		return err
	}
	return verifyManagedBINDRuntimeConfigExact(
		ctx, layout, receipt, legacyOptions,
	)
}

func verifyExistingManagedBINDTreeForSignedUpdate(
	layout bindHostLayout,
	state dnsEngineStateReceipt,
	tree binddns.VerifiedTree,
) error {
	return verifyExistingManagedBINDTreeForSignedUpdateWithSnapshotReader(
		layout, state, tree, captureDNSFileSnapshot,
	)
}

func verifyExistingManagedBINDTreeForSignedUpdateWithSnapshotReader(
	layout bindHostLayout,
	state dnsEngineStateReceipt,
	tree binddns.VerifiedTree,
	readSnapshot bindConfigSnapshotReader,
) error {
	receipt := tree.CurrentReceipt()
	if receipt.EngineEpoch != state.EngineEpoch ||
		receipt.Generation != state.Generation {
		return errors.New("existing BIND current receipt differs from managed state")
	}
	legacyOptions, err := bindStateTreePairContract(
		layout.GenerationRoot, state, tree, false, true, false,
	)
	if err != nil {
		return err
	}
	if err := verifyManagedBINDRuntimeConfigExactWithSnapshotReader(
		layout, receipt, legacyOptions, readSnapshot,
	); err != nil {
		return err
	}
	return nil
}

func supportedAPTBindLegacyRootProfile(profile hostplatform.Profile) bool {
	return certifyAPTBINDCapabilities(profile) == nil
}
