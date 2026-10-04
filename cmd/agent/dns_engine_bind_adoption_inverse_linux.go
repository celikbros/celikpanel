//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/alicelik/celikpanel/internal/bindroot"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
	"github.com/alicelik/celikpanel/internal/hostplatform"
)

// The supported adoption source is the vendor's already serving Debian BIND.
// Capture a bounded native inventory before publishing the first intent.
func captureBINDAdoptionNativeInventory(ctx context.Context, mainConfig string) ([]dnsenginerecovery.BINDAdoptionNativeZoneV1, error) {
	checkConf, err := firstTrustedExecutable([]string{"/usr/sbin/named-checkconf", "/usr/bin/named-checkconf"}, "named-checkconf")
	if err != nil {
		return nil, err
	}
	output, err := runTrackedBINDValidation(ctx, checkConf, "named-checkconf", "-l", mainConfig)
	if err != nil {
		return nil, err
	}
	return dnsenginerecovery.ParseBINDAdoptionNativeInventory(string(output))
}

func probeBINDAdoptionSourceSOA(address string) dnsenginerecovery.BINDAdoptionSOAProbe {
	return func(ctx context.Context, domain, network string) (uint32, error) {
		result, err := probeDNSZoneSOA(ctx, network, address, domain)
		if err != nil {
			return 0, err
		}
		if !result.Authoritative || result.RCode != 0 || result.AnswerCount != 1 || len(result.SOASerials) != 1 || len(result.AnswerSOAOwners) != 1 || result.AnswerSOAOwners[0] != domain {
			return 0, errors.New("BIND adoption source lacks an exact local authoritative SOA")
		}
		return result.SOASerials[0], nil
	}
}

func prepareBINDIndependentAdoptionJournal(ctx context.Context, profile hostplatform.Profile, base dnsEngineSwitchJournal, configs bindConfigMutation, mainConfig string, evidence bindAdoptionRuntimeEvidence) (dnsEngineSwitchJournal, error) {
	if profile.PackageManager != hostplatform.PackageManagerAPT || !configs.ownerAware {
		return dnsEngineSwitchJournal{}, errors.New("BIND adoption inverse requires owner-aware Debian configuration")
	}
	gid, err := resolveBINDGroupGID(ctx)
	if err != nil {
		return dnsEngineSwitchJournal{}, err
	}
	unchanged, err := dnsenginerecovery.CaptureInstalledBINDUnchangedConfigV2(ctx, bindroot.APT, gid)
	if err != nil {
		return dnsEngineSwitchJournal{}, err
	}
	inventory, err := captureBINDAdoptionNativeInventory(ctx, mainConfig)
	if err != nil {
		return dnsEngineSwitchJournal{}, err
	}
	address, err := dnsenginerecovery.ProbeAuthorityIPv4Address(ctx, "named", evidence.topology.namedProcesses.MainPID, dnsenginerecovery.SSListenerRunner)
	if err != nil {
		return dnsEngineSwitchJournal{}, err
	}
	provisional := base
	provisional.Schema = dnsengineartifact.SwitchJournalSchemaV2
	provisional.InversePlan = &dnsengineartifact.BINDSwitchInversePlanV2{
		Kind:       dnsengineartifact.BINDSwitchInversePlanKindV2,
		HostLayout: "apt", ConfigAfter: configs.desiredSnapshots(),
		BINDUnchangedConfig: unchanged,
	}
	source, err := dnsenginerecovery.CaptureBINDAdoptionSourceProof(ctx, dnsJournalPolicy(), provisional, bindroot.APT, gid, inventory, probeBINDAdoptionSourceSOA(address))
	if err != nil {
		return dnsEngineSwitchJournal{}, err
	}
	journal, err := dnsJournalPolicy().BuildBINDSwitchInverseJournalV2(base, "apt", configs.desiredSnapshots(), dnsengineartifact.BINDSwitchSourceProofV2{SourceBIND: &source, BINDUnchangedConfig: unchanged})
	if err != nil {
		return dnsEngineSwitchJournal{}, err
	}
	if err := verifyBINDAdoptionFrozenSource(ctx, journal, evidence); err != nil {
		return dnsEngineSwitchJournal{}, err
	}
	return journal, nil
}

// Config and zone files remain source evidence during takeover. The native
// inventory may legitimately gain managed zones after the config is installed.
func verifyBINDAdoptionFrozenSource(ctx context.Context, journal dnsEngineSwitchJournal, evidence bindAdoptionRuntimeEvidence) error {
	if journal.Schema != dnsengineartifact.SwitchJournalSchemaV2 || journal.InversePlan == nil || journal.InversePlan.SourceBIND == nil {
		return errors.New("BIND adoption lacks its frozen independent source proof")
	}
	gid, err := resolveBINDGroupGID(ctx)
	if err != nil {
		return err
	}
	if err := dnsenginerecovery.VerifyBINDAdoptionSourceFiles(ctx, dnsJournalPolicy(), journal, bindroot.APT, gid); err != nil {
		return err
	}
	address, err := dnsenginerecovery.ProbeAuthorityIPv4Address(ctx, "named", evidence.topology.namedProcesses.MainPID, dnsenginerecovery.SSListenerRunner)
	if err != nil {
		return err
	}
	if err := dnsenginerecovery.VerifyBINDAdoptionSourceAnswers(ctx, journal, probeBINDAdoptionSourceSOA(address)); err != nil {
		return fmt.Errorf("BIND adoption source answers: %w", err)
	}
	if err := dnsenginerecovery.VerifyBINDAdoptionSourceFiles(ctx, dnsJournalPolicy(), journal, bindroot.APT, gid); err != nil {
		return err
	}
	return nil
}

func applyBINDAdoptionWithFrozenSource(ctx context.Context, configs bindConfigMutation, journal dnsEngineSwitchJournal, evidence bindAdoptionRuntimeEvidence, verifyRuntime func() error) error {
	policy, err := resolveBINDConfigOwnerPolicy(ctx, configs.layout)
	if err != nil {
		return err
	}
	return configs.applyOwnerAwareWithPolicyAndOps(policy, bindConfigApplyOps{
		beforeCompensation: func() error {
			if err := verifyRuntime(); err != nil {
				return err
			}
			return verifyBINDAdoptionFrozenSource(ctx, journal, evidence)
		},
		write: func(path string, data []byte, mode os.FileMode, before *dnsFileSnapshot) error {
			if err := verifyRuntime(); err != nil {
				return err
			}
			if err := verifyBINDAdoptionFrozenSource(ctx, journal, evidence); err != nil {
				return err
			}
			return bindConfigWriterForPolicy(policy)(path, data, mode, before)
		},
	})
}

func restoreBINDAdoptionWithSourceGuard(ctx context.Context, configs bindConfigMutation, guard func() error) error {
	policy, err := resolveBINDConfigOwnerPolicy(ctx, configs.layout)
	if err != nil {
		return err
	}
	return configs.restoreOwnerAwareWithPolicyAndOps(policy, bindConfigRestoreOps{
		write: func(path string, data []byte, mode os.FileMode, before *dnsFileSnapshot) error {
			if err := guard(); err != nil {
				return err
			}
			return bindConfigWriterForPolicy(policy)(path, data, mode, before)
		},
	})
}
