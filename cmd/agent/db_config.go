package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/hostcmd"
	"github.com/alicelik/celikpanel/internal/transport"
)

// Writing a database server's own configuration file (postgresql.conf,
// pg_hba.conf, a MariaDB option file) as root. These are the server owner's
// files (D-022); a bad one stops the database at its next start, and a bad
// pg_hba.conf locks the owner and the Panel out. So, after the pre-image,
// version and shape checks of updateManagedConfig, and still before the live
// file is touched (9 Oct 2026; D-025 invariants 1, 3 and 4):
//
//   - pg_hba.conf: every changed or added line must be one PostgreSQL accepts,
//     and the local administrator access must survive (db_config_hba.go);
//   - postgresql.conf: the installed `postgres` reads a copy placed next to
//     the file (`postgres -C config_file -D <dir> -c config_file=<copy>`). It
//     parses the file and every file it includes with the server's own parser
//     and value checks, prints one setting and exits; it starts nothing, takes
//     no lock and is safe beside a running server. `-C` first is the one form
//     PostgreSQL lets root run;
//   - a MariaDB option file: the installed `mariadbd` reads the copy
//     (`mariadbd --defaults-file=<copy> --datadir=<empty private directory>
//     --help --verbose`). It reads the option file with the server's own
//     reader, refuses an unknown variable or an unusable value, prints its help
//     and exits. The private data directory keeps it away from the real one.
//
// Then: the previous file is kept as a timestamped backup next to the file, the
// new one replaces it atomically with the same owner and mode and only if the
// file still is the one that was read, and the running service is told:
//
//   - PostgreSQL is reloaded, never restarted. For pg_hba.conf the running
//     server is first asked what it makes of the installed file
//     (pg_hba_file_rules reads the file from disk without a reload), so a file
//     it refuses is put back before it was ever loaded.
//   - MariaDB reads its option files only when it starts and has no reload
//     that re-reads them, so it is left alone and the answer says the change
//     waits for the next restart, which is the owner's decision.
//
// If the reload fails, the previous file is put back (only if the file still is
// the one this write installed) and the service is made to read it; the answer
// carries the first line the service's side said and one of four reasons, each
// for what is verified (10 Oct 2026): the unit reloaded the previous file; the
// unit's reload failed again but the server, asked directly, re-read it; which
// settings the server runs with could not be established; or the previous file
// could not be put back, and only then is a kept copy named.
//
// Bir veritabanı sunucusunun kendi yapılandırma dosyasını root olarak yazmak.
// Bunlar sunucu sahibinin dosyalarıdır; bozuk bir dosya veritabanını bir sonraki
// başlatmada durdurur, bozuk bir pg_hba.conf sahibi ve Panel'i dışarıda bırakır.
// Bu yüzden canlı dosyaya dokunmadan önce: pg_hba.conf'ta değişen her satır
// PostgreSQL'in kabul ettiği bir satır olmalı ve yerel yönetici erişimi
// korunmalıdır; postgresql.conf'u kurulu `postgres`, MariaDB seçenek dosyasını
// kurulu `mariadbd` bir kopya üzerinden okur. Sonra önceki dosya yanında zaman
// damgalı yedek olarak tutulur, yenisi aynı sahip ve kiple, yalnız dosya hâlâ
// okunan dosyaysa atomik olarak yerine konur. PostgreSQL yeniden yüklenir, asla
// yeniden başlatılmaz; MariaDB'ye dokunulmaz. Yeniden yükleme başarısız olursa
// önceki dosya geri konur.

type dbConfigKind int

const (
	dbConfigPostgreSQL dbConfigKind = iota + 1
	dbConfigHBA
	dbConfigMariaDB
)

type dbConfigTarget struct {
	kind dbConfigKind
	// unit is the systemd unit that owns the file.
	unit string
	// postgres is where this cluster's own `postgres` is expected; empty means
	// "the one on PATH".
	postgres string
}

var dbConfigDebianPostgreSQL = regexp.MustCompile(`^/etc/postgresql/([0-9]+(?:\.[0-9]+)?)/([A-Za-z0-9][A-Za-z0-9_.-]*)/(postgresql\.conf|pg_hba\.conf)$`)

