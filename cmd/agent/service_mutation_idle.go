package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var errServiceMutationNotIdle = errors.New("service mutation state is not idle")

// checkServiceMutationIdle is intentionally read-only. Release tooling calls it
// after stopping the panel, before pairing a panel database snapshot with the
// privileged agent ledger. Missing, unsafe, inconsistent, active or externally
// locked state is rejected rather than repaired here.
// checkServiceMutationIdle bilerek salt okunurdur. Sürüm araçları paneli
// durdurduktan sonra, panel veritabanı anlık görüntüsünü ayrıcalıklı agent
// ledger'ıyla eşleştirmeden önce bunu çağırır. Eksik, güvensiz, tutarsız, etkin
// veya dışarıdan kilitli durum burada onarılmak yerine reddedilir.
func checkServiceMutationIdle(stateDir, lockPath string) error {
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
	info, err := os.Lstat(stateDir)
	if err != nil {
		return fmt.Errorf("%w: inspect service mutation state directory: %v", errServiceMutationNotIdle, err)
	}
	if err := secureServiceMutationStateDirectoryStat(stateDir, info); err != nil {
		return fmt.Errorf("%w: %v", errServiceMutationNotIdle, err)
	}

	raw, exists, err := readSecureServiceMutationLedger(
		filepath.Join(stateDir, "service-mutations.json"),
		serviceMutationLedgerMaxSize,
	)
	if err != nil {
		return fmt.Errorf("%w: %v", errServiceMutationNotIdle, err)
	}
	if exists {
		ledger, err := decodeServiceMutationLedger(raw)
		if err != nil {
			return fmt.Errorf("%w: %v", errServiceMutationNotIdle, err)
		}
		if ledger.ActiveRequestID != "" {
			return fmt.Errorf("%w: privileged mutation %s is active", errServiceMutationNotIdle, ledger.ActiveRequestID)
		}
		for requestID, job := range ledger.Jobs {
			if job == nil || job.RequestID != requestID {
				return fmt.Errorf("%w: service mutation ledger identity is inconsistent", errServiceMutationNotIdle)
			}
			if serviceMutationStatusActive(job.Status) {
				return fmt.Errorf("%w: privileged mutation %s has active status %s", errServiceMutationNotIdle, requestID, job.Status)
			}
		}
	}
	if err := probeServiceMutationFileLockIdle(lockPath); err != nil {
		return fmt.Errorf("%w: %v", errServiceMutationNotIdle, err)
	}
	busy, err := packageManagerMutationBusy()
	if err != nil {
		return fmt.Errorf("%w: inspect package manager activity: %v", errServiceMutationNotIdle, err)
	}
	if busy {
		return fmt.Errorf("%w: the host package manager is active", errServiceMutationNotIdle)
	}
	return nil
}
