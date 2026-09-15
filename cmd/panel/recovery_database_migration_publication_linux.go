//go:build linux

package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

func (v *recoveryDatabaseMigration) workInventory() (map[string]databaseMigrationFile, error) {
	names, e := databaseMigrationNames(v.work)
	if e != nil {
		return nil, e
	}
	files := map[string]databaseMigrationFile{}
	for _, name := range names {
		switch name {
		case databaseMigrationDB, databaseMigrationDB + "-wal", databaseMigrationDB + "-shm", databaseMigrationDB + "-journal":
		default:
			return nil, fmt.Errorf("unknown migration work entry is preserved: %s", name)
		}
		file, e := databaseMigrationInspect(v.work, name, v.c.owner.uid, v.c.owner.gid)
		if e != nil {
			return nil, e
		}
		files[name] = file
	}
	if _, ok := files[databaseMigrationDB]; !ok {
		return nil, fmt.Errorf("migration work database is missing")
	}
	return files, nil
}
func (v *recoveryDatabaseMigration) verifySealedWork(seal databaseMigrationSeal) error {
	want := v.admission.Work
	want.UID = 0
	want.GID = 0
	if e := databaseMigrationSameDirectory(v.work, want); e != nil {
		return e
	}
	now, e := v.workInventory()
	if e != nil {
		return e
	}
	if !bytes.Equal(databaseMigrationCanonical(now), databaseMigrationCanonical(seal.Files)) {
		return fmt.Errorf("sealed migration work changed; original evidence is retained")
	}
	return v.revalidate()
}
func (v *recoveryDatabaseMigration) sealWork() (databaseMigrationSeal, []byte, error) {
	var seal databaseMigrationSeal
	var e error
	v.work, e = databaseMigrationOpenAt(v.transaction, "work", true)
	if e != nil {
		return seal, nil, e
	}
	raw, e := databaseMigrationRead(v.authority, "seal.json")
	if e == nil {
		if e = databaseMigrationDecode(raw, &seal); e != nil {
			return seal, nil, e
		}
		if seal.Schema != databaseMigrationSealSchema || seal.AdmissionSHA256 != databaseMigrationDigest(v.admissionRaw) {
			return seal, nil, fmt.Errorf("migration seal does not bind admission")
		}
	} else if errors.Is(e, unix.ENOENT) {
		if e = databaseMigrationSameDirectory(v.work, v.admission.Work); e != nil {
			return seal, nil, e
		}
		files, e := v.workInventory()
		if e != nil {
			return seal, nil, e
		}
		checks := []databaseMigrationFile{v.before}
		for _, f := range files {
			checks = append(checks, f)
		}
		if e = v.c.readers(checks); e != nil {
			return seal, nil, e
		}
		again, e := v.workInventory()
		if e != nil {
			return seal, nil, e
		}
		if !bytes.Equal(databaseMigrationCanonical(files), databaseMigrationCanonical(again)) {
			return seal, nil, fmt.Errorf("migration work changed before sealing")
		}
		if e = v.requireBefore(false); e != nil {
			return seal, nil, e
		}
		seal = databaseMigrationSeal{databaseMigrationSealSchema, databaseMigrationDigest(v.admissionRaw), files}
		if e = databaseMigrationRecord(v.authority, "seal.json", seal); e != nil {
			return seal, nil, e
		}
		raw = databaseMigrationCanonical(seal)
		if e = v.pinRecord("seal.json", raw); e != nil {
			return seal, nil, e
		}
		v.point("database_seal_intent_durable")
	} else {
		return seal, nil, e
	}
	// The durable seal precedes this explicitly bounded work-directory metadata
	// transition. The original DB and every sidecar remain byte-for-byte untouched.
	st, e := databaseMigrationStat(v.work)
	if e != nil {
		return seal, nil, e
	}
	actual := databaseMigrationDirID(st)
	frozen := v.admission.Work
	frozen.UID = 0
	frozen.GID = 0
	if actual != v.admission.Work && actual != frozen {
		return seal, nil, fmt.Errorf("migration work directory ownership changed unexpectedly")
	}
	checks := []databaseMigrationFile{v.before}
	for _, f := range seal.Files {
		checks = append(checks, f)
	}
	if e = v.c.readers(checks); e != nil {
		return seal, nil, e
	}
	if e = v.pinRecord("seal.json", raw); e != nil {
		return seal, nil, e
	}
	if e = databaseMigrationSameDirectory(v.work, actual); e != nil {
		return seal, nil, e
	}
	inventory, e := v.workInventory()
	if e != nil {
		return seal, nil, e
	}
	if !bytes.Equal(databaseMigrationCanonical(inventory), databaseMigrationCanonical(seal.Files)) {
		return seal, nil, fmt.Errorf("migration work changed before directory freeze")
	}
	if e = v.requireBefore(false); e != nil {
		return seal, nil, e
	}
	if actual == v.admission.Work {
		if e = v.work.Chown(0, 0); e != nil {
			return seal, nil, e
		}
		if e = v.work.Sync(); e != nil {
			return seal, nil, e
		}
		if e = v.transaction.Sync(); e != nil {
			return seal, nil, e
		}
	}
	v.point("database_work_sealed")
	if e = v.verifySealedWork(seal); e != nil {
		return seal, nil, e
	}
	return seal, raw, nil
}
func (v *recoveryDatabaseMigration) buildPublication(seal databaseMigrationSeal, sealRaw []byte) (databaseMigrationPublication, []byte, error) {
	var record databaseMigrationPublication
	if _, ok := seal.Files[databaseMigrationDB+"-journal"]; ok {
		return record, nil, fmt.Errorf("migration work contains a rollback journal; retain it and recover the admitted operation")
	}
	name, e := databaseMigrationRandom(".build-")
	if e != nil {
		return record, nil, e
	}
	build, e := databaseMigrationMkdir(v.authority, name, 0, 0, 0700)
	if e != nil {
		return record, nil, e
	}
	defer build.Close()
	source, e := databaseMigrationOpenAt(v.work, databaseMigrationDB, false)
	if e != nil {
		return record, nil, e
	}
	defer source.Close()
	if e = databaseMigrationCopy(build, databaseMigrationDB, source, seal.Files[databaseMigrationDB].Identity.Size, 0, 0); e != nil {
		return record, nil, e
	}
	if _, ok := seal.Files[databaseMigrationDB+"-wal"]; ok {
		wal, e := databaseMigrationOpenAt(v.work, databaseMigrationDB+"-wal", false)
		if e != nil {
			return record, nil, e
		}
		defer wal.Close()
		info, e := wal.Stat()
		if e != nil {
			return record, nil, e
		}
		if info.Size() > 0 {
			size, e := recoverPinnedLiveSQLiteWAL(&pinnedSQLiteSidecar{file: wal, info: info})
			if e != nil {
				return record, nil, e
			}
			if size > 0 {
				if e = databaseMigrationCopy(build, databaseMigrationDB+"-wal", wal, size, 0, 0); e != nil {
					return record, nil, e
				}
			}
		}
	}
	v.point("database_build_copied")
	// SQLite may checkpoint/delete sidecars only in this disposable private copy.
	// The sealed work directory and its original WAL/SHM are never opened by SQLite.
	path := filepath.Join(build.Name(), databaseMigrationDB)
	if e = normalizeStandaloneSQLiteSnapshot(path); e != nil {
		return record, nil, e
	}
	if e = databaseMigrationNoSidecars(build, databaseMigrationDB); e != nil {
		return record, nil, e
	}
	if e = checkCompletedUpdateDatabase(path); e != nil {
		return record, nil, e
	}
	file, e := databaseMigrationOpenAt(build, databaseMigrationDB, false)
	if e != nil {
		return record, nil, e
	}
	if e = file.Chown(int(v.c.owner.uid), int(v.c.owner.gid)); e == nil {
		e = file.Chmod(0600)
	}
	if e == nil {
		e = file.Sync()
	}
	file.Close()
	if e != nil {
		return record, nil, e
	}
	if e = build.Sync(); e != nil {
		return record, nil, e
	}
	after, e := databaseMigrationInspect(build, databaseMigrationDB, v.c.owner.uid, v.c.owner.gid)
	if e != nil {
		return record, nil, e
	}
	directory, e := databaseMigrationDirectoryProof(build, 0, 0, 0700)
	if e != nil {
		return record, nil, e
	}
	if directory.Dev != v.parentID.Dev {
		return record, nil, fmt.Errorf("database publication requires one filesystem")
	}
	if e = v.verifySealedWork(seal); e != nil {
		return record, nil, e
	}
	if e = v.requireBefore(false); e != nil {
		return record, nil, e
	}
	record = databaseMigrationPublication{databaseMigrationPublicationSchema, databaseMigrationDigest(v.admissionRaw), databaseMigrationDigest(sealRaw), name, directory, v.before, after}
	if e = databaseMigrationRecord(v.authority, "publication.json", record); e != nil {
		return record, nil, e
	}
	v.point("database_publication_intent_durable")
	return record, databaseMigrationCanonical(record), nil
}
func (v *recoveryDatabaseMigration) readPublication() (databaseMigrationPublication, []byte, error) {
	var r databaseMigrationPublication
	raw, e := databaseMigrationRead(v.authority, "publication.json")
	if errors.Is(e, unix.ENOENT) {
		return r, nil, errDatabaseMigrationAbsent
	}
	if e != nil {
		return r, nil, e
	}
	if e = databaseMigrationDecode(raw, &r); e != nil {
		return r, nil, e
	}
	if r.Schema != databaseMigrationPublicationSchema || r.AdmissionSHA256 != databaseMigrationDigest(v.admissionRaw) || r.Before != v.before || !databaseMigrationBuildPattern.MatchString(r.Build) || r.Directory.Mode != unix.S_IFDIR|0700 || r.Directory.UID != 0 || r.Directory.GID != 0 || r.Directory.Dev != v.parentID.Dev || r.After.Identity.Mode != unix.S_IFREG|0600 || r.After.Identity.UID != v.c.owner.uid || r.After.Identity.GID != v.c.owner.gid || r.After.Identity.Links != 1 || r.After.Identity.Dev != v.parentID.Dev || !databaseMigrationTokenPattern.MatchString(r.After.SHA256) {
		return r, nil, fmt.Errorf("database publication does not bind the admitted before-image")
	}
	sealRaw, e := databaseMigrationRead(v.authority, "seal.json")
	if e != nil {
		return r, nil, e
	}
	if databaseMigrationDigest(sealRaw) != r.SealSHA256 {
		return r, nil, fmt.Errorf("database publication seal changed")
	}
	var seal databaseMigrationSeal
	if e = databaseMigrationDecode(sealRaw, &seal); e != nil {
		return r, nil, e
	}
	if seal.Schema != databaseMigrationSealSchema || seal.AdmissionSHA256 != r.AdmissionSHA256 {
		return r, nil, fmt.Errorf("invalid database migration seal")
	}
	if e = v.pinRecord("publication.json", raw); e != nil {
		return r, nil, e
	}
	if e = v.pinRecord("seal.json", sealRaw); e != nil {
		return r, nil, e
	}
	return r, raw, v.revalidate()
}
func (v *recoveryDatabaseMigration) openBuild(r databaseMigrationPublication) (*os.File, error) {
	b, e := databaseMigrationOpenAt(v.authority, r.Build, true)
	if e != nil {
		return nil, e
	}
	fail := func(e error) (*os.File, error) { b.Close(); return nil, e }
	if e = databaseMigrationSameDirectory(b, r.Directory); e != nil {
		return fail(e)
	}
	names, e := databaseMigrationNames(b)
	if e != nil {
		return fail(e)
	}
	if len(names) != 1 || names[0] != databaseMigrationDB {
		return fail(fmt.Errorf("database publication build has unexpected entries"))
	}
	return b, nil
}
func (v *recoveryDatabaseMigration) readReceipt(name string, raw []byte) (bool, error) {
	actual, e := databaseMigrationRead(v.authority, name)
	if errors.Is(e, unix.ENOENT) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	expected := databaseMigrationCanonical(databaseMigrationReceipt{"celikpanel/database-publication-receipt/v1", databaseMigrationDigest(raw)})
	if !bytes.Equal(actual, expected) {
		return false, fmt.Errorf("database publication receipt changed: %s", name)
	}
	if e = v.pinRecord(name, actual); e != nil {
		return false, e
	}
	return true, nil
}
func (v *recoveryDatabaseMigration) receipt(name string, raw []byte) error {
	receipt := databaseMigrationReceipt{"celikpanel/database-publication-receipt/v1", databaseMigrationDigest(raw)}
	if e := databaseMigrationRecord(v.authority, name, receipt); e != nil {
		return e
	}
	if e := v.pinRecord(name, databaseMigrationCanonical(receipt)); e != nil {
		return e
	}
	return v.revalidate()
}
func (v *recoveryDatabaseMigration) pair(build *os.File, r databaseMigrationPublication) (bool, bool, error) {
	if e := databaseMigrationSameDirectory(build, r.Directory); e != nil {
		return false, false, e
	}
	a, e := v.current()
	if e != nil {
		return false, false, e
	}
	b, e := databaseMigrationInspect(build, databaseMigrationDB, v.c.owner.uid, v.c.owner.gid)
	if e != nil {
		return false, false, e
	}
	if e = databaseMigrationNoSidecars(v.parent, databaseMigrationDB); e != nil {
		return false, false, e
	}
	if e = v.revalidate(); e != nil {
		return false, false, e
	}
	return databaseMigrationSameFile(a, r.Before, true) && databaseMigrationSameFile(b, r.After, true), databaseMigrationSameFile(a, r.After, true) && databaseMigrationSameFile(b, r.Before, true), nil
}
func (v *recoveryDatabaseMigration) exchange(build *os.File, r databaseMigrationPublication, forward bool) error {
	old, new, e := v.pair(build, r)
	if e != nil {
		return e
	}
	if forward && !old || !forward && !new {
		return fmt.Errorf("database exchange pair changed")
	}
	// Retain exact descriptor metadata over the last fault seam; only our rename
	// may change ctime. Neither same-content replacement nor owner writes qualify.
	canonical, e := v.current()
	if e != nil {
		return e
	}
	staged, e := databaseMigrationInspect(build, databaseMigrationDB, v.c.owner.uid, v.c.owner.gid)
	if e != nil {
		return e
	}
	if e = v.c.readers([]databaseMigrationFile{canonical, staged}); e != nil {
		return e
	}
	prefix := "database_publish"
	if !forward {
		prefix = "database_restore"
	}
	v.point(prefix + "_before_exchange")
	if e = v.revalidate(); e != nil {
		return e
	}
	if e = databaseMigrationSameDirectory(build, r.Directory); e != nil {
		return e
	}
	a, e := v.current()
	if e != nil {
		return e
	}
	b, e := databaseMigrationInspect(build, databaseMigrationDB, v.c.owner.uid, v.c.owner.gid)
	if e != nil {
		return e
	}
	if a != canonical || b != staged {
		return fmt.Errorf("database owner state changed before exchange")
	}
	if e = databaseMigrationNoSidecars(v.parent, databaseMigrationDB); e != nil {
		return e
	}
	if e = unix.Renameat2(int(v.parent.Fd()), databaseMigrationDB, int(build.Fd()), databaseMigrationDB, unix.RENAME_EXCHANGE); e != nil {
		return e
	}
	v.point(prefix + "_exchange_done")
	if e = v.parent.Sync(); e != nil {
		return e
	}
	if e = build.Sync(); e != nil {
		return e
	}
	v.point(prefix + "_exchange_durable")
	return nil
}
func publishRecoveryDatabaseMigrationWith(snapshot string, c recoveryDatabaseMigrationConfig) error {
	v, e := openRecoveryDatabaseMigration(snapshot, c)
	if e != nil {
		return e
	}
	defer v.close()
	if e = v.requireActive("update"); e != nil {
		return e
	}
	if e = v.openWorkspace(false); e != nil {
		return e
	}
	if e = v.readAdmission(); e != nil {
		return e
	}
	if e = databaseMigrationAbsent(v.authority, "restoration.json"); e != nil {
		return e
	}
	if e = databaseMigrationAbsent(v.authority, "restored.json"); e != nil {
		return e
	}
	r, raw, e := v.readPublication()
	if errors.Is(e, errDatabaseMigrationAbsent) {
		if e = databaseMigrationAbsent(v.authority, "published.json"); e != nil {
			return e
		}
		if e = v.requireBefore(false); e != nil {
			return e
		}
		seal, sealRaw, e := v.sealWork()
		if e != nil {
			return e
		}
		r, raw, e = v.buildPublication(seal, sealRaw)
		if e != nil {
			return e
		}
	} else if e != nil {
		return e
	}
	r, raw, e = v.readPublication()
	if e != nil {
		return e
	}
	build, e := v.openBuild(r)
	if e != nil {
		return e
	}
	defer build.Close()
	receipt, e := v.readReceipt("published.json", raw)
	if e != nil {
		return e
	}
	old, new, e := v.pair(build, r)
	if e != nil {
		return e
	}
	if old {
		if receipt {
			return fmt.Errorf("published database receipt conflicts with before pair")
		}
		if e = v.exchange(build, r, true); e != nil {
			return e
		}
	} else if !new {
		return fmt.Errorf("database publication pair is unrecognized")
	}
	_, new, e = v.pair(build, r)
	if e != nil {
		return e
	}
	if !new {
		return fmt.Errorf("database publication result changed")
	}
	if e = v.receipt("published.json", raw); e != nil {
		return e
	}
	v.point("database_published_durable")
	_, new, e = v.pair(build, r)
	if e != nil {
		return e
	}
	if !new {
		return fmt.Errorf("database publication changed after receipt")
	}
	return nil
}
func restoreRecoveryDatabaseMigrationWith(snapshot string, c recoveryDatabaseMigrationConfig) error {
	v, e := openRecoveryDatabaseMigration(snapshot, c)
	if e != nil {
		return e
	}
	defer v.close()
	if e = v.requireActive("rollback"); e != nil {
		return e
	}
	e = v.openWorkspace(false)
	if errors.Is(e, errDatabaseMigrationAbsent) {
		return v.verifyBeforeOnly(false)
	}
	if e != nil {
		return e
	}
	e = v.readAdmission()
	if errors.Is(e, errDatabaseMigrationAbsent) {
		if e = v.journalGrammar(); e != nil {
			return e
		}
		for _, name := range []string{"seal.json", "publication.json", "published.json", "restoration.json", "restored.json"} {
			if e = databaseMigrationAbsent(v.authority, name); e != nil {
				return e
			}
		}
		return v.verifyBeforeOnly(false)
	}
	if e != nil {
		return e
	}
	r, raw, e := v.readPublication()
	if errors.Is(e, errDatabaseMigrationAbsent) {
		for _, name := range []string{"published.json", "restoration.json", "restored.json"} {
			if e = databaseMigrationAbsent(v.authority, name); e != nil {
				return e
			}
		}
		// Incomplete working copies and their sidecars have no authority over the
		// unchanged canonical DB. Preserve them without reopening them through SQLite.
		return v.verifyBeforeOnly(false)
	}
	if e != nil {
		return e
	}
	build, e := v.openBuild(r)
	if e != nil {
		return e
	}
	defer build.Close()
	published, e := v.readReceipt("published.json", raw)
	if e != nil {
		return e
	}
	old, new, e := v.pair(build, r)
	if e != nil {
		return e
	}
	if !old && !new {
		return fmt.Errorf("database rollback pair changed")
	}
	var restore databaseMigrationRestoration
	restoreRaw, e := databaseMigrationRead(v.authority, "restoration.json")
	if errors.Is(e, unix.ENOENT) {
		if e = databaseMigrationAbsent(v.authority, "restored.json"); e != nil {
			return e
		}
		if old && published {
			return fmt.Errorf("database was reverted without a restoration intent")
		}
		restore = databaseMigrationRestoration{databaseMigrationRestoreSchema, databaseMigrationDigest(raw), new}
		if e = databaseMigrationRecord(v.authority, "restoration.json", restore); e != nil {
			return e
		}
		restoreRaw = databaseMigrationCanonical(restore)
		v.point("database_restore_intent_durable")
	} else if e != nil {
		return e
	} else {
		if e = databaseMigrationDecode(restoreRaw, &restore); e != nil {
			return e
		}
		if restore.Schema != databaseMigrationRestoreSchema || restore.PublicationSHA256 != databaseMigrationDigest(raw) {
			return fmt.Errorf("database restoration intent changed")
		}
	}
	if e = v.pinRecord("restoration.json", restoreRaw); e != nil {
		return e
	}
	restored, e := v.readReceipt("restored.json", restoreRaw)
	if e != nil {
		return e
	}
	if new {
		if restored || !restore.Exchange {
			return fmt.Errorf("database restoration receipt conflicts with current pair")
		}
		if e = v.exchange(build, r, false); e != nil {
			return e
		}
	}
	old, _, e = v.pair(build, r)
	if e != nil {
		return e
	}
	if !old {
		return fmt.Errorf("database restoration did not recover before pair")
	}
	if e = v.receipt("restored.json", restoreRaw); e != nil {
		return e
	}
	v.point("database_restored_durable")
	old, _, e = v.pair(build, r)
	if e != nil {
		return e
	}
	if !old {
		return fmt.Errorf("restored database changed after receipt")
	}
	return nil
}
func databaseMigrationHistory(ctx context.Context, db *sql.DB) ([]byte, error) {
	rows, e := db.QueryContext(ctx, "SELECT version,filename,sha256,applied_at FROM schema_migrations ORDER BY version")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	type entry struct {
		Version                     int
		Filename, SHA256, AppliedAt sql.NullString
	}
	var entries []entry
	for rows.Next() {
		var r entry
		if e = rows.Scan(&r.Version, &r.Filename, &r.SHA256, &r.AppliedAt); e != nil {
			return nil, e
		}
		entries = append(entries, r)
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	return databaseMigrationCanonical(entries), nil
}
func (v *recoveryDatabaseMigration) historicalSchema() error {
	source, e := v.a.SnapshotDatabase()
	if e != nil {
		return e
	}
	defer source.Close()
	db, e := sql.Open("sqlite", sqliteSnapshotURI(fmt.Sprintf("/proc/self/fd/%d", source.Fd()), true))
	if e != nil {
		return e
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var version int
	if e = db.QueryRowContext(ctx, "SELECT MAX(version) FROM schema_migrations").Scan(&version); e != nil {
		return e
	}
	expectedHistory, e := databaseMigrationHistory(ctx, db)
	if e != nil {
		return e
	}
	return checkWALAwareServiceOperationsIdleWith(filepath.Join(v.c.parent, databaseMigrationDB), func(path string) error {
		if e := validateServiceOperationSnapshot(path, serviceOperationSnapshotSchemaNormal); e != nil {
			return e
		}
		if e := validateServiceOperationSnapshotSchema(path, version, true); e != nil {
			return e
		}
		current, e := sql.Open("sqlite", sqliteSnapshotURI(path, true))
		if e != nil {
			return e
		}
		defer current.Close()
		history, e := databaseMigrationHistory(ctx, current)
		if e != nil {
			return e
		}
		if !bytes.Equal(history, expectedHistory) {
			return fmt.Errorf("restored database migration history differs from snapshot")
		}
		return nil
	})
}
func verifyRecoveryDatabaseMigrationWith(snapshot string, c recoveryDatabaseMigrationConfig) error {
	v, e := openRecoveryDatabaseMigration(snapshot, c)
	if e != nil {
		return e
	}
	defer v.close()
	if v.id.Operation != "update" && v.id.Operation != "rollback" {
		return fmt.Errorf("database verification requires an existing release operation")
	}
	late := v.id.Phase == "completion" || v.id.Phase == "completion-scheduler" || v.id.Phase == "scheduler"
	if !late && v.id.Phase != "active" {
		return fmt.Errorf("database verification phase is unsupported")
	}
	if !late {
		if e = c.stopped(); e != nil {
			return e
		}
		if e = c.readers([]databaseMigrationFile{v.before}); e != nil {
			return e
		}
	}
	e = v.openWorkspace(false)
	if errors.Is(e, errDatabaseMigrationAbsent) && v.id.Operation == "rollback" {
		return v.verifyBeforeOnly(late)
	}
	if e != nil {
		return e
	}
	e = v.readAdmission()
	if errors.Is(e, errDatabaseMigrationAbsent) && v.id.Operation == "rollback" {
		if e = v.journalGrammar(); e != nil {
			return e
		}
		for _, name := range []string{"seal.json", "publication.json", "published.json", "restoration.json", "restored.json"} {
			if e = databaseMigrationAbsent(v.authority, name); e != nil {
				return e
			}
		}
		return v.verifyBeforeOnly(late)
	}
	if e != nil {
		return e
	}
	r, raw, e := v.readPublication()
	if errors.Is(e, errDatabaseMigrationAbsent) && v.id.Operation == "rollback" {
		for _, name := range []string{"published.json", "restoration.json", "restored.json"} {
			if e = databaseMigrationAbsent(v.authority, name); e != nil {
				return e
			}
		}
		return v.verifyBeforeOnly(late)
	}
	if e != nil {
		return e
	}
	build, e := v.openBuild(r)
	if e != nil {
		return e
	}
	defer build.Close()
	want, retired := r.After, r.Before
	if v.id.Operation == "rollback" {
		rr, e := databaseMigrationRead(v.authority, "restoration.json")
		if e != nil {
			return e
		}
		var restore databaseMigrationRestoration
		if e = databaseMigrationDecode(rr, &restore); e != nil {
			return e
		}
		if restore.Schema != databaseMigrationRestoreSchema || restore.PublicationSHA256 != databaseMigrationDigest(raw) {
			return fmt.Errorf("invalid database restoration provenance")
		}
		if e = v.pinRecord("restoration.json", rr); e != nil {
			return e
		}
		ok, e := v.readReceipt("restored.json", rr)
		if e != nil {
			return e
		}
		if !ok {
			return fmt.Errorf("restored database receipt is missing")
		}
		want, retired = r.Before, r.After
	} else {
		ok, e := v.readReceipt("published.json", raw)
		if e != nil {
			return e
		}
		if !ok {
			return fmt.Errorf("published database receipt is missing")
		}
		if e = databaseMigrationAbsent(v.authority, "restoration.json"); e != nil {
			return e
		}
		if e = databaseMigrationAbsent(v.authority, "restored.json"); e != nil {
			return e
		}
	}
	preserved, e := databaseMigrationInspect(build, databaseMigrationDB, c.owner.uid, c.owner.gid)
	if e != nil {
		return e
	}
	if !databaseMigrationSameFile(preserved, retired, true) {
		return fmt.Errorf("retired database before/after evidence changed")
	}
	var current databaseMigrationFile
	if late {
		current, e = databaseMigrationInspectLive(v.parent, databaseMigrationDB, c.owner.uid, c.owner.gid)
	} else {
		current, e = v.current()
	}
	if e != nil {
		return e
	}
	if late {
		if !databaseMigrationStableFile(current, want) {
			return fmt.Errorf("running database identity or owner changed")
		}
		if v.id.Operation == "update" {
			e = checkCompletedUpdateDatabaseWALAware(filepath.Join(c.parent, databaseMigrationDB))
		} else {
			e = v.historicalSchema()
		}
		if e != nil {
			return e
		}
	} else {
		if !databaseMigrationSameFile(current, want, true) {
			return fmt.Errorf("stopped database content changed")
		}
		if e = databaseMigrationNoSidecars(v.parent, databaseMigrationDB); e != nil {
			return e
		}
	}
	v.point("database_verify_before_final_proof")
	if e = databaseMigrationSameDirectory(build, r.Directory); e != nil {
		return e
	}
	again, e := databaseMigrationInspect(build, databaseMigrationDB, c.owner.uid, c.owner.gid)
	if e != nil {
		return e
	}
	if !databaseMigrationSameFile(again, retired, true) {
		return fmt.Errorf("retired database changed during verification")
	}
	if late {
		again, e = databaseMigrationInspectLive(v.parent, databaseMigrationDB, c.owner.uid, c.owner.gid)
		if e != nil {
			return e
		}
		if !databaseMigrationStableFile(again, want) {
			return fmt.Errorf("running database changed during verification")
		}
	} else {
		again, e = v.current()
		if e != nil {
			return e
		}
		if !databaseMigrationSameFile(again, want, true) {
			return fmt.Errorf("stopped database changed during verification")
		}
		if e = databaseMigrationNoSidecars(v.parent, databaseMigrationDB); e != nil {
			return e
		}
	}
	if v.id.Operation == "update" {
		for _, name := range []string{"restoration.json", "restored.json"} {
			if e = databaseMigrationAbsent(v.authority, name); e != nil {
				return e
			}
		}
	}
	return v.revalidate()
}

// The pre-publication path has no exchange receipt. Its proof therefore also
// binds the exact missing component, including a never-completed preparation.
func (v *recoveryDatabaseMigration) beforeOnlyKind() string {
	if v.root == nil {
		return "root"
	}
	if v.transaction == nil {
		return "token"
	}
	if v.authority == nil {
		return "authority"
	}
	if v.admissionRaw == nil {
		return "admission"
	}
	return "publication"
}
func (v *recoveryDatabaseMigration) verifyBeforeOnlyAbsence() error {
	want := v.beforeOnlyKind()
	probe, e := openRecoveryDatabaseMigration(v.id.Snapshot, v.c)
	if e != nil {
		return e
	}
	defer probe.close()
	e = probe.openWorkspace(false)
	if errors.Is(e, errDatabaseMigrationAbsent) {
		if probe.beforeOnlyKind() != want {
			return fmt.Errorf("database preparation absence changed")
		}
		return v.revalidate()
	}
	if e != nil {
		return e
	}
	e = probe.readAdmission()
	if errors.Is(e, errDatabaseMigrationAbsent) {
		if want != "admission" {
			return fmt.Errorf("database admission absence changed")
		}
		if e = probe.journalGrammar(); e != nil {
			return e
		}
		for _, name := range []string{"seal.json", "publication.json", "published.json", "restoration.json", "restored.json"} {
			if e = databaseMigrationAbsent(probe.authority, name); e != nil {
				return e
			}
		}
		return v.revalidate()
	}
	if e != nil {
		return e
	}
	if want != "publication" {
		return fmt.Errorf("database publication absence changed")
	}
	_, _, e = probe.readPublication()
	if !errors.Is(e, errDatabaseMigrationAbsent) {
		if e != nil {
			return e
		}
		return fmt.Errorf("database publication appeared during verification")
	}
	for _, name := range []string{"published.json", "restoration.json", "restored.json"} {
		if e = databaseMigrationAbsent(probe.authority, name); e != nil {
			return e
		}
	}
	if e = probe.revalidate(); e != nil {
		return e
	}
	return v.revalidate()
}
func (v *recoveryDatabaseMigration) verifyBeforeOnly(late bool) error {
	if !late {
		if e := v.requireBefore(false); e != nil {
			return e
		}
		v.point("database_verify_before_final_proof")
		if e := v.requireBefore(false); e != nil {
			return e
		}
		return v.verifyBeforeOnlyAbsence()
	}
	now, e := databaseMigrationInspectLive(v.parent, databaseMigrationDB, v.c.owner.uid, v.c.owner.gid)
	if e != nil {
		return e
	}
	if !databaseMigrationStableFile(now, v.before) {
		return fmt.Errorf("restored unchanged database identity changed")
	}
	if e = v.historicalSchema(); e != nil {
		return e
	}
	v.point("database_verify_before_final_proof")
	again, e := databaseMigrationInspectLive(v.parent, databaseMigrationDB, v.c.owner.uid, v.c.owner.gid)
	if e != nil {
		return e
	}
	if !databaseMigrationStableFile(again, v.before) {
		return fmt.Errorf("unchanged database identity changed during verification")
	}
	return v.verifyBeforeOnlyAbsence()
}
