package main

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// One-click application installers. An "app" is a recipe on top of the pieces
// the panel already provides — a site (docroot, vhost, PHP pool) and a
// database. WordPress is the first: download the official tarball, verify it,
// extract it into the docroot, write a wp-config.php wired to a real database
// with unique salts, and hand the files to the site user. After this the site
// serves WordPress's setup wizard — PHP, MySQL and WordPress all really wired.
//
// Tek tıkla uygulama kurucuları. Bir "uygulama", panelin zaten sağladığı
// parçaların üstüne bir reçetedir — bir site (docroot, vhost, PHP havuzu) ve
// bir veritabanı. WordPress ilki: resmi tarball'ı indir, doğrula, docroot'a
// aç, gerçek bir veritabanına ve benzersiz salt'lara bağlı bir wp-config.php
// yaz ve dosyaları site kullanıcısına ver. Sonrasında site WordPress'in
// kurulum sihirbazını sunar — PHP, MySQL ve WordPress gerçekten bağlı.

type InstallWordPressRequest struct {
	DocRoot  string `json:"doc_root"`
	DBName   string `json:"db_name"`
	DBUser   string `json:"db_user"`
	DBPass   string `json:"db_pass"`
	DBHost   string `json:"db_host"`
	Username string `json:"username"` // site's system user, for chown
}

type InstallWordPressResponse struct {
	Installed bool   `json:"installed"`
	Detail    string `json:"detail,omitempty"`
	Error     string `json:"error,omitempty"`
}

const wpTarballURL = "https://wordpress.org/latest.tar.gz"

func (a *Agent) InstallWordPress(req *InstallWordPressRequest, resp *InstallWordPressResponse) error {
	if req.DocRoot == "" || req.DBName == "" {
		resp.Error = "doc_root and db_name are required"
		return nil
	}
	// Refuse to overwrite an existing WordPress (or any non-empty install)
	// rather than clobber a live site.
	// Var olan bir WordPress'in (ya da boş olmayan herhangi bir kurulumun)
	// üzerine yazmayı reddet; canlı siteyi ezmektense.
	if fileExistsAgent(filepath.Join(req.DocRoot, "wp-config.php")) {
		resp.Error = "WordPress is already installed in this document root"
		return nil
	}

	tmpDir, err := os.MkdirTemp("", "celik-wp-*")
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	defer os.RemoveAll(tmpDir)

	tarball := filepath.Join(tmpDir, "wp.tar.gz")
	if err := downloadFile(wpTarballURL, tarball); err != nil {
		resp.Error = fmt.Sprintf("download failed: %v", err)
		return nil
	}
	// Verify against the checksum WordPress.org publishes next to the
	// tarball — a corrupted or tampered download must not reach the docroot.
	// Tarball'ın yanında WordPress.org'un yayınladığı sağlama ile doğrula —
	// bozuk ya da kurcalanmış indirme docroot'a ulaşmamalı.
	if err := verifySHA1(tarball, wpTarballURL+".sha1"); err != nil {
		resp.Error = fmt.Sprintf("integrity check failed: %v", err)
		return nil
	}

	// Extract; the tarball unpacks into a top-level wordpress/ directory.
	// Aç; tarball tepe-seviye bir wordpress/ dizinine açılır.
	if out, err := exec.Command("tar", "-xzf", tarball, "-C", tmpDir).CombinedOutput(); err != nil {
		resp.Error = fmt.Sprintf("extract failed: %s", strings.TrimSpace(string(out)))
		return nil
	}
	wpDir := filepath.Join(tmpDir, "wordpress")
	if !fileExistsAgent(wpDir) {
		resp.Error = "unexpected archive layout"
		return nil
	}

	// Move the WordPress files into the docroot (which already exists and is
	// owned by the site user).
	// WordPress dosyalarını docroot'a taşı (zaten var ve site kullanıcısına
	// ait).
	entries, _ := os.ReadDir(wpDir)
	for _, e := range entries {
		src := filepath.Join(wpDir, e.Name())
		dst := filepath.Join(req.DocRoot, e.Name())
		if out, err := exec.Command("cp", "-a", src, dst).CombinedOutput(); err != nil {
			resp.Error = fmt.Sprintf("copy %s: %s", e.Name(), strings.TrimSpace(string(out)))
			return nil
		}
	}

	dbHost := req.DBHost
	if dbHost == "" {
		dbHost = "localhost"
	}
	if err := os.WriteFile(filepath.Join(req.DocRoot, "wp-config.php"),
		[]byte(wpConfig(req.DBName, req.DBUser, req.DBPass, dbHost)), 0o640); err != nil {
		resp.Error = fmt.Sprintf("wp-config: %v", err)
		return nil
	}

	// Everything belongs to the site user so WordPress (running as that user
	// via PHP-FPM) can write uploads, plugins and updates.
	// Her şey site kullanıcısına ait olsun ki WordPress (PHP-FPM ile o
	// kullanıcı olarak çalışan) yüklemeleri, eklentileri ve güncellemeleri
	// yazabilsin.
	if req.Username != "" && os.Getenv("CELIKPANEL_MAIL_DIR") == "" {
		_ = exec.Command("chown", "-R", req.Username+":"+webServerGroup(), req.DocRoot).Run()
	}

	resp.Installed = true
	resp.Detail = "WordPress downloaded, configured and ready for setup"
	return nil
}

