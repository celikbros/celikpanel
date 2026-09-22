package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/alicelik/celikpanel/internal/recoverycheckpoint"
	"github.com/alicelik/celikpanel/internal/recoveryobs"
	"github.com/alicelik/celikpanel/internal/recoveryruntime"
)

// Native root or authorized sudo remains the recovery principal. The optional
// owner view is loopback-only, temporary and read-only; it cannot dispatch work.
func runEntry(args []string) int {
	if len(args) > 0 && args[0] == "view" {
		return runOwnerView(args)
	}
	if len(args) > 0 && args[0] == "runtime-status" {
		return runRuntimeStatus(args, os.Geteuid(), recoveryruntime.InspectPromotion, os.Stdout, os.Stderr)
	}
	if len(args) > 0 && args[0] == "prepare-firewall-runtime" {
		return dispatchFirewallPreparation(args, os.Geteuid(), recoveryruntime.PrepareFirewallRuntime, os.Stdout, func(message string) { fmt.Fprintln(os.Stderr, message) })
	}
	if len(args) > 0 && args[0] == "prepare-runtime" {
		return dispatchRuntimePreparation(args, os.Geteuid(), prepareRuntime, func(message string) { fmt.Fprintln(os.Stderr, message) })
	}
	if os.Geteuid() == 0 && launcherDispatchCommand(args) {
		if err := dispatchLauncher(args); err != nil {
			fmt.Fprintln(os.Stderr, "The selected recovery entry could not be verified. Preserve its evidence. "+err.Error())
			return exitUnavailable
		}
	}
	if len(args) > 0 && args[0] == "verify-firewall-unit" {
		return dispatchFirewallUnitVerification(args, os.Geteuid(), recoveryruntime.VerifyFirewallUnit, func(message string) { fmt.Fprintln(os.Stderr, message) })
	}
	if len(args) > 0 && (args[0] == "verify-material-support" || args[0] == "prepare-recovery-material" || args[0] == "material-root" || args[0] == "completion-material-root" || args[0] == "verify-installed-completion" || args[0] == "database-policy" || args[0] == "verify-database-support") {
		return dispatchMaterial(args, os.Geteuid(), runMaterial, os.Stdout, func(message string) { fmt.Fprintln(os.Stderr, message) })
	}
	if len(args) > 0 && args[0] == "probe-update-database" {
		return dispatchDatabaseProbe(args, os.Geteuid(), runDatabaseProbe, func(message string) { fmt.Fprintln(os.Stderr, message) })
	}
	if len(args) > 0 && databaseActionCommand(args[0]) {
		return dispatchDatabaseAction(args, os.Geteuid(), runDatabaseAction, os.Stdout, func(message string) { fmt.Fprintln(os.Stderr, message) })
	}
	if len(args) > 0 && (args[0] == "restore-resource" || args[0] == "publish-resource") {
		return dispatchPublication(args, os.Geteuid(), runPublication, func(message string) { fmt.Fprintln(os.Stderr, message) })
	}
	if len(args) > 0 && args[0] == "verify-compatibility" {
		return dispatchCompatibility(args, os.Geteuid(), checkRecoveryCompatibility, func(message string) { fmt.Fprintln(os.Stderr, message) })
	}
	if len(args) > 0 && args[0] == "checkpoint" {
		if os.Geteuid() != 0 {
			return exitNotOwner
		}
		if len(args) != 3 || args[1] != "--name" || !recoverycheckpoint.ValidName(args[2]) {
			return exitUsage
		}
		if recoverycheckpoint.Publish(args[2]) != nil {
			fmt.Fprintln(os.Stderr, "Recovery checkpoint observation is unavailable.")
			return exitUnavailable
		}
		return exitOK
	}
	return dispatchEntry(args, os.Geteuid(), func() int { return run(args, cliRuntime{os.Geteuid, recoveryobs.Read, os.Stdout, os.Stderr}) }, runSelectedRuntime, enrollRuntime, func(message string) { fmt.Fprintln(os.Stderr, message) })
}
func dispatchEntry(args []string, uid int, observe func() int, execute func([]string) error, enroll func(string) error, report func(string)) int {
	if len(args) == 0 || args[0] == "status" || args[0] == "version" {
		return observe()
	}
	if uid != 0 {
		report("Owner authentication is required. Use your root or authorized sudo session.")
		return exitNotOwner
	}
	var err error
	switch {
	case len(args) == 1 && args[0] == "recover":
		err = execute(nil)
	case len(args) == 4 && args[0] == "recover" && args[1] == "--retry" && args[2] == "--snapshot" &&
		regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z-from-unknown-to-[0-9a-f]{40}-[0-9a-f]{32}$`).MatchString(args[3]):
		err = execute([]string{"--owner-retry", "--snapshot", args[3]})
	case len(args) == 7 && args[0] == "--verify-final-state" && args[1] == "--expected-version" && args[3] == "--expected-commit" && args[5] == "--expected-sequence" &&
		regexp.MustCompile(`^v[0-9A-Za-z.-]+$`).MatchString(args[2]) && regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(args[4]) && regexp.MustCompile(`^[1-9][0-9]{0,18}$`).MatchString(args[6]):
		err = execute(args)
	case len(args) == 5 && args[0] == "enroll-runtime" && args[1] == "--source" && args[3] == "--transaction-fd" && args[4] == "9" && filepath.IsAbs(args[2]) && filepath.Clean(args[2]) == args[2]:
		err = enroll(args[2])
	default:
		report("Usage: recovery status --request-id <id> [--json] | view --request-id <id> [--port 2084] [--lang en|tr] | runtime-status [--json] [--lang en|tr] | version | recover [--retry --snapshot <exact pending snapshot>]")
		return exitUsage
	}
	if err != nil {
		report("Recovery could not be verified. Preserve the current operation and its evidence; no new update was started. " + err.Error())
		return exitUnavailable
	}
	return exitOK
}

func dispatchCompatibility(args []string, uid int, check func(string) error, report func(string)) int {
	if uid != 0 {
		report("Owner authentication is required. Use your root or authorized sudo session.")
		return exitNotOwner
	}
	if len(args) != 3 || args[0] != "verify-compatibility" || args[1] != "--mode" ||
		(args[2] != "--normal" && args[2] != "--bootstrap-pre-ledger" && args[2] != "--bootstrap-schema17") {
		return exitUsage
	}
	if err := check(args[2]); err != nil {
		report("The selected recovery runtime cannot verify this installation. The update has not stopped the panel. " + err.Error())
		return exitUnavailable
	}
	return exitOK
}
