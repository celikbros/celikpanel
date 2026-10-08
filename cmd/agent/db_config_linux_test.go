//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

// The owner's postgresql.conf as a Debian server holds it after some years:
// their own comments, a setting PostgreSQL knows and the Panel's screen does
// not, an include, a duplicate, odd spacing. Everything a write does not name
// must come back byte for byte.
const ownerPostgreSQLConf = "# -----------------------------\n" +
	"# PostgreSQL configuration file\n" +
	"# -----------------------------\n" +
	"\n" +
	"data_directory = '/var/lib/postgresql/17/main'\t\t# use data in another directory\n" +
	"#listen_addresses = 'localhost'\t\t# what IP address(es) to listen on;\n" +
	"max_connections = 100\t\t\t# (change requires restart)\n" +
	"   shared_buffers=128MB # tuned by hand, 2025-03\n" +
	"pg_stat_statements.track = all\n" +
	"include_dir = 'conf.d'\t\t\t# include files ending in '.conf' from\n" +
	"max_connections = 150\n" +
	"# end of the owner's part"

// dbConfigFakes replaces every program the database configuration path runs.
type dbConfigFakes struct {
	// validator
	runs      [][]string
	runDirs   []string
	runStderr string
	runFails  bool
	// what the candidate file held and how it was protected while it was read
	candidateContent string
	candidateMode    os.FileMode
	// service
	state        string
	reloads      []string
	reloadErrors []error
	unitLog      string
	// the running PostgreSQL
	queries     []string
	queryAnswer func(statement string) (string, error)
}

func installDBConfigFakes(t *testing.T) *dbConfigFakes {
	t.Helper()
	fake := &dbConfigFakes{state: dbUnitActive}
	oldLook, oldNow, oldStat := dbConfigLookPath, dbConfigNow, dbConfigStat
	oldRun, oldState, oldLog, oldQuery := dbConfigRun, dbConfigUnitState, dbConfigUnitLog, dbConfigPostgreSQLQuery
	t.Cleanup(func() {
		dbConfigLookPath, dbConfigNow, dbConfigStat = oldLook, oldNow, oldStat
		dbConfigRun, dbConfigUnitState, dbConfigUnitLog, dbConfigPostgreSQLQuery = oldRun, oldState, oldLog, oldQuery
	})
	dbConfigLookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	dbConfigStat = func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	dbConfigNow = func() time.Time { return time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC) }
	dbConfigRun = func(_ context.Context, dir, name string, args ...string) (string, error) {
		fake.runs = append(fake.runs, append([]string{name}, args...))
		fake.runDirs = append(fake.runDirs, dir)
		for _, arg := range args {
			for _, prefix := range []string{"config_file=", "--defaults-file="} {
				if candidate, ok := strings.CutPrefix(arg, prefix); ok {
					if data, err := os.ReadFile(candidate); err == nil {
						fake.candidateContent = string(data)
					}
					if info, err := os.Stat(candidate); err == nil {
						fake.candidateMode = info.Mode().Perm()
					}
				}
			}
		}
		if fake.runFails {
			// A real exit status, as the validating program gives.
			return fake.runStderr, exec.Command("sh", "-c", "exit 1").Run()
		}
		return fake.runStderr, nil
	}
	dbConfigUnitState = func(string) string { return fake.state }
	dbConfigUnitLog = func(string, time.Time) string { return fake.unitLog }
	dbConfigPostgreSQLQuery = func(statement string) (string, error) {
		fake.queries = append(fake.queries, statement)
		if fake.queryAnswer == nil {
			return "", errors.New("psql: could not connect")
		}
		return fake.queryAnswer(statement)
	}
	return fake
}

func (f *dbConfigFakes) reload(unit string) error {
	f.reloads = append(f.reloads, unit)
	if len(f.reloadErrors) == 0 {
		return nil
	}
	err := f.reloadErrors[0]
	f.reloadErrors = f.reloadErrors[1:]
	return err
}

