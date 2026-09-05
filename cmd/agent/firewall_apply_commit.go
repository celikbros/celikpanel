package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	firewallApplyCommitPhasePrefix = "commit/firewall-apply/v1/"
	firewallApplyJournalFileName   = "firewall-apply-journal.json"
	firewallApplyJournalVersion    = 1
	firewallApplyJournalMaxSize    = 128 << 10
	firewallApplyJournalStageLimit = 8

	firewallApplyCommitIntent    = "intent"
	firewallApplyCommitPublished = "published"

	firewallApplyConvergenceTimeout = 45 * time.Second

	firewallApplyJournalFaultBeforeRename = "before_rename"
	firewallApplyJournalFaultAfterRename  = "after_rename_before_directory_sync"
	firewallApplyJournalFaultAfterSync    = "after_directory_sync"
)

// A committed firewall plan that cannot be completed ends here. The phase is
// deliberately outside the commit-receipt namespace: the ledger invariant ties
// an intent receipt to an active job and a published receipt to a succeeded
// one, and this job is neither - it is finished, failed, and the host is free.
// Tamamlanamayan, taahhut edilmis bir guvenlik duvari plani burada biter. Asama
// bilerek taahhut-makbuzu ad alaninin disindadir.
const firewallApplyFailedPhase = "firewall_apply_failed"

const (
	firewallApplyFailedUntouchedCode = "firewall_apply_failed_before_host_change"
	firewallApplyFailedRestoredCode  = "firewall_apply_failed_host_restored"
)

const firewallApplyFailureReasonLimit = 400

// The two systemd unit-file states this plan is allowed to have found the boot
// restore unit in, and the only two it knows how to put back.
// Bu planin acilis geri yukleme unitini bulabilecegi ve geri koyabilecegi iki
// systemd unit-dosya durumu.
const (
	firewallRestoreUnitEnabled  = "enabled"
	firewallRestoreUnitDisabled = "disabled"
)

// firewallHostOutcome is what a convergence leaves behind on the host. It is
// the fact a committed plan's failure has to be judged on: a plan that cannot
// be completed may only be failed cleanly - and the host's mutation ledger
// released - when the host is provably where the plan found it. R-046 drew
// this line for the mail path; R-054 needed it here.
// firewallHostOutcome, bir yakinsamanin makinede biraktigi durumdur.
type firewallHostOutcome int

const (
	// firewallHostUntouched: the failure happened before any host change.
	firewallHostUntouched firewallHostOutcome = iota
	// firewallHostRestored: the durable half of the plan was written, the live
	// ruleset provably was not, and everything written was put back and read
	// back. This outcome may only be reached through a fault that proves the
	// kernel accepted nothing - see convergeFirewallApplyPlan.
	firewallHostRestored
	// firewallHostConverged: the committed plan is applied and verified.
	firewallHostConverged
	// firewallHostAmbiguous: the host was changed and could not be proved put
	// back, or a ruleset may be half applied. This is the only outcome that
	// may hold the ledger.
	firewallHostAmbiguous
)

// What a clean failure does NOT undo, said out loud. Nothing this plan did
// survives it, but the firewall is not on either, and an operator reading this
// has to be told that rather than left to guess.
// Temiz bir basarisizligin geri ALMADIGI sey, acikca soylenir.
const firewallApplyResidueSentence = "The firewall was not changed: this server " +
	"is exactly as protected, or unprotected, as it was before this request."

// And the one thing a recovery cannot know. Each attempt puts back only what
// it itself wrote; an attempt that was killed never got to. So a failure
// reported after a restart may sit on a host an earlier attempt left half
// configured, and it says so instead of implying the server is untouched.
// Ve bir kurtarmanin bilemeyecegi tek sey.
const firewallApplyInterruptedAttemptSentence = "An earlier attempt was " +
	"interrupted before it could put back its own work, so the saved firewall " +
	"policy on this server may not match what is live; turning the firewall on " +
	"or off again once this server can load nftables converges it."

// firewallApplyCleanFailureText names the terminal failure. afterRestart says
// whether startup recovery is speaking, which is the only case that has to
// warn about an interrupted predecessor it could not undo.
// firewallApplyCleanFailureText, kalici basarisizligi adlandirir.
func firewallApplyCleanFailureText(
	outcome firewallHostOutcome,
	cause error,
	afterRestart bool,
) (code string, message string, clean bool) {
	reason := "unknown"
	if cause != nil {
		reason = strings.TrimSpace(cause.Error())
	}
	if reason == "" {
		reason = "unknown"
	}
	if len(reason) > firewallApplyFailureReasonLimit {
		reason = reason[:firewallApplyFailureReasonLimit] + "..."
	}
	tail := ""
	if afterRestart {
		tail = firewallApplyInterruptedAttemptSentence + " "
	}
	tail += firewallApplyResidueSentence + " Reason: " + reason
	switch outcome {
	case firewallHostUntouched:
		return firewallApplyFailedUntouchedCode,
			"The committed firewall change was abandoned without changing " +
				"anything on this server. " + tail,
			true
	case firewallHostRestored:
		return firewallApplyFailedRestoredCode,
			"The committed firewall change could not be applied: this server " +
				"could not load nftables, so no rule reached the kernel. The saved " +
				"firewall policy and the boot restore unit this attempt had already " +
				"written were put back as this attempt found them, and read back. " +
				tail,
			true
	default:
		return "", "", false
	}
}

var firewallApplyJournalFaultHook func(string) error

// Replaceable only by focused startup-recovery tests. Production always uses
// the fixed nft/systemctl runner and fixed root-owned snapshot path.
var recoverFirewallApplyHost = func(
	ctx context.Context,
	journal *firewallApplyJournal,
) (firewallHostOutcome, error) {
	firewallMu.Lock()
	defer firewallMu.Unlock()
	return convergeFirewallApplyPlan(
		ctx,
		journal,
		hostFirewallCommandRunner{ctx: ctx},
		fileFirewallStateStore{path: firewallSnapshotPath},
	)
}

type firewallApplyJournal struct {
	Version   int    `json:"version"`
	RequestID string `json:"request_id"`
	Qualifier string `json:"qualifier"`
	Enabled   bool   `json:"enabled"`
	Persist   bool   `json:"persist"`
	TCPPorts  []int  `json:"tcp_ports,omitempty"`
	UDPPorts  []int  `json:"udp_ports,omitempty"`
	SSHPorts  []int  `json:"ssh_ports,omitempty"`
	// NoSSHService records that this host was proven to carry no SSH service
	// when the intent was written. It is the only thing that makes an enabled
	// journal with an empty SSH port set valid, so recovery after a crash
	// replays exactly the plan the operator accepted and nothing wider.
	// NoSSHService, niyet yazıldığında bu sunucuda hiç SSH servisi olmadığının
	// kanıtlandığını kaydeder. Açık bir günlüğü boş SSH port kümesiyle geçerli
	// kılan tek şey budur; böylece bir çökme sonrası kurtarma, operatörün kabul
	// ettiği planın tam olarak aynısını, daha genişini değil, yeniden oynatır.
	NoSSHService        bool   `json:"no_ssh_service,omitempty"`
	PriorSnapshotExists bool   `json:"prior_snapshot_exists"`
	PriorSnapshot       []byte `json:"prior_snapshot,omitempty"`
	// PriorRestoreUnit records the boot restore unit's state before this plan
	// touched it, and exists for one reason: a persisting plan that writes the
	// unit and then finds it cannot load nftables must be able to put the unit
	// back and prove it, instead of leaving a machine that will turn its
	// firewall on by itself at the next boot for a request that never
	// succeeded. It is empty on a plan that does not persist, and empty on a
	// journal written before this field existed, in which case the unit is
	// simply not provable and the plan stays fail-closed.
	// PriorRestoreUnit, bu plan dokunmadan once acilis geri yukleme unitinin
	// durumunu kaydeder. Kalicilastiran bir plan nftables'i yukleyemedigini
	// anlarsa uniti geri koyabilmeli ve bunu kanitlayabilmelidir.
	PriorRestoreUnit string `json:"prior_restore_unit,omitempty"`
}

