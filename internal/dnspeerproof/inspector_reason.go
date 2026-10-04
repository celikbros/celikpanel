package dnspeerproof

import (
	"regexp"
	"strings"

	"github.com/alicelik/celikpanel/internal/transport"
)

// InspectorReasonPrefixV1 starts the only stderr line an owner-enrolled
// inspector may use to explain an incomplete observation to the primary:
//
//	celikpanel-peer-inspect-reason/v1 <request-sha256> <reason>
//
// The request digest binds the line to the exact challenge; the reason is one
// reviewed token (transport.ValidDNSPeerInspectorReason). No inspector, SSH or
// native command output crosses to the primary, and the line never proves
// anything: the deletion stays pending whatever it says.
const InspectorReasonPrefixV1 = "celikpanel-peer-inspect-reason/v1"

var inspectorReasonLine = regexp.MustCompile(
	`^` + regexp.QuoteMeta(InspectorReasonPrefixV1) + ` ([0-9a-f]{64}) ([a-z_]{1,40})$`,
)

// FormatInspectorReason returns the bounded stderr line for a reviewed reason,
// or false when the digest or reason is not reviewed.
func FormatInspectorReason(requestSHA256, reason string) (string, bool) {
	line := InspectorReasonPrefixV1 + " " + requestSHA256 + " " + reason
	if !inspectorReasonLine.MatchString(line) || !transport.ValidDNSPeerInspectorReason(reason) {
		return "", false
	}
	return line, true
}

// ParseInspectorReason accepts only stderr that is exactly one reviewed reason
// line bound to requestSHA256. Anything else yields "".
func ParseInspectorReason(stderr []byte, requestSHA256 string) string {
	if len(stderr) == 0 || len(stderr) > 256 {
		return ""
	}
	text := strings.TrimSuffix(string(stderr), "\n")
	if strings.ContainsAny(text, "\r\n") {
		return ""
	}
	match := inspectorReasonLine.FindStringSubmatch(text)
	if match == nil || match[1] != requestSHA256 || !transport.ValidDNSPeerInspectorReason(match[2]) {
		return ""
	}
	return match[2]
}
