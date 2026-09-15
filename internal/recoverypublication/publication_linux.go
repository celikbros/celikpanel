//go:build linux

package recoverypublication

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const prefixRoot = "/opt/celikpanel"
const snapshotRoot = "/var/backups/celikpanel/update-snapshots"
const candidateRoot = "/var/backups/celikpanel/releases"
const transactionRoot = "/var/lib/celikpanel-release-transaction"
const journalName = ".recovery-publications"

type config struct {
	anchor, prefix, snapshots, candidates, transaction string
	fd                                                 int
	stopped                                            func() error
	databaseOwner                                      func() (uint32, uint32, error) // Private test identity seam; nil resolves native celikpanel.
	checkpoint                                         func(string)                   // Private deterministic fault seam; nil in production.
}

func production() config {
	return config{anchor: "/", prefix: prefixRoot, snapshots: snapshotRoot, candidates: candidateRoot, transaction: transactionRoot, fd: 9, stopped: verifyStopped}
}

// ProbeCurrentResources is a read-only eligibility check before quiescing.
// It requires the inherited native release lock but no active operation or
// snapshot, never creates a journal, and makes no service/runtime health claim.
func ProbeCurrentResources() error { return probeCurrentResources(production()) }
func probeCurrentResources(c config) error {
	if os.Geteuid() != 0 || os.Getegid() != 0 {
		return ErrUnavailable
	}
	prefix, err := openPath(c.anchor, c.prefix)
	if err != nil {
		return ErrUnavailable
	}
	defer prefix.Close()
	txn, err := openPath(c.anchor, c.transaction)
	if err != nil {
		return ErrUnavailable
	}
	defer txn.Close()
	if st, e := fstat(txn); e != nil || st.Mode&07777 != 0700 {
		return ErrUnavailable
	}
	pid, tid := rootIdentity(prefix), rootIdentity(txn)
	if err = verifyLock(txn, c.fd); err != nil {
		return err
	}
	// A default ACL on the publication parent must not be inherited by stages.
	if _, err = readAttributes(prefix); err != nil {
		return err
	}
	roots := map[string]*os.File{}
	before := map[string]tree{}
	defer func() {
		for _, f := range roots {
			f.Close()
		}
	}()
	for _, resource := range []string{"bin", "web"} {
		f, e := openAt(prefix, resource, true)
		if e != nil {
			return ErrUnavailable
		}
		roots[resource] = f
		before[resource], err = scan(f)
		if err != nil {
			return err
		}
		files := before[resource].entryMap()
		if resource == "bin" && (files["agent"].SHA == "" || files["panel"].SHA == "") || resource == "web" && files["index.html"].SHA == "" {
			return ErrUnavailable
		}
	}
	for resource, f := range roots {
		after, e := scan(f)
		if e != nil || !before[resource].equal(after) || !samePath(prefix, resource, f) {
			return ErrOwnerChanged
		}
	}
	if !sameRootPath(c, c.prefix, prefix, pid) || !sameRootPath(c, c.transaction, txn, tid) {
		return ErrOwnerChanged
	}
	return verifyLock(txn, c.fd)
}

// RestoreExisting restores only bin or web for the exact existing active
// rollback. Token and native lock are read from fixed roots, never arguments.
func RestoreExisting(r Request) error { return publish(r, "rollback", production()) }

// ApplyExisting uses the same protocol for a forward update. Its complete v6
// snapshot must already exist before any product tree can be exchanged.
func ApplyExisting(r Request) error { return publish(r, "update", production()) }

type intent struct {
	Schema            string   `json:"schema"`
	Resource          string   `json:"resource"`
	Operation         string   `json:"operation"`
	Snapshot          string   `json:"snapshot"`
	SnapshotManifest  string   `json:"snapshot_manifest_sha256"`
	CandidateRoot     string   `json:"candidate_root"`
	CandidateManifest string   `json:"candidate_manifest_sha256"`
	TokenHash         string   `json:"transaction_token_sha256"`
	Parent            identity `json:"parent"`
	Stage             string   `json:"stage"`
	Before            tree     `json:"before"`
	After             tree     `json:"after"`
	MaterialSHA       string   `json:"recovery_material_sha256,omitempty"`
}
type inputs struct {
	c                                                          config
	request                                                    Request
	operation                                                  string
	token                                                      string
	marker                                                     []byte
	prefix, transaction, snapshot, candidate, oldRoot, newRoot *os.File
	old, new                                                   tree
	snapshotRows, candidateRows                                map[string]string
	prefixID, transactionID, snapshotID, candidateID           identity
	material                                                   *verifiedMaterial
}

