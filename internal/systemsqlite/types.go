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
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Purpose     string     `json:"purpose"`
	Kind        string     `json:"kind"`
	Mutable     bool       `json:"mutable"`
	Available   bool       `json:"available"`
	PathHint    string     `json:"path_hint"`
	SizeBytes   int64      `json:"size_bytes,omitempty"`
	ModifiedAt  *time.Time `json:"modified_at,omitempty"`
	JournalMode string     `json:"journal_mode,omitempty"`
	UserVersion int        `json:"user_version"`
	// SchemaVersion is the version THIS PRODUCT means when it says schema
	// version, which is not the same number as UserVersion above.
	//
	// UserVersion is SQLite's own `PRAGMA user_version`: an integer a program
	// may set in the file, and which CelikPanel sets only on the component
	// catalogue. The panel's own database keeps its version in the
	// schema_migrations table instead - it was 38 while the screen, reading
	// the pragma nobody had set, showed 0 under the words "Schema version".
	//
	// So this field carries the number the product actually has, and is nil
	// where there is none. A screen must show nothing rather than a zero: a
	// zero looks like an answer.
	//
	// SchemaVersion, BU URUNUN sema surumu derken kastettigi sayidir ve
	// yukaridaki UserVersion ile ayni sayi degildir. Panelin kendi veritabani
	// surumunu schema_migrations tablosunda tutar; ekran ise kimsenin
	// belirlemedigi pragma'yi okuyup "Sema surumu" yazisinin altinda 0
	// gosteriyordu. Deger yoksa nil kalir: sifir, bir cevap gibi gorunur.
	SchemaVersion *int     `json:"schema_version,omitempty"`
	Status        string   `json:"status"`
	StatusMessage string   `json:"status_message,omitempty"`
	Actions       []string `json:"actions"`
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
