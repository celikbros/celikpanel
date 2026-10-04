package main

import (
	"fmt"
	"io"
	"path/filepath"
)

// This preparation runs in the verified candidate entry, not an older selected
// launcher. It never starts a service, applies policy, or initiates an update.
func dispatchFirewallPreparation(args []string, uid int, prepare func(string, int) (string, error), output io.Writer, report func(string)) int {
	if uid != 0 {
		return exitNotOwner
	}
	if len(args) != 5 || args[0] != "prepare-firewall-runtime" || args[1] != "--source" || !filepath.IsAbs(args[2]) || filepath.Clean(args[2]) != args[2] || filepath.Base(args[2]) != "firewall-runtime" || args[3] != "--transaction-fd" || args[4] != "9" {
		return exitUsage
	}
	generation, err := prepare(args[2], 9)
	if err != nil {
		report("Independent firewall preparation could not be verified before service downtime. Preserve the files and review this preflight failure before retrying the same release in CelikPanel. " + err.Error())
		return exitUnavailable
	}
	if _, err = fmt.Fprintln(output, generation); err != nil {
		return exitOutput
	}
	return exitOK
}

func dispatchFirewallUnitVerification(args []string, uid int, verify func(string) error, report func(string)) int {
	if uid != 0 {
		return exitNotOwner
	}
	if len(args) != 3 || args[0] != "verify-firewall-unit" || args[1] != "--unit" || !filepath.IsAbs(args[2]) || filepath.Clean(args[2]) != args[2] || filepath.Base(args[2]) != "celikpanel-firewall-restore.service" {
		return exitUsage
	}
	if err := verify(args[2]); err != nil {
		report("The independent helper required by this firewall unit could not be verified. Preserve its files; the owner must restore the matching retained generation before resuming this unit transition. " + err.Error())
		return exitUnavailable
	}
	return exitOK
}