func (v *inputs) close() {
	if v.material != nil {
		v.material.close()
	}
	for _, f := range []*os.File{v.oldRoot, v.newRoot, v.snapshot, v.candidate, v.transaction, v.prefix} {
		if f != nil {
			f.Close()
		}
	}
}
func rootIdentity(f *os.File) identity {
	st, _ := fstat(f)
	v := id(st, true)
	v.Links = 0
	v.Size = 0
	return v
}
func sameRootPath(c config, path string, f *os.File, want identity) bool {
	fresh, err := openPath(c.anchor, path)
	if err != nil {
		return false
	}
	defer fresh.Close()
	return rootIdentity(fresh) == want && rootIdentity(f) == want
}
func verifyLock(txn *os.File, fd int) error {
	held, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
	_ = held
	if err != nil {
		return ErrUnavailable
	}
	var st, path unix.Stat_t
	if unix.Fstat(fd, &st) != nil || unix.Fstatat(int(txn.Fd()), "transaction.lock", &path, unix.AT_SYMLINK_NOFOLLOW) != nil || !reflect.DeepEqual(id(st, false), id(path, false)) || !safe(st, false) || st.Mode&07777 != 0600 || st.Size != 0 {
		return ErrUnavailable
	}
	raw, err := os.ReadFile(fmt.Sprintf("/proc/self/fdinfo/%d", fd))
	if err != nil || len(raw) > 16384 {
		return ErrUnavailable
	}
	exclusive := false
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 9 && fields[0] == "lock:" && fields[2] == "FLOCK" && fields[3] == "ADVISORY" && fields[4] == "WRITE" && fields[7] == "0" && fields[8] == "EOF" {
			exclusive = true
		}
	}
	if !exclusive {
		return ErrUnavailable
	}
	other, err := unix.Openat(int(txn.Fd()), "transaction.lock", unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return ErrUnavailable
	}
	defer unix.Close(other)
	err = unix.Flock(other, unix.LOCK_EX|unix.LOCK_NB)
	if err == nil {
		unix.Flock(other, unix.LOCK_UN)
		return ErrUnavailable
	}
	if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
		return ErrUnavailable
	}
	return nil
}

var markerPattern = regexp.MustCompile(`\Aversion=1\ntoken=([0-9a-f]{64})\noperation=(update|rollback)\nsnapshot=([^\n]+)\n\z`)

