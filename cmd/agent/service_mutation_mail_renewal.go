package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
)

var errMailRenewalRecoveryRequired = errors.New("mail renewal paused: retained service operation evidence needs review; the server owner must resolve the recorded operation through its recovery path before renewal retries; existing services and the pending certificate are preserved")

// This constructor never invokes general Agent recovery. Even the same renewal
// left active by another process is retained, not guessed failed or replayed.
// The ordinary Agent still owns supported interrupted-operation recovery.
func newMailRenewalMutationManager(stateDir, lockPath string, request *ServiceMutationBeginRequest) (*serviceMutationManager, error) {
	if request == nil || !validMutationIdentity(request.RequestID) ||
		!validMutationIdentity(request.OwnerID) || request.Kind != "mail_host_certificate" ||
		!serviceMutationCanonicalFQDN(request.Target) ||
		!mutationpayload.ValidMailHostCertificateQualifier(request.PackageName) {
		return nil, errors.New("invalid scoped mail renewal identity")
	}
	scope := *request // Caller changes cannot broaden the retained authority.
	return newServiceMutationManagerWithScope(stateDir, lockPath, nil, &scope)
}

func mailRenewalRequestMatches(scope, request *ServiceMutationBeginRequest) bool {
	return scope != nil && request != nil && scope.RequestID == request.RequestID &&
		scope.OwnerID == request.OwnerID && scope.Kind == request.Kind &&
		scope.Target == request.Target && scope.PackageName == request.PackageName
}

// Caller holds m.mu and the common host/publication locks, and has just reloaded
// the canonical ledger. Every call is observation only: no journal cleanup,
// host recovery dispatch, background recovery waiter, or terminal publication.
func (m *serviceMutationManager) observeMailRenewalAdmissionLocked() error {
	if m.ledger.ActiveRequestID != "" {
		return errors.Join(errMailRenewalRecoveryRequired, errServiceMutationBusy)
	}
	for _, job := range m.ledger.Jobs {
		if job.Status == serviceMutationStatusPending {
			return errMailRenewalRecoveryRequired
		}
	}
	if previous := m.ledger.Jobs[m.mailRenewalScope.RequestID]; previous != nil &&
		(!serviceMutationIdentityMatches(previous, m.mailRenewalScope) || previous.OwnerID != m.mailRenewalScope.OwnerID) {
		return errMailRenewalRecoveryRequired
	}
	dir := filepath.Dir(m.ledgerPath)
	if err := observeMailRenewalStages(dir); err != nil {
		return err
	}
	// These readers validate metadata and payload without converging a service.
	// Historical journals may survive terminal publication. They are not proof
	// that native settings are still accepted; the mail preflight checks that.
	if _, _, err := readFirewallApplyJournal(firewallApplyJournalPath(m)); err != nil {
		return errors.Join(errMailRenewalRecoveryRequired, fmt.Errorf("observe firewall journal: %w", err))
	}
	if _, _, err := readMailTLSSyncJournal(mailTLSSyncJournalPath(m)); err != nil {
		return errors.Join(errMailRenewalRecoveryRequired, fmt.Errorf("observe mail TLS journal: %w", err))
	}
	if _, _, err := readDNSClusterConfigJournal(dnsClusterConfigJournalPath(m)); err != nil {
		return errors.Join(errMailRenewalRecoveryRequired, fmt.Errorf("observe DNS cluster journal: %w", err))
	}
	// A DNS switch may release the active pointer while retaining its journal.
	// It has a dedicated recovery owner; renewal must not dispatch that owner.
	if _, found, err := readSecureServiceMutationLedger(filepath.Join(dir, dnsEngineSwitchJournalFile), dnsEngineSwitchJournalLimit); err != nil || found {
		return errors.Join(errMailRenewalRecoveryRequired, err)
	}
	return nil
}

func observeMailRenewalStages(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return errors.Join(errMailRenewalRecoveryRequired, err)
	}
	for _, entry := range entries {
		name := entry.Name()
		for _, prefix := range []string{".service-mutations-", ".firewall-apply-journal-", ".mail-tls-sync-journal-", ".dns-cluster-config-journal-"} {
			if strings.HasPrefix(name, prefix) {
				return errMailRenewalRecoveryRequired
			}
		}
	}
	return nil
}
