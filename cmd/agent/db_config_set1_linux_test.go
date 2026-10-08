//go:build linux

package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

// Corrections from the first native measurement of settings writes
// (set1, 2026-10-08; evidence deploy/e2e/release-recovery/evidence/set1-20261010).

// rereadAnswer is what the running PostgreSQL prints for the re-read question,
// the way psql prints it with --tuples-only --no-align.
func rereadAnswer(file string, before, loaded string, signal string, firstError string) string {
	out := "file=" + file + "\nbefore=" + before + "\nsignal=" + signal + "\nwaited=1\nloaded=" + loaded + "\n"
	if firstError != "" {
		out += "error=" + firstError + "\n"
	}
	return out
}

// The measured defect (Debian 13, Ubuntu 24.04, Arch): an owner's drop-in makes
// the unit's reload fail after it has signalled the server. The first reload
// fails, the previous file is put back, the reload with it fails too, and the
// answer said the previous file "could not be put back with certainty" and
// named a copy of that same file as "the other version". The file WAS back,
// byte for byte, and the server ran the previous settings.
func TestReloadFailingTwiceWithThePreviousFileBackIsNotCalledNotRestored(t *testing.T) {
	fake := installDBConfigFakes(t)
	path := writeOwnerFile(t, "postgresql.conf", ownerPostgreSQLConf, 0o640)
	failed := errors.New("systemctl reload failed: exit status 1, output: Failed to reload set1-owner-pooler.service: Unit set1-owner-pooler.service not found.")
	fake.reloadErrors = []error{failed, failed}
	var filesWhenAsked []string
	fake.queryAnswer = func(statement string) (string, error) {
		filesWhenAsked = append(filesWhenAsked, readFileForTest(t, path))
		if !strings.Contains(statement, "pg_reload_conf()") || !strings.Contains(statement, "pg_conf_load_time()") ||
			!strings.Contains(statement, "pg_file_settings") || !strings.Contains(statement, "current_setting('config_file')") {
			t.Fatalf("the server was not told to re-read and asked what it loaded: %q", statement)
		}
		return rereadAnswer(path, "1791497283.412345", "1791497284.418822", "true", ""), nil
	}
	wanted := ownerPostgreSQLConf + "\nwork_mem = 16MB\n"

	_, err := applyDatabaseConfigUpdate(postgresTarget, dbConfigPreimage(t, path), []byte(wanted), fake.reload)
	rpcErr := wantRefusal(t, err, transport.ConfigErrorReloadFailed, transport.ConfigReloadRestoredUnitFailed)

	if got := readFileForTest(t, path); got != ownerPostgreSQLConf {
		t.Fatalf("the previous file is not back: %q", got)
	}
	// The server was asked once, after the previous file was back in place.
	if len(filesWhenAsked) != 1 || filesWhenAsked[0] != ownerPostgreSQLConf {
		t.Fatalf("the server was asked %d time(s), with the file %q", len(filesWhenAsked), filesWhenAsked)
	}
	// No "other version" exists, so none is named and none is left behind.
	if rpcErr.Name != "" {
		t.Fatalf("the answer names a copy although the file on disk is the previous file: %q", rpcErr.Name)
	}
	if beside := namesBeside(t, path); len(beside) != 0 {
		t.Fatalf("files left beside the configuration file: %v", beside)
	}
	if rpcErr.Unit != "postgresql@17-main" || !strings.Contains(rpcErr.Detail, "set1-owner-pooler.service") {
		t.Fatalf("the answer does not say which unit's reload fails and what it said: %+v", rpcErr)
	}
	if len(fake.reloads) != 2 {
		t.Fatalf("unit reloads = %v, want one with the new file and one with the previous", fake.reloads)
	}
}

