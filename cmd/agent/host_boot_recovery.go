package main

import (
	"context"
	"errors"
	"log"
	"path/filepath"
	"time"

	"github.com/alicelik/celikpanel/internal/hostplatform"
)

// R-048. A machine that lost power comes back with an interrupted mutation in
// its ledger, and the agent's startup recovery is what finishes or undoes it.
// On a real boot the agent starts while systemd is still `starting`, so the
// host-profile probe refused, the recovery never ran, and - because it was a
// one-shot - never ran again. The interrupted request kept the ledger, and
// every host mutation on the machine was refused until a person restarted the
// agent by hand. Restarting once always decided it correctly, so the decision
// was never the defect; when it was made was.
//
// Two rules follow, and neither of them changes what the recovery decides.
//
// First: a host that is still booting is "not yet", not "no". The probe is
// retried on a bounded schedule before anything is decided. Only a failure the
// detector marks as durable - an unsupported or broken host - fails the plan,
// and it fails it cleanly, the way a committed mail TLS plan that can never
// converge now does.
//
// Second: nothing may end a boot holding a lease no one is left to release. If
// the window closes with the host still unreadable, the ledger is released with
// a durable reason instead of being left leased by a process that no longer
// exists.
//
// The waiting happens on its own goroutine. The agent's socket opens on time,
// the panel can still ask, and while the wait is running the answer it gets is
// the true one: a mutation is unresolved and the host is busy.
//
// R-048. Elektrigi kesilen bir makine, defterinde yarim kalmis bir mutasyonla
// geri gelir; agent'in baslangic kurtarmasi onu bitiren ya da geri alan seydir.
// Gercek bir aciliste agent, systemd henuz `starting` iken baslar; makine
// profili yoklamasi reddeder, kurtarma hic calismaz ve - tek atislik oldugu
// icin - bir daha da calismaz. Karar dogruydu; ne zaman verildigi yanlisti.
//
// Beklemek kendi goroutine'inde olur: agent'in soketi zamaninda acilir, panel
// sorabilir ve bekleme surerken aldigi cevap dogrudur.

var (
	// hostBootRecoveryWindow is how long a startup recovery may wait for a host
	// that is still starting. It is generous on purpose: the cost of waiting is
	// that the interrupted mutation - which is already unresolved - stays
	// unresolved a little longer, while the cost of giving up too early is
	// abandoning a plan the machine could still have finished.
	// hostBootRecoveryWindow, hala acilan bir makine icin beklenebilecek suredir.
	hostBootRecoveryWindow = 5 * time.Minute
	// hostBootRecoveryInterval is how often the probe is repeated inside that
	// window. Each probe runs one bounded systemctl read.
	hostBootRecoveryInterval = 3 * time.Second
)

type hostRecoveryReadiness int

const (
	// hostRecoveryDecideNow: the recovery may decide, right now, exactly as it
	// did before this fix. It is also the answer for a probe failure this code
	// did not classify - an unclassified failure is not a verdict, so it must
	// not start a wait and must not end a plan; it goes down the path it always
	// went down and is judged there.
	hostRecoveryDecideNow hostRecoveryReadiness = iota
	// hostRecoveryNotYet: the host has not finished starting. Ask again.
	hostRecoveryNotYet
	// hostRecoveryNever: this host cannot be inspected at all, and no amount of
	// asking again will change that.
	hostRecoveryNever
)

// hostRecoveryProbe is the single place the startup recovery asks whether the
// host can be read yet. It is a variable so a test can drive all three answers
// without a systemd.
// hostRecoveryProbe, kurtarmanin makineyi okuyup okuyamayacagini sordugu tek yer.
var hostRecoveryProbe = probeHostRecoveryReadiness

func probeHostRecoveryReadiness() (hostRecoveryReadiness, error) {
	_, err := verifiedHostProfileForAnyFamily()
	switch {
	case err == nil:
		return hostRecoveryDecideNow, nil
	case hostplatform.StillStarting(err):
		return hostRecoveryNotYet, err
	case hostplatform.Unsupported(err):
		return hostRecoveryNever, err
	default:
		return hostRecoveryDecideNow, err
	}
}

const (
	// hostRecoveryReleasedUnsupportedCode: the host itself is the obstacle, and
	// waiting was pointless.
	hostRecoveryReleasedUnsupportedCode = "host_unsupported_after_restart"
	// hostRecoveryReleasedWindowCode: the host never finished starting inside
	// the window the recovery is allowed to wait.
	hostRecoveryReleasedWindowCode = "host_not_ready_within_recovery_window"
)

