//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
)

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
	if err := validate(
		state, stateExists, journalExists, requireResolved,
	); err != nil {
		return err
	}
	if mutation && stateExists && isLegacyDNSEngineState(state) {
		return validateTuplelessPowerDNSLegacyMutationAuthority()
	}
	return nil
}

func validateTuplelessPowerDNSLegacyMutationAuthority() error {
	config, err := dnsClusterConfigReadFile(dnsClusterConf)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	parsed, ok := parseManagedDNSClusterPowerDNSConfig(string(config))
	if !ok || len(parsed.AllowAXFRIPs) != 1 ||
		parsed.NotifyIP != parsed.AllowAXFRIPs[0] ||
		string(config) != dnsClusterConfig(&DNSClusterRequest{
			Role: dnsRolePaired, PeerIP: parsed.AllowAXFRIPs[0],
		}) {
		return errors.New("tuple-less PowerDNS pair configuration is not exact")
	}
	db, err := openPDNSEngineDB(pdnsDBPath(), true)
	if err != nil {
		return err
	}
	defer db.Close()
	var producers, consumers int
	if err := db.QueryRow(`SELECT COUNT(*) FROM domains WHERE account = ? AND UPPER(type) = 'PRODUCER'`,
		pdnsBINDCatalogAccount).Scan(&producers); err != nil {
		return err
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM domains WHERE account = ? AND UPPER(type) = 'CONSUMER'`,
		pdnsPeerCatalogAccount).Scan(&consumers); err != nil {
		return err
	}
	switch {
	case producers == 1 && consumers == 0:
		identity, enabled, err := managedPDNSLegacyCatalogIdentity(
			context.Background(), pdnsDBPath(),
		)
		if err != nil || !enabled {
			if err == nil {
				err = errors.New("tuple-less PowerDNS producer identity is unavailable")
			}
			return err
		}
		_, primary, err := readManagedPDNSPrimaryCatalogWithIdentity(
			context.Background(), identity,
		)
		if err != nil || !primary {
			if err == nil {
				err = errors.New("tuple-less PowerDNS producer authority is not exact")
			}
			return err
		}
		return nil
	case producers == 0 && consumers == 1:
		return errors.New("tuple-less PowerDNS consumer requires V3 mutation authority")
	default:
		return errors.New("tuple-less PowerDNS catalog authority is ambiguous")
	}
}