// dbConfigTargetFor recognises the database configuration files the scanner
// offers for editing. Anything else is not a database file.
func dbConfigTargetFor(path string) (dbConfigTarget, bool) {
	slashed := filepath.ToSlash(path)
	kindOf := func(name string) dbConfigKind {
		if name == "pg_hba.conf" {
			return dbConfigHBA
		}
		return dbConfigPostgreSQL
	}
	if match := dbConfigDebianPostgreSQL.FindStringSubmatch(slashed); match != nil {
		return dbConfigTarget{
			kind:     kindOf(match[3]),
			unit:     "postgresql@" + match[1] + "-" + match[2],
			postgres: "/usr/lib/postgresql/" + match[1] + "/bin/postgres",
		}, true
	}
	switch slashed {
	case "/var/lib/pgsql/data/postgresql.conf", "/var/lib/postgres/data/postgresql.conf",
		"/var/lib/pgsql/data/pg_hba.conf", "/var/lib/postgres/data/pg_hba.conf":
		return dbConfigTarget{kind: kindOf(filepath.Base(slashed)), unit: "postgresql"}, true
	case "/etc/my.cnf", "/usr/local/mysql/my.cnf":
		return dbConfigTarget{kind: dbConfigMariaDB, unit: "mariadb"}, true
	}
	if strings.HasPrefix(slashed, "/etc/mysql/") && strings.HasSuffix(slashed, ".cnf") {
		return dbConfigTarget{kind: dbConfigMariaDB, unit: "mariadb"}, true
	}
	return dbConfigTarget{}, false
}

const (
	dbUnitActive   = "active"
	dbUnitInactive = "inactive"
	dbUnitUnknown  = "unknown"
)

// Swapped by tests. Production runs the installed programs.
// Testlerde değiştirilir.
var (
	dbConfigLookPath = exec.LookPath
	dbConfigNow      = time.Now
	dbConfigStat     = os.Stat

	// dbConfigRun runs one validating program and returns what it printed on
	// standard error.
	dbConfigRun = func(ctx context.Context, dir, name string, args ...string) (stderr string, err error) {
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Dir = dir
		// The answers read below must not arrive translated, and a PGDATA
		// from the Agent's environment must not choose the files.
		cmd.Env = append(dbConfigCleanEnv(os.Environ()), "LC_ALL=C", "LANGUAGE=C")
		var captured strings.Builder
		cmd.Stderr = &captured
		err = cmd.Run()
		return captured.String(), err
	}

	// dbConfigUnitState reads whether the unit that owns the file is running.
	dbConfigUnitState = func(unit string) string {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "systemctl", "show", unit,
			"--property=LoadState", "--property=ActiveState").Output()
		if err != nil {
			return dbUnitUnknown
		}
		values := map[string]string{}
		for _, line := range strings.Split(string(out), "\n") {
			if name, value, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
				values[name] = value
			}
		}
		if values["LoadState"] != "loaded" {
			return dbUnitUnknown
		}
		switch values["ActiveState"] {
		case "active", "reloading":
			return dbUnitActive
		case "inactive", "failed":
			return dbUnitInactive
		}
		return dbUnitUnknown
	}

	// dbConfigUnitLog returns what the unit wrote to the journal since `since`.
	dbConfigUnitLog = func(unit string, since time.Time) string {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		out, _ := exec.CommandContext(ctx, "journalctl", "--unit", unit, "--no-pager", "--output=cat",
			"--since=@"+strconv.FormatInt(since.Unix(), 10), "--lines=40").Output()
		return string(out)
	}

	// dbConfigPostgreSQLQuery asks the running PostgreSQL as its own operating
	// system account over the local socket, the way every other statement of
	// the Agent reaches it.
	dbConfigPostgreSQLQuery = func(statement string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "sudo", "-u", "postgres", "psql",
			"--no-psqlrc", "--set", "ON_ERROR_STOP=on", "--no-align", "--tuples-only", "--quiet")
		cmd.Stdin = strings.NewReader(statement + "\n")
		cmd.Env = append(dbConfigCleanEnv(os.Environ()), "LC_ALL=C", "LANGUAGE=C")
		out, err := cmd.Output()
		return string(out), err
	}
)

func dbConfigCleanEnv(env []string) []string {
	kept := make([]string, 0, len(env))
	for _, entry := range env {
		name, _, _ := strings.Cut(entry, "=")
		switch name {
		case "PGDATA", "PGHOST", "PGPORT", "PGUSER", "PGDATABASE", "PGSERVICE", "PGOPTIONS",
			"LC_ALL", "LANGUAGE", "LC_MESSAGES", "MYSQL_HOME", "MARIADB_HOME":
			continue
		}
		kept = append(kept, entry)
	}
	return kept
}

const dbConfigBackupMarker = ".celikpanel-backup-"

// The newest backups of one file that are kept. Each save leaves one; without
// a bound they would only ever grow in the owner's configuration directory.
const dbConfigBackupsKept = 10

