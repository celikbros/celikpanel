package main

import (
	"reflect"
	"testing"
)

func TestScheduledRetentionNeverDeletesManualBackups(t *testing.T) {
	backups := []backupRetentionRecord{
		{Name: "scheduled_files_20260727_030000.tar.gz", Type: "files", Scheduled: true},
		{Name: "full_20260727_020000.tar.gz", Type: "full", Scheduled: false},
		{Name: "scheduled_full_20260727_010000.tar.gz", Type: "full", Scheduled: true},
		{Name: "files_20260726_230000.tar.gz", Type: "files", Scheduled: false},
	}
	want := []string{"scheduled_full_20260727_010000.tar.gz"}
	if got := scheduledBackupsToDelete(backups, 1); !reflect.DeepEqual(got, want) {
		t.Fatalf("scheduledBackupsToDelete() = %#v, want %#v", got, want)
	}
}
