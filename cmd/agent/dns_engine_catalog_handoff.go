package main

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"sort"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/hostplatform"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

func requiresPrimaryCatalogSerial(
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) bool {
	return manifest.Topology == transport.DNSTopologyPaired &&
		manifest.PairRole == transport.DNSPairRolePrimary
}

func validatePrimaryCatalogSerialContract(
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	serial uint32,
) error {
	if requiresPrimaryCatalogSerial(manifest) {
		if serial == 0 {
			return errors.New("paired primary DNS engine state is missing its catalog serial")
		}
		return nil
	}
	if serial != 0 {
		return errors.New("non-primary DNS engine state unexpectedly binds a catalog serial")
	}
	return nil
}

func pairRoleForEngineState(
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) string {
	if manifest.Topology == transport.DNSTopologyPaired &&
		manifest.Mode == transport.DNSEngineSwitchModeSwitch {
		return manifest.PairRole
	}
	return ""
}

func validateEngineStateCatalogContract(
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	state dnsEngineStateReceipt,
) error {
	expectedRole := pairRoleForEngineState(manifest)
	legacySecondary := expectedRole == transport.DNSPairRoleSecondary &&
		state.PairRole == "" && state.PrimaryCatalogSerial == 0
	if state.PairRole != expectedRole && !legacySecondary {
		return errors.New("DNS engine state pair role differs from the switch manifest")
	}
	return validatePrimaryCatalogSerialContract(
		manifest, state.PrimaryCatalogSerial,
	)
}

func primaryCatalogManifestMembers(
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) []string {
	members := make([]string, 0, len(manifest.Zones))
	for _, zone := range manifest.Zones {
		if !zone.Delete {
			members = append(members, zone.Domain)
		}
	}
	sort.Strings(members)
	return members
}

func verifyPrimaryCatalogHandoffEvidenceAt(
	ctx context.Context,
	evidence dnsPrimaryCatalogEvidence,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	serial uint32,
	soa dnsZoneSOAProbe,
	axfr dnsCatalogAXFRProbe,
) error {
	if err := validatePrimaryCatalogSerialContract(manifest, serial); err != nil {
		return err
	}
	if err := validateDNSPrimaryCatalogEvidence(evidence); err != nil {
		return err
	}
	domain, err := binddns.CatalogDomain(manifest.LocalIP)
	if err != nil {
		return err
	}
	if evidence.LocalIP != manifest.LocalIP || evidence.PeerIP != manifest.PeerIP ||
		evidence.Domain != domain || evidence.Serial != serial ||
		!slices.Equal(evidence.Members, primaryCatalogManifestMembers(manifest)) {
		return errors.New("primary catalog durable evidence differs from the engine switch manifest")
	}
	if soa == nil || axfr == nil {
		return errors.New("primary catalog live proof is unavailable")
	}
	proofCtx, cancel := context.WithTimeout(ctx, dnsPairProofLimit)
	defer cancel()
	liveSerial, err := exactDNSZoneSerialAtWithProbe(
		proofCtx, evidence.LocalIP, evidence.Domain, soa,
	)
	if err != nil || liveSerial != serial {
		return errors.New("primary catalog live SOA differs from its durable serial")
	}
	live, err := axfr(proofCtx, evidence.LocalIP, evidence.Domain)
	if err != nil || live.Serial != serial ||
		!slices.Equal(live.Members, evidence.Members) {
		return errors.New("primary catalog live AXFR differs from its durable evidence")
	}
	return nil
}

func verifyManagedPDNSPairIdentity(
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) error {
	if manifest.Topology != transport.DNSTopologyPaired {
		return nil
	}
	if err := validateDNSClusterConfigTarget(); err != nil {
		return err
	}
	actual, err := dnsClusterConfigReadFile(dnsClusterConf)
	if err != nil {
		return err
	}
	expected := dnsClusterConfig(&DNSClusterRequest{
		Role: dnsRolePaired, PeerIP: manifest.PeerIP, PeerNS: manifest.PeerNS,
	})
	if string(actual) != expected {
		return errors.New("PowerDNS managed pair configuration differs from the switch manifest")
	}
	return nil
}

