//go:build !linux

package main

import (
	"context"
	"errors"

	"github.com/alicelik/celikpanel/internal/transport"
)

func verifyFreshPrimaryArtifactsAbsentV3(dnsEngineSwitchJournal) error {
	return errors.New("v3 archive requires Linux native evidence")
}
func archiveCommittedFreshPrimaryV3(context.Context, dnsEngineSwitchJournal, dnsEngineStateReceipt) error {
	return errors.New("v3 archive requires Linux native evidence")
}
func exactArchivedFreshPrimaryProvenanceV3(context.Context, transport.DNSEngine, string, transport.ServiceMutationBinding) (bool, bool, error) {
	state, exists, err := readDNSEngineState()
	if err != nil {
		return false, true, err
	}
	if exists && state.NativeCatalogV3 != "" {
		return false, true, errors.New("v3 archived provenance requires Linux native evidence")
	}
	return false, false, nil
}
