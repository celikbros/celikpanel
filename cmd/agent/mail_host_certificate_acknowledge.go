package main

import (
	"errors"
	"github.com/alicelik/celikpanel/internal/mailhostartifact"
	"path/filepath"
)

var errMailRenewalCompletionUnverified = errors.New("mail renewal completion is not verified; the server owner must review the recorded operation and selected certificate; the pending renewal is preserved for supported recovery")

// Removing pending work is a lifecycle transition too. Selection alone cannot
// acknowledge an interrupted activation. Observe fresh evidence under the same
// locks as writers, without starting general recovery or creating a new job.
func acknowledgeMailHostRenewal(expected mailHostRenewal, readSelected func() (mailHostCertificateReceipt, error), remove func(mailHostRenewal) error) (result error) {
	if _, err := mailhostartifact.CanonicalPending(expected); err != nil {
		return err
	}
	lock, err := acquireServiceMutationHostAndPublicationLocks(serviceMutationLockFile())
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, lock.Close()) }()
	blocked, err := productionReleaseTransactionPresent()
	if err != nil || blocked {
		return errors.Join(errMailRenewalCompletionUnverified, err)
	}
	return panelCertWithPublishLock(func() error {
		receipt, err := readSelected()
		if err != nil {
			return errors.Join(errMailRenewalCompletionUnverified, err)
		}
		if err = mailhostartifact.ValidateReceipt(receipt); err != nil {
			return errors.Join(errMailRenewalCompletionUnverified, err)
		}
		path := filepath.Join(serviceMutationStateDirectory(), "service-mutations.json")
		raw, found, err := readSecureServiceMutationLedger(path, serviceMutationLedgerMaxSize)
		if err != nil || !found {
			return errors.Join(errMailRenewalCompletionUnverified, err)
		}
		ledger, err := decodeServiceMutationLedger(raw)
		if err != nil {
			return errors.Join(errMailRenewalCompletionUnverified, err)
		}
		job := ledger.Jobs[receipt.RequestID]
		phase, err := formatMailHostCertificateCommitPhase(mailHostCertificateCommitPublished, receipt.RequestID, receipt.Domain, receipt.Qualifier)
		if err != nil || job == nil || job.Status != serviceMutationStatusSucceeded ||
			job.Kind != "mail_host_certificate" || job.Target != receipt.Domain || job.PackageName != receipt.Qualifier || job.Phase != phase ||
			mailHostCertLineageName(receipt.Domain) != expected.Lineage || receipt.LeafSHA256 != expected.LeafSHA256 {
			return errMailRenewalCompletionUnverified
		}
		// Use only the read-only scoped observer, never the recovering constructor.
		scope := &ServiceMutationBeginRequest{RequestID: job.RequestID, OwnerID: job.OwnerID, Kind: job.Kind, Target: job.Target, PackageName: job.PackageName}
		observer := &serviceMutationManager{ledgerPath: path, ledger: ledger, mailRenewalScope: scope}
		if err := observer.observeMailRenewalAdmissionLocked(); err != nil {
			return err
		}
		return remove(expected)
	})
}