// What releasing the ledger does NOT mean, said out loud. The ledger's lease is
// what is given up - the claim one interrupted request had on every host
// mutation - and nothing else. The mutation's own durable journal stays exactly
// where it is, so the host is never presented as clean, and the failed job
// keeps its reason so the interruption is never forgotten.
// Defteri birakmanin ne ANLAMA GELMEDIGI, acikca soylenir.
const hostRecoveryResidueSentence = "Nothing the interrupted mutation changed was undone and nothing about it was discarded: " +
	"its durable journal is still on this host, and the next change of the same kind reconciles that journal before it does anything else."

const hostRecoveryUnsupportedMessage = "The agent restarted after an interrupted mutation and this host could not be inspected at all, " +
	"so the mutation could not be decided. The ledger was released so the rest of the host stays usable. " +
	hostRecoveryResidueSentence

const hostRecoveryWindowMessage = "The agent restarted after an interrupted mutation and the host had not finished starting when the recovery window closed, " +
	"so the mutation could not be decided. The ledger was released so the rest of the host stays usable. " +
	hostRecoveryResidueSentence

// releasedUndecidedHostRecoveryCode reports whether a ledger error code is one
// of the two this file writes when it releases an undecided lease.
func releasedUndecidedHostRecoveryCode(code string) bool {
	return code == hostRecoveryReleasedUnsupportedCode ||
		code == hostRecoveryReleasedWindowCode
}

// startupRecoveryNeedsTheHostLocked reports whether this reconciliation is
// going to read the host at all. A ledger with nothing active and no durable
// DNS engine journal decides nothing and touches nothing, so it must never wait
// for a boot; and an active job whose worker is still alive is settled from the
// process table alone. Everything else ends up running host commands.
// startupRecoveryNeedsTheHostLocked, bu uzlastirmanin makineyi okuyup
// okumayacagini bildirir.
func (m *serviceMutationManager) startupRecoveryNeedsTheHostLocked() bool {
	if requestID := m.ledger.ActiveRequestID; requestID != "" {
		job := m.ledger.Jobs[requestID]
		if job == nil {
			return false
		}
		return !serviceMutationWorkerMatches(job.WorkerPID, job.WorkerStarted)
	}
	_, exists, err := readDNSEngineSwitchJournalAt(
		filepath.Join(filepath.Dir(m.ledgerPath), dnsEngineSwitchJournalFile),
	)
	return err == nil && exists
}

// deferRecoveryUntilTheHostCanBeReadLocked runs before the startup recovery
// dispatches to any per-kind reconciliation. The caller owns m.mu and the host
// mutation lock, and when this reports that it handled the reconciliation the
// lock has been released or deliberately retained by the poison path, exactly
// as every other branch of reconcilePersistedActive does.
// deferRecoveryUntilTheHostCanBeReadLocked, baslangic kurtarmasi herhangi bir
// tur-ozel uzlastirmaya dagitmadan once calisir.
func (m *serviceMutationManager) deferRecoveryUntilTheHostCanBeReadLocked(
	lock *serviceMutationFileLock,
) (bool, error) {
	if !m.startupRecoveryNeedsTheHostLocked() {
		return false, nil
	}
	readiness, cause := hostRecoveryProbe()
	switch readiness {
	case hostRecoveryNotYet:
		log.Printf(
			"Startup recovery is waiting for this host to finish starting before it decides the interrupted mutation: %v",
			cause,
		)
		m.armHostBootRecoveryLocked()
		return true, lock.Close()
	case hostRecoveryNever:
		log.Printf(
			"Startup recovery cannot inspect this host, so the interrupted mutation is being released rather than held: %v",
			cause,
		)
		return true, m.releaseUndecidedHostMutationLeaseLocked(
			lock, hostRecoveryReleasedUnsupportedCode, hostRecoveryUnsupportedMessage,
		)
	default:
		return false, nil
	}
}

// armHostBootRecoveryLocked starts the one waiter this process may have. The
// caller owns m.mu.
func (m *serviceMutationManager) armHostBootRecoveryLocked() {
	if m.hostBootWait != nil {
		return
	}
	done := make(chan struct{})
	m.hostBootWait = done
	go m.awaitHostBootThenDecide(time.Now().Add(hostBootRecoveryWindow), done)
}

