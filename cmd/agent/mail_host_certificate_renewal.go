package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"path/filepath"
	"strings"
	"time"

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

func deployPendingMailHostCertificate() error {
	raw, found, err := readSecureServiceMutationLedger(mailHostRenewalPendingPath(), 512)
	if err != nil || !found {
		return err
	}
	pending, err := decodeMailHostRenewal(raw)
	if err != nil {
		return err
	}
	lineage := pending.Lineage

	manager, err := agentServiceMutationManager()
	if err != nil {
		return err
	}
	domain, currentLeaf, err := currentMailHostCertificateIdentity()
	if err != nil {
		return err
	}
	if mailHostCertLineageName(domain) != lineage {
		return clearMailHostCertificateRenewal(pending)
	}
	_, _, leaf, _, err := readMailHostCertificateSource(domain)
	if err != nil {
		return err
	}
	if panelCertificateLeafSHA256(leaf) != pending.LeafSHA256 {
		return errors.New("queued renewal source generation changed")
	}
	if panelCertificateLeafSHA256(leaf) == currentLeaf {
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
	ownerID := hex.EncodeToString(digest[16:])
	resume := false
	if previous := manager.status(requestID); previous != nil {
		resume = previous.Status == serviceMutationStatusFailed
	}
	job, err := manager.begin(&ServiceMutationBeginRequest{RequestID: requestID, OwnerID: ownerID, Kind: "mail_host_certificate", Target: domain, PackageName: commitment.Qualifier, Resume: resume})
	if err != nil {
		return err
	}
	if job.Status == serviceMutationStatusSucceeded {
		return clearMailHostCertificateRenewal(pending)
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

type mailHostRenewal struct {
	Lineage    string `json:"lineage"`
	LeafSHA256 string `json:"leaf_sha256"`
}

func decodeMailHostRenewal(raw []byte) (mailHostRenewal, error) {
	var value mailHostRenewal
	if len(raw) > 512 || json.Unmarshal(raw, &value) != nil {
		return value, errors.New("invalid host renewal queue")
	}
	canonical, _ := json.Marshal(value)
	if !bytes.Equal(raw, canonical) {
		return value, errors.New("noncanonical host renewal queue")
	}
	suffix := strings.TrimPrefix(value.Lineage, "celikpanel-mail-")
	if suffix == value.Lineage || len(suffix) != 24 {
		return value, errors.New("invalid host renewal lineage")
	}
	decoded, err := hex.DecodeString(suffix)
	if err != nil || hex.EncodeToString(decoded) != suffix {
		return value, errors.New("invalid host renewal lineage")
	}
	if err := validatePanelCertificateLeafSHA256(value.LeafSHA256); err != nil {
		return value, err
	}
	return value, nil
}
