//go:build linux

package dnsenginerecovery

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"golang.org/x/sys/unix"
	_ "modernc.org/sqlite"
)

const pdnsLogicalLimit = 512 << 20

// CapturePDNSSourceDatabaseProof reads one logical SQLite transaction. It
// never checkpoints, restores, or writes the source database or its sidecars.
// The caller must bind path to the installed host policy and hold the DNS lock.
func CapturePDNSSourceDatabaseProof(ctx context.Context, path string) (dnsengineartifact.PDNSSourceDatabaseProofV1, error) {
	var empty dnsengineartifact.PDNSSourceDatabaseProofV1
	if ctx == nil || ctx.Err() != nil || !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" {
		return empty, errors.New("invalid PowerDNS source path")
	}
	fd, err := openPDNSDatabaseNoSymlinks(path)
	if err != nil {
		return empty, err
	}
	defer unix.Close(fd)
	var before unix.Stat_t
	if err := unix.Fstat(fd, &before); err != nil {
		return empty, err
	}
	if err := validSourceDBStat(before); err != nil {
		return empty, err
	}
	if err := validateSourceSidecars(path, before); err != nil {
		return empty, err
	}
	// SQLite can create WAL/SHM even with mode=ro. Query a disposable copy.
	directory, err := os.MkdirTemp("", "celikpanel-pdns-source-")
	if err != nil {
		return empty, err
	}
	defer os.RemoveAll(directory)
	copied := filepath.Join(directory, "pdns.sqlite3")
	if err := copyPDNSSourceFile(ctx, fd, copied, before); err != nil {
		return empty, err
	}
	walBefore, walExists, err := copyPDNSSourceSidecar(ctx, path+"-wal", copied+"-wal", before)
	if err != nil {
		return empty, err
	}
	if err := requireNoPDNSSourceRollbackJournal(path); err != nil {
		return empty, err
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(copied)+"?mode=ro&_query_only=1&_busy_timeout=1000")
	if err != nil {
		return empty, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	digest, err := logicalSQLiteDigest(ctx, tx)
	if err != nil {
		return empty, err
	}
	if err := tx.Commit(); err != nil {
		return empty, err
	}
	var after, named unix.Stat_t
	if err := unix.Fstat(fd, &after); err != nil {
		return empty, err
	}
	namedFD, err := openPDNSDatabaseNoSymlinks(path)
	if err != nil {
		return empty, err
	}
	statErr := unix.Fstat(namedFD, &named)
	closeErr := unix.Close(namedFD)
	if statErr != nil || closeErr != nil {
		return empty, errors.Join(statErr, closeErr)
	}
	if !sameSourceDBStat(before, after) || !sameSourceDBStat(after, named) {
		return empty, errors.New("PowerDNS source database identity changed during proof")
	}
	if err := reproveSourceSidecar(path+"-wal", walBefore, walExists); err != nil {
		return empty, err
	}
	if err := requireNoPDNSSourceRollbackJournal(path); err != nil {
		return empty, err
	}
	if err := validateSourceSidecars(path, after); err != nil {
		return empty, err
	}
	return dnsengineartifact.PDNSSourceDatabaseProofV1{Path: path, LogicalSHA256: digest, Mode: before.Mode & 0o7777, UID: before.Uid, GID: before.Gid, Device: uint64(before.Dev), Inode: before.Ino}, ctx.Err()
}

func VerifyPDNSSourceDatabaseProof(ctx context.Context, path string, proof dnsengineartifact.PDNSSourceDatabaseProofV1) error {
	if proof.Path != path || !dnsengineartifact.ValidGeneration(proof.LogicalSHA256) {
		return errors.New("PowerDNS source proof does not bind the installed path")
	}
	current, err := CapturePDNSSourceDatabaseProof(ctx, path)
	if err != nil {
		return err
	}
	if current != proof {
		return errors.New("PowerDNS source logical database or file identity differs from frozen proof")
	}
	return nil
}

