package main

import (
	"errors"
	"fmt"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
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
	relationship, err := dnsengineartifact.CompareV1(ownership, state)
	return err == nil && relationship == dnsengineartifact.LaterBINDPublication
}

func verifyCurrentDNSEngineOwnership(
	ownership, state dnsEngineStateReceipt,
	verifyBIND func(dnsEngineStateReceipt) error,
) error {
	relationship, err := dnsengineartifact.CompareV1(ownership, state)
	if err != nil {
		return err
	}
	if relationship == dnsengineartifact.SamePublication {
		return nil
	}
	if verifyBIND == nil {
		return errors.New("current BIND publication proof is unavailable")
	}
	if err := verifyBIND(state); err != nil {
		return fmt.Errorf("verify current BIND publication under its acquisition ownership: %w", err)
	}
	return nil
}
