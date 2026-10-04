package main

import (
	"errors"
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
		report("The database transition could not be verified. Preserve this operation and its database evidence. The server owner can inspect the recovery runtime with sudo /usr/libexec/celikpanel/recovery runtime-status, then resume this same operation with sudo /usr/libexec/celikpanel/recovery recover. " + err.Error())
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
		reason := "Database migration readiness could not be verified."
		if errors.Is(err, recoverypublication.ErrUnsupportedMetadata) {
			reason = "This recovery version does not support the database's filesystem attributes."
		}
		if errors.Is(err, recoverypublication.ErrUnsupportedDatabaseParent) {
			report("This update requires /var/lib/celikpanel to be owned by celikpanel:celikpanel with mode 0750. The observed directory layout is unsupported and has been preserved. This check has not stopped services. The server owner should review the directory ownership and permissions in the update details, preserve intentional settings, and retry from the panel only after choosing a supported layout or a compatible recovery version. " + err.Error())
			return exitOutput
		}
		report(reason + " This check has not stopped services. The server owner should review the database and parent metadata in the update details, preserve existing attributes, and retry from the panel only after the reported requirement is resolved. " + err.Error())
		return exitOutput
	}
	return exitOK
}
