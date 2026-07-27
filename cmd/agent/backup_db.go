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
	"sort"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/backupspec"
	"github.com/alicelik/celikpanel/internal/services"
)

const maxBackupCommandStderr = 64 << 10

type backupCommand struct {
	Name string
	Args []string
}

var backupCommandContext = exec.CommandContext
var backupDatabaseTimeout = 2 * time.Hour

var dumpDatabaseToFile = dumpDatabase
var restoreDatabaseFromFile = restoreDatabase

func validateDatabaseIdentity(database backupspec.DatabaseIdentity) (backupspec.DatabaseIdentity, error) {
	if database.ID < 1 {
		return backupspec.DatabaseIdentity{}, errors.New("invalid database ID")
	}
	if err := services.ValidateSQLIdentifier(database.Name); err != nil {
		return backupspec.DatabaseIdentity{}, fmt.Errorf("invalid database name: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(database.Type)) {
	case "", "mysql", "mariadb":
		database.Type = "mysql"
	case "postgresql", "postgres":
		database.Type = "postgresql"
	default:
		return backupspec.DatabaseIdentity{}, errors.New("unsupported database type")
	}
	return database, nil
}

func validateDatabaseSet(databases []backupspec.DatabaseIdentity) ([]backupspec.DatabaseIdentity, error) {
	result := make([]backupspec.DatabaseIdentity, 0, len(databases))
	seenIDs := make(map[int]bool, len(databases))
	seenTargets := make(map[string]bool, len(databases))
	for _, database := range databases {
		normalized, err := validateDatabaseIdentity(database)
		if err != nil {
			return nil, err
		}
		if seenIDs[normalized.ID] {
			return nil, errors.New("duplicate database ID")
		}
		targetKey := normalized.Type + "\x00" + normalized.Name
		if seenTargets[targetKey] {
			return nil, errors.New("duplicate database target")
		}
		seenIDs[normalized.ID] = true
		seenTargets[targetKey] = true
		result = append(result, normalized)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func databaseDumpCommand(database backupspec.DatabaseIdentity) (backupCommand, error) {
	database, err := validateDatabaseIdentity(database)
	if err != nil {
		return backupCommand{}, err
	}
	if database.Type == "postgresql" {
		return backupCommand{
			Name: "sudo",
			Args: []string{"-u", "postgres", "pg_dump", "--no-owner", "--no-privileges", "--dbname", database.Name},
		}, nil
	}
	return backupCommand{
		Name: "mysqldump",
		Args: []string{
			"--protocol=socket", "--user=root", "--single-transaction",
			"--routines", "--triggers", "--skip-lock-tables", database.Name,
		},
	}, nil
}

func databaseRestoreCommand(database backupspec.DatabaseIdentity) (backupCommand, error) {
	database, err := validateDatabaseIdentity(database)
	if err != nil {
		return backupCommand{}, err
	}
	if database.Type == "postgresql" {
		return backupCommand{
			Name: "sudo",
			Args: []string{"-u", "postgres", "psql", "--set", "ON_ERROR_STOP=on", "--dbname", database.Name},
		}, nil
	}
	return backupCommand{
		Name: "mysql",
		Args: []string{"--protocol=socket", "--user=root", "--database", database.Name},
	}, nil
}

type limitedBuffer struct {
	buffer bytes.Buffer
	left   int
}

func (b *limitedBuffer) Write(data []byte) (int, error) {
	original := len(data)
	if b.left > 0 {
		keep := len(data)
		if keep > b.left {
			keep = b.left
		}
		_, _ = b.buffer.Write(data[:keep])
		b.left -= keep
	}
	return original, nil
}

func runBackupCommand(ctx context.Context, command backupCommand, stdin io.Reader, stdout io.Writer) error {
	cmd := backupCommandContext(ctx, command.Name, command.Args...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	stderr := &limitedBuffer{left: maxBackupCommandStderr}
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.buffer.String())
		if message != "" {
			return fmt.Errorf("%s failed: %w: %s", command.Name, err, message)
		}
		return fmt.Errorf("%s failed: %w", command.Name, err)
	}
	return nil
}

func dumpDatabase(database backupspec.DatabaseIdentity, destination string) (returnErr error) {
	command, err := databaseDumpCommand(database)
	if err != nil {
		return err
	}
	out, err := openPrivateExclusive(destination)
	if err != nil {
		return err
	}
	defer func() {
		if returnErr != nil {
			_ = out.Close()
			_ = os.Remove(destination)
		}
	}()
	gz := gzip.NewWriter(out)
	ctx, cancel := context.WithTimeout(context.Background(), backupDatabaseTimeout)
	defer cancel()
	if err := runBackupCommand(ctx, command, nil, gz); err != nil {
		_ = gz.Close()
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	if err := out.Sync(); err != nil {
		return err
	}
	return out.Close()
}

func restoreDatabase(database backupspec.DatabaseIdentity, source string) error {
	command, err := databaseRestoreCommand(database)
	if err != nil {
		return err
	}
	file, _, err := secureOpenRegular(source)
	if err != nil {
		return err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gz.Close()
	ctx, cancel := context.WithTimeout(context.Background(), backupDatabaseTimeout)
	defer cancel()
	return runBackupCommand(ctx, command, gz, io.Discard)
}

func validateRestoreDatabase(expected, requested backupspec.DatabaseIdentity) (backupspec.DatabaseIdentity, error) {
	expected, err := validateDatabaseIdentity(expected)
	if err != nil {
		return backupspec.DatabaseIdentity{}, err
	}
	requested, err = validateDatabaseIdentity(requested)
	if err != nil {
		return backupspec.DatabaseIdentity{}, err
	}
	if expected != requested {
		return backupspec.DatabaseIdentity{}, errors.New("database identity mismatch")
	}
	return expected, nil
}

func validateFullRestoreSet(expected, requested []backupspec.DatabaseIdentity) ([]backupspec.DatabaseIdentity, error) {
	expected, err := validateDatabaseSet(expected)
	if err != nil {
		return nil, err
	}
	requested, err = validateDatabaseSet(requested)
	if err != nil {
		return nil, err
	}
	if len(expected) != len(requested) {
		return nil, errors.New("full restore database set mismatch")
	}
	for i := range expected {
		if expected[i] != requested[i] {
			return nil, errors.New("full restore database set mismatch")
		}
	}
	return expected, nil
}
