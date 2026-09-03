package main

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const (
	serviceOperationReleaseTransactionRoot       = "/var/lib/celikpanel-release-transaction"
	serviceOperationReleaseTransactionLockPath   = serviceOperationReleaseTransactionRoot + "/transaction.lock"
	serviceOperationReleaseTransactionActivePath = serviceOperationReleaseTransactionRoot + "/active"
	serviceOperationReleaseSnapshotRootDefault   = "/var/backups/celikpanel/update-snapshots"
	serviceOperationRescueSnapshotRootDefault    = "/var/backups/celikpanel/recovery-snapshots"
	serviceOperationRestoreUser                  = "celikpanel"
	serviceOperationRestoreGroup                 = "celikpanel"
)

var serviceOperationReleaseTransactionSnapshotPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
var serviceOperationReleaseSnapshotStagingPattern = regexp.MustCompile(`^\.release-snapshot\.incomplete\.[1-9][0-9]*\.[a-f0-9]{32}$`)

// serviceOperationReleaseSnapshotRoot is a package variable only so Linux
// tests can bind the contract to a private root-owned fixture. Production code
// never accepts an environment or CLI override for this trust anchor.
// serviceOperationReleaseSnapshotRoot yalnızca Linux testlerinin sözleşmeyi
// özel root sahipliğindeki fixture'a bağlayabilmesi için paket değişkenidir.
// Üretim kodu bu güven kökü için ortam veya CLI override'ı kabul etmez.
var serviceOperationReleaseSnapshotRoot = serviceOperationReleaseSnapshotRootDefault

// serviceOperationRescueSnapshotRoot is variable only so tests can bind the
// immutable production path contract to a private root-owned fixture.
var serviceOperationRescueSnapshotRoot = serviceOperationRescueSnapshotRootDefault

type serviceOperationDatabaseAction string

const (
	serviceOperationDatabaseActionCreate  serviceOperationDatabaseAction = "create"
	serviceOperationDatabaseActionRestore serviceOperationDatabaseAction = "restore"
)

type serviceOperationRestoreOwner struct {
	uid uint32
	gid uint32
}

type serviceOperationReleaseTransaction struct {
	fd        int
	token     string
	operation string
	snapshot  string
}

type panelCommandModes struct {
	createAdmin                bool
	validateAdminCredentials   bool
	countUsers                 bool
	countUsersReadOnlyWALAware bool
	checkIdle                  bool
	checkPreLedgerIdle         bool
	checkWALAwareIdle          bool
	checkWALAwarePreLedgerIdle bool
	createOrRestore            bool
	rescueSnapshot             bool
	proveSnapshotEquivalence   bool
	migrateOnly                bool
	generateControlPlaneKey    bool
	createControlPlaneArchive  bool
	restoreControlPlaneArchive bool
	inspectControlPlaneArchive bool
	demo                       bool
	insecureCookies            bool
}

type serviceOperationRestoreHooks struct {
	beforeRename func() error
	afterRename  func() error
}

// restoreServiceOperationSnapshot is an offline, root-only operation. The
// caller must stop both CelikPanel services and retain the inherited global
// release-guard descriptor until this process exits.
// restoreServiceOperationSnapshot çevrim dışı, yalnız root işlemdir. Çağıran,
// iki CelikPanel servisini de durdurmalı ve devralınan global yayın-koruması
// descriptor'ını bu süreç çıkana kadar tutmalıdır.
func restoreServiceOperationSnapshot(
	sourcePath string,
	schema serviceOperationSnapshotSchema,
	transaction serviceOperationReleaseTransaction,
) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("service operation restore must run as root")
	}
	if err := validateServiceOperationReleaseTransaction(transaction, "rollback"); err != nil {
		return err
	}
	if err := validateServiceOperationReleaseTransactionPath(
		sourcePath,
		transaction,
		"restore source",
	); err != nil {
		return err
	}
	owner, err := lookupServiceOperationRestoreOwner()
	if err != nil {
		return err
	}
	if err := verifyServiceOperationReleaseTransaction(transaction); err != nil {
		return err
	}
	if err := verifyCelikPanelServicesStopped(); err != nil {
		return err
	}
	return restoreServiceOperationSnapshotWithOwner(sourcePath, schema, owner, serviceOperationRestoreHooks{})
}