// writeOwnerFile puts a file the way a package or the owner left it.
func writeOwnerFile(t *testing.T, name, content string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func dbConfigPreimage(t *testing.T, path string) dnsFileSnapshot {
	t.Helper()
	data, metadata, err := readDNSFileForSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	return dnsFileSnapshot{
		Path: path, Exists: true, Mode: uint32(metadata.Mode.Perm()),
		OwnerKnown: metadata.OwnerKnown, UID: metadata.UID, GID: metadata.GID,
		SHA256: digestDNSBytes(data), Data: data,
	}
}

func readFileForTest(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// namesBeside lists what stands in the file's directory, without the file.
func namesBeside(t *testing.T, path string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		if entry.Name() != filepath.Base(path) {
			names = append(names, entry.Name())
		}
	}
	return names
}

func wantRefusal(t *testing.T, err error, code transport.ConfigErrorCode, reason string) *transport.ConfigRPCError {
	t.Helper()
	rpcErr := configRPCError(err)
	if rpcErr == nil {
		t.Fatalf("err = %v, want the typed refusal %s/%s", err, code, reason)
	}
	if rpcErr.Code != code || rpcErr.Reason != reason {
		t.Fatalf("refusal = %+v, want %s/%s", rpcErr, code, reason)
	}
	return rpcErr
}

func allowConfigPath(t *testing.T) {
	t.Helper()
	old := configAuthorize
	t.Cleanup(func() { configAuthorize = old })
	configAuthorize = func(path string) (string, error) { return filepath.Clean(path), nil }
}

// --- the read ---------------------------------------------------------------

// The defect: an editor that could not read the file opened on nothing, and
// Save then wrote that nothing. A file that cannot be read is an error with no
// content; an empty file is a known, empty answer with a version.
func TestGetConfigAnswersAnUnreadableFileAsAnErrorNotAsEmpty(t *testing.T) {
	allowConfigPath(t)
	agent := &Agent{}
	dir := t.TempDir()

	var missing transport.ConfigResponse
	if err := agent.GetConfig(&transport.GetConfigArgs{Path: filepath.Join(dir, "postgresql.conf")}, &missing); err != nil {
		t.Fatal(err)
	}
	if missing.Error == nil || missing.Error.Code != transport.ConfigErrorUnreadable {
		t.Fatalf("a missing file was answered as %+v, want the unreadable error", missing)
	}
	if missing.Content != "" || missing.Version != "" {
		t.Fatalf("an unreadable file still carried content or a version: %+v", missing)
	}

	// A directory where the file should be is not a file that reads as empty.
	if err := os.Mkdir(filepath.Join(dir, "pg_hba.conf"), 0o755); err != nil {
		t.Fatal(err)
	}
	var notAFile transport.ConfigResponse
	if err := agent.GetConfig(&transport.GetConfigArgs{Path: filepath.Join(dir, "pg_hba.conf")}, &notAFile); err != nil {
		t.Fatal(err)
	}
	if notAFile.Error == nil || notAFile.Content != "" || notAFile.Version != "" {
		t.Fatalf("a directory was answered as %+v, want an error", notAFile)
	}

	empty := filepath.Join(dir, "empty.cnf")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	var known transport.ConfigResponse
	if err := agent.GetConfig(&transport.GetConfigArgs{Path: empty}, &known); err != nil {
		t.Fatal(err)
	}
	if known.Error != nil || known.Content != "" || known.Version != configVersion(nil) {
		t.Fatalf("an empty file = %+v, want a known empty answer with its version", known)
	}

	full := filepath.Join(dir, "my.cnf")
	if err := os.WriteFile(full, []byte("[mysqld]\nmax_connections = 60\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var read transport.ConfigResponse
	if err := agent.GetConfig(&transport.GetConfigArgs{Path: full}, &read); err != nil {
		t.Fatal(err)
	}
	if read.Error != nil || read.Content != "[mysqld]\nmax_connections = 60\n" ||
		read.Version != configVersion([]byte("[mysqld]\nmax_connections = 60\n")) {
		t.Fatalf("read = %+v", read)
	}
}

// --- the checks every write passes first --------------------------------------

func TestUpdateConfigRequiresTheVersionOfTheFileItReplaces(t *testing.T) {
	allowConfigPath(t)
	original := "worker_processes 1;\n"
	path := writeOwnerFile(t, "example.conf", original, 0o644)
	agent := &Agent{configReload: func(string) error { t.Fatal("a refused write reloaded a service"); return nil }}

	update := func(version string) *transport.ConfigRPCError {
		var reply transport.UpdateConfigResponse
		if err := agent.UpdateConfig(&transport.UpdateConfigArgs{Path: path, Content: "worker_processes 2;\n", Version: version}, &reply); err != nil {
			t.Fatal(err)
		}
		if reply.Success {
			t.Fatalf("version %q: the write was accepted", version)
		}
		if got := readFileForTest(t, path); got != original {
			t.Fatalf("version %q: the file was changed to %q", version, got)
		}
		return reply.Error
	}
	if rpcErr := update(""); rpcErr == nil || rpcErr.Code != transport.ConfigErrorVersionRequired {
		t.Fatalf("no version: %+v, want version_required", rpcErr)
	}
	if rpcErr := update(configVersion([]byte("an older file\n"))); rpcErr == nil || rpcErr.Code != transport.ConfigErrorChanged {
		t.Fatalf("stale version: %+v, want changed", rpcErr)
	}

	// The file cannot be read at the moment of the write: unknown, not empty,
	// and nothing is created in its place.
	gone := filepath.Join(filepath.Dir(path), "gone.conf")
	var reply transport.UpdateConfigResponse
	if err := agent.UpdateConfig(&transport.UpdateConfigArgs{Path: gone, Content: "x = 1\n", Version: configVersion(nil)}, &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Error == nil || reply.Error.Code != transport.ConfigErrorUnreadable {
		t.Fatalf("an unreadable pre-image: %+v, want unreadable", reply.Error)
	}
	if _, err := os.Stat(gone); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a file was created over an unreadable pre-image: %v", err)
	}
}

func TestUpdateConfigRefusesEmptyAndMalformedContent(t *testing.T) {
	original := ownerPostgreSQLConf
	path := writeOwnerFile(t, "postgresql.conf", original, 0o644)
	version := configVersion([]byte(original))
	noReload := func(string) error { t.Fatal("a refused write reloaded a service"); return nil }
	cases := map[string]struct {
		content []byte
		reason  string
	}{
		"empty":                            {nil, transport.ConfigInvalidEmpty},
		"only blank space":                 {[]byte(" \n\t\n"), transport.ConfigInvalidEmpty},
		"a NUL byte":                       {[]byte("max_connections = 1\x00\n"), transport.ConfigInvalidShape},
		"larger than a configuration file": {bytes.Repeat([]byte("# x\n"), configMaxBytes/4+1), transport.ConfigInvalidShape},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := updateManagedConfig(path, tc.content, version, noReload)
			wantRefusal(t, err, transport.ConfigErrorValidationFail, tc.reason)
			if got := readFileForTest(t, path); got != original {
				t.Fatalf("the file was changed to %q", got)
			}
			if beside := namesBeside(t, path); len(beside) != 0 {
				t.Fatalf("a refused write left files behind: %v", beside)
			}
		})
	}
}

func TestUpdateConfigWithTheSameContentWritesNothing(t *testing.T) {
	path := writeOwnerFile(t, "postgresql.conf", ownerPostgreSQLConf, 0o640)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := updateManagedConfig(path, []byte(ownerPostgreSQLConf), configVersion([]byte(ownerPostgreSQLConf)),
		func(string) error { t.Fatal("an unchanged file reloaded a service"); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if !outcome.Unchanged || outcome.Version != configVersion([]byte(ownerPostgreSQLConf)) || outcome.Backup != "" {
		t.Fatalf("outcome = %+v, want unchanged with the same version and no backup", outcome)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) || len(namesBeside(t, path)) != 0 {
		t.Fatal("the file was rewritten, or something was left beside it")
	}
}

func TestDatabaseConfigTargets(t *testing.T) {
	cases := map[string]dbConfigTarget{
		"/etc/postgresql/17/main/postgresql.conf":   {kind: dbConfigPostgreSQL, unit: "postgresql@17-main", postgres: "/usr/lib/postgresql/17/bin/postgres"},
		"/etc/postgresql/15/main/pg_hba.conf":       {kind: dbConfigHBA, unit: "postgresql@15-main", postgres: "/usr/lib/postgresql/15/bin/postgres"},
		"/etc/postgresql/9.6/web-1/postgresql.conf": {kind: dbConfigPostgreSQL, unit: "postgresql@9.6-web-1", postgres: "/usr/lib/postgresql/9.6/bin/postgres"},
		"/var/lib/postgres/data/postgresql.conf":    {kind: dbConfigPostgreSQL, unit: "postgresql"},
		"/var/lib/pgsql/data/pg_hba.conf":           {kind: dbConfigHBA, unit: "postgresql"},
		"/etc/mysql/mariadb.conf.d/50-server.cnf":   {kind: dbConfigMariaDB, unit: "mariadb"},
		"/etc/mysql/my.cnf":                         {kind: dbConfigMariaDB, unit: "mariadb"},
		"/etc/my.cnf":                               {kind: dbConfigMariaDB, unit: "mariadb"},
	}
	for path, want := range cases {
		got, ok := dbConfigTargetFor(path)
		if !ok || got != want {
			t.Errorf("%s = %+v (%v), want %+v", path, got, ok, want)
		}
	}
	for _, path := range []string{
		"/etc/nginx/nginx.conf", "/etc/postfix/main.cf", "/etc/postgresql/17/main/pg_ident.conf",
		"/etc/postgresql/17/main/conf.d/postgresql.conf", "/etc/postgresql/../17/main/postgresql.conf",
		"/etc/mysql/debian.cnf.bak", "/srv/postgresql.conf",
	} {
		if got, ok := dbConfigTargetFor(path); ok {
			t.Errorf("%s was taken for a database configuration file: %+v", path, got)
		}
	}
}

// --- postgresql.conf ------------------------------------------------------------

var postgresTarget = dbConfigTarget{kind: dbConfigPostgreSQL, unit: "postgresql@17-main", postgres: "/usr/lib/postgresql/17/bin/postgres"}

func TestPostgreSQLConfIsValidatedInstalledBackedUpAndReloaded(t *testing.T) {
	fake := installDBConfigFakes(t)
	path := writeOwnerFile(t, "postgresql.conf", ownerPostgreSQLConf, 0o640)
	fake.queryAnswer = func(string) (string, error) {
		return "file=" + path + "\nrestart=max_connections\nrestart=shared_buffers\n", nil
	}
	// The one line the owner changed on the screen; every other byte is theirs.
	wanted := strings.Replace(ownerPostgreSQLConf, "   shared_buffers=128MB # tuned by hand, 2025-03", "   shared_buffers = 256MB # tuned by hand, 2025-03", 1)

	outcome, err := applyDatabaseConfigUpdate(postgresTarget, dbConfigPreimage(t, path), []byte(wanted), fake.reload)
	if err != nil {
		t.Fatal(err)
	}
	if got := readFileForTest(t, path); got != wanted {
		t.Fatalf("installed file differs from what was sent:\n got %q\nwant %q", got, wanted)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %v, want the owner's 0640", info.Mode().Perm())
	}

	// The validator: the cluster's own postgres is not on this test host, so
	// the one on PATH; -C first; the copy next to the file, with its mode.
	if len(fake.runs) != 1 {
		t.Fatalf("validator runs = %v", fake.runs)
	}
	run := fake.runs[0]
	if run[0] != "/usr/bin/postgres" || run[1] != "-C" || run[3] != "-D" || run[4] != filepath.Dir(path) {
		t.Fatalf("validator command = %v", run)
	}
	candidate := strings.TrimPrefix(run[6], "config_file=")
	if filepath.Dir(candidate) != filepath.Dir(path) || !strings.HasPrefix(filepath.Base(candidate), ".postgresql.conf.celikpanel-candidate-") {
		t.Fatalf("validation copy = %q, want a dot file next to %s", candidate, path)
	}
	if fake.candidateContent != wanted || fake.candidateMode != 0o640 {
		t.Fatalf("the validator read %q with mode %v", fake.candidateContent, fake.candidateMode)
	}
	if _, err := os.Stat(candidate); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the validation copy was left behind")
	}

	// The backup: the previous bytes, the same mode, a name no include reads.
	wantBackup := path + ".celikpanel-backup-20261009T120000Z"
	if outcome.Backup != wantBackup || readFileForTest(t, wantBackup) != ownerPostgreSQLConf {
		t.Fatalf("backup = %q", outcome.Backup)
	}
	if info, _ := os.Stat(wantBackup); info.Mode().Perm() != 0o640 {
		t.Fatalf("backup mode = %v, want 0640", info.Mode().Perm())
	}
	if strings.HasSuffix(wantBackup, ".conf") || strings.HasSuffix(wantBackup, ".cnf") {
		t.Fatal("an include directive would read the backup")
	}

	if len(fake.reloads) != 1 || fake.reloads[0] != "postgresql@17-main" {
		t.Fatalf("reloads = %v, want one reload of the cluster's unit", fake.reloads)
	}
	if outcome.Applied != transport.ConfigAppliedReloaded || outcome.DaemonCheck != transport.ConfigDaemonAccepted ||
		outcome.Version != configVersion([]byte(wanted)) || strings.Join(outcome.RestartRequired, ",") != "max_connections,shared_buffers" {
		t.Fatalf("outcome = %+v", outcome)
	}
}

func TestPostgreSQLConfRefusedByPostgresChangesNothing(t *testing.T) {
	// The lines are the ones PostgreSQL 17.11 printed for these files.
	cases := map[string]struct {
		stderr, detail, name string
		line                 int
	}{
		"an unknown setting": {
			stderr: "2026-10-08 02:53:32.875 GMT [1170] LOG:  skipping missing configuration file \"/var/lib/postgresql/17/main/postgresql.auto.conf\"\n" +
				"2026-10-08 02:53:32.875 GMT [1170] LOG:  unrecognized configuration parameter \"no_such_setting\" in file \"CANDIDATE\" line 854\n" +
				"2026-10-08 02:53:32.875 GMT [1170] FATAL:  configuration file \"CANDIDATE\" contains errors\n",
			detail: `unrecognized configuration parameter "no_such_setting" in file "PATH" line 854`, name: "no_such_setting", line: 854,
		},
		"a value of the wrong kind": {
			stderr: "2026-10-08 02:53:32.886 GMT [1173] LOG:  invalid value for parameter \"max_connections\": \"lots\"\n" +
				"2026-10-08 02:53:32.886 GMT [1173] FATAL:  configuration file \"CANDIDATE\" contains errors\n",
			detail: `invalid value for parameter "max_connections": "lots"`, name: "max_connections",
		},
		"a syntax error": {
			stderr: "2026-10-08 02:53:32.898 GMT [1176] LOG:  syntax error in file \"CANDIDATE\" line 12, near token \"'\"\n" +
				"2026-10-08 02:53:32.898 GMT [1176] FATAL:  configuration file \"CANDIDATE\" contains errors\n",
			detail: `syntax error in file "PATH" line 12, near token "'"`, line: 12,
		},
		"a value out of range": {
			stderr: "2026-10-08 02:53:32.909 GMT [1179] LOG:  99999999 is outside the valid range for parameter \"port\" (1 .. 65535)\n" +
				"2026-10-08 02:53:32.909 GMT [1179] FATAL:  configuration file \"CANDIDATE\" contains errors\n",
			detail: `99999999 is outside the valid range for parameter "port" (1 .. 65535)`, name: "port",
		},
		"only the closing line": {
			stderr: "2026-10-08 02:53:32.909 GMT [1179] FATAL:  configuration file \"CANDIDATE\" contains errors\n",
			detail: `configuration file "PATH" contains errors`,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			fake := installDBConfigFakes(t)
			path := writeOwnerFile(t, "postgresql.conf", ownerPostgreSQLConf, 0o644)
			fake.runFails = true
			original := dbConfigRun
			dbConfigRun = func(ctx context.Context, dir, program string, args ...string) (string, error) {
				_, err := original(ctx, dir, program, args...)
				candidate := strings.TrimPrefix(args[5], "config_file=")
				return strings.ReplaceAll(tc.stderr, "CANDIDATE", candidate), err
			}

			_, err := applyDatabaseConfigUpdate(postgresTarget, dbConfigPreimage(t, path), []byte(ownerPostgreSQLConf+"\nno_such_setting = 1\n"), fake.reload)
			rpcErr := wantRefusal(t, err, transport.ConfigErrorValidationFail, transport.ConfigInvalidDaemon)
			if want := strings.ReplaceAll(tc.detail, "PATH", path); rpcErr.Detail != want || rpcErr.Line != tc.line || rpcErr.Name != tc.name {
				t.Fatalf("refusal = %+v\n want detail %q line %d name %q", rpcErr, want, tc.line, tc.name)
			}
			if got := readFileForTest(t, path); got != ownerPostgreSQLConf {
				t.Fatalf("the file was changed: %q", got)
			}
			if beside := namesBeside(t, path); len(beside) != 0 {
				t.Fatalf("a refused write left files behind: %v", beside)
			}
			if len(fake.reloads) != 0 || len(fake.queries) != 0 {
				t.Fatalf("a refused write reached the service: reloads %v, queries %d", fake.reloads, len(fake.queries))
			}
		})
	}
}

func TestDatabaseConfigIsNotInstalledWithoutItsValidator(t *testing.T) {
	fake := installDBConfigFakes(t)
	dbConfigLookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	for name, target := range map[string]dbConfigTarget{"postgresql.conf": postgresTarget, "50-server.cnf": {kind: dbConfigMariaDB, unit: "mariadb"}} {
		path := writeOwnerFile(t, name, "max_connections = 100\n", 0o644)
		_, err := applyDatabaseConfigUpdate(target, dbConfigPreimage(t, path), []byte("max_connections = 200\n"), fake.reload)
		wantRefusal(t, err, transport.ConfigErrorValidationFail, transport.ConfigInvalidNoValidator)
		if got := readFileForTest(t, path); got != "max_connections = 100\n" {
			t.Fatalf("%s was changed without being checked: %q", name, got)
		}
		if beside := namesBeside(t, path); len(beside) != 0 {
			t.Fatalf("%s: files left behind: %v", name, beside)
		}
	}
	if len(fake.runs) != 0 || len(fake.reloads) != 0 {
		t.Fatalf("runs %v reloads %v", fake.runs, fake.reloads)
	}

	// A program that cannot be started at all is the same answer: unchecked.
	dbConfigLookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	dbConfigRun = func(context.Context, string, string, ...string) (string, error) {
		return "", errors.New("fork/exec /usr/bin/postgres: exec format error")
	}
	path := writeOwnerFile(t, "postgresql.conf", "max_connections = 100\n", 0o644)
	_, err := applyDatabaseConfigUpdate(postgresTarget, dbConfigPreimage(t, path), []byte("max_connections = 200\n"), fake.reload)
	wantRefusal(t, err, transport.ConfigErrorValidationFail, transport.ConfigInvalidNoValidator)
}

func TestFailedReloadPutsThePreviousFileBack(t *testing.T) {
	fake := installDBConfigFakes(t)
	path := writeOwnerFile(t, "postgresql.conf", ownerPostgreSQLConf, 0o640)
	fake.reloadErrors = []error{errors.New("systemctl reload failed: exit status 1, output: Job for postgresql@17-main.service failed.\nSee \"systemctl status\"")}
	fake.unitLog = "Reloading PostgreSQL Cluster 17-main...\n" +
		"Error: could not exec /usr/lib/postgresql/17/bin/pg_ctl reload: password=hunter2 rejected\n" +
		"postgresql@17-main.service: Control process exited, code=exited, status=1/FAILURE\n"
	var seenAtReload []string
	reload := func(unit string) error {
		seenAtReload = append(seenAtReload, readFileForTest(t, path))
		return fake.reload(unit)
	}
	wanted := ownerPostgreSQLConf + "\nwork_mem = 8MB\n"

	_, err := applyDatabaseConfigUpdate(postgresTarget, dbConfigPreimage(t, path), []byte(wanted), reload)
	rpcErr := wantRefusal(t, err, transport.ConfigErrorReloadFailed, transport.ConfigReloadRestored)
	if !strings.HasPrefix(rpcErr.Detail, "Error: could not exec /usr/lib/postgresql/17/bin/pg_ctl reload") {
		t.Fatalf("detail = %q, want the first line of the unit's journal that names a failure", rpcErr.Detail)
	}
	if strings.Contains(rpcErr.Detail, "hunter2") {
		t.Fatalf("the detail carries a password: %q", rpcErr.Detail)
	}
	if got := readFileForTest(t, path); got != ownerPostgreSQLConf {
		t.Fatalf("the previous file is not back: %q", got)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o640 {
		t.Fatalf("mode after the restore = %v", info.Mode().Perm())
	}
	// Reloaded once with the new file, once more with the previous one.
	if len(seenAtReload) != 2 || seenAtReload[0] != wanted || seenAtReload[1] != ownerPostgreSQLConf {
		t.Fatalf("files at each reload = %q", seenAtReload)
	}
	if beside := namesBeside(t, path); len(beside) != 0 {
		t.Fatalf("after a restore nothing needs to stay beside the file: %v", beside)
	}
}

func TestFailedReloadThatCannotBeUndoneSaysSoAndKeepsTheOtherVersion(t *testing.T) {
	fake := installDBConfigFakes(t)
	path := writeOwnerFile(t, "postgresql.conf", ownerPostgreSQLConf, 0o640)
	fake.reloadErrors = []error{errors.New("reload failed"), errors.New("reload failed again")}

	_, err := applyDatabaseConfigUpdate(postgresTarget, dbConfigPreimage(t, path), []byte(ownerPostgreSQLConf+"\nwork_mem = 8MB\n"), fake.reload)
	rpcErr := wantRefusal(t, err, transport.ConfigErrorReloadFailed, transport.ConfigReloadNotRestored)
	if got := readFileForTest(t, path); got != ownerPostgreSQLConf {
		t.Fatalf("the previous file is not back on disk: %q", got)
	}
	if rpcErr.Name == "" || readFileForTest(t, rpcErr.Name) != ownerPostgreSQLConf {
		t.Fatalf("the answer does not name a kept copy: %+v", rpcErr)
	}

	// The owner edited the file after the Panel installed its version: the
	// restore must not undo their edit.
	fake = installDBConfigFakes(t)
	path = writeOwnerFile(t, "postgresql.conf", ownerPostgreSQLConf, 0o640)
	ownerEdit := ownerPostgreSQLConf + "\n# edited on the server meanwhile\n"
	reload := func(string) error {
		if err := os.WriteFile(path, []byte(ownerEdit), 0o640); err != nil {
			t.Fatal(err)
		}
		return errors.New("reload failed")
	}
	_, err = applyDatabaseConfigUpdate(postgresTarget, dbConfigPreimage(t, path), []byte(ownerPostgreSQLConf+"\nwork_mem = 8MB\n"), reload)
	rpcErr = wantRefusal(t, err, transport.ConfigErrorReloadFailed, transport.ConfigReloadNotRestored)
	if got := readFileForTest(t, path); got != ownerEdit {
		t.Fatalf("the owner's later edit was overwritten by the restore: %q", got)
	}
	if readFileForTest(t, rpcErr.Name) != ownerPostgreSQLConf {
		t.Fatal("the previous version was not kept")
	}
}

func TestStoppedServiceIsNotReloaded(t *testing.T) {
	fake := installDBConfigFakes(t)
	fake.state = dbUnitInactive
	path := writeOwnerFile(t, "postgresql.conf", ownerPostgreSQLConf, 0o644)
	wanted := ownerPostgreSQLConf + "\nwork_mem = 8MB\n"
	outcome, err := applyDatabaseConfigUpdate(postgresTarget, dbConfigPreimage(t, path), []byte(wanted), fake.reload)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Applied != transport.ConfigAppliedNotRunning || len(fake.reloads) != 0 || len(fake.queries) != 0 {
		t.Fatalf("outcome = %+v, reloads %v, queries %d", outcome, fake.reloads, len(fake.queries))
	}
	if readFileForTest(t, path) != wanted {
		t.Fatal("the validated file was not installed")
	}
}

func TestPostgresReportingAnErrorAfterTheReloadPutsTheFileBack(t *testing.T) {
	fake := installDBConfigFakes(t)
	path := writeOwnerFile(t, "postgresql.conf", ownerPostgreSQLConf, 0o644)
	fake.queryAnswer = func(string) (string, error) {
		return "file=" + path + "\nerror=13:ssl_cert_file:could not load server certificate file\n", nil
	}
	_, err := applyDatabaseConfigUpdate(postgresTarget, dbConfigPreimage(t, path), []byte(ownerPostgreSQLConf+"\nssl_cert_file = 'x'\n"), fake.reload)
	rpcErr := wantRefusal(t, err, transport.ConfigErrorValidationFail, transport.ConfigInvalidDaemon)
	if rpcErr.Line != 13 || rpcErr.Name != "ssl_cert_file" || rpcErr.Detail != "could not load server certificate file" {
		t.Fatalf("refusal = %+v", rpcErr)
	}
	if readFileForTest(t, path) != ownerPostgreSQLConf || len(fake.reloads) != 2 {
		t.Fatalf("the previous file was not put back and reloaded: reloads %v", fake.reloads)
	}

	// An answer about another cluster's file says nothing about this one.
	fake = installDBConfigFakes(t)
	path = writeOwnerFile(t, "postgresql.conf", ownerPostgreSQLConf, 0o644)
	fake.queryAnswer = func(string) (string, error) {
		return "file=/etc/postgresql/16/other/postgresql.conf\nerror=1:x:broken\nrestart=shared_buffers\n", nil
	}
	outcome, err := applyDatabaseConfigUpdate(postgresTarget, dbConfigPreimage(t, path), []byte(ownerPostgreSQLConf+"\nwork_mem = 8MB\n"), fake.reload)
	if err != nil || len(outcome.RestartRequired) != 0 || outcome.Applied != transport.ConfigAppliedReloaded {
		t.Fatalf("outcome = %+v, err = %v", outcome, err)
	}
}

// The file changed on the server between the read and the install (an owner at
// the console): the install is conditional on the bytes that were read.
func TestDatabaseConfigIsNotInstalledOverAFileThatChangedMeanwhile(t *testing.T) {
	fake := installDBConfigFakes(t)
	path := writeOwnerFile(t, "postgresql.conf", ownerPostgreSQLConf, 0o644)
	pre := dbConfigPreimage(t, path)
	ownerEdit := ownerPostgreSQLConf + "\n# edited on the server meanwhile\n"
	original := dbConfigRun
	dbConfigRun = func(ctx context.Context, dir, program string, args ...string) (string, error) {
		if err := os.WriteFile(path, []byte(ownerEdit), 0o644); err != nil {
			t.Fatal(err)
		}
		return original(ctx, dir, program, args...)
	}
	_, err := applyDatabaseConfigUpdate(postgresTarget, pre, []byte(ownerPostgreSQLConf+"\nwork_mem = 8MB\n"), fake.reload)
	if rpcErr := configRPCError(err); rpcErr == nil || rpcErr.Code != transport.ConfigErrorChanged {
		t.Fatalf("err = %v, want the changed refusal", err)
	}
	if got := readFileForTest(t, path); got != ownerEdit {
		t.Fatalf("the owner's edit was replaced: %q", got)
	}
	if beside := namesBeside(t, path); len(beside) != 0 || len(fake.reloads) != 0 {
		t.Fatalf("left behind %v, reloads %v", beside, fake.reloads)
	}
}

// --- MariaDB --------------------------------------------------------------------

const ownerMariaDBConf = "# The MariaDB server, tuned 2024\n" +
	"[server]\n\n[mysqld]\n" +
	"pid-file                = /run/mysqld/mysqld.pid\n" +
	"bind-address            = 127.0.0.1\n" +
	";key_buffer_size        = 128M\n" +
	"max_connections=150   # raised for the shop\n" +
	"!includedir /etc/mysql/extra.d/\n" +
	"[mariadb-11.8]\nplugin-load-add = auth_socket\n"

func TestMariaDBOptionFileIsValidatedInstalledAndNotReloaded(t *testing.T) {
	fake := installDBConfigFakes(t)
	path := writeOwnerFile(t, "50-server.cnf", ownerMariaDBConf, 0o644)
	wanted := strings.Replace(ownerMariaDBConf, "max_connections=150   # raised for the shop", "max_connections = 300   # raised for the shop", 1)
	reload := func(string) error {
		t.Fatal("MariaDB was reloaded: it does not re-read its option files on a reload")
		return nil
	}
	outcome, err := applyDatabaseConfigUpdate(dbConfigTarget{kind: dbConfigMariaDB, unit: "mariadb"}, dbConfigPreimage(t, path), []byte(wanted), reload)
	if err != nil {
		t.Fatal(err)
	}
	if readFileForTest(t, path) != wanted {
		t.Fatal("the installed file differs from what was sent")
	}
	if outcome.Applied != transport.ConfigAppliedRestartRequired || outcome.DaemonCheck != transport.ConfigDaemonAccepted ||
		readFileForTest(t, outcome.Backup) != ownerMariaDBConf {
		t.Fatalf("outcome = %+v", outcome)
	}
	// --defaults-file first, then a private data directory that wins over the
	// file's own, so the real one is never opened.
	run := fake.runs[0]
	if run[0] != "/usr/bin/mariadbd" || !strings.HasPrefix(run[1], "--defaults-file="+filepath.Dir(path)+"/.50-server.cnf.celikpanel-candidate-") ||
		!strings.HasPrefix(run[2], "--datadir=") || run[3] != "--help" || run[4] != "--verbose" {
		t.Fatalf("validator command = %v", run)
	}
	private := strings.TrimPrefix(run[2], "--datadir=")
	if private == "" || fake.runDirs[0] != private {
		t.Fatalf("data directory %q, working directory %q", private, fake.runDirs[0])
	}
	if _, err := os.Stat(private); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the private data directory was left behind")
	}
	if strings.HasSuffix(strings.TrimPrefix(run[1], "--defaults-file="), ".cnf") {
		t.Fatal("an !includedir would read the validation copy")
	}
}

func TestMariaDBOptionFileRefusedByMariaDBChangesNothing(t *testing.T) {
	// The lines are the ones mariadbd 11.8.6 printed for these files.
	cases := map[string]struct {
		stderr, detail, name string
		line                 int
	}{
		"an unknown variable": {
			stderr: "2026-10-08  5:53:33 0 [Warning] Could not open mysql.plugin table: \"Table 'mysql.plugin' doesn't exist\". Some options may be missing from the help text\n" +
				"2026-10-08  5:53:33 0 [ERROR] /usr/sbin/mariadbd: unknown variable 'no_such_variable=3'\n",
			detail: "unknown variable 'no_such_variable=3'", name: "no_such_variable",
		},
		"a value it cannot use": {
			stderr: "2026-10-08  5:53:33 0 [ERROR] Buffered error: Unknown suffix 'l' used for variable 'max_connections' (value 'lots'). Legal suffix characters are: K, M, G, T, P, E\n" +
				"2026-10-08  5:53:33 0 [ERROR] Buffered error: /usr/sbin/mariadbd: Error while setting value 'lots' to 'max_connections'\n",
			detail: "Unknown suffix 'l' used for variable 'max_connections' (value 'lots'). Legal suffix characters are: K, M, G, T, P, E", name: "max_connections",
		},
		"a broken group header": {
			stderr: "error: Wrong group definition in config file: CANDIDATE at line 4\nFatal error in defaults handling. Program aborted\n",
			detail: "Wrong group definition in config file: PATH at line 4", line: 4,
		},
		"an option before any group": {
			stderr: "error: Found option without preceding group in config file: CANDIDATE at line: 1\nFatal error in defaults handling. Program aborted\n",
			detail: "Found option without preceding group in config file: PATH at line: 1", line: 1,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			fake := installDBConfigFakes(t)
			path := writeOwnerFile(t, "50-server.cnf", ownerMariaDBConf, 0o644)
			fake.runFails = true
			original := dbConfigRun
			dbConfigRun = func(ctx context.Context, dir, program string, args ...string) (string, error) {
				_, err := original(ctx, dir, program, args...)
				return strings.ReplaceAll(tc.stderr, "CANDIDATE", strings.TrimPrefix(args[0], "--defaults-file=")), err
			}
			_, err := applyDatabaseConfigUpdate(dbConfigTarget{kind: dbConfigMariaDB, unit: "mariadb"}, dbConfigPreimage(t, path),
				[]byte(ownerMariaDBConf+"no_such_variable = 3\n"), fake.reload)
			rpcErr := wantRefusal(t, err, transport.ConfigErrorValidationFail, transport.ConfigInvalidDaemon)
			if want := strings.ReplaceAll(tc.detail, "PATH", path); rpcErr.Detail != want || rpcErr.Name != tc.name || rpcErr.Line != tc.line {
				t.Fatalf("refusal = %+v\n want detail %q name %q line %d", rpcErr, want, tc.name, tc.line)
			}
			if readFileForTest(t, path) != ownerMariaDBConf || len(namesBeside(t, path)) != 0 {
				t.Fatal("the file was changed, or something was left beside it")
			}
		})
	}
}