func formatFirewallApplyCommitPhase(state, requestID, qualifier string) (string, error) {
	if (state != firewallApplyCommitIntent && state != firewallApplyCommitPublished) ||
		!validMutationIdentity(requestID) ||
		!mutationpayload.ValidFirewallApplyQualifier(qualifier) {
		return "", errors.New("invalid firewall apply commit phase identity")
	}
	return firewallApplyCommitPhasePrefix + state + "/" + requestID + "/" + qualifier, nil
}

func parseFirewallApplyCommitPhase(value string) (
	state, requestID, qualifier string,
	err error,
) {
	if !strings.HasPrefix(value, firewallApplyCommitPhasePrefix) {
		return "", "", "", errors.New("not a firewall apply commit phase")
	}
	remainder := strings.TrimPrefix(value, firewallApplyCommitPhasePrefix)
	state, remainder, found := strings.Cut(remainder, "/")
	if !found {
		return "", "", "", errors.New("invalid firewall apply commit phase")
	}
	requestID, qualifier, found = strings.Cut(remainder, "/")
	if !found {
		return "", "", "", errors.New("invalid firewall apply commit phase")
	}
	canonical, formatErr := formatFirewallApplyCommitPhase(state, requestID, qualifier)
	if formatErr != nil || canonical != value {
		return "", "", "", errors.New("invalid firewall apply commit phase")
	}
	return state, requestID, qualifier, nil
}

func equalFirewallPorts(left, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func canonicalAgentFirewallPorts(ports []int) ([]int, error) {
	frozen := append([]int(nil), ports...)
	if len(frozen) > 4096 {
		return nil, errors.New("agent-derived firewall port set exceeds the limit")
	}
	for _, port := range frozen {
		if port < 1 || port > 65535 {
			return nil, errors.New("agent-derived firewall port is invalid")
		}
	}
	sort.Ints(frozen)
	result := frozen[:0]
	for _, port := range frozen {
		if len(result) == 0 || result[len(result)-1] != port {
			result = append(result, port)
		}
	}
	return result, nil
}

func validateFirewallApplyJournal(journal *firewallApplyJournal) error {
	if journal == nil || journal.Version != firewallApplyJournalVersion ||
		!validMutationIdentity(journal.RequestID) ||
		!mutationpayload.ValidFirewallApplyQualifier(journal.Qualifier) {
		return errors.New("firewall apply journal identity is invalid")
	}
	commitment, err := mutationpayload.CanonicalFirewallApply(
		journal.Enabled,
		journal.Persist,
		journal.TCPPorts,
		journal.UDPPorts,
	)
	if err != nil || commitment.Qualifier != journal.Qualifier ||
		!equalFirewallPorts(commitment.TCPPorts, journal.TCPPorts) ||
		!equalFirewallPorts(commitment.UDPPorts, journal.UDPPorts) {
		return errors.New("firewall apply journal payload is not canonical")
	}
	if !journal.Enabled && !journal.Persist {
		return errors.New("firewall apply journal contains forbidden live-only disable")
	}
	if !journal.Enabled &&
		(journal.PriorSnapshotExists || len(journal.PriorSnapshot) != 0) {
		return errors.New("disabled firewall journal contains unused prior snapshot data")
	}
	sshPorts, err := canonicalAgentFirewallPorts(journal.SSHPorts)
	if err != nil || !equalFirewallPorts(sshPorts, journal.SSHPorts) ||
		(journal.Enabled && len(sshPorts) == 0 && !journal.NoSSHService) ||
		(!journal.Enabled && len(sshPorts) != 0) {
		return errors.New("firewall apply journal SSH snapshot is invalid")
	}
	// The no-SSH escape is narrow on purpose: it may only stand on an enabled
	// journal that protects no SSH port at all. It can never widen a plan.
	// SSH'sız kaçış bilerek dardır: yalnız hiçbir SSH portunu korumayan, açık
	// bir günlükte durabilir. Bir planı asla genişletemez.
	if journal.NoSSHService && (!journal.Enabled || len(sshPorts) != 0) {
		return errors.New("firewall apply journal no-SSH marker is invalid")
	}
	if journal.PriorSnapshotExists {
		if len(journal.PriorSnapshot) == 0 ||
			len(journal.PriorSnapshot) > maxFirewallSnapshotSize {
			return errors.New("firewall apply journal prior snapshot is invalid")
		}
	} else if len(journal.PriorSnapshot) != 0 {
		return errors.New("firewall apply journal has bytes for an absent prior snapshot")
	}
	if journal.Enabled && journal.PriorSnapshotExists {
		if err := validateFirewallSnapshot(journal.PriorSnapshot); err != nil {
			return fmt.Errorf("firewall apply journal prior snapshot: %w", err)
		}
	}
	// A recorded unit state is one of exactly two words, and only a persisting
	// plan may record one. Absence is allowed and means "not provable", which
	// is how a journal written by an older agent stays readable and stays
	// fail-closed instead of being rejected mid-upgrade.
	// Kaydedilmis bir unit durumu tam olarak iki kelimeden biridir ve yalnizca
	// kalicilastiran bir plan kaydedebilir. Yokluk serbesttir.
	switch journal.PriorRestoreUnit {
	case "":
	case firewallRestoreUnitEnabled, firewallRestoreUnitDisabled:
		if !journal.Persist {
			return errors.New("firewall apply journal records a restore unit state it never touches")
		}
	default:
		return errors.New("firewall apply journal restore unit state is invalid")
	}
	return nil
}

func encodeFirewallApplyJournal(journal *firewallApplyJournal) ([]byte, error) {
	if err := validateFirewallApplyJournal(journal); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(journal)
	if err != nil {
		return nil, fmt.Errorf("encode firewall apply journal: %w", err)
	}
	if len(raw) > firewallApplyJournalMaxSize {
		return nil, errors.New("firewall apply journal exceeds the size limit")
	}
	return raw, nil
}

func decodeFirewallApplyJournal(raw []byte) (*firewallApplyJournal, error) {
	if len(raw) == 0 || len(raw) > firewallApplyJournalMaxSize {
		return nil, errors.New("firewall apply journal has invalid size")
	}
	var journal firewallApplyJournal
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&journal); err != nil {
		return nil, fmt.Errorf("decode firewall apply journal: %w", err)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("firewall apply journal contains multiple JSON values")
		}
		return nil, fmt.Errorf("decode firewall apply journal trailer: %w", err)
	}
	canonical, err := encodeFirewallApplyJournal(&journal)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(raw, canonical) {
		return nil, errors.New("firewall apply journal is not canonical")
	}
	return &journal, nil
}

func firewallApplyJournalPath(manager *serviceMutationManager) string {
	if manager == nil {
		return ""
	}
	return filepath.Join(filepath.Dir(manager.ledgerPath), firewallApplyJournalFileName)
}

