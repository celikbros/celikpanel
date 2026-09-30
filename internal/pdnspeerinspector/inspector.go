// Package pdnspeerinspector obtains a read-only observation from the local
// PowerDNS process. It never changes native service or database state.
package pdnspeerinspector

import (
	"context"
	"errors"
	"net"
	"time"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/pdnspeerproof"
	"github.com/alicelik/celikpanel/internal/transport"
)

const PolicySchemaV1 = "celikpanel-pdns-peer-inspector-policy/v1"

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

const DatabasePath = "/var/lib/powerdns/pdns.sqlite3"
const ConfigPath = "/etc/powerdns/pdns.conf"

type OwnerPolicyV1 struct {
	Schema         string `json:"schema"`
	PrimaryIP      string `json:"primary_ip"`
	PeerIP         string `json:"peer_ip"`
	CatalogName    string `json:"catalog_name"`
	CatalogAccount string `json:"catalog_account"`
}

func (p OwnerPolicyV1) ValidateRequest(r pdnspeerproof.RequestV1) error {
	if r.Validate() != nil || p.Schema != PolicySchemaV1 || p.PrimaryIP != r.PrimaryIP || p.PeerIP != r.PeerIP || p.CatalogName != r.CatalogName || p.CatalogAccount == "" || len(p.CatalogAccount) > 128 || p.PrimaryIP == p.PeerIP {
		return errors.New("PowerDNS owner policy does not authorize request")
	}
	ip := net.ParseIP(p.PeerIP)
	catalog, err := binddns.CatalogDomain(p.PrimaryIP)
	if err != nil || ip == nil || ip.To4() == nil || ip.String() != p.PeerIP || !ip.IsGlobalUnicast() || catalog != p.CatalogName {
		return errors.New("PowerDNS owner policy authority is invalid")
	}
	for _, c := range p.CatalogAccount {
		if c < 'a' || c > 'z' {
			if c < '0' || c > '9' {
				if c != '-' && c != '_' {
					return errors.New("PowerDNS catalog account is invalid")
				}
			}
		}
	}
	return nil
}

type PolicyReader interface {
	Read(context.Context) (OwnerPolicyV1, string, error)
}
type Snapshot struct {
	ProcessID             uint64
	ProcessStartTicks     uint64
	ConfigSHA256          string
	DatabaseDevice        uint64
	DatabaseInode         uint64
	ListenersVerified     bool
	ControlSocketVerified bool
	CatalogSerial         uint32
	CatalogMembers        []string
	CatalogTransferred    bool
	ZoneState             string // unloaded, loaded, unknown; independent daemon control observation
}
type Reader interface {
	Read(context.Context, pdnspeerproof.RequestV1, OwnerPolicyV1) (Snapshot, error)
}

func Inspect(ctx context.Context, request pdnspeerproof.RequestV1, reader Reader, policy PolicyReader, now func() time.Time) (pdnspeerproof.ResponseV1, error) {
	if request.Validate() != nil || reader == nil || policy == nil || now == nil {
		return pdnspeerproof.ResponseV1{}, errors.New("invalid PowerDNS inspection request")
	}
	firstPolicy, firstHash, err := policy.Read(ctx)
	if err != nil || firstHash == "" || firstPolicy.ValidateRequest(request) != nil {
		return pdnspeerproof.ResponseV1{}, errors.New("PowerDNS owner policy unavailable or mismatched")
	}
	at := now().Unix()
	if at < request.IssuedAtUnix-5 || at > request.ExpiresAtUnix {
		return pdnspeerproof.ResponseV1{}, errors.New("PowerDNS request outside validity interval")
	}
	digest, err := pdnspeerproof.RequestSHA256(request)
	if err != nil {
		return pdnspeerproof.ResponseV1{}, err
	}
	first, err := reader.Read(ctx, request, firstPolicy)
	if err != nil {
		return pdnspeerproof.ResponseV1{}, err
	}
	second, err := reader.Read(ctx, request, firstPolicy)
	if err != nil {
		return pdnspeerproof.ResponseV1{}, err
	}
	secondPolicy, secondHash, err := policy.Read(ctx)
	if err != nil || secondHash != firstHash || secondPolicy.ValidateRequest(request) != nil {
		return pdnspeerproof.ResponseV1{}, errors.New("PowerDNS owner policy changed")
	}
	catalogState, memberState, nativeState := "unknown", "unknown", "unknown"
	if complete(first) && complete(second) && first.ProcessID == second.ProcessID && first.ProcessStartTicks == second.ProcessStartTicks && first.ConfigSHA256 == second.ConfigSHA256 && first.DatabaseDevice == second.DatabaseDevice && first.DatabaseInode == second.DatabaseInode && first.CatalogSerial == second.CatalogSerial {
		a, ea := pdnspeerproof.CatalogMembersSHA256(request.PrimaryIP, first.CatalogSerial, first.CatalogMembers)
		b, eb := pdnspeerproof.CatalogMembersSHA256(request.PrimaryIP, second.CatalogSerial, second.CatalogMembers)
		if ea == nil && eb == nil && a == b && a == request.CatalogMembersSHA256 && first.CatalogSerial == request.CatalogSerial {
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
		} else if ea == nil && eb == nil && a == b {
			catalogState = "stale"
		}
	}
	observed := now().Unix()
	if observed < request.IssuedAtUnix || observed > request.ExpiresAtUnix {
		return pdnspeerproof.ResponseV1{}, errors.New("PowerDNS observation expired")
	}
	response := pdnspeerproof.ResponseV1{Schema: pdnspeerproof.ResponseSchemaV1, RequestSHA256: digest, Nonce: request.Nonce, Attempt: request.Attempt, PrimaryIP: request.PrimaryIP, PeerIP: request.PeerIP, CatalogName: request.CatalogName, CatalogSerial: request.CatalogSerial, CatalogMembersSHA256: request.CatalogMembersSHA256, DeletedZone: request.DeletedZone, View: request.View, CatalogState: catalogState, MemberState: memberState, NativeState: nativeState, ObservedAtUnix: observed}
	if response.Validate() != nil {
		return pdnspeerproof.ResponseV1{}, errors.New("invalid PowerDNS observation")
	}
	return response, nil
}
func complete(s Snapshot) bool {
	return s.ProcessID > 0 && s.ProcessStartTicks > 0 && len(s.ConfigSHA256) == 64 && s.DatabaseDevice > 0 && s.DatabaseInode > 0 && s.ListenersVerified && s.ControlSocketVerified && s.CatalogTransferred && s.CatalogSerial > 0 && (s.ZoneState == "unloaded" || s.ZoneState == "loaded" || s.ZoneState == "unknown")
}
