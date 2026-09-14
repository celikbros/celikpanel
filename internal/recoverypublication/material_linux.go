//go:build linux

package recoverypublication

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/sys/unix"
)

const materialRoot = "/var/lib/celikpanel-release-state/recovery-material/v1"
const materialLimit = int64(48 << 20)

// These are evidence, never executable recovery code. Recovery executes its
// separately selected installed kit and compares the saved data contracts.
var materialFiles = []string{
	"release.version", "release.commit", "release.tree",
	"deploy/release-recovery.protocol", "deploy/release-sequence-policy",
	"deploy/release-recovery-runner.sh", "deploy/release-transaction-start-guard.sh",
	"deploy/systemd/celikpanel-release-recovery.service", "deploy/systemd/celikpanel-release-recovery.timer",
	"deploy/systemd/celikpanel-agent.service", "deploy/systemd/celikpanel-panel.service",
	"deploy/systemd/celikpanel-firewall-restore.service",
}

type materialResource struct {
	Resource string `json:"resource"`
	Old      tree   `json:"old"`
	New      tree   `json:"new"`
	Target   tree   `json:"target"`
}
type materialRecord struct {
	Schema            string             `json:"schema"`
	Snapshot          string             `json:"snapshot"`
	SnapshotManifest  string             `json:"snapshot_manifest_sha256"`
	CandidateRoot     string             `json:"candidate_root"`
	CandidateManifest string             `json:"candidate_manifest_sha256"`
	TokenHash         string             `json:"transaction_token_sha256"`
	CandidateCommit   string             `json:"candidate_commit"`
	CandidateTree     string             `json:"candidate_tree"`
	Resources         []materialResource `json:"resources"`
	Data              tree               `json:"data"`
	DataManifest      string             `json:"data_manifest_sha256"`
}
type materialTransaction struct {
	operation, token string
	markers          map[string][]byte
}
type verifiedMaterial struct {
	c                                 config
	path, sha                         string
	raw                               []byte
	record                            materialRecord
	proof                             materialTransaction
	root, data, snapshot, transaction *os.File
	rootID, snapshotID, transactionID identity
}

func (m *verifiedMaterial) close() {
	for _, f := range []*os.File{m.root, m.data, m.snapshot, m.transaction} {
		if f != nil {
			f.Close()
		}
	}
}
func materialBase(c config) string {
	if c.anchor == "/" {
		return materialRoot
	}
	return filepath.Join(c.anchor, "recovery-material", "v1")
}

// Unlike openPath, optionalPath distinguishes absent evidence from an unsafe
// path. A symlink, unreadable object or malformed existing directory never
// selects legacy fallback.
func optionalPath(c config, path string) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, ErrUnavailable
	}
	rel, err := filepath.Rel(c.anchor, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
		return nil, ErrUnavailable
	}
	f, err := openPath(c.anchor, c.anchor)
	if err != nil {
		return nil, ErrUnavailable
	}
	if rel == "." {
		return f, nil
	}
	for _, part := range strings.Split(rel, "/") {
		next, e := openAt(f, part, true)
		f.Close()
		if errors.Is(e, unix.ENOENT) {
			return nil, ErrMaterialAbsent
		}
		if e != nil {
			return nil, ErrUnavailable
		}
		f = next
	}
	return f, nil
}
func readMaterialTransaction(txn *os.File, snapshot string) (materialTransaction, error) {
	p := materialTransaction{markers: map[string][]byte{}}
	st, err := fstat(txn)
	if err != nil || st.Mode&07777 != 0700 {
		return p, ErrUnavailable
	}
	if _, err = txn.Seek(0, 0); err != nil {
		return p, ErrUnavailable
	}
	names, err := txn.Readdirnames(8)
	if err != nil || len(names) > 4 {
		return p, ErrUnavailable
	}
	for _, name := range names {
		if name == "transaction.lock" {
			continue
		}
		if name != "active" && name != "completion.pending" && name != "scheduler-restore.pending" {
			return p, ErrUnavailable
		}
		raw, e := readPrivateFile(txn, name, 512)
		match := markerPattern.FindSubmatch(raw)
		if e != nil || match == nil || string(match[3]) != snapshot {
			return p, ErrUnavailable
		}
		if p.token != "" && (p.token != string(match[1]) || p.operation != string(match[2])) {
			return p, ErrUnavailable
		}
		p.token, p.operation = string(match[1]), string(match[2])
		p.markers[name] = raw
	}
	if p.token == "" {
		return p, ErrUnavailable
	}
	if _, active := p.markers["active"]; active {
		if len(p.markers) != 1 {
			return p, ErrUnavailable
		}
	} else {
		if p.operation != "rollback" || len(p.markers) < 1 || len(p.markers) > 2 {
			return p, ErrUnavailable
		}
		if len(p.markers) == 2 && !bytes.Equal(p.markers["completion.pending"], p.markers["scheduler-restore.pending"]) {
			return p, ErrUnavailable
		}
	}
	return p, nil
}