func readFirewallApplyJournal(path string) (*firewallApplyJournal, bool, error) {
	if filepath.Base(path) != firewallApplyJournalFileName ||
		!filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, false, errors.New("invalid firewall apply journal path")
	}
	raw, exists, err := readSecureServiceMutationLedger(path, firewallApplyJournalMaxSize)
	if err != nil || !exists {
		return nil, exists, err
	}
	journal, err := decodeFirewallApplyJournal(raw)
	if err != nil {
		return nil, true, err
	}
	return journal, true, nil
}

func writeFirewallApplyJournal(path string, journal *firewallApplyJournal) error {
	raw, err := encodeFirewallApplyJournal(journal)
	if err != nil {
		return err
	}
	if filepath.Base(path) != firewallApplyJournalFileName ||
		!filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("invalid firewall apply journal path")
	}
	dir := filepath.Dir(path)
	if err := ensureSecureServiceMutationStateDirectory(dir); err != nil {
		return fmt.Errorf("secure firewall apply journal directory: %w", err)
	}
	if _, _, err := readFirewallApplyJournal(path); err != nil {
		return fmt.Errorf("validate existing firewall apply journal: %w", err)
	}
	stage, err := os.CreateTemp(dir, ".firewall-apply-journal-*.json")
	if err != nil {
		return fmt.Errorf("stage firewall apply journal: %w", err)
	}
	stagePath := stage.Name()
	published := false
	defer func() {
		_ = stage.Close()
		if !published {
			_ = os.Remove(stagePath)
		}
	}()
	if err := stage.Chown(int(serviceMutationRequiredOwnerUID), int(serviceMutationRequiredOwnerGID)); err != nil {
		return fmt.Errorf("set firewall apply journal owner: %w", err)
	}
	if err := stage.Chmod(0o600); err != nil {
		return fmt.Errorf("set firewall apply journal mode: %w", err)
	}
	if _, err := stage.Write(raw); err != nil {
		return fmt.Errorf("write firewall apply journal: %w", err)
	}
	if err := stage.Sync(); err != nil {
		return fmt.Errorf("sync firewall apply journal: %w", err)
	}
	if err := stage.Close(); err != nil {
		return fmt.Errorf("close firewall apply journal: %w", err)
	}
	if firewallApplyJournalFaultHook != nil {
		if err := firewallApplyJournalFaultHook(firewallApplyJournalFaultBeforeRename); err != nil {
			return fmt.Errorf("injected failure before firewall apply journal rename: %w", err)
		}
	}
	if err := os.Rename(stagePath, path); err != nil {
		return fmt.Errorf("publish firewall apply journal: %w", err)
	}
	published = true
	if firewallApplyJournalFaultHook != nil {
		if err := firewallApplyJournalFaultHook(firewallApplyJournalFaultAfterRename); err != nil {
			return fmt.Errorf("injected failure after firewall apply journal rename: %w", err)
		}
	}
	if err := syncServiceMutationDirectory(path); err != nil {
		return fmt.Errorf("sync firewall apply journal directory: %w", err)
	}
	if firewallApplyJournalFaultHook != nil {
		if err := firewallApplyJournalFaultHook(firewallApplyJournalFaultAfterSync); err != nil {
			return fmt.Errorf("injected failure after firewall apply journal sync: %w", err)
		}
	}
	return nil
}

func cleanupAbandonedFirewallApplyJournalStages(stateDir string) error {
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		return fmt.Errorf("inspect abandoned firewall journal stages: %w", err)
	}
	var stages []string
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".firewall-apply-journal-") &&
			strings.HasSuffix(name, ".json") {
			stages = append(stages, filepath.Join(stateDir, name))
		}
	}
	if len(stages) > firewallApplyJournalStageLimit {
		return errors.New("abandoned firewall journal stage count exceeds the limit")
	}
	sort.Strings(stages)
	for _, stage := range stages {
		raw, exists, err := readSecureServiceMutationLedger(stage, firewallApplyJournalMaxSize)
		if err != nil || !exists {
			if err == nil {
				err = errors.New("firewall journal stage disappeared")
			}
			return fmt.Errorf("validate abandoned firewall journal stage: %w", err)
		}
		if _, err := decodeFirewallApplyJournal(raw); err != nil {
			return fmt.Errorf("validate abandoned firewall journal stage: %w", err)
		}
	}
	for _, stage := range stages {
		if err := os.Remove(stage); err != nil {
			return fmt.Errorf("remove abandoned firewall journal stage: %w", err)
		}
	}
	if len(stages) != 0 {
		return syncServiceMutationDirectory(
			filepath.Join(stateDir, firewallApplyJournalFileName),
		)
	}
	return nil
}

