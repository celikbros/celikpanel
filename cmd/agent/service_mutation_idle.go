package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alicelik/celikpanel/internal/transport"
)

var errServiceMutationNotIdle = errors.New("service mutation state is not idle")
var errInitialServiceMutationLedgerInvalid = errors.New("initial service mutation ledger is not exact")

type serviceMutationReadinessError struct {
	reason string
	err    error
}

func (e *serviceMutationReadinessError) Error() string { return e.err.Error() }
func (e *serviceMutationReadinessError) Unwrap() error { return e.err }

func markServiceMutationNotIdle(reason string, err error) error {
	return &serviceMutationReadinessError{reason: reason, err: err}
}

func serviceMutationReadinessReason(err error) (string, bool) {
	var readinessErr *serviceMutationReadinessError
	if !errors.As(err, &readinessErr) {
		return "", false
	}
	return readinessErr.reason, true
}

func serviceMutationLockProbeError(err error) error {
	wrapped := fmt.Errorf("%w: %v", errServiceMutationNotIdle, err)
	if errors.Is(err, errServiceMutationHostBusy) {
		return markServiceMutationNotIdle(transport.HostMutationReasonHostLock, wrapped)
	}
	return wrapped
}

func serviceMutationPackageManagerBusyError() error {
	return markServiceMutationNotIdle(
		transport.HostMutationReasonPackageManager,
		fmt.Errorf("%w: the host package manager is active", errServiceMutationNotIdle),
	)
}

func serviceMutationActiveError(format string, args ...any) error {
	return markServiceMutationNotIdle(
		transport.HostMutationReasonAgentMutation,
		fmt.Errorf("%w: "+format, append([]any{errServiceMutationNotIdle}, args...)...),
	)
}

// checkServiceMutationIdle is intentionally read-only. Release tooling calls it
// after stopping the panel, before pairing a panel database snapshot with the
// privileged agent ledger. Missing, unsafe, inconsistent, active or externally
// locked state is rejected rather than repaired here.
// checkServiceMutationIdle bilerek salt okunurdur. Sürüm araçları paneli
// durdurduktan sonra, panel veritabanı anlık görüntüsünü ayrıcalıklı agent
// ledger'ıyla eşleştirmeden önce bunu çağırır. Eksik, güvensiz, tutarsız, etkin
// veya dışarıdan kilitli durum burada onarılmak yerine reddedilir.
func checkServiceMutationIdle(stateDir, lockPath string) error {
	return checkServiceMutationIdlePolicy(stateDir, lockPath, false, true)
}

// checkServiceMutationIdleUnderExternalLock repeats every state and package-manager
// proof while a trusted caller already holds the common mutation flock.
// checkServiceMutationIdleUnderExternalLock, güvenilir çağıran ortak mutation
// flock kilidini zaten tutarken tüm durum ve paket yöneticisi kanıtlarını yineler.
func checkServiceMutationIdleUnderExternalLock(stateDir, lockPath string) error {
	return checkServiceMutationIdlePolicy(stateDir, lockPath, false, false)
}

// checkPreLedgerServiceMutationIdle permits a missing state directory or a secure state directory without a ledger;
// any existing ledger and runtime lock remain subject to the normal strict checks.
// checkPreLedgerServiceMutationIdle, eksik durum dizinine veya ledger içermeyen güvenli bir durum dizinine izin verir;
// var olan ledger ve çalışma zamanı kilidi normal sıkı kontrollere tabi kalır.
func checkPreLedgerServiceMutationIdle(stateDir, lockPath string) error {
	return checkServiceMutationIdlePolicy(stateDir, lockPath, true, true)
}

// checkPreLedgerServiceMutationIdleUnderExternalLock repeats the strict
// pre-ledger proof while a trusted caller already holds the common flock.
// checkPreLedgerServiceMutationIdleUnderExternalLock, güvenilir çağıran ortak flock
// kilidini zaten tutarken sıkı ledger-öncesi kanıtı yineler.
func checkPreLedgerServiceMutationIdleUnderExternalLock(stateDir, lockPath string) error {
	return checkServiceMutationIdlePolicy(stateDir, lockPath, true, false)
}

