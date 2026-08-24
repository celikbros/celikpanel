//go:build !linux

package main

import (
	"context"
	"errors"
)

func resolvePDNSGroupGID(context.Context) (uint32, error) {
	return 0, errors.New("PowerDNS service group proof requires Linux")
}

func captureHostPDNSConfigObservations(
	pdnsConfigOwnerPolicy,
) ([]pdnsConfigObservation, error) {
	return nil, errors.New("secure PowerDNS config snapshots require Linux")
}

func secureWritePDNSConfigReplacingObservation(
	pdnsConfigOwnerPolicy,
	pdnsConfigObservation,
	dnsFileSnapshot,
) error {
	return errors.New("secure PowerDNS config replacement requires Linux")
}

func secureRemovePDNSConfigReplacingObservation(
	pdnsConfigOwnerPolicy,
	pdnsConfigObservation,
) error {
	return errors.New("secure PowerDNS config removal requires Linux")
}