// --- pg_hba.conf ----------------------------------------------------------------

var hbaTarget = dbConfigTarget{kind: dbConfigHBA, unit: "postgresql@17-main", postgres: "/usr/lib/postgresql/17/bin/postgres"}

func TestHBAIsRefusedBeforeAnythingIsWritten(t *testing.T) {
	fake := installDBConfigFakes(t)
	path := writeOwnerFile(t, "pg_hba.conf", debianHBA, 0o640)
	headerOnly := "# PostgreSQL Client Authentication Configuration File\n# Managed by CelikPanel\n#\n# TYPE  DATABASE        USER            ADDRESS                 METHOD\n"

	_, err := applyDatabaseConfigUpdate(hbaTarget, dbConfigPreimage(t, path), []byte(headerOnly), fake.reload)
	wantRefusal(t, err, transport.ConfigErrorValidationFail, transport.ConfigInvalidLockout)

	_, err = applyDatabaseConfigUpdate(hbaTarget, dbConfigPreimage(t, path), []byte(debianHBA+"host all app 192.0.2.0/24\n"), fake.reload)
	rpcErr := wantRefusal(t, err, transport.ConfigErrorValidationFail, transport.ConfigInvalidSyntax)
	if rpcErr.Line != strings.Count(debianHBA, "\n")+1 || rpcErr.Detail != "end-of-line before authentication method" {
		t.Fatalf("refusal = %+v", rpcErr)
	}

	if readFileForTest(t, path) != debianHBA || len(namesBeside(t, path)) != 0 {
		t.Fatal("a refused pg_hba.conf changed the file or left something beside it")
	}
	if len(fake.runs) != 0 || len(fake.reloads) != 0 || len(fake.queries) != 0 {
		t.Fatalf("a refused pg_hba.conf reached a program: %v %v %d", fake.runs, fake.reloads, len(fake.queries))
	}
}