func primaryCatalogEvidenceForEngine(
	ctx context.Context,
	profile hostplatform.Profile,
	engine transport.DNSEngine,
	state dnsEngineStateReceipt,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) (dnsPrimaryCatalogEvidence, error) {
	if state.Engine != engine {
		return dnsPrimaryCatalogEvidence{}, errors.New("primary catalog source engine receipt changed")
	}
	switch engine {
	case transport.DNSEngineBIND:
		layout, err := bindLayout(profile)
		if err != nil {
			return dnsPrimaryCatalogEvidence{}, err
		}
		publisher, _, err := newHostBINDPublisher(layout)
		if err != nil {
			return dnsPrimaryCatalogEvidence{}, err
		}
		tree, err := publisher.LoadCurrent()
		if err != nil {
			return dnsPrimaryCatalogEvidence{}, err
		}
		receipt := tree.CurrentReceipt()
		if receipt.Generation != state.Generation || receipt.EngineEpoch != state.EngineEpoch {
			return dnsPrimaryCatalogEvidence{},
				errors.New("BIND primary catalog receipt differs from its active engine state")
		}
		pairing := receipt.Pairing
		if pairing == nil || pairing.Role != binddns.PairRolePrimary ||
			pairing.LocalIP != manifest.LocalIP || pairing.LocalNS != manifest.LocalNS ||
			pairing.PeerIP != manifest.PeerIP || pairing.PeerNS != manifest.PeerNS {
			return dnsPrimaryCatalogEvidence{},
				errors.New("BIND primary catalog pair identity differs from the switch manifest")
		}
		evidence, primary, err := bindPrimaryCatalogEvidence(tree)
		if err != nil {
			return dnsPrimaryCatalogEvidence{}, err
		}
		if !primary {
			return dnsPrimaryCatalogEvidence{}, errors.New("BIND source is not a catalog primary")
		}
		return evidence, nil
	case transport.DNSEnginePowerDNS:
		if err := verifyPDNSStateManifestReceipt(ctx, state); err != nil {
			return dnsPrimaryCatalogEvidence{}, err
		}
		if err := verifyManagedPDNSPairIdentity(manifest); err != nil {
			return dnsPrimaryCatalogEvidence{}, err
		}
		evidence, primary, err := managedPDNSPrimaryCatalogEvidence(ctx)
		if err != nil {
			return dnsPrimaryCatalogEvidence{}, err
		}
		if !primary {
			return dnsPrimaryCatalogEvidence{}, errors.New("PowerDNS source is not a managed catalog primary")
		}
		return evidence, nil
	default:
		return dnsPrimaryCatalogEvidence{}, errors.New("primary catalog source engine is unsupported")
	}
}

func verifyPDNSStateManifestReceipt(
	ctx context.Context,
	state dnsEngineStateReceipt,
) error {
	if state.Engine != transport.DNSEnginePowerDNS || state.Generation != "" {
		return errors.New("PowerDNS active engine state identity is invalid")
	}
	db, err := openPDNSEngineDB(pdnsDBPath(), true)
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var engine, requestID, ownerID, qualifier, schema string
	var epoch, sourceRevision int64
	if err := tx.QueryRowContext(ctx, `
		SELECT engine, engine_epoch, request_id, owner_id, qualifier,
		 source_revision, schema
		FROM celikpanel_dns_engine_manifest_receipt WHERE singleton = 1
	`).Scan(
		&engine, &epoch, &requestID, &ownerID, &qualifier,
		&sourceRevision, &schema,
	); err != nil {
		return err
	}
	if engine != string(transport.DNSEnginePowerDNS) ||
		epoch != state.EngineEpoch || requestID != state.MutationRequestID ||
		ownerID != state.MutationOwnerID || qualifier != state.ManifestQualifier ||
		sourceRevision != state.SourceRevision || schema != pdnsManifestSchema {
		return errors.New("PowerDNS database manifest receipt differs from its active engine state")
	}
	return tx.Commit()
}

