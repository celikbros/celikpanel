//go:build !linux

package pdnspeerenrollment

import "errors"

// Peer inspection is not supported on this platform. No enrollment or
// credential is read and no remote proof can be admitted.
type Code string

const Unsupported Code = "pdns_peer_inspection_unsupported_platform"

type StateError struct{ Code Code }

func (e StateError) Error() string { return string(e.Code) }
func IsCode(err error, code Code) bool {
	var state StateError
	return errors.As(err, &state) && state.Code == code
}

type Snapshot struct{}

func Read() (Snapshot, error) { return Snapshot{}, StateError{Unsupported} }
func Recheck(Snapshot) error  { return StateError{Unsupported} }