// VerifyRecoveryMaterial checks only existing transaction-bound evidence; it
// does not require stopped services, create evidence, or execute saved data.
// The caller still verifies the complete snapshot (including DB/TLS) before
// restoring those resources. This package verifies both product payload trees.
func VerifyRecoveryMaterial(snapshot string) (string, error) {
	m, err := readMaterial(snapshot, production())
	if err != nil {
		return "", err
	}
	defer m.close()
	return filepath.Join(m.path, "data"), nil
}
func readMaterial(snapshot string, c config) (*verifiedMaterial, error) {
	if os.Geteuid() != 0 || os.Getegid() != 0 || !ValidSnapshot(snapshot) {
		return nil, ErrUnavailable
	}
	m := &verifiedMaterial{c: c}
	okay := false
	defer func() {
		if !okay {
			m.close()
		}
	}()
	var err error
	m.transaction, err = openPath(c.anchor, c.transaction)
	if err != nil || verifyLock(m.transaction, c.fd) != nil {
		return nil, ErrUnavailable
	}
	m.transactionID = rootIdentity(m.transaction)
	m.proof, err = readMaterialTransaction(m.transaction, snapshot)
	if err != nil {
		return nil, err
	}
	m.path = filepath.Join(materialBase(c), digest([]byte(m.proof.token)))
	m.root, err = optionalPath(c, m.path)
	if errors.Is(err, ErrMaterialAbsent) {
		if e := refuseMissingMaterialIntent(c, m.proof.token); e != nil {
			return nil, e
		}
		if e := refuseOtherMaterialToken(c, snapshot, m.proof.token); e != nil {
			return nil, e
		}
		return nil, ErrMaterialAbsent
	}
	if err != nil {
		return nil, err
	}
	if st, e := fstat(m.root); e != nil || st.Mode&07777 != 0700 {
		return nil, ErrUnavailable
	}
	m.rootID = rootIdentity(m.root)
	m.raw, err = readPrivateFile(m.root, "material.json", materialLimit)
	if err != nil || decodeExact(m.raw, &m.record) != nil {
		return nil, ErrUnavailable
	}
	m.sha = digest(m.raw)
	r := m.record
	if r.Schema != MaterialSchema || r.Snapshot != snapshot || !ValidManifest(r.SnapshotManifest) || !ValidManifest(r.CandidateManifest) || r.TokenHash != digest([]byte(m.proof.token)) || filepath.Clean(r.CandidateRoot) != r.CandidateRoot || filepath.Dir(r.CandidateRoot) != c.candidates || !releasePattern.MatchString(filepath.Base(r.CandidateRoot)) || !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(r.CandidateCommit) || !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(r.CandidateTree) || snapshotPattern.FindStringSubmatch(snapshot)[2] != r.CandidateCommit || !strings.HasPrefix(filepath.Base(r.CandidateRoot), r.CandidateCommit[:12]+"-") || !ValidManifest(r.DataManifest) || len(r.Resources) != 2 {
		return nil, ErrUnavailable
	}
	m.snapshot, err = openPath(c.anchor, filepath.Join(c.snapshots, snapshot))
	if err != nil {
		return nil, ErrUnavailable
	}
	if st, e := fstat(m.snapshot); e != nil || st.Mode&07777 != 0700 {
		return nil, ErrUnavailable
	}
	m.snapshotID = rootIdentity(m.snapshot)
	m.data, err = openAt(m.root, "data", true)
	if err != nil {
		return nil, ErrUnavailable
	}
	if err = m.revalidate(); err != nil {
		return nil, err
	}
	okay = true
	return m, nil
}
func (m *verifiedMaterial) revalidate() error {
	c, r := m.c, m.record
	if !sameRootPath(c, m.path, m.root, m.rootID) || !sameRootPath(c, filepath.Join(c.snapshots, r.Snapshot), m.snapshot, m.snapshotID) || !sameRootPath(c, c.transaction, m.transaction, m.transactionID) || !samePath(m.root, "data", m.data) || verifyLock(m.transaction, c.fd) != nil {
		return fmt.Errorf("material bound evidence: %w", ErrOwnerChanged)
	}
	p, err := readMaterialTransaction(m.transaction, r.Snapshot)
	if err != nil || !reflect.DeepEqual(p, m.proof) {
		return fmt.Errorf("material bound evidence: %w", ErrOwnerChanged)
	}
	raw, err := readPrivateFile(m.root, "material.json", materialLimit)
	if err != nil || !bytes.Equal(raw, m.raw) {
		return fmt.Errorf("material bound evidence: %w", ErrOwnerChanged)
	}
	if _, err = m.root.Seek(0, 0); err != nil {
		return fmt.Errorf("material structural proof: %w", ErrUnavailable)
	}
	names, err := m.root.Readdirnames(4)
	if err != nil || len(names) != 2 {
		return fmt.Errorf("material structural proof: %w", ErrUnavailable)
	}
	sort.Strings(names)
	if names[0] != "data" || names[1] != "material.json" {
		return fmt.Errorf("material structural proof: %w", ErrUnavailable)
	}
	rows, err := manifest(m.snapshot, r.SnapshotManifest)
	if err != nil {
		return err
	}
	for name, want := range map[string]string{"snapshot.version": "6\n", "target-release.commit": r.CandidateCommit + "\n", "target-release.tree": r.CandidateTree + "\n"} {
		got, e := manifestValue(m.snapshot, rows, name)
		if e != nil || got != want {
			return fmt.Errorf("material bound evidence: %w", ErrOwnerChanged)
		}
	}
	for i, name := range []string{"bin", "web"} {
		resource := r.Resources[i]
		if resource.Resource != name || !validRecordedTree(resource.New) {
			return fmt.Errorf("material recorded tree: %w", ErrUnavailable)
		}
		old, e := openAt(m.snapshot, name, true)
		if e != nil {
			return fmt.Errorf("material bound evidence: %w", ErrOwnerChanged)
		}
		fresh, e := scan(old)
		bound := samePath(m.snapshot, name, old)
		old.Close()
		if e != nil || !bound || !fresh.equal(resource.Old) || checkManifestTree(fresh, rows, name) != nil {
			return fmt.Errorf("material bound evidence: %w", ErrOwnerChanged)
		}
		required := []string{"agent", "panel"}
		if name == "web" {
			required = []string{"index.html"}
		}
		for _, file := range required {
			if resource.Old.entryMap()[file].SHA == "" || resource.New.entryMap()[file].SHA == "" {
				return ErrUnavailable
			}
		}
		expected := (&inputs{request: Request{Resource: name}, old: resource.Old, new: resource.New}).expectedCandidate()
		if !expected.equal(resource.Target) {
			return fmt.Errorf("material target tree: %w", ErrUnavailable)
		}
	}
	data, err := scan(m.data)
	if err != nil || !data.equal(r.Data) {
		return fmt.Errorf("material bound evidence: %w", ErrOwnerChanged)
	}
	dataRows, err := manifest(m.data, r.DataManifest)
	if err != nil || validateMaterialData(data, dataRows, r) != nil {
		return fmt.Errorf("material data inventory: %w", ErrUnavailable)
	}
	// The full data tree proof above includes SHA256SUMS and metadata files.
	for name, want := range map[string]string{"candidate-root": r.CandidateRoot + "\n", "candidate-manifest-sha256": r.CandidateManifest + "\n", "release.version": "1\n", "release.commit": r.CandidateCommit + "\n", "release.tree": r.CandidateTree + "\n"} {
		got, e := manifestValue(m.data, dataRows, name)
		if e != nil || got != want {
			return fmt.Errorf("material bound evidence: %w", ErrOwnerChanged)
		}
	}
	return nil
}
func validRecordedTree(t tree) bool {
	if len(t.Entries) == 0 || len(t.Entries) > maxEntries {
		return false
	}
	previous := ""
	directories := map[string]bool{".": true}
	var size int64
	for i, e := range t.Entries {
		if e.Path <= previous || (i == 0 && e.Path != ".") || (i > 0 && !validRelative(e.Path)) {
			return false
		}
		previous = e.Path
		st := e.Identity
		if st.UID != 0 || st.GID != 0 || st.Mode&07022 != 0 {
			return false
		}
		kind := st.Mode & unix.S_IFMT
		if kind == unix.S_IFDIR {
			if e.SHA != "" {
				return false
			}
			directories[e.Path] = true
		} else if kind == unix.S_IFREG {
			if !ValidManifest(e.SHA) || st.Links != 1 || st.Size < 0 || st.Size > maxFile {
				return false
			}
			size += st.Size
		} else {
			return false
		}
		if i > 0 && !directories[filepath.Dir(e.Path)] {
			return false
		}
		if size > maxTree {
			return false
		}
		if len(e.Attributes) > 32 {
			return false
		}
		previousAttr := ""
		attrSize := 0
		for _, a := range e.Attributes {
			if !strings.HasPrefix(a.Name, "user.") || a.Name <= previousAttr || len(a.Name) > 255 || len(a.Value) > 65536 || a.SHA != digest(a.Value) {
				return false
			}
			previousAttr = a.Name
			attrSize += len(a.Value)
		}
		if attrSize > 128<<10 {
			return false
		}
	}
	return t.Entries[0].Identity.Mode&unix.S_IFMT == unix.S_IFDIR
}
func validateMaterialData(t tree, rows map[string]string, r materialRecord) error {
	files := map[string]bool{"candidate-root": true, "candidate-manifest-sha256": true}
	for _, name := range materialFiles {
		files[name] = true
	}
	if len(rows) != len(files) {
		return ErrUnavailable
	}
	for _, e := range t.Entries {
		if len(e.Attributes) != 0 {
			return ErrUnavailable
		}
		if e.Identity.Mode&unix.S_IFMT == unix.S_IFDIR {
			if (e.Path != "." && e.Path != "deploy" && e.Path != "deploy/systemd") || e.Identity.Mode&07777 != 0700 {
				return ErrUnavailable
			}
			continue
		}
		mode := uint32(0600)
		if strings.HasPrefix(e.Path, "deploy/systemd/") {
			mode = 0644
		}
		if e.Identity.Mode&07777 != mode {
			return ErrUnavailable
		}
		if e.Path == "SHA256SUMS" {
			if e.SHA != r.DataManifest {
				return ErrUnavailable
			}
			continue
		}
		if !files[e.Path] || rows[e.Path] != e.SHA {
			return ErrUnavailable
		}
		delete(files, e.Path)
	}
	if len(files) != 0 {
		return ErrUnavailable
	}
	return nil
}

