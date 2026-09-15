package main

import (
	"fmt"
	"io"
	"regexp"

	"github.com/alicelik/celikpanel/internal/recoverypublication"
)

func databaseActionCommand(command string) bool {
	switch command {
	case "prepare-update-database", "publish-update-database", "restore-update-database", "verify-update-database":
		return true
	default:
		return false
	}
}

var databaseWorkPath = regexp.MustCompile(`\A/var/lib/celikpanel/\.release-db-migrations/[0-9a-f]{64}/work\z`)

// These commands operate only on the already admitted native transaction. They
// accept neither a database path nor a new token, and never execute migrations.
// Komutlar yalnız kabul edilmiş yerel işlemde çalışır. Veritabanı yolu veya yeni
// token kabul etmez; migration çalıştırmaz.
func dispatchDatabaseAction(args []string, uid int, execute func(string, string) (string, error), out io.Writer, report func(string)) int {
	if uid != 0 {
		report("Owner authentication is required. Use your root or authorized sudo session.")
		return exitNotOwner
	}
	if len(args) != 3 || !databaseActionCommand(args[0]) || args[1] != "--snapshot" || !recoverypublication.ValidSnapshot(args[2]) {
		return exitUsage
	}
	work, err := execute(args[0], args[2])
	if err != nil {
		report("The database transition could not be verified. Preserve this operation and its database evidence; use recovery for the same operation. " + err.Error())
		return exitOutput
	}
	if args[0] == "prepare-update-database" {
		if !databaseWorkPath.MatchString(work) {
			return exitOutput
		}
		if _, err := fmt.Fprintln(out, work); err != nil {
			return exitOutput
		}
	} else if work != "" {
		return exitOutput
	}
	return exitOK
}

func dispatchDatabaseProbe(args []string, uid int, probe func() error, report func(string)) int {
	if uid != 0 {
		return exitNotOwner
	}
	if len(args) != 1 || args[0] != "probe-update-database" {
		return exitUsage
	}
	if err := probe(); err != nil {
		report("Database metadata is not supported for isolated migration. Panel services have not been stopped. " + err.Error())
		return exitOutput
	}
	return exitOK
}
