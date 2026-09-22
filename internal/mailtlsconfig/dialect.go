package mailtlsconfig

import (
	"errors"
	"regexp"
	"strings"
)

var ErrDovecotVersionUnknown = errors.New("Dovecot version could not be verified; inspect the installed dovecot executable and retry the same operation")
var ErrDovecotVersionUnsupported = errors.New("the observed Dovecot version has no supported configuration dialect; the server owner must review the installed version before retrying")

var dovecotVersion = regexp.MustCompile(`^([0-9]+)\.([0-9]+)(?:\.[0-9]+)*(?:[-+][A-Za-z0-9][A-Za-z0-9._+~-]*)?$`)

// DovecotIs24 accepts only the two implemented configuration dialects. A failed,
// empty or unrecognized observation is never silently converted to modern=true.
// No raw command output is included in guidance.
func DovecotIs24(output []byte) (bool, error) {
	if len(output) == 0 || len(output) > 4096 {
		return false, ErrDovecotVersionUnknown
	}
	fields := strings.Fields(string(output))
	if len(fields) == 0 {
		return false, ErrDovecotVersionUnknown
	}
	parts := dovecotVersion.FindStringSubmatch(fields[0])
	if len(parts) != 3 {
		return false, ErrDovecotVersionUnknown
	}
	if parts[1] != "2" {
		return false, ErrDovecotVersionUnsupported
	}
	switch parts[2] {
	case "3":
		return false, nil
	case "4":
		return true, nil
	default:
		return false, ErrDovecotVersionUnsupported
	}
}
