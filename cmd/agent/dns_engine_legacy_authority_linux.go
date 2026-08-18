//go:build linux

package main

import "fmt"

func inspectLegacyPowerDNSDurableAuthorityOnHost(requireResolved bool) error {
	return inspectLegacyPowerDNSAuthorityOnHost(requireResolved, false)
}

func inspectLegacyPowerDNSMutationAuthorityOnHost(requireResolved bool) error {
	return inspectLegacyPowerDNSAuthorityOnHost(requireResolved, true)
}

func inspectLegacyPowerDNSAuthorityOnHost(requireResolved, mutation bool) error {
	validate := validateLegacyPowerDNSDurableAuthority
	if mutation {
		validate = validateLegacyPowerDNSMutationAuthority
	}
	_, journalExists, err := readDNSEngineSwitchJournal()
	if err != nil {
		return fmt.Errorf("inspect durable DNS engine switch journal: %w", err)
	}
	if journalExists {
		return validate(
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
	return validate(
		state, stateExists, journalExists, requireResolved,
	)
}
