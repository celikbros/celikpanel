//go:build linux

// Package pdnspeertransport exchanges one PowerDNS-native, read-only peer
// observation using a separately pinned SSH forced command and wire schema.
package pdnspeertransport

import (
	"context"

	"github.com/alicelik/celikpanel/internal/dnspeertransport"
	"github.com/alicelik/celikpanel/internal/pdnspeerproof"
	"github.com/alicelik/celikpanel/internal/transport"
)

type Enrollment = dnspeertransport.Enrollment
type Code = dnspeertransport.Code
type Unknown = dnspeertransport.Unknown

const (
	CodeEnrollment  = dnspeertransport.CodeEnrollment
	CodeUnavailable = dnspeertransport.CodeUnavailable
	CodeMalformed   = dnspeertransport.CodeMalformed
	maxWireSize     = 4096
)

func IsCode(err error, code Code) bool { return dnspeertransport.IsCode(err, code) }

// InspectorReason returns the reviewed reason the authenticated PowerDNS
// inspector reported for an incomplete observation, or "".
func InspectorReason(err error) string { return dnspeertransport.InspectorReason(err) }

// pdnsInspectorReason keeps only the reasons pdns-peer-inspect reports. A
// BIND-only token from the PowerDNS channel is not trusted as a detail.
func pdnsInspectorReason(reason string) string {
	if reason == transport.DNSPeerInspectorReasonConfigUnreviewed {
		return reason
	}
	return ""
}

// Exchanger must derive authentication from the SSH handshake, not response
// content. In production, only SSH uses the fixed PDNS forced command.
type Exchanger interface {
	Exchange(context.Context, Enrollment, []byte) ([]byte, pdnspeerproof.PeerAuthentication, error)
}

type SSH struct{}

func (SSH) Exchange(ctx context.Context, enrollment Enrollment, request []byte) ([]byte, pdnspeerproof.PeerAuthentication, error) {
	return dnspeertransport.PowerDNSSSH{}.Exchange(ctx, enrollment, request)
}

// Inspect validates the enrolled identity, exact PDNS request and one bounded
// response. It does not assert native absence or consume a challenge; the
// caller must still recheck local evidence and call pdnspeerproof.Verify with a
// durable consume-once callback before a terminal result.
func Inspect(ctx context.Context, enrollment Enrollment, request pdnspeerproof.RequestV1, exchanger Exchanger) (pdnspeerproof.ResponseV1, pdnspeerproof.PeerAuthentication, error) {
	var zero pdnspeerproof.ResponseV1
	var noAuth pdnspeerproof.PeerAuthentication
	if enrollment.Validate() != nil || exchanger == nil || request.Validate() != nil ||
		request.PeerIP != enrollment.PeerIP || request.PeerIdentitySHA256 != enrollment.HostKeySHA256 {
		return zero, noAuth, Unknown{Code: CodeEnrollment}
	}
	raw, err := pdnspeerproof.EncodeRequest(request)
	if err != nil || len(raw) > maxWireSize {
		return zero, noAuth, Unknown{Code: CodeEnrollment}
	}
	bounded, cancel := context.WithTimeout(ctx, enrollment.Timeout)
	defer cancel()
	answer, auth, err := exchanger.Exchange(bounded, enrollment, raw)
	if reason, failed := dnspeertransport.CommandFailureReason(err, raw); failed {
		// The pinned host ran the owner's forced command, which exited
		// non-zero. At most one reviewed token bound to this request survives.
		return zero, noAuth, Unknown{Code: CodeUnavailable, Reason: pdnsInspectorReason(reason)}
	}
	if err != nil || !auth.Established || auth.PeerIP != enrollment.PeerIP ||
		auth.IdentitySHA256 != enrollment.HostKeySHA256 {
		return zero, noAuth, Unknown{Code: CodeUnavailable}
	}
	if len(answer) == 0 || len(answer) > maxWireSize+1 {
		return zero, noAuth, Unknown{Code: CodeMalformed}
	}
	if answer[len(answer)-1] == '\n' {
		answer = answer[:len(answer)-1]
	}
	if len(answer) == 0 || len(answer) > maxWireSize {
		return zero, noAuth, Unknown{Code: CodeMalformed}
	}
	response, err := pdnspeerproof.DecodeResponse(answer)
	if err != nil {
		return zero, noAuth, Unknown{Code: CodeMalformed}
	}
	return response, auth, nil
}

var _ Exchanger = SSH{}
var _ error = Unknown{}
