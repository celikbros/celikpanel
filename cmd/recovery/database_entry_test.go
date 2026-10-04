package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/recoverypublication"
	"github.com/alicelik/celikpanel/internal/recoveryruntime"
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

func TestDatabasePreflightDistinguishesUnsupportedMetadataFromUnknown(t *testing.T) {
	for _, tc := range []struct {
		name    string
		failure error
		want    string
		wrong   string
	}{
		{"unsupported", recoverypublication.ErrUnsupportedMetadata, "does not support the database's filesystem attributes", "readiness could not be verified"},
		{"unsupported parent", recoverypublication.ErrUnsupportedDatabaseParent, "observed directory layout is unsupported", "filesystem attributes"},
		{"unknown", recoverypublication.ErrUnavailable, "readiness could not be verified", "does not support the database's filesystem attributes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var message string
			code := dispatchDatabaseProbe([]string{"probe-update-database"}, 0, func() error { return tc.failure }, func(v string) { message = v })
			if code != exitOutput || !strings.Contains(message, tc.want) || strings.Contains(message, tc.wrong) {
				t.Fatalf("incorrect classification: %d %s", code, message)
			}
			if !strings.Contains(message, "server owner") || !strings.Contains(message, "retry from the panel") {
				t.Fatalf("owner action is missing: %s", message)
			}
		})
	}
}

func TestDatabaseFailureGuidanceUsesAcceptedOwnerCommands(t *testing.T) {
	snapshot := "20260915T010000Z-from-unknown-to-" + strings.Repeat("a", 40) + "-" + strings.Repeat("b", 32)
	var message string
	var output bytes.Buffer
	code := dispatchDatabaseAction([]string{"publish-update-database", "--snapshot", snapshot}, 0, func(string, string) (string, error) { return "", errors.New("unknown evidence") }, &output, func(v string) { message = v })
	if code != exitOutput {
		t.Fatal(code)
	}
	prefix := "sudo /usr/libexec/celikpanel/recovery "
	parts := strings.Split(message, prefix)
	if len(parts) != 3 {
		t.Fatalf("owner commands missing: %s", message)
	}
	observation := strings.TrimRight(strings.Fields(parts[1])[0], ",.")
	resume := strings.TrimRight(strings.Fields(parts[2])[0], ",.")
	var diagnostics bytes.Buffer
	if got := runRuntimeStatus([]string{observation}, 0, func() (recoveryruntime.PromotionStatus, error) {
		return recoveryruntime.PromotionStatus{Phase: "none"}, nil
	}, &output, &diagnostics); got != exitOK {
		t.Fatalf("suggested read command rejected: %s exit=%d", observation, got)
	}
	called := false
	if got := dispatchEntry([]string{resume}, 0, func() int { t.Fatal("resume became observation"); return exitUsage }, func(args []string) error {
		called = true
		if len(args) != 0 {
			t.Fatal("resume introduced authority")
		}
		return nil
	}, func(string) error { t.Fatal("resume became enrollment"); return nil }, func(string) {}); got != exitOK || !called {
		t.Fatalf("suggested recovery rejected: %s exit=%d", resume, got)
	}
}

func TestDatabaseUnsupportedParentGuidancePreservesOwnerLayout(t *testing.T) {
	var message string
	code := dispatchDatabaseProbe([]string{"probe-update-database"}, 0, func() error { return recoverypublication.ErrUnsupportedDatabaseParent }, func(v string) { message = v })
	for _, want := range []string{"/var/lib/celikpanel", "celikpanel:celikpanel", "0750", "has been preserved", "This check has not stopped services", "server owner", "preserve intentional settings", "retry from the panel", "compatible recovery version"} {
		if code != exitOutput || !strings.Contains(message, want) {
			t.Fatalf("missing actionable parent guidance %q: %d %s", want, code, message)
		}
	}
	for _, unsafe := range []string{"chmod", "chown", "readiness could not be verified", "filesystem attributes"} {
		if strings.Contains(message, unsafe) {
			t.Fatalf("unsupported layout misclassified or silently normalized: %s", message)
		}
	}
}