func TestHBAIsShownToTheRunningServerBeforeItIsLoaded(t *testing.T) {
	wanted := debianHBA + "hostssl all app 192.0.2.0/24 scram-sha-256\n"

	t.Run("the server refuses the installed file: it is put back and never loaded", func(t *testing.T) {
		fake := installDBConfigFakes(t)
		path := writeOwnerFile(t, "pg_hba.conf", debianHBA, 0o640)
		var seenByServer string
		fake.queryAnswer = func(string) (string, error) {
			seenByServer = readFileForTest(t, path)
			return "file=" + path + "\nerror=18:hostssl record cannot match because SSL is disabled\n", nil
		}
		_, err := applyDatabaseConfigUpdate(hbaTarget, dbConfigPreimage(t, path), []byte(wanted), fake.reload)
		rpcErr := wantRefusal(t, err, transport.ConfigErrorValidationFail, transport.ConfigInvalidDaemon)
		if rpcErr.Line != 18 || rpcErr.Detail != "hostssl record cannot match because SSL is disabled" {
			t.Fatalf("refusal = %+v", rpcErr)
		}
		if seenByServer != wanted {
			t.Fatal("the server was not asked about the new file")
		}
		if readFileForTest(t, path) != debianHBA || len(fake.reloads) != 0 || len(namesBeside(t, path)) != 0 {
			t.Fatalf("after the refusal: reloads %v, beside %v", fake.reloads, namesBeside(t, path))
		}
	})

	t.Run("the server accepts it: reloaded, with a backup", func(t *testing.T) {
		fake := installDBConfigFakes(t)
		path := writeOwnerFile(t, "pg_hba.conf", debianHBA, 0o640)
		fake.queryAnswer = func(string) (string, error) { return "file=" + path + "\n", nil }
		outcome, err := applyDatabaseConfigUpdate(hbaTarget, dbConfigPreimage(t, path), []byte(wanted), fake.reload)
		if err != nil {
			t.Fatal(err)
		}
		if outcome.DaemonCheck != transport.ConfigDaemonAccepted || outcome.Applied != transport.ConfigAppliedReloaded ||
			len(fake.reloads) != 1 || readFileForTest(t, path) != wanted || readFileForTest(t, outcome.Backup) != debianHBA {
			t.Fatalf("outcome = %+v, reloads %v", outcome, fake.reloads)
		}
		if info, _ := os.Stat(path); info.Mode().Perm() != 0o640 {
			t.Fatalf("mode = %v, want the owner's 0640", info.Mode().Perm())
		}
		if len(fake.runs) != 0 {
			t.Fatalf("pg_hba.conf has no validating program, yet one was run: %v", fake.runs)
		}
	})

	for name, answer := range map[string]func(string) (string, error){
		"the server cannot be asked": nil,
		"the answer is about another cluster": func(string) (string, error) {
			return "file=/etc/postgresql/16/other/pg_hba.conf\nerror=1:broken\n", nil
		},
	} {
		t.Run(name+": installed and reloaded, marked as not checked", func(t *testing.T) {
			fake := installDBConfigFakes(t)
			path := writeOwnerFile(t, "pg_hba.conf", debianHBA, 0o640)
			fake.queryAnswer = answer
			outcome, err := applyDatabaseConfigUpdate(hbaTarget, dbConfigPreimage(t, path), []byte(wanted), fake.reload)
			if err != nil {
				t.Fatal(err)
			}
			if outcome.DaemonCheck != transport.ConfigDaemonNotChecked || len(fake.reloads) != 1 || readFileForTest(t, path) != wanted {
				t.Fatalf("outcome = %+v, reloads %v", outcome, fake.reloads)
			}
		})
	}
}

