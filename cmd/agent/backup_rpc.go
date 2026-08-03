package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/hostingpath"
)

const (
	backupCommandTimeout   = 30 * time.Minute
	maxBackupDownloadBytes = 512 << 20
	maxBackupCommandError  = 64 << 10
)

var (
	databaseIdentifierPattern = regexp.MustCompile(`^[A-Za-z0-9_]{1,63}$`)
	filesBackupNamePattern    = regexp.MustCompile(`^(scheduled_)?(files|full)_[0-9]{8}_[0-9]{6}\.tar\.gz$`)
	databaseBackupNamePattern = regexp.MustCompile(`^db_(mysql|postgresql)_([1-9][0-9]*)_[0-9]{8}_[0-9]{6}\.sql\.gz$`)
)

// BackupInfo is deliberately path-free. A browser and the unprivileged panel
// only need an opaque leaf name; the root agent derives every filesystem path
// from immutable subscription/domain identities.
type BackupInfo struct {
	Name       string    `json:"name"`
	Size       int64     `json:"size"`
	Type       string    `json:"type"` // "full", "files", "database"
	DatabaseID int       `json:"database_id,omitempty"`
	Scheduled  bool      `json:"scheduled,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// BackupRequest contains identities, never a tenant-supplied domain name or
// an absolute document root. DatabaseName is resolved by the panel from
// DatabaseID after checking subscription+domain ownership; the privileged
// boundary validates the identifier again before exec.CommandContext.
type BackupRequest struct {
	SubscriptionID int    `json:"subscription_id"`
	DomainID       int    `json:"domain_id"`
	Type           string `json:"type"` // "full", "files", "database"
	DatabaseID     int    `json:"database_id,omitempty"`
	DatabaseName   string `json:"database_name,omitempty"`
	DatabaseType   string `json:"database_type,omitempty"` // "mysql", "postgresql"
	Scheduled      bool   `json:"scheduled,omitempty"`
}

type BackupResponse struct {
	Success bool       `json:"success"`
	Backup  BackupInfo `json:"backup,omitempty"`
	Error   string     `json:"error,omitempty"`
}

type ListBackupsRequest struct {
	SubscriptionID int `json:"subscription_id"`
	DomainID       int `json:"domain_id"`
}

type ListBackupsResponse struct {
	Backups []BackupInfo `json:"backups"`
}

type RestoreRequest struct {
	SubscriptionID int    `json:"subscription_id"`
	DomainID       int    `json:"domain_id"`
	BackupName     string `json:"backup_name"`
	DatabaseID     int    `json:"database_id,omitempty"`
	DatabaseName   string `json:"database_name,omitempty"`
	DatabaseType   string `json:"database_type,omitempty"`
}

type DeleteBackupRequest struct {
	SubscriptionID int    `json:"subscription_id"`
	DomainID       int    `json:"domain_id"`
	BackupName     string `json:"backup_name"`
}

type backupFileRecord struct {
	Name      string
	Size      int64
	CreatedAt time.Time
}

// Production uses a root-owned directory. The environment override is for
// isolated development/tests only; secure Linux helpers still require an
// absolute, symlink-free root and resolve all operations beneath it.
var backupBaseDir = func() string {
	if d := os.Getenv("CELIKPANEL_BACKUP_DIR"); d != "" {
		return d
	}
	return "/var/backups/celikpanel"
}()

func backupScope(subscriptionID, domainID int) (string, error) {
	if subscriptionID <= 0 || domainID <= 0 {
		return "", errors.New("subscription and domain IDs must be positive")
	}
	scope := path.Join(
		"subscriptions", strconv.Itoa(subscriptionID),
		"domains", strconv.Itoa(domainID),
	)
	if _, err := hostingpath.ValidateRelativePath(scope); err != nil {
		return "", err
	}
	return scope, nil
}

func normalizeDatabaseType(databaseType string) (string, error) {
	switch databaseType {
	case "mysql", "mariadb":
		return "mysql", nil
	case "postgresql":
		return "postgresql", nil
	default:
		return "", fmt.Errorf("unsupported database type: %s", databaseType)
	}
}

func validateDatabaseIdentity(databaseID int, databaseName, databaseType string) (string, error) {
	if databaseID <= 0 {
		return "", errors.New("database ID must be positive")
	}
	if !databaseIdentifierPattern.MatchString(databaseName) {
		return "", errors.New("database name is not an allowed identifier")
	}
	return normalizeDatabaseType(databaseType)
}

func generatedBackupName(backupType, databaseType string, databaseID int, now time.Time) (string, error) {
	return generatedBackupNameWithSchedule(backupType, databaseType, databaseID, now, false)
}

func generatedBackupNameWithSchedule(
	backupType, databaseType string,
	databaseID int,
	now time.Time,
	scheduled bool,
) (string, error) {
	timestamp := now.UTC().Format("20060102_150405")
	switch backupType {
	case "files", "full":
		prefix := ""
		if scheduled {
			prefix = "scheduled_"
		}
		return fmt.Sprintf("%s%s_%s.tar.gz", prefix, backupType, timestamp), nil
	case "database":
		if scheduled {
			return "", errors.New("scheduled database backups are not supported")
		}
		normalizedType, err := normalizeDatabaseType(databaseType)
		if err != nil {
			return "", err
		}
		if databaseID <= 0 {
			return "", errors.New("database ID must be positive")
		}
		return fmt.Sprintf("db_%s_%d_%s.sql.gz", normalizedType, databaseID, timestamp), nil
	default:
		return "", errors.New("invalid backup type")
	}
}

func parseBackupName(name string) (backupType, databaseType string, databaseID int, err error) {
	if err := hostingpath.ValidateFileName(name); err != nil {
		return "", "", 0, fmt.Errorf("invalid backup name: %w", err)
	}
	if match := filesBackupNamePattern.FindStringSubmatch(name); match != nil {
		return match[2], "", 0, nil
	}
	match := databaseBackupNamePattern.FindStringSubmatch(name)
	if match == nil {
		return "", "", 0, errors.New("unrecognized backup name")
	}
	databaseID64, parseErr := strconv.ParseInt(match[2], 10, 32)
	if parseErr != nil || databaseID64 <= 0 {
		return "", "", 0, errors.New("invalid database backup identity")
	}
	return "database", match[1], int(databaseID64), nil
}

func isScheduledBackupName(name string) bool {
	match := filesBackupNamePattern.FindStringSubmatch(name)
	return match != nil && match[1] != ""
}

func databaseDumpCommand(ctx context.Context, databaseName, databaseType string) (*exec.Cmd, error) {
	normalizedType, err := normalizeDatabaseType(databaseType)
	if err != nil {
		return nil, err
	}
	if !databaseIdentifierPattern.MatchString(databaseName) {
		return nil, errors.New("database name is not an allowed identifier")
	}
	switch normalizedType {
	case "mysql":
		return exec.CommandContext(ctx, "mysqldump",
			"--single-transaction", "--routines", "--triggers", databaseName), nil
	case "postgresql":
		return exec.CommandContext(ctx, "pg_dump", "--dbname", databaseName), nil
	default:
		panic("normalizeDatabaseType returned an unsupported value")
	}
}

func databaseRestoreCommand(ctx context.Context, databaseName, databaseType string) (*exec.Cmd, error) {
	normalizedType, err := normalizeDatabaseType(databaseType)
	if err != nil {
		return nil, err
	}
	if !databaseIdentifierPattern.MatchString(databaseName) {
		return nil, errors.New("database name is not an allowed identifier")
	}
	switch normalizedType {
	case "mysql":
		return exec.CommandContext(ctx, "mysql", databaseName), nil
	case "postgresql":
		return exec.CommandContext(ctx, "psql",
			"--dbname", databaseName, "--set", "ON_ERROR_STOP=1"), nil
	default:
		panic("normalizeDatabaseType returned an unsupported value")
	}
}

func boundedCommandError(buffer *bytes.Buffer) string {
	output := buffer.String()
	if len(output) > maxBackupCommandError {
		output = output[:maxBackupCommandError] + "…"
	}
	return strings.TrimSpace(output)
}

func createDatabaseBackup(
	ctx context.Context,
	scope, backupName, databaseName, databaseType string,
) (size int64, retErr error) {
	file, cleanup, err := secureCreateBackupFile(backupBaseDir, scope, backupName)
	if err != nil {
		return 0, err
	}
	keep := false
	defer func() {
		if closeErr := file.Close(); retErr == nil && closeErr != nil {
			retErr = closeErr
		}
		if !keep {
			cleanup()
		}
	}()

	gzipWriter := gzip.NewWriter(file)
	command, err := databaseDumpCommand(ctx, databaseName, databaseType)
	if err != nil {
		_ = gzipWriter.Close()
		return 0, err
	}
	var stderr bytes.Buffer
	command.Stdout = gzipWriter
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		_ = gzipWriter.Close()
		return 0, fmt.Errorf("database dump failed: %w: %s", err, boundedCommandError(&stderr))
	}
	if err := gzipWriter.Close(); err != nil {
		return 0, err
	}
	if err := file.Sync(); err != nil {
		return 0, err
	}
	info, err := file.Stat()
	if err != nil {
		return 0, err
	}
	keep = true
	return info.Size(), nil
}

func restoreDatabaseBackup(
	ctx context.Context,
	scope, backupName, databaseName, databaseType string,
) error {
	file, _, err := secureOpenBackupFile(backupBaseDir, scope, backupName)
	if err != nil {
		return err
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzipReader.Close()

	command, err := databaseRestoreCommand(ctx, databaseName, databaseType)
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	command.Stdin = gzipReader
	command.Stdout = io.Discard
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("database restore failed: %w: %s", err, boundedCommandError(&stderr))
	}
	return nil
}

func (a *Agent) CreateBackup(req *BackupRequest, resp *BackupResponse) error {
	scope, err := backupScope(req.SubscriptionID, req.DomainID)
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	backupName, err := generatedBackupNameWithSchedule(
		req.Type, req.DatabaseType, req.DatabaseID, time.Now(), req.Scheduled,
	)
	if err != nil {
		resp.Error = err.Error()
		return nil
	}

	var size int64
	switch req.Type {
	case "files", "full":
		if req.DatabaseID != 0 || req.DatabaseName != "" || req.DatabaseType != "" {
			resp.Error = "database fields are not allowed for file backups"
			return nil
		}
		documentRoot, err := hostingpath.DocumentRoot(req.SubscriptionID, req.DomainID)
		if err != nil {
			resp.Error = err.Error()
			return nil
		}
		size, err = secureCreateFilesBackup(
			documentRoot, backupBaseDir, scope, backupName,
		)
		if err != nil {
			resp.Error = fmt.Sprintf("failed to create files backup: %v", err)
			return nil
		}
	case "database":
		normalizedType, err := validateDatabaseIdentity(
			req.DatabaseID, req.DatabaseName, req.DatabaseType,
		)
		if err != nil {
			resp.Error = err.Error()
			return nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), backupCommandTimeout)
		defer cancel()
		size, err = createDatabaseBackup(
			ctx, scope, backupName, req.DatabaseName, normalizedType,
		)
		if err != nil {
			resp.Error = fmt.Sprintf("failed to create database backup: %v", err)
			return nil
		}
	default:
		resp.Error = "invalid backup type"
		return nil
	}

	resp.Success = true
	resp.Backup = BackupInfo{
		Name:       backupName,
		Size:       size,
		Type:       req.Type,
		DatabaseID: req.DatabaseID,
		Scheduled:  req.Scheduled,
		CreatedAt:  time.Now().UTC(),
	}
	return nil
}

func (a *Agent) ListBackups(req *ListBackupsRequest, resp *ListBackupsResponse) error {
	scope, err := backupScope(req.SubscriptionID, req.DomainID)
	if err != nil {
		return err
	}
	records, err := secureListBackupFiles(backupBaseDir, scope)
	if err != nil {
		return err
	}
	resp.Backups = make([]BackupInfo, 0, len(records))
	for _, record := range records {
		backupType, _, databaseID, err := parseBackupName(record.Name)
		if err != nil {
			return fmt.Errorf("unsafe backup directory entry %q: %w", record.Name, err)
		}
		resp.Backups = append(resp.Backups, BackupInfo{
			Name:       record.Name,
			Size:       record.Size,
			Type:       backupType,
			DatabaseID: databaseID,
			Scheduled:  isScheduledBackupName(record.Name),
			CreatedAt:  record.CreatedAt,
		})
	}
	sort.Slice(resp.Backups, func(i, j int) bool {
		return resp.Backups[i].CreatedAt.After(resp.Backups[j].CreatedAt)
	})
	return nil
}

func (a *Agent) RestoreBackup(req *RestoreRequest, resp *BackupResponse) error {
	scope, err := backupScope(req.SubscriptionID, req.DomainID)
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	backupType, nameDatabaseType, nameDatabaseID, err := parseBackupName(req.BackupName)
	if err != nil {
		resp.Error = err.Error()
		return nil
	}

	switch backupType {
	case "files", "full":
		if req.DatabaseID != 0 || req.DatabaseName != "" || req.DatabaseType != "" {
			resp.Error = "database fields are not allowed for file restore"
			return nil
		}
		documentRoot, err := hostingpath.DocumentRoot(req.SubscriptionID, req.DomainID)
		if err != nil {
			resp.Error = err.Error()
			return nil
		}
		if err := secureRestoreFilesBackup(
			documentRoot, backupBaseDir, scope, req.BackupName,
		); err != nil {
			resp.Error = fmt.Sprintf("failed to restore files backup: %v", err)
			return nil
		}
	case "database":
		normalizedType, err := validateDatabaseIdentity(
			req.DatabaseID, req.DatabaseName, req.DatabaseType,
		)
		if err != nil {
			resp.Error = err.Error()
			return nil
		}
		if req.DatabaseID != nameDatabaseID || normalizedType != nameDatabaseType {
			resp.Error = "database backup identity does not match the restore target"
			return nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), backupCommandTimeout)
		defer cancel()
		if err := restoreDatabaseBackup(
			ctx, scope, req.BackupName, req.DatabaseName, normalizedType,
		); err != nil {
			resp.Error = fmt.Sprintf("failed to restore database backup: %v", err)
			return nil
		}
	default:
		resp.Error = "unsupported backup type"
		return nil
	}
	resp.Success = true
	return nil
}

func (a *Agent) DeleteBackup(req *DeleteBackupRequest, resp *bool) error {
	scope, err := backupScope(req.SubscriptionID, req.DomainID)
	if err != nil {
		return err
	}
	if _, _, _, err := parseBackupName(req.BackupName); err != nil {
		return err
	}
	if err := secureDeleteBackupFile(backupBaseDir, scope, req.BackupName); err != nil {
		return err
	}
	*resp = true
	return nil
}
