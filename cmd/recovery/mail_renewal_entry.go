package main

import (
	"fmt"
	"io"
	"path/filepath"
)

// This preparation runs in the verified candidate entry, not an older selected
// launcher. It never starts a service, applies policy, or initiates an update.
func dispatchMailRenewalPreparation(args []string, uid int, prepare func(string, int) (string, error), output io.Writer, report func(string)) int {
	if uid != 0 {
		return exitNotOwner
	}
	if len(args) != 5 || args[0] != "prepare-mail-renewal-runtime" || args[1] != "--source" || !filepath.IsAbs(args[2]) || filepath.Clean(args[2]) != args[2] || filepath.Base(args[2]) != "mail-renewal-runtime" || args[3] != "--transaction-fd" || args[4] != "9" {
		return exitUsage
	}
	generation, err := prepare(args[2], 9)
	if err != nil {
		report("Independent mail renewal preparation could not be verified before service downtime. Preserve the files and review this preflight failure before retrying the same release in CelikPanel. " + err.Error())
		return exitUnavailable
	}
	if _, err = fmt.Fprintln(output, generation); err != nil {
		return exitOutput
	}
	return exitOK
}

// Only the trusted recovery executable runs; historical Agent binaries are read.
func dispatchMailApplicationCompatibility(args []string, uid int, check func(string) error, report func(string)) int {
	if uid != 0 {
		return exitNotOwner
	}
	if len(args) != 3 || (args[0] != "verify-mail-application" && args[0] != "verify-agent-native-contract") || args[1] != "--bin" || !filepath.IsAbs(args[2]) || filepath.Clean(args[2]) != args[2] || filepath.Base(args[2]) != "bin" {
		return exitUsage
	}
	if err := check(args[2]); err != nil {
		report("This application's compatibility with independent mail renewal could not be verified. The server owner must keep the current renewal files and choose a release or snapshot carrying a matching supported Agent contract before resuming the same operation. " + err.Error())
		return exitUnavailable
	}
	return exitOK
}
