//go:build linux

package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
	"github.com/alicelik/celikpanel/internal/hostplatform"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/pdnsnative"
	"github.com/alicelik/celikpanel/internal/transport"
	"golang.org/x/sys/unix"
)

// The journal can retain a package guard's persistent mask only when this
// request observed an unmasked target before package installation. A preexisting
// mask belongs to the owner and cannot be adopted as this transaction's mask.
func validateFreshPDNSTargetBeforePackagesV3(before dnsUnitSnapshot) error {
	if before.Name != "pdns.service" || before.ActiveState != "inactive" ||
		!((before.LoadState == "not-found" && before.UnitFileState == "") ||
			(before.LoadState == "loaded" && before.UnitFileState == "disabled")) {
		return errors.New("fresh PowerDNS target has an unowned or unsupported pre-package unit state")
	}
	return nil
}

// validateFreshPDNSTargetAfterOwnGuardMaskV3 is the after-package check for a
// target admitted by freshPDNSTargetOwnGuardMaskV3: nothing was installed now
// and the sealed unit is unchanged.
func validateFreshPDNSTargetAfterOwnGuardMaskV3(before, after dnsUnitSnapshot, installedNow bool) error {
	if !installedNow && before == freshPDNSGuardMaskV3 && after == before {
		return nil
	}
	return errors.New("fresh PowerDNS target changed after its guarded package seal")
}

func validateFreshPDNSTargetAfterPackagesV3(before, after dnsUnitSnapshot, installedNow bool) error {
	if err := validateFreshPDNSTargetBeforePackagesV3(before); err != nil {
		return err
	}
	if installedNow {
		if after == (dnsUnitSnapshot{Name: "pdns.service", LoadState: "masked", ActiveState: "inactive", UnitFileState: "masked"}) {
			return nil
		}
	} else if before == after && after == (dnsUnitSnapshot{Name: "pdns.service", LoadState: "loaded", ActiveState: "inactive", UnitFileState: "disabled"}) {
		return nil
	}
	return errors.New("fresh PowerDNS target changed outside the guarded package install")
}

// freshPDNSGuardMaskV3 is the package guard's persistent seal of a stopped,
// never-enabled PowerDNS unit.
var freshPDNSGuardMaskV3 = dnsUnitSnapshot{Name: "pdns.service", LoadState: "masked", ActiveState: "inactive", UnitFileState: "masked"}

// freshPDNSTargetOwnGuardMaskV3 reports whether a masked pre-package target
// is this product's own rollback standby: the state a rolled-back first
// install leaves (its guarded package install sealed the unit, the inverse
// restored that seal and kept the packages). It is admitted only when every
// PowerDNS package is already installed, so nothing is installed now, and the
// PowerDNS install-ownership receipt records that CelikPanel itself installed
// exactly these packages (a non-empty missing-before set, not an adoption of
// packages that were already present). Without that receipt the mask stays
// the owner's and the target is refused, as before. The frozen journal keeps
// the masked preimage, so an inverse returns the unit to the same seal.
//
// Maskelenmiş bir hedef yalnız, PowerDNS paketlerini CelikPanel'in kendisinin
// kurduğunu kanıtlayan kurulum makbuzu varken ve şimdi hiçbir paket
// kurulmayacakken, geri alınmış ilk kurulumun bekleme durumu olarak kabul edilir.
func freshPDNSTargetOwnGuardMaskV3(
	before dnsUnitSnapshot,
	allPackagesPresent bool,
	packages []string,
	profile hostplatform.Profile,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) (bool, error) {
	return freshPDNSTargetOwnGuardMaskWithReceiptV3(
		before, allPackagesPresent, packages, profile, manifest, readDNSEngineInstallOwnership,
	)
}

func freshPDNSTargetOwnGuardMaskWithReceiptV3(
	before dnsUnitSnapshot,
	allPackagesPresent bool,
	packages []string,
	profile hostplatform.Profile,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	read func(transport.DNSEngine) (dnsEngineInstallOwnershipReceipt, bool, error),
) (bool, error) {
	if before != freshPDNSGuardMaskV3 || !allPackagesPresent ||
		manifest.TargetEngine != transport.DNSEnginePowerDNS || read == nil {
		return false, nil
	}
	receipt, exists, err := read(transport.DNSEnginePowerDNS)
	if err != nil {
		return false, err
	}
	if !exactDNSEngineInstallOwnership(receipt, exists, transport.DNSEnginePowerDNS, profile.PackageManager, packages) ||
		receipt.AdoptedPresent || len(receipt.MissingBefore) == 0 {
		return false, nil
	}
	return true, nil
}

