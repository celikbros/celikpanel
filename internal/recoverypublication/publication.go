// Package recoverypublication publishes only transaction-bound product trees.
// It does not admit snapshots, launch updates, delete retired trees, or restore
// application databases. The caller must first verify the complete v6 snapshot
// and retained candidate contracts; this package rechecks their exact manifests,
// relevant payloads, native transaction identity and stopped coordinators.
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
