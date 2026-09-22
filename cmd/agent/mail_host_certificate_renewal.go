package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"path/filepath"
	"sync"
	"time"

	"github.com/alicelik/celikpanel/internal/mailhostartifact"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
)

func mailHostRenewalPendingPath() string {
	return filepath.Join(serviceMutationStateDirectory(), "mail-host-certificate-renewal.pending")
}

func runMailHostCertificateRenewalWorker() {
	for {
		if err := deployPendingMailHostCertificate(); err != nil && !errors.Is(err, errServiceMutationBusy) && !errors.Is(err, errServiceMutationHostBusy) {
			log.Printf("Mail host certificate renewal remains pending: %v", err)
		}
		time.Sleep(time.Minute)
	}
}

// Keep a poisoned manager (and any still-active worker lease) reachable. A new
// polling iteration must not discard fail-closed state and create another owner.
var mailRenewalExecution struct {
	sync.Mutex
	retained *serviceMutationManager
}

func deployPendingMailHostCertificate() error {
	return deployPendingMailHostCertificateWithRetry("")
}

func deployPendingMailHostCertificateWithRetry(ownerRequest string) error {
	mailRenewalExecution.Lock()
	defer mailRenewalExecution.Unlock()
	if held := mailRenewalExecution.retained; held != nil {
		held.mu.Lock()
		err := held.healthErrorLocked()
		active := held.active != nil
		held.mu.Unlock()
		if err != nil {
			return err
		}
		if active {
			return errServiceMutationBusy
		}
		mailRenewalExecution.retained = nil
	}

	raw, found, err := readSecureServiceMutationLedger(mailHostRenewalPendingPath(), 512)
	if err != nil || !found {
		if err == nil && ownerRequest != "" {
			return errors.New("explicit failed renewal retry has no pending operation")
		}
		return err
	}
	pending, err := decodeMailHostRenewal(raw)
	if err != nil {
		return err
	}
	lineage := pending.Lineage

	domain, currentLeaf, err := currentMailHostCertificateIdentity()
	if err != nil {
		return err
	}
	if mailHostCertLineageName(domain) != lineage {
		return errors.New("queued mail renewal belongs to another selected hostname; the server owner must review the retained queue and accepted mail identity")
	}
	_, _, leaf, _, err := readMailHostCertificateSource(domain)
	if err != nil {
		return err
	}
	if panelCertificateLeafSHA256(leaf) != pending.LeafSHA256 {
		return errors.New("queued renewal source generation changed")
	}
	if ownerRequest == "" && panelCertificateLeafSHA256(leaf) == currentLeaf {
		return clearMailHostCertificateRenewal(pending)
	}
	commitment, err := mutationpayload.CanonicalMailHostCertificate(domain, "renewal@celikpanel.invalid", buildCommit)
	if err != nil {
		return err
	}
	// The unattended operation is bound to this precise source generation;
	// another Certbot generation gets another durable request identity.
	digest := sha256.Sum256(append([]byte("mail-host-renewal/v1/"+domain+"/"+buildCommit+"/"), leaf...))
	requestID := hex.EncodeToString(digest[:16])
	if ownerRequest != "" && (!validMutationIdentity(ownerRequest) || ownerRequest != requestID || panelCertificateLeafSHA256(leaf) == currentLeaf) {
		return errors.New("explicit failed renewal retry does not match an unpublished pending operation")
	}
	ownerID := hex.EncodeToString(digest[16:])
	request := &ServiceMutationBeginRequest{RequestID: requestID, OwnerID: ownerID, Kind: "mail_host_certificate", Target: domain, PackageName: commitment.Qualifier}
	manager, err := newMailRenewalMutationManager("", "", request)
	if manager != nil {
		mailRenewalExecution.retained = manager
	}
	if err != nil {
		return err
	}
	manager.mailRenewalFailedOwnerRequest = ownerRequest
	resume := false
	if previous := manager.status(requestID); previous != nil {
		resume = previous.Status == serviceMutationStatusFailed
	}
	request.Resume = resume
	job, err := manager.begin(request)
	if err != nil {
		return err
	}
	if job.Status == serviceMutationStatusSucceeded {
		// A matching current leaf already returned above. Historical execution
		// success cannot erase a queue after the owner changes current selection.
		// Keep both facts and require review rather than rewriting owner state.
		return errors.New("mail host renewal previously completed, but the selected certificate differs; the server owner must review the current mail certificate before retrying")
	}
	ctx, finish, err := manager.acquireStep(ServiceMutationBinding{MutationRequestID: requestID, MutationOwnerID: ownerID}, newServiceMutationStepClaim(serviceMutationStepIssueMailHostCertificate, domain, commitment.Qualifier, "issue"))
	if err != nil {
		return err
	}
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				_, _ = manager.heartbeat(&ServiceMutationHeartbeatRequest{RequestID: requestID, OwnerID: ownerID})
			}
		}
	}()
	_, _, freshLeaf, _, err := readMailHostCertificateSource(domain)
	if err == nil && panelCertificateLeafSHA256(freshLeaf) != panelCertificateLeafSHA256(leaf) {
		err = errors.New("renewed source changed after mutation admission")
	}
	if err == nil {
		_, err = publishMailHostCertificateSource(ctx, domain, requestID, commitment.Qualifier, panelCertificateLeafSHA256(leaf))
	}
	finish()
	if err != nil {
		_, finishErr := manager.finish(&ServiceMutationFinishRequest{RequestID: requestID, OwnerID: ownerID, Success: false, FailureCode: "mail_host_renewal_failed", Message: "Mail host certificate renewal remains pending."})
		return errors.Join(err, finishErr)
	}
	return clearMailHostCertificateRenewal(pending)
}

type mailHostRenewal = mailhostartifact.Pending

func decodeMailHostRenewal(raw []byte) (mailHostRenewal, error) {
	return mailhostartifact.DecodePending(raw)
}
