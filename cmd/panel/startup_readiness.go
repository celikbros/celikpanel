package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/secrets"
)

// The updater runs this closed mode as the panel account after the candidate's
// database is published and before completion.pending exists. It walks the
// normal start up to, but excluding, binding the listener and dialing the
// Agent. It never creates, migrates, repairs or seeds anything: where the
// normal start would create a file, the check either reports "would create"
// (SQLite sidecars, demo accounts) or fails (state an installed panel has
// already created on its first start). One product-authored line explains a
// failure; no path, key material or raw library error is printed.
//
// Güncelleyici bu kapalı kipi panel hesabıyla, aday veritabanı yayımlandıktan
// sonra ve completion.pending oluşmadan önce çalıştırır. Olağan açılışı
// dinleyiciyi bağlamadan ve Agent'a bağlanmadan önceki adıma kadar izler;
// hiçbir şey oluşturmaz, taşımaz, onarmaz. Hata tek, ürünün yazdığı bir satırdır.
const startupReadinessFlag = "--check-startup-readiness"

type startupReadinessFailure struct{ code, text string }

func (failure *startupReadinessFailure) Error() string {
	return "panel startup check failed: " + failure.code + ": " + failure.text
}

func startupFail(code, text string) error {
	return &startupReadinessFailure{code: code, text: text}
}

var errStartupWouldCreateTLS = errors.New("startup readiness: the normal start would create a TLS pair")

type startupReadinessReport struct {
	listen      string
	scheme      string
	pins        []string
	wouldCreate []string
}

// Test seams only; production always uses defaultStartupReadinessDeps.
type startupReadinessDeps struct {
	database func(path string) error
	license  func(file string) error
}

func defaultStartupReadinessDeps() startupReadinessDeps {
	return startupReadinessDeps{
		database: checkStartupDatabaseReadOnly,
		license: func(file string) error {
			_, err := newServerLicense(file)
			return err
		},
	}
}

// runStartupReadinessEntry handles the mode before ordinary flag parsing, so
// no other panel mode or runtime flag can be combined with it.
func runStartupReadinessEntry(args []string, stdout, stderr io.Writer) (bool, int) {
	present := false
	for _, argument := range args {
		if argument == startupReadinessFlag || strings.HasPrefix(argument, startupReadinessFlag+"=") {
			present = true
		}
	}
	if !present {
		return false, 0
	}
	if len(args) != 1 || args[0] != startupReadinessFlag {
		fmt.Fprintln(stderr, startupFail("usage", "the startup readiness check accepts no other argument").Error())
		return true, 2
	}
	report, err := checkStartupReadiness(defaultStartupReadinessDeps())
	if err != nil {
		var failure *startupReadinessFailure
		if !errors.As(err, &failure) {
			failure = &startupReadinessFailure{code: "unclassified", text: "the startup readiness check stopped for an unclassified reason"}
		}
		fmt.Fprintln(stderr, failure.Error())
		return true, 1
	}
	writeStartupReadinessReport(stdout, report)
	return true, 0
}

func writeStartupReadinessReport(w io.Writer, report startupReadinessReport) {
	fmt.Fprintf(w, "listen=%s\nscheme=%s\n", report.listen, report.scheme)
	for _, pin := range report.pins {
		fmt.Fprintf(w, "pin=%s\n", pin)
	}
	for _, item := range report.wouldCreate {
		fmt.Fprintf(w, "would_create=%s\n", item)
	}
	fmt.Fprintln(w, "ready")
}