func prepareFirewallApplyJournal(
	ctx context.Context,
	commitment mutationpayload.FirewallApplyCommitment,
	runner firewallCommandRunner,
	store firewallStateStore,
) (*firewallApplyJournal, error) {
	if ctx == nil || runner == nil || store == nil ||
		!mutationpayload.ValidFirewallApplyQualifier(commitment.Qualifier) {
		return nil, errors.New("invalid firewall apply preparation")
	}
	recomputed, err := mutationpayload.CanonicalFirewallApply(
		commitment.Enabled,
		commitment.Persist,
		commitment.TCPPorts,
		commitment.UDPPorts,
	)
	if err != nil || recomputed.Qualifier != commitment.Qualifier ||
		!equalFirewallPorts(recomputed.TCPPorts, commitment.TCPPorts) ||
		!equalFirewallPorts(recomputed.UDPPorts, commitment.UDPPorts) {
		return nil, errors.New("firewall apply commitment is not canonical")
	}
	if !commitment.Enabled && !commitment.Persist {
		return nil, errors.New("live-only firewall disable is not authorized")
	}
	if _, err := runner.LookPath("nft"); err != nil {
		return nil, errors.New("firewall engine (nftables) is not installed")
	}
	// R-054: having the nft binary is not having a firewall engine. On a host
	// whose running kernel was replaced without a restart nft is present and
	// cannot reach the kernel at all, and the old code discovered that only
	// after the durable intent had been written - which is how one ordinary
	// "Turn on" click poisoned a whole control plane. Ask the kernel here,
	// before the transaction exists, so the answer is a plain refusal on a
	// host that was never touched and never became busy.
	// R-054: nft ikilisine sahip olmak, bir guvenlik duvari motoruna sahip
	// olmak degildir. Cekirdegi yeniden baslatilmadan degistirilmis bir
	// makinede nft vardir ve cekirdege hic ulasamaz. Soruyu islem var olmadan
	// once sor.
	if _, err := discoverFirewallTables(runner); err != nil {
		return nil, err
	}
	priorRestoreUnit := ""
	if commitment.Persist {
		if _, err := runner.LookPath("systemctl"); err != nil {
			return nil, errors.New("systemd client failed security validation")
		}
		// Read where the boot restore unit stands before anything moves it, so
		// a plan that has to be abandoned can put it back and prove it.
		// Hicbir sey oynatmadan once acilis geri yukleme unitinin nerede
		// durdugunu oku.
		priorRestoreUnit = readFirewallRestoreUnitState(runner)
	}
	priorSnapshot, priorSnapshotExists, err := store.Load()
	if err != nil {
		return nil, err
	}
	if commitment.Enabled && priorSnapshotExists {
		if err := validateFirewallSnapshot(priorSnapshot); err != nil {
			return nil, err
		}
	}
	if !commitment.Enabled {
		priorSnapshot = nil
		priorSnapshotExists = false
	}
	var sshPorts []int
	noSSHService := false
	if commitment.Enabled {
		sshPorts, err = detectSSHPortsWithRunner(runner)
		if err != nil {
			// A host proven to have no SSH service has no door for this
			// firewall to lock, so the escape-hatch proof is moot rather than
			// violated. Every other reason — including an SSH service that is
			// merely not listening right now — is still a refusal.
			// Hiç SSH servisi olmadığı kanıtlanmış bir sunucuda bu güvenlik
			// duvarının kilitleyeceği bir kapı yoktur; kaçış yolu kanıtı
			// çiğnenmiş değil, konusuz kalmıştır. Diğer her neden — şu an
			// dinlemeyen bir SSH servisi dâhil — hâlâ bir reddir.
			refusal := classifySSHDiscovery(runner, err)
			if refusal.reason != transport.SSHDiscoveryNoService {
				return nil, refusal
			}
			noSSHService = true
			sshPorts = nil
		} else {
			sshPorts, err = canonicalAgentFirewallPorts(sshPorts)
			if err != nil || len(sshPorts) == 0 {
				return nil, errors.New("SSH listener discovery returned an invalid canonical snapshot")
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.New("service mutation lease ended before firewall commit intent")
	}
	return &firewallApplyJournal{
		Version:             firewallApplyJournalVersion,
		Qualifier:           commitment.Qualifier,
		Enabled:             commitment.Enabled,
		Persist:             commitment.Persist,
		TCPPorts:            append([]int(nil), commitment.TCPPorts...),
		UDPPorts:            append([]int(nil), commitment.UDPPorts...),
		SSHPorts:            append([]int(nil), sshPorts...),
		NoSSHService:        noSSHService,
		PriorSnapshotExists: priorSnapshotExists,
		PriorSnapshot:       append([]byte(nil), priorSnapshot...),
		PriorRestoreUnit:    priorRestoreUnit,
	}, nil
}

// readFirewallRestoreUnitState answers with one of the two words this plan can
// put back, or with nothing at all. Nothing is a truthful answer: a unit in
// some third state, or a systemctl that would not say, is one this plan must
// not later claim to have restored.
// readFirewallRestoreUnitState, bu planin geri koyabilecegi iki kelimeden
// biriyle ya da hicbir seyle yanit verir.
func readFirewallRestoreUnitState(runner firewallCommandRunner) string {
	out, err := runner.Output(
		"systemctl",
		"show",
		"--no-pager",
		"--property=UnitFileState",
		"--value",
		firewallRestoreUnitName,
	)
	if err != nil {
		return ""
	}
	switch state := strings.TrimSpace(string(out)); state {
	case firewallRestoreUnitEnabled, firewallRestoreUnitDisabled:
		return state
	default:
		return ""
	}
}

func activeDirectFirewallApplyJob(job *ServiceMutationJob) bool {
	return job != nil && serviceMutationStatusActive(job.Status) &&
		(job.Kind == "firewall_apply" || job.Kind == "firewall_sync") &&
		job.Target == "nftables"
}

func firewallApplyJobMatchesJournal(
	job *ServiceMutationJob,
	journal *firewallApplyJournal,
) bool {
	return activeDirectFirewallApplyJob(job) && journal != nil &&
		job.RequestID == journal.RequestID &&
		job.PackageName == journal.Qualifier &&
		mutationpayload.ValidFirewallApplyQualifier(job.PackageName)
}

func cloneFirewallApplyJournal(journal *firewallApplyJournal) *firewallApplyJournal {
	if journal == nil {
		return nil
	}
	copy := *journal
	copy.TCPPorts = append([]int(nil), journal.TCPPorts...)
	copy.UDPPorts = append([]int(nil), journal.UDPPorts...)
	copy.SSHPorts = append([]int(nil), journal.SSHPorts...)
	copy.PriorSnapshot = append([]byte(nil), journal.PriorSnapshot...)
	return &copy
}

// commitStandaloneFirewallApplyIntent durably records the complete derived
// plan before any host effect. A successful intent write is the transaction's
// linearization point and makes forward convergence non-cancellable.
func commitStandaloneFirewallApplyIntent(
	ctx context.Context,
	prepared *firewallApplyJournal,
) (*firewallApplyJournal, error) {
	if ctx == nil || prepared == nil {
		return nil, errors.New("invalid firewall apply commit intent")
	}
	tracker, _ := ctx.Value(serviceMutationExecutionTrackerKey{}).(*serviceMutationExecutionTracker)
	if tracker == nil || tracker.manager == nil || tracker.runtime == nil {
		return nil, errors.New("firewall apply commit intent requires a durable execution tracker")
	}
	m, runtime := tracker.manager, tracker.runtime
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.healthErrorLocked(); err != nil {
		return nil, err
	}
	if m.active != runtime || runtime.job == nil || runtime.steps != 1 ||
		runtime.job.WorkerPID != 0 ||
		runtime.job.Status != serviceMutationStatusRunning {
		return nil, errors.New("firewall apply commit intent lost the active mutation step")
	}
	job := runtime.job
	if !activeDirectFirewallApplyJob(job) ||
		job.PackageName != prepared.Qualifier ||
		!mutationpayload.ValidFirewallApplyQualifier(job.PackageName) {
		return nil, errors.New("firewall apply commit intent rejected the mutation identity")
	}
	if strings.HasPrefix(job.Phase, firewallApplyCommitPhasePrefix) {
		return nil, errors.New("firewall apply job already crossed its commit point")
	}
	now := m.now()
	if ctx.Err() != nil || !now.Before(job.LeaseExpiresAt) || !now.Before(job.DeadlineAt) {
		return nil, errors.New("service mutation lease ended before firewall commit intent")
	}
	journal := cloneFirewallApplyJournal(prepared)
	journal.RequestID = job.RequestID
	if err := validateFirewallApplyJournal(journal); err != nil {
		return nil, err
	}
	if err := writeFirewallApplyJournal(firewallApplyJournalPath(m), journal); err != nil {
		return nil, m.poisonLocked(fmt.Errorf("persist firewall apply journal: %w", err))
	}
	intentPhase, err := formatFirewallApplyCommitPhase(
		firewallApplyCommitIntent,
		job.RequestID,
		job.PackageName,
	)
	if err != nil {
		return nil, m.poisonLocked(err)
	}
	before := cloneServiceMutationLedger(m.ledger)
	job.Phase = intentPhase
	job.UpdatedAt = now
	if err := m.persistLedgerMutationLocked(before); err != nil {
		return nil, err
	}
	runtime.firewallApplyCommittedPhase = intentPhase
	return journal, nil
}

func poisonFirewallApplyConvergence(ctx context.Context, cause error) error {
	if ctx == nil || cause == nil {
		return errors.New("invalid firewall convergence poison request")
	}
	tracker, _ := ctx.Value(serviceMutationExecutionTrackerKey{}).(*serviceMutationExecutionTracker)
	if tracker == nil || tracker.manager == nil || tracker.runtime == nil {
		return errors.New("firewall convergence poison requires a durable execution tracker")
	}
	m, runtime := tracker.manager, tracker.runtime
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active != runtime || runtime.job == nil || runtime.steps != 1 ||
		runtime.firewallApplyCommittedPhase == "" {
		return errors.New("firewall convergence poison lost the committed mutation")
	}
	return m.poisonLocked(fmt.Errorf("firewall host convergence is ambiguous: %w", cause))
}

// failStandaloneFirewallApply ends a committed plan that cannot succeed, with
// its reason written durably and the ledger released. It is the exact mirror
// of publishStandaloneFirewallApply below, and the only door out of a commit
// other than success or poison.
// failStandaloneFirewallApply, basarili olamayacak, taahhut edilmis bir plani
// nedeni kalici olarak yazilmis ve defter serbest birakilmis halde bitirir.
func failStandaloneFirewallApply(
	ctx context.Context,
	journal *firewallApplyJournal,
	code, message string,
) error {
	tracker, _ := ctx.Value(serviceMutationExecutionTrackerKey{}).(*serviceMutationExecutionTracker)
	if tracker == nil || tracker.manager == nil || tracker.runtime == nil {
		return errors.New("firewall apply failure requires a durable execution tracker")
	}
	m, runtime := tracker.manager, tracker.runtime
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.healthErrorLocked(); err != nil {
		return err
	}
	if m.active != runtime || runtime.job == nil || runtime.steps != 1 ||
		runtime.job.WorkerPID != 0 ||
		runtime.firewallApplyCommittedPhase == "" ||
		!firewallApplyJobMatchesJournal(runtime.job, journal) {
		return errors.New("firewall apply failure lost the committed mutation")
	}
	runtime.firewallApplyCommittedPhase = ""
	if err := m.finishRuntimeTerminalLocked(
		runtime, false, firewallApplyFailedPhase, code, message,
	); err != nil {
		if m.active == runtime {
			return m.poisonLocked(fmt.Errorf(
				"persist terminal firewall failure: %w", err,
			))
		}
		return err
	}
	return nil
}

func publishStandaloneFirewallApply(
	ctx context.Context,
	journal *firewallApplyJournal,
) error {
	tracker, _ := ctx.Value(serviceMutationExecutionTrackerKey{}).(*serviceMutationExecutionTracker)
	if tracker == nil || tracker.manager == nil || tracker.runtime == nil {
		return errors.New("firewall publication requires a durable execution tracker")
	}
	m, runtime := tracker.manager, tracker.runtime
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.healthErrorLocked(); err != nil {
		return err
	}
	if m.active != runtime || runtime.job == nil || runtime.steps != 1 ||
		runtime.job.WorkerPID != 0 ||
		runtime.firewallApplyCommittedPhase == "" ||
		!firewallApplyJobMatchesJournal(runtime.job, journal) {
		return errors.New("firewall publication lost the committed mutation")
	}
	persisted, exists, err := readFirewallApplyJournal(firewallApplyJournalPath(m))
	if err != nil || !exists || !firewallApplyJobMatchesJournal(runtime.job, persisted) ||
		!equalFirewallApplyJournals(persisted, journal) {
		if err == nil {
			err = errors.New("firewall apply journal identity changed before publication")
		}
		return m.poisonLocked(err)
	}
	publishedPhase, err := formatFirewallApplyCommitPhase(
		firewallApplyCommitPublished,
		runtime.job.RequestID,
		runtime.job.PackageName,
	)
	if err != nil {
		return m.poisonLocked(err)
	}
	runtime.firewallApplyCommittedPhase = publishedPhase
	if err := m.finishRuntimeTerminalLocked(runtime, true, publishedPhase, "", ""); err != nil {
		if m.active == runtime {
			return m.poisonLocked(fmt.Errorf(
				"persist terminal firewall receipt after host convergence: %w",
				err,
			))
		}
		return err
	}
	return nil
}

func equalFirewallApplyJournals(left, right *firewallApplyJournal) bool {
	if left == nil || right == nil {
		return left == right
	}
	leftRaw, leftErr := encodeFirewallApplyJournal(left)
	rightRaw, rightErr := encodeFirewallApplyJournal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftRaw, rightRaw)
}

func firewallRunnerWithContext(
	runner firewallCommandRunner,
	ctx context.Context,
) firewallCommandRunner {
	if host, ok := runner.(hostFirewallCommandRunner); ok {
		host.ctx = ctx
		return host
	}
	return runner
}

func applyStandaloneFirewallV2(
	ctx context.Context,
	commitment mutationpayload.FirewallApplyCommitment,
	runner firewallCommandRunner,
	store firewallStateStore,
	response *FirewallStatusResponse,
) error {
	firewallMu.Lock()
	defer firewallMu.Unlock()
	response.EngineAvailable = false
	prepared, err := prepareFirewallApplyJournal(ctx, commitment, runner, store)
	if err != nil {
		// A refused SSH discovery names its reason so the panel can offer the
		// operator the exact way forward instead of one opaque sentence.
		// Reddedilen bir SSH keşfi nedenini adlandırır; böylece panel
		// operatöre kapalı tek bir cümle yerine tam olarak izlenecek yolu
		// sunabilir.
		var refusal *sshDiscoveryRefusal
		if errors.As(err, &refusal) {
			response.SSHDiscoveryReason = refusal.reason
		}
		response.PersistenceState = firewallPersistenceUnverified
		response.PersistenceError = err.Error()
		response.Error = err.Error()
		return nil
	}
	response.EngineAvailable = true
	journal, err := commitStandaloneFirewallApplyIntent(ctx, prepared)
	if err != nil {
		response.PersistenceState = firewallPersistenceUnverified
		response.PersistenceError = err.Error()
		response.Error = err.Error()
		return nil
	}

	convergenceCtx, cancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		firewallApplyConvergenceTimeout,
	)
	defer cancel()
	convergenceRunner := firewallRunnerWithContext(runner, convergenceCtx)
	outcome, err := convergeFirewallApplyPlan(convergenceCtx, journal, convergenceRunner, store)
	if err != nil {
		// R-054. A committed plan that failed on a host provably where the
		// plan found it is finished as failed here, so one firewall request
		// that this machine cannot serve cannot hold every other mutation on
		// it. Anything the plan may still be holding on the host stays
		// fail-closed: the ledger is poisoned and startup recovery gets the
		// next word.
		// R-054. Kanitli bicimde planin buldugu yerde duran bir makinede
		// basarisiz olan, taahhut edilmis bir plan burada kalici olarak
		// bitirilir; boylece bu makinenin karsilayamadigi tek bir guvenlik
		// duvari istegi, uzerindeki diger tum mutasyonlari tutamaz.
		response.PersistenceState = firewallPersistenceUnverified
		response.PersistenceError = err.Error()
		if code, message, clean := firewallApplyCleanFailureText(outcome, err, false); clean {
			if failErr := failStandaloneFirewallApply(ctx, journal, code, message); failErr != nil {
				log.Printf(
					"Firewall V2 commit could not be failed cleanly: %v; cause: %v",
					failErr, err,
				)
				response.Error = "firewall commit requires startup recovery"
				return nil
			}
			log.Printf("Firewall V2 commit failed cleanly and released the ledger: %v", err)
			response.Error = message
			return nil
		}
		poisonErr := poisonFirewallApplyConvergence(ctx, err)
		log.Printf("Firewall V2 convergence failed after durable intent: %v; poison: %v", err, poisonErr)
		response.Error = "firewall commit requires startup recovery"
		return nil
	}
	populateFirewallApplyResponse(journal, response)
	if err := publishStandaloneFirewallApply(ctx, journal); err != nil {
		log.Printf("Firewall V2 host convergence completed with receipt error: %v", err)
	}
	return nil
}

