package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
)

const (
	vpnPeerSyncCommitPhasePrefix   = "commit/vpn-peer-sync/v1/"
	vpnPeerSyncReceiptMarkerPrefix = "# CelikPanel VPN peer sync receipt v1: "

	vpnPeerSyncCommitIntent    = "intent"
	vpnPeerSyncCommitPublished = "published"

	vpnPeerSyncRecoveryTimeout = 30 * time.Second
	vpnPeerSyncConfigMaxSize   = 1 << 20
	vpnPeerSyncStageMaxCount   = 32
)

// R-055. A committed VPN peer plan that cannot be completed ends here. The
// phase is deliberately outside the commit-receipt namespace: the ledger
// invariant ties an intent receipt to an active job and a published receipt to
// a succeeded one, and this job is neither - it is finished, failed, and the
// host is free.
// Tamamlanamayan, taahhut edilmis bir VPN peer plani burada biter. Asama
// bilerek taahhut-makbuzu ad alaninin disindadir.
const vpnPeerSyncFailedPhase = "vpn_peer_sync_failed"

const (
	vpnPeerSyncFailedUntouchedCode = "vpn_peer_sync_failed_before_host_change"
	vpnPeerSyncFailedRestoredCode  = "vpn_peer_sync_failed_host_restored"
)

// What a convergence leaves behind on the host is judged by the one rule in
// host_mutation_outcome.go. These names stay because this path's call sites
// read in this path's vocabulary - they are the same type and the same four
// values, not a second set.
// Bir yakinsamanin makinede biraktigi durum, host_mutation_outcome.go'daki tek
// kuralla yargilanir. Bu adlar bu yolun sozlugu icindir; ayni turdur.
type vpnPeerSyncHostOutcome = hostMutationOutcome

const (
	// vpnPeerSyncHostUntouched: the failure happened before any host change.
	vpnPeerSyncHostUntouched = hostMutationUntouched
	// vpnPeerSyncHostRestored: the live interface was re-synchronised from
	// the exact configuration this attempt found on disk, and that durable
	// configuration was read back and still matches it. Both halves are read
	// from the host; neither is assumed.
	vpnPeerSyncHostRestored = hostMutationRestored
	// vpnPeerSyncHostConverged: the committed peer set is published.
	vpnPeerSyncHostConverged = hostMutationConverged
	// vpnPeerSyncHostAmbiguous: the live interface or the durable
	// configuration could not be proved back where this attempt found it.
	// This is the only outcome that may hold the ledger.
	vpnPeerSyncHostAmbiguous = hostMutationAmbiguous
)

// What a clean failure does NOT undo, said out loud. The peer set on this
// server is the one it already had - which is not the same as saying nothing
// is wrong with it, and an operator reading this has to be told which.
// Temiz bir basarisizligin geri ALMADIGI sey, acikca soylenir.
const vpnPeerSyncResidueSentence = "The VPN was not otherwise changed: this " +
	"server keeps the peer list it already had, and the WireGuard service is " +
	"exactly as installed, or not installed, as it was before this request."

// And the one thing a recovery cannot know. Each attempt puts back only what
// it itself changed; an attempt that was killed never got to. So a failure
// reported after a restart may sit on a host an earlier attempt left with a
// live interface that does not match the saved configuration.
// Ve bir kurtarmanin bilemeyecegi tek sey.
const vpnPeerSyncInterruptedAttemptSentence = "An earlier attempt was " +
	"interrupted before it could put back its own work, so the peers on the " +
	"running WireGuard interface may not match the saved VPN configuration on " +
	"this server; applying the peer list again once this server can load " +
	"WireGuard converges it."

// vpnPeerSyncFailureVoice is this path's words for the two failures it may end
// on. The shape and the order of the message, and the rule about which
// outcomes are offered one at all, are shared.
// vpnPeerSyncFailureVoice, bu yolun bitebilecegi iki basarisizlik icin kendi
// sozleridir; bicim, sira ve kural paylasilir.
var vpnPeerSyncFailureVoice = hostMutationFailureVoice{
	untouchedCode: vpnPeerSyncFailedUntouchedCode,
	restoredCode:  vpnPeerSyncFailedRestoredCode,
	untouchedLead: "The committed VPN peer change was abandoned without changing " +
		"anything on this server.",
	restoredLead: "The committed VPN peer change could not be applied. Everything " +
		"this attempt changed was put back and proved: the saved VPN " +
		"configuration on this server was read back exactly as this attempt " +
		"found it, and the running WireGuard interface, where one was running, " +
		"was synchronised back to it.",
	residue:     vpnPeerSyncResidueSentence,
	interrupted: vpnPeerSyncInterruptedAttemptSentence,
}

