//go:build linux

package main

import (
	"errors"
	"github.com/alicelik/celikpanel/internal/mailrenewalruntime"
	"path/filepath"
)

// Volatile runtime may be absent after boot; durable enrollment may not. Prove
// existing owner identity, accepted plan, selection and canonical evidence before
// publishing a new directory. Never normalize an existing owner-modified path.
func prepareIndependentMailRuntime() error {
	gid, ok := lookupGroupID("celikpanel")
	if !ok || gid < 0 || uint32(gid) != serviceMutationRequiredOwnerGID {
		return errors.New("retained mail service group identity is unavailable; the owner must restore the enrolled identity")
	}
	raw, found, err := readSecureServiceMutationLedger(filepath.Join(serviceMutationStateDirectory(), "service-mutations.json"), serviceMutationLedgerMaxSize)
	if err != nil || !found {
		return errors.Join(errMailRenewalCompletionUnverified, err)
	}
	if _, err = decodeServiceMutationLedger(raw); err != nil {
		return err
	}
	plan, err := loadMailHostCertificatePlan()
	if err != nil {
		return err
	}
	domain, _, err := currentMailHostCertificateIdentity()
	if err != nil {
		return err
	}
	if domain != plan.Myhostname {
		return errors.New("accepted mail identity and selected certificate differ")
	}
	blocked, err := productionReleaseTransactionPresent()
	if err != nil || blocked {
		return errors.Join(errServiceMutationHostBusy, err)
	}
	return mailrenewalruntime.Ensure(uint32(gid))
}
