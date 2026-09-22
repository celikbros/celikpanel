//go:build linux

package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/alicelik/celikpanel/internal/mailrenewalintent"
)

func unselectedMailVMCheckpoint(t *testing.T, point string, request *ServiceMutationBeginRequest, oldLeaf, newLeaf string) {
	t.Helper()
	trial := os.Getenv("CP_MAIL_UNSELECTED_TRIAL")
	if !validMutationIdentity(trial) {
		t.Fatal("exact trial identity required")
	}
	raw, err := json.Marshal(map[string]string{"trial": trial, "point": point, "request": request.RequestID, "owner": request.OwnerID, "domain": request.Target, "qualifier": request.PackageName, "old_leaf": oldLeaf, "new_leaf": newLeaf, "build": buildCommit})
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(filepath.Join("/root/celikpanel-release-recovery-lab", "mail-unselected-"+trial+"-"+point+".json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err = file.Sync(); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	if err = syscall.Kill(os.Getpid(), syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	t.Fatal("SIGKILL returned")
}

// Uses the real admission writer and native certificate material. The kill is
// before the active ledger, after its lease, or after durable publication intent
// but before selecting the staged generation. A fresh source-bound helper must
// resume; this test cannot invoke a normal Agent or alter native configuration.
func TestMailRenewalDisposableVMKillBeforeSelection(t *testing.T) {
	requireDisposableMailVM(t)
	if raw, err := hex.DecodeString(buildCommit); err != nil || len(raw) != 20 {
		t.Fatal("native trial requires an exact source build identity before admission")
	}
	if _, err := os.Stat("/opt/celikpanel/bin/agent"); !os.IsNotExist(err) {
		t.Fatal("management must be absent")
	}
	point := os.Getenv("CP_MAIL_UNSELECTED_CUT")
	if point != "before_ledger" && point != "leased" && point != "intent" {
		t.Fatal("unsupported cut")
	}
	domain, oldLeaf, err := currentMailHostCertificateIdentity()
	if err != nil || domain != "mail.setup.celikpanel.test" {
		t.Fatal("wrong selected fixture", err)
	}
	cert, key, leaf, _, err := readMailHostCertificateSource(domain)
	if err != nil {
		t.Fatal(err)
	}
	newLeaf := panelCertificateLeafSHA256(leaf)
	if newLeaf == oldLeaf {
		t.Fatal("new fixture source required")
	}
	id, owner, qualifier, err := mailrenewalintent.Identity(domain, buildCommit, leaf)
	if err != nil {
		t.Fatal(err)
	}
	request := &ServiceMutationBeginRequest{RequestID: id, OwnerID: owner, Kind: "mail_host_certificate", Target: domain, PackageName: qualifier}
	m, err := newMailRenewalMutationManager("", "", request)
	if err != nil {
		t.Fatal(err)
	}
	if point == "before_ledger" {
		original := m.mailRenewalBeforeAdmission
		m.mailRenewalBeforeAdmission = func(r *ServiceMutationBeginRequest) error {
			if err := original(r); err != nil {
				return err
			}
			unselectedMailVMCheckpoint(t, point, r, oldLeaf, newLeaf)
			return nil
		}
	}
	if _, err = m.begin(request); err != nil {
		t.Fatal(err)
	}
	if point == "leased" {
		unselectedMailVMCheckpoint(t, point, request, oldLeaf, newLeaf)
	}
	ctx, finish, err := m.acquireStep(ServiceMutationBinding{MutationRequestID: id, MutationOwnerID: owner}, newServiceMutationStepClaim(serviceMutationStepIssueMailHostCertificate, domain, qualifier, "issue"))
	if err != nil {
		t.Fatal(err)
	}
	defer finish()
	receipt, err := newMailHostCertificateReceipt(id, qualifier, domain, leaf)
	if err != nil {
		t.Fatal(err)
	}
	err = panelCertWithPublishLock(func() error {
		if err := preflightMailHostCertificateReload(ctx, domain); err != nil {
			return err
		}
		stage, err := stageMailHostCertificateMaterial(domain, managedMailHostTLSDir, cert, key, receipt)
		if err != nil {
			return err
		}
		defer stage.close()
		_, err = commitStandaloneMailHostCertificateStep(ctx, func() error { unselectedMailVMCheckpoint(t, point, request, oldLeaf, newLeaf); return stage.publish() }, func(context.Context) error { return nil })
		return err
	})
	t.Fatal("cut not reached", err)
}

func TestMailRenewalDisposableVMKillUnselectedTerminal(t *testing.T) {
	requireDisposableMailVM(t)
	if raw, err := hex.DecodeString(buildCommit); err != nil || len(raw) != 20 {
		t.Fatal("native trial requires an exact source build identity before admission")
	}
	if _, err := os.Stat("/opt/celikpanel/bin/agent"); !os.IsNotExist(err) {
		t.Fatal("management must be absent")
	}
	domain, oldLeaf, err := currentMailHostCertificateIdentity()
	if err != nil || domain != "mail.setup.celikpanel.test" {
		t.Fatal("wrong selected fixture", err)
	}
	_, _, leaf, _, err := readMailHostCertificateSource(domain)
	if err != nil {
		t.Fatal(err)
	}
	id, owner, qualifier, err := mailrenewalintent.Identity(domain, buildCommit, leaf)
	if err != nil {
		t.Fatal(err)
	}
	request := &ServiceMutationBeginRequest{RequestID: id, OwnerID: owner, Kind: "mail_host_certificate", Target: domain, PackageName: qualifier}
	raw, found, err := readSecureServiceMutationLedger(mailHostRenewalPendingPath(), 512)
	if err != nil || !found {
		t.Fatal("pending missing", err)
	}
	pending, err := decodeMailHostRenewal(raw)
	if err != nil {
		t.Fatal(err)
	}
	_, err = recoverUnselectedMailRenewalAt(pending, serviceMutationStateDirectory(), serviceMutationLockFile(), managedMailHostTLSDir, buildCommit, func(host string) ([]byte, error) {
		_, _, leaf, _, e := readMailHostCertificateSource(host)
		return leaf, e
	}, preflightMailHostCertificateReload, func(point string) error {
		if point == serviceMutationWriteFaultAfterRename {
			unselectedMailVMCheckpoint(t, "terminal", request, oldLeaf, pending.LeafSHA256)
		}
		return nil
	})
	t.Fatal("terminal cut not reached", err)
}
