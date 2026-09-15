//go:build linux

package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/alicelik/celikpanel/internal/recoverypublication"
	"golang.org/x/sys/unix"
)

const databaseMigrationAdmissionSchema = "celikpanel/database-migration-admission/v1"
const databaseMigrationSealSchema = "celikpanel/database-migration-seal/v1"
const databaseMigrationPublicationSchema = "celikpanel/database-publication-intent/v1"
const databaseMigrationRestoreSchema = "celikpanel/database-restoration-intent/v1"
const databaseMigrationRootName = ".release-db-migrations"
const databaseMigrationDB = "celikpanel.db"

var errDatabaseMigrationAbsent = errors.New("database migration evidence is absent")

var databaseMigrationTokenPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var databaseMigrationBuildPattern = regexp.MustCompile(`^\.build-[0-9a-f]{32}$`)
var databaseMigrationRecordPattern = regexp.MustCompile(`^\.record-[0-9a-f]{32}$`)

type recoveryDatabaseAuthority interface {
	Identity() recoverypublication.DatabaseAuthorityIdentity
	SnapshotDatabase() (*os.File, error)
	Revalidate() error
	Close() error
}
type recoveryDatabaseMigrationConfig struct {
	parent     string
	owner      serviceOperationRestoreOwner
	authority  func(string) (recoveryDatabaseAuthority, error)
	stopped    func() error
	readers    func([]databaseMigrationFile) error
	checkpoint func(string)
}
type databaseMigrationAdmission struct {
	Schema                 string                     `json:"schema"`
	Snapshot               string                     `json:"snapshot"`
	TokenSHA256            string                     `json:"transaction_token_sha256"`
	MaterialSHA256         string                     `json:"material_sha256"`
	SnapshotManifestSHA256 string                     `json:"snapshot_manifest_sha256"`
	CandidatePanelSHA256   string                     `json:"candidate_panel_sha256"`
	Parent                 databaseMigrationDirectory `json:"parent"`
	Before                 databaseMigrationFile      `json:"before"`
	Root                   databaseMigrationDirectory `json:"root"`
	Transaction            databaseMigrationDirectory `json:"transaction"`
	Authority              databaseMigrationDirectory `json:"authority"`
	Work                   databaseMigrationDirectory `json:"work"`
	Initial                databaseMigrationFile      `json:"initial"`
}
type databaseMigrationSeal struct {
	Schema          string                           `json:"schema"`
	AdmissionSHA256 string                           `json:"admission_sha256"`
	Files           map[string]databaseMigrationFile `json:"files"`
}
type databaseMigrationPublication struct {
	Schema          string                     `json:"schema"`
	AdmissionSHA256 string                     `json:"admission_sha256"`
	SealSHA256      string                     `json:"seal_sha256"`
	Build           string                     `json:"build"`
	Directory       databaseMigrationDirectory `json:"directory"`
	Before          databaseMigrationFile      `json:"before"`
	After           databaseMigrationFile      `json:"after"`
}
type databaseMigrationRestoration struct {
	Schema            string `json:"schema"`
	PublicationSHA256 string `json:"publication_sha256"`
	Exchange          bool   `json:"exchange"`
}
type databaseMigrationReceipt struct {
	Schema       string `json:"schema"`
	IntentSHA256 string `json:"intent_sha256"`
}
type recoveryDatabaseMigration struct {
	c                                          recoveryDatabaseMigrationConfig
	a                                          recoveryDatabaseAuthority
	id                                         recoverypublication.DatabaseAuthorityIdentity
	before                                     databaseMigrationFile
	parentID                                   databaseMigrationDirectory
	parent, root, transaction, authority, work *os.File
	admission                                  databaseMigrationAdmission
	admissionRaw                               []byte
	records                                    map[string][]byte
	recordIDs                                  map[string]databaseMigrationIdentity
}