func readMarker(txn *os.File, op, snapshot string) ([]byte, string, error) {
	raw, err := readPrivateFile(txn, "active", 512)
	if err != nil {
		return nil, "", ErrUnavailable
	}
	match := markerPattern.FindSubmatch(raw)
	if match == nil || string(match[2]) != op || string(match[3]) != snapshot {
		return nil, "", ErrUnavailable
	}
	for _, name := range []string{"quiesce.pending", "completion.pending", "scheduler-restore.pending", "rollback-takeover.pending"} {
		var st unix.Stat_t
		if e := unix.Fstatat(int(txn.Fd()), name, &st, unix.AT_SYMLINK_NOFOLLOW); !errors.Is(e, unix.ENOENT) {
			return nil, "", ErrUnavailable
		}
	}
	return raw, string(match[1]), nil
}
func loadCandidate(r Request, op string, c config) (*inputs, error) {
	if os.Geteuid() != 0 || os.Getegid() != 0 || !ValidResource(r.Resource) || !ValidSnapshot(r.Snapshot) || !ValidManifest(r.SnapshotManifest) || !ValidManifest(r.CandidateManifest) || filepath.Clean(r.CandidateRoot) != r.CandidateRoot || filepath.Dir(r.CandidateRoot) != c.candidates || !releasePattern.MatchString(filepath.Base(r.CandidateRoot)) || (op != "update" && op != "rollback") {
		return nil, ErrUnavailable
	}
	v := &inputs{c: c, request: r, operation: op}
	okay := false
	defer func() {
		if !okay {
			v.close()
		}
	}()
	var err error
	if v.prefix, err = openPath(c.anchor, c.prefix); err != nil {
		return nil, ErrUnavailable
	}
	if v.transaction, err = openPath(c.anchor, c.transaction); err != nil {
		return nil, ErrUnavailable
	}
	if err = verifyLock(v.transaction, c.fd); err != nil {
		return nil, err
	}
	if v.marker, v.token, err = readMarker(v.transaction, op, r.Snapshot); err != nil {
		return nil, err
	}
	if v.snapshot, err = openPath(c.anchor, filepath.Join(c.snapshots, r.Snapshot)); err != nil {
		return nil, ErrUnavailable
	}
	if v.candidate, err = openPath(c.anchor, r.CandidateRoot); err != nil {
		return nil, ErrUnavailable
	}
	for _, f := range []*os.File{v.snapshot, v.candidate, v.transaction} {
		st, e := fstat(f)
		if e != nil || st.Mode&07777 != 0700 {
			return nil, ErrUnavailable
		}
	}
	v.prefixID = rootIdentity(v.prefix)
	v.transactionID = rootIdentity(v.transaction)
	v.snapshotID = rootIdentity(v.snapshot)
	v.candidateID = rootIdentity(v.candidate)
	if v.snapshotRows, err = manifest(v.snapshot, r.SnapshotManifest); err != nil {
		return nil, err
	}
	if v.candidateRows, err = manifest(v.candidate, r.CandidateManifest); err != nil {
		return nil, err
	}
	version, err := manifestValue(v.snapshot, v.snapshotRows, "snapshot.version")
	if err != nil || version != "6\n" {
		return nil, ErrUnavailable
	}
	target, err := manifestValue(v.snapshot, v.snapshotRows, "target-release.commit")
	if err != nil {
		return nil, err
	}
	commit, err := manifestValue(v.candidate, v.candidateRows, "release.commit")
	if err != nil || target != commit || strings.TrimSpace(commit) != snapshotPattern.FindStringSubmatch(r.Snapshot)[2] || !strings.HasPrefix(filepath.Base(r.CandidateRoot), strings.TrimSpace(commit)[:12]+"-") {
		return nil, ErrUnavailable
	}
	version, err = manifestValue(v.candidate, v.candidateRows, "release.version")
	if err != nil || version != "1\n" {
		return nil, ErrUnavailable
	}
	if v.oldRoot, err = openAt(v.snapshot, r.Resource, true); err != nil {
		return nil, ErrUnavailable
	}
	if r.Resource == "web" {
		web, e := openAt(v.candidate, "web", true)
		if e != nil {
			return nil, ErrUnavailable
		}
		v.newRoot, err = openAt(web, "dist", true)
		web.Close()
	} else {
		v.newRoot, err = openAt(v.candidate, "bin", true)
	}
	if err != nil {
		return nil, ErrUnavailable
	}
	if v.old, err = scan(v.oldRoot); err != nil {
		return nil, err
	}
	if err = checkManifestTree(v.old, v.snapshotRows, r.Resource); err != nil {
		return nil, err
	}
	if v.new, err = scan(v.newRoot); err != nil {
		return nil, err
	}
	sourcePrefix := "bin"
	if r.Resource == "web" {
		sourcePrefix = "web/dist"
	}
	if err = checkManifestTree(v.new, v.candidateRows, sourcePrefix); err != nil {
		return nil, err
	}
	if r.Resource == "bin" {
		m := v.new.entryMap()
		old := v.old.entryMap()
		for _, name := range []string{"panel", "agent"} {
			if m[name].SHA == "" || old[name].SHA == "" {
				return nil, ErrUnavailable
			}
		}
	} else {
		if v.new.entryMap()["index.html"].SHA == "" || v.old.entryMap()["index.html"].SHA == "" {
			return nil, ErrUnavailable
		}
	}
	if err = v.revalidate(); err != nil {
		return nil, err
	}
	okay = true
	return v, nil
}
func (v *inputs) revalidate() error {
	c := v.c
	r := v.request
	if !sameRootPath(c, c.prefix, v.prefix, v.prefixID) || !sameRootPath(c, c.transaction, v.transaction, v.transactionID) || !sameRootPath(c, filepath.Join(c.snapshots, r.Snapshot), v.snapshot, v.snapshotID) || (v.candidate != nil && !sameRootPath(c, r.CandidateRoot, v.candidate, v.candidateID)) {
		return ErrOwnerChanged
	}
	if err := verifyLock(v.transaction, c.fd); err != nil {
		return err
	}
	raw, _, err := readMarker(v.transaction, v.operation, r.Snapshot)
	if err != nil || !bytes.Equal(raw, v.marker) {
		return ErrOwnerChanged
	}
	if _, err = manifest(v.snapshot, r.SnapshotManifest); err != nil {
		return err
	}
	if v.candidate != nil {
		if _, err = manifest(v.candidate, r.CandidateManifest); err != nil {
			return err
		}
	}
	old, err := scan(v.oldRoot)
	if err != nil || !old.equal(v.old) {
		return ErrOwnerChanged
	}
	if v.newRoot != nil {
		fresh, err := scan(v.newRoot)
		if err != nil || !fresh.equal(v.new) {
			return ErrOwnerChanged
		}
	}
	if !samePath(v.snapshot, r.Resource, v.oldRoot) {
		return ErrOwnerChanged
	}
	if v.candidate != nil {
		if r.Resource == "bin" {
			if !samePath(v.candidate, "bin", v.newRoot) {
				return ErrOwnerChanged
			}
		} else {
			web, e := openAt(v.candidate, "web", true)
			if e != nil {
				return ErrOwnerChanged
			}
			same := samePath(web, "dist", v.newRoot)
			web.Close()
			if !same {
				return ErrOwnerChanged
			}
		}
	}
	if v.material != nil {
		if err := v.material.revalidate(); err != nil {
			return fmt.Errorf("publication material recheck: %w", err)
		}
	}
	if c.stopped == nil {
		return ErrUnavailable
	}
	return c.stopped()
}
func (v *inputs) expectedCandidate() tree {
	var wanted tree
	if v.request.Resource == "web" {
		wanted.Entries = append([]entry{}, v.new.Entries...)
	} else {
		wanted.Entries = append([]entry{}, v.old.Entries...)
		source := v.new.entryMap()
		for i, e := range wanted.Entries {
			if e.Path == "panel" || e.Path == "agent" {
				wanted.Entries[i] = source[e.Path]
			}
		}
	}
	for i := range wanted.Entries {
		e := &wanted.Entries[i]
		if v.request.Resource == "web" || e.Path == "." || e.Path == "panel" || e.Path == "agent" {
			mode := uint32(0644)
			if e.Identity.Mode&unix.S_IFMT == unix.S_IFDIR || v.request.Resource == "bin" && e.Path != "." {
				mode = 0755
			}
			e.Identity.Mode = e.Identity.Mode&unix.S_IFMT | mode
			e.Identity.UID = 0
			e.Identity.GID = 0
		}
	}
	return wanted
}
func (v *inputs) beforeAllowed(t tree) bool {
	desired := v.expectedCandidate()
	if t.semantic() == v.old.semantic() || t.semantic() == desired.semantic() {
		return true
	}
	if v.material != nil || v.request.Resource != "bin" {
		return false
	}
	old, new := v.old.entryMap(), desired.entryMap()
	if len(t.Entries) != len(old) {
		return false
	}
	for _, e := range t.Entries {
		o, ok := old[e.Path]
		if !ok {
			return false
		}
		n := new[e.Path]
		if semanticEntry(e) != semanticEntry(o) && semanticEntry(e) != semanticEntry(n) {
			return false
		}
	}
	return true
}
func randomName(prefix string) (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", ErrUnavailable
	}
	return prefix + hex.EncodeToString(b[:]), nil
}
func (c config) point(name string) {
	if c.checkpoint != nil {
		c.checkpoint(name)
	}
}
func (v *inputs) stage(journal *os.File, wanted tree) (string, tree, error) {
	name, err := randomName(".stage-")
	if err != nil {
		return "", tree{}, err
	}
	if unix.Mkdirat(int(journal.Fd()), name, 0700) != nil || unix.Fsync(int(journal.Fd())) != nil {
		return "", tree{}, ErrUnavailable
	}
	target, err := openAt(journal, name, true)
	if err != nil {
		return "", tree{}, ErrUnavailable
	}
	defer target.Close()
	for _, e := range wanted.Entries {
		if e.Path == "." {
			continue
		}
		parent, base, close, eparent := relativeParent(target, e.Path)
		if eparent != nil {
			return "", tree{}, eparent
		}
		if e.Identity.Mode&unix.S_IFMT == unix.S_IFDIR {
			err = unix.Mkdirat(int(parent.Fd()), base, 0700)
			close()
			if err != nil {
				return "", tree{}, ErrUnavailable
			}
			continue
		}
		src := v.oldRoot
		source := v.old.entryMap()[e.Path]
		if v.operation == "update" && (v.request.Resource == "web" || e.Path == "panel" || e.Path == "agent") {
			src = v.newRoot
			source = v.new.entryMap()[e.Path]
		}
		sourceParent, sourceName, closeSource, eopen := relativeParent(src, e.Path)
		if eopen != nil {
			close()
			return "", tree{}, eopen
		}
		input, eopen := openAt(sourceParent, sourceName, false)
		if eopen != nil {
			closeSource()
			close()
			return "", tree{}, ErrOwnerChanged
		}
		st, eopen := fstat(input)
		if eopen != nil || id(st, false) != source.Identity {
			input.Close()
			closeSource()
			close()
			return "", tree{}, ErrOwnerChanged
		}
		fd, eopen := unix.Openat(int(parent.Fd()), base, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
		if eopen != nil {
			input.Close()
			closeSource()
			close()
			return "", tree{}, ErrUnavailable
		}
		output := os.NewFile(uintptr(fd), "publication-stage")
		n, eCopy := io.Copy(output, io.LimitReader(input, maxFile+1))
		after, eStat := fstat(input)
		bound := samePath(sourceParent, sourceName, input)
		input.Close()
		closeSource()
		eMode := unix.Fchmod(fd, e.Identity.Mode&0777)
		eAttrs := copyAttributes(output, e.Attributes)
		eSync := unix.Fsync(fd)
		output.Close()
		close()
		if eCopy != nil || eStat != nil || n != source.Identity.Size || id(after, false) != source.Identity || !bound || eMode != nil || eAttrs != nil || eSync != nil {
			return "", tree{}, ErrOwnerChanged
		}
		v.c.point("stage_file_written")
	}
	// Directory metadata and durability are finalized bottom-up, including root.
	for i := len(wanted.Entries) - 1; i >= 0; i-- {
		e := wanted.Entries[i]
		if e.Identity.Mode&unix.S_IFMT != unix.S_IFDIR {
			continue
		}
		f := target
		owned := false
		if e.Path != "." {
			parent, base, close, er := relativeParent(target, e.Path)
			if er != nil {
				return "", tree{}, er
			}
			f, err = openAt(parent, base, true)
			close()
			if err != nil {
				return "", tree{}, err
			}
			owned = true
		}
		er := unix.Fchmod(int(f.Fd()), e.Identity.Mode&0777)
		ea := copyAttributes(f, e.Attributes)
		es := unix.Fsync(int(f.Fd()))
		if owned {
			f.Close()
		}
		if er != nil || ea != nil || es != nil {
			return "", tree{}, ErrUnavailable
		}
	}
	actual, err := scan(target)
	if err != nil || actual.semantic() != wanted.semantic() {
		return "", tree{}, ErrOwnerChanged
	}
	if unix.Fsync(int(journal.Fd())) != nil {
		return "", tree{}, ErrUnavailable
	}
	v.c.point("stage_ready")
	return name, actual, nil
}
func (v *inputs) journal() (*os.File, error) {
	base, err := privateDir(v.prefix, journalName)
	if err != nil {
		return nil, err
	}
	defer base.Close()
	token, err := privateDir(base, digest([]byte(v.token)))
	if err != nil {
		return nil, err
	}
	defer token.Close()
	result, err := privateDir(token, v.operation+"-"+v.request.Resource)
	if err != nil {
		return nil, err
	}
	if err = validateJournalObjects(result); err != nil {
		result.Close()
		return nil, err
	}
	return result, nil
}
func validateJournalObjects(result *os.File) error {
	if _, err := result.Seek(0, 0); err != nil {
		return ErrUnavailable
	}
	names, err := result.Readdirnames(67)
	if err != nil && err != io.EOF {
		return ErrUnavailable
	}
	if len(names) > 66 {
		return ErrUnavailable
	}
	for _, name := range names {
		stage := stagePattern.MatchString(name)
		if name != "intent.json" && name != "published" && !stage && !temporaryPattern.MatchString(name) {
			return ErrOwnerChanged
		}
		// Orphan stages and unpublished private receipts may be left by a crash.
		// Preserve them, but never follow/adopt a link or foreign object as one.
		f, e := openAt(result, name, stage)
		if e != nil {
			return ErrOwnerChanged
		}
		st, e := fstat(f)
		bound := samePath(result, name, f)
		f.Close()
		if e != nil || !bound || (!stage && (st.Mode&07777 != 0600 || st.Size > maxIntent)) {
			return ErrOwnerChanged
		}
	}
	return nil
}

// When update already published a before/after intent, rollback must respect
// that evidence too. A matching candidate hash alone does not excuse edits to
// the captured current or retired tree between publication and takeover.
func (v *inputs) verifyForwardBeforeRestore() error {
	if v.operation != "rollback" {
		return nil
	}
	path := filepath.Join(v.c.prefix, journalName, digest([]byte(v.token)), "update-"+v.request.Resource)
	parent, err := optionalPath(v.c, filepath.Dir(path))
	if errors.Is(err, ErrMaterialAbsent) {
		return v.requireOldWithoutForward()
	}
	if err != nil {
		return ErrUnavailable
	}
	defer parent.Close()
	journal, err := openAt(parent, filepath.Base(path), true)
	if errors.Is(err, unix.ENOENT) {
		return v.requireOldWithoutForward() // Legacy unchanged; v2 requires old snapshot state.
	}
	if err != nil {
		return ErrOwnerChanged
	}
	defer journal.Close()
	st, err := fstat(journal)
	if err != nil || st.Mode&07777 != 0700 || validateJournalObjects(journal) != nil {
		return ErrOwnerChanged
	}
	jid := rootIdentity(journal)
	raw, err := readPrivateFile(journal, "intent.json", maxIntent)
	if errors.Is(err, unix.ENOENT) {
		if _, e := readPrivateFile(journal, "published", 4096); !errors.Is(e, unix.ENOENT) {
			return ErrOwnerChanged
		}
		return v.requireOldWithoutForward() // No committed forward publication authority.
	}
	var forward intent
	if err != nil || decodeExact(raw, &forward) != nil || !v.validIntent(forward, "update", v.expectedCandidate()) {
		return ErrOwnerChanged
	}
	current, err := openAt(v.prefix, v.request.Resource, true)
	if err != nil {
		return ErrOwnerChanged
	}
	defer current.Close()
	if forward.Schema == MaterialNoopIntentSchema {
		actual, e := scan(current)
		if e != nil || !actual.equal(forward.After) || !samePath(v.prefix, v.request.Resource, current) || !sameRootPath(v.c, path, journal, jid) {
			return ErrOwnerChanged
		}
		receipt, e := readPrivateFile(journal, "published", 4096)
		if errors.Is(e, unix.ENOENT) {
			return nil
		}
		if e != nil || !bytes.Equal(receipt, []byte("format=celikpanel-resource-publication-v1\nintent="+digest(raw)+"\n")) {
			return ErrOwnerChanged
		}
		return nil
	}
	staged, err := openAt(journal, forward.Stage, true)
	if err != nil {
		return ErrOwnerChanged
	}
	defer staged.Close()
	a, err := scan(current)
	if err != nil {
		return err
	}
	b, err := scan(staged)
	if err != nil {
		return err
	}
	before := a.equal(forward.Before) && b.equal(forward.After)
	after := a.equal(forward.After) && b.equal(forward.Before)
	if !before && !after || !samePath(v.prefix, v.request.Resource, current) || !samePath(journal, forward.Stage, staged) || !sameRootPath(v.c, path, journal, jid) {
		return ErrOwnerChanged
	}
	receipt, err := readPrivateFile(journal, "published", 4096)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil || !after || !bytes.Equal(receipt, []byte("format=celikpanel-resource-publication-v1\nintent="+digest(raw)+"\n")) {
		return ErrOwnerChanged
	}
	return nil
}
func (v *inputs) validIntent(record intent, operation string, wanted tree) bool {
	r := v.request
	schema, materialSHA := Schema, ""
	if v.material != nil {
		schema, materialSHA = MaterialIntentSchema, v.material.sha
		if operation == "update" && record.Before.semantic() != v.old.semantic() {
			return false
		}
	}
	stageValid := stagePattern.MatchString(record.Stage)
	if record.Schema == MaterialNoopIntentSchema {
		schema = MaterialNoopIntentSchema
		stageValid = operation == "update" && v.material != nil && modernMaterial(v.material.record.Schema) && record.Stage == "" && record.Before.equal(record.After)
	}
	return record.Schema == schema && record.MaterialSHA == materialSHA && record.Resource == r.Resource && record.Operation == operation && record.Snapshot == r.Snapshot && record.SnapshotManifest == r.SnapshotManifest && record.CandidateRoot == r.CandidateRoot && record.CandidateManifest == r.CandidateManifest && record.TokenHash == digest([]byte(v.token)) && record.Parent == v.prefixID && stageValid && v.beforeAllowed(record.Before) && record.After.semantic() == wanted.semantic()
}
func publish(r Request, operation string, c config) error {
	v, err := load(r, operation, c)
	if err != nil {
		return err
	}
	defer v.close()
	wanted := v.old
	if operation == "update" {
		wanted = v.expectedCandidate()
	}
	journal, err := v.journal()
	if err != nil {
		return err
	}
	defer journal.Close()
	journalPath := filepath.Join(c.prefix, journalName, digest([]byte(v.token)), operation+"-"+r.Resource)
	journalID := rootIdentity(journal)
	checkJournal := func() error {
		if !sameRootPath(c, journalPath, journal, journalID) {
			return ErrOwnerChanged
		}
		return nil
	}
	raw, err := readPrivateFile(journal, "intent.json", maxIntent)
	var record intent
	if errors.Is(err, unix.ENOENT) {
		if e := v.verifyForwardBeforeRestore(); e != nil {
			return e
		}
		if _, e := readPrivateFile(journal, "published", 4096); !errors.Is(e, unix.ENOENT) {
			return ErrOwnerChanged
		}
		current, e := openAt(v.prefix, r.Resource, true)
		if e != nil {
			return ErrOwnerChanged
		}
		before, e := scan(current)
		bound := samePath(v.prefix, r.Resource, current)
		current.Close()
		if e != nil || !bound || !v.beforeAllowed(before) || (v.material != nil && operation == "update" && before.semantic() != v.old.semantic()) {
			return ErrOwnerChanged
		}
		if before.semantic() == wanted.semantic() && !(operation == "update" && v.material != nil && modernMaterial(v.material.record.Schema)) {
			if err = v.revalidate(); err != nil {
				return err
			}
			check, e := openAt(v.prefix, r.Resource, true)
			if e != nil {
				return ErrOwnerChanged
			}
			after, e := scan(check)
			bound := samePath(v.prefix, r.Resource, check)
			check.Close()
			if e != nil || !bound || !before.equal(after) {
				return ErrOwnerChanged
			}
			return checkJournal()
		}
		noop := before.semantic() == wanted.semantic()
		var stage string
		after := before
		if !noop {
			stage, after, e = v.stage(journal, wanted)
			if e != nil {
				return e
			}
		}
		if e = v.revalidate(); e != nil {
			return e
		}
		current, e = openAt(v.prefix, r.Resource, true)
		if e != nil {
			return ErrOwnerChanged
		}
		now, e := scan(current)
		bound = samePath(v.prefix, r.Resource, current)
		current.Close()
		if e != nil || !bound || !before.equal(now) {
			return ErrOwnerChanged
		}
		record = intent{Schema: Schema, Resource: r.Resource, Operation: operation, Snapshot: r.Snapshot, SnapshotManifest: r.SnapshotManifest, CandidateRoot: r.CandidateRoot, CandidateManifest: r.CandidateManifest, TokenHash: digest([]byte(v.token)), Parent: v.prefixID, Stage: stage, Before: before, After: after}
		if v.material != nil {
			record.Schema = MaterialIntentSchema
			record.MaterialSHA = v.material.sha
			if noop {
				record.Schema = MaterialNoopIntentSchema
			}
		}
		raw = canonical(record)
		if int64(len(raw)) > maxIntent {
			return ErrUnavailable
		}
		temp, e := randomName(".intent-")
		if e != nil {
			return e
		}
		if e = writeNew(journal, temp, raw); e != nil {
			return e
		}
		if err = checkJournal(); err != nil {
			return err
		}
		if unix.Renameat2(int(journal.Fd()), temp, int(journal.Fd()), "intent.json", unix.RENAME_NOREPLACE) != nil || unix.Fsync(int(journal.Fd())) != nil {
			return ErrUnavailable
		}
		c.point("intent_durable")
		if err = checkJournal(); err != nil {
			return err
		}
	} else if err != nil {
		return ErrUnavailable
	} else if decodeExact(raw, &record) != nil {
		return ErrUnavailable
	}
	if err = checkJournal(); err != nil {
		return err
	}
	if !v.validIntent(record, operation, wanted) {
		return ErrOwnerChanged
	}
	if err = v.revalidate(); err != nil {
		return err
	}
	if record.Schema == MaterialNoopIntentSchema {
		return v.finishNoopPublication(journal, raw, record, checkJournal)
	}
	current, err := openAt(v.prefix, r.Resource, true)
	if err != nil {
		return ErrOwnerChanged
	}
	defer current.Close()
	staged, err := openAt(journal, record.Stage, true)
	if err != nil {
		return ErrOwnerChanged
	}
	defer staged.Close()
	a, err := scan(current)
	if err != nil {
		return err
	}
	b, err := scan(staged)
	if err != nil {
		return err
	}
	if !samePath(v.prefix, r.Resource, current) || !samePath(journal, record.Stage, staged) {
		return ErrOwnerChanged
	}
	if a.equal(record.Before) && b.equal(record.After) {
		if _, e := readPrivateFile(journal, "published", 4096); !errors.Is(e, unix.ENOENT) {
			return ErrOwnerChanged
		}
		if err = v.revalidate(); err != nil {
			return err
		}
		c.point("before_exchange")
		if err = checkJournal(); err != nil {
			return err
		}
		// Recheck both immutable tree identities after the last callback / external
		// observation and immediately before publishing through the held directory FDs.
		again, e := scan(current)
		if e != nil || !again.equal(record.Before) {
			return ErrOwnerChanged
		}
		again, e = scan(staged)
		if e != nil || !again.equal(record.After) {
			return ErrOwnerChanged
		}
		if !samePath(v.prefix, r.Resource, current) || !samePath(journal, record.Stage, staged) {
			return ErrOwnerChanged
		}
		if err = checkJournal(); err != nil {
			return err
		}
		if unix.Renameat2(int(journal.Fd()), record.Stage, int(v.prefix.Fd()), r.Resource, unix.RENAME_EXCHANGE) != nil {
			return ErrUnavailable
		}
		c.point("exchange_done")
		if unix.Fsync(int(v.prefix.Fd())) != nil || unix.Fsync(int(journal.Fd())) != nil {
			return ErrUnavailable
		}
		c.point("exchange_durable")
	} else if !a.equal(record.After) || !b.equal(record.Before) {
		return ErrOwnerChanged
	}
	// No retired tree is deleted here. A separate terminal-proof policy must own
	// cleanup. The receipt is corroborative; the exact exchanged pair is authority.
	if err = v.revalidate(); err != nil {
		return err
	}
	current.Close()
	staged.Close()
	current, err = openAt(v.prefix, r.Resource, true)
	if err != nil {
		return ErrOwnerChanged
	}
	defer current.Close()
	staged, err = openAt(journal, record.Stage, true)
	if err != nil {
		return ErrOwnerChanged
	}
	defer staged.Close()
	a, err = scan(current)
	if err != nil {
		return err
	}
	b, err = scan(staged)
	if err != nil {
		return err
	}
	if !a.equal(record.After) || !b.equal(record.Before) || !samePath(v.prefix, r.Resource, current) || !samePath(journal, record.Stage, staged) {
		return ErrOwnerChanged
	}
	if err = checkJournal(); err != nil {
		return err
	}
	expected := []byte("format=celikpanel-resource-publication-v1\nintent=" + digest(raw) + "\n")
	existing, e := readPrivateFile(journal, "published", 4096)
	if errors.Is(e, unix.ENOENT) {
		temp, e := randomName(".published-")
		if e != nil {
			return e
		}
		if e = writeNew(journal, temp, expected); e != nil {
			return e
		}
		c.point("receipt_staged")
		if err = checkJournal(); err != nil {
			return err
		}
		// A killed publisher leaves an unreferenced private temporary file, never
		// a partially written committed receipt that would block a proven retry.
		if unix.Renameat2(int(journal.Fd()), temp, int(journal.Fd()), "published", unix.RENAME_NOREPLACE) != nil {
			return ErrUnavailable
		}
		c.point("receipt_published")
	} else if e != nil || !bytes.Equal(existing, expected) {
		return ErrOwnerChanged
	}
	if unix.Fsync(int(v.prefix.Fd())) != nil || unix.Fsync(int(journal.Fd())) != nil {
		return ErrUnavailable
	}
	c.point("receipt_durable")
	return nil
}
func verifyStopped() error {
	for _, unit := range []string{"celikpanel-agent.service", "celikpanel-panel.service"} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		out, err := exec.CommandContext(ctx, "/usr/bin/systemctl", "show", unit, "-p", "ActiveState", "-p", "MainPID", "-p", "ControlGroup").Output()
		cancel()
		if err != nil || len(out) > 4096 {
			return ErrUnavailable
		}
		fields := map[string]string{}
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 {
				return ErrUnavailable
			}
			if _, duplicate := fields[parts[0]]; duplicate {
				return ErrUnavailable
			}
			if parts[0] != "ActiveState" && parts[0] != "MainPID" && parts[0] != "ControlGroup" {
				return ErrUnavailable
			}
			fields[parts[0]] = parts[1]
		}
		if len(fields) != 3 || (fields["ActiveState"] != "inactive" && fields["ActiveState"] != "failed") || fields["MainPID"] != "0" {
			return ErrUnavailable
		}
		cg := fields["ControlGroup"]
		if cg == "" {
			continue
		}
		if cg != "/system.slice/"+unit {
			return ErrUnavailable
		}
		groupPath := "/sys/fs/cgroup" + cg
		if _, e := os.Lstat(groupPath); errors.Is(e, os.ErrNotExist) {
			continue
		} else if e != nil {
			return ErrUnavailable
		}
		count := 0
		err = filepath.WalkDir(groupPath, func(path string, d os.DirEntry, e error) error {
			if e != nil {
				return e
			}
			count++
			if count > 4096 || d.Type()&os.ModeSymlink != 0 {
				return ErrUnavailable
			}
			if d.IsDir() {
				raw, er := os.ReadFile(filepath.Join(path, "cgroup.procs"))
				if er != nil || len(raw) > 4096 || len(bytes.TrimSpace(raw)) != 0 {
					return ErrUnavailable
				}
			}
			return nil
		})
		if err != nil {
			return ErrUnavailable
		}
	}
	return nil
}