// validateFreshPDNSPrimaryHostProfileV3 is the measured host class of the V3
// native catalog transform. It runs before any package, unit or file effect so
// an unmeasured host is refused while nothing has changed.
func validateFreshPDNSPrimaryHostProfileV3(profile hostplatform.Profile) error {
	if profile.ID != "debian" || profile.Version != "13" || profile.Arch != "amd64" ||
		profile.DistroFamily != hostplatform.DistroFamilyDebian ||
		profile.PackageManager != hostplatform.PackageManagerAPT ||
		profile.ServiceManager != hostplatform.ServiceManagerSystemd {
		return errors.New("PowerDNS as the paired primary is supported only on Debian 13 amd64 in this release; nothing was changed; install BIND as the paired primary on this host instead")
	}
	return nil
}

// prepareFreshPDNSPrimaryIntentV3 binds the prepared config and absent target
// before any candidate, config, unit or database effect. The caller still has
// to publish and read back this intent under the DNS/host mutation locks.
func prepareFreshPDNSPrimaryIntentV3(
	profile hostplatform.Profile,
	base dnsEngineSwitchJournal,
	configs pdnsConfigMutation,
) (dnsEngineSwitchJournal, error) {
	if profile.ID != "debian" || profile.Version != "13" || profile.Arch != "amd64" ||
		profile.DistroFamily != hostplatform.DistroFamilyDebian ||
		profile.PackageManager != hostplatform.PackageManagerAPT ||
		profile.ServiceManager != hostplatform.ServiceManagerSystemd {
		return dnsEngineSwitchJournal{}, errors.New("v3 fresh PowerDNS primary requires Debian APT and systemd")
	}
	if base.Schema != dnsengineartifact.SwitchJournalSchemaV1 ||
		base.Phase != dnsSwitchPhaseIntent ||
		base.Mode != transport.DNSEngineSwitchModeSwitch ||
		base.SourceEngine != "" || base.SourceEpoch != 0 || base.StateBefore.Exists ||
		base.TargetEngine != transport.DNSEnginePowerDNS ||
		base.Topology != transport.DNSTopologyPaired ||
		base.PairRole != transport.DNSPairRolePrimary ||
		base.PrimaryCatalogSerial != 1 ||
		base.PDNSBackupSHA256 != "" || base.PDNSBackupSize != 0 {
		return dnsEngineSwitchJournal{}, errors.New("v3 intent requires an uninitialized paired PowerDNS primary")
	}
	if err := configs.validateOwnerAware(); err != nil {
		return dnsEngineSwitchJournal{}, err
	}
	if !reflect.DeepEqual(configs.originalSnapshots(), base.ConfigBefore) {
		return dnsEngineSwitchJournal{}, errors.New("v3 PowerDNS config preimage differs from the journal")
	}
	if err := dnsJournalPolicy().ValidateSwitchJournal(base); err != nil {
		return dnsEngineSwitchJournal{}, err
	}
	path := pdnsSwitchCandidatePathV4(base.MutationRequestID)
	if err := verifyPDNSTargetStageFilesystemV4(path); err != nil {
		return dnsEngineSwitchJournal{}, err
	}
	build, buildSidecars, err := dnsengineartifact.PDNSFreshCandidateBuildPathsV3(path)
	if err != nil {
		return dnsEngineSwitchJournal{}, err
	}
	absentBefore := []string{pdnsDBPath(), pdnsDBPath() + "-wal", pdnsDBPath() + "-shm", pdnsDBPath() + "-journal", path, path + "-wal", path + "-shm", path + "-journal", build}
	for _, name := range append(absentBefore, buildSidecars...) {
		if _, err := os.Lstat(name); !errors.Is(err, os.ErrNotExist) {
			if err == nil {
				err = fmt.Errorf("PowerDNS fresh target path already exists: %s", name)
			}
			return dnsEngineSwitchJournal{}, err
		}
	}
	return dnsJournalPolicy().BuildPDNSFreshPrimaryJournalV3(base, configs.desiredSnapshots())
}

