// Package bindpeerinspector verifies a read-only native BIND deletion observation.
// It never changes BIND configuration or treats DNS transfer refusal as unloaded.
package bindpeerinspector

import (
	"context"
	"errors"
	"net"
	"time"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/dnspeerproof"
	"github.com/alicelik/celikpanel/internal/transport"
)

const PolicySchemaV1 = "celikpanel-bind-peer-inspector-policy/v1"

// ReasonError is an incomplete observation classified with one reviewed
// reason token (transport.ValidDNSPeerInspectorReason). The message stays
// local to the inspector; only the token may be reported to the primary.
type ReasonError struct {
	Reason  string
	Message string
}

func (e *ReasonError) Error() string { return e.Message }

func reasonError(reason, message string) error {
	return &ReasonError{Reason: reason, Message: message}
}

// Reason returns the reviewed reason token of an inspection error, or "".
func Reason(err error) string {
	var classified *ReasonError
	if errors.As(err, &classified) && transport.ValidDNSPeerInspectorReason(classified.Reason) {
		return classified.Reason
	}
	return ""
}

type OwnerPolicyV1 struct {
	Schema      string `json:"schema"`
	PrimaryIP   string `json:"primary_ip"`
	PeerIP      string `json:"peer_ip"`
	CatalogName string `json:"catalog_name"`
	View        string `json:"view"`
}

func (p OwnerPolicyV1) ValidateRequest(r dnspeerproof.RequestV1) error {
	if r.Validate() != nil || p.Schema != PolicySchemaV1 ||
		p.PrimaryIP != r.PrimaryIP || p.PeerIP != r.PeerIP || p.CatalogName != r.CatalogName ||
		p.View != dnspeerproof.DefaultView || r.View != p.View ||
		p.PrimaryIP == p.PeerIP {
		return errors.New("BIND peer owner policy does not authorize this request")
	}
	ip := net.ParseIP(p.PeerIP)
	catalog, err := binddns.CatalogDomain(p.PrimaryIP)
	if err != nil || ip == nil || ip.To4() == nil || ip.String() != p.PeerIP || !ip.IsGlobalUnicast() || catalog != p.CatalogName {
		return errors.New("BIND peer owner policy has an invalid authority")
	}
	return nil
}

type PolicyReader interface {
	Read(context.Context) (OwnerPolicyV1, string, error)
}

// Snapshot is obtained from the local BIND daemon and host, not from the
// request or a CelikPanel receipt. The native adapter must bind all fields to
// the same live named process and reject incomplete observations.
type Snapshot struct {
	ProcessID          uint64
	ProcessStartTicks  uint64
	ConfigSHA256       string
	ListenersVerified  bool
	CatalogSerial      uint32
	CatalogMembers     []string
	CatalogTransferred bool
	ZoneState          string // unloaded, loaded, unknown
}

type Reader interface {
	Read(context.Context, dnspeerproof.RequestV1) (Snapshot, error)
}

// Inspect requires the root-owned owner policy before any native read and
// rechecks its exact bytes after the two native observations.
func Inspect(ctx context.Context, request dnspeerproof.RequestV1, reader Reader, policy PolicyReader, now func() time.Time) (dnspeerproof.ResponseV1, error) {
	if request.Validate() != nil || reader == nil || policy == nil || now == nil {
		return dnspeerproof.ResponseV1{}, errors.New("invalid BIND peer inspection request")
	}
	firstPolicy, firstPolicyHash, err := policy.Read(ctx)
	if err != nil || firstPolicy.ValidateRequest(request) != nil || firstPolicyHash == "" {
		return dnspeerproof.ResponseV1{}, reasonError(transport.DNSPeerInspectorReasonPolicy,
			"BIND peer owner policy is unavailable or mismatched")
	}
	at := now().Unix()
	if at < request.IssuedAtUnix-5 || at > request.ExpiresAtUnix {
		return dnspeerproof.ResponseV1{}, reasonError(transport.DNSPeerInspectorReasonObservationExpired,
			"BIND peer inspection request is outside its valid interval")
	}
	digest, err := dnspeerproof.RequestSHA256(request)
	if err != nil {
		return dnspeerproof.ResponseV1{}, err
	}
	first, err := reader.Read(ctx, request)
	if err != nil {
		return dnspeerproof.ResponseV1{}, err
	}
	second, err := reader.Read(ctx, request)
	if err != nil {
		return dnspeerproof.ResponseV1{}, err
	}
	secondPolicy, secondPolicyHash, err := policy.Read(ctx)
	if err != nil || secondPolicy.ValidateRequest(request) != nil || secondPolicyHash != firstPolicyHash {
		return dnspeerproof.ResponseV1{}, reasonError(transport.DNSPeerInspectorReasonPolicy,
			"BIND peer owner policy changed during inspection")
	}
	catalogState, memberState, nativeState := "unknown", "unknown", "unknown"
	if complete(first) && complete(second) && first.ProcessID == second.ProcessID && first.ProcessStartTicks == second.ProcessStartTicks && first.ConfigSHA256 == second.ConfigSHA256 && first.CatalogSerial == second.CatalogSerial {
		firstDigest, firstErr := dnspeerproof.CatalogMembersSHA256(request.PrimaryIP, first.CatalogSerial, first.CatalogMembers)
		secondDigest, secondErr := dnspeerproof.CatalogMembersSHA256(request.PrimaryIP, second.CatalogSerial, second.CatalogMembers)
		if firstErr == nil && secondErr == nil && firstDigest == secondDigest && firstDigest == request.CatalogMembersSHA256 {
			catalogState = "transferred"
			memberState = "absent"
			for _, member := range first.CatalogMembers {
				if member == request.DeletedZone {
					memberState = "present"
				}
			}
			if first.ZoneState == second.ZoneState && (first.ZoneState == "loaded" || first.ZoneState == "unloaded") {
				nativeState = first.ZoneState
			}
		} else if firstErr == nil && secondErr == nil && firstDigest == secondDigest {
			catalogState = "stale"
		}
	}
	observed := now().Unix()
	if observed < request.IssuedAtUnix || observed > request.ExpiresAtUnix {
		return dnspeerproof.ResponseV1{}, reasonError(transport.DNSPeerInspectorReasonObservationExpired,
			"BIND peer observation expired")
	}
	response := dnspeerproof.ResponseV1{
		Schema: dnspeerproof.ResponseSchemaV1, RequestSHA256: digest, Nonce: request.Nonce, Attempt: request.Attempt,
		PrimaryIP: request.PrimaryIP, PeerIP: request.PeerIP, CatalogName: request.CatalogName,
		CatalogSerial: request.CatalogSerial, CatalogMembersSHA256: request.CatalogMembersSHA256,
		DeletedZone: request.DeletedZone, View: request.View, CatalogState: catalogState, MemberState: memberState,
		NativeState: nativeState, ObservedAtUnix: observed,
	}
	if response.Validate() != nil {
		return dnspeerproof.ResponseV1{}, errors.New("invalid BIND peer observation")
	}
	return response, nil
}

func complete(s Snapshot) bool {
	return s.ProcessID > 0 && s.ProcessStartTicks > 0 && len(s.ConfigSHA256) == 64 && s.ListenersVerified && s.CatalogTransferred && s.CatalogSerial > 0 && (s.ZoneState == "unloaded" || s.ZoneState == "loaded" || s.ZoneState == "unknown")
}
