package dnsengineartifact

import (
	"errors"

	"github.com/alicelik/celikpanel/internal/transport"
)

// AcquisitionV1 is the immutable engine tenure represented by legacy v1.
// SourceRevision identifies the acquisition's reviewed source, not later zones.
// Pair direction and endpoints are authority, never ordinary publication drift.
type AcquisitionV1 struct {
	Mode              string              `json:"mode"`
	Engine            transport.DNSEngine `json:"engine"`
	EngineEpoch       int64               `json:"engine_epoch"`
	PairRole          string              `json:"pair_role,omitempty"`
	PairLocalIP       string              `json:"pair_local_ip,omitempty"`
	PairPeerIP        string              `json:"pair_peer_ip,omitempty"`
	SourceRevision    int64               `json:"source_revision"`
	ManifestQualifier string              `json:"manifest_qualifier"`
	MutationRequestID string              `json:"mutation_request_id"`
	MutationOwnerID   string              `json:"mutation_owner_id"`
	NativeCatalogV3   string              `json:"-"`
}

// PublicationV1 is changeable configuration evidence within that tenure. It
// neither grants engine ownership nor proves what the native daemon is serving.
type PublicationV1 struct {
	Generation           string `json:"generation,omitempty"`
	PrimaryCatalogSerial uint32 `json:"primary_catalog_serial,omitempty"`
}

func SplitV1(state StateV1) (AcquisitionV1, PublicationV1, error) {
	if err := ValidateV1(state); err != nil {
		return AcquisitionV1{}, PublicationV1{}, err
	}
	return acquisitionFromV1(state), PublicationV1{Generation: state.Generation, PrimaryCatalogSerial: state.PrimaryCatalogSerial}, nil
}

type Relationship uint8

const (
	SamePublication Relationship = iota + 1
	LaterBINDPublication
)

// CompareV1 compares roles, not entire unrelated record lifetimes. A later BIND
// publication requires an independent current tree/runtime proof from the caller.
// Exact frozen journal/snapshot comparisons must not use this function instead.
// Secondary or PowerDNS evolution has no admitted publication transition here.
func CompareV1(ownership, current StateV1) (Relationship, error) {
	acquisition, before, err := SeparateV1(ownership)
	if err != nil {
		return 0, err
	}
	currentAcquisition, after, err := SeparateV1(current)
	if err != nil {
		return 0, err
	}
	if acquisition != currentAcquisition {
		return 0, errors.New("DNS engine state differs from its acquisition ownership")
	}
	return ComparePublicationsV1(acquisition, before, after)
}