// stageFreshPDNSPrimaryCandidateV3 runs only after the empty intent is durable.
// It leaves a candidate as evidence if any proof fails; independent recovery
// decides whether that exact file can be removed.
func stageFreshPDNSPrimaryCandidateV3(
	ctx context.Context,
	intent dnsEngineSwitchJournal,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	binding transport.ServiceMutationBinding,
) (dnsEngineSwitchJournal, error) {
	if intent.Schema != dnsengineartifact.SwitchJournalSchemaV3 ||
		intent.Phase != dnsSwitchPhaseIntent || intent.PDNSFreshPlan == nil ||
		intent.PDNSFreshPlan.Candidate != nil {
		return dnsEngineSwitchJournal{}, errors.New("v3 candidate requires a durable empty intent")
	}
	if err := dnsJournalPolicy().ValidateSwitchJournal(intent); err != nil {
		return dnsEngineSwitchJournal{}, err
	}
	if err := verifyPDNSTargetStageFilesystemV4(intent.PDNSCandidatePath); err != nil {
		return dnsEngineSwitchJournal{}, err
	}
	// Build, verify, own and read the database under this operation's
	// temporary name; only a complete, verified file is renamed to the
	// candidate name. A crash in between leaves the temporary file, which the
	// pre-start inverse removes (item 4b).
	build, _, err := dnsengineartifact.PDNSFreshCandidateBuildPathsV3(intent.PDNSCandidatePath)
	if err != nil {
		return dnsEngineSwitchJournal{}, err
	}
	if err := buildPDNSSwitchCandidateWithPrimaryCatalogSerial(
		ctx, build, manifest, binding, intent.PrimaryCatalogSerial,
	); err != nil {
		return dnsEngineSwitchJournal{}, err
	}
	if err := verifyPDNSSwitchDatabaseWithPrimaryCatalogSerial(
		ctx, build, manifest, binding, intent.PrimaryCatalogSerial,
	); err != nil {
		return dnsEngineSwitchJournal{}, err
	}
	if err := setPDNSDatabaseOwnership(build); err != nil {
		return dnsEngineSwitchJournal{}, err
	}
	staged, err := captureFreshPDNSSQLV3(ctx, build)
	if err != nil {
		return dnsEngineSwitchJournal{}, err
	}
	if err := publishFreshPDNSCandidateBuildV3(build, intent.PDNSCandidatePath); err != nil {
		return dnsEngineSwitchJournal{}, err
	}
	proof, err := dnsenginerecovery.CapturePDNSTargetCandidateV4(intent.PDNSCandidatePath)
	if err != nil {
		return dnsEngineSwitchJournal{}, err
	}
	return dnsJournalPolicy().StagePDNSFreshPrimaryCandidateV3(intent, proof, staged)
}

