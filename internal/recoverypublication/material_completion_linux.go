//go:build linux

package recoverypublication

import (
	"bytes"
	"errors"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"path/filepath"
)

func (p materialTransaction) updateCompletion() bool {
	_, active := p.markers["active"]
	return p.operation == "update" && !active && len(p.markers) > 0
}

// VerifyCompletionMaterial returns only previously sealed v2 data for an exact
// late update. A verified v1 contract has a separate compatibility result; it
// must not be confused with absent or corrupt material. No service is stopped,
// candidate path opened, product executed, or evidence created by this reader.
func VerifyCompletionMaterial(snapshot string) (string, error) {
	return verifyCompletionMaterial(snapshot, production())
}
func verifyCompletionMaterial(snapshot string, c config) (string, error) {
	m, err := readMaterialFor(snapshot, c, true)
	if err != nil {
		return "", err
	}
	defer m.close()
	if err = m.verifyInstalledCompletion(); err != nil {
		return "", err
	}
	if m.record.Schema == MaterialSchema {
		return "", ErrLegacyCompletionMaterial
	}
	return filepath.Join(m.path, "data"), nil
}

// VerifyInstalledCompletion proves exact installed bin/web publication. The
// caller additionally validates the complete DB/TLS snapshot, native foundation,
// updater data, service state and final runtime before clearing any marker.
func VerifyInstalledCompletion(snapshot string) error {
	return verifyInstalledCompletion(snapshot, production())
}
func verifyInstalledCompletion(snapshot string, c config) error {
	_, err := verifyCompletionMaterial(snapshot, c)
	return err
}

type completedResourceProof struct {
	m                                           *verifiedMaterial
	prefix, journals, journal, current, retired *os.File
	prefixID, journalsID, journalID             identity
	record                                      intent
	intentRaw, receiptRaw                       []byte
	legacyObserved                              *tree
}

func (p *completedResourceProof) close() {
	for _, f := range []*os.File{p.retired, p.current, p.journal, p.journals, p.prefix} {
		if f != nil {
			f.Close()
		}
	}
}
func validateCompletionJournals(root *os.File) error {
	if _, err := root.Seek(0, 0); err != nil {
		return ErrUnavailable
	}
	names, err := root.Readdirnames(3)
	if (err != nil && err != io.EOF) || len(names) != 2 {
		return ErrUnavailable
	}
	seen := map[string]bool{}
	for _, name := range names {
		if name != "update-bin" && name != "update-web" {
			return ErrUnavailable
		}
		seen[name] = true
	}
	if !seen["update-bin"] || !seen["update-web"] {
		return ErrUnavailable
	}
	return nil
}
func (p *completedResourceProof) revalidate() error {
	c := p.m.c
	tokenPath := filepath.Join(c.prefix, journalName, p.m.record.TokenHash)
	name := "update-" + p.record.Resource
	if !sameRootPath(c, c.prefix, p.prefix, p.prefixID) || !sameRootPath(c, tokenPath, p.journals, p.journalsID) ||
		!samePath(p.journals, name, p.journal) || rootIdentity(p.journal) != p.journalID ||
		!samePath(p.prefix, p.record.Resource, p.current) {
		return ErrOwnerChanged
	}
	if validateCompletionJournals(p.journals) != nil || validateJournalObjects(p.journal) != nil {
		return ErrOwnerChanged
	}
	raw, err := readPrivateFile(p.journal, "intent.json", maxIntent)
	if p.legacyObserved != nil {
		// Historical v1 no-op writers did not create publication authority. This
		// proves only eligibility for the original candidate-based completion path.
		if !errors.Is(err, unix.ENOENT) {
			return ErrOwnerChanged
		}
		if _, e := readPrivateFile(p.journal, "published", 4096); !errors.Is(e, unix.ENOENT) {
			return ErrOwnerChanged
		}
		actual, e := scan(p.current)
		if e != nil || !actual.equal(*p.legacyObserved) {
			return ErrOwnerChanged
		}
		return nil
	}
	if err != nil || !bytes.Equal(raw, p.intentRaw) {
		return ErrOwnerChanged
	}
	receipt, err := readPrivateFile(p.journal, "published", 4096)
	if err != nil || !bytes.Equal(receipt, p.receiptRaw) {
		return ErrOwnerChanged
	}
	current, err := scan(p.current)
	if err != nil || !current.equal(p.record.After) {
		return ErrOwnerChanged
	}
	if p.record.Schema == MaterialNoopIntentSchema {
		return nil
	}
	if !samePath(p.journal, p.record.Stage, p.retired) {
		return ErrOwnerChanged
	}
	retired, err := scan(p.retired)
	if err != nil || !retired.equal(p.record.Before) {
		return ErrOwnerChanged
	}
	return nil
}

