//go:build linux

package recoverypublication

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

const canonicalDatabaseParent = "/var/lib/celikpanel"
const databaseName = "celikpanel.db"
const maxDatabaseSnapshotFile = int64(16 << 30)

func databaseParent(c config) string {
	if c.anchor == "/" {
		return canonicalDatabaseParent
	}
	return filepath.Join(c.anchor, "database")
}
func databaseOwner(c config) (uint32, uint32, error) {
	if c.databaseOwner != nil {
		return c.databaseOwner()
	}
	u, e := user.Lookup("celikpanel")
	if e != nil {
		return 0, 0, ErrUnavailable
	}
	g, e := user.LookupGroup("celikpanel")
	if e != nil {
		return 0, 0, ErrUnavailable
	}
	uid, e := strconv.ParseUint(u.Uid, 10, 32)
	if e != nil || uid == 0 {
		return 0, 0, ErrUnavailable
	}
	gid, e := strconv.ParseUint(g.Gid, 10, 32)
	if e != nil || gid == 0 {
		return 0, 0, ErrUnavailable
	}
	return uint32(uid), uint32(gid), nil
}
func databaseIdentity(st unix.Stat_t, parent bool) DatabaseFileIdentity {
	v := DatabaseFileIdentity{Dev: uint64(st.Dev), Ino: st.Ino, Mode: st.Mode, UID: st.Uid, GID: st.Gid, Links: uint64(st.Nlink), Size: st.Size, MtimeSec: st.Mtim.Sec, MtimeNsec: st.Mtim.Nsec, CtimeSec: st.Ctim.Sec, CtimeNsec: st.Ctim.Nsec}
	if parent {
		v.Links = 0
		v.Size = 0
		v.MtimeSec = 0
		v.MtimeNsec = 0
		v.CtimeSec = 0
		v.CtimeNsec = 0
	}
	return v
}
func openDatabaseParent(c config, uid, gid uint32) (*os.File, error) {
	path := databaseParent(c)
	ancestor, e := openPath(c.anchor, filepath.Dir(path))
	if e != nil {
		return nil, e
	}
	defer ancestor.Close()
	fd, e := unix.Openat(int(ancestor.Fd()), filepath.Base(path), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NOATIME, 0)
	if e != nil {
		return nil, ErrUnavailable
	}
	f := os.NewFile(uintptr(fd), "database-parent")
	st, e := fstat(f)
	if e != nil || st.Mode&unix.S_IFMT != unix.S_IFDIR || (st.Mode&07777 != 0700 && st.Mode&07777 != 0750) || (st.Uid != 0 && st.Uid != uid) || (st.Gid != 0 && st.Gid != gid) || !samePath(ancestor, filepath.Base(path), f) {
		f.Close()
		return nil, ErrUnavailable
	}
	if a, e := readAttributes(f); e != nil || len(a) != 0 {
		f.Close()
		if e != nil {
			return nil, e
		}
		return nil, ErrUnsupportedMetadata
	}
	return f, nil
}

// New normal transitions require the layout that apply-only installation and
// native StateDirectory startup preserve. Historical authority readers retain
// the broader secure/quarantine layout contract in openDatabaseParent.
func requireNewDatabaseParent(st unix.Stat_t, uid, gid uint32) error {
	if st.Mode != unix.S_IFDIR|0750 || st.Uid != uid || st.Gid != gid {
		return ErrUnsupportedDatabaseParent
	}
	return nil
}

