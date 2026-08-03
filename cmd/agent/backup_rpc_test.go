package main

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestBackupRPCRejectsLegacyRequestsWithoutImmutableIdentity(t *testing.T) {
	agent := &Agent{}
	var create BackupResponse
	if err := agent.CreateBackup(&BackupRequest{Type: "files"}, &create); err != nil {
		t.Fatal(err)
	}
	if create.Success || !strings.Contains(create.Error, "IDs must be positive") {
		t.Fatalf("legacy create request was not rejected: %+v", create)
	}

	var list ListBackupsResponse
	if err := agent.ListBackups(&ListBackupsRequest{}, &list); err == nil {
		t.Fatal("legacy list request without identities was accepted")
	}

	var restore BackupResponse
	if err := agent.RestoreBackup(&RestoreRequest{
		SubscriptionID: 1,
		DomainID:       2,
		BackupName:     "../../etc/shadow",
	}, &restore); err != nil {
		t.Fatal(err)
	}
	if restore.Success || restore.Error == "" {
		t.Fatalf("traversal restore request was not rejected: %+v", restore)
	}
}

func TestDatabaseCommandsUseArgvAndRejectInjectionIdentifiers(t *testing.T) {
	for _, candidate := range []string{
		"tenant;touch_pwned",
		"tenant name",
		"tenant$(id)",
		"tenant/name",
		"-defaults_file",
		strings.Repeat("a", 64),
	} {
		if _, err := databaseDumpCommand(context.Background(), candidate, "mysql"); err == nil {
			t.Errorf("dump accepted unsafe database identifier %q", candidate)
		}
		if _, err := databaseRestoreCommand(context.Background(), candidate, "postgresql"); err == nil {
			t.Errorf("restore accepted unsafe database identifier %q", candidate)
		}
	}

	mysqlDump, err := databaseDumpCommand(context.Background(), "tenant_db", "mysql")
	if err != nil {
		t.Fatal(err)
	}
	if mysqlDump.Path == "bash" || mysqlDump.Path == "sh" {
		t.Fatalf("database dump unexpectedly uses a shell: %q", mysqlDump.Path)
	}
	if want := []string{
		"mysqldump", "--single-transaction", "--routines", "--triggers", "tenant_db",
	}; !reflect.DeepEqual(mysqlDump.Args, want) {
		t.Fatalf("mysqldump argv = %#v, want %#v", mysqlDump.Args, want)
	}

	postgresRestore, err := databaseRestoreCommand(
		context.Background(), "tenant_db", "postgresql",
	)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{
		"psql", "--dbname", "tenant_db", "--set", "ON_ERROR_STOP=1",
	}; !reflect.DeepEqual(postgresRestore.Args, want) {
		t.Fatalf("psql argv = %#v, want %#v", postgresRestore.Args, want)
	}
}

func TestBackupNamesContainOnlyOpaqueDatabaseIdentity(t *testing.T) {
	now := time.Date(2026, 7, 27, 12, 34, 56, 0, time.UTC)
	name, err := generatedBackupName("database", "mariadb", 42, now)
	if err != nil {
		t.Fatal(err)
	}
	if name != "db_mysql_42_20260727_123456.sql.gz" {
		t.Fatalf("name = %q", name)
	}
	backupType, databaseType, databaseID, err := parseBackupName(name)
	if err != nil {
		t.Fatal(err)
	}
	if backupType != "database" || databaseType != "mysql" || databaseID != 42 {
		t.Fatalf("parsed backup identity = %q, %q, %d", backupType, databaseType, databaseID)
	}
	for _, legacy := range []string{
		"db_customer_20260727_123456.sql.gz",
		"files_../../etc/passwd",
		"files_20260727_123456.tar.gz/child",
	} {
		if _, _, _, err := parseBackupName(legacy); err == nil {
			t.Errorf("legacy/unsafe backup name accepted: %q", legacy)
		}
	}
}

func TestScheduledBackupNamesAreDistinctFromManualBackups(t *testing.T) {
	now := time.Date(2026, 7, 27, 12, 34, 56, 0, time.UTC)
	manual, err := generatedBackupNameWithSchedule("files", "", 0, now, false)
	if err != nil {
		t.Fatal(err)
	}
	scheduled, err := generatedBackupNameWithSchedule("files", "", 0, now, true)
	if err != nil {
		t.Fatal(err)
	}
	if manual != "files_20260727_123456.tar.gz" || isScheduledBackupName(manual) {
		t.Fatalf("manual backup identity = %q", manual)
	}
	if scheduled != "scheduled_files_20260727_123456.tar.gz" || !isScheduledBackupName(scheduled) {
		t.Fatalf("scheduled backup identity = %q", scheduled)
	}
	backupType, _, _, err := parseBackupName(scheduled)
	if err != nil || backupType != "files" {
		t.Fatalf("parse scheduled backup = %q, %v", backupType, err)
	}
}
