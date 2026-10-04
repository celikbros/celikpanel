package main

import (
	"encoding/hex"
	"errors"
	"io"
	"time"

	"github.com/alicelik/celikpanel/internal/dnspeerproof"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
)

// mintBINDPeerDeletionRequest derives a fresh inspection challenge from
// durable V3 receipt fields and the source-bound catalog pair checked by the
// caller. The caller must also recheck the current accepted ledger operation,
// enrollment and native state before and after the network exchange.
func mintBINDPeerDeletionRequest(
	plan dnsV3PrimaryPropagationPlan,
	authority dnsPeerAXFRAuthority,
	peerIdentitySHA256 string,
	attempt uint64,
	now time.Time,
	randomness io.Reader,
) (dnspeerproof.RequestV1, error) {
	if !plan.Changed.Delete || plan.Legacy ||
		!servicemutationledger.ValidIdentity(plan.Operation.RequestID) ||
		!servicemutationledger.ValidIdentity(plan.Operation.OwnerID) ||
		plan.Operation.Generation < 0 ||
		!mutationpayload.ValidDNSZoneSyncV3Qualifier(plan.Operation.Qualifier) ||
		authority.sourceIP != plan.Evidence.LocalIP ||
		authority.peerIP != plan.Evidence.PeerIP ||
		authority.catalog != plan.Evidence.Domain ||
		authority.catalogSerial != plan.Evidence.Serial ||
		attempt == 0 || randomness == nil {
		return dnspeerproof.RequestV1{}, errors.New("native DNS deletion request is not bound to the accepted operation and pair")
	}
	if err := validateDNSV3PrimaryPropagationPlan(plan); err != nil {
		return dnspeerproof.RequestV1{}, errors.New("native DNS deletion request lacks durable catalog evidence")
	}
	digest, err := dnspeerproof.CatalogMembersSHA256(
		authority.sourceIP, authority.catalogSerial, plan.Evidence.Members,
	)
	if err != nil {
		return dnspeerproof.RequestV1{}, errors.New("native DNS deletion catalog digest is invalid")
	}
	var nonce [32]byte
	if _, err := io.ReadFull(randomness, nonce[:]); err != nil {
		return dnspeerproof.RequestV1{}, errors.New("native DNS deletion nonce could not be created")
	}
	issued := now.Unix()
	request := dnspeerproof.RequestV1{
		Schema:               dnspeerproof.RequestSchemaV1,
		MutationRequestID:    plan.Operation.RequestID,
		MutationOwnerID:      plan.Operation.OwnerID,
		DeletionGeneration:   plan.Operation.Generation,
		DeletionQualifier:    plan.Operation.Qualifier,
		PrimaryIP:            authority.sourceIP,
		PeerIP:               authority.peerIP,
		PeerIdentitySHA256:   peerIdentitySHA256,
		CatalogName:          authority.catalog,
		CatalogSerial:        authority.catalogSerial,
		CatalogMembersSHA256: digest,
		DeletedZone:          plan.Changed.Domain,
		View:                 dnspeerproof.DefaultView,
		Nonce:                hex.EncodeToString(nonce[:]),
		Attempt:              attempt,
		IssuedAtUnix:         issued,
		ExpiresAtUnix:        issued + 30,
	}
	if err := request.Validate(); err != nil {
		return dnspeerproof.RequestV1{}, errors.New("native DNS deletion challenge is invalid")
	}
	return request, nil
}