// checkInitialServiceMutationLedger is a read-only post-initialization proof.
// It accepts only the exact canonical empty bytes published by the one-shot
// initializer, and only while the shared mutation lock and package manager are idle.
// checkInitialServiceMutationLedger salt okunur bir başlatma-sonrası kanıtıdır.
// Yalnız tek-seferlik başlatıcının yayımladığı tam kanonik boş baytları ve ortak
// mutation kilidiyle paket yöneticisi boşta olduğunda kabul eder.
func checkInitialServiceMutationLedger(stateDir, lockPath string) error {
	return checkInitialServiceMutationLedgerPolicy(stateDir, lockPath, true)
}

// checkInitialServiceMutationLedgerUnderExternalLock repeats the exact canonical
// initializer proof while a trusted caller already holds the common flock.
// checkInitialServiceMutationLedgerUnderExternalLock, güvenilir çağıran ortak flock
// kilidini tutarken tam kanonik initializer kanıtını yineler.
func checkInitialServiceMutationLedgerUnderExternalLock(stateDir, lockPath string) error {
	return checkInitialServiceMutationLedgerPolicy(stateDir, lockPath, false)
}

func checkInitialServiceMutationLedgerPolicy(stateDir, lockPath string, probeMutationLock bool) error {
	if strings.TrimSpace(stateDir) == "" {
		stateDir = serviceMutationStateDirectory()
	}
	if strings.TrimSpace(lockPath) == "" {
		lockPath = serviceMutationLockFile()
	}
	stateDir = filepath.Clean(stateDir)
	lockPath = filepath.Clean(lockPath)
	if !filepath.IsAbs(stateDir) || !filepath.IsAbs(lockPath) {
		return fmt.Errorf("%w: service mutation paths must be absolute", errInitialServiceMutationLedgerInvalid)
	}
	if !probeMutationLock {
		if err := verifyInheritedServiceMutationFileLock(lockPath); err != nil {
			return fmt.Errorf("%w: inherited host lock proof failed: %v", errInitialServiceMutationLedgerInvalid, err)
		}
	}

	info, err := os.Lstat(stateDir)
	if err != nil {
		return fmt.Errorf("%w: inspect service mutation state directory: %v", errInitialServiceMutationLedgerInvalid, err)
	}
	if err := secureServiceMutationStateDirectoryStat(stateDir, info); err != nil {
		return fmt.Errorf("%w: %v", errInitialServiceMutationLedgerInvalid, err)
	}

	raw, exists, err := readSecureServiceMutationLedger(
		filepath.Join(stateDir, serviceMutationLedgerFileName),
		serviceMutationLedgerMaxSize,
	)
	if err != nil {
		return fmt.Errorf("%w: %v", errInitialServiceMutationLedgerInvalid, err)
	}
	if !exists {
		return fmt.Errorf("%w: service mutation ledger is not initialized", errInitialServiceMutationLedgerInvalid)
	}
	expected, err := canonicalInitialServiceMutationLedger()
	if err != nil {
		return fmt.Errorf("%w: encode canonical empty ledger: %v", errInitialServiceMutationLedgerInvalid, err)
	}
	if !bytes.Equal(raw, expected) {
		return fmt.Errorf("%w: ledger does not match the canonical empty initializer output", errInitialServiceMutationLedgerInvalid)
	}
	if _, err := inspectInitialServiceMutationStateEntries(stateDir, true, expected); err != nil {
		return fmt.Errorf("%w: %v", errInitialServiceMutationLedgerInvalid, err)
	}

	if probeMutationLock {
		if err := probeServiceMutationFileLockIdle(lockPath); err != nil {
			return fmt.Errorf("%w: %v", errInitialServiceMutationLedgerInvalid, err)
		}
	}
	busy, err := packageManagerMutationBusy()
	if err != nil {
		return fmt.Errorf("%w: inspect package manager activity: %v", errInitialServiceMutationLedgerInvalid, err)
	}
	if busy {
		return fmt.Errorf("%w: the host package manager is active", errInitialServiceMutationLedgerInvalid)
	}
	return nil
}