// "It runs with the settings it had before" is a claim about the server, and is
// made only on the server's own reading. Every reading that falls short is the
// unknown reason: the file is back, and which settings run is not asserted.
func TestWhatTheServerRunsIsUnknownUnlessTheServerSaysSo(t *testing.T) {
	cases := map[string]func(path string) (string, error){
		"the server cannot be reached": func(string) (string, error) {
			return "", errors.New("psql: error: connection to server on socket failed")
		},
		"it did not read its files again after the signal": func(path string) (string, error) {
			return rereadAnswer(path, "1791497283.412345", "1791497200.000001", "true", ""), nil
		},
		"it reports an error in the files on disk": func(path string) (string, error) {
			return rereadAnswer(path, "1791497283.4", "1791497284.4", "true", "12:work_mem:invalid value for parameter"), nil
		},
		"the reload signal was not sent": func(path string) (string, error) {
			return rereadAnswer(path, "1791497283.4", "1791497284.4", "false", ""), nil
		},
		"the answer is about another cluster's file": func(string) (string, error) {
			return rereadAnswer("/etc/postgresql/16/other/postgresql.conf", "1791497283.4", "1791497284.4", "true", ""), nil
		},
		"the answer is not the one asked for": func(path string) (string, error) {
			return "file=" + path + "\n", nil
		},
	}
	for name, answer := range cases {
		t.Run(name, func(t *testing.T) {
			fake := installDBConfigFakes(t)
			path := writeOwnerFile(t, "postgresql.conf", ownerPostgreSQLConf, 0o640)
			fake.reloadErrors = []error{errors.New("reload failed"), errors.New("reload failed again")}
			fake.queryAnswer = func(string) (string, error) { return answer(path) }

			_, err := applyDatabaseConfigUpdate(postgresTarget, dbConfigPreimage(t, path), []byte(ownerPostgreSQLConf+"\nwork_mem = 16MB\n"), fake.reload)
			rpcErr := wantRefusal(t, err, transport.ConfigErrorReloadFailed, transport.ConfigReloadRestoredUnknown)
			if got := readFileForTest(t, path); got != ownerPostgreSQLConf {
				t.Fatalf("the previous file is not back: %q", got)
			}
			if rpcErr.Name != "" || len(namesBeside(t, path)) != 0 {
				t.Fatalf("a copy is named or left although the file on disk is the previous file: %+v %v", rpcErr, namesBeside(t, path))
			}
		})
	}
}

// pg_hba.conf: the same classification, asked about hba_file and
// pg_hba_file_rules.
func TestHBAReloadFailingTwiceAsksTheServerAboutTheHBAFile(t *testing.T) {
	fake := installDBConfigFakes(t)
	previous := "local all postgres peer\nhost all all 127.0.0.1/32 scram-sha-256\n"
	path := writeOwnerFile(t, "pg_hba.conf", previous, 0o640)
	fake.reloadErrors = []error{errors.New("reload failed"), errors.New("reload failed again")}
	var reread string
	fake.queryAnswer = func(statement string) (string, error) {
		if strings.Contains(statement, "pg_reload_conf()") {
			reread = statement
			return rereadAnswer(path, "10.5", "11.5", "true", ""), nil
		}
		// The question before the reload: the installed file has no error.
		return "file=" + path + "\n", nil
	}
	target := dbConfigTarget{kind: dbConfigHBA, unit: "postgresql@17-main"}
	wanted := previous + "host all all 10.99.0.0/24 scram-sha-256\n"

	_, err := applyDatabaseConfigUpdate(target, dbConfigPreimage(t, path), []byte(wanted), fake.reload)
	wantRefusal(t, err, transport.ConfigErrorReloadFailed, transport.ConfigReloadRestoredUnitFailed)
	if !strings.Contains(reread, "current_setting('hba_file')") || !strings.Contains(reread, "pg_hba_file_rules") {
		t.Fatalf("the server was not asked about pg_hba.conf: %q", reread)
	}
	if readFileForTest(t, path) != previous {
		t.Fatal("the previous pg_hba.conf is not back")
	}
}

// PostgreSQL reloads the new file and then reports an error in it: the previous
// file is put back. If the reload with it fails, the file is still back, and
// the answer must not say otherwise.
func TestPreviousFileBackAfterPostgresReportedAnErrorIsClassifiedTheSameWay(t *testing.T) {
	fake := installDBConfigFakes(t)
	path := writeOwnerFile(t, "postgresql.conf", ownerPostgreSQLConf, 0o644)
	fake.reloadErrors = []error{nil, errors.New("reload failed")}
	fake.queryAnswer = func(statement string) (string, error) {
		if strings.Contains(statement, "pg_reload_conf()") {
			return rereadAnswer(path, "10.5", "11.5", "true", ""), nil
		}
		return "file=" + path + "\nerror=13:ssl_cert_file:could not load server certificate file\n", nil
	}
	_, err := applyDatabaseConfigUpdate(postgresTarget, dbConfigPreimage(t, path), []byte(ownerPostgreSQLConf+"\nssl_cert_file = 'x'\n"), fake.reload)
	rpcErr := wantRefusal(t, err, transport.ConfigErrorReloadFailed, transport.ConfigReloadRestoredUnitFailed)
	if readFileForTest(t, path) != ownerPostgreSQLConf || rpcErr.Name != "" || len(namesBeside(t, path)) != 0 {
		t.Fatalf("refusal %+v, beside %v", rpcErr, namesBeside(t, path))
	}
}

