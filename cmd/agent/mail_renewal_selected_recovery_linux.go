//go:build linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/alicelik/celikpanel/internal/mailhostartifact"
)

func selectedMailRenewalRecoveryJob(ledger serviceMutationLedger, pending mailHostRenewal, receipt mailHostCertificateReceipt) (*ServiceMutationJob, error) {
	if _, err := mailhostartifact.CanonicalPending(pending); err != nil {
		return nil, err
	}
	if err := mailhostartifact.ValidateReceipt(receipt); err != nil {
		return nil, err
	}
	if pending.Lineage != mailHostCertLineageName(receipt.Domain) || pending.LeafSHA256 != receipt.LeafSHA256 {
		return nil, errMailRenewalCompletionUnverified
	}
	job := ledger.Jobs[receipt.RequestID]
	if job == nil || job.Kind != "mail_host_certificate" || job.Target != receipt.Domain || job.PackageName != receipt.Qualifier {
		return nil, errMailRenewalRecoveryRequired
	}
	phase, err := formatMailHostCertificateCommitPhase(mailHostCertificateCommitPublished, job.RequestID, job.Target, job.PackageName)
	if err != nil {
		return nil, err
	}
	if job.Status == serviceMutationStatusSucceeded && job.Phase == phase && ledger.ActiveRequestID == "" {
		return nil, nil
	}
	if ledger.ActiveRequestID != job.RequestID || !activeDirectMailHostCertificateJob(job) {
		return nil, errMailRenewalRecoveryRequired
	}
	state, id, domain, qualifier, err := parseMailHostCertificateCommitPhase(job.Phase)
	if err != nil || state != mailHostCertificateCommitIntent || id != job.RequestID || domain != job.Target || qualifier != job.PackageName {
		return nil, errMailRenewalRecoveryRequired
	}
	return job, nil
}

// An unreadable process is unknown, not dead. Host exclusion is independently
// required; this observation cannot acquire a lease or authorize killing a PID.
func mailRenewalWorkerGone(job *ServiceMutationJob, read func(int) (string, error)) error {
	if job.WorkerPID == 0 && job.WorkerStarted == "" {
		return nil
	}
	if job.WorkerPID <= 0 || job.WorkerStarted == "" {
		return errMailRenewalRecoveryRequired
	}
	actual, err := read(job.WorkerPID)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errors.Join(errMailRenewalRecoveryRequired, err)
	}
	if actual == job.WorkerStarted || actual == "" {
		return errMailRenewalRecoveryRequired
	}
	return nil // The observed PID belongs to a different process incarnation.
}

func recoverIndependentSelectedMailRenewal(expected mailHostRenewal) error {
	mailRenewalExecution.Lock()
	defer mailRenewalExecution.Unlock()
	if mailRenewalExecution.retained != nil {
		return errMailRenewalRecoveryRequired
	}
	manager, err := recoverSelectedMailRenewalAt(expected, serviceMutationStateDirectory(), serviceMutationLockFile(), readSelectedMailHostReceipt, preflightMailHostCertificateReload)
	if manager != nil {
		mailRenewalExecution.retained = manager
	}
	return err
}

func recoverSelectedMailRenewalAt(expected mailHostRenewal, stateDir, lockPath string, readSelected func() (mailHostCertificateReceipt, error), preflight func(context.Context, string) error) (retained *serviceMutationManager, result error) {
	lock, err := acquireServiceMutationHostAndPublicationLocks(lockPath)
	if err != nil {
		return nil, err
	}
	transferred := false
	defer func() {
		if !transferred {
			result = errors.Join(result, lock.Close())
		}
	}()
	m := &serviceMutationManager{ledgerPath: filepath.Join(stateDir, "service-mutations.json"), lockPath: lockPath, now: func() time.Time { return time.Now().UTC() }, leaseDuration: serviceMutationLeaseDuration, overallDuration: serviceMutationOverallLimit, releaseTransactionPresent: productionReleaseTransactionPresent}
	m.mu.Lock()
	defer m.mu.Unlock()
	observe := func() (*ServiceMutationJob, error) {
		blocked, e := m.releaseTransactionPresent()
		if e != nil || blocked {
			return nil, errors.Join(errMailRenewalRecoveryRequired, e)
		}
		if e = m.reloadLedgerUnderHostLockLocked(); e != nil {
			return nil, e
		}
		var job *ServiceMutationJob
		e = panelCertWithPublishLock(func() error {
			raw, found, e := readSecureServiceMutationLedger(filepath.Join(stateDir, "mail-host-certificate-renewal.pending"), 512)
			if e != nil || !found {
				return errors.Join(errMailRenewalRecoveryRequired, e)
			}
			pending, e := decodeMailHostRenewal(raw)
			if e != nil || pending != expected {
				return errors.Join(errMailRenewalRecoveryRequired, e)
			}
			receipt, e := readSelected()
			if e != nil {
				return e
			}
			job, e = selectedMailRenewalRecoveryJob(m.ledger, pending, receipt)
			return e
		})
		return job, e
	}
	job, err := observe()
	if err != nil {
		return nil, err
	}
	if job == nil {
		return nil, nil
	}
	scope := &ServiceMutationBeginRequest{RequestID: job.RequestID, OwnerID: job.OwnerID, Kind: job.Kind, Target: job.Target, PackageName: job.PackageName}
	m.mailRenewalScope = scope
	if err = m.observeMailRenewalEvidenceLocked(); err != nil {
		return nil, err
	}
	if err = mailRenewalWorkerGone(job, serviceMutationProcessStartIdentity); err != nil {
		return nil, err
	}
	before, _ := json.Marshal(job)
	ctx, cancel := context.WithTimeout(context.Background(), mailHostCertificateRecoveryTimeout)
	defer cancel()
	// Observation only, before recovery intent is written. Reload commands below
	// use the actual durable tracker and inherited host-lock supervisor.
	if err = preflight(ctx, job.Target); err != nil {
		return nil, err
	}
	job, err = observe()
	if err != nil || job == nil {
		return nil, errors.Join(errMailRenewalRecoveryRequired, err)
	}
	after, _ := json.Marshal(job)
	if !bytes.Equal(before, after) {
		return nil, errMailRenewalRecoveryRequired
	}
	if err = m.observeMailRenewalEvidenceLocked(); err != nil {
		return nil, err
	}
	if err = mailRenewalWorkerGone(job, serviceMutationProcessStartIdentity); err != nil {
		return nil, err
	}
	handled, err := m.recoverPersistedMailHostCertificateLocked(job, lock)
	if !handled {
		return nil, errors.Join(errMailRenewalRecoveryRequired, err)
	}
	transferred = true // Shared recovery either closes the lock or retains poison.
	return m, err
}
