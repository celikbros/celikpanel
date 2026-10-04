package mailrenewalkit

import (
	"bytes"
	"encoding/json"
	"errors"
	"regexp"
)

const TransitionSchema = "celikpanel-mail-renewal-transition/v1"
const MaxTransitionSize = 128 << 10

// Transition is an immutable accepted before/after contract, not a status flag.
// Callers must separately prove owner authority, native exclusion, pinned parent
// and inode identity, source admission and durable before-image publication.
// A parsed record never grants permission to overwrite an observed owner edit.
type Transition struct {
	Schema      string            `json:"schema"`
	OperationID string            `json:"operation_id"`
	Previous    string            `json:"previous_generation"`
	Target      string            `json:"target_generation"`
	Before      map[string][]byte `json:"before"`
	After       map[string][]byte `json:"after"`
	TimerBefore TimerState        `json:"timer_before"`
	TimerAfter  TimerState        `json:"timer_after"`
}
type TimerState struct {
	Enablement string `json:"enablement"`
	Activity   string `json:"activity"`
}

var operationPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
var ErrTransition = errors.New("mail renewal transition is unverified; preserve owner configuration and the recorded operation")

func nativeFiles(files map[string][]byte) map[string][]byte {
	out := make(map[string][]byte, 3)
	for _, name := range []string{ServiceName, TimerName, HookName} {
		out[name] = bytes.Clone(files[name])
	}
	return out
}
func exactNativeFiles(actual, expected map[string][]byte) bool {
	if len(actual) != len(expected) {
		return false
	}
	for name, raw := range expected {
		value, ok := actual[name]
		if !ok || !bytes.Equal(value, raw) {
			return false
		}
	}
	return true
}
func validRetainedTimer(s TimerState) bool {
	return (s.Enablement == "enabled" || s.Enablement == "disabled") && (s.Activity == "active" || s.Activity == "inactive")
}

// NewTransition requires verified kit bytes supplied by the descriptor reader.
// A nil previous kit admits only absence or the exact recognized legacy hook;
// existing native units cannot be adopted without a matching old kit.
func NewTransition(operation string, before map[string][]byte, timer TimerState, previousManifest []byte, previousFiles map[string][]byte, targetManifest []byte, targetFiles map[string][]byte) (Transition, error) {
	target, err := Verify(targetManifest, targetFiles)
	if err != nil {
		return Transition{}, ErrTransition
	}
	t := Transition{Schema: TransitionSchema, OperationID: operation, Target: target.Generation, Before: make(map[string][]byte, len(before)), After: nativeFiles(targetFiles), TimerBefore: timer, TimerAfter: timer}
	for name, raw := range before {
		t.Before[name] = bytes.Clone(raw)
	}
	if len(previousManifest) == 0 {
		if len(previousFiles) != 0 {
			return Transition{}, ErrTransition
		}
		t.TimerAfter = TimerState{"enabled", "active"}
	} else {
		old, err := Verify(previousManifest, previousFiles)
		if err != nil {
			return Transition{}, ErrTransition
		}
		t.Previous = old.Generation
	}
	if err := t.Validate(previousManifest, previousFiles, targetManifest, targetFiles); err != nil {
		return Transition{}, err
	}
	return t, nil
}
func (t Transition) Validate(previousManifest []byte, previousFiles map[string][]byte, targetManifest []byte, targetFiles map[string][]byte) error {
	if t.Schema != TransitionSchema || t.Before == nil || !operationPattern.MatchString(t.OperationID) {
		return ErrTransition
	}
	target, err := Verify(targetManifest, targetFiles)
	if err != nil || target.Generation != t.Target || !exactNativeFiles(t.After, nativeFiles(targetFiles)) {
		return ErrTransition
	}
	if t.Previous == "" {
		if len(previousManifest) != 0 || len(previousFiles) != 0 || t.TimerBefore != (TimerState{"absent", "inactive"}) || t.TimerAfter != (TimerState{"enabled", "active"}) {
			return ErrTransition
		}
		if len(t.Before) != 0 && (len(t.Before) != 1 || !bytes.Equal(t.Before[HookName], LegacyHook())) {
			return ErrTransition
		}
	} else {
		previous, err := Verify(previousManifest, previousFiles)
		if err != nil || previous.Generation != t.Previous || t.Previous == t.Target || !exactNativeFiles(t.Before, nativeFiles(previousFiles)) || !validRetainedTimer(t.TimerBefore) || t.TimerAfter != t.TimerBefore {
			return ErrTransition
		}
	}
	return nil
}

// Encode/Decode require kit validation as well as canonical JSON. Unknown fields,
// duplicate keys, mixed generations and unrecorded native files never fall back.
func EncodeTransition(t Transition, oldManifest []byte, oldFiles map[string][]byte, newManifest []byte, newFiles map[string][]byte) ([]byte, error) {
	if err := t.Validate(oldManifest, oldFiles, newManifest, newFiles); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(t)
	if err != nil || len(raw)+1 > MaxTransitionSize {
		return nil, ErrTransition
	}
	return append(raw, '\n'), nil
}
func DecodeTransition(raw, oldManifest []byte, oldFiles map[string][]byte, newManifest []byte, newFiles map[string][]byte) (Transition, error) {
	var t Transition
	if len(raw) == 0 || len(raw) > MaxTransitionSize {
		return t, ErrTransition
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&t); err != nil {
		return Transition{}, ErrTransition
	}
	canonical, err := EncodeTransition(t, oldManifest, oldFiles, newManifest, newFiles)
	if err != nil || !bytes.Equal(raw, canonical) {
		return Transition{}, ErrTransition
	}
	return t, nil
}

// FileSide is only content classification within a separately validated accepted
// transition. Missing is explicit; unreadable must be returned as an error by
// the caller and cannot be passed here as absent. Before/after mixtures are
// expected during interrupted publication, but foreign bytes are not repaired.
type FileSide string

const (
	FileBefore FileSide = "before"
	FileAfter  FileSide = "after"
	FileEither FileSide = "either"
)

func (t Transition) FileSide(name string, raw []byte, present bool) (FileSide, error) {
	if (!present && len(raw) != 0) || (name != HookName && name != ServiceName && name != TimerName) {
		return "", ErrTransition
	}
	before, beforePresent := t.Before[name]
	after, afterPresent := t.After[name]
	if !afterPresent {
		return "", ErrTransition
	}
	old := present == beforePresent && (!present || bytes.Equal(raw, before))
	next := present && bytes.Equal(raw, after)
	if old && next {
		return FileEither, nil
	}
	if old {
		return FileBefore, nil
	}
	if next {
		return FileAfter, nil
	}
	return "", ErrTransition
}