func primaryCatalogSerialBoundBySourceState(
	state dnsEngineStateReceipt,
	durableSerial uint32,
) (uint32, error) {
	if durableSerial == 0 {
		return 0, errors.New("primary catalog durable serial is invalid")
	}
	if state.Mode != transport.DNSEngineSwitchModeSwitch {
		return 0, errors.New("primary catalog source receipt is not a directional engine switch")
	}
	switch {
	case state.PairRole == transport.DNSPairRolePrimary &&
		state.PrimaryCatalogSerial != 0:
		// The state serial is the last exact engine-handoff anchor. Ordinary
		// membership mutations advance the engine's durable catalog atomically,
		// but deliberately do not create a cross-resource state-file commit.
		// Catalog serials never wrap, so a durable value below the anchor is a
		// rollback/tamper signal; a greater value is proven live and becomes the
		// exact serial bound into the next switch journal and target receipt.
		if durableSerial < state.PrimaryCatalogSerial {
			return 0, errors.New("primary catalog durable serial predates its source receipt")
		}
		return durableSerial, nil
	case state.PairRole == "" && state.PrimaryCatalogSerial == 0:
		// v0.1.0-alpha.27 paired-primary receipts predate both fields. This
		// source-only compatibility path derives S from independently verified
		// durable and live authority, then binds it into the new journal/state.
		return durableSerial, nil
	default:
		return 0, errors.New("paired primary DNS source receipt has an incompatible pair role")
	}
}

func primaryCatalogSerialFromSource(
	ctx context.Context,
	profile hostplatform.Profile,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	state dnsEngineStateReceipt,
	stateExists bool,
) (uint32, error) {
	if !requiresPrimaryCatalogSerial(manifest) {
		if stateExists {
			if err := validateEngineStateCatalogContract(manifest, state); err != nil {
				return 0, err
			}
		}
		return 0, nil
	}
	if manifest.Mode != transport.DNSEngineSwitchModeSwitch {
		return 0, errors.New("paired primary catalog handoff requires switch mode")
	}
	if manifest.SourceEngine == "" {
		if stateExists || manifest.SourceEpoch != 0 {
			return 0, errors.New("initial primary catalog serial requires an empty source state")
		}
		return 1, nil
	}
	if !stateExists || state.EngineEpoch != manifest.SourceEpoch {
		return 0, errors.New("paired primary DNS source receipt is absent or changed")
	}
	evidence, err := primaryCatalogEvidenceForEngine(
		ctx, profile, manifest.SourceEngine, state, manifest,
	)
	if err != nil {
		return 0, err
	}
	serial, err := primaryCatalogSerialBoundBySourceState(
		state, evidence.Serial,
	)
	if err != nil {
		return 0, err
	}
	if err := verifyPrimaryCatalogHandoffEvidenceAt(
		ctx, evidence, manifest, serial,
		probeDNSZoneSOA, probeDNSCatalogAXFR,
	); err != nil {
		return 0, err
	}
	return serial, nil
}

func verifyCompletedPrimaryCatalogTarget(
	ctx context.Context,
	profile hostplatform.Profile,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	state dnsEngineStateReceipt,
) error {
	if err := validateEngineStateCatalogContract(manifest, state); err != nil {
		return err
	}
	if !requiresPrimaryCatalogSerial(manifest) {
		return nil
	}
	evidence, err := primaryCatalogEvidenceForEngine(
		ctx, profile, manifest.TargetEngine, state, manifest,
	)
	if err != nil {
		return err
	}
	return verifyPrimaryCatalogHandoffEvidenceAt(
		ctx, evidence, manifest, state.PrimaryCatalogSerial,
		probeDNSZoneSOA, probeDNSCatalogAXFR,
	)
}

func verifyRestoredDNSSwitchSource(
	ctx context.Context,
	profile hostplatform.Profile,
	systemctl string,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	journal dnsEngineSwitchJournal,
) error {
	switch journal.SourceEngine {
	case transport.DNSEnginePowerDNS:
		if err := verifyOnlyPDNSActive(ctx, systemctl); err != nil {
			return err
		}
	case transport.DNSEngineBIND:
		if err := verifyOnlyBINDActive(ctx, systemctl); err != nil {
			return err
		}
	case "":
		return verifyNoManagedDNSAuthority(ctx, systemctl, journal)
	default:
		return errors.New("restored DNS source engine is unsupported")
	}
	if !requiresPrimaryCatalogSerial(manifest) {
		return nil
	}
	state, exists, err := readDNSEngineState()
	if err != nil || !exists {
		if err == nil {
			err = errors.New("restored primary DNS source receipt is absent")
		}
		return err
	}
	serial, err := primaryCatalogSerialFromSource(ctx, profile, manifest, state, true)
	if err != nil {
		return err
	}
	if serial != journal.PrimaryCatalogSerial {
		return errors.New("restored primary catalog serial differs from the switch journal")
	}
	return nil
}
