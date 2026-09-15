// Package recoverypublication publishes only transaction-bound product trees.
// It does not admit snapshots, launch updates, delete retired trees, or restore
// application databases. The caller must first verify the complete v6 snapshot
// and either the retained candidate or sealed recovery-material contract. This
// package rechecks the exact manifests and relevant payloads. Publication also
// requires the native transaction lock and stopped coordinators; read-only
// material verification does not stop or require stopped services.
package recoverypublication

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
)

type Request struct {
	Resource          string
	Snapshot          string
	SnapshotManifest  string
	CandidateRoot     string
	CandidateManifest string
}

// ErrMaterialAbsent means no committed material authority exists for this
// transaction. Unsafe, malformed, or missing material with a surviving v2
// intent returns a different error and must never select legacy fallback.
var ErrMaterialAbsent = errors.New("recovery material is absent")

// ErrLegacyCompletionMaterial is returned only after verifying a v1 material
// and its completed product publications. Its data cannot replace the retained
// candidate during forward completion because it lacks the reviewed updater.
var ErrLegacyCompletionMaterial = errors.New("verified legacy material requires retained candidate completion data")

const MaterialSchema = "celikpanel/recovery-material/v1"
const MaterialSchemaV2 = "celikpanel/recovery-material/v2"
const MaterialSchemaV3 = "celikpanel/recovery-material/v3"
const DatabaseAdmissionSchema = "celikpanel/database-migration-admission/v1"

// ErrLegacyDatabaseMaterial identifies a verified older material/transition.
// Malformed v3 evidence never returns this compatibility result.
var ErrLegacyDatabaseMaterial = errors.New("verified legacy database recovery policy")

func modernMaterial(schema string) bool {
	return schema == MaterialSchemaV2 || schema == MaterialSchemaV3
}

const MaterialIntentSchema = "celikpanel/recovery-resource-intent/v2"
const MaterialNoopIntentSchema = "celikpanel/recovery-resource-noop-intent/v1"

var ErrUnavailable = errors.New("recovery publication unavailable; preserve resource evidence")
var ErrUnsupportedMetadata = errors.New("program metadata is not supported by this recovery protocol (ACL, capabilities or SELinux labels); preserve metadata and operation evidence")
var ErrOwnerChanged = errors.New("recovery resource differs from the accepted publication; preserve owner changes")
var hex64 = regexp.MustCompile(`^[0-9a-f]{64}$`)
var snapshotPattern = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z-from-(unknown|[0-9a-f]{40})-to-([0-9a-f]{40})-[0-9a-f]{32}$`)
var releasePattern = regexp.MustCompile(`^[0-9a-f]{12}-[0-9a-f]{24}$`)
var stagePattern = regexp.MustCompile(`^\.stage-[0-9a-f]{32}$`)
var temporaryPattern = regexp.MustCompile(`^\.(intent|published)-[0-9a-f]{32}$`)

const Schema = "celikpanel/recovery-resource-intent/v1"
const maxEntries = 16384
const maxFile = int64(256 << 20)
const maxTree = int64(1 << 30)
const maxManifest = int64(16 << 20)
const maxIntent = int64(16 << 20)

func digest(raw []byte) string    { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
func ValidResource(s string) bool { return s == "bin" || s == "web" }
func ValidSnapshot(s string) bool { return snapshotPattern.MatchString(s) }
func ValidManifest(s string) bool { return hex64.MatchString(s) }