func applyDatabaseConfigUpdate(target dbConfigTarget, pre dnsFileSnapshot, content []byte, reload func(string) error) (transport.UpdateConfigResponse, error) {
	path := pre.Path
	var none transport.UpdateConfigResponse
	if !pre.OwnerKnown {
		return none, errors.New("the owner of the configuration file cannot be read on this system")
	}

	// 1. What can be decided from the text alone.
	daemonCheck := transport.ConfigDaemonNotChecked
	if target.kind == dbConfigHBA {
		if refusal := validateHBACandidate(string(pre.Data), string(content)); refusal != nil {
			return none, refusal
		}
		if refusal := hbaLockoutRefusal(string(pre.Data), string(content)); refusal != nil {
			return none, refusal
		}
	} else {
		// 2. The service's own program reads a copy next to the file.
		if err := dbConfigValidateWithDaemon(target, pre, content); err != nil {
			return none, err
		}
		daemonCheck = transport.ConfigDaemonAccepted
	}

	state := dbConfigUnitState(target.unit)

	// 3. The previous file is kept, then replaced only if it still is the file
	// that was read.
	backup, err := dbConfigWriteBackup(pre)
	if err != nil {
		return none, fmt.Errorf("keep a backup of %s: %w", path, err)
	}
	if err := secureWriteConfigReplacingSnapshot(path, content, os.FileMode(pre.Mode), &pre); err != nil {
		_ = secureRemoveConfig(backup)
		if now, readErr := secureReadConfig(path); readErr == nil && configVersion(now) != configVersion(pre.Data) {
			return none, configChanged()
		}
		return none, fmt.Errorf("install %s: %w", path, err)
	}
	log.Printf("config write: %s installed; the previous file is kept as %s", path, backup)
	installed := pre
	installed.Data = content
	installed.SHA256 = digestDNSBytes(content)
	// putBack restores the previous file only where this write's file is still
	// in place; a later owner edit is not undone.
	putBack := func() error {
		return secureWriteConfigReplacingSnapshot(path, pre.Data, os.FileMode(pre.Mode), &installed)
	}
	// notRestored is the one answer for a previous file that is NOT back on
	// disk: the copy named holds it.
	notRestored := func(detail string, cause error) error {
		log.Printf("config write: %s could not be put back after a refused change: %v", path, cause)
		return &configRefusal{
			code: transport.ConfigErrorReloadFailed, reason: transport.ConfigReloadNotRestored,
			message: "the service did not accept the new configuration file and the previous one could not be put back; it is kept as " + backup,
			detail:  detail, name: backup, unit: target.unit,
		}
	}
	// previousFileBack classifies a change that was not kept once the previous
	// file IS back on disk, by what is verified about the running service
	// (10 Oct 2026). Measured on three platforms: the unit's reload failed
	// twice, the previous file was in place byte for byte, and the answer said
	// it could not be put back and named a copy of that same file as "the
	// other version". The copy is removed here: the file on disk is that file.
	// previousFileBack, önceki dosya diskte yerine konduktan sonra tutulmayan
	// bir değişikliği, çalışan hizmet hakkında doğrulanana göre sınıflandırır.
	previousFileBack := func(detail string) *configRefusal {
		_ = secureRemoveConfig(backup)
		refusal := &configRefusal{
			code: transport.ConfigErrorReloadFailed, detail: detail, unit: target.unit,
			reason: dbConfigLoadPreviousFile(target, path, reload),
		}
		switch refusal.reason {
		case transport.ConfigReloadRestored:
			refusal.message = "the service could not reload with the new configuration file; the previous file is back in place and loaded"
		case transport.ConfigReloadRestoredUnitFailed:
			refusal.message = "the unit's reload failed with the new configuration file and with the previous one; the previous file is back in place, and the server, asked directly, re-read it and runs with the settings it had before"
		default:
			refusal.message = "the unit's reload failed with the new configuration file and with the previous one; the previous file is back in place, and which settings the server runs with could not be established"
		}
		return refusal
	}

	result := transport.UpdateConfigResponse{Version: configVersion(content), Backup: backup}

	// 4. The running service.
	switch {
	case target.kind == dbConfigMariaDB:
		result.Applied = transport.ConfigAppliedRestartRequired
	case state == dbUnitInactive:
		result.Applied = transport.ConfigAppliedNotRunning
	default:
		if target.kind == dbConfigHBA && state == dbUnitActive {
			verdict, detail, line := dbConfigAskPostgreSQLAboutHBA(path)
			if verdict == dbDaemonRefused {
				if err := putBack(); err != nil {
					return none, notRestored(detail, err)
				}
				_ = secureRemoveConfig(backup)
				refusal := configInvalid(transport.ConfigInvalidDaemon,
					"PostgreSQL does not accept the new pg_hba.conf; the previous file is back in place and was never unloaded")
				refusal.detail, refusal.line = detail, line
				return none, refusal
			}
			if verdict == dbDaemonAccepted {
				daemonCheck = transport.ConfigDaemonAccepted
			}
		}
		started := dbConfigNow()
		if err := reload(target.unit); err != nil {
			detail := dbConfigReloadFailureLine(target.unit, started, err)
			if restoreErr := putBack(); restoreErr != nil {
				return none, notRestored(detail, restoreErr)
			}
			return none, previousFileBack(detail)
		}
		result.Applied = transport.ConfigAppliedReloaded
		if target.kind == dbConfigPostgreSQL && state == dbUnitActive {
			verdict, detail, line, name, restart := dbConfigAskPostgreSQLAboutSettings(path)
			if verdict == dbDaemonRefused {
				if err := putBack(); err != nil {
					return none, notRestored(detail, err)
				}
				// The previous file is back. Whether the server runs with it
				// is classified the same way as after a failed reload.
				back := previousFileBack(detail)
				if back.reason != transport.ConfigReloadRestored {
					return none, back
				}
				refusal := configInvalid(transport.ConfigInvalidDaemon,
					"PostgreSQL reported an error in the new postgresql.conf after reloading it; the previous file is back in place and loaded")
				refusal.detail, refusal.line, refusal.name = detail, line, name
				return none, refusal
			}
			result.RestartRequired = restart
		}
	}
	result.DaemonCheck = daemonCheck
	dbConfigPruneBackups(path)
	return result, nil
}

