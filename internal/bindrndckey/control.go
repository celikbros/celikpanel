package bindrndckey

import (
	"errors"
	"strings"

	"github.com/alicelik/celikpanel/internal/transport"
)

// UnavailableError is the typed result of an rndc call that failed because
// the control channel is unusable: no rndc.key/rndc.conf, a key rndc cannot
// read, an authentication refusal, or nothing accepting on the control port.
// Detail is the first line of rndc's own output. rndc never prints key
// material in these messages; they name a path or an address at most.
//
// UnavailableError, denetim kanalı kullanılamadığı için başarısız olan bir
// rndc çağrısının türlü sonucudur. Detail, rndc'nin kendi çıktısının ilk
// satırıdır; bu iletilerde anahtar içeriği yer almaz.
type UnavailableError struct {
	Detail string
}

func (e *UnavailableError) Error() string {
	if e == nil {
		return transport.DNSPublicationFailureBINDRNDCUnavailable
	}
	return "named cannot be asked about zone state (" +
		transport.DNSPublicationFailureBINDRNDCUnavailable + "): " + e.Detail
}

// Reason is the reviewed wire reason.
func (*UnavailableError) Reason() string {
	return transport.DNSPublicationFailureBINDRNDCUnavailable
}

// ReasonOf returns the reviewed reason carried anywhere in err's chain.
func ReasonOf(err error) (*UnavailableError, bool) {
	var unavailable *UnavailableError
	if errors.As(err, &unavailable) && unavailable != nil {
		return unavailable, true
	}
	return nil, false
}

// ClassifyControlFailure types one failed rndc invocation from its combined
// output. It returns nil for a successful command and for every failure that
// is about the question rather than the channel (for example "not found").
func ClassifyControlFailure(output []byte, commandErr error) error {
	if commandErr == nil {
		return nil
	}
	line := FirstLine(output)
	lower := strings.ToLower(line)
	switch {
	case strings.HasPrefix(lower, "rndc: neither ") && strings.HasSuffix(lower, " was found"):
		// rndc: neither /etc/rndc.conf nor /etc/rndc.key was found
	case strings.Contains(lower, "could not load rndc configuration"):
	case strings.HasPrefix(lower, "rndc: ") && strings.Contains(lower, "rndc.") &&
		(strings.Contains(lower, "permission denied") || strings.Contains(lower, "bad secret") ||
			strings.Contains(lower, "unknown key") || strings.Contains(lower, "file not found")):
		// rndc: error: open: /etc/bind/rndc.key: permission denied
	case strings.HasPrefix(lower, "rndc: connection to remote host closed"):
		// Authentication refused: wrong key, algorithm or clock.
	case strings.HasPrefix(lower, "rndc: connect failed:"):
		// Control channel refused: nothing listening, or not allowed.
	case strings.HasPrefix(lower, "rndc: recv failed:") || strings.HasPrefix(lower, "rndc: send failed:"):
	default:
		return nil
	}
	return &UnavailableError{Detail: line}
}

// FirstLine is the first non-empty line of output, bounded to 200 bytes and
// reduced to printable ASCII so it is safe to log and persist.
func FirstLine(output []byte) string {
	for _, raw := range strings.Split(string(output), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		var builder strings.Builder
		for _, r := range line {
			if builder.Len() >= 200 {
				break
			}
			if r >= 0x20 && r < 0x7f {
				builder.WriteRune(r)
			} else {
				builder.WriteByte('?')
			}
		}
		return builder.String()
	}
	return ""
}