// vpnPeerSyncCleanFailureText names the terminal failure. afterRestart says
// whether startup recovery is speaking, which is the only case that has to
// warn about an interrupted predecessor it could not undo.
// vpnPeerSyncCleanFailureText, kalici basarisizligi adlandirir.
func vpnPeerSyncCleanFailureText(
	outcome vpnPeerSyncHostOutcome,
	cause error,
	afterRestart bool,
) (code string, message string, clean bool) {
	return vpnPeerSyncFailureVoice.cleanFailureText(outcome, cause, afterRestart)
}

// proveVPNPeerSyncDurableConfig is this path's evidence about the durable half
// of the host: the configuration that is on disk right now is read back and
// compared to the exact bytes this attempt found there. A rollback that only
// wrote is not a rollback that is proved.
// proveVPNPeerSyncDurableConfig, makinenin kalici yarisi hakkindaki kanittir:
// diskteki yapilandirma geri okunur ve bu denemenin buldugu baytlarla
// karsilastirilir.
func proveVPNPeerSyncDurableConfig(found []byte) error {
	actual, err := readSecureVPNConfig()
	if err != nil {
		return fmt.Errorf("read back the VPN configuration after rollback: %w", err)
	}
	if !bytes.Equal(actual, found) {
		return errors.New(
			"the VPN configuration on this server does not match what this attempt found",
		)
	}
	return nil
}

// vpnPeerSyncRollbackOutcome judges what a failed attempt left on the host.
// The live rollback has to have succeeded and the durable configuration has to
// read back as this attempt found it; anything else is ambiguous and still
// holds the ledger. hostContacted separates a plan that never reached the
// interface at all from one that put it back.
//
// vpnPeerSyncRollbackOutcome, basarisiz bir denemenin makinede ne biraktigina
// karar verir. Canli geri alma basarili olmali ve kalici yapilandirma bu
// denemenin buldugu gibi geri okunmalidir; digeri belirsizdir.
func vpnPeerSyncRollbackOutcome(
	found []byte,
	hostContacted bool,
	rollbackErr error,
) (vpnPeerSyncHostOutcome, error) {
	if rollbackErr != nil {
		return vpnPeerSyncHostAmbiguous, rollbackErr
	}
	if err := proveVPNPeerSyncDurableConfig(found); err != nil {
		return vpnPeerSyncHostAmbiguous, err
	}
	if !hostContacted {
		return vpnPeerSyncHostUntouched, nil
	}
	return vpnPeerSyncHostRestored, nil
}

// failStandaloneVPNPeerSync ends a committed plan that cannot succeed, with
// its reason written durably and the ledger released. It is the mirror of the
// terminal success in commitStandaloneVPNPeerSyncStep, and the only door out
// of a failed attempt other than poison.
// failStandaloneVPNPeerSync, basarili olamayacak bir plani nedeni kalici olarak
// yazilmis ve defter serbest birakilmis halde bitirir.
func failStandaloneVPNPeerSync(ctx context.Context, code, message string) error {
	if ctx == nil {
		return errors.New("invalid VPN peer sync failure request")
	}
	tracker, _ := ctx.Value(serviceMutationExecutionTrackerKey{}).(*serviceMutationExecutionTracker)
	if tracker == nil || tracker.manager == nil || tracker.runtime == nil {
		return errors.New("VPN peer sync failure requires a durable execution tracker")
	}
	m, runtime := tracker.manager, tracker.runtime
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.healthErrorLocked(); err != nil {
		return err
	}
	if m.active != runtime || runtime.job == nil || runtime.steps != 1 ||
		runtime.job.WorkerPID != 0 || !activeDirectVPNPeerSyncJob(runtime.job) {
		return errors.New("VPN peer sync failure lost the active mutation step")
	}
	runtime.vpnPeerSyncPublishedPhase = ""
	if err := m.finishRuntimeTerminalLocked(
		runtime, false, vpnPeerSyncFailedPhase, code, message,
	); err != nil {
		if m.active == runtime {
			return m.poisonLocked(fmt.Errorf(
				"persist terminal VPN peer sync failure: %w", err,
			))
		}
		return err
	}
	return nil
}