// checkStartupReadiness follows the order of main(): data directory, database
// and users, secret key, license configuration, TLS and cookie posture, the
// served certificate pair, the listen address and the built interface.
func checkStartupReadiness(deps startupReadinessDeps) (startupReadinessReport, error) {
	var report startupReadinessReport
	info, err := os.Stat(dataDir())
	if errors.Is(err, os.ErrNotExist) || (err == nil && !info.IsDir()) {
		// Installed hosts always have the systemd StateDirectory. An empty new
		// directory would hold no database, key or TLS identity.
		return report, startupFail("data_directory_missing", "the panel data directory is missing or is not a directory")
	}
	if err != nil {
		return report, startupFail("data_directory_unreadable", "the panel data directory cannot be read by the panel account")
	}

	database := databaseFile()
	info, err = os.Lstat(database)
	if errors.Is(err, os.ErrNotExist) {
		// The normal start would create an empty database and then refuse to
		// run without a user; on an installed host that is never an update result.
		return report, startupFail("database_missing", "the panel database is missing; the normal start would create an empty one")
	}
	if err != nil || !info.Mode().IsRegular() {
		return report, startupFail("database_unverified", "the panel database is not a readable regular file")
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Lstat(database + suffix); errors.Is(err, os.ErrNotExist) {
			// Every normal SQLite open creates these; they are not required
			// beforehand and publication deliberately leaves them absent.
			report.wouldCreate = append(report.wouldCreate, "sqlite"+suffix)
		}
	}
	if deps.database == nil {
		return report, startupFail("database_unverified", "the panel database check is unavailable")
	}
	if err := deps.database(database); err != nil {
		var failure *startupReadinessFailure
		if errors.As(err, &failure) {
			return report, failure
		}
		return report, startupFail("database_unverified", "the panel database does not match this release's schema, migration history and idle operation queue")
	}

	if _, err := secrets.Load(filepath.Join(dataDir(), "secret.key")); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// An installed panel created this key on its first start. A new key
			// would silently orphan every credential sealed with the old one.
			return report, startupFail("secret_key_missing", "the panel secret key is missing; the normal start would create a new key and sealed credentials would become unreadable")
		}
		return report, startupFail("secret_key_invalid", "the panel secret key cannot be read or has the wrong size")
	}

	if deps.license == nil || deps.license(filepath.Join(dataDir(), "license.json")) != nil {
		// Only the configuration the start needs is checked. License state
		// (missing, expired, unverifiable) never stops the panel from starting.
		return report, startupFail("license_configuration_failed", "the license verification configuration cannot be loaded (machine identity or verification key)")
	}
	if os.Getenv("CELIKPANEL_PANEL_DEMO_FLAG") == "--demo" {
		report.wouldCreate = append(report.wouldCreate, "demo-accounts")
	}

	enabled, certPath, keyPath, err := tlsSettingsWith(func(string, string) error { return errStartupWouldCreateTLS })
	if errors.Is(err, errStartupWouldCreateTLS) {
		// The self-signed pair is created on an installed panel's first start;
		// a new one would change the identity owners and browsers already know.
		return report, startupFail("tls_pair_missing", "the panel TLS certificate pair is missing; the normal start would create a new self-signed certificate")
	}
	if err != nil {
		return report, startupFail("tls_settings_invalid", "the panel TLS settings are incomplete or the managed certificate link is invalid")
	}
	if !enabled && os.Getenv("CELIKPANEL_PANEL_INSECURE_COOKIES_FLAG") != "--insecure-cookies" {
		return report, startupFail("tls_required", "the panel would serve plain HTTP with secure cookies, which the normal start refuses")
	}
	server := &http.Server{}
	tlsOn, err := configurePanelHTTPTLS(server, certPath, keyPath)
	if err != nil {
		return report, startupFail("tls_pair_invalid", "the panel TLS certificate and private key cannot be loaded as a matching pair")
	}
	report.scheme = "http"
	if tlsOn {
		report.scheme = "https"
		report.pins, err = servedPanelPublicKeyPins(server.TLSConfig)
		if err != nil {
			return report, startupFail("tls_pair_invalid", "the panel TLS certificate cannot be parsed")
		}
	}

	report.listen = listenAddr()
	if !validStartupListenAddress(report.listen) {
		return report, startupFail("listen_address_invalid", "the panel listen address is not a valid host and port")
	}

	index, err := os.Open(filepath.Join(webDir(), "index.html"))
	if err != nil {
		return report, startupFail("web_index_missing", "the web interface index.html is missing or unreadable; the panel would serve no interface")
	}
	indexInfo, err := index.Stat()
	index.Close()
	if err != nil || !indexInfo.Mode().IsRegular() {
		return report, startupFail("web_index_missing", "the web interface index.html is missing or unreadable; the panel would serve no interface")
	}
	return report, nil
}

func validStartupListenAddress(address string) bool {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 || strconv.Itoa(number) != port {
		return false
	}
	return host == "" || net.ParseIP(host) != nil || host == "localhost"
}

// servedPanelPublicKeyPins returns the SubjectPublicKeyInfo SHA-256 pins of
// every leaf the configured listener can present: the default certificate
// (served without SNI, e.g. to a loopback IP) and the SNI pair when they differ.
// These are public values; the updater pins its loopback probe to them.
func servedPanelPublicKeyPins(config *tls.Config) ([]string, error) {
	if config == nil || len(config.Certificates) == 0 {
		return nil, errors.New("panel TLS configuration has no certificate")
	}
	served := []tls.Certificate{config.Certificates[0]}
	if config.GetCertificate != nil {
		named, err := config.GetCertificate(&tls.ClientHelloInfo{ServerName: "panel.invalid"})
		if err != nil || named == nil {
			return nil, errors.New("panel TLS configuration has no named certificate")
		}
		served = append(served, *named)
	}
	pins := make([]string, 0, len(served))
	seen := map[string]bool{}
	for _, certificate := range served {
		if len(certificate.Certificate) == 0 {
			return nil, errors.New("panel TLS certificate chain is empty")
		}
		leaf, err := x509.ParseCertificate(certificate.Certificate[0])
		if err != nil {
			return nil, err
		}
		digest := sha256.Sum256(leaf.RawSubjectPublicKeyInfo)
		pin := "sha256//" + base64.StdEncoding.EncodeToString(digest[:])
		if !seen[pin] {
			seen[pin] = true
			pins = append(pins, pin)
		}
	}
	return pins, nil
}

// checkStartupDatabaseReadOnly never opens the canonical database or its
// sidecars through SQLite. It reuses the pinned private WAL-aware copy that the
// updater's idle and independent completion checks already trust (created
// under the fixed /tmp workspace and removed before return), then requires the
// exact embedded schema and migration history, an idle queue and a user.
func checkStartupDatabaseReadOnly(databasePath string) error {
	users := -1
	err := checkWALAwareServiceOperationsIdleWith(databasePath, func(snapshotPath string) error {
		if err := checkCompletedUpdateDatabase(snapshotPath); err != nil {
			return err
		}
		count, err := countStartupUsers(snapshotPath)
		users = count
		return err
	})
	if err != nil {
		return startupFail("database_unverified", "the panel database does not match this release's schema, migration history and idle operation queue")
	}
	if users < 1 {
		// The normal start exits when no user exists.
		return startupFail("no_users", "the panel database has no user account; the normal start refuses to run without one")
	}
	return nil
}

func countStartupUsers(snapshotPath string) (int, error) {
	database, err := sql.Open("sqlite", sqliteSnapshotURI(snapshotPath, true))
	if err != nil {
		return 0, err
	}
	defer database.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var count int
	if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}
