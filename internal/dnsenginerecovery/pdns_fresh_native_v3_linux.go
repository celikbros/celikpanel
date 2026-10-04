//go:build linux

package dnsenginerecovery

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/pdnsnative"
	"golang.org/x/sys/unix"
)

// CaptureFreshPrimaryNativeV3 observes a copy of the exact installed main
// inode and WAL. It never opens SQLite against the live database: even a
// read-only SQLite connection can rewrite SHM. A changing main/WAL is unknown.
// The caller holds host/DNS locks, excludes the accepted worker, and proves
// the native daemon identity and package version separately.
func CaptureFreshPrimaryNativeV3(ctx context.Context, policy dnsengineartifact.JournalPolicy, journal dnsengineartifact.SwitchJournalV1) (pdnsnative.Snapshot, error) {
	var empty pdnsnative.Snapshot
	if ctx == nil || ctx.Err() != nil || policy.PDNSDatabasePath != "/var/lib/powerdns/pdns.sqlite3" ||
		journal.Schema != dnsengineartifact.SwitchJournalSchemaV3 || journal.PDNSFreshPlan == nil ||
		journal.PDNSFreshPlan.Candidate == nil ||
		(journal.Phase != dnsengineartifact.SwitchPhaseTargetEnableIntent &&
			journal.Phase != dnsengineartifact.SwitchPhaseTargetStarted &&
			journal.Phase != dnsengineartifact.SwitchPhaseTargetVerified &&
			journal.Phase != dnsengineartifact.SwitchPhaseCommitted) {
		return empty, errors.New("v3 native observation requires a started or possibly-started fixed target")
	}
	if err := policy.ValidateSwitchJournal(journal); err != nil {
		return empty, err
	}
	proof := journal.PDNSFreshPlan.Candidate
	if _, err := os.Lstat(proof.Path); !errors.Is(err, os.ErrNotExist) {
		return empty, errors.New("v3 staged candidate reappeared during live observation")
	}
	path := policy.PDNSDatabasePath
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
	if uint64(before.Dev) != proof.Device || before.Ino != proof.Inode ||
		before.Uid != proof.UID || before.Gid != proof.GID {
		return empty, errors.New("v3 live PowerDNS main inode differs from sealed candidate")
	}
	if err := validateSourceSidecars(path, before); err != nil {
		return empty, err
	}
	if err := requireNoPDNSSourceRollbackJournal(path); err != nil {
		return empty, err
	}
	directory, err := os.MkdirTemp("", "celikpanel-pdns-fresh-v3-")
	if err != nil {
		return empty, err
	}
	defer os.RemoveAll(directory)
	copyPath := filepath.Join(directory, "pdns.sqlite3")
	if err := copyPDNSSourceFile(ctx, fd, copyPath, before); err != nil {
		return empty, err
	}
	walBefore, walExists, err := copyPDNSSourceSidecar(ctx, path+"-wal", copyPath+"-wal", before)
	if err != nil {
		return empty, err
	}
	shmBefore, shmExists, err := copyPDNSSourceSidecar(ctx, path+"-shm", copyPath+"-shm", before)
	if err != nil {
		return empty, err
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(copyPath)+"?mode=ro&_query_only=1&_busy_timeout=1000")
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
	snapshot, err := pdnsnative.CaptureSQLiteSnapshot(ctx, tx)
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
	if err := errors.Join(statErr, closeErr); err != nil {
		return empty, err
	}
	if !sameSourceDBStat(before, after) || !sameSourceDBStat(after, named) {
		return empty, errors.New("v3 live PowerDNS main file changed during observation")
	}
	if err := reproveSourceSidecar(path+"-wal", walBefore, walExists); err != nil {
		return empty, err
	}
	if err := reproveSourceSidecar(path+"-shm", shmBefore, shmExists); err != nil {
		return empty, err
	}
	if err := requireNoPDNSSourceRollbackJournal(path); err != nil {
		return empty, err
	}
	if err := validateSourceSidecars(path, after); err != nil {
		return empty, err
	}
	return snapshot, ctx.Err()
}

// VerifyRecordedFreshPrimaryNativeV3 refuses an owner edit even when its SQL
// resembles a valid native transform. A missing Native receipt remains
// unknown and must be preserved for explicit forward reconciliation.
func VerifyRecordedFreshPrimaryNativeV3(ctx context.Context, policy dnsengineartifact.JournalPolicy, journal dnsengineartifact.SwitchJournalV1) error {
	if journal.PDNSFreshPlan == nil || journal.PDNSFreshPlan.Native == nil {
		return errors.New("v3 native PowerDNS observation was not durably recorded")
	}
	live, err := CaptureFreshPrimaryNativeV3(ctx, policy, journal)
	if err != nil {
		return err
	}
	catalog, err := binddns.CatalogDomain(journal.LocalIP)
	if err != nil {
		return err
	}
	if err := pdnsnative.VerifyRecordedFreshPrimaryCatalogTransition(
		*journal.PDNSFreshPlan.Staged, live, catalog, journal.PrimaryCatalogSerial, *journal.PDNSFreshPlan.Native,
	); err != nil {
		return fmt.Errorf("v3 native PowerDNS catalog differs from recorded transition: %w", err)
	}
	return nil
}
