//go:build linux

package dnsenginerecovery

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestPDNSSourceLogicalProofSurvivesCheckpointAndFindsOwnerEdit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pdns.sqlite3")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, statement := range []string{
		"PRAGMA journal_mode=WAL",
		"CREATE TABLE domains(id INTEGER PRIMARY KEY, name TEXT, blob BLOB, ratio REAL)",
		"CREATE TABLE domainmetadata(id INTEGER, kind TEXT, content TEXT)",
		"INSERT INTO domains VALUES(1,'example.test',x'00ff',1.5)",
		"INSERT INTO domainmetadata VALUES(1,'NSEC3PARAM','1 0 0 -')",
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	walBefore, err := os.ReadFile(path + "-wal")
	if err != nil {
		t.Fatal(err)
	}
	shmBefore, err := os.ReadFile(path + "-shm")
	if err != nil {
		t.Fatal(err)
	}
	proof, err := CapturePDNSSourceDatabaseProof(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	walAfter, err := os.ReadFile(path + "-wal")
	if err != nil {
		t.Fatal(err)
	}
	shmAfter, err := os.ReadFile(path + "-shm")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(walBefore, walAfter) || !bytes.Equal(shmBefore, shmAfter) {
		t.Fatal("source proof changed live WAL or SHM bytes")
	}
	if err := VerifyPDNSSourceDatabaseProof(ctx, path, proof); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		t.Fatal(err)
	}
	if err := VerifyPDNSSourceDatabaseProof(ctx, path, proof); err != nil {
		t.Fatalf("unchanged logical database after checkpoint: %v", err)
	}
	if _, err := db.Exec("INSERT INTO domainmetadata VALUES(1,'ALSO-NOTIFY','192.0.2.1')"); err != nil {
		t.Fatal(err)
	}
	if err := VerifyPDNSSourceDatabaseProof(ctx, path, proof); err == nil {
		t.Fatal("owner metadata edit matched frozen proof")
	}
}

func TestPDNSSourceProofRejectsSymlinkAndIdentityReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pdns.sqlite3")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE records(id INTEGER, content TEXT)"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := CapturePDNSSourceDatabaseProof(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if _, err := os.Lstat(path + suffix); !os.IsNotExist(err) {
			t.Fatalf("source sidecar %s appeared during capture: %v", suffix, err)
		}
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) || before.Size() != after.Size() {
		t.Fatal("capture mutated source database file")
	}
	link := filepath.Join(filepath.Dir(path), "linked.sqlite3")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := CapturePDNSSourceDatabaseProof(context.Background(), link); err == nil {
		t.Fatal("symlinked source accepted")
	}
	old := path + ".old"
	if err := os.Rename(path, old); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(old)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := VerifyPDNSSourceDatabaseProof(context.Background(), path, proof); err == nil {
		t.Fatal("replacement inode matched frozen proof")
	}
}

func TestPDNSSourceProofIncludesSQLiteSequenceAndAuxiliaryTables(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pdns.sqlite3")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, query := range []string{"CREATE TABLE records(id INTEGER PRIMARY KEY AUTOINCREMENT, content TEXT)", "CREATE TABLE cryptokeys(id INTEGER, flags INTEGER)", "INSERT INTO records(content) VALUES('same')"} {
		if _, err := db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	proof, err := CapturePDNSSourceDatabaseProof(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO records(content) VALUES('temporary')"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DELETE FROM records WHERE id=2"); err != nil {
		t.Fatal(err)
	}
	if err := VerifyPDNSSourceDatabaseProof(context.Background(), path, proof); err == nil {
		t.Fatal("SQLite sequence change with same visible records passed")
	}
	proof, err = CapturePDNSSourceDatabaseProof(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO cryptokeys VALUES(1,257)"); err != nil {
		t.Fatal(err)
	}
	if err := VerifyPDNSSourceDatabaseProof(context.Background(), path, proof); err == nil {
		t.Fatal("auxiliary DNSSEC table edit passed")
	}
}

func TestPDNSSourceProofRefusesRollbackJournal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pdns.sqlite3")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE records(id INTEGER PRIMARY KEY, content TEXT)"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	journal := path + "-journal"
	if err := os.WriteFile(journal, []byte("uncommitted rollback journal"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CapturePDNSSourceDatabaseProof(context.Background(), path); err == nil {
		t.Fatal("source with rollback journal was accepted")
	}
}
