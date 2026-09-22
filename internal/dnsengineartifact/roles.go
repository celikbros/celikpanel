package dnsengineartifact

import (
	"errors"

	"github.com/alicelik/celikpanel/internal/transport"
)

// AcquisitionV1 is the immutable engine tenure represented by legacy v1.
// SourceRevision identifies the acquisition's reviewed source, not later zones.
// Pair direction and endpoints are authority, never ordinary publication drift.
type AcquisitionV1 struct {
	Mode                                                  string
	Engine                                                transport.DNSEngine
	EngineEpoch                                           int64
	PairRole, PairLocalIP, PairPeerIP                     string
	SourceRevision                                        int64
	ManifestQualifier, MutationRequestID, MutationOwnerID string
}

// PublicationV1 is changeable configuration evidence within that tenure. It
// neither grants engine ownership nor proves what the native daemon is serving.
type PublicationV1 struct {
	Generation           string
	PrimaryCatalogSerial uint32
}

func SplitV1(state StateV1) (AcquisitionV1, PublicationV1, error) {
	if err := ValidateV1(state); err != nil {
		return AcquisitionV1{}, PublicationV1{}, err
	}
	return AcquisitionV1{
		Mode: state.Mode, Engine: state.Engine, EngineEpoch: state.EngineEpoch,
		PairRole: state.PairRole, PairLocalIP: state.PairLocalIP, PairPeerIP: state.PairPeerIP,
		SourceRevision: state.SourceRevision, ManifestQualifier: state.ManifestQualifier,
		MutationRequestID: state.MutationRequestID, MutationOwnerID: state.MutationOwnerID,
	}, PublicationV1{Generation: state.Generation, PrimaryCatalogSerial: state.PrimaryCatalogSerial}, nil
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
	acquisition, before, err := SplitV1(ownership)
	if err != nil {
		return 0, err
	}
	currentAcquisition, after, err := SplitV1(current)
	if err != nil {
		return 0, err
	}
	if acquisition != currentAcquisition {
		return 0, errors.New("DNS engine state differs from its acquisition ownership")
	}
	if before == after {
		return SamePublication, nil
	}
	if acquisition.Engine != transport.DNSEngineBIND || acquisition.PairRole == transport.DNSPairRoleSecondary || before.Generation == after.Generation || after.PrimaryCatalogSerial < before.PrimaryCatalogSerial {
		return 0, errors.New("DNS engine publication is not a supported continuation of its acquisition")
	}
	return LaterBINDPublication, nil
}