// awaitHostBootThenDecide is the bounded schedule. It re-enters the ordinary
// startup reconciliation the moment the host can be read, so the decision is
// made by exactly the code that would have made it had the machine been ready
// when the agent started - nothing about the decision is duplicated here.
// awaitHostBootThenDecide sinirli zamanlamadir; makine okunabilir olur olmaz
// olagan baslangic uzlastirmasina geri girer.
func (m *serviceMutationManager) awaitHostBootThenDecide(
	deadline time.Time,
	done chan struct{},
) {
	defer close(done)
	defer func() {
		m.mu.Lock()
		m.hostBootWait = nil
		m.mu.Unlock()
	}()
	for {
		readiness, cause := hostRecoveryProbe()
		switch readiness {
		case hostRecoveryDecideNow:
			err := m.reconcilePersistedActive()
			if err != nil {
				log.Printf(
					"Startup recovery decided the interrupted mutation after the host finished starting, and failed: %v",
					err,
				)
				return
			}
			if !m.stillHoldingAnUndecidedLease() {
				log.Printf(
					"Startup recovery decided the interrupted mutation after the host finished starting.",
				)
				return
			}
			// The host became readable and then stopped being readable again
			// before the reconciliation reached it. Keep waiting: the window,
			// not this race, is what ends the wait.
			// Makine okunabilir olup sonra tekrar okunamaz oldu; beklemeye devam.
		case hostRecoveryNever:
			m.releaseUndecidedHostMutationLease(
				hostRecoveryReleasedUnsupportedCode,
				hostRecoveryUnsupportedMessage,
				cause,
			)
			return
		}
		if !time.Now().Before(deadline) {
			m.releaseUndecidedHostMutationLease(
				hostRecoveryReleasedWindowCode,
				hostRecoveryWindowMessage,
				cause,
			)
			return
		}
		time.Sleep(hostBootRecoveryInterval)
	}
}

func (m *serviceMutationManager) stillHoldingAnUndecidedLease() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.poisoned == nil && m.active == nil && m.ledger.ActiveRequestID != ""
}

