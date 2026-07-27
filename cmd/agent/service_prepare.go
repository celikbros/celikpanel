package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Packages on Arch leave some first-run initialization to the operator. Run
// it only inside the explicit Install action, before reporting readiness.
func prepareInstalledService(serviceID, family string) error {
	if family != "pacman" {
		return nil
	}
	switch serviceID {
	case "postgresql":
		return prepareArchPostgreSQL()
	case "mariadb":
		return prepareArchMariaDB()
	case "clamav":
		return prepareArchClamAV()
	default:
		return nil
	}
}

func prepareArchPostgreSQL() error {
	const dataDir = "/var/lib/postgres/data"
	if _, err := os.Stat(filepath.Join(dataDir, "PG_VERSION")); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("postgresql data directory cannot be inspected: %w", err)
	}
	if out, err := exec.Command(
		"install", "-d", "-m", "0700", "-o", "postgres", "-g", "postgres", dataDir,
	).CombinedOutput(); err != nil {
		return commandFailure("postgresql data directory setup", out, err)
	}
	out, err := exec.Command(
		"runuser", "-u", "postgres", "--",
		"initdb", "--locale=C.UTF-8", "--encoding=UTF8", "-D", dataDir,
	).CombinedOutput()
	if err != nil {
		return commandFailure("postgresql cluster initialization", out, err)
	}
	return nil
}

func prepareArchMariaDB() error {
	const dataDir = "/var/lib/mysql"
	if _, err := os.Stat(filepath.Join(dataDir, "mysql")); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("mariadb data directory cannot be inspected: %w", err)
	}
	out, err := exec.Command(
		"mariadb-install-db",
		"--user=mysql", "--basedir=/usr", "--datadir="+dataDir,
	).CombinedOutput()
	if err != nil {
		return commandFailure("mariadb data directory initialization", out, err)
	}
	return nil
}

var clamAVDatabaseDir = "/var/lib/clamav"

func prepareArchClamAV() error {
	if clamAVSignaturesReady(clamAVDatabaseDir) {
		return nil
	}
	out, err := exec.Command("freshclam").CombinedOutput()
	if !clamAVSignaturesReady(clamAVDatabaseDir) {
		if err != nil {
			return commandFailure("clamav signature download", out, err)
		}
		return fmt.Errorf("clamav signature download completed without creating a daily signature database")
	}
	return nil
}

func clamAVSignaturesReady(dir string) bool {
	for _, name := range []string{"daily.cvd", "daily.cld", "daily.inc"} {
		if info, err := os.Stat(filepath.Join(dir, name)); err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

func commandFailure(label string, out []byte, err error) error {
	detail := strings.TrimSpace(string(out))
	if detail == "" {
		return fmt.Errorf("%s failed: %w", label, err)
	}
	return fmt.Errorf("%s failed: %w: %s", label, err, detail)
}
