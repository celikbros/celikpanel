//go:build linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"time"

	"github.com/alicelik/celikpanel/internal/mailrenewalintent"
)

func recoverIndependentPendingMailRenewal(expected mailHostRenewal) error {
	err := recoverIndependentSelectedMailRenewal(expected, "")
	if !errors.Is(err, errMailRenewalCompletionUnverified) {
		return err
	}
	mailRenewalExecution.Lock()
	defer mailRenewalExecution.Unlock()
	if mailRenewalExecution.retained != nil {
		return errMailRenewalRecoveryRequired
	}
	m, err := recoverUnselectedMailRenewalAt(expected, serviceMutationStateDirectory(), serviceMutationLockFile(), managedMailHostTLSDir, buildCommit, func(domain string) ([]byte, error) {
		_, _, leaf, _, err := readMailHostCertificateSource(domain)
		return leaf, err
	}, preflightMailHostCertificateReload, nil)
	if m != nil {
		mailRenewalExecution.retained = m
	}
	return err
}

func unselectedMailRenewalJobMatches(job *ServiceMutationJob, before mailrenewalintent.Before) bool {
	if job == nil || job.Status != serviceMutationStatusRunning || job.Kind != "mail_host_certificate" || job.RequestID != before.RequestID || job.OwnerID != before.OwnerID || job.Target != before.PreviousReceipt.Domain || job.PackageName != before.Qualifier || job.Attempt < 1 || job.ErrorCode != "" || job.ErrorMessage != "" {
		return false
	}
	if job.Phase == "leased" {
		return true
	}
	intent, err := formatMailHostCertificateCommitPhase(mailHostCertificateCommitIntent, job.RequestID, job.Target, job.PackageName)
	return err == nil && job.Phase == intent
}

// Only terminalizes an interrupted, provably unselected attempt. It does not
// select, reload, remove stages, renew a lease or reset Attempt. The existing
// bounded same-request admission governs the next execution. Unknown evidence,
// live workers, missing before-images and changed owner state retain the job.
func recoverUnselectedMailRenewalAt(expected mailHostRenewal, stateDir, lockPath, tlsDir, build string, readSource func(string) ([]byte, error), preflight func(context.Context, string) error, writeFault func(string) error) (retained *serviceMutationManager, result error) {
	if readSource == nil || preflight == nil {
		return nil, errMailRenewalRecoveryRequired
	}
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
	m := &serviceMutationManager{ledgerPath: filepath.Join(stateDir, "service-mutations.json"), lockPath: lockPath, now: func() time.Time { return time.Now().UTC() }, leaseDuration: serviceMutationLeaseDuration, overallDuration: serviceMutationOverallLimit, releaseTransactionPresent: productionReleaseTransactionPresent, writeFault: writeFault}
	m.mu.Lock()
	defer m.mu.Unlock()
	err = panelCertWithPublishLock(func() error {
		gate := func() error {
			blocked, e := m.releaseTransactionPresent()
			if e != nil || blocked {
				return errors.Join(errMailRenewalRecoveryRequired, e)
			}
			return nil
		}
		observe := func() (*ServiceMutationJob, error) {
			if e := gate(); e != nil {
				return nil, e
			}
			if e := m.reloadLedgerUnderHostLockLocked(); e != nil {
				return nil, e
			}
			job := m.ledger.Jobs[m.ledger.ActiveRequestID]
			if job == nil {
				return nil, errMailRenewalRecoveryRequired
			}
			name, e := mailrenewalintent.FileName(job.RequestID)
			if e != nil {
				return nil, e
			}
			before, found, e := mailrenewalintent.Read(filepath.Join(stateDir, name), uint32(serviceMutationRequiredOwnerGID))
			if e != nil || !found {
				return nil, errors.Join(errMailRenewalRecoveryRequired, e)
			}
			if before.Pending != expected || !unselectedMailRenewalJobMatches(job, before) {
				return nil, errMailRenewalRecoveryRequired
			}
			scope := &ServiceMutationBeginRequest{RequestID: job.RequestID, OwnerID: job.OwnerID, Kind: job.Kind, Target: job.Target, PackageName: job.PackageName}
			current, e := observeMailRenewalBeforeAt(stateDir, tlsDir, scope, build, readSource, gate)
			if e != nil {
				return nil, e
			}
			if current != before {
				return nil, errMailRenewalRecoveryRequired
			}
			m.mailRenewalScope = scope
			if e = m.observeMailRenewalEvidenceLocked(); e != nil {
				return nil, e
			}
			if e = mailRenewalWorkerGone(job, serviceMutationProcessStartIdentity); e != nil {
				return nil, e
			}
			return job, nil
		}
		job, e := observe()
		if e != nil {
			return e
		}
		original, _ := json.Marshal(job)
		ctx, cancel := context.WithTimeout(context.Background(), mailHostCertificateRecoveryTimeout)
		defer cancel()
		if e = preflight(ctx, job.Target); e != nil {
			return e
		}
		job, e = observe()
		if e != nil {
			return e
		}
		current, _ := json.Marshal(job)
		if !bytes.Equal(current, original) {
			return errMailRenewalRecoveryRequired
		}
		runtime := &serviceMutationRuntime{job: job, lock: lock, ctx: ctx, cancel: cancel}
		m.active = runtime
		transferred = true
		retained = m
		// No native mutation preceded this single terminal ledger transition. A
		// publication uncertainty retains the manager/lock through shared poisoning.
		if e = m.finishRuntimeTerminalLocked(runtime, false, "interrupted", "mail_renewal_interrupted_before_selection", "The interrupted renewal left the previously selected certificate unchanged. The pending source and staged evidence are preserved; the same request may retry within its remaining execution budget."); e != nil {
			return m.poisonLocked(e)
		}
		return nil
	})
	return retained, err
}
