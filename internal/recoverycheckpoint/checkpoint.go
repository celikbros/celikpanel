// Package recoverycheckpoint publishes nonauthorizing native recovery evidence.
// A record identifies a verified durable checkpoint; it never grants permission
// to mutate a release or proves that the full recovery has completed.
package recoverycheckpoint

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"time"
)

const Schema = "celikpanel/recovery-checkpoint/v1"
const Unit = "celikpanel-release-recovery.service"
const Root = "/var/lib/celikpanel-recovery-checkpoints"

var ErrUnavailable = errors.New("recovery checkpoint observation unavailable")
var hex64 = regexp.MustCompile(`^[0-9a-f]{64}$`)
var hex32 = regexp.MustCompile(`^[0-9a-f]{32}$`)
var digits = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
var snapshotName = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z-from-[A-Za-z0-9._-]+-to-[0-9a-f]{40}-[0-9a-f]{32}$`)
var uuid = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var checkpoints = map[string]int{"restore_admitted": 1, "payload_restored": 2, "units_reloaded": 3, "runtime_verified": 4, "schedulers_restored": 5}

// Record deliberately contains a token hash, never the transaction token.
type Record struct {
	Schema          string `json:"schema"`
	Snapshot        string `json:"snapshot"`
	TokenHash       string `json:"transaction_token_sha256"`
	Operation       string `json:"transaction_operation"`
	Phase           string `json:"transaction_phase"`
	Checkpoint      string `json:"checkpoint"`
	Sequence        int    `json:"sequence"`
	ObservedAt      string `json:"observed_at"`
	Unit            string `json:"recovery_unit"`
	InvocationID    string `json:"invocation_id"`
	MainPID         int    `json:"main_pid"`
	MainStartTicks  string `json:"main_start_ticks"`
	BootID          string `json:"boot_id"`
	RuntimeManifest string `json:"runtime_manifest_sha256"`
}

func ValidName(name string) bool { return checkpoints[name] != 0 }
func digest(raw []byte) string   { value := sha256.Sum256(raw); return hex.EncodeToString(value[:]) }
func (r Record) valid() bool {
	t, err := time.Parse(time.RFC3339Nano, r.ObservedAt)
	return r.Schema == Schema && snapshotName.MatchString(r.Snapshot) && hex64.MatchString(r.TokenHash) &&
		(r.Operation == "update" || r.Operation == "rollback") && (r.Phase == "active" || r.Phase == "completion.pending" || r.Phase == "scheduler-restore.pending") &&
		ValidName(r.Checkpoint) && r.Sequence > 0 && r.Sequence <= 1000000 && err == nil && t.Location() == time.UTC &&
		r.Unit == Unit && hex32.MatchString(r.InvocationID) && r.MainPID > 1 && digits.MatchString(r.MainStartTicks) && uuid.MatchString(r.BootID) && hex64.MatchString(r.RuntimeManifest)
}
func encode(r Record) ([]byte, error) {
	if !r.valid() {
		return nil, ErrUnavailable
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return nil, ErrUnavailable
	}
	return append(raw, '\n'), nil
}
func decode(raw []byte) (Record, error) {
	var r Record
	if len(raw) > 4096 || json.Unmarshal(raw, &r) != nil {
		return r, ErrUnavailable
	}
	canonical, err := encode(r)
	// Exact canonical encoding also rejects duplicate/unknown fields and coercion.
	if err != nil || !bytes.Equal(canonical, raw) {
		return Record{}, ErrUnavailable
	}
	return r, nil
}
func next(previous *Record, current Record) (Record, error) {
	current.Sequence = 1
	if previous != nil {
		if previous.TokenHash != current.TokenHash || previous.Snapshot != current.Snapshot || previous.RuntimeManifest != current.RuntimeManifest || previous.Sequence >= 1000000 {
			return Record{}, ErrUnavailable
		}
		same := previous.InvocationID == current.InvocationID && previous.BootID == current.BootID
		if same && (previous.MainPID != current.MainPID || previous.MainStartTicks != current.MainStartTicks || checkpoints[current.Checkpoint] < checkpoints[previous.Checkpoint]) {
			return Record{}, ErrUnavailable
		}
		if same {
			old, err := time.Parse(time.RFC3339Nano, previous.ObservedAt)
			now, e := time.Parse(time.RFC3339Nano, current.ObservedAt)
			if err != nil || e != nil || now.Before(old) {
				return Record{}, ErrUnavailable
			}
		}
		current.Sequence = previous.Sequence + 1
	}
	if !current.valid() {
		return Record{}, ErrUnavailable
	}
	return current, nil
}

type marker struct{ Snapshot, TokenHash, Operation, Phase string }

var markerPattern = regexp.MustCompile(`\Aversion=1\ntoken=([0-9a-f]{64})\noperation=(update|rollback)\nsnapshot=([^\n]+)\n\z`)

func parseMarker(raw []byte, phase string) (marker, error) {
	match := markerPattern.FindSubmatch(raw)
	if match == nil || !snapshotName.Match(match[3]) {
		return marker{}, ErrUnavailable
	}
	return marker{Snapshot: string(match[3]), TokenHash: digest(match[1]), Operation: string(match[2]), Phase: phase}, nil
}
func processStart(raw []byte) (string, error) {
	index := bytes.LastIndexByte(raw, ')')
	if index < 0 {
		return "", ErrUnavailable
	}
	fields := bytes.Fields(raw[index+1:])
	if len(fields) < 20 {
		return "", ErrUnavailable
	}
	value := string(fields[19])
	if !digits.MatchString(value) {
		return "", ErrUnavailable
	}
	if _, err := strconv.ParseUint(value, 10, 64); err != nil {
		return "", ErrUnavailable
	}
	return value, nil
}
