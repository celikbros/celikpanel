package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

type cpmoveRecordedCommand struct {
	executable string
	args       []string
	stdin      []byte
	hasStdin   bool
	hasTimeout bool
	tempPath   string
	fileMode   os.FileMode
	dirMode    os.FileMode
	pathExists bool
}

type cpmoveRecordingRunner struct {
	calls      []cpmoveRecordedCommand
	failCall   int
	failErr    error
	failures   map[int]error
	inspectErr error
}

func (r *cpmoveRecordingRunner) Run(ctx context.Context, executable string, args []string, stdin io.Reader) error {
	_, hasTimeout := ctx.Deadline()
	call := cpmoveRecordedCommand{
		executable: executable,
		args:       append([]string(nil), args...),
		hasTimeout: hasTimeout,
	}
	if stdin != nil {
		call.hasStdin = true
		file, ok := stdin.(*os.File)
		if !ok {
			r.inspectErr = fmt.Errorf("stdin type = %T, want *os.File", stdin)
		} else {
			call.tempPath = file.Name()
			if info, err := file.Stat(); err != nil {
				r.inspectErr = fmt.Errorf("stat stdin file: %w", err)
			} else {
				call.fileMode = info.Mode().Perm()
			}
			if info, err := os.Stat(filepath.Dir(file.Name())); err != nil {
				r.inspectErr = fmt.Errorf("stat stdin directory: %w", err)
			} else {
				call.dirMode = info.Mode().Perm()
			}
			if _, err := os.Stat(file.Name()); err == nil {
				call.pathExists = true
			} else if !os.IsNotExist(err) {
				r.inspectErr = fmt.Errorf("stat stdin path: %w", err)
			}
		}
		data, err := io.ReadAll(stdin)
		if err != nil {
			r.inspectErr = fmt.Errorf("read stdin: %w", err)
		} else {
			call.stdin = data
		}
	}
	r.calls = append(r.calls, call)
	if err, ok := r.failures[len(r.calls)]; ok {
		return err
	}
	if len(r.calls) == r.failCall {
		return r.failErr
	}
	return nil
}

func writeCpmoveDatabaseArchive(t *testing.T, dumpName, dump string) string {
	t.Helper()

	archiveRoot := t.TempDir()
	archivePath := filepath.Join(archiveRoot, "backup.tar.gz")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	header := &tar.Header{
		Name: "cpmove-user/mysql/" + dumpName + ".sql",
		Mode: 0o600,
		Size: int64(len(dump)),
	}
	if err := tarWriter.WriteHeader(header); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(tarWriter, dump); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(archivePath, 0o600); err != nil {
		t.Fatal(err)
	}
	previousRoot := cpmoveArchiveRoot
	cpmoveArchiveRoot = archiveRoot
	t.Cleanup(func() { cpmoveArchiveRoot = previousRoot })
	if runtime.GOOS != "windows" {
		previousOwner := cpmoveArchiveOwnerUID
		cpmoveArchiveOwnerUID = uint32(os.Geteuid())
		t.Cleanup(func() { cpmoveArchiveOwnerUID = previousOwner })
	}
	return archivePath
}

func TestImportCpmoveDatabaseUsesDirectMySQLAndSecureStdin(t *testing.T) {
	const dump = "INSERT INTO secrets VALUES ('opaque $(touch /tmp/pwn); `whoami`');\n"
	archivePath := writeCpmoveDatabaseArchive(t, "source_db", dump)
	runner := &cpmoveRecordingRunner{}
	resp := &CpmoveImportDBResponse{}

	if err := importCpmoveDatabase(&CpmoveImportDBRequest{
		Path:     archivePath,
		DumpName: "source_db",
		TargetDB: "target_db",
	}, resp, runner); err != nil {
		t.Fatal(err)
	}
	if !resp.Imported || resp.Error != "" {
		t.Fatalf("response = %+v, want successful import", resp)
	}
	if runner.inspectErr != nil {
		t.Fatal(runner.inspectErr)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("command count = %d, want 2", len(runner.calls))
	}

	wantArgs := [][]string{
		{"--execute", "CREATE DATABASE `target_db`;"},
		{"--database", "target_db"},
	}
	for i, call := range runner.calls {
		if call.executable != "mysql" {
			t.Fatalf("call %d executable = %q, want direct mysql invocation", i, call.executable)
		}
		if !call.hasTimeout {
			t.Fatalf("call %d has no deadline", i)
		}
		if !reflect.DeepEqual(call.args, wantArgs[i]) {
			t.Fatalf("call %d args = %#v, want %#v", i, call.args, wantArgs[i])
		}
		for _, arg := range call.args {
			if strings.Contains(arg, "$(touch") || strings.Contains(arg, "`whoami`") ||
				(call.tempPath != "" && strings.Contains(arg, call.tempPath)) {
				t.Fatalf("dump data or temp path leaked into command argument %q", arg)
			}
		}
	}
	if runner.calls[0].hasStdin {
		t.Fatal("database creation unexpectedly received stdin")
	}
	importCall := runner.calls[1]
	if !importCall.hasStdin || string(importCall.stdin) != dump {
		t.Fatalf("import stdin = %q, want exact opaque dump", importCall.stdin)
	}
	if importCall.fileMode != 0o600 {
		t.Fatalf("dump file mode = %#o, want 0600", importCall.fileMode)
	}
	if importCall.dirMode != 0o700 {
		t.Fatalf("dump directory mode = %#o, want 0700", importCall.dirMode)
	}
	if runtime.GOOS != "windows" && importCall.pathExists {
		t.Fatal("temporary dump pathname remained visible while mysql was running")
	}
	if _, err := os.Stat(importCall.tempPath); !os.IsNotExist(err) {
		t.Fatalf("temporary dump still exists after import: %v", err)
	}
}