// releaseUndecidedHostMutationLease is the unlocked entry the waiter uses. It
// takes the same host and publication locks a worker takes, so the release is
// published under the same exclusion as every other ledger write.
// releaseUndecidedHostMutationLease, bekleyicinin kullandigi kilitsiz giristir.
func (m *serviceMutationManager) releaseUndecidedHostMutationLease(
	code, message string,
	cause error,
) {
	lock, err := acquireServiceMutationHostAndPublicationLocks(m.lockPath)
	if err != nil {
		log.Printf(
			"DEGRADED service-mutations: the interrupted mutation could not be released because the host lock is held: %v",
			err,
		)
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.healthErrorLocked(); err != nil {
		_ = lock.Close()
		return
	}
	if m.active != nil {
		// This process started a mutation of its own while the wait ran. It
		// owns the ledger now and this waiter has nothing to release.
		_ = lock.Close()
		return
	}
	if err := m.reloadLedgerUnderHostLockLocked(); err != nil {
		_ = lock.Close()
		log.Printf(
			"DEGRADED service-mutations: the interrupted mutation could not be released because the ledger could not be re-read: %v",
			err,
		)
		return
	}
	if err := m.releaseUndecidedHostMutationLeaseLocked(lock, code, message); err != nil {
		log.Printf(
			"DEGRADED service-mutations: releasing the interrupted mutation failed: %v", err,
		)
		return
	}
	log.Printf(
		"The interrupted mutation could not be decided (%s) and its ledger lease was released with that reason recorded; "+
			"the host accepts mutations again and the mutation's own journal was left where it is: %v",
		code, cause,
	)
}

// releaseUndecidedHostMutationLeaseLocked gives up one interrupted request's
// claim on the ledger, and nothing else. The caller owns m.mu and the host
// mutation lock.
//
// How the lease is established to be dead, which is the whole licence for doing
// this: the recorded worker is proved gone by pid *and* process start identity,
// so a recycled pid cannot pass; and this code runs while holding the same host
// mutation flock every worker must hold to touch the host, which a live worker
// could not have let go of. Either proof alone would be weaker than the pair.
// If the recorded worker is in fact alive, nothing is released - a live lease is
// never taken away from the process that owns it.
//
// What is written is a terminal failure on the job itself: its status, its
// phase, and the reason, in the ledger, on disk, so the interruption survives
// every later boot. The mutation's own durable journal is not touched, so the
// host is not presented as clean; only the claim on the ledger is given up.
//
// releaseUndecidedHostMutationLeaseLocked, yarim kalmis tek bir istegin defter
// uzerindeki hakkini birakir; baska hicbir seyi degil. Kiranin olu oldugu iki
// ayri kanitla kurulur: kaydedilmis worker'in pid ve surec baslangic kimligiyle
// yok oldugu, ve bu kodun her worker'in tutmak zorunda oldugu host kilidini
// tutuyor olmasi.
func (m *serviceMutationManager) releaseUndecidedHostMutationLeaseLocked(
	lock *serviceMutationFileLock,
	code, message string,
) error {
	requestID := m.ledger.ActiveRequestID
	if requestID == "" {
		return lock.Close()
	}
	job := m.ledger.Jobs[requestID]
	if job == nil {
		closeErr := lock.Close()
		return errors.Join(
			errors.New("service mutation ledger lost its active job"), closeErr,
		)
	}
	if serviceMutationWorkerMatches(job.WorkerPID, job.WorkerStarted) {
		// Somebody is still running this. A held lease with a live owner is not
		// the state this exists to clear.
		return lock.Close()
	}
	writeErr := m.finishPersistedOrphanLocked(job, code, message)
	if m.poisoned != nil {
		m.poisonLock = lock
		return writeErr
	}
	closeErr := lock.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

// recoverReleasedUndecidedDNSEngineSwitchLocked is the other half of a release,
// and it runs on the next boot.
//
// An idle ledger beside a DNS engine journal that is not committed is normally
// an unexplained inconsistency, and the agent poisons on it. After a release it
// is explained, and its explanation is durable: the ledger carries a terminal
// failure for exactly that journal's mutation, with one of this file's two
// reasons. That state is not a surprise and must not wedge the host a second
// time - but neither may it be ignored, because the journal describes real work
// left half-done. So it is reconciled here, on its own terms, exactly as the
// next DNS mutation would reconcile it: finalize what committed, roll back what
// did not. A plan that can still be completed is never abandoned; it is only
// completed later.
//
// If it still cannot be reconciled, nothing is written and nothing is poisoned.
// The reason is already recorded, the host stays mutable, and the journal stays
// where it is for the next attempt.
//
// The caller owns m.mu and the host mutation lock.
//
// recoverReleasedUndecidedDNSEngineSwitchLocked, bir birakmanin diger yarisidir
// ve sonraki aciliste calisir. Tamamlanabilecek bir plan asla terk edilmez;
// yalnizca daha sonra tamamlanir.
func (m *serviceMutationManager) recoverReleasedUndecidedDNSEngineSwitchLocked(
	lock *serviceMutationFileLock,
) (bool, error) {
	journal, exists, err := readDNSEngineSwitchJournalAt(
		filepath.Join(filepath.Dir(m.ledgerPath), dnsEngineSwitchJournalFile),
	)
	if err != nil || !exists {
		return false, nil
	}
	job := m.ledger.Jobs[journal.MutationRequestID]
	if job == nil || job.Kind != "dns_engine_switch" ||
		job.RequestID != journal.MutationRequestID ||
		job.OwnerID != journal.MutationOwnerID ||
		job.Status != serviceMutationStatusFailed ||
		!releasedUndecidedHostRecoveryCode(job.ErrorCode) {
		return false, nil
	}
	// The host was already proved readable by the probe that let this
	// reconciliation run at all, so no second probe is taken here.
	// Bu uzlastirmanin calismasina izin veren yoklama makineyi zaten okunabilir
	// kanitladi; burada ikinci bir yoklama alinmaz.
	binding := switchJournalBinding(journal)
	ctx, cancel := context.WithTimeout(
		context.Background(), dnsEngineSwitchRecoveryLimit,
	)
	m.mu.Unlock()
	outcome, recoverErr := agentDNSEngineBackend.RecoverSwitch(
		ctx, journal.TargetEngine, journal.ManifestQualifier, binding,
	)
	cancel()
	m.mu.Lock()
	if recoverErr != nil {
		log.Printf(
			"A DNS engine transaction released after an undecidable boot could not be reconciled yet; "+
				"its reason is already recorded and the host stays usable: %v",
			recoverErr,
		)
		return true, lock.Close()
	}
	if outcome == dnsEngineSwitchRecoveryCommitted ||
		outcome == dnsEngineSwitchRecoveryFinalized {
		finalizeCtx, finalizeCancel := context.WithTimeout(
			context.Background(), dnsEngineSwitchRecoveryLimit,
		)
		m.mu.Unlock()
		finalizeErr := agentDNSEngineBackend.FinalizeSwitch(
			finalizeCtx, journal.TargetEngine, journal.ManifestQualifier, binding,
		)
		finalizeCancel()
		m.mu.Lock()
		if finalizeErr != nil {
			log.Printf(
				"A DNS engine transaction released after an undecidable boot reached its target but could not be finalized yet: %v",
				finalizeErr,
			)
			return true, lock.Close()
		}
	}
	// No ledger receipt is written. That mutation already has a terminal one -
	// the failure this file recorded when it released the lease - and rewriting
	// a finished job from a later boot would replace the answer the operator was
	// given with a different one. What is reconciled here is the host, not the
	// verdict: the machine stops carrying half-finished work, and the log says so.
	// Hicbir defter makbuzu yazilmaz: o mutasyonun zaten nihai bir makbuzu var.
	log.Printf(
		"The DNS engine transaction released after an undecidable boot was reconciled on the host (request %s, outcome %v).",
		journal.MutationRequestID, outcome,
	)
	return true, lock.Close()
}