func createReleaseServiceOperationSnapshot(
	sourcePath string,
	destinationPath string,
	schema serviceOperationSnapshotSchema,
	transaction serviceOperationReleaseTransaction,
) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("service operation snapshot must run as root")
	}
	if err := validateServiceOperationReleaseTransaction(transaction, "update"); err != nil {
		return err
	}
	if err := validateServiceOperationReleaseTransactionPath(
		destinationPath,
		transaction,
		"create destination",
	); err != nil {
		return err
	}
	if err := verifyServiceOperationReleaseTransaction(transaction); err != nil {
		return err
	}
	if err := verifyCelikPanelServicesStopped(); err != nil {
		return err
	}
	owner, err := lookupServiceOperationRestoreOwner()
	if err != nil {
		return err
	}
	return createReleaseServiceOperationSnapshotWithOwner(
		sourcePath,
		destinationPath,
		schema,
		owner,
	)
}

// ensureServiceOperationRescueSnapshot is the transaction-bound emergency
// snapshot mode used only while an UPDATE transaction owns the inherited
// release lock and both services are proven stopped. Unlike the normal release
// snapshot mode, it never normalizes or otherwise mutates the canonical DB.
func ensureServiceOperationRescueSnapshot(
	sourcePath string,
	destinationPath string,
	schema serviceOperationSnapshotSchema,
	transaction serviceOperationReleaseTransaction,
) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("service operation rescue snapshot must run as root")
	}
	if err := validateServiceOperationReleaseTransaction(transaction, "update"); err != nil {
		return err
	}
	if err := validateServiceOperationRescueSnapshotPath(destinationPath, transaction); err != nil {
		return err
	}
	if err := verifyServiceOperationReleaseTransaction(transaction); err != nil {
		return err
	}
	if err := verifyCelikPanelServicesStopped(); err != nil {
		return err
	}
	owner, err := lookupServiceOperationRestoreOwner()
	if err != nil {
		return err
	}
	return ensureServiceOperationRescueSnapshotWithOwner(
		sourcePath,
		destinationPath,
		schema,
		owner,
	)
}

func validateServiceOperationReleaseTransaction(
	transaction serviceOperationReleaseTransaction,
	expectedOperation string,
) error {
	if transaction.fd < 3 {
		return fmt.Errorf("release transaction descriptor must be inherited and greater than 2")
	}
	if len(transaction.token) != 64 {
		return fmt.Errorf("release transaction token must be exactly 64 lowercase hexadecimal characters")
	}
	for _, character := range transaction.token {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return fmt.Errorf("release transaction token must be exactly 64 lowercase hexadecimal characters")
		}
	}
	if transaction.operation != expectedOperation {
		return fmt.Errorf("release transaction operation must be exactly %s", expectedOperation)
	}
	if transaction.snapshot == "." ||
		transaction.snapshot == ".." ||
		!serviceOperationReleaseTransactionSnapshotPattern.MatchString(transaction.snapshot) {
		return fmt.Errorf("release transaction snapshot must be a safe basename of at most 128 characters")
	}
	return nil
}

func canonicalServiceOperationReleaseTransactionMarker(
	transaction serviceOperationReleaseTransaction,
) []byte {
	return []byte(fmt.Sprintf(
		"version=1\ntoken=%s\noperation=%s\nsnapshot=%s\n",
		transaction.token,
		transaction.operation,
		transaction.snapshot,
	))
}

func validateServiceOperationReleaseTransactionPath(
	databasePath string,
	transaction serviceOperationReleaseTransaction,
	purpose string,
) error {
	if databasePath == "" ||
		!filepath.IsAbs(databasePath) ||
		filepath.Clean(databasePath) != databasePath ||
		filepath.Base(databasePath) != serviceOperationSnapshotBasename {
		return fmt.Errorf(
			"%s must be a clean absolute %s path",
			purpose,
			serviceOperationSnapshotBasename,
		)
	}
	expectedParent := filepath.Join(serviceOperationReleaseSnapshotRoot, transaction.snapshot)
	if transaction.operation == "update" {
		stagingParent := filepath.Dir(filepath.Dir(databasePath))
		stagingBase := filepath.Base(stagingParent)
		if !serviceOperationReleaseSnapshotStagingPattern.MatchString(stagingBase) ||
			filepath.Dir(stagingParent) != serviceOperationReleaseSnapshotRoot ||
			filepath.Dir(databasePath) != filepath.Join(stagingParent, transaction.snapshot) {
			return fmt.Errorf(
				"%s snapshot parent does not match the canonical active release transaction staging child",
				purpose,
			)
		}
		return nil
	}
	if filepath.Dir(databasePath) != expectedParent {
		return fmt.Errorf(
			"%s snapshot parent does not match the canonical active release transaction child",
			purpose,
		)
	}
	return nil
}