// endVPNPeerSyncAttempt is the one door a failed live attempt goes through.
// It asks the shared rule whether this host may be handed back, finishes the
// plan cleanly when it may, and poisons when it may not - so a VPN request
// this machine cannot serve cannot hold every other mutation on it, and a host
// that may be half changed still does.
//
// endVPNPeerSyncAttempt, basarisiz bir canli denemenin gectigi tek kapidir.
// Makinenin geri verilip verilemeyecegini paylasilan kurala sorar.
func endVPNPeerSyncAttempt(
	ctx context.Context,
	found []byte,
	hostContacted bool,
	cause error,
	rollbackErr error,
	poisonMessage string,
) string {
	outcome, proofErr := vpnPeerSyncRollbackOutcome(found, hostContacted, rollbackErr)
	reason := cause
	if proofErr != nil {
		reason = errors.Join(cause, proofErr)
	}
	if code, message, clean := vpnPeerSyncCleanFailureText(outcome, reason, false); clean {
		if failErr := failStandaloneVPNPeerSync(ctx, code, message); failErr != nil {
			log.Printf(
				"VPN peer sync could not be failed cleanly: %v; cause: %v",
				failErr, reason,
			)
			return poisonMessage
		}
		log.Printf("VPN peer sync failed cleanly and released the ledger: %v", reason)
		return message
	}
	poisonErr := poisonVPNPeerSyncRollback(ctx, reason)
	log.Printf("VPN peer sync left an ambiguous host: %v; poison: %v", reason, poisonErr)
	return poisonMessage
}

func formatVPNPeerSyncCommitPhase(state, requestID, qualifier string) (string, error) {
	if (state != vpnPeerSyncCommitIntent && state != vpnPeerSyncCommitPublished) ||
		!validMutationIdentity(requestID) ||
		!mutationpayload.ValidVPNPeerSyncQualifier(qualifier) {
		return "", errors.New("invalid VPN peer sync commit phase identity")
	}
	return vpnPeerSyncCommitPhasePrefix + state + "/" + requestID + "/" + qualifier, nil
}

func parseVPNPeerSyncCommitPhase(value string) (
	state, requestID, qualifier string,
	err error,
) {
	if !strings.HasPrefix(value, vpnPeerSyncCommitPhasePrefix) {
		return "", "", "", errors.New("not a VPN peer sync commit phase")
	}
	remainder := strings.TrimPrefix(value, vpnPeerSyncCommitPhasePrefix)
	state, remainder, found := strings.Cut(remainder, "/")
	if !found {
		return "", "", "", errors.New("invalid VPN peer sync commit phase")
	}
	requestID, qualifier, found = strings.Cut(remainder, "/")
	if !found {
		return "", "", "", errors.New("invalid VPN peer sync commit phase")
	}
	canonical, err := formatVPNPeerSyncCommitPhase(state, requestID, qualifier)
	if err != nil || canonical != value {
		return "", "", "", errors.New("invalid VPN peer sync commit phase")
	}
	return state, requestID, qualifier, nil
}

func formatVPNPeerSyncReceiptMarker(requestID, qualifier string) (string, error) {
	if !validMutationIdentity(requestID) ||
		!mutationpayload.ValidVPNPeerSyncQualifier(qualifier) {
		return "", errors.New("invalid VPN peer sync receipt identity")
	}
	return vpnPeerSyncReceiptMarkerPrefix + requestID + " " + qualifier, nil
}

func parseVPNPeerSyncReceiptMarker(config []byte) (
	requestID, qualifier string,
	found bool,
	err error,
) {
	for _, line := range strings.Split(string(config), "\n") {
		if !strings.HasPrefix(line, vpnPeerSyncReceiptMarkerPrefix) {
			continue
		}
		if found {
			return "", "", false, errors.New("VPN config contains multiple peer sync receipts")
		}
		fields := strings.Split(strings.TrimPrefix(line, vpnPeerSyncReceiptMarkerPrefix), " ")
		if len(fields) != 2 {
			return "", "", false, errors.New("VPN config contains an invalid peer sync receipt")
		}
		canonical, markerErr := formatVPNPeerSyncReceiptMarker(fields[0], fields[1])
		if markerErr != nil || canonical != line {
			return "", "", false, errors.New("VPN config contains an invalid peer sync receipt")
		}
		requestID, qualifier, found = fields[0], fields[1], true
	}
	return requestID, qualifier, found, nil
}

func replaceVPNPeerSyncReceiptMarker(
	interfaceConfig, requestID, qualifier string,
) (string, error) {
	marker, err := formatVPNPeerSyncReceiptMarker(requestID, qualifier)
	if err != nil {
		return "", err
	}
	withoutReceipt, err := removeVPNPeerSyncReceiptMarker(interfaceConfig)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(withoutReceipt, "\n") + "\n" + marker + "\n", nil
}

