package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/alicelik/celikpanel/internal/hostplatform"
	"github.com/alicelik/celikpanel/internal/transport"
)

var errBINDSignedUpdatePreflightDeferred = errors.New("DNS transition compatibility is deferred to the existing locked post-install reconciliation")

// This deliberately has no recovery, write, reload or root-hardening operation.
// The updater runs this target-version proof while the old coordinators are
// still available. The post-install preparation remains a separate locked step.
// Bu kontrol kurtarma, yazma, yeniden yükleme veya izin değiştirme yapamaz.
// Yeni sürümün uyumluluğu eski panel çalışırken okunur; kurulum sonrası hazırlık
// ayrı ve kilitli bir adım olarak kalır.
type bindSignedUpdatePreflightOps struct {
	checkIdle        func() error
	detectProfile    func() (hostplatform.Profile, error)
	readJournal      func() (dnsEngineSwitchJournal, bool, error)
	readInstall      func() (dnsEngineInstallOwnershipReceipt, bool, error)
	readState        func() (dnsEngineStateReceipt, bool, error)
	readOwnership    func() (dnsEngineStateReceipt, bool, error)
	packageInstalled func(context.Context, hostplatform.Profile, string) (bool, error)
	parentExists     func() (bool, error)
	verifyExisting   func(context.Context, dnsEngineStateReceipt) error
}

func checkBINDSignedUpdateCompatibleUnderExternalLock(
	ctx context.Context, stateDir, lockPath string, preLedger bool,
) error {
	return checkBINDSignedUpdateCompatibleWithOps(ctx, bindSignedUpdatePreflightOps{
		checkIdle: func() error {
			if preLedger {
				return checkPreLedgerServiceMutationIdleUnderExternalLock(stateDir, lockPath)
			}
			return checkServiceMutationIdleUnderExternalLock(stateDir, lockPath)
		},
		detectProfile: verifiedHostProfileForAnyFamily,
		readJournal:   readDNSEngineSwitchJournal,
		readInstall: func() (dnsEngineInstallOwnershipReceipt, bool, error) {
			return readDNSEngineInstallOwnership(transport.DNSEngineBIND)
		},
		readState: readDNSEngineState,
		readOwnership: func() (dnsEngineStateReceipt, bool, error) {
			return readDNSEngineOwnership(transport.DNSEngineBIND)
		},
		packageInstalled: exactBINDPackageInstalledForSignedUpdate,
		parentExists: func() (bool, error) {
			_, err := os.Lstat(aptBINDCacheParentPath)
			if errors.Is(err, os.ErrNotExist) {
				return false, nil
			}
			return err == nil, err
		},
		verifyExisting: verifyExistingManagedBINDGenerationForPreflight,
	})
}

func checkBINDSignedUpdateCompatibleWithOps(ctx context.Context, ops bindSignedUpdatePreflightOps) error {
	if ctx == nil || ops.checkIdle == nil || ops.detectProfile == nil ||
		ops.readJournal == nil || ops.readInstall == nil || ops.readState == nil ||
		ops.readOwnership == nil || ops.packageInstalled == nil ||
		ops.parentExists == nil || ops.verifyExisting == nil {
		return errors.New("invalid read-only BIND update compatibility check")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ops.checkIdle(); err != nil {
		return fmt.Errorf("BIND update preflight requires the external mutation lock and idle state; finish the current server operation and review the update again: %w", err)
	}
	if journal, exists, err := ops.readJournal(); err != nil {
		return fmt.Errorf("cannot verify the recorded DNS transition before updating; review DNS operation recovery first: %w", err)
	} else if exists {
		if err := validateDNSEngineSwitchJournal(journal); err != nil {
			return err
		}
		// Existing signed updates reconcile supported retained journals. This
		// read-only probe covers settled BIND state; it must not forbid that
		// upgrade path or pretend to have proved the transitional outcome.
		return errBINDSignedUpdatePreflightDeferred
	}
	profile, err := ops.detectProfile()
	if err != nil {
		return fmt.Errorf("verify BIND update host capabilities: %w", err)
	}
	// The post-install root migration is APT-specific. Other package families
	// remain subject to the lock, idle-state and unresolved-transition checks.
	if profile.PackageManager != hostplatform.PackageManagerAPT {
		return nil
	}
	install, installExists, err := ops.readInstall()
	if err != nil {
		return fmt.Errorf("inspect BIND install ownership before updating: %w", err)
	}
	if installExists {
		if err := validateDNSEngineInstallOwnership(install); err != nil {
			return err
		}
		return errBINDSignedUpdatePreflightDeferred
	}
	state, stateExists, err := ops.readState()
	if err != nil {
		return fmt.Errorf("inspect current DNS engine state before updating: %w", err)
	}
	if stateExists {
		if err := validateDNSEngineState(state); err != nil {
			return err
		}
	}
	ownership, ownershipExists, err := ops.readOwnership()
	if err != nil {
		return fmt.Errorf("inspect BIND acquisition ownership before updating: %w", err)
	}
	if ownershipExists {
		if err := validateDNSEngineState(ownership); err != nil {
			return err
		}
		if ownership.Engine != transport.DNSEngineBIND {
			return errors.New("BIND acquisition ownership names another engine")
		}
	}
	managedState := ownership
	managed := ownershipExists
	if stateExists && state.Engine == transport.DNSEngineBIND {
		managedState, managed = state, true
		if ownershipExists && ownership != state &&
			!bindPublicationPreservesEngineOwnership(ownership, state) {
			return errors.New("BIND state and acquisition ownership conflict; the administrator must reconcile DNS infrastructure ownership before reviewing the update again")
		}
	}
	if !managed {
		return nil
	}
	if !supportedAPTBindLegacyRootProfile(profile) {
		return errors.New("managed BIND lacks verified APT and systemd capabilities for this update")
	}
	installed, err := ops.packageInstalled(ctx, profile, "bind9")
	if err != nil {
		return fmt.Errorf("verify managed bind9 package before updating: %w", err)
	}
	if !installed {
		return errors.New("managed BIND ownership exists but bind9 is absent; reconcile DNS infrastructure before reviewing the update again")
	}
	parentExists, err := ops.parentExists()
	if err != nil {
		return fmt.Errorf("inspect managed BIND cache parent before updating: %w", err)
	}
	if !parentExists {
		return errors.New("managed BIND cache parent is absent; reconcile DNS infrastructure before reviewing the update again")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Always prove the actual current generation and runtime configuration,
	// including when acquisition and publication receipts are byte-identical.
	if err := ops.verifyExisting(ctx, managedState); err != nil {
		return fmt.Errorf("current BIND generation or root cannot be verified without changing it; reconcile DNS infrastructure before reviewing the update again: %w", err)
	}
	return ctx.Err()
}