// --- MariaDB: a value MariaDB adjusts is not "accepted" ------------------------

// What mariadbd 11.8.6 (Debian 13, set1 run-b, 2026-10-08 22:09:13Z) printed for
// a 50-server.cnf holding `max_connections = plenty`, with exit status 0.
const mariadbAdjustedOnDebian13 = "2026-10-08 22:09:13 0 [Warning] Buffered warning: option 'max_connections': unsigned value 0 adjusted to 10\n" +
	"2026-10-08 22:09:13 0 [Warning] Could not open mysql.plugin table: \"Table 'mysql.plugin' doesn't exist\". Some options may be missing from the help text\n"

// Warnings the same run prints that say nothing about a value of the file: the
// empty private data directory (every platform), and Ubuntu 24.04's and Debian's
// stock 50-server.cnf, which sets expire_logs_days without a binary log.
const mariadbHarmlessStockWarnings = "2026-10-08 21:37:31 0 [Warning] You need to use --log-bin to make --expire-logs-days or --binlog-expire-logs-seconds work.\n" +
	"2026-10-08 22:09:13 0 [Warning] Could not open mysql.plugin table: \"Table 'mysql.plugin' doesn't exist\". Some options may be missing from the help text\n" +
	"2026-10-08 22:09:13 0 [Warning] 'innodb-file-format' was removed. It does nothing now and exists only for compatibility with old my.cnf files.\n"

// mariadbRuns answers the validator per file: the validation copy and the file
// that is on the server now.
func mariadbRuns(t *testing.T, fake *dbConfigFakes, path, forCandidate, forCurrent string) {
	t.Helper()
	dbConfigRun = func(_ context.Context, _ string, name string, args ...string) (string, error) {
		fake.runs = append(fake.runs, append([]string{name}, args...))
		file := strings.TrimPrefix(args[0], "--defaults-file=")
		if file == path {
			return forCurrent, nil
		}
		if !strings.Contains(file, ".celikpanel-candidate-") {
			t.Fatalf("mariadbd was run on %q", file)
		}
		return forCandidate, nil
	}
}

func TestMariaDBValueItAdjustsIsRefusedWithItsOwnLine(t *testing.T) {
	fake := installDBConfigFakes(t)
	path := writeOwnerFile(t, "50-server.cnf", ownerMariaDBConf, 0o644)
	mariadbRuns(t, fake, path, mariadbAdjustedOnDebian13, mariadbHarmlessStockWarnings)
	wanted := strings.Replace(ownerMariaDBConf, "max_connections=150   # raised for the shop", "max_connections = plenty   # raised for the shop", 1)
	reload := func(string) error { t.Fatal("MariaDB was reloaded"); return nil }

	_, err := applyDatabaseConfigUpdate(dbConfigTarget{kind: dbConfigMariaDB, unit: "mariadb"}, dbConfigPreimage(t, path), []byte(wanted), reload)
	rpcErr := wantRefusal(t, err, transport.ConfigErrorValidationFail, transport.ConfigInvalidDaemon)
	if rpcErr.Detail != "option 'max_connections': unsigned value 0 adjusted to 10" || rpcErr.Name != "max_connections" {
		t.Fatalf("refusal = %+v, want MariaDB's own line and the option it names", rpcErr)
	}
	if readFileForTest(t, path) != ownerMariaDBConf || len(namesBeside(t, path)) != 0 {
		t.Fatalf("a refused file changed something: %v", namesBeside(t, path))
	}
}