// dbConfigValidateWithDaemon lets the installed server program read a copy of
// the new content placed next to the file, so relative includes resolve as
// they will for the real file. The copy has the owner and mode of the file and
// a name no include directive picks up.
func dbConfigValidateWithDaemon(target dbConfigTarget, pre dnsFileSnapshot, content []byte) error {
	path := pre.Path
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		return fmt.Errorf("prepare a validation copy of %s: %w", path, err)
	}
	candidate := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".celikpanel-candidate-"+hex.EncodeToString(random))
	if err := secureWriteConfigOwnedBy(candidate, content, os.FileMode(pre.Mode), pre.UID, pre.GID); err != nil {
		return fmt.Errorf("write a validation copy of %s: %w", path, err)
	}
	defer func() {
		if err := secureRemoveConfig(candidate); err != nil {
			log.Printf("config write: the validation copy %s could not be removed: %v", candidate, err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	unavailable := func(program string, cause error) error {
		log.Printf("config write: %s cannot be validated: %s: %v", path, program, cause)
		refusal := configInvalid(transport.ConfigInvalidNoValidator,
			"the program that checks this configuration file could not be run, so the file was not replaced")
		refusal.name = program
		return refusal
	}

	if target.kind == dbConfigPostgreSQL {
		program := target.postgres
		if program != "" {
			if _, err := dbConfigStat(program); err != nil {
				program = ""
			}
		}
		if program == "" {
			found, err := dbConfigLookPath("postgres")
			if err != nil {
				return unavailable("postgres", err)
			}
			program = found
		}
		// -C must be the first argument: it is the form PostgreSQL allows root
		// to run, because it only reads.
		stderr, err := dbConfigRun(ctx, filepath.Dir(path), program,
			"-C", "config_file", "-D", filepath.Dir(path),
			"-c", "config_file="+candidate, "-c", "lc_messages=C")
		if err == nil {
			return nil
		}
		var exited *exec.ExitError
		if !errors.As(err, &exited) {
			return unavailable("postgres", err)
		}
		detail, line, name := postgresConfigError(stderr, candidate, path)
		refusal := configInvalid(transport.ConfigInvalidDaemon,
			"PostgreSQL does not accept the new postgresql.conf; nothing was changed")
		refusal.detail, refusal.line, refusal.name = detail, line, name
		return refusal
	}

	program := ""
	for _, name := range []string{"mariadbd", "mysqld"} {
		if found, err := dbConfigLookPath(name); err == nil {
			program = found
			break
		}
	}
	if program == "" {
		return unavailable("mariadbd", errors.New("neither mariadbd nor mysqld is on PATH"))
	}
	private, err := os.MkdirTemp("", "celikpanel-mariadb-check-")
	if err != nil {
		return unavailable("mariadbd", err)
	}
	defer os.RemoveAll(private)
	// --defaults-file must be first. The private, empty data directory given
	// after it wins over the file's own, so the real one is never opened.
	stderr, err := dbConfigRun(ctx, private, program,
		"--defaults-file="+candidate, "--datadir="+private, "--help", "--verbose")
	if err == nil {
		// Exit status 0 is not acceptance (10 Oct 2026). Measured on Debian 13
		// and Arch: `max_connections = plenty` exits 0 with
		// "[Warning] ... option 'max_connections': unsigned value 0 adjusted
		// to 10", and the server would start with 10. A value MariaDB says it
		// will not use as written is refused with MariaDB's own line. Only
		// that kind of warning counts: the same run also prints warnings that
		// say nothing about the file (the empty private data directory has no
		// mysql.plugin table; a stock Debian file sets expire_logs_days
		// without a binary log). And a warning the file on the server already
		// produces is not this change's: the current file is read the same
		// way and what it already says is left out.
		// Çıkış durumu 0 kabul değildir: MariaDB'nin yazıldığı gibi
		// kullanmayacağını söylediği değer, MariaDB'nin kendi satırıyla
		// reddedilir. Sunucudaki dosyanın zaten ürettiği uyarı bu değişikliğin
		// değildir.
		adjusted := mariadbValueWarnings(stderr)
		if len(adjusted) == 0 {
			return nil
		}
		already := map[string]bool{}
		if current, currentErr := dbConfigRun(ctx, private, program,
			"--defaults-file="+path, "--datadir="+private, "--help", "--verbose"); currentErr == nil {
			for _, warning := range mariadbValueWarnings(current) {
				already[warning.detail] = true
			}
		}
		for _, warning := range adjusted {
			if already[warning.detail] {
				continue
			}
			refusal := configInvalid(transport.ConfigInvalidDaemon,
				"MariaDB would not use a value in the new option file as it is written; nothing was changed")
			refusal.detail, refusal.name = dbConfigLine(warning.detail, candidate, path), warning.name
			return refusal
		}
		return nil
	}
	var exited *exec.ExitError
	if !errors.As(err, &exited) {
		return unavailable("mariadbd", err)
	}
	detail, line, name := mariadbConfigError(stderr, candidate, path)
	refusal := configInvalid(transport.ConfigInvalidDaemon,
		"MariaDB does not accept the new option file; nothing was changed")
	refusal.detail, refusal.line, refusal.name = detail, line, name
	return refusal
}

var (
	postgresSeverity  = regexp.MustCompile(`\b(LOG|WARNING|ERROR|FATAL|PANIC):\s+(.*)$`)
	postgresLine      = regexp.MustCompile(` line (\d+)`)
	postgresParameter = regexp.MustCompile(`parameter "([^"]+)"`)
	dbConfigSecret    = regexp.MustCompile(`(?i)((?:pass(?:word|wd)?|secrets?)[a-z_]*\s*=\s*)("[^"]*"|'[^']*'|\S+)`)
)

// dbConfigLine makes one line the screen may show: the validation copy's name
// replaced by the file's, anything that looks like a password assignment
// blanked, control characters removed, bounded.
func dbConfigLine(text, candidate, path string) string {
	text = strings.ReplaceAll(text, candidate, path)
	text = dbConfigSecret.ReplaceAllString(text, "${1}…")
	return hostcmd.Bounded(strings.Join(strings.Fields(text), " "), 300)
}

// postgresConfigError picks what `postgres -C` said about the file: the first
// message that is not about a missing postgresql.auto.conf, preferring the
// specific line over the closing "configuration file … contains errors".
func postgresConfigError(stderr, candidate, path string) (detail string, line int, name string) {
	var fatal string
	for _, raw := range strings.Split(stderr, "\n") {
		match := postgresSeverity.FindStringSubmatch(strings.TrimSpace(raw))
		if match == nil {
			continue
		}
		message := match[2]
		if strings.HasPrefix(message, "skipping missing configuration file") {
			continue
		}
		if match[1] == "FATAL" || match[1] == "PANIC" {
			if fatal == "" {
				fatal = message
			}
			continue
		}
		detail = message
		break
	}
	if detail == "" {
		detail = fatal
	}
	if detail == "" {
		// Not in the server's log format: its own refusal to run, say.
		for _, raw := range strings.Split(stderr, "\n") {
			if strings.TrimSpace(raw) != "" {
				detail = strings.TrimSpace(raw)
				break
			}
		}
	}
	if strings.Contains(detail, `"`+candidate+`"`) {
		if match := postgresLine.FindStringSubmatch(detail); match != nil {
			line, _ = strconv.Atoi(match[1])
		}
	}
	if match := postgresParameter.FindStringSubmatch(detail); match != nil {
		name = match[1]
	}
	return dbConfigLine(detail, candidate, path), line, name
}

var (
	mariadbVariable = regexp.MustCompile(`unknown (?:variable|option) '(?:--)?([A-Za-z0-9_.-]+)`)
	mariadbFor      = regexp.MustCompile(`(?:for variable|for option|to) '([A-Za-z0-9_.-]+)'`)
	mariadbLine     = regexp.MustCompile(`at line:? (\d+)`)
)

type mariadbValueWarning struct{ detail, name string }

// What my_getopt prints when it reads a value it will not use as written: a
// number it moves into range ("unsigned value 0 adjusted to 10", also signed
// and floating point) and a boolean it does not recognise ("boolean value
// 'maybe' wasn't recognized. Set to OFF."). Nothing else is a refusal.
var mariadbValueWarningText = regexp.MustCompile(`^option '([A-Za-z0-9_.-]+)': .*(?:\badjusted to\b|wasn't recognized)`)

// mariadbValueWarnings returns the [Warning] lines of a `--help --verbose` run
// that say MariaDB changes a value of the option file.
func mariadbValueWarnings(stderr string) []mariadbValueWarning {
	var found []mariadbValueWarning
	for _, raw := range strings.Split(stderr, "\n") {
		_, message, ok := strings.Cut(raw, "[Warning] ")
		if !ok {
			continue
		}
		message = strings.TrimPrefix(strings.TrimSpace(message), "Buffered warning: ")
		if match := mariadbValueWarningText.FindStringSubmatch(message); match != nil {
			found = append(found, mariadbValueWarning{detail: message, name: match[1]})
		}
	}
	return found
}

// mariadbConfigError picks what mariadbd said about the option file: its first
// [ERROR] line, or the option-file reader's own "error:" line.
func mariadbConfigError(stderr, candidate, path string) (detail string, line int, name string) {
	lines := strings.Split(stderr, "\n")
	for _, raw := range lines {
		if _, after, ok := strings.Cut(raw, "[ERROR] "); ok {
			detail = strings.TrimSpace(after)
			break
		}
	}
	if detail == "" {
		for _, raw := range lines {
			trimmed := strings.TrimSpace(raw)
			if strings.HasPrefix(trimmed, "error: ") {
				detail = strings.TrimPrefix(trimmed, "error: ")
				break
			}
		}
	}
	if detail == "" {
		for _, raw := range lines {
			trimmed := strings.TrimSpace(raw)
			if trimmed != "" && !strings.Contains(trimmed, "[Warning]") && !strings.Contains(trimmed, "[Note]") {
				detail = trimmed
				break
			}
		}
	}
	detail = strings.TrimPrefix(detail, "Buffered error: ")
	// "/usr/sbin/mariadbd: unknown variable …": the program's own path says
	// nothing about the file.
	if program, rest, ok := strings.Cut(detail, ": "); ok && strings.HasPrefix(program, "/") && !strings.ContainsAny(program, " '") {
		detail = rest
	}
	if match := mariadbVariable.FindStringSubmatch(detail); match != nil {
		name = match[1]
	} else if match := mariadbFor.FindStringSubmatch(detail); match != nil {
		name = match[1]
	}
	if strings.Contains(detail, candidate) {
		if match := mariadbLine.FindStringSubmatch(detail); match != nil {
			line, _ = strconv.Atoi(match[1])
		}
	}
	return dbConfigLine(detail, candidate, path), line, name
}

const (
	dbDaemonAccepted   = "accepted"
	dbDaemonRefused    = "refused"
	dbDaemonNotChecked = "not_checked"
)

// dbConfigAskPostgreSQLAboutHBA asks the running server what it makes of the
// pg_hba.conf that is on disk now. pg_hba_file_rules parses the file at the
// moment of the query, without a reload. "Not checked" when the server cannot
// be asked or the answer is about another cluster's file.
func dbConfigAskPostgreSQLAboutHBA(path string) (verdict, detail string, line int) {
	out, err := dbConfigPostgreSQLQuery(
		"SELECT 'file=' || current_setting('hba_file');\n" +
			"SELECT 'error=' || line_number || ':' || error FROM pg_hba_file_rules WHERE error IS NOT NULL ORDER BY line_number LIMIT 1;")
	if err != nil {
		log.Printf("config write: PostgreSQL could not be asked about %s: %s", path, hostcmd.Bounded(strings.Join(strings.Fields(hostcmd.Stderr(err)), " "), 300))
		return dbDaemonNotChecked, "", 0
	}
	file, first := dbConfigAnswer(out)
	if file != path {
		return dbDaemonNotChecked, "", 0
	}
	if first == "" {
		return dbDaemonAccepted, "", 0
	}
	number, message, _ := strings.Cut(first, ":")
	line, _ = strconv.Atoi(number)
	return dbDaemonRefused, dbConfigLine(message, path, path), line
}

// dbConfigAskPostgreSQLAboutSettings asks the reloaded server which entries of
// postgresql.conf it could not take, and which settings now wait for a restart.
func dbConfigAskPostgreSQLAboutSettings(path string) (verdict, detail string, line int, name string, restart []string) {
	out, err := dbConfigPostgreSQLQuery(
		"SELECT 'file=' || current_setting('config_file');\n" +
			"SELECT 'error=' || sourceline || ':' || coalesce(name, '') || ':' || error FROM pg_file_settings " +
			"WHERE error IS NOT NULL AND sourcefile = current_setting('config_file') " +
			"AND coalesce(name, '') NOT IN (SELECT name FROM pg_settings WHERE pending_restart) ORDER BY seqno LIMIT 1;\n" +
			"SELECT 'restart=' || name FROM pg_settings WHERE pending_restart ORDER BY name LIMIT 50;")
	if err != nil {
		log.Printf("config write: PostgreSQL could not be asked about %s: %s", path, hostcmd.Bounded(strings.Join(strings.Fields(hostcmd.Stderr(err)), " "), 300))
		return dbDaemonNotChecked, "", 0, "", nil
	}
	file, first := dbConfigAnswer(out)
	if file != path {
		return dbDaemonNotChecked, "", 0, "", nil
	}
	for _, raw := range strings.Split(out, "\n") {
		if setting, ok := strings.CutPrefix(strings.TrimSpace(raw), "restart="); ok && setting != "" {
			restart = append(restart, setting)
		}
	}
	if first == "" {
		return dbDaemonAccepted, "", 0, "", restart
	}
	parts := strings.SplitN(first, ":", 3)
	if len(parts) == 3 {
		line, _ = strconv.Atoi(parts[0])
		return dbDaemonRefused, dbConfigLine(parts[2], path, path), line, parts[1], restart
	}
	return dbDaemonRefused, dbConfigLine(first, path, path), 0, "", restart
}

// dbConfigLoadPreviousFile makes the running service read the previous file,
// which is back on disk, and answers with the ConfigReload* reason that is
// verified.
//
// The unit's reload first. If that fails too, nothing is known from it about
// what the server runs: a reload command that fails part-way may have signalled
// the server before it failed. Measured on Debian 13, Ubuntu 24.04 and Arch
// with an owner's drop-in whose ExecReload signals PostgreSQL and then fails:
// the first reload made the server read the NEW file, and only the second, also
// "failed", made it read the previous one again. So the server is then told
// directly and asked what it did: pg_reload_conf() over the local socket sends
// the same signal the unit would, and after it
//
//   - pg_conf_load_time() must be later than the moment the signal was sent.
//     PostgreSQL sets it only when a re-read of its configuration files got to
//     the end without a syntax or value error; with such an error it applies
//     nothing and leaves the time as it was;
//   - pg_file_settings (postgresql.conf) or pg_hba_file_rules (pg_hba.conf)
//     must report no error in the files that are on disk now;
//   - the answer must be about this file (config_file / hba_file), not
//     another cluster's.
//
// All three together are "the server re-read the previous file and runs with
// the settings it had before the change". pg_file_settings.applied is not used
// for that claim: it says what a re-read WOULD apply from the file, not what
// is loaded. Anything less is the unknown reason, never a guess.
//
// dbConfigLoadPreviousFile, diskte yerine konmuş önceki dosyayı çalışan hizmete
// okutur ve doğrulanan ConfigReload* gerekçesini döndürür. Birimin yeniden
// yüklemesi yine başarısız olursa sunucuya doğrudan sinyal gönderilir ve ne
// yüklediği sorulur; üç koşul birlikte sağlanmazsa yanıt "bilinmiyor"dur.
func dbConfigLoadPreviousFile(target dbConfigTarget, path string, reload func(string) error) string {
	againErr := reload(target.unit)
	if againErr == nil {
		return transport.ConfigReloadRestored
	}
	log.Printf("config write: reload of %s with the restored %s failed too: %v", target.unit, path, againErr)
	if target.kind != dbConfigPostgreSQL && target.kind != dbConfigHBA {
		return transport.ConfigReloadRestoredUnknown
	}
	if dbConfigPostgreSQLRereadVerified(target.kind, path) {
		return transport.ConfigReloadRestoredUnitFailed
	}
	return transport.ConfigReloadRestoredUnknown
}

// dbConfigPostgreSQLRereadVerified sends the running PostgreSQL the reload
// signal over the local socket and reports whether it verifiably re-read the
// files that are on disk now without error. See dbConfigLoadPreviousFile.
func dbConfigPostgreSQLRereadVerified(kind dbConfigKind, path string) bool {
	setting := "config_file"
	firstError := "SELECT 'error=' || sourceline || ':' || coalesce(name, '') || ':' || error FROM pg_file_settings " +
		"WHERE error IS NOT NULL " +
		"AND coalesce(name, '') NOT IN (SELECT name FROM pg_settings WHERE pending_restart) ORDER BY seqno LIMIT 1;"
	if kind == dbConfigHBA {
		setting = "hba_file"
		firstError = "SELECT 'error=' || line_number || ':' || error FROM pg_hba_file_rules WHERE error IS NOT NULL ORDER BY line_number LIMIT 1;"
	}
	out, err := dbConfigPostgreSQLQuery(
		"SELECT 'file=' || current_setting('" + setting + "');\n" +
			"SELECT 'before=' || extract(epoch from clock_timestamp());\n" +
			"SELECT 'signal=' || pg_reload_conf();\n" +
			// The session takes up the re-read between statements; one second
			// is far more than the postmaster needs to pass the signal on.
			"SELECT 'waited=' || count(*) FROM (SELECT pg_sleep(1)) AS waited;\n" +
			"SELECT 'loaded=' || extract(epoch from pg_conf_load_time());\n" +
			firstError)
	if err != nil {
		log.Printf("config write: PostgreSQL could not be asked to read %s again: %s", path, hostcmd.Bounded(strings.Join(strings.Fields(hostcmd.Stderr(err)), " "), 300))
		return false
	}
	values := map[string]string{}
	for _, raw := range strings.Split(out, "\n") {
		if name, value, ok := strings.Cut(strings.TrimSpace(raw), "="); ok {
			if _, seen := values[name]; !seen {
				values[name] = value
			}
		}
	}
	before, beforeErr := strconv.ParseFloat(values["before"], 64)
	loaded, loadedErr := strconv.ParseFloat(values["loaded"], 64)
	signalled := values["signal"] == "true" || values["signal"] == "t"
	reread := beforeErr == nil && loadedErr == nil && loaded >= before
	_, refused := values["error"]
	verified := values["file"] == path && signalled && reread && !refused
	if !verified {
		log.Printf("config write: PostgreSQL did not confirm that it read %s again (same file %t, signal sent %t, re-read after the signal %t, error reported %t)",
			path, values["file"] == path, signalled, reread, refused)
	}
	return verified
}

func dbConfigAnswer(out string) (file, firstError string) {
	for _, raw := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(raw)
		if value, ok := strings.CutPrefix(trimmed, "file="); ok {
			file = value
		}
		if value, ok := strings.CutPrefix(trimmed, "error="); ok && firstError == "" {
			firstError = value
		}
	}
	return file, firstError
}

var dbConfigFailureWords = regexp.MustCompile(`(?i)\b(error|fatal|invalid|failed|cannot|could not|unrecognized|not permitted)\b`)

// dbConfigReloadFailureLine is the first line the service's side said about a
// reload that failed: from the unit's journal when it names a failure, else
// what the reload command printed.
func dbConfigReloadFailureLine(unit string, since time.Time, reloadErr error) string {
	for _, raw := range strings.Split(dbConfigUnitLog(unit, since), "\n") {
		trimmed := strings.TrimSpace(raw)
		if trimmed != "" && dbConfigFailureWords.MatchString(trimmed) {
			return dbConfigLine(trimmed, "", "")
		}
	}
	text := ""
	if reloadErr != nil {
		text = reloadErr.Error()
	}
	return dbConfigLine(firstLine(text), "", "")
}

// dbConfigWriteBackup keeps the previous file next to it, with its owner and
// mode, under a name no include directive reads (it ends in a timestamp, not
// in .conf or .cnf).
func dbConfigWriteBackup(pre dnsFileSnapshot) (string, error) {
	stamp := dbConfigNow().UTC().Format("20060102T150405Z")
	for attempt := 0; attempt < 50; attempt++ {
		name := pre.Path + dbConfigBackupMarker + stamp
		if attempt > 0 {
			name += "-" + strconv.Itoa(attempt+1)
		}
		if _, err := os.Lstat(name); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		if err := secureWriteConfigOwnedBy(name, pre.Data, os.FileMode(pre.Mode), pre.UID, pre.GID); err != nil {
			return "", err
		}
		return name, nil
	}
	return "", errors.New("no free backup name")
}

// dbConfigPruneBackups removes the oldest of the Panel's own backups of one
// file beyond dbConfigBackupsKept. Only names this file's writes created match.
func dbConfigPruneBackups(path string) {
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		return
	}
	prefix := filepath.Base(path) + dbConfigBackupMarker
	var names []string
	for _, entry := range entries {
		if entry.Type().IsRegular() && strings.HasPrefix(entry.Name(), prefix) && dbConfigBackupName.MatchString(strings.TrimPrefix(entry.Name(), prefix)) {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	for len(names) > dbConfigBackupsKept {
		if err := secureRemoveConfig(filepath.Join(filepath.Dir(path), names[0])); err != nil {
			log.Printf("config write: old backup %s could not be removed: %v", names[0], err)
		}
		names = names[1:]
	}
}

var dbConfigBackupName = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z(?:-[0-9]+)?$`)