// publishFreshPDNSCandidateBuildV3 makes the complete build durable under the
// candidate name: fsync the file, rename it without replacing anything in the
// same root-private directory, fsync the directory. The inode, owner, mode and
// bytes the journal will seal are exactly the build's.
func publishFreshPDNSCandidateBuildV3(build, candidate string) error {
	if filepath.Dir(build) != filepath.Dir(candidate) {
		return errors.New("v3 candidate build is not beside its candidate name")
	}
	file, err := os.OpenFile(build, os.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(syncErr, closeErr); err != nil {
		return fmt.Errorf("sync v3 candidate build: %w", err)
	}
	dirFD, err := unix.Open(filepath.Dir(candidate), unix.O_DIRECTORY|unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer unix.Close(dirFD)
	if err := unix.Renameat2(dirFD, filepath.Base(build), dirFD, filepath.Base(candidate), unix.RENAME_NOREPLACE); err != nil {
		return fmt.Errorf("publish v3 candidate build: %w", err)
	}
	if err := unix.Fsync(dirFD); err != nil {
		return fmt.Errorf("sync v3 candidate directory: %w", err)
	}
	return nil
}

func captureFreshPDNSSQLV3(ctx context.Context, path string) (pdnsnative.Snapshot, error) {
	db, err := openPDNSEngineDB(path, true)
	if err != nil {
		return pdnsnative.Snapshot{}, err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return pdnsnative.Snapshot{}, err
	}
	defer tx.Rollback()
	return pdnsnative.CaptureSQLiteSnapshot(ctx, tx)
}

// activateFreshPDNSPrimaryCandidateV3 refuses replacement and verifies the
// same sealed inode at the live name before the daemon can touch SQLite.
func activateFreshPDNSPrimaryCandidateV3(journal dnsEngineSwitchJournal) error {
	if journal.Schema != dnsengineartifact.SwitchJournalSchemaV3 ||
		journal.Phase != dnsengineartifact.SwitchPhaseTargetEnableIntent ||
		journal.PDNSFreshPlan == nil || journal.PDNSFreshPlan.Candidate == nil {
		return errors.New("v3 PowerDNS target is not durably staged for activation")
	}
	if err := dnsJournalPolicy().ValidateSwitchJournal(journal); err != nil {
		return err
	}
	proof := *journal.PDNSFreshPlan.Candidate
	actual, err := dnsenginerecovery.CapturePDNSTargetCandidateV4(proof.Path)
	if err != nil || actual != proof {
		return errors.Join(errors.New("v3 PowerDNS candidate differs before activation"), err)
	}
	if err := verifyPDNSTargetStageFilesystemV4(proof.Path); err != nil {
		return err
	}
	privateFD, err := unix.Open(filepath.Dir(proof.Path), unix.O_DIRECTORY|unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer unix.Close(privateFD)
	liveFD, err := unix.Open(filepath.Dir(pdnsDBPath()), unix.O_DIRECTORY|unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer unix.Close(liveFD)
	if err := unix.Renameat2(privateFD, filepath.Base(proof.Path), liveFD, filepath.Base(pdnsDBPath()), unix.RENAME_NOREPLACE); err != nil {
		return err
	}
	if err := dnsenginerecovery.VerifyPDNSTargetLiveV4(proof, pdnsDBPath()); err != nil {
		return err
	}
	return syncAtomicParentDirectory(filepath.Dir(pdnsDBPath()))
}

// continueFreshPDNSPrimaryV3 never invokes the legacy rollback path. Before
// start, the independent inverse may remove only the sealed candidate; once
// start is attempted, an ambiguous result preserves the target for forward
// reconciliation instead of deleting daemon-written SQLite state.
func continueFreshPDNSPrimaryV3(
	ctx context.Context,
	profile hostplatform.Profile,
	systemctl string,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	binding transport.ServiceMutationBinding,
	configs pdnsConfigMutation,
	intent dnsEngineSwitchJournal,
	writeJournal func(dnsEngineSwitchJournal) error,
) (transport.SwitchDNSEngineV1Response, error) {
	fail := func(cause error) (transport.SwitchDNSEngineV1Response, error) {
		return transport.SwitchDNSEngineV1Response{}, fmt.Errorf(
			"fresh PowerDNS primary remains pending in its durable V3 journal; reconcile the exact operation: %w", cause,
		)
	}
	if err := verifyFreshPDNSNativeVersionV3(ctx, profile); err != nil {
		return fail(err)
	}
	staged, err := stageFreshPDNSPrimaryCandidateV3(ctx, intent, manifest, binding)
	if err != nil {
		return fail(err)
	}
	if err := writeJournal(staged); err != nil {
		return fail(err)
	}
	if err := configs.verifyOwnerAwarePreimage(ctx); err != nil {
		return fail(err)
	}
	if err := runDNSMutationWithSystemdParentProof(
		verifyBINDMaskParentMetadata,
		func() error { return configs.applyOwnerAware(ctx) },
	); err != nil {
		return fail(err)
	}
	effective, detail, err := effectiveManagedPowerDNSConfig()
	if err != nil {
		return fail(err)
	}
	if !effective {
		return fail(fmt.Errorf("managed PowerDNS config is not effective: %s", detail))
	}
	staged.Phase = dnsengineartifact.SwitchPhaseTargetEnableIntent
	if err := writeJournal(staged); err != nil {
		return fail(err)
	}
	if err := runDNSMutationWithSystemdParentProof(
		verifyBINDMaskParentMetadata,
		func() error { return activateFreshPDNSPrimaryCandidateV3(staged) },
	); err != nil {
		return fail(err)
	}
	// The exact inode is still unmodified here. A failed start or checkpoint
	// can have started the daemon, so neither failure authorizes deletion.
	if err := dnsenginerecovery.VerifyPDNSTargetLiveV4(*staged.PDNSFreshPlan.Candidate, pdnsDBPath()); err != nil {
		return fail(err)
	}
	if err := startPDNSTarget(ctx, profile, systemctl); err != nil {
		return fail(err)
	}
	staged.Phase = dnsSwitchPhaseTargetStarted
	if err := writeJournal(staged); err != nil {
		return fail(err)
	}
	if err := verifyOnlyPDNSActive(ctx, systemctl); err != nil {
		return fail(err)
	}
	live, err := dnsenginerecovery.CaptureFreshPrimaryNativeV3(ctx, dnsJournalPolicy(), staged)
	if err != nil {
		return fail(err)
	}
	catalog, err := binddns.CatalogDomain(manifest.LocalIP)
	if err != nil {
		return fail(err)
	}
	observed, err := dnsJournalPolicy().AttachPDNSFreshNativeObservationV3(staged, catalog, live)
	if err != nil {
		return fail(err)
	}
	if err := writeJournal(observed); err != nil {
		return fail(err)
	}
	nativeSerial := observed.PDNSFreshPlan.Native.Observed.NativeSerial
	if err := verifyDNSZoneManifestAuthority(ctx, manifest.Zones); err != nil {
		return fail(err)
	}
	if err := verifyPDNSPairingAuthority(ctx, manifest); err != nil {
		return fail(err)
	}
	nextState := dnsEngineStateReceipt{
		Schema: dnsEngineStateSchema, Mode: manifest.Mode,
		Engine: transport.DNSEnginePowerDNS, EngineEpoch: manifest.TargetEpoch,
		PairRole: transport.DNSPairRolePrimary, PairLocalIP: manifest.LocalIP, PairPeerIP: manifest.PeerIP,
		PrimaryCatalogSerial: nativeSerial, SourceRevision: manifest.SourceRevision,
		NativeCatalogV3:   dnsengineartifact.NativeCatalogDebian49V3,
		ManifestQualifier: manifest.Qualifier, MutationRequestID: binding.MutationRequestID,
		MutationOwnerID: binding.MutationOwnerID,
	}
	if err := verifyCompletedPrimaryCatalogTarget(ctx, profile, manifest, nextState); err != nil {
		return fail(err)
	}
	if err := publishExactFreshPrimaryStateV3(observed, nextState); err != nil {
		return fail(err)
	}
	observed.Phase = dnsSwitchPhaseTargetVerified
	if err := writeJournal(observed); err != nil {
		return fail(err)
	}
	observed.Phase = dnsSwitchPhaseCommitted
	if err := writeJournal(observed); err != nil {
		return fail(err)
	}
	return transport.SwitchDNSEngineV1Response{
		Applied: true, ActiveEngine: transport.DNSEnginePowerDNS,
		ActiveEpoch: manifest.TargetEpoch, AppliedZones: len(manifest.Zones),
		Detail: "PowerDNS is the verified paired primary DNS engine",
	}, nil
}

const freshPDNSDebian13PackageVersionV3 = "4.9.17-0+deb13u1"
const freshPDNSDebian13BinarySHA256V3 = "c11660ad7647fa3b42f0b0f1ace73cfde3765add380a4351889b6c6d86b46d84"

func verifyFreshPDNSNativeVersionV3(ctx context.Context, profile hostplatform.Profile) error {
	if profile.ID != "debian" || profile.Version != "13" || profile.Arch != "amd64" ||
		profile.PackageManager != hostplatform.PackageManagerAPT ||
		profile.ServiceManager != hostplatform.ServiceManagerSystemd {
		return errors.New("v3 native catalog transform is measured only on Debian 13 amd64")
	}
	dpkg, err := executableForProfile(profile, string(profile.PackageManager), "dpkg-query")
	if err != nil {
		return err
	}
	for _, name := range []string{"pdns-server", "pdns-backend-sqlite3"} {
		command := serviceMutationCommand(ctx, dpkg, "-W", "-f", "${Status}\t${Version}", "--", name)
		command.Env = bindSafeAPTCommandEnvironment()
		output, err := command.CombinedOutputLimited(64 << 10)
		if err != nil || string(output) != "install ok installed\t"+freshPDNSDebian13PackageVersionV3 {
			return fmt.Errorf("v3 native PowerDNS package %s is not the measured version: %w", name, err)
		}
	}
	const binary = "/usr/sbin/pdns_server"
	info, err := os.Lstat(binary)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o755 {
		return errors.New("v3 PowerDNS binary type or mode differs from the measured package")
	}
	f, err := os.Open(binary)
	if err != nil {
		return err
	}
	defer f.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return err
	}
	if hex.EncodeToString(hash.Sum(nil)) != freshPDNSDebian13BinarySHA256V3 {
		return errors.New("v3 PowerDNS binary differs from the measured native implementation")
	}
	return nil
}
