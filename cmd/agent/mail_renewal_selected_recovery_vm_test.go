//go:build linux

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"syscall"
	"testing"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
)

// Guarded native acceptance only: actual durable selection, followed by SIGKILL
// before either mail service reload or terminal publication. A separate helper
// process must reconcile this exact request without ordinary Agent startup.
func TestMailRenewalDisposableVMKillAfterSelection(t *testing.T) {
	requireDisposableMailVM(t)
	if _, err := os.Stat("/opt/celikpanel/bin/agent"); !os.IsNotExist(err) {
		t.Fatal("installed management must be absent")
	}
	domain, oldLeaf, err := currentMailHostCertificateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if domain != "mail.setup.celikpanel.test" {
		t.Fatal("wrong disposable identity")
	}
	cert, key, leaf, _, err := readMailHostCertificateSource(domain)
	if err != nil {
		t.Fatal(err)
	}
	newLeaf := panelCertificateLeafSHA256(leaf)
	if newLeaf == oldLeaf {
		t.Fatal("fixture source must be newer")
	}
	raw, err := os.ReadFile(mailHostRenewalPendingPath())
	if err != nil {
		t.Fatal(err)
	}
	pending, err := decodeMailHostRenewal(raw)
	if err != nil || pending.Lineage != mailHostCertLineageName(domain) || pending.LeafSHA256 != newLeaf {
		t.Fatal("queue must match fixture source")
	}
	c, err := mutationpayload.CanonicalMailHostCertificate(domain, "renewal@celikpanel.invalid", buildCommit)
	if err != nil {
		t.Fatal(err)
	}
	identity := make([]byte, 32)
	if _, err = rand.Read(identity); err != nil {
		t.Fatal(err)
	}
	request := &ServiceMutationBeginRequest{RequestID: hex.EncodeToString(identity[:16]), OwnerID: hex.EncodeToString(identity[16:]), Kind: "mail_host_certificate", Target: domain, PackageName: c.Qualifier}
	manager, err := newMailRenewalMutationManager("", "", request)
	if err != nil {
		t.Fatal(err)
	}
	mailRenewalExecution.retained = manager
	if _, err = manager.begin(request); err != nil {
		t.Fatal(err)
	}
	ctx, finish, err := manager.acquireStep(ServiceMutationBinding{MutationRequestID: request.RequestID, MutationOwnerID: request.OwnerID}, newServiceMutationStepClaim(serviceMutationStepIssueMailHostCertificate, domain, c.Qualifier, "issue"))
	if err != nil {
		t.Fatal(err)
	}
	defer finish()
	if err = preflightMailHostCertificateReload(ctx, domain); err != nil {
		t.Fatal(err)
	}
	receipt, err := newMailHostCertificateReceipt(request.RequestID, c.Qualifier, domain, leaf)
	if err != nil {
		t.Fatal(err)
	}
	err = panelCertWithPublishLock(func() error {
		stage, err := stageMailHostCertificateMaterial(domain, managedMailHostTLSDir, cert, key, receipt)
		if err != nil {
			return err
		}
		defer stage.close()
		_, err = commitStandaloneMailHostCertificateStep(ctx, stage.publish, func(context.Context) error {
			evidence := map[string]string{"request": request.RequestID, "owner": request.OwnerID, "domain": domain, "qualifier": c.Qualifier, "old_leaf": oldLeaf, "new_leaf": newLeaf, "checkpoint": "selected-before-reload"}
			raw, err := json.Marshal(evidence)
			if err != nil {
				return err
			}
			file, err := os.OpenFile("/root/celikpanel-release-recovery-lab/mail-selected-kill.json", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if err != nil {
				return err
			}
			if _, err = file.Write(raw); err != nil {
				file.Close()
				return err
			}
			if err = file.Sync(); err != nil {
				file.Close()
				return err
			}
			if err = file.Close(); err != nil {
				return err
			}
			dir, err := os.Open("/root/celikpanel-release-recovery-lab")
			if err != nil {
				return err
			}
			err = dir.Sync()
			dir.Close()
			if err != nil {
				return err
			}
			if err = syscall.Kill(os.Getpid(), syscall.SIGKILL); err != nil {
				return err
			}
			select {} // SIGKILL must terminate the unit, never return test success.
		})
		return err
	})
	t.Fatalf("selected SIGKILL checkpoint was not reached: %v", err)
}