func load(r Request, op string, c config) (*inputs, error) {
	m, err := readMaterial(r.Snapshot, c)
	if errors.Is(err, ErrMaterialAbsent) {
		return loadCandidate(r, op, c)
	}
	if err != nil {
		return nil, err
	}
	if r.SnapshotManifest != m.record.SnapshotManifest || r.CandidateRoot != m.record.CandidateRoot || r.CandidateManifest != m.record.CandidateManifest || !ValidResource(r.Resource) {
		m.close()
		return nil, ErrUnavailable
	}
	index := 0
	if r.Resource == "web" {
		index = 1
	}
	resource := m.record.Resources[index]
	if op == "update" {
		v, e := loadCandidate(r, op, c)
		if e != nil {
			m.close()
			return nil, e
		}
		v.material = m
		if !v.old.equal(resource.Old) || !v.new.equal(resource.New) || !v.expectedCandidate().equal(resource.Target) {
			v.close()
			return nil, ErrOwnerChanged
		}
		if e = v.revalidate(); e != nil {
			v.close()
			return nil, e
		}
		return v, nil
	}
	if op != "rollback" {
		m.close()
		return nil, ErrUnavailable
	}
	v := &inputs{c: c, request: r, operation: op, material: m, old: resource.Old, new: resource.New}
	okay := false
	defer func() {
		if !okay {
			v.close()
		}
	}()
	if v.prefix, err = openPath(c.anchor, c.prefix); err != nil {
		return nil, ErrUnavailable
	}
	if v.transaction, err = openPath(c.anchor, c.transaction); err != nil {
		return nil, ErrUnavailable
	}
	if v.marker, v.token, err = readMarker(v.transaction, op, r.Snapshot); err != nil {
		return nil, err
	}
	if v.snapshot, err = openPath(c.anchor, filepath.Join(c.snapshots, r.Snapshot)); err != nil {
		return nil, ErrUnavailable
	}
	v.prefixID = rootIdentity(v.prefix)
	v.transactionID = rootIdentity(v.transaction)
	v.snapshotID = rootIdentity(v.snapshot)
	if v.oldRoot, err = openAt(v.snapshot, r.Resource, true); err != nil {
		return nil, ErrUnavailable
	}
	if v.snapshotRows, err = manifest(v.snapshot, r.SnapshotManifest); err != nil {
		return nil, err
	}
	if err = v.revalidate(); err != nil {
		return nil, err
	}
	okay = true
	return v, nil
}
func (v *inputs) requireOldWithoutForward() error {
	if v.material == nil {
		return nil
	}
	current, err := openAt(v.prefix, v.request.Resource, true)
	if err != nil {
		return ErrOwnerChanged
	}
	defer current.Close()
	fresh, err := scan(current)
	if err != nil || fresh.semantic() != v.old.semantic() || !samePath(v.prefix, v.request.Resource, current) {
		return ErrOwnerChanged
	}
	return nil
}