func removeVPNPeerSyncReceiptMarker(interfaceConfig string) (string, error) {
	lines := strings.Split(strings.TrimRight(interfaceConfig, "\n"), "\n")
	filtered := make([]string, 0, len(lines))
	receiptSeen := false
	for _, line := range lines {
		if !strings.HasPrefix(line, vpnPeerSyncReceiptMarkerPrefix) {
			filtered = append(filtered, line)
			continue
		}
		if receiptSeen {
			return "", errors.New("VPN config contains multiple peer sync receipts")
		}
		if _, _, found, parseErr := parseVPNPeerSyncReceiptMarker([]byte(line)); parseErr != nil || !found {
			return "", errors.New("VPN config contains an invalid peer sync receipt")
		}
		receiptSeen = true
	}
	return strings.Join(filtered, "\n") + "\n", nil
}

func vpnPeerSyncCommitIdentity(
	ctx context.Context,
	qualifier string,
) (requestID string, err error) {
	tracker, _ := ctx.Value(serviceMutationExecutionTrackerKey{}).(*serviceMutationExecutionTracker)
	if tracker == nil || tracker.manager == nil || tracker.runtime == nil {
		return "", errors.New("VPN peer sync commit identity requires a durable execution tracker")
	}
	m, runtime := tracker.manager, tracker.runtime
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.healthErrorLocked(); err != nil {
		return "", err
	}
	if m.active != runtime || runtime.job == nil || runtime.steps != 1 ||
		runtime.job.Status != serviceMutationStatusRunning {
		return "", errors.New("VPN peer sync commit identity lost the active mutation step")
	}
	job := runtime.job
	if job.Kind != "vpn_peer_sync" || job.Target != "wireguard" ||
		job.PackageName != qualifier ||
		!mutationpayload.ValidVPNPeerSyncQualifier(job.PackageName) {
		return "", errors.New("VPN peer sync commit identity does not match the active job")
	}
	return job.RequestID, nil
}

// poisonVPNPeerSyncRollback keeps the active host flock when a failed or
// cancelled live mutation cannot prove that both live and durable state were
// restored. The deferred step release then only drops the step counter; a
// panel Finish(false) and every later Begin remain fail-closed.
func poisonVPNPeerSyncRollback(ctx context.Context, cause error) error {
	if ctx == nil || cause == nil {
		return errors.New("invalid VPN peer sync rollback poison request")
	}
	tracker, _ := ctx.Value(serviceMutationExecutionTrackerKey{}).(*serviceMutationExecutionTracker)
	if tracker == nil || tracker.manager == nil || tracker.runtime == nil {
		return errors.New("VPN peer sync rollback poison requires a durable execution tracker")
	}
	m, runtime := tracker.manager, tracker.runtime
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active != runtime || runtime.job == nil || runtime.steps != 1 ||
		(runtime.job.Status != serviceMutationStatusRunning &&
			runtime.job.Status != serviceMutationStatusCancelling) {
		return errors.New("VPN peer sync rollback poison lost the active mutation step")
	}
	return m.poisonLocked(fmt.Errorf("VPN peer sync rollback could not prove the previous host state: %w", cause))
}

func verifyPublishedVPNPeerSyncReceipt(requestID, qualifier string) (bool, error) {
	config, err := readSecureVPNConfig()
	if err != nil {
		return false, err
	}
	actualRequestID, actualQualifier, found, err := parseVPNPeerSyncReceiptMarker(config)
	if err != nil {
		return false, err
	}
	return found && actualRequestID == requestID && actualQualifier == qualifier, nil
}

func activeDirectVPNPeerSyncJob(job *ServiceMutationJob) bool {
	return job != nil && serviceMutationStatusActive(job.Status) &&
		job.Kind == "vpn_peer_sync" && job.Target == "wireguard"
}

func canonicalVPNPeerSyncStageName(name string) bool {
	prefix := "." + filepath.Base(wgConfPath()) + ".tmp-"
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".conf") {
		return false
	}
	random := strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".conf")
	if random == "" || len(random) > 20 {
		return false
	}
	for index := range random {
		if random[index] < '0' || random[index] > '9' {
			return false
		}
	}
	return true
}

