//go:build linux

package main

import (
	"bytes"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
)

// Run the actual rollback proof block with the actual standalone checker and a
// database left by SIGKILL. Agent admission is outside this block's test scope;
// only its final no-op probe is substituted after the real ledger comparison.
func TestPendingRollbackReadsCrashWALWithoutReplacingSource(t *testing.T) {
	repository, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	sourceList, err := os.ReadFile(filepath.Join(repository, "deploy/recovery/panel-checker.sources"))
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "panel-checker")
	build := exec.Command("go", append([]string{"build", "-o", binary}, strings.Fields(string(sourceList))...)...)
	build.Dir = repository
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("standalone build: %v\n%s", err, output)
	}
	script, err := os.ReadFile(filepath.Join(repository, "rollback.sh"))
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(string(script), "# A pending rollback may resume after its controlled panel start was killed\n")
	if start < 0 {
		t.Fatal("pending rollback proof block missing")
	}
	end := strings.Index(string(script)[start:], "\nfind \"$BIN_DIR\" \"$WEB_DIR\" -type f")
	if end < 0 {
		t.Fatal("pending rollback proof block end missing")
	}
	block := string(script)[start : start+end]
	for _, test := range []struct {
		name, transition, writer string
		version                  int
		pending, success         bool
	}{
		{"normal-pending-valid-wal", "normal", "metadata", 31, true, true},
		{"pre-ledger-pending-valid-wal", "pre-ledger", "metadata", 20, true, true},
		{"normal-fresh-still-rejects-wal", "normal", "metadata", 31, false, false},
		{"pre-ledger-fresh-still-rejects-wal", "pre-ledger", "metadata", 20, false, false},
		{"pending-queued-only-in-wal", "normal", "queued", 31, true, false},
		{"pending-corrupt-wal", "normal", "corrupt", 31, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "celikpanel.db")
			seedRecoveryCheckerDatabase(t, repository, path, test.version)
			writer := exec.Command(os.Args[0], "-test.run=^TestPendingRollbackCrashWALWriter$")
			writer.Env = append(os.Environ(), "CP_PENDING_WAL_DB="+path, "CP_PENDING_WAL_WRITE="+test.writer)
			output, err := writer.CombinedOutput()
			exit, ok := err.(*exec.ExitError)
			if !ok {
				t.Fatalf("writer must die by SIGKILL: %v\n%s", err, output)
			}
			status, ok := exit.Sys().(syscall.WaitStatus)
			if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
				t.Fatalf("writer did not crash: %v\n%s", err, output)
			}
			if test.writer == "corrupt" {
				file, err := os.OpenFile(path+"-wal", os.O_WRONLY, 0)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := file.WriteAt([]byte{0}, 0); err != nil {
					t.Fatal(err)
				}
				file.Close()
			}
			before := pendingRollbackSQLiteEvidence(t, path)
			wal, err := os.Stat(path + "-wal")
			if err != nil || wal.Size() <= 32 {
				t.Fatalf("real nonempty WAL missing: %v", err)
			}
			state := filepath.Join(root, "agent-state")
			snapshot := filepath.Join(root, "snapshot")
			for _, dir := range []string{state, filepath.Join(snapshot, "agent-state")} {
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if test.transition == "normal" {
				for _, dir := range []string{state, filepath.Join(snapshot, "agent-state")} {
					if err := os.WriteFile(filepath.Join(dir, "service-mutations.json"), []byte(`{"version":1,"jobs":{}}`), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			pending := "0"
			if test.pending {
				pending = "1"
			}
			harness := "set -euo pipefail\ndie() { printf '%s\\n' \"$*\" >&2; exit 1; }\n" + block
			command := exec.Command("/bin/bash", "-c", harness)
			command.Env = append(os.Environ(), "transition_state="+test.transition, "rollback_pending_resume="+pending,
				"PANEL_DB="+path, "PREFLIGHT_PANEL="+binary, "PREFLIGHT_AGENT=/usr/bin/true", "PREFLIGHT_SCHEMA17_BRIDGE=/usr/bin/false",
				"AGENT_STATE_DIR="+state, "AGENT_LEDGER="+filepath.Join(state, "service-mutations.json"), "snap="+snapshot,
				"MUTATION_LOCK="+filepath.Join(root, "unused-mutation.lock"), "MUTATION_LOCK_FD=9")
			output, err = command.CombinedOutput()
			if (err == nil) != test.success {
				t.Fatalf("rollback proof success=%v expected=%v: %v\n%s", err == nil, test.success, err, output)
			}
			if !reflect.DeepEqual(before, pendingRollbackSQLiteEvidence(t, path)) {
				t.Fatal("proof changed source DB/WAL/SHM bytes, identity or metadata")
			}
		})
	}
}

func TestPendingRollbackCrashWALWriter(t *testing.T) {
	path := os.Getenv("CP_PENDING_WAL_DB")
	if path == "" {
		t.Skip("only isolated SQLite writer child")
	}
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	if _, err := database.Exec(`PRAGMA journal_mode=WAL; PRAGMA wal_autocheckpoint=0; PRAGMA synchronous=FULL; PRAGMA user_version=7;`); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("CP_PENDING_WAL_WRITE") == "queued" {
		if _, err := database.Exec(`INSERT INTO service_operations(id,kind,service_id,status,phase,started_at,created_at,updated_at,request_id) VALUES('queued-crash','service_install','nginx','queued','queued','2026-09-14','2026-09-14','2026-09-14','queued-crash')`); err != nil {
			t.Fatal(err)
		}
	}
	if err := syscall.Kill(os.Getpid(), syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	select {}
}

func pendingRollbackSQLiteEvidence(t *testing.T, path string) map[string]string {
	t.Helper()
	result := map[string]string{}
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		info, err := os.Lstat(path + suffix)
		if os.IsNotExist(err) {
			result[suffix] = "absent"
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		stat := info.Sys().(*syscall.Stat_t)
		raw, err := os.ReadFile(path + suffix)
		if err != nil {
			t.Fatal(err)
		}
		var evidence bytes.Buffer
		fmt.Fprintf(&evidence, "%d:%d:%d:%d:%d:%d:%d:%v:%v|", stat.Dev, stat.Ino, stat.Mode, stat.Uid, stat.Gid, stat.Nlink, stat.Size, stat.Mtim, stat.Ctim)
		evidence.Write(raw)
		result[suffix] = evidence.String()
	}
	return result
}
