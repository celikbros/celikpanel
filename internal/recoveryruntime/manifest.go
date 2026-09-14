// Package recoveryruntime defines and verifies the independent recovery kit.
// Resolving a kit is read-only. It neither admits a release mutation nor proves
// that a snapshot or interrupted operation is compatible with that mutation.
package recoveryruntime

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"sort"
	"strings"
)

const (
	RuntimeRoot      = "/usr/libexec/celikpanel/recovery-runtimes/v1"
	SelectionPath    = "/var/lib/celikpanel-release-state/recovery-runtime.v1"
	ManifestName     = "runtime.manifest"
	ProtocolVersion  = 1
	SnapshotVersion  = 6
	MaxManifestSize  = 4096
	MaxSelectionSize = 160
	MaxBinarySize    = 128 << 20
	MaxScriptSize    = 4 << 20
	MaxRuntimeSize   = 256 << 20
)

const manifestHeader = "format=celikpanel-recovery-runtime-v1\nprotocol=1\nsnapshot=6\n"
const selectionHeader = "format=celikpanel-recovery-selection-v1\nruntime="

var ErrUnavailable = errors.New("recovery runtime is unavailable")
var ErrNotSelected = errors.New("recovery runtime is not selected")

type Reason string

const (
	ReasonNotSelected         Reason = "runtime_not_selected"
	ReasonMissing             Reason = "runtime_missing"
	ReasonUnsafeMetadata      Reason = "runtime_unsafe_metadata"
	ReasonInvalidSelection    Reason = "runtime_invalid_selection"
	ReasonUnsupported         Reason = "runtime_unsupported_protocol"
	ReasonInvalidManifest     Reason = "runtime_invalid_manifest"
	ReasonDigestMismatch      Reason = "runtime_digest_mismatch"
	ReasonInventoryMismatch   Reason = "runtime_inventory_mismatch"
	ReasonContentMismatch     Reason = "runtime_content_mismatch"
	ReasonChanged             Reason = "runtime_changed"
	ReasonReadFailed          Reason = "runtime_read_failed"
	ReasonPlatformUnsupported Reason = "runtime_platform_unsupported"
)

// Error deliberately exposes only a bounded code, never an OS error or path.
// Missing selection is separate from a selected runtime that cannot be proved.
// Neither result authorizes falling back to unverified candidate code.
type Error struct{ Reason Reason }

func (e *Error) Error() string { return "recovery runtime unavailable: " + string(e.Reason) }
func (e *Error) Is(target error) bool {
	return target == ErrUnavailable || (target == ErrNotSelected && e.Reason == ReasonNotSelected)
}
func fail(reason Reason) error { return &Error{Reason: reason} }
func ReasonCode(err error) string {
	var typed *Error
	if errors.As(err, &typed) {
		return string(typed.Reason)
	}
	return string(ReasonReadFailed)
}

type FileSpec struct {
	Path string
	Mode fs.FileMode
}

// ExpectedFiles is a fresh sorted fixed inventory. It excludes runtime.manifest,
// which always has mode 0600 and is itself named by the selected SHA-256.
func ExpectedFiles() []FileSpec {
	files := []FileSpec{
		{"bin/recovery", 0755}, {"bin/panel-checker", 0755},
		{"bin/agent-checker", 0755}, {"bin/schema17-bridge", 0755},
		{"update.sh", 0755}, {"rollback.sh", 0755},
		{"deploy/release-transaction-guard.sh", 0644},
		{"deploy/release-unit-transition.sh", 0644},
		{"deploy/release-recovery-foundation.sh", 0644},
		{"deploy/panel-tls-snapshot.sh", 0644},
		{"deploy/release-recovery-observation.sh", 0644},
		{"deploy/recovery/runtime-entry.sh", 0755},
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files
}

type Manifest struct{ Files map[string]string }

func ValidDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, c := range value {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
func Digest(raw []byte) string { digest := sha256.Sum256(raw); return hex.EncodeToString(digest[:]) }

func EncodeManifest(manifest Manifest) ([]byte, error) {
	specs := ExpectedFiles()
	if len(manifest.Files) != len(specs) {
		return nil, fail(ReasonInvalidManifest)
	}
	var b strings.Builder
	b.WriteString(manifestHeader)
	for _, spec := range specs {
		digest, ok := manifest.Files[spec.Path]
		if !ok || !ValidDigest(digest) {
			return nil, fail(ReasonInvalidManifest)
		}
		b.WriteString(digest + "  " + spec.Path + "\n")
	}
	return []byte(b.String()), nil
}

func ParseManifest(raw []byte) (Manifest, error) {
	if len(raw) == 0 || len(raw) > MaxManifestSize {
		return Manifest{}, fail(ReasonInvalidManifest)
	}
	if !strings.HasPrefix(string(raw), manifestHeader) {
		if strings.HasPrefix(string(raw), "format=celikpanel-recovery-runtime-") {
			return Manifest{}, fail(ReasonUnsupported)
		}
		return Manifest{}, fail(ReasonInvalidManifest)
	}
	specs := ExpectedFiles()
	lines := strings.Split(strings.TrimPrefix(string(raw), manifestHeader), "\n")
	if len(lines) != len(specs)+1 || lines[len(lines)-1] != "" {
		return Manifest{}, fail(ReasonInvalidManifest)
	}
	result := Manifest{Files: make(map[string]string, len(specs))}
	for index, spec := range specs {
		line := lines[index]
		if len(line) != 66+len(spec.Path) || line[64:66] != "  " || line[66:] != spec.Path || !ValidDigest(line[:64]) {
			return Manifest{}, fail(ReasonInvalidManifest)
		}
		result.Files[spec.Path] = line[:64]
	}
	return result, nil
}

func EncodeSelection(digest string) ([]byte, error) {
	if !ValidDigest(digest) {
		return nil, fail(ReasonInvalidSelection)
	}
	return []byte(selectionHeader + digest + "\n"), nil
}
func ParseSelection(raw []byte) (string, error) {
	if len(raw) > MaxSelectionSize || len(raw) != len(selectionHeader)+65 || !strings.HasPrefix(string(raw), selectionHeader) || raw[len(raw)-1] != '\n' {
		return "", fail(ReasonInvalidSelection)
	}
	digest := string(raw[len(selectionHeader) : len(raw)-1])
	if !ValidDigest(digest) {
		return "", fail(ReasonInvalidSelection)
	}
	return digest, nil
}

// Runtime retains pinned proof descriptors until Close. Revalidate repeats the
// selection, directory identity/inventory and payload proof before a caller
// uses the kit. This does not replace the release lock or snapshot admission.
// Callers must not modify Root or Digest; changes are rejected by Revalidate.
type Runtime struct {
	Root   string
	Digest string
	state  *runtimeState
}
