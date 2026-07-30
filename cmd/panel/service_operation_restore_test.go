package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateServiceOperationReleaseTransaction(t *testing.T) {
	valid := validServiceOperationReleaseTransaction("update", "release-20260728")
	tests := []struct {
		name      string
		mutate    func(*serviceOperationReleaseTransaction)
		expected  string
		operation string
	}{
		{name: "valid", operation: "update"},
		{
			name: "standard descriptor",
			mutate: func(transaction *serviceOperationReleaseTransaction) {
				transaction.fd = 2
			},
			operation: "update",
			expected:  "descriptor",
		},
		{
			name: "short token",
			mutate: func(transaction *serviceOperationReleaseTransaction) {
				transaction.token = strings.Repeat("a", 63)
			},
			operation: "update",
			expected:  "64 lowercase hexadecimal",
		},
		{
			name: "uppercase token",
			mutate: func(transaction *serviceOperationReleaseTransaction) {
				transaction.token = strings.Repeat("a", 63) + "F"
			},
			operation: "update",
			expected:  "64 lowercase hexadecimal",
		},
		{
			name:      "operation mismatch",
			operation: "rollback",
			expected:  "exactly rollback",
		},
		{
			name: "dot snapshot",
			mutate: func(transaction *serviceOperationReleaseTransaction) {
				transaction.snapshot = "."
			},
			operation: "update",
			expected:  "safe basename",
		},
		{
			name: "nested snapshot",
			mutate: func(transaction *serviceOperationReleaseTransaction) {
				transaction.snapshot = "nested/release"
			},
			operation: "update",
			expected:  "safe basename",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transaction := valid
			if test.mutate != nil {
				test.mutate(&transaction)
			}
			err := validateServiceOperationReleaseTransaction(transaction, test.operation)
			if test.expected == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.expected) {
				t.Fatalf("error=%v want substring %q", err, test.expected)
			}
		})
	}
}

func TestCanonicalServiceOperationReleaseTransactionMarker(t *testing.T) {
	transaction := validServiceOperationReleaseTransaction("rollback", "release-20260728")
	want := "version=1\n" +
		"token=" + strings.Repeat("a", 64) + "\n" +
		"operation=rollback\n" +
		"snapshot=release-20260728\n"
	if got := string(canonicalServiceOperationReleaseTransactionMarker(transaction)); got != want {
		t.Fatalf("marker=%q want %q", got, want)
	}
}

func TestValidateServiceOperationReleaseTransactionPathBindsCreateAndRestore(t *testing.T) {
	root := filepath.Clean(t.TempDir())
	previousRoot := serviceOperationReleaseSnapshotRoot
	serviceOperationReleaseSnapshotRoot = root
	t.Cleanup(func() { serviceOperationReleaseSnapshotRoot = previousRoot })

	update := validServiceOperationReleaseTransaction("update", "release-20260728")
	staging := ".release-snapshot.incomplete.123." + strings.Repeat("a", 32)
	validCreate := filepath.Join(root, staging, update.snapshot, serviceOperationSnapshotBasename)
	if err := validateServiceOperationReleaseTransactionPath(validCreate, update, "create destination"); err != nil {
		t.Fatalf("valid create destination: %v", err)
	}
	invalidCreatePaths := []string{
		filepath.Join(root, update.snapshot, serviceOperationSnapshotBasename),
		filepath.Join(root, ".release-snapshot.incomplete.0."+strings.Repeat("a", 32), update.snapshot, serviceOperationSnapshotBasename),
		filepath.Join(root, ".release-snapshot.incomplete.123."+strings.Repeat("A", 32), update.snapshot, serviceOperationSnapshotBasename),
		filepath.Join(root, staging, "extra", update.snapshot, serviceOperationSnapshotBasename),
		filepath.Join(filepath.Dir(root), staging, update.snapshot, serviceOperationSnapshotBasename),
	}
	for _, path := range invalidCreatePaths {
		err := validateServiceOperationReleaseTransactionPath(path, update, "create destination")
		if err == nil || !strings.Contains(err.Error(), "create destination snapshot parent") {
			t.Fatalf("invalid create path %q error=%v", path, err)
		}
	}

	rollback := validServiceOperationReleaseTransaction("rollback", "release-20260728")
	validRestore := filepath.Join(root, rollback.snapshot, serviceOperationSnapshotBasename)
	if err := validateServiceOperationReleaseTransactionPath(validRestore, rollback, "restore source"); err != nil {
		t.Fatalf("valid restore source: %v", err)
	}
	invalidRestorePaths := []string{
		filepath.Join(root, "other-release", serviceOperationSnapshotBasename),
		filepath.Join(root, staging, rollback.snapshot, serviceOperationSnapshotBasename),
		filepath.Join(filepath.Dir(root), rollback.snapshot, serviceOperationSnapshotBasename),
	}
	for _, path := range invalidRestorePaths {
		err := validateServiceOperationReleaseTransactionPath(path, rollback, "restore source")
		if err == nil || !strings.Contains(err.Error(), "restore source snapshot parent") {
			t.Fatalf("invalid restore path %q error=%v", path, err)
		}
	}
}