// convergeFirewallApplyPlan applies the committed plan and reports both
// whether it succeeded and what it left on the host. Only the second answer
// may decide whether a failure is allowed to be terminal.
// convergeFirewallApplyPlan taahhut edilmis plani uygular ve hem basarili olup
// olmadigini hem de makinede ne biraktigini bildirir.
func convergeFirewallApplyPlan(
	ctx context.Context,
	journal *firewallApplyJournal,
	runner firewallCommandRunner,
	store firewallStateStore,
) (firewallHostOutcome, error) {
	if err := validateFirewallApplyJournal(journal); err != nil {
		return firewallHostUntouched, err
	}
	if err := ctx.Err(); err != nil {
		return firewallHostUntouched, err
	}
	// Ask the kernel before writing anything. The pre-commit preparation asked
	// the same question; asking it again here is what makes a startup recovery
	// on a host that still cannot load nftables end in a clean failure rather
	// than in another poisoned ledger, and it costs one read.
	// Hicbir sey yazmadan once cekirdege sor. Ayni soruyu taahhut oncesi
	// hazirlik da sordu; burada tekrar sormak, hala nftables yukleyemeyen bir
	// makinedeki baslangic kurtarmasinin temiz bir basarisizlikla bitmesini
	// saglar.
	tables, err := discoverFirewallTables(runner)
	if err != nil {
		return firewallHostUntouched, err
	}
	wroteSnapshot, wroteUnit := false, false
	switch {
	case journal.Enabled && journal.Persist:
		if err := store.Save(firewallDesiredSnapshot(journal)); err != nil {
			return firewallHostAmbiguous, fmt.Errorf("write desired firewall snapshot: %w", err)
		}
		wroteSnapshot = true
		if err := setFirewallRestoreUnitEnabled(runner, true); err != nil {
			return firewallHostAmbiguous, err
		}
		wroteUnit = true
	case journal.Enabled && journal.PriorSnapshotExists:
		if err := store.Save(firewallDesiredSnapshot(journal)); err != nil {
			return firewallHostAmbiguous, fmt.Errorf("update existing firewall snapshot: %w", err)
		}
		wroteSnapshot = true
	case !journal.Enabled && journal.Persist:
		if err := setFirewallRestoreUnitEnabled(runner, false); err != nil {
			return firewallHostAmbiguous, err
		}
		wroteUnit = true
		if err := store.Remove(); err != nil {
			return firewallHostAmbiguous, fmt.Errorf("remove firewall snapshot: %w", err)
		}
		wroteSnapshot = true
	}
	if err := applyFirewallLivePlan(runner, journal, tables); err != nil {
		return firewallApplyLiveFailureOutcome(
			journal, runner, store, err, wroteSnapshot, wroteUnit,
		)
	}
	if err := verifyFirewallSnapshotPlan(store, journal); err != nil {
		return firewallHostAmbiguous, err
	}
	if journal.Persist {
		if err := verifyFirewallRestoreUnitState(runner, journal.Enabled); err != nil {
			return firewallHostAmbiguous, err
		}
	}
	if err := verifyFirewallLivePlan(runner, journal); err != nil {
		return firewallHostAmbiguous, err
	}
	return firewallHostConverged, nil
}

