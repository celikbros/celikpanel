// Package backupspec defines the versioned RPC contract shared by the
// unprivileged panel and the privileged agent.
package backupspec

import "time"

const (
	ProtocolVersion = 2
	MaxChunkBytes   = 1 << 20
	MaxJobKeyBytes  = 128
)

const (
	TypeFiles    = "files"
	TypeDatabase = "database"
	TypeFull     = "full"
)

const (
	OriginManual     = "manual"
	OriginScheduled  = "scheduled"
	OriginPreRestore = "pre_restore"
)

// DatabaseIdentity is resolved from durable tenant metadata by the panel.
// The agent validates it again before invoking a database client.
type DatabaseIdentity struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// RequestScope carries immutable tenant identity. Request structs repeat these
// fields at their top level because net/rpc's gob encoding does not flatten an
// embedded struct for older peers. DomainName is retained only to locate
// backups written by pre-v2 releases; new storage is ID-based.
type RequestScope struct {
	ProtocolVersion int    `json:"protocol_version"`
	SubscriptionID  int    `json:"subscription_id"`
	DomainID        int    `json:"domain_id"`
	DomainName      string `json:"domain_name"`
}

type CreateRequest struct {
	ProtocolVersion int    `json:"protocol_version"`
	SubscriptionID  int    `json:"subscription_id"`
	DomainID        int    `json:"domain_id"`
	DomainName      string `json:"domain_name"`
	Type            string `json:"type"`
	Origin          string `json:"origin"`
	// JobKey makes a logical create operation idempotent across RPC timeouts
	// and retries. Scheduled creates always set it; manual and pre-restore
	// creates leave it empty.
	JobKey    string             `json:"job_key,omitempty"`
	Database  DatabaseIdentity   `json:"database,omitempty"`
	Databases []DatabaseIdentity `json:"databases,omitempty"`

	// DatabaseName and DatabaseType keep the v2 panel wire-compatible with a
	// pre-v2 agent during a stopped-together upgrade or rollback. A v2 agent
	// uses Database instead and never trusts these fields for a v2 request.
	DatabaseName string `json:"database_name,omitempty"`
	DatabaseType string `json:"database_type,omitempty"`

	// SourceDir is sent during the v2 transition so a stopped-together
	// deployment can still be rolled back to the previous agent binary.
	// A v2 agent never trusts or uses it.
	SourceDir string `json:"source_dir,omitempty"`
}

type Info struct {
	// Path is retained for gob compatibility with pre-v2 panel binaries. It is
	// never serialized to the browser and callers must not trust it as input.
	Path       string    `json:"-"`
	Name       string    `json:"name"`
	Size       int64     `json:"size"`
	Type       string    `json:"type"`
	Origin     string    `json:"origin"`
	DatabaseID int       `json:"database_id,omitempty"`
	Legacy     bool      `json:"legacy,omitempty"`
	Restorable bool      `json:"restorable"`
	CreatedAt  time.Time `json:"created_at"`
}

type CreateResponse struct {
	Success bool   `json:"success"`
	Backup  Info   `json:"backup,omitempty"`
	Error   string `json:"error,omitempty"`
}

type ListRequest struct {
	ProtocolVersion int    `json:"protocol_version"`
	SubscriptionID  int    `json:"subscription_id"`
	DomainID        int    `json:"domain_id"`
	DomainName      string `json:"domain_name"`
}

type ListResponse struct {
	Backups []Info `json:"backups"`
}

type InspectRequest struct {
	ProtocolVersion int    `json:"protocol_version"`
	SubscriptionID  int    `json:"subscription_id"`
	DomainID        int    `json:"domain_id"`
	DomainName      string `json:"domain_name"`
	BackupName      string `json:"backup_name"`
}

type InspectResponse struct {
	Success   bool               `json:"success"`
	Backup    Info               `json:"backup,omitempty"`
	Databases []DatabaseIdentity `json:"databases,omitempty"`
	Error     string             `json:"error,omitempty"`
}

type RestoreRequest struct {
	ProtocolVersion int                `json:"protocol_version"`
	SubscriptionID  int                `json:"subscription_id"`
	DomainID        int                `json:"domain_id"`
	DomainName      string             `json:"domain_name"`
	BackupName      string             `json:"backup_name"`
	Database        DatabaseIdentity   `json:"database,omitempty"`
	Databases       []DatabaseIdentity `json:"databases,omitempty"`

	// TargetDir is a rollback compatibility field. A v2 agent derives the
	// document root from SubscriptionID and DomainID and ignores this value.
	TargetDir string `json:"target_dir,omitempty"`
}

type RestoreResponse struct {
	Success      bool   `json:"success"`
	SafetyBackup *Info  `json:"safety_backup,omitempty"`
	Error        string `json:"error,omitempty"`
}

type DeleteRequest struct {
	ProtocolVersion int    `json:"protocol_version"`
	SubscriptionID  int    `json:"subscription_id"`
	DomainID        int    `json:"domain_id"`
	DomainName      string `json:"domain_name"`
	BackupName      string `json:"backup_name"`
}

type ReadChunkRequest struct {
	ProtocolVersion int    `json:"protocol_version"`
	SubscriptionID  int    `json:"subscription_id"`
	DomainID        int    `json:"domain_id"`
	DomainName      string `json:"domain_name"`
	BackupName      string `json:"backup_name"`
	Offset          int64  `json:"offset"`
	MaxBytes        int    `json:"max_bytes"`
}

type ReadChunkResponse struct {
	Data   []byte `json:"data"`
	Offset int64  `json:"offset"`
	Size   int64  `json:"size"`
	EOF    bool   `json:"eof"`
}

// ValidJobKey reports whether key is safe to persist, log and compare as an
// opaque idempotency identifier. It deliberately accepts only a small ASCII
// alphabet so every transport and filesystem-facing diagnostic represents the
// same bytes.
func ValidJobKey(key string) bool {
	if len(key) < 1 || len(key) > MaxJobKeyBytes {
		return false
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' || c == ':' {
			continue
		}
		return false
	}
	return true
}