func validateServiceOperationRescueSnapshotPath(
	databasePath string,
	transaction serviceOperationReleaseTransaction,
) error {
	if databasePath == "" ||
		!filepath.IsAbs(databasePath) ||
		filepath.Clean(databasePath) != databasePath ||
		filepath.Base(databasePath) != serviceOperationSnapshotBasename {
		return fmt.Errorf(
			"rescue snapshot destination must be a clean absolute %s path",
			serviceOperationSnapshotBasename,
		)
	}
	expectedPath := filepath.Join(
		serviceOperationRescueSnapshotRoot,
		transaction.snapshot,
		serviceOperationSnapshotBasename,
	)
	if databasePath != expectedPath {
		return fmt.Errorf(
			"rescue snapshot destination must match the canonical active update transaction recovery path",
		)
	}
	return nil
}

func lookupServiceOperationRestoreOwner() (serviceOperationRestoreOwner, error) {
	account, err := user.Lookup(serviceOperationRestoreUser)
	if err != nil {
		return serviceOperationRestoreOwner{}, fmt.Errorf("look up %s user: %w", serviceOperationRestoreUser, err)
	}
	group, err := user.LookupGroup(serviceOperationRestoreGroup)
	if err != nil {
		return serviceOperationRestoreOwner{}, fmt.Errorf("look up %s group: %w", serviceOperationRestoreGroup, err)
	}
	uid, err := strconv.ParseUint(account.Uid, 10, 32)
	if err != nil || uid == 0 {
		return serviceOperationRestoreOwner{}, fmt.Errorf("%s user id must be a non-root numeric id", serviceOperationRestoreUser)
	}
	gid, err := strconv.ParseUint(group.Gid, 10, 32)
	if err != nil || gid == 0 {
		return serviceOperationRestoreOwner{}, fmt.Errorf("%s group id must be a non-root numeric id", serviceOperationRestoreGroup)
	}
	return serviceOperationRestoreOwner{uid: uint32(uid), gid: uint32(gid)}, nil
}

func verifyCelikPanelServicesStopped() error {
	return verifyCelikPanelServicesStoppedPlatform()
}

func validateServiceOperationDatabaseActionRequest(
	createPath string,
	restorePath string,
	schemaValue string,
	transaction serviceOperationReleaseTransaction,
	conflictingMode bool,
) (
	serviceOperationDatabaseAction,
	serviceOperationSnapshotSchema,
	bool,
	error,
) {
	createPath = strings.TrimSpace(createPath)
	restorePath = strings.TrimSpace(restorePath)
	schemaValue = strings.TrimSpace(schemaValue)
	transaction.token = strings.TrimSpace(transaction.token)
	transaction.operation = strings.TrimSpace(transaction.operation)
	transaction.snapshot = strings.TrimSpace(transaction.snapshot)
	requested := createPath != "" ||
		restorePath != "" ||
		schemaValue != "" ||
		transaction.fd != -1 ||
		transaction.token != "" ||
		transaction.operation != "" ||
		transaction.snapshot != ""
	if !requested {
		return "", "", false, nil
	}
	if createPath != "" && restorePath != "" {
		return "", "", true, fmt.Errorf("snapshot create and restore modes are mutually exclusive")
	}
	if createPath == "" && restorePath == "" {
		return "", "", true, fmt.Errorf("snapshot create or restore path is required")
	}
	if schemaValue == "" {
		return "", "", true, fmt.Errorf("snapshot schema is required")
	}
	if conflictingMode {
		return "", "", true, fmt.Errorf("snapshot mode is mutually exclusive with other panel modes")
	}
	schema, err := parseServiceOperationSnapshotSchema(schemaValue)
	if err != nil {
		return "", "", true, err
	}
	if createPath != "" {
		if err := validateServiceOperationReleaseTransaction(transaction, "update"); err != nil {
			return "", "", true, err
		}
		return serviceOperationDatabaseActionCreate, schema, true, nil
	}
	if err := validateServiceOperationReleaseTransaction(transaction, "rollback"); err != nil {
		return "", "", true, err
	}
	return serviceOperationDatabaseActionRestore, schema, true, nil
}

