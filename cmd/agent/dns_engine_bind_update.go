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
	checkIdle           func() error
	detectProfile       func() (hostplatform.Profile, error)
	readJournal         func() (dnsEngineSwitchJournal, bool, error)
	readLedger          func() (serviceMutationLedger, error)
	rollbackJournal     func(context.Context, dnsEngineSwitchJournal) error
	verifyRestored      func(context.Context, dnsEngineSwitchJournal) error
	verifyTarget        func(context.Context, dnsEngineSwitchJournal) error
	writeJournal        func(dnsEngineSwitchJournal) error
	removeJournal       func() error
	readInstall         func() (dnsEngineInstallOwnershipReceipt, bool, error)
	readEngineInstall   func(transport.DNSEngine) (dnsEngineInstallOwnershipReceipt, bool, error)
	readState           func() (dnsEngineStateReceipt, bool, error)
	readOwnership       func() (dnsEngineStateReceipt, bool, error)
	readEngineOwnership func(transport.DNSEngine) (dnsEngineStateReceipt, bool, error)
	writeOwnership      func(dnsEngineStateReceipt) error
	finalizeArtifacts   func(dnsEngineSwitchJournal) error
	retireInstall       func(dnsEngineSwitchJournal) error
	packageInstalled    func(context.Context, hostplatform.Profile, string) (bool, error)
	parentExists        func() (bool, error)
	prepare             func(context.Context) error
	hardenExisting      func(context.Context) error
	verifyExisting      func(context.Context, dnsEngineStateReceipt) error
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
			verifyTarget:    verifyDNSSwitchJournalTarget,
			writeJournal: func(journal dnsEngineSwitchJournal) error {
				return writeDNSEngineSwitchJournalForFaultDriver(
					dnsEngineSwitchFaultDriverSignedUpdateFinalize, journal,
				)
			},
			removeJournal: removeDNSEngineSwitchJournal,
			readInstall: func() (dnsEngineInstallOwnershipReceipt, bool, error) {
				return readDNSEngineInstallOwnership(transport.DNSEngineBIND)
			},
			readEngineInstall: readDNSEngineInstallOwnership,
			readState:         readDNSEngineState,
			readOwnership: func() (dnsEngineStateReceipt, bool, error) {
				return readDNSEngineOwnership(transport.DNSEngineBIND)
			},
			readEngineOwnership: readDNSEngineOwnership,
			writeOwnership:      writeDNSEngineOwnership,
			finalizeArtifacts:   finalizeCommittedDNSEngineSwitchArtifacts,
			retireInstall:       retireDNSEngineInstallOwnership,
			packageInstalled:    exactBINDPackageInstalledForSignedUpdate,
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