func validSourceDBStat(s unix.Stat_t) error {
	if s.Mode&unix.S_IFMT != unix.S_IFREG || s.Nlink != 1 || s.Size <= 0 || s.Size > pdnsLogicalLimit ||
		(s.Mode&0o7777 != 0o640 && s.Mode&0o7777 != 0o600) || s.Uid > 1<<31-1 || s.Gid > 1<<31-1 {
		return errors.New("PowerDNS source database has unsafe type, size or owner")
	}
	return nil
}
func sameSourceDBStat(a, b unix.Stat_t) bool {
	return a.Dev == b.Dev && a.Ino == b.Ino && a.Mode == b.Mode && a.Uid == b.Uid && a.Gid == b.Gid && a.Nlink == b.Nlink && a.Size == b.Size && a.Mtim == b.Mtim && a.Ctim == b.Ctim
}
func validateSourceSidecars(path string, db unix.Stat_t) error {
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		name := path + suffix
		fd, err := openPDNSDatabaseNoSymlinks(name)
		if errors.Is(err, unix.ENOENT) {
			continue
		}
		if err != nil {
			return fmt.Errorf("open PowerDNS source sidecar: %w", err)
		}
		var stat unix.Stat_t
		statErr := unix.Fstat(fd, &stat)
		closeErr := unix.Close(fd)
		if statErr != nil || closeErr != nil {
			return errors.Join(statErr, closeErr)
		}
		if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Nlink != 1 || stat.Uid != db.Uid || stat.Gid != db.Gid || stat.Mode&0o0022 != 0 || stat.Size > pdnsLogicalLimit {
			return errors.New("PowerDNS source sidecar has unsafe metadata")
		}
	}
	return nil
}

