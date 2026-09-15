package recoverypublication

import "os"

// DatabaseFileIdentity binds an exact filesystem object. Parent identities use
// zero size/link/timestamp fields: creating admission directories legitimately
// changes those values, while inode, ownership and permissions remain pinned.
type DatabaseFileIdentity struct {
	Dev       uint64 `json:"dev"`
	Ino       uint64 `json:"ino"`
	Mode      uint32 `json:"mode"`
	UID       uint32 `json:"uid"`
	GID       uint32 `json:"gid"`
	Links     uint64 `json:"links"`
	Size      int64  `json:"size"`
	MtimeSec  int64  `json:"mtime_sec"`
	MtimeNsec int64  `json:"mtime_nsec"`
	CtimeSec  int64  `json:"ctime_sec"`
	CtimeNsec int64  `json:"ctime_nsec"`
}
type DatabaseAttribute struct {
	Name   string `json:"name"`
	Value  []byte `json:"value"`
	SHA256 string `json:"sha256"`
}

// DatabaseBeforeEvidence is written with v3 material before database admission.
// It authorizes checking an untouched database after a crash before admission;
// it does not authorize replacing a differing database.
type DatabaseBeforeEvidence struct {
	Parent           DatabaseFileIdentity `json:"parent"`
	ParentAttributes []DatabaseAttribute  `json:"parent_attributes,omitempty"`
	File             DatabaseFileIdentity `json:"file"`
	SHA256           string               `json:"sha256"`
	Attributes       []DatabaseAttribute  `json:"attributes,omitempty"`
}
type DatabaseAuthorityIdentity struct {
	Snapshot               string
	TokenSHA256            string
	SnapshotManifestSHA256 string
	MaterialSHA256         string
	CandidatePanelSHA256   string
	SnapshotDatabaseSHA256 string
	Operation              string
	Phase                  string
	DatabaseBefore         DatabaseBeforeEvidence
}
type databaseAuthorityHandle interface {
	Revalidate() error
	SnapshotDatabase() (*os.File, error)
	Close() error
}

// DatabaseAuthority pins existing transaction, material and complete snapshot
// evidence. Callers must revalidate at each durable boundary. It does not assert
// current canonical database equality after a permitted database publication.
// The handle is not safe for concurrent use.
type DatabaseAuthority struct {
	identity DatabaseAuthorityIdentity
	handle   databaseAuthorityHandle
}

func copyDatabaseAttributes(src []DatabaseAttribute) []DatabaseAttribute {
	if src == nil {
		return nil
	}
	out := append([]DatabaseAttribute(nil), src...)
	for i := range out {
		out[i].Value = append([]byte(nil), src[i].Value...)
	}
	return out
}
func (a *DatabaseAuthority) Identity() DatabaseAuthorityIdentity {
	if a == nil {
		return DatabaseAuthorityIdentity{}
	}
	v := a.identity
	v.DatabaseBefore.Attributes = copyDatabaseAttributes(v.DatabaseBefore.Attributes)
	v.DatabaseBefore.ParentAttributes = copyDatabaseAttributes(v.DatabaseBefore.ParentAttributes)
	return v
}
func (a *DatabaseAuthority) Revalidate() error {
	if a == nil || a.handle == nil {
		return ErrUnavailable
	}
	return a.handle.Revalidate()
}

// SnapshotDatabase returns an independently opened read-only descriptor for the
// manifest-bound database. The caller owns it; Revalidate remains required.
func (a *DatabaseAuthority) SnapshotDatabase() (*os.File, error) {
	if a == nil || a.handle == nil {
		return nil, ErrUnavailable
	}
	return a.handle.SnapshotDatabase()
}
func (a *DatabaseAuthority) Close() error {
	if a == nil || a.handle == nil {
		return nil
	}
	h := a.handle
	a.handle = nil
	return h.Close()
}