func (m *verifiedMaterial) completedResource(resource materialResource) (*completedResourceProof, error) {
	p := &completedResourceProof{m: m}
	okay := false
	defer func() {
		if !okay {
			p.close()
		}
	}()
	var err error
	c := m.c
	p.prefix, err = openPath(c.anchor, c.prefix)
	if err != nil {
		return nil, ErrUnavailable
	}
	p.prefixID = rootIdentity(p.prefix)
	tokenPath := filepath.Join(c.prefix, journalName, m.record.TokenHash)
	p.journals, err = openPath(c.anchor, tokenPath)
	if err != nil {
		return nil, ErrUnavailable
	}
	p.journalsID = rootIdentity(p.journals)
	if p.journalsID.Mode&07777 != 0700 {
		return nil, ErrUnavailable
	}
	// A completed update has exactly its two forward journals, never rollback
	// authority or an unknown sibling. Unpublished private files within either
	// journal remain governed by the existing journal grammar.
	if err = validateCompletionJournals(p.journals); err != nil {
		return nil, err
	}
	p.journal, err = openAt(p.journals, "update-"+resource.Resource, true)
	if err != nil {
		return nil, ErrUnavailable
	}
	p.journalID = rootIdentity(p.journal)
	if p.journalID.Mode&07777 != 0700 || validateJournalObjects(p.journal) != nil {
		return nil, ErrUnavailable
	}
	p.intentRaw, err = readPrivateFile(p.journal, "intent.json", maxIntent)
	if errors.Is(err, unix.ENOENT) && m.record.Schema == MaterialSchema && resource.Old.semantic() == resource.Target.semantic() {
		p.record.Resource = resource.Resource
		p.current, err = openAt(p.prefix, resource.Resource, true)
		if err != nil {
			return nil, ErrOwnerChanged
		}
		observed, e := scan(p.current)
		if e != nil || observed.semantic() != resource.Target.semantic() {
			return nil, ErrOwnerChanged
		}
		p.legacyObserved = &observed
		if err = p.revalidate(); err != nil {
			return nil, err
		}
		okay = true
		return p, nil
	}
	if err != nil || decodeExact(p.intentRaw, &p.record) != nil {
		return nil, ErrUnavailable
	}
	v := &inputs{request: Request{Resource: resource.Resource, Snapshot: m.record.Snapshot, SnapshotManifest: m.record.SnapshotManifest, CandidateRoot: m.record.CandidateRoot, CandidateManifest: m.record.CandidateManifest}, operation: "update", token: m.proof.token, material: m, prefixID: p.prefixID, old: resource.Old, new: resource.New}
	if !v.validIntent(p.record, "update", resource.Target) {
		return nil, ErrOwnerChanged
	}
	p.receiptRaw, err = readPrivateFile(p.journal, "published", 4096)
	if err != nil || !bytes.Equal(p.receiptRaw, []byte("format=celikpanel-resource-publication-v1\nintent="+digest(p.intentRaw)+"\n")) {
		return nil, ErrOwnerChanged
	}
	p.current, err = openAt(p.prefix, resource.Resource, true)
	if err != nil {
		return nil, ErrOwnerChanged
	}
	if p.record.Schema != MaterialNoopIntentSchema {
		p.retired, err = openAt(p.journal, p.record.Stage, true)
		if err != nil {
			return nil, ErrOwnerChanged
		}
	}
	if err = p.revalidate(); err != nil {
		return nil, err
	}
	okay = true
	return p, nil
}

func (m *verifiedMaterial) verifyInstalledCompletion() error {
	if !m.proof.updateCompletion() {
		return ErrUnavailable
	}
	proofs := []*completedResourceProof{}
	defer func() {
		for _, p := range proofs {
			p.close()
		}
	}()
	for _, resource := range m.record.Resources {
		p, err := m.completedResource(resource)
		if err != nil {
			return err
		}
		proofs = append(proofs, p)
	}
	m.c.point("completion_resources_read")
	// Keep both trees pinned until the last read so a change to the first resource
	// while the second is being inspected cannot be accepted as current evidence.
	if err := m.revalidate(); err != nil {
		return err
	}
	for _, p := range proofs {
		if err := p.revalidate(); err != nil {
			return err
		}
	}
	return m.revalidate()
}
