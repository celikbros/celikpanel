//go:build linux

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestRejectDatabaseQuarantineUsersAndHandlesRejectsSameUIDProcess(t *testing.T) {
	procRoot, databaseStat := fakeDatabaseQuarantineProc(t, "910101", 24567)
	err := rejectDatabaseQuarantineUsersAndHandles(procRoot, 24567, databaseStat)
	if err == nil || !strings.Contains(err.Error(), "celikpanel UID") {
		t.Fatalf("error=%v want same UID rejection", err)
	}
}

func TestRejectDatabaseQuarantineUsersAndHandlesRejectsOpenDatabase(t *testing.T) {
	procRoot, databaseStat := fakeDatabaseQuarantineProc(t, "910102", 24568)
	fdPath := filepath.Join(procRoot, "910102", "fd", "7")
	if err := os.Link(filepath.Join(procRoot, "celikpanel.db"), fdPath); err != nil {
		t.Fatal(err)
	}
	err := rejectDatabaseQuarantineUsersAndHandles(procRoot, 24567, databaseStat)
	if err == nil || !strings.Contains(err.Error(), "database open") {
		t.Fatalf("error=%v want open database rejection", err)
	}
}

func TestRejectDatabaseQuarantineUsersAndHandlesAcceptsUnrelatedProcess(t *testing.T) {
	procRoot, databaseStat := fakeDatabaseQuarantineProc(t, "910103", 24568)
	if err := rejectDatabaseQuarantineUsersAndHandles(procRoot, 24567, databaseStat); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func fakeDatabaseQuarantineProc(
	t *testing.T,
	pid string,
	uid uint32,
) (string, unix.Stat_t) {
	t.Helper()
	procRoot := t.TempDir()
	databasePath := filepath.Join(procRoot, "celikpanel.db")
	if err := os.WriteFile(databasePath, []byte("database"), 0o600); err != nil {
		t.Fatal(err)
	}
	var databaseStat unix.Stat_t
	if err := unix.Stat(databasePath, &databaseStat); err != nil {
		t.Fatal(err)
	}
	processPath := filepath.Join(procRoot, pid)
	if err := os.MkdirAll(filepath.Join(processPath, "fd"), 0o700); err != nil {
		t.Fatal(err)
	}
	status := "Name:\thelper\nUid:\t" +
		fmt.Sprint(uid) + "\t" + fmt.Sprint(uid) + "\t" + fmt.Sprint(uid) + "\t" + fmt.Sprint(uid) + "\n"
	if err := os.WriteFile(filepath.Join(processPath, "status"), []byte(status), 0o600); err != nil {
		t.Fatal(err)
	}
	return procRoot, databaseStat
}