// wpConfig builds a complete, version-stable wp-config.php: DB constants, 8
// unique salts from crypto/rand, table prefix and the wp-settings require.
// Building it in full is more robust than editing wp-config-sample.php, whose
// shape changes between releases.
// wpConfig, eksiksiz ve sürümden bağımsız bir wp-config.php üretir: DB
// sabitleri, crypto/rand'dan 8 benzersiz salt, tablo öneki ve wp-settings
// require'ı. Tam üretmek, sürümler arası değişen wp-config-sample.php'yi
// düzenlemekten daha sağlamdır.
func wpConfig(dbName, dbUser, dbPass, dbHost string) string {
	salts := []string{"AUTH_KEY", "SECURE_AUTH_KEY", "LOGGED_IN_KEY", "NONCE_KEY",
		"AUTH_SALT", "SECURE_AUTH_SALT", "LOGGED_IN_SALT", "NONCE_SALT"}
	var b strings.Builder
	b.WriteString("<?php\n")
	fmt.Fprintf(&b, "define( 'DB_NAME', %s );\n", phpQuote(dbName))
	fmt.Fprintf(&b, "define( 'DB_USER', %s );\n", phpQuote(dbUser))
	fmt.Fprintf(&b, "define( 'DB_PASSWORD', %s );\n", phpQuote(dbPass))
	fmt.Fprintf(&b, "define( 'DB_HOST', %s );\n", phpQuote(dbHost))
	b.WriteString("define( 'DB_CHARSET', 'utf8mb4' );\n")
	b.WriteString("define( 'DB_COLLATE', '' );\n")
	for _, s := range salts {
		fmt.Fprintf(&b, "define( '%s', %s );\n", s, phpQuote(randomSalt()))
	}
	b.WriteString("$table_prefix = 'wp_';\n")
	b.WriteString("define( 'WP_DEBUG', false );\n")
	b.WriteString("if ( ! defined( 'ABSPATH' ) ) { define( 'ABSPATH', __DIR__ . '/' ); }\n")
	b.WriteString("require_once ABSPATH . 'wp-settings.php';\n")
	return b.String()
}

// phpQuote single-quotes a value for PHP source, escaping the two characters
// that matter inside single quotes.
// phpQuote, bir değeri PHP kaynağı için tek tırnağa alır; tek tırnak içinde
// önemli olan iki karakteri kaçırır.
func phpQuote(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "'", "\\'")
	return "'" + s + "'"
}

// randomSalt returns a 64-char high-entropy string for a WordPress key/salt.
// randomSalt, bir WordPress anahtarı/salt'ı için 64 karakterlik yüksek-entropi
// dizesi döndürür.
func randomSalt() string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*()-_=+[]{}"
	b := make([]byte, 64)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	for i := range b {
		b[i] = charset[int(b[i])%len(charset)]
	}
	return string(b)
}

// downloadFile streams a URL to a local path with a bounded timeout.
// downloadFile, bir URL'yi sınırlı zaman aşımıyla yerel bir yola akıtır.
func downloadFile(url, dest string) error {
	client := &http.Client{Timeout: 5 * time.Minute}
	res, err := client.Get(url)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", res.StatusCode)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, res.Body)
	return err
}

// verifySHA1 downloads the published .sha1 for a file and compares it.
// verifySHA1, bir dosya için yayınlanan .sha1'i indirir ve karşılaştırır.
func verifySHA1(path, sha1URL string) error {
	client := &http.Client{Timeout: 30 * time.Second}
	res, err := client.Get(sha1URL)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	want, err := io.ReadAll(io.LimitReader(res.Body, 128))
	if err != nil {
		return err
	}
	expected := strings.TrimSpace(strings.Fields(string(want))[0])

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, expected) {
		return fmt.Errorf("sha1 mismatch: got %s, want %s", got, expected)
	}
	return nil
}