func readSecureVPNPeerSyncStage(path string) ([]byte, error) {
	if filepath.Dir(filepath.Clean(path)) != filepath.Clean(wgConfDir) ||
		!canonicalVPNPeerSyncStageName(filepath.Base(path)) {
		return nil, errors.New("invalid VPN peer sync stage path")
	}
	file, info, err := secureOpenRegular(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if err := validateRepoFileMetadata(info, 0o600); err != nil {
		return nil, errors.New("VPN peer sync stage failed security validation")
	}
	data, err := io.ReadAll(io.LimitReader(file, vpnPeerSyncConfigMaxSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > vpnPeerSyncConfigMaxSize {
		return nil, errors.New("VPN peer sync stage exceeds the size limit")
	}
	after, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, after) || after.Mode() != info.Mode() {
		return nil, errors.New("VPN peer sync stage changed while it was read")
	}
	return data, nil
}

func findVPNPeerSyncRecoveryStage(requestID, qualifier string) (string, error) {
	if err := validateVPNDirectory(wgConfDir); err != nil {
		return "", err
	}
	entries, err := os.ReadDir(wgConfDir)
	if err != nil {
		return "", err
	}
	candidateCount := 0
	matching := ""
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "."+filepath.Base(wgConfPath())+".tmp-") ||
			!strings.HasSuffix(name, ".conf") {
			continue
		}
		candidateCount++
		if candidateCount > vpnPeerSyncStageMaxCount || !canonicalVPNPeerSyncStageName(name) {
			return "", errors.New("VPN peer sync recovery stages are ambiguous")
		}
		path := filepath.Join(wgConfDir, name)
		config, err := readSecureVPNPeerSyncStage(path)
		if err != nil {
			return "", err
		}
		stageRequestID, stageQualifier, found, err := parseVPNPeerSyncReceiptMarker(config)
		if err != nil {
			return "", err
		}
		if !found || stageRequestID != requestID || stageQualifier != qualifier {
			continue
		}
		if matching != "" {
			return "", errors.New("multiple VPN peer sync recovery stages match the durable intent")
		}
		matching = path
	}
	return matching, nil
}

func removeVPNPeerSyncRecoveryStage(path string) error {
	if path == "" {
		return nil
	}
	if err := secureRemoveRegular(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return syncAtomicParentDirectory(filepath.Dir(path))
}

func removeLegacyVPNPeerSyncRecoveryStages() error {
	if err := validateVPNDirectory(wgConfDir); err != nil {
		return err
	}
	entries, err := os.ReadDir(wgConfDir)
	if err != nil {
		return err
	}
	candidateCount := 0
	removed := false
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "."+filepath.Base(wgConfPath())+".tmp-") ||
			!strings.HasSuffix(name, ".conf") {
			continue
		}
		candidateCount++
		if candidateCount > vpnPeerSyncStageMaxCount || !canonicalVPNPeerSyncStageName(name) {
			return errors.New("legacy VPN peer sync recovery stages are ambiguous")
		}
		path := filepath.Join(wgConfDir, name)
		config, err := readSecureVPNPeerSyncStage(path)
		if err != nil {
			return err
		}
		if _, _, _, err := parseVPNPeerSyncReceiptMarker(config); err != nil {
			return err
		}
		if err := secureRemoveRegular(path); err != nil {
			return err
		}
		removed = true
	}
	if removed {
		return syncAtomicParentDirectory(filepath.Clean(wgConfDir))
	}
	return nil
}

