package main

import "path/filepath"

func dispatchDNSApplicationCompatibility(args []string, uid int, check func(string, string) error, report func(string)) int {
	if uid != 0 {
		return exitNotOwner
	}
	if len(args) != 5 || args[0] != "verify-dns-application" || args[1] != "--bin" || args[3] != "--state-root" ||
		!filepath.IsAbs(args[2]) || filepath.Clean(args[2]) != args[2] || filepath.Base(args[2]) != "bin" ||
		!filepath.IsAbs(args[4]) || filepath.Clean(args[4]) != args[4] {
		return exitUsage
	}
	if err := check(args[2], args[4]); err != nil {
		report("This application's DNS evidence compatibility could not be verified. The server owner must preserve the DNS files and choose a release or snapshot with a matching supported Agent before resuming the same operation. " + err.Error())
		return exitUnavailable
	}
	return exitOK
}
