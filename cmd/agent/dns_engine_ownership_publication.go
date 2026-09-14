package main

import (
	"errors"
	"fmt"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/transport"
)

// bindPublicationPreservesEngineOwnership recognizes only the fields advanced
// by ordinary BIND zone publication. Acquisition identity and directional
// authority must remain exact. This relationship alone is not proof of a live
// generation: callers must also verify the current tree and runtime config.
//
// BIND alan yayımı nesli ve katalog sayacını ilerletir; motorun edinim kimliği
// ve yönlü yetkisi değişmez. Bu ilişki tek başına yeterli değildir: çağıran,
// etkin ağacı ve çalışma yapılandırmasını ayrıca doğrulamalıdır.
func bindPublicationPreservesEngineOwnership(ownership, state dnsEngineStateReceipt) bool {
	if validateDNSEngineState(ownership) != nil || validateDNSEngineState(state) != nil ||
		ownership.Engine != transport.DNSEngineBIND || state.Engine != transport.DNSEngineBIND ||
		ownership.Generation == state.Generation || state.PairRole == binddns.PairRoleSecondary ||
		state.PrimaryCatalogSerial < ownership.PrimaryCatalogSerial {
		return false
	}
	candidate := ownership
	candidate.Generation = state.Generation
	candidate.PrimaryCatalogSerial = state.PrimaryCatalogSerial
	return candidate == state
}

func verifyCurrentDNSEngineOwnership(
	ownership, state dnsEngineStateReceipt,
	verifyBIND func(dnsEngineStateReceipt) error,
) error {
	if ownership == state {
		return nil
	}
	if !bindPublicationPreservesEngineOwnership(ownership, state) {
		return errors.New("DNS engine state differs from its acquisition ownership")
	}
	if verifyBIND == nil {
		return errors.New("current BIND publication proof is unavailable")
	}
	if err := verifyBIND(state); err != nil {
		return fmt.Errorf("verify current BIND publication under its acquisition ownership: %w", err)
	}
	return nil
}