// reconcilePersistedVPNPeerSyncHost makes the durable wg0.conf authoritative
// again after an agent crash. The atomic target rename is the only commit
// point: an intent never authorizes forward publication of a leftover stage.
// It reports what it left on the host beside whether it succeeded, because
// only the first answer may decide whether a failure is allowed to be
// terminal - see host_mutation_outcome.go.
// Basarili olup olmadiginin yaninda makinede ne biraktigini da bildirir.
func reconcilePersistedVPNPeerSyncHost(
	ctx context.Context,
	requestID, qualifier string,
) (success bool, outcome vpnPeerSyncHostOutcome, err error) {
	diskConfig, err := readSecureVPNConfig()
	if err != nil {
		// The durable configuration cannot even be read, so nothing here can
		// say what is on this host. Fail closed.
		// Kalici yapilandirma okunamiyor; hicbir sey makinenin durumunu
		// soyleyemez. Kapali-basarisiz.
		return false, vpnPeerSyncHostAmbiguous, fmt.Errorf("read VPN recovery target: %w", err)
	}
	targetRequestID, targetQualifier, markerFound, err := parseVPNPeerSyncReceiptMarker(diskConfig)
	if err != nil {
		return false, vpnPeerSyncHostAmbiguous, err
	}
	targetPublished := markerFound && targetRequestID == requestID && targetQualifier == qualifier
	if targetPublished {
		if err := syncAtomicParentDirectory(filepath.Dir(wgConfPath())); err != nil {
			return false, vpnPeerSyncHostAmbiguous, fmt.Errorf(
				"stabilize published VPN recovery target: %w", err,
			)
		}
	} else {
		stage, err := findVPNPeerSyncRecoveryStage(requestID, qualifier)
		if err != nil {
			return false, vpnPeerSyncHostAmbiguous, err
		}
		if stage != "" {
			if removeErr := removeVPNPeerSyncRecoveryStage(stage); removeErr != nil {
				return false, vpnPeerSyncHostAmbiguous, fmt.Errorf(
					"remove uncommitted VPN peer recovery stage: %w", removeErr,
				)
			}
		}
	}

	interfaceUp, err := probeWireGuardInterface(ctx)
	if err != nil {
		// R-055. This is the whole point of classifying a recovery. Asking the
		// host which WireGuard interfaces exist changes nothing: the durable
		// configuration was read and not written, and an uncommitted stage
		// that was removed was never authoritative for anything. A recovery
		// that cannot get an answer here used to poison the ledger and be
		// re-attempted at every agent start, which is how one VPN request took
		// a whole control plane with it. It is now a plan that cannot be
		// completed on a host it provably did not change.
		// R-055. Makineye hangi WireGuard arayuzlerinin var oldugunu sormak
		// hicbir seyi degistirmez. Burada yanit alamayan bir kurtarma eskiden
		// defteri zehirler ve her agent baslangicinda yeniden denenirdi.
		return false, vpnPeerSyncHostUntouched, fmt.Errorf(
			"probe VPN interface during recovery: %w", err,
		)
	}
	if interfaceUp {
		if err := applyWireGuardBytes(ctx, diskConfig); err != nil {
			// The live interface was being synchronised to the durable
			// configuration and could not be. It may be half applied, so it
			// stays ambiguous and still holds the lock.
			// Canli arayuz yarim uygulanmis olabilir; belirsiz kalir.
			return false, vpnPeerSyncHostAmbiguous, fmt.Errorf(
				"reconcile live VPN interface from durable config: %w", err,
			)
		}
	}
	return targetPublished, vpnPeerSyncHostConverged, nil
}

func reconcilePersistedLegacyVPNPeerSyncHost(ctx context.Context) (
	vpnPeerSyncHostOutcome,
	error,
) {
	diskConfig, err := readSecureVPNConfig()
	if err != nil {
		return vpnPeerSyncHostAmbiguous, fmt.Errorf("read legacy VPN recovery target: %w", err)
	}
	// A valid receipt may belong to an earlier completed bound update, but it
	// can never turn an unbound legacy job into success. Malformed receipts are
	// still ambiguous and fail closed.
	if _, _, _, err := parseVPNPeerSyncReceiptMarker(diskConfig); err != nil {
		return vpnPeerSyncHostAmbiguous, err
	}
	if err := removeLegacyVPNPeerSyncRecoveryStages(); err != nil {
		return vpnPeerSyncHostAmbiguous, err
	}
	interfaceUp, err := probeWireGuardInterface(ctx)
	if err != nil {
		return vpnPeerSyncHostUntouched, fmt.Errorf(
			"probe VPN interface during legacy recovery: %w", err,
		)
	}
	if interfaceUp {
		if err := applyWireGuardBytes(ctx, diskConfig); err != nil {
			return vpnPeerSyncHostAmbiguous, fmt.Errorf(
				"reconcile legacy live VPN interface from durable config: %w", err,
			)
		}
	}
	return vpnPeerSyncHostConverged, nil
}

