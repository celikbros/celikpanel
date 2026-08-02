package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/hostingpath"
	"github.com/alicelik/celikpanel/internal/hostname"
	"github.com/alicelik/celikpanel/internal/transport"
)

// WordPress installation is a privileged filesystem mutation. The panel sends
// immutable row identities; the agent derives the only accepted document root,
// stages a verified tree on the same filesystem, and publishes it with rename.
// No archive entry is ever extracted by a root shell command.

type InstallWordPressRequest = transport.InstallWordPressRequest
type InstallWordPressResponse = transport.InstallWordPressResponse

const (
	wpTarballURL          = "https://wordpress.org/latest.tar.gz"
	wpMaxCompressedBytes  = int64(64 << 20)
	wpMaxExtractedBytes   = int64(512 << 20)
	wpMaxExtractedFile    = int64(128 << 20)
	wpMaxArchiveEntries   = 50_000
	wpMaxArchivePathBytes = 4_096
)

type wordpressPathExchange interface {
	LockPaths() error
	Exchange() error
	PublishedRootMatches(os.FileInfo) (bool, error)
	SealOriginalRoot(os.FileInfo) error
	FinalizePublishedRoot(string) error
	SyncPublishedRoot() error
	SyncParents() error
	UnlockPaths() error
	Close() error
}

var (
	wordpressPrepareExchange = prepareWordPressPathExchange
	wordpressRemove          = os.Remove
	wordpressSyncTree        = syncWordPressTree
	wordpressSyncPublished   = func(exchange wordpressPathExchange) error {
		return exchange.SyncPublishedRoot()
	}
)

func (a *Agent) InstallWordPress(req *InstallWordPressRequest, resp *InstallWordPressResponse) error {
	if req == nil {
		resp.Error = "request is required"
		return nil
	}
	if err := requireExpectedBuildCommit(req.ExpectedBuildCommit, "WordPress installation"); err != nil {
		resp.Error = err.Error()
		return nil
	}
	canonicalDomain, domainErr := hostname.CanonicalFQDN(req.Domain)
	if req.SiteID <= 0 || req.SubscriptionID <= 0 || req.DomainID <= 0 ||
		!validWordPressOperationID(req.OperationID) || domainErr != nil ||
		canonicalDomain != req.Domain ||
		strings.TrimSpace(req.DBName) == "" || strings.TrimSpace(req.DBUser) == "" ||
		req.DBPass == "" || strings.TrimSpace(req.Username) == "" {
		resp.Error = "operation, site, subscription, domain, database and site-user identities are required"
		return nil
	}
	// Until publication begins, every failure is guaranteed to leave the live
	// document root unchanged. The panel may then remove only the database
	// resources proven to belong to this operation.
	resp.CompensationSafe = true

	docRoot, err := hostingpath.DocumentRoot(req.SubscriptionID, req.DomainID)
	if err != nil {
		resp.Error = "invalid hosting identity"
		return nil
	}

	mutationMu := siteMutationMutex(req.SiteID)
	mutationMu.Lock()
	defer mutationMu.Unlock()

	if _, err := inspectInstallableDocumentRoot(docRoot, canonicalDomain); err != nil {
		resp.Error = err.Error()
		return nil
	}

	downloadDir, err := os.MkdirTemp("", "celik-wp-download-*")
	if err != nil {
		resp.Error = fmt.Sprintf("prepare download: %v", err)
		return nil
	}
	defer os.RemoveAll(downloadDir)

	tarball := filepath.Join(downloadDir, "wordpress.tar.gz")
	if err := downloadFile(wpTarballURL, tarball); err != nil {
		resp.Error = fmt.Sprintf("download failed: %v", err)
		return nil
	}
	if err := verifySHA1(tarball, wpTarballURL+".sha1"); err != nil {
		resp.Error = fmt.Sprintf("integrity check failed: %v", err)
		return nil
	}

	sitesDir := filepath.Dir(filepath.Dir(docRoot))
	if err := requireCanonicalDirectory(sitesDir); err != nil {
		resp.Error = fmt.Sprintf("invalid hosting layout: %v", err)
		return nil
	}
	if err := requireWordPressStagingParent(sitesDir); err != nil {
		resp.Error = fmt.Sprintf("unsafe hosting layout: %v", err)
		return nil
	}
	stageDir, err := os.MkdirTemp(sitesDir, fmt.Sprintf(".wordpress-stage-%d-", req.DomainID))
	if err != nil {
		resp.Error = fmt.Sprintf("prepare staging directory: %v", err)
		return nil
	}
	removeStageOnReturn := true
	defer func() {
		if removeStageOnReturn {
			_ = os.RemoveAll(stageDir)
		}
	}()
	if err := os.Chmod(stageDir, 0o700); err != nil {
		resp.Error = fmt.Sprintf("protect staging directory: %v", err)
		return nil
	}

	if err := extractWordPressArchive(tarball, stageDir); err != nil {
		resp.Error = fmt.Sprintf("extract failed: %v", err)
		return nil
	}
	dbHost := strings.TrimSpace(req.DBHost)
	if dbHost == "" {
		dbHost = "localhost"
	}
	config, err := wpConfig(req.DBName, req.DBUser, req.DBPass, dbHost)
	if err != nil {
		resp.Error = fmt.Sprintf("generate wp-config: %v", err)
		return nil
	}
	if err := os.WriteFile(filepath.Join(stageDir, "wp-config.php"), []byte(config), 0o600); err != nil {
		resp.Error = fmt.Sprintf("write wp-config: %v", err)
		return nil
	}
	if err := applyWordPressOwnership(stageDir, req.Username); err != nil {
		resp.Error = fmt.Sprintf("apply site ownership: %v", err)
		return nil
	}

	// A tenant may have written into the document root while the download ran.
	// Recheck immediately before the atomic publication and never overwrite it.
	publishSnapshot, err := inspectInstallableDocumentRoot(docRoot, canonicalDomain)
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	warning, preserveOldRoot, compensationSafe, err := publishWordPressStage(
		stageDir,
		docRoot,
		sitesDir,
		canonicalDomain,
		publishSnapshot,
		func(exchange wordpressPathExchange) error {
			return exchange.FinalizePublishedRoot(req.Username)
		},
	)
	if err != nil {
		resp.CompensationSafe = compensationSafe
		resp.Error = fmt.Sprintf("publish failed: %v", err)
		return nil
	}
	removeStageOnReturn = !preserveOldRoot

	resp.CompensationSafe = false
	resp.Installed = true
	resp.Detail = "WordPress downloaded, configured and ready for setup"
	if warning != "" {
		resp.Detail += "; " + warning
	}
	return nil
}