func exactSucceededSignedUpdateDNSEngineLedger(
	ledger serviceMutationLedger,
	journal dnsEngineSwitchJournal,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) error {
	if err := validateServiceMutationLedger(&ledger); err != nil {
		return fmt.Errorf("validate signed-update service mutation ledger: %w", err)
	}
	if ledger.ActiveRequestID != "" {
		return errors.New("signed-update service mutation ledger has an active request")
	}
	job := ledger.Jobs[journal.MutationRequestID]
	wantPhase, err := formatDNSEngineSwitchPublishedPhase(
		journal.MutationRequestID, manifest.Qualifier,
	)
	if err != nil {
		return err
	}
	if job == nil || job.RequestID != journal.MutationRequestID ||
		job.OwnerID != journal.MutationOwnerID ||
		job.Kind != "dns_engine_switch" ||
		job.Target != string(manifest.TargetEngine) ||
		job.PackageName != manifest.Qualifier ||
		job.Status != serviceMutationStatusSucceeded || job.Phase != wantPhase ||
		job.Attempt <= 0 || job.StartedAt.IsZero() || job.UpdatedAt.IsZero() ||
		job.DeadlineAt.IsZero() || job.FinishedAt.IsZero() ||
		job.UpdatedAt.Before(job.StartedAt) ||
		job.DeadlineAt.Before(job.StartedAt) ||
		job.FinishedAt.Before(job.StartedAt) ||
		!job.UpdatedAt.Equal(job.FinishedAt) ||
		!job.LeaseExpiresAt.IsZero() || job.WorkerPID != 0 ||
		strings.TrimSpace(job.WorkerStarted) != "" ||
		strings.TrimSpace(job.WorkerCommand) != "" ||
		strings.TrimSpace(job.ErrorCode) != "" ||
		strings.TrimSpace(job.ErrorMessage) != "" {
		return errors.New("signed-update DNS journal lacks its exact published success ledger job")
	}
	return nil
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

func signedUpdateDNSEngineInstallOwnership(
	ops bindSignedUpdatePreparationOps,
	engine transport.DNSEngine,
) (dnsEngineInstallOwnershipReceipt, bool, error) {
	if ops.readEngineInstall != nil {
		return ops.readEngineInstall(engine)
	}
	if engine == transport.DNSEngineBIND && ops.readInstall != nil {
		return ops.readInstall()
	}
	return dnsEngineInstallOwnershipReceipt{}, false,
		errors.New("signed-update target install ownership reader is unavailable")
}

func signedUpdateDNSEngineOwnership(
	ops bindSignedUpdatePreparationOps,
	engine transport.DNSEngine,
) (dnsEngineStateReceipt, bool, error) {
	if ops.readEngineOwnership != nil {
		return ops.readEngineOwnership(engine)
	}
	if engine == transport.DNSEngineBIND && ops.readOwnership != nil {
		return ops.readOwnership()
	}
	return dnsEngineStateReceipt{}, false,
		errors.New("signed-update target ownership reader is unavailable")
}

func exactCommittedDNSEngineSignedUpdateProvenance(
	journal dnsEngineSwitchJournal,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	profile hostplatform.Profile,
	ops bindSignedUpdatePreparationOps,
) (dnsEngineStateReceipt, bool, bool, error) {
	if journal.Phase != dnsSwitchPhaseCommitted ||
		!transport.ValidDNSEngine(journal.TargetEngine) {
		return dnsEngineStateReceipt{}, false, false,
			errors.New("signed-update committed recovery requires an exact DNS engine target")
	}
	state, stateExists, err := ops.readState()
	if err != nil {
		return dnsEngineStateReceipt{}, false, false,
			fmt.Errorf("read committed DNS engine state receipt: %w", err)
	}
	if !stateExists || validateDNSEngineState(state) != nil ||
		!exactDNSEngineStateForJournal(state, journal) {
		return dnsEngineStateReceipt{}, false, false,
			errors.New("committed DNS engine state receipt is absent or differs from its journal")
	}
	install, installExists, err := signedUpdateDNSEngineInstallOwnership(
		ops, journal.TargetEngine,
	)
	if err != nil {
		return dnsEngineStateReceipt{}, false, false,
			fmt.Errorf("read committed DNS engine install ownership: %w", err)
	}
	if installExists {
		packages, err := managedDNSEnginePackagesForProfile(profile, journal.TargetEngine)
		if err != nil {
			return dnsEngineStateReceipt{}, false, false, err
		}
		request := signedUpdateRollbackEvidenceRequest(journal, manifest)
		if validateDNSEngineInstallOwnership(install) != nil ||
			!exactDNSEngineInstallEvidence(install, request, manifest) ||
			!exactDNSEngineInstallOwnership(
				install, true, journal.TargetEngine,
				profile.PackageManager, packages,
			) {
			return dnsEngineStateReceipt{}, false, false,
				errors.New("committed DNS engine install ownership differs from its journal")
		}
	}
	ownership, ownershipExists, err := signedUpdateDNSEngineOwnership(
		ops, journal.TargetEngine,
	)
	if err != nil {
		return dnsEngineStateReceipt{}, false, false,
			fmt.Errorf("read committed DNS engine ownership: %w", err)
	}
	// Same rule as the host path, from the same function. A receipt left at an
	// older epoch of the same engine — what returning to a previously used
	// engine always leaves behind — is provenance, not a contradiction, and the
	// publish a few steps later refreshes it.
	// Host yolundaki kuralın aynısı, aynı fonksiyondan. Aynı motorun daha eski
	// bir çağında kalmış makbuz — daha önce kullanılmış bir motora dönmenin her
	// zaman geride bıraktığı şey — bir çelişki değil köken kanıtıdır ve birkaç
	// adım sonraki yayım onu tazeler.
	if ownershipExists {
		if err := acceptableCommittedDNSEngineOwnership(ownership, state); err != nil {
			return dnsEngineStateReceipt{}, false, false, err
		}
	}
	if journal.Mode != transport.DNSEngineSwitchModeAdopt {
		if !installExists {
			if !ownershipExists {
				return dnsEngineStateReceipt{}, false, false,
					errors.New("committed DNS engine switch has no exact install or active ownership provenance")
			}
		}
	}
	return state, installExists, ownershipExists, nil
}

func recoverCommittedDNSEngineSwitchJournalForSignedUpdate(
	ctx context.Context,
	firstJournal dnsEngineSwitchJournal,
	ops bindSignedUpdatePreparationOps,
) (bool, error) {
	manifest, err := switchJournalManifest(firstJournal)
	if err != nil {
		return false, err
	}
	if ops.detectProfile == nil {
		return false, errors.New("signed-update committed recovery host profile detector is unavailable")
	}
	profile, err := ops.detectProfile()
	if err != nil {
		return false, err
	}
	firstLedger, err := ops.readLedger()
	if err != nil {
		return false, fmt.Errorf("read service mutation ledger before committed DNS engine recovery: %w", err)
	}
	if err := exactSucceededSignedUpdateDNSEngineLedger(
		firstLedger, firstJournal, manifest,
	); err != nil {
		return false, err
	}
	secondJournal, exists, err := ops.readJournal()
	if err != nil {
		return false, fmt.Errorf("reinspect committed DNS engine journal before target proof: %w", err)
	}
	if !exists || !reflect.DeepEqual(firstJournal, secondJournal) {
		return false, errors.New("committed DNS engine journal changed before signed-update target proof")
	}
	if err := ops.verifyTarget(ctx, secondJournal); err != nil {
		return false, fmt.Errorf("verify committed DNS engine target before provenance finalization: %w", err)
	}
	state, _, _, err := exactCommittedDNSEngineSignedUpdateProvenance(
		secondJournal, manifest, profile, ops,
	)
	if err != nil {
		return false, err
	}
	ownership, ownershipExists, err := signedUpdateDNSEngineOwnership(
		ops, secondJournal.TargetEngine,
	)
	if err != nil {
		return false, fmt.Errorf("reinspect committed DNS engine ownership: %w", err)
	}
	// This is the publish point, so a superseded receipt is overwritten rather
	// than merely tolerated: an absent receipt and one left at an older epoch of
	// the same engine both mean "the current state has not been published yet".
	// Anything else that differs is a genuine change under our feet and refuses.
	// Burası yayım noktası; dolayısıyla aşılmış bir makbuz yalnızca hoş
	// görülmez, üzerine yazılır: olmayan bir makbuz da aynı motorun daha eski bir
	// çağında kalmış bir makbuz da "güncel durum henüz yayımlanmadı" demektir.
	// Bundan farklı olan her şey ayağımızın altında gerçek bir değişikliktir ve
	// reddedilir.
	if !ownershipExists || supersededDNSEngineOwnership(ownership, state) {
		if err := ops.writeOwnership(state); err != nil {
			return false, fmt.Errorf("publish committed DNS engine ownership: %w", err)
		}
	} else if ownership != state {
		return false, errors.New("committed DNS engine ownership changed before finalization")
	}
	_, _, ownershipExists, err = exactCommittedDNSEngineSignedUpdateProvenance(
		secondJournal, manifest, profile, ops,
	)
	if err != nil {
		return false, fmt.Errorf("verify committed DNS engine ownership publication: %w", err)
	}
	if !ownershipExists {
		return false, errors.New("committed DNS engine ownership is absent after publication")
	}
	thirdJournal, exists, err := ops.readJournal()
	if err != nil {
		return false, fmt.Errorf("reinspect committed DNS engine journal before install retirement: %w", err)
	}
	if !exists || !reflect.DeepEqual(secondJournal, thirdJournal) {
		return false, errors.New("committed DNS engine journal changed before install retirement")
	}
	secondLedger, err := ops.readLedger()
	if err != nil {
		return false, fmt.Errorf("reread service mutation ledger before committed DNS engine finalization: %w", err)
	}
	if err := exactSucceededSignedUpdateDNSEngineLedger(
		secondLedger, thirdJournal, manifest,
	); err != nil {
		return false, err
	}
	if !reflect.DeepEqual(firstLedger, secondLedger) {
		return false, errors.New("service mutation ledger changed during committed DNS engine recovery")
	}
	if err := ops.checkIdle(); err != nil {
		return false, fmt.Errorf("reverify external mutation lock before DNS engine install retirement: %w", err)
	}
	if err := ops.finalizeArtifacts(thirdJournal); err != nil {
		return false, fmt.Errorf(`finalize committed DNS engine switch artifacts: %w`, err)
	}
	artifactJournal, exists, err := ops.readJournal()
	if err != nil {
		return false, fmt.Errorf(`reinspect committed DNS engine journal after artifact finalization: %w`, err)
	}
	if !exists || !reflect.DeepEqual(thirdJournal, artifactJournal) {
		return false, errors.New(`committed DNS engine journal changed during artifact finalization`)
	}
	artifactLedger, err := ops.readLedger()
	if err != nil {
		return false, fmt.Errorf(`reread service mutation ledger after committed DNS artifact finalization: %w`, err)
	}
	if err := exactSucceededSignedUpdateDNSEngineLedger(
		artifactLedger, artifactJournal, manifest,
	); err != nil {
		return false, err
	}
	if !reflect.DeepEqual(firstLedger, artifactLedger) {
		return false, errors.New(`service mutation ledger changed during committed DNS artifact finalization`)
	}
	_, _, ownershipExists, err = exactCommittedDNSEngineSignedUpdateProvenance(
		artifactJournal, manifest, profile, ops,
	)
	if err != nil {
		return false, fmt.Errorf(`verify committed DNS engine provenance after artifact finalization: %w`, err)
	}
	if !ownershipExists {
		return false, errors.New(`committed DNS engine ownership is absent after artifact finalization`)
	}
	if err := ops.checkIdle(); err != nil {
		return false, fmt.Errorf(`reverify external mutation lock after DNS artifact finalization: %w`, err)
	}
	if err := ops.retireInstall(artifactJournal); err != nil {
		return false, fmt.Errorf("retire committed DNS engine install ownership: %w", err)
	}
	_, installExists, ownershipExists, err := exactCommittedDNSEngineSignedUpdateProvenance(
		artifactJournal, manifest, profile, ops,
	)
	if err != nil {
		return false, fmt.Errorf("verify committed DNS engine provenance after install retirement: %w", err)
	}
	if installExists {
		return false, errors.New("committed DNS engine install ownership still exists after retirement")
	}
	if !ownershipExists {
		return false, errors.New("committed DNS engine ownership is absent after install retirement")
	}
	finalJournal, exists, err := ops.readJournal()
	if err != nil {
		return false, fmt.Errorf("reinspect committed DNS engine journal before removal: %w", err)
	}
	if !exists || !reflect.DeepEqual(artifactJournal, finalJournal) {
		return false, errors.New("committed DNS engine journal changed before signed-update removal")
	}
	finalLedger, err := ops.readLedger()
	if err != nil {
		return false, fmt.Errorf("reread service mutation ledger before committed DNS engine removal: %w", err)
	}
	if err := exactSucceededSignedUpdateDNSEngineLedger(
		finalLedger, finalJournal, manifest,
	); err != nil {
		return false, err
	}
	if !reflect.DeepEqual(firstLedger, finalLedger) {
		return false, errors.New("service mutation ledger changed before committed DNS engine removal")
	}
	if err := ops.checkIdle(); err != nil {
		return false, fmt.Errorf("reverify external mutation lock before committed DNS engine removal: %w", err)
	}
	if err := ops.removeJournal(); err != nil {
		return false, fmt.Errorf("remove finalized committed DNS engine journal: %w", err)
	}
	return true, nil
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
	if firstJournal.Phase == dnsSwitchPhaseCommitted {
		return recoverCommittedDNSEngineSwitchJournalForSignedUpdate(
			ctx, firstJournal, ops,
		)
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
		ops.verifyTarget == nil ||
		ops.writeJournal == nil || ops.removeJournal == nil ||
		ops.readInstall == nil ||
		ops.readState == nil || ops.readOwnership == nil ||
		ops.writeOwnership == nil || ops.finalizeArtifacts == nil ||
		ops.retireInstall == nil ||
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
	if _, err := recoverDNSEngineSwitchJournalForSignedUpdate(ctx, ops); err != nil {
		return err
	}
	if profile.PackageManager != hostplatform.PackageManagerAPT {
		return nil
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