// recoverPersistedVPNPeerSyncLocked handles every active direct peer-sync job
// with no live recorded worker. It temporarily installs a tracked runtime so
// recovery probes and syncconf commands retain the same crash-safe PID ledger.
// The caller holds m.mu and the host flock. The helper always returns with
// m.mu held; on ambiguity it deliberately retains the flock through m.active.
func (m *serviceMutationManager) recoverPersistedVPNPeerSyncLocked(
	job *ServiceMutationJob,
	lock *serviceMutationFileLock,
) (handled bool, err error) {
	if !activeDirectVPNPeerSyncJob(job) ||
		serviceMutationWorkerMatches(job.WorkerPID, job.WorkerStarted) {
		return false, nil
	}
	bound := mutationpayload.ValidVPNPeerSyncQualifier(job.PackageName)
	legacy := job.PackageName == ""
	if !bound && !legacy {
		m.poisonLock = lock
		return true, m.poisonLocked(errors.New("active VPN peer sync has an invalid payload qualifier"))
	}
	intent := false
	if bound {
		state, _, _, phaseErr := parseVPNPeerSyncCommitPhase(job.Phase)
		intent = phaseErr == nil && state == vpnPeerSyncCommitIntent
	}

	recoveryBase, cancel := context.WithTimeout(context.Background(), vpnPeerSyncRecoveryTimeout)
	runtime := &serviceMutationRuntime{job: job, lock: lock, ctx: recoveryBase, cancel: cancel}
	// Preserve the global lock order used by normal steps: stepMu, then m.mu.
	m.mu.Unlock()
	runtime.stepMu.Lock()
	m.mu.Lock()
	if m.active != nil || m.ledger.ActiveRequestID != job.RequestID {
		cancel()
		m.poisonLock = lock
		identityErr := m.poisonLocked(errors.New("VPN peer sync recovery identity changed"))
		m.mu.Unlock()
		runtime.stepMu.Unlock()
		m.mu.Lock()
		return true, identityErr
	}
	m.active = runtime
	runtime.steps = 1
	before := cloneServiceMutationLedger(m.ledger)
	runtime.job.Status = serviceMutationStatusCancelling
	if !intent {
		runtime.job.Phase = "recovering_vpn_peer_sync"
	}
	runtime.job.ErrorCode = "agent_restart_during_vpn_peer_sync"
	runtime.job.ErrorMessage = "The agent is reconciling durable and live VPN state after a restart."
	runtime.job.WorkerPID = 0
	runtime.job.WorkerStarted = ""
	runtime.job.WorkerCommand = ""
	runtime.job.UpdatedAt = m.now()
	if persistErr := m.persistLedgerMutationLocked(before); persistErr != nil {
		poisonErr := m.poisonLocked(fmt.Errorf("persist VPN peer sync recovery intent: %w", persistErr))
		runtime.steps = 0
		m.mu.Unlock()
		runtime.stepMu.Unlock()
		m.mu.Lock()
		return true, poisonErr
	}
	tracker := &serviceMutationExecutionTracker{
		manager:                 m,
		runtime:                 runtime,
		allowCancellingRecovery: true,
	}
	recoveryCtx := context.WithValue(
		recoveryBase,
		serviceMutationExecutionTrackerKey{},
		tracker,
	)
	m.mu.Unlock()
	success := false
	recoveryOutcome := vpnPeerSyncHostConverged
	var recoveryErr error
	if bound {
		success, recoveryOutcome, recoveryErr = reconcilePersistedVPNPeerSyncHost(
			recoveryCtx,
			runtime.job.RequestID,
			runtime.job.PackageName,
		)
	} else {
		recoveryOutcome, recoveryErr = reconcilePersistedLegacyVPNPeerSyncHost(recoveryCtx)
	}
	m.mu.Lock()
	runtime.steps = 0
	m.mu.Unlock()
	runtime.stepMu.Unlock()
	m.mu.Lock()
	if recoveryErr != nil {
		// R-055. Re-attempting a plan this host cannot serve is how one
		// unfinishable step took a whole control plane with it (R-046, then
		// R-054, then here): the same call failed at every start and the
		// ledger stayed held. A plan the recovery cannot complete, on a host
		// the recovery proved it left where it found it, is finished as failed
		// with its reason written durably, and the host becomes mutable again.
		// Everything else - a durable configuration that cannot be read, a
		// live interface that may be half synchronised - still poisons and
		// still keeps the lock.
		// R-055. Bu makinenin karsilayamayacagi bir plani tekrar denemek,
		// bitirilemeyen tek bir adimin butun kontrol duzlemini goturme
		// bicimiydi.
		code, message, clean := vpnPeerSyncCleanFailureText(
			recoveryOutcome, recoveryErr, true,
		)
		if clean {
			log.Printf(
				"Committed VPN peer plan failed cleanly during startup recovery: %v",
				recoveryErr,
			)
			runtime.vpnPeerSyncPublishedPhase = ""
			if finishErr := m.finishRuntimeTerminalLocked(
				runtime, false, vpnPeerSyncFailedPhase, code, message,
			); finishErr != nil {
				return true, m.poisonLocked(fmt.Errorf(
					"persist recovered VPN peer sync failure: %w", finishErr,
				))
			}
			return true, nil
		}
		return true, m.poisonLocked(recoveryErr)
	}
	if success {
		publishedPhase, phaseErr := formatVPNPeerSyncCommitPhase(
			vpnPeerSyncCommitPublished,
			runtime.job.RequestID,
			runtime.job.PackageName,
		)
		if phaseErr != nil {
			return true, m.poisonLocked(phaseErr)
		}
		if finishErr := m.finishRuntimeTerminalLocked(runtime, true, publishedPhase, "", ""); finishErr != nil {
			return true, m.poisonLocked(fmt.Errorf("persist recovered VPN peer sync success: %w", finishErr))
		}
		return true, nil
	}
	if finishErr := m.finishRuntimeTerminalLocked(
		runtime,
		false,
		"interrupted",
		"agent_restarted_before_vpn_peer_commit",
		"The agent restored the durable VPN configuration after a restart before the peer update commit point.",
	); finishErr != nil {
		return true, m.poisonLocked(fmt.Errorf("persist recovered VPN peer sync failure: %w", finishErr))
	}
	return true, nil
}

