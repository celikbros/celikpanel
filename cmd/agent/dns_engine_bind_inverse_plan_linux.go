//go:build linux

package main

import (
	"context"
	"errors"

	"github.com/alicelik/celikpanel/internal/bindroot"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
	"github.com/alicelik/celikpanel/internal/hostplatform"
	"github.com/alicelik/celikpanel/internal/transport"
)

// prepareBINDIndependentInverseJournal freezes the unchanged source before
// publishing an intent. It never upgrades an interrupted legacy journal.
func prepareBINDIndependentInverseJournal(ctx context.Context, profile hostplatform.Profile, base dnsEngineSwitchJournal, configs bindConfigMutation) (dnsEngineSwitchJournal, error) {
	if !requiresBINDIndependentSourceProof(profile, base) {
		return base, nil
	}
	if !configs.ownerAware {
		return dnsEngineSwitchJournal{}, errors.New("BIND inverse intent requires exact owner-aware config preimages")
	}
	policy, err := resolvePDNSConfigOwnerPolicy(ctx)
	if err != nil {
		return dnsEngineSwitchJournal{}, err
	}
	observations, err := captureHostPDNSConfigObservations(policy)
	if err != nil {
		return dnsEngineSwitchJournal{}, err
	}
	if err = validatePDNSConfigObservations(policy, observations); err != nil {
		return dnsEngineSwitchJournal{}, err
	}
	snapshots := make([]dnsFileSnapshot, len(observations))
	for i, observation := range observations {
		snapshots[i] = observation.Snapshot
	}
	database, err := dnsenginerecovery.CapturePDNSSourceDatabaseProof(ctx, pdnsDBPath())
	if err != nil {
		return dnsEngineSwitchJournal{}, err
	}
	proof := &dnsengineartifact.PDNSSourceProofV2{Kind: "pdns-source/v1", ConfigBefore: snapshots, Database: database}
	gid, err := resolveBINDGroupGID(ctx)
	if err != nil {
		return dnsEngineSwitchJournal{}, err
	}
	unchanged, err := dnsenginerecovery.CaptureInstalledBINDUnchangedConfigV2(ctx, bindroot.APT, gid)
	if err != nil {
		return dnsEngineSwitchJournal{}, err
	}
	journal, err := dnsJournalPolicy().BuildBINDSwitchInverseJournalV2(base, "apt", configs.desiredSnapshots(), dnsengineartifact.BINDSwitchSourceProofV2{SourcePDNS: proof, BINDUnchangedConfig: unchanged})
	if err != nil {
		return dnsEngineSwitchJournal{}, err
	}
	if err = verifyBINDIndependentSourceProof(ctx, journal); err != nil {
		return dnsEngineSwitchJournal{}, err
	}
	return journal, nil
}

func requiresBINDIndependentSourceProof(profile hostplatform.Profile, journal dnsEngineSwitchJournal) bool {
	if profile.PackageManager != hostplatform.PackageManagerAPT || journal.Mode != transport.DNSEngineSwitchModeSwitch ||
		journal.SourceEngine != transport.DNSEnginePowerDNS || journal.TargetEngine != transport.DNSEngineBIND ||
		journal.Topology != transport.DNSTopologyStandalone || !journal.StateBefore.Exists {
		return false
	}
	if len(journal.TargetUnitsBefore) != 2 || len(journal.SourceUnitsBefore) != 1 || journal.SourceUnitsBefore[0].Name != "pdns.service" || journal.SourceUnitsBefore[0].ActiveState != "active" {
		return false
	}
	seen := map[string]bool{}
	for _, unit := range journal.TargetUnitsBefore {
		if (unit.Name != "named.service" && unit.Name != "bind9.service") || unit.ActiveState != "inactive" || seen[unit.Name] {
			return false
		}
		seen[unit.Name] = true
	}
	return true
}

func verifyBINDIndependentSourceProof(ctx context.Context, journal dnsEngineSwitchJournal) error {
	if journal.Schema != dnsengineartifact.SwitchJournalSchemaV2 {
		return nil
	}
	if journal.InversePlan == nil || journal.InversePlan.SourcePDNS == nil {
		return errors.New("BIND rollback lacks its frozen PowerDNS source proof")
	}
	if err := dnsJournalPolicy().ValidateSwitchJournal(journal); err != nil {
		return err
	}
	policy, err := resolvePDNSConfigOwnerPolicy(ctx)
	if err != nil {
		return err
	}
	gid, err := resolveBINDGroupGID(ctx)
	if err != nil {
		return err
	}
	if err := dnsenginerecovery.VerifyInstalledBINDUnchangedConfigV2(ctx, dnsJournalPolicy(), journal, bindroot.APT, gid); err != nil {
		return err
	}
	proof := journal.InversePlan.SourcePDNS
	if _, err := capturePDNSConfigSnapshotsExactWithOps(policy, proof.ConfigBefore, hostPDNSConfigAccessOps()); err != nil {
		return err
	}
	if err := dnsenginerecovery.VerifyPDNSSourceDatabaseProof(ctx, pdnsDBPath(), proof.Database); err != nil {
		return err
	}
	_, err = capturePDNSConfigSnapshotsExactWithOps(policy, proof.ConfigBefore, hostPDNSConfigAccessOps())
	return err
}