// firewallApplyLiveFailureOutcome decides what a failed live apply left on the
// host. Exactly one class of failure may be called harmless: the one where
// this machine is proven unable to load any kernel module at all, because its
// running kernel module tree is not on disk. That proof is structural, does
// not depend on how nft worded itself, and means the kernel accepted nothing -
// so the live ruleset is untouched and only this attempt own durable writes
// have to be put back. Every other failure, including an nft that merely said
// something kernel-shaped, keeps the old behaviour: the ruleset may be half
// applied, the host is ambiguous, and the ledger is held.
//
// firewallApplyLiveFailureOutcome, basarisiz bir canli uygulamanin makinede ne
// biraktigina karar verir. Yalnizca tek bir hata sinifi zararsiz sayilabilir:
// makinenin hicbir cekirdek modulu yukleyemedigi kanitlanan durum. Bu kanit
// yapisaldir ve cekirdegin hicbir sey kabul etmedigi anlamina gelir. Diger her
// hata eski davranisi korur: kural seti yarim uygulanmis olabilir.
func firewallApplyLiveFailureOutcome(
	journal *firewallApplyJournal,
	runner firewallCommandRunner,
	store firewallStateStore,
	cause error,
	wroteSnapshot, wroteUnit bool,
) (firewallHostOutcome, error) {
	if firewallEngineFaultOf(cause) != firewallEngineFaultModulesMissing {
		return firewallHostAmbiguous, cause
	}
	if !wroteSnapshot && !wroteUnit {
		return firewallHostUntouched, cause
	}
	if err := restoreFirewallApplyPersistence(
		journal, runner, store, wroteSnapshot, wroteUnit,
	); err != nil {
		return firewallHostAmbiguous, errors.Join(cause, err)
	}
	return firewallHostRestored, cause
}

// restoreFirewallApplyPersistence puts back the durable half of a plan that
// could not reach the kernel, and then reads it back. It refuses anything it
// cannot prove: a disable plan does not carry the snapshot it removed, so that
// snapshot cannot be restored and the host stays ambiguous rather than being
// declared clean on an assumption.
// restoreFirewallApplyPersistence, cekirdege ulasamayan bir planin kalici
// yarisini geri koyar ve geri okur. Kanitlayamadigi hicbir seyi kabul etmez.
func restoreFirewallApplyPersistence(
	journal *firewallApplyJournal,
	runner firewallCommandRunner,
	store firewallStateStore,
	wroteSnapshot, wroteUnit bool,
) error {
	if wroteSnapshot {
		if !journal.Enabled {
			return errors.New(
				"the firewall policy this attempt removed was not recorded, so it cannot be put back",
			)
		}
		if err := restoreFirewallSnapshotState(
			store, journal.PriorSnapshot, journal.PriorSnapshotExists,
		); err != nil {
			return fmt.Errorf("put back the earlier firewall snapshot: %w", err)
		}
	}
	if wroteUnit {
		switch journal.PriorRestoreUnit {
		case firewallRestoreUnitEnabled, firewallRestoreUnitDisabled:
			if err := setFirewallRestoreUnitEnabled(
				runner, journal.PriorRestoreUnit == firewallRestoreUnitEnabled,
			); err != nil {
				return fmt.Errorf("put back the firewall restore unit: %w", err)
			}
		default:
			return errors.New(
				"the earlier state of the firewall restore unit was not recorded, so it cannot be put back",
			)
		}
	}
	if wroteSnapshot {
		actual, exists, err := store.Load()
		if err != nil {
			return fmt.Errorf("read back the restored firewall snapshot: %w", err)
		}
		if exists != journal.PriorSnapshotExists ||
			(exists && !bytes.Equal(actual, journal.PriorSnapshot)) {
			return errors.New("the restored firewall snapshot does not match what this attempt found")
		}
	}
	if wroteUnit {
		if err := verifyFirewallRestoreUnitState(
			runner, journal.PriorRestoreUnit == firewallRestoreUnitEnabled,
		); err != nil {
			return err
		}
	}
	return nil
}

func firewallDesiredSnapshot(journal *firewallApplyJournal) []byte {
	return encodeFirewallSnapshot(journal.TCPPorts, journal.UDPPorts, journal.SSHPorts)
}

func firewallEffectiveTCPPorts(journal *firewallApplyJournal) []int {
	return dedupeSorted(append(
		append([]int(nil), journal.TCPPorts...),
		journal.SSHPorts...,
	))
}