func validateServiceOperationRescueSnapshotRequest(
	destinationPath string,
	schemaValue string,
	transaction serviceOperationReleaseTransaction,
	conflictingMode bool,
) (serviceOperationSnapshotSchema, bool, error) {
	destinationPath = strings.TrimSpace(destinationPath)
	if destinationPath == "" {
		return "", false, nil
	}
	if conflictingMode {
		return "", true, fmt.Errorf("rescue snapshot mode is mutually exclusive with other database actions")
	}
	schemaValue = strings.TrimSpace(schemaValue)
	if schemaValue == "" {
		return "", true, fmt.Errorf("snapshot schema is required")
	}
	transaction.token = strings.TrimSpace(transaction.token)
	transaction.operation = strings.TrimSpace(transaction.operation)
	transaction.snapshot = strings.TrimSpace(transaction.snapshot)
	schema, err := parseServiceOperationSnapshotSchema(schemaValue)
	if err != nil {
		return "", true, err
	}
	if err := validateServiceOperationReleaseTransaction(transaction, "update"); err != nil {
		return "", true, err
	}
	if err := validateServiceOperationRescueSnapshotPath(destinationPath, transaction); err != nil {
		return "", true, err
	}
	return schema, true, nil
}

func validateMigrateOnlyRequest(enabled bool, conflictingMode bool) error {
	if enabled && conflictingMode {
		return errors.New("migrate-only mode is mutually exclusive with other panel modes")
	}
	return nil
}

func validatePanelCommandModes(modes panelCommandModes) error {
	oneShotModes := make([]string, 0, 16)
	for _, mode := range []struct {
		name    string
		enabled bool
	}{
		{name: "create-admin", enabled: modes.createAdmin},
		{name: "validate-admin-credentials-file", enabled: modes.validateAdminCredentials},
		{name: "count-users", enabled: modes.countUsers},
		{name: "count-users-read-only-wal-aware", enabled: modes.countUsersReadOnlyWALAware},
		{name: "check-service-operations-idle", enabled: modes.checkIdle},
		{name: "check-pre-ledger-service-operations-idle", enabled: modes.checkPreLedgerIdle},
		{name: "check-service-operations-idle-wal-aware", enabled: modes.checkWALAwareIdle},
		{name: "check-pre-ledger-service-operations-idle-wal-aware", enabled: modes.checkWALAwarePreLedgerIdle},
		{name: "service-operation-snapshot-create-or-restore", enabled: modes.createOrRestore},
		{name: "ensure-service-operation-rescue-snapshot", enabled: modes.rescueSnapshot},
		{name: "prove-pre-ledger-snapshot-equivalence", enabled: modes.proveSnapshotEquivalence},
		{name: "migrate-only", enabled: modes.migrateOnly},
		{name: "generate-control-plane-key", enabled: modes.generateControlPlaneKey},
		{name: "create-control-plane-archive", enabled: modes.createControlPlaneArchive},
		{name: "restore-control-plane-archive", enabled: modes.restoreControlPlaneArchive},
		{name: "inspect-control-plane-archive", enabled: modes.inspectControlPlaneArchive},
	} {
		if mode.enabled {
			oneShotModes = append(oneShotModes, mode.name)
		}
	}
	if len(oneShotModes) > 1 {
		return fmt.Errorf(
			"one-shot panel modes are mutually exclusive: %s",
			strings.Join(oneShotModes, ", "),
		)
	}
	if len(oneShotModes) == 0 {
		return nil
	}
	runtimeFlags := make([]string, 0, 2)
	if modes.demo {
		runtimeFlags = append(runtimeFlags, "demo")
	}
	if modes.insecureCookies {
		runtimeFlags = append(runtimeFlags, "insecure-cookies")
	}
	if len(runtimeFlags) != 0 {
		return fmt.Errorf(
			"one-shot panel mode %s cannot be combined with runtime flags: %s",
			oneShotModes[0],
			strings.Join(runtimeFlags, ", "),
		)
	}
	return nil
}

func serviceOperationRestoreCommandContract() string {
	return strings.Join([]string{
		"stop celikpanel-panel.service and celikpanel-agent.service",
		"hold the inherited global release transaction descriptor",
		"match the exact active transaction token, rollback operation, and snapshot basename",
		"use --restore-service-operation-snapshot with an absolute root-only celikpanel.db",
		"use --snapshot-schema=normal or --snapshot-schema=pre-ledger",
		"do not restore with cp -a",
	}, "; ")
}