// --- housekeeping ------------------------------------------------------------------

func TestOnlyTheNewestBackupsOfAFileAreKept(t *testing.T) {
	fake := installDBConfigFakes(t)
	fake.state = dbUnitInactive
	path := writeOwnerFile(t, "postgresql.conf", "max_connections = 0\n", 0o644)
	// Files beside it that are not this file's backups must survive.
	for _, other := range []string{"pg_hba.conf.celikpanel-backup-20200101T000000Z", "postgresql.conf.celikpanel-backup-notes", "postgresql.conf.bak"} {
		if err := os.WriteFile(filepath.Join(filepath.Dir(path), other), []byte("owner"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	clock := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	dbConfigNow = func() time.Time { clock = clock.Add(time.Minute); return clock }
	for i := 1; i <= dbConfigBackupsKept+3; i++ {
		content := []byte("max_connections = " + strings.Repeat("1", i) + "\n")
		if _, err := applyDatabaseConfigUpdate(postgresTarget, dbConfigPreimage(t, path), content, fake.reload); err != nil {
			t.Fatal(err)
		}
	}
	var backups, others int
	for _, name := range namesBeside(t, path) {
		if strings.HasPrefix(name, "postgresql.conf.celikpanel-backup-2026") {
			backups++
		} else {
			others++
		}
	}
	if backups != dbConfigBackupsKept || others != 3 {
		t.Fatalf("kept %d backups and %d other files, want %d and 3", backups, others, dbConfigBackupsKept)
	}
}

// --- the real programs, where this host has them -------------------------------------

func realProgram(t *testing.T, env string, candidates ...string) string {
	t.Helper()
	if fromEnv := os.Getenv(env); fromEnv != "" {
		candidates = append([]string{fromEnv}, candidates...)
	}
	for _, candidate := range candidates {
		matches, _ := filepath.Glob(candidate)
		for _, match := range matches {
			if info, err := os.Stat(match); err == nil && info.Mode().IsRegular() {
				return match
			}
		}
		if found, err := exec.LookPath(candidate); err == nil {
			return found
		}
	}
	return ""
}

func TestRealPostgresValidatesACandidateFile(t *testing.T) {
	program := realProgram(t, "CELIKPANEL_TEST_POSTGRES", "/usr/lib/postgresql/*/bin/postgres", "postgres")
	if program == "" {
		t.Skip("PostgreSQL is not installed on this host")
	}
	oldLook, oldStat := dbConfigLookPath, dbConfigStat
	t.Cleanup(func() { dbConfigLookPath, dbConfigStat = oldLook, oldStat })
	dbConfigLookPath = func(string) (string, error) { return program, nil }
	target := dbConfigTarget{kind: dbConfigPostgreSQL, unit: "postgresql"}

	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "conf.d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "conf.d", "owner.conf"), []byte("work_mem = 8MB\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "postgresql.conf")
	good := "# the owner's file\nmax_connections = 120\t\t# (change requires restart)\nshared_buffers = 128MB\ninclude_dir = 'conf.d'\n"
	if err := os.WriteFile(path, []byte(good), 0o644); err != nil {
		t.Fatal(err)
	}
	pre := dbConfigPreimage(t, path)

	if err := dbConfigValidateWithDaemon(target, pre, []byte(good+"work_mem = 16MB\n")); err != nil {
		t.Fatalf("a valid file was refused by %s: %v", program, err)
	}
	refusals := map[string]struct {
		content, name string
		line          int
	}{
		"an unknown setting":   {good + "no_such_setting = 1\n", "no_such_setting", 5},
		"a value out of range": {good + "port = 99999999\n", "port", 0},
		"an unclosed quote":    {good + "shared_buffers = '128MB\n", "", 5},
		"a missing include":    {good + "include 'nothere.conf'\n", "", 0},
	}
	for name, tc := range refusals {
		t.Run(name, func(t *testing.T) {
			err := dbConfigValidateWithDaemon(target, pre, []byte(tc.content))
			rpcErr := wantRefusal(t, err, transport.ConfigErrorValidationFail, transport.ConfigInvalidDaemon)
			if rpcErr.Detail == "" || strings.Contains(rpcErr.Detail, "celikpanel-candidate") {
				t.Fatalf("detail = %q, want the server's line with the file's own name", rpcErr.Detail)
			}
			if rpcErr.Name != tc.name || rpcErr.Line != tc.line {
				t.Fatalf("refusal = %+v, want name %q line %d", rpcErr, tc.name, tc.line)
			}
			t.Logf("%s: %s", name, rpcErr.Detail)
		})
	}
	if readFileForTest(t, path) != good {
		t.Fatal("validation changed the file")
	}
	if beside := namesBeside(t, path); len(beside) != 1 || beside[0] != "conf.d" {
		t.Fatalf("validation left files behind: %v", beside)
	}
}

func TestRealMariaDBValidatesACandidateFile(t *testing.T) {
	program := realProgram(t, "CELIKPANEL_TEST_MARIADBD", "mariadbd", "mysqld")
	if program == "" {
		t.Skip("MariaDB is not installed on this host")
	}
	oldLook := dbConfigLookPath
	t.Cleanup(func() { dbConfigLookPath = oldLook })
	dbConfigLookPath = func(name string) (string, error) {
		if name == "mariadbd" {
			return program, nil
		}
		return "", exec.ErrNotFound
	}
	target := dbConfigTarget{kind: dbConfigMariaDB, unit: "mariadb"}

	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data-that-must-stay-untouched")
	if err := os.Mkdir(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "50-server.cnf")
	good := "# the owner's file\n[server]\n[mysqld]\ndatadir = " + dataDir + "\nbind-address = 127.0.0.1\nmax_connections = 150\n[mariadb]\n"
	if err := os.WriteFile(path, []byte(good), 0o644); err != nil {
		t.Fatal(err)
	}
	pre := dbConfigPreimage(t, path)

	if err := dbConfigValidateWithDaemon(target, pre, []byte(strings.Replace(good, "150", "300", 1))); err != nil {
		t.Fatalf("a valid file was refused by %s: %v", program, err)
	}
	refusals := map[string]struct {
		content, name string
		line          int
	}{
		"an unknown variable":        {strings.Replace(good, "max_connections = 150", "no_such_variable = 3", 1), "no_such_variable", 0},
		"a value it cannot use":      {strings.Replace(good, "150", "lots", 1), "max_connections", 0},
		"a broken group header":      {"[mysqld\nmax_connections = 5\n", "", 1},
		"an option before any group": {"max_connections = 5\n[mysqld]\n", "", 1},
	}
	for name, tc := range refusals {
		t.Run(name, func(t *testing.T) {
			err := dbConfigValidateWithDaemon(target, pre, []byte(tc.content))
			rpcErr := wantRefusal(t, err, transport.ConfigErrorValidationFail, transport.ConfigInvalidDaemon)
			if rpcErr.Detail == "" || strings.Contains(rpcErr.Detail, "celikpanel-candidate") || strings.Contains(rpcErr.Detail, program) {
				t.Fatalf("detail = %q, want the server's line with the file's own name", rpcErr.Detail)
			}
			if rpcErr.Name != tc.name || rpcErr.Line != tc.line {
				t.Fatalf("refusal = %+v, want name %q line %d", rpcErr, tc.name, tc.line)
			}
			t.Logf("%s: %s", name, rpcErr.Detail)
		})
	}
	if entries, _ := os.ReadDir(dataDir); len(entries) != 0 {
		t.Fatalf("validation wrote into the data directory: %v", entries)
	}
	if readFileForTest(t, path) != good {
		t.Fatal("validation changed the file")
	}
}
