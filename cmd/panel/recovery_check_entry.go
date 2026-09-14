//go:build linux && celikpanel_recovery_checker

// This entry is built only from deploy/recovery/panel-checker.sources. Passing
// that explicit file list excludes the ordinary panel entry and HTTP startup.
// It reuses the production offline validators and restoration implementation;
// this is not a second snapshot decoder or a license bypass.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

// These persisted queue values otherwise live alongside HTTP handlers in
// service_operations.go. The source-list parity test binds them to that file.
const (
	serviceOperationQueued  = "queued"
	serviceOperationRunning = "running"
)

func main() {
	if err := runRecoveryPanelCheck(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "Recovery database check: "+err.Error())
		os.Exit(1)
	}
}

func runRecoveryPanelCheck(args []string) error {
	flags := flag.NewFlagSet("recovery-panel-checker", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	normal := flags.Bool("check-service-operations-idle", false, "")
	preLedger := flags.Bool("check-pre-ledger-service-operations-idle", false, "")
	walNormal := flags.Bool("check-service-operations-idle-wal-aware", false, "")
	completed := flags.Bool("check-completed-update-database-wal-aware", false, "")
	walPreLedger := flags.Bool("check-pre-ledger-service-operations-idle-wal-aware", false, "")
	restore := flags.String("restore-service-operation-snapshot", "", "")
	create := flags.String("create-service-operation-snapshot", "", "")
	rescue := flags.String("ensure-service-operation-rescue-snapshot", "", "")
	schema := flags.String("snapshot-schema", "", "")
	fd := flags.Int("release-transaction-fd", -1, "")
	token := flags.String("release-transaction-token", "", "")
	operation := flags.String("release-transaction-operation", "", "")
	snapshot := flags.String("release-transaction-snapshot", "", "")
	seen := map[string]bool{}
	for index := 0; index < len(args); index++ {
		argument := args[index]
		if !strings.HasPrefix(argument, "--") {
			return fmt.Errorf("only explicit offline checker flags are accepted")
		}
		name, _, hasValue := strings.Cut(strings.TrimPrefix(argument, "--"), "=")
		definition := flags.Lookup(name)
		if definition == nil || seen[name] {
			return fmt.Errorf("unsupported or repeated offline checker flag")
		}
		seen[name] = true
		boolean, isBoolean := definition.Value.(interface{ IsBoolFlag() bool })
		if !hasValue && !(isBoolean && boolean.IsBoolFlag()) {
			index++
			if index == len(args) || strings.HasPrefix(args[index], "--") {
				return fmt.Errorf("offline checker flag value is missing")
			}
		}
	}
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("invalid offline checker arguments")
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("positional arguments are not accepted")
	}
	modes := 0
	for _, enabled := range []bool{*normal, *preLedger, *walNormal, *walPreLedger, *completed, *restore != "", *create != "", *rescue != ""} {
		if enabled {
			modes++
		}
	}
	if modes != 1 {
		return fmt.Errorf("exactly one offline checker operation is required")
	}
	if *restore == "" && *create == "" && *rescue == "" {
		if len(seen) != 1 {
			return fmt.Errorf("read-only checks do not accept restoration arguments")
		}
		switch {
		case *normal:
			return checkServiceOperationsIdle(databaseFile())
		case *preLedger:
			return checkPreLedgerServiceOperationsIdle(databaseFile())
		case *walNormal:
			return checkWALAwareServiceOperationsIdle(databaseFile())
		case *completed:
			return checkCompletedUpdateDatabaseWALAware(databaseFile())
		default:
			return checkWALAwarePreLedgerServiceOperationsIdle(databaseFile())
		}
	}
	if len(seen) != 6 {
		return fmt.Errorf("snapshot action requires exactly its path, schema and four transaction fields")
	}
	transaction := serviceOperationReleaseTransaction{
		fd: *fd, token: *token, operation: *operation, snapshot: *snapshot,
	}
	if *rescue != "" {
		parsedSchema, requested, err := validateServiceOperationRescueSnapshotRequest(*rescue, *schema, transaction, false)
		if err != nil {
			return err
		}
		if !requested {
			return fmt.Errorf("explicit rescue snapshot action is required")
		}
		return ensureServiceOperationRescueSnapshot(databaseFile(), *rescue, parsedSchema, transaction)
	}
	action, parsedSchema, requested, err := validateServiceOperationDatabaseActionRequest(
		*create, *restore, *schema, transaction, false,
	)
	if err != nil {
		return err
	}
	if !requested {
		return fmt.Errorf("explicit guarded snapshot action is required")
	}
	switch action {
	case serviceOperationDatabaseActionCreate:
		return createReleaseServiceOperationSnapshot(databaseFile(), *create, parsedSchema, transaction)
	case serviceOperationDatabaseActionRestore:
		return restoreServiceOperationSnapshot(*restore, parsedSchema, transaction)
	default:
		return fmt.Errorf("unsupported guarded snapshot action")
	}
}