func recoveryDatabaseMigrationProduction() (recoveryDatabaseMigrationConfig, error) {
	owner, e := lookupServiceOperationRestoreOwner()
	if e != nil {
		return recoveryDatabaseMigrationConfig{}, e
	}
	c := recoveryDatabaseMigrationConfig{parent: "/var/lib/celikpanel", owner: owner, stopped: verifyCelikPanelServicesStopped}
	c.authority = func(snapshot string) (recoveryDatabaseAuthority, error) {
		return recoverypublication.OpenDatabaseAuthority(snapshot)
	}
	c.readers = func(files []databaseMigrationFile) error {
		for _, f := range files {
			st := unix.Stat_t{Dev: f.Identity.Dev, Ino: f.Identity.Ino}
			if e := rejectDatabaseQuarantineUsersAndHandles("/proc", owner.uid, st); e != nil {
				return e
			}
		}
		if len(files) == 0 {
			return rejectDatabaseQuarantineUsersAndHandles("/proc", owner.uid, unix.Stat_t{})
		}
		return nil
	}
	return c, nil
}
func prepareRecoveryDatabaseMigration(snapshot string) (string, error) {
	c, e := recoveryDatabaseMigrationProduction()
	if e != nil {
		return "", e
	}
	return prepareRecoveryDatabaseMigrationWith(snapshot, c)
}
func publishRecoveryDatabaseMigration(snapshot string) error {
	c, e := recoveryDatabaseMigrationProduction()
	if e != nil {
		return e
	}
	return publishRecoveryDatabaseMigrationWith(snapshot, c)
}
func restoreRecoveryDatabaseMigration(snapshot string) error {
	c, e := recoveryDatabaseMigrationProduction()
	if e != nil {
		return e
	}
	return restoreRecoveryDatabaseMigrationWith(snapshot, c)
}
func verifyRecoveryDatabaseMigration(snapshot string) error {
	c, e := recoveryDatabaseMigrationProduction()
	if e != nil {
		return e
	}
	return verifyRecoveryDatabaseMigrationWith(snapshot, c)
}
func databaseMigrationEvidenceIdentity(i recoverypublication.DatabaseFileIdentity) databaseMigrationIdentity {
	return databaseMigrationIdentity{i.Dev, i.Ino, i.Mode, i.UID, i.GID, i.Links, i.Size, unix.Timespec{Sec: i.MtimeSec, Nsec: i.MtimeNsec}, unix.Timespec{Sec: i.CtimeSec, Nsec: i.CtimeNsec}}
}
func openRecoveryDatabaseMigration(snapshot string, c recoveryDatabaseMigrationConfig) (*recoveryDatabaseMigration, error) {
	if os.Geteuid() != 0 || os.Getegid() != 0 || c.owner.uid == 0 || c.owner.gid == 0 {
		return nil, fmt.Errorf("database migration requires root and a non-root panel owner")
	}
	a, e := c.authority(snapshot)
	if e != nil {
		return nil, e
	}
	v := &recoveryDatabaseMigration{c: c, a: a, id: a.Identity(), records: map[string][]byte{}, recordIDs: map[string]databaseMigrationIdentity{}}
	fail := func(e error) (*recoveryDatabaseMigration, error) { v.close(); return nil, e }
	if v.id.Snapshot != snapshot || !databaseMigrationTokenPattern.MatchString(v.id.TokenSHA256) || !databaseMigrationTokenPattern.MatchString(v.id.SnapshotDatabaseSHA256) {
		return fail(fmt.Errorf("database authority identity is invalid"))
	}
	b := v.id.DatabaseBefore
	if len(b.Attributes) != 0 || len(b.ParentAttributes) != 0 {
		return fail(fmt.Errorf("database migration refuses unsupported owner attributes"))
	}
	v.before = databaseMigrationFile{databaseMigrationEvidenceIdentity(b.File), b.SHA256}
	v.parentID = databaseMigrationDirectory{b.Parent.Dev, b.Parent.Ino, b.Parent.Mode, b.Parent.UID, b.Parent.GID}
	if v.before.Identity.Mode != unix.S_IFREG|0600 || v.before.Identity.UID != c.owner.uid || v.before.Identity.GID != c.owner.gid || v.before.Identity.Links != 1 {
		return fail(fmt.Errorf("database authority owner differs from installed panel owner"))
	}
	if e = validateRootOwnedSnapshotDirectoryChain(filepath.Dir(c.parent)); e != nil {
		return fail(e)
	}
	v.parent, e = databaseMigrationOpenDirectory(c.parent)
	if e != nil {
		return fail(e)
	}
	if e = v.revalidate(); e != nil {
		return fail(e)
	}
	return v, nil
}
func (v *recoveryDatabaseMigration) close() {
	for _, f := range []*os.File{v.work, v.authority, v.transaction, v.root, v.parent} {
		if f != nil {
			f.Close()
		}
	}
	if v.a != nil {
		v.a.Close()
	}
}
func (v *recoveryDatabaseMigration) point(name string) {
	if v.c.checkpoint != nil {
		v.c.checkpoint(name)
	}
}
func (v *recoveryDatabaseMigration) pinRecord(name string, raw []byte) error {
	f, e := databaseMigrationOpenAt(v.authority, name, false)
	if e != nil {
		return e
	}
	defer f.Close()
	st, e := databaseMigrationStat(f)
	if e != nil {
		return e
	}
	actual, e := databaseMigrationRead(v.authority, name)
	if e != nil {
		return e
	}
	if !bytes.Equal(raw, actual) {
		return fmt.Errorf("database record changed before binding")
	}
	if e = databaseMigrationSameEntry(v.authority, name, f); e != nil {
		return e
	}
	id := databaseMigrationID(st)
	if prior, ok := v.recordIDs[name]; ok && prior != id {
		return fmt.Errorf("database record inode or metadata changed: %s", name)
	}
	v.recordIDs[name] = id
	v.records[name] = append([]byte(nil), raw...)
	return nil
}
func (v *recoveryDatabaseMigration) revalidate() error {
	if e := v.a.Revalidate(); e != nil {
		return e
	}
	if e := validateRootOwnedSnapshotDirectoryChain(filepath.Dir(v.c.parent)); e != nil {
		return e
	}
	if e := databaseMigrationSameDirectory(v.parent, v.parentID); e != nil {
		return e
	}
	for _, proof := range []struct {
		f              *os.File
		uid, gid, mode uint32
	}{{v.root, 0, v.c.owner.gid, 0710}, {v.transaction, 0, v.c.owner.gid, 0710}, {v.authority, 0, 0, 0700}} {
		if proof.f == nil {
			continue
		}
		id, e := databaseMigrationDirectoryProof(proof.f, proof.uid, proof.gid, proof.mode)
		if e != nil {
			return e
		}
		if e = databaseMigrationSameDirectory(proof.f, id); e != nil {
			return e
		}
	}
	for name, want := range v.records {
		f, e := databaseMigrationOpenAt(v.authority, name, false)
		if e != nil {
			return e
		}
		st, e := databaseMigrationStat(f)
		f.Close()
		if e != nil {
			return e
		}
		if databaseMigrationID(st) != v.recordIDs[name] {
			return fmt.Errorf("bound database record identity changed: %s", name)
		}

		actual, e := databaseMigrationRead(v.authority, name)
		if e != nil {
			return fmt.Errorf("bound database record unavailable: %s: %v", name, e)
		}
		if !bytes.Equal(actual, want) {
			return fmt.Errorf("bound database record changed: %s", name)
		}
	}
	if v.admissionRaw != nil {
		for _, p := range []struct {
			f  *os.File
			id databaseMigrationDirectory
		}{{v.root, v.admission.Root}, {v.transaction, v.admission.Transaction}, {v.authority, v.admission.Authority}} {
			if e := databaseMigrationSameDirectory(p.f, p.id); e != nil {
				return e
			}
		}
		raw, e := databaseMigrationRead(v.authority, "admission.json")
		if e != nil {
			return e
		}
		if !bytes.Equal(raw, v.admissionRaw) {
			return fmt.Errorf("database migration admission changed")
		}
		if e = v.journalGrammar(); e != nil {
			return e
		}
	}
	return nil
}
func (v *recoveryDatabaseMigration) requireActive(operation string) error {
	if v.id.Operation != operation || v.id.Phase != "active" {
		return fmt.Errorf("database migration requires the exact active %s operation", operation)
	}
	if e := v.c.stopped(); e != nil {
		return e
	}
	if e := v.c.readers([]databaseMigrationFile{v.before}); e != nil {
		return e
	}
	return v.revalidate()
}
func (v *recoveryDatabaseMigration) current() (databaseMigrationFile, error) {
	return databaseMigrationInspect(v.parent, databaseMigrationDB, v.c.owner.uid, v.c.owner.gid)
}
func (v *recoveryDatabaseMigration) requireBefore(renamed bool) error {
	now, e := v.current()
	if e != nil {
		return e
	}
	if !databaseMigrationSameFile(now, v.before, renamed) {
		return fmt.Errorf("canonical database no longer matches the admitted before-image")
	}
	if e = databaseMigrationNoSidecars(v.parent, databaseMigrationDB); e != nil {
		return e
	}
	return v.revalidate()
}
func (v *recoveryDatabaseMigration) openWorkspace(create bool) error {
	var e error
	v.root, e = databaseMigrationOpenAt(v.parent, databaseMigrationRootName, true)
	if errors.Is(e, unix.ENOENT) && create {
		v.root, e = databaseMigrationMkdir(v.parent, databaseMigrationRootName, 0, v.c.owner.gid, 0710)
		if e == nil {
			v.point("database_root_created")
		}
	}
	if errors.Is(e, unix.ENOENT) {
		return errDatabaseMigrationAbsent
	}
	if e != nil {
		return e
	}
	if _, e = databaseMigrationDirectoryProof(v.root, 0, v.c.owner.gid, 0710); e != nil {
		return e
	}
	v.transaction, e = databaseMigrationOpenAt(v.root, v.id.TokenSHA256, true)
	if errors.Is(e, unix.ENOENT) && create {
		v.transaction, e = databaseMigrationMkdir(v.root, v.id.TokenSHA256, 0, v.c.owner.gid, 0710)
		if e == nil {
			v.point("database_token_created")
		}
	}
	if errors.Is(e, unix.ENOENT) {
		return errDatabaseMigrationAbsent
	}
	if e != nil {
		return e
	}
	if _, e = databaseMigrationDirectoryProof(v.transaction, 0, v.c.owner.gid, 0710); e != nil {
		return e
	}
	v.authority, e = databaseMigrationOpenAt(v.transaction, "authority", true)
	if errors.Is(e, unix.ENOENT) && create {
		v.authority, e = databaseMigrationMkdir(v.transaction, "authority", 0, 0, 0700)
		if e == nil {
			v.point("database_authority_created")
		}
	}
	if errors.Is(e, unix.ENOENT) && !create {
		names, readErr := databaseMigrationNames(v.transaction)
		if readErr != nil {
			return readErr
		}
		if len(names) != 0 {
			return fmt.Errorf("incomplete database authority has unexpected residue")
		}
		if e = v.revalidate(); e != nil {
			return e
		}
		return errDatabaseMigrationAbsent
	}
	if e != nil {
		return e
	}
	if _, e = databaseMigrationDirectoryProof(v.authority, 0, 0, 0700); e != nil {
		return e
	}
	return nil
}
func (v *recoveryDatabaseMigration) journalGrammar() error {
	names, e := databaseMigrationNames(v.authority)
	if e != nil {
		return e
	}
	for _, name := range names {
		if databaseMigrationBuildPattern.MatchString(name) {
			f, e := databaseMigrationOpenAt(v.authority, name, true)
			if e != nil {
				return e
			}
			_, e = databaseMigrationDirectoryProof(f, 0, 0, 0700)
			f.Close()
			if e != nil {
				return e
			}
			continue
		}
		switch name {
		case "admission.json", "seal.json", "publication.json", "published.json", "restoration.json", "restored.json":
		default:
			if !databaseMigrationRecordPattern.MatchString(name) {
				return fmt.Errorf("unknown database migration authority entry: %s", name)
			}
		}
		if _, e = databaseMigrationRead(v.authority, name); e != nil {
			return e
		}
	}
	names, e = databaseMigrationNames(v.transaction)
	if e != nil {
		return e
	}
	for _, name := range names {
		if name != "authority" && name != "work" {
			return fmt.Errorf("unknown database migration transaction entry")
		}
	}
	return nil
}
func (v *recoveryDatabaseMigration) readAdmission() error {
	raw, e := databaseMigrationRead(v.authority, "admission.json")
	if errors.Is(e, unix.ENOENT) {
		return errDatabaseMigrationAbsent
	}
	if e != nil {
		return e
	}
	var a databaseMigrationAdmission
	if e = databaseMigrationDecode(raw, &a); e != nil {
		return e
	}
	if a.Schema != databaseMigrationAdmissionSchema || a.Snapshot != v.id.Snapshot || a.TokenSHA256 != v.id.TokenSHA256 || a.MaterialSHA256 != v.id.MaterialSHA256 || a.SnapshotManifestSHA256 != v.id.SnapshotManifestSHA256 || a.CandidatePanelSHA256 != v.id.CandidatePanelSHA256 || a.Parent != v.parentID || a.Before != v.before {
		return fmt.Errorf("database migration admission disagrees with current authority")
	}
	v.admission, v.admissionRaw = a, raw
	if e = v.pinRecord("admission.json", raw); e != nil {
		return e
	}
	return v.revalidate()
}
func prepareRecoveryDatabaseMigrationWith(snapshot string, c recoveryDatabaseMigrationConfig) (string, error) {
	v, e := openRecoveryDatabaseMigration(snapshot, c)
	if e != nil {
		return "", e
	}
	defer v.close()
	if e = v.requireActive("update"); e != nil {
		return "", e
	}
	if e = v.requireBefore(false); e != nil {
		return "", e
	}
	if e = v.openWorkspace(true); e != nil {
		return "", e
	}
	raw, e := databaseMigrationRead(v.authority, "admission.json")
	if e == nil {
		if e = v.readAdmission(); e != nil {
			return "", e
		}
		if e = databaseMigrationAbsent(v.authority, "seal.json"); e != nil {
			return "", fmt.Errorf("an admitted migration has already begun; recover the same operation: %w", e)
		}
		v.work, e = databaseMigrationOpenAt(v.transaction, "work", true)
		if e != nil {
			return "", e
		}
		if e = databaseMigrationSameDirectory(v.work, v.admission.Work); e != nil {
			return "", e
		}
		now, e := databaseMigrationInspect(v.work, databaseMigrationDB, c.owner.uid, c.owner.gid)
		if e != nil {
			return "", e
		}
		if now != v.admission.Initial {
			return "", fmt.Errorf("migration work already changed; never restart an unknown migration")
		}
		if e = databaseMigrationNoSidecars(v.work, databaseMigrationDB); e != nil {
			return "", e
		}
		if e = v.requireBefore(false); e != nil {
			return "", e
		}
		return v.work.Name(), nil
	}
	if !errors.Is(e, unix.ENOENT) {
		return "", e
	}
	_ = raw
	// Uncommitted preparations are preserved, never adopted as a completed copy.
	names, e := databaseMigrationNames(v.authority)
	if e != nil {
		return "", e
	}
	if len(names) != 0 {
		return "", fmt.Errorf("uncommitted database migration preparation is retained; recover this operation")
	}
	if e = databaseMigrationAbsent(v.transaction, "work"); e != nil {
		return "", e
	}
	v.work, e = databaseMigrationMkdir(v.transaction, "work", c.owner.uid, c.owner.gid, 0700)
	if e != nil {
		return "", e
	}
	v.point("database_work_created")
	source, e := v.a.SnapshotDatabase()
	if e != nil {
		return "", e
	}
	defer source.Close()
	st, e := databaseMigrationStat(source)
	if e != nil {
		return "", e
	}
	if e = databaseMigrationCopy(v.work, databaseMigrationDB, source, st.Size, c.owner.uid, c.owner.gid); e != nil {
		return "", e
	}
	v.point("database_work_copied")
	initial, e := databaseMigrationInspect(v.work, databaseMigrationDB, c.owner.uid, c.owner.gid)
	if e != nil {
		return "", e
	}
	sum, e := digestPinnedServiceOperationFile(source, st.Size)
	if e != nil {
		return "", e
	}
	if initial.SHA256 != fmt.Sprintf("%x", sum) || initial.SHA256 != v.id.SnapshotDatabaseSHA256 {
		return "", fmt.Errorf("migration working copy does not match snapshot")
	}
	if e = validateServiceOperationSnapshot(filepath.Join(v.work.Name(), databaseMigrationDB), serviceOperationSnapshotSchemaNormal); e != nil {
		return "", e
	}
	if e = v.requireBefore(false); e != nil {
		return "", e
	}
	rootID, e := databaseMigrationDirectoryProof(v.root, 0, c.owner.gid, 0710)
	if e != nil {
		return "", e
	}
	tokenID, e := databaseMigrationDirectoryProof(v.transaction, 0, c.owner.gid, 0710)
	if e != nil {
		return "", e
	}
	authID, e := databaseMigrationDirectoryProof(v.authority, 0, 0, 0700)
	if e != nil {
		return "", e
	}
	workID, e := databaseMigrationDirectoryProof(v.work, c.owner.uid, c.owner.gid, 0700)
	if e != nil {
		return "", e
	}
	admission := databaseMigrationAdmission{databaseMigrationAdmissionSchema, snapshot, v.id.TokenSHA256, v.id.MaterialSHA256, v.id.SnapshotManifestSHA256, v.id.CandidatePanelSHA256, v.parentID, v.before, rootID, tokenID, authID, workID, initial}
	if e = databaseMigrationRecordAt(v.authority, "admission.json", admission, func() { v.point("database_admission_staged") }); e != nil {
		return "", e
	}
	v.point("database_admission_durable")
	if e = v.readAdmission(); e != nil {
		return "", e
	}
	if e = v.requireBefore(false); e != nil {
		return "", e
	}
	return v.work.Name(), nil
}