func openCanonicalDatabase(parent *os.File, uid, gid uint32) (*os.File, error) {
	fd, e := unix.Openat(int(parent.Fd()), databaseName, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK|unix.O_NOATIME, 0)
	if e != nil {
		return nil, ErrUnavailable
	}
	f := os.NewFile(uintptr(fd), databaseName)
	st, e := fstat(f)
	if e != nil || st.Mode != unix.S_IFREG|0600 || st.Uid != uid || st.Gid != gid || st.Nlink != 1 || st.Size <= 0 || st.Size > maxDatabaseSnapshotFile {
		f.Close()
		return nil, ErrUnavailable
	}
	if a, e := readAttributes(f); e != nil || len(a) != 0 {
		f.Close()
		if e != nil {
			return nil, e
		}
		return nil, ErrUnsupportedMetadata
	}
	return f, nil
}
func noDatabaseSidecars(parent *os.File) error {
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		var st unix.Stat_t
		if e := unix.Fstatat(int(parent.Fd()), databaseName+suffix, &st, unix.AT_SYMLINK_NOFOLLOW); !errors.Is(e, unix.ENOENT) {
			return ErrOwnerChanged
		}
	}
	return nil
}
func hashPinnedFile(f *os.File, limit int64) (string, error) {
	before, e := fstat(f)
	if e != nil || before.Size < 0 || before.Size > limit {
		return "", ErrUnavailable
	}
	h := sha256.New()
	n, e := io.Copy(h, io.NewSectionReader(f, 0, before.Size))
	after, e2 := fstat(f)
	if e != nil || e2 != nil || n != before.Size || id(before, false) != id(after, false) {
		return "", ErrOwnerChanged
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func captureDatabaseBefore(c config) (*DatabaseBeforeEvidence, error) {
	uid, gid, e := databaseOwner(c)
	if e != nil {
		return nil, e
	}
	parent, e := openDatabaseParent(c, uid, gid)
	if e != nil {
		return nil, e
	}
	defer parent.Close()
	pst, e := fstat(parent)
	if e != nil {
		return nil, e
	}
	if e = requireNewDatabaseParent(pst, uid, gid); e != nil {
		return nil, e
	}
	db, e := openCanonicalDatabase(parent, uid, gid)
	if e != nil {
		return nil, e
	}
	defer db.Close()
	if e = noDatabaseSidecars(parent); e != nil {
		return nil, e
	}
	st, e := fstat(db)
	if e != nil {
		return nil, e
	}
	sum, e := hashPinnedFile(db, maxDatabaseSnapshotFile)
	if e != nil {
		return nil, e
	}
	if attrs, e := readAttributes(db); e != nil || len(attrs) != 0 {
		return nil, ErrUnsupportedMetadata
	}
	v := &DatabaseBeforeEvidence{Parent: databaseIdentity(pst, true), File: databaseIdentity(st, false), SHA256: sum}
	fresh, e := openDatabaseParent(c, uid, gid)
	if e != nil {
		return nil, e
	}
	defer fresh.Close()
	fst, e := fstat(fresh)
	if e != nil || databaseIdentity(fst, true) != v.Parent || !samePath(parent, databaseName, db) {
		return nil, ErrOwnerChanged
	}
	after, e := fstat(db)
	if e != nil || databaseIdentity(after, false) != v.File {
		return nil, ErrOwnerChanged
	}
	if e = noDatabaseSidecars(parent); e != nil {
		return nil, e
	}
	if !validDatabaseBefore(v) || v.File.UID != uid || v.File.GID != gid {
		return nil, ErrOwnerChanged
	}
	return v, nil
}
func verifyMaterialDatabaseBefore(want *DatabaseBeforeEvidence, c config) error {
	if want == nil {
		return nil
	}
	got, e := captureDatabaseBefore(c)
	if e != nil {
		return e
	}
	if !reflect.DeepEqual(got, want) {
		return ErrOwnerChanged
	}
	return nil
}
func snapshotTransition(snapshot *os.File, rows map[string]string) (string, error) {
	raw, e := manifestValue(snapshot, rows, "snapshot-transition.state")
	if e != nil {
		return "", e
	}
	switch raw {
	case "normal\n", "pre-ledger\n", "schema17\n":
		return strings.TrimSuffix(raw, "\n"), nil
	}
	return "", ErrUnavailable
}
func captureMaterialDatabaseBefore(snapshot *os.File, rows map[string]string, c config) (*DatabaseBeforeEvidence, error) {
	transition, e := snapshotTransition(snapshot, rows)
	if e != nil {
		return nil, e
	}
	if transition != "normal" {
		return nil, nil
	}
	if rows[databaseName] == "" {
		return nil, ErrUnavailable
	}
	return captureDatabaseBefore(c)
}
func validDatabaseBefore(v *DatabaseBeforeEvidence) bool {
	if v == nil || !ValidManifest(v.SHA256) || len(v.Attributes) != 0 || len(v.ParentAttributes) != 0 {
		return false
	}
	f, p := v.File, v.Parent
	return f.Dev != 0 && f.Ino != 0 && f.Mode == unix.S_IFREG|0600 && f.UID != 0 && f.GID != 0 && f.Links == 1 && f.Size > 0 && f.Size <= maxDatabaseSnapshotFile && f.MtimeNsec >= 0 && f.MtimeNsec < 1e9 && f.CtimeNsec >= 0 && f.CtimeNsec < 1e9 && p.Dev != 0 && p.Ino != 0 && p.Mode&unix.S_IFMT == unix.S_IFDIR && (p.Mode&07777 == 0700 || p.Mode&07777 == 0750) && (p.UID == 0 || p.UID == f.UID) && (p.GID == 0 || p.GID == f.GID) && p.Links == 0 && p.Size == 0 && p.MtimeSec == 0 && p.MtimeNsec == 0 && p.CtimeSec == 0 && p.CtimeNsec == 0
}
func validateDatabaseMaterial(snapshot *os.File, rows map[string]string, r materialRecord) error {
	if r.Schema != MaterialSchemaV3 {
		if r.DatabaseBefore != nil {
			return ErrUnavailable
		}
		return nil
	}
	transition, e := snapshotTransition(snapshot, rows)
	if e != nil {
		return e
	}
	if transition == "normal" {
		if !validDatabaseBefore(r.DatabaseBefore) || rows[databaseName] == "" {
			return ErrUnavailable
		}
	} else if r.DatabaseBefore != nil {
		return ErrUnavailable
	}
	return nil
}

// ProbeDatabaseMigration is a metadata-only preflight. A live WAL is permitted;
// it grants no stage/publication authority and does not hash a running database.
// The command wrapper additionally proves the empty native preflight boundary.
func ProbeDatabaseMigration() error { return probeDatabaseMigration(production()) }
func probeDatabaseMigration(c config) error {
	if os.Geteuid() != 0 || os.Getegid() != 0 {
		return ErrUnavailable
	}
	txn, e := openPath(c.anchor, c.transaction)
	if e != nil {
		return e
	}
	defer txn.Close()
	if e = verifyLock(txn, c.fd); e != nil {
		return e
	}
	tid := rootIdentity(txn)
	uid, gid, e := databaseOwner(c)
	if e != nil {
		return e
	}
	parent, e := openDatabaseParent(c, uid, gid)
	if e != nil {
		return e
	}
	defer parent.Close()
	pst, e := fstat(parent)
	if e != nil {
		return e
	}
	if e = requireNewDatabaseParent(pst, uid, gid); e != nil {
		return e
	}
	db, e := openCanonicalDatabase(parent, uid, gid)
	if e != nil {
		return e
	}
	defer db.Close()
	st, e := fstat(db)
	if e != nil {
		return e
	}
	c.point("database_metadata_probed")
	fresh, e := openDatabaseParent(c, uid, gid)
	if e != nil {
		return e
	}
	defer fresh.Close()
	after, e := fstat(fresh)
	current, e2 := fstat(db)
	if e != nil || e2 != nil || databaseIdentity(pst, true) != databaseIdentity(after, true) || st.Dev != current.Dev || st.Ino != current.Ino || st.Mode != current.Mode || st.Uid != current.Uid || st.Gid != current.Gid || current.Nlink != 1 || !samePath(parent, databaseName, db) || !sameRootPath(c, c.transaction, txn, tid) {
		return ErrOwnerChanged
	}
	if attrs, e := readAttributes(db); e != nil || len(attrs) != 0 {
		return ErrUnsupportedMetadata
	}
	return verifyLock(txn, c.fd)
}

// snapshotInventory proves the complete manifest inventory using pinned opens.
// The outer snapshot remains responsible for service-specific semantic checks;
// this authority never interprets TLS secrets or executes retained programs.
func snapshotInventory(root *os.File, expected string) (tree, map[string]string, error) {
	rows, e := manifest(root, expected)
	if e != nil {
		return tree{}, nil, e
	}
	result := tree{}
	seen := map[string]bool{}
	var total int64
	var visit func(*os.File, string) error
	visit = func(dir *os.File, path string) error {
		before, e := fstat(dir)
		if e != nil {
			return e
		}
		attrs, e := readAttributes(dir)
		if e != nil {
			return e
		}
		result.Entries = append(result.Entries, entry{Path: path, Identity: id(before, path == "."), Attributes: attrs})
		if _, e = dir.Seek(0, 0); e != nil {
			return e
		}
		names, e := dir.Readdirnames(maxEntries + 1)
		if e != nil && e != io.EOF {
			return e
		}
		if len(names) > maxEntries {
			return ErrUnavailable
		}
		sort.Strings(names)
		for _, name := range names {
			rel := name
			if path != "." {
				rel = path + "/" + name
			}
			if !validRelative(rel) || len(result.Entries) >= maxEntries {
				return ErrUnavailable
			}
			var st unix.Stat_t
			if unix.Fstatat(int(dir.Fd()), name, &st, unix.AT_SYMLINK_NOFOLLOW) != nil {
				return ErrOwnerChanged
			}
			kind := st.Mode & unix.S_IFMT
			if st.Uid != 0 || st.Mode&07022 != 0 || (kind != unix.S_IFDIR && kind != unix.S_IFREG) || st.Dev != before.Dev || (kind == unix.S_IFREG && st.Nlink != 1) {
				return ErrUnavailable
			}
			flags := unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_CLOEXEC | unix.O_NONBLOCK | unix.O_NOATIME
			if kind == unix.S_IFDIR {
				flags |= unix.O_DIRECTORY
			}
			fd, e := unix.Openat(int(dir.Fd()), name, flags, 0)
			if e != nil {
				return e
			}
			f := os.NewFile(uintptr(fd), rel)
			actual, e := fstat(f)
			if e != nil || id(st, false) != id(actual, false) {
				f.Close()
				return ErrOwnerChanged
			}
			if kind == unix.S_IFDIR {
				e = visit(f, rel)
			} else {
				attrs, e = readAttributes(f)
				var sum string
				if e == nil {
					sum, e = hashPinnedFile(f, maxDatabaseSnapshotFile)
				}
				if e == nil {
					if rel == "SHA256SUMS" {
						if sum != expected {
							e = ErrOwnerChanged
						}
					} else if rows[rel] == "" || rows[rel] != sum {
						e = ErrOwnerChanged
					} else {
						seen[rel] = true
					}
					result.Entries = append(result.Entries, entry{Path: rel, Identity: id(actual, false), SHA: sum, Attributes: attrs})
					total += actual.Size
					if total > 64<<30 {
						e = ErrUnavailable
					}
				}
			}
			bound := samePath(dir, name, f)
			f.Close()
			if e != nil {
				return e
			}
			if !bound {
				return ErrOwnerChanged
			}
		}
		after, e := fstat(dir)
		if e != nil || id(before, false) != id(after, false) {
			return ErrOwnerChanged
		}
		return nil
	}
	if e = visit(root, "."); e != nil {
		return tree{}, nil, e
	}
	if len(seen) != len(rows) {
		return tree{}, nil, ErrUnavailable
	}
	sort.Slice(result.Entries, func(i, j int) bool { return result.Entries[i].Path < result.Entries[j].Path })
	return result, rows, nil
}

type databaseAuthorityLinux struct {
	m          *verifiedMaterial
	inventory  tree
	database   entry
	boundFiles map[string]identity
}

func (a *databaseAuthorityLinux) Revalidate() error {
	if a == nil || a.m == nil {
		return ErrUnavailable
	}
	if e := a.m.revalidate(); e != nil {
		return e
	}
	fresh, _, e := snapshotInventory(a.m.snapshot, a.m.record.SnapshotManifest)
	if e != nil {
		return e
	}
	if !fresh.equal(a.inventory) {
		return ErrOwnerChanged
	}
	if e = a.m.revalidateTransaction(); e != nil {
		return e
	}
	files, e := databaseAuthorityFiles(a.m)
	if e != nil {
		return e
	}
	if !reflect.DeepEqual(files, a.boundFiles) {
		return ErrOwnerChanged
	}
	return nil
}
func (a *databaseAuthorityLinux) SnapshotDatabase() (*os.File, error) {
	if e := a.Revalidate(); e != nil {
		return nil, e
	}
	f, e := openAt(a.m.snapshot, databaseName, false)
	if e != nil {
		return nil, e
	}
	st, e := fstat(f)
	if e != nil || id(st, false) != a.database.Identity || !samePath(a.m.snapshot, databaseName, f) {
		f.Close()
		return nil, ErrOwnerChanged
	}
	return f, nil
}
func (a *databaseAuthorityLinux) Close() error {
	if a != nil && a.m != nil {
		a.m.close()
		a.m = nil
	}
	return nil
}
func databasePhase(p materialTransaction) string {
	if _, ok := p.markers["active"]; ok {
		return "active"
	}
	_, completion := p.markers["completion.pending"]
	_, scheduler := p.markers["scheduler-restore.pending"]
	if completion && scheduler {
		return "completion-scheduler"
	}
	if completion {
		return "completion"
	}
	return "scheduler"
}
func OpenDatabaseAuthority(snapshot string) (*DatabaseAuthority, error) {
	return openDatabaseAuthority(snapshot, production())
}
func openDatabaseAuthority(snapshot string, c config) (*DatabaseAuthority, error) {
	m, e := readMaterial(snapshot, c)
	if e != nil {
		return nil, e
	}
	okay := false
	defer func() {
		if !okay {
			m.close()
		}
	}()
	if m.proof.operation != "update" && m.proof.operation != "rollback" {
		return nil, ErrUnavailable
	}
	inventory, rows, e := snapshotInventory(m.snapshot, m.record.SnapshotManifest)
	if e != nil {
		return nil, e
	}
	transition, e := snapshotTransition(m.snapshot, rows)
	if e != nil {
		return nil, e
	}
	if e = m.revalidate(); e != nil {
		return nil, e
	}
	if m.record.Schema != MaterialSchemaV3 || transition != "normal" {
		if e = refuseDatabaseMaterialResidue(c, m.proof.token); e != nil {
			return nil, e
		}
		return nil, ErrLegacyDatabaseMaterial
	}
	uid, gid, e := databaseOwner(c)
	if e != nil {
		return nil, e
	}
	before := m.record.DatabaseBefore
	if !validDatabaseBefore(before) || before.File.UID != uid || before.File.GID != gid {
		return nil, ErrUnavailable
	}
	db := inventory.entryMap()[databaseName]
	if db.SHA == "" || db.Identity.Mode != unix.S_IFREG|0600 || db.Identity.UID != 0 || db.Identity.GID != 0 {
		return nil, ErrUnavailable
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if _, ok := inventory.entryMap()[databaseName+suffix]; ok {
			return nil, ErrUnavailable
		}
	}
	panel := m.record.Resources[0].Target.entryMap()["panel"].SHA
	if !ValidManifest(panel) {
		return nil, ErrUnavailable
	}
	boundFiles, e := databaseAuthorityFiles(m)
	if e != nil {
		return nil, e
	}
	h := &databaseAuthorityLinux{m: m, inventory: inventory, database: db, boundFiles: boundFiles}
	a := &DatabaseAuthority{handle: h, identity: DatabaseAuthorityIdentity{Snapshot: snapshot, TokenSHA256: m.record.TokenHash, SnapshotManifestSHA256: m.record.SnapshotManifest, MaterialSHA256: m.sha, CandidatePanelSHA256: panel, SnapshotDatabaseSHA256: db.SHA, Operation: m.proof.operation, Phase: databasePhase(m.proof), DatabaseBefore: *before}}
	c.point("database_authority_opened")
	if e = a.Revalidate(); e != nil {
		return nil, e
	}
	okay = true
	return a, nil
}

// VerifyDatabasePolicy distinguishes verified legacy data from required v3
// isolation. A malformed or orphaned material record never selects legacy.
func VerifyDatabasePolicy(snapshot string) error { return verifyDatabasePolicy(snapshot, production()) }
func verifyDatabasePolicy(snapshot string, c config) error {
	a, e := openDatabaseAuthority(snapshot, c)
	if e == nil {
		return a.Close()
	}
	if !errors.Is(e, ErrMaterialAbsent) {
		return e
	}
	// Strict material absence has already checked the native transaction and
	// surviving publication intents. Prove the legacy snapshot inventory too.
	txn, e := openPath(c.anchor, c.transaction)
	if e != nil {
		return e
	}
	defer txn.Close()
	if e = verifyLock(txn, c.fd); e != nil {
		return e
	}
	proof, e := readMaterialTransaction(txn, snapshot)
	if e != nil {
		return e
	}
	tid := rootIdentity(txn)
	snap, e := openPath(c.anchor, filepath.Join(c.snapshots, snapshot))
	if e != nil {
		return e
	}
	defer snap.Close()
	sid := rootIdentity(snap)
	if st, e := fstat(snap); e != nil || st.Mode&07777 != 0700 {
		return ErrUnavailable
	}
	raw, e := readPrivateFile(snap, "SHA256SUMS", maxManifest)
	if e != nil {
		return e
	}
	inventory, rows, e := snapshotInventory(snap, digest(raw))
	if e != nil {
		return e
	}
	if _, e = snapshotTransition(snap, rows); e != nil {
		return e
	}
	if e = validateLegacyDatabaseSnapshot(snap, rows, snapshot); e != nil {
		return e
	}
	if e = refuseDatabaseMaterialResidue(c, proof.token); e != nil {
		return e
	}
	c.point("database_legacy_absence_verified")
	again, e := readMaterial(snapshot, c)
	if again != nil {
		again.close()
	}
	if !errors.Is(e, ErrMaterialAbsent) {
		return ErrOwnerChanged
	}
	after, _, e := snapshotInventory(snap, digest(raw))
	if e != nil || !inventory.equal(after) || !sameRootPath(c, filepath.Join(c.snapshots, snapshot), snap, sid) || !sameRootPath(c, c.transaction, txn, tid) {
		return ErrOwnerChanged
	}
	p, e := readMaterialTransaction(txn, snapshot)
	if e != nil || !reflect.DeepEqual(p, proof) || verifyLock(txn, c.fd) != nil {
		return ErrOwnerChanged
	}
	if e = refuseDatabaseMaterialResidue(c, proof.token); e != nil {
		return e
	}
	return ErrLegacyDatabaseMaterial
}

func databaseAuthorityFiles(m *verifiedMaterial) (map[string]identity, error) {
	result := map[string]identity{}
	for _, name := range append([]string{"material.json"}, markerNames(m.proof)...) {
		parent := m.transaction
		if name == "material.json" {
			parent = m.root
		}
		f, e := openAt(parent, name, false)
		if e != nil {
			return nil, e
		}
		st, e := fstat(f)
		attrs, ae := readAttributes(f)
		bound := samePath(parent, name, f)
		f.Close()
		if e != nil || ae != nil || !bound || st.Mode&07777 != 0600 || len(attrs) != 0 {
			return nil, ErrOwnerChanged
		}
		result[name] = id(st, false)
	}
	return result, nil
}
func markerNames(p materialTransaction) []string {
	names := make([]string, 0, len(p.markers))
	for n := range p.markers {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
func validateLegacyDatabaseSnapshot(root *os.File, rows map[string]string, snapshot string) error {
	for name, want := range map[string]string{"snapshot.version": "6\n", "target-release.commit": snapshotPattern.FindStringSubmatch(snapshot)[2] + "\n"} {
		value, e := manifestValue(root, rows, name)
		if e != nil || value != want {
			return ErrUnavailable
		}
	}
	target, e := manifestValue(root, rows, "target-release.tree")
	if e != nil || len(target) != 41 || target[40] != '\n' {
		return ErrUnavailable
	}
	for _, ch := range target[:40] {
		if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f') {
			return ErrUnavailable
		}
	}
	if rows[databaseName] == "" {
		return ErrUnavailable
	}
	return noDatabaseSidecars(root)
}

// A database admission can exist before either product publication intent.
// Missing material must not turn surviving isolated-stage evidence into an
// older recovery policy. Only exact absence permits compatibility selection.
func refuseDatabaseMaterialResidue(c config, token string) error {
	path := databaseParent(c)
	ancestor, e := openPath(c.anchor, filepath.Dir(path))
	if e != nil {
		return e
	}
	defer ancestor.Close()
	var st unix.Stat_t
	e = unix.Fstatat(int(ancestor.Fd()), filepath.Base(path), &st, unix.AT_SYMLINK_NOFOLLOW)
	if errors.Is(e, unix.ENOENT) {
		return nil
	}
	if e != nil {
		return ErrUnavailable
	}
	uid, gid, e := databaseOwner(c)
	if e != nil {
		return e
	}
	parent, e := openDatabaseParent(c, uid, gid)
	if e != nil {
		return e
	}
	defer parent.Close()
	e = unix.Fstatat(int(parent.Fd()), ".release-db-migrations", &st, unix.AT_SYMLINK_NOFOLLOW)
	if errors.Is(e, unix.ENOENT) {
		return nil
	}
	if e != nil || st.Mode != unix.S_IFDIR|0710 || st.Uid != 0 || st.Gid != gid {
		return ErrUnavailable
	}
	fd, e := unix.Openat(int(parent.Fd()), ".release-db-migrations", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NOATIME, 0)
	if e != nil {
		return ErrUnavailable
	}
	root := os.NewFile(uintptr(fd), "database-migrations")
	defer root.Close()
	fresh, e := fstat(root)
	if e != nil || id(fresh, false) != id(st, false) || !samePath(parent, ".release-db-migrations", root) {
		return ErrOwnerChanged
	}
	e = unix.Fstatat(int(root.Fd()), digest([]byte(token)), &st, unix.AT_SYMLINK_NOFOLLOW)
	if !errors.Is(e, unix.ENOENT) {
		return ErrUnavailable
	}
	return nil
}