func checkServiceMutationIdlePolicy(stateDir, lockPath string, allowMissingState, probeMutationLock bool) error {
	if strings.TrimSpace(stateDir) == "" {
		stateDir = serviceMutationStateDirectory()
	}
	if strings.TrimSpace(lockPath) == "" {
		lockPath = serviceMutationLockFile()
	}
	stateDir = filepath.Clean(stateDir)
	lockPath = filepath.Clean(lockPath)
	if !filepath.IsAbs(stateDir) || !filepath.IsAbs(lockPath) {
		return fmt.Errorf("%w: service mutation paths must be absolute", errServiceMutationNotIdle)
	}
	if !probeMutationLock {
		if err := verifyInheritedServiceMutationFileLock(lockPath); err != nil {
			return fmt.Errorf("%w: inherited host lock proof failed: %v", errServiceMutationNotIdle, err)
		}
	}
	if allowMissingState {
		if _, err := os.Lstat(stateDir); os.IsNotExist(err) {
			if probeMutationLock {
				if err := probePreLedgerServiceMutationFileLockIdle(lockPath); err != nil {
					return serviceMutationLockProbeError(err)
				}
			}
			busy, err := packageManagerMutationBusy()
			if err != nil {
				return fmt.Errorf("%w: inspect package manager activity: %v", errServiceMutationNotIdle, err)
			}
			if busy {
				return serviceMutationPackageManagerBusyError()
			}
			return nil
		} else if err != nil {
			return fmt.Errorf("%w: inspect service mutation state directory: %v", errServiceMutationNotIdle, err)
		}
	}

	info, err := os.Lstat(stateDir)
	if err != nil {
		return fmt.Errorf("%w: inspect service mutation state directory: %v", errServiceMutationNotIdle, err)
	}
	stateDirectoryErr := secureServiceMutationStateDirectoryStat(stateDir, info)
	if allowMissingState {
		stateDirectoryErr = securePreLedgerServiceMutationStateDirectoryStat(stateDir, info)
	}
	if stateDirectoryErr != nil {
		return fmt.Errorf("%w: %v", errServiceMutationNotIdle, stateDirectoryErr)
	}

	raw, exists, err := readSecureServiceMutationLedger(
		filepath.Join(stateDir, serviceMutationLedgerFileName),
		serviceMutationLedgerMaxSize,
	)
	if err != nil {
		return fmt.Errorf("%w: %v", errServiceMutationNotIdle, err)
	}
	if !exists && !allowMissingState {
		return fmt.Errorf("%w: service mutation ledger is not initialized", errServiceMutationNotIdle)
	}
	if allowMissingState {
		expected, err := canonicalInitialServiceMutationLedger()
		if err != nil {
			return fmt.Errorf("%w: encode canonical empty ledger: %v", errServiceMutationNotIdle, err)
		}
		if _, err := inspectInitialServiceMutationStateEntries(stateDir, exists, expected); err != nil {
			return fmt.Errorf("%w: %v", errServiceMutationNotIdle, err)
		}
	}
	if exists {
		ledger, err := decodeServiceMutationLedger(raw)
		if err != nil {
			return fmt.Errorf("%w: %v", errServiceMutationNotIdle, err)
		}
		if ledger.ActiveRequestID != "" {
			return serviceMutationActiveError("privileged mutation %s is active", ledger.ActiveRequestID)
		}
		for requestID, job := range ledger.Jobs {
			if job == nil || job.RequestID != requestID {
				return fmt.Errorf("%w: service mutation ledger identity is inconsistent", errServiceMutationNotIdle)
			}
			if serviceMutationStatusActive(job.Status) {
				return serviceMutationActiveError(
					"privileged mutation %s has active status %s", requestID, job.Status,
				)
			}
		}
	}
	if probeMutationLock {
		if err := probeServiceMutationFileLockIdle(lockPath); err != nil {
			return serviceMutationLockProbeError(err)
		}
	}
	busy, err := packageManagerMutationBusy()
	if err != nil {
		return fmt.Errorf("%w: inspect package manager activity: %v", errServiceMutationNotIdle, err)
	}
	if busy {
		return serviceMutationPackageManagerBusyError()
	}
	return nil
}

func probePreLedgerServiceMutationFileLockIdle(lockPath string) error {
	lockDir := filepath.Dir(lockPath)
	if _, err := os.Lstat(lockDir); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return fmt.Errorf("inspect service mutation lock directory: %w", err)
	}
	return probeServiceMutationFileLockIdle(lockPath)
}
