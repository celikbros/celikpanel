// Package dnsengineartifact owns the wire and semantic roles of DNS engine
// evidence. It performs no filesystem access, live observation or mutation.
package dnsengineartifact

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
	"github.com/alicelik/celikpanel/internal/transport"
)

const StateSchemaV1 = "celikpanel-dns-engine-state/v1"

// StateV1 preserves historical producer field order and omission rules. It is
// the legacy combined wire format, not permission to compare all fields as owner
// authority. Frozen snapshots still require exact bytes; live roles use CompareV1.
type StateV1 struct {
	Schema               string              `json:"schema"`
	Mode                 string              `json:"mode"`
	Engine               transport.DNSEngine `json:"engine"`
	EngineEpoch          int64               `json:"engine_epoch"`
	Generation           string              `json:"generation,omitempty"`
	PairRole             string              `json:"pair_role,omitempty"`
	PairLocalIP          string              `json:"pair_local_ip,omitempty"`
	PairPeerIP           string              `json:"pair_peer_ip,omitempty"`
	PrimaryCatalogSerial uint32              `json:"primary_catalog_serial,omitempty"`
	SourceRevision       int64               `json:"source_revision"`
	ManifestQualifier    string              `json:"manifest_qualifier"`
	MutationRequestID    string              `json:"mutation_request_id"`
	MutationOwnerID      string              `json:"mutation_owner_id"`
}

func CanonicalV1(state StateV1) ([]byte, error) {
	if err := ValidateV1(state); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("encode DNS engine state: %w", err)
	}
	return append(encoded, '\n'), nil
}

func DecodeV1(data []byte) (StateV1, error) {
	if len(data) == 0 || len(data) > 64<<10 {
		return StateV1{}, errors.New("DNS engine state has an invalid size")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var state StateV1
	if err := decoder.Decode(&state); err != nil {
		return StateV1{}, fmt.Errorf("decode DNS engine state: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return StateV1{}, errors.New("DNS engine state contains trailing JSON")
	}
	canonical, err := CanonicalV1(state)
	if err != nil {
		return StateV1{}, err
	}
	if !bytes.Equal(data, canonical) {
		return StateV1{}, errors.New("DNS engine state is not canonical JSON")
	}
	return state, nil
}

func ValidateV1(state StateV1) error {
	if state.Schema != StateSchemaV1 {
		return errors.New("DNS engine state has an unsupported identity")
	}
	acquisition := acquisitionFromV1(state)
	if err := validateAcquisition(acquisition); err != nil {
		return err
	}
	return validatePublication(acquisition, PublicationV1{Generation: state.Generation, PrimaryCatalogSerial: state.PrimaryCatalogSerial})
}

func acquisitionFromV1(state StateV1) AcquisitionV1 {
	return AcquisitionV1{
		Mode: state.Mode, Engine: state.Engine, EngineEpoch: state.EngineEpoch,
		PairRole: state.PairRole, PairLocalIP: state.PairLocalIP, PairPeerIP: state.PairPeerIP,
		SourceRevision: state.SourceRevision, ManifestQualifier: state.ManifestQualifier,
		MutationRequestID: state.MutationRequestID, MutationOwnerID: state.MutationOwnerID,
	}
}

func validateAcquisition(state AcquisitionV1) error {
	if !transport.ValidDNSEngine(state.Engine) ||
		(state.Mode != transport.DNSEngineSwitchModeSwitch && state.Mode != transport.DNSEngineSwitchModeAdopt) ||
		state.EngineEpoch < 1 || state.SourceRevision < 0 ||
		!mutationpayload.ValidDNSEngineSwitchQualifier(state.ManifestQualifier) ||
		!servicemutationledger.ValidIdentity(state.MutationRequestID) || !servicemutationledger.ValidIdentity(state.MutationOwnerID) {
		return errors.New("DNS engine state has an unsupported identity")
	}
	if state.Mode == transport.DNSEngineSwitchModeAdopt && state.Engine != transport.DNSEnginePowerDNS {
		return errors.New("DNS engine adoption state must name PowerDNS")
	}
	if state.Mode == transport.DNSEngineSwitchModeAdopt && (state.PairRole != "" || state.PairLocalIP != "" || state.PairPeerIP != "") {
		return errors.New("legacy PowerDNS adoption state cannot claim directional primary identity")
	}
	if (state.PairLocalIP == "") != (state.PairPeerIP == "") {
		return errors.New("DNS engine state contains a partial pair address identity")
	}
	hasPairAddresses := state.PairLocalIP != ""
	if hasPairAddresses {
		localIP, peerIP := net.ParseIP(state.PairLocalIP), net.ParseIP(state.PairPeerIP)
		if localIP == nil || localIP.To4() == nil || localIP.String() != state.PairLocalIP || !localIP.IsGlobalUnicast() ||
			peerIP == nil || peerIP.To4() == nil || peerIP.String() != state.PairPeerIP || !peerIP.IsGlobalUnicast() || localIP.Equal(peerIP) {
			return errors.New("DNS engine state pair addresses are not canonical and distinct")
		}
	}
	switch state.PairRole {
	case transport.DNSPairRolePrimary, transport.DNSPairRoleSecondary:
		if !hasPairAddresses {
			return errors.New("paired DNS engine state is missing its address identity")
		}
	case "":
		if hasPairAddresses {
			return errors.New("standalone DNS engine state contains directional pair identity")
		}
	default:
		return errors.New("DNS engine state has an unsupported pair role")
	}
	return nil
}

func validatePublication(acquisition AcquisitionV1, publication PublicationV1) error {
	if acquisition.PairRole == transport.DNSPairRolePrimary {
		if publication.PrimaryCatalogSerial == 0 {
			return errors.New("paired primary DNS engine state is missing its catalog serial")
		}
	} else if publication.PrimaryCatalogSerial != 0 {
		return errors.New("non-primary DNS engine state contains a primary catalog serial")
	}
	if acquisition.Engine == transport.DNSEngineBIND {
		if !ValidGeneration(publication.Generation) {
			return errors.New("BIND engine state has an invalid generation")
		}
	} else if publication.Generation != "" {
		return errors.New("PowerDNS engine state unexpectedly names a BIND generation")
	}
	return nil
}

func ValidGeneration(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}