func TestMariaDBStockWarningsAreNotRefusals(t *testing.T) {
	fake := installDBConfigFakes(t)
	path := writeOwnerFile(t, "50-server.cnf", ownerMariaDBConf, 0o644)
	mariadbRuns(t, fake, path, mariadbHarmlessStockWarnings, mariadbHarmlessStockWarnings)
	wanted := strings.Replace(ownerMariaDBConf, "max_connections=150   # raised for the shop", "max_connections = 300   # raised for the shop", 1)
	reload := func(string) error { t.Fatal("MariaDB was reloaded"); return nil }

	outcome, err := applyDatabaseConfigUpdate(dbConfigTarget{kind: dbConfigMariaDB, unit: "mariadb"}, dbConfigPreimage(t, path), []byte(wanted), reload)
	if err != nil {
		t.Fatalf("a harmless stock warning was taken for a refusal: %v", err)
	}
	if outcome.DaemonCheck != transport.ConfigDaemonAccepted || readFileForTest(t, path) != wanted {
		t.Fatalf("outcome = %+v", outcome)
	}
	// Nothing in the copy's output was about a value, so the current file was
	// not read a second time.
	if len(fake.runs) != 1 {
		t.Fatalf("validator runs = %d, want 1", len(fake.runs))
	}
}

// A value the file on the server already holds, and MariaDB already adjusts at
// every start, is not this change's: the owner must still be able to save
// another line.
func TestMariaDBWarningTheCurrentFileAlreadyProducesDoesNotRefuseAnotherChange(t *testing.T) {
	fake := installDBConfigFakes(t)
	path := writeOwnerFile(t, "50-server.cnf", ownerMariaDBConf, 0o644)
	already := "2026-10-08 22:09:13 0 [Warning] Buffered warning: option 'thread_cache_size': unsigned value 100000 adjusted to 16384\n"
	mariadbRuns(t, fake, path, already+mariadbHarmlessStockWarnings, already)
	wanted := strings.Replace(ownerMariaDBConf, "max_connections=150   # raised for the shop", "max_connections = 300   # raised for the shop", 1)

	if _, err := applyDatabaseConfigUpdate(dbConfigTarget{kind: dbConfigMariaDB, unit: "mariadb"}, dbConfigPreimage(t, path), []byte(wanted), func(string) error { return nil }); err != nil {
		t.Fatalf("a warning the current file already produces refused an unrelated change: %v", err)
	}
	if len(fake.runs) != 2 || fake.runs[1][1] != "--defaults-file="+path {
		t.Fatalf("validator runs = %v, want the copy and then the current file", fake.runs)
	}

	// A new adjusted value beside the old one is still refused.
	fake = installDBConfigFakes(t)
	path = writeOwnerFile(t, "50-server.cnf", ownerMariaDBConf, 0o644)
	mariadbRuns(t, fake, path, already+mariadbAdjustedOnDebian13, already)
	_, err := applyDatabaseConfigUpdate(dbConfigTarget{kind: dbConfigMariaDB, unit: "mariadb"}, dbConfigPreimage(t, path), []byte(wanted), func(string) error { return nil })
	if rpcErr := wantRefusal(t, err, transport.ConfigErrorValidationFail, transport.ConfigInvalidDaemon); rpcErr.Name != "max_connections" {
		t.Fatalf("refusal = %+v", rpcErr)
	}
}

func TestMariaDBValueWarningsAreRecognisedNarrowly(t *testing.T) {
	stderr := mariadbHarmlessStockWarnings +
		"2026-10-08 22:09:13 0 [Warning] Buffered warning: option 'max_connections': unsigned value 0 adjusted to 10\n" +
		"2026-10-08 22:09:13 0 [Warning] option 'wait_timeout': signed value -5 adjusted to 1\n" +
		"2026-10-08 22:09:13 0 [Warning] Buffered warning: option 'skip-name-resolve': boolean value 'maybe' wasn't recognized. Set to OFF.\n" +
		"2026-10-08 22:09:13 0 [Note] option 'x': adjusted to nothing, in a note\n"
	found := mariadbValueWarnings(stderr)
	var names []string
	for _, warning := range found {
		names = append(names, warning.name)
	}
	if strings.Join(names, ",") != "max_connections,wait_timeout,skip-name-resolve" {
		t.Fatalf("recognised %v", names)
	}
}
