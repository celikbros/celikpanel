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