func requireCanonicalDirectory(directory string) error {
	info, err := os.Lstat(directory)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is not a real directory", directory)
	}
	resolved, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return err
	}
	if filepath.Clean(resolved) != filepath.Clean(directory) {
		return fmt.Errorf("%s traverses a symbolic link", directory)
	}
	return nil
}

type wordpressDocumentRootSnapshot struct {
	info            os.FileInfo
	placeholderName string
}

func validWordPressOperationID(value string) bool {
	if len(value) != 32 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func inspectInstallableDocumentRoot(
	docRoot, domain string,
) (wordpressDocumentRootSnapshot, error) {
	if err := requireCanonicalDirectory(docRoot); err != nil {
		return wordpressDocumentRootSnapshot{}, fmt.Errorf("document root is unavailable: %w", err)
	}
	rootInfo, err := os.Lstat(docRoot)
	if err != nil {
		return wordpressDocumentRootSnapshot{}, fmt.Errorf("inspect document root: %w", err)
	}
	entries, err := os.ReadDir(docRoot)
	if err != nil {
		return wordpressDocumentRootSnapshot{}, fmt.Errorf("read document root: %w", err)
	}
	if len(entries) == 0 {
		return wordpressDocumentRootSnapshot{info: rootInfo}, nil
	}
	expectedName, expectedContent := celikPanelSitePlaceholder(domain, "php")
	if len(entries) != 1 || entries[0].Name() != expectedName {
		return wordpressDocumentRootSnapshot{},
			fmt.Errorf("document root contains customer content; existing content was not changed")
	}
	content, fileInfo, err := readWordPressPlaceholder(
		filepath.Join(docRoot, expectedName),
		int64(len(expectedContent)),
	)
	if err != nil || !fileInfo.Mode().IsRegular() || !bytes.Equal(content, expectedContent) {
		return wordpressDocumentRootSnapshot{},
			fmt.Errorf("document root placeholder is not the exact CelikPanel placeholder; existing content was not changed")
	}
	return wordpressDocumentRootSnapshot{
		info:            rootInfo,
		placeholderName: expectedName,
	}, nil
}

func publishWordPressStage(
	stageDir, docRoot, sitesDir, domain string,
	expected wordpressDocumentRootSnapshot,
	finalize func(wordpressPathExchange) error,
) (warning string, preserveOldRoot bool, compensationSafe bool, returnErr error) {
	stageInfo, err := os.Lstat(stageDir)
	if err != nil {
		return "", false, true, fmt.Errorf("inspect staged root: %w", err)
	}
	if !stageInfo.IsDir() || stageInfo.Mode()&os.ModeSymlink != 0 {
		return "", false, true, fmt.Errorf("staged root is not a real directory")
	}
	if err := wordpressSyncTree(stageDir); err != nil {
		return "", false, true, fmt.Errorf("sync staged tree: %w", err)
	}
	exchanger, err := wordpressPrepareExchange(stageDir, docRoot)
	if err != nil {
		return "", false, true, fmt.Errorf("prepare atomic document-root exchange: %w", err)
	}
	defer exchanger.Close()
	if err := exchanger.LockPaths(); err != nil {
		return "", false, true, fmt.Errorf("lock document-root paths: %w", err)
	}
	if err := exchanger.Exchange(); err != nil {
		unlockErr := exchanger.UnlockPaths()
		return "", false, true, errors.Join(
			fmt.Errorf("atomically exchange staged tree: %w", err),
			unlockErr,
		)
	}
	restore := func(cause error) (bool, error) {
		if err := exchanger.Exchange(); err != nil {
			return false, fmt.Errorf("%v; restore original document root: %w", cause, err)
		}
		syncErr := exchanger.SyncParents()
		unlockErr := exchanger.UnlockPaths()
		if syncErr != nil || unlockErr != nil {
			var syncFailure, unlockFailure error
			if syncErr != nil {
				syncFailure = fmt.Errorf("sync restored document root: %w", syncErr)
			}
			if unlockErr != nil {
				unlockFailure = fmt.Errorf("unlock restored document root: %w", unlockErr)
			}
			return false, errors.Join(
				cause,
				syncFailure,
				unlockFailure,
			)
		}
		return true, cause
	}
	publishedMatches, err := exchanger.PublishedRootMatches(stageInfo)
	if err != nil || !publishedMatches {
		if err == nil {
			err = fmt.Errorf("published document root is no longer reachable at its canonical path")
		}
		safe, restoreErr := restore(fmt.Errorf("validate published document root: %w", err))
		return "", false, safe, restoreErr
	}

	actual, err := inspectInstallableDocumentRoot(stageDir, domain)
	if err != nil || !os.SameFile(expected.info, actual.info) ||
		expected.placeholderName != actual.placeholderName {
		if err == nil {
			err = fmt.Errorf("document root identity changed during publication")
		}
		safe, restoreErr := restore(fmt.Errorf("validate exchanged document root: %w", err))
		return "", false, safe, restoreErr
	}
	if finalize != nil {
		if err := finalize(exchanger); err != nil {
			safe, restoreErr := restore(fmt.Errorf("finalize published tree: %w", err))
			return "", false, safe, restoreErr
		}
	}
	if err := wordpressSyncPublished(exchanger); err != nil {
		safe, restoreErr := restore(fmt.Errorf("sync published tree: %w", err))
		return "", false, safe, restoreErr
	}
	if err := exchanger.SyncParents(); err != nil {
		safe, restoreErr := restore(fmt.Errorf("sync publication directories: %w", err))
		return "", false, safe, restoreErr
	}
	if err := exchanger.SealOriginalRoot(expected.info); err != nil {
		if unlockErr := exchanger.UnlockPaths(); unlockErr != nil {
			return "WordPress is live, but the previous document root and site-home permissions require administrator review",
				true, false, nil
		}
		return "WordPress is live, but the previous document root could not be sealed and was preserved for administrator review",
			true, false, nil
	}
	if err := exchanger.UnlockPaths(); err != nil {
		return "WordPress is live, but site-home write permissions could not be restored automatically",
			true, false, nil
	}
	// A tenant process may still hold a writable descriptor for the canonical
	// placeholder that existed before publication. Even after sealing the old
	// root, deleting that pathname would create a final content-loss race. Keep
	// this tiny, root-private recovery tree instead; an empty old root remains
	// safe to remove below.
	if expected.placeholderName != "" {
		return "the previous CelikPanel placeholder was preserved in a private recovery path",
			true, false, nil
	}

	actual, err = inspectInstallableDocumentRoot(stageDir, domain)
	if err != nil || !os.SameFile(expected.info, actual.info) ||
		expected.placeholderName != actual.placeholderName {
		return "the previous document root changed during cleanup and was preserved in a private recovery path",
			true, false, nil
	}
	if err := wordpressRemove(stageDir); err != nil {
		return "unexpected files appeared during cleanup and were preserved in a private recovery path",
			true, false, nil
	}
	if err := exchanger.SyncParents(); err != nil {
		return "WordPress is live, but cleanup directory durability could not be confirmed", false, false, nil
	}
	return "", false, false, nil
}

func extractWordPressArchive(archivePath, destination string) error {
	entries, err := os.ReadDir(destination)
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return fmt.Errorf("staging directory is not empty")
	}

	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	reader := tar.NewReader(gz)
	seen := make(map[string]struct{})
	var total int64
	count := 0
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		count++
		if count > wpMaxArchiveEntries {
			return fmt.Errorf("archive contains too many entries")
		}
		if len(header.Name) == 0 || len(header.Name) > wpMaxArchivePathBytes ||
			strings.ContainsRune(header.Name, '\x00') || strings.ContainsRune(header.Name, rune(92)) {
			return fmt.Errorf("archive contains an invalid path")
		}
		cleanName := path.Clean(header.Name)
		if path.IsAbs(cleanName) || cleanName == "." || cleanName == ".." ||
			strings.HasPrefix(cleanName, "../") ||
			(cleanName != "wordpress" && !strings.HasPrefix(cleanName, "wordpress/")) {
			return fmt.Errorf("archive entry escapes the wordpress directory: %q", header.Name)
		}
		if _, exists := seen[cleanName]; exists {
			return fmt.Errorf("archive contains duplicate entry %q", cleanName)
		}
		seen[cleanName] = struct{}{}
		if cleanName == "wordpress" {
			if header.Typeflag != tar.TypeDir {
				return fmt.Errorf("wordpress archive root is not a directory")
			}
			continue
		}

		rel := strings.TrimPrefix(cleanName, "wordpress/")
		target := filepath.Join(destination, filepath.FromSlash(rel))
		relCheck, err := filepath.Rel(destination, target)
		if err != nil || relCheck == ".." || strings.HasPrefix(relCheck, ".."+string(filepath.Separator)) {
			return fmt.Errorf("archive entry escapes staging directory")
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o750); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if header.Size < 0 || header.Size > wpMaxExtractedFile ||
				total > wpMaxExtractedBytes-header.Size {
				return fmt.Errorf("archive exceeds extraction limits")
			}
			total += header.Size
			if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
			if err != nil {
				return err
			}
			_, copyErr := io.CopyN(out, reader, header.Size)
			closeErr := out.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		default:
			return fmt.Errorf("archive entry %q uses unsupported type %d", cleanName, header.Typeflag)
		}
	}

	for _, required := range []struct {
		name string
		dir  bool
	}{
		{name: "wp-settings.php"},
		{name: "wp-admin", dir: true},
		{name: "wp-includes", dir: true},
	} {
		info, err := os.Lstat(filepath.Join(destination, required.name))
		if err != nil || info.Mode()&os.ModeSymlink != 0 || info.IsDir() != required.dir {
			return fmt.Errorf("archive is missing required WordPress path %s", required.name)
		}
	}
	return nil
}

