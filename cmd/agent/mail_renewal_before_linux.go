//go:build linux

package main

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/alicelik/celikpanel/internal/mailhoststore"
	"github.com/alicelik/celikpanel/internal/mailrenewalintent"
	"golang.org/x/sys/unix"
)

// Called only for new admission/retry, under fresh host and ledger exclusion.
// An active interrupted job is refused before this point: missing historical
// before-images are never synthesized after mutation.
func (m *serviceMutationManager) persistMailRenewalBeforeAdmissionLocked(request *ServiceMutationBeginRequest) error {
	if !mailRenewalRequestMatches(m.mailRenewalScope, request) {
		return errMailRenewalRecoveryRequired
	}
	return panelCertWithPublishLock(func() error {
		return persistMailRenewalBeforeAt(filepath.Dir(m.ledgerPath), managedMailHostTLSDir, request, buildCommit, func(domain string) ([]byte, error) {
			_, _, leaf, _, err := readMailHostCertificateSource(domain)
			return leaf, err
		}, func() error {
			blocked, err := m.releaseTransactionPresent()
			if err != nil || blocked {
				return errors.Join(errMailRenewalRecoveryRequired, err)
			}
			return nil
		})
	})
}

// Caller holds certificate publication exclusion in addition to host/ledger
// locks. Observe again around each durable boundary; a saved receipt alone is
// never authority to replace a subsequently changed owner selection.
func persistMailRenewalBeforeAt(stateDir, tlsDir string, request *ServiceMutationBeginRequest, build string, readSource func(string) ([]byte, error), revalidate func() error) error {
	if request == nil || request.Kind != "mail_host_certificate" || readSource == nil || revalidate == nil {
		return errMailRenewalRecoveryRequired
	}
	observe := func() (mailrenewalintent.Before, error) {
		return observeMailRenewalBeforeAt(stateDir, tlsDir, request, build, readSource, revalidate)
	}
	before, err := observe()
	if err != nil {
		return err
	}
	name, err := mailrenewalintent.FileName(before.RequestID)
	if err != nil {
		return err
	}
	return mailrenewalintent.Write(filepath.Join(stateDir, name), before, uint32(serviceMutationRequiredOwnerGID), func() error {
		fresh, err := observe()
		if err != nil {
			return err
		}
		if fresh != before {
			return errMailRenewalRecoveryRequired
		}
		return nil
	})
}

// Read-only: recovery must compare this observation with an already persisted
// before-image. This function cannot create or repair that evidence.
func observeMailRenewalBeforeAt(stateDir, tlsDir string, request *ServiceMutationBeginRequest, build string, readSource func(string) ([]byte, error), revalidate func() error) (mailrenewalintent.Before, error) {
	if request == nil || request.Kind != "mail_host_certificate" || readSource == nil || revalidate == nil {
		return mailrenewalintent.Before{}, errMailRenewalRecoveryRequired
	}
	if err := revalidate(); err != nil {
		return mailrenewalintent.Before{}, err
	}
	raw, found, err := readSecureServiceMutationLedger(filepath.Join(stateDir, "mail-host-certificate-renewal.pending"), 512)
	if err != nil || !found {
		return mailrenewalintent.Before{}, errors.Join(errMailRenewalRecoveryRequired, err)
	}
	pending, err := decodeMailHostRenewal(raw)
	if err != nil {
		return mailrenewalintent.Before{}, err
	}
	fd, err := openTrustedPanelTLSDirectoryOwned(tlsDir, 0)
	if err != nil {
		return mailrenewalintent.Before{}, err
	}
	defer unix.Close(fd)
	selected, err := mailhoststore.ObserveCurrentAt(fd, validateMailHostCertificatePair)
	if err != nil {
		return mailrenewalintent.Before{}, err
	}
	leaf, err := readSource(request.Target)
	if err != nil {
		return mailrenewalintent.Before{}, err
	}
	before, err := mailrenewalintent.New(build, leaf, selected.Receipt, selected.IdentitySHA256)
	if err != nil {
		return mailrenewalintent.Before{}, err
	}
	if request.Target != before.PreviousReceipt.Domain || request.RequestID != before.RequestID || request.OwnerID != before.OwnerID || request.PackageName != before.Qualifier || before.Pending != pending {
		return mailrenewalintent.Before{}, errMailRenewalRecoveryRequired
	}
	return before, nil
}

// The commit callback owns manager.mu plus host/ledger/certificate publication
// locks. Admission evidence is rechecked immediately before selecting bytes:
// staging or a slow native preflight must not hide a later owner replacement.
func verifyMailRenewalBeforePublicationLocked(ctx context.Context) error {
	if ctx == nil {
		return errMailRenewalRecoveryRequired
	}
	tracker, _ := ctx.Value(serviceMutationExecutionTrackerKey{}).(*serviceMutationExecutionTracker)
	if tracker == nil || tracker.manager == nil || tracker.runtime == nil {
		return errMailRenewalRecoveryRequired
	}
	m := tracker.manager
	runtime := tracker.runtime
	if m.active != runtime || runtime.job == nil || m.mailRenewalScope == nil || runtime.job.Status != serviceMutationStatusRunning {
		return errMailRenewalRecoveryRequired
	}
	job := runtime.job
	request := &ServiceMutationBeginRequest{RequestID: job.RequestID, OwnerID: job.OwnerID, Kind: job.Kind, Target: job.Target, PackageName: job.PackageName}
	if !mailRenewalRequestMatches(m.mailRenewalScope, request) {
		return errMailRenewalRecoveryRequired
	}
	return verifyMailRenewalBeforeAt(filepath.Dir(m.ledgerPath), managedMailHostTLSDir, request, buildCommit, func(domain string) ([]byte, error) {
		_, _, leaf, _, err := readMailHostCertificateSource(domain)
		return leaf, err
	}, func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		blocked, err := m.releaseTransactionPresent()
		if err != nil || blocked {
			return errors.Join(errMailRenewalRecoveryRequired, err)
		}
		return nil
	})
}

func verifyMailRenewalBeforeAt(stateDir, tlsDir string, request *ServiceMutationBeginRequest, build string, readSource func(string) ([]byte, error), revalidate func() error) error {
	if request == nil {
		return errMailRenewalRecoveryRequired
	}
	name, err := mailrenewalintent.FileName(request.RequestID)
	if err != nil {
		return err
	}
	before, found, err := mailrenewalintent.Read(filepath.Join(stateDir, name), uint32(serviceMutationRequiredOwnerGID))
	if err != nil || !found {
		return errors.Join(errMailRenewalRecoveryRequired, err)
	}
	current, err := observeMailRenewalBeforeAt(stateDir, tlsDir, request, build, readSource, revalidate)
	if err != nil {
		return err
	}
	if current != before {
		return errMailRenewalRecoveryRequired
	}
	return nil
}