// applyFirewallLivePlan writes the plan into the kernel. The table list it
// works from was read by the caller before anything on this host was changed,
// so a kernel that cannot answer is never discovered here for the first time.
// applyFirewallLivePlan plani cekirdege yazar. Calistigi tablo listesi,
// makinede hicbir sey degismeden once cagiran tarafindan okunmustur.
func applyFirewallLivePlan(
	runner firewallCommandRunner,
	journal *firewallApplyJournal,
	tables []byte,
) error {
	present := firewallTablePresent(tables)
	if !journal.Enabled {
		if !present {
			return nil
		}
		out, err := runner.CombinedOutput(
			"nft",
			[]string{"delete", "table", "inet", fwTable},
			"",
		)
		if err != nil {
			return newFirewallEngineError("nft disable failed", out, err)
		}
		return nil
	}
	rules := buildFirewallRuleset(
		present,
		firewallEffectiveTCPPorts(journal),
		journal.UDPPorts,
	)
	out, err := runner.CombinedOutput("nft", []string{"-f", "-"}, rules)
	if err != nil {
		return newFirewallEngineError("nft apply failed", out, err)
	}
	return nil
}

func verifyFirewallSnapshotPlan(
	store firewallStateStore,
	journal *firewallApplyJournal,
) error {
	actual, exists, err := store.Load()
	if err != nil {
		return fmt.Errorf("read back firewall snapshot: %w", err)
	}
	var expected []byte
	expectedExists := journal.PriorSnapshotExists
	switch {
	case journal.Enabled && (journal.Persist || journal.PriorSnapshotExists):
		expected, expectedExists = firewallDesiredSnapshot(journal), true
	case !journal.Enabled && journal.Persist:
		expected, expectedExists = nil, false
	default:
		expected = journal.PriorSnapshot
	}
	if exists != expectedExists || (exists && !bytes.Equal(actual, expected)) {
		return errors.New("firewall snapshot readback does not match the committed plan")
	}
	return nil
}

func verifyFirewallRestoreUnitState(
	runner firewallCommandRunner,
	enabled bool,
) error {
	out, err := runner.Output(
		"systemctl",
		"show",
		"--no-pager",
		"--property=UnitFileState",
		"--value",
		firewallRestoreUnitName,
	)
	if err != nil {
		return fmt.Errorf("read back firewall restore unit state: %w", err)
	}
	want := "disabled"
	if enabled {
		want = "enabled"
	}
	if strings.TrimSpace(string(out)) != want {
		return fmt.Errorf(
			"firewall restore unit readback is %q, want %q",
			strings.TrimSpace(string(out)),
			want,
		)
	}
	return nil
}

func parseExactFirewallPortRule(line, protocol string) ([]int, bool, error) {
	prefix := protocol + " dport "
	if !strings.HasPrefix(line, prefix) {
		return nil, false, nil
	}
	body := strings.TrimSpace(strings.TrimPrefix(line, prefix))
	if !strings.HasSuffix(body, " accept") {
		return nil, true, errors.New("firewall port rule has an unexpected verdict")
	}
	body = strings.TrimSpace(strings.TrimSuffix(body, " accept"))
	var tokens []string
	if strings.HasPrefix(body, "{") && strings.HasSuffix(body, "}") {
		body = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(body, "{"), "}"))
		tokens = strings.Split(body, ",")
	} else {
		tokens = []string{body}
	}
	if len(tokens) == 0 {
		return nil, true, errors.New("firewall port rule is empty")
	}
	ports := make([]int, 0, len(tokens))
	for _, token := range tokens {
		port, err := strconv.Atoi(strings.TrimSpace(token))
		if err != nil || port < 1 || port > 65535 {
			return nil, true, errors.New("firewall port rule contains an invalid port")
		}
		ports = append(ports, port)
	}
	canonical, err := canonicalAgentFirewallPorts(ports)
	if err != nil || len(canonical) != len(ports) {
		return nil, true, errors.New("firewall port rule is not canonical")
	}
	return canonical, true, nil
}

func verifyFirewallLivePlan(
	runner firewallCommandRunner,
	journal *firewallApplyJournal,
) error {
	tables, err := runner.Output("nft", "list", "tables")
	if err != nil {
		return fmt.Errorf("read back nft tables: %w", err)
	}
	present := firewallTablePresent(tables)
	if present != journal.Enabled {
		return errors.New("live firewall presence does not match the committed plan")
	}
	if !present {
		return nil
	}
	out, err := runner.Output("nft", "list", "table", "inet", fwTable)
	if err != nil {
		return fmt.Errorf("read back live firewall table: %w", err)
	}
	actual, err := canonicalFirewallRulesetReadback(out)
	if err != nil {
		return err
	}
	expected, err := canonicalFirewallRulesetReadback([]byte(buildFirewallRuleset(
		false,
		firewallEffectiveTCPPorts(journal),
		journal.UDPPorts,
	)))
	if err != nil {
		return fmt.Errorf("canonicalize expected firewall ruleset: %w", err)
	}
	if actual != expected {
		return errors.New("live firewall ruleset readback does not exactly match the committed plan")
	}
	return nil
}