// PrepareRecoveryMaterial seals non-executable recovery data before either
// product resource changes. An already applied release cannot be retrospectively
// granted publication authority by creating this evidence.
func PrepareRecoveryMaterial(r Request) error { return prepareRecoveryMaterial(r, production()) }
func prepareRecoveryMaterial(r Request, c config) error {
	r.Resource = "bin"
	bin, err := loadCandidate(r, "update", c)
	if err != nil {
		return err
	}
	defer bin.close()
	r.Resource = "web"
	web, err := loadCandidate(r, "update", c)
	if err != nil {
		return err
	}
	defer web.close()
	if bin.token != web.token || !bytes.Equal(bin.marker, web.marker) {
		return ErrUnavailable
	}
	before := map[string]tree{}
	for _, v := range []*inputs{bin, web} {
		current, e := openAt(v.prefix, v.request.Resource, true)
		if e != nil {
			return ErrUnavailable
		}
		t, e := scan(current)
		bound := samePath(v.prefix, v.request.Resource, current)
		current.Close()
		if e != nil || !bound || t.semantic() != v.old.semantic() {
			return ErrOwnerChanged
		}
		before[v.request.Resource] = t
	}
	if err = noMaterialIntents(bin); err != nil {
		return err
	}
	old, err := readMaterial(r.Snapshot, c)
	if err == nil {
		defer old.close()
		if old.record.SnapshotManifest != r.SnapshotManifest || old.record.CandidateRoot != r.CandidateRoot || old.record.CandidateManifest != r.CandidateManifest || !old.record.Resources[0].Old.equal(bin.old) || !old.record.Resources[0].New.equal(bin.new) || !old.record.Resources[1].Old.equal(web.old) || !old.record.Resources[1].New.equal(web.new) {
			return ErrOwnerChanged
		}
		return revalidatePreparation(bin, web, before)
	}
	if !errors.Is(err, ErrMaterialAbsent) {
		return err
	}
	commit, e := manifestValue(bin.candidate, bin.candidateRows, "release.commit")
	if e != nil {
		return e
	}
	candidateTree, e := manifestValue(bin.candidate, bin.candidateRows, "release.tree")
	if e != nil || !regexp.MustCompile(`^[0-9a-f]{40}\n$`).MatchString(candidateTree) {
		return ErrUnavailable
	}
	snapshotTree, e := manifestValue(bin.snapshot, bin.snapshotRows, "target-release.tree")
	if e != nil || snapshotTree != candidateTree {
		return ErrUnavailable
	}
	base, err := createMaterialBase(c)
	if err != nil {
		return err
	}
	defer base.Close()
	baseID := rootIdentity(base)
	stage, err := randomName(".material-")
	if err != nil {
		return err
	}
	if unix.Mkdirat(int(base.Fd()), stage, 0700) != nil || unix.Fsync(int(base.Fd())) != nil {
		return ErrUnavailable
	}
	stageRoot, err := openAt(base, stage, true)
	if err != nil {
		return ErrUnavailable
	}
	defer stageRoot.Close()
	data, err := privateDir(stageRoot, "data")
	if err != nil {
		return err
	}
	defer data.Close()
	deploy, err := privateDir(data, "deploy")
	if err != nil {
		return err
	}
	systemd, err := privateDir(deploy, "systemd")
	deploy.Close()
	if err != nil {
		return err
	}
	systemd.Close()
	checks := map[string]string{}
	for _, name := range materialFiles {
		raw, e := readRelative(bin.candidate, name, maxFile)
		if e != nil || bin.candidateRows[name] == "" || digest(raw) != bin.candidateRows[name] {
			return ErrOwnerChanged
		}
		if e = writeMaterialFile(data, name, raw); e != nil {
			return e
		}
		checks[name] = digest(raw)
		c.point("material_file_written")
	}
	for name, raw := range map[string][]byte{"candidate-root": []byte(r.CandidateRoot + "\n"), "candidate-manifest-sha256": []byte(r.CandidateManifest + "\n")} {
		if err = writeMaterialFile(data, name, raw); err != nil {
			return err
		}
		checks[name] = digest(raw)
	}
	names := make([]string, 0, len(checks))
	for name := range checks {
		names = append(names, name)
	}
	sort.Strings(names)
	var sums strings.Builder
	for _, name := range names {
		fmt.Fprintf(&sums, "%s  ./%s\n", checks[name], name)
	}
	if err = writeNew(data, "SHA256SUMS", []byte(sums.String())); err != nil {
		return err
	}
	copied, err := scan(data)
	if err != nil {
		return err
	}
	record := materialRecord{Schema: MaterialSchema, Snapshot: r.Snapshot, SnapshotManifest: r.SnapshotManifest, CandidateRoot: r.CandidateRoot, CandidateManifest: r.CandidateManifest, TokenHash: digest([]byte(bin.token)), CandidateCommit: strings.TrimSuffix(commit, "\n"), CandidateTree: strings.TrimSuffix(candidateTree, "\n"), Resources: []materialResource{{"bin", bin.old, bin.new, bin.expectedCandidate()}, {"web", web.old, web.new, web.expectedCandidate()}}, Data: copied, DataManifest: digest([]byte(sums.String()))}
	if err = validateMaterialData(copied, checks, record); err != nil {
		return err
	}
	raw := canonical(record)
	if int64(len(raw)) > materialLimit {
		return ErrUnavailable
	}
	if err = writeNew(stageRoot, "material.json", raw); err != nil {
		return err
	}
	c.point("material_ready")
	if err = revalidatePreparation(bin, web, before); err != nil {
		return err
	}
	for _, name := range materialFiles {
		value, e := readRelative(bin.candidate, name, maxFile)
		if e != nil || digest(value) != checks[name] {
			return ErrOwnerChanged
		}
	}
	after, err := scan(data)
	if err != nil || !after.equal(copied) || !samePath(stageRoot, "data", data) || !samePath(base, stage, stageRoot) || !sameRootPath(c, materialBase(c), base, baseID) {
		return ErrOwnerChanged
	}
	if unix.Fsync(int(data.Fd())) != nil || unix.Fsync(int(stageRoot.Fd())) != nil {
		return ErrUnavailable
	}
	if unix.Renameat2(int(base.Fd()), stage, int(base.Fd()), record.TokenHash, unix.RENAME_NOREPLACE) != nil {
		return ErrUnavailable
	}
	c.point("material_published")
	if unix.Fsync(int(base.Fd())) != nil {
		return ErrUnavailable
	}
	c.point("material_durable")
	verified, err := readMaterial(r.Snapshot, c)
	if err != nil {
		return err
	}
	defer verified.close()
	if verified.sha != digest(raw) {
		return ErrOwnerChanged
	}
	return revalidatePreparation(bin, web, before)
}
func writeMaterialFile(data *os.File, name string, raw []byte) error {
	parent, base, close, err := relativeParent(data, name)
	if err != nil {
		return err
	}
	defer close()
	if err = writeNew(parent, base, raw); err != nil {
		return err
	}
	if strings.HasPrefix(name, "deploy/systemd/") {
		f, e := openAt(parent, base, false)
		if e != nil {
			return ErrUnavailable
		}
		defer f.Close()
		if unix.Fchmod(int(f.Fd()), 0644) != nil || unix.Fsync(int(f.Fd())) != nil {
			return ErrUnavailable
		}
	}
	return nil
}
func createMaterialBase(c config) (*os.File, error) {
	path := materialBase(c)
	rel, err := filepath.Rel(c.anchor, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
		return nil, ErrUnavailable
	}
	f, err := openPath(c.anchor, c.anchor)
	if err != nil {
		return nil, err
	}
	for _, part := range strings.Split(rel, "/") {
		next, e := openAt(f, part, true)
		if errors.Is(e, unix.ENOENT) {
			next, e = privateDir(f, part)
		}
		f.Close()
		if e != nil {
			return nil, ErrUnavailable
		}
		f = next
	}
	st, err := fstat(f)
	if err != nil || st.Mode&07777 != 0700 {
		f.Close()
		return nil, ErrUnavailable
	}
	return f, nil
}
func noMaterialIntents(v *inputs) error {
	root, err := optionalPath(v.c, filepath.Join(v.c.prefix, journalName, digest([]byte(v.token))))
	if errors.Is(err, ErrMaterialAbsent) {
		return nil
	}
	if err != nil {
		return err
	}
	defer root.Close()
	names, err := root.Readdirnames(5)
	if (err != nil && err != io.EOF) || len(names) > 4 {
		return ErrUnavailable
	}
	for _, name := range names {
		if name != "update-bin" && name != "update-web" && name != "rollback-bin" && name != "rollback-web" {
			return ErrUnavailable
		}
		journal, e := openAt(root, name, true)
		if e != nil {
			return ErrUnavailable
		}
		e = validateJournalObjects(journal)
		for _, receipt := range []string{"intent.json", "published"} {
			var st unix.Stat_t
			if !errors.Is(unix.Fstatat(int(journal.Fd()), receipt, &st, unix.AT_SYMLINK_NOFOLLOW), unix.ENOENT) {
				e = ErrOwnerChanged
			}
		}
		journal.Close()
		if e != nil {
			return e
		}
	}
	return nil
}
func revalidatePreparation(bin, web *inputs, before map[string]tree) error {
	for _, v := range []*inputs{bin, web} {
		if err := v.revalidate(); err != nil {
			return err
		}
		current, err := openAt(v.prefix, v.request.Resource, true)
		if err != nil {
			return ErrOwnerChanged
		}
		now, err := scan(current)
		bound := samePath(v.prefix, v.request.Resource, current)
		current.Close()
		if err != nil || !bound || !now.equal(before[v.request.Resource]) {
			return ErrOwnerChanged
		}
	}
	return noMaterialIntents(bin)
}

