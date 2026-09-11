package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

const managedMailHostTLSDir = mutationpayload.MailHostCertificateDirectory
const mailHostCertificateCapability = transport.AgentCapabilityMailHostCertificateV1

func mailHostCertLineageName(domain string) string {
	digest := sha256.Sum256([]byte(domain))
	return "celikpanel-mail-" + hex.EncodeToString(digest[:12])
}

// This endpoint issues only the explicitly reviewed host identity. It cannot
// name customer certificate paths or replace customer SNI entries.
func (a *Agent) IssueMailHostCertificateV1(req *transport.IssueMailHostCertificateRequest, resp *transport.IssueMailHostCertificateResponse) error {
	*resp = transport.IssueMailHostCertificateResponse{}
	if req == nil {
		resp.Error = "mail host certificate request required"
		return nil
	}
	commitment, err := mutationpayload.CanonicalMailHostCertificate(req.Domain, req.Email, req.ExpectedBuildCommit)
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	if err = requireExpectedBuildCommit(commitment.ExpectedBuildCommit, "issue mail host certificate"); err != nil {
		resp.Error = err.Error()
		return nil
	}
	ctx, finish, err := a.requiredServiceMutationStep(req.ServiceMutationBinding, newServiceMutationStepClaim(serviceMutationStepIssueMailHostCertificate, commitment.Domain, commitment.Qualifier, "issue"))
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	defer finish()
	if !acquireSiteCertbot() {
		resp.Error = "another certificate operation is active"
		return nil
	}
	defer releaseSiteCertbot()
	if _, err = panelCertLookPath("certbot"); err != nil {
		resp.Error = "install certbot before issuing the host certificate"
		return nil
	}
	if err = preflightMailHostCertificate(ctx, commitment.Domain); err != nil {
		resp.Error = err.Error()
		return nil
	}
	args, err := a.panelCertificateChallengeArgs(ctx, commitment.Domain)
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	args = append([]string{"certonly"}, args...)
	args = append(args, "--preferred-challenges", "http", "--cert-name", mailHostCertLineageName(commitment.Domain), "-d", commitment.Domain, "--agree-tos", "--non-interactive", "--force-renewal", "--email", commitment.Email)
	if err = prepareManagedCertbotSourceOwnership(commitment.Domain, mailHostCertLineageName(commitment.Domain)); err != nil {
		resp.Error = err.Error()
		return nil
	}
	out, err := panelCertRunMutationCommand(ctx, panelCertIssueTimeout, "certbot", args...)
	if err != nil {
		resp.Error = panelCertCommandError("issue mail host certificate", out, err).Error()
		return nil
	}
	if err = panelCertEnsureRenewal(ctx); err != nil {
		resp.Error = err.Error()
		return nil
	}
	if err = writeMailHostCertificateDeployHook(); err != nil {
		resp.Error = err.Error()
		return nil
	}
	expires, err := publishMailHostCertificateSource(ctx, commitment.Domain, req.MutationRequestID, commitment.Qualifier, "")
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	resp.Issued = true
	resp.ExpiresAt = expires
	return nil
}

func publishMailHostCertificateSource(ctx context.Context, domain, requestID, qualifier, expectedLeaf string) (expires time.Time, err error) {
	err = panelCertWithPublishLock(func() error {
		if err := preflightMailHostCertificate(ctx, domain); err != nil {
			return err
		}
		cert, key, leaf, notAfter, readErr := readMailHostCertificateSource(domain)
		if readErr != nil {
			return readErr
		}
		if expectedLeaf != "" && expectedLeaf != panelCertificateLeafSHA256(leaf) {
			return errors.New("renewal source generation changed")
		}
		receipt, receiptErr := newMailHostCertificateReceipt(requestID, qualifier, domain, leaf)
		if receiptErr != nil {
			return receiptErr
		}
		stage, stageErr := stageMailHostCertificateMaterial(domain, managedMailHostTLSDir, cert, key, receipt)
		if stageErr != nil {
			return stageErr
		}
		defer stage.close()
		_, commitErr := commitStandaloneMailHostCertificateStep(ctx, stage.publish, func(convergence context.Context) error {
			return applyMailHostCertificateSelection(convergence, domain)
		})
		if commitErr != nil {
			return commitErr
		}
		expires = notAfter
		return nil
	})
	return
}

func preflightMailHostCertificate(ctx context.Context, domain string) error {
	journal, err := loadMailHostCertificatePlan()
	if err != nil {
		return err
	}
	if journal.Myhostname != domain {
		return errors.New("mail host identity differs from the reviewed mail configuration")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func loadMailHostCertificatePlan() (*mailTLSSyncJournal, error) {
	// Only publication of a successfully verified mail TLS configuration writes
	// this snapshot. The separate intent journal can describe a failed change.
	journal, found, err := readMailTLSSyncJournal(filepath.Join(serviceMutationStateDirectory(), mailTLSCommittedFileName))
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, errors.New("a committed mail TLS configuration is required")
	}
	if err = validateMailTLSSyncJournal(journal); err != nil {
		return nil, err
	}
	return journal, nil
}

func applyMailHostCertificateSelection(ctx context.Context, domain string) error {
	journal, err := loadMailHostCertificatePlan()
	if err != nil {
		return err
	}
	if journal.Myhostname != domain {
		return errors.New("committed mail identity changed before host certificate activation")
	}
	// The durable owner keeps the host lock while its supervised worker records
	// every privileged subprocess and survives panel disconnection.
	run := func(name string, args ...string) ([]byte, error) {
		return runMailHostCertificateCommand(ctx, name, args...)
	}
	mailMutex.Lock()
	defer mailMutex.Unlock()
	request := &SecureMailTLSRequest{Myhostname: domain, SNI: journal.SNI}
	var response SecureMailTLSResponse
	outcome, err := reconcileMailTLSHost(request, &response, run)
	if err != nil {
		return err
	}
	if outcome != mailTLSHostConverged || !response.Configured || response.Error != "" {
		return fmt.Errorf("mail host TLS activation did not converge: %s", response.Error)
	}
	return verifyMailTLSSyncPlan(journal, run)
}
