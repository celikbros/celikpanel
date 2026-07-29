package systemsqlite

import "time"

const (
	ProtocolVersion = 1

	DatabasePanel            = "panel"
	DatabasePowerDNS         = "powerdns"
	DatabaseRoundcube        = "roundcube"
	DatabaseComponentCatalog = "component-catalog"

	DefaultChunkSize = 256 * 1024
	MaxChunkSize     = 1024 * 1024
)

type Definition struct {
	ID                string
	Name              string
	Purpose           string
	Kind              string
	Path              string
	PathHint          string
	Mutable           bool
	Optimizable       bool
	SnapshotAllowed   bool
	WriterUID         uint32
	WriterGID         uint32
	WriterIdentitySet bool
}

type ListRequest struct {
	ProtocolVersion int `json:"protocol_version"`
}

type DatabaseRequest struct {
	ProtocolVersion int    `json:"protocol_version"`
	DatabaseID      string `json:"database_id"`
}

type DatabaseInfo struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Purpose       string     `json:"purpose"`
	Kind          string     `json:"kind"`
	Mutable       bool       `json:"mutable"`
	Available     bool       `json:"available"`
	PathHint      string     `json:"path_hint"`
	SizeBytes     int64      `json:"size_bytes,omitempty"`
	ModifiedAt    *time.Time `json:"modified_at,omitempty"`
	JournalMode   string     `json:"journal_mode,omitempty"`
	UserVersion   int        `json:"user_version"`
	Status        string     `json:"status"`
	StatusMessage string     `json:"status_message,omitempty"`
	Actions       []string   `json:"actions"`
}

type ListResponse struct {
	Success   bool           `json:"success"`
	Databases []DatabaseInfo `json:"databases"`
	Error     string         `json:"error,omitempty"`
}

type CheckResult struct {
	DatabaseID               string    `json:"database_id"`
	CheckedAt                time.Time `json:"checked_at"`
	IntegrityOK              bool      `json:"integrity_ok"`
	IntegrityMessage         string    `json:"integrity_message"`
	ForeignKeysOK            bool      `json:"foreign_keys_ok"`
	ForeignKeyViolations     int       `json:"foreign_key_violations"`
	ForeignKeyCheckTruncated bool      `json:"foreign_key_check_truncated,omitempty"`
}

type CheckResponse struct {
	Success bool        `json:"success"`
	Check   CheckResult `json:"check"`
	Error   string      `json:"error,omitempty"`
}

type SnapshotInfo struct {
	DatabaseID string    `json:"database_id"`
	Token      string    `json:"token"`
	SizeBytes  int64     `json:"size_bytes"`
	SHA256     string    `json:"sha256"`
	CreatedAt  time.Time `json:"created_at"`
	ExpiresAt  time.Time `json:"expires_at"`
}

type SnapshotResponse struct {
	Success  bool         `json:"success"`
	Snapshot SnapshotInfo `json:"snapshot"`
	Error    string       `json:"error,omitempty"`
}

type ReadSnapshotChunkRequest struct {
	ProtocolVersion int    `json:"protocol_version"`
	Token           string `json:"token"`
	Offset          int64  `json:"offset"`
	MaxBytes        int    `json:"max_bytes"`
}

type ReadSnapshotChunkResponse struct {
	Success    bool   `json:"success"`
	DatabaseID string `json:"database_id"`
	Data       []byte `json:"data,omitempty"`
	NextOffset int64  `json:"next_offset"`
	SizeBytes  int64  `json:"size_bytes"`
	EOF        bool   `json:"eof"`
	Error      string `json:"error,omitempty"`
}

type ReleaseSnapshotRequest struct {
	ProtocolVersion int    `json:"protocol_version"`
	Token           string `json:"token"`
}

type ReleaseSnapshotResponse struct {
	Success  bool   `json:"success"`
	Released bool   `json:"released"`
	Error    string `json:"error,omitempty"`
}

type OptimizeResult struct {
	DatabaseID  string    `json:"database_id"`
	OptimizedAt time.Time `json:"optimized_at"`
}

type OptimizeResponse struct {
	Success bool           `json:"success"`
	Result  OptimizeResult `json:"result"`
	Error   string         `json:"error,omitempty"`
}