// Any v2 authority belongs to the material, including a different resource's
// completed publication. Removing material cannot downgrade that transaction.
func refuseMissingMaterialIntent(c config, token string) error {
	root, err := optionalPath(c, filepath.Join(c.prefix, journalName, digest([]byte(token))))
	if errors.Is(err, ErrMaterialAbsent) {
		return nil
	}
	if err != nil {
		return err
	}
	defer root.Close()
	names, err := root.Readdirnames(5)
	if (err != nil && err != io.EOF) || len(names) > 4 {
		return ErrUnavailable
	}
	for _, name := range names {
		if name != "update-bin" && name != "update-web" && name != "rollback-bin" && name != "rollback-web" {
			return ErrUnavailable
		}
		journal, e := openAt(root, name, true)
		if e != nil {
			return ErrUnavailable
		}
		if e = validateJournalObjects(journal); e != nil {
			journal.Close()
			return e
		}
		raw, e := readPrivateFile(journal, "intent.json", maxIntent)
		journal.Close()
		if errors.Is(e, unix.ENOENT) {
			continue
		}
		var record intent
		if e != nil || decodeExact(raw, &record) != nil || record.Schema != Schema || record.MaterialSHA != "" {
			return ErrUnavailable
		}
	}
	return nil
}

// A valid marker with a different token must not hide an already sealed
// material for this snapshot. This bounded check examines only durable material
// headers. Known private capture stages are preserved, never adopted; a fresh
// Prepare call must prove all pre-mutation inputs again before creating a stage.
func refuseOtherMaterialToken(c config, snapshot, token string) error {
	base, err := optionalPath(c, materialBase(c))
	if errors.Is(err, ErrMaterialAbsent) {
		return nil
	}
	if err != nil {
		return err
	}
	defer base.Close()
	if st, e := fstat(base); e != nil || st.Mode&07777 != 0700 {
		return ErrUnavailable
	}
	baseID := rootIdentity(base)
	names, err := base.Readdirnames(1025)
	if (err != nil && err != io.EOF) || len(names) > 1024 {
		return ErrUnavailable
	}
	sort.Strings(names)
	var bytesRead int64
	stagePattern := regexp.MustCompile(`^\.material-[0-9a-f]{32}$`)
	for _, name := range names {
		if !hex64.MatchString(name) && !stagePattern.MatchString(name) {
			return ErrUnavailable
		}
		dir, e := openAt(base, name, true)
		if e != nil {
			return ErrUnavailable
		}
		st, e := fstat(dir)
		if e != nil || st.Mode&07777 != 0700 {
			dir.Close()
			return ErrUnavailable
		}
		if stagePattern.MatchString(name) {
			dir.Close()
			continue
		}
		raw, e := readPrivateFile(dir, "material.json", materialLimit)
		bytesRead += int64(len(raw))
		if e != nil || bytesRead > 256<<20 {
			dir.Close()
			return ErrUnavailable
		}
		var r materialRecord
		if decodeExact(raw, &r) != nil || r.Schema != MaterialSchema || !ValidSnapshot(r.Snapshot) || r.TokenHash != name || !ValidManifest(r.SnapshotManifest) || !ValidManifest(r.CandidateManifest) {
			dir.Close()
			return ErrUnavailable
		}
		children, e := dir.Readdirnames(4)
		if e != nil || len(children) != 2 {
			dir.Close()
			return ErrUnavailable
		}
		sort.Strings(children)
		if children[0] != "data" || children[1] != "material.json" {
			dir.Close()
			return ErrUnavailable
		}
		data, e := openAt(dir, "data", true)
		if e != nil {
			dir.Close()
			return ErrUnavailable
		}
		st, e = fstat(data)
		data.Close()
		bound := samePath(base, name, dir)
		dir.Close()
		if e != nil || st.Mode&07777 != 0700 || !bound {
			return ErrUnavailable
		}
		if r.Snapshot == snapshot || name == digest([]byte(token)) {
			return ErrOwnerChanged
		}
	}
	if !sameRootPath(c, materialBase(c), base, baseID) {
		return ErrOwnerChanged
	}
	return nil
}