func canonicalFirewallRulesetReadback(raw []byte) (string, error) {
	lines := make([]string, 0, 16)
	tcpFound, udpFound, loopbackFound := false, false, false
	for _, rawLine := range strings.Split(string(raw), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			return "", errors.New("live firewall readback contains unsupported annotations")
		}
		if line == "iif lo accept" || line == "iif \"lo\" accept" {
			if loopbackFound {
				return "", errors.New("live firewall contains multiple loopback rules")
			}
			loopbackFound = true
			// nft renders the interface-index symbol with quotes even when the
			// accepted input used the unquoted spelling. Both exact spellings
			// describe the same fixed loopback rule and no other iif expression
			// is accepted below.
			line = "iif lo accept"
		} else if strings.HasPrefix(line, "iif ") {
			return "", errors.New("live firewall contains an unexpected input-interface rule")
		} else if ports, found, err := parseExactFirewallPortRule(line, "tcp"); found {
			if err != nil || tcpFound {
				if err == nil {
					err = errors.New("live firewall contains multiple TCP port rules")
				}
				return "", err
			}
			tcpFound = true
			line = "tcp dport { " + joinInts(ports) + " } accept"
		} else if ports, found, err := parseExactFirewallPortRule(line, "udp"); found {
			if err != nil || udpFound {
				if err == nil {
					err = errors.New("live firewall contains multiple UDP port rules")
				}
				return "", err
			}
			udpFound = true
			line = "udp dport { " + joinInts(ports) + " } accept"
		} else {
			line = strings.Join(strings.Fields(line), " ")
			line = strings.Replace(
				line,
				"priority filter; policy drop;",
				"priority 0; policy drop;",
				1,
			)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n"), nil
}

func populateFirewallApplyResponse(
	journal *firewallApplyJournal,
	response *FirewallStatusResponse,
) {
	*response = FirewallStatusResponse{
		Enabled:          journal.Enabled,
		EngineAvailable:  true,
		TCPPorts:         nil,
		UDPPorts:         nil,
		SSHPorts:         nil,
		PersistenceState: "",
	}
	if journal.Enabled {
		response.TCPPorts = firewallEffectiveTCPPorts(journal)
		response.UDPPorts = append([]int(nil), journal.UDPPorts...)
		response.SSHPorts = append([]int(nil), journal.SSHPorts...)
		// An applied policy that protects no SSH port must say why, or the
		// empty port list reads as a discovery the operator never saw.
		// Hiçbir SSH portunu korumayan, uygulanmış bir politika nedenini
		// söylemelidir; yoksa boş port listesi, operatörün hiç görmediği bir
		// keşif gibi okunur.
		if journal.NoSSHService {
			response.SSHDiscoveryReason = transport.SSHDiscoveryNoService
		}
	}
	firewallLastPersistenceError = ""
	firewallLastRestoreError = ""
	var snapshot []byte
	snapshotExists := journal.PriorSnapshotExists
	switch {
	case journal.Enabled && (journal.Persist || journal.PriorSnapshotExists):
		snapshot, snapshotExists = firewallDesiredSnapshot(journal), true
	case !journal.Enabled && journal.Persist:
		snapshot, snapshotExists = nil, false
	default:
		snapshot = append([]byte(nil), journal.PriorSnapshot...)
	}
	setFirewallPersistenceStatus(
		response,
		snapshot,
		snapshotExists,
		nil,
		journal.Enabled,
	)
}

// recoverPersistedFirewallApplyLocked either proves that no firewall commit
// decision existed or completes the exact journaled forward plan. The caller
// holds m.mu and the common host flock. On ambiguity this function retains the
// flock in the poisoned manager.
func (m *serviceMutationManager) recoverPersistedFirewallApplyLocked(
	job *ServiceMutationJob,
	lock *serviceMutationFileLock,
) (handled bool, err error) {
	if !activeDirectFirewallApplyJob(job) {
		return false, nil
	}
	if !mutationpayload.ValidFirewallApplyQualifier(job.PackageName) {
		m.poisonLock = lock
		return true, m.poisonLocked(
			errors.New("active firewall mutation has an invalid or legacy payload qualifier"),
		)
	}
	intent := false
	if strings.HasPrefix(job.Phase, firewallApplyCommitPhasePrefix) {
		state, requestID, qualifier, phaseErr := parseFirewallApplyCommitPhase(job.Phase)
		if phaseErr != nil || requestID != job.RequestID ||
			qualifier != job.PackageName || state != firewallApplyCommitIntent {
			m.poisonLock = lock
			return true, m.poisonLocked(errors.New("active firewall mutation has an invalid commit receipt"))
		}
		intent = true
	}

	if serviceMutationWorkerMatches(job.WorkerPID, job.WorkerStarted) {
		before := cloneServiceMutationLedger(m.ledger)
		job.Status = serviceMutationStatusOrphaned
		if !intent {
			job.Phase = "waiting_for_orphaned_process"
		}
		job.ErrorCode = "agent_restart_worker_alive"
		job.ErrorMessage = "The previous firewall worker is still alive with the recorded process identity."
		job.UpdatedAt = m.now()
		writeErr := m.persistLedgerMutationLocked(before)
		if m.poisoned != nil {
			m.poisonLock = lock
			return true, writeErr
		}
		closeErr := lock.Close()
		if writeErr != nil {
			return true, errors.Join(writeErr, closeErr)
		}
		return true, closeErr
	}

	journalPath := firewallApplyJournalPath(m)
	journal, exists, journalErr := readFirewallApplyJournal(journalPath)
	if journalErr != nil {
		m.poisonLock = lock
		return true, m.poisonLocked(fmt.Errorf(
			"read firewall recovery journal: %w",
			journalErr,
		))
	}
	if !intent {
		if exists {
			// A journal without a ledger intent is an uncommitted or older
			// receipt. Its strict validation above is enough; it authorizes no
			// host effect for this interrupted job.
		}
		writeErr := m.finishPersistedOrphanLocked(
			job,
			"agent_restarted_before_firewall_commit",
			"The agent restarted before the firewall commit decision was durable.",
		)
		if m.poisoned != nil {
			m.poisonLock = lock
			return true, writeErr
		}
		closeErr := lock.Close()
		if writeErr != nil {
			return true, errors.Join(writeErr, closeErr)
		}
		return true, closeErr
	}
	if !exists || !firewallApplyJobMatchesJournal(job, journal) {
		m.poisonLock = lock
		return true, m.poisonLocked(
			errors.New("committed firewall mutation lost its exact recovery journal"),
		)
	}

	recoveryBase, cancel := context.WithTimeout(
		context.Background(),
		firewallApplyConvergenceTimeout,
	)
	runtime := &serviceMutationRuntime{
		job:                         job,
		lock:                        lock,
		ctx:                         recoveryBase,
		cancel:                      cancel,
		firewallApplyCommittedPhase: job.Phase,
	}
	m.mu.Unlock()
	runtime.stepMu.Lock()
	m.mu.Lock()
	if m.active != nil || m.ledger.ActiveRequestID != job.RequestID {
		cancel()
		m.poisonLock = lock
		identityErr := m.poisonLocked(
			errors.New("firewall recovery identity changed"),
		)
		m.mu.Unlock()
		runtime.stepMu.Unlock()
		m.mu.Lock()
		return true, identityErr
	}
	m.active = runtime
	runtime.steps = 1
	before := cloneServiceMutationLedger(m.ledger)
	runtime.job.Status = serviceMutationStatusCancelling
	runtime.job.ErrorCode = "agent_restart_during_firewall_commit"
	runtime.job.ErrorMessage = "The agent is completing a durable firewall commit after restart."
	runtime.job.WorkerPID = 0
	runtime.job.WorkerStarted = ""
	runtime.job.WorkerCommand = ""
	runtime.job.UpdatedAt = m.now()
	if persistErr := m.persistLedgerMutationLocked(before); persistErr != nil {
		poisonErr := m.poisonLocked(fmt.Errorf(
			"persist firewall recovery state: %w",
			persistErr,
		))
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
	recoveryOutcome, recoveryErr := recoverFirewallApplyHost(recoveryCtx, journal)
	cancel()
	m.mu.Lock()
	runtime.steps = 0
	m.mu.Unlock()
	runtime.stepMu.Unlock()
	m.mu.Lock()
	if recoveryErr != nil {
		// Re-attempting a plan this host cannot serve is how an ordinary
		// firewall click took a whole control plane with it (R-054): the same
		// nft call failed at every start and the ledger stayed held until the
		// machine was rebooted. A plan the recovery cannot complete, on a host
		// the recovery proved it left where it found it, is finished as failed
		// with its reason written durably, and the host becomes mutable again.
		// Everything else - a restoration that could not be proved, a ruleset
		// that may be half applied - still poisons and still keeps the lock.
		// Bu makinenin karsilayamayacagi bir plani tekrar denemek, siradan bir
		// guvenlik duvari tiklamasinin butun kontrol duzlemini goturme
		// bicimiydi (R-054).
		code, message, clean := firewallApplyCleanFailureText(
			recoveryOutcome, recoveryErr, true,
		)
		if clean {
			log.Printf(
				"Committed firewall plan failed cleanly during startup recovery: %v",
				recoveryErr,
			)
			runtime.firewallApplyCommittedPhase = ""
			if finishErr := m.finishRuntimeTerminalLocked(
				runtime, false, firewallApplyFailedPhase, code, message,
			); finishErr != nil {
				return true, m.poisonLocked(fmt.Errorf(
					"persist recovered firewall failure: %w", finishErr,
				))
			}
			return true, nil
		}
		return true, m.poisonLocked(fmt.Errorf(
			"recover committed firewall plan: %w",
			recoveryErr,
		))
	}
	publishedPhase, phaseErr := formatFirewallApplyCommitPhase(
		firewallApplyCommitPublished,
		runtime.job.RequestID,
		runtime.job.PackageName,
	)
	if phaseErr != nil {
		return true, m.poisonLocked(phaseErr)
	}
	runtime.firewallApplyCommittedPhase = publishedPhase
	if finishErr := m.finishRuntimeTerminalLocked(
		runtime,
		true,
		publishedPhase,
		"",
		"",
	); finishErr != nil {
		return true, m.poisonLocked(fmt.Errorf(
			"persist recovered firewall success: %w",
			finishErr,
		))
	}
	return true, nil
}
