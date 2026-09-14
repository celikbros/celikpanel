//go:build linux && celikpanel_recovery_checker

// Built only from deploy/recovery/agent-checker.sources. The exact production
// ledger, lock and package-manager checks are shared; normal Agent startup and
// RPC handlers are not part of this executable.
// Yalnız deploy/recovery/agent-checker.sources listesinden derlenir. Gerçek
// ledger, kilit ve paket yöneticisi kontrolleri ortaktır; normal Agent açılışı
// ve RPC işleyicileri bu çalıştırılabilir dosyaya dahil değildir.
package main

import (
	"errors"
	"fmt"
	"os"
)

func main() {
	if err := runRecoveryAgentCheck(os.Args[1:], os.Geteuid()); err != nil {
		fmt.Fprintln(os.Stderr, "Recovery agent check: "+err.Error())
		os.Exit(1)
	}
}

func runRecoveryAgentCheck(args []string, euid int) error {
	if euid != 0 {
		return errors.New("root owner authentication is required")
	}
	if len(args) != 1 {
		return errors.New("exactly one existing read-only checker flag is required")
	}
	switch args[0] {
	case "--check-service-mutation-idle":
		return checkServiceMutationIdle("", "")
	case "--check-service-mutation-idle-under-external-lock":
		return checkServiceMutationIdleUnderExternalLock("", "")
	case "--check-pre-ledger-service-mutation-idle":
		return checkPreLedgerServiceMutationIdle("", "")
	case "--check-pre-ledger-service-mutation-idle-under-external-lock":
		return checkPreLedgerServiceMutationIdleUnderExternalLock("", "")
	case "--check-initial-service-mutation-ledger":
		return checkInitialServiceMutationLedger("", "")
	case "--check-initial-service-mutation-ledger-under-external-lock":
		return checkInitialServiceMutationLedgerUnderExternalLock("", "")
	default:
		return errors.New("unsupported read-only checker flag")
	}
}