func wpConfig(dbName, dbUser, dbPass, dbHost string) (string, error) {
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
	for _, saltName := range salts {
		salt, err := randomSalt()
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "define( '%s', %s );\n", saltName, phpQuote(salt))
	}
	b.WriteString("$table_prefix = 'wp_';\n")
	b.WriteString("define( 'WP_DEBUG', false );\n")
	b.WriteString("if ( ! defined( 'ABSPATH' ) ) { define( 'ABSPATH', __DIR__ . '/' ); }\n")
	b.WriteString("require_once ABSPATH . 'wp-settings.php';\n")
	return b.String(), nil
}

func phpQuote(value string) string {
	backslash := string(rune(92))
	value = strings.ReplaceAll(value, backslash, backslash+backslash)
	value = strings.ReplaceAll(value, "'", backslash+"'")
	return "'" + value + "'"
}

func randomSalt() (string, error) {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*()-_=+[]{}"
	random := make([]byte, 64)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	for index := range random {
		random[index] = charset[int(random[index])%len(charset)]
	}
	return string(random), nil
}

func wpHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 || req.URL.Scheme != "https" {
				return fmt.Errorf("unsafe or excessive redirect")
			}
			return nil
		},
	}
}

func downloadFile(url, destination string) (resultErr error) {
	response, err := wpHTTPClient(5 * time.Minute).Get(url)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", response.StatusCode)
	}

	f, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if resultErr != nil {
			_ = os.Remove(destination)
		}
	}()
	written, copyErr := io.Copy(f, io.LimitReader(response.Body, wpMaxCompressedBytes+1))
	if copyErr == nil && written > wpMaxCompressedBytes {
		copyErr = fmt.Errorf("download exceeds %d bytes", wpMaxCompressedBytes)
	}
	if copyErr == nil {
		copyErr = f.Sync()
	}
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func verifySHA1(filePath, checksumURL string) error {
	response, err := wpHTTPClient(30 * time.Second).Get(checksumURL)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("checksum HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 129))
	if err != nil {
		return err
	}
	if len(body) > 128 {
		return fmt.Errorf("checksum response is too large")
	}
	fields := strings.Fields(string(body))
	if len(fields) == 0 || len(fields[0]) != sha1.Size*2 {
		return fmt.Errorf("invalid checksum response")
	}
	if _, err := hex.DecodeString(fields[0]); err != nil {
		return fmt.Errorf("invalid checksum response")
	}

	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()
	hash := sha1.New()
	if _, err := io.Copy(hash, f); err != nil {
		return err
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if !strings.EqualFold(actual, fields[0]) {
		return fmt.Errorf("sha1 mismatch")
	}
	return nil
}
