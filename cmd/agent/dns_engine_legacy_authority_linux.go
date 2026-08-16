//go:build linux

package main

import "fmt"

func inspectLegacyPowerDNSDurableAuthorityOnHost(requireResolved bool) error {
	_, journalExists, err := readDNSEngineSwitchJournal()
	if err != nil {
		return fmt.Errorf("inspect durable DNS engine switch journal: %w", err)
	}
	if journalExists {
		return validateLegacyPowerDNSDurableAuthority(
			dnsEngineStateReceipt{}, false, true, requireResolved,
		)
	}
	state, stateExists, err := readDNSEngineState()
	if err != nil {
		return fmt.Errorf("inspect durable DNS engine state: %w", err)
	}
	_, journalExists, err = readDNSEngineSwitchJournal()
	if err != nil {
		return fmt.Errorf("recheck durable DNS engine switch journal: %w", err)
	}
	return validateLegacyPowerDNSDurableAuthority(
		state, stateExists, journalExists, requireResolved,
	)
}