// commitStandaloneVPNPeerSyncStep is the direct V2 mutation's linearization
// gate. The caller already holds runtime.stepMu through acquireStep; m.mu gives
// cancel, expiry, heartbeat, and finish a strict before/after order around the
// durable intent, host rename+fsync, and terminal receipt writes.
func commitStandaloneVPNPeerSyncStep(
	ctx context.Context,
	commit func() error,
) (hostPublished bool, err error) {
	if ctx == nil || commit == nil {
		return false, errors.New("invalid VPN peer sync commit gate")
	}
	tracker, _ := ctx.Value(serviceMutationExecutionTrackerKey{}).(*serviceMutationExecutionTracker)
	if tracker == nil || tracker.manager == nil || tracker.runtime == nil {
		return false, errors.New("VPN peer sync commit gate requires a durable execution tracker")
	}
	m, runtime := tracker.manager, tracker.runtime
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.healthErrorLocked(); err != nil {
		return false, err
	}
	if m.active != runtime || runtime.job == nil || runtime.steps != 1 ||
		runtime.job.WorkerPID != 0 || runtime.job.Status != serviceMutationStatusRunning {
		return false, errors.New("VPN peer sync commit gate lost the active mutation step")
	}
	job := runtime.job
	if job.Kind != "vpn_peer_sync" || job.Target != "wireguard" ||
		!mutationpayload.ValidVPNPeerSyncQualifier(job.PackageName) {
		return false, errors.New("VPN peer sync commit gate rejected the mutation identity")
	}
	now := m.now()
	if ctx.Err() != nil || !now.Before(job.LeaseExpiresAt) || !now.Before(job.DeadlineAt) {
		return false, errors.New("service mutation lease ended before the VPN peer commit point")
	}
	intentPhase, err := formatVPNPeerSyncCommitPhase(
		vpnPeerSyncCommitIntent,
		job.RequestID,
		job.PackageName,
	)
	if err != nil {
		return false, err
	}
	publishedPhase, err := formatVPNPeerSyncCommitPhase(
		vpnPeerSyncCommitPublished,
		job.RequestID,
		job.PackageName,
	)
	if err != nil {
		return false, err
	}
	before := cloneServiceMutationLedger(m.ledger)
	job.Phase = intentPhase
	job.UpdatedAt = now
	if err := m.persistLedgerMutationLocked(before); err != nil {
		return false, err
	}
	commitErr := commit()
	if commitErr != nil {
		published, verifyErr := verifyPublishedVPNPeerSyncReceipt(job.RequestID, job.PackageName)
		if verifyErr != nil {
			return false, m.poisonLocked(fmt.Errorf(
				"verify VPN peer sync publication after commit error: %w",
				verifyErr,
			))
		}
		if !published {
			return false, commitErr
		}
		if syncErr := syncAtomicParentDirectory(filepath.Dir(wgConfPath())); syncErr != nil {
			runtime.vpnPeerSyncPublishedPhase = publishedPhase
			return true, m.poisonLocked(fmt.Errorf(
				"stabilize verified VPN peer sync publication: %w",
				syncErr,
			))
		}
	}
	runtime.vpnPeerSyncPublishedPhase = publishedPhase
	if err := m.finishRuntimeTerminalLocked(runtime, true, publishedPhase, "", ""); err != nil {
		if m.active == runtime {
			return true, m.poisonLocked(fmt.Errorf(
				"persist terminal VPN peer sync receipt after host publication: %w",
				err,
			))
		}
		return true, err
	}
	return true, nil
}