func logicalSQLiteDigest(ctx context.Context, tx *sql.Tx) (string, error) {
	h := sha256.New()
	var consumed int64
	write := func(b []byte) error {
		consumed += int64(len(b)) + 8
		if consumed > pdnsLogicalLimit {
			return errors.New("PowerDNS logical database proof exceeds bound")
		}
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(len(b)))
		_, _ = h.Write(n[:])
		_, _ = h.Write(b)
		return nil
	}
	for _, pragma := range []string{"user_version", "application_id", "encoding"} {
		var value any
		if err := tx.QueryRowContext(ctx, "PRAGMA "+pragma).Scan(&value); err != nil {
			return "", err
		}
		if err := write([]byte(pragma)); err != nil {
			return "", err
		}
		if err := write([]byte(fmt.Sprint(value))); err != nil {
			return "", err
		}
	}
	var integrity string
	if err := tx.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil {
		return "", err
	}
	if integrity != "ok" {
		return "", errors.New("PowerDNS source SQLite integrity check failed")
	}
	schema, err := tx.QueryContext(ctx, "SELECT type,name,tbl_name,sql FROM sqlite_schema ORDER BY type,name,tbl_name,sql")
	if err != nil {
		return "", err
	}
	var tables []string
	for schema.Next() {
		var kind, name, table string
		var definition sql.NullString
		if err := schema.Scan(&kind, &name, &table, &definition); err != nil {
			schema.Close()
			return "", err
		}
		for _, value := range []string{kind, name, table, definition.String} {
			if err := write([]byte(value)); err != nil {
				schema.Close()
				return "", err
			}
		}
		if kind == "table" {
			tables = append(tables, name)
		}
	}
	err = schema.Err()
	schema.Close()
	if err != nil {
		return "", err
	}
	sort.Strings(tables)
	for _, table := range tables {
		if err := write([]byte(table)); err != nil {
			return "", err
		}
		quoted := `"` + strings.ReplaceAll(table, `"`, `""`) + `"`
		rows, err := tx.QueryContext(ctx, "SELECT * FROM "+quoted)
		if err != nil {
			return "", fmt.Errorf("read PowerDNS source table %s: %w", table, err)
		}
		cols, err := rows.Columns()
		if err != nil {
			rows.Close()
			return "", err
		}
		if len(cols) > 256 {
			rows.Close()
			return "", errors.New("PowerDNS source table has too many columns")
		}
		for _, col := range cols {
			if err := write([]byte(col)); err != nil {
				rows.Close()
				return "", err
			}
		}
		var encoded [][]byte
		var rowsBytes int64
		for rows.Next() {
			if ctx.Err() != nil {
				rows.Close()
				return "", ctx.Err()
			}
			values := make([]any, len(cols))
			dest := make([]any, len(cols))
			for i := range values {
				dest[i] = &values[i]
			}
			if err := rows.Scan(dest...); err != nil {
				rows.Close()
				return "", err
			}
			var record bytes.Buffer
			for _, value := range values {
				var tag byte
				var data []byte
				switch v := value.(type) {
				case nil:
					tag = 0
				case int64:
					tag = 1
					data = make([]byte, 8)
					binary.BigEndian.PutUint64(data, uint64(v))
				case float64:
					tag = 2
					data = make([]byte, 8)
					binary.BigEndian.PutUint64(data, math.Float64bits(v))
				case string:
					tag = 3
					data = []byte(v)
				case []byte:
					tag = 4
					data = v
				default:
					rows.Close()
					return "", errors.New("unsupported PowerDNS SQLite value type")
				}
				record.WriteByte(tag)
				var n [8]byte
				binary.BigEndian.PutUint64(n[:], uint64(len(data)))
				record.Write(n[:])
				record.Write(data)
			}
			rowsBytes += int64(record.Len())
			if consumed+rowsBytes > pdnsLogicalLimit {
				rows.Close()
				return "", errors.New("PowerDNS source table exceeds proof bound")
			}
			encoded = append(encoded, record.Bytes())
			if len(encoded) > 1000000 {
				rows.Close()
				return "", errors.New("PowerDNS source row count exceeds proof bound")
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return "", err
		}
		sort.Slice(encoded, func(i, j int) bool { return bytes.Compare(encoded[i], encoded[j]) < 0 })
		for _, record := range encoded {
			if err := write(record); err != nil {
				return "", err
			}
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func copyPDNSSourceFile(ctx context.Context, fd int, destination string, before unix.Stat_t) error {
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	buffer := make([]byte, 64<<10)
	for offset := int64(0); offset < before.Size; {
		if err := ctx.Err(); err != nil {
			return err
		}
		length := len(buffer)
		if before.Size-offset < int64(length) {
			length = int(before.Size - offset)
		}
		n, err := unix.Pread(fd, buffer[:length], offset)
		if err != nil {
			return err
		}
		if n == 0 {
			return errors.New("PowerDNS source copy ended early")
		}
		if _, err := out.Write(buffer[:n]); err != nil {
			return err
		}
		offset += int64(n)
	}
	var after unix.Stat_t
	if err := unix.Fstat(fd, &after); err != nil {
		return err
	}
	if !sameSourceDBStat(before, after) {
		return errors.New("PowerDNS source changed while copied")
	}
	return nil
}
func copyPDNSSourceSidecar(ctx context.Context, path, destination string, database unix.Stat_t) (unix.Stat_t, bool, error) {
	var zero unix.Stat_t
	fd, err := openPDNSDatabaseNoSymlinks(path)
	if errors.Is(err, unix.ENOENT) {
		return zero, false, nil
	}
	if err != nil {
		return zero, false, err
	}
	defer unix.Close(fd)
	var before unix.Stat_t
	if err := unix.Fstat(fd, &before); err != nil {
		return zero, false, err
	}
	if before.Mode&unix.S_IFMT != unix.S_IFREG || before.Nlink != 1 || before.Uid != database.Uid || before.Gid != database.Gid || before.Mode&0o022 != 0 || before.Size > pdnsLogicalLimit {
		return zero, false, errors.New("unsafe PowerDNS source WAL")
	}
	if err := copyPDNSSourceFile(ctx, fd, destination, before); err != nil {
		return zero, false, err
	}
	return before, true, nil
}

func reproveSourceSidecar(path string, before unix.Stat_t, exists bool) error {
	fd, err := openPDNSDatabaseNoSymlinks(path)
	if errors.Is(err, unix.ENOENT) && !exists {
		return nil
	}
	if err != nil {
		return errors.New("PowerDNS source WAL presence changed")
	}
	defer unix.Close(fd)
	if !exists {
		return errors.New("PowerDNS source WAL appeared during proof")
	}
	var after unix.Stat_t
	if err := unix.Fstat(fd, &after); err != nil {
		return err
	}
	if !sameSourceDBStat(before, after) {
		return errors.New("PowerDNS source WAL changed during proof")
	}
	return nil
}

func requireNoPDNSSourceRollbackJournal(path string) error {
	if _, err := os.Lstat(path + "-journal"); err == nil {
		return errors.New("PowerDNS source has an active rollback journal")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
