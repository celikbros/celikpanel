package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestDatabaseCommandRejectsNewAuthorityAndInvalidResults(t *testing.T) {
	snapshot := "20260915T010000Z-from-unknown-to-" + strings.Repeat("a", 40) + "-" + strings.Repeat("b", 32)
	work := "/var/lib/celikpanel/.release-db-migrations/" + strings.Repeat("c", 64) + "/work"
	cases := []struct {
		name    string
		uid     int
		args    []string
		result  string
		failure error
		want    int
		calls   int
	}{
		{"owner required", 1000, []string{"prepare-update-database", "--snapshot", snapshot}, "", nil, exitNotOwner, 0},
		{"arbitrary database", 0, []string{"prepare-update-database", "--database", "/tmp/celikpanel.db"}, "", nil, exitUsage, 0},
		{"new token", 0, []string{"prepare-update-database", "--snapshot", snapshot, "--token", strings.Repeat("d", 64)}, "", nil, exitUsage, 0},
		{"path snapshot", 0, []string{"restore-update-database", "--snapshot", "../../snapshot"}, "", nil, exitUsage, 0},
		{"duplicate snapshot", 0, []string{"restore-update-database", "--snapshot", snapshot, "--snapshot", snapshot}, "", nil, exitUsage, 0},
		{"unknown command", 0, []string{"migrate-update-database", "--snapshot", snapshot}, "", nil, exitUsage, 0},
		{"exact preparation", 0, []string{"prepare-update-database", "--snapshot", snapshot}, work, nil, exitOK, 1},
		{"exact publication", 0, []string{"publish-update-database", "--snapshot", snapshot}, "", nil, exitOK, 1},
		{"exact restoration", 0, []string{"restore-update-database", "--snapshot", snapshot}, "", nil, exitOK, 1},
		{"exact verification", 0, []string{"verify-update-database", "--snapshot", snapshot}, "", nil, exitOK, 1},
		{"unavailable is not success", 0, []string{"restore-update-database", "--snapshot", snapshot}, "", errors.New("unavailable evidence"), exitOutput, 1},
		{"foreign work directory", 0, []string{"prepare-update-database", "--snapshot", snapshot}, "/tmp/work", nil, exitOutput, 1},
		{"mixed output", 0, []string{"prepare-update-database", "--snapshot", snapshot}, work + "\nextra", nil, exitOutput, 1},
		{"traversal output", 0, []string{"prepare-update-database", "--snapshot", snapshot}, work + "/../../other", nil, exitOutput, 1},
		{"unexpected publication output", 0, []string{"publish-update-database", "--snapshot", snapshot}, work, nil, exitOutput, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			var output bytes.Buffer
			got := dispatchDatabaseAction(tc.args, tc.uid, func(command, actual string) (string, error) {
				calls++
				if command != tc.args[0] || actual != snapshot {
					t.Fatal("command identity changed")
				}
				return tc.result, tc.failure
			}, &output, func(string) {})
			if got != tc.want || calls != tc.calls {
				t.Fatalf("exit=%d calls=%d want=%d/%d", got, calls, tc.want, tc.calls)
			}
			if got != exitOK && output.Len() != 0 {
				t.Fatal("failure emitted a usable workspace")
			}
			if tc.name == "exact preparation" && output.String() != work+"\n" {
				t.Fatal("exact workspace was not returned")
			}
		})
	}
}

func TestDatabasePreflightCannotBecomeMutation(t *testing.T) {
	for _, args := range [][]string{{"probe-update-database"}, {"probe-update-database", "--snapshot", "x"}, {"publish-update-database"}} {
		called := false
		got := dispatchDatabaseProbe(args, 0, func() error { called = true; return errors.New("unsupported metadata") }, func(string) {})
		if len(args) == 1 && args[0] == "probe-update-database" {
			if !called || got != exitOutput {
				t.Fatal("unknown metadata admitted")
			}
		} else if called || got != exitUsage {
			t.Fatal("probe accepted mutation authority")
		}
	}
	if got := dispatchDatabaseProbe([]string{"probe-update-database"}, 1000, func() error { t.Fatal("unprivileged probe executed"); return nil }, func(string) {}); got != exitNotOwner {
		t.Fatal(got)
	}
}