func TestImportCpmoveDatabaseRejectsMetacharactersBeforeExecution(t *testing.T) {
	tests := []struct {
		name     string
		dumpName string
		targetDB string
		wantErr  string
	}{
		{name: "target database", dumpName: "source_db", targetDB: "target;touch_pwn", wantErr: "invalid target database name"},
		{name: "dump name", dumpName: "source;touch_pwn", targetDB: "target_db", wantErr: "invalid dump name"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &cpmoveRecordingRunner{}
			resp := &CpmoveImportDBResponse{}
			err := importCpmoveDatabase(&CpmoveImportDBRequest{
				Path:     filepath.Join(t.TempDir(), "does-not-matter.tar.gz"),
				DumpName: test.dumpName,
				TargetDB: test.targetDB,
			}, resp, runner)
			if err != nil {
				t.Fatal(err)
			}
			if resp.Error != test.wantErr {
				t.Fatalf("error = %q, want %q", resp.Error, test.wantErr)
			}
			if len(runner.calls) != 0 {
				t.Fatalf("executed %d command(s) for rejected input", len(runner.calls))
			}
		})
	}
}

func TestImportCpmoveDatabaseDoesNotLeakDumpSecretsInErrors(t *testing.T) {
	const secret = "dump-password-NEVER-RETURN"
	archivePath := writeCpmoveDatabaseArchive(t, "source_db", "SELECT '"+secret+"';\n")
	tests := []struct {
		name    string
		failErr error
		wantErr string
	}{
		{name: "client failure", failErr: errors.New("mysql echoed " + secret), wantErr: "mysql import failed"},
		{name: "timeout", failErr: context.DeadlineExceeded, wantErr: "mysql import timed out"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &cpmoveRecordingRunner{failCall: 2, failErr: test.failErr}
			resp := &CpmoveImportDBResponse{}
			err := importCpmoveDatabase(&CpmoveImportDBRequest{
				Path:     archivePath,
				DumpName: "source_db",
				TargetDB: "target_db",
			}, resp, runner)
			if err != nil {
				t.Fatal(err)
			}
			if resp.Imported {
				t.Fatal("failed import reported success")
			}
			if resp.Error != test.wantErr {
				t.Fatalf("error = %q, want %q", resp.Error, test.wantErr)
			}
			if strings.Contains(resp.Error, secret) {
				t.Fatalf("dump secret leaked in response: %q", resp.Error)
			}
			if len(runner.calls) != 3 || !runner.calls[1].hasTimeout {
				t.Fatalf("import and cleanup commands were not executed: %+v", runner.calls)
			}
			cleanup := runner.calls[2]
			wantCleanup := []string{"--execute", "DROP DATABASE IF EXISTS `target_db`;"}
			if cleanup.executable != "mysql" || !cleanup.hasTimeout ||
				cleanup.hasStdin || !reflect.DeepEqual(cleanup.args, wantCleanup) {
				t.Fatalf("cleanup command = %+v, want deadline-bound direct DROP", cleanup)
			}
			if _, err := os.Stat(runner.calls[1].tempPath); !os.IsNotExist(err) {
				t.Fatalf("temporary dump still exists after failure: %v", err)
			}
		})
	}
}

func TestImportCpmoveDatabaseDoesNotDropWhenExclusiveCreateFails(t *testing.T) {
	archivePath := writeCpmoveDatabaseArchive(t, "source_db", "SELECT 1;\n")
	runner := &cpmoveRecordingRunner{failCall: 1, failErr: errors.New("database exists")}
	resp := &CpmoveImportDBResponse{}

	if err := importCpmoveDatabase(&CpmoveImportDBRequest{
		Path:     archivePath,
		DumpName: "source_db",
		TargetDB: "target_db",
	}, resp, runner); err != nil {
		t.Fatal(err)
	}
	if resp.Imported || resp.Error != "mysql create failed" {
		t.Fatalf("response = %+v, want exclusive create failure", resp)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("command count = %d, want no DROP after failed CREATE", len(runner.calls))
	}
}

func TestImportCpmoveDatabaseReportsFailedCompensation(t *testing.T) {
	archivePath := writeCpmoveDatabaseArchive(t, "source_db", "SELECT 1;\n")
	runner := &cpmoveRecordingRunner{failures: map[int]error{
		2: errors.New("import failed"),
		3: errors.New("drop failed"),
	}}
	resp := &CpmoveImportDBResponse{}

	if err := importCpmoveDatabase(&CpmoveImportDBRequest{
		Path:     archivePath,
		DumpName: "source_db",
		TargetDB: "target_db",
	}, resp, runner); err != nil {
		t.Fatal(err)
	}
	if resp.Imported {
		t.Fatal("failed import and cleanup reported success")
	}
	want := "mysql import failed; database cleanup failed; manual reconciliation is required"
	if resp.Error != want {
		t.Fatalf("error = %q, want %q", resp.Error, want)
	}
	if len(runner.calls) != 3 {
		t.Fatalf("command count = %d, want CREATE, import, and DROP", len(runner.calls))
	}
}