func TestValidateServiceOperationDatabaseActionRequestBindsOperation(t *testing.T) {
	createTransaction := validServiceOperationReleaseTransaction("update", "release-create")
	action, schema, requested, err := validateServiceOperationDatabaseActionRequest(
		"/snapshot/release-create/celikpanel.db",
		"",
		"normal",
		createTransaction,
		false,
	)
	if err != nil || !requested || action != serviceOperationDatabaseActionCreate || schema != serviceOperationSnapshotSchemaNormal {
		t.Fatalf("create action=%q schema=%q requested=%v err=%v", action, schema, requested, err)
	}

	restoreTransaction := validServiceOperationReleaseTransaction("rollback", "release-restore")
	action, schema, requested, err = validateServiceOperationDatabaseActionRequest(
		"",
		"/snapshot/release-restore/celikpanel.db",
		"pre-ledger",
		restoreTransaction,
		false,
	)
	if err != nil || !requested || action != serviceOperationDatabaseActionRestore || schema != serviceOperationSnapshotSchemaPreLedger {
		t.Fatalf("restore action=%q schema=%q requested=%v err=%v", action, schema, requested, err)
	}

	wrongOperation := validServiceOperationReleaseTransaction("rollback", "release-create")
	_, _, requested, err = validateServiceOperationDatabaseActionRequest(
		"/snapshot/release-create/celikpanel.db",
		"",
		"normal",
		wrongOperation,
		false,
	)
	if !requested || err == nil || !strings.Contains(err.Error(), "exactly update") {
		t.Fatalf("cross-operation request requested=%v error=%v", requested, err)
	}
}

func TestValidateServiceOperationRescueSnapshotPathBindsUpdateTransaction(t *testing.T) {
	root := filepath.Clean(t.TempDir())
	previousRoot := serviceOperationRescueSnapshotRoot
	serviceOperationRescueSnapshotRoot = root
	t.Cleanup(func() { serviceOperationRescueSnapshotRoot = previousRoot })

	transaction := validServiceOperationReleaseTransaction("update", "release-rescue")
	validPath := filepath.Join(root, transaction.snapshot, serviceOperationSnapshotBasename)
	if err := validateServiceOperationRescueSnapshotPath(validPath, transaction); err != nil {
		t.Fatalf("valid rescue path: %v", err)
	}
	invalidPaths := []string{
		filepath.Join(root, "other-release", serviceOperationSnapshotBasename),
		filepath.Join(root, transaction.snapshot, "other.db"),
		filepath.Join(filepath.Dir(root), transaction.snapshot, serviceOperationSnapshotBasename),
		filepath.Join(root, transaction.snapshot, "nested", serviceOperationSnapshotBasename),
		filepath.Join(root, transaction.snapshot) + string(filepath.Separator) + ".." +
			string(filepath.Separator) + transaction.snapshot + string(filepath.Separator) +
			serviceOperationSnapshotBasename,
		filepath.Join("relative", transaction.snapshot, serviceOperationSnapshotBasename),
	}
	for _, path := range invalidPaths {
		err := validateServiceOperationRescueSnapshotPath(path, transaction)
		if err == nil {
			t.Fatalf("invalid rescue path %q was accepted", path)
		}
	}
}

func TestValidateServiceOperationRescueSnapshotRequestRequiresExactUpdate(t *testing.T) {
	root := filepath.Clean(t.TempDir())
	previousRoot := serviceOperationRescueSnapshotRoot
	serviceOperationRescueSnapshotRoot = root
	t.Cleanup(func() { serviceOperationRescueSnapshotRoot = previousRoot })

	transaction := validServiceOperationReleaseTransaction("update", "release-rescue")
	destination := filepath.Join(root, transaction.snapshot, serviceOperationSnapshotBasename)
	schema, requested, err := validateServiceOperationRescueSnapshotRequest(
		destination,
		"normal",
		transaction,
		false,
	)
	if err != nil || !requested || schema != serviceOperationSnapshotSchemaNormal {
		t.Fatalf("schema=%q requested=%v err=%v", schema, requested, err)
	}

	rollback := validServiceOperationReleaseTransaction("rollback", transaction.snapshot)
	_, requested, err = validateServiceOperationRescueSnapshotRequest(
		destination,
		"normal",
		rollback,
		false,
	)
	if !requested || err == nil || !strings.Contains(err.Error(), "exactly update") {
		t.Fatalf("rollback requested=%v err=%v want exact update rejection", requested, err)
	}

	_, requested, err = validateServiceOperationRescueSnapshotRequest(
		destination,
		"normal",
		transaction,
		true,
	)
	if !requested || err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("conflict requested=%v err=%v", requested, err)
	}
}

func validServiceOperationReleaseTransaction(
	operation string,
	snapshot string,
) serviceOperationReleaseTransaction {
	return serviceOperationReleaseTransaction{
		fd:        3,
		token:     strings.Repeat("a", 64),
		operation: operation,
		snapshot:  snapshot,
	}
}
